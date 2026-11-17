package strm

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRestartPreservesKeyAndUnknownOutput(t *testing.T) {
	data, output := t.TempDir(), t.TempDir()
	t.Cleanup(Stop)
	unknown := filepath.Join(output, "unmanaged.txt")
	if err := os.WriteFile(unknown, []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(data, output); err != nil {
		t.Fatal(err)
	}
	signature, err := SignPlayback("file-id", 1)
	if err != nil || !VerifyPlayback("file-id", 1, signature) || VerifyPlayback("file-id", 2, signature) || VerifyPlayback("another-file", 1, signature) {
		t.Fatal("playback signature did not bind file identity and version")
	}
	keyPath := filepath.Join(data, "strm-playback.key")
	before, _ := os.ReadFile(keyPath)
	Stop()
	if err := Initialize(data, output); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(keyPath)
	if len(before) != 32 || !bytes.Equal(before, after) {
		t.Fatal("playback key rotated during restart")
	}
	if signatureAfterRestart, err := SignPlayback("file-id", 1); err != nil || signatureAfterRestart != signature {
		t.Fatal("playback signature changed across restart")
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatal("unknown output was removed")
	}
	for _, name := range []string{"library", "trash"} {
		if info, err := os.Stat(filepath.Join(output, name)); err != nil || !info.IsDir() {
			t.Fatalf("output %s: %v", name, err)
		}
	}
}

func TestInvalidKeyDoesNotSilentlyRegenerate(t *testing.T) {
	Stop()
	t.Cleanup(Stop)
	data := t.TempDir()
	keyPath := filepath.Join(data, "strm-playback.key")
	if err := os.WriteFile(keyPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(data, t.TempDir()); err == nil {
		t.Fatal("corrupt key accepted")
	}
	after, _ := os.ReadFile(keyPath)
	if string(after) != "corrupt" || Initialized() {
		t.Fatal("failed startup rewrote key or became ready")
	}
}
