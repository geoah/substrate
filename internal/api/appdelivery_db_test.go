package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// appHook is the fixture's webhook trigger, appHookKey its key, and
	// appDeliveryOther the second webhook trigger.
	appHook          = "applehealth-webhook"
	appHookKey       = "the-trigger-key-0123456789"
	appDeliveryOther = "other-webhook"
	triggerScope     = "read:" + kindTrigger
)

// appDeliveryEnv is the engine whose repository declares, under its own
// authority, a photo kind with a blob-ref, an album kind with a repeated
// one, an internal scan kind with one, a watch kind that references a
// trigger, and two keyed webhook triggers, appHook and appDeliveryOther,
// whose callable echoes what it received into an echo record, or, when the
// fixture fails its deliveries, raises.
type appDeliveryEnv struct {
	*dbEnv
	photo, album, scan, watch, echo string
}

func newAppDeliveryEnv(t *testing.T, failing bool, opts ...dbOption) *appDeliveryEnv {
	t.Helper()
	env := newDBEnv(t, append([]dbOption{withSamples("tasks"), withAppFlow()}, opts...)...)
	e := &appDeliveryEnv{dbEnv: env}
	e.photo, e.album, e.scan, e.watch, e.echo = appDeliveryInstall(t, env.ds, failing)
	return e
}

// appDeliveryInstall declares the fixture's kinds, its two functions and its
// two triggers into ds, and answers the photo, album, scan, watch and echo
// kinds.
func appDeliveryInstall(t *testing.T, ds substrate.Dataset, failing bool) (photo, album, scan, watch, echo string) {
	t.Helper()
	own := ds.Repository().Authority
	media, vault, ops, hooks := own+"/media", own+"/vault", own+"/ops", own+"/hooks"
	photo, album, scan, watch, echo = media+"/photo", media+"/album", vault+"/scan", ops+"/watch", hooks+"/echo"
	kind := func(pkg, name string, data map[string]any) map[string]any {
		return vocabulary.KindManifest(pkg, map[string]any{"singular": name}, data)
	}
	props := func(p map[string]any) map[string]any { return map[string]any{"properties": p} }
	docs := []map[string]any{
		vocabulary.PackageManifest(media, 1),
		kind(media, "photo", props(map[string]any{
			"caption": map[string]any{"type": "string"},
			"image":   map[string]any{"type": "blobref"},
		})),
		kind(media, "album", props(map[string]any{"images": map[string]any{"type": "blobref", "repeated": true}})),
		vocabulary.PackageManifest(vault, 1),
		kind(vault, "scan", map[string]any{"purpose": vocabulary.PurposeInternal, "properties": map[string]any{"file": map[string]any{"type": "blobref"}}}),
		vocabulary.PackageManifest(ops, 1),
		kind(ops, "watch", props(map[string]any{"trigger": map[string]any{"type": "reference", "kind": kindTrigger}})),
		vocabulary.PackageManifest(hooks, 1),
		kind(hooks, "echo", props(map[string]any{
			"payload": map[string]any{"type": "string"}, "device": map[string]any{"type": "string"},
		})),
		vocabulary.FunctionManifest(hooks, "receive", map[string]any{
			"description": "echoes a webhook request", "runtime": vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{echo}},
			"source": `
def main(input, host):
    req = (input.get("envelope") or {}).get("request") or {}
    headers = req.get("headers") or {}
    host.effects.put("` + echo + `", host.ids.minted(input["envelope"]["fire"]["id"]), properties={
        "payload": (req.get("body") or {}).get("text") or "",
        "device": headers.get("x-device") or "absent",
    })
    return {"output": {}}
`,
		}),
		vocabulary.FunctionManifest(hooks, "refuse", map[string]any{
			"description": "refuses every webhook request", "runtime": vocabulary.RuntimePython,
			"source": `
def main(input, host):
    raise RuntimeError("a day could not be filed")
`,
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, docs); err != nil {
		t.Fatalf("declare the delivery fixture: %v", err)
	}
	callable := hooks + "/receive"
	if failing {
		callable = hooks + "/refuse"
	}
	for _, id := range []string{appHook, appDeliveryOther} {
		if _, err := ds.Put(context.Background(), substrate.ActorSystem, substrate.PutInput{Kind: kindTrigger, ID: id, Properties: map[string]any{
			"label": "Apple Health webhook", "enabled": true,
			"source":   map[string]any{"webhook": map[string]any{"key": appHookKey, "headers": []any{"x-device"}}},
			"callable": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindFunction, callable)},
		}}); err != nil {
			t.Fatalf("put trigger %s: %v", id, err)
		}
	}
	return photo, album, scan, watch, echo
}

