package api

import (
	"context"
	"slices"

	"github.com/geoah/substrate/internal/reach"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// What a function reaches when it runs (decision 0219): the kinds its own
// `permissions.reads.kinds` and `permissions.writes` name, and, since a call
// runs whatever runs under it, the reach of every function its
// `permissions.call` may invoke and every agent its `permissions.agents` may
// run. Each callee's effects are held to the callee's own grant, so the
// closure over those two lists is what a call of the function may touch,
// and it is what an app's `call:` scope is held to: the call is admitted
// only where the token's read and write scopes cover every entry.

// kindFunction is the declaration kind a function row is.
const kindFunction = corePackage + "/function"

// kindPackageRow is the declaration kind a package row is.
const kindPackageRow = corePackage + "/package"

// kindDeclarationRow is the kind whose rows are kind declarations.
const kindDeclarationRow = corePackage + "/kind"

// functionReach is one function's resolved reach: the kinds, or globs over
// kinds, it reads and writes, sorted and unique.
type functionReach struct {
	reads, writes []string
}

// resolveReach uses the same transitive grants as an install approval.
func resolveReach(ctx context.Context, ds reach.Reader, rec *substrate.Record) (functionReach, error) {
	reach, err := reach.Callable(ctx, ds, rec)
	if err != nil {
		return functionReach{}, err
	}
	var out functionReach
	for _, r := range reach {
		if r.Write {
			out.writes = append(out.writes, r.Pattern)
		} else {
			out.reads = append(out.reads, r.Pattern)
		}
	}
	slices.Sort(out.reads)
	slices.Sort(out.writes)
	return out, nil
}

// uncoveredReach names what the grant's read and write scopes do not cover
// of a reach, `read <kind>` and `write <kind>`, in the reach's order. A kind
// the registry holds is held to its declaration (scopeKind): its purpose,
// which a glob never reaches when internal, and the traits it binds. A
// missing kind needs an exact scope because its purpose is unknown.
func uncoveredReach(g scope.Grant, reach functionReach, registry map[string]substrate.KindInfo) []string {
	var out []string
	for _, k := range reach.reads {
		if !g.ReachCovered(scope.Read, k, scopeKind(k, registry), internalKinds(registry)...) {
			out = append(out, "read "+k)
		}
	}
	for _, k := range reach.writes {
		if !g.ReachCovered(scope.Write, k, scopeKind(k, registry), internalKinds(registry)...) {
			out = append(out, "write "+k)
		}
	}
	return out
}

// internalKinds supplies the exceptions scope globs omit but code globs reach.
func internalKinds(registry map[string]substrate.KindInfo) []string {
	var out []string
	for ref, k := range registry {
		if internalKind(k) {
			out = append(out, ref)
		}
	}
	return out
}
