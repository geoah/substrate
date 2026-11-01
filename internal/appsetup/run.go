package appsetup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// keyAlphabet is the webhook key alphabet the trigger kind admits, 64
	// characters, so one random byte masked to six bits picks one uniformly.
	keyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	keyLength   = 32
	// movedProblem is a run's stop for a plan that moved from the approved
	// one, whose page then plans the request again.
	movedProblem = "What approving sets up changed after you approved it. Check what it sets up now, then approve again."
)

// errMoved is a row that no longer is what the owner approved, found while
// it ran.
var errMoved = errors.New("what approving sets up changed after the approval")

// Runner runs approvals' set ups against the binary's catalog, one per
// repository at a time.
type Runner struct {
	catalog *catalog.Catalog
	// turns is each repository's one slot, by authority: two set ups that
	// installed the same requirement at once would each replace what the
	// other landed.
	turns sync.Map
}

// NewRunner builds a runner over cat; nil ships nothing.
func NewRunner(cat *catalog.Catalog) *Runner { return &Runner{catalog: cat} }

// Outcome is how a run ended.
type Outcome struct {
	// Failure is why it stopped, nil when every row landed.
	Failure *substrate.AuthorizationFailure
	// Moved is set when it stopped because what approving sets up is no
	// longer what was approved, which the owner reads afresh before
	// approving again.
	Moved bool
	// Gone is set when its request went first: nothing answers it.
	Gone bool
}