// echoes lists the echo records the deliveries wrote.
func (e *appDeliveryEnv) echoes(t *testing.T) []*substrate.Record {
	t.Helper()
	page, err := e.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{e.echo}}})
	if err != nil {
		t.Fatalf("list echoes: %v", err)
	}
	return page.Records
}

// waitEchoes waits for n echo records, the fires running behind the door's
// answer.
func (e *appDeliveryEnv) waitEchoes(t *testing.T, n int) []*substrate.Record {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		got := e.echoes(t)
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for %d deliveries, %d arrived", n, len(got))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// pending lists the deliveries a trigger holds: accepted, running or parked.
func (e *appDeliveryEnv) pending(t *testing.T, trigger string) []substrate.TriggerFailure {
	t.Helper()
	rows, err := e.ds.TriggerFailures(context.Background(), trigger)
	if err != nil {
		t.Fatalf("trigger failures of %s: %v", trigger, err)
	}
	return rows
}

func appDeliveryDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return substrate.BlobDigestPrefix + hex.EncodeToString(sum[:])
}

// appDeliveryMint mints an app's token holding scopes and answers its record
// id beside its secret.
func appDeliveryMint(t *testing.T, ds substrate.Dataset, in substrate.MintTokenInput) (id, secret string) {
	t.Helper()
	if in.Label == "" {
		in.Label = "Notes"
	}
	info, secret, err := ds.MintToken(context.Background(), in)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return info.ID, secret
}

// appDeliveryRevoke deletes a token's record: it no longer authenticates.
func appDeliveryRevoke(t *testing.T, ds substrate.Dataset, id string) {
	t.Helper()
	if _, err := ds.Delete(context.Background(), substrate.ActorPerson, tokenType, id, substrate.DeleteInput{}); err != nil {
		t.Fatalf("revoke token %s: %v", id, err)
	}
}

// appDeliveryReceived records what the door handed the engine's webhook
// receiver, then hands it on: the key and the headers the door passed are
// its hand-off, which the fire cannot show, since the engine keeps only a
// trigger's declared headers and refuses to declare a credential's.
type appDeliveryReceived struct {
	substrate.Service
	mu    sync.Mutex
	calls []webhookCall
}

func (s *appDeliveryReceived) ReceiveWebhook(ctx context.Context, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, webhookCall{authority, trigger, key, req})
	s.mu.Unlock()
	return s.Service.ReceiveWebhook(ctx, authority, trigger, key, req)
}

func TestAnAppsTokenDeliversToTheWebhookItsScopeNames(t *testing.T) {
	t.Parallel()
	received := &appDeliveryReceived{}
	e := newAppDeliveryEnv(t, false, withService(func(s substrate.Service) substrate.Service {
		received.Service = s
		return received
	}))
	app := appScopedToken(t, e.dbEnv, "webhook:"+appHook)
	// The trigger is keyed and the bearer is not its key: the token stands in
	// for the key, or the engine would refuse it.
	rec := postHook(e.h, "/webhooks/"+e.repository+"/"+appHook, []byte(`{"n":1}`), map[string]string{
		"Content-Type": "application/json", "Authorization": "Bearer " + app, "X-Device": "watch",
	})
	wantStatus(t, rec, http.StatusAccepted)
	got := e.waitEchoes(t, 1)
	if len(got) != 1 || got[0].Properties["payload"] != `{"n":1}` || got[0].Properties["device"] != "watch" {
		t.Fatalf("the delivery reached the callable as %+v, want once with its body", got)
	}
	received.mu.Lock()
	if len(received.calls) != 1 {
		t.Fatalf("the delivery reached the engine %d times, want once", len(received.calls))
	}
	c := received.calls[0]
	received.mu.Unlock()
	if c.key != "" {
		t.Fatalf("the token reached the engine as the key %q", c.key)
	}
	if _, has := c.req.Headers["authorization"]; has {
		t.Fatal("the token reached the callable's headers")
	}
	// The key still works beside it, as it does for every other sender.
	keyed := postHook(e.h, "/webhooks/"+e.repository+"/"+appHook+"/"+appHookKey, nil, nil)
	wantStatus(t, keyed, http.StatusAccepted)
	e.waitEchoes(t, 2)
}

