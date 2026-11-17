package strm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
)

func withOutputRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	conf.STRMOutputMu.Lock()
	previous := conf.STRMOutputPath
	conf.STRMOutputPath = root
	conf.STRMOutputMu.Unlock()
	t.Cleanup(func() {
		conf.STRMOutputMu.Lock()
		conf.STRMOutputPath = previous
		conf.STRMOutputMu.Unlock()
	})
	if err := Initialize(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Stop)
	return root
}

func TestWriteNewSTRMIsAtomicContainedAndNeverOverwrites(t *testing.T) {
	root := withOutputRoot(t)
	content := []byte("http://127.0.0.1:5344/s/file?sign=stable\n")
	hash, err := WriteNewSTRM(context.Background(), "movies/Film.strm", content)
	if err != nil || hash != hashBytes(content) {
		t.Fatalf("write failed: %s %v", hash, err)
	}
	file := filepath.Join(root, "library/movies/Film.strm")
	actual, err := os.ReadFile(file)
	if err != nil || string(actual) != string(content) {
		t.Fatalf("wrong output: %q %v", actual, err)
	}
	exists, matches, err := STRMHashState("movies/Film.strm", hashBytes(content))
	if err != nil || !exists || !matches {
		t.Fatalf("manifest hash was not verified: exists=%v matches=%v err=%v", exists, matches, err)
	}
	if _, matches, err = STRMHashState("movies/Film.strm", hashBytes([]byte("other"))); err != nil || matches {
		t.Fatalf("modified-content check was not rejected: matches=%v err=%v", matches, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("wrong artifact permission: %v %v", info, err)
	}
	if _, err = WriteNewSTRM(context.Background(), "movies/Film.strm", []byte("replace")); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("existing file overwritten: %v", err)
	}
	actual, _ = os.ReadFile(file)
	if string(actual) != string(content) {
		t.Fatal("conflict changed existing content")
	}
	for _, relative := range []string{"../escape.strm", "/absolute.strm", "movies/../escape.strm", "movies\\escape.strm"} {
		if _, err = WriteNewSTRM(context.Background(), relative, content); !errors.Is(err, ErrArtifactConflict) {
			t.Fatalf("unsafe target %q accepted: %v", relative, err)
		}
	}
}

func TestWriteNewSTRMRejectsExistingSymlinkDirectory(t *testing.T) {
	root := withOutputRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "library", "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteNewSTRM(context.Background(), "escape/file.strm", []byte("no")); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("symlink directory accepted: %v", err)
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("write escaped library root")
	}
	if _, _, err := STRMHashState("escape/file.strm", "ignored"); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("symlink parent accepted during manifest check: %v", err)
	}
}

func TestTrashMoveAndRestoreAreContainedNoOverwriteAndRetryable(t *testing.T) {
	root := withOutputRoot(t)
	content := []byte("signed playback entry\n")
	if _, err := WriteNewSTRM(context.Background(), "movies/Film.strm", content); err != nil {
		t.Fatal(err)
	}
	expected := hashBytes(content)
	trash := "task-id/action-id/Film.strm"
	if err := MoveSTRMToTrash(context.Background(), "movies/Film.strm", trash, expected); err != nil {
		t.Fatal(err)
	}
	if err := MoveSTRMToTrash(context.Background(), "movies/Film.strm", trash, expected); err != nil {
		t.Fatalf("retry after completed move was not idempotent: %v", err)
	}
	if exists, _, err := STRMHashState("movies/Film.strm", expected); err != nil || exists {
		t.Fatalf("recycled file remained in library: exists=%v err=%v", exists, err)
	}
	trashed, err := os.ReadFile(filepath.Join(root, "trash", filepath.FromSlash(trash)))
	if err != nil || string(trashed) != string(content) {
		t.Fatalf("trash content mismatch: %q %v", trashed, err)
	}
	if _, err = WriteNewSTRM(context.Background(), "movies/Film.strm", []byte("manual file")); err != nil {
		t.Fatal(err)
	}
	if err = RestoreSTRMFromTrash(context.Background(), trash, "movies/Film.strm", expected); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("restore replaced existing library file: %v", err)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "library", "movies", "Film.strm"))
	if string(actual) != "manual file" {
		t.Fatal("restore changed an unrelated target")
	}
	if err = os.Remove(filepath.Join(root, "library", "movies", "Film.strm")); err != nil {
		t.Fatal(err)
	}
	if err = RestoreSTRMFromTrash(context.Background(), trash, "movies/Film.strm", expected); err != nil {
		t.Fatal(err)
	}
	if err = RestoreSTRMFromTrash(context.Background(), trash, "movies/Film.strm", expected); err != nil {
		t.Fatalf("retry after completed restore was not idempotent: %v", err)
	}
	actual, err = os.ReadFile(filepath.Join(root, "library", "movies", "Film.strm"))
	if err != nil || string(actual) != string(content) {
		t.Fatalf("restored content mismatch: %q %v", actual, err)
	}
	if _, err = os.Stat(filepath.Join(root, "trash", filepath.FromSlash(trash))); !os.IsNotExist(err) {
		t.Fatalf("restore left trash source behind: %v", err)
	}
}

func TestTrashMoveRejectsSymlinkAndModifiedSource(t *testing.T) {
	root := withOutputRoot(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "file.strm"), []byte("outside"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "library", "linked")); err != nil {
		t.Fatal(err)
	}
	if err := MoveSTRMToTrash(context.Background(), "linked/file.strm", "task/action/file.strm", hashBytes([]byte("outside"))); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("symlink source was accepted: %v", err)
	}
	if _, err := WriteNewSTRM(context.Background(), "changed.strm", []byte("new content")); err != nil {
		t.Fatal(err)
	}
	if err := MoveSTRMToTrash(context.Background(), "changed.strm", "task/action/changed.strm", hashBytes([]byte("old content"))); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("modified source was accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "file.strm")); err != nil {
		t.Fatalf("outside file was changed: %v", err)
	}
}

func TestTrashMoveResumesAfterLinkBeforeSourceRemoval(t *testing.T) {
	root := withOutputRoot(t)
	content := []byte("pending rename state\n")
	if _, err := WriteNewSTRM(context.Background(), "series/episode.strm", content); err != nil {
		t.Fatal(err)
	}
	trashDir := filepath.Join(root, "trash", "task", "action")
	if err := os.MkdirAll(trashDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "library", "series", "episode.strm"), filepath.Join(trashDir, "episode.strm")); err != nil {
		t.Fatal(err)
	}
	if err := MoveSTRMToTrash(context.Background(), "series/episode.strm", "task/action/episode.strm", hashBytes(content)); err != nil {
		t.Fatalf("interrupted move did not reconcile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "library", "series", "episode.strm")); !os.IsNotExist(err) {
		t.Fatalf("reconciliation did not finish source removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(trashDir, "episode.strm")); err != nil {
		t.Fatalf("reconciliation lost trash target: %v", err)
	}
}
