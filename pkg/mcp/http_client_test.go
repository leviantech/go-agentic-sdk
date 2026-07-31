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

// TestHTTPClientStreaming covers tools/call with _meta.streaming:
// partial message/mcp.streamMessage events, progress notification skipped,
// endOfStream flag, and chunk delivery.
func TestHTTPClientStreaming(t *testing.T) {
	var gotMeta bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/call" {
			_, gotMeta = req.Params["_meta"].(map[string]any)
			w.Header().Set("Content-Type", "text/event-stream")
			chunk := func(text string) map[string]any {
				return map[string]any{
					"jsonrpc": "2.0", "method": "message/mcp.streamMessage",
					"params": map[string]any{"message": map[string]any{
						"jsonrpc": "2.0", "id": req.ID,
						"result": map[string]any{"content": []any{
							map[string]any{"type": "text", "text": text},
						}},
					}},
				}
			}
			final := chunk("Hello, world")
			final["params"].(map[string]any)["_meta"] = map[string]any{"endOfStream": true}
			stream := []map[string]any{
				// progress notification without id: must be skipped
				{"jsonrpc": "2.0", "method": "notifications/progress",
					"params": map[string]any{"progress": 0.5}},
				chunk("Hel"),
				chunk("Hello, "),
				chunk("Hello, world"),
				final,
			}
			for _, ev := range stream {
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", mustJSON(ev))
			}
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

	var chunks []string
	out, err := cl.StreamCall(ctx, "echo", map[string]any{"x": 1}, func(c string) {
		chunks = append(chunks, c)
	})
	if err != nil {
		t.Fatalf("stream call: %v", err)
	}
	if !gotMeta {
		t.Fatal("_meta.streaming not requested")
	}
	if out != "Hello, world" {
		t.Fatalf("wrong result: %q", out)
	}
	joined := strings.Join(chunks, "")
	if joined != "Hello, world" {
		t.Fatalf("wrong chunks: %q", chunks)
	}
}

// TestHTTPClientStreamingError verifies an isError stream result surfaces.
func TestHTTPClientStreamingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/call" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", mustJSON(map[string]any{
				"jsonrpc": "2.0", "method": "message/mcp.streamMessage",
				"params": map[string]any{"_meta": map[string]any{"endOfStream": true},
					"message": map[string]any{
						"jsonrpc": "2.0", "id": req.ID,
						"result": map[string]any{"isError": true, "content": []any{
							map[string]any{"type": "text", "text": "boom"}}}}},
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
	if _, err := cl.StreamCall(ctx, "bad", map[string]any{}, func(string) {}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected tool error, got: %v", err)
	}
}

// TestHTTPClientStreamMessageSingleJSON: a server that wraps a normal
// response inside message/mcp.streamMessage (single JSON, no SSE) must
// still work for plain CallTool — the inner message is unwrapped.
func TestHTTPClientStreamMessageSingleJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/call" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "method": "message/mcp.streamMessage",
				"params": map[string]any{"_meta": map[string]any{"endOfStream": true},
					"message": map[string]any{
						"jsonrpc": "2.0", "id": req.ID,
						"result": map[string]any{"content": []any{
							map[string]any{"type": "text", "text": "wrapped"}}}}},
			})
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
	if out != "wrapped" {
		t.Fatalf("wrong result: %q", out)
	}
}

// TestHTTPClientNoClientTimeout: long SSE streams must not be killed by a
// fixed client timeout (the 60s client-level Timeout was removed).
func TestHTTPClientNoClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/call" {
			fl := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			// two chunks separated by >1s: would be cut by any
			// fixed sub-2s client timeout
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
				"jsonrpc": "2.0", "method": "message/mcp.streamMessage",
				"params": map[string]any{"message": map[string]any{
					"jsonrpc": "2.0", "id": req.ID,
					"result": map[string]any{"content": []any{
						map[string]any{"type": "text", "text": "slow"}}}}}}))
			fl.Flush()
			time.Sleep(1500 * time.Millisecond)
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
				"jsonrpc": "2.0", "method": "message/mcp.streamMessage",
				"params": map[string]any{"_meta": map[string]any{"endOfStream": true},
					"message": map[string]any{
						"jsonrpc": "2.0", "id": req.ID,
						"result": map[string]any{"content": []any{
							map[string]any{"type": "text", "text": "slowpoke"}}}}}}))
			fl.Flush()
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := cl.StreamCall(ctx, "slow", map[string]any{}, func(string) {})
	if err != nil {
		t.Fatalf("stream call: %v", err)
	}
	if out != "slowpoke" {
		t.Fatalf("wrong result: %q", out)
	}
}

// TestUnwrapStream covers the params.message extraction used by both
// transports when a server wraps responses in streamMessage.
func TestUnwrapStream(t *testing.T) {
	msg := rpcMessage{
		Method: "message/mcp.streamMessage",
		Params: json.RawMessage(`{"message":{"jsonrpc":"2.0","id":7,"result":{"content":[]}}}`),
	}
	inner := unwrapStream(msg)
	if inner == nil || string(inner.ID) != "7" {
		t.Fatalf("unwrap failed: %+v", inner)
	}
	if u := unwrapStream(rpcMessage{Method: "message/mcp.streamMessage", Params: json.RawMessage(`{}`)}); u != nil {
		t.Fatalf("empty params must unwrap to nil, got %+v", u)
	}
	if u := unwrapStream(rpcMessage{Method: "tools/call", Params: json.RawMessage(`{}`)}); u != nil {
		t.Fatalf("non-streamMessage must unwrap to nil")
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