// appDeliveryUnparsedGrant answers one secret as a token whose stored scopes
// do not parse. The mint refuses such scopes, so no token the engine holds
// carries them; the handler's fail-closed reading of one is what is under
// test.
type appDeliveryUnparsedGrant struct {
	substrate.Service
	secret, as string
}

func (s appDeliveryUnparsedGrant) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	if secret != s.secret {
		return s.Service.Authenticate(ctx, secret)
	}
	ds, info, err := s.Service.Authenticate(ctx, s.as)
	info.Scopes = []string{"everything"}
	return ds, info, err
}

// A bearer that is not an app's token delivering to this trigger in this
// repository is the key it always was, and is refused exactly as a wrong
// key is: the answer says nothing about the token.
func TestAWebhookTokenThatDoesNotDeliverIsRefusedLikeAWrongKey(t *testing.T) {
	t.Parallel()
	const unparsed = substrate.TokenSecretPrefix + "unparsed0123456789abcdef"
	var holder appDeliveryUnparsedGrant
	e := newAppDeliveryEnv(t, false, withService(func(s substrate.Service) substrate.Service {
		holder.Service, holder.secret = s, unparsed
		return &holder
	}))
	ctx := context.Background()
	// Another repository on the same engine, holding the same trigger.
	otherRepository := "other" + e.repository
	if _, err := e.svc.CreateRepository(ctx, otherRepository); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	other, err := e.svc.Dataset(ctx, otherRepository)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	appDeliveryInstall(t, other, false)
	hook := "/webhooks/" + e.repository + "/" + appHook
	scoped := substrate.MintTokenInput{Scopes: []string{"webhook:" + appHook}, ClientID: appClientID, ClientName: "Notes"}

	revokedID, revoked := appDeliveryMint(t, e.ds, scoped)
	appDeliveryRevoke(t, e.ds, revokedID)
	soon := e.clock.now().Add(time.Minute)
	expiring := scoped
	expiring.ExpiresAt = &soon
	_, expired := appDeliveryMint(t, e.ds, expiring)
	e.clock.advance(2 * time.Minute)
	_, ofOther := appDeliveryMint(t, other, scoped)
	holder.as = appScopedToken(t, e.dbEnv, "read:"+personKind)
	for name, c := range map[string]struct{ bearer, path string }{
		"without the scope":             {appScopedToken(t, e.dbEnv, "read:"+personKind), hook},
		"for another trigger":           {appScopedToken(t, e.dbEnv, "webhook:"+appDeliveryOther), hook},
		"revoked":                       {revoked, hook},
		"expired":                       {expired, hook},
		"of another repository":         {ofOther, hook},
		"to another repository's door":  {appScopedToken(t, e.dbEnv, "webhook:"+appHook), "/webhooks/" + otherRepository + "/" + appHook},
		"with full access and no key":   {e.token, hook},
		"whose grant does not parse":    {unparsed, hook},
		"not a token, under the prefix": {substrate.TokenSecretPrefix + "0123456789abcdef", hook},
	} {
		wrong := postHook(e.h, c.path, []byte(`{}`), map[string]string{"Authorization": "Bearer wrong-key-0123456789"})
		got := postHook(e.h, c.path, []byte(`{}`), map[string]string{"Authorization": "Bearer " + c.bearer})
		if got.Code != http.StatusNotFound {
			t.Errorf("%s: answered %d %s, want the door's 404", name, got.Code, got.Body.String())
			continue
		}
		if diff := webhookAnswerDiff(wrong, got); diff != "" {
			t.Errorf("%s: a token and a wrong key are told apart: %s", name, diff)
		}
	}
	if rows := e.pending(t, appHook); len(rows) != 0 || len(e.echoes(t)) != 0 {
		t.Fatalf("a refused delivery reached the trigger: %+v", rows)
	}
	otherRows, err := other.TriggerFailures(ctx, appHook)
	if err != nil || len(otherRows) != 0 {
		t.Fatalf("a refused delivery reached the other repository's trigger: %+v, %v", otherRows, err)
	}
}

