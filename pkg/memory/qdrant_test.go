package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQdrantAddAndSearch(t *testing.T) {
	var lastPath, lastMethod string
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		lastMethod = r.Method
		if r.URL.Path == "/collections/coll/points" && r.Method == http.MethodPut {
			_ = json.NewDecoder(r.Body).Decode(&lastBody)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/points/search") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"result": [
					{"id": 99, "score": 0.92, "payload": {"meta": {"content": "reset password"}, "orig_id": "doc-1"}},
					{"id": 98, "score": 0.5, "payload": {"meta": {"content": "invoice"}, "orig_id": "doc-2"}}
				]
			}`))
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()

	q := NewQdrantStore(srv.URL, "coll")
	ctx := context.Background()
	if err := q.Add(ctx, "doc-1", []float32{0.1, 0.2}, map[string]string{"content": "reset password"}); err != nil {
		t.Fatal(err)
	}
	if lastMethod != http.MethodPut || !strings.Contains(lastPath, "/coll/points") {
		t.Fatalf("wrong add request: %s %s", lastMethod, lastPath)
	}
	if lastBody["points"] == nil {
		t.Fatalf("payload missing points: %v", lastBody)
	}

	hits, err := q.Search(ctx, []float32{0.1, 0.2}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ID != "doc-1" || hits[0].Score != 0.92 || hits[0].Meta["content"] != "reset password" {
		t.Fatalf("wrong hit: %+v", hits[0])
	}
}

func TestQdrantErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":{"error":"bad"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	q := NewQdrantStore(srv.URL, "coll")
	ctx := context.Background()
	if err := q.Add(ctx, "x", []float32{1}, nil); err == nil {
		t.Fatal("must error on 500")
	}
	if _, err := q.Search(ctx, []float32{1}, 1); err == nil {
		t.Fatal("must error on 500")
	}
}

func TestQdrantRejectsUnsafeCollection(t *testing.T) {
	for _, bad := range []string{"../../admin", "a/b", "a b", "", "col\"", "col?x=1"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("must panic for collection %q", bad)
				}
			}()
			NewQdrantStore("http://localhost:6333", bad)
		}()
	}
	// valid names pass
	for _, good := range []string{"docs", "my_collection", "A-B_c2"} {
		NewQdrantStore("http://localhost:6333", good)
	}
}

func TestQdrantAPIKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("api-key")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"}}`))
	}))
	defer srv.Close()

	q := NewQdrantStore(srv.URL, "coll").WithAPIKey("sekret")
	if err := q.Add(context.Background(), "x", []float32{1}, nil); err != nil {
		t.Fatal(err)
	}
	if gotKey != "sekret" {
		t.Fatalf("api-key not sent: %q", gotKey)
	}
}
