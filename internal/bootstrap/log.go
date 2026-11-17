package bootstrap

import (
	"io"
	"log"
	"os"

	"github.com/OpenListTeam/OpenList/v4/cmd/flags"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/product"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/natefinch/lumberjack"
	"github.com/sirupsen/logrus"
)

func init() {
	formatter := logrus.TextFormatter{
		ForceColors:               true,
		EnvironmentOverrideColors: true,
		TimestampFormat:           "2006-01-02 15:04:05",
		FullTimestamp:             true,
	}
	logrus.SetFormatter(&formatter)
	utils.Log.SetFormatter(&formatter)
	// logrus.SetLevel(logrus.DebugLevel)
}

func setLog(l *logrus.Logger) {
	if !conf.IsSTRMMode() && (flags.Debug || flags.Dev) {
		l.SetLevel(logrus.DebugLevel)
		l.SetReportCaller(true)
	} else {
		l.SetLevel(logrus.InfoLevel)
		l.SetReportCaller(false)
	}
}

func protectSTRMLogs() {
	if conf.IsSTRMMode() {
		// long: InitConfig也会打印网络错误；必须在读取配置之前保护日志，重复调用不叠加formatter。
		if _, protected := logrus.StandardLogger().Formatter.(product.RedactingFormatter); !protected {
			logrus.SetFormatter(product.RedactingFormatter{Inner: logrus.StandardLogger().Formatter})
		}
		if _, protected := utils.Log.Formatter.(product.RedactingFormatter); !protected {
			utils.Log.SetFormatter(product.RedactingFormatter{Inner: utils.Log.Formatter})
		}
	}
}

func Log() {
	protectSTRMLogs()
	setLog(logrus.StandardLogger())
	setLog(utils.Log)
	logConfig := conf.Conf.Log
	if logConfig.Enable {
		var w io.Writer = &lumberjack.Logger{
			Filename:   logConfig.Name,
			MaxSize:    logConfig.MaxSize, // megabytes
			MaxBackups: logConfig.MaxBackups,
			MaxAge:     logConfig.MaxAge,   //days
			Compress:   logConfig.Compress, // disabled by default
		}
		if flags.Debug || flags.Dev || flags.LogStd {
			w = io.MultiWriter(os.Stdout, w)
		}
		logrus.SetOutput(w)
	}
	log.SetOutput(logrus.StandardLogger().Out)
	utils.Log.Infof("init logrus...")
	utils.Log = logrus.StandardLogger()
}