// appDeliveryRevokeOnCheck revokes a token once the door's checks admitted a
// delivery, before its body is read: the window between the check and the
// hand-off, which no request to the engine can land a revoke inside.
type appDeliveryRevokeOnCheck struct {
	substrate.Service
	revoke func()
}

func (s *appDeliveryRevokeOnCheck) CheckWebhook(ctx context.Context, authority, trigger, key string) (substrate.WebhookTarget, error) {
	target, err := s.Service.CheckWebhook(ctx, authority, trigger, key)
	if err == nil && s.revoke != nil {
		s.revoke()
	}
	return target, err
}

// A token revoked while its delivery's body arrives delivers nothing: the
// door reads the token again before it hands the delivery on.
func TestATokenRevokedWhileItsBodyArrivesDeliversNothing(t *testing.T) {
	t.Parallel()
	seam := &appDeliveryRevokeOnCheck{}
	e := newAppDeliveryEnv(t, false, withService(func(s substrate.Service) substrate.Service {
		seam.Service = s
		return seam
	}))
	hook := "/webhooks/" + e.repository + "/" + appHook
	id, app := appDeliveryMint(t, e.ds, substrate.MintTokenInput{Scopes: []string{"webhook:" + appHook}, ClientID: appClientID, ClientName: "Notes"})
	seam.revoke = func() { appDeliveryRevoke(t, e.ds, id) }
	got := postHook(e.h, hook, []byte(`{"n":1}`), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + app})
	seam.revoke = nil
	wrong := postHook(e.h, hook, []byte(`{"n":1}`), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer wrong-key-0123456789"})
	if got.Code != http.StatusNotFound {
		t.Fatalf("a delivery whose token was revoked mid-body = %d %s, want the door's 404", got.Code, got.Body.String())
	}
	if diff := webhookAnswerDiff(wrong, got); diff != "" {
		t.Fatalf("a revoked token and a wrong key are told apart: %s", diff)
	}
	if rows := e.pending(t, appHook); len(rows) != 0 || len(e.echoes(t)) != 0 {
		t.Fatalf("a revoked token's delivery reached the trigger: %+v", rows)
	}
}

