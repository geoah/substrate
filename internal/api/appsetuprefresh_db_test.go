package api

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/appsetup"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

// resolutions records every package the plan resolves, which for a package
// the binary does not ship is a read of its publisher's document.
type resolutions struct {
	appsetup.Catalog
	mu    sync.Mutex
	asked []string
}

func (r *resolutions) ResolveFor(ctx context.Context, id string, atLeast int64, ds substrate.Dataset) (*catalog.Bundle, error) {
	r.mu.Lock()
	r.asked = append(r.asked, id)
	r.mu.Unlock()
	return r.Catalog.ResolveFor(ctx, id, atLeast, ds)
}

func (r *resolutions) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.asked
	r.asked = nil
	return out
}

// A check reads a package's publisher only where the token's own scopes name
// the package. An app that names any package in a check must not make the
// server fetch its publisher's document, or refresh and upgrade it: the row
// it gets back is `unknown`, and the fetch is as far out of its reach.
func TestACheckReadsNoPublisherOfAPackageTheTokenCannotSee(t *testing.T) {
	t.Parallel()
	resolved := &resolutions{}
	e, refresher := publishingEnv(t, withSamples("people"), withConfig(func(c *Config) {
		resolved.Catalog = c.Packages
		c.Packages = resolved
	}))
	e.holdTraining(2, 0)
	body := checkRequest("read:"+personKind, `[{"type":"package","package":"`+dpTraining+`","version":3}]`)

	narrow := e.grant("read:"+personKind, []string{"read:" + personKind})
	resolved.take()
	if got := e.check(narrow, body); got.Setup[0].Action != substrate.SetupUnknown {
		t.Fatalf("a token that cannot see %s got its row: %+v", dpTraining, got.Setup[0])
	}
	if asked := refresher.list(); len(asked) != 0 {
		t.Fatalf("a token that cannot see %s refreshed it: %v", dpTraining, asked)
	}
	if asked := resolved.take(); slices.Contains(asked, dpTraining) {
		t.Fatalf("a token that cannot see %s had its publisher read: %v", dpTraining, asked)
	}

	// A token whose scopes name a kind of the package refreshes it and reads
	// its publisher, as the owner's does.
	reader := e.grant("read:"+dpSession, []string{"read:" + dpSession})
	resolved.take()
	e.check(reader, body)
	if asked := refresher.list(); len(asked) != 1 || asked[0] != e.db.repository+" "+dpTraining {
		t.Fatalf("a token that reads %s refreshed %v, want the package once", dpSession, asked)
	}
	if asked := resolved.take(); !slices.Contains(asked, dpTraining) {
		t.Fatalf("a token that reads %s did not have its publisher read: %v", dpSession, asked)
	}
}
