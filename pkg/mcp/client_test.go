package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// TestMCPHelperProcess is the fake MCP server. It is the test binary itself,
// re-executed by the client with GO_WANT_HELPER_PROCESS=1.
func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	serveFakeMCPServer()
	os.Exit(0)
}

func serveFakeMCPServer() {
	type helperRequest struct {
		ID     int             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req helperRequest
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "fake", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []any{
					map[string]any{
						"name":        "echo",
						"description": "Echoes the input text",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"text": map[string]any{"type": "string"},
							},
						},
					},
				},
			}
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			text, _ := p.Arguments["text"].(string)
			result = map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "echo: " + text}},
			}
			// Diagnostic noise: exercises the concurrent stderr path read by
			// Stderr()/Start() (race detector verifies the syncBuffer lock).
			fmt.Fprintln(os.Stderr, "tool-call:", req.Method)
		default:
			result = map[string]any{}
		}
		data, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
		fmt.Println(string(data))
	}
}

// TestClientEndToEnd spawns the fake server (this test binary) and verifies
// handshake, tool listing, tool calling, and registry registration.
func TestClientEndToEnd(t *testing.T) {
	cl := NewStdioClient(os.Args[0], "-test.run=TestMCPHelperProcess")
	cl.WithEnv(map[string]string{"GO_WANT_HELPER_PROCESS": "1"})
	defer cl.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err != nil {
		t.Fatalf("start: %v (stderr: %s)", err, cl.Stderr())
	}

	ts, err := cl.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(ts) != 1 || ts[0].Name != "echo" {
		t.Fatalf("wrong tools: %+v", ts)
	}

	out, err := cl.CallTool(ctx, "echo", map[string]any{"text": "halo"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if out != "echo: halo" {
		t.Fatalf("wrong result: %q", out)
	}

	// registration with prefix
	reg := tools.NewRegistry()
	n, err := cl.RegisterTo(reg, "fake")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 tool, got %d", n)
	}
	tt, ok := reg.Get("fake_echo")
	if !ok {
		t.Fatal("tool fake_echo not registered")
	}
	res, err := tt.Execute(ctx, map[string]any{"text": "dunia"})
	if err != nil || res != "echo: dunia" {
		t.Fatalf("execute via registry: %v / %s", err, res)
	}
}

// TestCallAfterClose ensures requests after Close are rejected.
func TestCallAfterClose(t *testing.T) {
	cl := NewStdioClient(os.Args[0], "-test.run=TestMCPHelperProcess")
	cl.WithEnv(map[string]string{"GO_WANT_HELPER_PROCESS": "1"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cl.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.ListTools(ctx); err == nil {
		t.Fatal("must error after close")
	}
}

// TestStartGagal verifies an error when the process cannot be launched.
func TestStartGagal(t *testing.T) {
	cl := NewStdioClient("/bin/definitely-not-exists-xyz")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err == nil {
		t.Fatal("must error")
	}
}
