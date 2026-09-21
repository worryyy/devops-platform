package db

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/worryyy/devops-platform/platform/server/internal/config"
)

// Open connects GORM to PostgreSQL with a bounded pool sized for the
// platform's single-instance API server.
func Open(ctx context.Context, cfg config.Config) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	}
	dialector := postgres.New(postgres.Config{DSN: cfg.DatabaseURL})
	db, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}
