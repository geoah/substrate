package engine

// A repository whose secrets did not travel (decision 0264).
//
// A repository's secrets are the sealed store: the login credential's
// password hash and TOTP seed, every account's OAuth token, every
// secret-typed property such as a provider's API key. They seal under the
// repository's data-encryption key, and the DEK is wrapped under the host
// credential key and nowhere else. An archive, an export of it or a 0.x
// directory brought to a host that does not hold that key carries secrets
// nothing there opens.
//
// A restore (restoreRow: the one door for an archive the database has no row
// of, an archive the operator takes, and a 0.x directory) refuses such a
// copy, naming the key id its wrap was written under, because the usual
// answer is to set that key, which keeps the secrets. So does the staging of
// a take. The operator who cannot names the repository in
// SUBSTRATE_IMPORT_WITHOUT_SECRETS (WithImportWithoutSecrets), and accepts
// the loss: the restore mints the repository a new DEK under this host's key
// and restores the rest, the sealed values that do not open under the new
// key included. Every record and every line of history arrives; only the
// secrets do not, nor the spooled webhook requests, which seal under the DEK
// and which the open deletes with them. A directory from before repository
// ids is keyed at the same boot (repositoryid.go), and the same listing lets
// its keying mint the new DEK.
//
// THE RULE: after the restore, no read, listing or use of a secret-typed
// property returns or acts on a ref whose sealed value the store does not
// hold. It reads as unset, whatever state the property's package is in then
// or later (admitted, parked, uninstalled with its records left, installed
// again) and however its type is spelled (`secret`, a property type based on
// it, the OAuth facility's `tokenRef`), and the owner is shown what to put
// back.
//
// IT HOLDS BY ORDER, NOT BY A LIST. A sealed value that does not open is
// itself the record of its loss: it names, in the clear, its ref and the
// record holding it. The restore keeps every one in the sealed store and
// marks the repository's row as it creates it
// (repositories.secrets_left_behind); the next open takes each one's ref out
// of the record that holds it, row by row and at any depth, whatever that
// record's kind is now, and removes the value in the same transaction
// (clearSecretsLeftBehind), then lifts the mark once it could clear every
// one. Nothing removes such a value but the write that clears its ref (or
// the GC's sweep of one no record holds): the archive's next pass leaves it out of
// the manifest because the table no longer holds it. A crash anywhere repeats
// harmlessly: what is left is found again. A value that opens under the
// repository's key is never touched, so a retry under the key the copy was
// written under keeps every secret. A second restore of the same copy finds
// the values the first one did not clear, and goes on. Each value taken
// raises an alert naming the record and the property, the console's list of
// what to put back, which the write that puts a value there again resolves;
// an account whose token it was also reads `erroring` with the reason, where
// its kind declares the facility's status. The credential's refs go too, so
// the owner signs in again only after `substratectl user reset`, which the
// host key still allows. Once the mark is lifted, a value that stops opening
// is damage, not a loss the operator accepted.

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ImportWithoutSecretsVar is the variable an operator lists the repositories
// in whose secrets the boot may import without.
const ImportWithoutSecretsVar = "SUBSTRATE_IMPORT_WITHOUT_SECRETS"

// lostTokenError is what an account whose token stayed behind says, the
// reason the console shows beside its reconnect.
const lostTokenError = "the sign-in to this account did not move with the repository to this server; reconnect it"

// WithImportWithoutSecrets names the repositories, each by its name or its
// id, that the boot imports without their secrets when this host's key does
// not open them (SUBSTRATE_IMPORT_WITHOUT_SECRETS). What follows the restore
// is the repository's mark, not the listing, so a repository the operator
// stops listing afterwards loses nothing more and keeps nothing it lost.
func WithImportWithoutSecrets(repositories ...string) Option {
	return func(o *options) {
		for _, r := range repositories {
			if r = strings.TrimSpace(r); r != "" {
				o.withoutSecrets = append(o.withoutSecrets, r)
			}
		}
	}
}

