package product

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCredentialsMaskedAndRetainedAcrossOrdinaryEdits(t *testing.T) {
	current := `{"cookie":"private-cookie","refresh_token":"rotated-token","AccessToken":"private-access","root_folder_id":"root"}`
	masked, configured := MaskAddition(current)
	if strings.Contains(masked, "private-") || strings.Contains(masked, "rotated-token") || len(configured) != 3 {
		t.Fatal("credentials exposed or configured state lost")
	}
	merged, err := MergeAdditionSecrets(current, `{"cookie":"","refresh_token":"","root_folder_id":"new-root"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err = json.Unmarshal([]byte(merged), &values); err != nil {
		t.Fatal(err)
	}
	if values["cookie"] != "private-cookie" || values["refresh_token"] != "rotated-token" || values["AccessToken"] != "private-access" || values["root_folder_id"] != "new-root" {
		t.Fatal("ordinary edit lost stored credentials or ignored root edit")
	}
	cleared, err := MergeAdditionSecrets(current, masked, []string{"cookie"})
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(cleared), &values)
	if values["cookie"] != "" || values["refresh_token"] != "rotated-token" {
		t.Fatal("explicit clear changed another credential")
	}
	if _, err = MergeAdditionSecrets(current, masked, []string{"root_folder_id"}); err == nil {
		t.Fatal("ordinary config accepted as secret clear")
	}
	if masked, _ = MaskAddition(`{"cookie":"broken-secret"`); masked != "{}" {
		t.Fatal("broken raw config exposed")
	}
}
