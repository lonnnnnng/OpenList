package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestStrmOptionsForScanPath(t *testing.T) {
	global := StrmOptions{OutputPath: "/quark/strm", SameDir: true}

	got := StrmOptionsForScanPath(global, &model.MediaScanPath{StrmPath: " /alipan/strm "})
	if got.OutputPath != "/alipan/strm" || got.SameDir {
		t.Fatalf("scan path override = %#v, want /alipan/strm with same-dir disabled", got)
	}

	got = StrmOptionsForScanPath(global, &model.MediaScanPath{})
	if got != global {
		t.Fatalf("empty scan path override = %#v, want global options %#v", got, global)
	}
}

func TestAutoStrmOptionsOnlyCreateMissingFiles(t *testing.T) {
	input := StrmOptions{APIRoot: "http://localhost:5244", OutputPath: "/quark/strm", Overwrite: true}
	got := autoStrmOptions(input)
	if got.Overwrite || got.APIRoot != input.APIRoot || got.OutputPath != input.OutputPath {
		t.Fatalf("autoStrmOptions() = %#v", got)
	}
	if !input.Overwrite {
		t.Fatal("autoStrmOptions() mutated its input")
	}
}

func TestGenerateStrmForItemsReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	written, err := GenerateStrmForItems(ctx, []model.MediaItem{{
		MediaType: model.MediaTypeVideo,
		FileName:  "movie.mp4",
	}}, StrmOptions{OutputPath: "/strm"})
	if written != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateStrmForItems() = (%d, %v), want (0, context.Canceled)", written, err)
	}
}

func TestStableStrmSuffix(t *testing.T) {
	path := "/cloud/show/Season 1/E01.mp4"
	first, second := stableStrmSuffix(path), stableStrmSuffix(path)
	hash := sha256.Sum256([]byte(path))
	want := hex.EncodeToString(hash[:4])
	if first != second || first != want || len(first) != 8 {
		t.Fatalf("stableStrmSuffix() = %q, %q; want stable %q", first, second, want)
	}
	if first == stableStrmSuffix("/cloud/other/Season 1/E01.mp4") {
		t.Fatal("different source paths must have distinct suffixes")
	}
}

func TestStrmTargetsUseEpisodeSeasons(t *testing.T) {
	item := &model.MediaItem{
		MediaType:   model.MediaTypeVideo,
		VideoType:   "tv",
		IsFolder:    true,
		FileName:    "Show source",
		FolderPath:  "/videos",
		ScrapedName: "Show",
	}
	episodes, err := json.Marshal([]model.EpisodeInfo{
		{FileName: "Season 1/E01.mp4", Index: 1, Season: 1},
		{FileName: "Season 2/E01.mp4", Index: 1, Season: 2},
		{FileName: "Specials/Bonus01.mp4", Index: 1, Season: 0, Title: "Specials / 彩蛋"},
	})
	if err != nil {
		t.Fatal(err)
	}
	item.Episodes = string(episodes)
	targets := StrmTargets(item)
	if len(targets) != 3 {
		t.Fatalf("got %d targets, want 3", len(targets))
	}
	want := []string{
		path.Join("Show", "Season 01", "Show S01E01 - Unknown.strm"),
		path.Join("Show", "Season 02", "Show S02E01 - Unknown.strm"),
		path.Join("Show", "Season 00", "Show S00E01 - Specials _ 彩蛋.strm"),
	}
	for i := range want {
		if targets[i].RelPath != want[i] {
			t.Errorf("target %d = %q, want %q", i, targets[i].RelPath, want[i])
		}
	}
}

func TestMovieFolderStrmTargetsUseEpisodeFiles(t *testing.T) {
	item := &model.MediaItem{
		MediaType:  model.MediaTypeVideo,
		IsFolder:   true,
		FolderPath: "/videos",
		FileName:   "Movies",
		Episodes:   `[{"file_name":"Film A.mkv","index":1},{"file_name":"Film B.mkv","index":2}]`,
	}
	targets := StrmTargets(item)
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want one per movie file", len(targets))
	}
	if targets[0].RawPath != "/videos/Movies/Film A.mkv" || targets[0].RelPath != "Film A/Film A.strm" {
		t.Errorf("first target = %+v", targets[0])
	}
	if targets[1].RawPath != "/videos/Movies/Film B.mkv" || targets[1].RelPath != "Film B/Film B.strm" {
		t.Errorf("second target = %+v", targets[1])
	}
}

func TestFilterShadowedMediaItems(t *testing.T) {
	items := []model.MediaItem{
		{MediaType: model.MediaTypeVideo, VideoType: "tv", IsFolder: true, FolderPath: "/videos", FileName: "Show", Episodes: `[{"file_name":"Season 1/E01.mp4","index":1}]`},
		{MediaType: model.MediaTypeVideo, FolderPath: "/videos/Show/Season 1", FileName: "E01.mp4"},
		{MediaType: model.MediaTypeVideo, IsFolder: true, FolderPath: "/videos/Show", FileName: "Season 1"},
		{MediaType: model.MediaTypeVideo, FolderPath: "/videos/Other", FileName: "Movie.mkv"},
		{MediaType: model.MediaTypeVideo, IsFolder: true, FolderPath: "/videos/Show", FileName: "Movies"},
	}
	path := &model.MediaScanPath{MediaType: model.MediaTypeVideo, PathMerge: true}
	got := filterShadowedMediaItems(items, path)
	if len(got) != 3 || got[0].FileName != "Show" || got[1].FileName != "Movie.mkv" || got[2].FileName != "Movies" {
		t.Fatalf("filterShadowedMediaItems() = %#v", got)
	}
	if got := filterShadowedMediaItems(items, &model.MediaScanPath{MediaType: model.MediaTypeVideo}); len(got) != len(items) {
		t.Fatalf("non-merge scan path unexpectedly filtered items: %d", len(got))
	}
}

func TestFilterPathMergeMediaItems(t *testing.T) {
	path := &model.MediaScanPath{MediaType: model.MediaTypeVideo, Path: "/videos", PathMerge: true}
	items := []model.MediaItem{
		{MediaType: model.MediaTypeVideo, IsFolder: true, FolderPath: "/videos", FileName: "Show"},
		{MediaType: model.MediaTypeVideo, FolderPath: "/videos", FileName: "Movie.mkv"},
		{MediaType: model.MediaTypeVideo, FolderPath: "/videos/Show", FileName: "E01.mkv"},
	}
	got := filterPathMergeMediaItems(items, path, map[string]struct{}{
		"/videos/Show":      {},
		"/videos/Movie.mkv": {},
	})
	if len(got) != 2 || got[0].FileName != "Show" || got[1].FileName != "Movie.mkv" {
		t.Fatalf("filterPathMergeMediaItems() = %#v", got)
	}
}