func TestAnAppsTokenReadsTheParkedDeliveriesOfItsWebhookAlone(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, true)
	// Each trigger's delivery fails in its callable and parks.
	for _, id := range []string{appHook, appDeliveryOther} {
		wantStatus(t, postHook(e.h, "/webhooks/"+e.repository+"/"+id+"/"+appHookKey, []byte(`{}`), nil), http.StatusAccepted)
	}
	for _, id := range []string{appHook, appDeliveryOther} {
		deadline := time.Now().Add(30 * time.Second)
		for {
			rows := e.pending(t, id)
			if len(rows) == 1 && strings.Contains(rows[0].LastError, "a day could not be filed") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s's delivery never parked: %+v", id, rows)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	hookRow := e.pending(t, appHook)[0]

	app := appScopedToken(t, e.dbEnv, "webhook:"+appHook)
	parked := "/api/v1/" + kindTrigger + "/"
	rec := appScopedDo(t, e.dbEnv, http.MethodGet, parked+appHook+"/parked", app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.OperationalList[substrate.TriggerFailure]](t, rec)
	if len(got.Items) != 1 || got.Items[0].FireID != hookRow.FireID || got.Items[0].Trigger != appHook {
		t.Fatalf("parked = %+v, want the one row of %s", got.Items, appHook)
	}
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodGet, parked+appDeliveryOther+"/parked", app, nil, ""), http.StatusForbidden)
	// The owner still reads any trigger's list; the rest of the verbs stay
	// the owner's.
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodGet, parked+appDeliveryOther+"/parked", e.token, nil, ""), http.StatusOK)
	failure := strconv.FormatInt(hookRow.ID, 10)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, parked + appHook + "/parked/" + failure + "/retry"},
		{http.MethodDelete, parked + appHook + "/parked/" + failure},
		{http.MethodPost, parked + appHook + "/wake"},
		{http.MethodGet, parked + "status"},
	} {
		if rec := appScopedDo(t, e.dbEnv, c.method, c.path, app, []byte(`{}`), "application/json"); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with an app's webhook token = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	if rows := e.pending(t, appHook); len(rows) != 1 || rows[0].ID != hookRow.ID {
		t.Fatalf("a refused verb moved the parked row: %+v", rows)
	}
	// A token holding no webhook scope reaches no parked list.
	reader := appScopedToken(t, e.dbEnv, triggerScope)
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodGet, parked+appHook+"/parked", reader, nil, ""), http.StatusForbidden)
}

func TestAnAppsTokenStoresBlobsForAKindWithABlobRef(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, false)
	// The owner stored these bytes first, under a name and a type of their
	// own.
	secret := []byte("the owner's scanned letter")
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs?name=letter-from-the-bank.pdf", e.token, secret, "application/pdf"), http.StatusCreated)

	app := appScopedToken(t, e.dbEnv, "write:"+e.photo)
	digest := appDeliveryDigest(secret)
	for _, path := range []string{"/api/v1/blobs?name=cat.jpg", "/api/v1/blobs/" + digest + "?name=cat.jpg"} {
		rec := appScopedDo(t, e.dbEnv, http.MethodPut, path, app, secret, "image/jpeg")
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
	if info, _, err := e.ds.GetBlob(context.Background(), digest); err != nil || info.Name != "letter-from-the-bank.pdf" {
		t.Fatalf("the app's upload changed the owner's manifest: %+v, %v", info, err)
	}
	// Bytes nobody stored before are the app's own, written as its actor.
	fresh := []byte("the app's own photo")
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs?name=own.jpg", app, fresh, "image/jpeg"), http.StatusCreated)
	if actor := appScopedLastActor(t, e.dbEnv, kindBlob, appDeliveryDigest(fresh)); actor != appActor {
		t.Fatalf("the app's blob was stored as %q, want its own actor", actor)
	}
	// Nothing said, nothing answered.
	rec := appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs", app, secret, "")
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeJSON[substrate.BlobInfo](t, rec); got.Name != "" || got.MediaType != "" {
		t.Fatalf("an upload that named nothing answered %+v", got)
	}
	// Reading a blob takes a record the token reads that names it (decision
	// 0219, TestAnAppReadsABlobARecordItReadsNamesAndNoOther): none does, so
	// the blob is as absent as one nobody stored.
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodGet, "/api/v1/blobs/"+digest, app, nil, ""), http.StatusNotFound)

	// A glob over a package reaches its blob-ref kind as a write does.
	glob := appScopedToken(t, e.dbEnv, "write:"+e.repository+"/media/*")
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs", glob, []byte("another"), "image/png"), http.StatusCreated)
}

func TestAnAppsTokenThatWritesNoBlobRefStoresNoBlob(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, false)
	for name, scopes := range map[string][]string{
		"a kind without a blob-ref":         {"write:" + taskKind},
		"a blob-ref kind it only reads":     {"read:" + e.photo},
		"an internal kind through a glob":   {"write:" + e.repository + "/vault/*"},
		"a webhook and nothing to write to": {"webhook:" + appHook},
	} {
		app := appScopedToken(t, e.dbEnv, scopes...)
		rec := appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs", app, []byte("bytes"), "image/png")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: PUT /blobs = %d %s, want 403", name, rec.Code, rec.Body.String())
		}
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
			t.Errorf("%s: WWW-Authenticate = %q", name, wa)
		}
	}
	if info, err := e.ds.StatBlob(context.Background(), appDeliveryDigest([]byte("bytes"))); err == nil {
		t.Fatalf("a refused upload reached the store: %+v", info)
	}
	// An exact scope reaches an internal kind, and its blob-ref with it.
	exact := appScopedToken(t, e.dbEnv, "write:"+e.scan)
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs", exact, []byte("bytes"), "image/png"), http.StatusCreated)
}

