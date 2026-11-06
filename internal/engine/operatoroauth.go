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
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

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

// The points testOAuthPoint runs at.
const (
	// oauthPointConfigInputs: an invocation's config has its inputs, and its
	// accounts have no token yet (resolveFunctionConfig).
	oauthPointConfigInputs = "config-inputs"
	// oauthPointTokenRead: an account's token is about to be read
	// (accountAccessToken).
	oauthPointTokenRead = "token-read"
	// oauthPointRefreshTurn: a refresh is about to take its credential's
	// turn (refreshCredential).
	oauthPointRefreshTurn = "refresh-turn"
	// oauthPointClientStamp: a client-change stamp is about to take the
	// account's lock (noteOAuthClientChanged).
	oauthPointClientStamp = "client-stamp"
)

// oauthPoint runs the test hook at point p.
func (ds *dataset) oauthPoint(p string) {
	if hook := ds.svc.testOAuthPoint; hook != nil {
		hook(p)
	}
}

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
	if b.OAuth2 == nil {
		return oauthClient{}, fmt.Errorf("%w: bundle %s declares no oauth2 block", substrate.ErrGuard, b.Package)
	}
	ri, err := ds.resolveBundleInput(ctx, b, b.OAuth2.ClientInput)
	if err != nil {
		return oauthClient{}, err
	}
	return ds.oauthClientFrom(ctx, b, ri)
}

