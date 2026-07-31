package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Tool is the contract for a tool the agent can call.
// Two implementation forms:
//   - FuncTool: wraps a plain function (most common)
//   - a struct implementing all four methods (for complex tools)
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any // OpenAI-style JSON Schema (type object)
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// FuncTool adapts a simple function into a Tool.
type FuncTool struct {
	N string
	D string
	S map[string]any
	F func(ctx context.Context, args map[string]any) (string, error)
}

func (t *FuncTool) Name() string        { return t.N }
func (t *FuncTool) Description() string { return t.D }
func (t *FuncTool) Schema() map[string]any {
	if t.S != nil {
		return t.S
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *FuncTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.F(ctx, args)
}

// Registry stores registered tools with concurrency-safe access.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds a tool; errors if the name is already registered.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Name()]; ok {
		return fmt.Errorf("tool %q is already registered", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// Execute calls a tool by name + raw JSON arguments.
// Always returns a string (tool result or error message) so the agent
// loop is never interrupted by a single tool failing.
func (r *Registry) Execute(ctx context.Context, name, argsJSON string) string {
	t, ok := r.Get(name)
	if !ok {
		return errJSON(fmt.Sprintf("tool %q not found", name))
	}
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return errJSON("invalid arguments: " + err.Error())
		}
	}
	res, err := t.Execute(ctx, args)
	if err != nil {
		return errJSON(err.Error())
	}
	return res
}

// errJSON wraps an arbitrary error message in a well-formed JSON object,
// escaping quotes/newlines so the LLM always receives valid JSON.
func errJSON(msg string) string {
	b, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		return `{"error":"tool failed"}`
	}
	return string(b)
}
