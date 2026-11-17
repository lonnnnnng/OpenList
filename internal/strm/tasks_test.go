package strm

import (
	"context"
	"errors"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestSourceKeyUsesRemoteIdentityWithoutExposingIt(t *testing.T) {
	first := sourceKey(SourceNode{Path: "/Movies/one.mkv", RemoteID: "remote-secret-id"})
	second := sourceKey(SourceNode{Path: "/Movies/renamed.mkv", RemoteID: "remote-secret-id"})
	if first != second || first == "remote-secret-id" {
		t.Fatal("remote ID should be stable and persisted only as a digest")
	}
	if sourceKey(SourceNode{Path: "/Movies/one.mkv"}) == sourceKey(SourceNode{Path: "/Movies/two.mkv"}) {
		t.Fatal("path fallback did not distinguish files without remote IDs")
	}
}

func TestMassDisappearanceGuardUsesMinimumAndStrictRatio(t *testing.T) {
	for _, test := range []struct {
		missing, managed int64
		want             bool
	}{
		{9, 10, false},
		{10, 50, false},
		{11, 50, true},
		{10, 40, true},
		{10, 0, false},
	} {
		if got := massDisappearanceExceeded(test.missing, test.managed); got != test.want {
			t.Fatalf("guard(%d/%d)=%v, want %v", test.missing, test.managed, got, test.want)
		}
	}
}

func TestGenerationScanRejectsPartialDirectoryTree(t *testing.T) {
	task := &model.STRMTask{ID: "task", SourcePath: "/", OutputSubdir: "movies", Extensions: []string{".mkv"}}
	root := SourceNode{Path: "/", IsDir: true}
	film := SourceNode{Path: "/film.mkv", OriginalName: "film.mkv", Size: 100, RemoteID: "remote-1"}
	broken := SourceNode{Path: "/broken", IsDir: true}
	reader := previewFixture{
		root:     root,
		children: map[string][]SourceNode{"/": {film, broken}},
		fail:     map[string]error{"/broken": errors.New("later page unavailable")},
	}
	candidates, _, complete, issues := scanGeneration(context.Background(), task, reader)
	if complete || len(candidates) != 1 || len(issues) != 1 {
		t.Fatalf("partial tree was treated as complete: complete=%v candidates=%d issues=%v", complete, len(candidates), issues)
	}
}
