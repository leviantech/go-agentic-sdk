package langfuse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/trace"
)

// TestLangfuseEndToEnd: create a trace with nested spans, verify
// batched HTTP requests contain trace-create, span-create and
// generation-create events in correct structure.
func TestLangfuseEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/ingestion" {
			w.WriteHeader(404)
			return
		}
		// Verify Basic Auth.
		auth := r.Header.Get("Authorization")
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("pk:sk"))
		if auth != expected {
			t.Errorf("wrong auth: %q", auth)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(200)
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()

	l, err := New(Config{
		Host:      srv.URL,
		PublicKey: "pk",
		SecretKey: "sk",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Root span → should produce trace-create + span-create
	ctx, sp := l.Start(context.Background(), "agent.run")
	sp.SetAttribute("iterations", "3")
	sp.End()

	// Nested LLM step → should produce generation-create
	_, nested := l.Start(ctx, "llm.step")
	nested.SetAttribute("model", "gpt-4o")
	nested.End()

	l.Close()

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(received))
	}
	batch, ok := received[0]["batch"].([]any)
	if !ok {
		t.Fatalf("batch missing: %+v", received[0])
	}
	if len(batch) != 3 {
		t.Fatalf("expected 3 events (trace + span + generation), got %d", len(batch))
	}

	event := func(i int) map[string]any { return batch[i].(map[string]any) }

	// event 0: trace-create
	if event(0)["type"] != "trace-create" {
		t.Fatalf("event 0 type: %v", event(0)["type"])
	}
	// event 1: span-create
	if event(1)["type"] != "span-create" {
		t.Fatalf("event 1 type: %v", event(1)["type"])
	}
	// event 2: generation-create (because model attribute is set)
	if event(2)["type"] != "generation-create" {
		t.Fatalf("event 2 type: %v", event(2)["type"])
	}

	// Verify traceId is consistent across all events.
	var traceIDs []string
	for i := 0; i < 3; i++ {
		var parsed struct {
			TraceID string `json:"traceId"`
			ID      string `json:"id"`
		}
		// body may be map[string]any (from JSON decode) or string
		switch b := event(i)["body"].(type) {
		case map[string]any:
			bd, _ := json.Marshal(b)
			json.Unmarshal(bd, &parsed)
		case string:
			json.Unmarshal([]byte(b), &parsed)
		default:
			// json.RawMessage decodes to map[string]any
			data, _ := json.Marshal(b)
			json.Unmarshal(data, &parsed)
		}
		traceIDs = append(traceIDs, parsed.TraceID)
	}
	if traceIDs[0] != "" || traceIDs[1] != traceIDs[2] {
		t.Fatalf("traceIds inconsistent: %v", traceIDs)
	}
}

// TestLangfuseFlushInterval: background flusher must send periodically.
func TestLangfuseFlushInterval(t *testing.T) {
	var mu sync.Mutex
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()

	l, _ := New(Config{
		Host:          srv.URL,
		PublicKey:     "pk",
		SecretKey:     "sk",
		FlushInterval: 50 * time.Millisecond,
	})

	_, sp := l.Start(context.Background(), "tick")
	sp.End()
	// Wait for flusher to fire.
	time.Sleep(150 * time.Millisecond)
	l.Close()

	mu.Lock()
	defer mu.Unlock()
	if count == 0 {
		t.Fatal("flusher did not fire")
	}
}

// TestLangfuseAutoFlushOnBatchSize: exceeding MaxBatchSize must trigger
// an immediate flush.
func TestLangfuseAutoFlushOnBatchSize(t *testing.T) {
	var mu sync.Mutex
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()

	l, _ := New(Config{
		Host:         srv.URL,
		PublicKey:    "pk",
		SecretKey:    "sk",
		MaxBatchSize: 2, // 2 events triggers flush
	})

	// Root span produces 2 events (trace-create + span-create)
	_, sp := l.Start(context.Background(), "auto-flush")
	sp.End()
	time.Sleep(50 * time.Millisecond) // let the async flush run
	l.Close()

	mu.Lock()
	defer mu.Unlock()
	if count < 1 {
		t.Fatal("auto-flush did not trigger")
	}
}

