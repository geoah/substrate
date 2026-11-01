package api

// APP SET UP (docs/plans/app-setup.md): an app's authorization request may
// name, beside its scopes, what approving sets up: catalog packages at a
// version or later, the account it delivers under and the webhook it
// delivers to (`authorization_details`, internal/authorize). The approval
// page shows the plan below, the approval runs it in the background as the
// owner, and only then is the code minted. The plan is computed again at
// every read, at the approval, which names the hash of the one it showed,
// and at the start of the run.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	setupTriggerKind   = corePackage + "/trigger"
	accountConfigTrait = corePackage + "/accountconfig"
	// setupKeyAlphabet is the webhook key alphabet the trigger kind admits,
	// 64 characters, so one random byte masked to six bits picks one
	// uniformly.
	setupKeyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	setupKeyLength   = 32
	// setupWholeRow is the failure row of a set up that stopped outside any
	// one row: waiting for another set up, or minting the code after.
	setupWholeRow = -1
)

// The states a setup row moves through while a set up runs.
const (
	setupWaiting = "waiting"
	setupRunning = "running"
	setupDone    = "done"
	setupFailed  = "failed"
)

// setupPlanner reads once what one plan needs: every bundle's status,
// quarantined ones included, and the registry.
type setupPlanner struct {
	h        *handler
	ds       substrate.Dataset
	statuses map[string]*substrate.BundleStatus
	kinds    map[string]substrate.KindInfo
	// packages are the request's package rows by package id, planned before
	// the account and webhook rows that are held to them.
	packages map[string]*substrate.AuthorizationSetup
}

