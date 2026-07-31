// Package otel adapts OpenTelemetry into the minimal trace.Tracer
// interface. It lives in its own package so the core trace package stays
// dependency-free: only users who opt in pull in the OTel SDK.
package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/leviantech/go-agentic-sdk/pkg/trace"
)

// Tracer adapts an OpenTelemetry TracerProvider into trace.Tracer so all
// agent.run / llm.step / tool.call spans appear in whatever OTel exporter
// is configured (OTLP, Jaeger, Zipkin, ...).
//
// Usage:
//
//	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
//	defer tp.Shutdown(ctx)
//	tr := otel.NewTracer(tp, "my-agent")
//	a, _ := agent.NewAgent(agent.WithLLM(...), agent.WithTracer(tr))
type Tracer struct {
	tr oteltrace.Tracer
}

// NewTracer wraps a TracerProvider. name is the OTel instrumentation
// library name (e.g. "go-agentic-sdk").
func NewTracer(tp *sdktrace.TracerProvider, name string) *Tracer {
	return &Tracer{tr: tp.Tracer(name)}
}

func (o *Tracer) Start(ctx context.Context, name string) (context.Context, trace.Span) {
	// OTel stores its own span in ctx (used for parent-child linking on
	// the next o.tr.Start); the wrapper is stored under the trace key so
	// trace.SpanFromContext keeps working inside custom tools.
	ctx, span := o.tr.Start(ctx, name)
	s := &spanAdapter{span: span}
	return trace.WithSpan(ctx, s), s
}

var _ trace.Tracer = (*Tracer)(nil)

// spanAdapter wraps an OTel span behind the minimal trace.Span interface.
type spanAdapter struct {
	span oteltrace.Span
}

func (s *spanAdapter) End() { s.span.End() }

func (s *spanAdapter) SetAttribute(key, value string) {
	s.span.SetAttributes(attribute.String(key, value))
}

var _ trace.Span = (*spanAdapter)(nil)
