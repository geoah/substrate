package engine

// The host credential store, ported from the (now retired) connectors
// service's `credentials` table: provider tokens
// live in an engine table per repository schema, addressed by opaque REFS the
// account records carry as secret-typed properties — a read surface never
// sees a raw token, and the store never sees the wire. Payloads seal with
// AES-GCM when the service has a credential key (WithCredentialKey);
// without one they store plain, loudly, so a dev database stays inspectable
// and production is expected to set the key.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Payload framing: one marker byte, then the JSON (plain) or
// nonce||ciphertext (sealed).
const (
	credPlain  byte = 'p'
	credSealed byte = 's'
)

// sealToken renders one token as its sealed row payload plus the
// denormalized expiry column value.
func (ds *dataset) sealToken(tok *oauth2.Token) (payload []byte, expires any, err error) {
	raw, err := json.Marshal(tok)
	if err != nil {
		return nil, nil, err
	}
	payload, err = ds.svc.sealCredential(raw)
	if err != nil {
		return nil, nil, err
	}
	if !tok.Expiry.IsZero() {
		expires = tok.Expiry.UTC()
	}
	return payload, expires, nil
}

// putCredential upserts one record's token under a ref, INSIDE the caller's
// transaction — the callback stores the credential and patches the account
// in one commit, so a half-connected account can never hold an unreferenced
// credential. Refresh never comes here: refreshed tokens
// persist through updateCredential's update-only compare-and-swap.
func (t *txn) putCredential(ref string, account eref, tok *oauth2.Token) error {
	payload, expires, err := t.ds.sealToken(tok)
	if err != nil {
		return err
	}
	_, err = t.exec(`
		INSERT INTO sealed (ref, record_kind, record_id, payload, expires_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (repository, ref) DO UPDATE
		    SET record_kind = EXCLUDED.record_kind, record_id = EXCLUDED.record_id, payload = EXCLUDED.payload,
		        expires_at = EXCLUDED.expires_at, updated_at = now()`,
		ref, account.Kind, account.ID, payload, expires)
	if err != nil {
		return fmt.Errorf("substrate/engine: put credential: %w", err)
	}
	return nil
}

