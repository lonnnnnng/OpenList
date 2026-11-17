package strm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var defaultVideoExtensions = []string{".avi", ".m2ts", ".m4v", ".mkv", ".mov", ".mp4", ".mpeg", ".mpg", ".ts", ".webm", ".wmv"}
var supportedVideoExtensions = map[string]bool{
	".3gp": true, ".avi": true, ".flv": true, ".m2ts": true, ".m2v": true,
	".m4v": true, ".mkv": true, ".mov": true, ".mp4": true, ".mpeg": true,
	".mpg": true, ".ogv": true, ".ts": true, ".webm": true, ".wmv": true,
}

// sourceKey 用远端稳定ID优先识别同一文件；无ID驱动退回源内路径，并只持久化摘要。作者：long
func sourceKey(node SourceNode) string {
	identity := node.RemoteID
	if identity == "" {
		identity = "path:" + node.Path
	} else {
		identity = "id:" + identity
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

type TaskDraft struct {
	Name            string   `json:"name"`
	StorageID       uint     `json:"storage_id"`
	SourcePath      string   `json:"source_path"`
	OutputSubdir    string   `json:"output_subdir"`
	EntryPrefix     string   `json:"entry_prefix"`
	Extensions      []string `json:"extensions"`
	MinSize         int64    `json:"min_size"`
	Cron            string   `json:"cron"`
	Timezone        string   `json:"timezone"`
	ScheduleEnabled bool     `json:"schedule_enabled"`
	DeleteSync      bool     `json:"delete_sync"`
	TrashDays       int      `json:"trash_days"`
}

func canonicalDraft(ctx context.Context, input TaskDraft) (TaskDraft, SourceBinding, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) > 100 {
		return input, SourceBinding{}, sourceError("invalid_task", "任务名称需为1到100个字符", nil)
	}
	if input.StorageID == 0 {
		return input, SourceBinding{}, sourceError("invalid_task", "请选择网盘存储", nil)
	}
	if input.SourcePath == "" {
		input.SourcePath = "/"
	}
	if !strings.HasPrefix(input.SourcePath, "/") || strings.Contains(input.SourcePath, "\\") || path.Clean(input.SourcePath) != input.SourcePath {
		return input, SourceBinding{}, sourceError("invalid_task", "源目录必须是规范的存储内绝对路径", nil)
	}
	if input.OutputSubdir == "" || strings.Contains(input.OutputSubdir, "\\") || path.IsAbs(input.OutputSubdir) || path.Clean(input.OutputSubdir) != input.OutputSubdir || input.OutputSubdir == "." || input.OutputSubdir == ".." || strings.HasPrefix(input.OutputSubdir, "../") {
		return input, SourceBinding{}, sourceError("invalid_task", "输出目录必须是library下独占的相对路径", nil)
	}
	if input.EntryPrefix != "" {
		u, err := url.Parse(input.EntryPrefix)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return input, SourceBinding{}, sourceError("invalid_task", "播放入口前缀需为不含凭据的HTTP或HTTPS地址", nil)
		}
		input.EntryPrefix = strings.TrimSuffix(input.EntryPrefix, "/")
	}
	if input.MinSize < 0 {
		return input, SourceBinding{}, sourceError("invalid_task", "最小文件大小不能小于0", nil)
	}
	if input.Cron != "" || input.ScheduleEnabled || input.DeleteSync {
		return input, SourceBinding{}, sourceError("draft_only", "当前里程碑只保存草稿；定时和缺失回收尚未开放", nil)
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return input, SourceBinding{}, sourceError("invalid_task", "任务时区无效", err)
	}
	if input.TrashDays == 0 {
		input.TrashDays = 7
	}
	if input.TrashDays < 1 || input.TrashDays > 365 {
		return input, SourceBinding{}, sourceError("invalid_task", "回收保留天数需在1到365天之间", nil)
	}
	if len(input.Extensions) == 0 {
		input.Extensions = append([]string(nil), defaultVideoExtensions...)
	}
	seen := make(map[string]bool, len(input.Extensions))
	for i, extension := range input.Extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if !supportedVideoExtensions[extension] {
			return input, SourceBinding{}, sourceError("invalid_task", "仅支持已登记的视频扩展名", nil)
		}
		if seen[extension] {
			return input, SourceBinding{}, sourceError("invalid_task", "文件扩展名不能重复", nil)
		}
		seen[extension] = true
		input.Extensions[i] = extension
	}
	if len(input.Extensions) > 32 {
		return input, SourceBinding{}, sourceError("invalid_task", "单个任务最多选择32种扩展名", nil)
	}
	binding, err := BindSource(ctx, input.StorageID, input.SourcePath)
	return input, binding, err
}

