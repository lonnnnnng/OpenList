package baidu_netdisk

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/net"
)

const (
	webBaseURL   = "https://pan.baidu.com"
	webUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"
	webPageSize  = 100
)

var errWebCookieExpired = errors.New("Baidu web Cookie is invalid or expired; copy the complete Cookie from a logged-in pan.baidu.com /api/list request")

func (d *BaiduNetdisk) cookieAuth() bool { return d.AuthType == "cookie" }

func (d *BaiduNetdisk) initWeb(ctx context.Context) error {
	d.Cookie = strings.TrimSpace(d.Cookie)
	if len(d.Cookie) >= 7 && strings.EqualFold(d.Cookie[:7], "cookie:") {
		d.Cookie = strings.TrimSpace(d.Cookie[7:])
	}
	if d.Cookie == "" || strings.ContainsAny(d.Cookie, "\r\n") {
		return errors.New("Baidu Cookie login requires the complete Cookie request header value on a single line")
	}
	if d.webClient == nil {
		d.webClient = net.NewHttpClient()
	}
	d.webClient.Timeout = 30 * time.Second
	// Resolve only the first redirect. Never forward the login Cookie to a CDN.
	d.webClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var result ListResp
	return d.webAPI(ctx, "/api/list", url.Values{
		"dir": {"/"}, "page": {"1"}, "num": {"1"},
	}, nil, &result)
}

// webRequest deliberately avoids logging request URLs and response bodies: both
// can contain credentials or signed media links.
func (d *BaiduNetdisk) webRequest(ctx context.Context, method, target string, form url.Values) (*http.Response, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, errors.New("invalid Baidu web request")
	}
	req.Header.Set("Cookie", d.Cookie)
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Referer", webBaseURL+"/")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodGet && req.URL.Hostname() != "pan.baidu.com" {
		req.Header.Set("Range", "bytes=0-0")
	}
	res, err := d.webClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("Baidu web request failed: %w", err)
	}
	return res, nil
}

func (d *BaiduNetdisk) webAPI(ctx context.Context, path string, query, form url.Values, result any) error {
	if query == nil {
		query = make(url.Values)
	}
	query.Set("app_id", "250528")
	query.Set("web", "1")
	query.Set("clienttype", "0")
	query.Set("channel", "chunlei")
	// A missing logid can produce a seemingly valid dlink that later fails
	// with 31362. Generate it for each request; it is not a user credential.
	requestID := make([]byte, 16)
	if _, err := rand.Read(requestID); err != nil {
		return errors.New("failed to generate Baidu request ID")
	}
	query.Set("logid", base64.StdEncoding.EncodeToString(requestID))
	method := http.MethodGet
	if form != nil {
		method = http.MethodPost
	}
	res, err := d.webRequest(ctx, method, webBaseURL+path+"?"+query.Encode(), form)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return errWebCookieExpired
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Baidu web API %s returned HTTP %d", path, res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024))
	if err != nil {
		return errors.New("failed to read Baidu web API response")
	}
	var envelope struct {
		Errno *int `json:"errno"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Errno == nil {
		return errors.New("Baidu web API did not return a valid JSON response with errno")
	}
	switch *envelope.Errno {
	case 0:
	case -6, 111, 20016, 20017:
		return errWebCookieExpired
	default:
		return fmt.Errorf("Baidu web API %s failed (errno=%d)", path, *envelope.Errno)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return errors.New("invalid Baidu web API response data")
	}
	return nil
}

func (d *BaiduNetdisk) getWebFiles(ctx context.Context, dir string) ([]File, error) {
	query := url.Values{"dir": {dir}, "num": {strconv.Itoa(webPageSize)}, "order": {d.OrderBy}}
	if d.OrderBy == "" {
		query.Set("order", "name")
	}
	if d.OrderDirection == "desc" {
		query.Set("desc", "1")
	}
	files := make([]File, 0)
	seen := make(map[int64]bool)
	for page := 1; ; page++ {
		query.Set("page", strconv.Itoa(page))
		var result ListResp
		if err := d.webAPI(ctx, "/api/list", query, nil, &result); err != nil {
			return nil, err
		}
		for _, file := range result.List {
			// Do not silently return an incomplete directory when pagination stalls.
			if seen[file.FsId] {
				return nil, errors.New("Baidu directory changed or pagination repeated; retry the scan")
			}
			seen[file.FsId] = true
			if !d.OnlyListVideoFile || file.Isdir == 1 || file.Category == 1 {
				files = append(files, file)
			}
		}
		if len(result.List) < webPageSize {
			return files, nil
		}
	}
}

func (d *BaiduNetdisk) linkWeb(ctx context.Context, file model.Obj) (*model.Link, error) {
	target, err := json.Marshal([]string{file.GetPath()})
	if err != nil {
		return nil, err
	}
	var result struct {
		Info []struct {
			File
			Errno int    `json:"errno"`
			Dlink string `json:"dlink"`
		} `json:"info"`
	}
	err = d.webAPI(ctx, "/api/filemetas", nil, url.Values{"target": {string(target)}, "dlink": {"1"}}, &result)
	if err != nil {
		return nil, err
	}
	if len(result.Info) != 1 || result.Info[0].Errno != 0 || result.Info[0].Isdir != 0 || result.Info[0].Path != file.GetPath() {
		return nil, errors.New("Baidu did not return download metadata for the requested file")
	}
	u, err := url.Parse(result.Info[0].Dlink)
	if err != nil || !validWebDownloadURL(u, "pcs.baidu.com") {
		return nil, errors.New("Baidu returned an invalid download resolver URL")
	}
	res, err := d.webRequest(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound && res.StatusCode != http.StatusTemporaryRedirect && res.StatusCode != http.StatusSeeOther {
		return nil, fmt.Errorf("Baidu download resolver returned HTTP %d instead of a CDN redirect; check the Cookie and account download permissions", res.StatusCode)
	}
	cdn, err := url.Parse(res.Header.Get("Location"))
	if err != nil || !validWebDownloadURL(cdn, "baidupcs.com") {
		return nil, errors.New("Baidu returned an invalid CDN redirect")
	}
	expiration := 5 * time.Minute
	if expires, err := strconv.ParseInt(cdn.Query().Get("expires"), 10, 64); err == nil && expires > 1_000_000_000 {
		remaining := time.Until(time.Unix(expires, 0).Add(-30 * time.Second))
		if remaining <= 0 {
			return nil, errors.New("Baidu returned an expired CDN redirect; retry playback")
		}
		if remaining < expiration {
			expiration = remaining
		}
	}
	// The player receives only the signed CDN URL, never the web login headers.
	return &model.Link{URL: cdn.String(), Expiration: &expiration}, nil
}

func validWebDownloadURL(u *url.URL, domain string) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == domain || strings.HasSuffix(host, "."+domain)
}
