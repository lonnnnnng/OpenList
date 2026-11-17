package handles

import (
	"fmt"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

func ListDriverInfo(c *gin.Context) {
	info := op.GetDriverInfoMap()
	if conf.IsSTRMMode() {
		filtered := make(map[string]driver.Info)
		for name, value := range info {
			if conf.STRMDriverAllowed(name) {
				filtered[name] = strmDriverInfo(value)
			}
		}
		info = filtered
	}
	common.SuccessResp(c, info)
}

func ListDriverNames(c *gin.Context) {
	if conf.IsSTRMMode() {
		common.SuccessResp(c, []string{"Quark", "AliyundriveOpen", "BaiduNetdisk", "UC", "Local"})
		return
	}
	common.SuccessResp(c, op.GetDriverNames())
}

func GetDriverInfo(c *gin.Context) {
	driverName := c.Query("driver")
	if conf.IsSTRMMode() && !conf.STRMDriverAllowed(driverName) {
		common.ErrorStrResp(c, "产品模式未开放此驱动", 404)
		return
	}
	infoMap := op.GetDriverInfoMap()
	items, ok := infoMap[driverName]
	if !ok {
		common.ErrorStrResp(c, fmt.Sprintf("driver [%s] not found", driverName), 404)
		return
	}
	if conf.IsSTRMMode() {
		items = strmDriverInfo(items)
	}
	common.SuccessResp(c, items)
}

func strmDriverInfo(info driver.Info) driver.Info {
	// long: 旧协议和云端写入设置不参与受管播放；复制字段列表，避免修改上游共享元数据。
	common := make([]driver.Item, 0, len(info.Common))
	for _, item := range info.Common {
		switch item.Name {
		case "mount_path", "order", "remark", "cache_expiration":
			common = append(common, item)
		}
	}
	additional := make([]driver.Item, 0, len(info.Additional))
	for _, item := range info.Additional {
		switch item.Name {
		case "remove_way", "rapid_upload", "internal_upload", "upload_thread", "upload_timeout", "upload_api", "use_dynamic_upload_api", "custom_upload_part_size", "low_bandwith_upload_mode":
			continue
		}
		additional = append(additional, item)
	}
	info.Common, info.Additional = common, additional
	return info
}
