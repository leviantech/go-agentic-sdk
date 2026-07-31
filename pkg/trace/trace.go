// Package trace provides a minimal, dependency-free tracing API.
// It is intentionally small so any backend (OpenTelemetry, Langfuse,
// a log sink) can be adapted by implementing Tracer. The agent emits
// spans for the whole run, each LLM step, and each tool call.
package trace

import "context"

// Span is one unit of work with a start and an end.
type Span interface {
	// End marks the span as finished. Safe to call once; further calls
	// are no-ops.
	End()
	// SetAttribute records a key/value pair on the span (before End).
	SetAttribute(key, value string)
}

// Tracer starts spans.
type Tracer interface {
	// Start begins a span named name. The returned context carries the
	// span so nested spans can be created from it.
	Start(ctx context.Context, name string) (context.Context, Span)
}

type spanCtxKey struct{}

// SpanFromContext returns the span carried by ctx, or nil.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanCtxKey{}).(Span); ok {
		return s
	}
	return nil
}

// WithSpan returns a context carrying s, so nested spans created from it
// (and SpanFromContext) can see the current span. Tracer implementations
// should call it from Start.
func WithSpan(ctx context.Context, s Span) context.Context {
	return context.WithValue(ctx, spanCtxKey{}, s)
}

func withSpan(ctx context.Context, s Span) context.Context {
	return context.WithValue(ctx, spanCtxKey{}, s)
}
