package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func main() {
	source := flag.String("source", "web/dist", "前端产物目录")
	output := flag.String("output", "public/strm-bundle.zip", "独立嵌入包")
	epoch := flag.Int64("epoch", 315532800, "固定构建时间")
	flag.Parse()
	if err := pack(*source, *output, time.Unix(*epoch, 0).UTC()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// pack 只读取真实前端产物，固定顺序/时间；用独立包避免清理上游dist。作者：long
func pack(source, output string, stamp time.Time) error {
	var paths []string
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("frontend bundle contains a symlink")
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(source, "index.html")); err != nil {
		return fmt.Errorf("frontend index.html is missing")
	}
	sort.Strings(paths)
	f, err := os.CreateTemp(filepath.Dir(output), ".strm-bundle-*")
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, path := range paths {
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		h := &zip.FileHeader{Name: "dist/" + filepath.ToSlash(name), Method: zip.Deflate}
		h.SetModTime(stamp)
		h.SetMode(0o644)
		entry, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err = entry.Write(bytes); err != nil {
			return err
		}
	}
	if err = w.Close(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), output)
}
