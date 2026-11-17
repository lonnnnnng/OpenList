package strm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/product"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

type SourceError struct {
	Code    string
	Message string
	cause   error
}

func (e *SourceError) Error() string { return e.Message }
func (e *SourceError) Unwrap() error { return e.cause }

func sourceError(code, message string, cause error) error {
	return &SourceError{Code: code, Message: message, cause: cause}
}

// SourceBinding 记录非敏感源身份；人工编辑账号也会改变版本，令牌自动轮换不改变绑定。作者：long
type SourceBinding struct {
	StorageID   uint   `json:"storage_id"`
	Driver      string `json:"driver"`
	SourcePath  string `json:"source_path"`
	Fingerprint string `json:"-"`
}

// SourceNode 的原生对象只存在服务端，客户端提交的路径不能伪装成已扫描的对象。作者：long
type SourceNode struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	OriginalName string    `json:"original_name"`
	IsDir        bool      `json:"is_dir"`
	Size         int64     `json:"size"`
	Mtime        time.Time `json:"mtime"`
	RemoteID     string    `json:"remote_id"`
	native       model.Obj
	binding      string
}

type sourceSnapshot struct {
	record *model.Storage
	driver driver.Driver
	mounts []model.Storage
}
type sourceLookup func(context.Context, uint) (sourceSnapshot, error)

type Source struct {
	Binding  SourceBinding
	driver   driver.Driver
	lookup   sourceLookup
	selected SourceNode
}

func lookupSource(ctx context.Context, id uint) (sourceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return sourceSnapshot{}, err
	}
	record, err := db.GetStorageById(id)
	if err != nil {
		return sourceSnapshot{}, err
	}
	d, err := op.GetStorageByMountPath(record.MountPath)
	if err != nil {
		return sourceSnapshot{}, err
	}
	var mounts []model.Storage
	// long: 检查完整配置拓扑，禁用的子挂载也不能被后续启用后悄悄纳入同一任务。
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return sourceSnapshot{}, err
		}
		items, total, err := db.GetStorages(page, 100)
		if err != nil {
			return sourceSnapshot{}, err
		}
		mounts = append(mounts, items...)
		if int64(len(mounts)) >= total {
			break
		}
		if len(items) == 0 {
			return sourceSnapshot{}, errors.New("存储配置读取不完整")
		}
	}
	return sourceSnapshot{record: record, driver: d, mounts: mounts}, nil
}

