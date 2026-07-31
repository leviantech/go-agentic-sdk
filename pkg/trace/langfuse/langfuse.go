// Package langfuse sends agent traces to a Langfuse instance via the
// public REST ingestion API. It is dependency-free: the adapter speaks
// plain HTTP with Basic Auth and batches events in-memory before flushing.
//
// Usage:
//
//	l, err := langfuse.New(langfuse.Config{
//	    Host:        os.Getenv("LANGFUSE_HOST"),
//	    PublicKey:   os.Getenv("LANGFUSE_PUBLIC_KEY"),
//	    SecretKey:   os.Getenv("LANGFUSE_SECRET_KEY"),
//	    FlushInterval: 2 * time.Second, // optional
//	})
//	defer l.Close()
//
//	a, _ := agent.NewAgent(agent.WithLLM(...), agent.WithTracer(l))
package langfuse

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/leviantech/go-agentic-sdk/pkg/trace"
)

// Config holds Langfuse connection parameters. Host, PublicKey and
// SecretKey are required; the rest are optional.
type Config struct {
	// Host is the Langfuse base URL, e.g. "https://cloud.langfuse.com".
	// No trailing slash.
	Host string
	// PublicKey and SecretKey for HTTP Basic Auth.
	PublicKey string
	SecretKey string
	// FlushInterval controls the background flusher. Zero means no
	// background flush — call Flush() manually.
	FlushInterval time.Duration
	// HTTPClient is optional; nil uses http.DefaultClient.
	HTTPClient *http.Client
	// MaxBatchSize: when the queue reaches this count, a flush is
	// triggered immediately. Zero uses a sensible default (100).
	MaxBatchSize int
}

// Tracer sends Langfuse-compatible traces via the ingestion API.
// Implements trace.Tracer.
type Tracer struct {
	host   string
	auth   string // "Basic base64(publicKey:secretKey)"
	client *http.Client

	mu      sync.Mutex
	queue   []ingestionEvent
	maxSize int

	cancelFlush context.CancelFunc
}

// ingestionEvent is one Langfuse ingestion batch item.
type ingestionEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Body      json.RawMessage `json:"body"`
}

// New creates a Tracer backed by Langfuse. Call Close() to flush and stop.
func New(cfg Config) (*Tracer, error) {
	if cfg.Host == "" {
		cfg.Host = "https://cloud.langfuse.com"
	}
	cfg.Host = strings.TrimSuffix(cfg.Host, "/")
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = 100
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	auth := basicAuth(cfg.PublicKey, cfg.SecretKey)

	t := &Tracer{
		host:    cfg.Host,
		auth:    auth,
		client:  client,
		maxSize: cfg.MaxBatchSize,
	}
	if cfg.FlushInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		t.cancelFlush = cancel
		go t.flushLoop(ctx, cfg.FlushInterval)
	}
	return t, nil
}

// Start begins a new Langfuse trace. The root span (agent.run) creates
// both a trace-create and a span-create event; nested spans create only
// a span-create (parent-child is tracked via parentObservationId in ctx).
func (t *Tracer) Start(ctx context.Context, name string) (context.Context, trace.Span) {
	traceID := idFromCtx(ctx)
	spanID := uuid()
	now := nowISO()

	// If the parent already established a Langfuse trace, create a child
	// span; otherwise this is the root and we also create the trace.
	if traceID == "" {
		traceID = uuid()
		t.enqueue("trace-create", map[string]any{
			"id":        traceID,
			"name":      name,
			"timestamp": now,
		})
		ctx = withTraceID(ctx, traceID)
	}

	parentID := parentIDFromCtx(ctx)
	sp := &langfuseSpan{
		t:       t,
		traceID: traceID,
		spanID:  spanID,
		name:    name,
		start:   now,
		parent:  parentID,
	}
	// SetAttribute may be called before End; the creation event is
	// emitted at End so we have the final attribute set.
	return withSpanID(ctx, spanID), sp
}

// Close flushes remaining events and stops the background flusher.
func (t *Tracer) Close() {
	if t.cancelFlush != nil {
		t.cancelFlush()
	}
	t.Flush()
}

// Flush sends all buffered events to Langfuse immediately.
func (t *Tracer) Flush() {
	t.mu.Lock()
	events := t.queue
	t.queue = nil
	t.mu.Unlock()
	if len(events) == 0 {
		return
	}
	body := map[string]any{"batch": events}
	data, _ := json.Marshal(body)

	req, err := http.NewRequest(http.MethodPost, t.host+"/api/public/ingestion", bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", t.auth)
	resp, err := t.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

func (t *Tracer) enqueue(eventType string, body any) {
	data, _ := json.Marshal(body)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.queue = append(t.queue, ingestionEvent{
		ID:        uuid(),
		Type:      eventType,
		Timestamp: nowISO(),
		Body:      data,
	})
	if len(t.queue) >= t.maxSize {
		go t.Flush()
	}
}

func (t *Tracer) flushLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.Flush()
		}
	}
}

// langfuseSpan implements trace.Span backed by Langfuse ingestion events.
type langfuseSpan struct {
	t       *Tracer
	traceID string
	spanID  string
	name    string
	start   string
	parent  string
	attrs   map[string]string
	done    bool
}

func (s *langfuseSpan) SetAttribute(key, value string) {
	if s.done {
		return
	}
	if s.attrs == nil {
		s.attrs = map[string]string{}
	}
	s.attrs[key] = value
}

func (s *langfuseSpan) End() {
	if s.done {
		return
	}
	s.done = true
	now := nowISO()
	body := map[string]any{
		"id":        s.spanID,
		"traceId":   s.traceID,
		"name":      s.name,
		"startTime": s.start,
		"endTime":   now,
	}
	if s.parent != "" {
		body["parentObservationId"] = s.parent
	}
	if len(s.attrs) > 0 {
		body["metadata"] = s.attrs
	}
	// Heuristic: LLM steps contain model name → send as generation.
	if _, isLLM := s.attrs["model"]; isLLM {
		s.t.enqueue("generation-create", body)
	} else {
		s.t.enqueue("span-create", body)
	}
}

// --- context helpers (carry Langfuse trace/span IDs) ---

type langfuseTraceIDKey struct{}
type langfuseSpanIDKey struct{}

func withTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, langfuseTraceIDKey{}, id)
}
func withSpanID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, langfuseSpanIDKey{}, id)
}
func idFromCtx(ctx context.Context) string {
	if id, _ := ctx.Value(langfuseTraceIDKey{}).(string); id != "" {
		return id
	}
	return ""
}
func parentIDFromCtx(ctx context.Context) string {
	if id, _ := ctx.Value(langfuseSpanIDKey{}).(string); id != "" {
		return id
	}
	return ""
}

var _ trace.Tracer = (*Tracer)(nil)
var _ trace.Span = (*langfuseSpan)(nil)

// --- helpers ---

func uuid() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func basicAuth(user, pass string) string {
	// Encode "user:pass" as base64 for the Basic header.
	enc := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	return "Basic " + enc
}
