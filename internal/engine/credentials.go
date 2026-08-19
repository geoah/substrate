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
// nonce||ciphertext (sealed). `credBoundSealed` marks a payload whose GCM
// additional data binds it to the address it was written at, so a row moved,
// copied or swapped without the key stops decrypting; `credSealed` is the older
// unbound form, which the open path still reads until every store is resealed
// ([0023](../../docs/decisions/0023-a-sealed-payload-is-bound-to-its-address.md)).
const (
	credPlain       byte = 'p'
	credSealed      byte = 's'
	credBoundSealed byte = 'a'
)

// sealedAAD is the binding a sealed-store row seals under: its ref, record kind
// and record id, joined by NUL. A ref is prefixed hex, a record kind is
// `{authority}/{name}` and a record id draws a frozen alphabet, so none holds a
// NUL and the join is unambiguous (0023).
func sealedAAD(ref, recordKind, recordID string) []byte {
	return []byte(ref + "\x00" + recordKind + "\x00" + recordID)
}

// sealToken renders one token as its sealed row payload plus the
// denormalized expiry column value, bound to the row (ref, account) it lands in.
func (ds *dataset) sealToken(tok *oauth2.Token, aad []byte) (payload []byte, expires any, err error) {
	raw, err := json.Marshal(tok)
	if err != nil {
		return nil, nil, err
	}
	payload, err = ds.sealPayload(raw, aad)
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
	payload, expires, err := t.ds.sealToken(tok, sealedAAD(ref, account.Kind, account.ID))
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
	payload, expires, err := ds.sealToken(tok, sealedAAD(ref, account.Kind, account.ID))
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
	raw, err := ds.openPayload(payload, sealedAAD(ref, account.Kind, account.ID))
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

// newAEAD builds the AES-256-GCM cipher for one 32-byte key.
func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// sealWith frames and seals one payload under an AEAD, binding aad as GCM
// additional data. A non-nil aad frames `credBoundSealed` and the open side must
// present the same aad; a nil aad frames the older unbound `credSealed`.
func sealWith(aead cipher.AEAD, raw, aad []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	frame := credSealed
	if aad != nil {
		frame = credBoundSealed
	}
	out := append([]byte{frame}, nonce...)
	return aead.Seal(out, nonce, raw, aad), nil
}

// openWith opens one sealed-framed payload under an AEAD, presenting aad as GCM
// additional data. The caller passes the aad the framing byte calls for: the
// row's binding for `credBoundSealed`, nil for the unbound `credSealed`.
func openWith(aead cipher.AEAD, payload, aad []byte) ([]byte, error) {
	body := payload[1:]
	if len(body) < aead.NonceSize() {
		return nil, errors.New("sealed credential too short")
	}
	return aead.Open(nil, body[:aead.NonceSize()], body[aead.NonceSize():], aad)
}

// credentialAEAD derives the AES-GCM cipher off the configured HOST key; nil
// means the host runs keyless and host-keyed wraps write plain.
func (s *service) credentialAEAD() (cipher.AEAD, error) {
	if len(s.credKey) == 0 {
		return nil, nil
	}
	return newAEAD(s.credKey)
}

// sealCredential seals under the HOST key: the DEK wraps in the control
// plane and nothing else. Repository payloads seal under the repository's
// own DEK (dataset.sealPayload). aad binds the wrap to its owner (0023); a
// keyless host stores plain-marked and cannot bind.
func (s *service) sealCredential(raw, aad []byte) ([]byte, error) {
	aead, err := s.credentialAEAD()
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return append([]byte{credPlain}, raw...), nil
	}
	return sealWith(aead, raw, aad)
}

func (s *service) openCredential(payload, aad []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}
	switch payload[0] {
	case credPlain:
		return payload[1:], nil
	case credSealed, credBoundSealed:
		aead, err := s.credentialAEAD()
		if err != nil {
			return nil, err
		}
		if aead == nil {
			return nil, errors.New("sealed credential but no credential key configured")
		}
		if payload[0] == credSealed {
			aad = nil // the unbound framing sealed no additional data
		}
		return openWith(aead, payload, aad)
	default:
		return nil, fmt.Errorf("unknown credential framing %q", payload[0])
	}
}

// deriveCredentialKey decodes the credential key to its AES-256 key. The
// accepted form is standard-base64 of exactly 32 bytes and nothing else: a
// passphrase run through one hash is a key an offline attacker with a database
// dump enumerates from a dictionary, so the key must carry 256 bits of entropy
// the code can check rather than 256 bits an operator promises
// ([0024](../../docs/decisions/0024-the-credential-key-is-key-material-not-a-passphrase.md)).
// Empty is the keyless service; any other non-conforming value is refused, so
// a misconfigured host cannot boot sealing everything under a weak key.
func deriveCredentialKey(key string) ([]byte, error) {
	if key == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("substrate/engine: SUBSTRATE_CREDENTIAL_KEY must be base64 of exactly 32 bytes (generate one with: openssl rand -base64 32)")
	}
	return raw, nil
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
// path recognizes a value naming an existing sealed row of the same record as
// a carried ref: a check against server-side state, never trust in caller
// bytes. That recognition is per-property (storeSecretProps): a value naming a
// row a DIFFERENT property of the record owns is refused, so two properties
// cannot share one sealed row (#233).
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
	payload, err := t.ds.sealPayload([]byte(plaintext), sealedAAD(ref, owner.Kind, owner.ID))
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
		var owner eref
		err := ds.db.QueryRowContext(ctx,
			`SELECT payload, record_kind, record_id FROM sealed WHERE ref = $1`, stored).
			Scan(&payload, &owner.Kind, &owner.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("substrate/engine: secret ref has no sealed row")
		}
		if err != nil {
			return "", err
		}
		raw, err := ds.openPayload(payload, sealedAAD(stored, owner.Kind, owner.ID))
		if err != nil {
			return "", fmt.Errorf("substrate/engine: open stored secret: %w", err)
		}
		return string(raw), nil
	case strings.HasPrefix(stored, sealedPropPrefix):
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedPropPrefix))
		if err != nil {
			return "", fmt.Errorf("substrate/engine: decode sealed property: %w", err)
		}
		// The retired inline-sealed form predates the binding: unbound framing.
		out, err := openWithFallback(raw, ds.dek, ds.svc.credKey, nil)
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
	// The retired inline-sealed form predates the binding: unbound framing.
	out, err := s.openCredential(raw, nil)
	if err != nil {
		return "", fmt.Errorf("substrate/engine: open sealed property: %w", err)
	}
	return string(out), nil
}
