package jobs

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/qahnaarln/project-17/apps/server/internal/platform/telemetry"
)

// ObserveQueues — STU-598: queue depth and the age of the oldest waiting item, read at
// every metric collection: River jobs by queue and kind, pipeline checks 8–11 by stage.
func ObserveQueues(pool *pgxpool.Pool) error {
	m := telemetry.Meter()
	depth, err := m.Int64ObservableGauge("cms.jobs.depth", metric.WithDescription("Available and scheduled River jobs"))
	if err != nil {
		return err
	}
	jobAge, err := m.Float64ObservableGauge("cms.jobs.oldest_age", metric.WithUnit("s"), metric.WithDescription("Age of the oldest available River job"))
	if err != nil {
		return err
	}
	checks, err := m.Int64ObservableGauge("cms.checks.queued", metric.WithDescription("Queued pipeline checks 8–11"))
	if err != nil {
		return err
	}
	checkAge, err := m.Float64ObservableGauge("cms.checks.oldest_age", metric.WithUnit("s"), metric.WithDescription("Age of the oldest queued pipeline check"))
	if err != nil {
		return err
	}
	_, err = m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		return observe(ctx, pool, o, depth, jobAge, checks, checkAge)
	}, depth, jobAge, checks, checkAge)
	return err
}

func observe(ctx context.Context, pool *pgxpool.Pool, o metric.Observer, depth metric.Int64ObservableGauge, jobAge metric.Float64ObservableGauge, checks metric.Int64ObservableGauge, checkAge metric.Float64ObservableGauge) error {
	rows, err := pool.Query(ctx, `SELECT queue, kind, count(*),
	    COALESCE(EXTRACT(EPOCH FROM now() - min(scheduled_at) FILTER (WHERE scheduled_at <= now())), 0)::float8
	  FROM river_job WHERE state IN ('available', 'scheduled', 'retryable') GROUP BY queue, kind`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var queue, kind string
		var n int64
		var age float64
		if err := rows.Scan(&queue, &kind, &n, &age); err != nil {
			rows.Close()
			return err
		}
		attrs := metric.WithAttributes(attribute.String("messaging.destination.name", queue), attribute.String("cms.job_kind", kind))
		o.ObserveInt64(depth, n, attrs)
		o.ObserveFloat64(jobAge, age, attrs)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = pool.Query(ctx, `SELECT stage, count(*), COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8
	  FROM checks WHERE status = 'queued' GROUP BY stage`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var stage string
		var n int64
		var age float64
		if err := rows.Scan(&stage, &n, &age); err != nil {
			return err
		}
		attrs := metric.WithAttributes(attribute.String("cms.stage", stage))
		o.ObserveInt64(checks, n, attrs)
		o.ObserveFloat64(checkAge, age, attrs)
	}
	return rows.Err()
}
