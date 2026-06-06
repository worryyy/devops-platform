package database

import (
	"context"
	"time"

	"campus-forum/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func NewMySQL(cfg config.Config) (*gorm.DB, error) {
	return gorm.Open(mysql.Open(cfg.MySQLDSN), &gorm.Config{})
}

func PingMySQL(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return sqlDB.PingContext(pingCtx)
}
