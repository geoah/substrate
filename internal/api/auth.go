package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

type ctxKey int

const (
	ctxKeyDataset ctxKey = iota
	ctxKeyActor
	ctxKeyPeer
	ctxKeyRequestID
	// ctxKeyLease is an app token's standing for a stream (tokenlease.go).
	ctxKeyLease
	// ctxKeyMCPDoor marks a request the MCP door makes (mcp.go): the one
	// place an assistant's token authenticates (decision 0230). It is set on
	// the context alone, never read from a header, so no client can claim it.
	ctxKeyMCPDoor
	// ctxKeyHeld marks a request whose person is not an admin: the routes
	// for an admin alone refuse it, and the rest are held to their roles by
	// the person's view (decision 0294).
	ctxKeyHeld
)

// heldToRoles reports whether the request's person is held to their roles.
func heldToRoles(ctx context.Context) bool {
	held, _ := ctx.Value(ctxKeyHeld).(bool)
	return held
}

// personView is the dataset a token's person reaches the repository through:
// the repository whole for an admin, the person's view held to their roles
// for anybody else (decision 0276). The roles are read on each request, so a
// role changed or taken away holds from the next one; a stream outlives its
// request, and its lease compares the roles' print (rolesPrint) with the one
// answered here.
func personView(ctx context.Context, ds substrate.Dataset, tok substrate.TokenInfo) (substrate.Dataset, bool, string, error) {
	held, err := ds.PersonRoles(substrate.WithIdentity(ctx, tok.Identity))
	if err != nil {
		return nil, false, "", err
	}
	at, err := rolesPrint(held)
	if err != nil {
		return nil, false, "", err
	}
	roles := rolesOf(held, ds.Repository().LocalNames()...)
	if roles.Admin() {
		return ds, false, at, nil
	}
	return newPersonDataset(ds, roles), true, at, nil
}

// rolesPrint is the roles a person holds as one comparable string: which
// roles, and every rule of each.
func rolesPrint(held []substrate.RoleRules) (string, error) {
	sorted := slices.SortedFunc(slices.Values(held), func(a, b substrate.RoleRules) int { return strings.Compare(a.ID, b.ID) })
	raw, err := json.Marshal(sorted)
	return string(raw), err
}

// withMCPDoor marks ctx as the MCP door's.
func withMCPDoor(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyMCPDoor, true)
}

// onMCPDoor reports whether the MCP door made the request.
func onMCPDoor(ctx context.Context) bool {
	on, _ := ctx.Value(ctxKeyMCPDoor).(bool)
	return on
}

// withRequestAuth binds the request's authenticated write context: the actor
// the token writes as, derived from the token and never named by the request,
// the token id the engine stamps on every changelog entry the request
// appends, and the person the request acts for, which a function or an agent
// it starts records as its `onBehalfOf` (decision 0270).
func withRequestAuth(ctx context.Context, ds substrate.Dataset, tok substrate.TokenInfo, actor substrate.Actor) context.Context {
	ctx = context.WithValue(ctx, ctxKeyDataset, ds)
	ctx = substrate.WithPrincipal(ctx, tok.ID)
	ctx = substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)
	ctx = substrate.WithIdentity(ctx, tok.Identity)
	return context.WithValue(ctx, ctxKeyActor, actor)
}

// DatasetFrom returns the authenticated repository dataset, nil when absent.
func DatasetFrom(ctx context.Context) substrate.Dataset {
	ds, _ := ctx.Value(ctxKeyDataset).(substrate.Dataset)
	return ds
}

// ActorFrom returns the acting attribution for the request.
func ActorFrom(ctx context.Context) substrate.Actor {
	a, _ := ctx.Value(ctxKeyActor).(substrate.Actor)
	return a
}

