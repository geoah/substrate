package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// What an app's token reaches beside the records once its approval set up a
// webhook (docs/plans/app-setup.md): the webhook door with the token in place
// of the key, that trigger's parked list, blob storage under the blob rule,
// and never a trigger's key.

const (
	appHook      = "applehealth-webhook"
	appHookKey   = "the-trigger-key-0123456789"
	otherRepo    = "other.example.com"
	photoKind    = "samples.substrate.reamde.dev/media/photo"
	scanKind     = "samples.substrate.reamde.dev/vault/scan"
	watchKind    = "samples.substrate.reamde.dev/ops/watch"
	triggerScope = "read:" + kindTrigger
)

// grantHookSvc is the engine's door in miniature: every trigger it holds is
// keyed, and the key or the door's webhook grant for that repository and
// trigger admits a delivery.
type grantHookSvc struct {
	*fakeService
	mu    sync.Mutex
	keys  map[string]string // "<authority>/<trigger>" -> its key
	calls []webhookCall
}

func (s *grantHookSvc) admit(ctx context.Context, authority, trigger, key string) error {
	want, ok := s.keys[authority+"/"+trigger]
	if !ok || (!substrate.WebhookGranted(ctx, authority, trigger) && key != want) {
		return substrate.ErrNotFound
	}
	return nil
}

func (s *grantHookSvc) CheckWebhook(ctx context.Context, authority, trigger, key string) (substrate.WebhookTarget, error) {
	if err := s.admit(ctx, authority, trigger, key); err != nil {
		return substrate.WebhookTarget{}, err
	}
	return substrate.WebhookTarget{MaxBody: substrate.WebhookInlineDefault}, nil
}

func (s *grantHookSvc) ReceiveWebhook(ctx context.Context, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	if err := s.admit(ctx, authority, trigger, key); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, webhookCall{authority, trigger, key, req})
	return "hook-1", nil
}

func newGrantHookSvc() *grantHookSvc {
	svc := &grantHookSvc{fakeService: newFakeService(), keys: map[string]string{
		fakeRepository + "/" + appHook:    appHookKey,
		fakeRepository + "/other-webhook": appHookKey,
		otherRepo + "/" + appHook:         appHookKey,
	}}
	svc.addRepository(otherRepo)
	return svc
}

// appToken registers an app's token for repository holding scopes.
func appToken(svc *fakeService, repository string, scopes ...string) string {
	return svc.tokenWith(repository, func(i *substrate.TokenInfo) {
		i.Scopes, i.ClientID, i.ClientName = scopes, appClientID, "Notes"
	})
}

func TestAnAppsTokenDeliversToTheWebhookItsScopeNames(t *testing.T) {
	svc := newGrantHookSvc()
	h := New(Config{Service: svc})
	app := appToken(svc.fakeService, fakeRepository, "webhook:"+appHook)
	rec := postHook(h, "/webhooks/"+fakeRepository+"/"+appHook, []byte(`{"n":1}`), map[string]string{
		"Content-Type": "application/json", "Authorization": "Bearer " + app,
	})
	wantStatus(t, rec, http.StatusAccepted)
	if len(svc.calls) != 1 {
		t.Fatalf("the delivery reached the service %d times, want once", len(svc.calls))
	}
	c := svc.calls[0]
	if c.key != "" {
		t.Fatalf("the token reached the service as the key %q", c.key)
	}
	if _, has := c.req.Headers["authorization"]; has {
		t.Fatal("the token reached the callable's headers")
	}
	// The key still works beside it, as it does for every other sender.
	keyed := postHook(h, "/webhooks/"+fakeRepository+"/"+appHook+"/"+appHookKey, nil, nil)
	wantStatus(t, keyed, http.StatusAccepted)
}

