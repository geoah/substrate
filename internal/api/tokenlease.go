package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A token is checked once, at the door, and most requests end within
// seconds. A stream does not: a watch or an agent stream would carry on
// after its token was revoked, the person signed out everywhere, or the
// token's lifetime ran out (decisions 0195 and 0272). The lease is that
// token's standing for the rest of the stream: its expiry, checked on every
// call, and its record, read again through Authenticate (a read of the token
// cache, which a token write empties), so a dead token stops the stream
// before it writes anything more. Every stream reads it again whenever its
// repository's changelog moves, which a revoke and a sign-out do.
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

// leaseFrom is the request's lease, nil for a request no token opened.
func leaseFrom(ctx context.Context) *tokenLease {
	l, _ := ctx.Value(ctxKeyLease).(*tokenLease)
	return l
}

// alive answers nil while the token stands. The expiry is checked on every
// call; the record is read again when the last read is older than every, so
// zero reads it every time. Only a refusal ends the lease: a read that could
// not be made (a database that did not answer) leaves the stream running,
// and the next call reads again.
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
		if errors.Is(err, substrate.ErrAuth) {
			return errLeaseEnded
		}
		return nil
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
