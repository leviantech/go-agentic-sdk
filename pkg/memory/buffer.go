package memory

import (
	"sync"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
)

// Memory stores conversation history between turns.
type Memory interface {
	Messages() []llm.Message
	Add(m llm.Message)
	Clear()
}

// ConversationBuffer stores all messages as-is (without summarization).
type ConversationBuffer struct {
	mu   sync.Mutex
	msgs []llm.Message
}

func NewConversationBuffer() *ConversationBuffer {
	return &ConversationBuffer{}
}

func (b *ConversationBuffer) Messages() []llm.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]llm.Message, len(b.msgs))
	copy(out, b.msgs)
	return out
}

func (b *ConversationBuffer) Add(m llm.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = append(b.msgs, m)
}

func (b *ConversationBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = nil
}

// Ensure satisfies the interface at compile time.
var _ Memory = (*ConversationBuffer)(nil)
