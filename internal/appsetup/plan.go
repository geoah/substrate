// Package appsetup is what an app's approval sets up (decision 0202,
// docs/plans/app-setup.md): an authorization request may name, beside its
// scopes, catalog packages at a version or later, the account the app
// delivers under and the webhook it delivers to. Plan is what approving them
// would do in one repository, row by row; a Runner sets the approved plan up
// as the owner before the code is issued. Both read the repository through
// substrate.Dataset and the shipped closures through the catalog, so the
// HTTP layer keeps the handlers and the wire alone.
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

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// accountConfigTrait is the trait an account detail's kind implements.
const accountConfigTrait = vocabulary.PackageCore + "/" + vocabulary.TraitAccountConfig

// packageKind is the kind of a package's own record, whose source and
// shipped-version stamp say whose the package is.
var packageKind = vocabulary.CoreKind(vocabulary.DocPackage)

// Options narrows a plan.
type Options struct {
	// ActionOnly names packages whose rows are planned no further than their
	// action, their requirements and what blocks them: no closure facts and
	// no upgrade preview, which a reader that may not see the row has no
	// business paying for.
	ActionOnly map[string]bool
}

// Plan is what approving details would set up in ds's repository: one row
// per detail, in the request's order, and the digest of the rows an approval
// names. Nothing is cached: a row reads the repository as it is. cat is the
// binary's catalog; nil ships nothing.
func Plan(ctx context.Context, ds substrate.Dataset, cat *catalog.Catalog, details []substrate.AuthorizationDetail, opts Options) ([]substrate.AuthorizationSetup, string, error) {
	if len(details) == 0 {
		return []substrate.AuthorizationSetup{}, "", nil
	}
	p := &planner{
		ds: ds, cat: cat, opts: opts, statuses: map[string]*substrate.BundleStatus{},
		kinds: map[string]substrate.KindInfo{}, packages: map[string]*substrate.AuthorizationSetup{},
		owned: map[string]ownership{},
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
// ones included, the registry, and each package record it asks whose it is.
type planner struct {
	ds       substrate.Dataset
	cat      *catalog.Catalog
	opts     Options
	statuses map[string]*substrate.BundleStatus
	kinds    map[string]substrate.KindInfo
	// packages are the request's package rows by package id, planned before
	// the account and webhook rows that are held to them.
	packages map[string]*substrate.AuthorizationSetup
	owned    map[string]ownership
}

// ownership is whose a package is, as its package record says.
type ownership struct {
	// declared is set for a package the owner declared themselves, and
	// version is the version they hold it at.
	declared bool
	version  int64
}

func (p *planner) bundle(id string) *catalog.Bundle {
	if p.cat == nil {
		return nil
	}
	b, _ := p.cat.ByID(id)
	return b
}

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

// packageBlock is why approving cannot bring package id to need, nil when
// nothing stands in its way but a preview: a package the catalog does not
// ship, a sample, a quarantined copy, one the owner declared below need, or
// one this server ships below need.
func (p *planner) packageBlock(ctx context.Context, id string, need int64) (*packageProblem, error) {
	b, st := p.bundle(id), p.statuses[id]
	switch {
	case b == nil:
		return notShipped(), nil
	case b.Tier == substrate.TierSample:
		return sample(), nil
	case st != nil && st.Quarantined:
		return quarantined(id), nil
	case st != nil && st.Installed && p.held(id) >= need:
		return nil, nil
	}
	if problem, err := p.ownedBlock(ctx, id); problem != nil || err != nil {
		return problem, err
	}
	if b.Version < need {
		return serverUpdate(b.Version, need), nil
	}
	return nil, nil
}

func (p *planner) packageRow(ctx context.Context, d substrate.AuthorizationDetail) (substrate.AuthorizationSetup, error) {
	info := &substrate.AuthorizationPackage{ID: d.Package, Required: d.Version, Held: p.held(d.Package)}
	row := substrate.AuthorizationSetup{Type: substrate.DetailPackage, Action: p.action(d.Package, d.Version), Package: info}
	b := p.bundle(d.Package)
	if b != nil {
		info.Description, info.Tier, info.Shipped = b.Description, b.Tier, b.Version
	}
	// Installed or not: a package row admits the account and webhook rows of
	// its package, which would otherwise reach the owner's own packages and
	// the samples they imported, and nothing names those as the app's.
	problem, err := p.packageBlock(ctx, d.Package, d.Version)
	if err != nil {
		return row, err
	}
	if row.Blocked = problem.own(); row.Blocked != nil || row.Action == substrate.SetupNone {
		return row, nil
	}
	steps, unmet, err := p.cat.Requirements(ctx, d.Package, p.ds)
	if err != nil {
		return row, err
	}
	actionOnly := p.opts.ActionOnly[d.Package]
	if !actionOnly {
		info.Kinds, info.Functions, info.Triggers = p.facts(b)
	}
	if info.Requires, problem, err = p.chain(ctx, b, steps, unmet, actionOnly); err != nil || problem != nil || actionOnly {
		row.Blocked = problem.needsFirst()
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

// chain is what installing b brings first, in the order it lands: steps, the
// requirements the catalog installs, each held to the floor the chain puts
// under it, and after them unmet, what the chain needs that the catalog
// cannot install, a package it does not ship or a sample, which the install
// would be refused on. The problem is the first step's that blocks.
func (p *planner) chain(ctx context.Context, b *catalog.Bundle, steps []*catalog.Bundle, unmet []catalog.Unmet, actionOnly bool) ([]substrate.AuthorizationPackageStep, *packageProblem, error) {
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
		step := substrate.AuthorizationPackageStep{
			ID: dep.ID, Description: dep.Description, Action: p.stepAction(dep.ID), Held: p.held(dep.ID), Shipped: dep.Version,
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
		}
		out = append(out, step)
	}
	for _, u := range unmet {
		step := substrate.AuthorizationPackageStep{ID: u.ID, Action: p.stepAction(u.ID), Held: p.held(u.ID)}
		problem := notShipped()
		if u.Bundle != nil {
			step.Description, step.Shipped, problem = u.Bundle.Description, u.Bundle.Version, sample()
		}
		blocks(&step, problem)
		out = append(out, step)
	}
	return out, first, nil
}

// stepAction is what an install does to a requirement it brings first: an
// upgrade of one the repository holds, else an install.
func (p *planner) stepAction(id string) string {
	if st := p.statuses[id]; st != nil && st.Installed {
		return substrate.SetupUpgrade
	}
	return substrate.SetupInstall
}

// stepBlock is packageBlock for a requirement the catalog installs: it lands
// whatever the repository holds, so only what would refuse or replace it
// counts.
func (p *planner) stepBlock(ctx context.Context, dep *catalog.Bundle, floor int64) (*packageProblem, error) {
	if st := p.statuses[dep.ID]; st != nil && st.Quarantined {
		return quarantined(dep.ID), nil
	}
	if problem, err := p.ownedBlock(ctx, dep.ID); problem != nil || err != nil {
		return problem, err
	}
	if dep.Version < floor {
		return serverUpdate(dep.Version, floor), nil
	}
	return nil, nil
}

// upgradeBlock is why upgrading an installed package cannot run from the
// approval page: what its preview would refuse on, or a conversion that loses
// data or discards edits, which only its provider page confirms. A preview
// that fails blocks it too, as it costs a catalog entry its offer.
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
	}
	return nil, nil
}

// facts lists what installing b brings of its own: the kinds the repository
// does not hold yet, every function with what it may write, and every
// trigger as it ships.
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
		functions = append(functions, substrate.AuthorizationFunction{ID: f.ID, Writes: slices.Clone(f.Writes)})
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
		// `owner` is the one restriction an approval meets: the set up
		// writes as the owner.
		if w, _ := pd["writer"].(string); w != "" && w != vocabulary.WriterOwner {
			return fmt.Sprintf("The %s property is written by %s alone.", name, writerWords(w))
		}
	}
	return ""
}

// writerWords names a property's writer role in the owner's words.
func writerWords(writer string) string {
	switch writer {
	case vocabulary.WriterOAuth:
		return "the provider's sign-in"
	case vocabulary.WriterConnector:
		return "the provider's sync"
	}
	return "the " + writer
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
