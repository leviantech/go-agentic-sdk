// Package builtin contains built-in SDK tools (always available without a skill).
package builtin

import (
	"context"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// TimeTool reports the server's local time. Example tool using struct methods.
type TimeTool struct{}

func (TimeTool) Name() string        { return "get_current_time" }
func (TimeTool) Description() string { return "Returns the current local time of the server." }
func (TimeTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (TimeTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}

// RegisterBuiltin registers all built-in tools in the registry.
func RegisterBuiltin(reg *tools.Registry) error {
	return reg.Register(TimeTool{})
}
