package memory

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"testing"
)

// fakeDB is a minimal database/sql driver that understands the three SQL
// shapes PGVectorStore emits, so the adapter can be tested without Postgres.
type fakeDB struct {
	lastQuery string
	lastArgs  []driver.Value
	row       []driver.Value
}

func (f *fakeDB) Open(string) (driver.Conn, error) { return f, nil }

func (f *fakeDB) Connect(context.Context) (driver.Conn, error) { return f, nil }
func (f *fakeDB) Driver() driver.Driver                        { return f }

func (f *fakeDB) Prepare(string) (driver.Stmt, error) { panic("unexpected Prepare") }
func (f *fakeDB) Close() error                        { return nil }
func (f *fakeDB) Begin() (driver.Tx, error)           { panic("unexpected Begin") }

func (f *fakeDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	f.lastQuery = query
	f.lastArgs = toValues(args)
	if strings.HasPrefix(strings.TrimSpace(query), "CREATE TABLE") {
		return fakeResult{}, nil
	}
	if strings.HasPrefix(strings.TrimSpace(query), "INSERT") {
		return fakeResult{}, nil
	}
	panic("unexpected exec: " + query)
}

func (f *fakeDB) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	f.lastQuery = query
	f.lastArgs = toValues(args)
	return &fakeRows{row: f.row}, nil
}

func toValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

type fakeRows struct {
	row []driver.Value
	got bool
}

func (r *fakeRows) Columns() []string { return []string{"id", "meta", "score"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.got {
		return io.EOF
	}
	r.got = true
	copy(dest, r.row)
	return nil
}

func TestPGVectorStoreAddAndSearch(t *testing.T) {
	f := &fakeDB{row: []driver.Value{"doc-1", `{"content":"reset password"}`, float64(0.92)}}
	db := sql.OpenDB(f)

	p := NewPGVectorStore(db, "documents", 3)
	if err := p.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.lastQuery, `CREATE TABLE IF NOT EXISTS "documents"`) {
		t.Fatalf("bad schema SQL: %s", f.lastQuery)
	}

	if err := p.Add(context.Background(), "doc-1", []float32{0.1, 0.2, 0.3}, map[string]string{"content": "reset password"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.lastQuery, `INSERT INTO "documents"`) {
		t.Fatalf("bad insert SQL: %s", f.lastQuery)
	}
	if got := f.lastArgs[2].(string); got != "[0.1,0.2,0.3]" {
		t.Fatalf("vector arg = %q, want pgvector literal", got)
	}

	hits, err := p.Search(context.Background(), []float32{0.1, 0.2, 0.3}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "doc-1" || hits[0].Score != 0.92 {
		t.Fatalf("wrong hit: %+v", hits)
	}
	if hits[0].Meta["content"] != "reset password" {
		t.Fatalf("meta not decoded: %+v", hits[0].Meta)
	}
}

func TestPGVectorStoreRejectsWrongDims(t *testing.T) {
	p := NewPGVectorStore(sql.OpenDB(&fakeDB{}), "docs", 3)
	if err := p.Add(context.Background(), "x", []float32{1, 2}, nil); err == nil {
		t.Fatal("must reject wrong dims")
	}
}

func TestPGVectorStoreRejectsUnsafeTable(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("must panic on unsafe table name")
		}
	}()
	NewPGVectorStore(sql.OpenDB(&fakeDB{}), `tbl"; DROP TABLE x; --`, 3)
}

func TestPGVectorStoreQuotesTable(t *testing.T) {
	if got := quoteIdent("my.docs"); got != `"my.docs"` {
		t.Fatalf("quoteIdent = %q", got)
	}
	if got := quoteIdent("mixed Case"); got != `"mixed Case"` {
		t.Fatalf("quoteIdent = %q", got)
	}
}
