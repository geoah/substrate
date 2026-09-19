package substrate

import "time"

// The words a `sync`-trait record's syncState carries. A plain string on the
// record, not a state machine (decision 0085): the dispatcher and the sync
// function both write it, and no move between the five is illegal.
const (
	SyncStateNever     = "never"
	SyncStateRunning   = "running"
	SyncStateOK        = "ok"
	SyncStateErroring  = "erroring"
	SyncStateThrottled = "throttled"
)

// SyncStatus is one `sync`-trait record's synchronization as `GET
// /api/v1/sync/status` lists it: the trait's own properties read off the
// record, joined with the status of every record-sourced trigger whose
// source names the record's kind. Nothing here is stored beyond the record;
// the triggers half is the same computation `…/trigger/status` answers.
type SyncStatus struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Title is the record's rendered title, the account's own name.
	Title string `json:"title,omitempty"`
	// State is syncState, or `never` when the record carries none.
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
	Paused  bool   `json:"paused"`
	// The last run: when it finished, when it started and how long it took.
	LastSyncedAt       *time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncStartedAt  *time.Time `json:"lastSyncStartedAt,omitempty"`
	LastSyncDurationMs int64      `json:"lastSyncDurationMs,omitempty"`
	// The request pair: the owner stamps RequestedAt, the sync answers with
	// RequestedAck equal to it once the requested run has finished.
	RequestedAt  *time.Time `json:"requestedAt,omitempty"`
	RequestedAck *time.Time `json:"requestedAck,omitempty"`
	// Progress is the bounded drain the body reports, absent when it reports
	// none.
	Progress *SyncProgress `json:"progress,omitempty"`
	Error    string        `json:"error,omitempty"`
	ErrorAt  *time.Time    `json:"errorAt,omitempty"`
	// Streams is the per-stream slice a multi-stream provider reports, keyed
	// by the stream's name.
	Streams map[string]SyncStream `json:"streams,omitempty"`
	// Triggers is every trigger whose record source matches the kind, with
	// its cursor, lag, parked and pending counts; a schedule trigger that
	// fires the same callable is not tied to a kind and is not here.
	Triggers []TriggerStatus `json:"triggers"`
}

// SyncProgress is the trait's `syncProgress` object: a bounded drain's phase
// and its counts. Zero counts are written, because a progress bar reads
// `done` against `total`.
type SyncProgress struct {
	Phase   string `json:"phase,omitempty"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Pending int64  `json:"pending"`
}

// SyncStream is one entry of the trait's `syncStreams` map: the stream's
// cursor (whatever shape the provider keeps), when it last drained, what it
// still holds, and its own state, message and request acknowledgement where
// the body reports them per stream.
type SyncStream struct {
	Cursor       any        `json:"cursor,omitempty"`
	LastAt       *time.Time `json:"lastAt,omitempty"`
	Pending      int64      `json:"pending"`
	State        string     `json:"state,omitempty"`
	Message      string     `json:"message,omitempty"`
	RequestedAck *time.Time `json:"requestedAck,omitempty"`
}
