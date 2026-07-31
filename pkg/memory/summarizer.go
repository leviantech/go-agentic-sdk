package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
)

// SummarizeFn compacts a slice of messages into a single summary string.
type SummarizeFn func(ctx context.Context, msgs []llm.Message) (string, error)

// Summarizer wraps another Memory. When the inner history exceeds max
// messages, the oldest messages are compacted into a summary (via
// SummarizeFn, or dropped when no function is set) and only the tail is kept.
// The summary is re-injected as a system message on Messages().
type Summarizer struct {
	mu        sync.Mutex
	inner     Memory
	max       int
	summary   string
	summarize SummarizeFn
}

func NewSummarizer(inner Memory, max int) *Summarizer {
	if max <= 0 {
		max = 10
	}
	return &Summarizer{inner: inner, max: max}
}

// WithSummarizeFn sets the compaction function (e.g. an LLM call).
func (s *Summarizer) WithSummarizeFn(fn SummarizeFn) *Summarizer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summarize = fn
	return s
}

func (s *Summarizer) Messages() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.inner.Messages()
	if s.summary == "" {
		return msgs
	}
	out := []llm.Message{{
		Role:    llm.RoleSystem,
		Content: fmt.Sprintf("Summary of previous conversation: %s", s.summary),
	}}
	return append(out, msgs...)
}

func (s *Summarizer) Add(m llm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.Add(m)
	if len(s.inner.Messages()) <= s.max {
		return
	}
	s.compact()
}

func (s *Summarizer) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.Clear()
	s.summary = ""
}

// compact moves old messages to the summary, keeping the last max messages.
func (s *Summarizer) compact() {
	all := s.inner.Messages()
	keep := len(all) - s.max
	if keep < 1 {
		keep = 1
	}
	old := all[:keep]
	tail := all[keep:]

	if s.summarize != nil {
		if txt, err := s.summarize(context.Background(), old); err == nil {
			s.summary = txt
		} else {
			s.summary = fmt.Sprintf("%d old messages ignored (summary failed: %v)", len(old), err)
		}
	} else {
		s.summary = fmt.Sprintf("%d old messages ignored.", len(old))
	}

	// reset inner to the tail
	s.inner.Clear()
	for _, m := range tail {
		s.inner.Add(m)
	}
}

var _ Memory = (*Summarizer)(nil)
