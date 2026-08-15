package engine

// Changelog signing: a per-repository Ed25519 key signs every entry's chain
// hash (chain.go), so an attacker with database write access but WITHOUT the
// host credential key cannot rewrite history and quietly re-chain it. The
// ceiling is stated, not hidden: whoever holds both the database and the
// credential key is the host operator, and no in-database scheme defends
// against the party who runs the database. A public key read out of the same
// mutable database proves internal consistency only; the trust anchor is the
// (public key, signed_from_seq) pair logged at activation and pinned outside
// the database, plus remembered heads.
//
// Activation is DURABLE and ONE-WAY. The environment toggle
// (SUBSTRATE_CHANGELOG_SIGNING) only selects whether a repository activates
// at its next open; it never deactivates. Once signed_from_seq is set, an
// entry at or after it without a valid signature is a verification failure,
// and the engine refuses to append unsigned — a lost credential key stops
// writes rather than silently shedding the guarantee.
//
// The key rules are STRICTER than the DEK's, deliberately: the DEK wrap
// falls back to plain framing on a keyless host, which is fatal here — the
// signature exists precisely to resist a database-only attacker, so the seed
// must never sit beside the signatures it mints. Activation refuses without
// a credential key, and the loader refuses plain framing and wrong lengths
// rather than falling back.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// signingState is one repository's durable signing state, as the control
// plane holds it.
type signingState struct {
	// wrappedSeed is the Ed25519 seed under the host credential key; nil
	// until activation.
	wrappedSeed []byte
	public      ed25519.PublicKey
	// signedFrom is the first seq the signature guarantee covers; 0 until
	// activation, never unset after.
	signedFrom int64
}

// datasetSigning is the signing state a dataset holds live: the unwrapped
// key beside the durable facts.
type datasetSigning struct {
	key        ed25519.PrivateKey
	public     ed25519.PublicKey
	signedFrom int64
}

func (ds *dataset) signing() datasetSigning {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.signState
}

func (ds *dataset) setSigning(st datasetSigning) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.signState = st
}

// refreshSigning re-reads the durable signing state and, when another
// process has activated signing since this dataset opened, upgrades the
// dataset in place. Called from settleChain UNDER the changelog lock, on the
// one path a stale `signedFrom == 0` would otherwise let an unsigned entry
// through (adversarial review #4: the activation CAS orders the activating
// process, not a dataset another process already opened).
func (ds *dataset) refreshSigning(ctx context.Context) (datasetSigning, error) {
	st, err := ds.svc.loadSigningState(ctx, ds.scope.Repository)
	if errors.Is(err, sql.ErrNoRows) {
		// The creation window: the seed transaction commits BEFORE the
		// control-plane row exists, and signing cannot be active on a
		// repository that does not have a row yet.
		return ds.signing(), nil
	}
	if err != nil {
		return datasetSigning{}, err
	}
	if st.signedFrom == 0 {
		return ds.signing(), nil
	}
	key, err := ds.svc.openSigningSeed(st.wrappedSeed)
	if err != nil {
		// The mark stands even where the key does not open: the caller
		// refuses to append rather than appending unsigned.
		live := datasetSigning{public: st.public, signedFrom: st.signedFrom}
		ds.setSigning(live)
		return live, nil
	}
	live := datasetSigning{key: key, public: st.public, signedFrom: st.signedFrom}
	ds.setSigning(live)
	return live, nil
}

// loadSigningState reads one repository's signing columns off the control
// plane.
func (s *service) loadSigningState(ctx context.Context, repoID string) (signingState, error) {
	var st signingState
	var wrapped, public []byte
	var from sql.NullInt64
	err := s.maint.QueryRowContext(ctx,
		`SELECT signing_key, signing_public, signed_from_seq FROM repositories WHERE id = $1`,
		repoID).Scan(&wrapped, &public, &from)
	if err != nil {
		return signingState{}, err
	}
	st.wrappedSeed = wrapped
	if len(public) > 0 {
		if len(public) != ed25519.PublicKeySize {
			return signingState{}, fmt.Errorf("substrate/engine: repository %s stores a %d-byte signing public key; refusing it", repoID, len(public))
		}
		st.public = ed25519.PublicKey(public)
	}
	if from.Valid {
		st.signedFrom = from.Int64
	}
	return st, nil
}

