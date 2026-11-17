package product

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalPath 在写入前固定真实父目录；macOS的/tmp等系统别名可以使用，末级符号链接不接管。作者：long
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	probe := abs
	var missing []string
	for {
		info, err := os.Lstat(probe)
		if err == nil {
			if probe == abs && info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("configured path must not be a symlink")
			}
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", err
		}
		missing = append(missing, filepath.Base(probe))
		probe = parent
	}
	real, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		real = filepath.Join(real, missing[i])
	}
	return real, nil
}

// PrivateDirectory 仅收紧明确声明的私有目录，不更改外置配置的公共父目录。作者：long
func PrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private path is not a real directory")
	}
	return os.Chmod(path, 0o700)
}

// PrivateFile 拒绝符号链接并独占创建，防止凭据写到声明路径之外。作者：long
func PrivateFile(path string) error {
	i, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if e != nil {
			return e
		}
		return f.Close()
	}
	if err != nil {
		return err
	}
	if !i.Mode().IsRegular() {
		return fmt.Errorf("private path is not a regular file")
	}
	return os.Chmod(path, 0o600)
}
