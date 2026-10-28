// Package telemetry wires OpenTelemetry into the CMS (docs/observability.md, STU-590…602):
// providers and the OTLP export, the SQL tracer, River middleware and log correlation.
package telemetry

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// Scope names the instrumentation library of the CMS.
const Scope = "github.com/qahnaarln/project-17/apps/server"

// Tracer and Meter of the CMS; until Setup they are no-op (the global delegates).
func Tracer() trace.Tracer { return otel.Tracer(Scope) }
func Meter() metric.Meter  { return otel.Meter(Scope) }

// Propagator — W3C trace context; baggage is not propagated.
var Propagator propagation.TextMapPropagator = propagation.TraceContext{}

// Setup installs the providers when OTEL_EXPORTER_OTLP_ENDPOINT is set (STU-590);
// otherwise the global no-op providers stay. The exporters read the standard OTEL_*
// variables (endpoint, headers, protocol http/protobuf); the sampler reads
// OTEL_TRACES_SAMPLER(_ARG), parent-based always-on by default.
func Setup(ctx context.Context, getenv func(string) string, version string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(Propagator)
	if getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	name := getenv("OTEL_SERVICE_NAME")
	if name == "" {
		name = "cms"
	}
	attrs := []resource.Option{resource.WithAttributes(semconv.ServiceName(name), semconv.ServiceVersion(version))}
	if env := getenv("CMS_DEPLOYMENT_ENV"); env != "" {
		attrs = append(attrs, resource.WithAttributes(semconv.DeploymentEnvironmentName(env)))
	}
	res, err := resource.New(ctx, append(attrs, resource.WithFromEnv())...)
	if err != nil {
		return nil, err
	}
	spans, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	metrics, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spans), sdktrace.WithResource(res))
	// OTEL_METRIC_EXPORT_INTERVAL wins; otherwise queue gauges are read every 30 s (STU-598).
	var readerOpts []sdkmetric.PeriodicReaderOption
	if getenv("OTEL_METRIC_EXPORT_INTERVAL") == "" {
		readerOpts = append(readerOpts, sdkmetric.WithInterval(30*time.Second))
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metrics, readerOpts...)))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	return func(ctx context.Context) error { return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx)) }, nil
}

// TraceID — the hex trace ID of the current span, empty without a valid one (STU-595).
func TraceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}
