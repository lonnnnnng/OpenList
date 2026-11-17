package db

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"gorm.io/gorm"
)

func CreateSTRMTask(task *model.STRMTask) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var tasks []model.STRMTask
		if err := tx.Select("id", "output_subdir").Find(&tasks).Error; err != nil {
			return err
		}
		for _, existing := range tasks {
			if outputOverlaps(existing.OutputSubdir, task.OutputSubdir) {
				return fmt.Errorf("STRM输出目录与任务 %s 重叠", existing.ID)
			}
		}
		return tx.Create(task).Error
	})
}

func outputOverlaps(a, b string) bool {
	a, b = path.Clean(a), path.Clean(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func GetSTRMTask(id string) (*model.STRMTask, error) {
	var task model.STRMTask
	if err := db.First(&task, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func ListSTRMTasks(offset, limit int) ([]model.STRMTask, int64, error) {
	var total int64
	query := db.Model(&model.STRMTask{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tasks []model.STRMTask
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

func UpdateSTRMTask(task *model.STRMTask) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var tasks []model.STRMTask
		if err := tx.Select("id", "output_subdir").Where("id <> ?", task.ID).Find(&tasks).Error; err != nil {
			return err
		}
		for _, existing := range tasks {
			if outputOverlaps(existing.OutputSubdir, task.OutputSubdir) {
				return fmt.Errorf("STRM输出目录与任务 %s 重叠", existing.ID)
			}
		}
		// long: Select让结构体更新显式包含false零值，同时保留serializer:json对扩展名切片的编码。
		result := tx.Model(&model.STRMTask{}).Where("id = ? AND config_revision = ? AND state = ? AND active_run_id IS NULL", task.ID, task.ConfigRevision-1, "draft").Select(
			"Name", "StorageID", "Driver", "SourcePath", "SourceFingerprint", "OutputSubdir", "EntryPrefix",
			"Extensions", "SidecarExtensions", "MinSize", "Cron", "Timezone", "ScheduleEnabled",
			"DeleteSync", "TrashDays", "ConfigRevision",
		).Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func DeleteSTRMTask(id string) error {
	// long: 草稿还没有运行产物；硬删除释放唯一输出目录，避免用户无法复用未启用草稿路径。
	result := db.Unscoped().Delete(&model.STRMTask{}, "id = ? AND state = ? AND active_run_id IS NULL", id, "draft")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func StartSTRMRun(taskID string, run *model.STRMRun) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var task model.STRMTask
		if err := tx.Select("generation").First(&task, "id = ? AND state IN ? AND config_revision = ? AND active_run_id IS NULL", taskID, []string{"draft", "active"}, run.ConfigRevision).Error; err != nil {
			return err
		}
		// long: 尝试编号在事务内递增，失败运行也不复用编号；缺失判断只读取完整成功基线。
		run.Generation = task.Generation + 1
		result := tx.Model(&model.STRMTask{}).Where("id = ? AND state IN ? AND config_revision = ? AND generation = ? AND active_run_id IS NULL", taskID, []string{"draft", "active"}, run.ConfigRevision, task.Generation).Updates(map[string]any{"active_run_id": run.ID, "generation": run.Generation})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(run).Error
	})
}

func FinishSTRMRun(taskID, runID string, run *model.STRMRun, success bool) error {
	return db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"active_run_id": nil}
		state := "active"
		activate := success
		if !success {
			var actions int64
			if err := tx.Model(&model.STRMAction{}).Where("task_id = ? AND run_id = ?", taskID, runID).Count(&actions).Error; err != nil {
				return err
			}
			activate = actions > 0
		}
		if activate {
			updates["state"] = state
		}
		if success {
			var task model.STRMTask
			if err := tx.Select("last_complete_generation").First(&task, "id = ? AND active_run_id = ?", taskID, runID).Error; err != nil {
				return err
			}
			if task.LastCompleteGeneration > 0 {
				// long: 首次完整运行只建立基线；之后每次完整成功才为未出现文件增加一次确认。
				if err := tx.Model(&model.STRMFile{}).Where("task_id = ? AND state = ? AND last_seen_generation < ?", taskID, "active", run.Generation).UpdateColumn("missing_confirmations", gorm.Expr("missing_confirmations + 1")).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.STRMFile{}).Where("task_id = ? AND state = ? AND missing_confirmations >= ?", taskID, "active", 2).Count(&run.Missing).Error; err != nil {
					return err
				}
			} else {
				run.Missing = 0
			}
			updates["last_complete_generation"] = run.Generation
		}
		if err := tx.Model(&model.STRMRun{}).Where("id = ? AND task_id = ?", runID, taskID).Updates(map[string]any{
			"status": run.Status, "complete": run.Complete, "added": run.Added, "updated": run.Updated,
			"missing": run.Missing, "failed": run.Failed, "error_code": run.ErrorCode, "error": run.Error,
			"finished_at": run.FinishedAt, "updated_at": run.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		result := tx.Model(&model.STRMTask{}).Where("id = ? AND active_run_id = ?", taskID, runID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func CreateSTRMRun(run *model.STRMRun) error { return db.Create(run).Error }

func GetSTRMRun(id string) (*model.STRMRun, error) {
	var run model.STRMRun
	if err := db.First(&run, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func ListSTRMRuns(taskID string, offset, limit int) ([]model.STRMRun, int64, error) {
	query := db.Model(&model.STRMRun{}).Where("task_id = ?", taskID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var runs []model.STRMRun
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&runs).Error; err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

func GetSTRMFile(taskID, sourceKey string) (*model.STRMFile, error) {
	var file model.STRMFile
	if err := db.First(&file, "task_id = ? AND source_key = ?", taskID, sourceKey).Error; err != nil {
		return nil, err
	}
	return &file, nil
}

func GetSTRMFileByID(id string) (*model.STRMFile, error) {
	var file model.STRMFile
	if err := db.First(&file, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &file, nil
}

func GetSTRMArtifactByTaskPath(taskID, outputPath string) (*model.STRMArtifact, error) {
	var artifact model.STRMArtifact
	if err := db.First(&artifact, "task_id = ? AND output_path = ?", taskID, outputPath).Error; err != nil {
		return nil, err
	}
	return &artifact, nil
}

func GetSTRMArtifactByFileID(taskID, fileID string) (*model.STRMArtifact, error) {
	var artifact model.STRMArtifact
	if err := db.First(&artifact, "task_id = ? AND file_id = ? AND kind = ?", taskID, fileID, "strm").Error; err != nil {
		return nil, err
	}
	return &artifact, nil
}

func SaveSTRMTaskPublicURL(taskID, runID, publicURL string) error {
	return db.Transaction(func(tx *gorm.DB) error { return saveSTRMTaskPublicURL(tx, taskID, runID, publicURL) })
}

func saveSTRMTaskPublicURL(tx *gorm.DB, taskID, runID, publicURL string) error {
	var task model.STRMTask
	if err := tx.Select("id", "public_url").First(&task, "id = ? AND active_run_id = ?", taskID, runID).Error; err != nil {
		return err
	}
	if task.PublicURL == publicURL {
		return tx.Model(&model.STRMTask{}).Where("id = ? AND active_run_id = ?", taskID, runID).Update("state", "active").Error
	}
	if task.PublicURL != "" {
		return gorm.ErrRecordNotFound
	}
	result := tx.Model(&model.STRMTask{}).Where("id = ? AND active_run_id = ? AND public_url = ''", taskID, runID).Updates(map[string]any{"public_url": publicURL, "state": "active"})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func CreateSTRMFile(file *model.STRMFile) error { return db.Create(file).Error }

func ListSTRMConfirmedMissingFiles(taskID string, offset, limit int) (files []model.STRMFile, missing, managed int64, err error) {
	missingQuery := db.Model(&model.STRMFile{}).Where("task_id = ? AND state = ? AND missing_confirmations >= ?", taskID, "active", 2)
	if err = missingQuery.Count(&missing).Error; err != nil {
		return nil, 0, 0, err
	}
	if err = db.Model(&model.STRMFile{}).Where("task_id = ? AND state = ?", taskID, "active").Count(&managed).Error; err != nil {
		return nil, 0, 0, err
	}
	if err = missingQuery.Order("source_path ASC").Offset(offset).Limit(limit).Find(&files).Error; err != nil {
		return nil, 0, 0, err
	}
	return files, missing, managed, nil
}

func MarkSTRMFileSeen(file *model.STRMFile, runID string, generation uint64) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var task model.STRMTask
		if err := tx.Select("generation", "active_run_id").First(&task, "id = ?", file.TaskID).Error; err != nil {
			return err
		}
		if task.ActiveRunID == nil || *task.ActiveRunID != runID || task.Generation != generation {
			return gorm.ErrRecordNotFound
		}
		file.LastSeenGeneration = generation
		file.MissingConfirmations = 0
		file.UpdatedAt = time.Now()
		return tx.Save(file).Error
	})
}

func GetSTRMArtifact(outputPath string) (*model.STRMArtifact, error) {
	var artifact model.STRMArtifact
	if err := db.First(&artifact, "output_path = ?", outputPath).Error; err != nil {
		return nil, err
	}
	return &artifact, nil
}

func CreateSTRMAction(action *model.STRMAction) error { return db.Create(action).Error }

func CreateSTRMTrashPlan(plan *model.STRMTrashPlan, items []model.STRMTrashPlanItem) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var task model.STRMTask
		if err := tx.Select("id", "config_revision", "generation", "last_complete_generation", "active_run_id").First(&task, "id = ?", plan.TaskID).Error; err != nil {
			return err
		}
		if task.ActiveRunID != nil || task.ConfigRevision != plan.ConfigRevision || task.Generation != plan.Generation || task.LastCompleteGeneration != plan.LastCompleteGeneration {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Create(plan).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		return tx.Create(&items).Error
	})
}

func GetSTRMTrashPlan(id string) (*model.STRMTrashPlan, error) {
	var plan model.STRMTrashPlan
	if err := db.First(&plan, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func ListSTRMTrashPlanItems(planID string) ([]model.STRMTrashPlanItem, error) {
	var items []model.STRMTrashPlanItem
	err := db.Where("plan_id = ?", planID).Order("created_at ASC").Find(&items).Error
	return items, err
}

func UpdateSTRMTrashPlanItem(id, status, code string) error {
	return db.Model(&model.STRMTrashPlanItem{}).Where("id = ?", id).Updates(map[string]any{"status": status, "error_code": code, "updated_at": time.Now()}).Error
}

func ClaimSTRMTrashPlan(planID, taskID string, confirmMass bool) (*model.STRMTrashPlan, error) {
	var plan model.STRMTrashPlan
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&plan, "id = ? AND task_id = ?", planID, taskID).Error; err != nil {
			return err
		}
		if (plan.Status != "previewed" && plan.Status != "partial") || time.Now().After(plan.ExpiresAt) {
			return gorm.ErrRecordNotFound
		}
		if plan.MassDisappearance && !confirmMass {
			return fmt.Errorf("大量缺失需要显式确认")
		}
		var task model.STRMTask
		if err := tx.Select("config_revision", "generation", "last_complete_generation", "active_run_id").First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.ActiveRunID != nil || task.ConfigRevision != plan.ConfigRevision || task.Generation != plan.Generation || task.LastCompleteGeneration != plan.LastCompleteGeneration {
			return gorm.ErrRecordNotFound
		}
		var missing int64
		if err := tx.Model(&model.STRMFile{}).Where("task_id = ? AND state = ? AND missing_confirmations >= ?", taskID, "active", 2).Count(&missing).Error; err != nil {
			return err
		}
		if missing != plan.TotalMissing {
			return gorm.ErrRecordNotFound
		}
		lock := tx.Model(&model.STRMTask{}).Where("id = ? AND config_revision = ? AND generation = ? AND last_complete_generation = ? AND active_run_id IS NULL", taskID, plan.ConfigRevision, plan.Generation, plan.LastCompleteGeneration).Update("active_run_id", planID)
		if lock.Error != nil {
			return lock.Error
		}
		if lock.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		result := tx.Model(&model.STRMTrashPlan{}).Where("id = ? AND task_id = ? AND status IN ? AND expires_at > ?", planID, taskID, []string{"previewed", "partial"}, time.Now()).Update("status", "executing")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		plan.Status = "executing"
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_ = db.Model(&model.STRMTrashPlan{}).Where("id = ? AND status = ?", planID, "previewed").Update("status", "stale").Error
	}
	return &plan, err
}

func FinishSTRMTrashPlan(planID, status string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var plan model.STRMTrashPlan
		if err := tx.Select("task_id").First(&plan, "id = ? AND status = ?", planID, "executing").Error; err != nil {
			return err
		}
		result := tx.Model(&model.STRMTask{}).Where("id = ? AND active_run_id = ?", plan.TaskID, planID).Update("active_run_id", nil)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&model.STRMTrashPlan{}).Where("id = ? AND status = ?", planID, "executing").Updates(map[string]any{"status": status, "updated_at": time.Now()}).Error
	})
}

func RecoverSTRMTrashPlans() error {
	now := time.Now()
	return db.Transaction(func(tx *gorm.DB) error {
		var plans []model.STRMTrashPlan
		if err := tx.Where("status = ?", "executing").Find(&plans).Error; err != nil {
			return err
		}
		for _, plan := range plans {
			if err := tx.Model(&model.STRMTask{}).Where("id = ? AND active_run_id = ?", plan.TaskID, plan.ID).Update("active_run_id", nil).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.STRMTrashPlan{}).Where("id = ? AND status = ?", plan.ID, "executing").Updates(map[string]any{
				"status": "partial", "expires_at": now.Add(15 * time.Minute), "updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func BeginSTRMTrashItem(planID, itemID, actionID string) (*model.STRMTrashPlanItem, *model.STRMArtifact, error) {
	var item model.STRMTrashPlanItem
	var artifact model.STRMArtifact
	err := db.Transaction(func(tx *gorm.DB) error {
		var plan model.STRMTrashPlan
		if err := tx.Select("task_id").First(&plan, "id = ? AND status = ?", planID, "executing").Error; err != nil {
			return err
		}
		var task model.STRMTask
		if err := tx.Select("active_run_id").First(&task, "id = ?", plan.TaskID).Error; err != nil || task.ActiveRunID == nil || *task.ActiveRunID != planID {
			return gorm.ErrRecordNotFound
		}
		if err := tx.First(&item, "id = ? AND plan_id = ? AND status IN ?", itemID, planID, []string{"planned", "applying"}).Error; err != nil {
			return err
		}
		if err := tx.First(&artifact, "id = ? AND task_id = ? AND state = ? AND content_hash = ? AND output_path = ?", item.ArtifactID, plan.TaskID, "active", item.ExpectedHash, item.OutputPath).Error; err != nil {
			return err
		}
		if item.ActionID != actionID {
			return gorm.ErrRecordNotFound
		}
		if item.Status == "planned" {
			action := &model.STRMAction{ID: actionID, TaskID: artifact.TaskID, ArtifactID: artifact.ID, Operation: "trash", OldPath: item.OutputPath, NewPath: item.TrashPath, ExpectedOldHash: item.ExpectedHash, ExpectedNewHash: item.ExpectedHash, State: "applying", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if err := tx.Create(action).Error; err != nil {
				return err
			}
		}
		if item.Status == "planned" {
			item.Status, item.UpdatedAt = "applying", time.Now()
			return tx.Save(&item).Error
		}
		return nil
	})
	return &item, &artifact, err
}

func CommitSTRMTrashItem(planID, itemID, actionID string, trashedAt time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var item model.STRMTrashPlanItem
		if err := tx.First(&item, "id = ? AND plan_id = ? AND action_id = ? AND status = ?", itemID, planID, actionID, "applying").Error; err != nil {
			return err
		}
		result := tx.Model(&model.STRMArtifact{}).Where("id = ? AND state = ? AND output_path = ? AND content_hash = ?", item.ArtifactID, "active", item.OutputPath, item.ExpectedHash).Updates(map[string]any{"state": "trashed", "trash_path": item.TrashPath, "trashed_at": trashedAt, "updated_at": trashedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		var artifact model.STRMArtifact
		if err := tx.Select("file_id").First(&artifact, "id = ?", item.ArtifactID).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.STRMFile{}).Where("id = ? AND state = ?", artifact.FileID, "active").Update("state", "trashed").Error; err != nil {
			return err
		}
		if err := tx.Model(&model.STRMTrashPlanItem{}).Where("id = ? AND status = ?", itemID, "applying").Updates(map[string]any{"status": "committed", "updated_at": trashedAt}).Error; err != nil {
			return err
		}
		result = tx.Model(&model.STRMAction{}).Where("id = ? AND state = ?", actionID, "applying").Updates(map[string]any{"state": "committed", "updated_at": trashedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func ListSTRMTrashedArtifacts(taskID string, offset, limit int) ([]model.STRMArtifact, int64, error) {
	query := db.Model(&model.STRMArtifact{}).Where("task_id = ? AND state = ?", taskID, "trashed")
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.STRMArtifact
	if err := query.Order("trashed_at DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func GetSTRMArtifactByID(id string) (*model.STRMArtifact, error) {
	var artifact model.STRMArtifact
	if err := db.First(&artifact, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &artifact, nil
}

func BeginSTRMRestore(artifactID, actionID string) (*model.STRMArtifact, *model.STRMAction, error) {
	var artifact model.STRMArtifact
	var action model.STRMAction
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&artifact, "id = ? AND state = ?", artifactID, "trashed").Error; err != nil {
			return err
		}
		var existing model.STRMAction
		if err := tx.Where("artifact_id = ? AND operation = ? AND state = ?", artifactID, "restore", "applying").Order("created_at DESC").First(&existing).Error; err == nil {
			action = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		action = model.STRMAction{ID: actionID, TaskID: artifact.TaskID, ArtifactID: artifact.ID, Operation: "restore", OldPath: artifact.TrashPath, NewPath: artifact.OutputPath, ExpectedOldHash: artifact.ContentHash, ExpectedNewHash: artifact.ContentHash, State: "applying", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := tx.Create(&action).Error; err != nil {
			return err
		}
		return nil
	})
	return &artifact, &action, err
}

func CommitSTRMRestore(artifact *model.STRMArtifact, actionID string, restoredAt time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.STRMArtifact{}).Where("id = ? AND task_id = ? AND state = ? AND trash_path = ? AND content_hash = ?", artifact.ID, artifact.TaskID, "trashed", artifact.TrashPath, artifact.ContentHash).Updates(map[string]any{"state": "active", "trash_path": "", "trashed_at": nil, "updated_at": restoredAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&model.STRMFile{}).Where("id = ? AND state = ?", artifact.FileID, "trashed").Update("state", "active").Error; err != nil {
			return err
		}
		result = tx.Model(&model.STRMAction{}).Where("id = ? AND operation = ? AND state = ?", actionID, "restore", "applying").Updates(map[string]any{"state": "committed", "updated_at": restoredAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func ListSTRMTrashPlans(taskID string, offset, limit int) ([]model.STRMTrashPlan, int64, error) {
	query := db.Model(&model.STRMTrashPlan{}).Where("task_id = ?", taskID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var plans []model.STRMTrashPlan
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&plans).Error; err != nil {
		return nil, 0, err
	}
	return plans, total, nil
}

func FinishSTRMAction(id, state, code, message string) error {
	return db.Model(&model.STRMAction{}).Where("id = ?", id).Updates(map[string]any{"state": state, "error_code": code, "error": message}).Error
}

func CommitSTRMArtifact(action *model.STRMAction, file *model.STRMFile, artifact *model.STRMArtifact, publicURL string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(file).Error; err != nil {
			return err
		}
		if err := tx.Save(artifact).Error; err != nil {
			return err
		}
		if err := saveSTRMTaskPublicURL(tx, action.TaskID, action.RunID, publicURL); err != nil {
			return err
		}
		result := tx.Model(&model.STRMAction{}).Where("id = ? AND state = ?", action.ID, "applying").Updates(map[string]any{"state": "committed", "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func GetSTRMActions(taskID, runID string) ([]model.STRMAction, error) {
	var actions []model.STRMAction
	err := db.Where("task_id = ? AND run_id = ?", taskID, runID).Order("created_at ASC").Find(&actions).Error
	return actions, err
}
