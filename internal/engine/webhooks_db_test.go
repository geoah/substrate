package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// hookEchoSource writes what the delivery envelope carried: the body text or
// the transcription part, one header, the mode, the method, the fire id and
// the first file part's blob digest. Every assertion below reads that widget.
var hookEchoSource = `
WIDGET = "` + widgetType + `"

def echo(input, host, rid):
    env = input.get("envelope") or {}
    req = env.get("request") or {}
    body = (req.get("body") or {}).get("text") or ""
    blob = ""
    val = ""
    for p in req.get("parts") or []:
        if p.get("blob"):
            blob = p["blob"]
        if p.get("name") == "transcription":
            val = p.get("value") or ""
    headers = req.get("headers") or {}
    host.effects.put(WIDGET, rid, properties={
        "name": body or val,
        "want": headers.get("x-test") or "",
        "mode": input.get("mode") or "",
        "target": blob,
        "op": req.get("method") or "",
        "record": (env.get("fire") or {}).get("id") or "",
    })
    return {"output": {}}

def main(input, host):
    return echo(input, host, "hook-echo")
`

// hookGatedSource fails until a widget named gate exists, then echoes: the
// first delivery parks, and the retry proves the parked payload came back.
var hookGatedSource = hookEchoSource + `
def main(input, host):
    if host.records.get(WIDGET, "gate") is None:
        raise RuntimeError("gate closed")
    return echo(input, host, "gated-echo")
`

const hookKey = "0123456789abcdef-XYZ"

func hookTrigger(id string, source map[string]any, fn string, enabled bool) enginetest.Trigger {
	return enginetest.Trigger{
		ID: id,
		Properties: map[string]any{
			"enabled":  enabled,
			"source":   source,
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/function", fnAuthority+"/"+fn),
		},
	}
}

func webhookSource(key string) map[string]any {
	arm := map[string]any{}
	if key != "" {
		arm["key"] = key
	}
	return map[string]any{"webhook": arm}
}

func jsonHook(body, header string) substrate.WebhookRequest {
	return substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"x-test": header, "content-type": "application/json"},
		Query:   map[string][]string{},
		Body:    []byte(body),
	}
}

// newHookDataset is newFnDataset keeping the SERVICE too: the door is a
// service method, since a public request carries no dataset.
func newHookDataset(t *testing.T, triggers []enginetest.Trigger, fns ...map[string]any) (substrate.Service, substrate.Dataset, fnOps) {
	t.Helper()
	svc, ds := newDataset(t)
	if err := enginetest.Install(context.Background(), ds, owner, fnConnector(triggers, fns...)); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	ops, ok := ds.(fnOps)
	if !ok {
		t.Fatal("dataset does not implement the automation seam")
	}
	return svc, ds, ops
}

func hookEcho(t *testing.T, ds substrate.Dataset, id string) map[string]any {
	t.Helper()
	rec, err := ds.Get(context.Background(), widgetType, id)
	if err != nil {
		t.Fatalf("the delivery wrote nothing at %s: %v", id, err)
	}
	return rec.Properties
}

