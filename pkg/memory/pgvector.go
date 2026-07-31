package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// PGVectorStore is a VectorStore backed by a Postgres table with the
// pgvector extension (vector type + cosine distance operator <=>).
//
// It takes any *sql.DB, so you bring your own driver (pgx, lib/pq, ...):
//
//	store := memory.NewPGVectorStore(db, "documents", 1536)
//	if err := store.EnsureSchema(ctx); err != nil { ... }
//
// The table is NOT auto-created on Add/Search; call EnsureSchema once (or
// create it manually) so construction can stay side-effect free.
type PGVectorStore struct {
	db       *sql.DB
	table    string
	dims     int
	tableSQL string // quoted, sanitized at construction
}

// NewPGVectorStore wraps an existing *sql.DB. dims must match the
// dimensionality of the embeddings you store.
func NewPGVectorStore(db *sql.DB, table string, dims int) *PGVectorStore {
	if db == nil {
		panic("memory: NewPGVectorStore requires a non-nil *sql.DB")
	}
	if table == "" {
		panic("memory: NewPGVectorStore requires a table name")
	}
	return &PGVectorStore{
		db:       db,
		table:    table,
		dims:     dims,
		tableSQL: quoteIdent(table),
	}
}

// EnsureSchema creates the table if it does not exist. Requires the
// pgvector extension to be installed in the database.
func (p *PGVectorStore) EnsureSchema(ctx context.Context) error {
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id      text PRIMARY KEY,
	meta    jsonb NOT NULL DEFAULT '{}'::jsonb,
	embedding vector(%d)
)`, p.tableSQL, p.dims)
	_, err := p.db.ExecContext(ctx, q)
	return err
}

// Add inserts (or upserts) a point. Meta is stored as a JSONB column.
func (p *PGVectorStore) Add(ctx context.Context, id string, vector []float32, meta map[string]string) error {
	if len(vector) != p.dims {
		return fmt.Errorf("pgvector: vector dims %d, store expects %d", len(vector), p.dims)
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	q := fmt.Sprintf(`INSERT INTO %s (id, meta, embedding) VALUES ($1, $2::jsonb, $3::vector)
ON CONFLICT (id) DO UPDATE SET meta = EXCLUDED.meta, embedding = EXCLUDED.embedding`, p.tableSQL)
	_, err = p.db.ExecContext(ctx, q, id, metaJSON, vecString(vector))
	return err
}

// Search runs a cosine-distance scan (<=>) and returns the k nearest
// neighbors with similarity scores in [0,1].
func (p *PGVectorStore) Search(ctx context.Context, vector []float32, k int) ([]VectorHit, error) {
	q := fmt.Sprintf(`SELECT id, meta, 1 - (embedding <=> $1::vector) AS score
FROM %s
ORDER BY embedding <=> $1::vector
LIMIT $2`, p.tableSQL)
	rows, err := p.db.QueryContext(ctx, q, vecString(vector), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := []VectorHit{}
	for rows.Next() {
		var (
			id       string
			metaJSON string
			score    float32
		)
		if err := rows.Scan(&id, &metaJSON, &score); err != nil {
			return nil, err
		}
		h := VectorHit{ID: id, Score: score}
		_ = json.Unmarshal([]byte(metaJSON), &h.Meta)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// quoteIdent double-quotes a Postgres identifier and rejects anything that
// could break out of the quoting (injection guard for the table name).
func quoteIdent(name string) string {
	if name == "" || strings.ContainsAny(name, `"`+"\x00") {
		panic("memory: unsafe table name for pgvector store")
	}
	return `"` + name + `"`
}

// vecString renders a []float32 as the literal pgvector accepts:
// '[0.1,0.2,0.3]'. Values are printed with 7 significant digits, which is
// plenty for float32.
func vecString(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%.7g", f)
	}
	b.WriteByte(']')
	return b.String()
}

var _ VectorStore = (*PGVectorStore)(nil)
