package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestATokenLeaseEndsAtItsExpiryAndOnARefusedRead(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := func() time.Time { return now }
	expires := now.Add(time.Minute)
	reads := 0
	refuse := false
	lease := &tokenLease{expires: &expires, now: clock, checked: now, recheck: func(context.Context) error {
		reads++
		if refuse {
			return substrate.ErrAuth
		}
		return nil
	}}
	ctx := context.Background()

	if err := lease.alive(ctx, 2*time.Second); err != nil || reads != 0 {
		t.Fatalf("a fresh lease = %v after %d reads, want standing without a read", err, reads)
	}
	now = now.Add(3 * time.Second)
	if err := lease.alive(ctx, 2*time.Second); err != nil || reads != 1 {
		t.Fatalf("a stale lease = %v after %d reads, want one read", err, reads)
	}
	if err := lease.alive(ctx, 0); err != nil || reads != 2 {
		t.Fatalf("every=0 = %v after %d reads, want a read every call", err, reads)
	}
	refuse = true
	if err := lease.alive(ctx, 0); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a refused read = %v, want ErrAuth", err)
	}
	refuse = false
	now = expires
	if err := lease.alive(ctx, time.Hour); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("at its expiry the lease = %v, want ErrAuth without waiting for a read", err)
	}
	var none *tokenLease
	if err := none.alive(ctx, 0); err != nil {
		t.Fatalf("a full-access token's absent lease = %v", err)
	}
}
