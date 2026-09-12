package api

// Codex regression round 2 (server, API surface):
//   #1  the bundle lifecycle is gated behind the engine's exclusive fence
//   #4  a successful uninstall acks with a tombstone, never an error
//   #16 a malformed trailing closer {}} is bad_request

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// ---- bundle-lifecycle fake (#1 gate, #4 ack) --------------------------------

type bundleDataset struct {
	*fakeDataset
	pkg         string
	statusErr   error
	uninstalled bool
	// boundInput/boundRecord record the last bind call, for the handler test.
	boundInput  string
	boundRecord string
}

func (d *bundleDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return nil, nil
}

func (d *bundleDataset) BundleStatus(_ context.Context, id string) (substrate.BundleStatus, error) {
	if d.statusErr != nil {
		return substrate.BundleStatus{}, d.statusErr
	}
	authority, pkg, _ := strings.Cut(d.pkg, "/")
	return substrate.BundleStatus{
		ID: id, Name: pkg, Authority: authority, Package: pkg,
		Installed: true, Enabled: true,
	}, nil
}

func (d *bundleDataset) BundlePackage(_ context.Context, id string) (string, error) {
	// The lifecycle PATCH addresses the bundle by id; an empty one is the
	// generic-route param bug (a3, not {id}), so refuse it the way the engine
	// would rather than answering for a bundle nobody named.
	if id == "" {
		return "", fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	return d.pkg, nil
}
func (d *bundleDataset) DisableBundle(context.Context, string) error { return nil }
func (d *bundleDataset) BindBundleInput(_ context.Context, _, input, record string) error {
	d.boundInput, d.boundRecord = input, record
	return nil
}
func (d *bundleDataset) EnableBundle(context.Context, string) error { return nil }
func (d *bundleDataset) UninstallBundle(context.Context, string) error {
	d.uninstalled = true
	return nil
}
func (d *bundleDataset) PurgeBundle(context.Context, string) (int, error) { return 3, nil }

type bundleService struct {
	*fakeService
	ds *bundleDataset
}

func (s *bundleService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	_, info, err := s.fakeService.Authenticate(ctx, secret)
	if err != nil {
		return nil, info, err
	}
	return s.ds, info, nil
}

func newBundleEnv(t *testing.T) (*testEnv, *bundleDataset) {
	t.Helper()
	fs := newFakeService()
	bd := &bundleDataset{fakeDataset: fs.datasets[fakeRepository], pkg: "widgets.example.com/widgets"}
	svc := &bundleService{fakeService: fs, ds: bd}
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	return &testEnv{svc: fs, h: New(Config{Service: svc, Now: clock.now}), clock: clock}, bd
}

// bundlePath is the bundle RECORD; its lifecycle is record state, so
// enable/disable/uninstall/purge are a PATCH of it (decision 0033).
const bundlePath = "/api/v1/substrate.reamde.dev/core/bundle/widgets.example.com%2Fwidgets"

const bindPath = bundlePath + "/bind"

// TestBundleBindValidatesAndAnswersStatus drives the bind endpoint: a bind
// reaches the engine with its input and record and answers the refreshed
// status; an empty record is the unbind spelling; a missing input is a 400
// before the engine is touched.
func TestBundleBindValidatesAndAnswersStatus(t *testing.T) {
	env, bd := newBundleEnv(t)
	tok := env.svc.token(fakeRepository)

	rec := env.do(t, http.MethodPost, bindPath, tok, map[string]any{"input": "client", "record": "cfg-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("bind status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if bd.boundInput != "client" || bd.boundRecord != "cfg-1" {
		t.Fatalf("bind reached the engine as (%q, %q)", bd.boundInput, bd.boundRecord)
	}
	var st substrate.BundleStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || !st.Installed {
		t.Fatalf("bind answers the refreshed status: %v %s", err, rec.Body.String())
	}

	rec = env.do(t, http.MethodPost, bindPath, tok, map[string]any{"input": "client", "record": ""})
	if rec.Code != http.StatusOK || bd.boundRecord != "" {
		t.Fatalf("unbind: %d (record %q)", rec.Code, bd.boundRecord)
	}

	bd.boundInput = ""
	rec = env.do(t, http.MethodPost, bindPath, tok, map[string]any{"record": "cfg-1"})
	if rec.Code != http.StatusBadRequest || bd.boundInput != "" {
		t.Fatalf("empty input must 400 before the engine: %d (%q)", rec.Code, bd.boundInput)
	}
}

// TestBundleUninstallAcksTombstone pins codex regress #4: uninstall deletes the
// bundle row, so reloading its status afterward always fails — the handler must
// NOT reload it. Even with a status read that errors (the deleted row), a
// successful uninstall answers 200 {"uninstalled": true}. Uninstall is a
// transition of the bundle record's `uninstalled` state, a PATCH of it.
func TestBundleUninstallAcksTombstone(t *testing.T) {
	env, bd := newBundleEnv(t)
	bd.statusErr = substrate.ErrNotFound // the row is gone once uninstall ran
	tok := env.svc.token(fakeRepository)

	rec := env.do(t, http.MethodPatch, bundlePath, tok,
		map[string]any{"properties": map[string]any{"uninstalled": true}})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[map[string]any](t, rec)
	if out["uninstalled"] != true {
		t.Fatalf("uninstall body = %v, want {\"uninstalled\": true}", out)
	}
	if !bd.uninstalled {
		t.Fatal("uninstall verb never ran")
	}
}

// TestBundleLifecycleIsRecordState drives the other three transitions as a
// PATCH of the bundle record: disable and enable answer the refreshed status,
// purge answers the tombstoned count, and a PATCH that names no lifecycle state
// is a 400 before any op runs (decision 0033).
func TestBundleLifecycleIsRecordState(t *testing.T) {
	env, _ := newBundleEnv(t)
	tok := env.svc.token(fakeRepository)

	rec := env.do(t, http.MethodPatch, bundlePath, tok,
		map[string]any{"properties": map[string]any{"disabled": true}})
	wantStatus(t, rec, http.StatusOK)
	if st := decodeJSON[substrate.BundleStatus](t, rec); !st.Installed {
		t.Fatalf("disable answers the refreshed status: %s", rec.Body.String())
	}

	rec = env.do(t, http.MethodPatch, bundlePath, tok,
		map[string]any{"properties": map[string]any{"disabled": false}})
	wantStatus(t, rec, http.StatusOK)

	rec = env.do(t, http.MethodPatch, bundlePath, tok,
		map[string]any{"properties": map[string]any{"purging": true}})
	wantStatus(t, rec, http.StatusOK)
	if out := decodeJSON[map[string]any](t, rec); out["purged"] != float64(3) {
		t.Fatalf("purge body = %v, want {\"purged\": 3}", out)
	}

	// A PATCH that carries no lifecycle state names the accepted ones.
	rec = env.do(t, http.MethodPatch, bundlePath, tok,
		map[string]any{"properties": map[string]any{"authority": "x"}})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

// ---- scoped changefeed refill (#13) -----------------------------------------

// ---- strict REST decode trailing closer (#16) -------------------------------

// TestStrictDecodeRejectsTrailingCloser pins codex regress #16: a body like
// {}} must be bad_request. Decoder.More() missed it; a required io.EOF second
// decode catches the stray closer.
func TestStrictDecodeRejectsTrailingCloser(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	req := httptest.NewRequest(http.MethodPost, recordsPath, strings.NewReader("{}}"))
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}
