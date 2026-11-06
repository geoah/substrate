package engine

// The operator's OAuth clients (docs/plans/host-defaults.md, decision 0232):
// a published provider the operator's defaults file lists under
// `oauthClients` signs in through the operator's client wherever the
// provider's OAuth client input resolves to no record at all. The fallback
// lives inside client resolution and nowhere else, so the consent, the code
// exchange, the refresh, the webhook signing key, the bundle status and the
// operator defaults read all take the same answer. The operator's client
// secret stays in this process: it reaches the provider's token endpoint and
// no record, sealed value, changelog line or function.
//
// Each account records the client its grant was issued to (`oauthClient`, a
// `writer: oauth` object of the client id and whose client it was). A grant
// is bound to the client that obtained it, so an account whose recorded
// client is not the one resolving now is not refreshed and reads as needing
// a reconnect.

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// propOAuthClient is an account's record of the client its grant came from:
// an object of `clientId` and `source` (oauthClientOperator or
// oauthClientOwn), written by the OAuth facility at connect.
const propOAuthClient = "oauthClient"

// The two sources an account's grant can come from.
const (
	oauthClientOperator = "operator"
	oauthClientOwn      = "own"
)

// reasonOAuthClientChanged is what an account whose grant came from another
// client than the one resolving now says in its tokenError.
const reasonOAuthClientChanged = "the OAuth client changed since this account connected; reconnect the account"

// oauthClient is the client a provider's flow runs through: the person's own,
// read off the client input's resolved record, or the operator's, from the
// defaults file. The id and the secret always come from the same one.
type oauthClient struct {
	ID     string
	Secret string
	// Operator is set when the client is the operator's.
	Operator bool
}

// source is the word an account's provenance records for c.
func (c oauthClient) source() string {
	if c.Operator {
		return oauthClientOperator
	}
	return oauthClientOwn
}

// provenance is the `oauthClient` value a grant through c records: the id
// and the source, never the secret.
func (c oauthClient) provenance() map[string]any {
	return map[string]any{"clientId": c.ID, "source": c.source()}
}

// oauthClientOf resolves the OAuth client of b: the record the bundle's
// client input resolves to, or, where the input resolves to no record at all,
// the operator's client for the package (operatorCovers). An input that
// resolves wrong (ambiguous, dangling) or to an incomplete record is a
// refusal and never falls back: the person's own choice is in the way, and
// the setup item says which. The sealed clientSecret of an own record is
// opened here, the one read that needs its plaintext.
func (ds *dataset) oauthClientOf(ctx context.Context, b *vocabulary.Bundle) (oauthClient, error) {
	if err := ds.admitOutsidePackage(ctx, b.Package); err != nil {
		return oauthClient{}, err
	}
	if b.OAuth2 == nil {
		return oauthClient{}, fmt.Errorf("%w: bundle %s declares no oauth2 block", substrate.ErrGuard, b.Package)
	}
	ri, err := ds.resolveBundleInput(ctx, b, b.OAuth2.ClientInput)
	if err != nil {
		return oauthClient{}, err
	}
	if ri.Row == nil {
		if op := ds.operatorCovers(b, ri); op != nil {
			return oauthClient{ID: op.ClientID, Secret: op.ClientSecret.Value(), Operator: true}, nil
		}
		return oauthClient{}, fmt.Errorf("%w: bundle %s's %q input does not resolve (%s) — the OAuth client record carries clientId and clientSecret",
			substrate.ErrGuard, b.Package, b.OAuth2.ClientInput, ri.Detail)
	}
	c := oauthClient{ID: propString(ri.Row, "clientId")}
	if c.Secret, err = ds.openSecretValue(ctx, propString(ri.Row, propClientSecret)); err != nil {
		return oauthClient{}, err
	}
	if c.ID == "" || c.Secret == "" {
		return oauthClient{}, fmt.Errorf("%w: bundle %s's client record %s/%s is missing clientId or clientSecret",
			substrate.ErrGuard, b.Package, ri.Input.Kind, ri.Row.ID)
	}
	return c, nil
}

// operatorCovers returns the operator's client for b when ri, the resolution
// of b's client input, found no record at all and the operator's client
// serves the package (operatorOAuthClient); nil otherwise.
func (ds *dataset) operatorCovers(b *vocabulary.Bundle, ri resolvedInput) *operatordefaults.OAuthClient {
	if ri.Row != nil || ri.Problem != substrate.SetupMissing {
		return nil
	}
	c, _ := ds.operatorOAuthClient(b.Package)
	return c
}

