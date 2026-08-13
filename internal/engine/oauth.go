package engine

// OAuth as a host facility (substrate-primitives §4): the connectors
// service's OAuth engine, ported. Bundles DECLARE auth — the oauth2 trait's
// standard fields live on the client input's resolved record — and the
// host runs it: the start/callback pair connects an account record, tokens
// land in the credential store as secret-typed refs, a refresh loop keeps
// them fresh, and account deletion revokes the grant through the ordinary
// finalizer flow before GC collects.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/oauthflow"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// oauthEndpoints is the flow package's shape, spelled locally so the engine
// reads it off configuration rows without importing the package everywhere.
type oauthEndpoints = oauthflow.Endpoints

// finalizerOAuth is the hold the facility puts on a connected account
// record: its deletion waits for revocation and credential teardown.
const finalizerOAuth = "substrate.oauth"

// actorOAuth is the host OAuth facility's writer identity: the ONLY actor
// permitted to write an account's `writer: oauth` properties — tokenRef,
// tokenStatus, grantedScopes. Its writes are the facility's
// own, never a stranger's.
const actorOAuth substrate.Actor = "substrate.oauth"

// The namespace both host hands share — actorOAuth above and
// substrate.ActorSystem — is refused wherever a REQUEST names an actor
// (substrate.ReservedActor, enforced on the X-Substrate-Actor header). The
// facility and the engine open their transactions under these actors
// directly, so nothing internal goes through that door.

// The tokenStatus values the facility reports on account records.
const (
	tokenStatusConnected = "connected"
	tokenStatusErroring  = "erroring"
)

// oauthRefreshWindow is how far ahead of expiry the refresh loop acts.
const oauthRefreshWindow = 10 * time.Minute

