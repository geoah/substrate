package pushrelay

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/geoah/substrate/internal/pushwire"
)

// Refusal is one row of the relay's answers that are not a push sent: the
// status, the code a server acts on and a message for a log. Each is a value
// here, so a test and the fixtures hold the relay to the plan's table.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

// The relay's refusals, in the order of the plan's table. A bad request's
// message says what was wrong, so it is built per request (BadRequest).
var (
	StaleSignature     = Refusal{http.StatusUnauthorized, pushwire.CodeStaleSignature, "the signature's time is more than " + strconv.Itoa(int(pushwire.SignatureWindow.Seconds())) + " s from the relay's clock"}
	SignatureRefused   = Refusal{http.StatusUnauthorized, pushwire.CodeSignatureRefused, "no signature, or one the push key's public key does not verify"}
	KeyRefused         = Refusal{http.StatusForbidden, pushwire.CodeKeyRefused, "the push key does not open under this relay's seal keys"}
	KeyExpired         = Refusal{http.StatusForbidden, pushwire.CodeKeyExpired, "the push key was issued more than 90 days ago"}
	TopicRefused       = Refusal{http.StatusForbidden, pushwire.CodeTopicRefused, "this relay does not push to that topic"}
	EnvironmentRefused = Refusal{http.StatusForbidden, pushwire.CodeEnvironmentRefused, "this relay holds no Apple key for that environment"}
	Replayed           = Refusal{http.StatusConflict, pushwire.CodeReplayed, "this signed request was accepted before"}
	Gone               = Refusal{http.StatusGone, pushwire.CodeGone, "Apple says this install is no longer registered"}
	TooLarge           = Refusal{http.StatusRequestEntityTooLarge, pushwire.CodeBadRequest, "the body is over " + strconv.Itoa(pushwire.MaxBody) + " bytes"}
	RateLimited        = Refusal{http.StatusTooManyRequests, pushwire.CodeRateLimited, "a limit was reached: retry after Retry-After seconds"}
	Upstream           = Refusal{http.StatusBadGateway, pushwire.CodeUpstream, "Apple could not take the push"}
	Internal           = Refusal{http.StatusInternalServerError, pushwire.CodeInternal, "the relay failed"}
	NotFound           = Refusal{http.StatusNotFound, pushwire.CodeBadRequest, "no such route"}
	MethodNotAllowed   = Refusal{http.StatusMethodNotAllowed, pushwire.CodeBadRequest, "the route takes POST"}
)

// BadRequest is the 400 for a body the wire refuses, with what was wrong.
func BadRequest(message string) Refusal {
	return Refusal{http.StatusBadRequest, pushwire.CodeBadRequest, message}
}

// Body is the refusal's envelope.
func (r Refusal) Body() []byte {
	b, _ := json.Marshal(pushwire.ErrorResponse{Error: pushwire.ErrorDetail{Code: r.Code, Message: r.Message}})
	return b
}
