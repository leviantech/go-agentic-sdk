package memory

import (
	"context"
	"math"
	"sort"
	"sync"
)

// MemVectorStore is an in-process brute-force cosine similarity store.
// Fine for small-to-medium corpora (SDK default); swap in a pgvector/
// Qdrant adapter for production scale.
type MemVectorStore struct {
	mu    sync.RWMutex
	ids   map[string]bool
	items []memItem
}

type memItem struct {
	id     string
	vector []float32
	meta   map[string]string
}

func NewMemVectorStore() *MemVectorStore {
	return &MemVectorStore{ids: map[string]bool{}}
}

func (s *MemVectorStore) Add(_ context.Context, id string, vector []float32, meta map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ids[id] {
		return errDuplicateVectorID
	}
	s.ids[id] = true
	s.items = append(s.items, memItem{id: id, vector: vector, meta: meta})
	return nil
}

func (s *MemVectorStore) Search(_ context.Context, vector []float32, k int) ([]VectorHit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if k <= 0 {
		k = 5
	}
	hits := make([]VectorHit, 0, len(s.items))
	for _, it := range s.items {
		// Copy vector + meta so callers cannot mutate store internals.
		vec := make([]float32, len(it.vector))
		copy(vec, it.vector)
		meta := make(map[string]string, len(it.meta))
		for k, v := range it.meta {
			meta[k] = v
		}
		hits = append(hits, VectorHit{
			ID:     it.id,
			Score:  cosine(it.vector, vector),
			Meta:   meta,
			Vector: vec,
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func cosine(a, b []float32) float32 {
	var dot, na, nb float32
	for i := 0; i < len(a) && i < len(b); i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na*nb)))
}

var _ VectorStore = (*MemVectorStore)(nil)