// A bearer that is not an app's token delivering to this trigger in this
// repository is the key it always was, and is refused exactly as a wrong
// key is: the answer says nothing about the token.
func TestAWebhookTokenThatDoesNotDeliverIsRefusedLikeAWrongKey(t *testing.T) {
	svc := newGrantHookSvc()
	h := New(Config{Service: svc})
	hook := "/webhooks/" + fakeRepository + "/" + appHook
	revoked := appToken(svc.fakeService, fakeRepository, "webhook:"+appHook)
	delete(svc.tokens, revoked)
	past := time.Now().Add(-time.Hour)
	expired := svc.tokenWith(fakeRepository, func(i *substrate.TokenInfo) {
		i.Scopes, i.ExpiresAt = []string{"webhook:" + appHook}, &past
	})
	for name, c := range map[string]struct{ bearer, path string }{
		"without the scope":             {appToken(svc.fakeService, fakeRepository, "read:"+personKind), hook},
		"for another trigger":           {appToken(svc.fakeService, fakeRepository, "webhook:other-webhook"), hook},
		"revoked":                       {revoked, hook},
		"expired":                       {expired, hook},
		"of another repository":         {appToken(svc.fakeService, otherRepo, "webhook:"+appHook), hook},
		"to another repository's door":  {appToken(svc.fakeService, fakeRepository, "webhook:"+appHook), "/webhooks/" + otherRepo + "/" + appHook},
		"with full access and no key":   {svc.token(fakeRepository), hook},
		"whose grant does not parse":    {appToken(svc.fakeService, fakeRepository, "everything"), hook},
		"not a token, under the prefix": {substrate.TokenSecretPrefix + "0123456789abcdef", hook},
	} {
		wrong := postHook(h, c.path, []byte(`{}`), map[string]string{"Authorization": "Bearer wrong-key-0123456789"})
		got := postHook(h, c.path, []byte(`{}`), map[string]string{"Authorization": "Bearer " + c.bearer})
		if got.Code != http.StatusNotFound {
			t.Errorf("%s: answered %d %s, want the door's 404", name, got.Code, got.Body.String())
			continue
		}
		if diff := webhookAnswerDiff(wrong, got); diff != "" {
			t.Errorf("%s: a token and a wrong key are told apart: %s", name, diff)
		}
	}
	if len(svc.calls) != 0 {
		t.Fatalf("a refused delivery reached the service: %+v", svc.calls)
	}
}

// deliveryDataset answers the verbs the fake leaves unmodeled: a trigger's
// parked rows, and a blob store whose bytes keep what their first upload
// said about them.
type deliveryDataset struct {
	*fakeDataset
	parked map[string][]substrate.TriggerFailure
	stored map[string]substrate.BlobInfo
	puts   int
	actor  substrate.Actor
}

func (d *deliveryDataset) TriggerFailures(_ context.Context, id string) ([]substrate.TriggerFailure, error) {
	rows, ok := d.parked[id]
	if !ok {
		return nil, noSuch("trigger", id)
	}
	return rows, nil
}

func (d *deliveryDataset) PutBlob(_ context.Context, actor substrate.Actor, up substrate.BlobUpload, data []byte, _ string) (*substrate.BlobInfo, error) {
	d.puts++
	d.actor = actor
	sum := sha256.Sum256(data)
	digest := substrate.BlobDigestPrefix + hex.EncodeToString(sum[:])
	if held, ok := d.stored[digest]; ok {
		return &held, nil
	}
	info := substrate.BlobInfo{Digest: digest, Size: int64(len(data)), Name: up.Name, MediaType: up.MediaType, Status: substrate.BlobStored}
	d.stored[digest] = info
	return &info, nil
}

