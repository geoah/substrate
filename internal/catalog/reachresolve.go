package catalog

import (
	"context"
	"fmt"
	"slices"

	"github.com/geoah/substrate/internal/reach"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func mergeReach(out []Reach, r Reach) []Reach {
	i := slices.IndexFunc(out, func(e Reach) bool { return e.Pattern == r.Pattern && e.Write == r.Write })
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

type candidateReach struct {
	reach.Reader
	rows map[string]*substrate.Record
}

func (c candidateReach) Get(ctx context.Context, kind, id string) (*substrate.Record, error) {
	if r := c.rows[vocabulary.RecordPath(kind, id)]; r != nil {
		return r, nil
	}
	return c.Reader.Get(ctx, kind, id)
}

func (c candidateReach) List(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	page, err := c.Reader.List(ctx, q)
	if err != nil {
		return nil, err
	}
	out := *page
	out.Records = nil
	for _, r := range page.Records {
		if c.rows[vocabulary.RecordPath(r.Kind, r.ID)] == nil {
			out.Records = append(out.Records, r)
		}
	}
	if q.After == "" {
		for _, r := range c.rows {
			if slices.Contains(q.Filter.Kinds, r.Kind) {
				out.Records = append(out.Records, r)
			}
		}
	}
	slices.SortFunc(out.Records, func(a, b *substrate.Record) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return &out, nil
}

// ResolvedReach resolves callees against all candidates, then the repository.
// A missing callee refuses the plan rather than approving incomplete reach.
func (b *Bundle) ResolvedReach(ctx context.Context, ds reach.Reader, chain []*Bundle) ([]Reach, error) {
	reader := candidateReach{Reader: ds, rows: map[string]*substrate.Record{}}
	for _, candidate := range chain {
		for _, d := range candidate.vocabularyDocs {
			kind := mstr(d, "kind")
			id := docID(d)
			reader.rows[vocabulary.RecordPath(kind, id)] = &substrate.Record{Kind: kind, ID: id, Properties: mmap(d, "data")}
		}
	}
	var out []Reach
	for _, r := range b.Reach() {
		r.By = slices.Clone(r.By)
		out = append(out, r)
	}
	roots := []struct{ kind, id string }{}
	for _, f := range b.functions {
		roots = append(roots, struct{ kind, id string }{vocabulary.CoreKind(vocabulary.DocFunction), f.ID})
	}
	for _, a := range b.agents {
		roots = append(roots, struct{ kind, id string }{vocabulary.CoreKind(vocabulary.DocAgent), a.ID})
	}
	for _, t := range b.triggers {
		kind, id, ok := vocabulary.SplitRecordPath(t.Callable)
		if !ok {
			return nil, fmt.Errorf("%w: trigger %s names invalid callable %s", substrate.ErrValidation, t.ID, t.Callable)
		}
		roots = append(roots, struct{ kind, id string }{kind, id})
	}
	for _, root := range roots {
		rec, err := reader.Get(ctx, root.kind, root.id)
		if err != nil {
			return nil, err
		}
		if rec == nil || rec.DeletedAt != nil {
			return nil, fmt.Errorf("%w: missing callable %s", substrate.ErrValidation, root.id)
		}
		reach, err := reach.Callable(ctx, reader, rec)
		if err != nil {
			return nil, err
		}
		for _, r := range reach {
			out = mergeReach(out, r)
		}
	}
	return out, nil
}
