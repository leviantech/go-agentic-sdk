package agent

import (
	"context"
	"testing"

	"github.com/leviantech/go-agentic-sdk/pkg/guardrails"
	"github.com/leviantech/go-agentic-sdk/pkg/llm/mock"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// TestRunStreamEvents verifies streaming: tool call in round 1,
// final answer in the next round, all events emitted.
func TestRunStreamEvents(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FuncTool{
		N: "ping",
		D: "test tool",
		F: func(_ context.Context, _ map[string]any) (string, error) {
			return `{"pong":true}`, nil
		},
	})
	a, _ := NewAgent(
		WithLLM(&mock.Stream{ToolCallName: "ping", FinalAnswer: "pong received"}),
		WithTools(reg.List()...),
	)

	ch, err := a.RunStream(context.Background(), "try")
	if err != nil {
		t.Fatalf("runstream: %v", err)
	}
	var gotToolCall, gotFinal bool
	var finalText string
	for e := range ch {
		switch e.Type {
		case EventToolCall:
			gotToolCall = e.ToolCall.Name == "ping"
		case EventFinal:
			gotFinal = true
			finalText = e.Message.Content
		case EventRunError:
			t.Fatalf("run error: %v", e.Err)
		}
	}
	if !gotToolCall {
		t.Fatal("tool_call event not emitted")
	}
	if !gotFinal || finalText != "pong received" {
		t.Fatalf("final wrong: got=%v text=%q", gotFinal, finalText)
	}
}

// TestObserverMenerimaEvent verifies the observer receives the event sequence.
func TestObserverMenerimaEvent(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FuncTool{
		N: "ping",
		D: "test tool",
		F: func(_ context.Context, _ map[string]any) (string, error) {
			return "pong", nil
		},
	})
	var order []EventType
	a, _ := NewAgent(
		WithLLM(&mock.Scripted{ToolCallName: "ping", FinalAnswer: "done"}),
		WithTools(reg.List()...),
		WithObserver(ObserverFunc(func(e Event) { order = append(order, e.Type) })),
	)
	if _, err := a.Run(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	want := []EventType{EventRunStart, EventLLMStep, EventToolCall, EventToolResult, EventLLMStep, EventFinal}
	if len(order) != len(want) {
		t.Fatalf("event count = %d (want %d): %v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("event[%d] = %s (want %s): %v", i, order[i], want[i], order)
		}
	}
}

// TestGuardrailInputBlock verifies forbidden input is blocked.
func TestGuardrailInputBlock(t *testing.T) {
	a, _ := NewAgent(
		WithLLM(&mock.Stub{Answer: "ok"}),
		WithGuardrails(guardrails.NewContentFilter("secret")),
	)
	if _, err := a.Run(context.Background(), "reveal the secret"); err == nil {
		t.Fatal("forbidden input must be blocked")
	}
}

// TestGuardrailOutputBlock verifies forbidden output is blocked.
func TestGuardrailOutputBlock(t *testing.T) {
	a, _ := NewAgent(
		WithLLM(&mock.Stub{Answer: "token secret = abc"}),
		WithGuardrails(guardrails.NewContentFilter("secret")),
	)
	if _, err := a.Run(context.Background(), "x"); err == nil {
		t.Fatal("forbidden output must be blocked")
	}
}

// TestRateLimit verifies the rate limiter rejects excess requests.
func TestRateLimit(t *testing.T) {
	rl := guardrails.NewRateLimiter(1, 2) // 1 token/second, burst 2
	ok := 0
	for i := 0; i < 5; i++ {
		if rl.Allow() {
			ok++
		}
	}
	if ok != 2 {
		t.Fatalf("burst 2, but %d accepted", ok)
	}
}
