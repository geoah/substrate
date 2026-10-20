package engine

// OAuth as a host facility: bundles DECLARE auth (the oauth2 trait's
// standard fields live on the client input's resolved record) and the host
// runs it: the start/callback pair connects an account record, tokens
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
	"sync"
	"time"

	"golang.org/x/oauth2"

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

// Why the facility last failed to refresh an account's token, and when. They
// are not part of the accountconfig trait: a trait property is a contract
// every implementor must declare, and a repository's own account kind, or a
// provider installed before these shipped, would stop loading. The facility
// writes each one only where the account's kind declares it `writer: oauth`,
// and stamps `erroring` either way.
const (
	propTokenError   = "tokenError"
	propTokenErrorAt = "tokenErrorAt"
)

// oauthWrites reports whether a kind declares name as a `writer: oauth`
// property: the only slot the facility may fill, and a write to an undeclared
// one is refused whole.
func oauthWrites(ty *vocabulary.Kind, name string) bool {
	p, ok := ty.Prop(name)
	return ok && p.Writer == vocabulary.WriterOAuth
}

// noteTokenRefresh stamps one refresh's outcome onto the account: a failure
// writes `erroring` with its reason and when, a success writes `connected` and
// clears both. It writes nothing when the row already says so, because the
// loop retries an account erroring with a failure that may pass (a 5xx, a
// timeout) every pass, an invocation can refresh on demand as often, and each
// write is a changelog entry; `tokenErrorAt` is therefore when the current
// reason first appeared.
// The reason is the flow package's text, which carries the HTTP status and
// the RFC 6749 code and never the provider's description, a token or the
// client secret.
// A failure names the credential generation gen it is about, and is stamped
// only while the account's credential is still that generation: a reconnect
// that stored a new grant while the old one's refusal was in flight keeps
// its `connected` (0180). The check runs under the account's record lock,
// which the callback holds while it stores the new grant, so either the
// callback committed first and the stamp sees the new generation, or the
// stamp committed first and the callback's patch overwrites it. A zero gen
// skips the check.
// Callers pass a context detached from their caller's: the outcome is the
// provider's answer, and it is recorded however the caller ended.
func (ds *dataset) noteTokenRefresh(ctx context.Context, account eref, cause error, gen time.Time) {
	status, reason := tokenStatusConnected, ""
	if cause != nil {
		status, reason = tokenStatusErroring, boundText(firstLine(cause.Error()), syncErrorMax)
	}
	// A read outside the transaction first: the common case is a row that
	// already says so, and it takes no lock.
	row, err := ds.loadRowDB(ctx, account)
	if err != nil || row == nil || row.DeletedAt != nil {
		return
	}
	ty, err := ds.resolveType(row.Kind)
	if err != nil {
		return
	}
	if len(tokenStampProps(ty, row, status, reason)) == 0 {
		return
	}
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
		if cause != nil && !gen.IsZero() {
			var current time.Time
			err := t.row(`SELECT updated_at FROM sealed WHERE ref = $1`, propString(row, propTokenRef)).Scan(&current)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if !current.Equal(gen) {
				return nil
			}
		}
		ty, err := t.resolveType(row.Kind)
		if err != nil {
			return err
		}
		props := tokenStampProps(ty, row, status, reason)
		if len(props) == 0 {
			return nil
		}
		_, err = t.patch(account, substrate.PatchInput{Properties: props})
		return err
	})
	if err != nil {
		ds.svc.log.Warn("substrate: oauth refresh: stamping the account", "record", account.ID, "error", err)
	}
}

// tokenStampProps is the patch that makes row say status and reason, empty
// when it already does.
func tokenStampProps(ty *vocabulary.Kind, row *erow, status, reason string) map[string]any {
	props := map[string]any{}
	if propString(row, propTokenStatus) != status {
		props[propTokenStatus] = status
	}
	if oauthWrites(ty, propTokenError) && propString(row, propTokenError) != reason {
		var text, at any // nil clears the property
		if reason != "" {
			text, at = reason, nowUTC().Format(time.RFC3339Nano)
		}
		props[propTokenError] = text
		if oauthWrites(ty, propTokenErrorAt) {
			props[propTokenErrorAt] = at
		}
	}
	return props
}

// oauthRefreshWindow is how far ahead of expiry the refresh loop acts.
const oauthRefreshWindow = 10 * time.Minute

// findAccountRef resolves what a consent names as its account record: the
// record path, `<authority>/<package>/<kind>/<id>`, and nothing shorter.
// Identity is the (kind, id) pair everywhere else in the engine, and `owner`
// under google and `owner` under slack are two records (issue #574), so a
// bare id is refused rather than searched (record 0102), with the refusal a
// bare kind gets: the word, the form, and every live account row the
// repository holds under it, so the caller copies one instead of guessing.
func (ds *dataset) findAccountRef(ctx context.Context, record string) (eref, error) {
	kind, id, ok := vocabulary.SplitRecordPath(record)
	if !ok {
		held, err := ds.accountPathsHolding(ctx, record)
		if err != nil {
			return eref{}, err
		}
		msg := fmt.Sprintf("%q is a bare record id, and an account is named in full as <authority>/<package>/<kind>/<id>", record)
		if len(held) > 0 {
			msg += "; this repository holds " + strings.Join(held, ", ")
		}
		return eref{}, fmt.Errorf("%w: %s", substrate.ErrValidation, msg)
	}
	return ds.accountRefAt(ctx, eref{Kind: kind, ID: id})
}

