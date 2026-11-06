package egress

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
)

// ErrUnprocessed marks a failed request the endpoint cannot have acted on: no
// request it made put its headers on the wire, or the endpoint answered it
// with a redirect or a 4xx refusal before doing any work. A caller that pays
// per request reads it as a request that cost nothing. Any other failure,
// a dropped connection, a timeout waiting for the answer, a 5xx, a reply that
// does not decode, may come after the endpoint did the work and billed it.
var ErrUnprocessed = errors.New("egress: the endpoint did not process the request")

// Unprocessed marks err as ErrUnprocessed. Its text stays err's own, and
// errors.Is still finds everything err wraps. A nil err stays nil.
func Unprocessed(err error) error {
	if err == nil || errors.Is(err, ErrUnprocessed) {
		return err
	}
	return unprocessedError{err}
}

type unprocessedError struct{ err error }

func (e unprocessedError) Error() string   { return e.err.Error() }
func (e unprocessedError) Unwrap() []error { return []error{e.err, ErrUnprocessed} }

// RefusedStatus reports whether an endpoint answering with status refused the
// request before acting on it: a redirect, which a CredentialClient does not
// follow, or a 4xx (a malformed request, a refused key, an unknown model, a
// rate limit). A 5xx is not one: the endpoint may have failed after the work.
func RefusedStatus(status int) bool {
	return status >= http.StatusMultipleChoices && status < http.StatusInternalServerError &&
		status != http.StatusNotModified
}

// SendWatch records whether a request made under the context WatchSends
// returned wrote its headers: before that no endpoint can have read the
// request, so nothing can have processed it. A request whose headers went out
// counts as sent even when its body then failed, so the watch errs toward sent.
type SendWatch struct{ sent atomic.Bool }

// WatchSends returns ctx with a trace that records, on the returned watch,
// every request made under it that wrote its headers. Traces compose, so a
// watch inside another still reports to both.
func WatchSends(ctx context.Context) (context.Context, *SendWatch) {
	w := &SendWatch{}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteHeaders: func() { w.sent.Store(true) },
	}), w
}

// Sent reports whether any request made under the watch's context wrote its
// headers.
func (w *SendWatch) Sent() bool { return w.sent.Load() }

// IsRedirect reports whether err is a redirect a CredentialClient refused to
// follow.
func IsRedirect(err error) bool {
	var r *RedirectError
	return errors.As(err, &r)
}
