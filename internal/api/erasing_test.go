package api

// A create at an id whose permanent delete is still erasing what the record
// owned answers 503 with the reason erasing and a Retry-After a client waits
// out on its own: the compaction pass lifts it with nobody acting.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestACreateRefusedWhileErasingIsRetriedLater(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("%w: the permanent delete of example.com/things/box/box-1 still has at least 3 of what it owned to erase", substrate.ErrErasing)
	status, p := problemFor(err)
	if status != http.StatusServiceUnavailable || p.Code != codeUnavailable || p.Reason != substrate.ReasonErasing || p.Message != err.Error() {
		t.Fatalf("the refusal maps to %d %+v, want 503 unavailable erasing with its message", status, p)
	}
	rec := httptest.NewRecorder()
	writeSubstrateError(rec, httptest.NewRequest(http.MethodPut, "/api/v1/example.com/things/box/box-1", nil), err)
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After is %q, want the minute the pass takes to come back", got)
	}
}
