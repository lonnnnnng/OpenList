package media

import (
	"encoding/json"
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

func TestFilterShadowedMediaItems(t *testing.T) {
	items := []model.MediaItem{
		{MediaType: model.MediaTypeVideo, VideoType: "tv", IsFolder: true, FolderPath: "/videos", FileName: "Show", Episodes: `[{"file_name":"E01.mp4","index":1}]`},
		{MediaType: model.MediaTypeVideo, FolderPath: "/videos/Show", FileName: "E01.mp4"},
		{MediaType: model.MediaTypeVideo, IsFolder: true, FolderPath: "/videos/Show", FileName: "Season 1"},
		{MediaType: model.MediaTypeVideo, FolderPath: "/videos/Other", FileName: "Movie.mkv"},
	}
	path := &model.MediaScanPath{MediaType: model.MediaTypeVideo, PathMerge: true}
	got := filterShadowedMediaItems(items, path)
	if len(got) != 2 || got[0].FileName != "Show" || got[1].FileName != "Movie.mkv" {
		t.Fatalf("filterShadowedMediaItems() = %#v", got)
	}
	if got := filterShadowedMediaItems(items, &model.MediaScanPath{MediaType: model.MediaTypeVideo}); len(got) != len(items) {
		t.Fatalf("non-merge scan path unexpectedly filtered items: %d", len(got))
	}
}
