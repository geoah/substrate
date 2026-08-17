package blobbytes_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/testdb"
)

// blobsDDL is the shipped `blobs` table, minus the row level security the
// engine's own suite owns. The backend's statements never name the repository
// column: the connection's setting fills it and the policy scopes the read, so
// what is exercised here is the byte handling.
const blobsDDL = `
CREATE TABLE blobs (
    repository text        NOT NULL DEFAULT 'repotest',
    digest     text        NOT NULL,
    mime_type  text        NOT NULL DEFAULT '',
    name       text        NOT NULL DEFAULT '',
    size       bigint      NOT NULL,
    bytes      bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, digest)
)`

func newPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testdb.NewSchema(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(context.Background(), blobsDDL); err != nil {
		t.Fatalf("create the blobs table: %v", err)
	}
	return db
}

func TestPostgresStore(t *testing.T) {
	t.Parallel()
	db := newPostgresDB(t)
	b := blobbytes.NewPostgres()
	conformance(t, func(t *testing.T, repository string) blobbytes.Store {
		t.Helper()
		// One table per subtest, because this fixture has no row level
		// security to keep two repositories apart — that is the engine's
		// isolation suite, not this one.
		s, err := b.Repository(repository, db)
		if err != nil {
			t.Fatalf("bind %s: %v", repository, err)
		}
		t.Cleanup(func() {
			if _, err := db.ExecContext(context.Background(), `DELETE FROM blobs`); err != nil {
				t.Fatalf("clear the blobs table: %v", err)
			}
		})
		return s
	})
	refuseBadRepository(t, b, db)
}

// The postgres backend is the one that can settle bytes and manifest in a
// single transaction, and the engine's write path keys on exactly that
// interface. A refactor that drops it would silently move every deployment
// onto the two-step path.
func TestPostgresSettlesInTheCallersTransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newPostgresDB(t)
	s, err := blobbytes.NewPostgres().Repository("repotest", db)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	tx, ok := s.(blobbytes.InTransaction)
	if !ok {
		t.Fatal("the postgres store no longer settles inside the caller's transaction")
	}

	data := []byte("bytes that roll back with their transaction")
	digest := digestOf(data)
	dbTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.PutTx(ctx, dbTx, blobbytes.Blob{
		Digest: digest, Name: "note.txt", MimeType: "text/plain",
		Size: int64(len(data)), Bytes: data,
	}); err != nil {
		t.Fatalf("put in transaction: %v", err)
	}
	// Inside the transaction the bytes are already there, which is what the
	// engine's "a manifest is stored only once its bytes exist" guard reads.
	held, err := tx.ExistsTx(ctx, dbTx, digest)
	if err != nil || !held {
		t.Fatalf("ExistsTx inside the transaction: %v %v", held, err)
	}
	// Outside it, they are not.
	held, err = s.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("exists outside: %v", err)
	}
	if held {
		t.Fatal("an uncommitted put is visible outside its transaction")
	}
	if err := dbTx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	held, err = s.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("exists after rollback: %v", err)
	}
	if held {
		t.Fatal("the bytes survived the rollback of the transaction that wrote them")
	}
}

// The name and mime type columns predate the manifest carrying either, and a
// put still fills them: an existing deployment's rows keep their shape.
func TestPostgresKeepsTheRowShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newPostgresDB(t)
	s, err := blobbytes.NewPostgres().Repository("repotest", db)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	data := []byte("a named blob")
	digest := digestOf(data)
	if err := s.(blobbytes.InTransaction).PutTx(ctx, db, blobbytes.Blob{
		Digest: digest, Name: "report.pdf", MimeType: "application/pdf",
		Size: int64(len(data)), Bytes: data,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	var name, mime string
	var size int64
	if err := db.QueryRowContext(ctx,
		`SELECT name, mime_type, size FROM blobs WHERE digest = $1`, digest).
		Scan(&name, &mime, &size); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if name != "report.pdf" || mime != "application/pdf" || size != int64(len(data)) {
		t.Fatalf("row holds (%q, %q, %d)", name, mime, size)
	}
}