// accountRefAt confirms the path names a live row. The trait and the bundle's
// own gates are oauthAccountOf's, so a kind that is not an account config
// fails there, with that function's message.
func (ds *dataset) accountRefAt(ctx context.Context, ref eref) (eref, error) {
	row, err := ds.loadRowDB(ctx, ref)
	if err != nil {
		return eref{}, err
	}
	if row == nil || row.DeletedAt != nil {
		return eref{}, fmt.Errorf("%w: record %s", substrate.ErrNotFound, vocabulary.RecordPath(ref.Kind, ref.ID))
	}
	return ref, nil
}

// accountPathsHolding lists, in kind order, the record path of every live row
// carrying the id under a kind that implements the accountconfig trait: the
// spellings a bare-id refusal hands back.
func (ds *dataset) accountPathsHolding(ctx context.Context, id string) ([]string, error) {
	var idents []string
	for _, ty := range ds.registry().Kinds() {
		if ty.Implements(vocabulary.TraitAccountConfigCore) {
			idents = append(idents, ty.Identity)
		}
	}
	if len(idents) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(idents)
	if err != nil {
		return nil, err
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind FROM records
		WHERE id = $1 AND deleted_at IS NULL
		  AND kind IN (SELECT jsonb_array_elements_text($2::jsonb))
		ORDER BY kind`, id, raw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var paths []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		paths = append(paths, vocabulary.RecordPath(kind, id))
	}
	return paths, rows.Err()
}

// oauthAccountOf loads and gates the account record a flow addresses: it
// must be a live accountconfig-trait record of an installed, enabled bundle.
func (ds *dataset) oauthAccountOf(ctx context.Context, account eref) (*erow, *vocabulary.Bundle, error) {
	record := vocabulary.RecordPath(account.Kind, account.ID)
	row, err := ds.loadRowDB(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	if row == nil || row.DeletedAt != nil {
		return nil, nil, fmt.Errorf("%w: record %s", substrate.ErrNotFound, record)
	}
	ty, err := ds.resolveType(row.Kind)
	if err != nil {
		return nil, nil, err
	}
	if !ty.Implements(vocabulary.TraitAccountConfigCore) {
		return nil, nil, fmt.Errorf("%w: %s is not an %s-trait account record",
			substrate.ErrValidation, record, vocabulary.TraitAccountConfigCore)
	}
	b, ok := ds.registry().BundleOf(ty.Package)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s belongs to no bundle", substrate.ErrValidation, record)
	}
	st, err := ds.bundleStateOf(ctx, b.Package)
	if err != nil {
		return nil, nil, err
	}
	if st.blocked() {
		return nil, nil, fmt.Errorf("%w: bundle %s is not live — enable it before connecting accounts",
			substrate.ErrGuard, b.Package)
	}
	return row, b, nil
}

// bundleOAuthMeta returns the bundle's TRUSTED provider metadata, or a refusal
// when the bundle declares no oauth2 flow. Endpoints ONLY ever come from here.
func bundleOAuthMeta(b *vocabulary.Bundle) (*vocabulary.BundleOAuth2, error) {
	if b.OAuth2 == nil {
		return nil, fmt.Errorf("%w: bundle %s declares no oauth2 provider metadata", substrate.ErrGuard, b.Package)
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
		return "", "", fmt.Errorf("%w: bundle %s declares no oauth2 block", substrate.ErrGuard, b.Package)
	}
	ri, err := ds.resolveBundleInput(ctx, b, b.OAuth2.ClientInput)
	if err != nil {
		return "", "", err
	}
	if ri.Row == nil {
		return "", "", fmt.Errorf("%w: bundle %s's %q input does not resolve (%s) — the OAuth client record carries clientId and clientSecret",
			substrate.ErrGuard, b.Package, b.OAuth2.ClientInput, ri.Detail)
	}
	clientID = propString(ri.Row, "clientId")
	clientSecret, err = ds.openSecretValue(ctx, propString(ri.Row, "clientSecret"))
	if err != nil {
		return "", "", err
	}
	if clientID == "" || clientSecret == "" {
		return "", "", fmt.Errorf("%w: bundle %s's client record %s/%s is missing clientId or clientSecret",
			substrate.ErrGuard, b.Package, ri.Input.Kind, ri.Row.ID)
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

// StartOAuth begins the connect flow for one account record, named by its
// record path `<authority>/<package>/<kind>/<id>`: it returns the provider
// consent URL, state signed over (repository, record path, nonce). The
// requested scope union is DERIVED from the account's enabled feature
// toggles; the flow is gated on the OWNER TIER —
// an actor DECLARED below owner (an authority's or a bundle's own hand) may
// not start or restart a consent. An undeclared actor reads
// as owner (actorTier's default), so the gate binds declared machine actors,
// not unknown names.
// The nonce's hash lands in oauth_flows beside the flow's PKCE verifier, with
// the state's own expiry: the callback consumes it exactly once, so a captured
// state cannot replay.
func (ds *dataset) StartOAuth(ctx context.Context, actor substrate.Actor, record string) (string, error) {
	fl := ds.svc.oauth
	if fl == nil {
		return "", fmt.Errorf("%w: oauth is not configured on this substrate", substrate.ErrValidation)
	}
	if ds.actorTier(actor) != substrate.TierOwner {
		return "", fmt.Errorf("%w: only the owner may start an oauth flow, not %s", substrate.ErrForbidden, actor)
	}
	account, err := ds.findAccountRef(ctx, record)
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
	return fl.AuthCodeURL(ep, oauthflow.State{Repository: ds.Repository().ID, Record: vocabulary.RecordPath(row.Kind, row.ID), Nonce: nonce}, verifier)
}

// putOAuthFlow persists one started flow: the nonce hashed (a database read
// must not hand out redeemable states), the PKCE verifier sealed like any
// credential. Expired leftovers prune opportunistically.
func (ds *dataset) putOAuthFlow(ctx context.Context, nonce string, account eref, verifier string, exp time.Time) error {
	// The PKCE verifier is an ephemeral row in oauth_flows, not the sealed
	// store, and 0023 binds the sealed store: it keeps the unbound framing.
	sealed, err := ds.sealPayload([]byte(verifier), nil)
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
// accountOfFlow is the account a pending flow was started for: the identity
// `start` stored beside the verifier. It READS, and consumeOAuthFlow still
// deletes, so a consent that fails before the exchange (a disabled bundle, a
// bundle whose client input went away) leaves the state usable once the
// obstacle is gone, exactly as it did when the id was searched instead.
func (ds *dataset) accountOfFlow(ctx context.Context, nonce string) (eref, error) {
	var ref eref
	err := ds.db.QueryRowContext(ctx, `
		SELECT record_kind, record_id FROM oauth_flows
		WHERE nonce_hash = $1 AND expires_at > now()`, hashToken(nonce)).Scan(&ref.Kind, &ref.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return eref{}, fmt.Errorf("%w: unknown or already-used oauth state", substrate.ErrAuth)
	}
	if err != nil {
		return eref{}, err
	}
	return ref, nil
}

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
	verifier, err := ds.openPayload(sealed, nil)
	if err != nil {
		return "", fmt.Errorf("substrate/engine: open oauth flow verifier: %w", err)
	}
	return string(verifier), nil
}

// CompleteOAuth finishes a consent: the provider redirected the browser back
// with the signed state and a code. Unauthenticated by nature — the state IS
// the authentication — so it lives on the service and resolves the repository
// itself. Returns the connected record's path.
func (s *service) CompleteOAuth(ctx context.Context, state, code string) (string, error) {
	if s.oauth == nil {
		return "", fmt.Errorf("%w: oauth is not configured on this substrate", substrate.ErrValidation)
	}
	st, err := s.oauth.VerifyState(state)
	if err != nil {
		return "", fmt.Errorf("%w: %w", substrate.ErrAuth, err)
	}
	dsAny, err := s.Dataset(ctx, st.Repository)
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
	record := st.Record
	// THE FLOW ROW IS THE BINDING: `start` wrote the account's (kind, id) into
	// oauth_flows beside the verifier, and the state carries the same identity
	// as a record path, so the two must agree before anything is exchanged
	// (issue #574). A state signed under the binary that carried a bare id
	// disagrees here and its consent is restarted; a state lives
	// oauthflow.StateTTL, so no stored state is migrated.
	account, err := ds.accountOfFlow(ctx, st.Nonce)
	if err != nil {
		return err
	}
	if vocabulary.RecordPath(account.Kind, account.ID) != record {
		return fmt.Errorf("%w: unknown or already-used oauth state", substrate.ErrAuth)
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
		ds.svc.log.Warn("substrate: oauth code exchange failed", "record", record, "error", err)
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
			ds.svc.log.Warn("substrate: oauth could not derive account email", "record", record, "error", err)
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
		// The shared registry-dependency lock before the row lock below, the
		// order every put takes (changelog < registry-dep < subject-type <
		// record, stated at rows.go changelogLockKey; inTx has taken the first):
		// the patch this transaction ends in takes it too, but only after the
		// row is held, and a vocabulary apply holding the exclusive side while
		// waiting on this row would deadlock against a transaction queueing for
		// the shared side with the row in hand.
		if err := t.lockRegistryDepShared(); err != nil {
			return err
		}
		if err := t.lockRecord(account); err != nil {
			return err
		}
		row, err := t.loadRow(account, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return fmt.Errorf("%w: account %s was deleted while its consent was in flight", substrate.ErrConflict, record)
		}
		// The stored ref names the credential row; reusing it keeps the same
		// credential-store key across a reconnect.
		ref := propString(row, propTokenRef)
		if ref == "" {
			if ref, err = newID(); err != nil {
				return err
			}
		}
		if err := t.putCredential(ref, account, tok); err != nil {
			return err
		}
		ty, err := t.resolveType(row.Kind)
		if err != nil {
			return err
		}
		// The secret-typed ref, the status, and the granted scope set land on
		// the record; the facility's finalizer holds it against GC so deletion
		// runs teardown first.
		props := map[string]any{propTokenRef: ref, propTokenStatus: tokenStatusConnected}
		props[propGrantedScopes] = stringsToAny(grantedScopes)
		// A fresh consent answers the last refresh failure: its reason goes.
		if ty, err := ds.resolveType(row.Kind); err == nil {
			for _, name := range []string{propTokenError, propTokenErrorAt} {
				if oauthWrites(ty, name) && row.Props[name] != nil {
					props[name] = nil
				}
			}
		}
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
		// A `sync`-trait account's half of the reconnect rides the same patch
		// at the tiers its properties' writers need: one changelog entry, so
		// a record trigger sees the new status, the cleared error and the
		// sync request together and fires once.
		tiers := map[string]substrate.Tier{}
		syncOnReconnect(ty, row, t.now, props, tiers)
		t.propTiers = &propTiers{ref: account, tiers: tiers}
		defer func() { t.propTiers = nil }()
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

// oauthPassBudget bounds one repository's refresh pass and one finalizer
// pass. The process walks every repository in turn, so a token endpoint
// that answers slowly, each call inside oauthflow.CallTimeout, still cannot
// hold every later repository's refresh behind it.
const oauthPassBudget = 2 * time.Minute

// The refresh backoff (#913). A refresh the token endpoint refused with a 4xx
// other than 408 or 429 (oauthflow.Refused) meets the same refusal when it is
// sent again, so neither the refresh loop nor an invocation sends it again
// until the backoff ends: a minute after the first refusal, doubling with
// each refusal after that, up to six hours. A new generation of the
// credential (a reconnect, a refresh that landed) starts over.
const (
	oauthBackoffFirst = time.Minute
	oauthBackoffMax   = 6 * time.Hour
)

// oauthParkAfter is the backoff step at which a refused credential leaves
// the refresh loop's work list the way an `invalid_grant` one does, and the
// account is told to reconnect (0180). The step doubles from a minute, so
// it reaches an hour at the seventh refusal of one generation, about an
// hour after the first: a refusal that has lasted that long is not passing.
const oauthParkAfter = time.Hour

// A provider that rotates refresh tokens (WHOOP) spends the one a refresh
// sends, so the fresh token is the only grant left the moment the provider
// answers (#913). The call and the store therefore run detached from the
// caller's context, each under its own bound: a canceled invocation, the
// pass budget or a shutdown cannot cut them between the provider's answer
// and the store. The store is tried oauthStoreAttempts times, pausing
// oauthStoreRetryPause and then double that between attempts, inside
// oauthStoreBudget for all of them.
const (
	oauthCallBudget      = oauthflow.CallTimeout
	oauthStoreBudget     = commitBudget
	oauthStoreAttempts   = 3
	oauthStoreRetryPause = 250 * time.Millisecond
)

// oauthRefreshes is every credential's refresh state in this process, keyed
// by repository and credential ref. A repository has one writer process
// (0083), so a turn taken here serializes every refresh of a credential.
// It is memory only: a restart forgets every backoff, and the first refresh
// after it meets the refusal again and starts at the first step.
var oauthRefreshes = &refreshTable{entries: map[refreshKey]*refreshEntry{}}

type refreshKey struct{ repository, ref string }

// refreshTable holds the entries. mu guards the map and every field of an
// entry except turn. pending is only set or taken by the hand holding the
// turn, and it is read under mu as well because leave and arm decide
// whether to forget an entry on it.
type refreshTable struct {
	mu      sync.Mutex
	entries map[refreshKey]*refreshEntry
}

type refreshEntry struct {
	// turn holds one token, taken from reading the stored token to storing
	// the fresh one. Two refreshes of one rotating refresh token would both
	// reach the provider, and the compare-and-swap would keep the token the
	// provider rotated away.
	turn chan struct{}
	// users counts the hands holding or waiting for the turn. The entry
	// leaves the table at zero unless a backoff is armed.
	users int
	// The backoff: the generation (sealed.updated_at) it was armed for, its
	// step, when it ends, and the refusal it answers with until then. step
	// is zero when none is armed.
	gen   time.Time
	step  time.Duration
	until time.Time
	err   error
	// refusals counts the refusals of generation gen since the first, at
	// since; the repeated-refusal park reports both.
	refusals int
	since    time.Time
	// pending is a fresh token the provider issued whose store failed: the
	// one live grant of a provider that rotates refresh tokens. The next
	// refresh tries to store it first and, until a store lands, it stands in
	// for the stored token. While it is held the entry stays in the table.
	pending *unstoredToken
}

// unstoredToken is a refreshed token held in memory because its store
// failed, with the generation and the account the refresh read it for.
type unstoredToken struct {
	tok     *oauth2.Token
	gen     time.Time
	account eref
}

// take waits for the credential's turn, or for ctx to end.
func (t *refreshTable) take(ctx context.Context, k refreshKey) (*refreshEntry, error) {
	t.mu.Lock()
	e := t.entries[k]
	if e == nil {
		e = &refreshEntry{turn: make(chan struct{}, 1)}
		t.entries[k] = e
	}
	e.users++
	t.mu.Unlock()
	select {
	case e.turn <- struct{}{}:
		return e, nil
	case <-ctx.Done():
		t.leave(k, e)
		return nil, ctx.Err()
	}
}

// give hands the turn back.
func (t *refreshTable) give(k refreshKey, e *refreshEntry) {
	<-e.turn
	t.leave(k, e)
}

func (t *refreshTable) leave(k refreshKey, e *refreshEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e.users--
	if e.users == 0 && e.step == 0 && e.pending == nil {
		delete(t.entries, k)
	}
}

// backedOff returns the refusal a backoff armed for generation gen still
// answers with at now, or nil. A backoff armed for another generation is
// dropped: the credential it refused has been replaced.
func (t *refreshTable) backedOff(e *refreshEntry, gen, now time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.step == 0 {
		return nil
	}
	if !e.gen.Equal(gen) {
		e.gen, e.step, e.until, e.err = time.Time{}, 0, time.Time{}, nil
		e.refusals, e.since = 0, time.Time{}
		return nil
	}
	if now.Before(e.until) {
		return e.err
	}
	return nil
}

// arm records a refusal of generation gen at now and returns the step it
// waits: the first step, or double the last one for the same generation, up
// to the cap. It counts the generation's refusals from the first.
func (t *refreshTable) arm(e *refreshEntry, gen, now time.Time, err error) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	step := oauthBackoffFirst
	if e.step > 0 && e.gen.Equal(gen) {
		step = min(2*e.step, oauthBackoffMax)
		e.refusals++
	} else {
		e.refusals, e.since = 1, now
	}
	e.gen, e.step, e.until, e.err = gen, step, now.Add(step), err
	// An entry nobody uses whose backoff ended more than the cap ago has not
	// been refused since, a deleted credential's among them. Forgetting it
	// keeps the table to the credentials refused lately; a refusal after
	// that starts at the first step, as one after a restart does.
	cutoff := now.Add(-oauthBackoffMax)
	for k, other := range t.entries {
		if other.users == 0 && other.step > 0 && other.pending == nil && other.until.Before(cutoff) {
			delete(t.entries, k)
		}
	}
	return step
}

// clear drops the backoff once a refresh landed.
func (t *refreshTable) clear(e *refreshEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e.gen, e.step, e.until, e.err = time.Time{}, 0, time.Time{}, nil
	e.refusals, e.since = 0, time.Time{}
}

// refusalState is the backoff in force: its step, and how many refusals of
// its generation it counts since when.
func (t *refreshTable) refusalState(e *refreshEntry) (step time.Duration, refusals int, since time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return e.step, e.refusals, e.since
}

// pendingToken is the token kept in memory for e, or nil. Called by the hand
// holding the turn.
func (t *refreshTable) pendingToken(e *refreshEntry) *unstoredToken {
	t.mu.Lock()
	defer t.mu.Unlock()
	return e.pending
}

// keep sets or, with nil, drops the token kept in memory for e. Called by
// the hand holding the turn.
func (t *refreshTable) keep(e *refreshEntry, p *unstoredToken) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e.pending = p
}

// oauthNow is the clock the refresh backoff runs on: the OAuth client's own
// (oauthflow.Client.Now, the seam its state expiry reads), else the wall
// clock.
func (ds *dataset) oauthNow() time.Time {
	if c := ds.svc.oauth; c != nil && c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// refreshOutcome is what one refreshCredential call came to.
type refreshOutcome struct {
	// tok is the token to use: the fresh one, the one another hand stored
	// while the caller waited for the turn, or one kept in memory because
	// its store failed. Nil when the refresh failed.
	tok *oauth2.Token
	// gen is the generation read under the turn, the anchor a caller parks a
	// refused credential on.
	gen time.Time
	// refreshed is set when this call stored a fresh token.
	refreshed bool
	// unstored is set when tok is a fresh token whose store failed: it is
	// held in memory until a later refresh stores it.
	unstored bool
	// failed is why the stored token was not refreshed: the provider's
	// refusal, a transport error, the refusal a backoff still answers with,
	// or a stored credential that cannot be refreshed at all.
	failed error
	// backedOff is set when failed is a backoff's answer and the provider
	// was not asked.
	backedOff bool
	// armed is set when failed armed a backoff step, which logged it.
	armed bool
	// step, refusals and since are the backoff in force when failed is a
	// refusal, armed by this call or an earlier one: its step, and how many
	// refusals of the generation it counts since when.
	step     time.Duration
	refusals int
	since    time.Time
}

// refreshCredential refreshes the credential under ref, bound to the account
// want, whose token the caller read at generation seen and found due. It is
// the one path from a stored refresh token to the provider, shared by the
// refresh loop and an invocation's token resolution. It holds the
// credential's turn from reading the token to storing the fresh one and
// reads the token again under it, so a refresh that waited behind another
// finds the token the other stored instead of sending a refresh token the
// provider may have rotated away. A credential in backoff answers with its
// last refusal and the provider is not asked.
//
// Once the provider is asked, nothing the caller does reaches the call or
// the store (0180): both run detached, under their own bounds, because a
// provider that rotates refresh tokens has spent the old one by the time
// it answers. A store that fails every attempt keeps the fresh token in
// memory, and the next call under the turn tries to store it first; until
// a store lands, the kept token stands in for the stored one. Nothing asks
// the provider while the process cannot write (a read-only service, a lost
// writer lease). A refusal is recorded only while the credential is still
// the generation the refused token came from. The error is the
// engine's own failure (the turn, reading the credential); everything else
// is the outcome's.
func (ds *dataset) refreshCredential(ctx context.Context, ref string, want eref, seen time.Time, ep oauthEndpoints) (refreshOutcome, error) {
	k := refreshKey{repository: ds.scope.Repository, ref: ref}
	e, err := oauthRefreshes.take(ctx, k)
	if err != nil {
		return refreshOutcome{}, err
	}
	defer oauthRefreshes.give(k, e)
	tok, account, gen, err := ds.getCredential(ctx, ref)
	if err != nil {
		return refreshOutcome{}, err
	}
	out := refreshOutcome{gen: gen}
	if account != want {
		out.failed = errors.New("the stored credential reference belongs to another account")
		return out, nil
	}
	landed := false
	// A token an earlier refresh could not store is the live grant: it is
	// stored before anything else, and while it cannot be, it stands in for
	// the stored one, so the provider is asked only once it is due and with
	// its own refresh token, never the spent one the store still holds.
	if p := oauthRefreshes.pendingToken(e); p != nil {
		if !p.gen.Equal(gen) || p.account != account {
			// A reconnect stored a new grant over the spent one. The kept
			// token is not revoked: at some providers a revoke ends every
			// token of the user's grant, the reconnect's among them.
			oauthRefreshes.keep(e, nil)
			ds.svc.log.Warn("substrate: oauth refresh: a rotated token that was never stored was replaced by a reconnect; it is dropped",
				"record", account.ID, "ref", ref)
		} else {
			storeCtx, cancelStore := ds.oauthStoreContext(ctx)
			defer cancelStore()
			swapped, err := ds.storeRefreshedToken(storeCtx, ref, account, p.tok, gen)
			switch {
			case err != nil:
				ds.svc.log.Error("substrate: oauth refresh: the token the provider rotated is still not stored; it is kept in memory and stands in for the stored one until a store lands",
					"record", account.ID, "ref", ref, "attempts", oauthStoreAttempts, "error", err)
				tok, out.unstored = p.tok, true
			case !swapped:
				oauthRefreshes.keep(e, nil)
				ds.svc.log.Warn("substrate: oauth refresh succeeded, but a reconnect or a teardown replaced the credential first; the refreshed token is not stored",
					"record", account.ID)
				out.tok = p.tok
				return out, nil
			default:
				oauthRefreshes.keep(e, nil)
				oauthRefreshes.clear(e)
				ds.noteTokenRefresh(storeCtx, account, nil, time.Time{})
				ds.svc.log.Info("substrate: oauth refresh: the token kept in memory is stored", "record", account.ID, "ref", ref)
				if tok, _, gen, err = ds.getCredential(storeCtx, ref); err != nil {
					return refreshOutcome{}, err
				}
				out.gen, landed = gen, true
				if !accessTokenDue(tok) {
					out.tok, out.refreshed = tok, true
					return out, nil
				}
			}
		}
	}
	if out.unstored {
		if !accessTokenDue(tok) {
			out.tok = tok
			return out, nil
		}
	} else if !landed && !gen.Equal(seen) && (tok.Expiry.IsZero() || nowUTC().Before(tok.Expiry)) {
		// Another hand stored a token after the caller read its own (a
		// refresh that held the turn first, a reconnect): that token is the
		// answer while it is unexpired, however short a lifetime the provider
		// gave it, so concurrent callers make one provider call.
		out.tok = tok
		return out, nil
	}
	if tok.RefreshToken == "" {
		out.failed = fmt.Errorf("token for %s expired and cannot refresh", account.ID)
		return out, nil
	}
	if refusal := oauthRefreshes.backedOff(e, gen, ds.oauthNow()); refusal != nil {
		out.failed, out.backedOff = refusal, true
		out.step, out.refusals, out.since = oauthRefreshes.refusalState(e)
		return out, nil
	}
	// A caller that already ended does not start a call it will not use.
	// Past this point the call is not the caller's to cut.
	if err := ctx.Err(); err != nil {
		return refreshOutcome{}, err
	}
	// A process that cannot write does not ask either: a provider that
	// rotates refresh tokens spends the one it is sent, and the store of the
	// fresh one would be refused. A refresh sent while the writer lease was
	// lost is how the owner's WHOOP grant was lost (#913).
	if err := ds.oauthWriteRefusal(); err != nil {
		return refreshOutcome{}, err
	}
	callCtx, cancelCall := context.WithTimeout(context.WithoutCancel(ctx), ds.oauthCallBudget())
	fresh, err := ds.svc.oauth.Refresh(callCtx, ep, tok)
	cancelCall()
	// The store's bound starts once the provider has answered, so a slow
	// answer or a call that ran out its own bound still leaves the store,
	// and the stamp of a failure, their whole budget.
	storeCtx, cancelStore := ds.oauthStoreContext(ctx)
	defer cancelStore()
	if err != nil {
		// A reconnect that landed while the refusal was in flight replaced
		// the grant the provider refused: the refusal says nothing about the
		// new one, so nothing is armed or stamped, and a caller waiting
		// behind the reconnect gets its token.
		replaced, rerr := ds.credentialReplaced(storeCtx, ref, gen)
		if rerr == nil && replaced {
			ds.svc.log.Info("substrate: oauth refresh refused, but a reconnect replaced the credential first; nothing is recorded",
				"record", account.ID, "error", err)
			if now, nowAccount, _, gerr := ds.getCredential(storeCtx, ref); gerr == nil && nowAccount == account &&
				(now.Expiry.IsZero() || nowUTC().Before(now.Expiry)) {
				out.tok = now
				return out, nil
			}
			out.failed = err
			return out, nil
		}
		out.failed = err
		stamp := err
		if oauthflow.Refused(err) {
			step := oauthRefreshes.arm(e, gen, ds.oauthNow(), err)
			out.armed = true
			out.step, out.refusals, out.since = oauthRefreshes.refusalState(e)
			ds.svc.log.Warn("substrate: oauth refresh refused; no refresh of this credential is sent until the backoff ends or the account reconnects",
				"record", account.ID, "backoff", step, "refusals", out.refusals, "error", err)
			// A refusal at or past the park's step says what the park said,
			// so the next invocation's refusal of a parked credential keeps
			// the count, the first refusal and the reconnect ask.
			if step >= oauthParkAfter {
				stamp = repeatedRefusal(out.refusals, out.since, err)
			}
		}
		if hook := ds.svc.testOAuthBeforeStamp; hook != nil {
			hook()
		}
		ds.noteTokenRefresh(storeCtx, account, stamp, gen)
		return out, nil
	}
	// Update-only, compare-and-swap on the generation read under the turn: a
	// teardown or a reconnect that landed while the provider call was in
	// flight wins, and this refresh persists nothing (never recreating a
	// deleted credential). The fresh token still serves the caller.
	swapped, err := ds.storeRefreshedToken(storeCtx, ref, account, fresh, gen)
	out.tok = fresh
	if err != nil {
		// The provider may have spent the refresh token this call sent, so
		// the fresh token may be the only grant left: it is kept, and the
		// caller still gets its access token.
		oauthRefreshes.keep(e, &unstoredToken{tok: fresh, gen: gen, account: account})
		out.unstored = true
		ds.svc.log.Error("substrate: oauth refresh: the provider rotated the refresh token but the store failed; the token is kept in memory and stands in for the stored one until a store lands",
			"record", account.ID, "ref", ref, "attempts", oauthStoreAttempts, "error", err)
		return out, nil
	}
	if !swapped {
		// A provider that rotates refresh tokens has spent the one this call
		// sent, and the token it returned is dropped here. A lost swap was
		// the first suspect when a credential was refused for days (#913),
		// and nothing recorded whether one had happened.
		ds.svc.log.Warn("substrate: oauth refresh succeeded, but a reconnect or a teardown replaced the credential first; the refreshed token is not stored",
			"record", account.ID)
		return out, nil
	}
	oauthRefreshes.keep(e, nil)
	oauthRefreshes.clear(e)
	ds.noteTokenRefresh(storeCtx, account, nil, time.Time{})
	out.refreshed = true
	return out, nil
}

// oauthStoreContext is the bound a refresh's store and stamps run under:
// detached from the caller, oauthStoreBudget from now.
func (ds *dataset) oauthStoreContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), oauthStoreBudget)
}

// oauthCallBudget bounds one refresh's provider call: oauthCallBudget, or a
// test's shorter one.
func (ds *dataset) oauthCallBudget() time.Duration {
	if d := ds.svc.testOAuthCallBudget; d > 0 {
		return d
	}
	return oauthCallBudget
}

// oauthWriteRefusal is the standing refusal a refresh's store would meet:
// a service opened read-only, or a process that lost its writer lease
// (0083). Neither passes inside a refresh, so a refresh does not ask the
// provider while one stands, and its store is not tried again on one.
func (ds *dataset) oauthWriteRefusal() error {
	if ds.svc.readOnly {
		return ErrDirectoryReadOnly
	}
	return ds.svc.lease.err()
}

// repeatedRefusal is the reason a credential refused until its backoff step
// reached oauthParkAfter carries (0180): how many refusals of its
// generation, since when, the last answer, and the ask.
func repeatedRefusal(refusals int, since time.Time, last error) error {
	return fmt.Errorf("refused %d times since %s, last answer: %w; reconnect the account",
		refusals, since.UTC().Format(time.RFC3339), last)
}

// storeRefreshedToken stores a refreshed token through updateCredential's
// compare-and-swap, trying again when the store fails: a token the provider
// rotated must not be lost to one failed commit. A lost swap is an answer,
// not a failure, and is not tried again; neither is a refusal that does not
// pass inside a refresh (oauthWriteRefusal: a read-only service, a lost
// writer lease).
func (ds *dataset) storeRefreshedToken(ctx context.Context, ref string, account eref, tok *oauth2.Token, seen time.Time) (bool, error) {
	pause := oauthStoreRetryPause
	var err error
	for attempt := 1; ; attempt++ {
		var swapped bool
		swapped, err = ds.updateCredential(ctx, ref, account, tok, seen)
		if err == nil {
			return swapped, nil
		}
		if errors.Is(err, ErrDirectoryReadOnly) || errors.Is(err, ErrWriterLeaseLost) || attempt == oauthStoreAttempts {
			return false, fmt.Errorf("after %d attempts: %w", attempt, err)
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("after %d attempts: %w", attempt, err)
		case <-time.After(pause):
		}
		pause *= 2
	}
}

// accountAccessToken resolves a credential ref into a LIVE access token for
// one invocation: a token expiring within a minute refreshes and persists on
// the spot (refreshCredential), so a slow refresh loop never hands a body a
// corpse. While the credential is backed off, the error is its last refusal
// and the provider is not asked.
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
		return "", errors.New("the stored credential reference belongs to another account")
	}
	if !accessTokenDue(tok) {
		return tok.AccessToken, nil
	}
	if ds.svc.oauth == nil || tok.RefreshToken == "" {
		return "", fmt.Errorf("token for %s expired and cannot refresh", account.ID)
	}
	out, err := ds.refreshCredential(ctx, ref, account, seen, ep)
	if err != nil {
		return "", err
	}
	if out.failed != nil {
		return "", out.failed
	}
	return out.tok.AccessToken, nil
}

// accessTokenDue reports whether a stored token is too close to its expiry to
// hand an invocation: inside a minute of it, or past it.
func accessTokenDue(tok *oauth2.Token) bool {
	return !tok.Expiry.IsZero() && !nowUTC().Add(time.Minute).Before(tok.Expiry)
}

// RefreshOAuthTokens is the central refresh loop's pass: every credential
// expiring inside the window refreshes against its bundle's declared token
// endpoint. Disabled and uninstalled bundles are skipped — frozen accounts
// refresh on demand after re-enable. Returns how many tokens refreshed.
func (ds *dataset) RefreshOAuthTokens(ctx context.Context) (int, error) {
	if ds.svc.oauth == nil {
		return 0, nil
	}
	// A process that cannot write sends no refresh (refreshCredential): the
	// pass ends here and the next one tries again.
	if err := ds.oauthWriteRefusal(); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, oauthPassBudget)
	defer cancel()
	horizon := nowUTC().Add(oauthRefreshWindow)
	refs, err := ds.expiringCredentials(ctx, horizon)
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
		// The budget ran out or the process is stopping: what is left waits
		// for the next pass.
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		tok, account, seen, err := ds.getCredential(ctx, ref)
		if err != nil {
			ds.svc.log.Warn("substrate: oauth refresh: reading a credential", "ref", ref, "error", err)
			continue
		}
		if tok.RefreshToken == "" {
			continue
		}
		// An invocation or a reconnect stored a token past the window since
		// the list was read: nothing is due this pass.
		if tok.Expiry.IsZero() || !tok.Expiry.Before(horizon) {
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
		b, ok := ds.registry().BundleOf(ty.Package)
		if !ok || states[b.Package].blocked() {
			continue
		}
		meta, err := bundleOAuthMeta(b)
		if err != nil {
			ds.svc.log.Warn("substrate: oauth refresh: bundle metadata", "bundle", b.Package, "error", err)
			continue
		}
		clientID, clientSecret, err := ds.oauthClientOf(ctx, b)
		if err != nil {
			ds.svc.log.Warn("substrate: oauth refresh: bundle configuration", "bundle", b.Package, "error", err)
			continue
		}
		ep := oauthEndpointsFor(meta, clientID, clientSecret, nil)
		out, err := ds.refreshCredential(ctx, ref, account, seen, ep)
		if errors.Is(err, errCredentialGone) {
			continue // torn down since the list was read
		}
		if err != nil {
			return n, err
		}
		switch {
		case out.refreshed:
			n++
		case out.failed == nil:
			// Another hand's token, or a fresh token kept in memory because
			// its store failed, which logged it.
		case ctx.Err() != nil:
			// The pass is over: nothing is concluded from its last answer.
		case oauthflow.GrantRefused(out.failed):
			// `invalid_grant` is final for this refresh token, and only a
			// reconnect replaces it: retrying it every pass would call the
			// provider once a minute for nothing. A parked credential leaves
			// the work list, so the refusal is logged once, here.
			parked, perr := ds.parkRefusedCredential(ctx, ref, account, out.gen)
			if perr != nil {
				return n, perr
			}
			if parked {
				ds.svc.log.Warn("substrate: oauth refresh refused with invalid_grant; the refresh loop skips this credential until the account reconnects",
					"record", account.ID, "error", out.failed)
			}
		case out.step >= oauthParkAfter:
			// Any other refusal that has lasted until the backoff step
			// reached an hour (the seventh, armed by this pass or by an
			// invocation) is parked the same way, and the account says how
			// long and asks for a reconnect (0180). The park is a
			// compare-and-swap on the refused generation, so a reconnect in
			// flight keeps its credential listed.
			parked, perr := ds.parkRefusedCredential(ctx, ref, account, out.gen)
			if perr != nil {
				return n, perr
			}
			if parked {
				// The refusal that reached the step stamped this reason
				// already; this stamp writes nothing unless that one failed.
				ds.noteTokenRefresh(ctx, account, repeatedRefusal(out.refusals, out.since, out.failed), out.gen)
				ds.svc.log.Warn("substrate: oauth refresh refused repeatedly; the refresh loop skips this credential until the account reconnects",
					"record", account.ID, "refusals", out.refusals, "since", out.since, "error", out.failed)
			}
		case out.backedOff:
			// Logged when its step was armed.
		case !out.armed:
			ds.svc.log.Warn("substrate: oauth refresh failed — the account needs a reconnect if this persists",
				"record", account.ID, "error", out.failed)
		}
	}
	return n, nil
}

// ProcessOAuthFinalizers runs teardown for deleted accounts: every
// tombstoned record still holding the facility's finalizer has its grant
// revoked (best effort, against the bundle's declared revocation endpoint),
// its stored credentials dropped, and the finalizer released — after which
// the ordinary GC sweep collects it. Purge rides exactly this path.
func (ds *dataset) ProcessOAuthFinalizers(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, oauthPassBudget)
	defer cancel()
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

// revokeAccountGrants posts every stored token of a deleted account, and
// every token kept in memory for it, to its
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
	refs, err := ds.credentialRefsFor(ctx, eref{Kind: typeIdent, ID: recordID})
	if err != nil {
		return
	}
	// A token kept in memory because its store failed is the account's live
	// grant at a provider that rotates refresh tokens, and the stored one is
	// spent: it is revoked beside the stored ones and dropped, so nothing
	// holds it once the rows are gone (0180).
	kept := ds.takeKeptTokens(ctx, refs)
	ty, ok := ds.registry().ByIdentity(typeIdent)
	if !ok {
		return
	}
	b, ok := ds.registry().BundleOf(ty.Package)
	if !ok || b.OAuth2 == nil {
		return
	}
	revocationURL := b.OAuth2.RevocationEndpoint
	if revocationURL == "" {
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
	for _, tok := range kept {
		if err := ds.svc.oauth.Revoke(ctx, revocationURL, tok); err != nil {
			ds.svc.log.Warn("substrate: oauth revoke of a token kept in memory", "record", recordID, "error", err)
		}
	}
}

// takeKeptTokens drops the tokens kept in memory for refs and returns them.
// It takes each credential's turn, so a refresh in flight finishes first and
// a token its failed store keeps is taken too.
func (ds *dataset) takeKeptTokens(ctx context.Context, refs []string) []*oauth2.Token {
	var kept []*oauth2.Token
	for _, ref := range refs {
		k := refreshKey{repository: ds.scope.Repository, ref: ref}
		e, err := oauthRefreshes.take(ctx, k)
		if err != nil {
			return kept
		}
		if p := oauthRefreshes.pendingToken(e); p != nil {
			kept = append(kept, p.tok)
			oauthRefreshes.keep(e, nil)
		}
		oauthRefreshes.give(k, e)
	}
	return kept
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
