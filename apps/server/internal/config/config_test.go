package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{"CMS_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 15*time.Second || cfg.DatabaseURL != "postgres://x" {
		t.Errorf("конфигурация по умолчанию: %+v", cfg)
	}
}

func TestCustomValues(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{
		"CMS_DATABASE_URL": "postgres://x", "CMS_HTTP_ADDR": "127.0.0.1:9000",
		"CMS_LOG_LEVEL": "debug", "CMS_SHUTDOWN_TIMEOUT": "3s",
		"CMS_CDN_PURGE_URL": "https://cdn.example/purge", "CMS_CDN_PURGE_TOKEN": "t",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second ||
		cfg.CDNPurgeURL != "https://cdn.example/purge" || cfg.CDNPurgeToken != "t" {
		t.Errorf("конфигурация: %+v", cfg)
	}
}

func TestBlankValuesFallBackToDefaults(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{"CMS_DATABASE_URL": "postgres://x", "CMS_HTTP_ADDR": "  "}))
	if err != nil || cfg.HTTPAddr != ":8080" {
		t.Errorf("пустое значение: %+v, %v", cfg, err)
	}
}

func TestErrorsAreReportedTogether(t *testing.T) {
	_, err := FromEnv(env(map[string]string{"CMS_LOG_LEVEL": "loud", "CMS_SHUTDOWN_TIMEOUT": "-1s"}))
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	for _, want := range []string{"CMS_DATABASE_URL", "CMS_LOG_LEVEL", "CMS_SHUTDOWN_TIMEOUT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %s: %v", want, err)
		}
	}
}

func TestInvalidDuration(t *testing.T) {
	for _, v := range []string{"soon", "0s", "-5s"} {
		if _, err := FromEnv(env(map[string]string{"CMS_DATABASE_URL": "x", "CMS_SHUTDOWN_TIMEOUT": v})); err == nil {
			t.Errorf("%q: ожидалась ошибка", v)
		}
	}
}
