package agent

import (
	"context"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// AsTool wraps an Agent as a tools.Tool so it can be called by another
// agent (multi-agent / sub-agent pattern). The calling agent invokes it
// with {"input": "..."} and receives the sub-agent's full answer.
func AsTool(a *Agent, name, description string) tools.Tool {
	return &tools.FuncTool{
		N: name,
		D: description,
		S: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Task/question for the sub-agent",
				},
			},
			"required": []string{"input"},
		},
		F: func(ctx context.Context, args map[string]any) (string, error) {
			in, _ := args["input"].(string)
			if in == "" {
				return `{"error": "empty input"}`, nil
			}
			return a.Run(ctx, in)
		},
	}
}
