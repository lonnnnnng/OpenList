package embyrefresh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdatesEndpointAppendsToEmbyBasePath(t *testing.T) {
	got, err := updatesEndpoint(" http://127.0.0.1:8096/emby/ ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:8096/emby/Library/Media/Updated" {
		t.Fatalf("updatesEndpoint() = %q", got)
	}
}

func TestUpdatesEndpointRejectsInvalidURL(t *testing.T) {
	for _, input := range []string{"", "ftp://emby.local", "http:///emby"} {
		if _, err := updatesEndpoint(input); err == nil {
			t.Errorf("updatesEndpoint(%q) accepted an invalid URL", input)
		}
	}
}

func TestPostBatchSendsCreatedPathsWithEmbyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emby/Library/Media/Updated" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Emby-Token"); got != "test-key" {
			t.Errorf("X-Emby-Token = %q", got)
		}
		var body mediaUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(body.Updates) != 1 || body.Updates[0].Path != "/media/test.strm" || body.Updates[0].UpdateType != "Created" {
			t.Errorf("Updates = %#v", body.Updates)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	err := postBatch(context.Background(), server.URL+"/emby/Library/Media/Updated", "test-key", []mediaUpdate{{Path: "/media/test.strm", UpdateType: "Created"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostBatchDoesNotIncludeAPIKeyInError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad token"))
	}))
	defer server.Close()

	err := postBatch(context.Background(), server.URL, "secret-value", []mediaUpdate{{Path: "/media/test.strm", UpdateType: "Created"}})
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error should omit credentials, got %v", err)
	}
}

func TestPostBatchDoesNotForwardAPIKeyAcrossRedirect(t *testing.T) {
	var redirected bool
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		if got := r.Header.Get("X-Emby-Token"); got != "" {
			t.Errorf("redirected request forwarded API key %q", got)
		}
	}))
	defer second.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", second.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer first.Close()

	err := postBatch(context.Background(), first.URL, "secret-value", []mediaUpdate{{Path: "/media/test.strm", UpdateType: "Created"}})
	if err == nil {
		t.Fatal("postBatch() succeeded after a redirect")
	}
	if redirected {
		t.Fatal("postBatch() followed a redirect")
	}
}
