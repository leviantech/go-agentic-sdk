package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/leviantech/go-agentic-sdk/pkg/llm/mock"
)

// TestSubAgent verifies the multi-agent pattern:
// the outer agent calls the sub-agent tool, the result is used for the final answer.
func TestSubAgent(t *testing.T) {
	// sub-agent: simple calculation (no tools)
	sub, err := NewAgent(WithLLM(&mock.Stub{Answer: "42"}))
	if err != nil {
		t.Fatal(err)
	}

	// outer agent: round 1 calls the "calculate" tool, round 2 answers
	outer, err := NewAgent(
		WithLLM(&mock.Scripted{ToolCallName: "calculate", FinalAnswer: "the result is 42"}),
		WithTools(AsTool(sub, "calculate", "Calculates something")),
	)
	if err != nil {
		t.Fatal(err)
	}

	out, err := outer.Run(context.Background(), "what is 6*7?")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "42") {
		t.Fatalf("answer does not contain the sub-agent result: %s", out)
	}
}

// TestSubAgentAsTool verifies AsTool produces a valid Tool.
func TestSubAgentAsTool(t *testing.T) {
	sub, _ := NewAgent(WithLLM(&mock.Stub{Answer: "ok"}))
	tt := AsTool(sub, "name", "description")
	if tt.Name() != "name" || tt.Description() != "description" {
		t.Fatalf("tool metadata wrong")
	}
	if _, err := tt.Execute(context.Background(), map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
}
