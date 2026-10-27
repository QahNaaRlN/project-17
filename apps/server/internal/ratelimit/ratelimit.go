// Package ratelimit — лимиты частоты запросов (08 §7, docs/delivery-cache-limits.md,
// STU-530…535): фиксированное окно 1 с, счётчик в Redis `rl:{subject}:{class}:{window}`.
// Без Redis счётчики живут в памяти процесса; сбой Redis не отказывает в запросе (fail open).
package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/qahnaarln/project-17/apps/server/internal/redisstore"
)

// Limits — лимиты в секунду; 0 выключает лимит.
type Limits struct {
	HumanCommands   int // CMS_RATE_HUMAN_COMMANDS
	HumanRequests   int // CMS_RATE_HUMAN_REQUESTS — все запросы /api/v1
	ServiceCommands int // CMS_RATE_SERVICE_COMMANDS
	AgentCommands   int // CMS_RATE_AGENT_COMMANDS
	Delivery        int // CMS_RATE_DELIVERY — на ключ доставки
	Preview         int // CMS_RATE_PREVIEW — на субъект preview-токена
}

// Defaults — значения 08 §7.
func Defaults() Limits {
	return Limits{HumanCommands: 50, HumanRequests: 200, ServiceCommands: 20, AgentCommands: 5, Delivery: 500, Preview: 200}
}

// Limiter считает запросы субъектов.
type Limiter struct {
	redis *redisstore.Client
	now   func() time.Time

	mu     sync.Mutex
	window int64
	counts map[string]int
}

// New — счётчики в Redis; nil — в памяти процесса.
func New(c *redisstore.Client) *Limiter {
	return &Limiter{redis: c, now: time.Now, counts: map[string]int{}}
}

// WithClock подменяет часы (тесты).
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.now = now
	return l
}

// Allow учитывает запрос subject в классе class и сообщает, укладывается ли он в limit;
// при отказе — сколько ждать до конца окна.
func (l *Limiter) Allow(ctx context.Context, subject, class string, limit int) (bool, time.Duration) {
	if l == nil || limit <= 0 {
		return true, 0
	}
	now := l.now()
	window := now.Unix()
	wait := time.Unix(window+1, 0).Sub(now)
	var n int64
	if l.redis != nil {
		key := "rl:" + subject + ":" + class + ":" + strconv.FormatInt(window, 10)
		pipe := l.redis.R.Pipeline()
		incr := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, 2*time.Second)
		if _, err := pipe.Exec(ctx); !l.redis.Observe(ctx, err) {
			return true, 0
		}
		n = incr.Val()
	} else {
		l.mu.Lock()
		if l.window != window {
			l.window, l.counts = window, map[string]int{}
		}
		l.counts[subject+":"+class]++
		n = int64(l.counts[subject+":"+class])
		l.mu.Unlock()
	}
	return n <= int64(limit), wait
}
