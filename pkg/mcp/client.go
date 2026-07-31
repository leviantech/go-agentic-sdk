// Package mcp implements a Model Context Protocol (MCP) client over stdio
// (JSON-RPC 2.0, newline-delimited). It lets agents use tools exposed by
// MCP servers — e.g. `npx -y @modelcontextprotocol/server-filesystem .` —
// by registering them as ordinary SDK tools.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// protocolVersion is the MCP protocol version spoken by this client.
const protocolVersion = "2024-11-05"

// sdkVersion identifies this client in the initialize handshake.
const sdkVersion = "0.2.0"

// rpcRequest is a JSON-RPC 2.0 request.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcNotification is a JSON-RPC 2.0 notification (no id, no reply).
type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// rpcMessage is an incoming JSON-RPC message (response or notification).
type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// Client is a stdio MCP client wrapping a subprocess.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	out     *bufio.Reader
	stderr  *bytes.Buffer

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcMessage
	closed  bool
}

// NewStdioClient creates a client that will spawn command with args.
// Call Start to launch it and complete the MCP handshake.
func NewStdioClient(command string, args ...string) *Client {
	return &Client{
		cmd:     exec.Command(command, args...),
		pending: map[int]chan rpcMessage{},
	}
}

// WithEnv appends environment variables for the MCP server process.
func (c *Client) WithEnv(extra map[string]string) *Client {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	c.cmd.Env = env
	return c
}

// Start launches the server process and performs the MCP initialize handshake.
func (c *Client) Start(ctx context.Context) error {
	in, err := c.cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	c.stdin = in
	c.out = bufio.NewReader(out)
	c.stderr = &bytes.Buffer{}
	c.cmd.Stderr = c.stderr

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start mcp server %q: %w (stderr: %s)",
			c.cmd.Path, err, strings.TrimSpace(c.stderr.String()))
	}
	go c.readLoop()

	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return fmt.Errorf("mcp handshake failed: %w", err)
	}
	return nil
}

// Close terminates the server process.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	return nil
}

// Stderr returns whatever the server process wrote to stderr (diagnostics).
func (c *Client) Stderr() string {
	return strings.TrimSpace(c.stderr.String())
}

// initialize runs the MCP initialize request + initialized notification.
func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "go-agentic-sdk",
			"version": sdkVersion,
		},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	c.notify("notifications/initialized", map[string]any{})
	return nil
}

// Tool is a tool exposed by an MCP server.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ListTools returns the tools exposed by the server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	raw, err := c.call(ctx, "tools/list", map[string]any{})
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
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	raw, err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
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

// RegisterTo registers every tool of the server into the registry.
// When prefix is non-empty, tool names become <prefix>_<tool> to avoid
// collisions between servers (and with skill/builtin tools).
func (c *Client) RegisterTo(reg *tools.Registry, prefix string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ts, err := c.ListTools(ctx)
	if err != nil {
		return 0, err
	}
	for _, t := range ts {
		name := t.Name
		if prefix != "" {
			name = prefix + "_" + t.Name
		}
		if err := reg.Register(&mcpTool{client: c, method: t.Name, name: name, desc: t.Description, schema: t.InputSchema}); err != nil {
			return 0, err
		}
	}
	return len(ts), nil
}

// call sends a request and waits for its response.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("mcp client is closed")
	}
	c.nextID++
	id := c.nextID
	ch := make(chan rpcMessage, 1)
	c.pending[id] = ch
	data, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	_, err = c.stdin.Write(append(data, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write to mcp server: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// notify sends a JSON-RPC notification (fire-and-forget).
func (c *Client) notify(method string, params any) {
	data, err := json.Marshal(rpcNotification{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	_, _ = c.stdin.Write(append(data, '\n'))
}

// readLoop dispatches incoming messages to waiting callers.
func (c *Client) readLoop() {
	for {
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			c.closePending(fmt.Errorf("mcp connection closed: %w", err))
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			continue // notification
		}
		var id int
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (c *Client) closePending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.pending {
		ch <- rpcMessage{Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	c.pending = map[int]chan rpcMessage{}
}
