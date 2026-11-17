package handles

import (
	"context"
	"errors"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/strm"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

func strmReady(ctx context.Context) bool {
	if !strm.Initialized() || db.GetDb() == nil {
		return false
	}
	select {
	case <-conf.StoragesLoadSignal():
	default:
		return false
	}
	sqlDB, err := db.GetDb().DB()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx) == nil
}

// STRMReady 表示运行基础就绪，单个账号失效不应造成整个容器不断重启。作者：long
func STRMReady(c *gin.Context) {
	ready := strmReady(c.Request.Context())
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"ready": ready})
}

func STRMHealth(c *gin.Context) {
	taskExecutionReady := strm.Initialized() && strings.TrimSpace(conf.Conf.STRM.PublicURL) != ""
	common.SuccessResp(c, gin.H{
		"ready": strmReady(c.Request.Context()), "product_version": conf.ProductVersion,
		"upstream_commit": conf.UpstreamCommit, "frontend_commit": conf.FrontendCommit,
		"output_root": conf.Conf.STRM.OutputRoot, "public_url": conf.Conf.STRM.PublicURL,
		"milestone": "M2", "task_execution_available": taskExecutionReady,
	})
}

// STRMBrowse 只返回当前单物理存储的只读目录条目，底层错误不能转换成空目录。作者：long
func STRMBrowse(c *gin.Context) {
	storageID, err := strconv.ParseUint(c.Query("storage_id"), 10, 32)
	if err != nil || storageID == 0 {
		common.ErrorStrResp(c, "请选择有效存储", 400)
		return
	}
	readCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	sourcePath := c.Query("path")
	if sourcePath == "" {
		sourcePath = "/"
	}
	source, err := strm.OpenSource(readCtx, uint(storageID), sourcePath)
	if err != nil {
		var sourceErr *strm.SourceError
		if errors.As(err, &sourceErr) {
			common.ErrorWithDataResp(c, errors.New(sourceErr.Message), 400, gin.H{"error_code": sourceErr.Code})
		} else {
			common.ErrorResp(c, err, 400)
		}
		return
	}
	items, err := source.List(readCtx, source.Root())
	if err != nil {
		var sourceErr *strm.SourceError
		if errors.As(err, &sourceErr) {
			common.ErrorWithDataResp(c, errors.New(sourceErr.Message), 400, gin.H{"error_code": sourceErr.Code})
		} else {
			common.ErrorResp(c, err, 400)
		}
		return
	}
	if err = readCtx.Err(); err != nil || !strm.Initialized() {
		common.ErrorStrResp(c, "目录读取已取消，结果不可用", 408)
		return
	}
	common.SuccessResp(c, gin.H{"storage_id": storageID, "path": source.Root().Path, "fingerprint": source.Binding.Fingerprint, "complete": true, "items": items})
}