// findAccountRef resolves a bare account id WITHIN the accountconfig trait's
// implementor types (identity is the (type, id) pair, so a bare id names
// nothing by itself; the trait is the scope the OAuth surface pins). Exactly
// one live row resolves; two implementor types holding the same id is an
// ambiguity the caller must break by renaming one.
func (ds *dataset) findAccountRef(ctx context.Context, recordID string) (eref, error) {
	var idents []string
	for _, ty := range ds.registry().Kinds() {
		if ty.Implements(vocabulary.TraitAccountConfigCore) {
			idents = append(idents, ty.Identity)
		}
	}
	if len(idents) == 0 {
		return eref{}, fmt.Errorf("%w: record %s", substrate.ErrNotFound, recordID)
	}
	raw, err := json.Marshal(idents)
	if err != nil {
		return eref{}, err
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind FROM records
		WHERE id = $1 AND deleted_at IS NULL
		  AND kind IN (SELECT jsonb_array_elements_text($2::jsonb))
		ORDER BY kind`, recordID, raw)
	if err != nil {
		return eref{}, err
	}
	defer func() { _ = rows.Close() }()
	var types []string
	for rows.Next() {
		var typ string
		if err := rows.Scan(&typ); err != nil {
			return eref{}, err
		}
		types = append(types, typ)
	}
	if err := rows.Err(); err != nil {
		return eref{}, err
	}
	switch len(types) {
	case 0:
		return eref{}, fmt.Errorf("%w: record %s", substrate.ErrNotFound, recordID)
	case 1:
		return eref{Kind: types[0], ID: recordID}, nil
	default:
		return eref{}, fmt.Errorf("%w: id %s names an account record of more than one type (%s) — address it by full identity",
			substrate.ErrConflict, recordID, strings.Join(types, ", "))
	}
}

// oauthAccountOf loads and gates the account record a flow addresses: it
// must be a live accountconfig-trait record of an installed, enabled bundle.
func (ds *dataset) oauthAccountOf(ctx context.Context, account eref) (*erow, *vocabulary.Bundle, error) {
	recordID := account.ID
	row, err := ds.loadRowDB(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	if row == nil || row.DeletedAt != nil {
		return nil, nil, fmt.Errorf("%w: record %s", substrate.ErrNotFound, recordID)
	}
	ty, err := ds.resolveType(row.Kind)
	if err != nil {
		return nil, nil, err
	}
	if !ty.Implements(vocabulary.TraitAccountConfigCore) {
		return nil, nil, fmt.Errorf("%w: %s is not an %s-trait account record",
			substrate.ErrValidation, recordID, vocabulary.TraitAccountConfigCore)
	}
	b, ok := ds.registry().BundleOf(ty.Authority)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s belongs to no bundle", substrate.ErrValidation, recordID)
	}
	st, err := ds.bundleStateOf(ctx, b.Authority)
	if err != nil {
		return nil, nil, err
	}
	if st.blocked() {
		return nil, nil, fmt.Errorf("%w: bundle %s is not live — enable it before connecting accounts",
			substrate.ErrGuard, b.Authority)
	}
	return row, b, nil
}

// bundleOAuthMeta returns the bundle's TRUSTED provider metadata, or a refusal
// when the bundle declares no oauth2 flow. Endpoints ONLY ever come from here.
func bundleOAuthMeta(b *vocabulary.Bundle) (*vocabulary.BundleOAuth2, error) {
	if b.OAuth2 == nil {
		return nil, fmt.Errorf("%w: bundle %s declares no oauth2 provider metadata", substrate.ErrGuard, b.Authority)
	}
	return b.OAuth2, nil
}

// oauthClientOf reads the OAuth CLIENT credentials off the bundle's resolved
// client input — the only oauth values that live on a (mutable) row. An
// unresolved input is a first-class refusal: no client record, no flow. The
// sealed-at-rest clientSecret is opened HERE, the one host read that needs
// the plaintext.
func (ds *dataset) oauthClientOf(ctx context.Context, b *vocabulary.Bundle) (clientID, clientSecret string, err error) {
	if b.OAuth2 == nil {
		return "", "", fmt.Errorf("%w: bundle %s declares no oauth2 block", substrate.ErrGuard, b.Authority)
	}
	ri, err := ds.resolveBundleInput(ctx, b, b.OAuth2.ClientInput)
	if err != nil {
		return "", "", err
	}
	if ri.Row == nil {
		return "", "", fmt.Errorf("%w: bundle %s's %q input does not resolve (%s) — the OAuth client record carries clientId and clientSecret",
			substrate.ErrGuard, b.Authority, b.OAuth2.ClientInput, ri.Detail)
	}
	clientID = propString(ri.Row, "clientId")
	clientSecret, err = ds.openSecretValue(ctx, propString(ri.Row, "clientSecret"))
	if err != nil {
		return "", "", err
	}
	if clientID == "" || clientSecret == "" {
		return "", "", fmt.Errorf("%w: bundle %s's client record %s/%s is missing clientId or clientSecret",
			substrate.ErrGuard, b.Authority, ri.Input.Kind, ri.Row.ID)
	}
	return clientID, clientSecret, nil
}

// oauthScopesForAccount unions the OAuth scopes an account's ENABLED feature
// toggles request: a toggle absent from the manifest's
// featureScopes (an unwired feature) contributes nothing, so gmail/calendar
// cannot be requested while unmapped. Deterministic order.
func oauthScopesForAccount(meta *vocabulary.BundleOAuth2, row *erow) []string {
	if row == nil || len(meta.FeatureScopes) == 0 {
		return nil
	}
	set := map[string]bool{}
	for toggle, scopes := range meta.FeatureScopes {
		if on, _ := row.Props[toggle].(bool); on {
			for _, s := range scopes {
				set[s] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// StartOAuth begins the connect flow for one account record being created:
// it returns the provider consent URL, state signed over (repository, record,
// nonce). The requested scope union is DERIVED from the account's enabled
// feature toggles; the flow is gated on the OWNER TIER —
// an actor DECLARED below owner (a bundle's or connector's own hand) may
// not start or restart a consent. An undeclared actor reads
// as owner (actorTier's default), so the gate binds declared machine actors,
// not unknown names.
// The nonce's hash lands in oauth_flows beside the flow's PKCE verifier, with
// the state's own expiry: the callback consumes it exactly once, so a captured
// state cannot replay.
func (ds *dataset) StartOAuth(ctx context.Context, actor substrate.Actor, recordID string) (string, error) {
	fl := ds.svc.oauth
	if fl == nil {
		return "", fmt.Errorf("%w: oauth is not configured on this substrate", substrate.ErrValidation)
	}
	if ds.actorTier(actor) != substrate.TierOwner {
		return "", fmt.Errorf("%w: only the owner may start an oauth flow, not %s", substrate.ErrForbidden, actor)
	}
	account, err := ds.findAccountRef(ctx, recordID)
	if err != nil {
		return "", err
	}
	row, b, err := ds.oauthAccountOf(ctx, account)
	if err != nil {
		return "", err
	}
	meta, err := bundleOAuthMeta(b)
	if err != nil {
		return "", err
	}
	clientID, clientSecret, err := ds.oauthClientOf(ctx, b)
	if err != nil {
		return "", err
	}
	ep := oauthEndpointsFor(meta, clientID, clientSecret, oauthScopesForAccount(meta, row))
	nonce, err := newID()
	if err != nil {
		return "", err
	}
	verifier := oauthflow.NewVerifier()
	if err := ds.putOAuthFlow(ctx, nonce, row.ref(), verifier, nowUTC().Add(oauthflow.StateTTL)); err != nil {
		return "", err
	}
	return fl.AuthCodeURL(ep, oauthflow.State{Username: ds.Repository().Name, Record: row.ID, Nonce: nonce}, verifier)
}

// putOAuthFlow persists one started flow: the nonce hashed (a database read
// must not hand out redeemable states), the PKCE verifier sealed like any
// credential. Expired leftovers prune opportunistically.
func (ds *dataset) putOAuthFlow(ctx context.Context, nonce string, account eref, verifier string, exp time.Time) error {
	sealed, err := ds.sealPayload([]byte(verifier))
	if err != nil {
		return err
	}
	if _, err := ds.db.ExecContext(ctx,
		`DELETE FROM oauth_flows WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err = ds.db.ExecContext(ctx, `
		INSERT INTO oauth_flows (nonce_hash, record_kind, record_id, verifier, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		hashToken(nonce), account.Kind, account.ID, sealed, exp)
	if err != nil {
		return fmt.Errorf("substrate/engine: put oauth flow: %w", err)
	}
	return nil
}

// consumeOAuthFlow atomically consumes a pending flow — one DELETE …
// RETURNING, so exactly one callback per started flow wins — and returns its
// PKCE verifier.
func (ds *dataset) consumeOAuthFlow(ctx context.Context, nonce string, account eref) (string, error) {
	var sealed []byte
	err := ds.db.QueryRowContext(ctx, `
		DELETE FROM oauth_flows
		WHERE nonce_hash = $1 AND record_kind = $2 AND record_id = $3 AND expires_at > now()
		RETURNING verifier`, hashToken(nonce), account.Kind, account.ID).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: unknown or already-used oauth state", substrate.ErrAuth)
	}
	if err != nil {
		return "", err
	}
	verifier, err := ds.openPayload(sealed)
	if err != nil {
		return "", fmt.Errorf("substrate/engine: open oauth flow verifier: %w", err)
	}
	return string(verifier), nil
}

// CompleteOAuth finishes a consent: the provider redirected the browser back
// with the signed state and a code. Unauthenticated by nature — the state IS
// the authentication — so it lives on the service and resolves the repository
// itself. Returns the connected record's id.
func (s *service) CompleteOAuth(ctx context.Context, state, code string) (string, error) {
	if s.oauth == nil {
		return "", fmt.Errorf("%w: oauth is not configured on this substrate", substrate.ErrValidation)
	}
	st, err := s.oauth.VerifyState(state)
	if err != nil {
		return "", fmt.Errorf("%w: %w", substrate.ErrAuth, err)
	}
	dsAny, err := s.Dataset(ctx, st.Username)
	if err != nil {
		return "", fmt.Errorf("%w: %w", substrate.ErrAuth, err)
	}
	ds := dsAny.(*dataset)
	if err := ds.completeOAuth(ctx, st, code); err != nil {
		return "", err
	}
	return st.Record, nil
}

func (ds *dataset) completeOAuth(ctx context.Context, st oauthflow.State, code string) error {
	recordID := st.Record
	account, err := ds.findAccountRef(ctx, recordID)
	if err != nil {
		return err
	}
	row0, b, err := ds.oauthAccountOf(ctx, account)
	if err != nil {
		return err
	}
	meta, err := bundleOAuthMeta(b)
	if err != nil {
		return err
	}
	clientID, clientSecret, err := ds.oauthClientOf(ctx, b)
	if err != nil {
		return err
	}
	// The scope set this consent is granted for, derived from the account's
	// enabled toggles — persisted with the credential so a later feature that
	// needs an absent grant can require a reconnect.
	grantedScopes := oauthScopesForAccount(meta, row0)
	ep := oauthEndpointsFor(meta, clientID, clientSecret, grantedScopes)
	// The pending flow is consumed BEFORE the exchange: the state is
	// one-time even when the exchange then fails — a captured state must not
	// be retryable ammunition. A pre-nonce or replayed state has no row and
	// dies here.
	verifier, err := ds.consumeOAuthFlow(ctx, st.Nonce, account)
	if err != nil {
		return err
	}
	tok, err := ds.svc.oauth.Exchange(ctx, ep, code, verifier)
	if err != nil {
		// Logged here, bounded and sanitized by the flow package (status +
		// RFC 6749 code, never the provider's description or body); the
		// unauthenticated callback answers with a fixed message and a
		// correlation id, never this text.
		ds.svc.log.Warn("substrate: oauth code exchange failed", "record", recordID, "error", err)
		return fmt.Errorf("%w: the provider code exchange failed", substrate.ErrValidation)
	}
	// Derive the connected account's email from the grant:
	// the user never types it — the facility reads it off the provider's
	// account-info endpoint (People `people/me` / OIDC userinfo, from TRUSTED
	// manifest metadata) with the fresh access token and writes it to the
	// `writer: oauth` property the bundle names. Best-effort: a userinfo failure
	// logs and leaves email unset rather than failing the whole connect. The
	// network call stays OUTSIDE the write transaction below.
	var derivedEmail string
	if meta.EmailEndpoint != "" && meta.EmailProperty != "" {
		if email, err := ds.svc.oauth.AccountEmail(ctx, meta.EmailEndpoint, tok.AccessToken); err != nil {
			ds.svc.log.Warn("substrate: oauth could not derive account email", "record", recordID, "error", err)
		} else {
			derivedEmail = email
		}
	}
	// Credential row and account patch land in ONE transaction, with the
	// account row locked and re-checked live under it: a concurrent delete
	// either commits first (this refuses — no orphan credential) or waits and
	// finds the connected account whole, finalizer included (wave-3 review
	// #5). A reconnect overwrites in place: the record keeps its ref, the
	// store keeps one row per account.
	// The patch is attributed to the OAuth facility's own actor: tokenRef,
	// tokenStatus and grantedScopes are `writer: oauth`, so ONLY this actor may
	// set them. Still an internal write — it bypasses the
	// system-type guard and the owner-gate the facility itself enforced above.
	return ds.inTx(ctx, actorOAuth, true, func(t *txn) error {
		if err := t.lockRecord(account); err != nil {
			return err
		}
		row, err := t.loadRow(account, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return fmt.Errorf("%w: account %s was deleted while its consent was in flight", substrate.ErrConflict, recordID)
		}
		// The stored ref is sealed at rest; open it to reuse
		// the same credential-store key across a reconnect.
		ref, err := ds.svc.openPropValue(propString(row, propTokenRef))
		if err != nil {
			return err
		}
		if ref == "" {
			if ref, err = newID(); err != nil {
				return err
			}
		}
		if err := t.putCredential(ref, account, tok); err != nil {
			return err
		}
		// The secret-typed ref, the status, and the granted scope set land on
		// the record; the facility's finalizer holds it against GC so deletion
		// runs teardown first.
		props := map[string]any{propTokenRef: ref, propTokenStatus: tokenStatusConnected}
		props[propGrantedScopes] = stringsToAny(grantedScopes)
		// The derived email lands only when the bundle names a real `writer:
		// oauth` property (Finalize enforces the pairing; this re-checks against
		// the live type so a misconfigured bundle skips instead of failing the
		// connect with an undeclared-property write).
		if derivedEmail != "" && meta.EmailProperty != "" {
			if ty, err := ds.resolveType(row.Kind); err == nil {
				if p, ok := ty.Prop(meta.EmailProperty); ok && p.Writer == vocabulary.WriterOAuth {
					props[meta.EmailProperty] = derivedEmail
				}
			}
		}
		_, err = t.patch(account, substrate.PatchInput{
			Properties:    props,
			AddFinalizers: []string{finalizerOAuth},
		})
		return err
	})
}

