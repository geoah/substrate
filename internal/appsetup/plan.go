// Package appsetup is what an app's approval sets up (decisions 0202 and
// 0214, docs/plans/app-setup.md): an authorization request may name, beside
// its scopes, packages at a version or later, of the binary's catalog or
// published by an authority document, the account the app delivers under and
// the webhook it delivers to. Plan is what approving them would do in one
// repository, row by row; a Runner sets the approved plan up as the owner
// before the code is issued. Both read the repository through
// substrate.Dataset and the closures through the catalog, so the HTTP layer
// keeps the handlers and the wire alone.
package appsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Catalog is what a plan and a run ask of the catalog: the closure a package
// id resolves to, through the binary's closures and the authority documents
// (decision 0212, catalog.Catalog.Resolve), the requirements and the upgrade
// preview of one, and the install door both sources take. catalog.Catalog is
// one; a test substitutes one that publishes from memory.
type Catalog interface {
	// ResolveFor is Resolve for one repository: a package under the
	// repository's own authority is its own to declare, never installed
	// from its own document (decision 0212).
	ResolveFor(ctx context.Context, id string, atLeast int64, ds substrate.Dataset) (*catalog.Bundle, error)
	Requirements(ctx context.Context, id string, ds substrate.Dataset) ([]*catalog.Bundle, []catalog.Unmet, error)
	Upgrade(ctx context.Context, id string, ds substrate.Dataset, held *substrate.BundleStatus) (*substrate.BundleUpgrade, error)
	InstallConfirmed(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset, confirm *substrate.ConversionConfirm) (*catalog.Bundle, []substrate.SuggestedMapping, error)
	// InstallApproved binds every package written to the approved install set.
	InstallApproved(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset, expect catalog.InstallExpectations) (*catalog.Bundle, []substrate.SuggestedMapping, error)
}

// accountConfigTrait is the trait an account detail's kind implements.
const accountConfigTrait = vocabulary.PackageCore + "/" + vocabulary.TraitAccountConfig

// packageKind is the kind of a package's own record, whose source and
// shipped-version stamp say whose the package is.
var packageKind = vocabulary.CoreKind(vocabulary.DocPackage)

// Options narrows a plan.
type Options struct {
	// Approved is the scopes the approval leaves checked, which every
	// closure the plan installs or upgrades is held to (decision 0215): a
	// package whose code reaches past them is blocked. The zero value
	// covers nothing.
	Approved scope.Approved
	// ActionOnly names packages whose rows are planned no further than their
	// action, their requirements and what blocks them: no closure facts and
	// no upgrade preview, which a reader that may not see the row has no
	// business paying for.
	ActionOnly map[string]bool
}

