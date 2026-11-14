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

// errRolesChanged ends a stream whose person's roles changed since it opened:
// it reads through the view those roles built (personView). The token
// stands, so it is no ErrAuth: the client opens the stream again, on a view
// of the roles held now. A watch ends without a terminal frame, as a drain
// does, and both clients reconnect from their cursor.
var errRolesChanged = fmt.Errorf("%w: the roles of the person this token acts for changed since the stream opened: open it again", substrate.ErrUnavailable)

// errLeaseUnread ends a stream whose token or roles could not be read again:
// what was read when it opened is no answer now (decision 0276: a read of
// the rules that fails is an error, never a wider answer). It is no
// ErrAuth: the token may stand, and the client opens the stream again.
var errLeaseUnread = fmt.Errorf("%w: the token or the roles it is held to could not be read again: open the stream again", substrate.ErrUnavailable)

func withTokenLease(ctx context.Context, l *tokenLease) context.Context {
	return context.WithValue(ctx, ctxKeyLease, l)
}

// leaseFrom is the request's lease, nil for a request no token opened.
func leaseFrom(ctx context.Context) *tokenLease {
	l, _ := ctx.Value(ctxKeyLease).(*tokenLease)
	return l
}

// alive answers nil while the token stands and its person holds the roles
// the stream opened under. The expiry is checked on every call; the record
// and the roles are read again when the last read is older than every, so
// zero reads them every time. A refusal ends the lease `auth`, a change of
// the roles and a read that could not be made (a database that did not
// answer) `unavailable`: no stream goes on under an answer it cannot read
// again.
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
		switch {
		case errors.Is(err, substrate.ErrAuth):
			return errLeaseEnded
		case errors.Is(err, errRolesChanged):
			return errRolesChanged
		}
		return fmt.Errorf("%w: %w", errLeaseUnread, err)
	}
	l.mu.Lock()
	l.checked = now
	l.mu.Unlock()
	return nil
}

// leaseTransferEvery is how stale a long transfer's last token read may be
// (an export, a blob's bytes): it reads the lease between its writes, and a
// read per write of a large body buys nothing a one-second window does not.
const leaseTransferEvery = time.Second

// leaseAgentEvery is how stale an agent stream's last token read may be.
// Its events are the app's own chat, one model token each, so a database read
// per event buys nothing a two-second window does not; a watch reads the
// record before every batch, since its rows reach past the app's own writes.
const leaseAgentEvery = 2 * time.Second
