package api

import (
	"context"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// peerHook is the one trigger peerHookSvc holds.
const peerHook = "applehealth-webhook"

// peerHookSvc is a webhook door holding peerHook, which admits a delivery an
// app's token is granted (substrate.WebhookGranted) and nothing else, and an
// Authenticate that knows that one token. The pace in front of the door is
// the handler's; this answers what stands behind it.
type peerHookSvc struct {
	*stubService
	app string
}

func (s *peerHookSvc) Authenticate(_ context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	if secret != s.app {
		return nil, substrate.TokenInfo{}, substrate.ErrAuth
	}
	return stubDataset{}, substrate.TokenInfo{
		ID: "app1", Scopes: []string{"webhook:" + peerHook},
		ClientID: "https://notes.example.org/client.json", ClientName: "Notes",
	}, nil
}

func (s *peerHookSvc) admit(ctx context.Context, authority, trigger string) error {
	if trigger != peerHook || !substrate.WebhookGranted(ctx, authority, trigger) {
		return substrate.ErrNotFound
	}
	return nil
}

func (s *peerHookSvc) CheckWebhook(ctx context.Context, authority, trigger, _ string) (substrate.WebhookTarget, error) {
	if err := s.admit(ctx, authority, trigger); err != nil {
		return substrate.WebhookTarget{}, err
	}
	return substrate.WebhookTarget{MaxBody: substrate.WebhookInlineDefault}, nil
}

func (s *peerHookSvc) ReceiveWebhook(ctx context.Context, authority, trigger, _ string, _ substrate.WebhookRequest) (string, error) {
	if err := s.admit(ctx, authority, trigger); err != nil {
		return "", err
	}
	return "hook-1", nil
}

// Behind a proxy every client shares the proxy's address. Once the operator
// lists it, a client spending its webhook pace on tokens that do not exist
// leaves another client's delivery through the same proxy untouched (#1114).
func TestAWebhookPaceBehindATrustedProxyIsEachClients(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trusted []netip.Prefix
		want    int
	}{
		{"trusted", []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, http.StatusAccepted},
		// The same requests with no trusted proxy: the shared pace refuses the
		// app, which is the bug the setting exists for.
		{"untrusted", nil, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &peerHookSvc{stubService: &stubService{}, app: substrate.TokenSecretPrefix + "the-apps-own-token"}
			now := time.Unix(1_000_000, 0)
			h := New(Config{Service: svc, AuthInterval: time.Minute, Now: func() time.Time { return now }, TrustedProxies: tc.trusted})
			hook := "/webhooks/" + stubRepository + "/" + peerHook
			bogus := map[string]string{
				"Authorization":   "Bearer " + substrate.TokenSecretPrefix + "0123456789abcdef",
				"X-Forwarded-For": "203.0.113.66",
			}
			for range 10 {
				postHook(h, hook, []byte(`{}`), bogus)
			}
			rec := postHook(h, hook, []byte(`{"n":1}`), map[string]string{
				"Content-Type":    "application/json",
				"Authorization":   "Bearer " + svc.app,
				"X-Forwarded-For": "198.51.100.7",
			})
			wantStatus(t, rec, tc.want)
		})
	}
}
