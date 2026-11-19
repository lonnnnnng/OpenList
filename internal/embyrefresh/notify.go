package embyrefresh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	log "github.com/sirupsen/logrus"
)

const (
	queueSize       = 8192
	maxUpdatePaths  = 100
	batchDelay      = time.Second
	requestTimeout  = 15 * time.Second
	maxRequestTries = 3
)

var (
	changedPaths = make(chan string, queueSize)
	workerOnce   sync.Once
	client       = &http.Client{
		Timeout:   requestTimeout,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
)

type mediaUpdate struct {
	Path       string `json:"Path"`
	UpdateType string `json:"UpdateType"`
}

type mediaUpdateRequest struct {
	Updates []mediaUpdate `json:"Updates"`
}

// NotifyStrmCreated queues the local STRM path for an incremental Emby refresh.
func NotifyStrmCreated(path string) {
	path = filepath.Clean(path)
	if path == "." || !strings.EqualFold(filepath.Ext(path), ".strm") {
		return
	}
	if _, _, ok := config(); !ok {
		return
	}
	workerOnce.Do(func() { go runQueue() })
	select {
	case changedPaths <- path:
	default:
		log.Warnf("[media] Emby refresh queue is full; skipped STRM path %s", path)
	}
}

func config() (string, string, bool) {
	baseURL := strings.TrimSpace(setting.GetStr(conf.MediaEmbyRefreshURL))
	apiKey := strings.TrimSpace(setting.GetStr(conf.MediaEmbyRefreshAPIKey))
	return baseURL, apiKey, baseURL != "" && apiKey != ""
}

func runQueue() {
	for firstPath := range changedPaths {
		paths := map[string]struct{}{firstPath: {}}
		timer := time.NewTimer(batchDelay)
	collect:
		for len(paths) < maxUpdatePaths {
			select {
			case path := <-changedPaths:
				paths[path] = struct{}{}
			case <-timer.C:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}

		ordered := make([]string, 0, len(paths))
		for path := range paths {
			ordered = append(ordered, path)
		}
		sort.Strings(ordered)
		if err := sendUpdates(context.Background(), ordered); err != nil {
			log.Errorf("[media] notify Emby about %d STRM files failed: %v", len(ordered), err)
		}
	}
}

func sendUpdates(ctx context.Context, paths []string) error {
	baseURL, apiKey, ok := config()
	if !ok || len(paths) == 0 {
		return nil
	}
	endpoint, err := updatesEndpoint(baseURL)
	if err != nil {
		return err
	}
	for start := 0; start < len(paths); start += maxUpdatePaths {
		end := min(start+maxUpdatePaths, len(paths))
		updates := make([]mediaUpdate, 0, end-start)
		for _, path := range paths[start:end] {
			updates = append(updates, mediaUpdate{Path: path, UpdateType: "Created"})
		}
		if err := postBatch(ctx, endpoint, apiKey, updates); err != nil {
			return err
		}
	}
	return nil
}

func updatesEndpoint(baseURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid Emby API base URL")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/Library/Media/Updated"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func postBatch(ctx context.Context, endpoint, apiKey string, updates []mediaUpdate) error {
	body, err := json.Marshal(mediaUpdateRequest{Updates: updates})
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < maxRequestTries; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Emby-Token", apiKey)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
				cancel()
				return nil
			}
			err = fmt.Errorf("Emby returned HTTP %d", resp.StatusCode)
			if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
				cancel()
				return err
			}
		}
		cancel()
		lastErr = err
		if attempt+1 < maxRequestTries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
			}
		}
	}
	return lastErr
}
