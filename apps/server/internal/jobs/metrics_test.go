package jobs_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

// STU-598: queue depth and age come from River jobs and queued pipeline checks.
func TestObserveQueues(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	pool := pgtest.NewDB(t)
	queue, err := jobs.NewInserter(pool)
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := pool.Begin(ctx)
	if err := jobs.Enqueue(jobs.WithClient(queue)(ctx), tx, jobs.PurgeArgs{Keys: []string{"k"}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// A queued pipeline check of a submitted Change Set counts per stage.
	if _, err := pool.Exec(ctx, `WITH p AS (INSERT INTO projects (id, slug, name) VALUES (gen_random_uuid(), 'metrics', 'M') RETURNING id),
	  a AS (INSERT INTO actors (id, kind, display_name) VALUES (gen_random_uuid(), 'human', 'A') RETURNING id),
	  c AS (INSERT INTO changesets (id, project_id, kind, title, owner_id, state) SELECT gen_random_uuid(), p.id, 'standard', 't', a.id, 'checking' FROM p, a RETURNING id)
	  INSERT INTO checks (id, changeset_id, content_hash, stage, status, blocking) SELECT gen_random_uuid(), c.id, decode('00', 'hex'), 'visual', 'queued', false FROM c`); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ObserveQueues(pool); err != nil {
		t.Fatal(err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	depth := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if g, ok := m.Data.(metricdata.Gauge[int64]); ok && m.Name == "cms.jobs.depth" {
				for _, p := range g.DataPoints {
					if kind, _ := p.Attributes.Value("cms.job_kind"); kind.AsString() == (jobs.PurgeArgs{}).Kind() {
						depth = int(p.Value)
					}
				}
			}
		}
	}
	queued := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if g, ok := m.Data.(metricdata.Gauge[int64]); ok && m.Name == "cms.checks.queued" {
				for _, p := range g.DataPoints {
					queued += int(p.Value)
				}
			}
		}
	}
	if depth != 1 || queued != 1 {
		t.Fatal(rm.ScopeMetrics)
	}
	// A broken database surfaces as a collection error, not a panic.
	if _, err := pool.Exec(ctx, `ALTER TABLE checks RENAME TO checks_broken`); err != nil {
		t.Fatal(err)
	}
	if err := reader.Collect(ctx, &rm); err == nil {
		t.Fatal("collection error lost")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE river_job RENAME TO river_job_broken`); err != nil {
		t.Fatal(err)
	}
	if err := reader.Collect(ctx, &rm); err == nil {
		t.Fatal("collection error lost")
	}
}
