package agent

import (
	"github.com/leviantech/go-agentic-sdk/pkg/llm"
)

// EventType identifies the type of event in a single agent run.
type EventType string

const (
	// EventRunStart is emitted when a run starts.
	EventRunStart EventType = "run_start"
	// EventLLMStep is emitted each time the LLM responds (partial content while streaming).
	EventLLMStep EventType = "llm_step"
	// EventToolCall is emitted when a tool is called.
	EventToolCall EventType = "tool_call"
	// EventToolResult is emitted after a tool finishes.
	EventToolResult EventType = "tool_result"
	// EventFinal is emitted when the final answer is received.
	EventFinal EventType = "final"
	// EventRunError is emitted when a run fails.
	EventRunError EventType = "run_error"
)

// Event is a single observability point in a run.
type Event struct {
	Type      EventType
	Iteration int
	Message   llm.Message // EventLLMStep / EventFinal
	ToolCall  llm.ToolCall
	Result    string // EventToolResult
	Err       error  // EventRunError
}

// Observer receives all events of a run.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a plain function into an Observer.
type ObserverFunc func(Event)

func (f ObserverFunc) Observe(e Event) { f(e) }
