package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceHandler wraps a slog.Handler and stamps trace_id/span_id from the active
// span onto every record. This makes Loki log lines correlate to Tempo traces:
// a Loki derived field turns the trace_id into a clickable jump to the trace.
// No-op when the context carries no valid span (e.g. tracing disabled), so it is
// safe to install unconditionally.
type TraceHandler struct{ slog.Handler }

// NewTraceHandler wraps h so emitted records carry the current span's IDs.
func NewTraceHandler(h slog.Handler) slog.Handler { return TraceHandler{h} }

// Handle adds trace_id/span_id (when a valid span is in ctx) then delegates.
func (h TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs/WithGroup rewrap so the trace stamping survives logger.With(...) —
// embedding alone would return the bare inner handler and lose it.
func (h TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return TraceHandler{h.Handler.WithAttrs(attrs)}
}

func (h TraceHandler) WithGroup(name string) slog.Handler {
	return TraceHandler{h.Handler.WithGroup(name)}
}
