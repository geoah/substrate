package api

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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

// functionReach is one function's resolved reach: the kinds, or globs over
// kinds, it reads and writes, sorted and unique.
type functionReach struct {
	reads, writes []string
}

// recordReader reads one record by kind and id, as a dataset's Get does.
type recordReader func(ctx context.Context, kind, id string) (*substrate.Record, error)

// resolveReach is the reach of the function row fn, read through get. A
// callee that is missing or deleted adds nothing: the engine refuses the
// sub-call at run time, so there is nothing it could reach.
func resolveReach(ctx context.Context, get recordReader, fn *substrate.Record) (functionReach, error) {
	var out functionReach
	seen := map[string]bool{vocabulary.RecordPath(kindFunction, fn.ID): true}
	if err := addReach(ctx, get, fn, kindFunction, seen, &out); err != nil {
		return functionReach{}, err
	}
	slices.Sort(out.reads)
	slices.Sort(out.writes)
	out.reads = slices.Compact(out.reads)
	out.writes = slices.Compact(out.writes)
	return out, nil
}

// addReach adds rec's own grants to out, then those of what it may call. A
// function names functions and agents; an agent's tools are held to its own
// grants, so an agent adds its reads and writes alone.
func addReach(ctx context.Context, get recordReader, rec *substrate.Record, kind string, seen map[string]bool, out *functionReach) error {
	perms, _ := rec.Properties["permissions"].(map[string]any)
	readsBlock, _ := perms["reads"].(map[string]any)
	out.reads = append(out.reads, declaredIdentities(readsBlock["kinds"], vocabulary.CoreKind(vocabulary.DocKind))...)
	out.writes = append(out.writes, declaredIdentities(perms["writes"], vocabulary.CoreKind(vocabulary.DocKind))...)
	if kind != kindFunction {
		return nil
	}
	for _, callee := range []struct{ key, kind string }{{"call", kindFunction}, {"agents", kindAgent}} {
		for _, id := range declaredIdentities(perms[callee.key], callee.kind) {
			path := vocabulary.RecordPath(callee.kind, id)
			if seen[path] {
				continue
			}
			seen[path] = true
			next, err := get(ctx, callee.kind, id)
			if errors.Is(err, substrate.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if next == nil || next.DeletedAt != nil {
				continue
			}
			if err := addReach(ctx, get, next, callee.kind, seen, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// declaredIdentities reads a grant list off a declaration row as identities:
// each entry a string or a stored `{ref: …}`, and a reference path under
// pin (`<pin>/<identity>`) cut back to the identity it names.
func declaredIdentities(v any, pin string) []string {
	out := kindList(v)
	for i, id := range out {
		out[i] = strings.TrimPrefix(id, pin+"/")
	}
	return out
}

// uncoveredReach names what the grant's read and write scopes do not cover
// of a reach, `read <kind>` and `write <kind>`, in the reach's order. A kind
// the registry holds is held to its declared purpose, which a glob never
// reaches when internal; one it does not hold is held by pattern alone.
func uncoveredReach(g scope.Grant, reach functionReach, registry map[string]substrate.KindInfo) []string {
	var out []string
	internal := func(kind string) bool {
		k, ok := registry[kind]
		return ok && internalKind(k)
	}
	for _, k := range reach.reads {
		if !g.ReachCovered(scope.Read, k, internal(k)) {
			out = append(out, "read "+k)
		}
	}
	for _, k := range reach.writes {
		if !g.ReachCovered(scope.Write, k, internal(k)) {
			out = append(out, "write "+k)
		}
	}
	return out
}
