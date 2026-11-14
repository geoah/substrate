package engine

import (
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
)

// The cause a thread settles with, beside its status and its reason (decision
// 0315): which failure, which budget, what stop. The reason keeps the words;
// the cause is the one fact a client acts on, from a closed set
// (substrate.AgentCauses), so nothing downstream reads a vendor's prose.

const (
	propThreadCause      = "cause"
	propThreadRetryAfter = "retryAfter"
)

// turnFailure is why an `error` settle happened: its cause, and the whole
// seconds a provider asked to wait where it named a wait.
type turnFailure struct {
	cause      string
	retryAfter int
}

// failureCauses maps the class a wire sorted a failed model call into
// (internal/llm) onto the cause the thread settles with.
var failureCauses = map[llm.FailureClass]string{
	llm.FailureKeyRefused:       substrate.CauseProviderKeyRefused,
	llm.FailureRateLimited:      substrate.CauseProviderRateLimited,
	llm.FailureBilling:          substrate.CauseProviderBilling,
	llm.FailureOverloaded:       substrate.CauseProviderOverloaded,
	llm.FailureUnavailable:      substrate.CauseProviderUnavailable,
	llm.FailureModelUnavailable: substrate.CauseModelUnavailable,
	llm.FailureContextTooLong:   substrate.CauseContextTooLong,
	llm.FailureRejected:         substrate.CauseProviderRejected,
}

// operatorFailureWords say a cause for a call made with the operator's key,
// in place of the endpoint's own answer (modelFailure).
var operatorFailureWords = map[string]string{
	substrate.CauseProviderRateLimited: "it is limiting requests",
	substrate.CauseProviderBilling:     "its account is out of credit or over its quota",
	substrate.CauseProviderOverloaded:  "it is overloaded",
	substrate.CauseProviderUnavailable: "it could not be reached or failed while answering",
	substrate.CauseModelUnavailable:    "it does not offer this model to the key",
	substrate.CauseContextTooLong:      "the conversation is too long for the model",
	substrate.CauseProviderRejected:    "it refused the request",
	substrate.CauseInternal:            "the call failed",
}

// maxRetryAfterSeconds bounds the wait a thread stores: a vendor's header is
// its own, and a client counts the seconds down.
const maxRetryAfterSeconds = 24 * 60 * 60

// modelFailure sorts a failed model call: the cause its thread settles with,
// and the words the thread keeps as its reason.
//
// The words are the endpoint's own answer, scrubbed of the key by the wire,
// where the row is the repository's own: its endpoint and what it says are
// the person's to read. A call made with the operator's key is said in the
// substrate's words instead, and the answer goes to the log, because that
// text names the operator's endpoint (the request line of a refusal, the
// address of a failed dial), which is the deployment's and not the
// repository's.
func (l *agentLoop) modelFailure(err error) (turnFailure, string) {
	if isAllowance(err) {
		return turnFailure{cause: substrate.CauseAllowanceSpent}, err.Error()
	}
	class, wait := llm.FailureOf(err)
	fail := turnFailure{cause: failureCauses[class]}
	if fail.cause == "" {
		fail.cause = substrate.CauseInternal
	}
	if wait > 0 {
		fail.retryAfter = int(math.Min(math.Ceil(wait.Seconds()), maxRetryAfterSeconds))
	}
	if l.provider.operator == nil {
		return fail, err.Error()
	}
	l.ds.svc.log.Warn("substrate: a model call made with the operator's key failed",
		"repository", l.ds.info.ID, "provider", l.provider.id, "cause", fail.cause, "error", err.Error())
	reason := fmt.Sprintf("%s could not answer with %s's key: %s.", l.provider.name, l.ds.siteName(), operatorFailureWords[fail.cause])
	if fail.retryAfter > 0 {
		reason += fmt.Sprintf(" It asked to wait %s.", (time.Duration(fail.retryAfter) * time.Second).String())
	}
	return fail, reason
}

// settledCause is the cause a settle with status and reason writes: the
// failure's for an error, the budget's for an overbudget run, the stop's for
// a stopped one, and none for a run that finished.
func settledCause(status, reason string, fail turnFailure) turnFailure {
	switch status {
	case threadError:
		if fail.cause == "" {
			fail.cause = substrate.CauseInternal
		}
		return fail
	case threadOverBudget:
		switch {
		case strings.HasPrefix(reason, reasonDeadline):
			return turnFailure{cause: substrate.CauseDeadline}
		case strings.HasPrefix(reason, reasonMaxToolCalls):
			return turnFailure{cause: substrate.CauseToolCallLimit}
		}
		return turnFailure{cause: substrate.CauseTurnLimit}
	case threadStopped:
		if reason == reasonShutdown {
			return turnFailure{cause: substrate.CauseShutdown}
		}
		return turnFailure{cause: substrate.CauseRequested}
	}
	return turnFailure{}
}

// stampCause writes a settle's cause into the thread's properties, clearing
// what an earlier run left where this one has none. A repository whose stored
// thread kind predates the property (a shipped upgrade that has not landed)
// settles without it: the status must land whatever the declaration says.
func (ds *dataset) stampCause(props map[string]any, c turnFailure) {
	ty, ok := ds.registry().ByIdentity(typeThread)
	if !ok {
		return
	}
	if _, declared := ty.Prop(propThreadCause); !declared {
		return
	}
	props[propThreadCause], props[propThreadRetryAfter] = nil, nil
	if c.cause != "" {
		props[propThreadCause] = c.cause
	}
	if c.retryAfter > 0 {
		props[propThreadRetryAfter] = c.retryAfter
	}
}

// sayableError is an error under words that may be stored on a thread and
// said to a caller. Its chain is still the original's, so every sentinel
// matches through it; only its text is replaced.
type sayableError struct {
	words string
	err   error
}

func (e *sayableError) Error() string { return e.words }
func (e *sayableError) Unwrap() error { return e.err }

// sayable is err as a thread's reason, an API answer and a stream's error
// event may carry it. A failure of the run's own reads and writes is said in
// its own words, which are the engine's, unless it is a failure to reach
// the database: a connection error names the database's host, its address,
// its name and its user, which are the deployment's and no caller's to read.
// That one is said plainly, and the error itself goes to the log with what
// the run was doing.
func (l *agentLoop) sayable(doing string, err error) error {
	if !namesTheDatabase(err) {
		return err
	}
	l.ds.svc.log.Error("substrate: an agent run could not reach the database",
		"repository", l.ds.info.ID, "thread", l.threadID, "during", doing, "error", err.Error())
	return &sayableError{words: "the database could not be reached", err: err}
}

// namesTheDatabase reports whether err is a failure to reach the database,
// whose text names where the database is.
func namesTheDatabase(err error) bool {
	var connect *pgconn.ConnectError
	var op *net.OpError
	var dns *net.DNSError
	return errors.As(err, &connect) || errors.As(err, &op) || errors.As(err, &dns)
}

// withCause is props with a settle's cause stamped in (stampCause).
func (ds *dataset) withCause(props map[string]any, c turnFailure) map[string]any {
	ds.stampCause(props, c)
	return props
}

// failed wraps a run's error with the cause its thread settled with, for the
// entries that answer it (substrate.AgentFailure).
func failed(c turnFailure, err error) error {
	var already *substrate.AgentFailure
	if err == nil || errors.As(err, &already) {
		return err
	}
	if c.cause == "" {
		c.cause = substrate.CauseInternal
	}
	return &substrate.AgentFailure{Cause: c.cause, RetryAfter: c.retryAfter, Err: err}
}