// SaveTaskDraft 只校验本地绑定配置，不访问网盘或生成文件。作者：long
func SaveTaskDraft(ctx context.Context, input TaskDraft) (*model.STRMTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var binding SourceBinding
	var err error
	input, binding, err = canonicalDraft(ctx, input)
	if err != nil {
		return nil, err
	}
	task := &model.STRMTask{
		ID: uuid.NewString(), Name: input.Name, State: "draft", StorageID: input.StorageID, Driver: binding.Driver,
		SourcePath: binding.SourcePath, SourceFingerprint: binding.Fingerprint,
		OutputSubdir: input.OutputSubdir, EntryPrefix: input.EntryPrefix,
		Extensions: input.Extensions, MinSize: input.MinSize, Cron: "", Timezone: input.Timezone,
		ScheduleEnabled: false, DeleteSync: false, TrashDays: input.TrashDays,
		ConfigRevision: 1, BaselineRevision: 0,
	}
	if err = db.CreateSTRMTask(task); err != nil {
		return nil, sourceError("output_conflict", "保存草稿失败，输出目录可能已被其他任务占用", err)
	}
	return task, nil
}

func UpdateTaskDraft(ctx context.Context, current *model.STRMTask, revision uint64, input TaskDraft) (*model.STRMTask, error) {
	if current == nil || current.State != "draft" {
		return nil, sourceError("task_state", "只有草稿任务可以修改", nil)
	}
	if current.ConfigRevision != revision {
		return nil, sourceError("revision_conflict", "任务已被其他请求修改，请刷新后重试", nil)
	}
	input, binding, err := canonicalDraft(ctx, input)
	if err != nil {
		return nil, err
	}
	updated := *current
	updated.Name, updated.StorageID, updated.Driver, updated.SourcePath = input.Name, input.StorageID, binding.Driver, binding.SourcePath
	updated.SourceFingerprint, updated.OutputSubdir, updated.EntryPrefix = binding.Fingerprint, input.OutputSubdir, input.EntryPrefix
	updated.Extensions, updated.MinSize, updated.Timezone, updated.TrashDays = input.Extensions, input.MinSize, input.Timezone, input.TrashDays
	updated.ConfigRevision++
	if err = db.UpdateSTRMTask(&updated); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, sourceError("revision_conflict", "任务已被其他请求修改，请刷新后重试", err)
		}
		// long: 数据库约束错误只进受脱敏保护的管理员日志，HTTP响应不回显驱动配置或SQL细节。
		utils.Log.Errorf("STRM draft update failed: %v", err)
		return nil, sourceError("task_update_failed", "更新草稿失败，请重试或检查任务状态", err)
	}
	return &updated, nil
}

type PreviewItem struct {
	SourcePath string    `json:"source_path"`
	OutputPath string    `json:"output_path"`
	RemoteID   string    `json:"remote_id"`
	Size       int64     `json:"size"`
	Mtime      time.Time `json:"mtime"`
	SourceKey  string    `json:"-"`
}
type PreviewIssue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type TaskPreview struct {
	RunID          string         `json:"run_id"`
	TaskID         string         `json:"task_id"`
	Complete       bool           `json:"complete"`
	ReadOnly       bool           `json:"read_only"`
	Directories    int            `json:"directories"`
	Files          int64          `json:"files"`
	Candidates     int64          `json:"candidates"`
	Items          []PreviewItem  `json:"items"`
	ItemsTruncated bool           `json:"items_truncated"`
	Issues         []PreviewIssue `json:"issues"`
}

type previewReader interface {
	Root() SourceNode
	List(context.Context, SourceNode) ([]SourceNode, error)
}

// PreviewTask 只读扫描并返回候选清单；任一目录失败会持久化partial，永不推导缺失或回收。作者：long
func PreviewTask(ctx context.Context, task *model.STRMTask) (*TaskPreview, error) {
	if task == nil || (task.State != "draft" && task.State != "active") {
		return nil, sourceError("task_state", "当前任务状态不允许预览", nil)
	}
	started := time.Now()
	result := &TaskPreview{RunID: uuid.NewString(), TaskID: task.ID, Complete: false, ReadOnly: true, Items: []PreviewItem{}, Issues: []PreviewIssue{}}
	bound := SourceBinding{StorageID: task.StorageID, Driver: task.Driver, SourcePath: task.SourcePath, Fingerprint: task.SourceFingerprint}
	if bound.Driver == "" {
		// long: Driver字段由当前存储绑定重新取得，任务持久化不把敏感Addition复制到快照。
		binding, err := BindSource(ctx, task.StorageID, task.SourcePath)
		if err != nil {
			result.Issues = append(result.Issues, previewIssue("/", err))
			return persistPreview(task, result, started)
		}
		bound.Driver = binding.Driver
	}
	source, err := OpenBoundSource(ctx, bound)
	if err != nil {
		result.Issues = append(result.Issues, previewIssue(task.SourcePath, err))
		return persistPreview(task, result, started)
	}
	result = buildTaskPreview(ctx, task, source, result.RunID)
	return persistPreview(task, result, started)
}

func previewIssue(directory string, err error) PreviewIssue {
	var sourceErr *SourceError
	if errors.As(err, &sourceErr) {
		return PreviewIssue{Path: directory, Code: sourceErr.Code, Message: sourceErr.Message}
	}
	return PreviewIssue{Path: directory, Code: "source_read_failed", Message: "源目录读取失败，本轮结果不完整"}
}

