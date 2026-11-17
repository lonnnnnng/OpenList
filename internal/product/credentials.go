package product

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func secretField(key string) bool {
	switch strings.ToLower(key) {
	case "password", "cookie", "refresh_token", "access_token", "accesstoken", "client_secret", "api_key", "token":
		return true
	}
	return false
}

// MaskAddition 管理响应只声明凭据是否已配置，不能将SQLite里的授权原文回传浏览器。作者：long
func MaskAddition(raw string) (string, []string) {
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &values) != nil || values == nil {
		// long: 无法确认字段边界的损坏配置不作为调试原文输出，避免意外回显凭据。
		return "{}", nil
	}
	var configured []string
	for key, value := range values {
		if !secretField(key) {
			continue
		}
		var text string
		if json.Unmarshal(value, &text) == nil && text != "" {
			configured = append(configured, key)
		}
		values[key] = json.RawMessage(`""`)
	}
	sort.Strings(configured)
	encoded, _ := json.Marshal(values)
	return string(encoded), configured
}

// MergeAdditionSecrets 留空代表保留最新数据库凭据，显式清除只接受已知授权字段。作者：long
func MergeAdditionSecrets(current, incoming string, clear []string) (string, error) {
	var old, next map[string]json.RawMessage
	if json.Unmarshal([]byte(current), &old) != nil || json.Unmarshal([]byte(incoming), &next) != nil || next == nil {
		return "", fmt.Errorf("存储附加配置不是有效JSON对象")
	}
	cleared := make(map[string]bool)
	for _, key := range clear {
		if !secretField(key) {
			return "", fmt.Errorf("不能通过凭据清除动作修改普通配置")
		}
		cleared[key] = true
	}
	for key, value := range old {
		if !secretField(key) {
			continue
		}
		if cleared[key] {
			next[key] = json.RawMessage(`""`)
			continue
		}
		var text string
		if next[key] == nil || string(next[key]) == "null" || (json.Unmarshal(next[key], &text) == nil && text == "") {
			next[key] = value
		}
	}
	encoded, err := json.Marshal(next)
	return string(encoded), err
}
