// Package store is the bridge's own SQLite database, beside the one each
// network's bridgev2 keeps: the outbox and its sequence, the media the outbox
// names, the claimed actions, the map from the server's account records to
// the bridge's logins, and the mirror the local Matrix side folds events
// from. It holds session keys only indirectly (the network databases sit in
// the same directory), so the directory is 0700.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	// Registers sqlite3-fk-wal: foreign keys, WAL and a busy timeout on every
	// connection, the driver the mautrix bridges themselves run on.
	_ "go.mau.fi/util/dbutil/litestream"
)

// Driver is the database/sql driver every database of the bridge opens with.
const Driver = "sqlite3-fk-wal"

// DSN is the data source for a database file. Immediate transactions take
// the write lock at BEGIN, so two writers never deadlock upgrading a read.
func DSN(path string) string {
	return "file:" + path + "?_txlock=immediate"
}

// Store is the bridge's database and the media directory beside it.
type Store struct {
	db       *sql.DB
	dir      string
	mediaDir string
	now      func() time.Time

	notifyMu sync.Mutex
	notify   []chan struct{}
}

// Open opens (creating) the store in dir. The directory and the media
// directory are made 0700.
func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating the store directory: %w", err)
	}
	// MkdirAll leaves an existing directory's mode alone; the store holds
	// what links every network, so it is never readable by others.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("restricting the store directory: %w", err)
	}
	mediaDir := filepath.Join(dir, "media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating the media directory: %w", err)
	}
	db, err := sql.Open(Driver, DSN(filepath.Join(dir, "bridge.db")))
	if err != nil {
		return nil, err
	}
	// One connection: every write is a short transaction, and one connection
	// is what keeps the outbox's sequence free of gaps without a busy retry.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, dir: dir, mediaDir: mediaDir, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Dir is the store's directory, where the network databases live too.
func (s *Store) Dir() string { return s.dir }

// SetClock replaces the clock, for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Now is the store's clock.
func (s *Store) Now() time.Time { return s.now() }

