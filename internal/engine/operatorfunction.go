package engine

// THE OPERATOR'S FUNCTION SECRETS (decision 0232, docs/plans/host-defaults.md
// "Function secrets"). A key in the defaults file's `functionSecrets` is never
// a record: at invocation the engine hands it to one call as
// `config.settings.apiKey`, and only when all of these hold:
//
//   - the function's package is one the file lists, and its stored `source` is
//     `published`, so its code is the publisher's and the repository cannot
//     edit what the key reaches;
//   - the function is one of that entry's `consumers`;
//   - the repository holds no `apiKey` secret of its own for the package (one
//     with an empty value is none), because the person's key always wins;
//   - the allowance admits the call at the entry's `pricePerCall`, reserved
//     before the body runs, settled at that price once the body may have
//     started and at nothing when the runner never started it.
//
// A trigger delivery to such a function is held at the allowance as an agent
// trigger is (spend.go holdDelivery, decision 0149): the dispatcher asks before
// it claims the delivery, and a call the allowance refuses after that, its
// room taken by a call racing it, ends the delivery unclaimed
// (errUnstartedAtAllowance). Either way the cursor or the fire state stays,
// and the next pass after the reset delivers the same change or occurrence.
//
// An operator-paid call gets the key ALONE in `config.settings`: none of the
// person's settings ride beside it. That is the LLM rule of the same record
// (nothing the person writes reaches an operator-paid request), and it is what
// keeps the key off loopback and off every origin but the one the body pins:
// a body falls back to its own default origin when `baseUrl` is absent, and
// the person's `baseUrl` is the only way to name another. Refusing the call
// when `baseUrl` were not the pinned origin would hold the same line, but only
// as long as the engine knew each provider's origin; dropping the settings
// needs no such knowledge.
//
// Never anywhere else: not the sealed store, not a record, not the changelog,
// not an export, not another package's function and not another input. The
// key joins the invocation scrubber like every injected secret.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// operatorFunctionSecretName is the setting the operator's key stands in for:
// the `apiKey` secret under the package's own id, and `config.settings.apiKey`
// in the body.
const operatorFunctionSecretName = "apiKey"

// operatorCharge is one operator-paid function call: its open reservation and
// the price it settles at.
type operatorCharge struct {
	r     *operatorReservation
	cents float64
}

// settle closes the call's reservation: at its price when the body may have
// started, whatever it answered, since nothing tells the engine whether a
// request left; at nothing when it never ran, which the runner says with
// runner.ErrNotStarted. Nil-safe, so a caller settles whatever
// resolveFunctionConfig handed it.
func (c *operatorCharge) settle(ctx context.Context, ds *dataset, ran bool) {
	if c == nil {
		return
	}
	cents := 0.0
	if ran {
		cents = c.cents
	}
	ds.settleOperator(ctx, c.r, cents)
}

// operatorFunctionEntry is the defaults file's entry for b's package when
// that package is published, or nil. The entry is the package's whatever the
// function; operatorFunctionPrice says whether fn is one of its consumers.
func (ds *dataset) operatorFunctionEntry(b *vocabulary.Bundle) *operatordefaults.FunctionSecret {
	op := ds.svc.operatorDefaults
	if op == nil || b == nil {
		return nil
	}
	entry := op.FunctionSecrets[b.Package]
	if entry == nil {
		return nil
	}
	g, ok := ds.registry().PackageByName(b.Package)
	if !ok || g.Source != vocabulary.SourcePublished {
		return nil
	}
	return entry
}

// operatorFunctionPrice is what one call of fn costs the operator in cents,
// and whether fn is one of entry's consumers at all.
func operatorFunctionPrice(entry *operatordefaults.FunctionSecret, fn *vocabulary.Function) (float64, bool) {
	if entry == nil || !slices.Contains(entry.Consumers, fn.Name) {
		return 0, false
	}
	// Load held every price to non-negative decimal digits, so this parses.
	cents, err := strconv.ParseFloat(entry.PricePerCall[fn.Name], 64)
	if err != nil || cents < 0 {
		return 0, false
	}
	return cents, true
}

// ownFunctionSecret reports whether the repository holds its own key for b:
// a live `<bundle>/apiKey` secret with a value. The stored value is the
// sealed reference, so emptiness is read without opening it, the way the
// setup item reads it, and one that fails to open is still the person's.
func (ds *dataset) ownFunctionSecret(ctx context.Context, b *vocabulary.Bundle) (bool, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: kindSecret, ID: settingPrefix(b) + operatorFunctionSecretName})
	if err != nil {
		return false, fmt.Errorf("substrate/engine: read %s's own key: %w", b.Package, err)
	}
	return row != nil && row.DeletedAt == nil && propString(row, propSettingValue) != "", nil
}

// operatorPaidCall answers the defaults file's entry and the price when a
// call of fn would run on the operator's key: fn is a listed consumer of the
// published package and the repository holds no key of its own for it.
func (ds *dataset) operatorPaidCall(ctx context.Context, b *vocabulary.Bundle, fn *vocabulary.Function) (*operatordefaults.FunctionSecret, float64, bool, error) {
	entry := ds.operatorFunctionEntry(b)
	price, consumer := operatorFunctionPrice(entry, fn)
	if !consumer {
		return nil, 0, false, nil
	}
	own, err := ds.ownFunctionSecret(ctx, b)
	if err != nil || own {
		return nil, 0, false, err
	}
	return entry, price, true, nil
}

