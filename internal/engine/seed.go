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
// A authority whose stored rows are NOT shipped vocabulary (a user or a bundle
// took the name) is skipped whole: the upgrade never seizes a name it does not
// already own here.
func (ds *dataset) upgradeShippedVocabulary(ctx context.Context) error {
	reg := ds.svc.base
	stored, err := ds.storedDeclarationVersions(ctx)
	if err != nil {
		return err
	}
	current := ds.registry()

	// What each shipped authority would have to write, and what it would leave
	// alone. A authority with nothing to write is not touched at all.
	upgrade := map[string]bool{}
	keep := map[string]bool{}
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
			case compareSchemaVersion(d.version(), have) > 0:
				write = true // the shipped declaration moved forward
			default:
				keep[d.key()] = true // same or older than stored: never a downgrade
			}
		}
		if write {
			upgrade[aname] = true
		}
	}
	if len(upgrade) == 0 {
		return nil
	}

	// ONE TRANSACTION for the whole repository, under the substrate's own
	// actor, so the upgrade is a legible set of entries in the changelog and either
	// all of it lands or none of it does.
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.lockKey(registryDepKey(ds)); err != nil {
			return err
		}
		_, err := t.projectAuthorities(reg, upgrade, projectOpts{
			skip: func(key string) bool { return keep[key] },
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: upgrade shipped vocabulary of %s: %w", ds.info.Name, err)
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

// storedDeclarationVersions reads every stored declaration's version, keyed
// exactly as authorityDeclarations keys it. A row without one reads as the empty
// version, which compares below everything — so a declaration written before
// versions were mandatory upgrades on the next open rather than sticking.
func (ds *dataset) storedDeclarationVersions(ctx context.Context) (map[string]string, error) {
	args := make([]any, 0, len(vocabularyKindRefs))
	ph := make([]string, 0, len(vocabularyKindRefs))
	for i, ident := range vocabularyKindRefs {
		args = append(args, ident)
		ph = append(ph, "$"+strconv.Itoa(i+1))
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, id, COALESCE(props->>'version', '') FROM records
		WHERE kind IN (`+strings.Join(ph, ", ")+`) AND deleted_at IS NULL`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var typ, id, version string
		if err := rows.Scan(&typ, &id, &version); err != nil {
			return nil, err
		}
		out[typ+"\x00"+id] = version
	}
	return out, rows.Err()
}

// --- version ordering ---------------------------------------------------------

// compareSchemaVersion orders two declaration versions the way Kubernetes
// orders API versions, because that is the shape the manifests use
// (`v1alpha1`, `v1beta2`, `v1`, `v2`): a GA version outranks any pre-release
// of the same major, beta outranks alpha, and the trailing number breaks the
// tie. Anything unparseable falls back to a plain string comparison, so two
// spellings of the same convention still order deterministically and an
// unfamiliar one never silently wins.
//
// It returns -1, 0 or 1 for a < b, a == b, a > b.
func compareSchemaVersion(a, b string) int {
	if a == b {
		return 0
	}
	av, aok := parseVocabularyVersion(a)
	bv, bok := parseVocabularyVersion(b)
	if !aok || !bok {
		return strings.Compare(a, b)
	}
	if av.major != bv.major {
		return cmpInt(av.major, bv.major)
	}
	if av.stage != bv.stage {
		return cmpInt(av.stage, bv.stage)
	}
	return cmpInt(av.minor, bv.minor)
}

// vocabularyVersion is a parsed `v<major>[alpha|beta<minor>]`.
type vocabularyVersion struct {
	major int
	// stage orders the maturity: alpha 0, beta 1, GA 2.
	stage int
	minor int
}

func parseVocabularyVersion(s string) (vocabularyVersion, bool) {
	var v vocabularyVersion
	rest, ok := strings.CutPrefix(s, "v")
	if !ok || rest == "" {
		return v, false
	}
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return v, false
	}
	major, err := strconv.Atoi(rest[:digits])
	if err != nil {
		return v, false
	}
	v.major = major
	tail := rest[digits:]
	switch {
	case tail == "":
		v.stage = 2 // GA
		return v, true
	case strings.HasPrefix(tail, "alpha"):
		v.stage = 0
		tail = strings.TrimPrefix(tail, "alpha")
	case strings.HasPrefix(tail, "beta"):
		v.stage = 1
		tail = strings.TrimPrefix(tail, "beta")
	default:
		return v, false
	}
	if tail == "" {
		return v, true
	}
	minor, err := strconv.Atoi(tail)
	if err != nil {
		return v, false
	}
	v.minor = minor
	return v, true
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
