package pushwire_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/pushrelay"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/pushwire/pushwiretest"
)

var update = flag.Bool("update", false, "rewrite testdata/ from the code")

// The values every fixture is made from. None is anybody's key: each is
// derived from a label, so the fixtures are the same on every machine.
var (
	fixtureNow        = time.Unix(1763459123, 0)
	fixtureRegistered = time.Unix(1763459000, 0)
	fixtureTopic      = "dev.reamde.substrate.chat"
	fixtureThread     = "8f2c41d07a"
)

var stream = pushwiretest.Stream

type world struct {
	keys   pushwiretest.Keys
	sealer *pushrelay.Sealer
	signer ed25519.PrivateKey
}

func newWorld(t *testing.T) world {
	t.Helper()
	sealKey := stream("seal key", pushrelay.SealKeySize)
	signer := ed25519.NewKeyFromSeed(stream("signing seed", ed25519.SeedSize))
	other := ed25519.NewKeyFromSeed(stream("other seed", ed25519.SeedSize))
	sealer, err := pushrelay.NewSealer(sealKey, nil, pushwiretest.Nonces())
	if err != nil {
		t.Fatal(err)
	}
	return world{
		keys: pushwiretest.Keys{
			Now:            fixtureNow.Unix(),
			RegisteredAt:   fixtureRegistered.Unix(),
			SealKey:        base64.StdEncoding.EncodeToString(sealKey),
			SigningSeed:    pushwire.Encode(signer.Seed()),
			PublicKey:      pushwire.Encode(signer.Public().(ed25519.PublicKey)),
			OtherSeed:      pushwire.Encode(other.Seed()),
			OtherPublicKey: pushwire.Encode(other.Public().(ed25519.PublicKey)),
			Topic:          fixtureTopic,
		},
		sealer: sealer,
		signer: signer,
	}
}

// golden writes v to testdata/name under -update and otherwise fails when
// the file differs from it.
func golden(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, b) {
		t.Errorf("%s is not what the code makes: go test ./internal/pushwire -run TestFixtures -update, and read the diff", path)
	}
}

