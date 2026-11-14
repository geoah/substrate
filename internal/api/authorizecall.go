package api

import (
	"context"
	"sort"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// What the approval page shows for a `call:` scope (decision 0219): the
// functions the pattern matches today, each with the reach its own
// permissions declare, and whether the request's read and write scopes cover
// it, since a call of a function they do not cover is refused.

// functionRow is one function row with its reach resolved, read for the page
// before the view is built, which reads nothing.
type functionRow struct {
	rec   *substrate.Record
	reach functionReach
}

// readFunctions is every live function row of the repository with its
// reach, sorted by id.
func readFunctions(ctx context.Context, ds substrate.Dataset) ([]functionRow, error) {
	var out []functionRow
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{kindFunction}}, First: agentListPage}
	for {
		page, err := ds.List(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, rec := range page.Records {
			if rec.Kind != kindFunction || rec.DeletedAt != nil {
				continue
			}
			reach, err := resolveReach(ctx, ds, rec)
			if err != nil {
				return nil, err
			}
			out = append(out, functionRow{rec: rec, reach: reach})
		}
		if page.Cursor == "" || len(page.Records) == 0 {
			break
		}
		q.After = page.Cursor
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rec.ID < out[j].rec.ID })
	return out, nil
}

// matchedFunctions lists the functions one resolved call scope matches
// today, each held against the request's other scopes, resolved: the grant
// a token minted from the whole request would carry. A request with no read
// or write scope covers nothing.
func matchedFunctions(s scope.Scope, fns []functionRow, requested []scope.Scope, registry map[string]substrate.KindInfo) []substrate.AuthorizationCallFunction {
	out := []substrate.AuthorizationCallFunction{}
	g, err := scope.NewGrant([]string{s.String()})
	if err != nil {
		return out
	}
	var held []string
	for _, r := range requested {
		if r.Verb == scope.Read || r.Verb == scope.Write {
			held = append(held, r.String())
		}
	}
	covering, coverErr := scope.NewGrant(held)
	for _, f := range fns {
		if !g.Calls(f.rec.ID) {
			continue
		}
		description, _ := f.rec.Properties["description"].(string)
		row := substrate.AuthorizationCallFunction{
			Identity: f.rec.ID, Description: description,
			Reads: emptyNotNil(f.reach.reads), Writes: emptyNotNil(f.reach.writes),
		}
		if coverErr == nil {
			row.Uncovered = uncoveredReach(appReach{covering}, f.reach, registry)
		} else {
			row.Uncovered = uncoveredReach(appReach{}, f.reach, registry)
		}
		row.Covered = len(row.Uncovered) == 0
		out = append(out, row)
	}
	return out
}

// emptyNotNil is a list that serializes `[]` rather than `null`.
func emptyNotNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
