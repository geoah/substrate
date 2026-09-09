package engine

// The per-repository data-encryption key (DEK). Every sealed-store payload
// seals under the repository's own random 32-byte key; the control plane
// holds that key wrapped under the host credential key, and the repository's
// recoverykey record holds it wrapped to the user's age recipient. The two
// planes never mix: nothing host-keyed sits in the repository, and the user
// plane (changelog + sealed + blobs) is recoverable with the age identity
// alone. Every payload is bound-framed under the DEK, from the repository's
// first write: a plain one and a host-key-sealed one are refused wherever
// they are met (decision 0059). Each DEK wrap names the host key it is under
// (`dek_key_id`), so a wrong SUBSTRATE_CREDENTIAL_KEY is reported as the
// wrong key, by id, and not as damage.

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
// repository's row stops opening.
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
// the DEK unwrapped and the id of the host key its wrap names.
type repoKeys struct {
	dek   []byte
	keyID string
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
		`SELECT dek, dek_key_id FROM repositories WHERE id = $1`, repoID).
		Scan(&wrapped, &keyID)
	if err != nil {
		return k, err
	}
	k.keyID = keyID.String
	// A repository is born with its DEK (CreateRepository) and an import
	// refuses a manifest without one, so a row with no wrap is damage rather
	// than a repository waiting for a key.
	if len(wrapped) == 0 {
		return k, fmt.Errorf("substrate/engine: repository %s holds no wrapped DEK, so nothing can open its sealed store", repoID)
	}
	k.dek, err = s.unwrapDEK(wrapped, repoID, k.keyID)
	return k, err
}

// nullString is s as a nullable text column value: NULL for the empty string.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// sealRepoPayload seals one payload under a repository's DEK, binding aad to
// the row it lands in: the maintenance paths' seal, outside a dataset. A
// caller with no DEK is a programming error, and storing plaintext silently
// would be worse.
func (s *service) sealRepoPayload(dek, raw, aad []byte) ([]byte, error) {
	aead, err := aeadOf(dek)
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return nil, errors.New("substrate/engine: no DEK to seal a repository payload under")
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
// aad as the row's binding (presented for `credBoundSealed` payloads only).
func (ds *dataset) openPayload(payload, aad []byte) ([]byte, error) {
	return openRepoPayload(payload, ds.dek, aad)
}

// OpenPayloadWithKey opens one sealed-store payload under the supplied key,
// presenting aad as the row's binding: the recovery tooling's read, and the
// proof that a DEK recovered through the age wrap is sufficient on its own.
func OpenPayloadWithKey(key, payload, aad []byte) ([]byte, error) {
	return openRepoPayload(payload, key, aad)
}

// errPlainRefused is the refusal of a `credPlain` payload in a repository's
// sealed store: every payload seals under the DEK, so a plain one can only
// have been planted.
var errPlainRefused = errors.New("refusing a plain-framed ('p') sealed payload: every payload of a repository is bound-framed ('a') under its DEK")

// openRepoPayload is the one open order for a repository's sealed payloads:
// the DEK and nothing else (decision 0059). A plain payload is refused, and
// aad is presented only for the bound framing, since the unbound `credSealed`
// sealed no additional data.
func openRepoPayload(payload, dek, aad []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}
	framing := payload[0]
	switch framing {
	case credPlain:
		return nil, errPlainRefused
	case credSealed:
		aad = nil
	case credBoundSealed:
		// aad is the row's binding
	default:
		return nil, fmt.Errorf("unknown credential framing %q", framing)
	}
	aead, err := aeadOf(dek)
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return nil, errors.New("no key to try")
	}
	out, err := openWith(aead, payload, aad)
	if err != nil {
		return nil, fmt.Errorf("sealed payload framed %q does not open under the repository DEK, the one key this repository's payloads seal under: %w", framing, err)
	}
	return out, nil
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
