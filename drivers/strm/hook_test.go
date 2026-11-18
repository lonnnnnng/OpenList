package strm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCreateLocalDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "library", "movie")
	if err := createLocalDirectory(nested); err != nil {
		t.Fatalf("createLocalDirectory() error = %v", err)
	}

	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
}

func TestCreateLocalDirectoryPreservesExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits are not supported on Windows")
	}

	root := t.TempDir()
	nested := filepath.Join(root, "library", "movie")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("create existing directory: %v", err)
	}
	if err := createLocalDirectory(nested); err != nil {
		t.Fatalf("createLocalDirectory() error = %v", err)
	}

	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("stat existing directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("existing directory permissions = %#o, want %#o", got, 0o700)
	}
}

func TestWriteLocalStrmReplacesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library", "show", "episode.strm")
	for _, content := range []string{"first", "second"} {
		if err := writeLocalStrm(path, []byte(content)); err != nil {
			t.Fatalf("writeLocalStrm() error = %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read local STRM: %v", err)
		}
		if string(got) != content {
			t.Fatalf("local STRM content = %q, want %q", got, content)
		}
	}
}
