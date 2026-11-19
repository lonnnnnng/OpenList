package baidu_netdisk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

type webRoundTripper func(*http.Request) (*http.Response, error)

func (f webRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func webResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{Request: r, StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func webTestDriver(t *testing.T, handler webRoundTripper) *BaiduNetdisk {
	t.Helper()
	d := &BaiduNetdisk{Addition: Addition{AuthType: "cookie", Cookie: "BDUSS=test-login"}, webClient: &http.Client{Transport: handler}}
	return d
}

func TestWebPaginationFiltersAfterPaging(t *testing.T) {
	var pages []int
	d := webTestDriver(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "BDUSS=test-login" || r.URL.Query().Get("access_token") != "" {
			t.Fatal("web requests must use the Cookie without OAuth credentials")
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		files := []File{}
		if page == 1 {
			for id := 1; id <= webPageSize; id++ {
				files = append(files, File{FsId: int64(id), Category: 2})
			}
		} else if page == 2 {
			files = []File{{FsId: 101, Category: 1, Path: "/视频/001.mp4"}, {FsId: 102, Isdir: 1, Path: "/视频/剧集"}}
		} else {
			t.Fatalf("unexpected page %d", page)
		}
		raw, _ := json.Marshal(ListResp{List: files})
		return webResponse(r, 200, string(raw)), nil
	})
	d.OnlyListVideoFile = true
	files, err := d.List(context.Background(), &model.Object{Path: "/视频"}, model.ListArgs{})
	if err != nil || len(files) != 2 || fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("pagination/filtering: files=%d pages=%v error=%v", len(files), pages, err)
	}
}

func TestWebPaginationRejectsRepeatedPage(t *testing.T) {
	d := webTestDriver(t, func(r *http.Request) (*http.Response, error) {
		files := make([]File, webPageSize)
		for i := range files {
			files[i].FsId = int64(i + 1)
		}
		raw, _ := json.Marshal(ListResp{List: files})
		return webResponse(r, 200, string(raw)), nil
	})
	_, err := d.getWebFiles(context.Background(), "/")
	if err == nil || !strings.Contains(err.Error(), "pagination") {
		t.Fatalf("expected explicit incomplete-list error, got %v", err)
	}
}

func TestWebLinkResolvesWithoutLeakingCookie(t *testing.T) {
	path := "/视频/带\"引号 & 空格.mp4"
	var calls int
	d := webTestDriver(t, func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Hostname() {
		case "pan.baidu.com":
			if r.URL.Path == "/api/list" {
				return webResponse(r, 200, `{"errno":0,"list":[]}`), nil
			}
			if r.Method != http.MethodPost {
				t.Fatal("metadata must be posted")
			}
			if r.URL.Query().Get("logid") == "" {
				t.Fatal("metadata without logid produces an unusable download URL")
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			var paths []string
			if json.Unmarshal([]byte(r.PostForm.Get("target")), &paths) != nil || len(paths) != 1 || paths[0] != path {
				t.Fatal("path was not JSON and form encoded correctly")
			}
			raw, _ := json.Marshal(map[string]any{"errno": 0, "info": []any{map[string]any{"path": path, "dlink": "https://d.pcs.baidu.com/file/test?secret=hidden"}}})
			return webResponse(r, 200, string(raw)), nil
		case "d.pcs.baidu.com":
			if r.Header.Get("Cookie") == "" || r.Header.Get("Range") != "bytes=0-0" {
				t.Fatal("resolver needs login headers and a bounded GET")
			}
			res := webResponse(r, 302, "")
			res.Header.Set("Location", "https://cdn.baidupcs.com/file/test?expires="+strconv.FormatInt(time.Now().Add(2*time.Minute).Unix(), 10))
			return res, nil
		default:
			t.Fatal("login request followed redirect to CDN")
			return nil, errors.New("unexpected host")
		}
	})
	if err := d.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	link, err := d.Link(context.Background(), &model.Object{Path: path}, model.LinkArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(link.Header) != 0 || !strings.HasPrefix(link.URL, "https://cdn.baidupcs.com/") || link.Expiration == nil || *link.Expiration > 90*time.Second || *link.Expiration <= 0 {
		t.Fatalf("unsafe redirect or invalid cache expiry: calls=%d header=%v expiration=%v", calls, link.Header, link.Expiration)
	}
	if d.Config().PreferProxy || !d.Config().NoUpload {
		t.Fatal("Cookie mode should prefer direct playback and disable upload")
	}
}

func TestWebAPIErrorsDoNotLookLikeEmptyDirectories(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		expired bool
	}{
		{"expired", 200, `{"errno":-6}`, true},
		{"unauthorized", 401, "login", true},
		{"login page", 200, "<html>login</html>", false},
		{"missing errno", 200, `{"list":[]}`, false},
		{"missing file", 200, `{"errno":-9}`, false},
		{"rate limit", 429, "try later", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := webTestDriver(t, func(r *http.Request) (*http.Response, error) { return webResponse(r, tc.status, tc.body), nil })
			_, err := d.getWebFiles(context.Background(), "/")
			if err == nil || errors.Is(err, errWebCookieExpired) != tc.expired {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebRejectsInvalidDownloadURLs(t *testing.T) {
	for _, target := range []string{"http://d.pcs.baidu.com/file", "https://pcs.baidu.com.evil.test/file", "https://evilpcs.baidu.com/file", "https://user:pass@d.pcs.baidu.com/file", "https://d.pcs.baidu.com:8080/file", "/file"} {
		u, _ := url.Parse(target)
		if validWebDownloadURL(u, "pcs.baidu.com") {
			t.Errorf("accepted %q", target)
		}
	}
	for _, target := range []string{"https://d.pcs.baidu.com/file", "https://pcs.baidu.com:443/file"} {
		u, _ := url.Parse(target)
		if !validWebDownloadURL(u, "pcs.baidu.com") {
			t.Errorf("rejected %q", target)
		}
	}
}

func TestWebReadOnlyOperationsNeverCallOAuth(t *testing.T) {
	d := webTestDriver(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network request"); return nil, nil })
	ctx := context.Background()
	obj := &model.Object{Path: "/test"}
	checks := []func() error{
		func() error { _, err := d.MakeDir(ctx, obj, "new"); return err },
		func() error { _, err := d.Move(ctx, obj, obj); return err },
		func() error { _, err := d.Rename(ctx, obj, "new"); return err },
		func() error { return d.Copy(ctx, obj, obj) },
		func() error { return d.Remove(ctx, obj) },
		func() error { _, err := d.Put(ctx, obj, nil, nil); return err },
		func() error { _, err := d.PutRapid(ctx, obj, nil); return err },
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, errs.NotSupport) {
			t.Errorf("operation %d: %v", i, err)
		}
	}
	if (&BaiduNetdisk{Addition: Addition{AuthType: "oauth"}}).Config() != config || (&BaiduNetdisk{}).Config() != config {
		t.Fatal("existing OAuth storage defaults changed")
	}
}

func TestWebCancellationAndErrorRedaction(t *testing.T) {
	d := webTestDriver(t, func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.webRequest(ctx, "GET", "https://d.pcs.baidu.com/file?secret=hidden", nil)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("cancellation or redaction failed: %v", err)
	}
}

func TestWebLinkRejectsMissingMetadataAndUnusableRedirects(t *testing.T) {
	for _, tc := range []struct {
		name       string
		metadata   string
		status     int
		location   string
		resolverOK bool
	}{
		{"empty info", `{"errno":0,"info":[]}`, 302, "https://cdn.baidupcs.com/file", false},
		{"wrong file", `{"errno":0,"info":[{"path":"/other","dlink":"https://d.pcs.baidu.com/file"}]}`, 302, "https://cdn.baidupcs.com/file", false},
		{"per-file error", `{"errno":0,"info":[{"path":"/test","errno":-9,"dlink":"https://d.pcs.baidu.com/file"}]}`, 302, "https://cdn.baidupcs.com/file", false},
		{"untrusted resolver", `{"errno":0,"info":[{"path":"/test","dlink":"https://evil.test/file"}]}`, 302, "https://cdn.baidupcs.com/file", false},
		{"denied", "", 403, "", true},
		{"media instead of redirect", "", 200, "", true},
		{"missing location", "", 302, "", true},
		{"untrusted CDN", "", 302, "https://evil.test/file", true},
		{"expired CDN", "", 302, "https://cdn.baidupcs.com/file?expires=1000000001", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolverCalled := false
			d := webTestDriver(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Hostname() == "pan.baidu.com" {
					body := tc.metadata
					if body == "" {
						body = `{"errno":0,"info":[{"path":"/test","dlink":"https://d.pcs.baidu.com/file"}]}`
					}
					return webResponse(r, 200, body), nil
				}
				resolverCalled = true
				res := webResponse(r, tc.status, "")
				res.Header.Set("Location", tc.location)
				return res, nil
			})
			if _, err := d.linkWeb(context.Background(), &model.Object{Path: "/test"}); err == nil {
				t.Fatal("unusable download link accepted")
			}
			if resolverCalled != tc.resolverOK {
				t.Fatal("invalid metadata reached download resolver")
			}
		})
	}
}
