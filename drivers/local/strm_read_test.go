package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSTRMDirectoryItemPreservesSymlinkIdentity(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.mkv")
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.mkv")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	item := (&Local{}).FileInfoToObj(context.Background(), info, "/", root)
	marker, ok := item.(interface{ IsSourceSymlink() bool })
	if !ok || !marker.IsSourceSymlink() {
		t.Fatal("source symlink lost identity before task boundary validation")
	}
}
