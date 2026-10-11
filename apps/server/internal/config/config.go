// Package config читает конфигурацию сервера из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Config — конфигурация процесса cms.
type Config struct {
	HTTPAddr        string        // CMS_HTTP_ADDR, по умолчанию ":8080"
	DatabaseURL     string        // CMS_DATABASE_URL, обязательно
	LogLevel        slog.Level    // CMS_LOG_LEVEL: debug | info | warn | error
	ShutdownTimeout time.Duration // CMS_SHUTDOWN_TIMEOUT, по умолчанию 15s
	CDNPurgeURL     string        // CMS_CDN_PURGE_URL — вебхук purge CDN; пусто — purge только в журнал
	CDNPurgeToken   string        // CMS_CDN_PURGE_TOKEN — Bearer-токен вебхука
	S3Endpoint      string
	S3AccessKey     string
	S3SecretKey     string
	S3Bucket        string
	S3Secure        bool
}

// FromEnv читает конфигурацию; getenv — обычно os.Getenv.
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:        valueOr(getenv("CMS_HTTP_ADDR"), ":8080"),
		DatabaseURL:     getenv("CMS_DATABASE_URL"),
		ShutdownTimeout: 15 * time.Second,
		CDNPurgeURL:     getenv("CMS_CDN_PURGE_URL"),
		CDNPurgeToken:   getenv("CMS_CDN_PURGE_TOKEN"),
		S3Endpoint:      getenv("CMS_S3_ENDPOINT"),
		S3AccessKey:     getenv("CMS_S3_ACCESS_KEY"),
		S3SecretKey:     getenv("CMS_S3_SECRET_KEY"),
		S3Bucket:        getenv("CMS_S3_BUCKET"),
		S3Secure:        getenv("CMS_S3_SECURE") != "false",
	}
	var errs []error
	if cfg.S3Endpoint != "" || cfg.S3AccessKey != "" || cfg.S3SecretKey != "" || cfg.S3Bucket != "" {
		if cfg.S3Endpoint == "" || cfg.S3AccessKey == "" || cfg.S3SecretKey == "" || cfg.S3Bucket == "" {
			errs = append(errs, errors.New("CMS_S3_ENDPOINT, ACCESS_KEY, SECRET_KEY, BUCKET нужны вместе"))
		}
		if raw := getenv("CMS_S3_SECURE"); raw != "" && raw != "true" && raw != "false" {
			errs = append(errs, errors.New("CMS_S3_SECURE: true или false"))
		}
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("CMS_DATABASE_URL не задан"))
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(valueOr(getenv("CMS_LOG_LEVEL"), "info"))); err != nil {
		errs = append(errs, fmt.Errorf("CMS_LOG_LEVEL: %w", err))
	}
	if raw := getenv("CMS_SHUTDOWN_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("CMS_SHUTDOWN_TIMEOUT: недопустимая длительность %q", raw))
		} else {
			cfg.ShutdownTimeout = d
		}
	}
	return cfg, errors.Join(errs...)
}

func valueOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