// turn waits for the repository's one set up at a time.
func (r *Runner) turn(ctx context.Context, authority string) (func(), error) {
	v, _ := r.turns.LoadOrStore(authority, make(chan struct{}, 1))
	slot := v.(chan struct{})
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Run sets up details in ds's repository as actor, approved being the rows
// the approval was checked against, in details' order. It plans again once
// it holds the repository's turn, since a set up before it may have landed
// some of it, and runs that plan only where it is the approved one or less:
// what runs is what the page showed. Packages land first, then accounts,
// then webhooks, so an account's kind and a webhook's trigger are there by
// the time their rows run. What landed before a stop stays, as it does when
// the owner installs from the provider page. report records each row's
// progress, in details' order, and answers false once the request is gone.
func (r *Runner) Run(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, details []substrate.AuthorizationDetail,
	approved []substrate.AuthorizationSetup, report func(progress []string) bool,
) Outcome {
	progress := make([]string, len(approved))
	for i, row := range Started(approved, approved) {
		progress[i] = row.Progress
	}
	stop := func(row int, message string, moved bool) Outcome {
		progress[row] = substrate.ProgressFailed
		if !report(progress) {
			return Outcome{Gone: true}
		}
		return Outcome{Failure: &substrate.AuthorizationFailure{Row: &row, Message: message}, Moved: moved}
	}
	release, err := r.turn(ctx, ds.Repository().Authority)
	if err != nil {
		return Outcome{Failure: &substrate.AuthorizationFailure{Message: "Another set up in this repository did not finish in time. Try again."}}
	}
	defer release()
	rows, _, err := Plan(ctx, ds, r.catalog, details, Options{})
	if err != nil {
		return Outcome{Failure: &substrate.AuthorizationFailure{Message: FailureWords("Reading what to set up", err)}}
	}
	// Nothing is written unless every row still is what the owner approved.
	if i := FirstBlocked(rows); i >= 0 {
		return stop(i, rows[i].Blocked.Problem, true)
	}
	if i := Moved(approved, rows); i >= 0 {
		return stop(i, movedProblem, true)
	}
	for i, row := range Started(approved, rows) {
		progress[i] = row.Progress
	}
	if !report(progress) {
		return Outcome{Gone: true}
	}
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	rank := map[string]int{substrate.DetailPackage: 0, substrate.DetailAccount: 1, substrate.DetailWebhook: 2}
	sort.SliceStable(order, func(a, b int) bool { return rank[rows[order[a]].Type] < rank[rows[order[b]].Type] })
	for _, i := range order {
		if rows[i].Action == substrate.SetupNone {
			continue
		}
		progress[i] = substrate.ProgressRunning
		if !report(progress) {
			return Outcome{Gone: true}
		}
		if err := r.setUpRow(ctx, ds, actor, details[i], approved[i]); err != nil {
			if errors.Is(err, errMoved) {
				return stop(i, movedProblem, true)
			}
			return stop(i, FailureWords(stepWords(details[i]), err), false)
		}
		progress[i] = substrate.ProgressDone
	}
	if !report(progress) {
		return Outcome{Gone: true}
	}
	// The bindings the owner approved, once more before the code exists: a
	// webhook that changed hands while the rows ran would deliver elsewhere.
	i, err := boundElsewhere(ctx, ds, details, approved)
	switch {
	case err != nil:
		return Outcome{Failure: &substrate.AuthorizationFailure{Message: FailureWords("Checking what was set up", err)}}
	case i >= 0:
		return stop(i, movedProblem, true)
	}
	return Outcome{}
}

// Moved is the first row a plan made after the approval would run beyond what
// the approved plan showed, -1 when none would: a row may keep its approved
// action or have come to need nothing, a package row may lose requirements
// another set up landed but never gain one or change what it does to one, and
// a webhook row keeps the package it delivers to, whatever its action. A row
// the owner saw as in place that needs work again, or a requirement they
// removed since, is theirs to approve again.
func Moved(approved, now []substrate.AuthorizationSetup) int {
	for i, row := range now {
		was := approved[i]
		if row.Webhook != nil && (was.Webhook == nil || row.Webhook.Package != was.Webhook.Package) {
			return i
		}
		if row.Action == substrate.SetupNone {
			continue
		}
		if row.Action != was.Action {
			return i
		}
		if row.Package == nil {
			continue
		}
		for _, step := range row.Package.Requires {
			if was.Package == nil || !slices.ContainsFunc(was.Package.Requires, func(s substrate.AuthorizationPackageStep) bool {
				return s.ID == step.ID && s.Action == step.Action
			}) {
				return i
			}
		}
	}
	return -1
}

// setUpRow runs one row: a package through the catalog's own install door,
// requirements first, as from the provider page; an account created once; a
// webhook turned on, held to the package the approved row named.
func (r *Runner) setUpRow(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, d substrate.AuthorizationDetail, approved substrate.AuthorizationSetup) error {
	switch d.Type {
	case substrate.DetailPackage:
		if r.catalog == nil {
			return fmt.Errorf("%w: this server ships no catalog", substrate.ErrNotFound)
		}
		_, _, err := r.catalog.InstallConfirmed(ctx, actor, d.Package, ds, nil)
		return err
	case substrate.DetailAccount:
		// ifVersion 0 creates and never resurrects: a live row that arrived
		// since the plan is left alone, and one deleted since is the plan
		// moving.
		zero := int64(0)
		_, err := ds.Put(ctx, actor, substrate.PutInput{
			Kind: d.Kind, ID: d.ID, Properties: cloneProperties(d.Properties), IfVersion: &zero,
		})
		if !errors.Is(err, substrate.ErrConflict) {
			return err
		}
		rec, err := ds.Get(ctx, d.Kind, d.ID)
		switch {
		case err != nil:
			return err
		case rec.DeletedAt != nil:
			return errMoved
		}
		return nil
	case substrate.DetailWebhook:
		return enableTrigger(ctx, ds, actor, d.Trigger, approved.Webhook.Package)
	}
	return fmt.Errorf("%w: no set up for a %q detail", substrate.ErrValidation, d.Type)
}

// enableTrigger enables a webhook trigger and gives it a key where it has
// none, so the public door is never open. What the plan checked is checked
// again first: the trigger must still take webhook deliveries its provider
// does not sign, for the package pkg the approval named. An existing key is
// never read out, replaced or returned, and the write is held to the version
// read, so an edit meanwhile refuses it rather than being undone.
func enableTrigger(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, id, pkg string) error {
	rec, err := ds.Get(ctx, vocabulary.TriggerKind, id)
	if errors.Is(err, substrate.ErrNotFound) {
		return errMoved
	}
	if err != nil {
		return err
	}
	if !stillBound(rec, pkg) {
		return errMoved
	}
	enabled, _ := rec.Properties["enabled"].(bool)
	keyed := vocabulary.TriggerKey(rec.Properties) != ""
	if enabled && keyed {
		return nil
	}
	props := map[string]any{"enabled": true}
	if !keyed {
		key, err := newWebhookKey()
		if err != nil {
			return err
		}
		source, _ := rec.Properties["source"].(map[string]any)
		source = maps.Clone(source)
		webhook := maps.Clone(vocabulary.TriggerWebhook(rec.Properties))
		webhook["key"] = key
		source["webhook"] = webhook
		props["source"] = source
	}
	version := rec.Version
	_, err = ds.Put(ctx, actor, substrate.PutInput{Kind: vocabulary.TriggerKind, ID: id, Properties: props, IfVersion: &version})
	return err
}

// stillBound reports whether a trigger is still the webhook an approval
// named: live, taking deliveries its provider does not sign, for pkg.
func stillBound(rec *substrate.Record, pkg string) bool {
	return rec.DeletedAt == nil && vocabulary.TriggerWebhook(rec.Properties) != nil && !vocabulary.TriggerSigned(rec.Properties) &&
		vocabulary.CallablePackage(vocabulary.ReferencePath(rec.Properties["callable"])) == pkg
}

// boundElsewhere is the first webhook row whose trigger is no longer the one
// the approval named, -1 when every one still is.
func boundElsewhere(ctx context.Context, ds substrate.Dataset, details []substrate.AuthorizationDetail, approved []substrate.AuthorizationSetup) (int, error) {
	for i, d := range details {
		if d.Type != substrate.DetailWebhook {
			continue
		}
		rec, err := ds.Get(ctx, vocabulary.TriggerKind, d.Trigger)
		switch {
		case errors.Is(err, substrate.ErrNotFound):
			return i, nil
		case err != nil:
			return -1, err
		case !stillBound(rec, approved[i].Webhook.Package):
			return i, nil
		}
	}
	return -1, nil
}

// newWebhookKey is a key from the system's random source.
func newWebhookKey() (string, error) {
	b := make([]byte, keyLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = keyAlphabet[b[i]&63]
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

// stepWords names a row's step in the owner's words. The page shows a
// failure under its row, whose title names what it is.
func stepWords(d substrate.AuthorizationDetail) string {
	switch d.Type {
	case substrate.DetailPackage:
		return "Installing it"
	case substrate.DetailAccount:
		return "Creating the account"
	}
	return "Turning the webhook on"
}

// FailureWords is a failed step in the owner's words. A refusal the owner can
// act on is said as it is; a fault of the server itself is logged and named
// as one, never shown, since its text describes the deployment.
func FailureWords(what string, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return what + " did not finish in time. Try again."
	case ownersToRead(err):
		return what + " failed: " + err.Error()
	}
	slog.Error("appsetup: a set up step failed", "step", what, "error", err)
	return what + " failed on the server. Try again, and if it fails again the server log says why."
}

// ownersToRead reports whether err is a refusal of the owner's request, whose
// text the API would show them, rather than a fault of the server.
func ownersToRead(err error) bool {
	for _, kind := range []error{
		substrate.ErrValidation, substrate.ErrParked, substrate.ErrNotFound, substrate.ErrConflict,
		substrate.ErrLossyConversion, substrate.ErrGuard, substrate.ErrForbidden, substrate.ErrAuth, substrate.ErrTooLarge,
	} {
		if errors.Is(err, kind) {
			return true
		}
	}
	var ve *substrate.ValidationError
	return errors.As(err, &ve)
}
