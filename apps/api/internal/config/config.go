package config

import "os"

type Config struct {
	AppPort       string
	MySQLDSN      string
	MongoURI      string
	MongoDatabase string
}

func Load() Config {
	return Config{
		AppPort:       getEnv("APP_PORT", "8080"),
		MySQLDSN:      getEnv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/campus_forum?charset=utf8mb4&parseTime=True&loc=Local"),
		MongoURI:      getEnv("MONGO_URI", "mongodb://127.0.0.1:27017"),
		MongoDatabase: getEnv("MONGO_DATABASE", "campus_forum"),
	}
}

func getEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
