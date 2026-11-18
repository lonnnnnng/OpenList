package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	stdpath "path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/fs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/internal/sign"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
)

var apiRootHolder atomic.Value

// SetAPIRoot 记录本次运行使用的站点地址（由 HTTP 层注入，供异步任务复用）
func SetAPIRoot(u string) {
	if u == "" {
		return
	}
	apiRootHolder.Store(strings.TrimSuffix(u, "/"))
}

// GetAPIRoot 返回当前站点地址
func GetAPIRoot() string {
	if v, ok := apiRootHolder.Load().(string); ok && v != "" {
		return v
	}
	return strings.TrimSuffix(conf.Conf.SiteURL, "/")
}

// StrmOptions STRM 生成参数
type StrmOptions struct {
	APIRoot    string // 站点地址前缀，例如 https://example.com
	Template   string // 内容模板
	Overwrite  bool   // 是否覆盖已存在文件
	SameDir    bool   // 是否与媒体文件同目录（同名 .strm）
	OutputPath string // 输出目录（VFS 路径，仅 SameDir=false 时使用）
}

// StrmTarget 一个待生成的 .strm 目标
type StrmTarget struct {
	RelPath     string // 相对输出目录的路径
	RawPath     string // 对应媒体文件的 VFS 路径
	SameDirPath string // 与媒体文件同目录时的 .strm 路径
}

// strmSibling 计算与媒体文件同目录、同名的 .strm 路径
func strmSibling(rawPath string, isDir bool) string {
	if isDir {
		return stdpath.Join(rawPath, stdpath.Base(rawPath)+".strm")
	}
	ext := stdpath.Ext(rawPath)
	if ext == "" {
		return rawPath + ".strm"
	}
	return strings.TrimSuffix(rawPath, ext) + ".strm"
}

// DefaultStrmOptions 从设置中读取 STRM 参数
func DefaultStrmOptions(apiRoot string) StrmOptions {
	return StrmOptions{
		APIRoot:    strings.TrimSuffix(apiRoot, "/"),
		Template:   setting.GetStr(conf.MediaStrmTemplate, "{site_url}/d{path}?sign={sign}"),
		Overwrite:  setting.GetBool(conf.MediaStrmOverwrite),
		SameDir:    setting.GetBool(conf.MediaStrmSameDir),
		OutputPath: setting.GetStr(conf.MediaStrmPath, "/strm"),
	}
}

// StrmOptionsForScanPath applies a path-specific output directory when configured.
func StrmOptionsForScanPath(opts StrmOptions, scanPath *model.MediaScanPath) StrmOptions {
	if scanPath != nil && strings.TrimSpace(scanPath.StrmPath) != "" {
		opts.OutputPath = strings.TrimSpace(scanPath.StrmPath)
		opts.SameDir = false
	}
	return opts
}

// RenderStrmContent 渲染 .strm 内容（单行 URL）
func RenderStrmContent(tpl, apiRoot, rawPath string) string {
	if tpl == "" {
		tpl = "{site_url}/d{path}?sign={sign}"
	}
	apiRoot = strings.TrimSuffix(apiRoot, "/")
	p := rawPath
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	replaced := strings.NewReplacer(
		"{site_url}", apiRoot,
		"{path}", utils.EncodePath(p, true),
		"{sign}", sign.Sign(rawPath),
		"{raw_path}", p,
		"{name}", stdpath.Base(p),
	).Replace(tpl)
	return strings.TrimSpace(replaced)
}

// sanitizeName 清洗文件/目录名中的非法字符
func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Unknown"
	}
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_",
		"\n", " ", "\r", " ",
	)
	s = replacer.Replace(s)
	if runes := []rune(s); len(runes) > 120 {
		s = string(runes[:120])
	}
	return strings.TrimSpace(s)
}

func itemTitle(item *model.MediaItem) string {
	if item.ScrapedName != "" {
		return item.ScrapedName
	}
	name := item.FileName
	if !item.IsFolder {
		name = strings.TrimSuffix(name, stdpath.Ext(name))
	}
	return name
}

func itemYear(item *model.MediaItem) string {
	if len(item.ReleaseDate) >= 4 {
		return item.ReleaseDate[:4]
	}
	return ""
}

func parseEpisodes(item *model.MediaItem) []model.EpisodeInfo {
	if item.Episodes == "" {
		return nil
	}
	var eps []model.EpisodeInfo
	if err := json.Unmarshal([]byte(item.Episodes), &eps); err != nil {
		return nil
	}
	return eps
}

// MediaItemFolder 条目所在目录
func MediaItemFolder(item *model.MediaItem) string {
	if item == nil {
		return "/"
	}
	folder := strings.TrimSuffix(item.FolderPath, "/")
	if folder == "" {
		folder = "/"
	}
	return folder
}

