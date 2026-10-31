package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A token is checked once, at the door, and most requests end within
// seconds. A stream does not: a change feed watch or an agent stream an
// app's token opened would carry on after the owner revoked the token or its
// approved lifetime ran out (decision 0195). The lease is that token's
// standing for the rest of the stream: its expiry, checked on every call, and
// its record, read again through Authenticate, so a revoked token stops the
// stream before it writes anything more.
type tokenLease struct {
	recheck func(context.Context) error
	expires *time.Time
	now     func() time.Time

	mu      sync.Mutex
	checked time.Time
}

// errLeaseEnded ends a stream whose token no longer stands. It is an
// ErrAuth, so the terminal frame and a stream's error event say `auth`, and a
// client signs in again rather than retrying.
var errLeaseEnded = fmt.Errorf("%w: the token was revoked or its approved time is up", substrate.ErrAuth)

func withTokenLease(ctx context.Context, l *tokenLease) context.Context {
	return context.WithValue(ctx, ctxKeyLease, l)
}

// leaseFrom is the request's lease, nil for a token the door does not lease
// (a full-access token, which has no approved lifetime and is the owner's).
func leaseFrom(ctx context.Context) *tokenLease {
	l, _ := ctx.Value(ctxKeyLease).(*tokenLease)
	return l
}

// alive answers nil while the token stands. The expiry is checked on every
// call; the record is read again when the last read is older than every, so
// zero reads it every time.
func (l *tokenLease) alive(ctx context.Context, every time.Duration) error {
	if l == nil {
		return nil
	}
	now := l.now()
	if l.expires != nil && !now.Before(*l.expires) {
		return errLeaseEnded
	}
	l.mu.Lock()
	due := every <= 0 || now.Sub(l.checked) >= every
	l.mu.Unlock()
	if !due {
		return nil
	}
	if err := l.recheck(ctx); err != nil {
		return errLeaseEnded
	}
	l.mu.Lock()
	l.checked = now
	l.mu.Unlock()
	return nil
}

// leaseAgentEvery is how stale an agent stream's last token read may be.
// Its events are the app's own chat, one model token each, so a database read
// per event buys nothing a two-second window does not; a watch reads the
// record before every batch, since its rows reach past the app's own writes.
const leaseAgentEvery = 2 * time.Second
