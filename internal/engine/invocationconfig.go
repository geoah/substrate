package engine

// The runner's config resolution: a bundle function's invocation carries
// `config` — the bundle's `inject: functions` inputs, each resolved to one
// record (secrets resolved, keyed by input name under `inputs`), the bundle's
// own `setting` and `secret` records keyed by name under `settings`
// (settings.go), and the bundle's account records, each with a live access
// token injected when the bundle authenticates over the host OAuth facility. The resolved map
// crosses only into the invocation; the invocation SCRUBBER (scrub.go)
// holds every surface that leaves the runner boundary — logs, errors,
// outputs — to the injected values. Functions outside any bundle get nil.

import (
	"context"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The accountconfig trait's host-written properties (substrate.reamde.dev/core):
// tokenRef is a secret-typed credential-store reference — never a raw token —
// tokenStatus is the connection state the facility last reported, and
// grantedScopes is the scope set the last consent was granted for. All three
// carry `writer: oauth`: only the OAuth facility may set them.
const (
	propTokenRef      = "tokenRef"
	propTokenStatus   = "tokenStatus"
	propGrantedScopes = "grantedScopes"
	// propClientSecret is the oauth2 trait's client secret: an OAuth-facility
	// input the host consumes, never injected into a function.
	propClientSecret = "clientSecret"
)

// resolveFunctionConfig builds one invocation's `config` map, plus the list
// of INJECTED SECRET VALUES (secret-typed properties resolved into the map,
// live access tokens, the bundle's own secrets) the invocation scrubber holds
// every outbound surface to. Nil config for a function outside any bundle.
func (ds *dataset) resolveFunctionConfig(ctx context.Context, fn *vocabulary.Function) (map[string]any, []string, error) {
	b, ok := ds.registry().BundleOf(fn.Package)
	if !ok {
		return nil, nil, nil
	}
	cfg := map[string]any{"bundle": b.Package}
	g, ok := ds.registry().PackageByName(b.Package)
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
	//
	// The OAuth client input is resolved ONCE, before anything is injected:
	// the record a body reads (its `apiBase` among the rest) and the client
	// each account's grant is checked against come from that one resolution.
	// Resolved twice, a client record deleted between the two reads would
	// inject the record's `apiBase` while the second read fell back to the
	// operator's client, and an operator-connected account's token would
	// reach that base. Beside the operator's client no client record is
	// injected at all, so a body of such an account calls the provider's own
	// API.
	oauthEnabled := b.OAuth2 != nil
	var clientInput resolvedInput
	var client oauthClient
	var clientErr error
	if oauthEnabled {
		var err error
		if clientInput, err = ds.resolveBundleInput(ctx, b, b.OAuth2.ClientInput); err != nil {
			return nil, nil, err
		}
		client, clientErr = ds.oauthClientFrom(ctx, ds.db, b, clientInput)
	}

	var secrets []string
	inputs := map[string]any{}
	for _, name := range b.InputOrder {
		in := b.Inputs[name]
		if in.Inject != vocabulary.BundleInputInjectFunctions {
			continue
		}
		var ri resolvedInput
		if oauthEnabled && name == b.OAuth2.ClientInput {
			if clientErr == nil && client.Operator {
				continue
			}
			ri = clientInput
		} else {
			var err error
			if ri, err = ds.resolveBundleInput(ctx, b, name); err != nil {
				return nil, nil, err
			}
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

	// The bundle's SETTINGS (settings.go, decision record 0076): the core
	// `setting` and `secret` records under the bundle's own id prefix, keyed
	// by the name the id ends in. A secret arrives in plaintext and joins the
	// scrubber list, exactly as an input's own secret does. Absent when the
	// bundle ships none, so a function of a bundle with no settings sees the
	// key missing rather than an empty map.
	settings, ssecrets, err := ds.bundleSettings(ctx, b)
	if err != nil {
		return nil, nil, err
	}
	if len(settings) > 0 {
		cfg["settings"] = settings
	}
	secrets = append(secrets, ssecrets...)
	ds.oauthPoint(oauthPointConfigInputs)

	// Each account's token comes from the client resolved above, and only
	// when its grant was issued to that client (accountAccessToken): a grant
	// through the operator's client never meets a base the person's own
	// record names. A configuration that cannot yield a client injects no
	// tokens; each account carries a tokenError the body can surface.
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
				ref := propString(row, propTokenRef)
				account := eref{Kind: t.Identity, ID: row.ID}
				switch {
				case ref == "":
				case clientErr != nil:
					entry["tokenError"] = ds.noClientTokenError(ctx, ref, account, clientErr)
				default:
					// The ref is bound to THIS account: a credential row
					// names the account it was issued for, and a tokenRef
					// pointing anywhere else is refused rather than resolved
					// (see accountAccessToken).
					token, err := ds.accountAccessToken(ctx, ref, account, b.OAuth2, client)
					if err != nil {
						// A dead grant must not park every delivery of the
						// bundle: the body sees the error beside the account
						// and decides. A credential in backoff answers with
						// its last refusal, so a refused grant costs no
						// provider call per delivery.
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
	// `version` rides beside the properties for the same reason the delivery
	// envelope carries one: a provider sync reaches its account through
	// `config.accounts`, never through a host read, and a guarded write needs
	// the version the invocation actually read (decision 0093).
	out := map[string]any{"id": row.ID, "kind": row.Kind, "version": row.Version, "properties": props}
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
	return ds.liveRowsOn(ctx, ds.db, typeIdent)
}

// liveRowsOn is liveRowsOf read through q, the pool or the caller's
// transaction (loadRowOn).
func (ds *dataset) liveRowsOn(ctx context.Context, q dbx, typeIdent string) ([]*erow, error) {
	rs, err := q.QueryContext(ctx, `
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
