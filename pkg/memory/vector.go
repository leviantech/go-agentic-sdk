package memory

import "context"

// Embedder converts text into a dense vector representation.
// Implementations: OpenAI embeddings, local models, or a fake for tests.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// VectorStore is a semantic index. Implementations: in-memory
// (MemVectorStore), Qdrant, pgvector, etc.
type VectorStore interface {
	Add(ctx context.Context, id string, vector []float32, meta map[string]string) error
	// Search returns the k nearest neighbors of vector (cosine similarity).
	Search(ctx context.Context, vector []float32, k int) ([]VectorHit, error)
}

// VectorHit is one search result.
type VectorHit struct {
	ID     string
	Score  float32 // cosine similarity, closer to 1 = more similar
	Meta   map[string]string
	Vector []float32
}
