package llm

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/providersecret"
)

// A provider call that fails is sorted into a class where the wire can tell:
// the adapters rebuild every provider error as scrubbed text, so the class is
// the fact about the failure a caller acts on without reading a vendor's
// words. Each wire sorts its own vendor's status, error type and error code
// in one function (openaiFailure, anthropicFailure), and nothing above this
// package matches on a vendor's prose.

// FailureClass is what kind of failure a provider call ended in. The empty
// class is a failure the wire could not sort.
type FailureClass string

const (
	// FailureKeyRefused is the vendor refusing the key the request carried
	// (providersecret.RefusesKey).
	FailureKeyRefused FailureClass = "key_refused"
	// FailureRateLimited is the vendor limiting requests or tokens for a
	// while; the failure may carry how long it asked the caller to wait.
	FailureRateLimited FailureClass = "rate_limited"
	// FailureBilling is an account out of credit or over the quota it pays
	// for: waiting does not lift it, paying does.
	FailureBilling FailureClass = "billing"
	// FailureOverloaded is the vendor shedding load.
	FailureOverloaded FailureClass = "overloaded"
	// FailureUnavailable is a fault on the vendor's side, a timeout, or a
	// connection that failed or dropped.
	FailureUnavailable FailureClass = "unavailable"
	// FailureModelUnavailable is a model the vendor does not offer to this
	// key: one it does not know, or one the key may not use.
	FailureModelUnavailable FailureClass = "model_unavailable"
	// FailureContextTooLong is a request past the model's context window or
	// the vendor's request size. ErrContextTooLong is the narrower fact the
	// loop compacts on.
	FailureContextTooLong FailureClass = "context_too_long"
	// FailureRejected is the vendor refusing the request itself: a parameter
	// the model does not take, a tool schema it cannot read, content it will
	// not process. The same request meets the same answer.
	FailureRejected FailureClass = "rejected"
)

// failure marks an error with its class and the wait the vendor named.
type failure struct {
	class      FailureClass
	retryAfter time.Duration
	err        error
}

func (f *failure) Error() string { return f.err.Error() }
func (f *failure) Unwrap() error { return f.err }

// classified marks err with class, and with retryAfter where the vendor named
// a wait. An empty class leaves err as it was. The mark keeps err whole, so a
// wire marks the error it has already scrubbed.
func classified(class FailureClass, retryAfter time.Duration, err error) error {
	if err == nil {
		return nil
	}
	// A context overflow is read off the scrubbed text (providerError), which
	// knows more phrasings of it than any one vendor's type or code says.
	if errors.Is(err, ErrContextTooLong) {
		class = FailureContextTooLong
	}
	if class == "" {
		return err
	}
	return &failure{class: class, retryAfter: max(0, retryAfter), err: err}
}

// FailureOf is the class of the provider failure err carries, and the wait
// the vendor asked for (0 for none). A refused key and a context overflow
// are classes whichever mark carries them.
func FailureOf(err error) (FailureClass, time.Duration) {
	if err == nil {
		return "", 0
	}
	if providersecret.KeyRefusal(err) != 0 {
		return FailureKeyRefused, 0
	}
	var f *failure
	if errors.As(err, &f) {
		return f.class, f.retryAfter
	}
	if errors.Is(err, ErrContextTooLong) {
		return FailureContextTooLong, 0
	}
	return "", 0
}

// retryAfterOf reads the wait a response asks for: retry-after-ms, else
// retry-after in seconds or as an HTTP date. Both vendors and the gateways
// in front of them send one of these on a rate limit.
func retryAfterOf(header http.Header) (time.Duration, bool) {
	if v := header.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(ms * float64(time.Millisecond)), true
		}
	}
	if v := header.Get("Retry-After"); v != "" {
		if s, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(s * float64(time.Second)), true
		}
		if t, err := time.Parse(time.RFC1123, v); err == nil {
			return time.Until(t), true
		}
	}
	return 0, false
}

