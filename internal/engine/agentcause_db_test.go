package engine

// The cause a thread settles with (decision 0315): each failure of a model
// call as its wire sorted it, each budget and each stop, on the thread row,
// on the result and on the error the entries answer; cleared by a run that
// finishes; and, for a call made with the operator's key, in the substrate's
// words and never the endpoint's.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
)

// failureOf is the cause and wait err carries as a failed run.
func failureOf(t *testing.T, err error) (string, int) {
	t.Helper()
	var failure *substrate.AgentFailure
	if !errors.As(err, &failure) {
		t.Fatalf("%v is no failed run: it carries no cause", err)
	}
	return failure.Cause, failure.RetryAfter
}

func TestAFailedModelCallSettlesTheThreadWithItsCause(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		turn       fakeTurn
		cause      string
		retryAfter int
	}{
		{"a rate limit", fakeTurn{status: 429, retryAfter: "20", errBody: `{"error":{"message":"Rate limit reached for requests.","type":"requests","code":"rate_limit_exceeded"}}`}, substrate.CauseProviderRateLimited, 20},
		{"an account over its quota", fakeTurn{status: 429, errBody: `{"error":{"message":"You exceeded your current quota.","type":"insufficient_quota","code":"insufficient_quota"}}`}, substrate.CauseProviderBilling, 0},
		{"an overloaded vendor", fakeTurn{status: 503, errBody: `{"error":{"message":"The engine is currently overloaded.","type":"server_error"}}`}, substrate.CauseProviderOverloaded, 0},
		{"a vendor fault", fakeTurn{status: 500}, substrate.CauseProviderUnavailable, 0},
		{"a model the key is not offered", fakeTurn{status: 404, errBody: `{"error":{"message":"The model does not exist or you do not have access to it.","type":"invalid_request_error","code":"model_not_found"}}`}, substrate.CauseModelUnavailable, 0},
		{"a request the vendor refuses", fakeTurn{status: 400, errBody: `{"error":{"message":"Unsupported value.","type":"invalid_request_error","code":"unsupported_value"}}`}, substrate.CauseProviderRejected, 0},
		{"a key the vendor refuses", fakeTurn{status: 401, errBody: `{"error":{"message":"Incorrect API key provided.","type":"invalid_request_error","code":"invalid_api_key"}}`}, substrate.CauseProviderKeyRefused, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds, fake := openAgentDataset(t)
			fake.script("chat", tc.turn)
			log := newEventLog()
			_, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, chatter, substrate.ChatInput{Message: "hi"}, log.emit)
			if cause, wait := failureOf(t, err); cause != tc.cause || wait != tc.retryAfter {
				t.Fatalf("the failed turn's error carries %q after %ds, want %q after %ds: %v", cause, wait, tc.cause, tc.retryAfter, err)
			}
			thread := log.snapshot()[0].Thread
			props := threadProps(t, ds, thread)
			if props["status"] != threadError || props[propThreadCause] != tc.cause || intProp(props, propThreadRetryAfter) != tc.retryAfter {
				t.Fatalf("thread = %v, cause %v, retryAfter %v; want error, %s, %d", props["status"], props[propThreadCause], props[propThreadRetryAfter], tc.cause, tc.retryAfter)
			}
			if reason, _ := props["reason"].(string); reason == "" || strings.Contains(reason, "row-key-chatllm") {
				t.Fatalf("thread reason = %q, want the failure's words without the row's key", reason)
			}

			// A continue that finishes clears the cause with the reason.
			if tc.cause == substrate.CauseProviderKeyRefused {
				return
			}
			fake.script("chat", fakeTurn{content: "there"})
			res, err := ds.ContinueAgentTurn(context.Background(), substrate.ActorPerson, thread, nil)
			if err != nil || res.Status != threadOK || res.Cause != "" || res.RetryAfter != 0 {
				t.Fatalf("the retry = %+v, %v, want ok with no cause", res, err)
			}
			props = threadProps(t, ds, thread)
			if props[propThreadCause] != nil || props[propThreadRetryAfter] != nil || props["reason"] != nil {
				t.Fatalf("a finished run left cause %v, retryAfter %v, reason %v", props[propThreadCause], props[propThreadRetryAfter], props["reason"])
			}
		})
	}
}

