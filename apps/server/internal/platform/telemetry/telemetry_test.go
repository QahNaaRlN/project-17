package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/qahnaarln/project-17/apps/server/internal/platform/telemetry"
)

func recorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(old) })
	return rec
}

// STU-590: without an endpoint nothing is installed; with one the providers start and stop.
func TestSetup(t *testing.T) {
	ctx := context.Background()
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	stop, err := telemetry.Setup(ctx, getenv, "test")
	if err != nil || stop(ctx) != nil {
		t.Fatal(err)
	}
	old, oldMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetTracerProvider(old); otel.SetMeterProvider(oldMeter) })
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = "http://127.0.0.1:1"
	env["CMS_DEPLOYMENT_ENV"] = "test"
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	stop, err = telemetry.Setup(ctx, getenv, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatal("tracer provider not installed")
	}
	// The collector is unreachable: shutdown reports the failed export and returns.
	short, cancel := context.WithTimeout(ctx, 200_000_000)
	defer cancel()
	_ = stop(short)
	if telemetry.TraceID(ctx) != "" {
		t.Fatal("trace id without a span")
	}
}

// STU-594: spans are named after the sqlc query and only inside a trace.
func TestQueryTracer(t *testing.T) {
	for sql, want := range map[string]string{
		"-- name: GetChangeset :one\nSELECT 1": "GetChangeset",
		"  -- name: X :exec":                   "X",
		"SELECT 1":                             "sql",
		"-- name: ":                            "sql",
	} {
		if got := telemetry.QueryName(sql); got != want {
			t.Fatal(sql, got)
		}
	}
	rec := recorder(t)
	tracer := telemetry.QueryTracer{}
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	if len(rec.Ended()) != 0 {
		t.Fatal("span outside a trace")
	}
	parent, span := telemetry.Tracer().Start(context.Background(), "parent")
	ctx = tracer.TraceQueryStart(parent, nil, pgx.TraceQueryStartData{SQL: "-- name: Q :one\nSELECT $1", Args: []any{"secret"}})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("boom")})
	span.End()
	got := rec.Ended()[0]
	if got.Name() != "sql Q" || got.Status().Description != "query failed" || got.Parent().SpanID() != span.SpanContext().SpanID() {
		t.Fatal(got.Name(), got.Status())
	}
	for _, a := range got.Attributes() {
		if strings.Contains(a.Value.Emit(), "secret") || strings.Contains(a.Value.Emit(), "SELECT") {
			t.Fatal(a)
		}
	}
}

// STU-593: an inserted job carries the trace; its span links back to it.
func TestRiverMiddleware(t *testing.T) {
	rec := recorder(t)
	parent, span := telemetry.Tracer().Start(context.Background(), "request")
	params := []*rivertype.JobInsertParams{{Metadata: []byte(`{"x":1}`)}, {}}
	_, _ = (&telemetry.JobInsertMiddleware{}).InsertMany(parent, params, func(context.Context) ([]*rivertype.JobInsertResult, error) { return nil, nil })
	span.End()
	var meta map[string]any
	if json.Unmarshal(params[0].Metadata, &meta) != nil || meta["x"] != float64(1) || !strings.Contains(meta["traceparent"].(string), span.SpanContext().TraceID().String()) {
		t.Fatal(string(params[0].Metadata))
	}
	plain := []*rivertype.JobInsertParams{{}}
	_, _ = (&telemetry.JobInsertMiddleware{}).InsertMany(context.Background(), plain, func(context.Context) ([]*rivertype.JobInsertResult, error) { return nil, nil })
	if plain[0].Metadata != nil {
		t.Fatal(string(plain[0].Metadata))
	}
	job := &rivertype.JobRow{ID: 7, Kind: "purge", Queue: "default", Attempt: 1, Metadata: params[0].Metadata}
	if err := (&telemetry.WorkerMiddleware{}).Work(context.Background(), job, func(ctx context.Context) error { return errors.New("fail") }); err == nil {
		t.Fatal("error lost")
	}
	worked := rec.Ended()[1]
	if worked.Name() != "job purge" || len(worked.Links()) != 1 || worked.Links()[0].SpanContext.TraceID() != span.SpanContext().TraceID() || worked.Status().Description != "job failed" {
		t.Fatal(worked.Name(), worked.Links())
	}
	if err := (&telemetry.WorkerMiddleware{}).Work(context.Background(), &rivertype.JobRow{Kind: "x"}, func(context.Context) error { return nil }); err != nil || len(rec.Ended()[2].Links()) != 0 {
		t.Fatal(err)
	}
}

// STU-602: records with a context carry the trace, project and actor.
func TestLogHandler(t *testing.T) {
	recorder(t)
	var out bytes.Buffer
	log := slog.New(telemetry.LogHandler{Handler: slog.NewJSONHandler(&out, nil)}).With("app", "cms").WithGroup("g")
	ctx, span := telemetry.Tracer().Start(context.Background(), "request")
	ctx = telemetry.WithScope(ctx, telemetry.LogScope{ProjectID: "p1", ActorID: "a1"})
	log.InfoContext(ctx, "hello", "k", "v")
	span.End()
	line := out.String()
	for _, want := range []string{`"traceId":"` + span.SpanContext().TraceID().String(), `"projectId":"p1"`, `"actorId":"a1"`, `"app":"cms"`} {
		if !strings.Contains(line, want) {
			t.Fatal(line, want)
		}
	}
	if telemetry.TraceID(ctx) != span.SpanContext().TraceID().String() {
		t.Fatal("trace id")
	}
	out.Reset()
	log.InfoContext(telemetry.WithScope(context.Background(), telemetry.LogScope{}), "plain")
	if strings.Contains(out.String(), "traceId") || strings.Contains(out.String(), "projectId") {
		t.Fatal(out.String())
	}
}

// STU-590, 594, 602: edge cases — a bare sqlc name, a scope filled in place, a broken
// resource configuration and the meter of the CMS.
func TestTelemetryEdges(t *testing.T) {
	if telemetry.QueryName("-- name: Bare") != "sql" {
		t.Fatal("name without a kind")
	}
	ctx := telemetry.WithScope(context.Background(), telemetry.LogScope{})
	if telemetry.WithScope(ctx, telemetry.LogScope{ProjectID: "p"}) != ctx {
		t.Fatal("scope not filled in place")
	}
	var out bytes.Buffer
	slog.New(telemetry.LogHandler{Handler: slog.NewJSONHandler(&out, nil)}).InfoContext(ctx, "x")
	if !strings.Contains(out.String(), `"projectId":"p"`) || strings.Contains(out.String(), "actorId") {
		t.Fatal(out.String())
	}
	if c, err := telemetry.Meter().Int64Counter("cms.test"); err != nil || c == nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "broken")
	env := map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:1", "OTEL_SERVICE_NAME": "svc"}
	if _, err := telemetry.Setup(context.Background(), func(k string) string { return env[k] }, "v"); err == nil {
		t.Fatal("broken resource attributes accepted")
	}
}
