// Package cloudhls proxies provider playlists without exposing login headers or
// signed upstream URLs to clients. Each opaque session is scoped to one video.
package cloudhls

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/net"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

const (
	sessionTTL   = 12 * time.Hour
	maxSessions  = 128
	maxManifest  = 4 * 1024 * 1024
	maxResources = 100000
)

type resource struct {
	url      string
	parent   string
	playlist bool
}

type session struct {
	mu        sync.Mutex
	source    *model.HLSSource
	resources map[string]resource
	expires   time.Time
}

type Proxy struct {
	mu       sync.Mutex
	sessions map[string]*session
	keys     map[[32]byte]string
	client   *http.Client
}

func New(client *http.Client) *Proxy {
	if client == nil {
		client = net.NewHttpClient()
	}
	copy := *client
	copy.Timeout = 45 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Proxy{sessions: make(map[string]*session), keys: make(map[[32]byte]string), client: &copy}
}

var defaultOnce sync.Once
var defaultProxy *Proxy

func Default() *Proxy {
	defaultOnce.Do(func() { defaultProxy = New(nil) })
	return defaultProxy
}

func (p *Proxy) Open(source *model.HLSSource, prefix string) (string, error) {
	if source == nil || !validSourceURL(source.URL) {
		return "", errors.New("invalid cloud HLS source")
	}
	key := sha256.Sum256([]byte(source.URL + "\x00" + source.Header.Get("Cookie")))
	p.mu.Lock()
	defer p.mu.Unlock()
	for token, s := range p.sessions {
		if time.Now().After(s.expires) {
			delete(p.sessions, token)
		}
	}
	for k, token := range p.keys {
		if p.sessions[token] == nil {
			delete(p.keys, k)
		}
	}
	if token := p.keys[key]; token != "" {
		return strings.TrimRight(prefix, "/") + "/" + token + "/index.m3u8", nil
	}
	if len(p.sessions) >= maxSessions {
		return "", errors.New("too many cloud HLS sessions; try again later")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("cannot create cloud HLS session")
	}
	token := hex.EncodeToString(b)
	p.sessions[token] = &session{source: source, resources: map[string]resource{"index.m3u8": {url: source.URL, playlist: true}}, expires: time.Now().Add(sessionTTL)}
	p.keys[key] = token
	return strings.TrimRight(prefix, "/") + "/" + token + "/index.m3u8", nil
}

func validSourceURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && (u.Port() == "" || u.Port() == "443") && u.Fragment == ""
}

func resolveResource(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", errors.New("invalid playlist URL")
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", errors.New("invalid playlist resource")
	}
	u := b.ResolveReference(r)
	// UC uses one CDN host for the playlist and its resources. Do not let a
	// playlist send its account Cookie to another origin or to an arbitrary URL.
	if !validSourceURL(u.String()) || !strings.EqualFold(u.Host, b.Host) {
		return "", errors.New("cloud HLS resource uses an untrusted origin")
	}
	return u.String(), nil
}

var uriAttribute = regexp.MustCompile(`\bURI="([^"]*)"`)

// rewrite uses stable parent/position IDs. Refreshing signed upstream URLs does
// not invalidate segment URLs already buffered in a player's playlist.
func rewrite(raw []byte, base, parent, prefix string) ([]byte, map[string]resource, error) {
	if !strings.HasPrefix(string(raw), "#EXTM3U") {
		return nil, nil, errors.New("cloud HLS endpoint did not return a playlist")
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	resources := make(map[string]resource)
	index := 0
	add := func(ref string, playlist bool) (string, error) {
		u, err := resolveResource(base, ref)
		if err != nil {
			return "", err
		}
		parsed, _ := url.Parse(u)
		ext := strings.ToLower(path.Ext(parsed.Path))
		playlist = playlist || ext == ".m3u8" || ext == ".m3u"
		if playlist {
			ext = ".m3u8"
		} else if !utils.SliceContains([]string{".ts", ".mp4", ".m4s", ".aac", ".vtt", ".key"}, ext) {
			ext = ".bin"
		}
		id := sha256.Sum256([]byte(parent + "/" + strconv.Itoa(index)))
		index++
		name := hex.EncodeToString(id[:16]) + ext
		resources[name] = resource{url: u, parent: parent, playlist: playlist}
		return strings.TrimRight(prefix, "/") + "/" + name, nil
	}
	nextPlaylist := false
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			u, err := add(line, nextPlaylist)
			if err != nil {
				return nil, nil, err
			}
			lines[i] = u
			nextPlaylist = false
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			nextPlaylist = true
		}
		matches := uriAttribute.FindAllStringSubmatchIndex(line, -1)
		if strings.Contains(line, "URI=") && len(matches) == 0 {
			return nil, nil, errors.New("unsupported HLS URI attribute")
		}
		var out strings.Builder
		last := 0
		for _, m := range matches {
			ref := line[m[2]:m[3]]
			playlist := strings.HasPrefix(line, "#EXT-X-MEDIA:") || strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:")
			u, err := add(ref, playlist)
			if err != nil {
				return nil, nil, err
			}
			out.WriteString(line[last:m[2]])
			out.WriteString(u)
			last = m[3]
		}
		if len(matches) > 0 {
			out.WriteString(line[last:])
			lines[i] = out.String()
		}
	}
	return []byte(strings.Join(lines, "\n")), resources, nil
}