// operatorOAuthClient returns the operator's client for a package, or nil and
// why not. The defaults file must list the package under `oauthClients`, the
// repository must hold it as a published provider, and every account kind
// of the package must declare `oauthClient` as `writer: oauth`: a grant whose
// client cannot be recorded could not be told from one through the person's
// own, so an older install of the provider is upgraded before the operator's
// client serves it. The reason is empty where the file does not list the
// package.
func (ds *dataset) operatorOAuthClient(pkg string) (*operatordefaults.OAuthClient, string) {
	op := ds.svc.operatorDefaults
	if op == nil {
		return nil, ""
	}
	c := op.OAuthClients[pkg]
	if c == nil {
		return nil, ""
	}
	g, ok := ds.registry().PackageByName(pkg)
	if !ok || g.Bundle == nil || g.Bundle.OAuth2 == nil {
		return nil, pkg + " is not installed"
	}
	if g.Source != vocabulary.SourcePublished {
		return nil, fmt.Sprintf("%s is this repository's own copy, not the published provider; %s's sign-in serves only the published one", pkg, ds.siteName())
	}
	accounts := 0
	for _, name := range g.KindOrder {
		k := g.Kinds[name]
		if !k.Implements(vocabulary.TraitAccountConfigCore) {
			continue
		}
		if !oauthWrites(k, propOAuthClient) {
			return nil, fmt.Sprintf("the installed %s predates sign-in through %s; upgrade it", pkg, ds.siteName())
		}
		accounts++
	}
	if accounts == 0 {
		return nil, pkg + " declares no account kind"
	}
	return c, ""
}

// oauthClientChanged reports whether row, an account of kind ty, holds a
// grant from another client than now. An account that recorded no client
// connected before the record existed, through the person's own client. A
// kind that cannot record one is held to nothing, as before the record
// existed; the operator's client never serves such a kind.
func oauthClientChanged(ty *vocabulary.Kind, row *erow, now oauthClient) bool {
	if ty == nil || !oauthWrites(ty, propOAuthClient) {
		return false
	}
	recorded, ok := row.Props[propOAuthClient].(map[string]any)
	if !ok {
		return now.Operator
	}
	source, _ := recorded["source"].(string)
	id, _ := recorded["clientId"].(string)
	return source != now.source() || id != now.ID
}

// noteOAuthClientChanged stamps an account whose grant came from another
// client than now as erroring, with reasonOAuthClientChanged. The change is
// checked again under the account's lock, so a reconnect that landed in
// between keeps its `connected`. A read outside the transaction first skips
// the lock where the row already says so, because the refresh loop meets a
// changed account on every pass.
func (ds *dataset) noteOAuthClientChanged(ctx context.Context, account eref, now oauthClient) {
	row, err := ds.loadRowDB(ctx, account)
	if err != nil || row == nil || row.DeletedAt != nil {
		return
	}
	ty, err := ds.resolveType(row.Kind)
	if err != nil || !oauthClientChanged(ty, row, now) ||
		len(tokenStampProps(ty, row, tokenStatusErroring, reasonOAuthClientChanged)) == 0 {
		return
	}
	err = ds.inTx(ctx, actorOAuth, true, func(t *txn) error {
		if err := t.lockRegistryDepShared(); err != nil {
			return err
		}
		if err := t.lockRecord(account); err != nil {
			return err
		}
		row, err := t.loadRow(account, true)
		if err != nil || row == nil || row.DeletedAt != nil {
			return err
		}
		ty, err := t.resolveType(row.Kind)
		if err != nil {
			return err
		}
		if !oauthClientChanged(ty, row, now) {
			return nil
		}
		props := tokenStampProps(ty, row, tokenStatusErroring, reasonOAuthClientChanged)
		if len(props) == 0 {
			return nil
		}
		_, err = t.patch(account, substrate.PatchInput{Properties: props})
		return err
	})
	if err != nil {
		ds.svc.log.Warn("substrate: oauth: stamping an account whose client changed", "record", account.ID, "error", err)
	}
}

