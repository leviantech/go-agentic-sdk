package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestExecuteErrorIsValidJSON: error text with quotes/newlines must not
// corrupt the JSON delivered to the LLM.
func TestExecuteErrorIsValidJSON(t *testing.T) {
	r := NewRegistry()
	r.Register(&FuncTool{
		N: "fail",
		F: func(_ context.Context, _ map[string]any) (string, error) {
			return "", errBoom{}
		},
	})
	res := r.Execute(context.Background(), "fail", "")
	var parsed map[string]string
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %s (%v)", res, err)
	}
	if parsed["error"] == "" {
		t.Fatalf("error message lost: %+v", parsed)
	}
}

type errBoom struct{}

func (errBoom) Error() string {
	return `boom "quoted" and
newline`
}

func TestRegisterDupError(t *testing.T) {
	r := NewRegistry()
	dummy := func(_ context.Context, _ map[string]any) (string, error) { return "", nil }
	if err := r.Register(&FuncTool{N: "a", F: dummy}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register(&FuncTool{N: "a", F: dummy}); err == nil {
		t.Fatal("registering a duplicate name must error")
	}
}

func TestRegisterGuards(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatal("nil tool must error")
	}
	if err := r.Register(&FuncTool{N: "empty-fn"}); err == nil {
		t.Fatal("nil callback must error")
	}
	if err := r.Register(&FuncTool{N: ""}); err == nil {
		t.Fatal("empty name must error")
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