// StrmTargets 计算条目对应的 STRM 目标列表。
// 图片/书籍返回空列表（不生成 STRM）。
func StrmTargets(item *model.MediaItem) []StrmTarget {
	if item == nil {
		return nil
	}
	title := sanitizeName(itemTitle(item))
	year := itemYear(item)
	folder := MediaItemFolder(item)

	switch item.MediaType {
	case model.MediaTypeVideo:
		if item.VideoType == "tv" {
			series := title
			season := item.Season
			if season <= 0 {
				season = 1
			}
			var out []StrmTarget
			if item.IsFolder {
				for i, ep := range parseEpisodes(item) {
					num := ep.Index
					if num <= 0 {
						num = i + 1
					}
					epSeason := ep.Season
					if epSeason == 0 && !strings.Contains(ep.Title, "彩蛋") {
						epSeason = season
					}
					epTitle := sanitizeName(ep.Title)
					epRaw := stdpath.Join(folder, item.FileName, ep.FileName)
					out = append(out, StrmTarget{
						RelPath: stdpath.Join(series, fmt.Sprintf("Season %02d", epSeason),
							fmt.Sprintf("%s S%02dE%02d - %s.strm", series, epSeason, num, epTitle)),
						RawPath:     epRaw,
						SameDirPath: strmSibling(epRaw, false),
					})
				}
				return out
			}
			num := item.Episode
			if num <= 0 {
				num = 1
			}
			raw := stdpath.Join(folder, item.FileName)
			return []StrmTarget{{
				RelPath: stdpath.Join(series, fmt.Sprintf("Season %02d", season),
					fmt.Sprintf("%s S%02dE%02d.strm", series, season, num)),
				RawPath:     raw,
				SameDirPath: strmSibling(raw, false),
			}}
		}
		name := title
		if year != "" {
			name = fmt.Sprintf("%s (%s)", title, year)
		}
		raw := stdpath.Join(folder, item.FileName)
		return []StrmTarget{{
			RelPath:     stdpath.Join(name, name+".strm"),
			RawPath:     raw,
			SameDirPath: strmSibling(raw, item.IsFolder),
		}}
	case model.MediaTypeMusic:
		artist := sanitizeName(item.AlbumArtist)
		if artist == "" {
			artist = "Unknown Artist"
		}
		album := sanitizeName(item.AlbumName)
		if album == "" {
			album = "Unknown Album"
		}
		track := item.TrackNumber
		if track <= 0 {
			track = 1
		}
		raw := stdpath.Join(folder, item.FileName)
		return []StrmTarget{{
			RelPath:     stdpath.Join(artist, album, fmt.Sprintf("%02d - %s.strm", track, title)),
			RawPath:     raw,
			SameDirPath: strmSibling(raw, false),
		}}
	default:
		return nil
	}
}

// GenerateStrmForItems 为一组条目生成 STRM 文件，返回写入数量
func GenerateStrmForItems(ctx context.Context, items []model.MediaItem, opts StrmOptions) (int, error) {
	if !opts.SameDir && opts.OutputPath == "" {
		return 0, fmt.Errorf("strm output path is empty")
	}
	// 归一输出根目录，后续用它校验每个目标文件都在根目录之内
	outRoot := ""
	if !opts.SameDir {
		outRoot = stdpath.Clean("/" + strings.TrimSpace(opts.OutputPath))
	}
	written := 0
	touchedDirs := make(map[string]struct{})
	defer syncGeneratedStrmDirs(ctx, touchedDirs)
	for i := range items {
		item := &items[i]
		if item.Hidden {
			continue
		}
		for _, target := range StrmTargets(item) {
			dst := target.SameDirPath
			if !opts.SameDir {
				dst = stdpath.Join(outRoot, target.RelPath)
				// RelPath 含刮削得到的艺术家/专辑名等外部数据，必须保证不会借 ../ 逃出输出目录
				if dst != outRoot && !strings.HasPrefix(dst, strings.TrimSuffix(outRoot, "/")+"/") {
					continue
				}
			}
			if dst == "" {
				continue
			}
			dir := stdpath.Dir(dst)
			touchedDirs[dir] = struct{}{}
			if !opts.Overwrite {
				if _, err := fs.Get(ctx, dst, &fs.GetArgs{NoLog: true}); err == nil {
					continue
				}
			}
			data := []byte(RenderStrmContent(opts.Template, opts.APIRoot, target.RawPath))
			file := &stream.FileStream{
				Ctx: ctx,
				Obj: &model.Object{
					Name:     stdpath.Base(dst),
					Size:     int64(len(data)),
					Modified: time.Now(),
				},
				Reader:   bytes.NewReader(data),
				Mimetype: "text/plain",
			}
			if err := fs.PutDirectly(ctx, stdpath.Dir(dst), file, true); err != nil {
				return written, fmt.Errorf("write %s: %w", dst, err)
			}
			written++
		}
	}
	return written, nil
}