func generationIssue(path string, err error) PreviewIssue {
	var sourceErr *SourceError
	if errors.As(err, &sourceErr) {
		return PreviewIssue{Path: path, Code: sourceErr.Code, Message: sourceErr.Message}
	}
	if errors.Is(err, ErrArtifactConflict) {
		return PreviewIssue{Path: path, Code: "output_conflict", Message: "目标文件或manifest内容已变化，原文件已保留"}
	}
	return PreviewIssue{Path: path, Code: "write_failed", Message: "此文件未安全生成；请检查冲突或重试"}
}

func buildTaskPreview(ctx context.Context, task *model.STRMTask, source previewReader, runID string) *TaskPreview {
	result := &TaskPreview{RunID: runID, TaskID: task.ID, Complete: true, ReadOnly: true, Items: []PreviewItem{}, Issues: []PreviewIssue{}}
	var err error
	type pending struct {
		node  SourceNode
		depth int
	}
	queue := []pending{{node: source.Root()}}
	visited := make(map[string]bool)
	allowed := make(map[string]bool, len(task.Extensions))
	for _, ext := range task.Extensions {
		allowed[strings.ToLower(ext)] = true
	}
	const maxDirectories, maxDepth, maxSamples = 10000, 128, 1000
	for len(queue) > 0 {
		if err = ctx.Err(); err != nil {
			result.Complete = false
			result.Issues = append(result.Issues, PreviewIssue{Code: "canceled", Message: "预览已取消，不能用于缺失判断"})
			break
		}
		current := queue[0]
		queue = queue[1:]
		if visited[current.node.Path] {
			result.Complete = false
			result.Issues = append(result.Issues, PreviewIssue{Path: current.node.Path, Code: "cycle", Message: "目录路径重复，已停止遍历该分支"})
			continue
		}
		visited[current.node.Path] = true
		result.Directories++
		if result.Directories > maxDirectories || current.depth > maxDepth {
			result.Complete = false
			result.Issues = append(result.Issues, PreviewIssue{Path: current.node.Path, Code: "limit", Message: "预览达到目录或层级上限，结果不完整"})
			break
		}
		children, listErr := source.List(ctx, current.node)
		if listErr != nil {
			result.Complete = false
			result.Issues = append(result.Issues, previewIssue(current.node.Path, listErr))
			continue
		}
		for _, child := range children {
			if child.IsDir {
				queue = append(queue, pending{node: child, depth: current.depth + 1})
				continue
			}
			result.Files++
			if child.Size < task.MinSize || !allowed[strings.ToLower(filepath.Ext(child.OriginalName))] {
				continue
			}
			result.Candidates++
			if len(result.Items) < maxSamples {
				relative := strings.TrimPrefix(child.Path, strings.TrimSuffix(task.SourcePath, "/"))
				relative = strings.TrimPrefix(relative, "/")
				result.Items = append(result.Items, PreviewItem{SourcePath: child.Path, OutputPath: path.Join(task.OutputSubdir, relative) + ".strm", RemoteID: child.RemoteID, Size: child.Size, Mtime: child.Mtime, SourceKey: sourceKey(child)})
			} else {
				result.ItemsTruncated = true
			}
		}
	}
	return result
}

func persistPreview(task *model.STRMTask, result *TaskPreview, started time.Time) (*TaskPreview, error) {
	finished := time.Now()
	var err error
	run := &model.STRMRun{ID: result.RunID, TaskID: task.ID, Trigger: "manual_preview", Preview: true, ConfigRevision: task.ConfigRevision, Status: "succeeded", Complete: result.Complete, Added: result.Candidates, Skipped: result.Files - result.Candidates, Failed: int64(len(result.Issues)), StartedAt: &started, FinishedAt: &finished, CreatedAt: started, UpdatedAt: finished}
	if !result.Complete {
		run.Status, run.ErrorCode, run.Error = "partial", "preview_incomplete", "只读预览不完整；不允许生成或缺失判断"
	}
	if err = db.CreateSTRMRun(run); err != nil {
		return nil, fmt.Errorf("保存预览记录失败: %w", err)
	}
	return result, nil
}

func GetTask(id string) (*model.STRMTask, error) {
	task, err := db.GetSTRMTask(id)
	if err != nil {
		return nil, err
	}
	return task, nil
}

type GenerationResult struct {
	RunID      string         `json:"run_id"`
	TaskID     string         `json:"task_id"`
	Generation uint64         `json:"generation"`
	Complete   bool           `json:"complete"`
	Status     string         `json:"status"`
	Candidates int64          `json:"candidates"`
	Added      int64          `json:"added"`
	Updated    int64          `json:"updated"`
	Unchanged  int64          `json:"unchanged"`
	Missing    int64          `json:"missing_confirmed"`
	Failed     int64          `json:"failed"`
	Issues     []PreviewIssue `json:"issues"`
}