func sourceFingerprint(snapshot sourceSnapshot, sourcePath string, id uint) (string, error) {
	s, d := snapshot.record, snapshot.driver
	if s == nil || d == nil || d.GetStorage() == nil || s.ID != id || d.GetStorage().ID != id || s.Disabled || d.GetStorage().Disabled || s.Status != op.WORK || d.GetStorage().Status != op.WORK {
		return "", sourceError("source_unavailable", "绑定存储未就绪，请检查账号和启用状态", nil)
	}
	if !conf.STRMDriverAllowed(s.Driver) || s.Driver != d.Config().Name || d.GetStorage().Driver != s.Driver || d.GetStorage().MountPath != s.MountPath || strings.Contains(s.MountPath, ".balance") || strings.HasPrefix(s.Remark, "ref:/") {
		return "", sourceError("source_boundary", "任务必须绑定单个物理存储，不能使用聚合或引用挂载", nil)
	}
	full := utils.GetFullPath(s.MountPath, sourcePath)
	for _, mount := range snapshot.mounts {
		if mount.ID == id {
			continue
		}
		other := utils.GetActualMountPath(mount.MountPath)
		// long: 选定源既不能包含子挂载，也不能落在子挂载下面，避免同名路径实际换成另一个账号。
		if other != s.MountPath && utils.IsSubPath(s.MountPath, other) && (utils.IsSubPath(full, other) || utils.IsSubPath(other, full)) {
			return "", sourceError("source_boundary", "所选目录与子挂载重叠，请拆成独立任务", nil)
		}
		if other == s.MountPath {
			return "", sourceError("source_boundary", "同一路径存在多个物理存储，请取消聚合后重试", nil)
		}
	}
	var scope struct {
		DriveType    string `json:"drive_type"`
		AlipanType   string `json:"alipan_type"`
		DownloadAPI  string `json:"download_api"`
		UseOnlineAPI bool   `json:"use_online_api"`
		APIAddress   string `json:"api_url_address"`
		OnlyVideo    bool   `json:"only_list_video_file"`
		Transcoded   bool   `json:"use_transcoding_address"`
		ShowHidden   bool   `json:"show_hidden"`
	}
	if json.Unmarshal([]byte(s.Addition), &scope) != nil {
		return "", sourceError("source_config", "存储附加配置无法读取，请重新保存配置", nil)
	}
	if err := validateSourceVisibility(s.Driver, scope.OnlyVideo, scope.ShowHidden); err != nil {
		return "", err
	}
	root := ""
	switch r := d.(type) {
	case driver.IRootId:
		root = r.GetRootId()
	case driver.IRootPath:
		root = r.GetRootPath()
		if s.Driver == "Local" {
			canonical, err := product.CanonicalPath(root)
			if err != nil {
				return "", sourceError("source_boundary", "本地测试根目录不能是符号链接", err)
			}
			root = canonical
		}
	}
	if root == "" {
		return "", sourceError("source_config", "存储根目录尚未配置", nil)
	}
	// long: Cookie/Token永不进入指纹；人工编辑的Modified用于保守检测同根换账号，自动刷新沿用原Modified。
	encoded, err := json.Marshal(struct {
		ID                                        uint
		Driver, Mount, Root, Path, ManualRevision string
		Scope                                     any
	}{s.ID, s.Driver, s.MountPath, root, sourcePath, s.Modified.UTC().Format(time.RFC3339Nano), scope})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validateSourceVisibility(driverName string, onlyVideo, showHidden bool) error {
	if onlyVideo {
		return sourceError("source_config", "严格扫描前请关闭存储的“只返回视频文件”开关", nil)
	}
	if driverName == "Local" && !showHidden {
		return sourceError("source_config", "本地目录需开启“显示隐藏文件”，保证扫描可见内容完整", nil)
	}
	return nil
}

// OpenSource 只进行身份/目录读取；任务接入时还须协调存储编辑与运行的生命周期互斥。作者：long
func OpenSource(ctx context.Context, storageID uint, sourcePath string) (*Source, error) {
	return openSource(ctx, storageID, sourcePath, lookupSource)
}

// BindSource 只验证非敏感存储身份与配置，不访问网盘；保存草稿不会触发目录读取。作者：long
func BindSource(ctx context.Context, storageID uint, sourcePath string) (SourceBinding, error) {
	if sourcePath == "" {
		sourcePath = "/"
	}
	if sourcePath != "/" {
		sourcePath = strings.TrimSuffix(sourcePath, "/")
	}
	snapshot, err := lookupSource(ctx, storageID)
	if err != nil {
		return SourceBinding{}, sourceError("source_unavailable", "无法读取绑定存储，请检查配置和连接", err)
	}
	fingerprint, err := sourceFingerprint(snapshot, sourcePath, storageID)
	if err != nil {
		return SourceBinding{}, err
	}
	return SourceBinding{StorageID: storageID, Driver: snapshot.record.Driver, SourcePath: sourcePath, Fingerprint: fingerprint}, nil
}

// OpenBoundSource 在显式预览/浏览时才联网，并确认保存草稿后源身份没有变化。作者：long
func OpenBoundSource(ctx context.Context, expected SourceBinding) (*Source, error) {
	source, err := openSource(ctx, expected.StorageID, expected.SourcePath, lookupSource)
	if err != nil {
		return nil, err
	}
	if source.Binding.Fingerprint != expected.Fingerprint || source.Binding.Driver != expected.Driver {
		return nil, sourceError("source_changed", "存储账号或源配置已改变，请重新预览并建立基线", nil)
	}
	return source, nil
}

func openSource(ctx context.Context, storageID uint, sourcePath string, lookup sourceLookup) (*Source, error) {
	if sourcePath == "" {
		sourcePath = "/"
	}
	if sourcePath != "/" {
		sourcePath = strings.TrimSuffix(sourcePath, "/")
	}
	snapshot, err := lookup(ctx, storageID)
	if err != nil {
		return nil, sourceError("source_unavailable", "无法读取绑定存储，请检查配置和连接", err)
	}
	fingerprint, err := sourceFingerprint(snapshot, sourcePath, storageID)
	if err != nil {
		return nil, err
	}
	source := &Source{Binding: SourceBinding{storageID, snapshot.record.Driver, sourcePath, fingerprint}, driver: snapshot.driver, lookup: lookup}
	object, err := op.GetFreshReadOnly(ctx, source.driver, sourcePath)
	if err != nil {
		return nil, source.readError(err)
	}
	if !object.IsDir() {
		return nil, sourceError("source_not_directory", "任务源必须是目录", nil)
	}
	if isSourceSymlink(object) {
		return nil, sourceError("source_boundary", "所选目录是符号链接，不能保证扫描边界安全", nil)
	}
	if err = source.check(ctx); err != nil {
		return nil, err
	}
	source.selected = source.node(sourcePath, object)
	return source, nil
}

func (s *Source) readError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return sourceError("canceled", "源读取已取消或超时，本轮结果不能用于缺失判断", err)
	}
	if errors.Is(err, op.ErrReadOnlyInvalidResult) {
		return sourceError("invalid_directory", "目录条目不完整或有歧义，请检查后重试", err)
	}
	if errs.IsNotFoundError(err) {
		return sourceError("source_path_not_found", "源目录不存在或已无法访问", err)
	}
	if errors.Is(err, errs.NotFolder) {
		return sourceError("source_not_directory", "源路径中的某一级不是目录", err)
	}
	return sourceError("source_read_failed", "源目录读取失败，请检查网盘连接后重试", err)
}

