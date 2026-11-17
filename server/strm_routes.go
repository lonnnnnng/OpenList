package server

import (
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/server/handles"
	"github.com/gin-gonic/gin"
)

func strmAdminRoutes(g *gin.RouterGroup) {
	if conf.IsSTRMMode() {
		g.POST("/strm/tasks/:id/generate", handles.STRMGenerateTask)
		g.GET("/strm/health", handles.STRMHealth)
		g.GET("/strm/browse", handles.STRMBrowse)
		g.GET("/strm/tasks", handles.STRMTasks)
		g.POST("/strm/tasks", handles.STRMCreateTask)
		g.GET("/strm/tasks/:id", handles.STRMGetTask)
		g.PUT("/strm/tasks/:id", handles.STRMUpdateTask)
		g.DELETE("/strm/tasks/:id", handles.STRMDeleteTask)
		g.POST("/strm/tasks/:id/preview", handles.STRMPreviewTask)
		g.GET("/strm/tasks/:id/missing-preview", handles.STRMMissingPreview)
		g.POST("/strm/tasks/:id/trash-preview", handles.STRMTrashPreview)
		g.POST("/strm/tasks/:id/trash-preview/:plan_id/execute", handles.STRMTrashExecute)
		g.GET("/strm/tasks/:id/trash-plans", handles.STRMTrashPlans)
		g.GET("/strm/tasks/:id/trash", handles.STRMTrashList)
		g.POST("/strm/tasks/:id/trash/:artifact_id/restore", handles.STRMTrashRestore)
		g.GET("/strm/tasks/:id/runs", handles.STRMTaskRuns)
		g.GET("/strm/runs/:id", handles.STRMGetRun)
	}
}

func strmPublicRoutes(g *gin.RouterGroup) {
	if conf.IsSTRMMode() {
		g.GET("/strm/:file_id", handles.STRMPlayback)
		g.HEAD("/strm/:file_id", handles.STRMPlayback)
	}
}
