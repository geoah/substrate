package push

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/apns"
	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/pushrelay"
	"github.com/geoah/substrate/internal/pushwire"
)

// fakeApple stands in for Apple's push service behind the relay.
type fakeApple struct {
	mu     sync.Mutex
	got    []apns.Notification
	answer apns.Response
}

func (a *fakeApple) Send(_ context.Context, n apns.Notification) (apns.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.got = append(a.got, n)
	return a.answer, nil
}

// A push the server signs goes through the relay's own handler to Apple,
// and the relay's answers come back classified: sent, a key another
// signer's push cannot use, and an install Apple says is gone. No call
// leaves the test.
func TestAPushThroughTheRelayToApple(t *testing.T) {
	t.Parallel()
	seal := make([]byte, pushrelay.SealKeySize)
	if _, err := rand.Read(seal); err != nil {
		t.Fatal(err)
	}
	sealer, err := pushrelay.NewSealer(seal, nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	apple := &fakeApple{answer: apns.Response{Status: 200, ID: "apns-id"}}
	relay, err := pushrelay.New(pushrelay.Config{
		Topics: []string{"dev.example.chat"},
		Apple:  map[string]pushrelay.Sender{pushwire.EnvironmentProduction: apple},
		Sealer: sealer,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(relay)
	t.Cleanup(srv.Close)
	client := New(Options{HTTP: egress.CredentialClient(srv.Client().Transport, Timeout)})

	signer := newKeySigner()
	generation := make([]byte, pushwire.GenerationSize)
	if _, err := rand.Read(generation); err != nil {
		t.Fatal(err)
	}
	key, err := sealer.Seal(pushrelay.PushKey{
		Platform: pushwire.PlatformAPNs, Token: strings.Repeat("5f", 32), Topic: "dev.example.chat",
		Environment: pushwire.EnvironmentProduction, PublicKey: pushwire.Encode(signer.PublicKey()),
		Generation: pushwire.Encode(generation), Issued: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := pushwire.PushRequest{PushKey: key, Event: pushwire.EventReply, Thread: "8f2c41d07a", Badge: 3, EventID: strings.Repeat("e", pushwire.MinEventID)}
	if a := client.Send(context.Background(), srv.URL, signer, req); a.Outcome != Sent {
		t.Fatalf("a signed push: %+v", a)
	}
	apple.mu.Lock()
	if len(apple.got) != 1 {
		t.Fatalf("Apple got %d notifications", len(apple.got))
	}
	var payload struct {
		Substrate struct {
			Event   string `json:"event"`
			Thread  string `json:"thread"`
			EventID string `json:"eventId"`
		} `json:"substrate"`
	}
	if err := json.Unmarshal(apple.got[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	apple.mu.Unlock()
	if payload.Substrate.Event != req.Event || payload.Substrate.Thread != req.Thread || payload.Substrate.EventID != req.EventID {
		t.Fatalf("Apple's payload: %+v", payload)
	}

	// Another signer's push under this key is refused and deletes the
	// device, and so is an install Apple says is gone.
	req.EventID = strings.Repeat("f", pushwire.MinEventID)
	other := sha256.Sum256([]byte("another push test signer"))
	if a := client.Send(context.Background(), srv.URL, keySigner{key: ed25519.NewKeyFromSeed(other[:])}, req); a.Outcome != Delete || a.Code != pushwire.CodeSignatureRefused {
		t.Fatalf("another signer's push: %+v", a)
	}
	apple.mu.Lock()
	apple.answer = apns.Response{Status: 410, Reason: "Unregistered"}
	apple.mu.Unlock()
	req.EventID = strings.Repeat("g", pushwire.MinEventID)
	if a := client.Send(context.Background(), srv.URL, signer, req); a.Outcome != Delete || a.Code != pushwire.CodeGone {
		t.Fatalf("a push to an install Apple says is gone: %+v", a)
	}
}
