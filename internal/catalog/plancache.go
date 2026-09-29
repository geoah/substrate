package catalog

import (
	"context"
	"reflect"
	"slices"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// planCache holds the upgrade previews a catalog read reuses (issue #445).
// The listing previews every installed entry on every read, and a preview
// stages the closure against the stored vocabulary and counts the live rows
// its guards and conversions touch, so without it a repository holding eight
// stamped samples stages eight closures per GET.
//
// A preview is a function of three things: the shipped closure (fixed for this
// catalog), the dataset instance that counts it (its service's conversion
// ceiling, its registry) and the repository's committed state. The key holds
// all three: the entry id, the dataset instance, and the changelog head with
// its history generation. The head is exact for the state because every
// declaration, stamp, digest and live row a preview reads is written through
// the changelog, and the fold commits in the same transaction as the entry.
// A vocabulary key alone would not be: the blockers, the conversion counts and
// a lossy plan's hash read live records, and a stale hash is one the door
// refuses as a conflict while the next preview served the same stale hash.
//
// The head is read BEFORE the plan is counted. Everything the plan reads after
// it is at least as new as the head: the tables by commit order, and the
// registry because a vocabulary write holds the dataset's lock from before its
// commit until the registry is published (engine commitAndPublish). A write
// that lands while the plan is counted moves the head past the key, so a plan
// that may have seen part of it is never served again.
//
// Bounds: one slot per repository, holding at most one plan per catalog
// entry, all at one head. A later head, another generation or another
// dataset instance replaces the slot, which is how a closed dataset's plans
// are dropped: its successor's first read finds a slot that is not its own.
type planCache struct {
	mu    sync.Mutex
	repos map[string]*repositoryPlans
}

// repositoryPlans is one repository's previews, every one counted over ds at
// head and keyed by catalog entry id.
type repositoryPlans struct {
	ds    substrate.Dataset
	head  substrate.ChangelogHead
	plans map[string]substrate.BundleUpgrade
}

// plan answers entry id's preview in ds: the cached one while the head is the
// one it was counted at, else compute's, cached for the next read. An error is
// never cached, so a failed read is retried by the next one.
func (pc *planCache) plan(ctx context.Context, ds substrate.Dataset, id string, compute func() (*substrate.BundleUpgrade, error)) (*substrate.BundleUpgrade, error) {
	// The slot compares dataset instances, and comparing an interface whose
	// dynamic value is not comparable panics, so such a dataset plans afresh.
	if !reflect.ValueOf(ds).Comparable() {
		return compute()
	}
	head, err := ds.Head(ctx)
	if err != nil {
		return nil, err
	}
	repository := ds.Repository().ID
	if p, ok := pc.get(ds, repository, head, id); ok {
		return &p, nil
	}
	p, err := compute()
	if err != nil || p == nil {
		return p, err
	}
	pc.put(ds, repository, head, id, *p)
	return p, nil
}

func (pc *planCache) get(ds substrate.Dataset, repository string, head substrate.ChangelogHead, id string) (substrate.BundleUpgrade, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	slot := pc.repos[repository]
	if slot == nil || slot.ds != ds || slot.head != head {
		return substrate.BundleUpgrade{}, false
	}
	p, ok := slot.plans[id]
	if !ok {
		return substrate.BundleUpgrade{}, false
	}
	return clonePlan(p), true
}

func (pc *planCache) put(ds substrate.Dataset, repository string, head substrate.ChangelogHead, id string, p substrate.BundleUpgrade) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	slot := pc.repos[repository]
	switch {
	case slot == nil || slot.ds != ds || slot.head.Generation != head.Generation:
		slot = &repositoryPlans{ds: ds, head: head, plans: map[string]substrate.BundleUpgrade{}}
		if pc.repos == nil {
			pc.repos = map[string]*repositoryPlans{}
		}
		pc.repos[repository] = slot
	case head.Seq > slot.head.Seq:
		// The plans counted at the older head can never be served again.
		slot.head = head
		slot.plans = map[string]substrate.BundleUpgrade{}
	case head.Seq < slot.head.Seq:
		// Counted before a write this slot has already seen: stale on arrival.
		return
	}
	slot.plans[id] = clonePlan(p)
}

// clonePlan copies a plan's slices, so a caller that edits the preview it was
// handed never edits the cached one or races another reader of it.
func clonePlan(p substrate.BundleUpgrade) substrate.BundleUpgrade {
	p.Changes = slices.Clone(p.Changes)
	p.Blockers = slices.Clone(p.Blockers)
	p.Steps = slices.Clone(p.Steps)
	return p
}
