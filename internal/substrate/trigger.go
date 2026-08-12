package substrate

import "time"

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
// last fire (schedule sources) and how many parked failures it holds.
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
	Parked   int64      `json:"parked"`
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