// migrations run in order; each runs once. A landed one is never edited.
var migrations = []string{
	`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE outbox (
		seq INTEGER PRIMARY KEY,
		type TEXT NOT NULL,
		account TEXT NOT NULL DEFAULT '',
		body BLOB NOT NULL,
		media TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	);
	CREATE TABLE media (
		id TEXT PRIMARY KEY,
		mime_type TEXT NOT NULL,
		file_name TEXT NOT NULL,
		size INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE claims (
		action TEXT PRIMARY KEY,
		attempt TEXT NOT NULL,
		version INTEGER NOT NULL,
		digest TEXT NOT NULL,
		claimed INTEGER NOT NULL,
		network TEXT NOT NULL,
		account TEXT NOT NULL,
		kind TEXT NOT NULL,
		request TEXT NOT NULL,
		txn TEXT NOT NULL,
		claimed_at INTEGER NOT NULL,
		dispatched_at INTEGER,
		outcome TEXT,
		outcome_at INTEGER,
		result_message TEXT,
		error_code TEXT,
		error_message TEXT
	);
	CREATE TABLE accounts (
		record_id TEXT PRIMARY KEY,
		network TEXT NOT NULL,
		login_id TEXT NOT NULL,
		settings TEXT NOT NULL DEFAULT '{}',
		version INTEGER NOT NULL DEFAULT 0,
		instance TEXT NOT NULL DEFAULT '',
		UNIQUE (network, login_id)
	);
	CREATE TABLE refs (
		kind TEXT NOT NULL,
		record_id TEXT NOT NULL,
		network TEXT NOT NULL,
		login_id TEXT NOT NULL,
		chat_id TEXT NOT NULL,
		message_id TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (kind, record_id)
	);
	CREATE TABLE rooms (
		room_id TEXT PRIMARY KEY,
		network TEXT NOT NULL,
		portal_id TEXT NOT NULL,
		receiver TEXT NOT NULL,
		pinned INTEGER,
		archived INTEGER,
		muted_until INTEGER,
		marked_unread INTEGER,
		deleted INTEGER NOT NULL DEFAULT 0,
		announced INTEGER NOT NULL DEFAULT 0,
		UNIQUE (network, portal_id, receiver)
	);
	CREATE TABLE members (
		room_id TEXT NOT NULL,
		mxid TEXT NOT NULL,
		user_id TEXT NOT NULL,
		membership TEXT NOT NULL,
		power INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (room_id, mxid)
	);
	CREATE TABLE parts (
		event_id TEXT PRIMARY KEY,
		room_id TEXT NOT NULL,
		message_id TEXT NOT NULL,
		part_id TEXT NOT NULL,
		idx INTEGER NOT NULL,
		sender TEXT NOT NULL,
		from_owner INTEGER NOT NULL,
		ts INTEGER NOT NULL,
		content TEXT NOT NULL,
		edit_count INTEGER NOT NULL DEFAULT 0,
		edited_at INTEGER,
		deleted TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL DEFAULT '',
		UNIQUE (room_id, message_id, part_id)
	);
	CREATE INDEX parts_message ON parts (room_id, message_id);
	CREATE INDEX parts_room_ts ON parts (room_id, ts);
	CREATE TABLE reactions (
		event_id TEXT PRIMARY KEY,
		room_id TEXT NOT NULL,
		message_id TEXT NOT NULL,
		sender TEXT NOT NULL,
		emoji_id TEXT NOT NULL,
		reaction_key TEXT NOT NULL,
		ts INTEGER NOT NULL
	);
	CREATE INDEX reactions_message ON reactions (room_id, message_id);
	CREATE TABLE users_seen (
		network TEXT NOT NULL,
		account TEXT NOT NULL,
		user_id TEXT NOT NULL,
		digest TEXT NOT NULL,
		PRIMARY KEY (network, account, user_id)
	);
	CREATE TABLE sends (
		account TEXT NOT NULL,
		chat TEXT NOT NULL,
		text_digest TEXT NOT NULL,
		at INTEGER NOT NULL
	);
	CREATE INDEX sends_account_at ON sends (account, at);
	CREATE TABLE ghosts (
		network TEXT NOT NULL,
		user_id TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		avatar TEXT NOT NULL DEFAULT '',
		identifiers TEXT NOT NULL DEFAULT '[]',
		is_bot INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (network, user_id)
	);`,
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("creating the schema version table: %w", err)
	}
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return err
		}
	case err != nil:
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("the store is at schema %d, newer than this build knows (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, i+1); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Tx is one transaction. Events appended in it get their sequence numbers
// in it, so a rolled-back transaction leaves no gap.
type Tx struct {
	tx       *sql.Tx
	s        *Store
	appended bool
}

// Do runs fn in one transaction and commits it unless fn fails.
func (s *Store) Do(ctx context.Context, fn func(*Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	tx := &Tx{tx: sqlTx, s: s}
	if err := fn(tx); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return err
	}
	if tx.appended {
		s.signal()
	}
	return nil
}

// Changed returns a channel that receives after every commit that appended
// events. It is buffered by one, so a slow reader sees one wakeup for many
// commits.
func (s *Store) Changed() <-chan struct{} {
	ch := make(chan struct{}, 1)
	s.notifyMu.Lock()
	s.notify = append(s.notify, ch)
	s.notifyMu.Unlock()
	return ch
}

func (s *Store) signal() {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	for _, ch := range s.notify {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Get reads a key.
func (s *Store) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// Set writes a key.
func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kv (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Delete removes a key.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, key)
	return err
}

// GetInt reads an integer key, 0 when absent.
func (s *Store) GetInt(ctx context.Context, key string) (int64, error) {
	v, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// SetInt writes an integer key.
func (s *Store) SetInt(ctx context.Context, key string, v int64) error {
	return s.Set(ctx, key, strconv.FormatInt(v, 10))
}

// The keys the bridge keeps.
const (
	KeyInstance   = "instance"
	KeyOutboxNext = "outbox.next"
	KeyApplied    = "push.applied"
	KeyInflight   = "push.inflight"
	KeyAuth       = "auth"
	KeyFeed       = "feed.bookmark"
	KeyInstanceID = "instance.record"
)

// InstanceID returns this store's instance id, minting it on first use. A
// new store is a new instance: its stream starts at 1 under the new id.
func (s *Store) InstanceID(ctx context.Context) (string, error) {
	id, ok, err := s.Get(ctx, KeyInstance)
	if err != nil {
		return "", err
	}
	if ok {
		return id, nil
	}
	id, err = newUUID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING`, KeyInstance, id)
	if err != nil {
		return "", err
	}
	id, _, err = s.Get(ctx, KeyInstance)
	return id, err
}

// newUUID is a random (version 4) UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// RandomID is a random hex id of n bytes.
func RandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