type MissingPreviewItem struct {
	SourcePath           string `json:"source_path"`
	OutputPath           string `json:"output_path"`
	Size                 int64  `json:"size"`
	MissingConfirmations int    `json:"missing_confirmations"`
	OutputState          string `json:"output_state"`
}

type MissingPreview struct {
	TaskID                 string               `json:"task_id"`
	Generation             uint64               `json:"generation"`
	LastCompleteGeneration uint64               `json:"last_complete_generation"`
	TotalManaged           int64                `json:"total_managed"`
	TotalMissing           int64                `json:"total_missing"`
	MassDisappearance      bool                 `json:"mass_disappearance"`
	ItemsTruncated         bool                 `json:"items_truncated"`
	Items                  []MissingPreviewItem `json:"items"`
}

type TrashPlanResult struct {
	PlanID            string    `json:"plan_id"`
	ExpiresAt         time.Time `json:"expires_at"`
	Generation        uint64    `json:"generation"`
	Eligible          int64     `json:"eligible"`
	Blocked           int64     `json:"blocked"`
	MassDisappearance bool      `json:"mass_disappearance"`
}

type TrashExecutionResult struct {
	PlanID    string `json:"plan_id"`
	Status    string `json:"status"`
	Committed int64  `json:"committed"`
	Failed    int64  `json:"failed"`
}

func CreateTrashPlan(ctx context.Context, task *model.STRMTask) (*TrashPlanResult, error) {
	if task == nil || task.State != "active" {
		return nil, sourceError("task_state", "只有已生成任务可以创建回收预览", nil)
	}
	if task.ActiveRunID != nil || task.LastCompleteGeneration == 0 {
		return nil, sourceError("baseline_unavailable", "任务正在运行或尚无完整扫描基线", nil)
	}
	const batchSize = 100
	const maxPlanItems = 10000
	offset := 0
	var missingTotal, managedTotal int64
	items := []model.STRMTrashPlanItem{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, sourceError("canceled", "回收预览已取消", err)
		}
		files, totalMissing, totalManaged, err := db.ListSTRMConfirmedMissingFiles(task.ID, offset, batchSize)
		if err != nil {
			return nil, err
		}
		missingTotal, managedTotal = totalMissing, totalManaged
		for _, file := range files {
			artifact, artifactErr := db.GetSTRMArtifactByFileID(task.ID, file.ID)
			if errors.Is(artifactErr, gorm.ErrRecordNotFound) {
				continue
			}
			if artifactErr != nil {
				return nil, artifactErr
			}
			if artifact.State != "active" || file.MissingConfirmations < 2 || file.LastSeenGeneration >= task.LastCompleteGeneration {
				continue
			}
			exists, matches, hashErr := STRMHashState(artifact.OutputPath, artifact.ContentHash)
			if hashErr != nil || !exists || !matches {
				continue
			}
			if len(items) >= maxPlanItems {
				return nil, sourceError("trash_plan_limit", "本次回收候选超过安全计划上限，请缩小任务范围后重新预览", nil)
			}
			actionID := uuid.NewString()
			trashPath := path.Join(task.ID, actionID, path.Base(artifact.OutputPath))
			items = append(items, model.STRMTrashPlanItem{
				ID: uuid.NewString(), PlanID: "", ArtifactID: artifact.ID, ActionID: actionID,
				OutputPath: artifact.OutputPath, TrashPath: trashPath, ExpectedHash: artifact.ContentHash,
				OutputState: "ready_for_review", Status: "planned",
			})
		}
		offset += len(files)
		if offset >= int(totalMissing) || len(files) == 0 {
			break
		}
	}
	created := time.Now()
	plan := &model.STRMTrashPlan{
		ID: uuid.NewString(), TaskID: task.ID, ConfigRevision: task.ConfigRevision,
		Generation: task.Generation, LastCompleteGeneration: task.LastCompleteGeneration,
		TotalManaged: managedTotal, TotalMissing: missingTotal, Eligible: int64(len(items)),
		Blocked: missingTotal - int64(len(items)), MassDisappearance: massDisappearanceExceeded(missingTotal, managedTotal),
		Status: "previewed", ExpiresAt: created.Add(15 * time.Minute), CreatedAt: created, UpdatedAt: created,
	}
	for i := range items {
		items[i].PlanID = plan.ID
		items[i].CreatedAt, items[i].UpdatedAt = created, created
	}
	if err := db.CreateSTRMTrashPlan(plan, items); err != nil {
		return nil, sourceError("trash_plan_stale", "任务状态已变化，请重新生成缺失预览", err)
	}
	return &TrashPlanResult{PlanID: plan.ID, ExpiresAt: plan.ExpiresAt, Generation: plan.Generation, Eligible: plan.Eligible, Blocked: plan.Blocked, MassDisappearance: plan.MassDisappearance}, nil
}

func TrashPlans(taskID string, offset, limit int) ([]model.STRMTrashPlan, int64, error) {
	return db.ListSTRMTrashPlans(taskID, offset, limit)
}

