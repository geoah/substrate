package pushrelay

import (
	"container/list"
	"math"
	"sync"
	"time"
)

// Limits are the relay's allowances per minute, each a token bucket that
// holds one minute's worth and refills evenly. Every one is at least 1.
type Limits struct {
	// Install is the pushes per install, keyed by a digest of Apple's token,
	// charged once the signature verifies.
	Install int
	// Caller is the requests to /v1/ per caller address.
	Caller int
	// Register is the registrations per caller address.
	Register int
	// Total is the pushes in all, charged once the signature verifies.
	Total int
	// RegisterTotal is the registrations in all.
	RegisterTotal int
	// RefusedTotal is the refusals in all. Past it a refusal is answered
	// rate_limited and logged no more, which bounds what a flood of bad
	// requests costs the logs; a request that would be sent never charges
	// it and is never held by it.
	RefusedTotal int
}

// DefaultLimits are the plan's defaults.
var DefaultLimits = Limits{Install: 20, Caller: 600, Register: 30, Total: 12000, RegisterTotal: 3000, RefusedTotal: 3000}

// The limits' names, in metrics and logs.
const (
	limitInstall       = "install"
	limitCaller        = "caller"
	limitRegister      = "register"
	limitTotal         = "total"
	limitRegisterTotal = "register_total"
	limitRefusedTotal  = "refused_total"
)

// maxBuckets bounds the keyed buckets of every limit together; past it the
// least recently used is dropped, and comes back full.
const maxBuckets = 100_000

type bucket struct {
	tokens float64
	at     time.Time
}

// take spends one token of a bucket of perMinute, or says how long until
// one is there.
func (b *bucket) take(now time.Time, perMinute int) (bool, time.Duration) {
	capacity := float64(perMinute)
	rate := capacity / 60 // tokens per second
	if b.at.IsZero() {
		b.tokens, b.at = capacity, now
	}
	if elapsed := now.Sub(b.at).Seconds(); elapsed > 0 {
		b.tokens = math.Min(capacity, b.tokens+elapsed*rate)
		b.at = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / rate * float64(time.Second))
}

// limiter holds every bucket, in memory, for this replica alone.
type limiter struct {
	limits Limits
	mu     sync.Mutex
	global map[string]*bucket
	keyed  map[string]*list.Element
	order  *list.List // front is the most recently used
}

type keyedBucket struct {
	key string
	b   bucket
}

func newLimiter(l Limits) *limiter {
	return &limiter{limits: l, global: map[string]*bucket{}, keyed: map[string]*list.Element{}, order: list.New()}
}

func (l *limiter) perMinute(name string) int {
	switch name {
	case limitInstall:
		return l.limits.Install
	case limitCaller:
		return l.limits.Caller
	case limitRegister:
		return l.limits.Register
	case limitTotal:
		return l.limits.Total
	case limitRegisterTotal:
		return l.limits.RegisterTotal
	default:
		return l.limits.RefusedTotal
	}
}

// takeGlobal spends one of an unkeyed limit.
func (l *limiter) takeGlobal(name string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.global[name]
	if b == nil {
		b = &bucket{}
		l.global[name] = b
	}
	return b.take(now, l.perMinute(name))
}

// take spends one of a keyed limit's bucket for key.
func (l *limiter) take(name, key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := name + "\x00" + key
	e, ok := l.keyed[k]
	if ok {
		l.order.MoveToFront(e)
	} else {
		if l.order.Len() >= maxBuckets {
			oldest := l.order.Back()
			l.order.Remove(oldest)
			delete(l.keyed, oldest.Value.(*keyedBucket).key)
		}
		e = l.order.PushFront(&keyedBucket{key: k})
		l.keyed[k] = e
	}
	return e.Value.(*keyedBucket).b.take(now, l.perMinute(name))
}

// retryAfter is a wait as Retry-After writes it: whole seconds, at least 1.
func retryAfter(d time.Duration) int {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return s
}
