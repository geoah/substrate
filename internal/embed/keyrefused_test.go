package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/providersecret"
)

// An answer that refuses the key is marked with its status, so the drain can
// tell a refused key from a refused input or an endpoint that is down. A 403
// that refuses one request (a region, a model, a missing permission,
// moderation) carries no mark, so one such answer does not mark the
// operator's key failing for every repository. The mark carries no key.
func TestEmbedMarksARefusedKeyWithItsStatus(t *testing.T) {
	t.Parallel()
	const key = "zq-refused-key-fixture"
	for _, c := range []struct {
		name    string
		status  int
		body    string
		refused bool
	}{
		{
			"401 invalid_api_key", http.StatusUnauthorized,
			`{"error":{"message":"Incorrect API key provided: ` + key + `","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`, true,
		},
		{"401 with no error body", http.StatusUnauthorized, `unauthorized`, true},
		{
			"403 invalid_api_key", http.StatusForbidden,
			`{"error":{"message":"refused ` + key + `","type":"invalid_request_error","code":"invalid_api_key"}}`, true,
		},
		{
			"403 authentication_error", http.StatusForbidden,
			`{"error":{"message":"refused ` + key + `","type":"authentication_error"}}`, true,
		},
		{
			"403 region", http.StatusForbidden,
			`{"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}`, false,
		},
		{
			"403 missing scope", http.StatusForbidden,
			`{"error":{"message":"You have insufficient permissions for this operation. Missing scopes: model.request.","type":"invalid_request_error","param":null,"code":null}}`, false,
		},
		{
			"403 moderation", http.StatusForbidden,
			`{"error":{"code":403,"message":"Your chosen model requires moderation and your input was flagged"}}`, false,
		},
		{"403 with no error body", http.StatusForbidden, `forbidden`, false},
		{"400", http.StatusBadRequest, `{"error":{"message":"refused ` + key + `","type":"invalid_request_error","code":"invalid_api_key"}}`, false},
		{"429", http.StatusTooManyRequests, `{"error":{"message":"slow down","type":"rate_limit_error"}}`, false},
		{"500", http.StatusInternalServerError, `{"error":{"message":"oops","type":"api_error"}}`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			t.Cleanup(srv.Close)
			client, err := New(Config{BaseURL: srv.URL, APIKey: key, Model: DefaultModel})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Embed(context.Background(), []string{"anything"})
			if err == nil {
				t.Fatal("no error")
			}
			want := 0
			if c.refused {
				want = c.status
			}
			if got := providersecret.KeyRefusal(err); got != want {
				t.Fatalf("KeyRefusal = %d, want %d: %v", got, want, err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatalf("the error carries the key: %v", err)
			}
		})
	}
}
