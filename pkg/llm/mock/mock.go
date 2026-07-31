// Package mock provides a fake LLM for testing without an API key.
package mock

import (
	"context"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// Scripted mimics an LLM with predefined behavior.
// Useful for deterministically testing the agent loop:
// the first round calls a tool, subsequent rounds answer with the final result.
type Scripted struct {
	ToolCallName string // tool "called" on the first round
	FinalAnswer  string // answer given after the tool has executed
	round        int
}

func (m *Scripted) Chat(_ context.Context, _ []llm.Message, _ []tools.Tool) (llm.Message, error) {
	m.round++
	if m.round == 1 && m.ToolCallName != "" {
		return llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:        "call_1",
				Name:      m.ToolCallName,
				Arguments: `{}`,
			}},
		}, nil
	}
	return llm.Message{Role: llm.RoleAssistant, Content: m.FinalAnswer}, nil
}

// Stub is an LLM that answers directly without tools (for simple tests).
type Stub struct {
	Answer string
}

func (s *Stub) Chat(_ context.Context, _ []llm.Message, _ []tools.Tool) (llm.Message, error) {
	return llm.Message{Role: llm.RoleAssistant, Content: s.Answer}, nil
}
