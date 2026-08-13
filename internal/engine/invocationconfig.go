package engine

// The runner's config resolution: a bundle function's invocation carries
// `config` — the bundle's `inject: functions` inputs, each resolved to one
// record (secrets resolved, keyed by input name under `inputs`), and the
// bundle's account records, each with a live access token injected when the
// bundle authenticates over the host OAuth facility. The resolved map
// crosses only into the invocation; the invocation SCRUBBER (scrub.go)
// holds every surface that leaves the runner boundary — logs, errors,
// outputs — to the injected values. Functions outside any bundle get nil,
// exactly as wave 1 left it.

import (
	"context"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The accountconfig trait's host-written properties (core.substrate.reamde.dev):
// tokenRef is a secret-typed credential-store reference — never a raw token —
// tokenStatus is the connection state the facility last reported, and
// grantedScopes is the scope set the last consent was granted for. All three
// carry `writer: oauth`: only the OAuth facility may set them (review-google
// #2).
const (
	propTokenRef      = "tokenRef"
	propTokenStatus   = "tokenStatus"
	propGrantedScopes = "grantedScopes"
	// propClientSecret is the oauth2 trait's client secret — an OAuth-facility
	// input the host consumes, never injected into a function (review-google
	// #4).
	propClientSecret = "clientSecret"
)

// resolveFunctionConfig builds one invocation's `config` map, plus the list
// of INJECTED SECRET VALUES (secret-typed properties resolved into the map,
// live access tokens) the invocation scrubber holds every outbound surface
// to. Nil config for a function outside any bundle.
func (ds *dataset) resolveFunctionConfig(ctx context.Context, fn *vocabulary.Function) (map[string]any, []string, error) {
	b, ok := ds.registry().BundleOf(fn.Authority)
	if !ok {
		return nil, nil, nil
	}
	cfg := map[string]any{"bundle": b.Authority}
	g, ok := ds.registry().AuthorityByName(b.Authority)
	if !ok {
		return cfg, nil, nil
	}

	// The OAUTH-FACILITY secrets are never injected: the
	// client input's clientSecret and each account's tokenRef are the host's
	// inputs to resolve a token — the function receives the resolved access
	// token instead, so a compromised dependency has no client secret or
	// credential ref to exfiltrate. A connector's OWN input secrets (an
	// apiToken it needs to call its provider) ARE injected, and — like the
	// access token — the invocation scrubber holds them to the runner
	// boundary. Only `inject: functions` inputs cross; a facility-read input
	// (the OAuth client) never does.
	var secrets []string
	inputs := map[string]any{}
	for _, name := range b.InputOrder {
		in := b.Inputs[name]
		if in.Inject != vocabulary.BundleInputInjectFunctions {
			continue
		}
		ri, err := ds.resolveBundleInput(ctx, b, name)
		if err != nil {
			return nil, nil, err
		}
		if ri.Row == nil {
			// An unresolved input injects nothing; the body sees the key
			// absent and refuses in its own words. Which record was wanted
			// is the status page's story, not the invocation's.
			continue
		}
		ity, _ := ds.registry().ByIdentity(in.Kind)
		view, vsecrets := ds.injectedRecordConfig(ctx, ity, ri.Row)
		inputs[name] = view
		secrets = append(secrets, vsecrets...)
	}
	if len(inputs) > 0 {
		cfg["inputs"] = inputs
	}
	oauthEnabled := b.OAuth2 != nil

	// The refresh endpoints (client creds + token endpoint from the manifest,
	// no scopes) are resolved once for the whole bundle. A configuration that
	// cannot yield client credentials simply injects no tokens — each account
	// carries a tokenError the body can surface.
	var refreshEP oauthEndpoints
	haveClient := false
	if oauthEnabled {
		clientID, clientSecret, cerr := ds.oauthClientOf(ctx, b)
		if cerr == nil {
			refreshEP = oauthEndpointsFor(b.OAuth2, clientID, clientSecret, nil)
			haveClient = true
		}
	}

	var accounts []any
	for _, tn := range g.KindOrder {
		t := g.Kinds[tn]
		if !t.Implements(vocabulary.TraitAccountConfigCore) {
			continue
		}
		rows, err := ds.liveRowsOf(ctx, t.Identity)
		if err != nil {
			return nil, nil, err
		}
		for _, row := range rows {
			entry, esecrets := ds.injectedRecordConfig(ctx, t, row)
			secrets = append(secrets, esecrets...)
			if oauthEnabled {
				ref, _ := ds.svc.openPropValue(propString(row, propTokenRef))
				switch {
				case ref == "":
				case !haveClient:
					entry["tokenError"] = "the bundle configuration is missing its client credentials"
				default:
					// The ref is bound to THIS account: a credential row
					// names the account it was issued for, and a tokenRef
					// pointing anywhere else is refused rather than resolved
					// (see accountAccessToken).
					token, err := ds.accountAccessToken(ctx, ref, refreshEP,
						eref{Kind: t.Identity, ID: row.ID})
					if err != nil {
						// A dead grant must not park every delivery of the
						// bundle: the body sees the error beside the account
						// and decides.
						entry["tokenError"] = err.Error()
					} else {
						entry["token"] = token
						secrets = append(secrets, token)
					}
				}
			}
			accounts = append(accounts, entry)
		}
	}
	cfg["accounts"] = accounts
	return cfg, secrets, nil
}

// propString reads a string property off a row, empty when absent or
// non-string.
func propString(row *erow, name string) string {
	s, _ := row.Props[name].(string)
	return s
}

// injectedRecordConfig flattens one record row for the invocation config:
// id, kind, states, title, and its properties with every secret-typed value
// RESOLVED to the material the body needs, plus those materials as the
// strings the invocation scrubber holds to the runner boundary. View and
// scrubber list are built in ONE pass so they cannot disagree: injecting the
// stored ref while arming the scrubber with the resolved value would break
// the body and leak the stored form in the same move. The OAUTH-FACILITY
// secrets (clientSecret, tokenRef) are omitted entirely; the function gets a
// resolved token, never those. A secret that fails to resolve (a dangling
// ref, a wrong credential key) surfaces INLINE under `secretErrors`, the
// same shape as tokenError: one bad value must not park every delivery of
// the whole bundle.
func (ds *dataset) injectedRecordConfig(ctx context.Context, ty *vocabulary.Kind, row *erow) (map[string]any, []string) {
	props := map[string]any{}
	secretErrors := map[string]string{}
	var secrets []string
	for k, v := range row.Props {
		if isOAuthFacilitySecret(ty, k) {
			continue
		}
		if s, isStr := v.(string); isStr && ty != nil {
			if p, ok := ty.Prop(k); ok && p.Secret() {
				plain, err := ds.openSecretValue(ctx, s)
				if err != nil {
					secretErrors[k] = err.Error()
					continue
				}
				props[k] = plain
				if plain != "" {
					secrets = append(secrets, plain)
				}
				continue
			}
		}
		props[k] = v
	}
	for k, v := range row.States {
		props[k] = v
	}
	if row.Title != "" {
		props["title"] = row.Title
	}
	out := map[string]any{"id": row.ID, "kind": row.Kind, "properties": props}
	if len(secretErrors) > 0 {
		out["secretErrors"] = secretErrors
	}
	return out, secrets
}

// isOAuthFacilitySecret reports whether a property is one the OAuth facility
// owns as its own input — the oauth2 config's clientSecret or an account's
// tokenRef — which must never reach a function body. Other
// secret-typed properties (a connector's apiToken) are the body's to use.
func isOAuthFacilitySecret(ty *vocabulary.Kind, name string) bool {
	if ty == nil {
		return false
	}
	if name == propClientSecret && ty.Implements(vocabulary.TraitOAuth2Core) {
		return true
	}
	if name == propTokenRef && ty.Implements(vocabulary.TraitAccountConfigCore) {
		return true
	}
	return false
}

// liveRowsOf reads every live record row of one type, ordered by id.
func (ds *dataset) liveRowsOf(ctx context.Context, typeIdent string) ([]*erow, error) {
	rs, err := ds.db.QueryContext(ctx, `
		SELECT `+recordCols+` FROM records
		WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, typeIdent)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rs.Close() }()
	var out []*erow
	for rs.Next() {
		row, err := scanRecord(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rs.Err()
}

// oauthEndpointsFor assembles the flow package's shape from the TRUSTED
// manifest metadata (endpoints), the config row's client credentials, and the
// account's derived scopes. Endpoints never come from the
// mutable row.
func oauthEndpointsFor(meta *vocabulary.BundleOAuth2, clientID, clientSecret string, scopes []string) oauthEndpoints {
	return oauthEndpoints{
		AuthURL:       meta.AuthorizationEndpoint,
		TokenURL:      meta.TokenEndpoint,
		RevocationURL: meta.RevocationEndpoint,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		Scopes:        scopes,
	}
}