func bearerSecret(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

// authenticate resolves the bearer token and the acting actor. A token
// without scopes has full access to its repository, so for it this is the
// whole check: the hash lookup finds the token record, and the repository
// holding it is the repository the request runs in. A token an app received
// gets the scoped dataset instead (decision 0194).
//
// The actor is derived from the token alone: a full-access token writes as
// the person, an app's as its app (decision 0270). No request names one, so
// none can claim the substrate's own writing hands, which are decided at
// write time and could forge a credential ref, a connected account's address
// or a shipped kind declaration (the authority chokepoint, engine/seed.go).
// Which client made a write is on its token record.
// On a refusal ctx is nil and status, code and msg are the answer; cause is
// set on a 503 alone, the service's error behind it, which is logged and never
// shown.
func (h *handler) authenticate(r *http.Request) (ctx context.Context, status int, code, msg string, cause error) {
	secret := bearerSecret(r)
	if secret == "" {
		return nil, http.StatusUnauthorized, codeAuth, "missing bearer token", nil
	}
	ds, tok, err := h.svc.Authenticate(r.Context(), secret)
	if err != nil {
		// A genuine bad/expired/unknown token is 401. A well-formed token whose
		// repository could not be OPENED is not an auth failure — it
		// must not masquerade as "invalid token", or a bricked open reads as a
		// bad credential. Report it as a service condition instead.
		if errors.Is(err, substrate.ErrAuth) {
			return nil, http.StatusUnauthorized, codeAuth, "invalid token", nil
		}
		return nil, http.StatusServiceUnavailable, codeUnavailable, "repository temporarily unavailable", err
	}
	// Expiry is server-enforced. The engine's Authenticate is the
	// authoritative gate — it never hands back a dataset for an expired token —
	// but the check is repeated here so the guarantee does not rest on one
	// implementation: a token past its expiry is an auth failure, not a
	// forbidden, since the credential itself is spent.
	if exp := tok.ExpiresAt; exp != nil && h.now().After(*exp) {
		return nil, http.StatusUnauthorized, codeAuth, "token expired", nil
	}
	ctx = r.Context()
	// Every token acts for its person, and is held to that person's roles:
	// an app's, an assistant's and the person's own.
	whole := ds
	ds, held, opened, err := personView(ctx, ds, tok)
	if err != nil {
		return nil, http.StatusServiceUnavailable, codeUnavailable, "repository temporarily unavailable", err
	}
	// A stream outlives this check: the lease reads the token again before
	// the stream writes on, and whenever the repository's changelog moves, so
	// a revoked or expired token stops every stream it opened, a full
	// token's, an assistant's and an app's alike. It reads the person's roles
	// too: the stream reads through the view they built, so a role changed
	// or taken away ends it, and the client opens it again on the roles held
	// now.
	tokenID := tok.ID
	lease := &tokenLease{expires: tok.ExpiresAt, now: h.now, checked: h.now(), recheck: func(ctx context.Context) error {
		_, again, err := h.svc.Authenticate(ctx, secret)
		if err != nil {
			return err
		}
		if again.ID != tokenID {
			return substrate.ErrAuth
		}
		now, err := whole.PersonRoles(substrate.WithIdentity(ctx, again.Identity))
		if err != nil {
			return err
		}
		if p, err := rolesPrint(now); err != nil || p != opened {
			return errRolesChanged
		}
		return nil
	}}
	ctx = withTokenLease(ctx, lease)
	if held {
		ctx = context.WithValue(ctx, ctxKeyHeld, true)
	}
	if scope.IsMCP(tok.Scopes) {
		// An assistant's token (decision 0230): the owner's tier, no scope
		// mark, at the MCP door alone, on the dataset that keeps the auth
		// material out of its writes. Its writes are its app's derived
		// actor whatever actor it names, as an app's are.
		if !onMCPDoor(ctx) {
			return nil, http.StatusForbidden, codeForbidden,
				"an assistant's token works at " + h.issuerOf(r) + mcpPath + " alone", nil
		}
		actor := appActorOf(tok)
		if actor != substrate.ActorPerson {
			ctx = substrate.WithClientID(ctx, tok.ClientID)
		}
		// The owner's hand at the gates a person's door alone passes
		// (installing, importing, the owner's label keys), under the
		// assistant's own actor.
		ctx = substrate.WithAssistant(ctx)
		return withRequestAuth(ctx, newAssistantDataset(ds), tok, actor), 0, "", "", nil
	}
	if tok.Scoped() {
		// An app's token: the dataset it gets is held to its scopes, the
		// engine's owner-only gates see the mark, and its writes are its own
		// derived `app:<host>` actor whatever actor it names, since an actor
		// resolves a tier and a manager hand and an app picks neither. A
		// grant that does not parse fails closed: read as absent it would be
		// full access.
		grant, gerr := scope.NewGrant(tok.Scopes)
		if gerr != nil {
			return nil, http.StatusUnauthorized, codeAuth, "invalid token", nil
		}
		ctx = substrate.WithScopes(ctx, tok.Scopes)
		actor := appActorOf(tok)
		// The full client id beside the actor: the label and annotation
		// namespace the app keeps, which its host-only actor does not tell
		// apart from another app's on the same host.
		if actor != substrate.ActorPerson {
			ctx = substrate.WithClientID(ctx, tok.ClientID)
		}
		return withRequestAuth(ctx, newScopedDataset(ds, grant, actor), tok, actor), 0, "", "", nil
	}
	// A full-access token is the person's. No request names its actor: a
	// header an older client still sends is not read.
	return withRequestAuth(ctx, ds, tok, substrate.ActorPerson), 0, "", "", nil
}

// appActorOf is the actor an app's token writes under, from what its
// approval proved (decision 0209). A scoped token with no client id (none
// the authorization flow mints) writes as the person.
func appActorOf(tok substrate.TokenInfo) substrate.Actor {
	return appActorFor(tok.ClientID, tok.AppAuthority, tok.AppLocal)
}

// appActorFor is an app's actor (authorize.AppActorFor).
func appActorFor(clientID, authority string, local bool) substrate.Actor {
	return authorize.AppActorFor(clientID, authority, local)
}

// A route takes a token through one of three doors, which the route list
// names (decision 0294, access_test.go):
//   - requireAuth admits a token whose person holds `admin`, and refuses an
//     app's and a person held to their roles: every route behind it is an
//     admin's unless it is registered behind another, so a route added
//     later is an admin's by default (decision 0194);
//   - requireAuthAllowingApps admits every token, each held to its rules by
//     the scoped view the bearer check hands it: an app's to its scopes, a
//     person's to their roles;
//   - requirePerson admits a person's own token, held to their roles or
//     not, for what is theirs alone (their sign-ins and their identity),
//     and refuses an app's.
type door int

const (
	doorAdmin door = iota
	doorRules
	doorPerson
)

func (h *handler) requireAuth(next http.Handler) http.Handler {
	return h.bearer(next, doorAdmin)
}

func (h *handler) requireAuthAllowingApps(next http.Handler) http.Handler {
	return h.bearer(next, doorRules)
}

func (h *handler) requirePerson(next http.Handler) http.Handler {
	return h.bearer(next, doorPerson)
}

func (h *handler) bearer(next http.Handler, d door) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, status, code, msg, cause := h.authenticate(r)
		if ctx == nil {
			// The cause of a 503 is a database or an open failing, which the
			// client is not shown, so the log is the only place it surfaces.
			if status == http.StatusServiceUnavailable {
				writeUnavailableCause(w, r, cause, unavailablePayload(cause, msg))
				return
			}
			if status == http.StatusUnauthorized {
				// RFC 9728 §5.1: a client that holds no token, or a dead one,
				// learns where to get one from the refusal itself.
				meta := `resource_metadata="` + h.issuerOf(r) + prMetadataPath + `"`
				challenge := "Bearer " + meta
				if bearerSecret(r) != "" {
					challenge = `Bearer error="invalid_token", ` + meta
				}
				w.Header().Set("WWW-Authenticate", challenge)
			}
			writeError(w, status, code, msg)
			return
		}
		if _, assistant := DatasetFrom(ctx).(*assistantDataset); assistant && !mcpRouteAllowed(r) {
			writeError(w, http.StatusForbidden, codeForbidden,
				"an assistant's token does not reach "+r.Method+" "+r.URL.Path)
			return
		}
		if _, scoped := substrate.ScopesFrom(ctx); scoped && d != doorRules {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
			writeError(w, http.StatusForbidden, codeForbidden,
				"an app's token does not reach "+r.Method+" "+r.URL.Path+": it is a person's alone")
			return
		}
		if heldToRoles(ctx) && d == doorAdmin {
			writeJSON(w, http.StatusForbidden, substrate.ErrorEnvelope{Error: substrate.ErrorPayload{
				Code: codeForbidden, Reason: substrate.ReasonNotPermitted,
				Message: r.Method + " " + r.URL.Path + " is for a person who holds the admin role",
			}})
			return
		}
		if h.paced(ctx, w, DatasetFrom(ctx).Repository().ID, ownTokenRequest(ctx, r)) {
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
