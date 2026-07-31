package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
)

func TestWindowTrim(t *testing.T) {
	w := NewWindow(3)
	for i := 0; i < 6; i++ {
		w.Add(llm.Message{Role: llm.RoleUser, Content: "m"})
	}
	msgs := w.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages remaining, got %d", len(msgs))
	}
}

func TestSummarizerDrop(t *testing.T) {
	s := NewSummarizer(NewConversationBuffer(), 4)
	for i := 0; i < 8; i++ {
		s.Add(llm.Message{Role: llm.RoleUser, Content: "m"})
	}
	msgs := s.Messages()
	// summary is inserted as a system message
	if len(msgs) != 5 { // 1 summary + 4 messages
		t.Fatalf("expected 5 messages (1 summary + 4), got %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleSystem || !strings.Contains(msgs[0].Content, "Summary") {
		t.Fatalf("summary missing: %+v", msgs[0])
	}
}

func TestSummarizerCustomFn(t *testing.T) {
	inner := NewConversationBuffer()
	s := NewSummarizer(inner, 3).WithSummarizeFn(func(_ context.Context, msgs []llm.Message) (string, error) {
		return "SUMMARY-" + msgs[0].Content, nil
	})
	s.Add(llm.Message{Role: llm.RoleUser, Content: "a"})
	s.Add(llm.Message{Role: llm.RoleUser, Content: "b"})
	s.Add(llm.Message{Role: llm.RoleUser, Content: "c"})
	s.Add(llm.Message{Role: llm.RoleUser, Content: "d"}) // triggers compact
	msgs := s.Messages()
	if !strings.Contains(msgs[0].Content, "SUMMARY-a") {
		t.Fatalf("custom summarize not used: %s", msgs[0].Content)
	}
}
