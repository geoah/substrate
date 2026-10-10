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

// The words a status row's Health carries.
const (
	HealthOK      = "ok"
	HealthFailing = "failing"
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
	// and not a failure. InFlight counts the rows the server is delivering
	// right now (an agent run's claim, a retry by hand): listed there with
	// `running` set, and not counted as parked. On a record trigger InFlight
	// also counts the deliveries a pass or a wake is running now, an agent
	// delivery's claim counted once. On a schedule trigger both also count
	// the occurrences that are due and not yet settled or parked past, at
	// most 100: InFlight the one the dispatcher is running, Pending the ones
	// waiting for a pass to reach them. Those are not listed under
	// `…/parked`.
	Parked   int64 `json:"parked"`
	Pending  int64 `json:"pending"`
	InFlight int64 `json:"inFlight"`
	// LastPassAt is when a dispatcher pass last reached a record or schedule
	// trigger: its turn among the record triggers, or a look for due
	// occurrences. LastDeliveredAt is when a delivery of it last settled
	// (ran, skipped or parked past), a wake's and a hand retry's included;
	// a fire that lost its fire state to another dispatcher settled nothing.
	// Neither is stored: the server process keeps both in memory, so a
	// restart clears them, each is absent until that process reaches the
	// trigger, and a webhook trigger carries neither.
	LastPassAt      *time.Time `json:"lastPassAt,omitempty"`
	LastDeliveredAt *time.Time `json:"lastDeliveredAt,omitempty"`
	// LastParkedError is the newest parked delivery's error: its first line,
	// cut at 500 bytes, so a list says why without reading `…/parked`.
	// LastParkedAt is when that delivery parked. Both are absent while
	// Parked is 0.
	LastParkedError string     `json:"lastParkedError,omitempty"`
	LastParkedAt    *time.Time `json:"lastParkedAt,omitempty"`
	// Held says why the dispatcher holds an agent trigger's deliveries: the
	// spend cap reached (the agent's `budgets.spendCentsPerDay` or the
	// repository's `substrate.reamde.dev/llm/spendCentsPerDay` setting) and
	// the spend so far. A held delivery is not claimed, so the cursor or
	// fire state stays and nothing parks; the next dispatcher pass after the
	// spend falls under the cap, or the cap is raised, delivers it. Absent
	// while the trigger is not held.
	Held string `json:"held,omitempty"`
	// Error names a trigger the dispatcher cannot run: an unparseable row or
	// a callable that no longer resolves.
	Error string `json:"error,omitempty"`
	// Health is `failing` while the trigger's `trigger.failing/<id>` alert
	// (a core/alert record) is open, and `ok` otherwise. The dispatcher opens
	// that alert once every delivery since the trigger's newest ok run has
	// parked for longer than SUBSTRATE_HEALTH_FAILING_AFTER, and the next ok
	// delivery, dispatched or retried by hand, resolves it. FailingSince is
	// the open alert's firstSeenAt, the oldest of those parks; absent while
	// Health is `ok`.
	Health       string     `json:"health"`
	FailingSince *time.Time `json:"failingSince,omitempty"`
	// LastOkAt is when the trigger's newest ok run finished, dispatched or
	// retried by hand; retention keeps that run however old. Absent when the
	// trigger never delivered.
	LastOkAt *time.Time `json:"lastOkAt,omitempty"`
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
	// Stream is the sync stream the trigger's CURRENT callable declares (a
	// function's `stream`): the stream `sync status` marks erroring for
	// this park. Absent when the callable declares none.
	Stream string `json:"stream,omitempty"`
	// Running is set while the server is delivering this row: an agent run
	// that holds it as its claim, or a retry by hand. A retry of a running
	// row answers conflict.
	Running bool `json:"running,omitempty"`
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
