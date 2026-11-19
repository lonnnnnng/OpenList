package handles

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
)

func TestRedirectCloudHLSKeepsProviderAuthenticationOnServer(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://openlist.test/d/uc/video.mp4", nil)
	redirect(c, &model.Link{HLS: &model.HLSSource{
		URL:    "https://video.drive.uc.cn/playlist.m3u8?secret=upstream",
		Header: http.Header{"Cookie": {"Video-Auth=private"}},
	}})
	location := w.Header().Get("Location")
	if w.Code != http.StatusFound || !strings.Contains(location, "/hls/") || !strings.HasSuffix(location, "/index.m3u8") {
		t.Fatalf("expected local HLS entry, got HTTP %d", w.Code)
	}
	if strings.Contains(location+w.Body.String(), "upstream") || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("provider credentials exposed")
	}
}
