package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr string

	DatabaseURL string
	JWTSecret   string
	LogLevel    slog.Level

	// Optional for now; consumers land in later phases (Redis session/cache
	// P1 late, MinIO artifacts P3+).
	RedisAddr      string
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
}

// Load reads the environment and fails fast when a critical key is missing,
// reporting every missing key at once instead of the first one.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:       getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		RedisAddr:      os.Getenv("REDIS_ADDR"),
		MinioEndpoint:  os.Getenv("MINIO_ENDPOINT"),
		MinioAccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey: os.Getenv("MINIO_SECRET_KEY"),
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}

	cfg.LogLevel = parseLevel(os.Getenv("LOG_LEVEL"))
	return cfg, nil
}

func parseLevel(value string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
