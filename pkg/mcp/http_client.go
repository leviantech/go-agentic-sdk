package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// HTTPClient is an MCP client using the streamable HTTP transport
// (MCP spec 2025-06-18): POST JSON-RPC to an endpoint, responses come
// back either as a single JSON document or as an SSE stream.
//
//	cl := mcp.NewHTTPClient("https://example.com/mcp")
//	cl.Start(ctx)
//	cl.RegisterTo(reg, "remote")
type HTTPClient struct {
	url    string
	apiKey string
	client *http.Client

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcMessage
	session string
	closed  bool
}

func NewHTTPClient(url string) *HTTPClient {
	return &HTTPClient{
		url:     url,
		client:  &http.Client{Timeout: 60 * time.Second},
		pending: map[int]chan rpcMessage{},
	}
}

// WithAPIKey sets the Authorization: Bearer header (optional).
func (h *HTTPClient) WithAPIKey(key string) *HTTPClient {
	h.apiKey = key
	return h
}

// Start performs the initialize handshake over HTTP and stores the
// session id (Mcp-Session-Id) for subsequent requests.
func (h *HTTPClient) Start(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "go-agentic-sdk",
			"version": sdkVersion,
		},
	}
	if _, err := h.call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp http handshake failed: %w", err)
	}
	h.notify("notifications/initialized", map[string]any{})
	return nil
}

// Close ends the session (DELETE /mcp per the spec, best-effort).
func (h *HTTPClient) Close() error {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	if h.session != "" {
		req, err := http.NewRequest(http.MethodDelete, h.url, nil)
		if err == nil {
			req.Header.Set("Mcp-Session-Id", h.session)
			if h.apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+h.apiKey)
			}
			resp, err := h.client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}
	}
	return nil
}

// ListTools returns the tools exposed by the server.
func (h *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	raw, err := h.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool invokes a tool and returns the concatenated text content.
func (h *HTTPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	raw, err := h.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, part := range out.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	res := strings.TrimSpace(b.String())
	if out.IsError {
		return res, fmt.Errorf("mcp tool %q returned an error: %s", name, res)
	}
	return res, nil
}

// RegisterTo registers every server tool into the registry with a prefix.
func (h *HTTPClient) RegisterTo(reg *tools.Registry, prefix string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ts, err := h.ListTools(ctx)
	if err != nil {
		return 0, err
	}
	for _, t := range ts {
		name := t.Name
		if prefix != "" {
			name = prefix + "_" + t.Name
		}
		if err := reg.Register(&mcpTool{caller: h, method: t.Name, name: name, desc: t.Description, schema: t.InputSchema}); err != nil {
			return 0, err
		}
	}
	return len(ts), nil
}

func (h *HTTPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, errors.New("mcp http client is closed")
	}
	h.nextID++
	id := h.nextID
	ch := make(chan rpcMessage, 1)
	h.pending[id] = ch
	session := h.session
	h.mu.Unlock()

	data, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		h.dropPending(id)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(data))
	if err != nil {
		h.dropPending(id)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		h.dropPending(id)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		h.dropPending(id)
		return nil, fmt.Errorf("mcp http returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Capture a new session id if the server assigns one.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		h.mu.Lock()
		h.session = sid
		h.mu.Unlock()
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		// SSE: read events; deliver the JSON-RPC message inside each data line.
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var msg rpcMessage
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &msg); err != nil {
				continue
			}
			if h.deliver(msg) {
				break
			}
		}
	} else {
		var msg rpcMessage
		if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
			h.dropPending(id)
			return nil, err
		}
		h.deliver(msg)
	}

	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, fmt.Errorf("mcp %s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// notify sends a JSON-RPC notification (no id) and drains the response.
func (h *HTTPClient) notify(method string, params any) {
	data, err := json.Marshal(rpcNotification{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	session := h.session
	h.mu.Unlock()

	req, err := http.NewRequest(http.MethodPost, h.url, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// deliver routes a response to its pending caller; returns true when the
// message matched a pending request (call can finish).
func (h *HTTPClient) deliver(msg rpcMessage) bool {
	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		return false
	}
	var id int
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return false
	}
	h.mu.Lock()
	ch := h.pending[id]
	delete(h.pending, id)
	h.mu.Unlock()
	if ch != nil {
		ch <- msg
		return true
	}
	return false
}

func (h *HTTPClient) dropPending(id int) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}
