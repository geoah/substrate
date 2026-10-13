package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// failingDriver answers every statement with rows, and the statements that
// read one point per span (the carry and the previous reading, `LIMIT 1`)
// and a recompute's next page (`LIMIT $11`) with rows whose first Next fails,
// as a statement timeout does halfway through a result.
type failingDriver struct{}

func (failingDriver) Open(string) (driver.Conn, error) { return failingConn{}, nil }

type failingConn struct{}

func (failingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not used")
}
func (failingConn) Close() error              { return nil }
func (failingConn) Begin() (driver.Tx, error) { return failingTx{}, nil }
func (failingConn) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (failingConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "LIMIT 1) p") || strings.Contains(query, "LIMIT $11") {
		return &failingRows{err: errors.New("canceling statement due to statement timeout")}, nil
	}
	return &failingRows{}, nil
}

type failingTx struct{}

func (failingTx) Commit() error   { return nil }
func (failingTx) Rollback() error { return nil }

type failingRows struct{ err error }

func (*failingRows) Columns() []string { return []string{"i"} }
func (*failingRows) Close() error      { return nil }
func (r *failingRows) Next([]driver.Value) error {
	if r.err != nil {
		return r.err
	}
	return io.EOF
}

var registerFailingDriver sync.Once

func failingTxn(t *testing.T) *sql.Tx {
	t.Helper()
	registerFailingDriver.Do(func() { sql.Register("substrate-failing-rows", failingDriver{}) })
	db, err := sql.Open("substrate-failing-rows", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

// A result that fails halfway fails the read. database/sql closes rows itself
// when Next fails, so the Close after the loop answers nil; a read that
// trusted it took a carry it never got, or a page it never finished, as
// complete and answered success.
func TestObservedReadsFailWhenAnIterationFails(t *testing.T) {
	ctx := context.Background()
	tx := failingTxn(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	r := &aggRun{ctx: ctx, tx: tx, p: &aggPlan{}, u: &aggUnits{duration: "s"}, meta: aggMeta{sources: map[string]bool{}}}
	if _, err := r.readSeries(&aggStream{form: "counter"}, [][2]time.Time{{start, start.Add(time.Hour)}}); err == nil ||
		!strings.Contains(err.Error(), "statement timeout") {
		t.Errorf("a carry read that failed halfway: err = %v, want the timeout", err)
	}
	if _, err := r.readCarries(&aggStream{form: "gauge"}, [][2]time.Time{{start, start.Add(time.Hour)}}); err == nil {
		t.Error("a gauge carry read that failed halfway answered no error")
	}
	w := &txn{ctx: ctx, tx: tx}
	after := pointKey{ord: -1}
	if _, err := w.readPointsAfter(obsStreamForm{form: "gauge"}, &after, nil, 10, func(scannedPoint) {}); err == nil ||
		!strings.Contains(err.Error(), "statement timeout") {
		t.Errorf("a recompute page that failed halfway: err = %v, want the timeout", err)
	}
}
