package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

var (
	errConnRefused = errors.New("connection refused")
	errIteration   = errors.New("connection lost mid-result")
)

// fakeConnector is a database/sql connector with no server behind it. With
// refuse set, every connect fails, the shape of a transient outage. Without
// it, every query answers one row of two strings and then fails, the shape of
// a connection lost mid-result: database/sql closes the rows itself when Next
// fails, so Close reports nothing and only rows.Err carries the error.
type fakeConnector struct{ refuse bool }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) {
	if c.refuse {
		return nil, errConnRefused
	}
	return fakeConn{}, nil
}

func (c fakeConnector) Driver() driver.Driver { return fakeDriver{c} }

type fakeDriver struct{ c fakeConnector }

func (d fakeDriver) Open(string) (driver.Conn, error) { return d.c.Connect(context.Background()) }

type fakeConn struct{}

func (fakeConn) Prepare(string) (driver.Stmt, error) { return fakeStmt{}, nil }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("fake: no transactions") }

type fakeStmt struct{}

func (fakeStmt) Close() error  { return nil }
func (fakeStmt) NumInput() int { return -1 }
func (fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("fake: no writes")
}
func (fakeStmt) Query([]driver.Value) (driver.Rows, error) { return &fakeRows{}, nil }

type fakeRows struct{ sent bool }

func (*fakeRows) Columns() []string { return []string{"a", "b"} }
func (*fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.sent {
		return errIteration
	}
	r.sent = true
	dest[0], dest[1] = "a.example.substrate.reamde.dev/p/k", "body"
	return nil
}

// A read that fails for any reason but a missing row must not plan a skip:
// commitEmbedding deletes the queue row of a skipped job, so the property
// would not embed again until its text changed.
func TestComputeEmbeddingKeepsQueueRowOnReadError(t *testing.T) {
	t.Parallel()
	db := sql.OpenDB(fakeConnector{refuse: true})
	defer db.Close()
	ds := &dataset{db: db}
	plan, err := ds.computeEmbedding(context.Background(), nil, eref{Kind: "a.example.substrate.reamde.dev/p/k", ID: "x"}, "body")
	if !errors.Is(err, errConnRefused) || plan != nil {
		t.Fatalf("plan = %+v, err = %v: a failed read must return its error and no plan", plan, err)
	}
}

// The prune deletes what the listing names, so a listing cut short by a lost
// connection must fail the prune, not pass as the complete set.
func TestPruneUnembeddableReportsAnIterationError(t *testing.T) {
	t.Parallel()
	db := sql.OpenDB(fakeConnector{})
	defer db.Close()
	err := pruneUnembeddable(context.Background(), db, vocabulary.NewRegistry(), nil)
	if !errors.Is(err, errIteration) {
		t.Fatalf("pruneUnembeddable = %v, want the iteration error", err)
	}
}