// webhookKeyIn digs a trigger's key out of an answer's properties.
func webhookKeyIn(props map[string]any) (any, bool) {
	src, _ := props["source"].(map[string]any)
	hook, _ := src["webhook"].(map[string]any)
	k, ok := hook["key"]
	return k, ok
}

// appDeliveryOffer serves a trigger's single-record read with a live offer
// for its source carrying the key, held in one map every read hands out. An
// offer is a mapping source's, and only core could declare a mapping onto
// core's trigger kind, so the engine holds none; the shared map is how a
// read that cut the key in place would show.
type appDeliveryOffer struct {
	substrate.Dataset
	id   string
	meta map[string]substrate.PropertyMeta
}

func (d appDeliveryOffer) Get(ctx context.Context, kind, id string) (*substrate.Record, error) {
	rec, err := d.Dataset.Get(ctx, kind, id)
	if err == nil && kind == kindTrigger && id == d.id {
		rec.PropertyMeta = d.meta
	}
	return rec, err
}

func TestATriggerAnAppsTokenReadsNeverCarriesItsKey(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, false)
	meta := map[string]substrate.PropertyMeta{
		"source": {Alternatives: []substrate.PropertyAlternative{
			{Actor: vocabulary.ActorID("bundle:example.com:hooks"), Value: map[string]any{"webhook": map[string]any{"key": appHookKey}}},
		}},
	}
	e.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return appDeliveryOffer{Dataset: ds, id: appHook, meta: meta}
	}
	path := vocabulary.RecordPath(kindTrigger, appHook)
	e.put(t, e.watch, "w1", map[string]any{"trigger": map[string]any{vocabulary.ReferenceValueKey: path}})
	app := appScopedToken(t, e.dbEnv, triggerScope, "read:"+e.watch)
	noKey := func(what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		wantStatus(t, rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), appHookKey) {
			t.Fatalf("%s carried the trigger's key: %s", what, rec.Body.String())
		}
	}
	read := func(target string) *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodGet, target, app, nil, "")
	}

	get := read("/api/v1/" + path)
	noKey("a get", get)
	rec := decodeJSON[substrate.Record](t, get)
	src, _ := rec.Properties["source"].(map[string]any)
	if hook, _ := src["webhook"].(map[string]any); hook == nil || hook["headers"] == nil || rec.Properties["label"] != "Apple Health webhook" {
		t.Fatalf("the get lost more than the key: %+v", rec.Properties)
	}
	if m, ok := rec.PropertyMeta["source"]; !ok || len(m.Alternatives) != 1 {
		t.Fatalf("the get lost the offer itself: %+v", rec.PropertyMeta)
	}
	list := "/api/v1/records?filter=" + url.QueryEscape(`{"kinds":["`+kindTrigger+`"]}`)
	noKey("a list", read(list))
	noKey("a list of every kind it reads", read("/api/v1/records"))
	search := read("/api/v1/records?q=Apple")
	noKey("a search", search)
	if !strings.Contains(search.Body.String(), appHook) {
		t.Fatalf("the search found no trigger: %s", search.Body.String())
	}
	expanded := "/api/v1/records?expand=trigger&filter=" + url.QueryEscape(`{"kinds":["`+e.watch+`"]}`)
	exp := read(expanded)
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
		if rec := read(target); rec.Code != http.StatusForbidden {
			t.Errorf("%s = %d %s, want 403", name, rec.Code, rec.Body.String())
		}
	}

	// Nothing the app read was cut out of the record the engine holds or the
	// offer every read hands out, and the owner still reads it whole.
	if k, ok := webhookKeyIn(e.get(t, kindTrigger, appHook).Properties); !ok || k != appHookKey {
		t.Fatalf("the app's read cut the key out of the stored record")
	}
	if alt := meta["source"].Alternatives[0].Value.(map[string]any); alt["webhook"].(map[string]any)["key"] != appHookKey {
		t.Fatalf("the app's read cut the key out of the offer: %+v", alt)
	}
	owner := appScopedDo(t, e.dbEnv, http.MethodGet, "/api/v1/"+path, e.token, nil, "")
	wantStatus(t, owner, http.StatusOK)
	if k, ok := webhookKeyIn(decodeJSON[substrate.Record](t, owner).Properties); !ok || k != appHookKey {
		t.Fatalf("the owner's read lost the key: %s", owner.Body.String())
	}
}

