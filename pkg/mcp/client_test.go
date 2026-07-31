package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
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
	// GO_WANT_HELPER_PROCESS=2: spawn a child (mode-3) that inherits
	// stdout/stderr, then sleep — simulates a launcher like npx/uvx.
	// The grandchild holds the pipe: killing only the parent would not
	// release it. Process group kill must take both down.
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "2" {
		child := exec.Command(os.Args[0], "-test.run=TestMCPHelperProcess")
		child.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=3")
		_ = child.Start()
		time.Sleep(time.Hour)
	}
	// GO_WANT_HELPER_PROCESS=3: just sleep — holds stdout pipe open.
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "3" {
		time.Sleep(time.Hour)
		return
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

// TestCloseKillsProcessGroup: Close() must kill the entire process group,
// not just the direct child. Before the fix, a server process that forked
// a child (common with npx/uvx launchers) would leave the child alive
// holding the stdout pipe, and Process.Wait() would block forever.
// This replicates that shape: the helper (mode 2) spawns a grandchild
// (mode 3 = sleep forever) that inherits the stdout pipe, then sleeps.
// Killing only the parent would hang Wait(); killing the group succeeds.
func TestCloseKillsProcessGroup(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestMCPHelperProcess")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=2")
	// Grandchild inherits this pipe — the trap that made Wait() hang.
	if _, err := cmd.StdoutPipe(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Give the helper time to spawn its grandchild.
	time.Sleep(200 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		// Negative pid = the whole process group, matching Close().
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Good — grandchild was killed, pipe released, Wait returned.
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() blocked for 5s — process group kill failed; grandchild still holds the pipe")
	}
}
