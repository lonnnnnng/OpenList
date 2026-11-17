package model

import (
	"gorm.io/gorm"
	"time"
)

// STRMTask 绑定物理存储与独占输出；配置版本与完整扫描基线分开，改配置不能触发旧文件删除。作者：long
type STRMTask struct {
	ID                     string         `gorm:"primaryKey;size:36" json:"id"`
	Name                   string         `json:"name"`
	State                  string         `gorm:"index;default:draft" json:"state"`
	StorageID              uint           `gorm:"index" json:"storage_id"`
	Driver                 string         `json:"driver"`
	SourcePath             string         `json:"source_path"`
	SourceFingerprint      string         `json:"-"`
	OutputSubdir           string         `gorm:"uniqueIndex" json:"output_subdir"`
	PublicURL              string         `json:"public_url"`
	EntryPrefix            string         `json:"entry_prefix"`
	Extensions             []string       `gorm:"serializer:json;type:text" json:"extensions"`
	SidecarExtensions      []string       `gorm:"serializer:json;type:text" json:"sidecar_extensions"`
	MinSize                int64          `json:"min_size"`
	Cron                   string         `json:"cron"`
	Timezone               string         `gorm:"default:Asia/Shanghai" json:"timezone"`
	ScheduleEnabled        bool           `json:"schedule_enabled"`
	DeleteSync             bool           `json:"delete_sync"`
	TrashDays              int            `gorm:"default:7" json:"trash_days"`
	ConfigRevision         uint64         `gorm:"default:1" json:"config_revision"`
	BaselineRevision       uint64         `json:"baseline_revision"`
	Generation             uint64         `json:"generation"`               // long: 已预留的运行编号，失败运行也递增。
	LastCompleteGeneration uint64         `json:"last_complete_generation"` // long: 仅完整成功运行推进，缺失判断以此为基线。
	ActiveRunID            *string        `json:"active_run_id"`
	NextRunAt              *time.Time     `json:"next_run_at"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
	DeletedAt              gorm.DeletedAt `gorm:"index" json:"-"`
}

type STRMRun struct {
	ID                string     `gorm:"primaryKey;size:36" json:"id"`
	TaskID            string     `gorm:"index;uniqueIndex:strm_request,priority:1" json:"task_id"`
	RequestID         *string    `gorm:"uniqueIndex:strm_request,priority:2" json:"-"`
	Trigger           string     `json:"trigger"`
	Preview           bool       `json:"preview"`
	ConfigRevision    uint64     `json:"config_revision"`
	Generation        uint64     `json:"generation"`
	Status            string     `gorm:"index" json:"status"`
	Complete          bool       `json:"complete"`
	Added             int64      `json:"added"`
	Updated           int64      `json:"updated"`
	Deleted           int64      `json:"deleted"`
	Missing           int64      `json:"missing"`
	Skipped           int64      `json:"skipped"`
	Failed            int64      `json:"failed"`
	ErrorCode         string     `json:"error_code"`
	Error             string     `json:"error"`
	StartedAt         *time.Time `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
	CancelRequestedAt *time.Time `json:"cancel_requested_at"`
	CreatedAt         time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type STRMScanDir struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	RunID     string    `gorm:"uniqueIndex:strm_scan_dir,priority:1" json:"run_id"`
	Path      string    `gorm:"uniqueIndex:strm_scan_dir,priority:2" json:"path"`
	Status    string    `json:"status"`
	Count     int64     `json:"count"`
	ErrorCode string    `json:"error_code"`
	Error     string    `json:"error"`
	UpdatedAt time.Time `json:"updated_at"`
}