// appDeliveryLookups counts the token lookups the webhook door asks for.
type appDeliveryLookups struct {
	substrate.Service
	n atomic.Int32
}

func (s *appDeliveryLookups) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	s.n.Add(1)
	return s.Service.Authenticate(ctx, secret)
}

// The door takes no bearer, so a bearer shaped like a token is a lookup
// anybody can ask for: one that finds no token is charged to its source, and
// a source that spent its pace is looked up no more until it refills.
func TestABearerThatIsNoTokenIsLookedUpAtItsSourcesPace(t *testing.T) {
	t.Parallel()
	lookups := &appDeliveryLookups{}
	e := newAppDeliveryEnv(t, false,
		withService(func(s substrate.Service) substrate.Service { lookups.Service = s; return lookups }),
		withConfig(func(c *Config) { c.AuthInterval = time.Minute }))
	app := appScopedToken(t, e.dbEnv, "webhook:"+appHook)
	hook := "/webhooks/" + e.repository + "/" + appHook
	bogus := map[string]string{"Authorization": "Bearer " + substrate.TokenSecretPrefix + "0123456789abcdef"}
	lookups.n.Store(0)
	for range 10 {
		if rec := postHook(e.h, hook, []byte(`{}`), bogus); rec.Code != http.StatusNotFound {
			t.Fatalf("a bearer that is no token = %d, want the door's 404", rec.Code)
		}
	}
	if n := lookups.n.Load(); n != authAllowance/costPaired {
		t.Fatalf("ten bearers that are no token were looked up %d times, want %d", n, authAllowance/costPaired)
	}
	// Once the source's pace refills, an app's token from it delivers again.
	e.clock.advance(time.Minute)
	rec := postHook(e.h, hook, []byte(`{"n":1}`), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + app})
	wantStatus(t, rec, http.StatusAccepted)
}

// revokingBody revokes a token the first time the upload's body is read.
type revokingBody struct {
	r      *strings.Reader
	once   sync.Once
	revoke func()
}

func (b *revokingBody) Read(p []byte) (int, error) {
	b.once.Do(b.revoke)
	return b.r.Read(p)
}

// A blob upload may wait minutes for a slot and for its body: an app's token
// revoked meanwhile stores nothing, as the webhook door delivers nothing.
func TestATokenRevokedWhileItsBlobArrivesStoresNothing(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, false)
	id, app := appDeliveryMint(t, e.ds, substrate.MintTokenInput{Scopes: []string{"write:" + e.photo}, ClientID: appClientID, ClientName: "Notes"})
	body := &revokingBody{r: strings.NewReader("a photo"), revoke: func() { appDeliveryRevoke(t, e.ds, id) }}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/blobs", body)
	req.Header.Set("Authorization", "Bearer "+app)
	req.Header.Set("Content-Type", "image/jpeg")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
	if info, err := e.ds.StatBlob(context.Background(), appDeliveryDigest([]byte("a photo"))); err == nil {
		t.Fatalf("a token revoked mid-body stored %+v", info)
	}
}