// newDeliveryEnv is a fake whose repository declares the trigger kind, a
// photo kind with a blob-ref, an internal kind with one, and a kind that
// references a trigger.
func newDeliveryEnv(t *testing.T) (*appEnv, *deliveryDataset) {
	t.Helper()
	e := newAppEnv(t)
	inner := e.svc.datasets[fakeRepository]
	inner.types = append(inner.types,
		substrate.KindInfo{
			Identity: kindTrigger, Name: "trigger", Authority: coreAuthorityName, Package: "core", Version: 1, Source: "builtin",
			Definition: map[string]any{"purpose": vocabulary.PurposeInternal, "properties": map[string]any{
				"label":   map[string]any{"type": "string"},
				"enabled": map[string]any{"type": "bool"},
				"source":  map[string]any{"type": "object"},
			}},
		},
		substrate.KindInfo{
			Identity: photoKind, Name: "photo", Authority: "samples.substrate.reamde.dev", Package: "media", Version: 1, Source: "installed",
			Definition: map[string]any{"properties": map[string]any{
				"caption": map[string]any{"type": "string"},
				"image":   map[string]any{"type": "blobref"},
			}},
		},
		substrate.KindInfo{
			Identity: scanKind, Name: "scan", Authority: "samples.substrate.reamde.dev", Package: "vault", Version: 1, Source: "installed",
			Definition: map[string]any{"purpose": vocabulary.PurposeInternal, "properties": map[string]any{
				"file": map[string]any{"type": "blobref"},
			}},
		},
		substrate.KindInfo{
			Identity: watchKind, Name: "watch", Authority: "samples.substrate.reamde.dev", Package: "ops", Version: 1, Source: "installed",
			Definition: map[string]any{"properties": map[string]any{
				"trigger": map[string]any{"type": "reference", "kind": kindTrigger},
			}},
		},
	)
	d := &deliveryDataset{
		fakeDataset: inner,
		parked: map[string][]substrate.TriggerFailure{
			appHook:         {{ID: 7, Trigger: appHook, FireID: "hook-abc", Attempts: 1, LastError: "a day could not be filed", ParkedAt: time.Unix(0, 0).UTC()}},
			"other-webhook": {{ID: 8, Trigger: "other-webhook", FireID: "hook-def", Attempts: 1, LastError: "x", ParkedAt: time.Unix(0, 0).UTC()}},
		},
		stored: map[string]substrate.BlobInfo{},
	}
	e.svc.wrap = func(fd *fakeDataset) substrate.Dataset {
		if fd == inner {
			return d
		}
		return &mintingDataset{fakeDataset: fd, svc: e.svc}
	}
	return e, d
}

func TestAnAppsTokenReadsTheParkedDeliveriesOfItsWebhookAlone(t *testing.T) {
	e, _ := newDeliveryEnv(t)
	app := appToken(e.svc, fakeRepository, "webhook:"+appHook)
	parked := "/api/v1/" + kindTrigger + "/"
	rec := e.do(http.MethodGet, parked+appHook+"/parked", app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.OperationalList[substrate.TriggerFailure]](t, rec)
	if len(got.Items) != 1 || got.Items[0].FireID != "hook-abc" {
		t.Fatalf("parked = %+v, want the one row of %s", got.Items, appHook)
	}
	wantStatus(t, e.do(http.MethodGet, parked+"other-webhook/parked", app, nil, ""), http.StatusForbidden)
	// The owner still reads any trigger's list; the rest of the verbs stay
	// the owner's.
	wantStatus(t, e.do(http.MethodGet, parked+"other-webhook/parked", e.owner, nil, ""), http.StatusOK)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, parked + appHook + "/parked/7/retry"},
		{http.MethodDelete, parked + appHook + "/parked/7"},
		{http.MethodPost, parked + appHook + "/wake"},
		{http.MethodGet, parked + "status"},
	} {
		if rec := e.do(c.method, c.path, app, []byte(`{}`), "application/json"); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with an app's webhook token = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	// A token holding no webhook scope reaches no parked list.
	reader := appToken(e.svc, fakeRepository, triggerScope)
	wantStatus(t, e.do(http.MethodGet, parked+appHook+"/parked", reader, nil, ""), http.StatusForbidden)
}

func putBlobAs(e *appEnv, bearer, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	return e.do(http.MethodPut, path, bearer, body, contentType)
}

