package llm

import (
	"context"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// StreamChunk is a chunk of LLM streaming output.
type StreamChunk struct {
	Content  string    // accumulated content so far (partial)
	ToolCall *ToolCall // complete tool call (when sent)
	Err      error
}

// StreamLLM is an optional interface for LLMs that support streaming.
// Providers that do not implement it still work
// (the agent will use the plain Chat).
type StreamLLM interface {
	ChatStream(ctx context.Context, messages []Message, tools []tools.Tool) (<-chan StreamChunk, error)
}