// statusFailure sorts a status no error type or code decided. It is the part
// the two wires share, because it is HTTP's and not a vendor's: a gateway in
// front of either answers these itself.
func statusFailure(status int) FailureClass {
	switch status {
	case 0:
		return ""
	case http.StatusPaymentRequired:
		return FailureBilling
	case http.StatusNotFound:
		return FailureModelUnavailable
	case http.StatusRequestEntityTooLarge:
		return FailureContextTooLong
	case http.StatusTooManyRequests:
		return FailureRateLimited
	case http.StatusRequestTimeout:
		return FailureUnavailable
	case http.StatusServiceUnavailable, 529:
		return FailureOverloaded
	}
	switch {
	case status >= http.StatusInternalServerError:
		return FailureUnavailable
	case status >= http.StatusBadRequest:
		return FailureRejected
	}
	return ""
}

// anthropicFailure sorts an Anthropic error, from an HTTP failure or from a
// stream's error event (whose status was the stream's 200), by the error
// type the Messages API documents, then by status. message is the error's
// own message, read for the one case the type does not say: an account out
// of credit answers `invalid_request_error`.
//
//	400 invalid_request_error   413 request_too_large    500 api_error
//	401 authentication_error    429 rate_limit_error     504 timeout_error
//	402 billing_error           403 permission_error     529 overloaded_error
//	404 not_found_error
func anthropicFailure(status int, errType, message string) FailureClass {
	switch errType {
	case "authentication_error":
		return FailureKeyRefused
	case "billing_error":
		return FailureBilling
	case "rate_limit_error":
		return FailureRateLimited
	case "overloaded_error":
		return FailureOverloaded
	case "api_error", "timeout_error":
		return FailureUnavailable
	case "request_too_large":
		return FailureContextTooLong
	case "not_found_error", "permission_error":
		// On the Messages endpoint the resource a key cannot find or may not
		// use is the model it named.
		return FailureModelUnavailable
	case "invalid_request_error":
		if strings.Contains(strings.ToLower(message), "credit balance") {
			return FailureBilling
		}
		return FailureRejected
	}
	if status == http.StatusOK {
		// A stream's error event of a type this does not know.
		return FailureUnavailable
	}
	return statusFailure(status)
}

// openaiFailure sorts an OpenAI-wire error by the error's code and type as
// OpenAI documents them, then by status. The gateways that speak this wire
// (OpenRouter, LiteLLM, Azure) copy the codes or answer by status alone.
//
//	401 invalid_api_key            429 rate_limit_exceeded    500 server_error
//	403 unsupported_country_...    429 insufficient_quota     503 overloaded
//	404 model_not_found            400 context_length_exceeded
func openaiFailure(status int, errType, errCode string) FailureClass {
	switch errCode {
	case "invalid_api_key":
		return FailureKeyRefused
	case "insufficient_quota", "billing_hard_limit_reached", "billing_not_active":
		return FailureBilling
	case "rate_limit_exceeded":
		return FailureRateLimited
	case "model_not_found":
		return FailureModelUnavailable
	case "context_length_exceeded", "string_above_max_length":
		return FailureContextTooLong
	}
	switch errType {
	case "insufficient_quota":
		return FailureBilling
	case "server_error":
		if status == http.StatusServiceUnavailable {
			return FailureOverloaded
		}
		return FailureUnavailable
	case "overloaded_error":
		return FailureOverloaded
	}
	if status == http.StatusUnauthorized {
		return FailureKeyRefused
	}
	if status == 0 || status == http.StatusOK {
		// A stream's error event with a type and code this does not know.
		if errType != "" || errCode != "" {
			return FailureUnavailable
		}
		return ""
	}
	return statusFailure(status)
}

// transportFailure sorts an error no response came with: a dial that failed,
// a timeout, a connection that dropped mid-answer. A canceled context is the
// caller leaving, not the provider failing. An address the egress gate will
// not dial and a redirect it will not follow are this server's own refusals
// of the row's configuration, which no wait lifts.
func transportFailure(err error) FailureClass {
	var blocked *egress.BlockedError
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return ""
	case errors.As(err, &blocked), egress.IsRedirect(err):
		return ""
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF),
		errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.EPIPE):
		return FailureUnavailable
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return FailureUnavailable
	}
	return ""
}