// planSetup is what approving details would set up in ds's repository: one
// row per detail, in the request's order, and the digest of the rows an
// approval names. Nothing is cached: a row reads the repository as it is.
func (h *handler) planSetup(ctx context.Context, ds substrate.Dataset, details []substrate.AuthorizationDetail) ([]substrate.AuthorizationSetup, string, error) {
	if len(details) == 0 {
		return []substrate.AuthorizationSetup{}, "", nil
	}
	p := &setupPlanner{
		h: h, ds: ds, statuses: map[string]*substrate.BundleStatus{},
		kinds: map[string]substrate.KindInfo{}, packages: map[string]*substrate.AuthorizationSetup{},
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
	raw, err := json.Marshal(rows)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return rows, hex.EncodeToString(sum[:]), nil
}

// blocked marks a row as one approving cannot set up, saying why and where
// the owner goes about it.
func blocked(row substrate.AuthorizationSetup, problem, fix string) substrate.AuthorizationSetup {
	row.Action, row.Problem, row.Fix = substrate.SetupBlocked, problem, fix
	return row
}

// providerPage is the console page a published package is managed on.
func providerPage(pkg string) string {
	authority, name := vocabulary.SplitPackageRef(pkg)
	return "/providers/" + url.PathEscape(authority) + "/" + url.PathEscape(name)
}

// heldRelease is the catalog release a repository holds of a package: the
// shipped version a provider install stamped, else the stored version, and 0
// when it holds none.
func heldRelease(st *substrate.BundleStatus) int64 {
	if st == nil || !st.Installed {
		return 0
	}
	if st.ShippedVersion > 0 {
		return st.ShippedVersion
	}
	return st.Version
}

func (p *setupPlanner) held(id string) int64 { return heldRelease(p.statuses[id]) }

// declaredByHand reports whether the owner declared a package themselves:
// its kinds are the repository's own, and no provider install stamped it.
// Installing the published closure over it would take it from them.
func (p *setupPlanner) declaredByHand(id string) bool {
	if st := p.statuses[id]; st != nil && st.ShippedVersion > 0 {
		return false
	}
	for _, k := range p.kinds {
		if vocabulary.KindPackage(k.Identity) == id {
			return k.Source == vocabulary.SourceInstalled
		}
	}
	return false
}

func (p *setupPlanner) packageRow(ctx context.Context, d substrate.AuthorizationDetail) (substrate.AuthorizationSetup, error) {
	info := &substrate.AuthorizationPackage{ID: d.Package, Required: d.Version, Held: p.held(d.Package)}
	row := substrate.AuthorizationSetup{Type: substrate.DetailPackage, Package: info}
	st := p.statuses[d.Package]
	installed := st != nil && st.Installed
	var b *catalog.Bundle
	if p.h.catalog != nil {
		b, _ = p.h.catalog.ByID(d.Package)
	}
	if b != nil {
		info.Name, info.Description, info.Tier, info.Shipped = b.Name, b.Description, b.Tier, b.Version
	}
	page := providerPage(d.Package)
	switch {
	case b == nil && installed && info.Held >= d.Version:
		row.Action = substrate.SetupNone
		return row, nil
	case b == nil:
		return blocked(row, fmt.Sprintf("this server does not ship %s, so it cannot set it up", d.Package), ""), nil
	case b.Tier == substrate.TierSample:
		return blocked(row, fmt.Sprintf("%s is a sample, which you add yourself from All data", d.Package), "/data"), nil
	case st != nil && st.Quarantined:
		return blocked(row, fmt.Sprintf("%s is quarantined: %s", d.Package, st.QuarantineReason), page), nil
	case installed && info.Held >= d.Version:
		row.Action = substrate.SetupNone
		return row, nil
	case p.declaredByHand(d.Package):
		return blocked(row, fmt.Sprintf("you declared %s yourself, at version %d: installing the published version would replace yours",
			d.Package, info.Held), page), nil
	case b.Version < d.Version:
		return blocked(row, fmt.Sprintf("this server ships version %d of %s, and the app needs version %d: the server needs an update",
			b.Version, d.Package, d.Version), ""), nil
	}
	row.Action = substrate.SetupInstall
	if installed {
		row.Action = substrate.SetupUpgrade
	}
	steps, err := p.h.catalog.Requirements(ctx, d.Package, p.ds)
	if err != nil {
		return row, err
	}
	// The floor each requirement is installed to: the highest any bundle the
	// chain lands puts under it.
	floors := map[string]int64{}
	for _, lb := range append([]*catalog.Bundle{b}, steps...) {
		for req, v := range lb.RequiresAtLeast {
			floors[req] = max(floors[req], v)
		}
	}
	var problem, fix string
	upgrading := []string{}
	if row.Action == substrate.SetupUpgrade {
		upgrading = append(upgrading, d.Package)
	}
	for _, dep := range steps {
		step := substrate.AuthorizationPackageStep{
			ID: dep.ID, Name: dep.Name, Action: substrate.SetupInstall, Held: p.held(dep.ID), Shipped: dep.Version,
		}
		if step.Held > 0 {
			step.Action = substrate.SetupUpgrade
			upgrading = append(upgrading, dep.ID)
		}
		if floor := floors[dep.ID]; dep.Version < floor && problem == "" {
			step.Action = substrate.SetupBlocked
			problem = fmt.Sprintf("this server ships version %d of %s, which %s needs at version %d: the server needs an update",
				dep.Version, dep.ID, d.Package, floor)
		}
		info.Requires = append(info.Requires, step)
	}
	p.closureFacts(info, append(steps, b))
	for _, id := range upgrading {
		if problem != "" {
			break
		}
		if problem, err = p.upgradeProblem(ctx, id); err != nil {
			return row, err
		}
		fix = providerPage(id)
	}
	if problem != "" {
		return blocked(row, problem, fix), nil
	}
	return row, nil
}

// upgradeProblem is why upgrading an installed package cannot run from the
// approval page: what its preview would refuse on, or a conversion that loses
// data or discards edits, which only its provider page confirms. A preview
// that fails blocks the row, as it costs a catalog entry its offer.
func (p *setupPlanner) upgradeProblem(ctx context.Context, id string) (string, error) {
	up, err := p.h.catalog.Upgrade(ctx, id, p.ds, p.statuses[id])
	if err != nil {
		if ctx.Err() != nil {
			return "", err
		}
		slog.Error("authorize: upgrade preview failed", "bundle", id, "error", err)
		return failedPreviewBlocker, nil
	}
	switch {
	case up == nil:
		return "", nil
	case len(up.Blockers) > 0:
		return fmt.Sprintf("upgrading %s is refused: %s", id, strings.Join(up.Blockers, "; ")), nil
	case up.Lossy || up.DiscardsEdits:
		return fmt.Sprintf("upgrading %s would change records in a way its provider page asks you to confirm first", id), nil
	}
	return "", nil
}

// closureFacts lists what the bundles a row lands bring: the kinds the
// repository does not hold yet, every function with what it may write, and
// every trigger as it ships.
func (p *setupPlanner) closureFacts(info *substrate.AuthorizationPackage, bundles []*catalog.Bundle) {
	for _, b := range bundles {
		declarations, records := b.Documents()
		for _, doc := range declarations {
			id := docMetadataID(doc)
			data, _ := doc["data"].(map[string]any)
			switch doc["kind"] {
			case vocabulary.CoreKind(vocabulary.DocKind):
				if _, held := p.kinds[id]; held {
					continue
				}
				info.Kinds = append(info.Kinds, substrate.AuthorizationKind{
					Identity: id, Label: definitionLabel(data), Purpose: definitionPurpose(data), Added: true,
				})
			case vocabulary.CoreKind(vocabulary.DocFunction):
				info.Functions = append(info.Functions, substrate.AuthorizationFunction{ID: id, Writes: functionWrites(id, data)})
			}
		}
		for _, doc := range records {
			if doc["kind"] != setupTriggerKind {
				continue
			}
			data, _ := doc["data"].(map[string]any)
			props, _ := data["properties"].(map[string]any)
			enabled, _ := props["enabled"].(bool)
			info.Triggers = append(info.Triggers, substrate.AuthorizationTrigger{
				ID: docMetadataID(doc), Callable: referencePath(props["callable"]),
				Webhook: triggerWebhook(props) != nil, Enabled: enabled,
			})
		}
	}
}

func (p *setupPlanner) accountRow(ctx context.Context, d substrate.AuthorizationDetail) (substrate.AuthorizationSetup, error) {
	acct := &substrate.AuthorizationAccount{Kind: d.Kind, ID: d.ID, Properties: d.Properties}
	row := substrate.AuthorizationSetup{Type: substrate.DetailAccount, Account: acct}
	pkg := vocabulary.KindPackage(d.Kind)
	prow := p.packages[pkg]
	if prow == nil {
		return blocked(row, fmt.Sprintf("%s belongs to %s, a package this request does not set up", d.Kind, pkg), ""), nil
	}
	def, label, found := p.definition(d.Kind, prow)
	acct.Label = label
	switch {
	case !found:
		return blocked(row, fmt.Sprintf("%s declares no kind %s", pkg, d.Kind), ""), nil
	case !implementsTrait(def, accountConfigTrait):
		return blocked(row, fmt.Sprintf("%s is not an account kind", d.Kind), ""), nil
	}
	if problem := accountPropertyProblem(d, def); problem != "" {
		return blocked(row, problem, ""), nil
	}
	row.Action = substrate.SetupCreate
	if _, held := p.kinds[d.Kind]; !held {
		return row, nil
	}
	// A row that is there, live or deleted, is left alone: the write is
	// create-only.
	_, err := p.ds.Get(ctx, d.Kind, d.ID)
	switch {
	case err == nil:
		row.Action = substrate.SetupNone
	case !errors.Is(err, substrate.ErrNotFound):
		return row, err
	}
	return row, nil
}

// definition is a kind's declaration as it will stand once the set up ran:
// from the closure a package row installs or upgrades, else from the
// registry.
func (p *setupPlanner) definition(kind string, prow *substrate.AuthorizationSetup) (map[string]any, *substrate.KindLabel, bool) {
	fromClosure := func() (map[string]any, *substrate.KindLabel, bool) {
		if p.h.catalog == nil {
			return nil, nil, false
		}
		b, ok := p.h.catalog.ByID(prow.Package.ID)
		if !ok {
			return nil, nil, false
		}
		declarations, _ := b.Documents()
		for _, doc := range declarations {
			if doc["kind"] == vocabulary.CoreKind(vocabulary.DocKind) && docMetadataID(doc) == kind {
				data, _ := doc["data"].(map[string]any)
				return data, definitionLabel(data), true
			}
		}
		return nil, nil, false
	}
	fromRegistry := func() (map[string]any, *substrate.KindLabel, bool) {
		k, ok := p.kinds[kind]
		return k.Definition, k.Label, ok
	}
	first, second := fromRegistry, fromClosure
	if prow.Action == substrate.SetupInstall || prow.Action == substrate.SetupUpgrade {
		first, second = fromClosure, fromRegistry
	}
	if def, label, ok := first(); ok {
		return def, label, true
	}
	return second()
}

// accountPropertyProblem holds an account's properties to what an owner's
// approval may write: properties the kind declares, never a secret (the
// owner types those), and none another hand alone writes.
func accountPropertyProblem(d substrate.AuthorizationDetail, def map[string]any) string {
	declared, _ := def["properties"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(d.Properties)) {
		pd, ok := declared[name].(map[string]any)
		if !ok {
			return fmt.Sprintf("%s declares no property %s", d.Kind, name)
		}
		if t, _ := pd["type"].(string); t == string(vocabulary.DatatypeSecret) {
			return fmt.Sprintf("%s is a secret, which you write yourself", name)
		}
		if managed, _ := pd["managed"].(bool); managed {
			return fmt.Sprintf("%s is the server's to write", name)
		}
		// `owner` is the one restriction an approval meets: the set up
		// writes as the owner.
		if w, _ := pd["writer"].(string); w != "" && w != vocabulary.WriterOwner {
			return fmt.Sprintf("%s is written by the %s alone", name, w)
		}
	}
	return ""
}

func (p *setupPlanner) webhookRow(ctx context.Context, d substrate.AuthorizationDetail) (substrate.AuthorizationSetup, error) {
	plan := &substrate.AuthorizationWebhookPlan{Trigger: d.Trigger}
	row := substrate.AuthorizationSetup{Type: substrate.DetailWebhook, Webhook: plan}
	props, err := p.triggerProperties(ctx, d.Trigger)
	if err != nil {
		return row, err
	}
	if props == nil {
		return blocked(row, fmt.Sprintf("there is no trigger %s here, and no package this request sets up ships one", d.Trigger), ""), nil
	}
	callable := referencePath(props["callable"])
	plan.Package = setupCallablePackage(callable)
	switch {
	case triggerWebhook(props) == nil:
		return blocked(row, fmt.Sprintf("%s takes no webhook deliveries", d.Trigger), ""), nil
	case plan.Package == "":
		return blocked(row, fmt.Sprintf("%s delivers to %s, which no package ships", d.Trigger, callable), ""), nil
	case p.packages[plan.Package] == nil:
		return blocked(row, fmt.Sprintf("%s delivers to %s, which belongs to %s, a package this request does not set up",
			d.Trigger, callable, plan.Package), ""), nil
	}
	plan.Enabled, _ = props["enabled"].(bool)
	plan.Keyed = triggerKeyOf(props) != ""
	row.Action = substrate.SetupArm
	if plan.Enabled && plan.Keyed {
		row.Action = substrate.SetupNone
	}
	return row, nil
}

// triggerProperties is a trigger's properties as the set up will find them:
// the live row's, or, where there is none, those a package this request
// installs or upgrades ships under the id. Nil when neither has one.
func (p *setupPlanner) triggerProperties(ctx context.Context, id string) (map[string]any, error) {
	rec, err := p.ds.Get(ctx, setupTriggerKind, id)
	switch {
	case err == nil && rec.DeletedAt == nil:
		return rec.Properties, nil
	case err != nil && !errors.Is(err, substrate.ErrNotFound):
		return nil, err
	}
	for _, pkg := range slices.Sorted(maps.Keys(p.packages)) {
		prow := p.packages[pkg]
		if prow.Action != substrate.SetupInstall && prow.Action != substrate.SetupUpgrade {
			continue
		}
		bundles := []string{pkg}
		for _, step := range prow.Package.Requires {
			bundles = append(bundles, step.ID)
		}
		for _, bid := range bundles {
			b, ok := p.h.catalog.ByID(bid)
			if !ok {
				continue
			}
			_, records := b.Documents()
			for _, doc := range records {
				if doc["kind"] == setupTriggerKind && docMetadataID(doc) == id {
					data, _ := doc["data"].(map[string]any)
					props, _ := data["properties"].(map[string]any)
					return props, nil
				}
			}
		}
	}
	return nil, nil
}

// needsAction reports whether approving rows sets anything up.
func needsAction(rows []substrate.AuthorizationSetup) bool {
	return slices.ContainsFunc(rows, func(r substrate.AuthorizationSetup) bool { return r.Action != substrate.SetupNone })
}

// firstBlocked is the first row approving cannot set up, -1 when none.
func firstBlocked(rows []substrate.AuthorizationSetup) int {
	return slices.IndexFunc(rows, func(r substrate.AuthorizationSetup) bool { return r.Action == substrate.SetupBlocked })
}

// --- reading declarations and records ---

func docMetadataID(doc map[string]any) string {
	md, _ := doc["metadata"].(map[string]any)
	id, _ := md["id"].(string)
	return id
}

func definitionLabel(data map[string]any) *substrate.KindLabel {
	l, _ := data["label"].(map[string]any)
	singular, _ := l["singular"].(string)
	plural, _ := l["plural"].(string)
	if singular == "" || plural == "" {
		return nil
	}
	return &substrate.KindLabel{Singular: singular, Plural: plural}
}

// definitionPurpose is a kind's declared purpose where it is not the
// primary one an absent purpose reads as.
func definitionPurpose(data map[string]any) string {
	p, _ := data["purpose"].(string)
	if p == vocabulary.PurposePrimary {
		return ""
	}
	return p
}

// kindPurpose is definitionPurpose of a registry kind.
func kindPurpose(k substrate.KindInfo) string { return definitionPurpose(k.Definition) }

// functionWrites is a function's `permissions.writes`, each named in full: a
// bare name is a kind of the function's own package.
func functionWrites(id string, data map[string]any) []string {
	perms, _ := data["permissions"].(map[string]any)
	list, _ := perms["writes"].([]any)
	authority, pkg := vocabulary.SplitPackageRef(vocabulary.KindPackage(id))
	out := make([]string, 0, len(list))
	for _, v := range list {
		w, _ := v.(string)
		if w != "" && !vocabulary.Qualified(w) && !vocabulary.IsTypeGlob(w) {
			w = vocabulary.KindRef(authority, pkg, w)
		}
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// implementsTrait reports whether a declaration binds trait, whatever
// variant or arguments it binds it with.
func implementsTrait(def map[string]any, trait string) bool {
	traits, _ := def["traits"].([]any)
	return slices.ContainsFunc(traits, func(v any) bool {
		s, _ := v.(string)
		name, _, _ := strings.Cut(s, "(")
		return strings.TrimSpace(name) == trait
	})
}

// referencePath reads a reference value in either shape: the object every
// write stores, or the bare path a manifest authors.
func referencePath(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s, _ := t[vocabulary.ReferenceValueKey].(string)
		return s
	}
	return ""
}

// setupCallablePackage is the package a trigger's callable belongs to:
// `substrate.reamde.dev/core/function/<authority>/<package>/<name>` names
// `<authority>/<package>`. Empty for anything else. It is the engine's rule
// for which package's trigger an id is (engine callablePackage).
func setupCallablePackage(callable string) string {
	for _, kind := range []string{corePackage + "/function/", corePackage + "/agent/"} {
		if rest, ok := strings.CutPrefix(callable, kind); ok {
			if parts := strings.SplitN(rest, "/", 3); len(parts) == 3 {
				return parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

// triggerWebhook is a trigger's webhook source, nil when it has none.
func triggerWebhook(props map[string]any) map[string]any {
	source, _ := props["source"].(map[string]any)
	webhook, _ := source["webhook"].(map[string]any)
	return webhook
}

// triggerKeyOf is a trigger's webhook key, empty when it has none.
func triggerKeyOf(props map[string]any) string {
	key, _ := triggerWebhook(props)["key"].(string)
	return key
}

// --- running it ---

// setupTurn waits for the repository's one set up at a time: two that
// installed the same requirement at once would each replace what the other
// landed.
func (h *handler) setupTurn(ctx context.Context, authority string) (func(), error) {
	v, _ := h.setups.LoadOrStore(authority, make(chan struct{}, 1))
	slot := v.(chan struct{})
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// runSetup runs an approval's set up, detached from the request that
// started it, and answers the request: the code once every row landed, the
// request pending again with the failing row otherwise. What landed before a
// failure stays, as it does when the owner installs from the provider page.
// rows is the plan the approval was checked against; the run plans again
// once it holds the repository's turn, since a set up before it may have
// landed some of it.
func (h *handler) runSetup(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, req authorize.Request, rows []substrate.AuthorizationSetup,
	approve func(authorize.Request) (authorize.Approval, error),
) {
	ctx, cancel := context.WithTimeout(ctx, authorize.SetUpTTL)
	defer cancel()
	authority := ds.Repository().Authority
	progress := make([]string, len(rows))
	report := func() {
		if err := h.authz.Progress(req.ID, authority, progress); err != nil {
			slog.Warn("authorize: a set up outlived its request", "request", req.ID, "error", err)
		}
	}
	fail := func(row int, msg string) {
		if err := h.authz.Fail(req.ID, authority, &substrate.AuthorizationFailure{Row: row, Message: msg}); err != nil {
			slog.Warn("authorize: a set up outlived its request", "request", req.ID, "error", err)
		}
	}
	mark := func(rows []substrate.AuthorizationSetup) {
		for i, row := range rows {
			progress[i] = setupWaiting
			if row.Action == substrate.SetupNone {
				progress[i] = setupDone
			}
		}
		report()
	}
	mark(rows)
	release, err := h.setupTurn(ctx, authority)
	if err != nil {
		fail(setupWholeRow, "another set up in this repository did not finish in time: try again")
		return
	}
	defer release()
	rows, _, err = h.planSetup(ctx, ds, req.Details)
	if err != nil {
		fail(setupWholeRow, setupFailure("Reading what to set up", err))
		return
	}
	mark(rows)
	packages := map[string]bool{}
	for _, d := range req.Details {
		if d.Type == substrate.DetailPackage {
			packages[d.Package] = true
		}
	}
	// Packages land first, so an account's kind and a webhook's trigger are
	// there by the time their rows run.
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	rank := map[string]int{substrate.DetailPackage: 0, substrate.DetailAccount: 1, substrate.DetailWebhook: 2}
	sort.SliceStable(order, func(a, b int) bool { return rank[rows[order[a]].Type] < rank[rows[order[b]].Type] })
	for _, i := range order {
		row := rows[i]
		if row.Action == substrate.SetupNone {
			continue
		}
		if row.Action == substrate.SetupBlocked {
			progress[i] = setupFailed
			report()
			fail(i, row.Problem)
			return
		}
		progress[i] = setupRunning
		report()
		if err := h.setUpRow(ctx, ds, actor, req.Details[i], packages); err != nil {
			progress[i] = setupFailed
			report()
			fail(i, setupFailure(setupWhat(req.Details[i]), err))
			return
		}
		progress[i] = setupDone
	}
	report()
	_, _, err = h.authz.Finish(req.ID, authority, approve, func(req authorize.Request, code string) string {
		return withParams(req.RedirectURI, url.Values{"code": {code}}, req)
	})
	switch {
	case errors.Is(err, authorize.ErrNotPending):
		slog.Warn("authorize: a set up finished after its request lapsed", "request", req.ID)
	case errors.Is(err, authorize.ErrFull):
		fail(setupWholeRow, "everything is set up, and the server is holding too many authorization codes to issue one: try again shortly")
	case err != nil:
		fail(setupWholeRow, setupFailure("Everything is set up, and approving", err))
	}
}

// setUpRow runs one row: a package through the catalog's own install door,
// requirements first, as from the provider page; an account created once; a
// webhook armed.
func (h *handler) setUpRow(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, d substrate.AuthorizationDetail, packages map[string]bool) error {
	switch d.Type {
	case substrate.DetailPackage:
		if h.catalog == nil {
			return fmt.Errorf("%w: this server ships no catalog", substrate.ErrNotFound)
		}
		_, _, err := h.catalog.InstallConfirmed(ctx, actor, d.Package, ds, nil)
		return err
	case substrate.DetailAccount:
		// ifVersion 0 creates and never resurrects: a row that arrived
		// since the plan, live or deleted, is left alone.
		zero := int64(0)
		_, err := ds.Put(ctx, actor, substrate.PutInput{
			Kind: d.Kind, ID: d.ID, Properties: cloneProperties(d.Properties), IfVersion: &zero,
		})
		if errors.Is(err, substrate.ErrConflict) {
			return nil
		}
		return err
	case substrate.DetailWebhook:
		return armTrigger(ctx, ds, actor, d.Trigger, packages)
	}
	return fmt.Errorf("%w: no set up for a %q detail", substrate.ErrValidation, d.Type)
}

// armTrigger enables a webhook trigger and gives it a key where it has none,
// so the public door is never open. The callable is checked again first: the
// trigger must still deliver to a package the request named. An existing key
// is never read out, replaced or returned, and the write is held to the
// version read, so an edit meanwhile refuses it rather than being undone.
func armTrigger(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, id string, packages map[string]bool) error {
	rec, err := ds.Get(ctx, setupTriggerKind, id)
	if errors.Is(err, substrate.ErrNotFound) || (err == nil && rec.DeletedAt != nil) {
		return fmt.Errorf("%w: there is no trigger %s here", substrate.ErrValidation, id)
	}
	if err != nil {
		return err
	}
	callable := referencePath(rec.Properties["callable"])
	if pkg := setupCallablePackage(callable); !packages[pkg] {
		return fmt.Errorf("%w: %s now delivers to %s, which this request does not set up", substrate.ErrValidation, id, callable)
	}
	webhook := triggerWebhook(rec.Properties)
	if webhook == nil {
		return fmt.Errorf("%w: %s takes no webhook deliveries", substrate.ErrValidation, id)
	}
	enabled, _ := rec.Properties["enabled"].(bool)
	keyed := triggerKeyOf(rec.Properties) != ""
	if enabled && keyed {
		return nil
	}
	props := map[string]any{"enabled": true}
	if !keyed {
		key, err := newWebhookKey()
		if err != nil {
			return err
		}
		source := maps.Clone(rec.Properties["source"].(map[string]any))
		webhook = maps.Clone(webhook)
		webhook["key"] = key
		source["webhook"] = webhook
		props["source"] = source
	}
	version := rec.Version
	_, err = ds.Put(ctx, actor, substrate.PutInput{Kind: setupTriggerKind, ID: id, Properties: props, IfVersion: &version})
	return err
}

// newWebhookKey is a key from the system's random source, which never
// answers a key of zeroes.
func newWebhookKey() (string, error) {
	b := make([]byte, setupKeyLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = setupKeyAlphabet[b[i]&63]
	}
	return string(b), nil
}

// cloneProperties copies an account's properties deep, so a write that
// coerces a value in place never changes the request's own.
func cloneProperties(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return maps.Clone(in)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return maps.Clone(in)
	}
	return out
}

// setupWhat names a row's step in the owner's words.
func setupWhat(d substrate.AuthorizationDetail) string {
	switch d.Type {
	case substrate.DetailPackage:
		return "Installing " + d.Package
	case substrate.DetailAccount:
		return "Creating the account " + d.ID
	}
	return "Turning on the webhook " + d.Trigger
}

// setupFailure is a failed step in the owner's words. A fault of the server
// itself is logged and named as one, never shown: its text describes the
// deployment.
func setupFailure(what string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return what + " did not finish in time: try again"
	}
	status, payload := problemFor(err)
	if status >= http.StatusInternalServerError {
		slog.Error("authorize: a set up step failed", "step", what, "error", err)
		return what + " failed on the server: try again, and if it fails again the server log says why"
	}
	return what + " failed: " + payload.Message
}

// --- the approval page's view and the app's check ---

// addedKinds lists, for one resolved read or write scope, the kinds the
// rows would add that it reaches.
func addedKinds(s scope.Scope, rows []substrate.AuthorizationSetup) []substrate.AuthorizationKind {
	g, err := scope.NewGrant([]string{s.String()})
	if err != nil {
		return nil
	}
	var out []substrate.AuthorizationKind
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Package == nil || (row.Action != substrate.SetupInstall && row.Action != substrate.SetupUpgrade) {
			continue
		}
		for _, k := range row.Package.Kinds {
			internal := k.Purpose == vocabulary.PurposeInternal
			ok := g.Reads(k.Identity, internal)
			if s.Verb == scope.Write {
				ok = g.Writes(k.Identity, internal)
			}
			if ok && !seen[k.Identity] {
				seen[k.Identity] = true
				out = append(out, k)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// previousToken is the newest token the repository holds for clientID.
func previousToken(ctx context.Context, ds substrate.Dataset, clientID string) (*substrate.AuthorizationPrevious, error) {
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		return nil, err
	}
	var newest *substrate.TokenInfo
	for i := range tokens {
		if tokens[i].ClientID == clientID && (newest == nil || tokens[i].Created.After(newest.Created)) {
			newest = &tokens[i]
		}
	}
	if newest == nil {
		return nil, nil
	}
	scopes := slices.Clone(newest.Scopes)
	if scopes == nil {
		scopes = []string{}
	}
	return &substrate.AuthorizationPrevious{ApprovedAt: newest.Created, ExpiresAt: newest.ExpiresAt, Scopes: scopes}, nil
}

// grantedDetails is what the token response lists as set up: each package
// with the version now held, each account, and each webhook with where to
// deliver.
func grantedDetails(ctx context.Context, ds substrate.Dataset, issuer, authority string, details []substrate.AuthorizationDetail) []substrate.AuthorizationDetail {
	if len(details) == 0 {
		return nil
	}
	// A read that fails costs the response its versions, never the token.
	held := map[string]int64{}
	statuses, err := ds.BundleStatusesUncounted(ctx)
	if err != nil {
		slog.Warn("authorize: the held versions could not be read for a token response", "error", err)
	}
	for i := range statuses {
		held[statuses[i].ID] = heldRelease(&statuses[i])
	}
	out := make([]substrate.AuthorizationDetail, 0, len(details))
	for _, d := range details {
		switch d.Type {
		case substrate.DetailPackage:
			out = append(out, substrate.AuthorizationDetail{Type: d.Type, Package: d.Package, Version: held[d.Package]})
		case substrate.DetailAccount:
			out = append(out, substrate.AuthorizationDetail{Type: d.Type, Kind: d.Kind, ID: d.ID})
		case substrate.DetailWebhook:
			out = append(out, substrate.AuthorizationDetail{
				Type: d.Type, Trigger: d.Trigger,
				URL: issuer + "/webhooks/" + url.PathEscape(authority) + "/" + url.PathEscape(d.Trigger),
			})
		}
	}
	return out
}

// postAuthorizationCheck answers an app, with its own token, whether it is
// still set up: the scopes it would ask for now that its token lacks, each
// detail's action where the token may see it, and when the token ends. A row
// is `unknown` unless the token reaches what it names: a package one of its
// read or write scopes names a kind of, an account whose kind it reads, a
// webhook it delivers to. The owner's own token sees every row.
func (h *handler) postAuthorizationCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in substrate.AuthorizationCheck
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	if err := authorize.CheckDetails(in.Details); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ds := DatasetFrom(ctx)
	authority := ds.Repository().Authority
	// The plan reads the repository whole; what the token may learn of it is
	// cut below, row by row.
	owner, grant, full := ds, scope.Grant{}, true
	if sd, ok := ds.(*scopedDataset); ok {
		owner, grant, full = sd.inner, sd.grant, false
	}
	missing := []string{}
	if strings.TrimSpace(in.Scope) != "" {
		requested, err := scope.ParseRequest(in.Scope)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
		stored := grant.Scopes()
		for _, s := range requested {
			resolved := s.Resolve(authority).String()
			if !full && !slices.Contains(stored, resolved) && !slices.Contains(missing, resolved) {
				missing = append(missing, resolved)
			}
		}
	}
	rows, _, err := h.planSetup(ctx, owner, in.Details)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	var registry map[string]substrate.KindInfo
	if !full {
		kinds, err := owner.Kinds(ctx)
		if err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		registry = make(map[string]substrate.KindInfo, len(kinds))
		for _, k := range kinds {
			registry[k.Identity] = k
		}
	}
	out := substrate.AuthorizationCheckResult{Scopes: substrate.AuthorizationCheckScopes{Missing: missing}, Setup: make([]substrate.AuthorizationSetup, len(rows))}
	for i, row := range rows {
		visible := full
		if !full {
			switch row.Type {
			case substrate.DetailPackage:
				visible = grant.NamesPackage(row.Package.ID)
			case substrate.DetailAccount:
				// A kind the registry does not hold yet is read as internal,
				// which a glob never reaches.
				k, held := registry[row.Account.Kind]
				visible = grant.Reads(row.Account.Kind, !held || internalKind(k))
			case substrate.DetailWebhook:
				visible = grant.Delivers(row.Webhook.Trigger)
			}
		}
		out.Setup[i] = checkRow(row, visible)
	}
	if lease := leaseFrom(ctx); lease != nil {
		out.ExpiresAt = lease.expires
	} else if full {
		tokens, err := owner.Tokens(ctx)
		if err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		principal := substrate.PrincipalFrom(ctx)
		for _, t := range tokens {
			if t.ID == principal {
				out.ExpiresAt = t.ExpiresAt
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// checkRow cuts a planned row to what a check answers: the action, and for a
// package its versions, where the token may see it; the row's own identity,
// which the app sent, where it may not.
func checkRow(row substrate.AuthorizationSetup, visible bool) substrate.AuthorizationSetup {
	out := substrate.AuthorizationSetup{Type: row.Type, Action: row.Action}
	if !visible {
		out.Action = substrate.SetupUnknown
	}
	switch row.Type {
	case substrate.DetailPackage:
		out.Package = &substrate.AuthorizationPackage{ID: row.Package.ID, Required: row.Package.Required}
		if visible {
			out.Package.Held, out.Package.Shipped = row.Package.Held, row.Package.Shipped
		}
	case substrate.DetailAccount:
		out.Account = &substrate.AuthorizationAccount{Kind: row.Account.Kind, ID: row.Account.ID}
	case substrate.DetailWebhook:
		out.Webhook = &substrate.AuthorizationWebhookPlan{Trigger: row.Webhook.Trigger}
		if visible {
			out.Webhook.Enabled, out.Webhook.Keyed = row.Webhook.Enabled, row.Webhook.Keyed
		}
	}
	return out
}