func TestAnAppsTokenStoresBlobsForAKindWithABlobRef(t *testing.T) {
	e, d := newDeliveryEnv(t)
	// The owner stored these bytes first, under a name and a type of their
	// own.
	secret := []byte("the owner's scanned letter")
	wantStatus(t, putBlobAs(e, e.owner, "/api/v1/blobs?name=letter-from-the-bank.pdf", secret, "application/pdf"), http.StatusCreated)

	app := appToken(e.svc, fakeRepository, "write:"+photoKind)
	sum := sha256.Sum256(secret)
	digest := substrate.BlobDigestPrefix + hex.EncodeToString(sum[:])
	for _, path := range []string{"/api/v1/blobs?name=cat.jpg", "/api/v1/blobs/" + digest + "?name=cat.jpg"} {
		rec := putBlobAs(e, app, path, secret, "image/jpeg")
		wantStatus(t, rec, http.StatusCreated)
		got := decodeJSON[substrate.BlobInfo](t, rec)
		if got.Digest != digest || got.Size != int64(len(secret)) || got.Status != substrate.BlobStored {
			t.Fatalf("%s answered %+v, want the digest and size of what it sent", path, got)
		}
		if got.Name != "cat.jpg" || got.MediaType != "image/jpeg" {
			t.Fatalf("%s answered name %q and type %q: the stored manifest reached the app", path, got.Name, got.MediaType)
		}
		if strings.Contains(rec.Body.String(), "letter-from-the-bank") || strings.Contains(rec.Body.String(), "application/pdf") {
			t.Fatalf("%s answered %s", path, rec.Body.String())
		}
	}
	if d.actor != "app:notes.example.org" {
		t.Fatalf("the app's blob was stored as %q, want its own actor", d.actor)
	}
	// Nothing said, nothing answered.
	rec := putBlobAs(e, app, "/api/v1/blobs", secret, "")
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeJSON[substrate.BlobInfo](t, rec); got.Name != "" || got.MediaType != "" {
		t.Fatalf("an upload that named nothing answered %+v", got)
	}
	// Reading a blob stays the owner's.
	wantStatus(t, e.do(http.MethodGet, "/api/v1/blobs/"+digest, app, nil, ""), http.StatusForbidden)

	// A glob over a package reaches its blob-ref kind as a write does.
	glob := appToken(e.svc, fakeRepository, "write:samples.substrate.reamde.dev/media/*")
	wantStatus(t, putBlobAs(e, glob, "/api/v1/blobs", []byte("another"), "image/png"), http.StatusCreated)
}

func TestAnAppsTokenThatWritesNoBlobRefStoresNoBlob(t *testing.T) {
	e, d := newDeliveryEnv(t)
	for name, scopes := range map[string][]string{
		"a kind without a blob-ref":         {"write:" + taskKind},
		"a blob-ref kind it only reads":     {"read:" + photoKind},
		"an internal kind through a glob":   {"write:samples.substrate.reamde.dev/vault/*"},
		"a webhook and nothing to write to": {"webhook:" + appHook},
	} {
		app := appToken(e.svc, fakeRepository, scopes...)
		rec := putBlobAs(e, app, "/api/v1/blobs", []byte("bytes"), "image/png")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: PUT /blobs = %d %s, want 403", name, rec.Code, rec.Body.String())
		}
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
			t.Errorf("%s: WWW-Authenticate = %q", name, wa)
		}
	}
	if d.puts != 0 {
		t.Fatalf("a refused upload reached the store %d times", d.puts)
	}
	// An exact scope reaches an internal kind, and its blob-ref with it.
	exact := appToken(e.svc, fakeRepository, "write:"+scanKind)
	wantStatus(t, putBlobAs(e, exact, "/api/v1/blobs", []byte("bytes"), "image/png"), http.StatusCreated)
}

// webhookKeyIn digs a trigger's key out of an answer's properties.
func webhookKeyIn(props map[string]any) (any, bool) {
	src, _ := props["source"].(map[string]any)
	hook, _ := src["webhook"].(map[string]any)
	k, ok := hook["key"]
	return k, ok
}

