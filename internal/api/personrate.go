package api

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// personRate paces the requests each person sends, after their token is
// read: a token bucket per person (substrate.PersonOf) that holds burst
// requests and refills at rate a second. A request past it answers 429
// rate_limited with the Retry-After to wait. A rate of zero paces nothing.
// The keys are the server's own names for people, never a client's input,
// so the map holds at most one entry per person.
type personRate struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

func newPersonRate(rate float64, burst int, now func() time.Time) *personRate {
	if burst < 1 {
		burst = int(math.Ceil(rate))
	}
	return &personRate{rate: rate, burst: float64(burst), now: now, buckets: map[string]*bucket{}}
}

// allow charges one request to person, or answers how long to wait.
func (p *personRate) allow(person string) (bool, time.Duration) {
	if p == nil || p.rate <= 0 {
		return true, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	b, ok := p.buckets[person]
	if !ok {
		b = &bucket{units: p.burst, seen: now}
		p.buckets[person] = b
	} else if elapsed := now.Sub(b.seen); elapsed > 0 {
		b.units = math.Min(p.burst, b.units+p.rate*elapsed.Seconds())
		b.seen = now
	}
	if b.units < 1 {
		return false, time.Duration((1 - b.units) / p.rate * float64(time.Second))
	}
	b.units--
	return true, 0
}

// ownTokenRequest reports whether r manages the person's tokens with a token
// of the person's own, not an app's: the routes under /tokens.
func ownTokenRequest(ctx context.Context, r *http.Request) bool {
	return (r.URL.Path == "/tokens" || strings.HasPrefix(r.URL.Path, "/tokens/")) &&
		!substrate.IsAppActor(ActorFrom(ctx))
}

type pacedCtx struct{}

// withPaced marks ctx as a request already charged to its person: the MCP
// door charges the request it receives, and the requests its tools make in
// process carry the mark, so one call to the door is one request.
func withPaced(ctx context.Context) context.Context {
	return context.WithValue(ctx, pacedCtx{}, true)
}

// paced answers the request a 429 when the person it acts for has sent their
// rate's worth, and reports whether it did. ctx is the request's, after its
// token was read. A request of the person's own to manage their tokens
// (ownTokens) is paced by a bucket of its own, the same size, which no
// app's token reaches: an app that spent its person's requests cannot keep
// its person from revoking it.
func (h *handler) paced(ctx context.Context, w http.ResponseWriter, repository string, ownTokens bool) bool {
	if charged, _ := ctx.Value(pacedCtx{}).(bool); charged {
		return false
	}
	rate := h.personRate
	if ownTokens {
		rate = h.ownTokenRate
	}
	ok, wait := rate.allow(substrate.PersonOf(ctx, repository))
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
	writeError(w, http.StatusTooManyRequests, codeRateLimited,
		"too many requests: this person's requests are paced, so wait the Retry-After and send it again")
	return true
}
