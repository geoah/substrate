package engine

import (
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A parked row that is a webhook delivery still filing says so in a field,
// whether this process is filing it now or not, and a failure never does: a
// client stops matching the stored text to tell the two apart.
func TestAWebhookDeliveryStillFilingIsPendingNeverAFailure(t *testing.T) {
	t.Parallel()
	ds := &dataset{}
	filing := substrate.TriggerFailure{ID: 1, LastError: pendingWebhookError}
	ds.presentFailure(&filing)
	if !filing.Pending || filing.Running {
		t.Fatalf("a delivery still filing = %+v, want pending and not running", filing)
	}
	ds.runningClaims.Store(int64(2), struct{}{})
	running := substrate.TriggerFailure{ID: 2, LastError: pendingWebhookError}
	ds.presentFailure(&running)
	if !running.Pending || !running.Running {
		t.Fatalf("a delivery this process is filing = %+v, want pending and running", running)
	}
	for _, lastError := range []string{"the callable refused it", inFlightError} {
		failed := substrate.TriggerFailure{ID: 3, LastError: lastError}
		ds.presentFailure(&failed)
		if failed.Pending {
			t.Fatalf("a parked failure %q reads as pending", lastError)
		}
	}
}
