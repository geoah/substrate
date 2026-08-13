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

// wrapDEK seals a DEK under the host credential key for the control-plane
// row: the same plain/sealed framing the store uses, so a keyless host
// stores it plain-marked, loudly, and the boot warning is the operator's cue.
func (s *service) wrapDEK(dek []byte) ([]byte, error) { return s.sealCredential(dek) }

// unwrapDEK opens a control-plane wrapped DEK.
func (s *service) unwrapDEK(wrapped []byte) ([]byte, error) { return s.openCredential(wrapped) }

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
	return s.unwrapDEK(wrapped)
}

// adoptDEK gives a pre-DEK repository its key, compare-and-swap on NULL so
// two concurrent opens cannot mint two: the loser re-reads the winner's.
func (s *service) adoptDEK(ctx context.Context, repoID string) ([]byte, error) {
	dek, err := newDEK()
	if err != nil {
		return nil, err
	}
	wrapped, err := s.wrapDEK(dek)
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
// open has adopted one.
func (s *service) sealRepoPayload(dek, raw []byte) ([]byte, error) {
	if len(dek) == 0 {
		return s.sealCredential(raw)
	}
	aead, err := aeadOf(dek)
	if err != nil {
		return nil, err
	}
	return sealWith(aead, raw)
}

// aeadOf builds the AES-256-GCM cipher for one key.
func aeadOf(key []byte) (cipher.AEAD, error) {
	if len(key) == 0 {
		return nil, nil
	}
	return newAEAD(key)
}

// sealPayload seals one sealed-store payload under the repository's DEK.
// Every repository has one from open onward, so there is no keyless branch
// here: on a keyless HOST the weakness is the plain-marked wrap in the
// control plane, never an unencrypted payload.
func (ds *dataset) sealPayload(raw []byte) ([]byte, error) {
	aead, err := aeadOf(ds.dek)
	if err != nil {
		return nil, err
	}
	if aead == nil {
		// A dataset hand-built without a DEK is a programming error, but
		// storing plaintext silently would be worse.
		return nil, errors.New("substrate/engine: dataset has no DEK")
	}
	return sealWith(aead, raw)
}

// openPayload opens one sealed-store payload: plain framing as-is (keyless
// legacy), then the DEK, then the host key (payloads sealed before DEKs).
// `repository reseal` re-keys the stragglers so the fallback goes quiet.
func (ds *dataset) openPayload(payload []byte) ([]byte, error) {
	return openWithFallback(payload, ds.dek, ds.svc.credKey)
}

// OpenPayloadWithKey opens one sealed-store payload under an explicitly
// supplied key: the recovery tooling's read, and the proof that a DEK
// recovered through the age wrap is sufficient on its own.
func OpenPayloadWithKey(key, payload []byte) ([]byte, error) {
	return openWithFallback(payload, key, nil)
}

// openWithFallback is the shared open order for a repository's payloads.
func openWithFallback(payload, dek, hostKey []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}
	if payload[0] == credPlain {
		return payload[1:], nil
	}
	if payload[0] != credSealed {
		return nil, fmt.Errorf("unknown credential framing %q", payload[0])
	}
	var lastErr error
	for _, key := range [][]byte{dek, hostKey} {
		aead, err := aeadOf(key)
		if err != nil || aead == nil {
			continue
		}
		out, err := openWith(aead, payload)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("sealed payload but no key opens it")
	}
	return nil, lastErr
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
