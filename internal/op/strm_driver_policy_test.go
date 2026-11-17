package op

import (
	"context"
	"errors"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"testing"
)

func TestSTRMRejectsUnsupportedDriverBeforeDatabaseAccess(t *testing.T) {
	old := conf.STRMCommandMode
	conf.STRMCommandMode = true
	t.Cleanup(func() { conf.STRMCommandMode = old })
	storage := model.Storage{Driver: "Strm"}
	if id, err := CreateStorage(context.Background(), storage); id != 0 || !errors.Is(err, conf.ErrSTRMUnsupportedDriver) {
		t.Fatalf("create reached driver/database: %d %v", id, err)
	}
	if err := UpdateStorage(context.Background(), storage); !errors.Is(err, conf.ErrSTRMUnsupportedDriver) {
		t.Fatalf("update reached database: %v", err)
	}
}
