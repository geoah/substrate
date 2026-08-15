package engine

// The reseal migration: every secret value stored by an earlier release, as
// raw plaintext or as the retired inline-sealed form, MOVES into the sealed
// store, and the ref takes its place in the records fold and in every
// historical changelog payload. Rewriting changelog bytes is the one
// sanctioned mutation of history, and it is values-only: no entry is added,
// removed or reordered and no seq moves. Every historical value of a secret
// property is replaced by the record's CURRENT ref (or an inert erased
// marker when the record no longer holds one), so a rebuild folds the
// changelog to byte-identical records rows, and the material of rotated-away
// values is not retired into the log but gone, which is the point.
//
// Scope is the repository's live registry: a kind uninstalled before the
// migration keeps whatever its old entries held, because no declaration
// survives to say which properties were secret. The change feed fails closed
// for exactly those kinds (redactChangePayload), so the leftover bytes do
// not ride the wire.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// erasedSecretRef stands in for a historical secret value whose record no
// longer holds one: it folds like any ref, resolves to nothing, and redacts
// on every read surface like the rest.
const erasedSecretRef = secretRefPrefix + "erased"

// ResealReport is what one reseal did.
type ResealReport struct {
	Repository string
	Username   string
	// Records is how many record rows had a value moved into the store.
	Records int
	// Entries is how many changelog payloads were rewritten.
	Entries int
	// SealedRows is how many sealed-store payloads upgraded from the keyless
	// plain framing.
	SealedRows int
	Took       time.Duration
}

// ResealRepository moves one repository's legacy secret values into the
// sealed store and re-points history at the refs. It refuses without a
// credential key (a keyless run would move plaintext from one table to
// another and certify nothing), and it refuses until the repository's stored
// vocabulary carries the digest re-type of token.hash: on a store the new
// server has never opened, hash still reads as `secret`, and migrating it
// would break every bearer token's SQL containment match. The repository's
// changelog lock is held for the duration and the whole reseal is ONE
// transaction.
func (s *service) ResealRepository(ctx context.Context, username string) (ResealReport, error) {
	started := time.Now()
	if len(s.credKey) == 0 {
		return ResealReport{}, fmt.Errorf("substrate/engine: reseal without a credential key moves plaintext between tables and protects nothing; set SUBSTRATE_CREDENTIAL_KEY first")
	}
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return ResealReport{}, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return ResealReport{}, err
	}
	if ty, err := ds.resolveType(kindToken); err != nil || ty == nil ||
		ty.Props["hash"] == nil || ty.Props["hash"].Datatype != vocabulary.DatatypeDigest {
		return ResealReport{}, fmt.Errorf("substrate/engine: this repository's stored vocabulary still types token.hash as secret; start the server once under the upgraded binary (the boot-time vocabulary upgrade carries the re-type), then reseal")
	}
	// A reseal rewrites history and re-chains it (below); on a repository
	// with signing active that re-chain must re-sign, so a host that cannot
	// sign refuses the whole operation rather than stripping the guarantee.
	if st := ds.signing(); st.signedFrom > 0 && st.key == nil {
		return ResealReport{}, fmt.Errorf("substrate/engine: reseal refuses: changelog signing is active for this repository but the signing key is unavailable — a reseal without it would strip signature validity from history")
	}
	report := ResealReport{Repository: repo.ID, Username: repo.Username}
	// Not inTx: a reseal is not a write with an actor and appends no entry.
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	t := &txn{
		ctx: ctx, ds: ds, tx: tx, actor: substrate.ActorSystem, tier: substrate.TierMachine,
		now: nowUTC(), internal: true,
	}
	if err := t.reseal(&report); err != nil {
		_ = tx.Rollback()
		return report, err
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	report.Took = time.Since(started)
	return report, nil
}

// secretPropNames lists a kind's secret-typed property names, nil when it
// has none: the one enumeration both the migration and its tests key off.
func secretPropNames(ty *vocabulary.Kind) []string {
	var names []string
	for _, name := range ty.PropOrder {
		if ty.Props[name].Secret() {
			names = append(names, name)
		}
	}
	return names
}

// currentRefKey addresses one record's one secret property in the
// current-value map the changelog rewrite substitutes from.
type currentRefKey struct {
	kind, id, prop string
}

