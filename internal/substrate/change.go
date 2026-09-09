package substrate

import (
	"context"
	"time"
)

// Op is a changelog operation.
type Op string

const (
	OpPut    Op = "put"
	OpPatch  Op = "patch"
	OpDelete Op = "delete"
	OpMerge  Op = "merge"
	OpSplit  Op = "split"
	OpGC     Op = "gc"
	// OpDelivery is the engine's delivery ledger: the entry a trigger's
	// bookkeeping rides (a cursor or fire-state advance, a parked failure, a
	// paged drain's resume row), appended in the transaction that commits the
	// effects it acknowledges and folded back into the delivery tables on
	// replay. It is internal: every read a client reaches skips it, so it is
	// never on the wire and never matches a trigger's source.
	OpDelivery Op = "delivery"
)

// Change is one changelog row — the ordered, resumable record of every
// committed write. Payload carries op-specific detail (the diff for a
// patch, the moved sets for a merge, the dropped write for a precedence
// rejection). Affected is the public event: every record the entry moved,
// with the version each reached, so a client keeps a current copy by
// fetching each one (decision 0061). The stored replay effects never reach
// the wire: a storage spelling is not a contract.
type Change struct {
	Seq      int64          `json:"seq"`
	TS       time.Time      `json:"ts"`
	Actor    Actor          `json:"actor"`
	Op       Op             `json:"op"`
	RecordID string         `json:"recordId"`
	Kind     string         `json:"kind"`
	Payload  map[string]any `json:"payload,omitempty"`
	// Affected names each record the entry changed, at least the addressed
	// one: one element per (kind, id), in the order the entry first touched
	// them. A merge lists the winner and the tombstoned loser, a collector's
	// pass every record it purged.
	Affected []AffectedRecord `json:"affected,omitempty"`
	// Hash is the entry's checksum, hex: the SHA-256 of its canonical line,
	// the same value the segment file carries in `sum`. Every committed entry
	// carries one, stamped in the transaction that appended it. It is NOT
	// independently recomputable from this wire shape: the payload here is
	// redacted, and the checksum covers what is stored.
	Hash string `json:"hash,omitempty"`
}

// ChangeRow is one row of the change feed, history page and watch stream
// alike: the change plus each enabled trigger's stance on it. Triggers is
// omitted when no trigger matches, so absence means "nothing listens", never
// "unknown".
type ChangeRow struct {
	Change
	Triggers []ChangeTrigger `json:"triggers,omitempty"`
}

// ChangePage is one history page of the change feed, newest first. Cursor is
// the continuation, the seq the client passes as the next `before`; it is
// omitted only when the walk reached the bottom (absence means done), and a
// seq is never 0, so the zero value is the absent one. Head and Generation
// are the watch handoff, as on a list Page: `watch?from={head}&generation=
// {generation}` tails what this page did not hold.
type ChangePage struct {
	Changes    []ChangeRow `json:"changes"`
	Cursor     int64       `json:"cursor,omitempty"`
	Head       int64       `json:"head"`
	Generation string      `json:"generation"`
}

// AffectedRecord is one record a change moved, as the public event names it.
// Version is the version the record reached in this entry, the same number a
// read of the record returns until its next change, so a client whose copy
// already carries it or a later one need not fetch; it is absent on a purge,
// where the record has no version afterwards. Deleted is true when the entry
// tombstoned or purged the record; a fetch then answers the tombstone or
// `not_found`, and a client keeping a copy drops it either way.
type AffectedRecord struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int64  `json:"version,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// ChangelogHead is where a repository's changelog stands: its highest
// committed seq and the history generation that numbering belongs to. A
// change cursor is a seq under one generation, and it resumes only while the
// generation is the repository's and the seq is at or below the head: the
// generation changes when a repository's history is imported into a database
// that did not hold it, and holds across a restart and a rebuild, so a cursor
// saved from a history that was since replaced is refused instead of
// silently skipping the replacement's writes (decision 0056).
type ChangelogHead struct {
	Seq        int64  `json:"seq"`
	Generation string `json:"generation"`
}

// ChangeFilter narrows a changelog read or watch.
type ChangeFilter struct {
	Kinds         []string `json:"kinds,omitempty"`
	Ops           []Op     `json:"ops,omitempty"`
	Actors        []Actor  `json:"actors,omitempty"`
	ExcludeKinds  []string `json:"excludeKinds,omitempty"`
	ExcludeOps    []Op     `json:"excludeOps,omitempty"`
	ExcludeActors []Actor  `json:"excludeActors,omitempty"`
	// RecordID scopes the feed to one record: rows whose RecordID is the id,
	// plus a merge or split entry whose payload names the id as `winner` or
	// `loser`, because each of those changes two records under one entry
	// addressed to one of them. The scope follows the addressed pair only,
	// never the winner's later writes under a merged-away id.
	RecordID string `json:"recordId,omitempty"`
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

// ChangeFeedOps is the cross-collection feed seam: history paged backward,
// and each enabled trigger's stance on a row. It is an optional Dataset
// extension (see Dataset), so a dataset without it serves the forward reads
// and plain rows.
type ChangeFeedOps interface {
	ChangesBefore(ctx context.Context, before int64, f ChangeFilter, limit int) ([]Change, error)
	ChangeTriggers(ctx context.Context, changes []Change) (map[int64][]ChangeTrigger, error)
}
