package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/leviantech/go-agentic-sdk/pkg/llm/mock"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
	"github.com/leviantech/go-agentic-sdk/pkg/trace"
)

// fakeTracer records started and ended span names.
type fakeTracer struct {
	mu      sync.Mutex
	started []string
	ended   []string
	attrs   map[string]map[string]string // span name → attrs
}

func (f *fakeTracer) Start(_ context.Context, name string) (context.Context, trace.Span) {
	f.mu.Lock()
	f.started = append(f.started, name)
	f.mu.Unlock()
	return context.Background(), &fakeSpan{t: f, name: name}
}

type fakeSpan struct {
	t    *fakeTracer
	name string
	at   map[string]string
}

func (s *fakeSpan) SetAttribute(k, v string) {
	if s.at == nil {
		s.at = map[string]string{}
	}
	s.at[k] = v
}

func (s *fakeSpan) End() {
	s.t.mu.Lock()
	s.t.ended = append(s.t.ended, s.name)
	if s.at != nil {
		if s.t.attrs == nil {
			s.t.attrs = map[string]map[string]string{}
		}
		s.t.attrs[s.name] = s.at
	}
	s.t.mu.Unlock()
}

func TestTracerSpans(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FuncTool{
		N: "ping",
		D: "tool tes",
		F: func(_ context.Context, _ map[string]any) (string, error) {
			return "pong", nil
		},
	})
	tr := &fakeTracer{}
	a, _ := NewAgent(
		WithLLM(&mock.Scripted{ToolCallName: "ping", FinalAnswer: "done"}),
		WithTools(reg.List()...),
		WithTracer(tr),
	)
	if _, err := a.Run(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}

	want := map[string]int{"agent.run": 1, "llm.step": 2, "tool.call": 1}
	got := map[string]int{}
	for _, n := range tr.started {
		got[n]++
	}
	for n, c := range want {
		if got[n] != c {
			t.Fatalf("span %q started %d times, want %d", n, got[n], c)
		}
	}
	// all spans must end
	if len(tr.ended) != len(tr.started) {
		t.Fatalf("ended %d, started %d", len(tr.ended), len(tr.started))
	}
	// tool span carries the tool name attribute
	if attrs := tr.attrs["tool.call"]; attrs["tool"] != "ping" {
		t.Fatalf("tool span attrs wrong: %+v", tr.attrs["tool.call"])
	}
}