func (p *Proxy) request(ctx context.Context, s *session, target, method, byteRange string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, errors.New("invalid HLS request")
	}
	for _, name := range []string{"Cookie", "Referer", "User-Agent"} {
		if v := s.source.Header.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cloud HLS upstream request failed")
	}
	return res, nil
}

func expired(status int) bool { return status == 401 || status == 403 || status == 412 }

func (p *Proxy) playlist(ctx context.Context, s *session, id, prefix string) ([]byte, int, error) {
	r, ok := s.resources[id]
	if !ok || !r.playlist {
		return nil, 404, errors.New("unknown cloud HLS playlist")
	}
	res, err := p.request(ctx, s, r.url, http.MethodGet, "")
	if err != nil {
		return nil, 502, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, res.StatusCode, fmt.Errorf("cloud HLS playlist returned HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxManifest+1))
	if err != nil {
		return nil, 502, errors.New("failed to read cloud HLS playlist")
	}
	if len(raw) > maxManifest {
		return nil, 502, errors.New("cloud HLS playlist is too large")
	}
	body, children, err := rewrite(raw, r.url, id, prefix)
	if err != nil {
		return nil, 502, err
	}
	for key, child := range s.resources {
		if child.parent == id {
			delete(s.resources, key)
		}
	}
	for key := range children {
		if _, ok := s.resources[key]; !ok && len(s.resources) >= maxResources {
			return nil, 502, errors.New("too many cloud HLS resources")
		}
		s.resources[key] = children[key]
	}
	return body, 200, nil
}

func (p *Proxy) refresh(ctx context.Context, s *session, id, prefix string) error {
	if s.source.Refresh == nil {
		return errors.New("cloud HLS session expired; reopen the video")
	}
	chain := []string{}
	current := id
	for depth := 0; current != "index.m3u8"; depth++ {
		if depth >= 8 {
			return errors.New("cloud HLS playlist nesting is too deep")
		}
		r, ok := s.resources[current]
		if !ok {
			return errors.New("unknown cloud HLS resource")
		}
		if r.parent == "" {
			return errors.New("invalid cloud HLS resource parent")
		}
		chain = append(chain, r.parent)
		current = r.parent
	}
	source, err := s.source.Refresh(ctx)
	if err != nil {
		return err
	}
	if source == nil || !validSourceURL(source.URL) {
		return errors.New("invalid refreshed cloud HLS source")
	}
	s.source = source
	s.resources["index.m3u8"] = resource{url: source.URL, playlist: true}
	for i := len(chain) - 1; i >= 0; i-- {
		if _, _, err := p.playlist(ctx, s, chain[i], prefix); err != nil {
			return err
		}
	}
	return nil
}

func (p *Proxy) Serve(w http.ResponseWriter, r *http.Request, token, id, prefix string) {
	w.Header().Set("Cache-Control", "private, no-store")
	p.mu.Lock()
	s := p.sessions[token]
	p.mu.Unlock()
	if s == nil || time.Now().After(s.expires) {
		http.Error(w, "cloud HLS session expired; reopen the video", http.StatusGone)
		return
	}
	s.mu.Lock()
	resInfo, ok := s.resources[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "unknown cloud HLS resource", 404)
		return
	}
	if resInfo.playlist {
		body, status, err := p.playlist(r.Context(), s, id, prefix)
		if err != nil && expired(status) {
			if refreshErr := p.refresh(r.Context(), s, id, prefix); refreshErr != nil {
				err = refreshErr
			} else {
				body, _, err = p.playlist(r.Context(), s, id, prefix)
			}
		}
		s.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(200)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return
	}
	res, err := p.request(r.Context(), s, resInfo.url, r.Method, r.Header.Get("Range"))
	if err == nil && expired(res.StatusCode) {
		res.Body.Close()
		err = p.refresh(r.Context(), s, id, prefix)
		if err == nil {
			if fresh, ok := s.resources[id]; ok {
				res, err = p.request(r.Context(), s, fresh.url, r.Method, r.Header.Get("Range"))
			} else {
				err = errors.New("cloud HLS segment changed; reopen the video")
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 206 && res.StatusCode != 416 {
		http.Error(w, fmt.Sprintf("cloud HLS resource returned HTTP %d", res.StatusCode), 502)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := res.Header.Get(name); v != "" {
			w.Header().Set(name, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = utils.CopyWithBuffer(w, &stream.RateLimitReader{Reader: res.Body, Limiter: stream.ServerDownloadLimit, Ctx: r.Context()})
	}
}
