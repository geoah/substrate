package embed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/providersecret"
)

// A 401 or a 403 is the endpoint refusing the key, marked with its status so
// the drain can tell a refused key from a refused input or an endpoint that
// is down. No other status carries the mark, and the mark carries no key.
func TestEmbedMarksARefusedKeyWithItsStatus(t *testing.T) {
	t.Parallel()
	const key = "zq-refused-key-fixture"
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = fmt.Fprintf(w, `{"error":{"message":"refused %s","type":"invalid_request_error"}}`, key)
			}))
			t.Cleanup(srv.Close)
			c, err := New(Config{BaseURL: srv.URL, APIKey: key, Model: DefaultModel})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Embed(context.Background(), []string{"anything"})
			if err == nil {
				t.Fatal("no error")
			}
			want := 0
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				want = status
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
