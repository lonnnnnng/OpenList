package conf

import (
	"errors"
	"path/filepath"
	"sync"
)

var ErrSTRMUnsupportedDriver = errors.New("driver is not enabled in STRM product mode")

// STRMConfig 将媒体任务与上游文件管理配置隔离，避免旧设置重新开启云端写任务。作者：long
type STRMConfig struct {
	Enabled         bool   `json:"enabled" env:"ENABLED"`
	OutputRoot      string `json:"output_root" env:"OUTPUT_ROOT"`
	PublicURL       string `json:"public_url" env:"PUBLIC_URL"`
	ShutdownSeconds int    `json:"shutdown_seconds" env:"SHUTDOWN_SECONDS"`
}

var (
	STRMCommandMode bool
	STRMOutputPath  string
	STRMOutputMu    sync.RWMutex
	ProductVersion  = "v0.1.0-dev"
	FrontendCommit  = "unknown"
	UpstreamCommit  = "de1cdb3a054628f32c410a79571848c052ce7cc2"
)

func IsSTRMMode() bool { return STRMCommandMode || (Conf != nil && Conf.STRM.Enabled) }

// ApplySTRMPreset 即使读到旧配置也不恢复协议服务，媒体服务器自行负责转码。作者：long
func ApplySTRMPreset(c *Config, dataDir string) {
	c.STRM.Enabled = true
	c.S3.Enable, c.FTP.Enable, c.SFTP.Enable, c.MCP.Enable = false, false, false, false
	c.Cdn = ""
	if c.STRM.OutputRoot == "" {
		c.STRM.OutputRoot = filepath.Join(dataDir, "output")
	}
	if c.STRM.ShutdownSeconds <= 0 || c.STRM.ShutdownSeconds > 120 {
		c.STRM.ShutdownSeconds = 30
	}
}

// STRMDriverAllowed 限制初始化范围，未知驱动保留源码但不运行未验收的Init副作用。作者：long
func STRMDriverAllowed(name string) bool {
	switch name {
	case "Quark", "UC", "AliyundriveOpen", "BaiduNetdisk", "Local":
		return true
	}
	return false
}