func compact(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// install is one registration the fixtures push to.
type install struct {
	token, environment, generation string
}

func (w world) seal(t *testing.T, in install) (string, pushwiretest.Sealed) {
	t.Helper()
	s := pushwiretest.Sealed{
		Token: in.token, Topic: fixtureTopic, Environment: in.environment,
		PublicKey: w.keys.PublicKey, Generation: in.generation, Issued: fixtureRegistered.Unix(),
	}
	key, err := w.sealer.Seal(pushrelay.PushKey{
		Platform: pushwire.PlatformAPNs, Token: s.Token, Topic: s.Topic, Environment: s.Environment,
		PublicKey: s.PublicKey, Generation: s.Generation, Issued: s.Issued,
	})
	if err != nil {
		t.Fatal(err)
	}
	return key, s
}

func (w world) push(t *testing.T, key string, sealed pushwiretest.Sealed, p pushwire.PushRequest) pushwiretest.Push {
	t.Helper()
	p.PushKey = key
	if err := p.Check(); err != nil {
		t.Fatalf("a fixture the wire refuses: %v", err)
	}
	body := compact(t, p)
	n := pushwire.ApplePush(p, sealed.Generation, fixtureNow)
	return pushwiretest.Push{
		Now:       fixtureNow.Unix(),
		Key:       sealed,
		Signature: pushwire.Sign(w.signer, fixtureNow, []byte(body)),
		Body:      body,
		Apple: pushwiretest.Apple{
			Environment: sealed.Environment,
			Path:        "/3/device/" + sealed.Token,
			Headers:     n.Headers(sealed.Topic),
			Payload:     n.Payload,
		},
		Response: pushwiretest.Answer{Status: http.StatusOK, Body: json.RawMessage(compact(t, pushwire.PushResponse{Status: pushwire.StatusSent}))},
	}
}

// TestFixtures makes every fixture from the code and holds testdata/ to it;
// -update rewrites testdata/. The files are the contract the server and the
// apps test against, so a diff here is a change of the wire: read it.
func TestFixtures(t *testing.T) {
	w := newWorld(t)
	golden(t, "keys.json", w.keys)

	// One install on production, whose key every push but the largest
	// carries. Its register fixture is the request that made that key.
	phone := install{
		token:       hex.EncodeToString(stream("apple token", 32)),
		environment: pushwire.EnvironmentProduction,
		generation:  pushwire.Encode(stream("generation", pushwire.GenerationSize)),
	}
	reg := pushwire.RegisterRequest{
		Platform: pushwire.PlatformAPNs, Token: phone.token, Topic: fixtureTopic,
		Environment: phone.environment, PublicKey: w.keys.PublicKey, Generation: phone.generation,
	}
	if err := reg.Check(); err != nil {
		t.Fatal(err)
	}
	key, sealed := w.seal(t, phone)
	golden(t, "register.json", pushwiretest.Register{
		Request:  json.RawMessage(compact(t, reg)),
		Response: pushwiretest.Answer{Status: http.StatusOK, Body: json.RawMessage(compact(t, pushwire.RegisterResponse{PushKey: key}))},
	})

	eventID := func(label string, n int) string { return pushwire.Encode(stream(label, n)) }
	golden(t, "push-reply.json", w.push(t, key, sealed, pushwire.PushRequest{
		Event: pushwire.EventReply, Thread: fixtureThread, Badge: 3, EventID: eventID("event reply", 60),
	}))
	golden(t, "push-failed.json", w.push(t, key, sealed, pushwire.PushRequest{
		Event: pushwire.EventFailed, Thread: fixtureThread, Badge: 1, EventID: eventID("event failed", 60),
	}))
	// Two cards waiting on one thread: two pushes, one thread-id, two event
	// ids, and no collapse id: two requests Apple does not collapse.
	golden(t, "push-request-card-1.json", w.push(t, key, sealed, pushwire.PushRequest{
		Event: pushwire.EventRequest, Thread: fixtureThread, Badge: 4, EventID: eventID("event card 1", 60),
	}))
	golden(t, "push-request-card-2.json", w.push(t, key, sealed, pushwire.PushRequest{
		Event: pushwire.EventRequest, Thread: fixtureThread, Badge: 5, EventID: eventID("event card 2", 60),
	}))
	golden(t, "push-read.json", w.push(t, key, sealed, pushwire.PushRequest{
		Event: pushwire.EventRead, Thread: fixtureThread, Badge: 2, EventID: eventID("event read", 60),
	}))

	// Every bound at its largest: Apple's longest token, a development
	// install, a 128-character thread (too long to collapse by), the
	// highest badge and a 1,024-character event id. It still fits the
	// relay's body and Apple's payload.
	largest := install{
		token:       hex.EncodeToString(stream("apple token largest", pushwire.MaxToken/2)),
		environment: pushwire.EnvironmentDevelopment,
		generation:  pushwire.Encode(stream("generation largest", pushwire.GenerationSize)),
	}
	lkey, lsealed := w.seal(t, largest)
	golden(t, "push-reply-largest.json", w.push(t, lkey, lsealed, pushwire.PushRequest{
		Event: pushwire.EventReply, Thread: "t" + strings.Repeat("0123456789abcdef", 8)[:127], Badge: pushwire.MaxBadge,
		EventID: eventID("event largest", pushwire.MaxEventID*3/4),
	}))

	golden(t, "bad-requests.json", badRequests(t, reg, key))

	var refusals []pushwiretest.Refusal
	for _, r := range []pushrelay.Refusal{
		pushrelay.BadRequest("eventId is missing"), pushrelay.StaleSignature, pushrelay.SignatureRefused,
		pushrelay.KeyRefused, pushrelay.KeyExpired, pushrelay.TopicRefused, pushrelay.EnvironmentRefused,
		pushrelay.Replayed, pushrelay.Gone, pushrelay.TooLarge, pushrelay.RateLimited, pushrelay.Upstream,
		pushrelay.Internal,
	} {
		refusals = append(refusals, pushwiretest.Refusal{Status: r.Status, Body: r.Body()})
	}
	golden(t, "errors.json", refusals)

	golden(t, "signatures.json", signatureVectors(t, w))

	var prints []pushwiretest.Fingerprint
	for _, id := range []string{
		eventID("fingerprint short", 24), eventID("event reply", 60), eventID("event card 1", 60),
		eventID("event card 2", 60), eventID("event largest", pushwire.MaxEventID*3/4),
	} {
		prints = append(prints, pushwiretest.Fingerprint{EventID: id, Fingerprint: pushwire.EventFingerprint(id)})
	}
	golden(t, "fingerprints.json", prints)

	var endpoints []pushwiretest.Endpoint
	for _, relay := range endpointRelays {
		e := pushwiretest.Endpoint{Relay: relay}
		reg, err1 := pushwire.Endpoint(relay, pushwire.PathRegister)
		push, err2 := pushwire.Endpoint(relay, pushwire.PathPush)
		if err1 != nil || err2 != nil {
			e.Refused = true
		} else {
			e.Register, e.Push = reg, push
		}
		endpoints = append(endpoints, e)
	}
	golden(t, "endpoints.json", endpoints)
}

// endpointRelays are the relays endpoints.json answers for: the plan's
// table, then each thing a relay may not be.
var endpointRelays = []string{
	"https://push.example.com",
	"https://push.example.com/",
	"https://example.com/push",
	"https://example.com/push/",
	"https://push.example.com:8443",
	"https://push.example.com:000443",
	"https://push.example.com:65535",
	"https://example.com/push%20relay",
	"http://push.example.com",
	"HTTPS://push.example.com",
	"https://push.example.com?x=1",
	"https://push.example.com?",
	"https://push.example.com#top",
	"https://user@push.example.com",
	"https://push.example.com:0",
	"https://push.example.com:65536",
	"https://push.example.com:",
	"https://example.com/%ZZ",
	"https://example.com/push%",
	"https://ex%41mple.com",
	"https://%C3%A9xample.com",
	"https://[::1]:8443/push",
	"https://[::1",
	"https:push.example.com",
	"https:///push",
	"https://push.example.com/a b",
	"",
}

func signatureVectors(t *testing.T, w world) pushwiretest.Signatures {
	t.Helper()
	body := `{"pushKey":"v1.fixture","event":"reply","thread":"8f2c41d07a","badge":3,"eventId":"` + pushwire.Encode(stream("signature event", 48)) + `"}`
	other := strings.Replace(body, `"badge":3`, `"badge":4`, 1)
	header := pushwire.Sign(w.signer, fixtureNow, []byte(body))
	ts := fixtureNow.Unix()
	sig := header[strings.Index(header, ",s=")+3:]
	c := func(name, header, body, pub string, now int64, verify, relay string) pushwiretest.SignatureCase {
		return pushwiretest.SignatureCase{Name: name, Header: header, Body: body, PublicKey: pub, Now: now, Verify: verify, Relay: relay}
	}
	pub, otherPub := w.keys.PublicKey, w.keys.OtherPublicKey
	return pushwiretest.Signatures{
		Seed:        w.keys.SigningSeed,
		PublicKey:   pub,
		T:           ts,
		Body:        body,
		SignedBytes: string(pushwire.SignedBytes(fixtureNow, []byte(body))),
		Header:      header,
		Cases: []pushwiretest.SignatureCase{
			c("signed now", header, body, pub, ts, "ok", ""),
			c("the relay's clock 300 s ahead", header, body, pub, ts+300, "ok", ""),
			c("the relay's clock 300 s behind", header, body, pub, ts-300, "ok", ""),
			c("stale: the relay's clock 301 s ahead", header, body, pub, ts+301, "stale", ""),
			c("stale: the relay's clock 301 s behind", header, body, pub, ts-301, "stale", ""),
			c("another key", header, body, otherPub, ts, "refused", ""),
			c("a changed body", header, other, pub, ts, "refused", ""),
			c("another time over the same signature", fmt.Sprintf("t=%d,s=%s", ts+1, sig), body, pub, ts, "refused", ""),
			c("no signature", "", body, pub, ts, "refused", ""),
			c("the parts in another order", "s="+sig+fmt.Sprintf(",t=%d", ts), body, pub, ts, "refused", ""),
			c("a padded signature", header+"==", body, pub, ts, "refused", ""),
			c("a repeat: the first request again, within the window", header, body, pub, ts+10, "ok", pushwire.CodeReplayed),
		},
	}
}

// badRequests are bodies the wire refuses: each value in a second spelling
// the standard base64 decoder would take, or outside its closed set.
func badRequests(t *testing.T, reg pushwire.RegisterRequest, key string) []pushwiretest.BadRequest {
	t.Helper()
	var out []pushwiretest.BadRequest
	register := func(name string, change func(*pushwire.RegisterRequest)) {
		r := reg
		change(&r)
		out = append(out, pushwiretest.BadRequest{Name: name, Route: "register", Body: compact(t, r)})
	}
	push := func(name string, change func(*pushwire.PushRequest)) {
		p := pushwire.PushRequest{PushKey: key, Event: pushwire.EventReply, Thread: fixtureThread, Badge: 1, EventID: pushwire.Encode(stream("bad event", 48))}
		change(&p)
		out = append(out, pushwiretest.BadRequest{Name: name, Route: "push", Body: compact(t, p)})
	}
	register("a generation broken over two lines", func(r *pushwire.RegisterRequest) { r.Generation = r.Generation[:11] + "\n" + r.Generation[11:] })
	register("a public key broken by CR LF", func(r *pushwire.RegisterRequest) { r.PublicKey = r.PublicKey[:20] + "\r\n" + r.PublicKey[20:] })
	register("a padded generation", func(r *pushwire.RegisterRequest) { r.Generation += "==" })
	register("a generation in standard base64", func(r *pushwire.RegisterRequest) {
		r.Generation = strings.NewReplacer("-", "+", "_", "/").Replace(pushwire.Encode([]byte{0xfb, 0xff, 0xfe, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}))
	})
	register("an unknown platform", func(r *pushwire.RegisterRequest) { r.Platform = "fcm" })
	register("an unknown environment", func(r *pushwire.RegisterRequest) { r.Environment = "sandbox" })
	register("a token that is not hex", func(r *pushwire.RegisterRequest) { r.Token = strings.Repeat("zz", 32) })
	push("an event id that reaches 32 characters only by its line breaks", func(p *pushwire.PushRequest) {
		p.EventID = pushwire.Encode(stream("short event", 22)) + "\n\n"
	})
	push("an event id broken over two lines", func(p *pushwire.PushRequest) { p.EventID = p.EventID[:32] + "\n" + p.EventID[32:] })
	push("an unknown event", func(p *pushwire.PushRequest) { p.Event = "typing" })
	push("a thread that is no record id", func(p *pushwire.PushRequest) { p.Thread = "a b" })
	push("a badge over 999", func(p *pushwire.PushRequest) { p.Badge = 1000 })
	return out
}
