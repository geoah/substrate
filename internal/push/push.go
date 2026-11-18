// Package push is the server's one call out on its own initiative
// (docs/plans/push.md): one signed request to the relay a token's verified
// app's publisher names, and nothing else. It reads where that relay is
// (Relay), builds and signs the request with a substrate.PushSigner it is
// given, and classifies the relay's answer; it never sees the keys behind
// the signer, and it retries nothing.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/build"
	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/substrate"
)

// Timeout bounds one request to a relay, its answer read.
const Timeout = 5 * time.Second

// maxAnswer bounds what is read back from a relay.
const maxAnswer = 4 << 10

var (
	// ErrNoRelay is a token whose app names no relay: no app at all (a
	// password sign-in's, a console's), an app whose publisher was not
	// verified or whose access went to a loopback address, or a document
	// that names none for it.
	ErrNoRelay = errors.New("no relay")
	// ErrUnreadable is an authority document that could not be read just
	// now.
	ErrUnreadable = errors.New("the authority document could not be read")
)

// Documents reads an authority's document, from the reader's cache when a
// success is fresh (authority.Reader).
type Documents interface {
	Read(ctx context.Context, authority string, fresh bool) (authority.Source, error)
}

// Relay is the relay the server sends tok's pushes to: the one the document
// of the token's verified app's authority names for the token's client id
// (decision record 0209's rule: an unverified or a local app has none).
func Relay(ctx context.Context, docs Documents, tok substrate.TokenInfo) (string, error) {
	if docs == nil || tok.ClientID == "" || tok.AppAuthority == "" || tok.AppLocal {
		return "", ErrNoRelay
	}
	src, err := docs.Read(ctx, tok.AppAuthority, false)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	app, ok := src.Document.App(tok.ClientID)
	if !ok {
		return "", ErrNoRelay
	}
	relay := app.PushRelay()
	if relay == "" {
		return "", ErrNoRelay
	}
	if _, err := pushwire.Endpoint(relay, pushwire.PathPush); err != nil {
		return "", ErrNoRelay
	}
	return relay, nil
}

// Outcome is what became of one send.
type Outcome string

const (
	// Sent is a 200 from the relay.
	Sent Outcome = "sent"
	// Delete is an answer that says the device will never be sent to again
	// under the key it holds: Apple says the install is gone, or the relay
	// refused the key, its signature, its topic or its environment.
	Delete Outcome = "deleted"
	// Limited is the relay's rate limit.
	Limited Outcome = "limited"
	// Failed is everything else: the relay unreachable, a refusal that is a
	// clock's or a bug's, Apple unreachable behind it.
	Failed Outcome = "failed"
)

// The relay's codes that delete the device (docs/plans/push.md section 1).
var deletingCodes = map[string]bool{
	"gone": true, "signature_refused": true, "key_refused": true, "key_expired": true,
	"topic_refused": true, "environment_refused": true,
}

// Answer is one send's classified answer.
type Answer struct {
	Outcome Outcome
	// Status is the relay's HTTP status, 0 when none came back, and Code
	// the code of its error envelope.
	Status int
	Code   string
	// Err is why a send failed before an answer was read.
	Err error
}

// Client posts pushes.
type Client struct {
	http *http.Client
	now  func() time.Time
}

// Options configures a Client; every field may be zero.
type Options struct {
	// HTTP is the client requests go out on. Nil is egress's: public
	// addresses only unless the operator's SUBSTRATE_EGRESS_ALLOW lists
	// one, no redirect followed, Timeout.
	HTTP *http.Client
	Now  func() time.Time
}

// New builds a Client.
func New(o Options) *Client {
	c := &Client{http: o.HTTP, now: o.Now}
	if c.http == nil {
		c.http = egress.CredentialClient(egress.Transport(), Timeout)
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// Send posts one push to relay, signed by signer, and classifies the
// answer. Nothing is retried.
func (c *Client) Send(ctx context.Context, relay string, signer substrate.PushSigner, req pushwire.PushRequest) Answer {
	endpoint, err := pushwire.Endpoint(relay, pushwire.PathPush)
	if err != nil {
		return Answer{Outcome: Failed, Err: err}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Answer{Outcome: Failed, Err: err}
	}
	if len(body) > pushwire.MaxBody {
		return Answer{Outcome: Failed, Err: fmt.Errorf("a push body of %d bytes is past %d", len(body), pushwire.MaxBody)}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Answer{Outcome: Failed, Err: err}
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("User-Agent", "substrate/"+build.Version())
	hr.Header.Set(pushwire.SignatureHeader, signer.Sign(c.now(), body))
	start := time.Now()
	res, err := c.http.Do(hr)
	if err != nil {
		metrics.PushRelaySeconds.Observe(time.Since(start).Seconds())
		return Answer{Outcome: Failed, Err: err}
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	metrics.PushRelaySeconds.Observe(time.Since(start).Seconds())
	return classify(res.StatusCode, raw)
}

// classify reads a relay's answer by the table of section 1.
func classify(status int, raw []byte) Answer {
	a := Answer{Status: status}
	if status == http.StatusOK {
		a.Outcome = Sent
		return a
	}
	var env pushwire.ErrorResponse
	if json.Unmarshal(raw, &env) == nil {
		a.Code = env.Error.Code
	}
	switch {
	case deletingCodes[a.Code]:
		a.Outcome = Delete
	case status == http.StatusTooManyRequests || a.Code == "rate_limited":
		a.Outcome = Limited
	default:
		a.Outcome = Failed
	}
	return a
}

// Badge counts the person's unread `chat` threads that reads keeps, to
// substrate.PushBadgeMax: the one rule a push and GET /api/v1/push/badge
// both count by. reads is the asking token's view.
func Badge(ctx context.Context, store substrate.PushStore, reads func(context.Context, *substrate.Record) (bool, error), person string) (int, error) {
	threads, err := store.PushBadgeThreads(ctx, person)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, th := range threads {
		ok, err := reads(ctx, th)
		if err != nil {
			return 0, err
		}
		if ok {
			if n++; n == substrate.PushBadgeMax {
				break
			}
		}
	}
	return n, nil
}
