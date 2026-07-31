package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/leviantech/go-agentic-sdk/pkg/llm"
)

// fakeEmbedder maps tokens to a stable vector: one-hot style hashing
// (deterministic, no external API).
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, 16)
	for _, r := range strings.Fields(text) {
		idx := 0
		for _, c := range r {
			idx += int(c)
		}
		vec[idx%len(vec)] += 1
	}
	return vec, nil
}

func TestMemVectorStoreSearch(t *testing.T) {
	s := NewMemVectorStore()
	e := fakeEmbedder{}
	ctx := context.Background()

	doc1, _ := e.Embed(ctx, "cara reset password akun")
	doc2, _ := e.Embed(ctx, "invoice bulanan dan pembayaran")
	if err := s.Add("1", doc1, map[string]string{"content": "reset password"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("2", doc2, map[string]string{"content": "invoice"}); err != nil {
		t.Fatal(err)
	}
	// duplicate id must error
	if err := s.Add("1", doc1, nil); err == nil {
		t.Fatal("duplicate id must error")
	}

	q, _ := e.Embed(ctx, "cara reset password")
	hits, err := s.Search(q, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "1" {
		t.Fatalf("expected doc 1, got %+v", hits)
	}
}

func TestVectorMemoryInjectsRelevant(t *testing.T) {
	vm := NewVectorMemory(fakeEmbedder{}, NewMemVectorStore(), 2, 1)

	// Isi history dulu (tanpa agent; langsung via Add).
	vm.Add(llm.Message{Role: llm.RoleUser, Content: "cerita tentang kopi aceh"})
	vm.Add(llm.Message{Role: llm.RoleAssistant, Content: "kopi aceh itu enak"})
	vm.Add(llm.Message{Role: llm.RoleUser, Content: "cerita tentang reset password"})
	vm.Add(llm.Message{Role: llm.RoleUser, Content: "cara reset password saya?"})

	msgs := vm.Messages()
	// recentMax=2 → 2 pesan terakhir + retrieval context
	var hasRetrieval bool
	for _, m := range msgs {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Relevant past conversation") {
			hasRetrieval = true
		}
	}
	if !hasRetrieval {
		t.Fatalf("retrieval context tidak di-inject: %+v", msgs)
	}
}
