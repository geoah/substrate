package pushwire_test

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/pushwire/pushwiretest"
)

// sameJSON fails unless a and b are the same JSON value.
func sameJSON(t *testing.T, what string, a, b []byte) {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if !reflect.DeepEqual(x, y) {
		t.Errorf("%s:\n got %s\nwant %s", what, a, b)
	}
}

// Every request, response and envelope in testdata/ decodes through the
// wire's types and encodes back to the same JSON, a push body to the very
// bytes that were signed.
func TestEveryFixtureRoundTripsThroughTheTypes(t *testing.T) {
	var reg pushwiretest.Register
	pushwiretest.Read(t, "register.json", &reg)
	r, err := pushwire.DecodeRegister(reg.Request)
	if err != nil {
		t.Fatalf("register.json's request: %v", err)
	}
	b, _ := json.Marshal(r)
	sameJSON(t, "register request", b, reg.Request)
	var rr pushwire.RegisterResponse
	if err := json.Unmarshal(reg.Response.Body, &rr); err != nil || rr.PushKey == "" || len(rr.PushKey) > pushwire.MaxPushKey {
		t.Fatalf("register.json's response: %+v, %v", rr, err)
	}
	b, _ = json.Marshal(rr)
	sameJSON(t, "register response", b, reg.Response.Body)

	for _, name := range pushwiretest.PushNames {
		f := pushwiretest.LoadPush(t, name)
		p, err := pushwire.DecodePush([]byte(f.Body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b, _ := json.Marshal(p); string(b) != f.Body {
			t.Errorf("%s: the body encodes back as %s", name, b)
		}
		var resp pushwire.PushResponse
		if err := json.Unmarshal(f.Response.Body, &resp); err != nil || resp.Status != pushwire.StatusSent {
			t.Errorf("%s: response %s", name, f.Response.Body)
		}
	}

	var refusals []pushwiretest.Refusal
	pushwiretest.Read(t, "errors.json", &refusals)
	seen := map[string]bool{}
	for _, r := range refusals {
		var e pushwire.ErrorResponse
		if err := json.Unmarshal(r.Body, &e); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(e)
		sameJSON(t, "refusal", b, r.Body)
		seen[e.Error.Code] = true
	}
	for _, code := range pushwire.Codes {
		if !seen[code] {
			t.Errorf("errors.json has no envelope for %s", code)
		}
	}
	if len(seen) != len(pushwire.Codes) {
		t.Errorf("errors.json has codes outside the closed set: %v", seen)
	}
}

// The Apple requests in the fixtures are held to the plan's rules here, not
// to the code that wrote them: the headers per event, the collapse id, the
// app's words by key, and nothing a person reads taken from the request.
func TestTheApplePushesFollowThePlan(t *testing.T) {
	loc := map[string]string{
		pushwire.EventReply: pushwire.LocKeyReply, pushwire.EventFailed: pushwire.LocKeyFailed,
		pushwire.EventRequest: pushwire.LocKeyRequest,
	}
	for _, name := range pushwiretest.PushNames {
		t.Run(name, func(t *testing.T) {
			f := pushwiretest.LoadPush(t, name)
			p, err := pushwire.DecodePush([]byte(f.Body))
			if err != nil {
				t.Fatal(err)
			}
			h := f.Apple.Headers
			if h["apns-topic"] != f.Key.Topic || h["apns-push-type"] != "alert" {
				t.Errorf("topic or push type: %v", h)
			}
			if f.Apple.Path != "/3/device/"+f.Key.Token {
				t.Errorf("path %s", f.Apple.Path)
			}
			lifetime, priority := pushwire.AlertLifetime, "10"
			if p.Event == pushwire.EventRead {
				lifetime, priority = pushwire.ReadLifetime, "5"
			}
			if h["apns-priority"] != priority || h["apns-expiration"] != strconv.FormatInt(f.Now+int64(lifetime/time.Second), 10) {
				t.Errorf("priority or expiration: %v", h)
			}
			collapse, has := h["apns-collapse-id"]
			wantCollapse := (p.Event == pushwire.EventReply || p.Event == pushwire.EventFailed) && len(p.Thread) <= pushwire.MaxCollapseID
			if has != wantCollapse || (has && collapse != p.Thread) {
				t.Errorf("collapse id %q (present %v), want present %v", collapse, has, wantCollapse)
			}

			var payload struct {
				APS       map[string]any            `json:"aps"`
				Substrate pushwire.SubstratePayload `json:"substrate"`
			}
			if err := json.Unmarshal(f.Apple.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			want := pushwire.SubstratePayload{V: 1, Event: p.Event, Thread: p.Thread, G: f.Key.Generation, EventID: p.EventID}
			if payload.Substrate != want {
				t.Errorf("substrate %+v, want %+v", payload.Substrate, want)
			}
			if payload.APS["badge"] != float64(p.Badge) {
				t.Errorf("badge %v, want %d", payload.APS["badge"], p.Badge)
			}
			if p.Event == pushwire.EventRead {
				if len(payload.APS) != 2 || payload.APS["content-available"] != float64(1) {
					t.Errorf("a read's aps is %v", payload.APS)
				}
				return
			}
			wantAPS := map[string]any{
				"alert": map[string]any{"loc-key": loc[p.Event]}, "badge": float64(p.Badge), "sound": "default",
				"mutable-content": float64(1), "thread-id": p.Thread,
			}
			if !reflect.DeepEqual(payload.APS, wantAPS) {
				t.Errorf("aps %v, want %v", payload.APS, wantAPS)
			}
		})
	}
}

// Two cards waiting on one thread are two alerts: one thread-id to group
// them, two event ids, and no collapse id, which would let Apple drop the
// first before its extension ever saw its event id.
func TestTwoCardsOnOneThreadAreTwoAlerts(t *testing.T) {
	a := pushwiretest.LoadPush(t, "push-request-card-1.json")
	b := pushwiretest.LoadPush(t, "push-request-card-2.json")
	pa, _ := pushwire.DecodePush([]byte(a.Body))
	pb, _ := pushwire.DecodePush([]byte(b.Body))
	if pa.Event != pushwire.EventRequest || pb.Event != pushwire.EventRequest || pa.Thread != pb.Thread {
		t.Fatalf("the two cards are %+v and %+v", pa, pb)
	}
	if pa.EventID == pb.EventID {
		t.Error("the two cards share an event id")
	}
	for _, f := range []pushwiretest.Push{a, b} {
		if _, ok := f.Apple.Headers["apns-collapse-id"]; ok {
			t.Errorf("a card carries apns-collapse-id: %v", f.Apple.Headers)
		}
		var payload struct {
			APS struct {
				ThreadID string `json:"thread-id"`
			} `json:"aps"`
		}
		_ = json.Unmarshal(f.Apple.Payload, &payload)
		if payload.APS.ThreadID != pa.Thread {
			t.Errorf("thread-id %q, want %q", payload.APS.ThreadID, pa.Thread)
		}
	}
}

// The largest push the wire allows fits the relay's body and Apple's 4 KiB
// payload, with a 1,024-character event id.
func TestTheLargestPushFits(t *testing.T) {
	f := pushwiretest.LoadPush(t, "push-reply-largest.json")
	p, err := pushwire.DecodePush([]byte(f.Body))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.EventID) != pushwire.MaxEventID || len(f.Key.Token) != pushwire.MaxToken || p.Badge != pushwire.MaxBadge || len(p.Thread) != 128 {
		t.Fatalf("not the largest: event id %d, token %d, badge %d, thread %d", len(p.EventID), len(f.Key.Token), p.Badge, len(p.Thread))
	}
	if len(f.Body) > pushwire.MaxBody {
		t.Errorf("the body is %d bytes, over %d", len(f.Body), pushwire.MaxBody)
	}
	if len(f.Apple.Payload) > 4096 {
		t.Errorf("the Apple payload is %d bytes, over Apple's 4096", len(f.Apple.Payload))
	}
}

func TestSignatureVectors(t *testing.T) {
	var v pushwiretest.Signatures
	pushwiretest.Read(t, "signatures.json", &v)
	seed, _ := pushwire.Decode(v.Seed)
	key := ed25519.NewKeyFromSeed(seed)
	at := time.Unix(v.T, 0)
	if got := pushwire.Sign(key, at, []byte(v.Body)); got != v.Header {
		t.Errorf("Sign = %s, want %s", got, v.Header)
	}
	if got := string(pushwire.SignedBytes(at, []byte(v.Body))); got != v.SignedBytes {
		t.Errorf("SignedBytes = %q, want %q", got, v.SignedBytes)
	}
	if !strings.HasPrefix(v.SignedBytes, "substrate-push-v1\n"+strconv.FormatInt(v.T, 10)+"\n") {
		t.Errorf("signed bytes %q", v.SignedBytes)
	}
	for _, c := range v.Cases {
		pub, _ := pushwire.Decode(c.PublicKey)
		err := pushwire.Verify(c.Header, pub, time.Unix(c.Now, 0), []byte(c.Body))
		got := "ok"
		switch {
		case errors.Is(err, pushwire.ErrStale):
			got = "stale"
		case errors.Is(err, pushwire.ErrRefused):
			got = "refused"
		case err != nil:
			got = err.Error()
		}
		if got != c.Verify {
			t.Errorf("%s: Verify = %s, want %s", c.Name, got, c.Verify)
		}
	}
}

// A time a second outside the window is stale even with a signature that
// would verify, and the window is checked before the signature.
func TestVerifyChecksTheWindowFirst(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{}`)
	h := pushwire.Sign(key, now, body)
	if err := pushwire.Verify(h, other.Public().(ed25519.PublicKey), now.Add(301*time.Second), body); !errors.Is(err, pushwire.ErrStale) {
		t.Fatalf("a stale signature under another key = %v, want ErrStale", err)
	}
	if err := pushwire.Verify(h, key.Public().(ed25519.PublicKey), now.Add(300*time.Second+500*time.Millisecond), body); !errors.Is(err, pushwire.ErrStale) {
		t.Fatalf("300.5 s late = %v, want ErrStale", err)
	}
	for _, bad := range []string{"t=,s=x", "t=01,s=" + h[strings.Index(h, ",s=")+3:], "t=-1,s=x", "t=1", "t=99999999999999999999,s=x"} {
		if err := pushwire.Verify(bad, key.Public().(ed25519.PublicKey), now, body); !errors.Is(err, pushwire.ErrRefused) {
			t.Errorf("%q = %v, want ErrRefused", bad, err)
		}
	}
}

func TestEndpoint(t *testing.T) {
	var rows []pushwiretest.Endpoint
	pushwiretest.Read(t, "endpoints.json", &rows)
	for _, r := range rows {
		reg, err1 := pushwire.Endpoint(r.Relay, pushwire.PathRegister)
		push, err2 := pushwire.Endpoint(r.Relay, pushwire.PathPush)
		if r.Refused {
			if err1 == nil || err2 == nil {
				t.Errorf("%q: Endpoint = %q, %q; want refused", r.Relay, reg, push)
			}
			continue
		}
		if err1 != nil || err2 != nil || reg != r.Register || push != r.Push {
			t.Errorf("%q: Endpoint = %q, %q (%v, %v); want %q, %q", r.Relay, reg, push, err1, err2, r.Register, r.Push)
		}
	}
	// The plan's table, row by row.
	for relay, want := range map[string]string{
		"https://push.example.com":  "https://push.example.com/v1/push",
		"https://push.example.com/": "https://push.example.com/v1/push",
		"https://example.com/push":  "https://example.com/push/v1/push",
		"https://example.com/push/": "https://example.com/push/v1/push",
	} {
		if got, err := pushwire.Endpoint(relay, pushwire.PathPush); err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", relay, got, err, want)
		}
	}
	long := "https://push.example.com/" + strings.Repeat("a", pushwire.MaxRelay)
	if _, err := pushwire.Endpoint(long, pushwire.PathPush); err == nil {
		t.Error("a relay over 2,048 characters is accepted")
	}
	if !pushwire.SameRelay("https://push.example.com", "https://push.example.com/") ||
		pushwire.SameRelay("https://push.example.com", "https://example.com/push") ||
		pushwire.SameRelay("http://push.example.com", "http://push.example.com") {
		t.Error("SameRelay")
	}
}

func TestEventFingerprintVectors(t *testing.T) {
	var rows []pushwiretest.Fingerprint
	pushwiretest.Read(t, "fingerprints.json", &rows)
	seen := map[string]bool{}
	for _, r := range rows {
		got := pushwire.EventFingerprint(r.EventID)
		if got != r.Fingerprint || len(got) != 22 {
			t.Errorf("EventFingerprint(%.20s…) = %s, want %s", r.EventID, got, r.Fingerprint)
		}
		if seen[got] {
			t.Errorf("two event ids share the fingerprint %s", got)
		}
		seen[got] = true
	}
}

func TestDecodeRefusesWhatTheWireDoesNotAllow(t *testing.T) {
	f := pushwiretest.LoadPush(t, "push-reply.json")
	var good map[string]any
	if err := json.Unmarshal([]byte(f.Body), &good); err != nil {
		t.Fatal(err)
	}
	with := func(k string, v any) []byte {
		m := map[string]any{}
		for key, val := range good {
			m[key] = val
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return b
	}
	accepted := map[string][]byte{
		"as signed":                      []byte(f.Body),
		"an unknown key":                 with("priority", "high"),
		"badge 0":                        with("badge", 0),
		"badge 999":                      with("badge", 999),
		"an event id of 32":              with("eventId", strings.Repeat("A", 32)),
		"an event id of 1,024":           with("eventId", strings.Repeat("A", 1024)),
		"a thread of 128":                with("thread", strings.Repeat("a", 128)),
		"the local namespace's id shape": with("thread", "~/a"),
	}
	for name, body := range accepted {
		if _, err := pushwire.DecodePush(body); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	refused := map[string][]byte{
		"not JSON":               []byte(`{`),
		"an array":               []byte(`[]`),
		"null":                   []byte(`null`),
		"trailing data":          []byte(f.Body + `{}`),
		"no badge":               with("badge", nil),
		"a null badge":           []byte(strings.Replace(f.Body, `"badge":3`, `"badge":null`, 1)),
		"a badge as a string":    with("badge", "3"),
		"a fractional badge":     []byte(strings.Replace(f.Body, `"badge":3`, `"badge":3.5`, 1)),
		"a negative badge":       with("badge", -1),
		"badge 1000":             with("badge", 1000),
		"no push key":            with("pushKey", nil),
		"an empty push key":      with("pushKey", ""),
		"a push key of 1,025":    with("pushKey", strings.Repeat("a", 1025)),
		"an unknown event":       with("event", "typing"),
		"a thread with a space":  with("thread", "a b"),
		"a thread of 129":        with("thread", strings.Repeat("a", 129)),
		"an event id of 31":      with("eventId", strings.Repeat("A", 31)),
		"an event id of 1,025":   with("eventId", strings.Repeat("A", 1025)),
		"a padded event id":      with("eventId", strings.Repeat("A", 34)+"=="),
		"an event id in base64":  with("eventId", strings.Repeat("+", 32)),
		"an event key in caps":   []byte(strings.Replace(f.Body, `"event"`, `"Event"`, 1)),
		"an event id of numbers": with("eventId", 12345),
	}
	for name, body := range refused {
		if _, err := pushwire.DecodePush(body); !errors.Is(err, pushwire.ErrBadRequest) {
			t.Errorf("%s: %v, want ErrBadRequest", name, err)
		}
	}

	var reg pushwiretest.Register
	pushwiretest.Read(t, "register.json", &reg)
	var r map[string]any
	_ = json.Unmarshal(reg.Request, &r)
	rwith := func(k string, v any) []byte {
		m := map[string]any{}
		for key, val := range r {
			m[key] = val
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return b
	}
	for name, body := range map[string][]byte{
		"an unknown key":       rwith("model", "iPhone"),
		"a token in caps":      rwith("token", strings.ToUpper(r["token"].(string))),
		"a token of 400":       rwith("token", strings.Repeat("ab", 200)),
		"development":          rwith("environment", "development"),
		"a topic with a dash":  rwith("topic", "dev.example-app.chat"),
		"a token of 32":        rwith("token", strings.Repeat("a", 32)),
		"the largest topic":    rwith("topic", strings.Repeat("a", pushwire.MaxTopic)),
		"another public key":   rwith("publicKey", pushwire.Encode(make([]byte, 32))),
		"another generation":   rwith("generation", pushwire.Encode(make([]byte, 16))),
		"a key the relay adds": rwith("v", 2),
	} {
		if _, err := pushwire.DecodeRegister(body); err != nil {
			t.Errorf("register, %s: %v", name, err)
		}
	}
	for name, body := range map[string][]byte{
		"fcm":                       rwith("platform", "fcm"),
		"no platform":               rwith("platform", nil),
		"an unknown environment":    rwith("environment", "sandbox"),
		"a token of 31":             rwith("token", strings.Repeat("a", 31)),
		"a token of 401":            rwith("token", strings.Repeat("a", 401)),
		"a token that is not hex":   rwith("token", strings.Repeat("g", 64)),
		"no topic":                  rwith("topic", ""),
		"a topic with a slash":      rwith("topic", "dev/chat"),
		"a topic over 155":          rwith("topic", strings.Repeat("a", pushwire.MaxTopic+1)),
		"a public key of 31 bytes":  rwith("publicKey", pushwire.Encode(make([]byte, 31))),
		"a padded public key":       rwith("publicKey", pushwire.Encode(make([]byte, 32))+"="),
		"a generation of 17 bytes":  rwith("generation", pushwire.Encode(make([]byte, 17))),
		"a generation in base64":    rwith("generation", "++++++++++++++++++++++"),
		"a public key as a number":  rwith("publicKey", 7),
		"no generation":             rwith("generation", nil),
		"the body is not an object": []byte(`"apns"`),
	} {
		if _, err := pushwire.DecodeRegister(body); !errors.Is(err, pushwire.ErrBadRequest) {
			t.Errorf("register, %s: %v, want ErrBadRequest", name, err)
		}
	}
}
