package mcp

import (
	"context"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// toolCaller abstracts the two transports (stdio, HTTP) behind one call.
type toolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
}

// streamCaller is implemented by transports that support streaming tool
// calls (HTTP streamable transport). Chunks arrive via onChunk (nil for a
// non-streaming request); the accumulated result is still returned.
type streamCaller interface {
	StreamCall(ctx context.Context, name string, args map[string]any, onChunk func(string)) (string, error)
}

// mcpTool adapts an MCP server tool into a tools.Tool.
// The registered name may carry a server prefix; the underlying MCP
// method name is preserved for the actual call.
type mcpTool struct {
	caller toolCaller
	method string
	name   string
	desc   string
	schema map[string]any
}

func (t *mcpTool) Name() string        { return t.name }
func (t *mcpTool) Description() string { return t.desc }
func (t *mcpTool) Schema() map[string]any {
	if t.schema != nil {
		return t.schema
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *mcpTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if sc, ok := t.caller.(streamCaller); ok {
		return sc.StreamCall(ctx, t.method, args, nil)
	}
	return t.caller.CallTool(ctx, t.method, args)
}

var _ tools.Tool = (*mcpTool)(nil)