// lostSecretsList is the listed repositories as a set.
func lostSecretsList(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// acceptsLostSecrets reports whether the operator listed the repository,
// by its id or its name.
func (s *service) acceptsLostSecrets(id, name string) bool {
	return s.withoutSecrets[id] || s.withoutSecrets[name]
}

// refuseUnopened is the refusal of a copy whose DEK this host's key does not
// open: why, how to keep the secrets, and how to accept losing them. id is
// empty for a directory from before ids, which takes one at its import.
func refuseUnopened(name, id string, cause error) error {
	listed := name
	if id != "" {
		listed = name + " or " + id
	}
	return fmt.Errorf("the DEK in the manifest of repository %s does not open: %w. Restoring it would leave a repository whose secrets nothing opens. Set the key it was written under and restart, which keeps them; or, to restore it without its secrets (every account reconnected, every API key entered again, and the owner's sign-in set again with `substratectl user reset`), list %s in %s and restart",
		name, cause, listed, ImportWithoutSecretsVar)
}

// leaveSecretsBehind mints a new DEK for a listed repository whose copy this
// host's key does not open. It writes nothing: the sealed values stay in the
// copy for the restore to load and the open to clear
// (clearSecretsLeftBehind), and so do the spooled requests, which the open
// deletes. A boot cut short after it finds the same refusal and the same
// listing again.
//
// A host with no credential key refuses: its new DEK would be stored
// unsealed.
func (s *service) leaveSecretsBehind(name, id string, cause error) ([]byte, error) {
	if len(s.credKey) == 0 {
		return nil, fmt.Errorf("repository %s is listed in %s, but this host has no credential key: restoring it without its secrets would store its new DEK unsealed, so nothing was restored. Set SUBSTRATE_CREDENTIAL_KEY and restart (the DEK in its manifest does not open: %w)",
			name, ImportWithoutSecretsVar, cause)
	}
	dek, err := newDEK()
	if err != nil {
		return nil, err
	}
	s.log.Warn("substrate: restoring a repository without its secrets: this host's key does not open them and the operator listed it",
		"repository", name, "id", id, "variable", ImportWithoutSecretsVar, "cause", cause.Error())
	return dek, nil
}

// leftBehind is one sealed value a restore left behind: its ref and the
// record that holds it.
type leftBehind struct {
	ref, kind, id string
}

// alertKeySecretLeftBehind keys the alert raised for one property whose
// value a restore left behind.
const alertKeySecretLeftBehind = "secret.left-behind/"

func secretLeftBehindKey(owner eref, property string) string {
	return alertKeySecretLeftBehind + vocabulary.RecordPath(owner.Kind, owner.ID) + "/" + property
}

// leftBehindStage is the restore's step the test seam names
// (testImportFault): the repository's new DEK minted, nothing keyed or
// written yet.
const leftBehindStage = "after the secrets were left behind"

// clearSecretsLeftBehind is the open's half of a restore without secrets, on
// a repository the restore marked: every sealed value that does not open under
// the repository's DEK has its ref taken out of the record that held it and
// is removed, in one transaction, and the mark is lifted once that commits. A
// value that opens is kept, and so is one whose ref its record does not hold
// anywhere, with the mark: nothing removes a value but the write that clears
// its ref, or the GC's sweep of a value no record holds. The writes are the system's, folded as a
// migration's are, because a required secret (the credential's password ref)
// has no valid value left to write. Before them, the spooled webhook requests
// that do not open under the DEK are deleted: they name no record, and the
// drain would only set each aside as damage.
func (ds *dataset) clearSecretsLeftBehind(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	var marked bool
	if err := ds.svc.maint.QueryRowContext(ctx, `SELECT secrets_left_behind FROM repositories WHERE id = $1`, ds.info.ID).
		Scan(&marked); err != nil {
		return fmt.Errorf("substrate/engine: read whether repository %s has secrets left behind: %w", ds.name(), err)
	}
	if !marked {
		return nil
	}
	spooled, err := ds.dropUnopenedSpool()
	if err != nil {
		return fmt.Errorf("substrate/engine: delete the spooled requests repository %s left behind: %w", ds.name(), err)
	}
	var lost, cleared, accounts int
	var unfound []string
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		rows, err := t.query(`SELECT ref, record_kind, record_id, payload FROM sealed ORDER BY ref FOR UPDATE`)
		if err != nil {
			return err
		}
		var gone []leftBehind
		for rows.Next() {
			var lb leftBehind
			var payload []byte
			if err := rows.Scan(&lb.ref, &lb.kind, &lb.id, &payload); err != nil {
				_ = rows.Close()
				return err
			}
			if _, err := openRepoPayload(payload, ds.dek, sealedAAD(lb.ref, lb.kind, lb.id)); err != nil {
				gone = append(gone, lb)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, lb := range gone {
			n, account, err := t.clearLeftBehind(lb)
			if err != nil {
				return err
			}
			// A value is removed only with the write that takes its ref
			// out: one whose ref its record does not hold anywhere stays,
			// and so does the mark, for the GC's sweep of values no record
			// holds (gc.go sweepUnheldSealed) and the next open.
			if n == 0 {
				unfound = append(unfound, lb.ref)
				continue
			}
			cleared += n
			if account {
				accounts++
			}
			if _, err := t.exec(`DELETE FROM sealed WHERE ref = $1`, lb.ref); err != nil {
				return err
			}
			lost++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: clear the secrets repository %s (id %s) left behind: %w", ds.name(), ds.info.ID, err)
	}
	if len(unfound) > 0 {
		ds.svc.log.Warn("substrate: secrets a repository restored without them left behind are not held where their records say; they stay, and the next open looks again once the GC's sweep has taken the ones no record holds",
			"repository", ds.name(), "id", ds.info.ID, "refs", len(unfound))
	} else if _, err := ds.svc.maint.ExecContext(ctx, `UPDATE repositories SET secrets_left_behind = false WHERE id = $1`, ds.info.ID); err != nil {
		return fmt.Errorf("substrate/engine: lift the mark of the secrets repository %s left behind: %w", ds.name(), err)
	}
	if lost > 0 || spooled > 0 {
		ds.svc.log.Warn("substrate: cleared the secrets a repository restored without them still named; each is an alert its owner sees, the accounts read erroring, and the owner signs in after `substratectl user reset`",
			"repository", ds.name(), "id", ds.info.ID, "valuesRemoved", lost, "secretsCleared", cleared, "accountsToReconnect", accounts,
			"spooledRequestsDeleted", spooled)
	}
	return nil
}

// dropUnopenedSpool deletes every spooled webhook request that does not open
// under the repository's DEK, and says how many. A request that opens is
// kept for the drain.
func (ds *dataset) dropUnopenedSpool() (int, error) {
	files, err := changelogfile.SpoolFiles(ds.dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range files {
		_, fid, err := changelogfile.ParseSpoolFileName(f.Name)
		if err != nil {
			continue
		}
		sealed, err := changelogfile.ReadSpoolFile(ds.dir, f.Name)
		if err != nil {
			return n, err
		}
		if _, err := ds.openPayload(sealed, spoolAAD(ds.info.ID, fid)); err == nil {
			continue
		}
		if err := changelogfile.RemoveSpoolFile(ds.dir, f.Name); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// clearLeftBehind takes one ref out of the record that held it, live or
// tombstoned, in whatever property holds it and at any depth (dropRef),
// without asking the record's kind: a kind the registry does not hold now may come
// back. It reports how many properties it cleared and whether one was an
// account's token. A record purged since holds nothing, and a value the
// owner already replaced is not the ref.
func (t *txn) clearLeftBehind(lb leftBehind) (int, bool, error) {
	owner := eref{Kind: lb.kind, ID: lb.id}
	row, err := t.loadRow(owner, true)
	if err != nil || row == nil {
		return 0, false, err
	}
	before := row.clone()
	row.Props = maps.Clone(row.Props)
	var lost []string
	for _, name := range sortedKeys(row.Props) {
		v, found := dropRef(row.Props[name], lb.ref)
		if !found {
			continue
		}
		// A property that held only the ref goes; one that held more keeps
		// the rest of what it held.
		if l, ok := v.([]any); v == nil || (ok && len(l) == 0) {
			delete(row.Props, name)
		} else {
			row.Props[name] = v
		}
		lost = append(lost, name)
	}
	if len(lost) == 0 {
		return 0, false, nil
	}
	changed := slices.Clone(lost)
	account := slices.Contains(lost, propTokenRef)
	if account {
		// The facility's status where the kind declares it; where nothing
		// declares the kind now, only what the row already holds, which a
		// declaration admitted when it was written.
		ty, _ := t.resolveType(row.Kind)
		for _, st := range []struct {
			name  string
			value any
		}{
			{propTokenStatus, tokenStatusErroring},
			{propTokenError, lostTokenError},
			{propTokenErrorAt, t.now.UTC().Format(time.RFC3339Nano)},
		} {
			_, held := row.Props[st.name]
			if (ty != nil && oauthWrites(ty, st.name)) || (ty == nil && held) {
				row.Props[st.name] = st.value
				changed = append(changed, st.name)
			}
		}
	}
	res, err := t.foldRow(before, row, false, false)
	if err != nil {
		return 0, false, err
	}
	if !res.changed {
		return 0, false, nil
	}
	if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, row.ID, row.Kind,
		map[string]any{"properties": changed}); err != nil {
		return 0, false, err
	}
	path := vocabulary.RecordPath(row.Kind, row.ID)
	for _, name := range lost {
		summary := fmt.Sprintf("%s: %s did not move with the repository to this server; enter it again", path, name)
		if name == propTokenRef {
			summary = fmt.Sprintf("%s: the sign-in to this account did not move with the repository to this server; reconnect it", path)
		}
		if err := t.raiseAlert(substrate.ActorSystem, alertRaise{
			key: secretLeftBehindKey(owner, name), level: alertLevelWarning, summary: summary,
			detail: "The repository was imported without its secrets, because this server's credential key does not open them (SUBSTRATE_IMPORT_WITHOUT_SECRETS).",
			about:  []string{path}, count: 1,
		}); err != nil {
			return 0, false, err
		}
	}
	return len(lost), account, nil
}

// dropRef is v without ref, at any depth, as the GC's ownership predicate
// finds a ref (credentials.go sealedUnheld, `$.**`): a string equal to ref
// goes from the object or the list that holds it, and nil stands for v that
// is the ref itself. Everything else is kept as it was. found says whether it
// took one out.
func dropRef(v any, ref string) (_ any, found bool) {
	switch x := v.(type) {
	case string:
		if x == ref {
			return nil, true
		}
	case map[string]any:
		out := x
		for _, k := range sortedKeys(x) {
			e, ok := dropRef(x[k], ref)
			if !ok {
				continue
			}
			if !found {
				out, found = maps.Clone(x), true
			}
			if e == nil {
				delete(out, k)
			} else {
				out[k] = e
			}
		}
		return out, found
	case []any:
		out := make([]any, 0, len(x))
		for _, el := range x {
			e, ok := dropRef(el, ref)
			found = found || ok
			if ok && e == nil {
				continue
			}
			out = append(out, e)
		}
		if found {
			return out, true
		}
	}
	return v, false
}

// settleSecretsLeftBehind resolves the alert of each secret property a write
// gave a value again: the owner put back what the restore left behind.
func (t *txn) settleSecretsLeftBehind(ty *vocabulary.Kind, owner eref, row *erow, accepted []string) error {
	for _, name := range accepted {
		p, ok := ty.Prop(name)
		if !ok || !p.Secret() {
			continue
		}
		if v, ok := row.Props[name].(string); !ok || v == "" {
			continue
		}
		if err := t.resolveAlert(substrate.ActorSystem, secretLeftBehindKey(owner, name), nil); err != nil {
			return err
		}
	}
	return nil
}
