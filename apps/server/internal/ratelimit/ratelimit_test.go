package ratelimit_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/qahnaarln/project-17/apps/server/internal/ratelimit"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/redistest"
)

// STU-530, 531, 533: a fixed one-second window per subject and class.
func TestLimiter(t *testing.T) {
	ctx := context.Background()
	for name, l := range map[string]*ratelimit.Limiter{"memory": ratelimit.New(nil), "redis": ratelimit.New(redistest.New(t))} {
		t.Run(name, func(t *testing.T) {
			now := time.Unix(2_000_000_000, 250*int64(time.Millisecond))
			l.WithClock(func() time.Time { return now })
			for i := 0; i < 2; i++ {
				if ok, _ := l.Allow(ctx, "key:a", "delivery", 2); !ok {
					t.Fatal(i)
				}
			}
			ok, wait := l.Allow(ctx, "key:a", "delivery", 2)
			if ok || wait != 750*time.Millisecond {
				t.Fatal(ok, wait)
			}
			// Other subjects and classes have their own counters.
			if ok, _ := l.Allow(ctx, "key:b", "delivery", 2); !ok {
				t.Fatal("other subject")
			}
			if ok, _ := l.Allow(ctx, "key:a", "preview", 2); !ok {
				t.Fatal("other class")
			}
			now = now.Add(time.Second)
			if ok, _ := l.Allow(ctx, "key:a", "delivery", 2); !ok {
				t.Fatal("next window")
			}
			if ok, _ := l.Allow(ctx, "key:a", "delivery", 0); !ok {
				t.Fatal("0 switches the limit off")
			}
		})
	}
	var nilLimiter *ratelimit.Limiter
	if ok, _ := nilLimiter.Allow(ctx, "x", "y", 1); !ok {
		t.Fatal("nil limiter")
	}
}

// STU-533: a broken Redis never refuses a request and is logged once.
func TestLimiterFailsOpen(t *testing.T) {
	var logs bytes.Buffer
	l := ratelimit.New(redistest.Down(t, slog.New(slog.NewTextHandler(&logs, nil))))
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(context.Background(), "key:a", "delivery", 1); !ok {
			t.Fatal(i)
		}
	}
	if strings.Count(logs.String(), "redis недоступен") != 1 {
		t.Fatal(logs.String())
	}
}

func TestDefaults(t *testing.T) {
	if d := ratelimit.Defaults(); d.HumanCommands != 50 || d.HumanRequests != 200 || d.ServiceCommands != 20 || d.AgentCommands != 5 || d.Delivery != 500 || d.Preview != 200 {
		t.Fatal(d)
	}
}
