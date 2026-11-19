package cloudhls

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, body string) *http.Response {
	return &http.Response{Request: r, StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestRewritePlaylistResources(t *testing.T) {
	raw := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,URI="audio/list.m3u8?secret=audio"
#EXT-X-KEY:METHOD=AES-128,URI="key.bin?secret=key"
#EXT-X-MAP:URI="init.mp4?secret=init",BYTERANGE="100@0"
#EXT-X-STREAM-INF:BANDWIDTH=300000
variant/playlist.m3u8?secret=video
`
	body, resources, err := rewrite([]byte(raw), "https://video.drive.uc.cn/root/master.m3u8", "index.m3u8", "/hls/session")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret") || strings.Contains(string(body), "drive.uc.cn") || len(resources) != 4 {
		t.Fatalf("unrewritten resources: %s", body)
	}
	playlists := 0
	for _, r := range resources {
		if r.playlist {
			playlists++
		}
		if !strings.HasPrefix(r.url, "https://video.drive.uc.cn/root/") {
			t.Fatal("incorrect relative path")
		}
	}
	if playlists != 2 {
		t.Fatalf("playlist count %d", playlists)
	}
	body2, _, err := rewrite([]byte(strings.ReplaceAll(raw, "secret=", "token=")), "https://video.drive.uc.cn/root/master.m3u8", "index.m3u8", "/hls/session")
	if err != nil || string(body2) != string(body) {
		t.Fatal("resource IDs changed when signed queries rotated")
	}
}

func TestRejectUntrustedPlaylistResources(t *testing.T) {
	for _, raw := range []string{
		"not a playlist", "#EXTM3U\nhttps://evil.test/video.ts", "#EXTM3U\n//evil.test/video.ts", "#EXTM3U\nfile:///etc/passwd", "#EXTM3U\nhttp://video.drive.uc.cn/video.ts", "#EXTM3U\nhttps://user:pass@video.drive.uc.cn/video.ts", "#EXTM3U\n#EXT-X-KEY:URI=unquoted.key",
	} {
		if _, _, err := rewrite([]byte(raw), "https://video.drive.uc.cn/master.m3u8", "index.m3u8", "/hls/token"); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func openTest(t *testing.T, p *Proxy, source *model.HLSSource) (string, string) {
	t.Helper()
	u, err := p.Open(source, "https://openlist.test/base/hls")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(u, "/")
	token := parts[len(parts)-2]
	return token, "https://openlist.test/base/hls/" + token
}
func get(p *Proxy, token, id, prefix string, headers http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", prefix+"/"+id, nil)
	r.Header = headers
	w := httptest.NewRecorder()
	p.Serve(w, r, token, id, prefix)
	return w
}
func resourceID(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			u, err := url.Parse(line)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(u.Path, "/")
			return parts[len(parts)-1]
		}
	}
	t.Fatal("no resource")
	return ""
}

func TestProxyRefreshesExpiredSegmentAndDoesNotForwardCredentials(t *testing.T) {
	var refreshes atomic.Int32
	var segmentRequests atomic.Int32
	p := New(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || strings.Contains(r.Header.Get("Cookie"), "client-secret") {
			t.Fatal("client headers leaked upstream")
		}
		if !strings.Contains(r.Header.Get("Cookie"), "login=server") {
			t.Fatal("missing full provider Cookie")
		}
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			expected := "old"
			if refreshes.Load() > 0 {
				expected = "new"
			}
			if r.URL.Query().Get("token") != expected {
				t.Fatal("playlist not refreshed")
			}
			return response(r, 200, "#EXTM3U\n#EXTINF:5,\nsegment.ts?token="+expected+"\n#EXT-X-ENDLIST\n"), nil
		}
		segmentRequests.Add(1)
		if r.URL.Query().Get("token") == "old" {
			return response(r, 412, "expired"), nil
		}
		if r.Header.Get("Range") != "bytes=2-5" || !strings.Contains(r.Header.Get("Cookie"), "Video-Auth=new") {
			t.Fatal("range or renewed auth missing")
		}
		res := response(r, 206, "data")
		res.Header.Set("Content-Range", "bytes 2-5/20")
		res.Header.Set("Set-Cookie", "provider-secret=hidden")
		res.Header.Set("Location", "https://private.test")
		return res, nil
	})})
	source := &model.HLSSource{URL: "https://video.drive.uc.cn/media.m3u8?token=old", Header: http.Header{"Cookie": {"login=server; Video-Auth=old"}}}
	source.Refresh = func(context.Context) (*model.HLSSource, error) {
		refreshes.Add(1)
		return &model.HLSSource{URL: "https://video.drive.uc.cn/media.m3u8?token=new", Header: http.Header{"Cookie": {"login=server; Video-Auth=new"}}, Refresh: source.Refresh}, nil
	}
	token, prefix := openTest(t, p, source)
	manifest := get(p, token, "index.m3u8", prefix, nil)
	if manifest.Code != 200 || strings.Contains(manifest.Body.String(), "token=") {
		t.Fatal("invalid proxy playlist")
	}
	id := resourceID(t, manifest.Body.String())
	w := get(p, token, id, prefix, http.Header{"Range": {"bytes=2-5"}, "Cookie": {"client-secret=bad"}, "Authorization": {"client-auth"}})
	if w.Code != 206 || w.Body.String() != "data" || w.Header().Get("Content-Range") != "bytes 2-5/20" || refreshes.Load() != 1 || segmentRequests.Load() != 2 {
		t.Fatalf("refresh failed: %d %s refreshes=%d", w.Code, w.Body, refreshes.Load())
	}
	if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
		t.Fatal("upstream credentials exposed")
	}
	if got := get(p, token, "arbitrary.ts", prefix, nil); got.Code != 404 {
		t.Fatal("unregistered resource accepted")
	}
	if got := get(p, "invalid", id, prefix, nil); got.Code != 410 {
		t.Fatal("unknown session accepted")
	}
}

func TestProxyRefreshesNestedPlaylists(t *testing.T) {
	refreshed := false
	p := New(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/master.m3u8":
			return response(r, 200, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000\nchild.m3u8\n"), nil
		case "/child.m3u8":
			return response(r, 200, fmt.Sprintf("#EXTM3U\nseg.ts?fresh=%t\n", refreshed)), nil
		case "/seg.ts":
			if r.URL.Query().Get("fresh") == "false" {
				return response(r, 403, "expired"), nil
			}
			return response(r, 200, "ts-data"), nil
		}
		t.Fatalf("unexpected URL path %s", r.URL.Path)
		return nil, nil
	})})
	source := &model.HLSSource{URL: "https://video.drive.uc.cn/master.m3u8"}
	source.Refresh = func(context.Context) (*model.HLSSource, error) { refreshed = true; return source, nil }
	token, prefix := openTest(t, p, source)
	child := resourceID(t, get(p, token, "index.m3u8", prefix, nil).Body.String())
	segment := resourceID(t, get(p, token, child, prefix, nil).Body.String())
	w := get(p, token, segment, prefix, nil)
	if w.Code != 200 || w.Body.String() != "ts-data" || !refreshed {
		t.Fatalf("nested refresh: %d %s", w.Code, w.Body)
	}
}

func TestProxyBoundsSessionsAndRedactsUpstreamErrors(t *testing.T) {
	p := New(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(r, 302, "secret body"), nil })})
	source := &model.HLSSource{URL: "https://video.drive.uc.cn/media.m3u8?secret=hidden"}
	token, prefix := openTest(t, p, source)
	again, _ := openTest(t, p, source)
	if token != again {
		t.Fatal("repeated entry allocated another session")
	}
	w := get(p, token, "index.m3u8", prefix, nil)
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("unsafe upstream error: %s", w.Body)
	}
	p.sessions[token].expires = time.Now().Add(-time.Second)
	if get(p, token, "index.m3u8", prefix, nil).Code != 410 {
		t.Fatal("expired session accepted")
	}
	for i := 0; i < maxSessions; i++ {
		_, err := p.Open(&model.HLSSource{URL: fmt.Sprintf("https://video.drive.uc.cn/%d.m3u8", i)}, "/hls")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Open(source, "/hls"); err == nil {
		t.Fatal("session limit not enforced")
	}
}
