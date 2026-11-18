package handles

import (
	"encoding/json"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestMediaScanPathStoresStrmOutputOverride(t *testing.T) {
	setupMediaReviewTest(t)
	created := invokeHandler(t, nil, map[string]any{
		"media_type": "video",
		"name":       "Aliyun",
		"path":       "/alipan/videos",
		"strm_path":  " /alipan/strm ",
	}, CreateMediaScanPath)
	var createResp struct {
		Code int                 `json:"code"`
		Data model.MediaScanPath `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	if createResp.Code != 200 || createResp.Data.StrmPath != "/alipan/strm" {
		t.Fatalf("create response = %s", created.Body.String())
	}

	updated := invokeHandler(t, nil, map[string]any{
		"id":         createResp.Data.ID,
		"media_type": "video",
		"name":       "Aliyun",
		"path":       "/alipan/videos",
		"strm_path":  "",
	}, UpdateMediaScanPath)
	if updated.Code != 200 {
		t.Fatalf("update status = %d: %s", updated.Code, updated.Body.String())
	}
	got, err := db.GetMediaScanPath(createResp.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StrmPath != "" {
		t.Fatalf("cleared STRM path = %q, want global fallback", got.StrmPath)
	}
}