// STRMFile 保存稳定播放身份，源路径变更不等于允许入口指向另一个账号或根目录。作者：long
type STRMFile struct {
	ID                   string    `gorm:"primaryKey;size:36" json:"id"`
	TaskID               string    `gorm:"index;uniqueIndex:strm_source,priority:1" json:"task_id"`
	SourceKey            string    `gorm:"uniqueIndex:strm_source,priority:2" json:"-"`
	RemoteID             string    `json:"remote_id"`
	SourcePath           string    `json:"source_path"`
	SourceFingerprint    string    `json:"-"`
	Size                 int64     `json:"size"`
	Mtime                time.Time `json:"mtime"`
	RemoteHash           string    `json:"remote_hash"`
	LastSeenGeneration   uint64    `gorm:"index" json:"last_seen_generation"` // long: 最近一次安全确认该源文件存在的运行编号。
	MissingConfirmations int       `json:"missing_confirmations"`             // long: 只统计完整基线之后连续未见的次数。
	State                string    `gorm:"index;default:active" json:"state"`
	TokenVersion         uint64    `gorm:"default:1" json:"token_version"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// STRMArtifact 只有登记且摘要未改变的产物可被回收，未知文件不因目录为空而删除。作者：long
type STRMArtifact struct {
	ID          string     `gorm:"primaryKey;size:36" json:"id"`
	TaskID      string     `gorm:"index" json:"task_id"`
	FileID      string     `gorm:"index" json:"file_id"`
	Kind        string     `json:"kind"`
	OutputPath  string     `gorm:"uniqueIndex" json:"output_path"`
	ContentHash string     `json:"content_hash"`
	State       string     `gorm:"index" json:"state"`
	TrashPath   string     `json:"trash_path"`
	TrashedAt   *time.Time `json:"trashed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// STRMAction 对账数据库与文件系统之间的中断，不能把未完成rename当作已提交。作者：long
type STRMAction struct {
	ID              string    `gorm:"primaryKey;size:36" json:"id"`
	RunID           string    `gorm:"index" json:"run_id"`
	TaskID          string    `gorm:"index" json:"task_id"`
	ArtifactID      string    `gorm:"index" json:"artifact_id"`
	Operation       string    `json:"operation"`
	OldPath         string    `json:"old_path"`
	NewPath         string    `json:"new_path"`
	TempPath        string    `json:"-"`
	ExpectedOldHash string    `json:"-"`
	ExpectedNewHash string    `json:"-"`
	State           string    `gorm:"index" json:"state"`
	ErrorCode       string    `json:"error_code"`
	Error           string    `json:"error"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type STRMTrashPlan struct {
	ID                     string    `gorm:"primaryKey;size:36" json:"id"`
	TaskID                 string    `gorm:"index" json:"task_id"`
	ConfigRevision         uint64    `json:"config_revision"`
	Generation             uint64    `json:"generation"`
	LastCompleteGeneration uint64    `json:"last_complete_generation"`
	TotalManaged           int64     `json:"total_managed"`
	TotalMissing           int64     `json:"total_missing"`
	Eligible               int64     `json:"eligible"`
	Blocked                int64     `json:"blocked"`
	MassDisappearance      bool      `json:"mass_disappearance"`
	Status                 string    `gorm:"index" json:"status"`
	ExpiresAt              time.Time `gorm:"index" json:"expires_at"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type STRMTrashPlanItem struct {
	ID           string    `gorm:"primaryKey;size:36" json:"id"`
	PlanID       string    `gorm:"index;uniqueIndex:strm_trash_plan_item,priority:1" json:"plan_id"`
	ArtifactID   string    `gorm:"uniqueIndex:strm_trash_plan_item,priority:2" json:"artifact_id"`
	ActionID     string    `json:"action_id"`
	OutputPath   string    `json:"output_path"`
	TrashPath    string    `json:"trash_path"`
	ExpectedHash string    `json:"-"`
	OutputState  string    `json:"output_state"`
	Status       string    `gorm:"index" json:"status"`
	ErrorCode    string    `json:"error_code"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type STRMMediaServer struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	URL       string    `json:"url"`
	APIKey    string    `json:"-"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type STRMTaskMedia struct {
	TaskID    string    `gorm:"primaryKey;size:36" json:"task_id"`
	ServerID  string    `gorm:"primaryKey;size:36" json:"server_id"`
	LibraryID string    `gorm:"primaryKey" json:"library_id"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// STRMRefreshEvent 刷库可独立重试，不因媒体服务器失败回滚已生成的本地产物。作者：long
type STRMRefreshEvent struct {
	ID          string     `gorm:"primaryKey;size:36" json:"id"`
	RunID       string     `gorm:"uniqueIndex:strm_refresh,priority:1" json:"run_id"`
	ServerID    string     `gorm:"uniqueIndex:strm_refresh,priority:2" json:"server_id"`
	LibraryID   string     `gorm:"uniqueIndex:strm_refresh,priority:3" json:"library_id"`
	Status      string     `gorm:"index" json:"status"`
	Attempts    int        `json:"attempts"`
	NextRetryAt *time.Time `gorm:"index" json:"next_retry_at"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type STRMSchema struct {
	ID        uint `gorm:"primaryKey"`
	Version   uint
	UpdatedAt time.Time
}

func STRMModels() []any {
	return []any{&STRMTask{}, &STRMRun{}, &STRMScanDir{}, &STRMFile{}, &STRMArtifact{}, &STRMAction{}, &STRMTrashPlan{}, &STRMTrashPlanItem{}, &STRMMediaServer{}, &STRMTaskMedia{}, &STRMRefreshEvent{}, &STRMSchema{}}
}