func (t *txn) reseal(report *ResealReport) error {
	// The write path's own serialization: no writer appends mid-reseal.
	if err := t.lockKey(changelogLockKey); err != nil {
		return err
	}
	// VERIFY FIRST, inside this transaction, under this lock (adversarial
	// review, both passes): a reseal re-chains — and on a signed repository
	// RE-SIGNS — whatever bytes are stored, so run over tampered history it
	// would launder the tampering into freshly valid hashes and signatures.
	// It refuses instead. There is deliberately NO force path here: rebuild
	// installs a fold you can rebuild again, but a reseal mints signatures,
	// and the sanctioned way to re-attest bytes you have decided to accept
	// is the backfill (wipe the hashes, reopen), which cannot forge a
	// signature over the signed range.
	signing := t.ds.signing()
	var chainFinding string
	if _, err := verifyChainCore(t.ctx, t.tx, t.ds.scope.Repository, signing.signedFrom, signing.public,
		func(f string) {
			if chainFinding == "" {
				chainFinding = f
			}
		}); err != nil {
		return err
	}
	if chainFinding != "" {
		return fmt.Errorf("substrate/engine: reseal refuses: the chain does not verify (%s); a reseal over tampered history would notarize the tampering — run `repository verify`", chainFinding)
	}
	secretProps := map[string][]string{}
	for _, ty := range t.ds.registry().Kinds() {
		if names := secretPropNames(ty); len(names) > 0 {
			secretProps[ty.Identity] = names
		}
	}
	if len(secretProps) == 0 {
		return nil
	}
	idents := make([]string, 0, len(secretProps))
	for ident := range secretProps {
		idents = append(idents, ident)
	}
	sort.Strings(idents)
	current := map[currentRefKey]string{}
	for _, ident := range idents {
		if err := t.resealRecords(report, ident, secretProps[ident], current); err != nil {
			return err
		}
	}
	// The head hash BEFORE the rewrite: the epoch's old_head, which is what
	// lets a verifier holding a pre-reseal receipt see "sanctioned rewrite"
	// instead of "tampering".
	_, oldHead, err := t.chainHead()
	if err != nil {
		return err
	}
	minSeq, err := t.resealChangelog(report, secretProps, current)
	if err != nil {
		return err
	}
	if minSeq > 0 {
		// Every hash from the first rewritten entry to the head moves: the
		// rewritten payloads directly, everything after them through `prev`.
		newHead, err := t.rechainFrom(minSeq)
		if err != nil {
			return err
		}
		ep := chainEpoch{
			At: t.now, Reason: epochReseal, FromSeq: minSeq,
			OldHead: oldHead, NewHead: newHead,
		}
		if signing.signedFrom > 0 {
			ep.PublicKey = []byte(signing.public)
			ep.SignedFrom = signing.signedFrom
		}
		if err := t.recordEpoch(ep, signing.key); err != nil {
			return err
		}
	}
	return t.resealSealedStore(report)
}

// rechainFrom recomputes every entry hash from `from` to the head — the
// reseal's second half. Signatures re-mint where the signing state requires
// them and revert to the placeholder where it does not: an old signature over
// a moved hash would only ever read as tampering.
func (t *txn) rechainFrom(from int64) ([]byte, error) {
	prev := zeroHash
	if from > 1 {
		var raw []byte
		if err := t.row(`SELECT hash FROM changelog WHERE seq = $1`, from-1).Scan(&raw); err != nil {
			return nil, fmt.Errorf("substrate/engine: rechain: read the hash at seq %d: %w", from-1, err)
		}
		if len(raw) != 32 {
			return nil, fmt.Errorf("substrate/engine: rechain: seq %d has no hash to chain from", from-1)
		}
		copy(prev[:], raw)
	}
	after := from - 1
	head := prev
	signing := t.ds.signing()
	for {
		entries, err := t.chainPage(after, rebuildBatch)
		if err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			h, err := entryHash(t.ds.scope.Repository, e, prev)
			if err != nil {
				return nil, err
			}
			// Below signed_from_seq the placeholder stays: a reseal moves
			// values, it does not extend the signature guarantee backwards.
			sig := sigPlaceholder
			if signing.signedFrom > 0 && e.Seq >= signing.signedFrom {
				if signing.key == nil {
					return nil, fmt.Errorf("substrate/engine: rechain: signing is active from seq %d but the signing key is unavailable", signing.signedFrom)
				}
				sig = ed25519.Sign(signing.key, h[:])
			}
			res, err := t.exec(`UPDATE changelog SET hash = $2, sig = $3 WHERE seq = $1`, e.Seq, h[:], sig)
			if err != nil {
				return nil, err
			}
			if n, err := res.RowsAffected(); err != nil {
				return nil, err
			} else if n != 1 {
				return nil, fmt.Errorf("substrate/engine: rechain: stamping seq %d touched %d rows", e.Seq, n)
			}
			prev, head = h, h
			after = e.Seq
		}
	}
	return head[:], nil
}