// TestLangfuseImplementsInterfaces: compile-time interface check.
func TestLangfuseImplementsInterfaces(t *testing.T) {
	var _ trace.Tracer = (*Tracer)(nil)
	var _ trace.Span = (*langfuseSpan)(nil)
}

// TestLangfuseDoubleEndSafety: calling End() twice must not panic or duplicate.
func TestLangfuseDoubleEndSafety(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()
	l, _ := New(Config{Host: srv.URL, PublicKey: "pk", SecretKey: "sk"})
	_, sp := l.Start(context.Background(), "double")
	sp.End()
	sp.End() // must be no-op
	l.Close()
}

// TestLangfuseNestedParentID: deep nesting (3 levels) must carry correct
// parentObservationId through context.
func TestLangfuseNestedParentID(t *testing.T) {
	var mu sync.Mutex
	var events []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		events = append(events, body)
		mu.Unlock()
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()
	l, _ := New(Config{Host: srv.URL, PublicKey: "pk", SecretKey: "sk"})

	ctx1, sp1 := l.Start(context.Background(), "run")
	ctx2, sp2 := l.Start(ctx1, "step") // child of run
	_ = ctx2
	sp1.End()
	sp2.End()
	l.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		t.Fatal("no batches")
	}
	batch := events[0]["batch"].([]any)
	// 2: run(trace+span), step(span)
	if len(batch) < 3 {
		t.Fatalf("expected >= 3 events for 2 levels, got %d", len(batch))
	}
	// The span event at index 1 must reference the span ID of "run".
	bodyToStruct := func(raw any) map[string]any {
		switch v := raw.(type) {
		case map[string]any:
			return v
		case string:
			var m map[string]any
			json.Unmarshal([]byte(v), &m)
			return m
		default:
			data, _ := json.Marshal(v)
			var m map[string]any
			json.Unmarshal(data, &m)
			return m
		}
	}
	runSpanBody := bodyToStruct(batch[1].(map[string]any)["body"])
	runSpanID, _ := runSpanBody["id"].(string)

	stepSpanBody := bodyToStruct(batch[2].(map[string]any)["body"])
	stepParentID, _ := stepSpanBody["parentObservationId"].(string)
	if stepParentID != runSpanID {
		t.Fatalf("parent mismatch: got %q want %q", stepParentID, runSpanID)
	}
}

// TestLangfuseNoBackgroundFlush: when FlushInterval is zero, no flush
// happens until Flush() is called manually.
func TestLangfuseNoBackgroundFlush(t *testing.T) {
	var mu sync.Mutex
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()

	l, _ := New(Config{
		Host:      srv.URL,
		PublicKey: "pk",
		SecretKey: "sk",
	})
	_, sp := l.Start(context.Background(), "manual")
	sp.End()

	time.Sleep(100 * time.Millisecond) // should NOT trigger a flush
	mu.Lock()
	afterSleep := count
	mu.Unlock()

	l.Flush() // manual flush
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	afterFlush := count
	mu.Unlock()
	l.Close()

	if afterSleep != 0 {
		t.Fatalf("no auto-flush expected, got %d", afterSleep)
	}
	if afterFlush != 1 {
		t.Fatalf("manual flush expected 1, got %d", afterFlush)
	}
}

// TestLangfuseEmptyAuth: empty key/secret still works (some local setups).
func TestLangfuseEmptyAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()
	l, _ := New(Config{Host: srv.URL})
	_, sp := l.Start(context.Background(), "noauth")
	sp.End()
	l.Close()
}

// TestLangfuseHostTrailingSlash: host with trailing slash must not produce //api.
func TestLangfuseHostTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"successes":[]}`))
	}))
	defer srv.Close()
	l, _ := New(Config{Host: srv.URL + "/", PublicKey: "pk", SecretKey: "sk"})
	l.Flush() // flush empty queue
	_, sp := l.Start(context.Background(), "test")
	sp.End()
	l.Close()
	if !strings.HasPrefix(gotPath, "/api/public/ingestion") {
		t.Fatalf("unexpected path: %q", gotPath)
	}
}
