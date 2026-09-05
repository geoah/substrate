package blobbytes

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

// Postgres reads and writes the `blobs` bytea column, where blob bytes lived
// before the data root existed. It is NOT a runtime choice: config.Blobs
// never builds it and the engine refuses to boot while the column holds a
// row. It survives as the source side of `substratectl blobs migrate --from
// postgres`, which moves a deployment's bytes into the fs or s3 store.
type Postgres struct{}

// NewPostgres returns the migration-source backend over the `blobs` column.
func NewPostgres() *Postgres { return &Postgres{} }

// Name is BackendPostgres.
func (*Postgres) Name() string { return BackendPostgres }

// Repository binds the backend to one repository's scoped pool. The repository
// id is checked and then not used in any statement: every `blobs` row defaults
// its repository column from the connection's `substrate.repository` setting
// and the row level security policy keyed on that setting is what scopes the
// read, so naming the repository in the SQL as well would only add a second
// place to get it wrong.
func (*Postgres) Repository(repository string, db DB) (Store, error) {
	if err := checkRepository(repository); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("blobbytes: the postgres backend needs the repository's scoped pool")
	}
	return &postgresStore{db: db}, nil
}

type postgresStore struct{ db DB }

func (*postgresStore) Backend() string { return BackendPostgres }

// Put writes the bytes on the store's own pool, outside any transaction. The
// engine's write path uses PutTx instead; this is the door the backend
// migration comes in through.
func (p *postgresStore) Put(ctx context.Context, digest string, size int64, r io.Reader) error {
	if err := checkDigest(digest); err != nil {
		return err
	}
	data, err := readExactly(r, size)
	if err != nil {
		return err
	}
	return p.PutTx(ctx, p.db, Blob{Digest: digest, Size: size, Bytes: data})
}

// PutTx inserts the bytes inside the caller's transaction. First bytes win: a
// re-store of the same digest is a no-op, because the digest IS the content.
func (p *postgresStore) PutTx(ctx context.Context, tx DB, b Blob) error {
	if err := checkDigest(b.Digest); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO blobs (digest, name, mime_type, size, bytes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (repository, digest) DO NOTHING`,
		b.Digest, b.Name, b.MediaType, b.Size, b.Bytes)
	if err != nil {
		return fmt.Errorf("blobbytes: store blob bytes: %w", err)
	}
	return nil
}

func (p *postgresStore) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	if err := checkDigest(digest); err != nil {
		return nil, err
	}
	var data []byte
	err := p.db.QueryRowContext(ctx, `SELECT bytes FROM blobs WHERE digest = $1`, digest).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNotStored, digest)
	}
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (p *postgresStore) Exists(ctx context.Context, digest string) (bool, error) {
	return p.ExistsTx(ctx, p.db, digest)
}

func (p *postgresStore) ExistsTx(ctx context.Context, tx DB, digest string) (bool, error) {
	if err := checkDigest(digest); err != nil {
		return false, err
	}
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM blobs WHERE digest = $1`, digest).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (p *postgresStore) Delete(ctx context.Context, digest string) error {
	return p.DeleteTx(ctx, p.db, digest)
}

func (p *postgresStore) DeleteTx(ctx context.Context, tx DB, digest string) error {
	if err := checkDigest(digest); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM blobs WHERE digest = $1`, digest)
	return err
}

func (p *postgresStore) List(ctx context.Context, after string, limit int) ([]Object, error) {
	q := `SELECT digest, size, created_at FROM blobs WHERE digest > $1 ORDER BY digest`
	args := []any{after}
	if limit > 0 {
		q += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Object
	for rows.Next() {
		var o Object
		var at time.Time
		if err := rows.Scan(&o.Digest, &o.Size, &at); err != nil {
			return nil, err
		}
		o.At = at.UTC()
		out = append(out, o)
	}
	return out, rows.Err()
}

// readExactly drains r and refuses a body that is not the promised length: the
// digest is the identity, and a truncated read would store bytes that are not
// the ones the caller hashed.
func readExactly(r io.Reader, size int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("blobbytes: got %d bytes, the caller promised %d", len(data), size)
	}
	return data, nil
}

// Compile-time proof that the default backend still settles in the caller's
// transaction. The engine's one-transaction path keys on this interface.
var _ InTransaction = (*postgresStore)(nil)
