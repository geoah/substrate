package engine

// THE SEED RULE.
//
// The vocabulary a repository speaks is written in its OWN changelog, and nowhere
// else. Three write paths put it there, and they are the only three:
//
//   - THE SEED, once, at creation. The binary's embedded tree is copied into
//     the new repository's changelog as ordinary record entries under the actor
//     `bundle:core` — one transaction with the credential and the rest of the
//     creation (engine.go). Afterwards the tree has no standing over that
//     repository at all: dataset open READS the rows back (loadStoredVocabulary)
//     and never re-projects the tree. The v0 re-assert-and-prune at open is
//     deleted, and pruning shipped rows with it.
//   - THE UPGRADE, at the first open under a binary whose tree moved. Every
//     declaration carries a `version`; this diffs the repository's shipped
//     declarations against the binary's and APPENDS the difference as explicit
//     entries under the actor `substrate`. Convergent, idempotent,
//     one transaction per repository — and a repository nobody opens is never
//     touched.
//   - THE INSTALL, when a user installs a bundle: the catalog's manifests
//     are COPIED into the changelog under `bundle:<name>` (catalog/catalog.go). The
//     embedded catalog is a source, never an authority; nothing on the serving
//     path reads it.
//
// And one function decides who may write a declaration at all:
// authorizeDeclarationWrite, below.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// --- the authority chokepoint -------------------------------------------------

// authorizeDeclarationWrite is THE ONE PLACE that decides who may write a kind
// DECLARATION. Every declaration write in the
// engine reaches it through applyVocabularyBatch.
//
//   - SHIPPED vocabulary — an authority whose stored rows say `source: builtin`,
//     which is exactly what the creation seed and shipped upgrades write — is
//     writable only by a SUBSTRATE PATH: the seed, an install or an upgrade,
//     which is what the actors `substrate` and `bundle:<name>` name.
//     A generic API write into one is refused here; those actors cannot be
//     claimed by a request (substrate.ReservedActor, checked on the
//     X-Substrate-Actor header), so "substrate path" means what it says.
//   - Everything else — the repository's own kinds, and the bundle authorities
//     it installed — belongs to the repository's user, who may write it.
//
// It takes the authority NAME and the registry that currently holds it, so an authority
// nobody has yet (a first declaration) is the user's to create.
func authorizeDeclarationWrite(actor substrate.Actor, current *vocabulary.Registry, authority string) error {
	cur, ok := current.AuthorityByName(authority)
	if !ok || cur.Source != vocabulary.SourceBuiltin {
		return nil
	}
	if isSubstratePath(actor) {
		return nil
	}
	return fmt.Errorf("%w: %s is shipped vocabulary — it changes with the substrate (seed, upgrade, install), not through the API",
		substrate.ErrForbidden, authority)
}

// isSubstratePath reports whether an actor is one of the substrate's own
// vocabulary-writing hands: the engine itself (seed, upgrade) or a bundle
// (`bundle:<name>` — the seed's `bundle:core` and every install).
func isSubstratePath(actor substrate.Actor) bool {
	return actor == substrate.ActorSystem || substrate.IsBundleActor(actor)
}

// --- the seed -----------------------------------------------------------------

// seedShippedSchema writes the binary's embedded tree into a BRAND NEW
// repository's changelog, as ordinary record entries. It runs inside the creation
// transaction (engine.go createRepository), under the actor the caller opened
// that transaction with — `bundle:core`.
//
// It never prunes and never re-asserts: this is the one and only time the
// embedded tree writes itself into this repository wholesale.
func (t *txn) seedShippedSchema(reg *vocabulary.Registry) error {
	authorities := shippedAuthorities(reg)
	if len(authorities) == 0 {
		return fmt.Errorf("substrate/engine: the binary ships no vocabulary to seed")
	}
	_, err := t.projectAuthorities(reg, authorities, projectOpts{})
	return err
}

// shippedAuthorities is the binary's own vocabulary: the authorities the embedded tree
// declares.
func shippedAuthorities(reg *vocabulary.Registry) map[string]bool {
	authorities := map[string]bool{}
	for _, g := range reg.AuthorityList() {
		if g.Source == vocabulary.SourceBuiltin {
			authorities[g.Name] = true
		}
	}
	return authorities
}

