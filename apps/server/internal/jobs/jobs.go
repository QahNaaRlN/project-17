// Package jobs — фоновые задачи на очереди River (docs/spec/12-security-ops.md §1.1, D-16).
//
// Задачи ставятся в транзакции команды (outbox, OPS-010): если команда откатилась, задачи нет.
// Первая задача — инвалидация кэша Delivery и CDN по Surrogate-Key после публикации (PUB-021, SDK-022, STU-526).
package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// SurrogateKey — ключ кэша CDN для объекта (или "routes") в окружении проекта (API-033).
// Ключ включает проект и окружение, чтобы purge одного окружения не задевал другие.
func SurrogateKey(project, environment, object string) string {
	return project + ":" + environment + ":" + object
}

// PurgeArgs — задача purge CDN по ключам.
type PurgeArgs struct {
	Keys []string `json:"keys"`
}

// Kind — имя задачи в очереди.
func (PurgeArgs) Kind() string { return "cdn_purge" }

// Purger сбрасывает кэш CDN по Surrogate-Key.
type Purger interface {
	Purge(ctx context.Context, keys []string) error
}

// LogPurger только пишет в журнал — для окружений без CDN.
type LogPurger struct{ Log *slog.Logger }

// Purge записывает ключи в журнал.
func (p LogPurger) Purge(ctx context.Context, keys []string) error {
	p.Log.InfoContext(ctx, "cdn purge (без CDN)", "keys", keys)
	return nil
}

// HTTPPurger отправляет POST {"keys": [...]} на вебхук CDN (Fastly/Cloudflare-совместимый
// прокси или собственный адаптер) с заголовком Authorization: Bearer <token>.
type HTTPPurger struct {
	URL    string
	Token  string
	Client *http.Client
}

// Purge вызывает вебхук; ответ не 2xx — ошибка (River повторит задачу).
func (p HTTPPurger) Purge(ctx context.Context, keys []string) error {
	body, _ := json.Marshal(PurgeArgs{Keys: keys})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("cdn purge: %s", resp.Status)
	}
	return nil
}

// PurgeWorker исполняет задачи purge.
type PurgeWorker struct {
	river.WorkerDefaults[PurgeArgs]
	Purger Purger
}

// Work сбрасывает кэш CDN по ключам задачи.
func (w *PurgeWorker) Work(ctx context.Context, job *river.Job[PurgeArgs]) error {
	return w.Purger.Purge(ctx, job.Args.Keys)
}

// InvalidateArgs — задача инвалидации кэша Delivery на origin (STU-526). Публикация ставит
// её в outbox; после смены версий тегов она ставит purge CDN с теми же ключами, чтобы CDN
// не перезапросил старый ответ из Redis.
type InvalidateArgs struct {
	Keys []string `json:"keys"`
}

// Kind — имя задачи в очереди.
func (InvalidateArgs) Kind() string { return "delivery_cache_invalidate" }

// Invalidator меняет версии тегов кэша Delivery; nil-реализация ничего не делает.
type Invalidator interface {
	Invalidate(ctx context.Context, keys []string) error
}

// InvalidateWorker исполняет задачи инвалидации.
type InvalidateWorker struct {
	river.WorkerDefaults[InvalidateArgs]
	Cache Invalidator
}

// Work инвалидирует кэш и ставит purge CDN; ошибка Redis — повтор задачи River.
func (w *InvalidateWorker) Work(ctx context.Context, job *river.Job[InvalidateArgs]) error {
	if w.Cache != nil {
		if err := w.Cache.Invalidate(ctx, job.Args.Keys); err != nil {
			return err
		}
	}
	_, err := river.ClientFromContext[pgx.Tx](ctx).Insert(ctx, PurgeArgs{Keys: job.Args.Keys}, nil)
	return err
}

// Client — клиент очереди.
type Client = river.Client[pgx.Tx]

// NewInserter — клиент, который только ставит задачи (без обработчиков).
func NewInserter(pool *pgxpool.Pool) (*Client, error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{})
}

// NewProcessor — клиент, который ставит и исполняет задачи; запускается Start.
func NewProcessor(pool *pgxpool.Pool, purger Purger, cache Invalidator, log *slog.Logger, register ...func(*river.Workers) error) (*Client, error) {
	workers := river.NewWorkers()
	if err := river.AddWorkerSafely(workers, &PurgeWorker{Purger: purger}); err != nil {
		return nil, err
	}
	if err := river.AddWorkerSafely(workers, &InvalidateWorker{Cache: cache}); err != nil {
		return nil, err
	}
	for _, add := range register {
		if err := add(workers); err != nil {
			return nil, err
		}
	}
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}, "capability_delivery": {MaxWorkers: 2}, "capability_github": {MaxWorkers: 2}},
		Workers: workers,
		Logger:  log,
	})
}

type clientKey struct{}

// WithClient кладёт клиент очереди в контекст (подключается к шине команд через WithContext).
func WithClient(c *Client) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context { return context.WithValue(ctx, clientKey{}, c) }
}

// ErrNoQueue — в контексте нет клиента очереди (ошибка конфигурации).
var ErrNoQueue = errors.New("jobs: очередь задач не настроена")

// Enqueue ставит задачу в транзакции tx.
func Enqueue(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
	c, _ := ctx.Value(clientKey{}).(*Client)
	if c == nil {
		return ErrNoQueue
	}
	_, err := c.InsertTx(ctx, tx, args, nil)
	return err
}
