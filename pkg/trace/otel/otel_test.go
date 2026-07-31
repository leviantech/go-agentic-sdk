package otel

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	sdktraceTest "go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/leviantech/go-agentic-sdk/pkg/trace"
)

func TestOTelTracerEndToEnd(t *testing.T) {
	exporter := sdktraceTest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)

	tr := NewTracer(tp, "test-agent")

	// Root span.
	ctx, sp := tr.Start(context.Background(), "agent.run")
	sp.SetAttribute("iterations", "3")
	sp.End()

	// Nested span.
	_, nested := tr.Start(ctx, "llm.step")
	nested.SetAttribute("model", "gpt-4o-mini")
	nested.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	// Spans must be in order (simple processor).
	if spans[0].Name != "agent.run" || spans[1].Name != "llm.step" {
		t.Fatalf("wrong span names: %q %q", spans[0].Name, spans[1].Name)
	}
	// agent.run attributes.
	if len(spans[0].Attributes) == 0 {
		t.Fatal("agent.run has no attributes")
	}
	for _, a := range spans[0].Attributes {
		if string(a.Key) == "iterations" && a.Value.AsString() != "3" {
			t.Fatalf("wrong iterations: %s", a.Value.AsString())
		}
	}
	if spans[0].SpanContext.SpanID() == spans[1].SpanContext.SpanID() {
		t.Fatal("nested span must differ from parent")
	}
}

func TestOTelTracerImplementsInterfaces(t *testing.T) {
	var _ trace.Tracer = (*Tracer)(nil)
	var _ trace.Span = (*spanAdapter)(nil)
}

func TestOTelTracerSpanFromContext(t *testing.T) {
	// The OTel adapter stores a wrapped span via the trace package, so
	// trace.SpanFromContext returns a usable trace.Span (not nil).
	exporter := sdktraceTest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	tr := NewTracer(tp, "test")

	ctx, sp := tr.Start(context.Background(), "run")
	defer sp.End()

	got := trace.SpanFromContext(ctx)
	if got == nil {
		t.Fatal("SpanFromContext returned nil; adapter must store wrapped span")
	}
	// Must be usable without panic.
	got.SetAttribute("key", "val")
	got.End() // second call is a no-op via OTel (safe).
}
