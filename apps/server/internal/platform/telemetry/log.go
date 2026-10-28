package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type scopeKey struct{}

// LogScope — who a request acts for; log records carry it (STU-602).
type LogScope struct {
	ProjectID string
	ActorID   string
}

// WithScope remembers the project and actor of the request. A scope already in the
// context is filled in place, so records logged by outer middleware see it too.
func WithScope(ctx context.Context, s LogScope) context.Context {
	if held, ok := ctx.Value(scopeKey{}).(*LogScope); ok {
		*held = s
		return ctx
	}
	return context.WithValue(ctx, scopeKey{}, &s)
}

// LogHandler adds traceId, spanId, projectId and actorId to records logged with a
// context (STU-602); the wrapped handler formats them.
type LogHandler struct{ slog.Handler }

// Handle enriches the record and passes it on.
func (h LogHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("traceId", sc.TraceID().String()), slog.String("spanId", sc.SpanID().String()))
	}
	if s, ok := ctx.Value(scopeKey{}).(*LogScope); ok {
		if s.ProjectID != "" {
			r.AddAttrs(slog.String("projectId", s.ProjectID))
		}
		if s.ActorID != "" {
			r.AddAttrs(slog.String("actorId", s.ActorID))
		}
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs keeps the enrichment on derived handlers.
func (h LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return LogHandler{h.Handler.WithAttrs(attrs)}
}

// WithGroup keeps the enrichment on derived handlers.
func (h LogHandler) WithGroup(name string) slog.Handler { return LogHandler{h.Handler.WithGroup(name)} }