// --- the boot-time upgrade ----------------------------------------------------

// upgradeShippedVocabulary is the drift half of KI-13, closed. It
// runs at the FIRST OPEN of a repository in this process — the cheapest place
// that still satisfies "convergent, idempotent, per repository in one
// transaction", and the only one that leaves a repository nobody opens
// untouched.
//
// The diff is per DECLARATION and keyed on `version`:
//
//   - a declaration the binary ships and the repository lacks is APPENDED;
//   - a declaration whose shipped version is NEWER than the stored one is
//     re-projected;
//   - a declaration whose stored version is the same or NEWER is left exactly
//     as it stands — never a downgrade, and a repository ahead of the binary
//     simply stays ahead;
//   - nothing is ever pruned: a declaration the tree stopped shipping stays in
//     the repositories that already hold it, and a user-authored kind is not
//     the shipped tree's business in the first place.
//
// A re-projected declaration is held to the SAME refuse-breakage guards
// `/vocabulary/apply` takes: a narrowing diff that would strand live rows is
// refused, naming the repository, the kind, the property and the count. The two
// doors agreeing is the whole point — a guard one of them skips is not a guard,
// it is a shape the store can reach and the API cannot.
//
// A refusal SKIPS the upgrade; it does not fail the open. Failing would take
// the repository down and leave no way back in, since the migration the guard
// demands runs through the API that failure just closed.
//
// A authority whose stored rows are NOT shipped vocabulary (a user or a bundle
// took the name) is skipped whole: the upgrade never seizes a name it does not
// already own here.
func (ds *dataset) upgradeShippedVocabulary(ctx context.Context) error {
	reg := ds.svc.base
	stored, err := ds.storedDeclarations(ctx)
	if err != nil {
		return err
	}
	current := ds.registry()

	// What each shipped authority would have to write, and what it would leave
	// alone. A authority with nothing to write is not touched at all.
	upgrade := map[string]bool{}
	keep := map[string]bool{}
	// The kinds this upgrade will NOT rewrite, by identity: a declaration held
	// at its stored version keeps whatever shape it has, so it is not the
	// upgrade's business and must not be able to refuse the boot below.
	keptKinds := map[string]bool{}
	for _, aname := range sortedKeys(shippedAuthorities(reg)) {
		g, ok := reg.AuthorityByName(aname)
		if !ok {
			continue
		}
		if cur, ok := current.AuthorityByName(aname); ok && cur.Source != vocabulary.SourceBuiltin {
			continue // the name is somebody else's here; the tree does not take it
		}
		decls, err := authorityDeclarations(g)
		if err != nil {
			return err
		}
		write := false
		for _, d := range decls {
			have, exists := stored[d.key()]
			switch {
			case !exists:
				write = true // a declaration this repository has never had
			case vocabulary.CompareVersions(d.version(), have.version) > 0:
				write = true // the shipped declaration moved forward
			default:
				keep[d.key()] = true // same or older than stored: never a downgrade
				if d.typ == kindKind {
					keptKinds[d.id] = true
				}
			}
		}
		if write {
			upgrade[aname] = true
		}
	}
	if len(upgrade) == 0 {
		return nil
	}

	// The SAME refuse-breakage guards `/vocabulary/apply` takes
	// (vocabularywrite.go): a narrowing declaration diff — a property dropped,
	// renamed or kind-changed, an enum value or state removed, required added —
	// is refused while live rows still hold the old shape, with the count.
	//
	// The two doors used to disagree. An operator applying the same change by
	// hand was refused; the boot upgrade projected it silently, leaving rows
	// shaped one way under a declaration that said another, with nothing
	// anywhere reporting it. A guard only one door honors is not a guard.
	narrowings := classifyNarrowingsExcept(current, reg, upgrade, keptKinds)

	// The default check `/vocabulary/apply` takes, for the same reason the
	// narrowing guards are here: a declared default no write could store would
	// land at boot and break every create of that kind afterwards, and the door
	// that refuses it by hand would have caught it. It needs no live rows, so it
	// is decided before the transaction opens.
	badDefaults := checkDeclaredDefaults(reg, upgrade)

	// REFUSING THE UPGRADE IS NOT REFUSING THE REPOSITORY. A guard that failed
	// the open would take the repository down with it — and leave no way back
	// in, because the migration it demands has to be performed THROUGH the API
	// this failure just closed. So a stranded diff skips the upgrade, loudly:
	// the stored declarations stand, the repository opens on them, and the rows
	// the guard named can be deleted or backfilled by ordinary writes. The
	// binary's newer shape simply does not land until they are.
	//
	// This is the same answer /vocabulary/apply gives — the narrowing does not
	// land — differing only in what it costs a caller who did not ask for it.
	refused := badDefaults
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if len(refused) > 0 {
			return nil
		}
		if err := t.lockKey(registryDepKey(ds)); err != nil {
			return err
		}
		guards, err := narrowingGuards(t, narrowings)
		if err != nil {
			return err
		}
		if len(guards) > 0 {
			refused = guards
			return nil
		}
		_, err = t.projectAuthorities(reg, upgrade, projectOpts{
			skip: func(key string) bool { return keep[key] },
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: upgrade shipped vocabulary of %s: %w", ds.info.Name, err)
	}
	if len(refused) > 0 {
		// The message is the entire interface for the migration it is asking
		// for, so it names the repository, the kind, the property and the count.
		ds.svc.log.Error("substrate: REFUSED to upgrade a repository's shipped vocabulary — live rows hold the old shape, "+
			"or a declared default no write could store; "+
			"the stored declarations stand and this binary's newer ones will not land until it is resolved",
			"repository", ds.info.Name, "refused", strings.Join(refused, "; "))
		return nil
	}
	ds.svc.log.Info("substrate: upgraded a repository's shipped vocabulary from the embedded tree",
		"repository", ds.info.Name, "authorities", sortedKeys(upgrade))

	// The rows moved, so the live registry is rebuilt from them — the same
	// read every open does, so an upgraded repository and a freshly opened one
	// hold the identical registry.
	ds.mu.Lock()
	ds.reg = vocabulary.NewRegistry()
	ds.mu.Unlock()
	return ds.loadStoredVocabulary(ctx)
}

// storedDeclaration is one stored declaration as the version diff sees it:
// its version, and its declaring authority (the authority row's own id).
type storedDeclaration struct {
	version   int64
	authority string
}

// storedDeclarations reads every stored declaration's version and authority,
// keyed exactly as authorityDeclarations keys it. A row without a version —
// or one still holding a spelling the 0004 backfill somehow missed — reads
// as version 0, which compares below everything, so it upgrades on the next
// open rather than sticking.
func (ds *dataset) storedDeclarations(ctx context.Context) (map[string]storedDeclaration, error) {
	args := make([]any, 0, len(vocabularyKindRefs)+1)
	ph := make([]string, 0, len(vocabularyKindRefs))
	for i, ident := range vocabularyKindRefs {
		args = append(args, ident)
		ph = append(ph, "$"+strconv.Itoa(i+1))
	}
	args = append(args, kindAuthority)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, id, COALESCE(props->>'version', ''),
		       CASE WHEN kind = $`+strconv.Itoa(len(args))+` THEN id
		            ELSE COALESCE(props->>'authority', '') END
		FROM records
		WHERE kind IN (`+strings.Join(ph, ", ")+`) AND deleted_at IS NULL`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]storedDeclaration{}
	for rows.Next() {
		var typ, id, version, authority string
		if err := rows.Scan(&typ, &id, &version, &authority); err != nil {
			return nil, err
		}
		out[typ+"\x00"+id] = storedDeclaration{version: storedVersion(version), authority: authority}
	}
	return out, rows.Err()
}

// storedVersion parses the text a jsonb `props->>'version'` extraction hands
// back. Every row this binary writes holds a JSON number; anything else is
// the absent version 0, older than everything, so the row upgrades instead
// of sticking.
func storedVersion(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// Version ordering lives with the vocabulary (vocabulary.CompareVersions):
// the boot upgrade here, the upgrade preview and the tree checker all diff
// through the one comparator.
