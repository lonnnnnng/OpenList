package strm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/google/uuid"
)

var ErrArtifactConflict = errors.New("STRM目标已存在且不属于可安全管理的内容")

func libraryRootPath() (string, error) {
	conf.STRMOutputMu.RLock()
	output := conf.STRMOutputPath
	conf.STRMOutputMu.RUnlock()
	if output == "" {
		return "", fmt.Errorf("STRM输出目录尚未配置")
	}
	return filepathJoin(output, "library")
}

// filepathJoin 保持运行平台的分隔符，并拒绝输出根配置为空。作者：long
func filepathJoin(root, child string) (string, error) { return filepath.Join(root, child), nil }

func outputPathValid(relative string) bool {
	return relative != "" && !path.IsAbs(relative) && path.Clean(relative) == relative && relative != "." && relative != ".." && !strings.HasPrefix(relative, "../") && !strings.Contains(relative, "\\")
}

func hashBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func hashRootFile(root *os.Root, relative string) (string, error) {
	linkInfo, err := root.Lstat(relative)
	if err != nil {
		return "", err
	}
	if !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrArtifactConflict
	}
	file, err := root.Open(relative)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrArtifactConflict
	}
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func outputRootHandle() (*os.Root, error) {
	conf.STRMOutputMu.RLock()
	outputRoot := conf.STRMOutputPath
	conf.STRMOutputMu.RUnlock()
	if outputRoot == "" {
		return nil, fmt.Errorf("STRM输出目录尚未配置")
	}
	info, err := os.Lstat(outputRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrArtifactConflict
	}
	for _, child := range []string{"library", "trash"} {
		info, err = os.Lstat(filepath.Join(outputRoot, child))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrArtifactConflict
		}
	}
	return os.OpenRoot(outputRoot)
}

func hashIfRegular(root *os.Root, relative string) (bool, string, error) {
	_, err := root.Lstat(relative)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	hash, err := hashRootFile(root, relative)
	if err != nil {
		return true, "", ErrArtifactConflict
	}
	return true, hash, nil
}

func syncRootDirectory(root *os.Root, relative string) {
	directory, err := root.Open(relative)
	if err != nil {
		return
	}
	_ = directory.Sync()
	_ = directory.Close()
}

// MoveSTRMToTrash 在同一受限输出根内硬链接占位再移除原路径；中断重试可识别两边同摘要状态。作者：long
func MoveSTRMToTrash(ctx context.Context, outputPath, trashPath, expectedHash string) error {
	return moveManagedSTRM(ctx, "library/"+outputPath, "trash/"+trashPath, expectedHash)
}

// RestoreSTRMFromTrash 不覆盖library中的任何现有路径，重复调用可完成上次中断的恢复。作者：long
func RestoreSTRMFromTrash(ctx context.Context, trashPath, outputPath, expectedHash string) error {
	return moveManagedSTRM(ctx, "trash/"+trashPath, "library/"+outputPath, expectedHash)
}

func moveManagedSTRM(ctx context.Context, sourcePath, destinationPath, expectedHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !Initialized() || !outputPathValid(sourcePath) || !outputPathValid(destinationPath) || expectedHash == "" {
		return ErrArtifactConflict
	}
	root, err := outputRootHandle()
	if err != nil {
		return err
	}
	defer root.Close()
	for _, parent := range []string{path.Dir(sourcePath), path.Dir(destinationPath)} {
		if parent != "." {
			if err = ensureOutputDirectories(root, parent); err != nil {
				return err
			}
		}
	}
	sourceExists, sourceHash, err := hashIfRegular(root, sourcePath)
	if err != nil {
		return err
	}
	destinationExists, destinationHash, err := hashIfRegular(root, destinationPath)
	if err != nil {
		return err
	}
	if !sourceExists {
		if destinationExists && destinationHash == expectedHash {
			return nil
		}
		return ErrArtifactConflict
	}
	if sourceHash != expectedHash {
		return ErrArtifactConflict
	}
	if destinationExists {
		if destinationHash != expectedHash {
			return ErrArtifactConflict
		}
		// long: 两边同摘要表示上次在创建目标后中断；保留目标并仅移除仍匹配的源路径。
		if err = ctx.Err(); err != nil {
			return err
		}
		currentExists, currentHash, checkErr := hashIfRegular(root, sourcePath)
		if checkErr != nil || !currentExists || currentHash != expectedHash {
			return ErrArtifactConflict
		}
		if err = root.Remove(sourcePath); err != nil {
			return err
		}
		syncRootDirectory(root, path.Dir(sourcePath))
		return nil
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !Initialized() {
		return fmt.Errorf("STRM运行时已停止")
	}
	if err = root.Link(sourcePath, destinationPath); err != nil {
		if !os.IsExist(err) {
			return err
		}
		return ErrArtifactConflict
	}
	destinationExists, destinationHash, err = hashIfRegular(root, destinationPath)
	if err != nil || !destinationExists || destinationHash != expectedHash {
		return ErrArtifactConflict
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	currentExists, currentHash, err := hashIfRegular(root, sourcePath)
	if err != nil || !currentExists || currentHash != expectedHash {
		return ErrArtifactConflict
	}
	if err = root.Remove(sourcePath); err != nil {
		return err
	}
	syncRootDirectory(root, path.Dir(sourcePath))
	syncRootDirectory(root, path.Dir(destinationPath))
	return nil
}

// STRMHashState只把缺失文件当作可重建；存在但不匹配manifest的内容必须作为人工冲突保留。作者：long
func STRMHashState(relative, expectedHash string) (exists, matches bool, err error) {
	if !Initialized() || !outputPathValid(relative) {
		return false, false, ErrArtifactConflict
	}
	rootPath, err := libraryRootPath()
	if err != nil {
		return false, false, err
	}
	conf.STRMOutputMu.RLock()
	outputRoot := conf.STRMOutputPath
	conf.STRMOutputMu.RUnlock()
	for _, directory := range []string{outputRoot, rootPath} {
		info, statErr := os.Lstat(directory)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, false, ErrArtifactConflict
		}
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return false, false, err
	}
	defer root.Close()
	parent := path.Dir(relative)
	if parent != "." {
		current := ""
		for _, segment := range strings.Split(parent, "/") {
			current = path.Join(current, segment)
			info, statErr := root.Lstat(current)
			if os.IsNotExist(statErr) {
				return false, false, nil
			}
			if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false, false, ErrArtifactConflict
			}
		}
	}
	_, err = root.Lstat(relative)
	if os.IsNotExist(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	digest, err := hashRootFile(root, relative)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return true, false, ErrArtifactConflict
	}
	return true, digest == expectedHash, nil
}

