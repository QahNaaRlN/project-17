package telemetry

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// QueryTracer — STU-594: a span per SQL query named after its sqlc query
// (`-- name: X :one`); the SQL text and arguments never become attributes.
type QueryTracer struct{}

// QueryName — the sqlc name of a query, or "sql" for inline statements.
func QueryName(sql string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(sql), "-- name: ")
	if !ok {
		return "sql"
	}
	if name, _, ok := strings.Cut(rest, " "); ok && name != "" {
		return name
	}
	return "sql"
}

// TraceQueryStart opens the span only inside a trace: background pool work stays silent.
func (QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	name := QueryName(data.SQL)
	ctx, _ = Tracer().Start(ctx, "sql "+name, trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.DBSystemNamePostgreSQL, attribute.String("db.query.summary", name)))
	return ctx
}

// TraceQueryEnd records the outcome; an error keeps only its SQLSTATE-free message class.
func (QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if data.Err != nil {
		span.SetStatus(codes.Error, "query failed")
	}
	span.End()
}
