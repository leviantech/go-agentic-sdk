// Package agent contains the main agentic loop and configuration via functional options.
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/leviantech/go-agentic-sdk/pkg/guardrails"
	"github.com/leviantech/go-agentic-sdk/pkg/llm"
	"github.com/leviantech/go-agentic-sdk/pkg/memory"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// AgentConfig is the final configuration result (populated from options).
type AgentConfig struct {
	LLM           llm.LLM
	Memory        memory.Memory
	Tools         []tools.Tool
	SystemPrompt  string
	Name          string
	MaxIterations int                 // default 8
	Guardrails    []guardrails.Guardrail
	Observers     []Observer
}

// Agent runs the agentic loop: think → call tool → execute → repeat.
type Agent struct {
	cfg AgentConfig
}

// NewAgent builds an agent from functional options.
func NewAgent(opts ...Option) (*Agent, error) {
	a := &Agent{cfg: AgentConfig{
		SystemPrompt:  DefaultSystemPrompt(),
		MaxIterations: 8,
		Memory:        memory.NewConversationBuffer(),
	}}
	for _, o := range opts {
		o(a)
	}
	if a.cfg.LLM == nil {
		return nil, fmt.Errorf("agent requires an LLM (use agent.WithLLM)")
	}
	return a, nil
}

// Option is a functional configurator for Agent.
type Option func(*Agent)

func WithLLM(l llm.LLM) Option          { return func(a *Agent) { a.cfg.LLM = l } }
func WithMemory(m memory.Memory) Option { return func(a *Agent) { a.cfg.Memory = m } }
func WithSystemPrompt(s string) Option  { return func(a *Agent) { a.cfg.SystemPrompt = s } }
func WithName(n string) Option          { return func(a *Agent) { a.cfg.Name = n } }

func WithMaxIterations(n int) Option {
	return func(a *Agent) {
		if n > 0 {
			a.cfg.MaxIterations = n
		}
	}
}

// WithTools adds tools to the agent.
func WithTools(ts ...tools.Tool) Option {
	return func(a *Agent) { a.cfg.Tools = append(a.cfg.Tools, ts...) }
}

// WithGuardrails adds guardrails that validate input and output.
func WithGuardrails(g ...guardrails.Guardrail) Option {
	return func(a *Agent) { a.cfg.Guardrails = append(a.cfg.Guardrails, g...) }
}

// WithObserver adds an event observer for runs (observability).
func WithObserver(obs ...Observer) Option {
	return func(a *Agent) { a.cfg.Observers = append(a.cfg.Observers, obs...) }
}

// Run executes one conversation turn from user input.
// History is saved to memory so multi-turn conversations continue.
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	return a.runOnce(ctx, userInput, a.emit)
}

// RunStream is like Run, but sends all events to a channel
// (including partial content when the LLM supports streaming).
// The channel is closed when the run finishes.
func (a *Agent) RunStream(ctx context.Context, userInput string) (<-chan Event, error) {
	// initial synchronous validation so errors return to the caller quickly
	if err := a.validateInput(ctx, userInput); err != nil {
		return nil, err
	}
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		_, _ = a.runOnce(ctx, userInput, func(e Event) { ch <- e })
	}()
	return ch, nil
}

func (a *Agent) emit(e Event) {
	for _, o := range a.cfg.Observers {
		o.Observe(e)
	}
}

func (a *Agent) validateInput(ctx context.Context, input string) error {
	for _, g := range a.cfg.Guardrails {
		if err := g.ValidateInput(ctx, input); err != nil {
			return fmt.Errorf("input blocked: %w", err)
		}
	}
	return nil
}

func (a *Agent) validateOutput(ctx context.Context, output string) error {
	for _, g := range a.cfg.Guardrails {
		if err := g.ValidateOutput(ctx, output); err != nil {
			return fmt.Errorf("output blocked: %w", err)
		}
	}
	return nil
}

