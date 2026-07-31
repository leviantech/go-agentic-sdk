package mock

import (
	"context"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// Stream is a streaming LLM stub for deterministic tests.
// Behavior mirrors Scripted: round 1 emits a tool call (if ToolCallName set),
// later rounds emit the final answer as content chunks.
type Stream struct {
	ToolCallName string
	FinalAnswer  string
	round        int
}

// Chat implements llm.LLM (non-streaming fallback).
func (s *Stream) Chat(_ context.Context, _ []llm.Message, _ []tools.Tool) (llm.Message, error) {
	s.round++
	if s.round == 1 && s.ToolCallName != "" {
		return llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:        "call_1",
				Name:      s.ToolCallName,
				Arguments: `{}`,
			}},
		}, nil
	}
	return llm.Message{Role: llm.RoleAssistant, Content: s.FinalAnswer}, nil
}

func (s *Stream) ChatStream(_ context.Context, _ []llm.Message, _ []tools.Tool) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 8)
	go func() {
		defer close(ch)
		s.round++
		if s.round == 1 && s.ToolCallName != "" {
			ch <- llm.StreamChunk{ToolCall: &llm.ToolCall{
				ID:        "call_1",
				Name:      s.ToolCallName,
				Arguments: `{}`,
			}}
			return
		}
		for _, r := range s.FinalAnswer {
			ch <- llm.StreamChunk{Content: string(r)}
		}
	}()
	return ch, nil
}

var _ llm.StreamLLM = (*Stream)(nil)