// openSigningSeed unwraps a stored signing seed into the private key. It
// REFUSES plain framing: a signing seed written outside the credential key
// would be a signature anybody with the database could mint.
func (s *service) openSigningSeed(wrapped []byte) (ed25519.PrivateKey, error) {
	if len(wrapped) == 0 {
		return nil, errors.New("substrate/engine: no signing key stored")
	}
	if wrapped[0] != credSealed {
		return nil, errors.New("substrate/engine: the stored signing key is not sealed under the credential key; refusing it")
	}
	aead, err := s.credentialAEAD()
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return nil, errors.New("substrate/engine: the signing key is sealed but no credential key is configured (SUBSTRATE_CREDENTIAL_KEY)")
	}
	seed, err := openWith(aead, wrapped)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: open signing key: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("substrate/engine: stored signing seed is %d bytes, want %d; refusing it", len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// adoptSigning activates signing for one repository: mint a seed, seal it
// (never plain), set signed_from_seq to the NEXT seq compare-and-swap on
// NULL, and record a signed activation epoch. The loser of a concurrent
// activation re-reads the winner's state.
//
// The log line below is the PIN: the (public key, signed_from_seq) pair is
// what an operator writes down outside the database, because everything in
// the database — key rows, signatures, epochs — is rewritable by whoever
// holds it, and the pinned pair is what catches that.
func (s *service) adoptSigning(ctx context.Context, ds *dataset) (signingState, ed25519.PrivateKey, error) {
	if len(s.credKey) == 0 {
		return signingState{}, nil, errors.New("substrate/engine: SUBSTRATE_CHANGELOG_SIGNING without SUBSTRATE_CREDENTIAL_KEY would store the signing seed in the clear beside the signatures it mints; set the credential key first")
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return signingState{}, nil, err
	}
	aead, err := s.credentialAEAD()
	if err != nil {
		return signingState{}, nil, err
	}
	wrapped, err := sealWith(aead, seed)
	if err != nil {
		return signingState{}, nil, err
	}
	key := ed25519.NewKeyFromSeed(seed)
	public := key.Public().(ed25519.PublicKey)

	// The activation boundary is the seq AFTER the current head, read under
	// the changelog lock so no append moves it mid-activation; the same
	// transaction records the signed activation epoch, so the durable mark
	// and its attestation land together.
	var st signingState
	err = ds.inRawTx(ctx, func(t *txn) error {
		if err := t.lockKey(changelogLockKey); err != nil {
			return err
		}
		var head int64
		var headHash []byte
		if err := t.row(`SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
			return err
		}
		if head > 0 {
			if err := t.row(`SELECT hash FROM changelog WHERE seq = $1`, head).Scan(&headHash); err != nil {
				return err
			}
			if len(headHash) != 32 {
				return fmt.Errorf("substrate/engine: cannot activate signing over an unhashed head (seq %d); the backfill has not run", head)
			}
		}
		from := head + 1
		// The CAS runs on the MAINT pool (control plane), outside this scoped
		// transaction: the two pools cannot share one transaction, so the
		// order carries it — the control-plane mark lands first (the
		// guarantee), the epoch (the attestation) commits after. A crash
		// between the two leaves an activated repository with no activation
		// epoch; verify reports that state by name, and the guarantee itself
		// is never weaker than the mark. The changelog lock held by this
		// transaction is what keeps `from` true: an append blocks on it, so
		// no entry lands between the head read and the mark.
		res, err := s.maint.ExecContext(ctx, `
			UPDATE repositories SET signing_key = $1, signing_public = $2, signed_from_seq = $3
			WHERE id = $4 AND signed_from_seq IS NULL`,
			wrapped, []byte(public), from, ds.scope.Repository)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			// Another process won; nothing to record here.
			return errSigningRaced
		}
		st = signingState{wrappedSeed: wrapped, public: public, signedFrom: from}
		ep := chainEpoch{
			At: t.now, Reason: epochActivate, FromSeq: from,
			OldHead: headHash, NewHead: headHash,
			PublicKey: []byte(public), SignedFrom: from,
		}
		return t.recordEpoch(ep, key)
	})
	if errors.Is(err, errSigningRaced) {
		won, lerr := s.loadSigningState(ctx, ds.scope.Repository)
		if lerr != nil {
			return signingState{}, nil, lerr
		}
		wkey, kerr := s.openSigningSeed(won.wrappedSeed)
		if kerr != nil {
			return signingState{}, nil, kerr
		}
		return won, wkey, nil
	}
	if err != nil {
		return signingState{}, nil, err
	}
	s.log.Info("substrate: changelog signing ACTIVATED — pin this pair outside the database; it is what a verifier trusts",
		"repository", ds.scope.Repository,
		"publicKey", hex.EncodeToString(public),
		"signedFromSeq", st.signedFrom)
	return st, key, nil
}

// errSigningRaced marks a lost activation CAS: somebody else's activation is
// the one that stands.
var errSigningRaced = errors.New("substrate/engine: signing activation raced")

// ensureActivationEpoch repairs the activation crash window: the durable mark
// (maint pool) and its epoch (scoped transaction) cannot share a transaction,
// so a crash between them leaves an activated repository whose verification
// would fail FOREVER on "no valid activation epoch", with nothing sanctioned
// to record one after the fact (adversarial review). The open is that
// sanctioned place: the key is in hand, so the late epoch is signed exactly
// like a timely one. A keyless host leaves the state for verify to report.
func (s *service) ensureActivationEpoch(ctx context.Context, ds *dataset) error {
	st := ds.signing()
	if st.signedFrom == 0 || st.key == nil {
		return nil
	}
	return ds.inRawTx(ctx, func(t *txn) error {
		if err := t.lockKey(changelogLockKey); err != nil {
			return err
		}
		epochs, err := loadEpochs(t.ctx, t.tx, ds.scope.Repository, st.public)
		if err != nil {
			return err
		}
		valid := false
		publicHex := hex.EncodeToString(st.public)
		for _, ep := range epochs {
			if ep.Reason == epochActivate && ep.Signed && ep.SigOK != nil && *ep.SigOK &&
				ep.PublicKey == publicHex &&
				ep.FromSeq == st.signedFrom && ep.SignedFrom == st.signedFrom {
				valid = true
			}
		}
		if valid {
			return nil
		}
		_, headHash, err := t.chainHead()
		if err != nil {
			return err
		}
		ep := chainEpoch{
			At: t.now, Reason: epochActivate, FromSeq: st.signedFrom,
			OldHead: headHash, NewHead: headHash,
			PublicKey: []byte(st.public), SignedFrom: st.signedFrom,
		}
		s.log.Warn("substrate: recording a LATE activation epoch — a crash interrupted the original activation between its mark and its attestation",
			"repository", ds.scope.Repository, "signedFromSeq", st.signedFrom)
		return t.recordEpoch(ep, st.key)
	})
}

// inRawTx runs fn in a plain scoped transaction: no actor, no fold, no
// changelog entry — the shape activation, backfill and verify bookkeeping
// need. It settles nothing on purpose: fn must not fold.
func (ds *dataset) inRawTx(ctx context.Context, fn func(*txn) error) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	t := &txn{ctx: ctx, ds: ds, tx: tx, now: nowUTC(), internal: true}
	if err := fn(t); err != nil {
		_ = tx.Rollback()
		return err
	}
	if len(t.folded) > 0 || len(t.pending) > 0 {
		_ = tx.Rollback()
		return errors.New("substrate/engine: a raw transaction folded or appended; it may not")
	}
	return tx.Commit()
}

// recordEpoch writes one chain_epochs row inside the transaction, signed by
// key when one is given.
func (t *txn) recordEpoch(ep chainEpoch, key ed25519.PrivateKey) error {
	var sig []byte
	if key != nil {
		h := epochHash(t.ds.scope.Repository, ep)
		sig = ed25519.Sign(key, h[:])
	}
	var signedFrom any
	if ep.SignedFrom > 0 {
		signedFrom = ep.SignedFrom
	}
	_, err := t.exec(`
		INSERT INTO chain_epochs (at, reason, from_seq, old_head, new_head, public_key, signed_from, sig)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ep.At, ep.Reason, ep.FromSeq, ep.OldHead, ep.NewHead, ep.PublicKey, signedFrom, sig)
	if err != nil {
		return fmt.Errorf("substrate/engine: record chain epoch (%s): %w", ep.Reason, err)
	}
	return nil
}
