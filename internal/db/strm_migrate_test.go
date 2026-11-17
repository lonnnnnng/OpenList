package db

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestSTRMMigrationHonorsPrefixAndPreservesOtherData(t *testing.T) {
	oldDB, oldConf := db, conf.Conf
	t.Cleanup(func() { db, conf.Conf = oldDB, oldConf })
	conf.Conf = &conf.Config{Database: conf.Database{Type: "sqlite3"}}
	var err error
	db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migration.db")), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "fixture_"}})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = db.Exec("CREATE TABLE unrelated (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO unrelated (value) VALUES (?)", "preserve").Error; err != nil {
		t.Fatal(err)
	}
	if err = MigrateSTRM(); err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.STRMSchema{}).Where("id = ?", 1).Update("version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err = MigrateSTRM(); err != nil {
		t.Fatalf("schema v1 upgrade failed: %v", err)
	}
	var upgraded model.STRMSchema
	if err = db.First(&upgraded, 1).Error; err != nil || upgraded.Version != STRMSchemaVersion {
		t.Fatalf("schema v1 was not advanced: %+v %v", upgraded, err)
	}
	task := &model.STRMTask{ID: "task-a", Name: "电影", State: "draft", StorageID: 1, Driver: "Local", SourcePath: "/", SourceFingerprint: "digest", OutputSubdir: "movies", Extensions: []string{".mkv"}, Timezone: "Asia/Shanghai", TrashDays: 7, ConfigRevision: 1}
	if err = CreateSTRMTask(task); err != nil {
		t.Fatal(err)
	}
	lockedTask := &model.STRMTask{ID: "task-lock", Name: "锁测试", State: "draft", StorageID: 1, Driver: "Local", SourcePath: "/", SourceFingerprint: "digest", OutputSubdir: "locks", Extensions: []string{".mkv"}, Timezone: "Asia/Shanghai", TrashDays: 7, ConfigRevision: 1}
	if err = CreateSTRMTask(lockedTask); err != nil {
		t.Fatal(err)
	}
	run := &model.STRMRun{ID: "run-lock-1", TaskID: lockedTask.ID, ConfigRevision: 1, Status: "running"}
	if err = StartSTRMRun(lockedTask.ID, run); err != nil {
		t.Fatal(err)
	}
	if run.Generation != 1 {
		t.Fatalf("first run did not reserve generation 1: %d", run.Generation)
	}
	if err = StartSTRMRun(lockedTask.ID, &model.STRMRun{ID: "run-lock-2", TaskID: lockedTask.ID, ConfigRevision: 1}); err == nil {
		t.Fatal("concurrent task run acquired the same task lock")
	}
	lockedTask.ConfigRevision = 2
	if err = UpdateSTRMTask(lockedTask); err == nil {
		t.Fatal("draft edit was accepted while a run held the task lock")
	}
	ended := time.Now()
	run.Status, run.FinishedAt, run.UpdatedAt = "partial", &ended, ended
	if err = FinishSTRMRun(lockedTask.ID, run.ID, run, false); err != nil {
		t.Fatal(err)
	}
	released, err := GetSTRMTask(lockedTask.ID)
	if err != nil || released.State != "draft" || released.ActiveRunID != nil || released.Generation != 1 || released.LastCompleteGeneration != 0 {
		t.Fatalf("empty failed run did not release editable draft: %+v %v", released, err)
	}
	run = &model.STRMRun{ID: "run-lock-3", TaskID: lockedTask.ID, ConfigRevision: 1, Status: "running"}
	if err = StartSTRMRun(lockedTask.ID, run); err != nil {
		t.Fatal(err)
	}
	if run.Generation != 2 {
		t.Fatalf("partial run reused an attempt generation: %d", run.Generation)
	}
	if err = CreateSTRMAction(&model.STRMAction{ID: "action-lock", RunID: run.ID, TaskID: lockedTask.ID, State: "failed"}); err != nil {
		t.Fatal(err)
	}
	run.Status, run.FinishedAt, run.UpdatedAt = "partial", &ended, ended
	if err = FinishSTRMRun(lockedTask.ID, run.ID, run, false); err != nil {
		t.Fatal(err)
	}
	released, err = GetSTRMTask(lockedTask.ID)
	if err != nil || released.State != "active" || released.ActiveRunID != nil || released.Generation != 2 || released.LastCompleteGeneration != 0 {
		t.Fatalf("task with recorded output action remained editable: %+v %v", released, err)
	}
	run = &model.STRMRun{ID: "run-lock-4", TaskID: lockedTask.ID, ConfigRevision: 1, Status: "running"}
	if err = StartSTRMRun(lockedTask.ID, run); err != nil {
		t.Fatal(err)
	}
	file := &model.STRMFile{ID: "file-seen", TaskID: lockedTask.ID, SourceKey: "source-key", State: "active", MissingConfirmations: 2}
	if err = CreateSTRMFile(file); err != nil {
		t.Fatal(err)
	}
	if err = MarkSTRMFileSeen(file, run.ID, run.Generation); err != nil {
		t.Fatal(err)
	}
	var seen model.STRMFile
	if err = db.First(&seen, "id = ?", file.ID).Error; err != nil || seen.LastSeenGeneration != 3 || seen.MissingConfirmations != 0 {
		t.Fatalf("seen file did not record generation/reset misses: %+v %v", seen, err)
	}
	run.Status, run.Complete, run.FinishedAt, run.UpdatedAt = "succeeded", true, &ended, ended
	if err = FinishSTRMRun(lockedTask.ID, run.ID, run, true); err != nil {
		t.Fatal(err)
	}
	released, err = GetSTRMTask(lockedTask.ID)
	if err != nil || released.Generation != 3 || released.LastCompleteGeneration != 3 {
		t.Fatalf("complete baseline was not advanced separately: %+v %v", released, err)
	}
	run = &model.STRMRun{ID: "run-lock-5", TaskID: lockedTask.ID, ConfigRevision: 1, Status: "partial"}
	if err = StartSTRMRun(lockedTask.ID, run); err != nil {
		t.Fatal(err)
	}
	run.FinishedAt, run.UpdatedAt = &ended, ended
	if err = FinishSTRMRun(lockedTask.ID, run.ID, run, false); err != nil {
		t.Fatal(err)
	}
	if err = db.First(&seen, "id = ?", file.ID).Error; err != nil || seen.MissingConfirmations != 0 {
		t.Fatalf("partial run counted a missing file: %+v %v", seen, err)
	}
	run = &model.STRMRun{ID: "run-lock-6", TaskID: lockedTask.ID, ConfigRevision: 1, Status: "succeeded", Complete: true}
	if err = StartSTRMRun(lockedTask.ID, run); err != nil {
		t.Fatal(err)
	}
	run.FinishedAt, run.UpdatedAt = &ended, ended
	if err = FinishSTRMRun(lockedTask.ID, run.ID, run, true); err != nil {
		t.Fatal(err)
	}
	if err = db.First(&seen, "id = ?", file.ID).Error; err != nil || seen.MissingConfirmations != 1 || run.Missing != 0 {
		t.Fatalf("first complete absence confirmation is wrong: file=%+v run=%+v err=%v", seen, run, err)
	}
	run = &model.STRMRun{ID: "run-lock-7", TaskID: lockedTask.ID, ConfigRevision: 1, Status: "succeeded", Complete: true}
	if err = StartSTRMRun(lockedTask.ID, run); err != nil {
		t.Fatal(err)
	}
	run.FinishedAt, run.UpdatedAt = &ended, ended
	if err = FinishSTRMRun(lockedTask.ID, run.ID, run, true); err != nil {
		t.Fatal(err)
	}
	if err = db.First(&seen, "id = ?", file.ID).Error; err != nil || seen.MissingConfirmations != 2 || run.Missing != 1 {
		t.Fatalf("two complete absences were not identified for preview: file=%+v run=%+v err=%v", seen, run, err)
	}
	missingFiles, missingCount, managedCount, err := ListSTRMConfirmedMissingFiles(lockedTask.ID, 0, 1)
	if err != nil || missingCount != 1 || managedCount != 1 || len(missingFiles) != 1 || missingFiles[0].ID != file.ID {
		t.Fatalf("confirmed missing listing is inconsistent: files=%+v missing=%d managed=%d err=%v", missingFiles, missingCount, managedCount, err)
	}
	planTask := &model.STRMTask{ID: "task-plan", Name: "计划锁", State: "active", StorageID: 1, Driver: "Local", SourcePath: "/", SourceFingerprint: "digest", OutputSubdir: "plans", ConfigRevision: 4, Generation: 8, LastCompleteGeneration: 7}
	if err = CreateSTRMTask(planTask); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		misses := 0
		if i < 10 {
			misses = 2
		}
		if err = CreateSTRMFile(&model.STRMFile{ID: fmt.Sprintf("plan-file-%02d", i), TaskID: planTask.ID, SourceKey: fmt.Sprintf("plan-source-%02d", i), State: "active", MissingConfirmations: misses}); err != nil {
			t.Fatal(err)
		}
	}
	created := time.Now()
	plan := &model.STRMTrashPlan{ID: "plan-one", TaskID: planTask.ID, ConfigRevision: 4, Generation: 8, LastCompleteGeneration: 7, TotalManaged: 40, TotalMissing: 10, Eligible: 10, MassDisappearance: true, Status: "previewed", ExpiresAt: created.Add(time.Minute), CreatedAt: created, UpdatedAt: created}
	planItem := model.STRMTrashPlanItem{ID: "plan-item-one", PlanID: plan.ID, ArtifactID: "artifact-one", ActionID: "action-one", OutputPath: "plans/movie.strm", TrashPath: "task-plan/action-one/movie.strm", ExpectedHash: "digest", Status: "planned", CreatedAt: created, UpdatedAt: created}
	if err = CreateSTRMTrashPlan(plan, []model.STRMTrashPlanItem{planItem}); err != nil {
		t.Fatal(err)
	}
	if _, err = ClaimSTRMTrashPlan(plan.ID, planTask.ID, false); err == nil {
		t.Fatal("mass disappearance plan executed without explicit confirmation")
	}
	claimed, err := ClaimSTRMTrashPlan(plan.ID, planTask.ID, true)
	if err != nil || claimed.Status != "executing" {
		t.Fatalf("confirmed preview plan was not claimed: %+v %v", claimed, err)
	}
	if _, err = ClaimSTRMTrashPlan(plan.ID, planTask.ID, true); err == nil {
		t.Fatal("one-shot preview plan was claimed twice")
	}
	if err = FinishSTRMTrashPlan(plan.ID, "committed"); err != nil {
		t.Fatal(err)
	}
	if _, err = ClaimSTRMTrashPlan(plan.ID, planTask.ID, true); err == nil {
		t.Fatal("committed preview plan was replayed")
	}
	if err = db.Model(&model.STRMTask{}).Where("id = ?", planTask.ID).Update("active_run_id", plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.STRMTrashPlan{}).Where("id = ?", plan.ID).Update("status", "executing").Error; err != nil {
		t.Fatal(err)
	}
	if err = RecoverSTRMTrashPlans(); err != nil {
		t.Fatal(err)
	}
	recoveredPlan, err := GetSTRMTrashPlan(plan.ID)
	recoveredTask, taskErr := GetSTRMTask(planTask.ID)
	if err != nil || taskErr != nil || recoveredPlan.Status != "partial" || time.Until(recoveredPlan.ExpiresAt) <= 0 || recoveredTask.ActiveRunID != nil {
		t.Fatalf("interrupted trash plan was not made retryable: %+v %v", recoveredPlan, err)
	}
	if _, err = ClaimSTRMTrashPlan(plan.ID, planTask.ID, true); err != nil {
		t.Fatalf("recovered plan could not be retried: %v", err)
	}
	if err = FinishSTRMTrashPlan(plan.ID, "partial"); err != nil {
		t.Fatal(err)
	}
	if err = CreateSTRMTask(&model.STRMTask{ID: "task-b", Name: "冲突", State: "draft", StorageID: 1, OutputSubdir: "movies/sub"}); err == nil {
		t.Fatal("nested task output accepted")
	}
	first, err := GetSTRMTask("task-a")
	if err != nil {
		t.Fatal(err)
	}
	first.Name, first.ConfigRevision, first.Extensions = "已更新", 2, []string{".mkv", ".mp4"}
	if err = UpdateSTRMTask(first); err != nil {
		t.Fatal(err)
	}
	updated, err := GetSTRMTask("task-a")
	if err != nil || len(updated.Extensions) != 2 || updated.Extensions[1] != ".mp4" {
		t.Fatalf("serialized extensions not updated: %+v %v", updated, err)
	}
	if err = UpdateSTRMTask(first); err == nil {
		t.Fatal("stale config revision accepted")
	}
	if err = DeleteSTRMTask("task-a"); err != nil {
		t.Fatal(err)
	}
	if err = CreateSTRMTask(&model.STRMTask{ID: "task-c", Name: "复用", State: "draft", StorageID: 1, OutputSubdir: "movies"}); err != nil {
		t.Fatalf("deleted draft did not release output path: %v", err)
	}
	if err = DeleteSTRMTask("task-a"); err == nil {
		t.Fatal("missing task deletion reported success")
	}
	if !db.Migrator().HasTable("fixture_strm_tasks") || db.Migrator().HasTable("x_strm_tasks") {
		t.Fatal("table prefix ignored")
	}
	if err = MigrateSTRM(); err != nil {
		t.Fatal(err)
	}
	var value string
	db.Raw("SELECT value FROM unrelated").Scan(&value)
	if value != "preserve" {
		t.Fatal("migration changed unrelated data")
	}
	// long: 模拟建表成功但尚未登记版本就退出，恢复后既要补版本，也要保留已有表。
	if err = db.Where("id = ?", 1).Delete(&model.STRMSchema{}).Error; err != nil {
		t.Fatal(err)
	}
	if err = MigrateSTRM(); err != nil {
		t.Fatalf("interrupted first migration did not recover: %v", err)
	}
	var recovered model.STRMSchema
	if err = db.First(&recovered, 1).Error; err != nil || recovered.Version != STRMSchemaVersion {
		t.Fatalf("missing recovered schema version: %+v, %v", recovered, err)
	}
	if err = db.Model(&model.STRMSchema{}).Where("id = ?", 1).Update("version", STRMSchemaVersion+1).Error; err != nil {
		t.Fatal(err)
	}
	if err = MigrateSTRM(); err == nil {
		t.Fatal("newer database accepted by older binary")
	}
}
