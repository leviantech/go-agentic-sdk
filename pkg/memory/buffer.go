package memory

import "github.com/leviantech/go-agentic-sdk/pkg/llm"

// Memory stores conversation history between turns.
type Memory interface {
	Messages() []llm.Message
	Add(m llm.Message)
	Clear()
}

// ConversationBuffer stores all messages as-is (without summarization).
type ConversationBuffer struct {
	msgs []llm.Message
}

func NewConversationBuffer() *ConversationBuffer {
	return &ConversationBuffer{}
}

func (b *ConversationBuffer) Messages() []llm.Message {
	out := make([]llm.Message, len(b.msgs))
	copy(out, b.msgs)
	return out
}

func (b *ConversationBuffer) Add(m llm.Message) {
	b.msgs = append(b.msgs, m)
}

func (b *ConversationBuffer) Clear() {
	b.msgs = nil
}

// Ensure satisfies the interface at compile time.
var _ Memory = (*ConversationBuffer)(nil)
