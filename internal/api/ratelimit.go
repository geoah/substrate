package api

import (
	"context"
	"math"
	"net"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"
)

// defaultAuthInterval is the spacing the unauthenticated auth endpoints —
// registration, login and the two credential changes — allow per (client IP,
// username), per username and GLOBALLY: one ATTEMPT per interval. The global
// key is what makes a distributed attempt on many usernames cost the same as
// an attempt on one.
const defaultAuthInterval = 5 * time.Second

// An attempt is spelled in units so that a two-call GESTURE still costs one
// attempt. authAllowance is both the per-(IP,username) bucket's ceiling and
// what one interval refills; an ordinary request spends the whole of it, while
// the two calls of the registration gesture (`/register/enroll` then
// `/register`, one human action) spend half each. So the pair fires back to
// back — the console and substratectl do exactly that — while a SECOND gesture still
// waits the full interval, and login, the password change and the TOTP change
// keep the unchanged one-per-interval pacing. Loosening the pair is safe: the
// enroll call verifies the invite code and writes nothing.
const (
	authAllowance = 2
	costRequest   = authAllowance
	costPaired    = authAllowance / 2
)

// The GLOBAL bucket is a different animal from the per-(IP, username) ones. It
// exists to bound a DISTRIBUTED brute force — a spray across many usernames
// from many IPs, each of which slips past its own per-key bucket — so it must
// hold MANY attempts, not one. Sizing it at a single request's cost (the old
// `authAllowance`) made the whole substrate a one-attempt-per-interval funnel:
// two honest users logging in within 5s collided, and a poller could sit on
// the sole slot. So the global bucket's ceiling and refill are scaled up
// (globalAllowance) while a single request still costs `costRequest`: the
// spray is capped at globalBurst attempts per interval substrate-wide, an
// honest login is not throttled by someone else's.
const (
	globalKey       = "global"
	globalBurst     = 32
	globalAllowance = authAllowance * globalBurst
)

// allowanceFor is a key's bucket ceiling and per-interval refill: the global
// key holds globalBurst attempts, every other key holds one.
func allowanceFor(key string) float64 {
	if key == globalKey {
		return globalAllowance
	}
	return authAllowance
}

// maxLimiterEntries bounds the limiter map. Keys embed client-controlled
// input, so entries that can no longer refuse anything are swept and, when
// everything is still live, the oldest half is dropped.
const maxLimiterEntries = 4096

// rateLimiter is a token bucket per key: authAllowance units of capacity,
// refilled at authAllowance units per interval.
type rateLimiter struct {
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// bucket is one key's credit: the units left, and when that count was last
// brought up to date.
type bucket struct {
	units float64
	seen  time.Time
}

func newRateLimiter(interval time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{interval: interval, now: now, buckets: map[string]*bucket{}}
}

// allow charges cost units to EVERY key as one unit of work: either every key
// could pay (and every one is charged) or none is. The second return is how
// long the caller must wait when refused.
func (l *rateLimiter) allow(cost int, keys ...string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.trimLocked(now)
	var wait time.Duration
	for _, key := range keys {
		b := l.bucketLocked(key, now)
		if short := float64(cost) - b.units; short > 1e-9 {
			// Time for the missing units to accrue, at this key's refill rate.
			if w := time.Duration(short / allowanceFor(key) * float64(l.interval)); w > wait {
				wait = w
			}
		}
	}
	if wait > 0 {
		return false, wait
	}
	for _, key := range keys {
		l.buckets[key].units -= float64(cost)
	}
	return true, 0
}

// bucketLocked is one key's bucket, brought up to date: an unseen key starts
// full, and an existing one accrues what the elapsed time bought (never past
// the ceiling).
func (l *rateLimiter) bucketLocked(key string, now time.Time) *bucket {
	ceiling := allowanceFor(key)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{units: ceiling, seen: now}
		l.buckets[key] = b
		return b
	}
	if elapsed := now.Sub(b.seen); elapsed > 0 {
		b.units = math.Min(ceiling,
			b.units+ceiling*elapsed.Seconds()/l.interval.Seconds())
		b.seen = now
	}
	return b
}

// trimLocked keeps the map bounded: first entries that can no longer refuse
// anything (a full interval since the last charge is a full bucket, the
// ceiling being exactly one interval's refill), then — if the map is still at
// the cap — the oldest half.
func (l *rateLimiter) trimLocked(now time.Time) {
	if len(l.buckets) < maxLimiterEntries {
		return
	}
	for k, b := range l.buckets {
		if now.Sub(b.seen) >= l.interval {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) < maxLimiterEntries {
		return
	}
	keys := make([]string, 0, len(l.buckets))
	for k := range l.buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return l.buckets[keys[i]].seen.Before(l.buckets[keys[j]].seen) })
	for _, k := range keys[:len(keys)/2] {
		delete(l.buckets, k)
	}
}

func retryAfterSeconds(d time.Duration) int {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return s
}

// peerAddress records the transport peer, so the rate limit keys on the
// connection and never on something a request can say about itself. No
// header-rewriting middleware is installed today (see api.go), and this is
// what keeps that a local decision rather than a load-bearing one: if one is
// ever added, the limiter is already reading the address it wants.
func peerAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKeyPeer, r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// peerIP is the rate-limiting identity: the real peer address, never a
// header-derived one.
func peerIP(r *http.Request) string {
	addr, _ := r.Context().Value(ctxKeyPeer).(string)
	if addr == "" {
		addr = r.RemoteAddr
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// usernameRE is the declared username grammar; every other spelling shares
// one bucket, so unknown names buy neither map growth nor extra attempts.
var usernameRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,29}$`)

func limiterUsername(name string) string {
	if usernameRE.MatchString(name) {
		return name
	}
	return "-"
}
