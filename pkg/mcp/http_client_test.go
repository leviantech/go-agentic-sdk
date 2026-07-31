package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// TestHTTPClientSingleJSON covers the single-JSON response path
// (initialize + tools/list) and session id capture.
func TestHTTPClientSingleJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Mcp-Session-Id", "sess-1")
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocolVersion, "serverInfo": map[string]any{"name": "s", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "ping", "description": "Ping", "inputSchema": map[string]any{"type": "object"},
			}}}
		default:
			result = map[string]any{}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()

	cl := NewHTTPClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if cl.session != "sess-1" {
		t.Fatalf("session not captured: %q", cl.session)
	}

	ts, err := cl.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].Name != "ping" {
		t.Fatalf("wrong tools: %+v", ts)
	}

	reg := tools.NewRegistry()
	n, err := cl.RegisterTo(reg, "remote")
	if err != nil || n != 1 {
		t.Fatalf("register: n=%d err=%v", n, err)
	}
	tt, ok := reg.Get("remote_ping")
	if !ok {
		t.Fatal("remote_ping not registered")
	}
	if tt.Description() != "Ping" {
		t.Fatalf("desc wrong: %s", tt.Description())
	}
}

// TestHTTPClientSSEResponse covers the SSE response path (tools/call).
func TestHTTPClientSSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/call" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", mustJSON(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": "pong"}},
				},
			}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{"protocolVersion": protocolVersion}
		if req.Method == "tools/list" {
			result = map[string]any{"tools": []any{}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()

	cl := NewHTTPClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := cl.CallTool(ctx, "ping", map[string]any{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out != "pong" {
		t.Fatalf("wrong result: %q", out)
	}
}

// TestHTTPClientErrorStatus verifies non-2xx surfaces as an error.
func TestHTTPClientErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cl := NewHTTPClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got: %v", err)
	}
}

// TestHTTPClientBearer verifies the api key is sent as a bearer token.
func TestHTTPClientBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		result := map[string]any{"protocolVersion": protocolVersion}
		if req.Method == "tools/list" {
			result = map[string]any{"tools": []any{}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()

	cl := NewHTTPClient(srv.URL).WithAPIKey("tok123")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("wrong auth header: %q", gotAuth)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