func (s *Source) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return s.readError(err)
	}
	snapshot, err := s.lookup(ctx, s.Binding.StorageID)
	if err != nil {
		return sourceError("source_unavailable", "绑定存储不可用，本轮结果不能用于缺失判断", err)
	}
	fingerprint, err := sourceFingerprint(snapshot, s.Binding.SourcePath, s.Binding.StorageID)
	if err != nil {
		return err
	}
	if fingerprint != s.Binding.Fingerprint || snapshot.driver != s.driver {
		return sourceError("source_changed", "存储账号或源配置已改变，请重新预览并建立基线", nil)
	}
	return nil
}

func (s *Source) node(relativePath string, object model.Obj) SourceNode {
	native := model.UnwrapObjName(object)
	return SourceNode{Path: relativePath, Name: object.GetName(), OriginalName: native.GetName(), IsDir: native.IsDir(), Size: native.GetSize(), Mtime: native.ModTime(), RemoteID: native.GetID(), native: native, binding: s.Binding.Fingerprint}
}

func (s *Source) Root() SourceNode { return s.selected }

// GetFreshFile 重走源根到文件的目录链并复核绑定，不从预览缓存中重建驱动对象。作者：long
func (s *Source) GetFreshFile(ctx context.Context, sourcePath string) (SourceNode, error) {
	if !utils.IsSubPath(s.Binding.SourcePath, sourcePath) || sourcePath == s.Binding.SourcePath {
		return SourceNode{}, sourceError("source_boundary", "文件路径不在绑定源目录内", nil)
	}
	if err := s.check(ctx); err != nil { return SourceNode{}, err }
	object, err := op.GetFreshReadOnly(ctx, s.driver, sourcePath)
	if err != nil { return SourceNode{}, s.readError(err) }
	if object.IsDir() || isSourceSymlink(object) { return SourceNode{}, sourceError("source_boundary", "播放目标不是普通文件", nil) }
	if err = s.check(ctx); err != nil { return SourceNode{}, err }
	return s.node(sourcePath, object), nil
}

func (s *Source) LinkFreshFile(ctx context.Context, sourcePath string, header http.Header) (*model.Link, model.Obj, error) {
	file, err := s.GetFreshFile(ctx, sourcePath)
	if err != nil { return nil, nil, err }
	link, err := s.driver.Link(ctx, file.native, model.LinkArgs{Header: header, Redirect: false})
	if err != nil { return nil, nil, s.readError(err) }
	if link == nil { return nil, nil, sourceError("link_failed", "网盘没有返回可用的播放链接", nil) }
	if err = s.check(ctx); err != nil { _ = link.Close(); return nil, nil, err }
	return link, file.native, nil
}

// List 在目录请求前后重查绑定，任何变化都丢弃结果，不能先用新账号生成再让旧manifest判断缺失。作者：long
func (s *Source) List(ctx context.Context, parent SourceNode) ([]SourceNode, error) {
	if parent.native == nil || parent.binding != s.Binding.Fingerprint || !utils.IsSubPath(s.Binding.SourcePath, parent.Path) {
		return nil, sourceError("source_boundary", "目录对象不属于本次源读取", nil)
	}
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	objects, err := op.ListFreshReadOnly(ctx, s.driver, parent.native)
	if err != nil {
		return nil, s.readError(err)
	}
	if err = s.check(ctx); err != nil {
		return nil, err
	}
	children := make([]SourceNode, 0, len(objects))
	for _, object := range objects {
		native := model.UnwrapObjName(object)
		if isSourceSymlink(object) {
			return nil, sourceError("source_boundary", "本地源目录包含符号链接，不能保证扫描边界安全", nil)
		}
		children = append(children, s.node(path.Join(parent.Path, native.GetName()), object))
	}
	return children, nil
}

func isSourceSymlink(object model.Obj) bool {
	native := model.UnwrapObjName(object)
	symlink, ok := native.(interface{ IsSourceSymlink() bool })
	return ok && symlink.IsSourceSymlink()
}
