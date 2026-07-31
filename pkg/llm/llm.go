package llm

import (
	"context"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// LLM is an abstraction over the language model backend.
// Implementations: pkg/llm/openai, pkg/llm/anthropic, pkg/llm/mock, etc.
type LLM interface {
	Chat(ctx context.Context, messages []Message, tools []tools.Tool) (Message, error)
}
