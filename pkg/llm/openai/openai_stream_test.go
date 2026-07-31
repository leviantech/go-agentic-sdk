package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// TestChatStreamSSE verifies SSE parsing: content accumulates,
// tool call deltas are merged into a single complete ToolCall.
func TestChatStreamSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Hel"}}]}

data: {"choices":[{"delta":{"content":"lo"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"greet","arguments":""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"name\":\""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"Budi\"}"}}]}}]}

data: [DONE]
`)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Model: "test", APIKey: "k"})
	ch, err := c.ChatStream(context.Background(), nil, []tools.Tool{})
	if err != nil {
		t.Fatal(err)
	}
	var full string
	var calls []llm.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("chunk err: %v", chunk.Err)
		}
		if chunk.Content != "" {
			full = chunk.Content
		}
		if chunk.ToolCall != nil {
			calls = append(calls, *chunk.ToolCall)
		}
	}
	if full != "Hello" {
		t.Fatalf("accumulated content wrong: %q", full)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	c0 := calls[0]
	if c0.ID != "call_1" || c0.Name != "greet" || c0.Arguments != `{"name":"Budi"}` {
		t.Fatalf("tool call wrong: %+v", c0)
	}
}

// TestChatStreamHTTPError verifies errors on non-200 status.
func TestChatStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Model: "test", APIKey: "k"})
	if _, err := c.ChatStream(context.Background(), nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

// TestPayloadJSON verifies the streaming payload structure contains stream:true.
func TestPayloadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if body["stream"] != true {
			http.Error(w, "stream must be true", 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Model: "test", APIKey: "k"})
	ch, err := c.ChatStream(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	_ = strings.TrimSpace
}