func ExecuteTrashPlan(ctx context.Context, taskID, planID string, confirmMass bool) (*TrashExecutionResult, error) {
	plan, err := db.ClaimSTRMTrashPlan(planID, taskID, confirmMass)
	if err != nil {
		return nil, sourceError("trash_plan_stale", "回收预览已过期、被使用或任务状态已变化，请重新预览", err)
	}
	items, err := db.ListSTRMTrashPlanItems(plan.ID)
	if err != nil {
		_ = db.FinishSTRMTrashPlan(plan.ID, "partial")
		return nil, err
	}
	if len(items) == 0 {
		_ = db.FinishSTRMTrashPlan(plan.ID, "blocked")
		return &TrashExecutionResult{PlanID: plan.ID, Status: "blocked"}, nil
	}
	result := &TrashExecutionResult{PlanID: plan.ID, Status: "committed"}
	for _, item := range items {
		if item.Status == "committed" {
			result.Committed++
			continue
		}
		if ctx.Err() != nil {
			result.Failed++
			result.Status = "partial"
			break
		}
		planItem, _, beginErr := db.BeginSTRMTrashItem(plan.ID, item.ID, item.ActionID)
		if beginErr != nil {
			result.Failed++
			result.Status = "partial"
			_ = db.UpdateSTRMTrashPlanItem(item.ID, "blocked", "manifest_changed")
			continue
		}
		moveErr := MoveSTRMToTrash(ctx, planItem.OutputPath, planItem.TrashPath, planItem.ExpectedHash)
		if moveErr != nil {
			result.Failed++
			result.Status = "partial"
			continue
		}
		if err = db.CommitSTRMTrashItem(plan.ID, item.ID, planItem.ActionID, time.Now()); err != nil {
			result.Failed++
			result.Status = "partial"
			continue
		}
		result.Committed++
	}
	if err = db.FinishSTRMTrashPlan(plan.ID, result.Status); err != nil {
		return nil, err
	}
	return result, nil
}

func ListTrash(ctx context.Context, taskID string, offset, limit int) ([]model.STRMArtifact, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if _, err := db.GetSTRMTask(taskID); err != nil {
		return nil, 0, err
	}
	return db.ListSTRMTrashedArtifacts(taskID, offset, limit)
}

func RestoreTrashArtifact(ctx context.Context, taskID, artifactID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	artifact, err := db.GetSTRMArtifactByID(artifactID)
	if err != nil || artifact.TaskID != taskID || artifact.State != "trashed" {
		return sourceError("trash_item_unavailable", "回收项不存在或状态已变化", err)
	}
	item, action, err := db.BeginSTRMRestore(artifactID, uuid.NewString())
	if err != nil {
		return sourceError("restore_conflict", "回收项正在恢复或状态已变化", err)
	}
	if err = RestoreSTRMFromTrash(ctx, item.TrashPath, item.OutputPath, item.ContentHash); err != nil {
		return sourceError("restore_conflict", "恢复目标已存在、回收内容已改变或路径不安全；未覆盖任何文件", err)
	}
	if err = db.CommitSTRMRestore(item, action.ID, time.Now()); err != nil {
		return sourceError("restore_commit_failed", "文件已还原但状态提交失败，请再次尝试恢复以完成对账", err)
	}
	return nil
}

func massDisappearanceExceeded(missing, managed int64) bool {
	return missing >= 10 && managed > 0 && missing > managed/5
}

