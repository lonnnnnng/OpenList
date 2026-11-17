package strm

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

var initialized atomic.Bool
var keyMu sync.RWMutex
var playbackKey []byte

// Initialize 创建稳定播放密钥和独立输出区；未知文件不清理，损坏密钥必须显式恢复。作者：long
func Initialize(dataDir, outputRoot string) error {
	initialized.Store(false)
	keyPath := filepath.Join(dataDir, "strm-playback.key")
	i, err := os.Lstat(keyPath)
	if os.IsNotExist(err) {
		key := make([]byte, 32)
		if _, err = io.ReadFull(rand.Reader, key); err != nil {
			return err
		}
		f, e := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if e != nil {
			return e
		}
		_, err = f.Write(key)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else {
		if err != nil {
			return err
		}
		if !i.Mode().IsRegular() || i.Size() != 32 {
			return fmt.Errorf("invalid playback key; restore the private data backup")
		}
		if err = os.Chmod(keyPath, 0o600); err != nil {
			return err
		}
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	for _, path := range []string{outputRoot, filepath.Join(outputRoot, "library"), filepath.Join(outputRoot, "trash")} {
		if err = os.MkdirAll(path, 0o750); err != nil {
			return err
		}
		i, err = os.Lstat(path)
		if err != nil {
			return err
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path must be a real directory")
		}
		if err = os.Chmod(path, 0o750); err != nil {
			return err
		}
	}
	keyMu.Lock()
	playbackKey = append(playbackKey[:0], key...)
	keyMu.Unlock()
	initialized.Store(true)
	return nil
}

func Initialized() bool { return initialized.Load() }
func Stop() {
	initialized.Store(false)
	keyMu.Lock()
	for i := range playbackKey {
		playbackKey[i] = 0
	}
	playbackKey = nil
	keyMu.Unlock()
}

// SignPlayback 以独立播放密钥绑定文件ID和版本，管理Token轮换不影响STRM地址。作者：long
func SignPlayback(fileID string, version uint64) (string, error) {
	keyMu.RLock()
	defer keyMu.RUnlock()
	if !initialized.Load() || len(playbackKey) != 32 {
		return "", fmt.Errorf("playback service is not ready")
	}
	mac := hmac.New(sha256.New, playbackKey)
	_, _ = fmt.Fprintf(mac, "strm-read\n%s\n%d", fileID, version)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyPlayback(fileID string, version uint64, signature string) bool {
	expected, err := SignPlayback(fileID, version)
	if err != nil {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(expected)
	return err == nil && hmac.Equal(provided, want)
}