// An app's uploads wait for slots of their own: a full pool of them never
// holds the owner's back. It fills the package's one pool of app upload
// slots, so it cannot run beside another test that uploads as an app.
func TestAnAppsBlobUploadsHoldSlotsOfTheirOwn(t *testing.T) {
	e := newAppDeliveryEnv(t, false)
	app := appScopedToken(t, e.dbEnv, "write:"+e.photo)
	for range cap(appBlobPutSem) {
		appBlobPutSem <- struct{}{}
	}
	defer func() {
		for range cap(appBlobPutSem) {
			<-appBlobPutSem
		}
	}()
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs", e.token, []byte("the owner's"), "image/png"), http.StatusCreated)
	// The app's request gives up while it waits for a slot, and only then:
	// its context is canceled once a goroutine is seen parked in putBlob's
	// select with every slot of the app pool held. Had the upload taken the
	// owner's pool, which has slots free, it would never park there: it
	// stores the bytes and ends, and the wait below fails instead of a
	// cancellation racing a free slot.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPut, "/api/v1/blobs", strings.NewReader("the app's"))
	req.Header.Set("Authorization", "Bearer "+app)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.h.ServeHTTP(rec, req)
	}()
	if !waitParkedIn(t, done, "(*handler).putBlob(") {
		<-done
		t.Fatalf("the app's upload never waited for a slot of its own pool: %d %s", rec.Code, rec.Body)
	}
	cancel()
	<-done
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "blob store is busy, retry") {
		t.Fatalf("the refusal = %q, want the slot wait's", msg)
	}
	if info, err := e.ds.StatBlob(context.Background(), appDeliveryDigest([]byte("the app's"))); err == nil {
		t.Fatalf("an upload that found no slot stored %+v", info)
	}
}

// waitParkedIn waits until a goroutine is parked in a select inside the
// function frame names, and reports whether one was before the request
// ended or ten seconds passed.
func waitParkedIn(t *testing.T, done <-chan struct{}, frame string) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	buf := make([]byte, 1<<20)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return false
		default:
		}
		n := runtime.Stack(buf, true)
		for _, g := range strings.Split(string(buf[:n]), "\n\n") {
			header, _, _ := strings.Cut(g, "\n")
			if strings.Contains(header, "[select") && strings.Contains(g, frame) {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestABlobReferenceAnAppsTokenReadsCarriesNoManifestItCannotRead(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, false)
	letter := []byte("the owner's letter")
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs?name=letter-from-the-bank.pdf", e.token, letter, "application/pdf"), http.StatusCreated)
	digest := appDeliveryDigest(letter)
	e.put(t, e.photo, "p1", map[string]any{"caption": "a cat", "image": digest})
	path := "/api/v1/" + vocabulary.RecordPath(e.photo, "p1")

	// The owner's read resolves the reference to the manifest's name and
	// type, which the owner's upload gave the bytes.
	owner := appScopedDo(t, e.dbEnv, http.MethodGet, path, e.token, nil, "")
	wantStatus(t, owner, http.StatusOK)
	if !strings.Contains(owner.Body.String(), "letter-from-the-bank.pdf") {
		t.Fatalf("the owner's read carries no manifest: %s", owner.Body.String())
	}

	app := appScopedToken(t, e.dbEnv, "write:"+e.photo)
	rec := appScopedDo(t, e.dbEnv, http.MethodGet, path, app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); strings.Contains(body, "letter-from-the-bank") || strings.Contains(body, "application/pdf") {
		t.Fatalf("an app that does not read the blob kind read its manifest: %s", body)
	}
	got := decodeJSON[substrate.Record](t, rec)
	image, _ := got.Properties["image"].(map[string]any)
	if image["digest"] != digest || image["status"] != "stored" || got.Properties["caption"] != "a cat" {
		t.Fatalf("the cut took more than the manifest: %+v", got.Properties)
	}

	// A token that reads the manifests reads them through the reference too.
	reader := appScopedToken(t, e.dbEnv, "write:"+e.photo, "read:"+kindBlob)
	rec = appScopedDo(t, e.dbEnv, http.MethodGet, path, reader, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "letter-from-the-bank.pdf") {
		t.Fatalf("a token that reads the blob kind lost the manifest: %s", rec.Body.String())
	}
}