// stringsToAny renders a scope slice as the []any a repeated-string property
// write expects (nil stays nil — a null clears the property).
func stringsToAny(ss []string) []any {
	if len(ss) == 0 {
		return nil
	}
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// accountAccessToken resolves a credential ref into a LIVE access token for
// one invocation: a token expiring within the refresh window refreshes and
// persists on the spot, so a slow refresh loop never hands a body a corpse.
// `want` is the account the ref was read off. A credential row records the
// account it was issued for, and the two must agree: `tokenRef` is a
// `writer: oauth` property, but the facility is not the only hand that can
// ever reach one, and a ref re-pointed at ANOTHER account's row would hand
// this bundle a live grant issued for a different connection (and, across
// bundles, for a different provider). Refuse rather than resolve.
func (ds *dataset) accountAccessToken(ctx context.Context, ref string, ep oauthEndpoints, want eref) (string, error) {
	tok, account, seen, err := ds.getCredential(ctx, ref)
	if err != nil {
		return "", err
	}
	if account != want {
		return "", fmt.Errorf("the stored credential reference belongs to another account")
	}
	if tok.Expiry.IsZero() || nowUTC().Add(time.Minute).Before(tok.Expiry) {
		return tok.AccessToken, nil
	}
	if ds.svc.oauth == nil || tok.RefreshToken == "" {
		return "", fmt.Errorf("token for %s expired and cannot refresh", account.ID)
	}
	fresh, err := ds.svc.oauth.Refresh(ctx, ep, tok)
	if err != nil {
		return "", err
	}
	// Update-only, compare-and-swap: a teardown or reconnect that landed
	// while the provider call was in flight wins, and this refresh persists
	// nothing (never recreating a deleted credential). The fresh token still
	// serves THIS invocation.
	if _, err := ds.updateCredential(ctx, ref, account, fresh, seen); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

// RefreshOAuthTokens is the central refresh loop's pass: every credential
// expiring inside the window refreshes against its bundle's declared token
// endpoint. Disabled and uninstalled bundles are skipped — frozen accounts
// refresh on demand after re-enable. Returns how many tokens refreshed.
func (ds *dataset) RefreshOAuthTokens(ctx context.Context) (int, error) {
	if ds.svc.oauth == nil {
		return 0, nil
	}
	refs, err := ds.expiringCredentials(ctx, nowUTC().Add(oauthRefreshWindow))
	if err != nil {
		return 0, err
	}
	if len(refs) == 0 {
		return 0, nil
	}
	states, err := ds.bundleStates(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, ref := range refs {
		tok, account, seen, err := ds.getCredential(ctx, ref)
		if err != nil {
			ds.svc.log.Warn("substrate: oauth refresh: reading a credential", "ref", ref, "error", err)
			continue
		}
		if tok.RefreshToken == "" {
			continue
		}
		row, err := ds.loadRowDB(ctx, account)
		if err != nil || row == nil || row.DeletedAt != nil {
			continue // teardown owns deleted accounts
		}
		ty, err := ds.resolveType(row.Kind)
		if err != nil {
			continue // an orphaned (uninstalled) type refreshes after re-install
		}
		b, ok := ds.registry().BundleOf(ty.Authority)
		if !ok || states[b.Authority].blocked() {
			continue
		}
		meta, err := bundleOAuthMeta(b)
		if err != nil {
			ds.svc.log.Warn("substrate: oauth refresh: bundle metadata", "bundle", b.Authority, "error", err)
			continue
		}
		clientID, clientSecret, err := ds.oauthClientOf(ctx, b)
		if err != nil {
			ds.svc.log.Warn("substrate: oauth refresh: bundle configuration", "bundle", b.Authority, "error", err)
			continue
		}
		ep := oauthEndpointsFor(meta, clientID, clientSecret, nil)
		fresh, err := ds.svc.oauth.Refresh(ctx, ep, tok)
		if err != nil {
			ds.svc.log.Warn("substrate: oauth refresh failed — the account needs a reconnect if this persists",
				"record", account.ID, "error", err)
			_, _ = ds.patchInternal(ctx, actorOAuth, account.Kind, account.ID, substrate.PatchInput{
				Properties: map[string]any{propTokenStatus: tokenStatusErroring},
			})
			continue
		}
		// Update-only, compare-and-swap on the generation this pass read: a
		// finalizer teardown or a reconnect that landed while the provider
		// call was blocked wins, and the late refresh recreates nothing.
		swapped, err := ds.updateCredential(ctx, ref, account, fresh, seen)
		if err != nil {
			return n, err
		}
		if !swapped {
			continue
		}
		if status, _ := row.Props[propTokenStatus].(string); status != tokenStatusConnected {
			_, _ = ds.patchInternal(ctx, actorOAuth, account.Kind, account.ID, substrate.PatchInput{
				Properties: map[string]any{propTokenStatus: tokenStatusConnected},
			})
		}
		n++
	}
	return n, nil
}

// ProcessOAuthFinalizers runs teardown for deleted accounts: every
// tombstoned record still holding the facility's finalizer has its grant
// revoked (best effort, against the bundle's declared revocation endpoint),
// its stored credentials dropped, and the finalizer released — after which
// the ordinary GC sweep collects it. Purge rides exactly this path.
func (ds *dataset) ProcessOAuthFinalizers(ctx context.Context) (int, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, kind FROM records
		WHERE deleted_at IS NOT NULL AND $1 = ANY(finalizers) ORDER BY id`, finalizerOAuth)
	if err != nil {
		return 0, err
	}
	type victim struct{ id, typ string }
	var victims []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.id, &v.typ); err != nil {
			_ = rows.Close()
			return 0, err
		}
		victims = append(victims, v)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	n := 0
	for _, v := range victims {
		ds.revokeAccountGrants(ctx, v.id, v.typ)
		if err := ds.deleteCredentialsFor(ctx, eref{Kind: v.typ, ID: v.id}); err != nil {
			return n, err
		}
		if _, err := ds.patchInternal(ctx, substrate.ActorSystem, v.typ, v.id, substrate.PatchInput{
			RemoveFinalizers: []string{finalizerOAuth},
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// revokeAccountGrants posts every stored token of a deleted account to its
// bundle's revocation endpoint, taken from the TRUSTED manifest metadata
// — never the mutable config row, so a config edit cannot
// redirect a revoked refresh token at an attacker's server. Best effort: a
// failed revoke never strands the deletion, but it is logged — the flow
// package holds the provider to a 2xx, so a 401/500 answer is observable
// instead of silently counting as revoked.
func (ds *dataset) revokeAccountGrants(ctx context.Context, recordID, typeIdent string) {
	if ds.svc.oauth == nil {
		return
	}
	ty, ok := ds.registry().ByIdentity(typeIdent)
	if !ok {
		return
	}
	b, ok := ds.registry().BundleOf(ty.Authority)
	if !ok || b.OAuth2 == nil {
		return
	}
	revocationURL := b.OAuth2.RevocationEndpoint
	if revocationURL == "" {
		return
	}
	refs, err := ds.credentialRefsFor(ctx, eref{Kind: typeIdent, ID: recordID})
	if err != nil {
		return
	}
	for _, ref := range refs {
		tok, _, _, err := ds.getCredential(ctx, ref)
		if err != nil {
			continue
		}
		if err := ds.svc.oauth.Revoke(ctx, revocationURL, tok); err != nil {
			ds.svc.log.Warn("substrate: oauth revoke", "record", recordID, "error", err)
		}
	}
}

// credentialRefsFor lists a record's stored credential refs.
func (ds *dataset) credentialRefsFor(ctx context.Context, account eref) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx,
		`SELECT ref FROM sealed WHERE record_kind = $1 AND record_id = $2 ORDER BY ref`,
		account.Kind, account.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}
