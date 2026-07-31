package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"
)

// QdrantStore is a VectorStore backed by a Qdrant server (REST API).
// No extra dependencies: it talks to the Qdrant HTTP API directly.
//
//	store := memory.NewQdrantStore("http://localhost:6333", "my_collection")
type QdrantStore struct {
	baseURL    string // e.g. http://localhost:6333
	collection string
	apiKey     string // optional; sent as api-key header
	client     *http.Client
}

func NewQdrantStore(baseURL, collection string) *QdrantStore {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &QdrantStore{
		baseURL:    baseURL,
		collection: collection,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// WithAPIKey sets the Qdrant api-key header (optional).
func (q *QdrantStore) WithAPIKey(key string) *QdrantStore {
	q.apiKey = key
	return q
}

// idToUint maps a string id to a stable uint64 for Qdrant point ids.
func idToUint(id string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(id))
	return h.Sum64()
}

func (q *QdrantStore) Add(_ context.Context, id string, vector []float32, meta map[string]string) error {
	// Qdrant point ids are uint64 or UUID strings; store the original id
	// in the payload so Search can return it.
	payload := map[string]any{"meta": meta, "orig_id": id}
	body, err := json.Marshal(map[string]any{
		"points": []any{map[string]any{
			"id":      idToUint(id),
			"vector":  vector,
			"payload": payload,
		}},
	})
	if err != nil {
		return err
	}
	url := q.baseURL + "collections/" + q.collection + "/points?wait=true"
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}
	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant add returned %d", resp.StatusCode)
	}
	return nil
}

func (q *QdrantStore) Search(ctx context.Context, vector []float32, k int) ([]VectorHit, error) {
	body, err := json.Marshal(map[string]any{
		"vector":       vector,
		"limit":        k,
		"with_payload": true,
		"with_vector":  false,
	})
	if err != nil {
		return nil, err
	}
	url := q.baseURL + "collections/" + q.collection + "/points/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}
	resp, err := q.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qdrant search returned %d", resp.StatusCode)
	}

	var out struct {
		Result []struct {
			Score   float32        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	hits := make([]VectorHit, 0, len(out.Result))
	for _, r := range out.Result {
		h := VectorHit{Score: r.Score}
		if meta, ok := r.Payload["meta"].(map[string]any); ok {
			h.Meta = map[string]string{}
			for mk, mv := range meta {
				if s, ok := mv.(string); ok {
					h.Meta[mk] = s
				}
			}
		}
		if id, ok := r.Payload["orig_id"].(string); ok {
			h.ID = id
		}
		hits = append(hits, h)
	}
	return hits, nil
}

var _ VectorStore = (*QdrantStore)(nil)
