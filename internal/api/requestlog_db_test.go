package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// requestLogPreviewFault is a repository whose upgrade preview fails with
// err: a database fault, or (with leave set) the client going away while the
// preview runs, which the engine cannot be made to produce on demand. The
// rest of the listing is the engine's own reads.
type requestLogPreviewFault struct {
	substrate.Dataset
	err   error
	leave context.CancelFunc
}

func (d requestLogPreviewFault) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	if d.leave != nil {
		d.leave()
	}
	return substrate.BundleUpgrade{}, d.err
}

// A catalog listing the client abandons cancels its upgrade previews; that is
// not an ERROR line either, while a preview that fails on its own is one
// naming the route. The repository holds a published catalogue, the kind of
// entry the listing previews an upgrade for.
//
// Serial: it swaps the process's default logger to read what the server
// logged.
func TestCatalogPreviewCanceledByTheClientIsNotAnError(t *testing.T) {
	for _, clientGone := range []bool{false, true} {
		t.Run(fmt.Sprintf("client_gone=%v", clientGone), func(t *testing.T) {
			cat, err := catalog.Load(catalog.Shipped()...)
			if err != nil {
				t.Fatalf("load catalog: %v", err)
			}
			env := newDBEnv(t,
				withEngine(engine.WithLogger(slog.New(slog.DiscardHandler))),
				withConfig(func(c *Config) { c.Catalog = cat }))
			if err := enginetest.InstallCatalogues(context.Background(), env.ds, "environment"); err != nil {
				t.Fatalf("install the environment catalogue: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fault := requestLogPreviewFault{err: errRequestLogBoom}
			if clientGone {
				// The client leaves while the preview runs, so the listing's
				// own reads before it are answered.
				fault = requestLogPreviewFault{err: fmt.Errorf("upgrade preview: %w", context.Canceled), leave: cancel}
			}
			env.wrap = func(ds substrate.Dataset) substrate.Dataset {
				return requestLogPreviewFault{Dataset: ds, err: fault.err, leave: fault.leave}
			}
			logs := captureLogs(t)

			serveWith(env.h, ctx, http.MethodGet, "/api/v1/catalog", env.token, "")

			errs := logs.at(slog.LevelError)
			if clientGone {
				if len(errs) != 0 {
					t.Fatalf("a preview the client canceled was logged at ERROR: %+v", errs)
				}
				reached := false
				for _, l := range logs.at(slog.LevelDebug) {
					reached = reached || l.msg == "catalog: upgrade preview canceled by the client"
				}
				if !reached {
					t.Fatalf("the canceled preview was never reached: DEBUG lines %+v", logs.at(slog.LevelDebug))
				}
				return
			}
			if len(errs) == 0 {
				t.Fatal("a failed preview logged no ERROR line")
			}
			for _, l := range errs {
				if l.msg != "catalog: upgrade preview failed" || l.attrs["route"] != "/api/v1/catalog" || l.attrs["method"] != http.MethodGet {
					t.Errorf("ERROR line = %q %v, want the preview failure naming GET /api/v1/catalog", l.msg, l.attrs)
				}
			}
		})
	}
}
