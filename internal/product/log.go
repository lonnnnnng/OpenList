package product

import (
	"github.com/sirupsen/logrus"
	"regexp"
)

var secretQuery = regexp.MustCompile(`(?i)(refresh_ui|refresh_token|access_token|api_key|password|sign|token|cookie)=([^&\s"'<>]+)`)
var secretJSON = regexp.MustCompile(`(?i)("(?:refresh_token|access_token|client_secret|api_key|password|token|cookie)"\s*:\s*)"(?:\\.|[^"\\])*"`)
var secretUserInfo = regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`)

func redact(message string) string {
	message = secretJSON.ReplaceAllString(message, `${1}"[redacted]"`)
	message = secretUserInfo.ReplaceAllString(message, `${1}[redacted]@`)
	return secretQuery.ReplaceAllString(message, "${1}=[redacted]")
}

func Redact(message string) string { return redact(message) }

// RedactingFormatter 上游HTTP错误也可能携带授权查询串，不能只脱敏浏览器访问日志。作者：long
type RedactingFormatter struct{ Inner logrus.Formatter }

func (f RedactingFormatter) Format(e *logrus.Entry) ([]byte, error) {
	// long: 先脱敏原始消息，再交给formatter转义，避免JSON响应在转义后绕过匹配；不修改共享Entry。
	copy := *e
	copy.Message = redact(e.Message)
	copy.Data = make(logrus.Fields, len(e.Data))
	for key, value := range e.Data {
		if secretField(key) {
			copy.Data[key] = "[redacted]"
			continue
		}
		switch value := value.(type) {
		case string:
			copy.Data[key] = redact(value)
		case error:
			copy.Data[key] = redact(value.Error())
		default:
			copy.Data[key] = value
		}
	}
	return f.Inner.Format(&copy)
}
