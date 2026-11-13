package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// regressBundle is a bundle the repository holds under its own authority: a
// `client` input resolving one of its config records, and an item kind that
// carries data for a purge to tombstone.
type regressBundle struct {
	id     string
	config string
	item   string
}

func regressInstallBundle(t *testing.T, env *dbEnv) regressBundle {
	t.Helper()
	pkg := env.repository + "/widgets"
	b := regressBundle{id: pkg, config: pkg + "/widgetconfig", item: pkg + "/widget"}
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "the widgets bundle",
			"inputs":      map[string]any{"client": map[string]any{"kind": b.config}},
			"installs":    []any{b.config, b.item},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "widgetconfig"}, map[string]any{
			"purpose":    "internal",
			"properties": map[string]any{"endpoint": map[string]any{"type": "string"}},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}
	if _, err := env.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, docs); err != nil {
		t.Fatalf("install the widgets bundle: %v", err)
	}
	return b
}

// path is the bundle RECORD; its lifecycle is record state, so
// enable/disable/uninstall/purge are a PATCH of it (decision 0033).
func (b regressBundle) path() string {
	return "/api/v1/" + corePackage + "/bundle/" + url.PathEscape(b.id)
}

// putConfig creates one config record the way an owner does (the input
// kind's records are owner-managed), through the API.
func (b regressBundle) putConfig(t *testing.T, env *dbEnv) string {
	t.Helper()
	rec := env.do(t, http.MethodPost, recordsPath, env.token, map[string]any{
		"kind": b.config, "properties": map[string]any{"endpoint": "https://widgets.example.com"},
	})
	wantStatus(t, rec, http.StatusCreated)
	return decodeJSON[substrate.Record](t, rec).ID
}

func (b regressBundle) status(t *testing.T, env *dbEnv) substrate.BundleStatus {
	t.Helper()
	st, err := env.ds.BundleStatus(context.Background(), b.id)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	return st
}

// TestBundleBindValidatesAndAnswersStatus drives the bind endpoint: a bind
// reaches the engine with its input and record and answers the refreshed
// status; an empty record is the unbind spelling; a missing input is a 400
// that changes nothing.
func TestBundleBindValidatesAndAnswersStatus(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	b := regressInstallBundle(t, env)
	cfg := b.putConfig(t, env)

	rec := env.do(t, http.MethodPost, b.path()+"/bind", env.token, map[string]any{"input": "client", "record": cfg})
	wantStatus(t, rec, http.StatusOK)
	var st substrate.BundleStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || !st.Installed {
		t.Fatalf("bind answers the refreshed status: %v %s", err, rec.Body.String())
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Record != cfg || st.Inputs[0].Via != substrate.InputViaBound {
		t.Fatalf("bind answered inputs %+v, want client bound to %s", st.Inputs, cfg)
	}

	rec = env.do(t, http.MethodPost, b.path()+"/bind", env.token, map[string]any{"record": cfg})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if got := b.status(t, env).Inputs; len(got) != 1 || got[0].Via != substrate.InputViaBound {
		t.Fatalf("a refused bind changed the binding: %+v", got)
	}

	rec = env.do(t, http.MethodPost, b.path()+"/bind", env.token, map[string]any{"input": "client", "record": ""})
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.BundleStatus](t, rec).Inputs; len(got) != 1 || got[0].Via == substrate.InputViaBound {
		t.Fatalf("unbind answered inputs %+v, want the client no longer bound", got)
	}
}

// TestBundleUninstallAcksTombstone pins codex regress #4: uninstall deletes the
// bundle row, so reloading its status afterward always fails — the handler must
// NOT reload it. A successful uninstall answers 200 {"uninstalled": true}.
// Uninstall is a transition of the bundle record's `uninstalled` state, a
// PATCH of it.
func TestBundleUninstallAcksTombstone(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	b := regressInstallBundle(t, env)

	rec := env.do(t, http.MethodPatch, b.path(), env.token,
		map[string]any{"properties": map[string]any{"uninstalled": true}})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[map[string]any](t, rec)
	if out["uninstalled"] != true {
		t.Fatalf("uninstall body = %v, want {\"uninstalled\": true}", out)
	}
	// The verb ran: the bundle's kinds are gone, and a status read of it is
	// the failure the handler did not make.
	if _, err := env.ds.KindByRef(context.Background(), b.item); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the uninstalled bundle's kind still resolves: %v", err)
	}
	if st, err := env.ds.BundleStatus(context.Background(), b.id); err == nil && st.Installed {
		t.Fatalf("the bundle still reads installed: %+v", st)
	}
}

// TestBundleLifecycleIsRecordState drives the other three transitions as a
// PATCH of the bundle record: disable and enable answer the refreshed status,
// purge answers the tombstoned count, and a PATCH that names no lifecycle state
// is a 400 before any op runs (decision 0033).
func TestBundleLifecycleIsRecordState(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	b := regressInstallBundle(t, env)
	b.putConfig(t, env)
	env.put(t, b.item, "w1", map[string]any{"name": "one"})
	env.put(t, b.item, "w2", map[string]any{"name": "two"})

	// A PATCH that carries no lifecycle state names the accepted ones, and
	// the bundle stays as it was.
	rec := env.do(t, http.MethodPatch, b.path(), env.token,
		map[string]any{"properties": map[string]any{"authority": "x"}})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if st := b.status(t, env); !st.Installed || !st.Enabled {
		t.Fatalf("a refused PATCH moved the bundle: %+v", st)
	}

	rec = env.do(t, http.MethodPatch, b.path(), env.token,
		map[string]any{"properties": map[string]any{"disabled": true}})
	wantStatus(t, rec, http.StatusOK)
	if st := decodeJSON[substrate.BundleStatus](t, rec); !st.Installed || st.Enabled {
		t.Fatalf("disable answers the refreshed status: %s", rec.Body.String())
	}

	rec = env.do(t, http.MethodPatch, b.path(), env.token,
		map[string]any{"properties": map[string]any{"disabled": false}})
	wantStatus(t, rec, http.StatusOK)
	if st := decodeJSON[substrate.BundleStatus](t, rec); !st.Enabled {
		t.Fatalf("enable answers the refreshed status: %s", rec.Body.String())
	}

	// A purge runs on a disabled bundle (the engine's guard), and answers
	// what it tombstoned: the config and the two widgets.
	rec = env.do(t, http.MethodPatch, b.path(), env.token,
		map[string]any{"properties": map[string]any{"disabled": true}})
	wantStatus(t, rec, http.StatusOK)
	rec = env.do(t, http.MethodPatch, b.path(), env.token,
		map[string]any{"properties": map[string]any{"purging": true}})
	wantStatus(t, rec, http.StatusOK)
	if out := decodeJSON[map[string]any](t, rec); out["purged"] != float64(3) {
		t.Fatalf("purge body = %v, want {\"purged\": 3}", out)
	}
	if got := env.get(t, b.item, "w1"); got == nil || got.DeletedAt == nil {
		t.Fatalf("the purge left a widget live: %+v", got)
	}
}
