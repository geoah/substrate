package reach

import (
	"context"
	"fmt"
	"slices"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Entry is a kind or glob touched by code, with the declarations that touch it.
type Entry struct {
	Pattern string
	Write   bool
	By      []By
}

// By identifies a declaration by its document kind and full identity.
type By struct {
	Type string
	ID   string
}

// Reader resolves declarations in the candidate closure before the installed ones.
type Reader interface {
	Get(context.Context, string, string) (*substrate.Record, error)
	List(context.Context, substrate.Query) (*substrate.Page, error)
}

// Callable includes every callee's grants, even across package boundaries.
// Host tools run under the agent's grants. Function tools do not inherit its reads.
func Callable(ctx context.Context, ds Reader, rec *substrate.Record) ([]Entry, error) {
	var out []Entry
	seen := map[string]bool{}
	var visit func(*substrate.Record) error
	follow := func(kind, id string) error {
		if seen[vocabulary.RecordPath(kind, id)] {
			return nil
		}
		next, err := ds.Get(ctx, kind, id)
		if err != nil {
			return fmt.Errorf("resolve reach of %s: %w", vocabulary.RecordPath(kind, id), err)
		}
		if next == nil || next.DeletedAt != nil {
			return fmt.Errorf("%w: reach names missing callable %s", substrate.ErrValidation, id)
		}
		return visit(next)
	}
	visit = func(rec *substrate.Record) error {
		path := vocabulary.RecordPath(rec.Kind, rec.ID)
		if seen[path] {
			return nil
		}
		seen[path] = true
		data := rec.Properties
		perms := object(data, "permissions")
		by := By{Type: vocabulary.KindName(rec.Kind), ID: rec.ID}
		writes := kindGrants(rec.ID, list(perms, "writes"))
		if rec.Kind == vocabulary.CoreKind(vocabulary.DocAgent) {
			writes = AgentWrites(data, writes)
		}
		for _, k := range kindGrants(rec.ID, list(object(perms, "reads"), "kinds")) {
			out = mergeReach(out, Entry{Pattern: k, By: []By{by}})
		}
		for _, k := range writes {
			out = mergeReach(out, Entry{Pattern: k, Write: true, By: []By{by}})
		}
		if rec.Kind == vocabulary.CoreKind(vocabulary.DocFunction) {
			for _, edge := range []struct{ key, kind string }{{"call", vocabulary.CoreKind(vocabulary.DocFunction)}, {"agents", vocabulary.CoreKind(vocabulary.DocAgent)}} {
				for _, id := range vocabulary.ReferentIDs(list(perms, edge.key), edge.kind) {
					if err := follow(edge.kind, id); err != nil {
						return err
					}
				}
			}
			return nil
		}
		for _, entry := range list(data, "tools") {
			tool, _ := entry.(map[string]any)
			id := vocabulary.ReferentID(tool["function"], vocabulary.CoreKind(vocabulary.DocFunction))
			// These are the host arms agentLoop.dispatch implements under the agent's grants.
			switch id {
			case vocabulary.HostFunctionQuery, vocabulary.HostFunctionAsk, vocabulary.HostFunctionWrite, vocabulary.HostFunctionPropose, vocabulary.HostFunctionWait, vocabulary.HostFunctionVocabulary, vocabulary.HostFunctionEdit:
				continue
			}
			if err := follow(vocabulary.CoreKind(vocabulary.DocFunction), id); err != nil {
				return err
			}
		}
		for _, id := range vocabulary.ReferentIDs(list(data, "subagents"), vocabulary.CoreKind(vocabulary.DocAgent)) {
			if err := follow(vocabulary.CoreKind(vocabulary.DocAgent), id); err != nil {
				return err
			}
		}
		if patterns := list(data, "toolsFrom"); len(patterns) > 0 {
			q := substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.CoreKind(vocabulary.DocFunction)}}, First: 500}
			for {
				page, err := ds.List(ctx, q)
				if err != nil {
					return err
				}
				for _, fn := range page.Records {
					if fn.DeletedAt != nil || len(object(fn.Properties, "offer")) == 0 {
						continue
					}
					for _, entry := range patterns {
						p, _ := entry.(map[string]any)
						if vocabulary.MatchTypeGlob(str(p, "functions"), fn.ID) {
							if err := visit(fn); err != nil {
								return err
							}
							break
						}
					}
				}
				if page.Cursor == "" {
					break
				}
				q.After = page.Cursor
			}
		}
		return nil
	}
	if err := visit(rec); err != nil {
		return nil, err
	}
	return out, nil
}

func mergeReach(out []Entry, r Entry) []Entry {
	i := slices.IndexFunc(out, func(e Entry) bool { return e.Pattern == r.Pattern && e.Write == r.Write })
	if i < 0 {
		return append(out, r)
	}
	for _, by := range r.By {
		if !slices.Contains(out[i].By, by) {
			out[i].By = append(out[i].By, by)
		}
	}
	return out
}

// AgentWrites exempts the interaction grant required by ask; other writers keep it.
func AgentWrites(data map[string]any, writes []string) []string {
	for _, entry := range list(data, "tools") {
		tool, _ := entry.(map[string]any)
		if vocabulary.ReferentID(tool["function"], vocabulary.CoreKind(vocabulary.DocFunction)) == vocabulary.HostFunctionAsk {
			return slices.DeleteFunc(writes, func(k string) bool { return k == vocabulary.KindLLMInteraction })
		}
	}
	return writes
}

func object(data map[string]any, key string) map[string]any {
	out, _ := data[key].(map[string]any)
	return out
}
func list(data map[string]any, key string) []any { out, _ := data[key].([]any); return out }
func str(data map[string]any, key string) string { out, _ := data[key].(string); return out }

func kindGrants(id string, values []any) []string {
	entries := vocabulary.ReferentIDs(values, vocabulary.CoreKind(vocabulary.DocKind))
	authority, pkg := vocabulary.SplitPackageRef(vocabulary.KindPackage(id))
	var out []string
	for _, k := range entries {
		if k == "" {
			continue
		}
		if !vocabulary.Qualified(k) && !vocabulary.IsTypeGlob(k) {
			k = vocabulary.KindRef(authority, pkg, k)
		}
		out = append(out, k)
	}
	return out
}