// One dataset, the whole door: an open endpoint delivers the request, a keyed
// one checks its key, every refusal is the same ErrNotFound, a multipart
// request spools its file part to the blob store, and the status row carries
// the path.
func TestWebhookDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, ops := newHookDataset(t,
		[]enginetest.Trigger{
			hookTrigger("hook-open", webhookSource(""), "hookecho", true),
			hookTrigger("hook-keyed", webhookSource(hookKey), "hookecho", true),
			hookTrigger("hook-off", webhookSource(""), "hookecho", false),
			hookTrigger("hook-record", map[string]any{"record": map[string]any{"kinds": []any{gadgetType}}}, "hooknoop", true),
		},
		pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource),
		pyFn("hooknoop", map[string]any{}, []any{}, "def main(input, host):\n    return {}\n"),
	)

	t.Run("open endpoint delivers the request", func(t *testing.T) {
		fid, err := engine.ReceiveWebhookSync(ctx, svc, "geoah.example.com", "hook-open", "", jsonHook(`{"hello":"wörld"}`, "yes"))
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if !strings.HasPrefix(fid, "hook-") {
			t.Fatalf("fire id %q does not carry the public prefix", fid)
		}
		got := hookEcho(t, ds, "hook-echo")
		want := map[string]any{"name": `{"hello":"wörld"}`, "want": "yes", "mode": "webhook", "op": "POST", "record": fid, "target": ""}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %v, want %v", k, got[k], v)
			}
		}
	})

	t.Run("a keyed endpoint checks its key", func(t *testing.T) {
		for _, key := range []string{"", "wrong-key-wrong-key", hookKey + "x"} {
			if _, err := engine.ReceiveWebhookSync(ctx, svc, "geoah.example.com", "hook-keyed", key, jsonHook("no", "no")); !errors.Is(err, substrate.ErrNotFound) {
				t.Fatalf("key %q: err = %v, want ErrNotFound", key, err)
			}
		}
		if _, err := engine.ReceiveWebhookSync(ctx, svc, "geoah.example.com", "hook-keyed", hookKey, jsonHook("keyed", "k")); err != nil {
			t.Fatalf("the right key was refused: %v", err)
		}
		if got := hookEcho(t, ds, "hook-echo")["name"]; got != "keyed" {
			t.Fatalf("keyed delivery wrote %v", got)
		}
	})

	t.Run("every refusal is not found", func(t *testing.T) {
		cases := map[string][2]string{
			"unknown authority": {"nobody.example.com", "hook-open"},
			// The door takes the authority alone: the username is not a valid
			// path segment here.
			"the username":      {"geoah", "hook-open"},
			"unknown trigger":   {"geoah.example.com", "hook-missing"},
			"disabled trigger":  {"geoah.example.com", "hook-off"},
			"a record trigger":  {"geoah.example.com", "hook-record"},
			"the wrong hat key": {"geoah.example.com", "hook-keyed"},
		}
		for name, c := range cases {
			if _, err := engine.ReceiveWebhookSync(ctx, svc, c[0], c[1], "", jsonHook("x", "x")); !errors.Is(err, substrate.ErrNotFound) {
				t.Errorf("%s: err = %v, want ErrNotFound", name, err)
			}
		}
	})

	t.Run("a multipart request spools its file part", func(t *testing.T) {
		audio := []byte{0, 0, 0, 0x1c, 'f', 't', 'y', 'p', 'M', '4', 'A', ' ', 1, 2, 3}
		req := substrate.WebhookRequest{
			Method: "POST", ContentType: "multipart/form-data",
			Headers: map[string]string{"x-test": "multi"},
			Parts: []substrate.WebhookPart{
				{Name: "transcription", Value: "buy milk"},
				{Name: "audio", Filename: "recording.m4a", MediaType: "audio/mp4", Data: audio},
			},
		}
		if _, err := engine.ReceiveWebhookSync(ctx, svc, "geoah.example.com", "hook-open", "", req); err != nil {
			t.Fatalf("receive: %v", err)
		}
		got := hookEcho(t, ds, "hook-echo")
		if got["name"] != "buy milk" {
			t.Fatalf("transcription part = %v", got["name"])
		}
		digest, _ := got["target"].(string)
		if !strings.HasPrefix(digest, "blob-sha256-") {
			t.Fatalf("file part carried %q, want a blob digest", digest)
		}
		_, data, err := blobStoreOf(t, ds).GetBlob(ctx, digest)
		if err != nil {
			t.Fatalf("spooled blob unreadable: %v", err)
		}
		if string(data) != string(audio) {
			t.Fatal("spooled bytes differ from the part")
		}
		manifest, err := ds.Get(ctx, "core.substrate.reamde.dev/blob", digest)
		if err != nil {
			t.Fatalf("manifest: %v", err)
		}
		if manifest.Properties["name"] != "recording.m4a" || manifest.Properties["mediaType"] != "audio/mp4" {
			t.Fatalf("manifest = %v", manifest.Properties)
		}
		if manifest.Properties["createdBy"] != "substrate.webhook" {
			t.Fatalf("createdBy = %v, want the door's actor", manifest.Properties["createdBy"])
		}
	})

	t.Run("status carries the path", func(t *testing.T) {
		statuses, err := ops.TriggerStatuses(ctx)
		if err != nil {
			t.Fatal(err)
		}
		paths := map[string]string{}
		for _, st := range statuses {
			paths[st.ID] = st.WebhookPath
		}
		if paths["hook-open"] != "/webhooks/geoah.example.com/hook-open" || paths["hook-keyed"] != "/webhooks/geoah.example.com/hook-keyed" {
			t.Fatalf("webhook paths = %v", paths)
		}
		if paths["hook-record"] != "" {
			t.Fatalf("a record trigger reports a webhook path: %q", paths["hook-record"])
		}
	})

	t.Run("the door itself fires in the background", func(t *testing.T) {
		rc, ok := svc.(substrate.WebhookReceiver)
		if !ok {
			t.Fatal("service does not implement the webhook seam")
		}
		if _, err := rc.ReceiveWebhook(ctx, "geoah.example.com", "hook-open", "", jsonHook("detached", "bg")); err != nil {
			t.Fatalf("receive: %v", err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			rec, err := ds.Get(ctx, widgetType, "hook-echo")
			if err == nil && rec.Properties["name"] == "detached" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the detached fire never landed; last = %v", rec)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	t.Run("a bad key is refused at write time", func(t *testing.T) {
		callable := vocabulary.RecordPath("core.substrate.reamde.dev/function", fnAuthority+"/hookecho")
		for name, arm := range map[string]map[string]any{
			"short key":     {"key": "tooshort"},
			"bad alphabet":  {"key": "0123456789abcdef/../etc"},
			"unknown field": {"secret": hookKey},
		} {
			_, err := ds.Put(ctx, owner, substrate.PutInput{
				Kind: "core.substrate.reamde.dev/trigger",
				Properties: map[string]any{
					"source":   map[string]any{"webhook": arm},
					"callable": callable,
				},
			})
			if err == nil {
				t.Errorf("%s: landed", name)
			}
		}
	})
}

// A parked public delivery keeps its request: the retry re-delivers the same
// envelope, and the blob its file part spooled survives GC until then.
func TestWebhookParkedRetryReplaysRequest(t *testing.T) {
	// BlobUploadGrace is process-wide, so this test is not parallel.
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	svc, ds, ops := newHookDataset(t,
		[]enginetest.Trigger{hookTrigger("hook-gated", webhookSource(""), "hookgated", true)},
		pyFn("hookgated", map[string]any{
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{widgetType}}},
		}, []any{widgetType}, hookGatedSource),
	)

	audio := []byte("not really audio, but bytes all the same")
	req := substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data",
		Headers: map[string]string{"x-test": "parked"},
		Parts: []substrate.WebhookPart{
			{Name: "transcription", Value: "call the dentist"},
			{Name: "audio", MediaType: "audio/mp4", Data: audio},
		},
	}
	fid, err := engine.ReceiveWebhookSync(ctx, svc, "geoah.example.com", "hook-gated", "", req)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if _, err := ds.Get(ctx, widgetType, "gated-echo"); err == nil {
		t.Fatal("the gated body wrote through a closed gate")
	}
	failures, err := ops.TriggerFailures(ctx, "hook-gated")
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].FireID != fid {
		t.Fatalf("parked failures = %+v, want one for fire %s", failures, fid)
	}

	// The spooled blob is referenced by nothing but the parked payload, and
	// GC leaves it alone for that reason.
	bs := blobStoreOf(t, ds)
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	var digest string
	for _, m := range listBlobDigests(t, ds) {
		digest = m
	}
	if digest == "" {
		t.Fatal("the spooled blob's manifest is gone")
	}
	if _, data, err := bs.GetBlob(ctx, digest); err != nil || string(data) != string(audio) {
		t.Fatalf("parked payload's blob was collected: %v", err)
	}

	// Open the gate; the retry carries the original request.
	mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, ID: "gate", Properties: map[string]any{"name": "open"}})
	if _, err := ops.RetryTriggerFailure(ctx, "hook-gated", failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got := hookEcho(t, ds, "gated-echo")
	if got["name"] != "call the dentist" || got["want"] != "parked" || got["target"] != digest || got["record"] != fid {
		t.Fatalf("retried delivery echoed %v", got)
	}
	if left, err := ops.TriggerFailures(ctx, "hook-gated"); err != nil || len(left) != 0 {
		t.Fatalf("failures after retry = %v, %v", left, err)
	}
	// With the park gone and the digest held by a plain string property only,
	// the blob is an orphan again and GC takes it.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, _, err := bs.GetBlob(ctx, digest); err == nil {
		t.Fatal("orphan blob survived gc once nothing parked named it")
	}
}

// listBlobDigests lists the live blob manifests' ids.
func listBlobDigests(t *testing.T, ds substrate.Dataset) []string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/blob"}}, First: 50,
	})
	if err != nil {
		t.Fatalf("list blobs: %v", err)
	}
	var out []string
	for _, r := range page.Records {
		if r.DeletedAt == nil {
			out = append(out, r.ID)
		}
	}
	return out
}
