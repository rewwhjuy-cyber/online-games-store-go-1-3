package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Addr, DatabaseURL, JWTSecret string
	AccessTTL, RefreshTTL        time.Duration
}

func Load() Config {
	loadDotEnv(".env")
	return Config{Addr: env("APP_ADDR", ":8080"), DatabaseURL: env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/online_games_store?sslmode=disable"), JWTSecret: env("JWT_SECRET", "development-only-change-this-secret-32chars"), AccessTTL: duration("ACCESS_TTL", 15*time.Minute), RefreshTTL: duration("REFRESH_TTL", 30*24*time.Hour)}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func duration(k string, d time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	x, e := time.ParseDuration(v)
	if e != nil {
		return d
	}
	return x
}

func loadDotEnv(path string) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if key != "" {
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, value)
			}
		}
	}
}
