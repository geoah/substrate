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
	if err := t.resealChangelog(report, secretProps, current); err != nil {
		return err
	}
	return t.resealSealedStore(report)
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
// the next page loads, so memory stays bounded by the batch.
func (t *txn) resealChangelog(report *ResealReport, secretProps map[string][]string, current map[currentRefKey]string) error {
	isSecret := func(kind, prop string) bool {
		for _, name := range secretProps[kind] {
			if name == prop {
				return true
			}
		}
		return false
	}
	var after int64
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
			return err
		}
		n := 0
		for rows.Next() {
			var seq int64
			var raw []byte
			if err := rows.Scan(&seq, &raw); err != nil {
				_ = rows.Close()
				return err
			}
			n++
			after = seq
			if len(raw) == 0 {
				continue
			}
			payload, err := decodeNumberPreserving(raw)
			if err != nil {
				_ = rows.Close()
				return fmt.Errorf("substrate/engine: reseal changelog seq %d: %w", seq, err)
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
				return err
			}
			updates = append(updates, pending{seq: seq, payload: out})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		for _, u := range updates {
			if _, err := t.exec(`UPDATE changelog SET payload = $1 WHERE seq = $2`,
				u.payload, u.seq); err != nil {
				return err
			}
			report.Entries++
		}
		if n < rebuildBatch {
			return nil
		}
	}
}

// resealSealedStore re-keys every payload not already under the repository's
// DEK: keyless plain framings and host-key-sealed legacies alike. A payload
// the DEK already opens passes byte-identical, which is the idempotency.
func (t *txn) resealSealedStore(report *ResealReport) error {
	dekAEAD, err := aeadOf(t.ds.dek)
	if err != nil {
		return err
	}
	type pending struct {
		ref     string
		payload []byte
	}
	var updates []pending
	rows, err := t.query(`SELECT ref, payload FROM sealed`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ref string
		var payload []byte
		if err := rows.Scan(&ref, &payload); err != nil {
			_ = rows.Close()
			return err
		}
		if len(payload) > 0 && payload[0] == credSealed && dekAEAD != nil {
			if _, err := openWith(dekAEAD, payload); err == nil {
				continue
			}
		}
		raw, err := t.ds.openPayload(payload)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("substrate/engine: reseal sealed %s: %w", ref, err)
		}
		sealed, err := t.ds.sealPayload(raw)
		if err != nil {
			_ = rows.Close()
			return err
		}
		updates = append(updates, pending{ref: ref, payload: sealed})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, u := range updates {
		if _, err := t.exec(`UPDATE sealed SET payload = $1 WHERE ref = $2`,
			u.payload, u.ref); err != nil {
			return err
		}
		report.SealedRows++
	}
	return nil
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
