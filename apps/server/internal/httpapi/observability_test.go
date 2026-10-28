package httpapi_test

import (
	"context"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/ratelimit"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var (
	observedOnce sync.Once
	spans        *tracetest.SpanRecorder
	metricReader *sdkmetric.ManualReader
)

// observed installs in-memory providers once per test binary: instruments created at
// package init bind to the first meter provider.
func observed() (*tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	observedOnce.Do(func() {
		spans = tracetest.NewSpanRecorder()
		metricReader = sdkmetric.NewManualReader()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader)))
	})
	return spans, metricReader
}

// points returns the data points of a metric as attribute sets with values.
func points(t *testing.T, name string) map[string]float64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	key := func(s attribute.Set) string {
		var parts []string
		for _, kv := range s.ToSlice() {
			parts = append(parts, string(kv.Key)+"="+kv.Value.Emit())
		}
		return strings.Join(parts, ",")
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range data.DataPoints {
					out[key(p.Attributes)] += float64(p.Value)
				}
			case metricdata.Histogram[float64]:
				for _, p := range data.DataPoints {
					out[key(p.Attributes)] += float64(p.Count)
				}
			case metricdata.Gauge[int64]:
				for _, p := range data.DataPoints {
					out[key(p.Attributes)] = float64(p.Value)
				}
			}
		}
	}
	return out
}