func ensureOutputDirectories(root *os.Root, relative string) error {
	if !outputPathValid(relative) {
		return ErrArtifactConflict
	}
	current := ""
	for _, segment := range strings.Split(relative, "/") {
		current = path.Join(current, segment)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			if err = root.Mkdir(current, 0o750); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrArtifactConflict
		}
	}
	return nil
}

// WriteNewSTRM在受限Root内用同目录临时文件写入并原子占位；已有目标一律不覆盖，避免竞态抹掉手工文件。作者：long
func WriteNewSTRM(ctx context.Context, relative string, content []byte) (string, error) {
	return WriteSTRM(ctx, relative, content, "")
}

// WriteSTRM 仅当磁盘旧摘要仍匹配manifest时才原子更新；空expectedHash表示目标必须不存在。作者：long
func WriteSTRM(ctx context.Context, relative string, content []byte, expectedHash string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !Initialized() {
		return "", fmt.Errorf("STRM运行时尚未就绪")
	}
	if !outputPathValid(relative) {
		return "", ErrArtifactConflict
	}
	parent := path.Dir(relative)
	rootPath, err := libraryRootPath()
	if err != nil {
		return "", err
	}
	conf.STRMOutputMu.RLock()
	outputRoot := conf.STRMOutputPath
	conf.STRMOutputMu.RUnlock()
	rootInfo, err := os.Lstat(outputRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrArtifactConflict
	}
	if err = os.Mkdir(rootPath, 0o750); err != nil && !os.IsExist(err) {
		return "", err
	}
	rootInfo, err = os.Lstat(rootPath)
	if err != nil {
		return "", err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrArtifactConflict
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if parent != "." {
		if err = ensureOutputDirectories(root, parent); err != nil {
			return "", err
		}
	}
	base := path.Base(relative)
	if _, err = root.Lstat(relative); err == nil {
		if expectedHash == "" {
			return "", ErrArtifactConflict
		}
		oldHash, hashErr := hashRootFile(root, relative)
		if hashErr != nil || oldHash != expectedHash {
			return "", ErrArtifactConflict
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parentRoot, err := root.OpenRoot(parent)
	if err != nil {
		return "", err
	}
	defer parentRoot.Close()
	tmpName := ".strm-tmp-" + uuid.NewString()
	tmp, err := parentRoot.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return "", err
	}
	defer func() { _ = parentRoot.Remove(tmpName) }()
	if _, err = tmp.Write(content); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if !Initialized() {
		return "", fmt.Errorf("STRM运行时已停止")
	}
	if expectedHash == "" {
		if err = parentRoot.Link(tmpName, base); err != nil {
			if os.IsExist(err) {
				return "", ErrArtifactConflict
			}
			return "", err
		}
	} else {
		// long: 替换前再次比较旧摘要；内容已被管理员改动时保留原文件并报告冲突。
		oldHash, hashErr := hashRootFile(parentRoot, base)
		if hashErr != nil || oldHash != expectedHash {
			return "", ErrArtifactConflict
		}
		if err = parentRoot.Rename(tmpName, base); err != nil {
			return "", err
		}
	}
	if err = parentRoot.Remove(tmpName); err != nil {
		return hashBytes(content), err
	}
	if directory, openErr := parentRoot.Open("."); openErr == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return hashBytes(content), nil
}
