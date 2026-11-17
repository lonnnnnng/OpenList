package handles

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/strm"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type taskDraftRequest struct {
	strm.TaskDraft
	ConfigRevision uint64 `json:"config_revision"`
}

type trashExecuteRequest struct {
	ConfirmMassDisappearance bool `json:"confirm_mass_disappearance"`
}

func respondSTRMError(c *gin.Context, err error) {
	var sourceErr *strm.SourceError
	if errors.As(err, &sourceErr) {
		common.ErrorWithDataResp(c, errors.New(sourceErr.Message), http.StatusBadRequest, gin.H{"error_code": sourceErr.Code})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ErrorStrResp(c, "任务不存在或已被其他请求修改", http.StatusNotFound)
		return
	}
	common.ErrorResp(c, err, http.StatusBadRequest)
}

func parseSTRMPagination(c *gin.Context) (int, int, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		return 0, 0, errors.New("页码必须大于0")
	}
	perPage, err := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if err != nil || perPage < 1 || perPage > 100 {
		return 0, 0, errors.New("每页数量需在1到100之间")
	}
	return (page - 1) * perPage, perPage, nil
}

func STRMTasks(c *gin.Context) {
	offset, limit, err := parseSTRMPagination(c)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	tasks, total, err := db.ListSTRMTasks(offset, limit)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, common.PageResp{Content: tasks, Total: total})
}

func STRMCreateTask(c *gin.Context) {
	var request taskDraftRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	task, err := strm.SaveTaskDraft(c.Request.Context(), request.TaskDraft)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, task)
}

func STRMGetTask(c *gin.Context) {
	task, err := db.GetSTRMTask(c.Param("id"))
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, task)
}

func STRMUpdateTask(c *gin.Context) {
	current, err := db.GetSTRMTask(c.Param("id"))
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	var request taskDraftRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	updated, err := strm.UpdateTaskDraft(c.Request.Context(), current, request.ConfigRevision, request.TaskDraft)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, updated)
}

func STRMDeleteTask(c *gin.Context) {
	if err := db.DeleteSTRMTask(c.Param("id")); err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c)
}

func STRMPreviewTask(c *gin.Context) {
	task, err := db.GetSTRMTask(c.Param("id"))
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	preview, err := strm.PreviewTask(ctx, task)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, preview)
}

func STRMGenerateTask(c *gin.Context) {
	task, err := db.GetSTRMTask(c.Param("id"))
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Minute)
	defer cancel()
	result, err := strm.GenerateTask(ctx, task)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, result)
}

func STRMMissingPreview(c *gin.Context) {
	offset, limit, err := parseSTRMPagination(c)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	task, err := db.GetSTRMTask(c.Param("id"))
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	preview, err := strm.PreviewMissingFiles(ctx, task, offset, limit)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, preview)
}

func STRMTrashPreview(c *gin.Context) {
	task, err := db.GetSTRMTask(c.Param("id"))
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	plan, err := strm.CreateTrashPlan(ctx, task)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, plan)
}

func STRMTrashExecute(c *gin.Context) {
	var request trashExecuteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Minute)
	defer cancel()
	result, err := strm.ExecuteTrashPlan(ctx, c.Param("id"), c.Param("plan_id"), request.ConfirmMassDisappearance)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, result)
}

func STRMTrashPlans(c *gin.Context) {
	offset, limit, err := parseSTRMPagination(c)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	plans, total, err := strm.TrashPlans(c.Param("id"), offset, limit)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, common.PageResp{Content: plans, Total: total})
}

func STRMTrashList(c *gin.Context) {
	offset, limit, err := parseSTRMPagination(c)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	items, total, err := strm.ListTrash(c.Request.Context(), c.Param("id"), offset, limit)
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, common.PageResp{Content: items, Total: total})
}

func STRMTrashRestore(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := strm.RestoreTrashArtifact(ctx, c.Param("id"), c.Param("artifact_id")); err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c)
}

// STRMPlayback 仅接受独立密钥签名的文件ID与版本；云端路径和临时链接始终留在服务端。作者：long
func STRMPlayback(c *gin.Context) {
	file, err := db.GetSTRMFileByID(c.Param("file_id"))
	if err != nil || file.State != "active" || !strm.VerifyPlayback(file.ID, file.TokenVersion, c.Query("sig")) || c.Query("v") != strconv.FormatUint(file.TokenVersion, 10) {
		c.Status(http.StatusNotFound)
		return
	}
	task, err := db.GetSTRMTask(file.TaskID)
	if err != nil || task.State != "active" || task.SourceFingerprint != file.SourceFingerprint {
		c.Status(http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	bound := strm.SourceBinding{StorageID: task.StorageID, Driver: task.Driver, SourcePath: task.SourcePath, Fingerprint: task.SourceFingerprint}
	source, err := strm.OpenBoundSource(ctx, bound)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	playbackHeaders := make(http.Header)
	if userAgent := c.GetHeader("User-Agent"); userAgent != "" {
		playbackHeaders.Set("User-Agent", userAgent)
	}
	link, object, err := source.LinkFreshFile(ctx, file.SourcePath, playbackHeaders)
	if err != nil || link == nil || object == nil {
		c.Status(http.StatusBadGateway)
		return
	}
	defer link.Close()
	if err = common.Proxy(c.Writer, c.Request, link, object); err != nil && !c.Writer.Written() {
		c.Status(http.StatusBadGateway)
	}
}

func STRMTaskRuns(c *gin.Context) {
	offset, limit, err := parseSTRMPagination(c)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if _, err = db.GetSTRMTask(c.Param("id")); err != nil {
		respondSTRMError(c, err)
		return
	}
	runs, total, err := db.ListSTRMRuns(c.Param("id"), offset, limit)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, common.PageResp{Content: runs, Total: total})
}

func STRMGetRun(c *gin.Context) {
	run, err := db.GetSTRMRun(c.Param("id"))
	if err != nil {
		respondSTRMError(c, err)
		return
	}
	common.SuccessResp(c, run)
}
