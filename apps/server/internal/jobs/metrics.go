package jobs

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/qahnaarln/project-17/apps/server/internal/platform/telemetry"
)

// ObserveQueues — STU-598: queue depth and the age of the oldest waiting item, read at
// every metric collection: River jobs by queue and kind, pipeline checks 8–11 by stage.
func ObserveQueues(pool *pgxpool.Pool) error {
	// Instrument constructors fail only on invalid names; these are constants.
	m := telemetry.Meter()
	depth, _ := m.Int64ObservableGauge("cms.jobs.depth", metric.WithDescription("Available and scheduled River jobs"))
	jobAge, _ := m.Float64ObservableGauge("cms.jobs.oldest_age", metric.WithUnit("s"), metric.WithDescription("Age of the oldest available River job"))
	checks, _ := m.Int64ObservableGauge("cms.checks.queued", metric.WithDescription("Queued pipeline checks 8–11"))
	checkAge, _ := m.Float64ObservableGauge("cms.checks.oldest_age", metric.WithUnit("s"), metric.WithDescription("Age of the oldest queued pipeline check"))
	_, err := m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		return observe(ctx, pool, o, depth, jobAge, checks, checkAge)
	}, depth, jobAge, checks, checkAge)
	return err
}

// queueRow — one group of waiting items: its labels, size and the age of the oldest.
type queueRow struct {
	Name, Kind string
	Count      int64
	Age        float64
}

func queueRows(ctx context.Context, pool *pgxpool.Pool, sql string) ([]queueRow, error) {
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[queueRow])
}

func observe(ctx context.Context, pool *pgxpool.Pool, o metric.Observer, depth metric.Int64ObservableGauge, jobAge metric.Float64ObservableGauge, checks metric.Int64ObservableGauge, checkAge metric.Float64ObservableGauge) error {
	jobs, err := queueRows(ctx, pool, `SELECT queue, kind, count(*),
	    COALESCE(EXTRACT(EPOCH FROM now() - min(scheduled_at) FILTER (WHERE scheduled_at <= now())), 0)::float8
	  FROM river_job WHERE state IN ('available', 'scheduled', 'retryable') GROUP BY queue, kind`)
	if err != nil {
		return err
	}
	stages, err := queueRows(ctx, pool, `SELECT stage, '', count(*), COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8
	  FROM checks WHERE status = 'queued' GROUP BY stage`)
	if err != nil {
		return err
	}
	for _, r := range jobs {
		attrs := metric.WithAttributes(attribute.String("messaging.destination.name", r.Name), attribute.String("cms.job_kind", r.Kind))
		o.ObserveInt64(depth, r.Count, attrs)
		o.ObserveFloat64(jobAge, r.Age, attrs)
	}
	for _, r := range stages {
		attrs := metric.WithAttributes(attribute.String("cms.stage", r.Name))
		o.ObserveInt64(checks, r.Count, attrs)
		o.ObserveFloat64(checkAge, r.Age, attrs)
	}
	return nil
}
