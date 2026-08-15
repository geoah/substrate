package substrate

import "time"

// Op is a changelog operation.
type Op string

const (
	OpPut    Op = "put"
	OpPatch  Op = "patch"
	OpDelete Op = "delete"
	OpLink   Op = "link"
	OpUnlink Op = "unlink"
	OpMerge  Op = "merge"
	OpSplit  Op = "split"
	OpGC     Op = "gc"
)

// Change is one changelog row — the ordered, resumable record of every
// committed write. Payload carries op-specific detail (the diff for a
// patch, the moved sets for a merge, the dropped write for a precedence
// rejection).
type Change struct {
	Seq      int64          `json:"seq"`
	TS       time.Time      `json:"ts"`
	Actor    Actor          `json:"actor"`
	Op       Op             `json:"op"`
	RecordID string         `json:"recordId"`
	Kind     string         `json:"kind"`
	Payload  map[string]any `json:"payload,omitempty"`
	// Hash is the entry's chain hash, hex — a receipt a consumer can write
	// down and later check against `repository verify` output. It is NOT
	// independently recomputable from this wire shape: the payload here is
	// redacted, and the hash covers what is stored. Absent only on an entry
	// written before the chain existed and not yet backfilled.
	Hash string `json:"hash,omitempty"`
}

// ChangeFilter narrows a changelog read or watch.
type ChangeFilter struct {
	Kinds         []string `json:"kinds,omitempty"`
	Ops           []Op     `json:"ops,omitempty"`
	Actors        []Actor  `json:"actors,omitempty"`
	ExcludeKinds  []string `json:"excludeKinds,omitempty"`
	ExcludeOps    []Op     `json:"excludeOps,omitempty"`
	ExcludeActors []Actor  `json:"excludeActors,omitempty"`
	RecordID      string   `json:"recordId,omitempty"`
	// Q is a case-insensitive substring matched against the row's type,
	// actor, record id and payload text — the feed's one search box, a
	// cheap ILIKE at personal scale.
	Q string `json:"q,omitempty"`
}

// A ChangeTrigger's delivery state relative to one change row.
const (
	// ChangeTriggerPending: the trigger's cursor has not reached the seq.
	ChangeTriggerPending = "pending"
	// ChangeTriggerProcessed: the trigger's cursor passed the seq. The
	// cursor is delivery's durable record, so a false `when` skip and a
	// coalesced-away row both read processed — the word promises the
	// dispatcher moved past it, not that effects were applied; the run
	// ledger says which.
	ChangeTriggerProcessed = "processed"
	// ChangeTriggerParked: a trigger_failures row names this (trigger,
	// seq) — retried out and advanced past, retryable by hand.
	ChangeTriggerParked = "parked"
)
