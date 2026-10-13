package commandbus_test

import (
	"context"
	"sync"
	"testing"
)

func TestRevisionIsAtomicCoalescedAndIdempotent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	revision := func() int {
		var n int
		if err := e.pool.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM project_revisions WHERE project_id=$1),0)`, e.actor.ProjectID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	r := req("live", `{"text":"Live"}`)
	if _, err := e.bus.Dispatch(ctx, e.actor, r); err != nil {
		t.Fatal(err)
	}
	if revision() != 1 {
		t.Fatal("missing revision")
	}
	if _, err := e.bus.Dispatch(ctx, e.actor, r); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bus.Dispatch(ctx, e.actor, req("fail", `{"text":"fail"}`)); err == nil {
		t.Fatal("wanted failure")
	}
	if revision() != 1 {
		t.Fatal("replay/failure emitted")
	}
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.bus.Dispatch(ctx, e.actor, req(string(rune('a'+i)), `{"text":"Parallel"}`)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if revision() != 11 {
		t.Fatal("lost concurrent revision")
	}
	// A failure to persist invalidation must roll back the command and idempotency.
	if _, err := e.pool.Exec(ctx, `ALTER TABLE project_revisions ADD CONSTRAINT fault CHECK (revision<12)`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bus.Dispatch(ctx, e.actor, req("late-fail", `{"text":"Should rollback"}`)); err == nil {
		t.Fatal("wanted late failure")
	}
	if e.projectName(t) != "Parallel" || revision() != 11 {
		t.Fatal("partial commit")
	}
}
