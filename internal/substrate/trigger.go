package substrate

import (
	"time"
)

// ChangeTrigger is one enabled trigger's stance on one change row, as the
// /changes feed reports it. Only triggers the row can fire appear: a source
// mismatch or the callable's own write (self-actor exclusion) is omitted,
// not a fourth state.
type ChangeTrigger struct {
	Trigger  string `json:"trigger"` // the trigger record's id
	Callable string `json:"callable"`
	State    string `json:"state"`
	// Error is the parked delivery's last error, carried only when State is
	// parked so the feed says why without a second request.
	Error string `json:"error,omitempty"`
}

// The trigger source kinds a status row reports.
const (
	TriggerKindRecord   = "record"
	TriggerKindSchedule = "schedule"
	TriggerKindWebhook  = "webhook"
)

// TriggerStatus is one trigger's delivery bookkeeping, computed on read:
// its cursor (record sources), the changelog head, the lag between them, the
// last fire (schedule sources), how many parked failures it holds and how
// many accepted webhook requests it has yet to settle.
type TriggerStatus struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // record | schedule | webhook
	Callable string `json:"callable"`
	Enabled  bool   `json:"enabled"`
	Cursor   int64  `json:"cursor,omitempty"`
	Head     int64  `json:"head"`
	Lag      int64  `json:"lag,omitempty"`
	// LastFire is the newest schedule occurrence delivered (or parked past).
	LastFire *time.Time `json:"lastFire,omitempty"`
	// WebhookPath is a webhook trigger's public endpoint, relative to the
	// server root: "/webhooks/{authority}/{trigger}". The key, when the
	// trigger declares one, is on the record and never here.
	WebhookPath string `json:"webhookPath,omitempty"`
	// Parked counts the deliveries the trigger gave up on, listed under
	// `…/parked`. Pending counts the webhook requests the door accepted whose
	// fire has not settled: listed there too, with `lastError` saying so,
	// and not a failure.
	Parked  int64 `json:"parked"`
	Pending int64 `json:"pending"`
	// Error names a trigger the dispatcher cannot run: an unparseable row or
	// a callable that no longer resolves.
	Error string `json:"error,omitempty"`
}

// TriggerFailure is one parked delivery: the trigger gave up on this
// (seq or fire, record) after its retries and advanced past it. Retryable
// by hand.
type TriggerFailure struct {
	ID      int64  `json:"id"`
	Trigger string `json:"trigger"`
	Seq     int64  `json:"seq,omitempty"`
	// FireID is set (and Seq is 0) when the parked delivery was a schedule
	// occurrence or a webhook wake.
	FireID    string    `json:"fireId,omitempty"`
	RecordID  string    `json:"recordId,omitempty"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError"`
	ParkedAt  time.Time `json:"parkedAt"`
}

// TriggerReplayed is the reply to a replay: the seq the trigger's cursor was
// reset to, echoed so the caller sees what the dispatcher will walk from.
type TriggerReplayed struct {
	From int64 `json:"from"`
}

// TriggerRan is the reply to a run, a wake and a retry alike: how many
// deliveries the verb ran. Zero is an answer (nothing was due), not an error.
type TriggerRan struct {
	Ran int `json:"ran"`
}

// FunctionCalled is the reply to a call: the function's output, verbatim, and
// how many effects it applied under its own actor. Output is whatever the body
// returned, so it is always on the wire, null included.
type FunctionCalled struct {
	Output  any `json:"output"`
	Effects int `json:"effects"`
}
