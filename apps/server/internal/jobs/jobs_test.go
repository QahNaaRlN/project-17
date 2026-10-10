package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riverqueue/river"

	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

func TestSurrogateKey(t *testing.T) {
	if got := jobs.SurrogateKey("store", "staging", "routes"); got != "store:staging:routes" {
		t.Error(got)
	}
	if (jobs.PurgeArgs{}).Kind() != "cdn_purge" {
		t.Error("kind")
	}
}

func TestHTTPPurger(t *testing.T) {
	var got struct {
		auth string
		body jobs.PurgeArgs
	}
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	p := jobs.HTTPPurger{URL: srv.URL, Token: "secret"}
	if err := p.Purge(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if got.auth != "Bearer secret" || len(got.body.Keys) != 2 {
		t.Errorf("%+v", got)
	}
	status = http.StatusBadGateway
	if err := p.Purge(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("ошибка CDN: %v", err)
	}
	// Без токена заголовок не ставится; недоступный адрес и неверный URL — ошибки.
	status = http.StatusNoContent
	if err := (jobs.HTTPPurger{URL: srv.URL, Client: srv.Client()}).Purge(context.Background(), nil); err != nil || got.auth != "" {
		t.Errorf("без токена: %v %q", err, got.auth)
	}
	if err := (jobs.HTTPPurger{URL: "http://127.0.0.1:1"}).Purge(context.Background(), nil); err == nil {
		t.Error("недоступный адрес")
	}
	if err := (jobs.HTTPPurger{URL: "::"}).Purge(context.Background(), nil); err == nil {
		t.Error("неверный URL")
	}
}

type recordPurger struct{ keys chan []string }

func (p recordPurger) Purge(_ context.Context, keys []string) error {
	p.keys <- keys
	return nil
}

func TestQueueRunsPurgeAfterCommit(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	purged := recordPurger{keys: make(chan []string, 1)}
	var logs bytes.Buffer
	queue, err := jobs.NewProcessor(pool, purged, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Stop(ctx) })

	// Откат транзакции — задачи нет.
	qctx := jobs.WithClient(queue)(ctx)
	tx, _ := pool.Begin(ctx)
	if err := jobs.Enqueue(qctx, tx, jobs.PurgeArgs{Keys: []string{"rolled-back"}}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)

	tx, _ = pool.Begin(ctx)
	if err := jobs.Enqueue(qctx, tx, jobs.PurgeArgs{Keys: []string{"store:staging:x"}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if keys := <-purged.keys; len(keys) != 1 || keys[0] != "store:staging:x" {
		t.Errorf("purge: %v", keys)
	}

	tx, _ = pool.Begin(ctx)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := jobs.Enqueue(ctx, tx, jobs.PurgeArgs{}); err != jobs.ErrNoQueue {
		t.Errorf("без очереди: %v", err)
	}
}

func TestLogPurgerAndWorker(t *testing.T) {
	var logs bytes.Buffer
	w := &jobs.PurgeWorker{Purger: jobs.LogPurger{Log: slog.New(slog.NewTextHandler(&logs, nil))}}
	if err := w.Work(context.Background(), &river.Job[jobs.PurgeArgs]{Args: jobs.PurgeArgs{Keys: []string{"k"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "cdn purge") {
		t.Errorf("журнал: %s", logs.String())
	}
}