// updateCredential persists a refreshed token UPDATE-ONLY: the row must
// still exist, still belong to the same LIVE account, and still carry the
// updated_at generation the refresh read its input token at. A refresh that
// lost to teardown (finalizer delete) or to a concurrent reconnect writes
// nothing — an upsert here is how a slow provider call used to resurrect an
// orphan credential for a deleted account. Returns
// whether the swap landed.
func (ds *dataset) updateCredential(ctx context.Context, ref string, account eref, tok *oauth2.Token, seen time.Time) (bool, error) {
	payload, expires, err := ds.sealToken(tok)
	if err != nil {
		return false, err
	}
	res, err := ds.db.ExecContext(ctx, `
		UPDATE sealed SET payload = $4, expires_at = $5, updated_at = now()
		WHERE ref = $1 AND record_kind = $2 AND record_id = $3 AND updated_at = $6
		  AND EXISTS (SELECT 1 FROM records e WHERE e.kind = $2 AND e.id = $3 AND e.deleted_at IS NULL)`,
		ref, account.Kind, account.ID, payload, expires, seen)
	if err != nil {
		return false, fmt.Errorf("substrate/engine: update credential: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// getCredential resolves a ref to its token, owning record and updated_at
// generation (updateCredential's compare-and-swap anchor).
func (ds *dataset) getCredential(ctx context.Context, ref string) (*oauth2.Token, eref, time.Time, error) {
	var payload []byte
	var account eref
	var updatedAt time.Time
	err := ds.db.QueryRowContext(ctx,
		`SELECT payload, record_kind, record_id, updated_at FROM sealed WHERE ref = $1`, ref).
		Scan(&payload, &account.Kind, &account.ID, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, eref{}, time.Time{}, fmt.Errorf("%w: credential %s", errCredentialGone, ref)
	}
	if err != nil {
		return nil, eref{}, time.Time{}, err
	}
	raw, err := ds.svc.openCredential(payload)
	if err != nil {
		return nil, eref{}, time.Time{}, fmt.Errorf("substrate/engine: open credential %s: %w", ref, err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, eref{}, time.Time{}, fmt.Errorf("substrate/engine: decode credential %s: %w", ref, err)
	}
	return &tok, account, updatedAt, nil
}

// errCredentialGone marks a ref with no stored row: the account needs a
// reconnect, not a retry.
var errCredentialGone = errors.New("substrate/engine: credential not found")

// deleteCredentialsFor drops every credential a record holds — teardown.
func (ds *dataset) deleteCredentialsFor(ctx context.Context, account eref) error {
	_, err := ds.db.ExecContext(ctx, `DELETE FROM sealed WHERE record_kind = $1 AND record_id = $2`,
		account.Kind, account.ID)
	if err != nil {
		return fmt.Errorf("substrate/engine: delete credentials: %w", err)
	}
	return nil
}

// expiringCredentials lists refs whose tokens expire before the horizon —
// the refresh loop's work list.
func (ds *dataset) expiringCredentials(ctx context.Context, horizon time.Time) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT ref FROM sealed
		WHERE expires_at IS NOT NULL AND expires_at < $1 ORDER BY expires_at`, horizon.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// --- sealing -------------------------------------------------------------------

// credentialAEAD derives the AES-GCM cipher off the configured key; nil
// means the store writes plain.
func (s *service) credentialAEAD() (cipher.AEAD, error) {
	if len(s.credKey) == 0 {
		return nil, nil
	}
	block, err := aes.NewCipher(s.credKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *service) sealCredential(raw []byte) ([]byte, error) {
	aead, err := s.credentialAEAD()
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return append([]byte{credPlain}, raw...), nil
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{credSealed}, nonce...)
	return aead.Seal(out, nonce, raw, nil), nil
}

func (s *service) openCredential(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}
	switch payload[0] {
	case credPlain:
		return payload[1:], nil
	case credSealed:
		aead, err := s.credentialAEAD()
		if err != nil {
			return nil, err
		}
		if aead == nil {
			return nil, errors.New("sealed credential but no credential key configured")
		}
		body := payload[1:]
		if len(body) < aead.NonceSize() {
			return nil, errors.New("sealed credential too short")
		}
		return aead.Open(nil, body[:aead.NonceSize()], body[aead.NonceSize():], nil)
	default:
		return nil, fmt.Errorf("unknown credential framing %q", payload[0])
	}
}

// deriveCredentialKey stretches any non-empty key string to the AES-256 key.
func deriveCredentialKey(key string) []byte {
	if key == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// --- secret-property refs ------------------------------------------------
//
// A secret-typed property's STORED value is a ref into the sealed store: the
// material lives in `sealed`, encrypted under the credential key and owned
// by the record; the records fold and the changelog delta both carry only
// the opaque address. The append-only log therefore never holds material,
// not even ciphertext; rotation DELETES the old sealed row instead of
// retiring old ciphertexts into immutable history; and re-keying touches one
// table. The engine-minted auth and OAuth refs (passwordRef, totpRef,
// tokenRef) are the same shape written by their own machinery, and the write
// path recognizes any value naming an existing sealed row of the same record
// as a carried ref: a check against server-side state, never trust in caller
// bytes.
//
// A record hard-deleted outside the OAuth teardown path may orphan its
// sealed rows; an orphan is encrypted material addressed by nothing, and the
// reseal migration reports nothing about it. Erasure-on-delete beyond the
// OAuth teardown is future work, not a leak.

// secretRefPrefix namespaces the refs storeSecretProps mints, so a generic
// reader recognizes a resolvable ref without probing the store for every
// legacy plaintext.
const secretRefPrefix = "secret:"

// sealedPropPrefix marks the RETIRED inline-sealed form: releases before the
// store-backed design encrypted the value directly into JSONB under this
// prefix. openSecretValue still opens it, and the reseal migration moves it
// into the store.
const sealedPropPrefix = "substrate:sealsecret:v1:"

// newSecretRef mints an unguessable ref for one stored secret value.
func newSecretRef() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return secretRefPrefix + hex.EncodeToString(raw), nil
}

// storeSecretValue seals one plaintext into the sealed store under a fresh
// ref owned by the record, inside the caller's transaction.
func (t *txn) storeSecretValue(owner eref, plaintext string) (string, error) {
	ref, err := newSecretRef()
	if err != nil {
		return "", err
	}
	payload, err := t.ds.svc.sealCredential([]byte(plaintext))
	if err != nil {
		return "", err
	}
	if _, err := t.exec(`
		INSERT INTO sealed (ref, record_kind, record_id, payload, updated_at)
		VALUES ($1, $2, $3, $4, now())`,
		ref, owner.Kind, owner.ID, payload); err != nil {
		return "", fmt.Errorf("substrate/engine: store secret value: %w", err)
	}
	return ref, nil
}

// sealedRefOf reports whether ref names an existing sealed row owned by the
// record: the carried-ref test the write path uses in place of trusting the
// value's bytes.
func (t *txn) sealedRefOf(ref string, owner eref) (bool, error) {
	var one int
	err := t.row(`SELECT 1 FROM sealed WHERE ref = $1 AND record_kind = $2 AND record_id = $3`,
		ref, owner.Kind, owner.ID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// openSecretValue resolves one stored secret value to its material: a secret
// ref reads its sealed row, the retired inline-sealed form opens in place,
// and a legacy plaintext passes through unchanged, so every read works
// before and after the reseal migration.
func (ds *dataset) openSecretValue(ctx context.Context, stored string) (string, error) {
	switch {
	case stored == "":
		return "", nil
	case strings.HasPrefix(stored, secretRefPrefix):
		var payload []byte
		err := ds.db.QueryRowContext(ctx,
			`SELECT payload FROM sealed WHERE ref = $1`, stored).Scan(&payload)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("substrate/engine: secret ref has no sealed row")
		}
		if err != nil {
			return "", err
		}
		raw, err := ds.svc.openCredential(payload)
		if err != nil {
			return "", fmt.Errorf("substrate/engine: open stored secret: %w", err)
		}
		return string(raw), nil
	case strings.HasPrefix(stored, sealedPropPrefix):
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedPropPrefix))
		if err != nil {
			return "", fmt.Errorf("substrate/engine: decode sealed property: %w", err)
		}
		out, err := ds.svc.openCredential(raw)
		if err != nil {
			return "", fmt.Errorf("substrate/engine: open sealed property: %w", err)
		}
		return string(out), nil
	default:
		return stored, nil
	}
}

// openPropValue opens the retired inline-sealed form, or passes any other
// value through: the tokenRef read sites want the REF a legacy release
// sealed inline, never the material behind it, so they must not resolve a
// store-backed ref the way openSecretValue does.
func (s *service) openPropValue(stored string) (string, error) {
	if !strings.HasPrefix(stored, sealedPropPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedPropPrefix))
	if err != nil {
		return "", fmt.Errorf("substrate/engine: decode sealed property: %w", err)
	}
	out, err := s.openCredential(raw)
	if err != nil {
		return "", fmt.Errorf("substrate/engine: open sealed property: %w", err)
	}
	return string(out), nil
}