func TestABudgetAndAStopSettleWithTheirCause(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	call := fakeTurn{calls: []fakeCall{{"annotate", `{"id":"01h2xcejqtf2nbrexx3vqjhp42"}`}}}
	fake.script("budget", call, call)
	res, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/budgeter", substrate.ChatInput{Message: "twice"}, nil)
	if err != nil || res.Status != threadOverBudget || res.Cause != substrate.CauseTurnLimit {
		t.Fatalf("a run out of turns = %+v, %v, want overbudget with turn_limit", res, err)
	}
	if got := threadProps(t, ds, res.Thread)[propThreadCause]; got != substrate.CauseTurnLimit {
		t.Fatalf("thread cause = %v, want turn_limit", got)
	}

	hold, _ := heldTurn(t)
	fake.script("chat", fakeTurn{content: "partial reply", hold: hold})
	log := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "hi"}, log.emit)
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread
	stopped, err := ds.StopAgentTurn(ctx, substrate.ActorPerson, thread)
	if err != nil || stopped.Status != threadStopped || stopped.Cause != substrate.CauseRequested {
		t.Fatalf("a stopped turn = %+v, %v, want stopped with requested", stopped, err)
	}
	if done := log.waitFor(t, substrate.AgentEventDone); done.Result.Cause != substrate.CauseRequested {
		t.Fatalf("the done event's result = %+v, want the cause on it", done.Result)
	}
	if got := threadProps(t, ds, thread)[propThreadCause]; got != substrate.CauseRequested {
		t.Fatalf("thread cause = %v, want requested", got)
	}
}

// The settles the loop derives from a status and its reason alone.
func TestSettledCauseReadsTheBudgetAndTheStop(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status, reason string
		fail           turnFailure
		want           turnFailure
	}{
		{threadOK, "", turnFailure{}, turnFailure{}},
		{threadOK, reasonMaxToolCalls + " (32)", turnFailure{}, turnFailure{}},
		{threadOverBudget, "max turns reached (8)", turnFailure{}, turnFailure{cause: substrate.CauseTurnLimit}},
		{threadOverBudget, reasonMaxToolCalls + " (32)", turnFailure{}, turnFailure{cause: substrate.CauseToolCallLimit}},
		{threadOverBudget, reasonDeadline, turnFailure{}, turnFailure{cause: substrate.CauseDeadline}},
		{threadOverBudget, reasonDeadlineLastTurn, turnFailure{}, turnFailure{cause: substrate.CauseDeadline}},
		{threadStopped, reasonStoppedBy(substrate.ActorPerson), turnFailure{}, turnFailure{cause: substrate.CauseRequested}},
		{threadStopped, stoppedByPhrase(substrate.ActorTypeApp), turnFailure{}, turnFailure{cause: substrate.CauseRequested}},
		{threadStopped, reasonShutdown, turnFailure{}, turnFailure{cause: substrate.CauseShutdown}},
		{threadError, "anything", turnFailure{}, turnFailure{cause: substrate.CauseInternal}},
		{threadError, "anything", turnFailure{cause: substrate.CauseProviderRateLimited, retryAfter: 7}, turnFailure{cause: substrate.CauseProviderRateLimited, retryAfter: 7}},
		// A failure recorded before a stop turned the settle into a stop is
		// not the stop's cause.
		{threadStopped, reasonShutdown, turnFailure{cause: substrate.CauseWriteFailed}, turnFailure{cause: substrate.CauseShutdown}},
	} {
		if got := settledCause(tc.status, tc.reason, tc.fail); got != tc.want {
			t.Errorf("settledCause(%s, %q, %+v) = %+v, want %+v", tc.status, tc.reason, tc.fail, got, tc.want)
		}
	}
}

// Every class a wire sorts a failure into has a cause from the closed set,
// and every cause a call made with the operator's key can fail with has
// words of the substrate's own.
func TestEveryFailureClassHasACause(t *testing.T) {
	t.Parallel()
	for class, cause := range failureCauses {
		if !slices.Contains(substrate.AgentCauses, cause) {
			t.Errorf("failure class %q maps to %q, which is not a cause", class, cause)
		}
		if cause != substrate.CauseProviderKeyRefused && operatorFailureWords[cause] == "" {
			t.Errorf("cause %q has no words for a call made with the operator's key", cause)
		}
	}
	if operatorFailureWords[substrate.CauseInternal] == "" {
		t.Error("a failure no wire sorted has no words for a call made with the operator's key")
	}
}

// A call made with the operator's key fails in the substrate's words: the
// endpoint's answer, which names the operator's endpoint, goes to the log
// and reaches neither the thread nor the caller.
func TestAnOperatorPaidCallFailsWithoutNamingTheOperatorsEndpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config { return operatorChatConfig(u, "", 1000) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	endpoint := strings.TrimPrefix(op.srv.URL, "http://")

	op.script("chat", fakeTurn{status: 429, retryAfter: "12", errBody: `{"error":{"message":"Rate limit reached at ` + op.srv.URL + ` for the operator's organization.","type":"requests","code":"rate_limit_exceeded"}}`})
	log := newEventLog()
	_, err := ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "hi"}, log.emit)
	if cause, wait := failureOf(t, err); cause != substrate.CauseProviderRateLimited || wait != 12 {
		t.Fatalf("the failure carries %q after %ds, want provider_rate_limited after 12s", cause, wait)
	}
	props := threadProps(t, ds, log.snapshot()[0].Thread)
	reason, _ := props["reason"].(string)
	for what, text := range map[string]string{"the error": err.Error(), "the thread's reason": reason} {
		if strings.Contains(text, endpoint) || strings.Contains(text, "organization") || strings.Contains(text, operatorTestKey) {
			t.Fatalf("%s carries the operator's endpoint or its answer: %q", what, text)
		}
	}
	if !strings.Contains(reason, "Example's key") || !strings.Contains(reason, "limiting requests") || props[propThreadCause] != substrate.CauseProviderRateLimited {
		t.Fatalf("thread reason = %q, cause %v", reason, props[propThreadCause])
	}

	// A connection that cannot be made names the address it dialed.
	op.srv.Close()
	log = newEventLog()
	_, err = ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "again"}, log.emit)
	if cause, _ := failureOf(t, err); cause != substrate.CauseProviderUnavailable {
		t.Fatalf("a dial that failed carries %q, want provider_unavailable: %v", cause, err)
	}
	reason, _ = threadProps(t, ds, log.snapshot()[0].Thread)["reason"].(string)
	if strings.Contains(err.Error(), endpoint) || strings.Contains(reason, endpoint) {
		t.Fatalf("a failed dial names the operator's endpoint: %q / %q", err, reason)
	}
}