func (a *Agent) runOnce(ctx context.Context, userInput string, emit func(Event)) (string, error) {
	if err := a.validateInput(ctx, userInput); err != nil {
		emit(Event{Type: EventRunError, Err: err})
		return "", err
	}

	messages := []llm.Message{{Role: llm.RoleSystem, Content: a.cfg.SystemPrompt}}
	messages = append(messages, a.cfg.Memory.Messages()...)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userInput})
	a.cfg.Memory.Add(llm.Message{Role: llm.RoleUser, Content: userInput})
	emit(Event{Type: EventRunStart})

	byName := make(map[string]tools.Tool, len(a.cfg.Tools))
	for _, t := range a.cfg.Tools {
		byName[t.Name()] = t
	}

	for i := 0; i < a.cfg.MaxIterations; i++ {
		resp, err := a.chatStep(ctx, messages, emit, i)
		if err != nil {
			emit(Event{Type: EventRunError, Iteration: i, Err: err})
			return "", fmt.Errorf("iteration %d: %w", i+1, err)
		}
		messages = append(messages, resp)

		if len(resp.ToolCalls) == 0 {
			if err := a.validateOutput(ctx, resp.Content); err != nil {
				emit(Event{Type: EventRunError, Iteration: i, Err: err})
				return "", err
			}
			a.cfg.Memory.Add(resp)
			emit(Event{Type: EventFinal, Iteration: i, Message: resp})
			return resp.Content, nil
		}

		for _, tc := range resp.ToolCalls {
			emit(Event{Type: EventToolCall, Iteration: i, ToolCall: tc})
			t, ok := byName[tc.Name]
			if !ok {
				res := fmt.Sprintf(`{"error": "tool %q is not registered"}`, tc.Name)
				messages = append(messages, llm.ToolResultMessage(tc.ID, res))
				emit(Event{Type: EventToolResult, Iteration: i, ToolCall: tc, Result: res})
				continue
			}
			var args map[string]any
			if err := unmarshalArgs(tc.Arguments, &args); err != nil {
				res := fmt.Sprintf(`{"error": "invalid arguments: %v"}`, err)
				messages = append(messages, llm.ToolResultMessage(tc.ID, res))
				emit(Event{Type: EventToolResult, Iteration: i, ToolCall: tc, Result: res})
				continue
			}
			res, err := t.Execute(ctx, args)
			if err != nil {
				res = fmt.Sprintf(`{"error": "%s"}`, err)
			}
			messages = append(messages, llm.ToolResultMessage(tc.ID, res))
			emit(Event{Type: EventToolResult, Iteration: i, ToolCall: tc, Result: res})
		}
	}
	err := fmt.Errorf("agent %q reached the %d iteration limit without a final answer",
		a.cfg.Name, a.cfg.MaxIterations)
	emit(Event{Type: EventRunError, Err: err})
	return "", err
}

// chatStep calls the LLM, using streaming when the provider supports it.
func (a *Agent) chatStep(ctx context.Context, messages []llm.Message, emit func(Event), iter int) (llm.Message, error) {
	if sl, ok := a.cfg.LLM.(llm.StreamLLM); ok {
		chunks, err := sl.ChatStream(ctx, messages, a.cfg.Tools)
		if err != nil {
			return llm.Message{}, err
		}
		var content strings.Builder
		var calls []llm.ToolCall
		for ch := range chunks {
			if ch.Err != nil {
				return llm.Message{}, ch.Err
			}
			if ch.Content != "" {
				content.WriteString(ch.Content)
				emit(Event{Type: EventLLMStep, Iteration: iter,
					Message: llm.Message{Role: llm.RoleAssistant, Content: content.String()}})
			}
			if ch.ToolCall != nil {
				calls = append(calls, *ch.ToolCall)
			}
		}
		return llm.Message{Role: llm.RoleAssistant, Content: content.String(), ToolCalls: calls}, nil
	}

	resp, err := a.cfg.LLM.Chat(ctx, messages, a.cfg.Tools)
	if err != nil {
		return llm.Message{}, err
	}
	emit(Event{Type: EventLLMStep, Iteration: iter, Message: resp})
	return resp, nil
}

// DefaultSystemPrompt returns the built-in default prompt.
func DefaultSystemPrompt() string {
	return "You are a helpful agent. Use available tools when needed to answer."
}