// PreviewMissingFiles 只核对两轮完整缺失记录和本地产物摘要；预览本身不改变manifest或文件。作者：long
func PreviewMissingFiles(ctx context.Context, task *model.STRMTask, offset, limit int) (*MissingPreview, error) {
	if task == nil || (task.State != "active" && task.State != "draft") {
		return nil, sourceError("task_state", "当前任务状态不允许查看缺失预览", nil)
	}
	if task.ActiveRunID != nil {
		return nil, sourceError("run_active", "任务正在运行，请等待本轮完成后再查看缺失预览", nil)
	}
	files, totalMissing, totalManaged, err := db.ListSTRMConfirmedMissingFiles(task.ID, offset, limit)
	if err != nil {
		return nil, err
	}
	result := &MissingPreview{
		TaskID: task.ID, Generation: task.Generation, LastCompleteGeneration: task.LastCompleteGeneration,
		TotalManaged: totalManaged, TotalMissing: totalMissing,
		MassDisappearance: massDisappearanceExceeded(totalMissing, totalManaged), ItemsTruncated: int64(offset+len(files)) < totalMissing, Items: []MissingPreviewItem{},
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, sourceError("canceled", "缺失预览已取消", err)
		}
		item := MissingPreviewItem{SourcePath: file.SourcePath, Size: file.Size, MissingConfirmations: file.MissingConfirmations, OutputState: "manifest_missing"}
		artifact, artifactErr := db.GetSTRMArtifactByFileID(task.ID, file.ID)
		if artifactErr == nil {
			item.OutputPath = artifact.OutputPath
			if artifact.State != "active" {
				item.OutputState = "artifact_inactive"
			} else if exists, matches, hashErr := STRMHashState(artifact.OutputPath, artifact.ContentHash); hashErr != nil {
				item.OutputState = "output_conflict"
			} else if !exists {
				item.OutputState = "output_missing"
			} else if !matches {
				item.OutputState = "output_modified"
			} else {
				item.OutputState = "ready_for_review"
			}
		} else if !errors.Is(artifactErr, gorm.ErrRecordNotFound) {
			return nil, artifactErr
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

type generationCandidate struct {
	node       SourceNode
	outputPath string
}

func playbackBase(task *model.STRMTask) (string, error) {
	base := strings.TrimSpace(task.PublicURL)
	if base == "" {
		base = strings.TrimSpace(task.EntryPrefix)
	}
	if base == "" && conf.Conf != nil {
		base = strings.TrimSpace(conf.Conf.STRM.PublicURL)
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", sourceError("public_url_required", "请配置媒体服务器可访问的STRM播放入口地址", nil)
	}
	return strings.TrimRight(base, "/"), nil
}

func playbackEntry(base, fileID string, version uint64) (string, error) {
	signature, err := SignPlayback(fileID, version)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", sourceError("public_url_invalid", "播放入口地址无法解析", nil)
	}
	serverPath := ""
	if conf.URL != nil {
		serverPath = strings.TrimRight(conf.URL.Path, "/")
	}
	if serverPath != "" && serverPath != "/" && !strings.HasSuffix(strings.TrimRight(u.Path, "/"), serverPath) {
		u.Path = strings.TrimRight(u.Path, "/") + serverPath
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/public/strm/" + url.PathEscape(fileID)
	query := url.Values{"v": {fmt.Sprint(version)}, "sig": {signature}}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func scanGeneration(ctx context.Context, task *model.STRMTask, source previewReader) ([]generationCandidate, int64, bool, []PreviewIssue) {
	type pending struct {
		node  SourceNode
		depth int
	}
	queue := []pending{{node: source.Root()}}
	visited := make(map[string]bool)
	allowed := make(map[string]bool, len(task.Extensions))
	for _, extension := range task.Extensions {
		allowed[strings.ToLower(extension)] = true
	}
	const maxDirectories, maxDepth, maxCandidates = 10000, 128, 100000
	var candidates []generationCandidate
	var files int64
	complete := true
	issues := []PreviewIssue{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, files, false, append(issues, PreviewIssue{Code: "canceled", Message: "扫描已取消，本轮没有写入文件"})
		}
		current := queue[0]
		queue = queue[1:]
		if visited[current.node.Path] {
			complete = false
			issues = append(issues, PreviewIssue{Path: current.node.Path, Code: "cycle", Message: "目录路径重复，无法确认扫描完整性"})
			continue
		}
		visited[current.node.Path] = true
		if len(visited) > maxDirectories || current.depth > maxDepth {
			return nil, files, false, append(issues, PreviewIssue{Path: current.node.Path, Code: "limit", Message: "目录或层级超过单轮生成上限，本轮没有写入文件"})
		}
		children, err := source.List(ctx, current.node)
		if err != nil {
			complete = false
			issues = append(issues, previewIssue(current.node.Path, err))
			continue
		}
		for _, child := range children {
			if child.IsDir {
				queue = append(queue, pending{node: child, depth: current.depth + 1})
				continue
			}
			files++
			if child.Size < task.MinSize || !allowed[strings.ToLower(filepath.Ext(child.OriginalName))] {
				continue
			}
			if len(candidates) >= maxCandidates {
				return nil, files, false, append(issues, PreviewIssue{Path: child.Path, Code: "limit", Message: "候选文件超过单轮生成上限，本轮没有写入文件"})
			}
			relative := strings.TrimPrefix(child.Path, strings.TrimSuffix(task.SourcePath, "/"))
			relative = strings.TrimPrefix(relative, "/")
			outputPath := path.Join(task.OutputSubdir, relative) + ".strm"
			if !outputPathValid(outputPath) {
				complete = false
				issues = append(issues, PreviewIssue{Path: child.Path, Code: "invalid_output", Message: "输出路径不符合安全边界"})
				continue
			}
			candidates = append(candidates, generationCandidate{node: child, outputPath: outputPath})
		}
	}
	return candidates, files, complete, issues
}

func GenerateTask(ctx context.Context, task *model.STRMTask) (*GenerationResult, error) {
	if task == nil || (task.State != "draft" && task.State != "active") {
		return nil, sourceError("task_state", "当前任务状态不允许生成", nil)
	}
	if !Initialized() {
		return nil, sourceError("runtime_not_ready", "STRM运行时尚未就绪", nil)
	}
	base, err := playbackBase(task)
	if err != nil {
		return nil, err
	}
	runID := uuid.NewString()
	started := time.Now()
	run := &model.STRMRun{ID: runID, TaskID: task.ID, Trigger: "manual_generate", Status: "running", ConfigRevision: task.ConfigRevision, StartedAt: &started, CreatedAt: started, UpdatedAt: started}
	if err = db.StartSTRMRun(task.ID, run); err != nil {
		return nil, sourceError("run_conflict", "任务正在运行或状态已变化，请刷新后重试", err)
	}
	result := &GenerationResult{RunID: runID, TaskID: task.ID, Status: "failed", Issues: []PreviewIssue{}}
	result.Generation = run.Generation
	finished := false
	defer func() {
		if !finished {
			finishTime := time.Now()
			run.Status, run.ErrorCode, run.Error = "failed", "generate_interrupted", "生成提前结束，未完成操作需人工检查"
			run.FinishedAt, run.UpdatedAt = &finishTime, finishTime
			_ = db.FinishSTRMRun(task.ID, runID, run, false)
		}
	}()

	bound := SourceBinding{StorageID: task.StorageID, Driver: task.Driver, SourcePath: task.SourcePath, Fingerprint: task.SourceFingerprint}
	source, err := OpenBoundSource(ctx, bound)
	if err != nil {
		result.Issues = append(result.Issues, previewIssue(task.SourcePath, err))
		result.Failed = 1
	} else {
		candidates, _, complete, issues := scanGeneration(ctx, task, source)
		result.Complete, result.Candidates = complete, int64(len(candidates))
		result.Issues = append(result.Issues, issues...)
		if !complete {
			result.Status = "partial"
			result.Failed = int64(len(issues))
		} else {
			for _, candidate := range candidates {
				if ctx.Err() != nil {
					result.Issues = append(result.Issues, PreviewIssue{Path: candidate.node.Path, Code: "canceled", Message: "生成已取消，剩余文件未处理"})
					result.Failed += 1
					break
				}
				outcome, generateErr := generateCandidate(ctx, task, source, candidate, runID, run.Generation, base)
				if generateErr != nil {
					result.Failed++
					result.Issues = append(result.Issues, generationIssue(candidate.node.Path, generateErr))
					continue
				}
				switch outcome {
				case "added":
					result.Added++
				case "updated":
					result.Updated++
				case "unchanged":
					result.Unchanged++
				}
			}
			if result.Failed == 0 {
				result.Status = "succeeded"
				if err = db.SaveSTRMTaskPublicURL(task.ID, runID, base); err != nil {
					result.Status = "partial"
					result.Failed = 1
					result.Issues = append(result.Issues, PreviewIssue{Code: "database_failed", Message: "生成完成但播放入口未能登记，请检查任务状态"})
				}
			} else {
				result.Status = "partial"
			}
		}
	}
	finishedAt := time.Now()
	run.Status, run.Complete, run.Added, run.Updated, run.Skipped, run.Failed = result.Status, result.Complete, result.Added, result.Updated, result.Unchanged, result.Failed
	run.ErrorCode, run.Error = "", ""
	if result.Status != "succeeded" {
		run.ErrorCode, run.Error = "generation_partial", "扫描或部分文件生成未完成；已写入内容仅限安全登记的STRM文件"
	}
	run.FinishedAt, run.UpdatedAt = &finishedAt, finishedAt
	success := result.Status == "succeeded" && result.Complete
	if err = db.FinishSTRMRun(task.ID, runID, run, success); err != nil {
		return nil, sourceError("run_finish_failed", "生成记录未能完成，请检查任务运行记录", err)
	}
	result.Missing = run.Missing
	finished = true
	return result, nil
}

func generateCandidate(ctx context.Context, task *model.STRMTask, source *Source, candidate generationCandidate, runID string, generation uint64, base string) (string, error) {
	fresh, err := source.GetFreshFile(ctx, candidate.node.Path)
	if err != nil {
		return "", err
	}
	if fresh.RemoteID != candidate.node.RemoteID || fresh.Size != candidate.node.Size || !fresh.Mtime.Equal(candidate.node.Mtime) {
		return "", sourceError("source_changed", "源文件在扫描后发生变化", nil)
	}
	key := sourceKey(fresh)
	file, fileErr := db.GetSTRMFile(task.ID, key)
	isNew := errors.Is(fileErr, gorm.ErrRecordNotFound)
	if fileErr != nil && !isNew {
		return "", fileErr
	}
	if isNew {
		file = &model.STRMFile{ID: uuid.NewString(), TaskID: task.ID, SourceKey: key, State: "active", TokenVersion: 1, CreatedAt: time.Now()}
	} else if file.SourceFingerprint != "" && file.SourceFingerprint != task.SourceFingerprint {
		return "", sourceError("source_changed", "源绑定与已登记文件不一致", nil)
	}
	if file.TokenVersion == 0 {
		file.TokenVersion = 1
	}
	now := time.Now()
	file.RemoteID, file.SourcePath, file.SourceFingerprint = fresh.RemoteID, fresh.Path, task.SourceFingerprint
	file.Size, file.Mtime = fresh.Size, fresh.Mtime
	file.LastSeenGeneration, file.MissingConfirmations, file.UpdatedAt = generation, 0, now
	if !isNew {
		// long: 源端已重新确认存在就先清除缺失状态；本地产物冲突另行报告，不能把源文件误判为再次缺失。
		if err = db.MarkSTRMFileSeen(file, runID, generation); err != nil {
			return "", sourceError("manifest_update_failed", "源文件已重现，但缺失状态未能更新", err)
		}
		if file.State == "trashed" {
			trashedArtifact, restoreErr := db.GetSTRMArtifactByFileID(task.ID, file.ID)
			if restoreErr != nil || trashedArtifact.State != "trashed" || trashedArtifact.OutputPath != candidate.outputPath {
				return "", sourceError("restore_manifest_conflict", "源文件已重现，但回收清单与当前路径不一致", restoreErr)
			}
			trashedArtifact, restoreAction, restoreErr := db.BeginSTRMRestore(trashedArtifact.ID, uuid.NewString())
			if restoreErr != nil {
				return "", sourceError("restore_state_failed", "无法开始恢复已回收的STRM文件", restoreErr)
			}
			if restoreErr = RestoreSTRMFromTrash(ctx, trashedArtifact.TrashPath, trashedArtifact.OutputPath, trashedArtifact.ContentHash); restoreErr != nil {
				return "", sourceError("restore_conflict", "回收文件已改变或原输出路径被占用", restoreErr)
			}
			if restoreErr = db.CommitSTRMRestore(trashedArtifact, restoreAction.ID, time.Now()); restoreErr != nil {
				return "", sourceError("restore_commit_failed", "回收文件已还原但清单提交失败，可重新运行完成对账", restoreErr)
			}
			file.State = "active"
		}
	}
	artifact, artifactErr := db.GetSTRMArtifactByTaskPath(task.ID, candidate.outputPath)
	artifactExists := artifactErr == nil
	if artifactErr != nil && !errors.Is(artifactErr, gorm.ErrRecordNotFound) {
		return "", artifactErr
	}
	if isNew && artifactExists {
		return "", ErrArtifactConflict
	}
	if !isNew {
		oldArtifact, oldErr := db.GetSTRMArtifactByFileID(task.ID, file.ID)
		if oldErr == nil && oldArtifact.OutputPath != candidate.outputPath {
			return "", ErrArtifactConflict
		}
		if oldErr != nil && !errors.Is(oldErr, gorm.ErrRecordNotFound) {
			return "", oldErr
		}
		if !artifactExists || artifact.FileID != file.ID {
			return "", ErrArtifactConflict
		}
	}
	if !artifactExists {
		artifact = &model.STRMArtifact{ID: uuid.NewString(), TaskID: task.ID, FileID: file.ID, Kind: "strm", OutputPath: candidate.outputPath, State: "active", CreatedAt: time.Now()}
	}
	entry, err := playbackEntry(base, file.ID, file.TokenVersion)
	if err != nil {
		return "", err
	}
	content := []byte(entry + "\n")
	newHash := hashBytes(content)
	if !isNew && artifact.ContentHash != newHash {
		file.TokenVersion++
		entry, err = playbackEntry(base, file.ID, file.TokenVersion)
		if err != nil {
			return "", err
		}
		content = []byte(entry + "\n")
		newHash = hashBytes(content)
	}
	if artifactExists && artifact.ContentHash == newHash {
		exists, matches, stateErr := STRMHashState(candidate.outputPath, artifact.ContentHash)
		if stateErr != nil {
			return "", stateErr
		}
		if exists && matches {
			return "unchanged", nil
		}
		if exists {
			return "", ErrArtifactConflict
		}
	}
	oldHash := ""
	operation := "create"
	if artifactExists {
		exists, matches, stateErr := STRMHashState(candidate.outputPath, artifact.ContentHash)
		if stateErr != nil {
			return "", stateErr
		}
		if exists && !matches {
			return "", ErrArtifactConflict
		}
		if exists {
			oldHash, operation = artifact.ContentHash, "update"
		}
	}
	if isNew {
		file.CreatedAt = now
	}
	artifact.FileID, artifact.ContentHash, artifact.State, artifact.UpdatedAt = file.ID, newHash, "active", now
	action := &model.STRMAction{ID: uuid.NewString(), RunID: runID, TaskID: task.ID, ArtifactID: artifact.ID, Operation: operation, OldPath: candidate.outputPath, NewPath: candidate.outputPath, ExpectedOldHash: oldHash, ExpectedNewHash: newHash, State: "applying", CreatedAt: now, UpdatedAt: now}
	if err = db.CreateSTRMAction(action); err != nil {
		return "", err
	}
	if _, err = WriteSTRM(ctx, candidate.outputPath, content, oldHash); err != nil {
		_ = db.FinishSTRMAction(action.ID, "failed", "write_conflict", "文件写入失败或目标内容已变化")
		return "", err
	}
	if err = db.CommitSTRMArtifact(action, file, artifact, base); err != nil {
		_ = db.FinishSTRMAction(action.ID, "failed", "manifest_commit_failed", "文件已写入但manifest提交失败，需人工核对")
		return "", err
	}
	if operation == "create" {
		return "added", nil
	}
	return "updated", nil
}
