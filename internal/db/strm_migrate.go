package db

import (
	"errors"
	"fmt"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"gorm.io/gorm"
)

const STRMSchemaVersion uint = 3

// MigrateSTRM 只追加产品表；发现更高版本时拒绝降级，不能尝试清库后继续运行。作者：long
func MigrateSTRM() error {
	if db.Migrator().HasTable(&model.STRMSchema{}) {
		var current model.STRMSchema
		// long: 建表后、写版本行前崩溃会留下空版本表；允许重走幂等迁移，不能将正常中断当作损坏库。
		if err := db.First(&current, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if current.Version > STRMSchemaVersion {
			return fmt.Errorf("STRM database is newer than this binary; restore a matching version")
		}
	}
	if err := AutoMigrate(model.STRMModels()...); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&model.STRMSchema{}).Where("id = ?", 1).Assign(model.STRMSchema{Version: STRMSchemaVersion}).FirstOrCreate(&model.STRMSchema{ID: 1}).Error
	})
}
