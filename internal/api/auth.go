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
	ctxKeyToken
	ctxKeyActor
	ctxKeyPeer
)

// actorHeader names the attribution the caller writes as; it must belong to
// the token's actor set.
const actorHeader = "X-Substrate-Actor"

func withRequestAuth(ctx context.Context, ds substrate.Dataset, tok substrate.TokenInfo, actor substrate.Actor) context.Context {
	ctx = context.WithValue(ctx, ctxKeyDataset, ds)
	ctx = context.WithValue(ctx, ctxKeyToken, tok)
	return context.WithValue(ctx, ctxKeyActor, actor)
}

// DatasetFrom returns the authenticated repository dataset, nil when absent.
func DatasetFrom(ctx context.Context) substrate.Dataset {
	ds, _ := ctx.Value(ctxKeyDataset).(substrate.Dataset)
	return ds
}

// TokenFrom returns the authenticated token metadata.
func TokenFrom(ctx context.Context) substrate.TokenInfo {
	t, _ := ctx.Value(ctxKeyToken).(substrate.TokenInfo)
	return t
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
// hands — `substrate`, `substrate.oauth`, and the `bundle:` / `connector:` /
// `function:` paths — decided by actor-name equality at write time, so a
// request allowed to claim one could forge a credential ref, a connected
// account's address, or a SHIPPED kind declaration (the authority chokepoint,
// engine/seed.go). This header is the only place a request names an actor, so
// it is the only place that has to say no.
func (h *handler) authenticate(r *http.Request) (context.Context, int, string, string) {
	secret := bearerSecret(r)
	if secret == "" {
		return nil, http.StatusUnauthorized, codeAuth, "missing bearer token"
	}
	ds, tok, err := h.svc.Authenticate(r.Context(), secret)
	if err != nil {
		// A genuine bad/expired/unknown token is 401. A well-formed token whose
		// repository could not be OPENED is not an auth failure — it
		// must not masquerade as "invalid token", or a bricked open reads as a
		// bad credential. Report it as a service condition instead.
		if errors.Is(err, substrate.ErrAuth) {
			return nil, http.StatusUnauthorized, codeAuth, "invalid token"
		}
		return nil, http.StatusServiceUnavailable, codeUnavailable, "repository temporarily unavailable"
	}
	// Expiry is server-enforced. The engine's Authenticate is the
	// authoritative gate — it never hands back a dataset for an expired token —
	// but the check is repeated here so the guarantee does not rest on one
	// implementation: a token past its expiry is an auth failure, not a
	// forbidden, since the credential itself is spent.
	if exp := tok.ExpiresAt; exp != nil && h.now().After(*exp) {
		return nil, http.StatusUnauthorized, codeAuth, "token expired"
	}
	actor := substrate.ActorAPI
	if want := strings.TrimSpace(r.Header.Get(actorHeader)); want != "" {
		if substrate.ReservedActor(substrate.Actor(want)) {
			return nil, http.StatusForbidden, codeForbidden,
				"actor " + want + " is reserved: the " + substrate.HostActorNamespace +
					" namespace and the " + substrate.BundleActorPrefix + ", " +
					substrate.ConnectorActorPrefix + " and " + substrate.FunctionActorPrefix +
					" paths are the substrate's own writing hands"
		}
		actor = substrate.Actor(want)
	}
	return withRequestAuth(r.Context(), ds, tok, actor), 0, "", ""
}

func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, status, code, msg := h.authenticate(r)
		if ctx == nil {
			// A 503 is transient: ruling A6 makes Retry-After mandatory on
			// every unavailable, the auth path included.
			if status == http.StatusServiceUnavailable {
				w.Header().Set("Retry-After", "1")
			}
			writeError(w, status, code, msg)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
