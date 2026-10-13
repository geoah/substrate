package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

type ctxKey int

const (
	ctxKeyDataset ctxKey = iota
	ctxKeyActor
	ctxKeyPeer
	ctxKeyRequestID
)

// actorHeader names the door the caller writes as. A token carries no actor
// set — it has full access to its repository — so the header is checked
// against one rule only: the substrate's own writing hands are refused
// (substrate.ReservedActor, authenticate below).
const actorHeader = "X-Substrate-Actor"

// withRequestAuth binds the request's authenticated write context. The actor
// is what the caller ASSERTED; the principal beside it is the token id this
// door RESOLVED, and the engine stamps that on every changelog entry the
// request appends, so attribution survives the caller's choice of actor name.
func withRequestAuth(ctx context.Context, ds substrate.Dataset, tok substrate.TokenInfo, actor substrate.Actor) context.Context {
	ctx = context.WithValue(ctx, ctxKeyDataset, ds)
	ctx = substrate.WithPrincipal(ctx, tok.ID)
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

// authenticate resolves the bearer token and the acting actor. A token has
// full access to its repository, so this is the whole check:
// the hash lookup finds the token record, and the repository holding it is
// the repository the request runs in.
//
// The actor is ATTRIBUTION, not authorization: the caller names which door it
// came through (`X-Substrate-Actor` — the console sends `console`, substratectl sends
// `substratectl`), and a request that names nothing is `api`, which is exactly what
// the substrate knows about it. The refusals are the substrate's own writing
// hands — `substrate`, `substrate.oauth`, the `bundle:` / `function:` /
// `agent:` paths and the retired `connector:` spelling of the first of them —
// decided by actor-name equality at write time, so a
// request allowed to claim one could forge a credential ref, a connected
// account's address, or a SHIPPED kind declaration (the authority chokepoint,
// engine/seed.go). This header is the only place a request names an actor, so
// it is the only place that has to say no.
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
	actor := substrate.ActorAPI
	if want := strings.TrimSpace(r.Header.Get(actorHeader)); want != "" {
		if substrate.ReservedActor(substrate.Actor(want)) {
			return nil, http.StatusForbidden, codeForbidden,
				"actor " + want + " is reserved: the " + substrate.HostActorNamespace +
					" namespace, the " + substrate.BundleActorPrefix + ", " +
					substrate.FunctionActorPrefix + " and " + substrate.AgentActorPrefix +
					" paths, and the retired connector: spelling, are the substrate's own writing hands", nil
		}
		actor = substrate.Actor(want)
	}
	return withRequestAuth(r.Context(), ds, tok, actor), 0, "", "", nil
}

func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, status, code, msg, cause := h.authenticate(r)
		if ctx == nil {
			// The cause of a 503 is a database or an open failing, which the
			// client is not shown, so the log is the only place it surfaces.
			if status == http.StatusServiceUnavailable {
				writeUnavailableCause(w, r, cause, msg)
				return
			}
			writeError(w, status, code, msg)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