// oauthDefaults is one item per package the operator's file lists under
// `oauthClients`, in package order, resolved by oauthClientOf's rules: where
// the client comes from now, and whether an account holds a working grant
// through it.
func (ds *dataset) oauthDefaults(ctx context.Context) ([]substrate.OperatorDefault, error) {
	op := ds.svc.operatorDefaults
	if op == nil {
		return nil, nil
	}
	var out []substrate.OperatorDefault
	for _, pkg := range slices.Sorted(maps.Keys(op.OAuthClients)) {
		item, err := ds.oauthDefault(ctx, pkg, op.OAuthClients[pkg])
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (ds *dataset) oauthDefault(ctx context.Context, pkg string, c *operatordefaults.OAuthClient) (substrate.OperatorDefault, error) {
	_, word := vocabulary.SplitPackageRef(pkg)
	item := substrate.OperatorDefault{
		ID: substrate.SlotOAuth + "/" + pkg, Slot: substrate.SlotOAuth, Label: word, Target: pkg,
		OperatorOffers: true, Notice: c.Notice,
	}
	b, installed := ds.registry().BundleOf(pkg)
	if !installed || b.OAuth2 == nil {
		// Nothing of the person's can stand in front of the operator's client
		// while the provider is absent, so installing it signs in through
		// the operator's.
		item.Source, item.Readiness, item.Message = substrate.SourceOperator, substrate.ReadinessNotSetUp, pkg+" is not installed"
		if msg := ds.svc.operatorInstalls.failure(ds.scope.Repository, pkg); msg != "" {
			item.Readiness, item.Message = substrate.ReadinessInstallFailed, msg
		}
		return item, nil
	}
	ri, err := ds.resolveBundleInput(ctx, b, b.OAuth2.ClientInput)
	if err != nil {
		return item, err
	}
	switch {
	case ds.operatorCovers(b, ri) != nil:
		item.Source = substrate.SourceOperator
	case ri.Row == nil && ri.Problem == substrate.SetupMissing:
		_, why := ds.operatorOAuthClient(pkg)
		item.Source, item.Readiness, item.Message = substrate.SourceNone, substrate.ReadinessNotSetUp, why
		return item, nil
	case ri.Row == nil:
		// Ambiguous or dangling: the person's own records are in the way.
		item.Source, item.Readiness, item.Message = substrate.SourceOwn, substrate.ReadinessNotSetUp, ri.Detail
		return item, nil
	default:
		item.Source = substrate.SourceOwn
		if propString(ri.Row, "clientId") == "" || propString(ri.Row, propClientSecret) == "" {
			item.Readiness = substrate.ReadinessNotSetUp
			item.Message = fmt.Sprintf("%s/%s is missing clientId or clientSecret", ri.Input.Kind, ri.Row.ID)
			return item, nil
		}
	}
	now := oauthClient{Operator: item.Source == substrate.SourceOperator}
	if now.Operator {
		now.ID = c.ClientID
	} else {
		now.ID = propString(ri.Row, "clientId")
	}
	item.Readiness, item.Message, err = ds.oauthAccountsReadiness(ctx, pkg, now)
	return item, err
}

// oauthAccountsReadiness reads a package's accounts against the client
// resolving now: reconnect-needed where one holds a grant from another client
// or its last refresh failed, needs-connect where none holds a grant, and
// ready otherwise.
func (ds *dataset) oauthAccountsReadiness(ctx context.Context, pkg string, now oauthClient) (string, string, error) {
	g, ok := ds.registry().PackageByName(pkg)
	if !ok {
		return substrate.ReadinessNotSetUp, pkg + " is not installed", nil
	}
	connected := false
	var failing string
	for _, name := range g.KindOrder {
		k := g.Kinds[name]
		if !k.Implements(vocabulary.TraitAccountConfigCore) {
			continue
		}
		rows, err := ds.liveRowsOf(ctx, k.Identity)
		if err != nil {
			return "", "", err
		}
		for _, row := range rows {
			if propString(row, propTokenRef) == "" {
				continue
			}
			if oauthClientChanged(k, row, now) {
				return substrate.ReadinessReconnectNeeded, reasonOAuthClientChanged, nil
			}
			if propString(row, propTokenStatus) == tokenStatusErroring {
				if failing == "" {
					failing = strings.TrimSpace(propString(row, propTokenError))
					if failing == "" {
						failing = "an account's sign-in stopped working; reconnect the account"
					}
				}
				continue
			}
			connected = true
		}
	}
	switch {
	case failing != "":
		return substrate.ReadinessReconnectNeeded, failing, nil
	case !connected:
		return substrate.ReadinessNeedsConnect, "no account is connected yet", nil
	}
	return substrate.ReadinessReady, "", nil
}
