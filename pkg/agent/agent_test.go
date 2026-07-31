package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
	"github.com/leviantech/go-agentic-sdk/pkg/llm/mock"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// TestToolErrorValidJSON: error text with quotes/newlines must still be
// delivered as well-formed JSON to the LLM (no broken tool results).
func TestToolErrorValidJSON(t *testing.T) {
	res := toolError(`boom "quoted" and
newline`)
	var parsed map[string]string
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		t.Fatalf("toolError output is not valid JSON: %s (%v)", res, err)
	}
	if parsed["error"] == "" {
		t.Fatalf("error message lost: %+v", parsed)
	}
}

// TestLoopEksekusiTool verifies the agentic loop:
// round 1 the LLM requests a tool, the tool executes, the result is sent back,
// round 2 the LLM gives the final answer.
func TestLoopEksekusiTool(t *testing.T) {
	var toolExecuted bool
	reg := tools.NewRegistry()
	reg.Register(&tools.FuncTool{
		N: "ping",
		D: "test tool",
		F: func(_ context.Context, _ map[string]any) (string, error) {
			toolExecuted = true
			return `{"pong":true}`, nil
		},
	})

	a, err := NewAgent(
		WithLLM(&mock.Scripted{ToolCallName: "ping", FinalAnswer: "pong received"}),
		WithTools(reg.List()...),
		WithMaxIterations(5),
	)
	if err != nil {
		t.Fatal(err)
	}

	out, err := a.Run(context.Background(), "try tool")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !toolExecuted {
		t.Fatal("tool was never executed")
	}
	if out != "pong received" {
		t.Fatalf("final answer wrong: %s", out)
	}
}

// TestLoopToolGagal ensures a tool error does not kill the loop.
func TestLoopToolGagal(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FuncTool{
		N: "rusak",
		D: "always errors",
		F: func(_ context.Context, _ map[string]any) (string, error) {
			return "", errTool
		},
	})

	a, _ := NewAgent(
		WithLLM(&mock.Scripted{ToolCallName: "rusak", FinalAnswer: "done"}),
		WithTools(reg.List()...),
	)
	if _, err := a.Run(context.Background(), "x"); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestMaxIterations verifies termination when the limit is reached.
func TestMaxIterations(t *testing.T) {
	a, _ := NewAgent(
		WithLLM(&loopForever{call: "ping"}),
		WithTools(registerPing(t)...),
		WithMaxIterations(3),
	)
	if _, err := a.Run(context.Background(), "x"); err == nil {
		t.Fatal("must error when the iteration limit is reached")
	}
}

// TestMemoriMultiTurn verifies history is preserved between turns.
func TestMemoriMultiTurn(t *testing.T) {
	a, _ := NewAgent(
		WithLLM(&mock.Stub{Answer: "ok"}),
	)
	if _, err := a.Run(context.Background(), "message one"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "message two"); err != nil {
		t.Fatal(err)
	}
	msgs := a.cfg.Memory.Messages()
	if len(msgs) != 4 { // user, assistant, user, assistant
		t.Fatalf("history wrong: %d messages", len(msgs))
	}
	if msgs[2].Content != "message two" {
		t.Fatalf("second message not stored: %s", msgs[2].Content)
	}
}

// --- helpers ---

var errTool = errToolType{}

type errToolType struct{}

func (errToolType) Error() string { return "tool broken" }

func registerPing(t *testing.T) []tools.Tool {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(&tools.FuncTool{
		N: "ping",
		D: "test tool",
		F: func(_ context.Context, _ map[string]any) (string, error) {
			return "pong", nil
		},
	})
	return reg.List()
}

// loopForever always calls a tool without ever answering.
type loopForever struct {
	call string
}

func (l *loopForever) Chat(_ context.Context, _ []llm.Message, _ []tools.Tool) (llm.Message, error) {
	return llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:        "call_loop",
			Name:      l.call,
			Arguments: `{}`,
		}},
	}, nil
}