// resealRecords moves one kind's legacy values into the store and fills the
// current-value map for the changelog rewrite. Tombstoned rows keep their
// properties until purge, so they migrate too.
func (t *txn) resealRecords(report *ResealReport, ident string, names []string, current map[currentRefKey]string) error {
	// The cursor closes before any migration runs: one transaction is one
	// connection, and a query under an open cursor is refused.
	type scanned struct {
		id  string
		raw []byte
	}
	var all []scanned
	rows, err := t.query(`SELECT id, props FROM records WHERE kind = $1`, ident)
	if err != nil {
		return err
	}
	for rows.Next() {
		var s scanned
		if err := rows.Scan(&s.id, &s.raw); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, s := range all {
		props, err := decodeNumberPreserving(s.raw)
		if err != nil {
			return fmt.Errorf("substrate/engine: reseal %s %s: %w", ident, s.id, err)
		}
		changed := false
		for _, name := range names {
			v, ok := props[name].(string)
			if !ok || v == "" {
				continue
			}
			current[currentRefKey{kind: ident, id: s.id, prop: name}] = v
			if strings.HasPrefix(v, secretRefPrefix) {
				continue
			}
			ref, err := t.migrateSecretValue(eref{Kind: ident, ID: s.id}, v)
			if err != nil {
				return fmt.Errorf("substrate/engine: reseal %s.%s of %s: %w", ident, name, s.id, err)
			}
			props[name] = ref
			current[currentRefKey{kind: ident, id: s.id, prop: name}] = ref
			changed = true
		}
		if !changed {
			continue
		}
		out, err := json.Marshal(props)
		if err != nil {
			return err
		}
		if _, err := t.exec(`UPDATE records SET props = $1 WHERE kind = $2 AND id = $3`,
			out, ident, s.id); err != nil {
			return err
		}
		report.Records++
	}
	return nil
}

// migrateSecretValue turns one legacy stored value into a ref: the retired
// inline-sealed form opens first; a plaintext that turns out to be the
// record's own existing sealed-store ref (a legacy inline-sealed tokenRef)
// unwraps to that ref; anything else is material and moves into the store.
func (t *txn) migrateSecretValue(owner eref, stored string) (string, error) {
	plain := stored
	if strings.HasPrefix(stored, sealedPropPrefix) {
		opened, err := t.ds.svc.openPropValue(stored)
		if err != nil {
			return "", err
		}
		plain = opened
	}
	if owned, err := t.sealedRefOf(plain, owner); err != nil {
		return "", err
	} else if owned {
		return plain, nil
	}
	return t.storeSecretValue(owner, plain)
}

// resealChangelog re-points every historical secret value at the record's
// current ref, one page at a time with the page's rewrites flushed before
// the next page loads, so memory stays bounded by the batch. It returns the
// FIRST rewritten seq (0 when nothing moved): where the re-chain starts.
func (t *txn) resealChangelog(report *ResealReport, secretProps map[string][]string, current map[currentRefKey]string) (int64, error) {
	isSecret := func(kind, prop string) bool {
		for _, name := range secretProps[kind] {
			if name == prop {
				return true
			}
		}
		return false
	}
	var after, minSeq int64
	for {
		type pending struct {
			seq     int64
			payload []byte
		}
		var updates []pending
		rows, err := t.query(`
			SELECT seq, payload FROM changelog
			WHERE seq > $1 ORDER BY seq LIMIT $2`, after, rebuildBatch)
		if err != nil {
			return 0, err
		}
		n := 0
		for rows.Next() {
			var seq int64
			var raw []byte
			if err := rows.Scan(&seq, &raw); err != nil {
				_ = rows.Close()
				return 0, err
			}
			n++
			after = seq
			if len(raw) == 0 {
				continue
			}
			payload, err := decodeNumberPreserving(raw)
			if err != nil {
				_ = rows.Close()
				return 0, fmt.Errorf("substrate/engine: reseal changelog seq %d: %w", seq, err)
			}
			changed := false
			forEachRecordDeltaSet(payload, func(kindRef, recordID string, set map[string]any) {
				for prop, v := range set {
					s, ok := v.(string)
					if !ok || s == "" || !isSecret(kindRef, prop) {
						continue
					}
					target, live := current[currentRefKey{kind: kindRef, id: recordID, prop: prop}]
					if !live {
						target = erasedSecretRef
					}
					if s != target {
						set[prop] = target
						changed = true
					}
				}
			})
			if !changed {
				continue
			}
			out, err := json.Marshal(payload)
			if err != nil {
				_ = rows.Close()
				return 0, err
			}
			updates = append(updates, pending{seq: seq, payload: out})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, err
		}
		_ = rows.Close()
		for _, u := range updates {
			if _, err := t.exec(`UPDATE changelog SET payload = $1 WHERE seq = $2`,
				u.payload, u.seq); err != nil {
				return 0, err
			}
			if minSeq == 0 || u.seq < minSeq {
				minSeq = u.seq
			}
			report.Entries++
		}
		if n < rebuildBatch {
			return minSeq, nil
		}
	}
}

// resealSealedStore is the migration's sealed-store half.
func (t *txn) resealSealedStore(report *ResealReport) error {
	n, err := t.rekeySealedStore()
	report.SealedRows += n
	return err
}

// rekeySealedStore re-keys every sealed payload the DEK does not already
// open: keyless plain framings and host-key-sealed legacies alike. The scan
// takes every row FOR UPDATE, so a concurrent TOTP step consume or token
// refresh serializes behind this transaction instead of being overwritten by
// a stale buffered copy. A payload the DEK already opens passes
// byte-identical, which is the idempotency. Shared by `repository reseal`
// and recovery enrollment: the recovery promise is only true once every
// payload is under the DEK the recovery key wraps.
func (t *txn) rekeySealedStore() (int, error) {
	dekAEAD, err := aeadOf(t.ds.dek)
	if err != nil {
		return 0, err
	}
	type pending struct {
		ref     string
		payload []byte
	}
	total := 0
	after := ""
	// One page of rows at a time, flushed before the next loads, so memory
	// stays bounded by the batch; the FOR UPDATE locks accumulate for the
	// transaction either way, which is what keeps a concurrent step consume
	// or token refresh serialized behind the rewrite.
	for {
		var updates []pending
		rows, err := t.query(`
			SELECT ref, payload FROM sealed
			WHERE ref > $1 ORDER BY ref LIMIT $2 FOR UPDATE`, after, rebuildBatch)
		if err != nil {
			return total, err
		}
		n := 0
		for rows.Next() {
			var ref string
			var payload []byte
			if err := rows.Scan(&ref, &payload); err != nil {
				_ = rows.Close()
				return total, err
			}
			n++
			after = ref
			if len(payload) > 0 && payload[0] == credSealed && dekAEAD != nil {
				if _, err := openWith(dekAEAD, payload); err == nil {
					continue
				}
			}
			raw, err := t.ds.openPayload(payload)
			if err != nil {
				_ = rows.Close()
				return total, fmt.Errorf("substrate/engine: re-key sealed %s: %w", ref, err)
			}
			sealed, err := t.ds.sealPayload(raw)
			if err != nil {
				_ = rows.Close()
				return total, err
			}
			updates = append(updates, pending{ref: ref, payload: sealed})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return total, err
		}
		_ = rows.Close()
		for _, u := range updates {
			if _, err := t.exec(`UPDATE sealed SET payload = $1 WHERE ref = $2`,
				u.payload, u.ref); err != nil {
				return total, err
			}
			total++
		}
		if n < rebuildBatch {
			return total, nil
		}
	}
}

// decodeNumberPreserving decodes stored JSONB without flattening numbers to
// float64: a rewritten payload must re-marshal every untouched value
// byte-faithfully, concurrency tokens included.
func decodeNumberPreserving(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
