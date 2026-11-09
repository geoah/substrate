package store

import (
	"context"
	"database/sql"
	"errors"
)

// AccountRef maps one of the server's account records to the login the
// bridge runs. A deleted record cannot be read, so this map is how the
// bridge learns which login a deletion names.
type AccountRef struct {
	RecordID string
	Network  string
	LoginID  string
	// Settings are the owner's toggles as last read, as JSON.
	Settings string
	Version  int64
	// Instance is the record id of the instance row the account names, the
	// instance that runs it.
	Instance string
}

// PutAccountRef records what an account record names.
func (s *Store) PutAccountRef(ctx context.Context, a AccountRef) error {
	if a.Settings == "" {
		a.Settings = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO accounts (record_id, network, login_id, settings, version, instance) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (record_id) DO UPDATE SET network = excluded.network, login_id = excluded.login_id,
			settings = excluded.settings, version = excluded.version, instance = excluded.instance`,
		a.RecordID, a.Network, a.LoginID, a.Settings, a.Version, a.Instance)
	if err != nil {
		// Another record named the same login (a re-created account): the
		// newer record wins the login.
		if _, err2 := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE network = ? AND login_id = ? AND record_id != ?`,
			a.Network, a.LoginID, a.RecordID); err2 != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO accounts (record_id, network, login_id, settings, version, instance) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (record_id) DO UPDATE SET network = excluded.network, login_id = excluded.login_id,
				settings = excluded.settings, version = excluded.version, instance = excluded.instance`,
			a.RecordID, a.Network, a.LoginID, a.Settings, a.Version, a.Instance)
	}
	return err
}

func scanAccountRef(row *sql.Row) (*AccountRef, error) {
	var a AccountRef
	err := row.Scan(&a.RecordID, &a.Network, &a.LoginID, &a.Settings, &a.Version, &a.Instance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AccountByRecord reads the login an account record names.
func (s *Store) AccountByRecord(ctx context.Context, recordID string) (*AccountRef, error) {
	return scanAccountRef(s.db.QueryRowContext(ctx,
		`SELECT record_id, network, login_id, settings, version, instance FROM accounts WHERE record_id = ?`, recordID))
}

// AccountByLogin reads the record that names a login.
func (s *Store) AccountByLogin(ctx context.Context, network, loginID string) (*AccountRef, error) {
	return scanAccountRef(s.db.QueryRowContext(ctx,
		`SELECT record_id, network, login_id, settings, version, instance FROM accounts WHERE network = ? AND login_id = ?`, network, loginID))
}

// DeleteAccountRef forgets an account record.
func (s *Store) DeleteAccountRef(ctx context.Context, recordID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE record_id = ?`, recordID)
	return err
}

// The kinds of record a ref caches.
const (
	RefChat    = "chat"
	RefMessage = "message"
)

// Ref is what a chat or message record names on the network. A record's
// row id is the server's composition, which the bridge cannot compute, so
// it learns it by reading the record once.
type Ref struct {
	Network   string
	LoginID   string
	ChatID    string
	MessageID string
}

// PutRef caches what a record names.
func (s *Store) PutRef(ctx context.Context, kind, recordID string, r Ref) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO refs (kind, record_id, network, login_id, chat_id, message_id) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, record_id) DO UPDATE SET network = excluded.network, login_id = excluded.login_id,
			chat_id = excluded.chat_id, message_id = excluded.message_id`,
		kind, recordID, r.Network, r.LoginID, r.ChatID, r.MessageID)
	return err
}

// GetRef reads a cached ref, nil when the record was never read.
func (s *Store) GetRef(ctx context.Context, kind, recordID string) (*Ref, error) {
	var r Ref
	err := s.db.QueryRowContext(ctx, `SELECT network, login_id, chat_id, message_id FROM refs WHERE kind = ? AND record_id = ?`, kind, recordID).
		Scan(&r.Network, &r.LoginID, &r.ChatID, &r.MessageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// AccountRefs reads the whole account map.
func (s *Store) AccountRefs(ctx context.Context) ([]AccountRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT record_id, network, login_id, settings, version, instance FROM accounts ORDER BY record_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountRef
	for rows.Next() {
		var a AccountRef
		if err := rows.Scan(&a.RecordID, &a.Network, &a.LoginID, &a.Settings, &a.Version, &a.Instance); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
