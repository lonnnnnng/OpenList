package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBundleDeterministicAndDoesNotClearSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dist")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "index.html"), []byte("<main>STRM</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	one, two := filepath.Join(root, "one.zip"), filepath.Join(root, "two.zip")
	stamp := time.Unix(1794830400, 0).UTC()
	if err := pack(source, one, stamp); err != nil {
		t.Fatal(err)
	}
	if err := pack(source, two, stamp); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(one)
	b, _ := os.ReadFile(two)
	if !bytes.Equal(a, b) {
		t.Fatal("same frontend produces different bundles")
	}
	if _, err := os.Stat(filepath.Join(source, "index.html")); err != nil {
		t.Fatal("pack cleared source")
	}
}