// A run refused before it starts says why in a reason the API sends: a
// provider row with no key.
func TestARowWithNoKeyRefusesWithItsReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{"apiKey": nil})
	_, err := ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "hi"}, nil)
	if !errors.Is(err, substrate.ErrValidation) || substrate.ReasonOf(err) != substrate.ReasonProviderKeyMissing {
		t.Fatalf("a chat on a keyless row = %v (reason %q), want validation with provider_key_missing", err, substrate.ReasonOf(err))
	}
	if n := len(agentThreadsOf(t, ds, "chatter")); n != 0 {
		t.Fatalf("%d threads opened for a run that never started", n)
	}
}

// A failure to reach the database names its host, its address, its name and
// its user: those words never become a thread's reason or a caller's answer,
// and every other failure of a run's own writes keeps its words.
func TestAFailureToReachTheDatabaseIsNotSaidInItsOwnWords(t *testing.T) {
	t.Parallel()
	connect := &pgconn.ConnectError{Config: &pgconn.Config{Host: "db.internal.example.com", Port: 5432, Database: "substrate", User: "substrate_rw"}}
	dial := &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 7), Port: 5432}, Err: errors.New("connection refused")}
	for _, err := range []error{connect, dial, &net.DNSError{Err: "no such host", Name: "db.internal.example.com"}} {
		wrapped := fmt.Errorf("%w: put message: %w", substrate.ErrUnavailable, err)
		if !namesTheDatabase(wrapped) {
			t.Fatalf("%T is not read as a failure to reach the database: %v", err, wrapped)
		}
		l := &agentLoop{ds: &dataset{svc: &service{log: slog.New(slog.DiscardHandler)}}}
		said := l.sayable("a message write", wrapped)
		if strings.Contains(said.Error(), "example.com") || strings.Contains(said.Error(), "10.0.0.7") || strings.Contains(said.Error(), "substrate_rw") {
			t.Fatalf("the words a thread keeps name the database: %q", said)
		}
		// The sentinel the engine classified it under still matches.
		if !errors.Is(said, substrate.ErrUnavailable) {
			t.Fatalf("the sayable error lost its sentinel: %v", said)
		}
	}
	for _, err := range []error{
		errors.New("the insert was refused"),
		context.DeadlineExceeded,
		fmt.Errorf("%w: llm/message: turn: required", substrate.ErrValidation),
		&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"},
	} {
		if namesTheDatabase(err) {
			t.Fatalf("%v is read as a failure to reach the database", err)
		}
	}
}

// The outcome a direct call stores under its Idempotency-Key is the one its
// thread settled with, cause included: a repeat answers what the first did.
func TestAnIdempotentCallsRepeatAnswersTheSettledCause(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	call := fakeTurn{calls: []fakeCall{{"annotate", `{"id":"01h2xcejqtf2nbrexx3vqjhp43"}`}}}
	fake.script("budget", call, call)
	ctx := substrate.WithIdempotencyKey(context.Background(), "cause-key-1")
	first, err := ds.CallAgent(ctx, crewPackage+"/budgeter", "annotate twice")
	if err != nil || first.Status != threadOverBudget || first.Cause != substrate.CauseTurnLimit {
		t.Fatalf("the first call = %+v, %v, want overbudget with turn_limit", first, err)
	}
	again, err := ds.CallAgent(ctx, crewPackage+"/budgeter", "annotate twice")
	if err != nil {
		t.Fatalf("the repeat: %v", err)
	}
	if again.Thread != first.Thread || again.Status != first.Status || again.Reason != first.Reason || again.Cause != first.Cause {
		t.Fatalf("the repeat answered %+v, and the first %+v", again, first)
	}
	if n := len(fake.requestsOf("budget")); n != 2 {
		t.Fatalf("%d model calls, want the first call's 2", n)
	}
}
