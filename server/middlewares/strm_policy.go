package middlewares

import (
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
)

// STRMPolicy 限制旧API，不能仅隐藏菜单却保留云写入、协议或媒体扫描旁路。作者：long
func STRMPolicy(c *gin.Context) {
	if !conf.IsSTRMMode() {
		c.Next()
		return
	}
	base := ""
	if conf.URL != nil {
		base = strings.TrimSuffix(conf.URL.Path, "/")
	}
	path := strings.TrimPrefix(c.Request.URL.Path, base)
	if strings.HasPrefix(path, "/api/") {
		allowed := false
		switch path {
		case "/api/public/settings", "/api/public/init_status", "/api/public/init/setup", "/api/public/env_check", "/api/auth/login", "/api/auth/login/hash", "/api/auth/logout", "/api/me", "/api/me/update", "/api/auth/2fa/generate", "/api/auth/2fa/verify", "/api/fs/list", "/api/fs/get", "/api/fs/dirs", "/api/admin/setting/get", "/api/admin/setting/list":
			allowed = true
		}
		for _, prefix := range []string{"/api/admin/storage/", "/api/admin/driver/", "/api/admin/strm/", "/api/public/strm/"} {
			if strings.HasPrefix(path, prefix) {
				allowed = true
			}
		}
		if !allowed {
			denySTRMLegacy(c)
			return
		}
		if c.FullPath() == "" {
			c.AbortWithStatusJSON(404, common.Resp[any]{Code: 404, Message: "接口不存在"})
			return
		}
	} else {
		for _, prefix := range []string{"/dav", "/s3", "/@emby", "/@mediabrowser", "/@rest", "/d", "/p", "/ad", "/ap", "/ae", "/sd", "/sad", "/tc"} {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				denySTRMLegacy(c)
				return
			}
		}
	}
	c.Next()
}

func denySTRMLegacy(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusForbidden, common.Resp[any]{Code: 403, Message: "产品模式已关闭此旧接口；网盘同步只读，播放使用受管文件入口"})
}

// STRMAdmin 在认证链内部校验身份，不能在Auth调用Next、执行完业务之后才检查管理员。作者：long
func STRMAdmin(c *gin.Context) {
	if conf.IsSTRMMode() {
		AuthAdmin(c)
		return
	}
	c.Next()
}

// STRMRecovery 不转储HTTP请求；panic本身可能含凭据，因此只记录固定诊断。作者：long
func STRMRecovery(c *gin.Context) {
	defer func() {
		if recover() != nil {
			log.Error("STRM请求异常，已中止")
			c.AbortWithStatusJSON(500, common.Resp[any]{Code: 500, Message: "请求异常，请查看任务状态后重试"})
		}
	}()
	c.Next()
}
