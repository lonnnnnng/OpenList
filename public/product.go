//go:build strm_product

package public

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"io/fs"
)

//go:embed strm-bundle.zip
var bundle []byte

var Public fs.FS

func init() {
	// long: 向既有静态服务提供同一个dist目录契约，前端压缩包不会另起Node进程。
	r, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		panic("invalid STRM frontend bundle")
	}
	Public = r
}
