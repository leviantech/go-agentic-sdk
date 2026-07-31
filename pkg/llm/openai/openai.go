// Package openai provides an LLMProvider for chat completions endpoints
// in OpenAI style: OpenAI, Azure OpenAI, and compatible endpoints
// (Ollama, vLLM, LM Studio, llama.cpp server).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// Config configures the OpenAI-compatible provider.
type Config struct {
	APIKey  string
	BaseURL string // e.g. https://api.openai.com/v1 (no trailing slash)
	Model   string
	Client  *http.Client
}

// Client implements llm.LLM.
type Client struct {
	cfg Config
}

func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	// Normalize: a trailing slash would produce "//chat/completions".
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{cfg: cfg}
}

func (c *Client) Chat(ctx context.Context, messages []llm.Message, tools []tools.Tool) (llm.Message, error) {
	payload := map[string]any{
		"model":    c.cfg.Model,
		"messages": mapMessages(messages),
	}
	// Only send the tools key when tools exist: several OpenAI-compatible
	// gateways reject an explicit empty array (or change behavior).
	if len(tools) > 0 {
		payload["tools"] = mapTools(tools)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return llm.Message{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return llm.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.cfg.Client.Do(req)
	if err != nil {
		return llm.Message{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return llm.Message{}, err
	}
	if resp.StatusCode != http.StatusOK {
		// Truncate the response body so large error payloads cannot leak into logs.
		const maxBody = 500
		body := strings.TrimSpace(string(raw))
		if len(body) > maxBody {
			body = body[:maxBody] + "...(truncated)"
		}
		return llm.Message{}, fmt.Errorf("LLM returned %d: %s",
			resp.StatusCode, body)
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return llm.Message{}, err
	}
	if out.Error != nil {
		return llm.Message{}, fmt.Errorf("LLM error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return llm.Message{}, fmt.Errorf("LLM returned no choices")
	}

	m := out.Choices[0].Message
	msg := llm.Message{Role: llm.RoleAssistant, Content: m.Content}
	for _, tc := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return msg, nil
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func mapMessages(msgs []llm.Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		item := map[string]any{"role": string(m.Role)}
		if m.Content != "" {
			item["content"] = m.Content
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				})
			}
			item["tool_calls"] = tcs
		}
		if m.Role == llm.RoleTool {
			item["tool_call_id"] = m.ToolCallID
		}
		out = append(out, item)
	}
	return out
}

func mapTools(ts []tools.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(ts))
	for _, t := range ts {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Schema(),
			},
		})
	}
	return out
}
