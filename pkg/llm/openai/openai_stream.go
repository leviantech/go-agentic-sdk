package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// ChatStream implements llm.StreamLLM for the OpenAI-style
// chat completions endpoint (SSE / stream=true).
func (c *Client) ChatStream(ctx context.Context, messages []llm.Message, tools []tools.Tool) (<-chan llm.StreamChunk, error) {
	payload := map[string]any{
		"model":    c.cfg.Model,
		"messages": mapMessages(messages),
		"tools":    mapTools(tools),
		"stream":   true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw := make([]byte, 500)
		n, _ := resp.Body.Read(raw)
		return nil, fmt.Errorf("LLM returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:n])))
	}

	ch := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var content strings.Builder
		callAcc := map[int]*llm.ToolCall{}

		flushCalls := func() {
			for _, tc := range callAcc {
				if tc != nil && tc.ID != "" && tc.Name != "" {
					cp := *tc
					ch <- llm.StreamChunk{ToolCall: &cp}
				}
			}
			callAcc = map[int]*llm.ToolCall{}
		}

		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				flushCalls()
				return
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			d := chunk.Choices[0].Delta
			if d.Content != "" {
				content.WriteString(d.Content)
				ch <- llm.StreamChunk{Content: content.String()}
			}
			for _, tc := range d.ToolCalls {
				acc := callAcc[tc.Index]
				if acc == nil {
					acc = &llm.ToolCall{}
					callAcc[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.ID = tc.ID
				}
				if tc.Function.Name != "" {
					acc.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.Arguments += tc.Function.Arguments
				}
			}
		}
		if err := sc.Err(); err != nil {
			ch <- llm.StreamChunk{Err: fmt.Errorf("stream read: %w", err)}
			return
		}
		flushCalls()
	}()

	return ch, nil
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

var _ llm.StreamLLM = (*Client)(nil)
