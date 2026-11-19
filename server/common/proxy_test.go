package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func TestProxyOverridesUpstreamContentDisposition(t *testing.T) {
	previousConfig := conf.Conf
	conf.Conf = conf.DefaultConfig("data")
	t.Cleanup(func() {
		conf.Conf = previousConfig
	})

	const content = "archive content"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="download"`)
		w.Header().Set("Content-Type", "application/x-rar-compressed")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, content)
	}))
	t.Cleanup(upstream.Close)

	file := &model.Object{
		Name: "测试文件.rar",
		Size: int64(len(content)),
	}
	link := &model.Link{URL: upstream.URL}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/sd/example", nil)

	err := Proxy(recorder, request, link, file)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status code = %d, want %d", got, want)
	}
	if got, want := recorder.Header().Get("Content-Disposition"), utils.GenerateContentDisposition(file.GetName()); got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	if got, want := recorder.Header().Get("Content-Type"), "application/x-rar-compressed"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := recorder.Body.String(), content; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestCloudHLSRedirectKeepsCredentialsOnServer(t *testing.T) {
	old := conf.Conf
	conf.Conf = conf.DefaultConfig(t.TempDir())
	conf.Conf.SiteURL = "https://media.example/openlist"
	t.Cleanup(func() { conf.Conf = old })
	link := &model.Link{
		HLS: &model.HLSSource{URL: "https://video.drive.uc.cn/media.m3u8?secret=hidden", Header: http.Header{"Cookie": {"login=hidden"}}},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://localhost/d/video.mp4", nil)
	if err := Proxy(w, r, link, &model.Object{Name: "video.mp4", Size: 100000}); err != nil {
		t.Fatal(err)
	}
	location := w.Header().Get("Location")
	if w.Code != 302 || !strings.HasPrefix(location, "https://media.example/openlist/hls/") || !strings.HasSuffix(location, "/index.m3u8") || strings.Contains(location, "hidden") {
		t.Fatalf("invalid local playlist redirect: %d %s", w.Code, location)
	}
	if ProxyRange(r.Context(), link, 100000) != link {
		t.Fatal("HLS was converted into an original-file byte range reader")
	}
	if link.Clone().HLS != link.HLS {
		t.Fatal("cloning lost the HLS source")
	}
}
