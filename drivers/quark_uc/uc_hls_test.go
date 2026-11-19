package quark

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/go-resty/resty/v2"
)

type ucRoundTrip func(*http.Request) (*http.Response, error)

func (f ucRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUCSelectAccessibleHLS(t *testing.T) {
	video := func(res string, accessible bool) ucVideo {
		v := ucVideo{Resolution: res, Accessible: accessible}
		v.VideoInfo.URL = "https://video.drive.uc.cn/" + res + ".m3u8"
		v.VideoInfo.Format = "m3u8"
		return v
	}
	vs := []ucVideo{video("super", false), video("low", true), video("high", true)}
	best, err := selectUCVideo(vs, "auto")
	if err != nil || best.Resolution != "high" {
		t.Fatalf("highest accessible video: %+v %v", best, err)
	}
	best, err = selectUCVideo(vs, "low")
	if err != nil || best.Resolution != "low" {
		t.Fatal("fixed resolution not honored")
	}
	if _, err = selectUCVideo(vs, "super"); err == nil {
		t.Fatal("inaccessible video selected")
	}
	vs[2].VideoInfo.URL = "https://video.drive.uc.cn.evil.test/media.m3u8"
	if _, err = selectUCVideo(vs, "high"); err == nil {
		t.Fatal("untrusted media URL accepted")
	}
}

func TestUCPlaybackMergesFullLoginAndTemporaryAuth(t *testing.T) {
	old := base.RestyClient
	t.Cleanup(func() { base.RestyClient = old })
	base.RestyClient = resty.New().SetTransport(ucRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/1/clouddrive/file/v2/play" || r.Method != "POST" || cookie.GetStr(r.Header.Get("Cookie"), "__pus") != "login" {
			t.Fatal("invalid playback request")
		}
		return &http.Response{StatusCode: 200, Request: r, Header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"Video-Auth=new; Path=/", "__puus=renewed; Path=/"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"video_list":[{"resolution":"high","accessable":true,"video_info":{"format":"m3u8","url":"https://video.drive.uc.cn/high.m3u8"}}]}}`))}, nil
	}))
	d := &QuarkOrUC{Addition: Addition{Cookie: "__pus=login; __puus=old; other=keep"}, conf: Conf{api: "https://pc-api.uc.cn/1/clouddrive"}}
	source, err := d.ucHLSSource(context.Background(), "test-file", "auto")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"__pus": "login", "__puus": "renewed", "Video-Auth": "new", "other": "keep"} {
		if cookie.GetStr(source.Header.Get("Cookie"), k) != v {
			t.Errorf("missing merged %s", k)
		}
	}
	if cookie.GetStr(d.Cookie, "Video-Auth") != "" {
		t.Fatal("video-scoped auth persisted into account login")
	}
	if source.Refresh == nil {
		t.Fatal("no session refresh")
	}
	if base.RestyClient.GetClient().CheckRedirect != nil {
		t.Fatal("UC playback changed the shared client's redirect policy")
	}
	if _, err := source.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUCDoesNotChangeQuarkPlaybackSelection(t *testing.T) {
	f := &File{Category: 1, Size: 10, File: true}
	d := &QuarkOrUC{Addition: Addition{UseTransCodingAddress: true}, config: driver.Config{Name: "Quark"}}
	if d.IsHLS(f) {
		t.Fatal("Quark playback changed")
	}
	d.config.Name = "UC"
	if !d.IsHLS(f) {
		t.Fatal("UC transcoding not selected")
	}
	d.UseTransCodingAddress = false
	if d.IsHLS(f) {
		t.Fatal("original UC download forced to HLS")
	}
}