func has(m map[string]float64, parts ...string) bool {
	for k := range m {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(k, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// STU-591, 592, 594, 595, 596, 597: an API request is one trace — HTTP, command, SQL —
// continuing an authenticated caller's traceparent; errors carry the trace ID.
// OPS-020: traces for requests, commands, jobs and SQL; traceId in errors; OPS-021: metrics with bounded labels.
func TestTracesAndMetrics(t *testing.T) {
	rec, _ := observed()
	e := setup(t)
	const trace = "4bf92f3577b34da6a3ce929d0e0e4736"
	headers := e.authed(map[string]string{"Idempotency-Key": "trace-1", "traceparent": "00-" + trace + "-00f067aa0ba902b7-01"})
	resp, body := e.do(t, "POST", "/api/v1/commands/create-changeset", `{"payload":{"title":"traced"}}`, headers)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = e.do(t, "POST", "/api/v1/commands/apply-operations", `{"payload":{"changesetId":"bad"}}`,
		e.authed(map[string]string{"Idempotency-Key": "trace-2", "traceparent": "00-" + trace + "-00f067aa0ba902b7-01"}))
	if resp.StatusCode != 400 || body["traceId"] != trace {
		t.Fatal(resp.StatusCode, body)
	}
	// STU-602: the request summary line carries the trace, project and actor.
	var line string
	for _, l := range strings.Split(e.logs.String(), "\n") {
		if strings.Contains(l, `"msg":"http request"`) && strings.Contains(l, trace) {
			line = l
		}
	}
	for _, want := range []string{`"route":"/api/v1/commands/{name}"`, `"projectId":"`, `"actorId":"`, `"spanId":"`} {
		if !strings.Contains(line, want) {
			t.Fatal(want, line)
		}
	}
	names := map[string]bool{}
	for _, s := range rec.Ended() {
		if s.SpanContext().TraceID().String() == trace {
			names[s.Name()] = true
		}
	}
	for _, want := range []string{"POST /api/v1/commands/{name}", "command create-changeset", "command apply-operations"} {
		if !names[want] {
			t.Fatal(want, names)
		}
	}
	sql := false
	for n := range names {
		sql = sql || strings.HasPrefix(n, "sql ")
	}
	if !sql {
		t.Fatal("no SQL spans", names)
	}
	// An unauthenticated caller cannot join a trace.
	resp, body = e.do(t, "GET", "/api/v1/changesets", "", map[string]string{"traceparent": "00-" + trace + "-00f067aa0ba902b7-01"})
	if resp.StatusCode != 401 || body["traceId"] == trace || len(body["traceId"].(string)) != 32 {
		t.Fatal(body)
	}
	if !has(points(t, "http.server.request.duration"), "http.route=/api/v1/commands/{name}") {
		t.Fatal(points(t, "http.server.request.duration"))
	}
	commands := points(t, "cms.command.duration")
	if !has(commands, "cms.command=create-changeset", "cms.outcome=ok") || !has(commands, "cms.command=apply-operations", "cms.outcome=PAYLOAD_INVALID") {
		t.Fatal(commands)
	}
	e.do(t, "POST", "/api/v1/commands/no-such-command", `{"payload":{}}`, e.authed(map[string]string{"Idempotency-Key": "trace-3"}))
	if !has(points(t, "cms.command.duration"), "cms.command=unknown") {
		t.Fatal("unknown command label")
	}
}

// STU-599, 603, 604: runtime diagnostics from the application server only grow a counter;
// Delivery outcomes are counted per kind.
func TestTelemetryIntakeAndCacheMetric(t *testing.T) {
	observed()
	e := setup(t)
	key := e.command(t, "create-delivery-key", `{"environment":"staging","name":"telemetry"}`)["key"].(string)
	auth := map[string]string{"Authorization": "Bearer " + key, "Content-Type": "application/json"}
	path := "/delivery/v1/store/staging/telemetry"
	resp, body := e.do(t, "POST", path, `{"diagnostics":[{"code":"CMS_SOURCE_TIMEOUT","count":3,"source":"source"},{"code":"CMS_MADE_UP","count":1}]}`, auth)
	if resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode, body)
	}
	got := points(t, "cms.runtime.diagnostics")
	if !has(got, "cms.diagnostic.code=CMS_SOURCE_TIMEOUT", "cms.diagnostic.source=source", "deployment.environment=staging") ||
		!has(got, "cms.diagnostic.code=OTHER", "cms.diagnostic.source=render") {
		t.Fatal(got)
	}
	for _, bad := range []string{
		`{"diagnostics":[]}`,
		`{"diagnostics":[{"code":"lower","count":1}]}`,
		`{"diagnostics":[{"code":"X","count":0}]}`,
		`{"diagnostics":[{"code":"X","count":1,"source":"browser"}]}`,
		`not json`,
		`{"diagnostics":[` + strings.Repeat(`{"code":"X","count":1},`, 50) + `{"code":"X","count":1}]}`,
	} {
		resp, body = e.do(t, "POST", path, bad, auth)
		expectProblem(t, resp, body, 400, "PAYLOAD_INVALID")
	}
	resp, body = e.do(t, "POST", path, `{"diagnostics":[{"code":"X","count":1}]}`, map[string]string{"Content-Type": "application/json"})
	expectProblem(t, resp, body, 401, "UNAUTHENTICATED")
	token := e.command(t, "create-preview-token", `{"environment":"staging"}`)["token"].(string)
	resp, body = e.do(t, "POST", path, `{"diagnostics":[{"code":"X","count":1}]}`, map[string]string{"Authorization": "Preview " + token})
	expectProblem(t, resp, body, 403, "FORBIDDEN")

	e.do(t, "GET", "/delivery/v1/store/staging/routes", "", auth)
	if !has(points(t, "cms.delivery.cache.requests"), "cms.delivery.kind=routes", "cms.cache.result=bypass") {
		t.Fatal(points(t, "cms.delivery.cache.requests"))
	}
}

// STU-604: telemetry is limited per delivery key; unmatched routes are labelled as such.
func TestTelemetryLimitAndUnmatchedRoute(t *testing.T) {
	observed()
	e := setup(t, func(d *httpapi.Deps) { d.Limiter = ratelimit.New(nil) })
	key := e.command(t, "create-delivery-key", `{"environment":"staging","name":"limit"}`)["key"].(string)
	auth := map[string]string{"Authorization": "Bearer " + key, "Content-Type": "application/json"}
	limited := false
	for range 15 {
		resp, _ := e.do(t, "POST", "/delivery/v1/store/staging/telemetry", `{"diagnostics":[{"code":"CMS_NODE_UNKNOWN","count":1}]}`, auth)
		limited = limited || resp.StatusCode == 429
	}
	if !limited {
		t.Fatal("no rate limit")
	}
	e.do(t, "GET", "/nowhere", "", nil)
	if !has(points(t, "http.server.request.duration"), "http.route=unmatched") {
		t.Fatal(points(t, "http.server.request.duration"))
	}
}