// oauthClientFrom is oauthClientOf over ri, a resolution of b's client input
// the caller already holds. An invocation injects the record it resolved and
// checks grants against the client from the same resolution: two
// resolutions would let a client record deleted between them pair the
// record's apiBase with the operator's client.
func (ds *dataset) oauthClientFrom(ctx context.Context, b *vocabulary.Bundle, ri resolvedInput) (oauthClient, error) {
	if err := ds.admitOutsidePackage(ctx, b.Package); err != nil {
		return oauthClient{}, err
	}
	if b.OAuth2 == nil {
		return oauthClient{}, fmt.Errorf("%w: bundle %s declares no oauth2 block", substrate.ErrGuard, b.Package)
	}
	var err error
	if ri.Row == nil {
		if op := ds.operatorCovers(b, ri); op != nil {
			return oauthClient{ID: op.ClientID, Secret: op.ClientSecret.Value(), Operator: true}, nil
		}
		return oauthClient{}, noClientError{fmt.Errorf("%w: bundle %s's %q input does not resolve (%s) — the OAuth client record carries clientId and clientSecret",
			substrate.ErrGuard, b.Package, b.OAuth2.ClientInput, ri.Detail)}
	}
	c := oauthClient{ID: propString(ri.Row, "clientId")}
	if c.Secret, err = ds.openSecretValue(ctx, propString(ri.Row, propClientSecret)); err != nil {
		return oauthClient{}, err
	}
	if c.ID == "" || c.Secret == "" {
		return oauthClient{}, noClientError{fmt.Errorf("%w: bundle %s's client record %s/%s is missing clientId or clientSecret",
			substrate.ErrGuard, b.Package, ri.Input.Kind, ri.Row.ID)}
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

// noClientError marks a client input that yields no usable client: no record
// and no operator's client, records that resolve wrong, or a record missing
// its id or its secret. Its text and its ErrGuard are the wrapped error's.
type noClientError struct{ err error }

func (e noClientError) Error() string { return e.err.Error() }
func (e noClientError) Unwrap() error { return e.err }

// isNoClient reports whether err says the client input yields no usable
// client, as opposed to a refusal or a failure that says nothing about it.
func isNoClient(err error) bool {
	var nc noClientError
	return errors.As(err, &nc)
}

// oauthClientChanged reports whether row, an account of kind ty, holds a
// grant from another client than now (grantClientChanged).
func oauthClientChanged(ty *vocabulary.Kind, row *erow, now oauthClient) bool {
	return grantClientChanged(ty, row.Props[propOAuthClient], now)
}

// grantClientChanged reports whether a grant to an account of kind ty, which
// recorded `recorded` as the grant's client, came from another client than
// now. An account that recorded no client connected before the record
// existed, through the person's own client. A kind that cannot record one is
// held to nothing, as before the record existed; the operator's client never
// serves such a kind.
func grantClientChanged(ty *vocabulary.Kind, recorded any, now oauthClient) bool {
	if ty == nil || !oauthWrites(ty, propOAuthClient) {
		return false
	}
	m, ok := recorded.(map[string]any)
	if !ok {
		return now.Operator
	}
	source, _ := m["source"].(string)
	id, _ := m["clientId"].(string)
	return source != now.source() || id != now.ID
}

// grantThroughOperator reports whether recorded, an account's `oauthClient`
// value, names the operator's client.
func grantThroughOperator(recorded any) bool {
	m, _ := recorded.(map[string]any)
	source, _ := m["source"].(string)
	return source == oauthClientOperator
}

// errOAuthClientChanged refuses a token whose grant came from another client
// than the one resolving now.
var errOAuthClientChanged = errors.New(reasonOAuthClientChanged)

// refuseOtherClient refuses g, a grant read with its account's recorded
// client (getGrant), when the grant came from another client than now, and
// stamps the account (noteOAuthClientChanged). A token is handed out or
// refreshed only after the read that produced it passed this check, so a
// reconnect through another client that lands after an earlier check cannot
// slip its token past it.
func (ds *dataset) refuseOtherClient(ctx context.Context, g credentialGrant, now oauthClient) error {
	ty, err := ds.resolveType(g.account.Kind)
	if err != nil || !grantClientChanged(ty, g.client, now) {
		return nil
	}
	ds.noteOAuthClientChanged(ctx, g.account, g.gen)
	return errOAuthClientChanged
}

// noClientTokenError is why an account gets no token while its bundle's
// client resolution failed with clientErr. A grant through the operator's
// client while no client resolves at all, as after the operator removes its
// client from the file, needs a reconnect, and the account is stamped so;
// any other failure is the bundle's configuration.
func (ds *dataset) noClientTokenError(ctx context.Context, ref string, account eref, clientErr error) string {
	const missing = "the bundle configuration is missing its client credentials"
	if !isNoClient(clientErr) {
		return missing
	}
	g, err := ds.getGrant(ctx, ref)
	if err != nil || g.account != account || !grantThroughOperator(g.client) {
		return missing
	}
	ds.noteOAuthClientChanged(ctx, account, g.gen)
	return reasonOAuthClientChanged
}

// noteOAuthClientChanged stamps account `erroring` with
// reasonOAuthClientChanged when its grant at credential generation gen came
// from another client than the one resolving now. The caller's client may
// predate a change that already settled the mismatch, so the client is
// resolved again here; a grant through the operator's client while no
// client resolves at all counts as changed. Under the account's lock the
// credential must still be generation gen, as for a refusal's stamp (0180):
// a reconnect that stored a new grant meanwhile keeps its `connected`, and
// so does a refresh that stored one through the client resolving then. A
// read outside the transaction first skips the lock where the row already
// says so, because the refresh loop meets a changed account on every pass.
func (ds *dataset) noteOAuthClientChanged(ctx context.Context, account eref, gen time.Time) {
	row, err := ds.loadRowDB(ctx, account)
	if err != nil || row == nil || row.DeletedAt != nil {
		return
	}
	ty, err := ds.resolveType(row.Kind)
	if err != nil || len(tokenStampProps(ty, row, tokenStatusErroring, reasonOAuthClientChanged)) == 0 {
		return
	}
	b, ok := ds.registry().BundleOf(ty.Package)
	if !ok || b.OAuth2 == nil {
		return
	}
	now, err := ds.oauthClientOf(ctx, b)
	none := isNoClient(err)
	if err != nil && !none {
		return
	}
	changed := func(ty *vocabulary.Kind, row *erow) bool {
		if none {
			return grantThroughOperator(row.Props[propOAuthClient])
		}
		return oauthClientChanged(ty, row, now)
	}
	if !changed(ty, row) {
		return
	}
	ds.oauthPoint(oauthPointClientStamp)
	err = ds.inTx(ctx, actorOAuth, true, func(t *txn) error {
		// completeOAuth's order: the shared registry-dependency lock, then
		// the record, then the credential row it stores under that lock.
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
		var current time.Time
		err = t.row(`SELECT updated_at FROM sealed WHERE ref = $1`, propString(row, propTokenRef)).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !current.Equal(gen) {
			return nil
		}
		ty, err := t.resolveType(row.Kind)
		if err != nil {
			return err
		}
		if !changed(ty, row) {
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