// Plan is what approving details would set up in ds's repository: one row
// per detail, in the request's order, and the digest of the rows an approval
// names. Nothing is cached: a row reads the repository as it is. cat
// resolves each package through the binary's closures and the authority
// documents; nil offers nothing.
func Plan(ctx context.Context, ds substrate.Dataset, cat Catalog, details []substrate.AuthorizationDetail, opts Options) ([]substrate.AuthorizationSetup, string, error) {
	if len(details) == 0 {
		return []substrate.AuthorizationSetup{}, "", nil
	}
	// A detail may name the owner's own package or kind by the repository's
	// full name, which is another spelling of `~` (decision record 0259).
	home := ds.Repository().Authority
	local := make([]substrate.AuthorizationDetail, len(details))
	for i, d := range details {
		d.Package = vocabulary.LocalizeRef(d.Package, home)
		d.Kind = vocabulary.LocalizeRef(d.Kind, home)
		local[i] = d
	}
	details = local
	p := &planner{
		ds: ds, cat: cat, opts: opts, statuses: map[string]*substrate.BundleStatus{},
		kinds: map[string]substrate.KindInfo{}, packages: map[string]*substrate.AuthorizationSetup{},
		owned: map[string]ownership{}, resolved: map[resolution]resolvedPackage{}, bundles: map[string]*catalog.Bundle{},
	}
	statuses, err := ds.BundleStatusesUncounted(ctx)
	if err != nil {
		return nil, "", err
	}
	for i := range statuses {
		p.statuses[statuses[i].ID] = &statuses[i]
	}
	kinds, err := ds.Kinds(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, k := range kinds {
		p.kinds[k.Identity] = k
	}
	rows := make([]substrate.AuthorizationSetup, len(details))
	// Package rows first: the account and webhook rows are held to them.
	for i, d := range details {
		if d.Type == substrate.DetailPackage {
			if rows[i], err = p.packageRow(ctx, d); err != nil {
				return nil, "", err
			}
			p.packages[d.Package] = &rows[i]
		}
	}
	for i, d := range details {
		switch d.Type {
		case substrate.DetailAccount:
			rows[i], err = p.accountRow(ctx, d)
		case substrate.DetailWebhook:
			rows[i], err = p.webhookRow(ctx, d)
		}
		if err != nil {
			return nil, "", err
		}
	}
	for i := range rows {
		if err := p.providerInputs(ctx, &rows[i]); err != nil {
			return nil, "", err
		}
	}
	hash, err := Hash(rows)
	if err != nil {
		return nil, "", err
	}
	return rows, hash, nil
}

// Hash digests rows as an approval names them: what each would do, without
// the progress a run reports on it.
func Hash(rows []substrate.AuthorizationSetup) (string, error) {
	plain := slices.Clone(rows)
	for i := range plain {
		plain[i].Progress = ""
	}
	raw, err := json.Marshal(plain)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// NeedsAction reports whether approving rows sets anything up.
func NeedsAction(rows []substrate.AuthorizationSetup) bool {
	return slices.ContainsFunc(rows, func(r substrate.AuthorizationSetup) bool { return r.Action != substrate.SetupNone })
}

// FirstBlocked is the first row approving cannot set up, -1 when none.
func FirstBlocked(rows []substrate.AuthorizationSetup) int {
	return slices.IndexFunc(rows, func(r substrate.AuthorizationSetup) bool { return r.Blocked != nil })
}

// Started is approved as its set up starts, each row carrying its first
// progress: done for a row now in place, which now plans, waiting for the
// rest.
func Started(approved, now []substrate.AuthorizationSetup) []substrate.AuthorizationSetup {
	out := slices.Clone(approved)
	for i := range out {
		out[i].Progress = substrate.ProgressWaiting
		if i < len(now) && now[i].Action == substrate.SetupNone {
			out[i].Progress = substrate.ProgressDone
		}
	}
	return out
}

// HeldRelease is the catalog release a repository holds of a package: the
// shipped version a provider install stamped, else the stored version, and 0
// when it holds none.
func HeldRelease(st *substrate.BundleStatus) int64 {
	if st == nil || !st.Installed {
		return 0
	}
	if st.ShippedVersion > 0 {
		return st.ShippedVersion
	}
	return st.Version
}

// ProviderPage is the console page a published package is managed on.
func ProviderPage(pkg string) string {
	authority, name := vocabulary.SplitPackageRef(pkg)
	return "/providers/" + url.PathEscape(authority) + "/" + url.PathEscape(name)
}

// planner reads once what one plan needs: every bundle's status, quarantined
// ones included, the registry, each package record it asks whose it is, and
// each package the catalog resolves for it.
type planner struct {
	ds       substrate.Dataset
	cat      Catalog
	opts     Options
	statuses map[string]*substrate.BundleStatus
	kinds    map[string]substrate.KindInfo
	// packages are the request's package rows by package id, planned before
	// the account and webhook rows that are held to them.
	packages map[string]*substrate.AuthorizationSetup
	owned    map[string]ownership
	// resolved is what the catalog answered for each package at each floor
	// it was asked at, once: a document-published package costs its
	// publisher's document a read.
	resolved map[resolution]resolvedPackage
	// bundles are the closures the plan resolved, by package id, which the
	// account and webhook rows read their kinds and triggers from.
	bundles map[string]*catalog.Bundle
}

// resolution is one package at one floor, as Catalog.Resolve was asked.
type resolution struct {
	id    string
	floor int64
}

// resolvedPackage is the catalog's answer: the closure, or why approving
// cannot bring the package to the floor.
type resolvedPackage struct {
	bundle  *catalog.Bundle
	problem *packageProblem
}

// ownership is whose a package is, as its package record says.
type ownership struct {
	// declared is set for a package the owner declared themselves, and
	// version is the version they hold it at.
	declared bool
	version  int64
}

// resolve is the closure package id resolves to at floor, or why approving
// cannot bring it there: a nil catalog offers nothing, and a package no
// source lists at the floor is not published, with the catalog's words
// (decision 0212). A closure resolved once is the plan's to read by id.
func (p *planner) resolve(ctx context.Context, id string, floor int64) (*catalog.Bundle, *packageProblem, error) {
	if p.cat == nil {
		return nil, notShipped(), nil
	}
	key := resolution{id: id, floor: floor}
	if r, done := p.resolved[key]; done {
		return r.bundle, r.problem, nil
	}
	b, err := p.cat.ResolveFor(ctx, id, floor, p.ds)
	switch {
	case errors.Is(err, catalog.ErrNotPublished):
		p.resolved[key] = resolvedPackage{problem: notPublished(err)}
	case err != nil:
		return nil, nil, err
	default:
		p.resolved[key] = resolvedPackage{bundle: b}
		p.bundles[id] = b
	}
	r := p.resolved[key]
	return r.bundle, r.problem, nil
}

// bundle is the closure the plan resolved for package id, nil where it
// resolved none.
func (p *planner) bundle(id string) *catalog.Bundle { return p.bundles[id] }

func (p *planner) held(id string) int64 { return HeldRelease(p.statuses[id]) }

// action is what approving does to a package the app needs at need: nothing
// where the repository holds it at need or later, an upgrade where it holds
// it below, an install where it holds none in its live registry.
func (p *planner) action(id string, need int64) string {
	switch st := p.statuses[id]; {
	case st == nil || !st.Installed:
		return substrate.SetupInstall
	case HeldRelease(st) >= need:
		return substrate.SetupNone
	}
	return substrate.SetupUpgrade
}

// ownedBlock is the block on installing over package id when the owner
// declared it themselves, nil when they did not. Installing the published
// closure over it would take it from them. Whose it is is read off the
// package record alone: a package of functions or traits declares no kind,
// and one applied by hand carries no bundle. It is the owner's when it is
// `installed` and no provider install stamped it; `published` and `builtin`
// ones are not, nor is one the repository does not hold.
func (p *planner) ownedBlock(ctx context.Context, id string) (*packageProblem, error) {
	o, read := p.owned[id]
	if !read {
		rec, err := p.ds.Get(ctx, packageKind, id)
		switch {
		case errors.Is(err, substrate.ErrNotFound):
		case err != nil:
			return nil, err
		case rec.DeletedAt == nil:
			source, _ := rec.Properties["source"].(string)
			stamp, _ := vocabulary.VersionValue(rec.Properties["shippedVersion"])
			o.declared = source == vocabulary.SourceInstalled && stamp <= 0
			o.version, _ = vocabulary.VersionValue(rec.Properties["version"])
		}
		p.owned[id] = o
	}
	if !o.declared {
		return nil, nil
	}
	return declaredByOwner(id, o.version), nil
}

// packageBlock is why approving cannot bring the resolved closure b of
// package id to need, nil when nothing stands in its way but a preview: a
// sample, a quarantined copy, one the owner declared below need, or one this
// server ships below need. A document-published closure resolved at need or
// later, so the last never blocks it.
func (p *planner) packageBlock(ctx context.Context, b *catalog.Bundle, id string, need int64) (*packageProblem, error) {
	st := p.statuses[id]
	switch {
	case b.KeptBy == substrate.KeptByRepository:
		return sample(), nil
	case st != nil && st.Quarantined:
		return quarantined(id), nil
	case st != nil && st.Installed && p.held(id) >= need:
		return nil, nil
	}
	if problem, err := p.ownedBlock(ctx, id); problem != nil || err != nil {
		return problem, err
	}
	if b.Origin() == nil && b.Version < need {
		return serverUpdate(b.Version, need), nil
	}
	return nil, nil
}

// publisherOf is where a document-published closure came from, as a row or
// a step names it; nil for one the binary ships.
func publisherOf(b *catalog.Bundle) *substrate.AuthorizationPackagePublisher {
	o := b.Origin()
	if o == nil {
		return nil
	}
	return &substrate.AuthorizationPackagePublisher{Authority: o.Authority, Name: o.Name, DocumentURL: o.DocumentURL, FromFile: o.FromFile, Digest: o.SHA256}
}

func (p *planner) packageRow(ctx context.Context, d substrate.AuthorizationDetail) (substrate.AuthorizationSetup, error) {
	info := &substrate.AuthorizationPackage{ID: d.Package, Required: d.Version, Held: p.held(d.Package)}
	row := substrate.AuthorizationSetup{Type: substrate.DetailPackage, Action: p.action(d.Package, d.Version), Package: info}
	// Installed or not: a package row admits the account and webhook rows of
	// its package, which would otherwise reach the owner's own packages and
	// the samples they imported, and nothing names those as the app's. So
	// every package resolves, through the binary's closures and the
	// authority documents (decision 0214).
	b, problem, err := p.resolve(ctx, d.Package, d.Version)
	if err != nil {
		return row, err
	}
	if b != nil {
		// Shipped is the version approving would install: the binary's, or
		// the newest the publisher lists at the floor or later.
		info.Description, info.KeptBy, info.Shipped, info.Publisher = b.Description, b.KeptBy, b.Version, publisherOf(b)
		info.OAuthHosts = slices.Clone(b.OAuthHosts)
		if origin := b.Origin(); origin != nil {
			info.AddonFor = origin.AddonFor
		}
		if info.Settings, err = p.emptySettings(ctx, b); err != nil {
			return row, err
		}
		if problem, err = p.packageBlock(ctx, b, d.Package, d.Version); err != nil {
			return row, err
		}
	}
	if row.Blocked = problem.own(); row.Blocked != nil || row.Action == substrate.SetupNone {
		return row, nil
	}
	// An add-on whose provider is not installed waits for it (decision
	// 0216): nothing installs now, and the provider's install plans it again
	// against the approval's token.
	if info.AddonFor != "" && p.held(info.AddonFor) == 0 {
		row.Action = substrate.SetupWait
		return row, nil
	}
	actionOnly := p.opts.ActionOnly[d.Package]
	if !actionOnly {
		info.Kinds, info.Functions, info.Triggers = p.facts(b)
	}
	if info.Requires, problem, err = p.chain(ctx, b, actionOnly); err != nil || problem != nil {
		row.Blocked = problem.needsFirst()
		return row, err
	}
	// What the requirements an install brings first ship to be filled in is
	// asked on the same page.
	for _, step := range info.Requires {
		dep := p.bundle(step.ID)
		if dep == nil {
			continue
		}
		settings, err := p.emptySettings(ctx, dep)
		if err != nil {
			return row, err
		}
		info.Settings = append(info.Settings, settings...)
	}
	// Every closure the approval installs is held to the scopes left
	// checked, the package's own first, then what it brings (decision 0215).
	if row.Blocked, err = p.reachBlock(ctx, b, info, actionOnly); err != nil || row.Blocked != nil || actionOnly {
		return row, err
	}
	// The preview runs last and stops at the first package it blocks: it
	// stages a closure against the owner's data.
	if row.Action == substrate.SetupUpgrade {
		if problem, err = p.upgradeBlock(ctx, d.Package); problem != nil || err != nil {
			row.Blocked = problem.own()
			return row, err
		}
	}
	for i := range info.Requires {
		step := &info.Requires[i]
		if step.Action != substrate.SetupUpgrade {
			continue
		}
		if problem, err = p.upgradeBlock(ctx, step.ID); problem != nil || err != nil {
			step.Blocked, row.Blocked = problem.own(), problem.needsFirst()
			return row, err
		}
	}
	return row, nil
}

// chain is what installing b brings first, in the order it lands, and the
// first step's problem that blocks. A closure the binary ships walks the
// catalog's own list (binaryChain); one an authority document published
// walks its requirements through the same resolution it came by
// (publishedChain).
func (p *planner) chain(ctx context.Context, b *catalog.Bundle, actionOnly bool) ([]substrate.AuthorizationPackageStep, *packageProblem, error) {
	if b.Origin() != nil {
		return p.publishedChain(ctx, b, actionOnly)
	}
	steps, unmet, err := p.cat.Requirements(ctx, b.ID, p.ds)
	if err != nil {
		return nil, nil, err
	}
	return p.binaryChain(ctx, b, steps, unmet, actionOnly)
}

// publishedChain is chain for a document-published closure: each
// requirement the repository does not hold at the floor the chain puts
// under it resolves as the package did, through the binary's closures and
// the authority documents (decision 0214), its own requirements first, once,
// in the order they land. One nothing offers at its floor is a step that is
// not published, and a binary sample stays the owner's to import.
func (p *planner) publishedChain(ctx context.Context, b *catalog.Bundle, actionOnly bool) ([]substrate.AuthorizationPackageStep, *packageProblem, error) {
	var out []substrate.AuthorizationPackageStep
	var first *packageProblem
	placed := map[string]bool{}
	visiting := map[string]bool{b.ID: true}
	var walk func(lb *catalog.Bundle) error
	walk = func(lb *catalog.Bundle) error {
		for _, req := range lb.Requires {
			floor := lb.RequiresAtLeast[req]
			if visiting[req] || placed[req] {
				continue
			}
			if st := p.statuses[req]; st != nil && st.Installed && HeldRelease(st) >= floor {
				continue
			}
			placed[req] = true
			step := substrate.AuthorizationPackageStep{ID: req, Action: p.stepAction(req), Held: p.held(req)}
			dep, problem, err := p.resolve(ctx, req, floor)
			if err != nil {
				return err
			}
			if dep != nil {
				step.Description, step.Shipped, step.Publisher = dep.Description, dep.Version, publisherOf(dep)
				switch {
				case dep.KeptBy == substrate.KeptByRepository:
					problem = sample()
				default:
					if problem, err = p.stepBlock(ctx, dep, floor); err != nil {
						return err
					}
				}
				if problem == nil {
					// Its own requirements land before it.
					visiting[req] = true
					if err := walk(dep); err != nil {
						return err
					}
					delete(visiting, req)
				}
				if !actionOnly {
					step.Kinds, step.Functions, step.Triggers = p.facts(dep)
					step.OAuthHosts = slices.Clone(dep.OAuthHosts)
				}
			}
			if problem != nil {
				step.Blocked = problem.own()
				if first == nil {
					first = problem
				}
			}
			out = append(out, step)
		}
		return nil
	}
	err := walk(b)
	return out, first, err
}

// binaryChain is chain for a closure the binary ships: steps, the
// requirements the catalog installs, each held to the floor the chain puts
// under it, and after them unmet, what the chain needs that the catalog
// cannot install, a package it does not ship or a sample, which the install
// would be refused on.
func (p *planner) binaryChain(ctx context.Context, b *catalog.Bundle, steps []*catalog.Bundle, unmet []catalog.Unmet, actionOnly bool) ([]substrate.AuthorizationPackageStep, *packageProblem, error) {
	var first *packageProblem
	blocks := func(step *substrate.AuthorizationPackageStep, problem *packageProblem) {
		step.Blocked = problem.own()
		if first == nil {
			first = problem
		}
	}
	chain := append([]*catalog.Bundle{b}, steps...)
	floors := map[string]int64{}
	for _, lb := range chain {
		for req, v := range lb.RequiresAtLeast {
			floors[req] = max(floors[req], v)
		}
	}
	var out []substrate.AuthorizationPackageStep
	for _, dep := range steps {
		p.bundles[dep.ID] = dep
		step := substrate.AuthorizationPackageStep{
			ID: dep.ID, Description: dep.Description, Action: p.stepAction(dep.ID), Held: p.held(dep.ID), Shipped: dep.Version, Publisher: publisherOf(dep),
		}
		problem, err := p.stepBlock(ctx, dep, floors[dep.ID])
		if err != nil {
			return out, first, err
		}
		if problem != nil {
			blocks(&step, problem)
		}
		if !actionOnly {
			step.Kinds, step.Functions, step.Triggers = p.facts(dep)
			step.OAuthHosts = slices.Clone(dep.OAuthHosts)
		}
		out = append(out, step)
	}
	for _, u := range unmet {
		step := substrate.AuthorizationPackageStep{ID: u.ID, Action: p.stepAction(u.ID), Held: p.held(u.ID)}
		problem := notShipped()
		switch {
		case u.Bundle != nil:
			step.Description, step.Shipped, problem = u.Bundle.Description, u.Bundle.Version, sample()
		case u.Err != nil:
			// Its document gave no closure, and says why: an interface this
			// server does not run, a version under the floor.
			problem = notPublished(u.Err)
		}
		blocks(&step, problem)
		out = append(out, step)
	}
	return out, first, nil
}

// reachBlock fills the reach of b and of each requirement its row brings,
// and is why the row cannot run when any of that code reaches past the
// scopes left checked: the package's own reach first, else the first
// requirement's, each step carrying its own block. actionOnly leaves the
// reach off the rows and keeps the block.
func (p *planner) reachBlock(ctx context.Context, b *catalog.Bundle, info *substrate.AuthorizationPackage, actionOnly bool) (*substrate.AuthorizationBlock, error) {
	chain := []*catalog.Bundle{b}
	for _, step := range info.Requires {
		if dep := p.bundle(step.ID); dep != nil {
			chain = append(chain, dep)
		}
	}
	kindOf := p.kindIn(chain)
	var first *substrate.AuthorizationBlock
	for i := range info.Requires {
		step := &info.Requires[i]
		dep := p.bundle(step.ID)
		if dep == nil {
			continue
		}
		reach, err := p.reach(ctx, dep, chain, kindOf)
		if err != nil {
			return nil, err
		}
		if !actionOnly {
			step.Reach = reach
		}
		if problem := reachesPast(reach); problem != nil {
			step.Blocked = problem.own()
			if first == nil {
				first = problem.needsFirst()
			}
		}
	}
	reach, err := p.reach(ctx, b, chain, kindOf)
	if err != nil {
		return nil, err
	}
	if !actionOnly {
		info.Reach = reach
	}
	if problem := reachesPast(reach); problem != nil {
		return problem.own(), nil
	}
	return first, nil
}

// reachesPast is the block a reach earns, nil when every entry is covered.
func reachesPast(reach []substrate.AuthorizationReach) *packageProblem {
	var uncovered []substrate.AuthorizationReach
	for _, r := range reach {
		if !r.Covered {
			uncovered = append(uncovered, r)
		}
	}
	if len(uncovered) == 0 {
		return nil
	}
	return reachesPastScopes(uncovered)
}

// reach is what b's code reaches, each entry held to the scopes left
// checked. kindOf says what an exact kind is to the scopes: whether its
// declared purpose is `internal`, which a glob scope never reaches, and the
// traits it binds, which a trait scope reaches.
func (p *planner) reach(ctx context.Context, b *catalog.Bundle, chain []*catalog.Bundle, kindOf func(kind string) scope.Kind) ([]substrate.AuthorizationReach, error) {
	var out []substrate.AuthorizationReach
	reach, err := b.ResolvedReach(ctx, p.ds, chain)
	if err != nil {
		return nil, err
	}
	for _, r := range reach {
		entry := substrate.AuthorizationReach{Pattern: r.Pattern, Write: r.Write, Scope: scope.Covering(r.Pattern, r.Write)}
		for _, by := range r.By {
			entry.By = append(entry.By, substrate.AuthorizationReachBy{Type: by.Type, ID: by.ID})
		}
		entry.Covered = p.opts.Approved.Covers(r.Pattern, r.Write, kindOf(r.Pattern), p.internalKinds(chain)...)
		out = append(out, entry)
	}
	return out, nil
}

// kindIn answers what a kind is to the scopes (scope.KindOf): as a closure of
// chain declares it, else as the registry holds it. A kind neither holds is
// read as internal and binding nothing, so only its exact scope covers it, as
// the set up check reads a kind the repository lacks.
func (p *planner) kindIn(chain []*catalog.Bundle) func(kind string) scope.Kind {
	return func(kind string) scope.Kind {
		for _, b := range chain {
			if k, ok := b.Kind(kind); ok {
				return scope.KindOf(kind, k.Definition)
			}
		}
		if k, held := p.kinds[kind]; held {
			return scope.KindOf(kind, k.Definition)
		}
		return scope.Kind{Ref: kind, Internal: true}
	}
}

// stepAction is what an install does to a requirement it brings first: an
// upgrade of one the repository holds, else an install.
func (p *planner) stepAction(id string) string {
	if st := p.statuses[id]; st != nil && st.Installed {
		return substrate.SetupUpgrade
	}
	return substrate.SetupInstall
}

// stepBlock is packageBlock for a requirement an install brings first: it
// lands whatever the repository holds, so only what would refuse or replace
// it counts.
func (p *planner) stepBlock(ctx context.Context, dep *catalog.Bundle, floor int64) (*packageProblem, error) {
	if st := p.statuses[dep.ID]; st != nil && st.Quarantined {
		return quarantined(dep.ID), nil
	}
	if problem, err := p.ownedBlock(ctx, dep.ID); problem != nil || err != nil {
		return problem, err
	}
	if dep.Origin() == nil && dep.Version < floor {
		return serverUpdate(dep.Version, floor), nil
	}
	return nil, nil
}

// upgradeBlock is why upgrading an installed package cannot run from the
// approval page: what its preview would refuse on, a conversion that loses
// data or discards edits, or history some kind keeps for less time, which
// only its provider page confirms. A preview that fails blocks it too, as it
// costs a catalog entry its offer.
func (p *planner) upgradeBlock(ctx context.Context, id string) (*packageProblem, error) {
	up, err := p.cat.Upgrade(ctx, id, p.ds, p.statuses[id])
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		slog.Error("appsetup: upgrade preview failed", "bundle", logSafe(id), "error", logSafe(err.Error()))
		return previewFailed(id), nil
	}
	switch {
	case up == nil:
		return nil, nil
	case len(up.Blockers) > 0:
		return upgradeRefused(id), nil
	case up.Lossy || up.DiscardsEdits:
		return upgradeConfirm(id), nil
	case len(up.RetentionShortens) > 0:
		return upgradeShortensHistory(id), nil
	}
	return nil, nil
}

// facts lists what installing b brings of its own: the kinds the repository
// does not hold yet, every function with what it may read and write and the
// hosts it declares, and every trigger as it ships.
func (p *planner) facts(b *catalog.Bundle) ([]substrate.AuthorizationKind, []substrate.AuthorizationFunction, []substrate.AuthorizationTrigger) {
	var kinds []substrate.AuthorizationKind
	for _, k := range b.Kinds() {
		if _, held := p.kinds[k.Identity]; held {
			continue
		}
		kinds = append(kinds, substrate.AuthorizationKind{Identity: k.Identity, Label: k.Label, Purpose: shownPurpose(k.Purpose), Added: true})
	}
	var functions []substrate.AuthorizationFunction
	for _, f := range b.Functions() {
		functions = append(functions, substrate.AuthorizationFunction{
			ID: f.ID, Reads: slices.Clone(f.Reads), Writes: slices.Clone(f.Writes), Network: slices.Clone(f.Network),
		})
	}
	var triggers []substrate.AuthorizationTrigger
	for _, t := range b.Triggers() {
		triggers = append(triggers, substrate.AuthorizationTrigger{ID: t.ID, Callable: t.Callable, Webhook: t.Webhook, Enabled: t.Enabled})
	}
	return kinds, functions, triggers
}

// shownPurpose is a declared purpose where it is not the primary one an
// absent purpose reads as.
func shownPurpose(purpose string) string {
	if purpose == vocabulary.PurposePrimary {
		return ""
	}
	return purpose
}

func (p *planner) accountRow(ctx context.Context, d substrate.AuthorizationDetail) (substrate.AuthorizationSetup, error) {
	acct := &substrate.AuthorizationAccount{Kind: d.Kind, ID: d.ID, Properties: d.Properties}
	row := substrate.AuthorizationSetup{Type: substrate.DetailAccount, Action: substrate.SetupCreate, Account: acct}
	prow := p.packages[vocabulary.KindPackage(d.Kind)]
	if prow == nil {
		row.Blocked = block(substrate.BlockNotRequested, "This account belongs to a package this request does not set up.", "")
		return row, nil
	}
	def, label, found := p.definition(d.Kind, prow)
	acct.Label = label
	switch {
	case !found:
		row.Blocked = block(substrate.BlockUnsupported, "The package this account belongs to declares no such kind.", "")
		return row, nil
	case !vocabulary.BindsTrait(def, accountConfigTrait):
		row.Blocked = block(substrate.BlockUnsupported, "This is not an account kind, so approving cannot create one.", "")
		return row, nil
	}
	if problem := accountPropertyProblem(d, def); problem != "" {
		row.Blocked = block(substrate.BlockUnsupported, problem, "")
		return row, nil
	}
	if _, held := p.kinds[d.Kind]; !held {
		return row, nil
	}
	// The write is create-only: a live row is left alone, and a deleted one
	// is never brought back, so it waits for its deletion to be collected.
	rec, err := p.ds.Get(ctx, d.Kind, d.ID)
	switch {
	case errors.Is(err, substrate.ErrNotFound):
	case err != nil:
		return row, err
	case rec.DeletedAt == nil:
		row.Action = substrate.SetupNone
	default:
		row.Blocked = block(substrate.BlockAccountDeleted,
			"This account was deleted, and your server has not cleared the deletion yet. It does within a few minutes; check again then, and approving creates the account again.", "")
	}
	return row, nil
}

// definition is a kind's declaration as it will stand once the set up ran:
// from the closure a package row installs or upgrades, else from the
// registry.
func (p *planner) definition(kind string, prow *substrate.AuthorizationSetup) (map[string]any, *substrate.KindLabel, bool) {
	fromClosure := func() (map[string]any, *substrate.KindLabel, bool) {
		b := p.bundle(prow.Package.ID)
		if b == nil {
			return nil, nil, false
		}
		k, ok := b.Kind(kind)
		return k.Definition, k.Label, ok
	}
	fromRegistry := func() (map[string]any, *substrate.KindLabel, bool) {
		k, ok := p.kinds[kind]
		return k.Definition, k.Label, ok
	}
	first, second := fromRegistry, fromClosure
	if prow.Action != substrate.SetupNone {
		first, second = fromClosure, fromRegistry
	}
	if def, label, ok := first(); ok {
		return def, label, true
	}
	return second()
}

// accountPropertyProblem holds an account's properties to what an owner's
// approval may write: properties the kind declares, never a secret (the
// owner types those), never one the server manages, and none another hand
// alone writes.
func accountPropertyProblem(d substrate.AuthorizationDetail, def map[string]any) string {
	declared, _ := def["properties"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(d.Properties)) {
		pd, ok := declared[name].(map[string]any)
		if !ok {
			return fmt.Sprintf("This kind of account has no %s property.", name)
		}
		if t, _ := pd["type"].(string); t == string(vocabulary.DatatypeSecret) {
			return fmt.Sprintf("The %s property is a secret, which you write yourself.", name)
		}
		if managed, _ := pd["managed"].(bool); managed {
			return fmt.Sprintf("The %s property is the server's to write.", name)
		}
		// A person is the one writer an approval meets: the set up writes as
		// the person who approves it.
		if writers := declaredWriters(pd["writer"]); writers != nil && !slices.Contains(writers, substrate.ActorTypePerson) {
			return fmt.Sprintf("The %s property is written by %s alone.", name, writerWords(writers))
		}
	}
	return ""
}

// declaredWriters reads a stored declaration's `writer:`, one type of actor
// or a list of them, as the list; nil where it declares none.
func declaredWriters(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, w := range v {
			if s, ok := w.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// writerWords names the types of actor a property is written by in a
// person's words.
func writerWords(writers []string) string {
	switch {
	case slices.Equal(writers, []string{substrate.ActorTypeSystem}):
		return "the provider's sign-in"
	case !slices.Contains(writers, substrate.ActorTypePerson) && (slices.Contains(writers, substrate.ActorTypeFunction) || slices.Contains(writers, substrate.ActorTypeAgent)):
		return "the provider's sync"
	}
	return "the " + strings.Join(writers, " or ")
}

func (p *planner) webhookRow(ctx context.Context, d substrate.AuthorizationDetail) (substrate.AuthorizationSetup, error) {
	plan := &substrate.AuthorizationWebhookPlan{Trigger: d.Trigger}
	row := substrate.AuthorizationSetup{Type: substrate.DetailWebhook, Action: substrate.SetupEnable, Webhook: plan}
	props, err := p.triggerProperties(ctx, d.Trigger)
	if err != nil {
		return row, err
	}
	if props == nil {
		row.Blocked = block(substrate.BlockNotRequested, "There is no such trigger here, and no package this request sets up ships one.", "")
		return row, nil
	}
	plan.Package = vocabulary.CallablePackage(vocabulary.ReferencePath(props["callable"]))
	switch {
	case vocabulary.TriggerWebhook(props) == nil:
		row.Blocked = block(substrate.BlockUnsupported, "This trigger takes no webhook deliveries.", "")
	case vocabulary.TriggerSigned(props):
		row.Blocked = block(substrate.BlockUnsupported, "Its provider signs what it sends to this webhook; an app cannot deliver to it.", "")
	case plan.Package == "":
		row.Blocked = block(substrate.BlockUnsupported, "This trigger runs code that belongs to no package.", "")
	case p.packages[plan.Package] == nil:
		row.Blocked = block(substrate.BlockNotRequested, "This trigger runs code from a package this request does not set up.", "")
	}
	if row.Blocked != nil {
		return row, nil
	}
	plan.Enabled, _ = props["enabled"].(bool)
	plan.Keyed = vocabulary.TriggerKey(props) != ""
	if plan.Enabled && plan.Keyed {
		row.Action = substrate.SetupNone
	}
	return row, nil
}

// triggerProperties is a trigger's properties as the set up will find them:
// the live row's, or, where there is none, those a package this request
// installs or upgrades ships under the id, itself or by a requirement it
// brings. Nil when neither has one.
func (p *planner) triggerProperties(ctx context.Context, id string) (map[string]any, error) {
	rec, err := p.ds.Get(ctx, vocabulary.TriggerKind, id)
	switch {
	case err == nil && rec.DeletedAt == nil:
		return rec.Properties, nil
	case err != nil && !errors.Is(err, substrate.ErrNotFound):
		return nil, err
	}
	for _, pkg := range slices.Sorted(maps.Keys(p.packages)) {
		prow := p.packages[pkg]
		if prow.Action == substrate.SetupNone {
			continue
		}
		bundles := []string{pkg}
		for _, step := range prow.Package.Requires {
			bundles = append(bundles, step.ID)
		}
		for _, bid := range bundles {
			if b := p.bundle(bid); b != nil {
				if t, ok := b.Trigger(id); ok {
					return t.Properties, nil
				}
			}
		}
	}
	return nil, nil
}

// Candidate purposes take precedence: an upgrade may make a public kind internal.
func (p *planner) internalKinds(chain []*catalog.Bundle) []string {
	purposes := map[string]bool{}
	for ref, k := range p.kinds {
		purposes[ref] = k.Definition["purpose"] == vocabulary.PurposeInternal
	}
	for _, b := range chain {
		for _, k := range b.Kinds() {
			purposes[k.Identity] = k.Purpose == vocabulary.PurposeInternal
		}
	}
	var out []string
	for ref, internal := range purposes {
		if internal {
			out = append(out, ref)
		}
	}
	return out
}
