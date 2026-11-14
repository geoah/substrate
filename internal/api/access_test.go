package api

import (
	"context"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/substrate"
)

// THE ROUTE LIST (decision 0294): every route the API serves is public,
// for an admin alone, or limited by rules. A served route that is not
// placed here fails TestEveryRouteIsPlacedInTheRouteList until it is, and
// the list is held to the doors the router puts each route behind:
//   - public takes no token, or authenticates on its own path (the MCP door,
//     the webhook door, the sign-in doors);
//   - admin is requireAuth: a token whose person holds `admin`;
//   - rules is requireAuthAllowingApps or requirePerson: every token held to
//     its rules, an app's to its scopes and a person's to their roles, by
//     the scoped view.
type access string

const (
	accessPublic access = "public"
	accessAdmin  access = "admin"
	accessRules  access = "rules"
)

var routeList = map[string]access{
	"GET /.well-known/oauth-authorization-server":                            accessPublic,
	"GET /.well-known/oauth-protected-resource":                              accessPublic,
	"GET /.well-known/oauth-protected-resource/mcp":                          accessPublic,
	"GET /.well-known/substrate/server.json":                                 accessPublic,
	"POST /api/v1/authorizations/check":                                      accessRules,
	"DELETE /api/v1/authorizations/{request}":                                accessAdmin,
	"GET /api/v1/authorizations/{request}":                                   accessAdmin,
	"POST /api/v1/authorizations/{request}":                                  accessAdmin,
	"PUT /api/v1/blobs":                                                      accessRules,
	"GET /api/v1/blobs/{digest}":                                             accessRules,
	"PUT /api/v1/blobs/{digest}":                                             accessRules,
	"GET /api/v1/catalog":                                                    accessAdmin,
	"GET /api/v1/catalog/{id}":                                               accessAdmin,
	"POST /api/v1/catalog/{id}/add":                                          accessAdmin,
	"GET /api/v1/changes":                                                    accessRules,
	"GET /api/v1/export":                                                     accessAdmin,
	"GET /api/v1/images":                                                     accessAdmin,
	"POST /api/v1/merge":                                                     accessAdmin,
	"GET /api/v1/oauth/callback":                                             accessPublic,
	"POST /api/v1/oauth/start":                                               accessAdmin,
	"GET /api/v1/operator/defaults":                                          accessAdmin,
	"POST /api/v1/packages/publish":                                          accessAdmin,
	"POST /api/v1/packages/release":                                          accessAdmin,
	"GET /api/v1/packages/releases":                                          accessAdmin,
	"GET /published/{repository}/{authority}":                                accessPublic,
	"GET /published/{repository}/{authority}/{package}/{version}":            accessPublic,
	"GET /api/v1/records":                                                    accessRules,
	"GET /api/v1/retention":                                                  accessAdmin,
	"POST /api/v1/records":                                                   accessRules,
	"POST /api/v1/repository/domain":                                         accessAdmin,
	"POST /api/v1/repository/domain/verify":                                  accessAdmin,
	"POST /api/v1/split":                                                     accessAdmin,
	"POST /api/v1/substrate.reamde.dev/applets/applet/{id}/act":              accessRules,
	"POST /api/v1/substrate.reamde.dev/applets/applet/{id}/preview":          accessRules,
	"POST /api/v1/substrate.reamde.dev/applets/applet/{id}/render":           accessRules,
	"POST /api/v1/substrate.reamde.dev/core/agent/{name}/call":               accessAdmin,
	"POST /api/v1/substrate.reamde.dev/core/agent/{name}/chat":               accessRules,
	"GET /api/v1/substrate.reamde.dev/core/bundle/status":                    accessAdmin,
	"POST /api/v1/substrate.reamde.dev/core/bundle/{id}/bind":                accessAdmin,
	"GET /api/v1/substrate.reamde.dev/core/bundle/{id}/status":               accessAdmin,
	"POST /api/v1/substrate.reamde.dev/core/function/{name}/call":            accessRules,
	"GET /api/v1/substrate.reamde.dev/core/trait/{id}/implementors":          accessRules,
	"GET /api/v1/substrate.reamde.dev/core/trigger/status":                   accessAdmin,
	"GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked":              accessRules,
	"DELETE /api/v1/substrate.reamde.dev/core/trigger/{id}/parked/{fid}":     accessAdmin,
	"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/parked/{fid}/retry": accessAdmin,
	"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/replay":             accessAdmin,
	"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/run":                accessAdmin,
	"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/wake":               accessAdmin,
	"GET /api/v1/substrate.reamde.dev/llm/thread/{id}/events":                accessRules,
	"POST /api/v1/substrate.reamde.dev/llm/thread/{id}/stop":                 accessRules,
	"GET /api/v1/sync/status":                                                accessAdmin,
	"GET /api/v1/units":                                                      accessRules,
	"GET /api/v1/units/convert":                                              accessRules,
	"POST /api/v1/vocabulary/apply":                                          accessRules,
	"POST /api/v1/vocabulary/plan":                                           accessAdmin,
	"POST /api/v1/vocabulary/takeover":                                       accessAdmin,
	"GET /api/v1/vocabulary/upgrade":                                         accessAdmin,
	"DELETE /api/v1/{a1}/{a2}/{a3}/{a4}":                                     accessRules,
	"GET /api/v1/{a1}/{a2}/{a3}/{a4}":                                        accessRules,
	"PATCH /api/v1/{a1}/{a2}/{a3}/{a4}":                                      accessRules,
	"PUT /api/v1/{a1}/{a2}/{a3}/{a4}":                                        accessRules,
	"POST /api/v1/{a1}/{a2}/{a3}/{a4}/pin":                                   accessRules,
	"POST /api/v1/{a1}/{a2}/{a3}/{a4}/release":                               accessRules,
	"GET /favicon.ico":                                                       accessPublic,
	"GET /healthz":                                                           accessPublic,
	"GET /identity":                                                          accessRules,
	"POST /login":                                                            accessPublic,
	"* /mcp":                                                                 accessPublic,
	"GET /metrics":                                                           accessPublic,
	"GET /oauth/authorize":                                                   accessPublic,
	"POST /oauth/revoke":                                                     accessPublic,
	"POST /oauth/token":                                                      accessPublic,
	"POST /password":                                                         accessPublic,
	"POST /register":                                                         accessPublic,
	"POST /register/enroll":                                                  accessPublic,
	"POST /repository/delete":                                                accessPublic,
	"POST /repository/rename":                                                accessPublic,
	"POST /signout/everywhere":                                               accessPublic,
	"* /site/*":                                                              accessPublic,
	"GET /tokens":                                                            accessRules,
	"POST /tokens":                                                           accessRules,
	"DELETE /tokens/{id}":                                                    accessRules,
	"POST /totp":                                                             accessPublic,
	"POST /totp/enroll":                                                      accessPublic,
	"POST /webhooks/{repository}/{trigger}":                                  accessPublic,
	"POST /webhooks/{repository}/{trigger}/{key}":                            accessPublic,
}

