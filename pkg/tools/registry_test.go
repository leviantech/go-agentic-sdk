package tools

import (
	"context"
	"strings"
	"testing"
)

func TestRegisterDupError(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&FuncTool{N: "a"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register(&FuncTool{N: "a"}); err == nil {
		t.Fatal("registering a duplicate name must error")
	}
}

func TestExecuteJSONArgs(t *testing.T) {
	r := NewRegistry()
	r.Register(&FuncTool{
		N: "echo",
		D: "returns input",
		S: map[string]any{"type": "object"},
		F: func(_ context.Context, args map[string]any) (string, error) {
			return args["msg"].(string), nil
		},
	})

	res := r.Execute(context.Background(), "echo", `{"msg":"hello"}`)
	if res != "hello" {
		t.Fatalf("wrong result: %s", res)
	}

	res = r.Execute(context.Background(), "echo", `{invalid`)
	if !strings.Contains(res, "error") {
		t.Fatalf("invalid arguments must error: %s", res)
	}

	res = r.Execute(context.Background(), "tak-ada", `{}`)
	if !strings.Contains(res, "not found") {
		t.Fatalf("missing tool must error: %s", res)
	}
}
