package redisstore_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/qahnaarln/project-17/apps/server/internal/redisstore"
)

// STU-522: availability changes are logged once, not on every request.
func TestObserve(t *testing.T) {
	var logs bytes.Buffer
	c := redisstore.New(redis.NewClient(&redis.Options{}), slog.New(slog.NewTextHandler(&logs, nil)))
	ctx := context.Background()
	if !c.Observe(ctx, nil) || !c.Observe(ctx, redis.Nil) || logs.Len() != 0 {
		t.Fatal(logs.String())
	}
	if c.Observe(ctx, errors.New("down")) || c.Observe(ctx, errors.New("down")) {
		t.Fatal("failure")
	}
	c.Observe(ctx, nil)
	if strings.Count(logs.String(), "redis недоступен") != 1 || strings.Count(logs.String(), "redis доступен") != 1 {
		t.Fatal(logs.String())
	}
	if _, err := redisstore.Open("http://nope", nil); err == nil {
		t.Fatal("bad url")
	}
	opened, err := redisstore.Open("redis://127.0.0.1:1/0", nil)
	if err != nil || opened.R.Options().ReadTimeout <= 0 || opened.Close() != nil {
		t.Fatal(err)
	}
}
