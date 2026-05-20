package config

import "os"

type Config struct {
	Port string

	DBDriver string

	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	DBSSLMode  string

	SQLitePath string

	RedisAddr     string
	RedisPassword string
	RedisDB       string

	BuildVersion string
	BuildCommit  string
	Environment  string
}

func Load(buildVersion, buildCommit string) Config {
	return Config{
		Port:          getenv("PORT", "8080"),
		DBDriver:      getenv("DB_DRIVER", "sqlite"),
		DBHost:        getenv("DB_HOST", "localhost"),
		DBPort:        getenv("DB_PORT", "5432"),
		DBName:        getenv("DB_NAME", "shortener"),
		DBUser:        getenv("DB_USER", "postgres"),
		DBPassword:    getenv("DB_PASSWORD", "postgres"),
		DBSSLMode:     getenv("DB_SSLMODE", "disable"),
		SQLitePath:    getenv("SQLITE_PATH", "./data.db"),
		RedisAddr:     getenv("REDIS_ADDR", ""),
		RedisPassword: getenv("REDIS_PASSWORD", ""),
		RedisDB:       getenv("REDIS_DB", "0"),
		BuildVersion:  buildVersion,
		BuildCommit:   buildCommit,
		Environment:   getenv("APP_ENV", "dev"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
