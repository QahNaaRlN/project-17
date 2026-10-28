package telemetry

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// metadataKey holds the inserting trace in the River job metadata.
const metadataKey = "traceparent"

// JobInsertMiddleware — STU-593: a job inserted inside a trace remembers it so that its
// span links back to the request.
type JobInsertMiddleware struct{ river.MiddlewareDefaults }

// InsertMany adds the traceparent to the metadata of every job of the batch.
func (*JobInsertMiddleware) InsertMany(ctx context.Context, params []*rivertype.JobInsertParams, doInner func(context.Context) ([]*rivertype.JobInsertResult, error)) ([]*rivertype.JobInsertResult, error) {
	carrier := propagation.MapCarrier{}
	Propagator.Inject(ctx, carrier)
	if tp := carrier[metadataKey]; tp != "" {
		for _, p := range params {
			meta := map[string]any{}
			if len(p.Metadata) > 0 {
				_ = json.Unmarshal(p.Metadata, &meta)
			}
			meta[metadataKey] = tp
			if b, err := json.Marshal(meta); err == nil {
				p.Metadata = b
			}
		}
	}
	return doInner(ctx)
}

// WorkerMiddleware — STU-593: a span per worked job, linked to the inserting trace.
type WorkerMiddleware struct{ river.MiddlewareDefaults }

// Work runs the job inside its span.
func (*WorkerMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	opts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindConsumer), trace.WithNewRoot(), trace.WithAttributes(
		attribute.String("messaging.system", "river"),
		attribute.String("messaging.destination.name", job.Queue),
		attribute.String("cms.job_kind", job.Kind),
		attribute.Int("cms.job_attempt", job.Attempt),
		attribute.String("messaging.message.id", strconv.FormatInt(job.ID, 10)),
	)}
	var meta map[string]any
	if json.Unmarshal(job.Metadata, &meta) == nil {
		if tp, ok := meta[metadataKey].(string); ok {
			linked := trace.SpanContextFromContext(Propagator.Extract(context.Background(), propagation.MapCarrier{metadataKey: tp}))
			if linked.IsValid() {
				opts = append(opts, trace.WithLinks(trace.Link{SpanContext: linked}))
			}
		}
	}
	ctx, span := Tracer().Start(ctx, "job "+job.Kind, opts...)
	defer span.End()
	err := doInner(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "job failed")
	}
	return err
}