// routeAccessIn reads each route's door off the router: the bearer
// middleware it is behind, or none.
func routeAccessIn(t *testing.T, h http.Handler) map[string]access {
	t.Helper()
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("the handler is a %T, not a chi router", h)
	}
	var probe *handler
	admin := reflect.ValueOf(probe.requireAuth).Pointer()
	rules := []uintptr{reflect.ValueOf(probe.requireAuthAllowingApps).Pointer(), reflect.ValueOf(probe.requirePerson).Pointer()}
	byPattern := map[string]map[string]access{}
	err := chi.Walk(routes, func(method, pattern string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		a := accessPublic
		for _, mw := range mws {
			switch p := reflect.ValueOf(mw).Pointer(); {
			case p == admin:
				a = accessAdmin
			case slices.Contains(rules, p):
				a = accessRules
			}
		}
		if byPattern[pattern] == nil {
			byPattern[pattern] = map[string]access{}
		}
		byPattern[pattern][method] = a
		return nil
	})
	if err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	all := everyMethod(t)
	out := map[string]access{}
	for pattern, methods := range byPattern {
		served := make([]string, 0, len(methods))
		for m := range methods {
			served = append(served, m)
		}
		slices.Sort(served)
		if slices.Equal(served, all) {
			out["* "+pattern] = methods[served[0]]
			continue
		}
		for m, a := range methods {
			out[m+" "+pattern] = a
		}
	}
	return out
}

// Every served route is placed in the route list, under the door the router
// puts it behind, and every route the list places is served.
func TestEveryRouteIsPlacedInTheRouteList(t *testing.T) {
	t.Parallel()
	served := routeAccessIn(t, everyOptionalRoute(t))
	for route, a := range served {
		placed, ok := routeList[route]
		switch {
		case !ok:
			t.Errorf("%s is served and the route list does not place it: add it as %s, or put it behind another door (decision 0294)", route, a)
		case placed != a:
			t.Errorf("%s is placed %s and served %s", route, placed, a)
		}
	}
	for route := range routeList {
		if _, ok := served[route]; !ok {
			t.Errorf("the route list places %s, which nothing serves", route)
		}
	}
}

// heldStub is a repository whose token's person holds the editor role alone.
type heldStub struct{ stubDataset }

func (heldStub) PersonRoles(context.Context) ([]substrate.RoleRules, error) {
	return []substrate.RoleRules{{ID: "editor", Rules: []substrate.RoleRule{{Verbs: []string{"read", "write"}, Kinds: []string{"*"}}}}}, nil
}

// A person held to their roles is refused every route for an admin at its
// door, before its handler reads anything: nothing they ask reaches the
// engine but through the scoped view or a door that is theirs.
func TestAPersonHeldToTheirRolesIsRefusedEveryRouteForAnAdmin(t *testing.T) {
	t.Parallel()
	env := newStubEnv(t, heldStub{})
	for route, a := range routeList {
		if a != accessAdmin {
			continue
		}
		method, pattern, _ := strings.Cut(route, " ")
		path := routeParam.ReplaceAllString(pattern, "x")
		rec := env.do(t, method, path, stubToken, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s answered a person held to their roles %d, want 403 (body %s)", route, rec.Code, rec.Body.String())
			continue
		}
		if got := decodeJSON[substrate.ErrorEnvelope](t, rec); got.Error.Reason != substrate.ReasonNotPermitted {
			t.Errorf("%s refused a person held to their roles with reason %q, want not_permitted", route, got.Error.Reason)
		}
	}
}

// routeParam is a pattern's path parameter.
var routeParam = regexp.MustCompile(`\{[^}]+\}`)
