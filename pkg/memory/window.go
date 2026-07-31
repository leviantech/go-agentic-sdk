package memory

import (
	"sync"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
)

// Window keeps only the last N messages (sliding window).
type Window struct {
	mu  sync.Mutex
	msgs []llm.Message
	max int
}

func NewWindow(max int) *Window {
	if max <= 0 {
		max = 10
	}
	return &Window{max: max}
}

func (w *Window) Messages() []llm.Message {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]llm.Message, len(w.msgs))
	copy(out, w.msgs)
	return out
}

func (w *Window) Add(m llm.Message) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, m)
	if len(w.msgs) > w.max {
		w.msgs = w.msgs[len(w.msgs)-w.max:]
	}
}

func (w *Window) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = nil
}

var _ Memory = (*Window)(nil)
