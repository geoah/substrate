package engine

// The per-repository data-encryption key (DEK). Every sealed-store payload
// seals under the repository's own random 32-byte key; the control plane
// holds that key wrapped under the host credential key, and the repository's
// recoverykey record holds it wrapped to the user's age recipient. The two
// planes never mix: nothing host-keyed sits in the repository, and the user
// plane (changelog + sealed + blobs) is recoverable with the age identity
// alone. A payload sealed before DEKs existed, or stored plain by a keyless
// release, opens through the host-key fallback until the repository's first
// open re-keys it under the DEK and marks the row `sealed_dek_only`; from
// then on both forms are refused (decision 0059). Each DEK wrap names the
// host key it is under (`dek_key_id`), so a wrong SUBSTRATE_CREDENTIAL_KEY is
// reported as the wrong key, by id, and not as damage.

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"filippo.io/age"
)

// newDEK mints a repository's 32-byte data-encryption key.
func newDEK() ([]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// dekAAD binds the control-plane DEK wrap to its repository. The wrap has no
// owning record, so it binds the literal "dek" and the repository id, which is
// its authority (0023): `dek\x00<authority>`, so a wrap lifted into another
// repository's row stops opening. A directory written before the authority
// was the id is bound to the old random id, and the boot check re-wraps it
// (repodir.go, migrateLegacyDir).
func dekAAD(repoID string) []byte {
	return []byte("dek\x00" + repoID)
}

// wrapDEK seals a DEK under the host credential key for the control-plane
// row: the same plain/sealed framing the store uses, so a keyless host
// stores it plain-marked, loudly, and the boot warning is the operator's cue.
// The wrap binds its repository so it does not open in another's row.
func (s *service) wrapDEK(dek []byte, repoID string) ([]byte, error) {
	return s.sealCredential(dek, dekAAD(repoID))
}

// hostKeyID names a host credential key without revealing it: 16 hex digits
// of a domain-separated SHA-256 over the key material. It is stored beside
// every DEK wrap (`repositories.dek_key_id`, the manifest's `dekKeyId`), so a
// host holding a different key is told which key the wrap wants instead of
// that no key opens it (0059). Sixty-four bits of a one-way hash over 256
// random bits identify the key and cannot be turned back into it. Empty for
// the keyless service, whose wraps are plain-marked and name no key.
func hostKeyID(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	sum := sha256.Sum256(append([]byte("substrate host key id\x00"), key...))
	return hex.EncodeToString(sum[:8])
}

// unwrapDEK opens a control-plane wrapped DEK, presenting its repository
// binding. keyID is the host-key id stored beside the wrap, empty for a wrap
// written before the id was recorded. The wrap is always tried, whatever the
// id says: the id explains a failure, it never causes one.
func (s *service) unwrapDEK(wrapped []byte, repoID, keyID string) ([]byte, error) {
	dek, err := s.openCredential(wrapped, dekAAD(repoID))
	if err != nil {
		return nil, s.wrongHostKey(repoID, keyID, err)
	}
	return dek, nil
}

// wrongHostKey says why a DEK wrap did not open in terms of key ids: the one
// the wrap names and the one this host holds.
func (s *service) wrongHostKey(repoID, keyID string, cause error) error {
	host := "unset"
	if s.credKeyID != "" {
		host = "id " + s.credKeyID
	}
	switch {
	case keyID == "":
		return fmt.Errorf("SUBSTRATE_CREDENTIAL_KEY (%s) does not open the DEK wrap of repository %s, which names no key id: %w", host, repoID, cause)
	case keyID == s.credKeyID:
		return fmt.Errorf("SUBSTRATE_CREDENTIAL_KEY (%s) is the key the DEK wrap of repository %s names and still does not open it, so the wrap or its binding is damaged: %w", host, repoID, cause)
	default:
		return fmt.Errorf("SUBSTRATE_CREDENTIAL_KEY (%s) is not the key the DEK wrap of repository %s was written under (id %s): %w", host, repoID, keyID, cause)
	}
}

// repoKeys is what the control-plane row says about one repository's keys:
// the DEK unwrapped (nil when the repository predates DEKs), the id of the
// host key its wrap names, and whether the sealed store is DEK-only (0059).
type repoKeys struct {
	dek     []byte
	keyID   string
	dekOnly bool
}

// repoKeys reads one repository's keys straight from the control plane: the
// authoritative read, used at open.
func (s *service) repoKeys(ctx context.Context, repoID string) (repoKeys, error) {
	return s.repoKeysOn(ctx, s.maint, repoID)
}

// repoKeysOn reads the keys through q, so a caller that also reads a sealed
// payload can take both from one transaction's snapshot (auth.go).
func (s *service) repoKeysOn(ctx context.Context, q dbx, repoID string) (repoKeys, error) {
	var k repoKeys
	var wrapped []byte
	var keyID sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT dek, dek_key_id, sealed_dek_only FROM repositories WHERE id = $1`, repoID).
		Scan(&wrapped, &keyID, &k.dekOnly)
	if err != nil {
		return k, err
	}
	k.keyID = keyID.String
	if len(wrapped) == 0 {
		return k, nil
	}
	k.dek, err = s.unwrapDEK(wrapped, repoID, k.keyID)
	return k, err
}

// adoptDEK gives a pre-DEK repository its key, compare-and-swap on NULL so
// two concurrent opens cannot mint two: the loser re-reads the winner's. The
// row names the host key the wrap is under; the store is DEK-only only once
// the re-key at open has run (retireLegacySealed).
func (s *service) adoptDEK(ctx context.Context, repoID string) ([]byte, error) {
	dek, err := newDEK()
	if err != nil {
		return nil, err
	}
	wrapped, err := s.wrapDEK(dek, repoID)
	if err != nil {
		return nil, err
	}
	res, err := s.maint.ExecContext(ctx,
		`UPDATE repositories SET dek = $1, dek_key_id = $2 WHERE id = $3 AND dek IS NULL`,
		wrapped, nullString(s.credKeyID), repoID)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 1 {
		return dek, nil
	}
	k, err := s.repoKeys(ctx, repoID)
	return k.dek, err
}

// nullString is s as a nullable text column value: NULL for the empty string.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// retireLegacySealed is the one-shot pass that makes a repository DEK-only
// (0059). A repository not yet marked has its sealed store re-keyed under the
// DEK, plain and host-key-sealed payloads included, and its row marked, so
// every later read refuses those forms. On a keyed host a wrap that names no
// key (a row from before the id was stored) is RE-WRAPPED under this host's
// key and the id written beside it: the old wrap may be plain-marked, from a
// keyless release, and an id must never name a key that protects nothing.
// A repository missing neither pays nothing here. It runs at open, under the
// per-repository singleflight, before the manifest is written, because the
// manifest carries the wrap, the id and the marker.
func (s *service) retireLegacySealed(ctx context.Context, ds *dataset, repo Repository) (Repository, error) {
	if repo.SealedDEKOnly && (repo.DEKKeyID != "" || s.credKeyID == "") {
		return repo, nil
	}
	if !repo.SealedDEKOnly {
		var n int
		if err := ds.inRawTx(ctx, func(t *txn) error {
			var err error
			n, err = t.rekeySealedStore()
			return err
		}); err != nil {
			return repo, fmt.Errorf("re-key the sealed store under the DEK: %w", err)
		}
		if n > 0 {
			s.log.Info("substrate: re-keyed legacy sealed payloads under the repository DEK; plain and host-key-sealed payloads are refused from here on",
				"repository", repo.ID, "payloads", n)
		}
		// The re-keyed files must be on disk before the row says the store is
		// DEK-only: a file the re-key could not write fails its transaction
		// above (ErrDirectoryWrite), and a failure after the commit is
		// latched (fileErr) and caught here, so a marker is never written
		// over stale host-key files, which would be a manifest that lies
		// until the next boot rewrites them. The open fails, unmarked, and
		// the boot check's rewrite of sealed/ from the table is the repair.
		if err := ds.directoryErr(); err != nil {
			return repo, fmt.Errorf("the re-keyed sealed files did not reach the directory: %w", err)
		}
	}
	if repo.DEKKeyID == "" && s.credKeyID != "" {
		wrapped, err := s.wrapDEK(ds.dek, repo.ID)
		if err != nil {
			return repo, err
		}
		if _, err := s.maint.ExecContext(ctx,
			`UPDATE repositories SET sealed_dek_only = true, dek = $2, dek_key_id = $3 WHERE id = $1`,
			repo.ID, wrapped, s.credKeyID); err != nil {
			return repo, err
		}
		if len(repo.DEK) > 0 && repo.DEK[0] == credPlain {
			s.log.Info("substrate: sealed a plain-marked DEK wrap under SUBSTRATE_CREDENTIAL_KEY",
				"repository", repo.ID, "keyId", s.credKeyID)
		}
		return s.repositoryByID(ctx, repo.ID)
	}
	if _, err := s.maint.ExecContext(ctx,
		`UPDATE repositories SET sealed_dek_only = true WHERE id = $1`, repo.ID); err != nil {
		return repo, err
	}
	return s.repositoryByID(ctx, repo.ID)
}

// sealRepoPayload seals one payload under a repository's DEK, or under the
// host key for a pre-DEK repository the maintenance paths touch before any
// open has adopted one, binding aad to the row it lands in.
func (s *service) sealRepoPayload(dek, raw, aad []byte) ([]byte, error) {
	if len(dek) == 0 {
		return s.sealCredential(raw, aad)
	}
	aead, err := aeadOf(dek)
	if err != nil {
		return nil, err
	}
	return sealWith(aead, raw, aad)
}

// aeadOf builds the AES-256-GCM cipher for one key.
func aeadOf(key []byte) (cipher.AEAD, error) {
	if len(key) == 0 {
		return nil, nil
	}
	return newAEAD(key)
}

// sealPayload seals one sealed-store payload under the repository's DEK, bound
// to aad (the row's ref, kind and id). Every repository has a DEK from open
// onward, so there is no keyless branch here: on a keyless HOST the weakness is
// the plain-marked wrap in the control plane, never an unencrypted payload.
func (ds *dataset) sealPayload(raw, aad []byte) ([]byte, error) {
	aead, err := aeadOf(ds.dek)
	if err != nil {
		return nil, err
	}
	if aead == nil {
		// A dataset hand-built without a DEK is a programming error, but
		// storing plaintext silently would be worse.
		return nil, errors.New("substrate/engine: dataset has no DEK")
	}
	return sealWith(aead, raw, aad)
}

// openPayload opens one sealed-store payload under the repository's DEK, with
// aad as the row's binding (presented for `credBoundSealed` payloads only). On
// a repository marked DEK-only the plain framing and the host-key fallback
// are refused (0059); until the first open has re-keyed the store and marked
// it, a plain payload opens as is and the host key is tried after the DEK,
// for material sealed before DEKs existed.
func (ds *dataset) openPayload(payload, aad []byte) ([]byte, error) {
	return openRepoPayload(payload, ds.dek, ds.svc.credKey, aad, ds.dekOnly)
}

// OpenPayloadWithKey opens one sealed-store payload under the supplied key
// ALONE, presenting aad as the row's binding: the recovery tooling's read, and
// the proof that a DEK recovered through the age wrap is sufficient on its
// own. A plain payload is refused here whatever the repository's marker says,
// because opening one proves nothing about the key.
func OpenPayloadWithKey(key, payload, aad []byte) ([]byte, error) {
	return openRepoPayload(payload, key, nil, aad, true)
}

// errPlainRefused is the refusal of a `credPlain` payload on a repository
// marked DEK-only: the store holds none, so one can only have been planted.
var errPlainRefused = errors.New("refusing a plain-framed ('p') sealed payload: this repository holds no legacy payload, so every payload is expected bound-framed ('a') under the repository DEK")

// openRepoPayload is the one open order for a repository's sealed payloads.
// dekOnly is the repository's marker (0059): set, only the DEK is tried and a
// plain payload is refused, and the failure names the framing found and the
// key expected; unset, the plain framing opens as is and the host key is the
// fallback after the DEK. aad is presented only for the bound framing; the
// unbound `credSealed` sealed no additional data, so it opens with nil.
func openRepoPayload(payload, dek, hostKey, aad []byte, dekOnly bool) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}
	framing := payload[0]
	switch framing {
	case credPlain:
		if dekOnly {
			return nil, errPlainRefused
		}
		return payload[1:], nil
	case credSealed:
		aad = nil
	case credBoundSealed:
		// aad is the row's binding
	default:
		return nil, fmt.Errorf("unknown credential framing %q", framing)
	}
	keys := [][]byte{dek, hostKey}
	if dekOnly {
		keys = keys[:1]
	}
	// The FIRST failure is the authoritative one: the DEK is tried first,
	// and a malformed DEK must not hide behind a host-key attempt's error.
	var firstErr error
	for _, key := range keys {
		aead, err := aeadOf(key)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if aead == nil {
			continue
		}
		out, err := openWith(aead, payload, aad)
		if err == nil {
			return out, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = errors.New("no key to try")
	}
	if dekOnly {
		return nil, fmt.Errorf("sealed payload framed %q does not open under the repository DEK, the one key this repository's payloads seal under (the host-key fallback is refused: no legacy payload remains): %w", framing, firstErr)
	}
	return nil, fmt.Errorf("sealed payload framed %q opens under neither the repository DEK nor the host key: %w", framing, firstErr)
}

// --- the age recovery wrap -------------------------------------------------

// recoveryAlgorithm names the one recovery scheme v1 speaks.
const recoveryAlgorithm = "age-x25519-v1"

// wrapDEKToRecipient seals a DEK to the user's age recipient: the bytes only
// the age identity opens, safe in the changelog.
func wrapDEKToRecipient(dek []byte, publicKey string) ([]byte, error) {
	recipient, err := age.ParseX25519Recipient(publicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: recovery public key is not an age recipient: %w",
			errBadRecoveryKey, err)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(dek); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// errBadRecoveryKey marks a recipient the wrap refuses.
var errBadRecoveryKey = errors.New("substrate/engine: bad recovery key")

// generateRecoveryIdentity mints an age identity server-side, for the
// registration path that brought no recipient of its own. The identity
// string is returned ONCE and never stored.
func generateRecoveryIdentity() (identity, publicKey string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}
