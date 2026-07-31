package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
)

var errDuplicateVectorID = errors.New("vector id already exists")

// VectorMemory is a Memory backed by embeddings: every added message is
// embedded and stored in the VectorStore; Messages() returns the original
// messages plus the most relevant past ones for the latest user input.
//
// Latest messages are always kept (short-term window); older semantically
// similar messages are re-injected as retrieval context.
type VectorMemory struct {
	mu        sync.Mutex
	embedder  Embedder
	store     VectorStore
	recent    []llm.Message // last N messages verbatim
	recentMax int
	k         int
}

func NewVectorMemory(embedder Embedder, store VectorStore, recentMax, k int) *VectorMemory {
	if recentMax <= 0 {
		recentMax = 10
	}
	if k <= 0 {
		k = 3
	}
	return &VectorMemory{
		embedder:  embedder,
		store:     store,
		recentMax: recentMax,
		k:         k,
	}
}

func (m *VectorMemory) Add(msg llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recent = append(m.recent, msg)
	if len(m.recent) > m.recentMax {
		// move messages that fall out of the window into the vector store
		overflow := m.recent[:len(m.recent)-m.recentMax]
		m.recent = m.recent[len(m.recent)-m.recentMax:]
		for i, old := range overflow {
			if old.Content == "" {
				continue
			}
			vec, err := m.embedder.Embed(context.Background(), old.Content)
			if err != nil {
				continue
			}
			_ = m.store.Add(context.Background(), fmt.Sprintf("msg-%p-%d", m, i), vec, map[string]string{"content": old.Content})
		}
	}
}

func (m *VectorMemory) Messages() []llm.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]llm.Message, 0, len(m.recent)+m.k)
	out = append(out, m.recent...)

	// Retrieve relevant context for the latest user message.
	last := m.lastUserMessage()
	if last != "" {
		if vec, err := m.embedder.Embed(context.Background(), last); err == nil {
			if hits, err := m.store.Search(context.Background(), vec, m.k); err == nil {
				for _, h := range hits {
					out = append(out, llm.Message{
						Role:    llm.RoleSystem,
						Content: "Relevant past conversation: " + h.Meta["content"],
					})
				}
			}
		}
	}
	return out
}

func (m *VectorMemory) lastUserMessage() string {
	for i := len(m.recent) - 1; i >= 0; i-- {
		if m.recent[i].Role == llm.RoleUser {
			return m.recent[i].Content
		}
	}
	return ""
}

func (m *VectorMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recent = nil
}

var _ Memory = (*VectorMemory)(nil)
