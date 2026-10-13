package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countArgon swaps the derivation for one that records how many run at once
// and holds each long enough for the callers to pile up. Not parallel: it
// replaces a package variable.
func countArgon(t *testing.T) (peak *atomic.Int32) {
	t.Helper()
	var running atomic.Int32
	peak = new(atomic.Int32)
	derive := argonIDKey
	argonIDKey = func(password, salt []byte, times, memory uint32, threads uint8, keyLen uint32) []byte {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return make([]byte, keyLen)
	}
	t.Cleanup(func() { argonIDKey = derive })
	return peak
}

// A burst of logins, registrations and password changes runs at most
// cap(argonSlots) argon2id derivations at once, whichever path each takes:
// the hash a registration writes and the check a login runs, against a real
// hash or the unknown repository's dummy, share one bound.
func TestArgonDerivationsShareOneBound(t *testing.T) {
	peak := countArgon(t)
	ctx := context.Background()
	const callers = 16
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			if i%2 == 0 {
				if _, err := hashPassword(ctx, "a-good-passphrase"); err != nil {
					t.Errorf("hash: %v", err)
				}
				return
			}
			verifyPassword(ctx, dummyPasswordHash, "a-good-passphrase")
		})
	}
	wg.Wait()
	limit := int32(cap(argonSlots))
	if got := peak.Load(); got > limit {
		t.Fatalf("%d argon2id derivations ran at once, the bound is %d", got, limit)
	}
	if limit < 1 || limit > 4 {
		t.Fatalf("the bound is %d, want 1 to 4", limit)
	}
}

// A caller that leaves while every slot is taken derives nothing: the hash
// answers its context's error and the check verifies false.
func TestArgonWaitEndsWithTheCaller(t *testing.T) {
	countArgon(t)
	for range cap(argonSlots) {
		argonSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range cap(argonSlots) {
			<-argonSlots
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := hashPassword(ctx, "a-good-passphrase"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hash with every slot taken: err = %v, want the context's", err)
	}
	if verifyPassword(ctx, dummyPasswordHash, "anything") {
		t.Fatal("a check that never ran verified true")
	}
}
