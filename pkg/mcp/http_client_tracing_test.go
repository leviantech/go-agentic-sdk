package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/trace"
)

// spyTracer records every Start call so tests can inspect which spans
// were emitted without pulling in OTel.
type spyTracer struct {
	spans []*spySpan
}

type spySpan struct {
	name  string
	attrs map[string]string
}

func (s *spyTracer) Start(_ context.Context, name string) (context.Context, trace.Span) {
	sp := &spySpan{name: name, attrs: map[string]string{}}
	s.spans = append(s.spans, sp)
	return context.WithValue(context.Background(), spySpanKey{}, sp), sp
}

func (s *spySpan) End() {}
func (s *spySpan) SetAttribute(k, v string) {
	s.attrs[k] = v
}

var _ trace.Tracer = (*spyTracer)(nil)
var _ trace.Span = (*spySpan)(nil)

type spySpanKey struct{}

func spySpanFrom(ctx context.Context) *spySpan {
	if sp, ok := ctx.Value(spySpanKey{}).(*spySpan); ok {
		return sp
	}
	return nil
}

// echoHandler returns a test handler that echoes tools/call and responds
// to tools/list/initialize with a dummy tool.
func echoHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.Method == http.MethodDelete {
			w.WriteHeader(200)
			return
		}
		// The initialize handshake sets the session id.
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}
		result := map[string]any{"protocolVersion": protocolVersion}
		switch req.Method {
		case "tools/list":
			result["tools"] = []any{map[string]any{
				"name": "echo", "description": "echo",
				"inputSchema": map[string]any{"type": "object"},
			}}
		case "tools/call":
			result["content"] = []any{map[string]any{"type": "text", "text": "ok"}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID, "result": result,
		})
	}
}

func TestHTTPClientWithTracerRecordsSpans(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echoHandler(t)))
	defer srv.Close()

	tr := &spyTracer{}
	cl := NewHTTPClient(srv.URL).WithTracer(tr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := cl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cl.ListTools(ctx)
	cl.CallTool(ctx, "echo", map[string]any{"text": "hi"})
	cl.Close()

	// Expected spans: initialize, notifications/initialized (notify),
	// tools/list, tools/call, DELETE (Close) = 5.
	if len(tr.spans) != 5 {
		t.Fatalf("expected 5 spans, got %d: %+v", len(tr.spans), tr.spans)
	}

	// Span 0: initialize.
	if tr.spans[0].attrs["mcp.method"] != "initialize" {
		t.Fatalf("span 0 method: %q", tr.spans[0].attrs["mcp.method"])
	}

	// Span 1: notifications/initialized (notify).
	if tr.spans[1].attrs["mcp.method"] != "notifications/initialized" {
		t.Fatalf("span 1 method: %q", tr.spans[1].attrs["mcp.method"])
	}

	// Span 2: tools/list.
	if tr.spans[2].attrs["mcp.method"] != "tools/list" {
		t.Fatalf("span 2 method: %q", tr.spans[2].attrs["mcp.method"])
	}

	// Span 3: tools/call.
	if tr.spans[3].attrs["mcp.method"] != "tools/call" {
		t.Fatalf("span 3 method: %q", tr.spans[3].attrs["mcp.method"])
	}

	// All spans carry the server URL.
	for i, sp := range tr.spans {
		if sp.attrs["mcp.server_url"] != srv.URL {
			t.Fatalf("span %d wrong URL: %q", i, sp.attrs["mcp.server_url"])
		}
	}

	// Span 4: DELETE (Close).
	if tr.spans[4].attrs["mcp.method"] != "DELETE" {
		t.Fatalf("span 4 method: %q", tr.spans[4].attrs["mcp.method"])
	}
}

func TestHTTPClientNoTracerStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echoHandler(t)))
	defer srv.Close()
	cl := NewHTTPClient(srv.URL).WithTracer(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cl.Close()
}

func TestHTTPClientTracerErrorSpan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, "bad")
	}))
	defer srv.Close()

	tr := &spyTracer{}
	cl := NewHTTPClient(srv.URL).WithTracer(tr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = cl.Start(ctx)

	found := false
	for _, sp := range tr.spans {
		if sp.attrs["error"] != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no span has error attribute after failed initialize")
	}
}
