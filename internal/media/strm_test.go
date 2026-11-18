package media

import (
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestStrmOptionsForScanPath(t *testing.T) {
	global := StrmOptions{OutputPath: "/quark/strm", SameDir: true}

	got := StrmOptionsForScanPath(global, &model.MediaScanPath{StrmPath: " /alipan/strm "})
	if got.OutputPath != "/alipan/strm" || got.SameDir {
		t.Fatalf("scan path override = %#v, want /alipan/strm with same-dir disabled", got)
	}

	got = StrmOptionsForScanPath(global, &model.MediaScanPath{})
	if got != global {
		t.Fatalf("empty scan path override = %#v, want global options %#v", got, global)
	}
}
