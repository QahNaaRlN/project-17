// Package redisstore — подключение к Redis для кэша Delivery API и лимитов запросов
// (docs/delivery-cache-limits.md, STU-520, 522). Redis хранит только производные данные
// (ST-001): его недоступность ухудшает задержку, но не доступность и не корректность.
package redisstore

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client — клиент Redis с отслеживанием доступности.
type Client struct {
	R    *redis.Client
	log  *slog.Logger
	down atomic.Bool
}

// ParseURL проверяет CMS_REDIS_URL при загрузке конфигурации.
func ParseURL(rawURL string) (*redis.Options, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	// Короткие тайм-ауты: недоступный Redis должен давать промах, а не ожидание.
	opts.DialTimeout = 500 * time.Millisecond
	opts.ReadTimeout = 200 * time.Millisecond
	opts.WriteTimeout = 200 * time.Millisecond
	opts.MaxRetries = 1
	return opts, nil
}

// Open подключается по CMS_REDIS_URL; соединение устанавливается при первом запросе.
func Open(rawURL string, log *slog.Logger) (*Client, error) {
	opts, err := ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return New(redis.NewClient(opts), log), nil
}

// New оборачивает готовый клиент.
func New(r *redis.Client, log *slog.Logger) *Client {
	return &Client{R: r, log: log}
}

// Observe отмечает результат обращения к Redis и сообщает, удалось ли оно. Смена
// состояния пишется в журнал один раз, а не на каждый запрос (STU-522).
func (c *Client) Observe(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, redis.Nil) {
		if c.down.Swap(false) {
			c.log.InfoContext(ctx, "redis доступен")
		}
		return true
	}
	if !c.down.Swap(true) {
		c.log.WarnContext(ctx, "redis недоступен: кэш Delivery выключен, лимиты не применяются", "error", err)
	}
	return false
}

// Close закрывает соединения.
func (c *Client) Close() error { return c.R.Close() }