// operatorFunctionConfig decides whether this call of fn runs on the
// operator's key. When it does, it reserves the call's price and answers the
// settings the body gets (the key alone) with the charge to settle; otherwise
// it answers nil and nothing is reserved. A call the allowance cannot admit
// is refused with ErrAllowance, never run on an empty key.
func (ds *dataset) operatorFunctionConfig(ctx context.Context, b *vocabulary.Bundle, fn *vocabulary.Function) (map[string]any, *operatorCharge, error) {
	entry, price, paid, err := ds.operatorPaidCall(ctx, b, fn)
	if err != nil || !paid {
		return nil, nil, err
	}
	r, err := ds.reserveOperator(ctx, substrate.SlotFunction, price)
	if err != nil {
		return nil, nil, err
	}
	settings := map[string]any{operatorFunctionSecretName: entry.APIKey.Value()}
	return settings, &operatorCharge{r: r, cents: price}, nil
}

// functionAllowanceHold says why a call of fn is held at the allowance, or ""
// when it may run: fn runs on the operator's key, and the day's spend leaves
// less than its price. The dispatcher asks it before it claims a delivery
// (spend.go holdDelivery).
func (ds *dataset) functionAllowanceHold(ctx context.Context, fn *vocabulary.Function) (string, error) {
	if ds.svc.operatorDefaults == nil || fn == nil {
		return "", nil
	}
	b, ok := ds.registry().BundleOf(fn.Package)
	if !ok {
		return "", nil
	}
	_, price, paid, err := ds.operatorPaidCall(ctx, b, fn)
	if err != nil || !paid {
		return "", err
	}
	used, _, resets, err := ds.operatorSpendToday(ctx)
	if err != nil {
		return "", err
	}
	limit := float64(ds.svc.operatorDefaults.Budget.CentsPerDay)
	if used+price <= limit+allowanceEpsilon {
		return "", nil
	}
	return ds.allowanceDetail(used, limit, resets), nil
}

// errUnstartedAtAllowance marks a root call the allowance refused while its
// config resolved: its body never started and nothing was written, so a
// delivery that meets it ends unclaimed and waits for the reset, as one held
// before its claim does, instead of failing.
var errUnstartedAtAllowance = errors.New("the allowance refused the call before its body started")

// unstartedAtAllowance marks err errUnstartedAtAllowance when it is the
// allowance's refusal, its text unchanged.
func unstartedAtAllowance(err error) error {
	if !isAllowance(err) {
		return err
	}
	return allowanceUnstarted{err}
}

type allowanceUnstarted struct{ error }

func (e allowanceUnstarted) Unwrap() error      { return e.error }
func (allowanceUnstarted) Is(target error) bool { return target == errUnstartedAtAllowance }

// allowanceText is the hold text an errUnstartedAtAllowance carries: the
// allowance's words, without the error's prefixes.
func allowanceText(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, allowancePrefix); i >= 0 {
		return msg[i:]
	}
	return msg
}

// heldAtAllowance reports whether a delivery attempt failed only because the
// allowance refused its root call before the body started: no agent thread
// opened and no page of a drain committed, so nothing has to be parked.
func heldAtAllowance(err error, threads *agentThreads) bool {
	return errors.Is(err, errUnstartedAtAllowance) && !errors.Is(err, errPagedParked) && threads.opened() == ""
}

// operatorCoversSetting reports whether the operator's key stands in for b's
// setting name, so its setup item does not ask the person for it.
func (ds *dataset) operatorCoversSetting(b *vocabulary.Bundle, name string) bool {
	return name == operatorFunctionSecretName && ds.operatorFunctionEntry(b) != nil
}

// functionDefaults is one item per package the defaults file lists under
// `functionSecrets`, in package order.
func (ds *dataset) functionDefaults(ctx context.Context) ([]substrate.OperatorDefault, error) {
	op := ds.svc.operatorDefaults
	if op == nil {
		return nil, nil
	}
	var out []substrate.OperatorDefault
	for _, pkg := range slices.Sorted(maps.Keys(op.FunctionSecrets)) {
		item, err := ds.functionDefault(ctx, pkg)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// functionDefault is one listed package's slot. Its source is the person's
// own key where they wrote one, else the operator's where the package is
// installed as the published provider, else none.
func (ds *dataset) functionDefault(ctx context.Context, pkg string) (substrate.OperatorDefault, error) {
	_, word := vocabulary.SplitPackageRef(pkg)
	item := substrate.OperatorDefault{
		ID: substrate.SlotFunction + "/" + pkg, Slot: substrate.SlotFunction,
		Label: capitalized(word), Target: pkg, OperatorOffers: true,
	}
	b, installed := ds.registry().BundleOf(pkg)
	if !installed {
		item.Source, item.Readiness = substrate.SourceNone, substrate.ReadinessNotSetUp
		item.Message = fmt.Sprintf("%s is not installed: install it to use %s's key", pkg, ds.siteName())
		return item, nil
	}
	own, err := ds.ownFunctionSecret(ctx, b)
	if err != nil {
		return item, err
	}
	switch {
	case own:
		item.Source, item.Readiness = substrate.SourceOwn, substrate.ReadinessReady
	case ds.operatorFunctionEntry(b) != nil:
		item.Source, item.Readiness = substrate.SourceOperator, substrate.ReadinessReady
	default:
		item.Source, item.Readiness = substrate.SourceNone, substrate.ReadinessNotSetUp
		item.Message = fmt.Sprintf("%s is not installed as the published provider, so %s's key does not reach it: install it from the catalog", pkg, ds.siteName())
	}
	return item, nil
}

// capitalized is word with its first letter in upper case, a package word as
// a label.
func capitalized(word string) string {
	if word == "" {
		return word
	}
	return strings.ToUpper(word[:1]) + word[1:]
}