func TestATriggerAnAppsTokenReadsNeverCarriesItsKey(t *testing.T) {
	e, d := newDeliveryEnv(t)
	path := vocabulary.RecordPath(kindTrigger, appHook)
	d.records[appHook] = &substrate.Record{
		ID: appHook, Kind: kindTrigger, Title: "Apple Health webhook", Version: 1,
		Properties: map[string]any{
			"label":   "Apple Health webhook",
			"enabled": true,
			"source":  map[string]any{"webhook": map[string]any{"key": appHookKey, "headers": []any{"x-device"}}},
		},
		Labels: map[string]any{},
	}
	d.meta[appHook] = map[string]substrate.PropertyMeta{
		"source": {Alternatives: []substrate.PropertyAlternative{
			{Actor: "bundle:example.com:hooks", Value: map[string]any{"webhook": map[string]any{"key": appHookKey}}},
		}},
	}
	d.records["w1"] = &substrate.Record{
		ID: "w1", Kind: watchKind, Version: 1, Labels: map[string]any{},
		Properties: map[string]any{"trigger": map[string]any{vocabulary.ReferenceValueKey: path}},
	}
	app := appToken(e.svc, fakeRepository, triggerScope, "read:"+watchKind)
	noKey := func(what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		wantStatus(t, rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), appHookKey) {
			t.Fatalf("%s carried the trigger's key: %s", what, rec.Body.String())
		}
	}

	get := e.do(http.MethodGet, "/api/v1/"+path, app, nil, "")
	noKey("a get", get)
	rec := decodeJSON[substrate.Record](t, get)
	src, _ := rec.Properties["source"].(map[string]any)
	if hook, _ := src["webhook"].(map[string]any); hook == nil || hook["headers"] == nil || rec.Properties["label"] != "Apple Health webhook" {
		t.Fatalf("the get lost more than the key: %+v", rec.Properties)
	}
	list := "/api/v1/records?filter=" + url.QueryEscape(`{"kinds":["`+kindTrigger+`"]}`)
	noKey("a list", e.do(http.MethodGet, list, app, nil, ""))
	noKey("a list of every kind it reads", e.do(http.MethodGet, "/api/v1/records", app, nil, ""))
	noKey("a search", e.do(http.MethodGet, "/api/v1/records?q=Apple", app, nil, ""))
	expanded := "/api/v1/records?expand=trigger&filter=" + url.QueryEscape(`{"kinds":["`+watchKind+`"]}`)
	exp := e.do(http.MethodGet, expanded, app, nil, "")
	noKey("an expanded reference", exp)
	if page := decodeJSON[substrate.Page](t, exp); page.Included[path] == nil {
		t.Fatalf("the expansion carried no trigger: %+v", page.Included)
	}

	// The engine compares the stored text of a condition's property and a
	// cursor carries the sort values, so neither reaches a trigger's source.
	for name, target := range map[string]string{
		"a condition on source": "/api/v1/records?filter=" + url.QueryEscape(`{"kinds":["`+kindTrigger+`"],"properties":{"source":{"prefix":"{"}}}`),
		"an order on source":    "/api/v1/records?orderBy=source&filter=" + url.QueryEscape(`{"kinds":["`+kindTrigger+`"]}`),
	} {
		if rec := e.do(http.MethodGet, target, app, nil, ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s = %d %s, want 403", name, rec.Code, rec.Body.String())
		}
	}

	// Nothing the app read was cut out of the record the dataset holds, and
	// the owner still reads it whole.
	if k, ok := webhookKeyIn(d.records[appHook].Properties); !ok || k != appHookKey {
		t.Fatalf("the app's read cut the key out of the stored record: %+v", d.records[appHook].Properties)
	}
	if alt := d.meta[appHook]["source"].Alternatives[0].Value.(map[string]any); alt["webhook"].(map[string]any)["key"] != appHookKey {
		t.Fatalf("the app's read cut the key out of the stored provenance: %+v", alt)
	}
	owner := e.do(http.MethodGet, "/api/v1/"+path, e.owner, nil, "")
	wantStatus(t, owner, http.StatusOK)
	if k, ok := webhookKeyIn(decodeJSON[substrate.Record](t, owner).Properties); !ok || k != appHookKey {
		t.Fatalf("the owner's read lost the key: %s", owner.Body.String())
	}
}
