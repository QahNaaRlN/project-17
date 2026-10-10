package publishing_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// purges — ключи задач purge в очереди, по порядку постановки.
func purges(t *testing.T, e *cmstest.Env) [][]string {
	t.Helper()
	rows, err := e.Pool.Query(context.Background(), "SELECT args FROM river_job WHERE kind = 'cdn_purge' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var args jobs.PurgeArgs
		_ = json.Unmarshal(raw, &args)
		out = append(out, args.Keys)
	}
	return out
}

func TestPublicationsEnqueuePurge(t *testing.T) {
	e := setup(t)
	doc, p1, p2 := staged(e)
	if _, err := promote(e, p1.ID, "production"); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p2.ID}, nil)
	obj := doc.String()
	want := [][]string{
		{"store:staging:routes", "store:staging:" + obj},       // publish v1
		{"store:staging:routes", "store:staging:" + obj},       // publish v2
		{"store:production:routes", "store:production:" + obj}, // promote
		{"store:staging:routes", "store:staging:" + obj},       // rollback
	}
	got := purges(t, e)
	if len(got) != 4 {
		t.Fatalf("задачи: %v", got)
	}
	for i, keys := range want {
		if len(got[i]) != 2 || got[i][0] != keys[0] || got[i][1] != keys[1] {
			t.Errorf("задача %d: %v, ожидалось %v", i, got[i], keys)
		}
	}
}

// Без очереди публикация не проходит: задача инвалидации — часть транзакции (OPS-010).
func TestPublishWithoutQueueFails(t *testing.T) {
	e := setup(t)
	cs, _ := e.Draft(e.Admin, "v1")
	approve(e, cs)
	bus := commandbus.New(e.Pool)
	workflow.Register(bus)
	publishing.Register(bus)
	raw, _ := json.Marshal(map[string]any{"changesetId": cs, "environment": "staging"})
	_, err := bus.Dispatch(context.Background(), e.Admin, commandbus.Request{Name: "publish", IdempotencyKey: "k", Payload: raw})
	if err != jobs.ErrNoQueue {
		t.Errorf("%v", err)
	}
}
