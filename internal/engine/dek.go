package engine

// The per-repository data-encryption key (DEK). Every sealed-store payload
// seals under the repository's own random 32-byte key; the control plane
// holds that key wrapped under the host credential key, and the repository's
// recoverykey record holds it wrapped to the user's age recipient. The two
// planes never mix: nothing host-keyed sits in the repository, and the user
// plane (changelog + sealed + blobs) is recoverable with the age identity
// alone. A payload sealed before DEKs existed opens through the host-key
// fallback until `repository reseal` re-keys it.

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
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
// owning record, so it binds the literal "dek" and the repository id (0023): a
// wrap lifted into another repository's row stops opening.
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

// unwrapDEK opens a control-plane wrapped DEK, presenting its repository binding.
func (s *service) unwrapDEK(wrapped []byte, repoID string) ([]byte, error) {
	return s.openCredential(wrapped, dekAAD(repoID))
}

// repoDEK fetches and unwraps one repository's DEK straight from the control
// plane: the authoritative read, used at open and by the maintenance-pool
// auth paths. Nil when the repository predates DEKs.
func (s *service) repoDEK(ctx context.Context, repoID string) ([]byte, error) {
	var wrapped []byte
	err := s.maint.QueryRowContext(ctx,
		`SELECT dek FROM repositories WHERE id = $1`, repoID).Scan(&wrapped)
	if err != nil {
		return nil, err
	}
	if len(wrapped) == 0 {
		return nil, nil
	}
	return s.unwrapDEK(wrapped, repoID)
}

// adoptDEK gives a pre-DEK repository its key, compare-and-swap on NULL so
// two concurrent opens cannot mint two: the loser re-reads the winner's.
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
		`UPDATE repositories SET dek = $1 WHERE id = $2 AND dek IS NULL`, wrapped, repoID)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 1 {
		return dek, nil
	}
	return s.repoDEK(ctx, repoID)
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

// openPayload opens one sealed-store payload: plain framing as-is (keyless
// legacy), then the DEK, then the host key (payloads sealed before DEKs). aad
// is the row's binding, presented only for `credBoundSealed` payloads.
// `repository reseal` re-keys the stragglers so the fallback goes quiet.
func (ds *dataset) openPayload(payload, aad []byte) ([]byte, error) {
	return openWithFallback(payload, ds.dek, ds.svc.credKey, aad)
}

// OpenPayloadWithKey opens one sealed-store payload under an explicitly
// supplied key, presenting aad as the row's binding: the recovery tooling's
// read, and the proof that a DEK recovered through the age wrap is sufficient
// on its own.
func OpenPayloadWithKey(key, payload, aad []byte) ([]byte, error) {
	return openWithFallback(payload, key, nil, aad)
}

// openWithFallback is the shared open order for a repository's payloads. It
// presents aad only for the bound framing; the unbound `credSealed` sealed no
// additional data, so it opens with nil.
func openWithFallback(payload, dek, hostKey, aad []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}
	switch payload[0] {
	case credPlain:
		return payload[1:], nil
	case credSealed:
		aad = nil
	case credBoundSealed:
		// aad is the row's binding
	default:
		return nil, fmt.Errorf("unknown credential framing %q", payload[0])
	}
	// The FIRST failure is the authoritative one: the DEK is tried first,
	// and a malformed DEK must not hide behind a host-key attempt's error.
	var firstErr error
	for _, key := range [][]byte{dek, hostKey} {
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
		firstErr = errors.New("sealed payload but no key opens it")
	}
	return nil, firstErr
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
