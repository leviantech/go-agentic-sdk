package trace

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// NoopTracer discards all spans (default when no tracer is configured).
type NoopTracer struct{}

func (NoopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, NoopSpan{}
}

// NoopSpan does nothing.
type NoopSpan struct{}

func (NoopSpan) End()                     {}
func (NoopSpan) SetAttribute(_, _ string) {}

var _ Tracer = NoopTracer{}
var _ Span = NoopSpan{}

// ConsoleTracer emits one JSON object per line to w:
//
//	{"name":"agent.run","ts":...,"start":true}
//	{"name":"agent.run","ts":...,"end":true,"attrs":{...}}
//
// Useful for local debugging and as a building block for real backends.
type ConsoleTracer struct {
	w   io.Writer
	mu  sync.Mutex
	now func() time.Time
}

func NewConsole(w io.Writer) *ConsoleTracer {
	return &ConsoleTracer{w: w, now: time.Now}
}

func (t *ConsoleTracer) Start(ctx context.Context, name string) (context.Context, Span) {
	s := &consoleSpan{t: t, name: name, start: t.now()}
	t.emit(map[string]any{"name": name, "ts": s.start, "start": true})
	return withSpan(ctx, s), s
}

func (t *ConsoleTracer) emit(ev map[string]any) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.w.Write(append(data, '\n'))
}

var _ Tracer = (*ConsoleTracer)(nil)

type consoleSpan struct {
	t     *ConsoleTracer
	name  string
	start time.Time
	attrs map[string]string
	done  bool
}

func (s *consoleSpan) SetAttribute(k, v string) {
	if s.done {
		return
	}
	if s.attrs == nil {
		s.attrs = map[string]string{}
	}
	s.attrs[k] = v
}

func (s *consoleSpan) End() {
	if s.done {
		return
	}
	s.done = true
	now := s.t.now()
	ev := map[string]any{"name": s.name, "ts": now, "end": true, "duration_ms": now.Sub(s.start).Milliseconds()}
	if len(s.attrs) > 0 {
		ev["attrs"] = s.attrs
	}
	s.t.emit(ev)
}

var _ Span = (*consoleSpan)(nil)