func filterShadowedMediaItems(items []model.MediaItem, scanPath *model.MediaScanPath) []model.MediaItem {
	if scanPath == nil || scanPath.MediaType != model.MediaTypeVideo || !scanPath.PathMerge {
		return items
	}
	mergedRoots := make([]string, 0)
	for i := range items {
		item := &items[i]
		if item.MediaType != model.MediaTypeVideo || item.VideoType != "tv" || !item.IsFolder || item.Hidden || len(parseEpisodes(item)) == 0 {
			continue
		}
		mergedRoots = append(mergedRoots, stdpath.Join(MediaItemFolder(item), item.FileName))
	}
	if len(mergedRoots) == 0 {
		return items
	}
	filtered := make([]model.MediaItem, 0, len(items))
	for i := range items {
		item := &items[i]
		itemPath := stdpath.Join(MediaItemFolder(item), item.FileName)
		shadowed := false
		for _, root := range mergedRoots {
			if itemPath != root && strings.HasPrefix(itemPath, strings.TrimSuffix(root, "/")+"/") {
				shadowed = true
				break
			}
		}
		if !shadowed {
			filtered = append(filtered, *item)
		}
	}
	return filtered
}

func syncGeneratedStrmDirs(ctx context.Context, dirs map[string]struct{}) {
	for dir := range dirs {
		objs, err := fs.List(ctx, dir, &fs.ListArgs{Refresh: true, NoLog: true})
		if err != nil {
			log.Errorf("[media] refresh STRM output directory %s failed: %v", dir, err)
			continue
		}
		op.HandleObjsUpdateHook(ctx, dir, objs)
	}
}

// GenerateStrmForType 按媒体类型（可选扫描路径）批量生成 STRM
func GenerateStrmForType(ctx context.Context, mediaType model.MediaType, scanPathID uint, opts StrmOptions) (int, error) {
	if scanPathID > 0 {
		return GenerateStrmForScanPath(ctx, mediaType, scanPathID, opts)
	}
	var items []model.MediaItem
	var err error
	items, err = db.ListAllMediaItems(mediaType)
	if err != nil {
		return 0, err
	}
	if mediaType != "" {
		filtered := make([]model.MediaItem, 0, len(items))
		for _, it := range items {
			if it.MediaType == mediaType {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	return GenerateStrmForItems(ctx, items, opts)
}

// GenerateStrmForAllPaths honors each scan path's STRM output override. Items
// without a scan path keep using the global STRM settings.
func GenerateStrmForAllPaths(ctx context.Context, mediaType model.MediaType, opts StrmOptions) (int, error) {
	paths, err := db.ListMediaScanPaths(mediaType)
	if err != nil {
		return 0, err
	}
	written := 0
	pathItemIDs := make(map[uint]struct{}, len(paths))
	for i := range paths {
		path := &paths[i]
		pathItemIDs[path.ID] = struct{}{}
		items, err := db.ListMediaItemsByScanPath(path.ID)
		if err != nil {
			return written, err
		}
		pathOpts := StrmOptionsForScanPath(opts, path)
		if !pathOpts.SameDir && pathOpts.OutputPath == "" {
			return written, fmt.Errorf("STRM output path is empty for scan path %d", path.ID)
		}
		items = filterShadowedMediaItems(filterMediaItems(items, mediaType), path)
		n, err := GenerateStrmForItems(ctx, items, pathOpts)
		written += n
		if err != nil {
			return written, err
		}
	}
	items, err := db.ListAllMediaItems(mediaType)
	if err != nil {
		return written, err
	}
	unscoped := make([]model.MediaItem, 0)
	for _, item := range items {
		if _, ok := pathItemIDs[item.ScanPathID]; !ok {
			unscoped = append(unscoped, item)
		}
	}
	n, err := GenerateStrmForItems(ctx, unscoped, opts)
	return written + n, err
}

func filterMediaItems(items []model.MediaItem, mediaType model.MediaType) []model.MediaItem {
	if mediaType == "" {
		return items
	}
	filtered := make([]model.MediaItem, 0, len(items))
	for _, item := range items {
		if item.MediaType == mediaType {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// TriggerStrmOnScanPaths generates STRM files independently for each scanned path.
func TriggerStrmOnScanPaths(paths []model.MediaScanPath, mediaType model.MediaType, apiRoot string) {
	if !setting.GetBool(conf.MediaStrmOnScan) {
		return
	}
	scanPaths := append([]model.MediaScanPath(nil), paths...)
	go func() {
		for i := range scanPaths {
			path := &scanPaths[i]
			if path.MediaType != mediaType {
				continue
			}
			opts := StrmOptionsForScanPath(DefaultStrmOptions(apiRoot), path)
			if !opts.SameDir && opts.OutputPath == "" {
				continue
			}
			n, err := GenerateStrmForScanPath(context.Background(), mediaType, path.ID, opts)
			if err != nil {
				log.Errorf("[media] generate strm for %s path %d failed: %+v", mediaType, path.ID, err)
				continue
			}
			log.Infof("[media] generated %d strm files for %s path %d", n, mediaType, path.ID)
		}
	}()
}

func GenerateStrmForScanPath(ctx context.Context, mediaType model.MediaType, scanPathID uint, opts StrmOptions) (int, error) {
	path, err := db.GetMediaScanPath(scanPathID)
	if err != nil {
		return 0, err
	}
	opts = StrmOptionsForScanPath(opts, path)
	items, err := db.ListMediaItemsByScanPath(scanPathID)
	if err != nil {
		return 0, err
	}
	return GenerateStrmForItems(ctx, filterShadowedMediaItems(filterMediaItems(items, mediaType), path), opts)
}
