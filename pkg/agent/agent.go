// Package agent contains the main agentic loop and configuration via functional options.
package agent

import (
	"context"
	"fmt"

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
	MaxIterations int // default 8
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

// Run executes one conversation turn from user input.
// History is stored in memory so multi-turn conversation continues.
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	messages := []llm.Message{{Role: llm.RoleSystem, Content: a.cfg.SystemPrompt}}
	messages = append(messages, a.cfg.Memory.Messages()...)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userInput})
	a.cfg.Memory.Add(llm.Message{Role: llm.RoleUser, Content: userInput})

	byName := make(map[string]tools.Tool, len(a.cfg.Tools))
	for _, t := range a.cfg.Tools {
		byName[t.Name()] = t
	}

	for i := 0; i < a.cfg.MaxIterations; i++ {
		resp, err := a.cfg.LLM.Chat(ctx, messages, a.cfg.Tools)
		if err != nil {
			return "", fmt.Errorf("iteration %d: %w", i+1, err)
		}
		messages = append(messages, resp)

		if len(resp.ToolCalls) == 0 {
			a.cfg.Memory.Add(resp)
			return resp.Content, nil
		}

		for _, tc := range resp.ToolCalls {
			t, ok := byName[tc.Name]
			if !ok {
				messages = append(messages, llm.ToolResultMessage(tc.ID,
					fmt.Sprintf(`{"error": "tool %q is not registered"}`, tc.Name)))
				continue
			}
			var args map[string]any
			if err := unmarshalArgs(tc.Arguments, &args); err != nil {
				messages = append(messages, llm.ToolResultMessage(tc.ID,
					fmt.Sprintf(`{"error": "invalid arguments: %v"}`, err)))
				continue
			}
			res, err := t.Execute(ctx, args)
			if err != nil {
				res = fmt.Sprintf(`{"error": "%s"}`, err)
			}
			messages = append(messages, llm.ToolResultMessage(tc.ID, res))
		}
	}
	return "", fmt.Errorf("agent %q reached the %d-iteration limit without a final answer",
		a.cfg.Name, a.cfg.MaxIterations)
}

// DefaultSystemPrompt returns the built-in default prompt.
func DefaultSystemPrompt() string {
	return "You are a helpful agent. Use the available tools when needed to answer."
}
