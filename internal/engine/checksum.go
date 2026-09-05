package engine

// THE CHECKSUM.
//
// Every changelog entry carries the SHA-256 checksum of its canonical line
// (changelogfile.Encode), stamped by the writing transaction at commit into
// `changelog.hash`. The same bytes are what the segment file under the data
// root carries per line, so the table and the file agree entry by entry and
// a boot check can compare them. The checksum detects corruption; it does
// not chain to the previous entry and nothing signs it.
//
// The checksum covers WHAT POSTGRES STORED, never what Go marshaled: payload
// is a jsonb column and Postgres re-renders it (key order, whitespace, number
// lexemes), so the write path reads the stored text back (RETURNING
// payload::text) and a verifier reads the same text later. Both sides pass it
// through changelogfile's canonical form, whose number rendering depends only
// on the numeric VALUE, so a Postgres major upgrade cannot move a checksum.

import (
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

// pendingEntry is one changelog row this transaction appended, as the
// checksum sees it: every column the line carries, with the payload as the
// STORED text. Line holds the encoded line once settleChecksums has run, and
// the segment writer appends exactly those bytes (mirrorAfterCommit), so what
// was checked before commit is what lands after it.
type pendingEntry struct {
	Seq   int64
	TS    time.Time
	Actor string
	// Principal is the verified token id behind the write, beside the actor
	// the caller asserted. Empty where no token stood behind the entry (the
	// seed, the boot upgrade, a background worker, registration and login),
	// and 'invalid' on the history written before real ones were stamped.
	Principal  string
	Op         string
	RecordID   string
	Kind       string
	CausedBy   int64
	CausedByOK bool
	// PayloadText is the jsonb column's own rendering (payload::text), never
	// the bytes Go sent.
	PayloadText []byte
	Line        []byte
}

// fileEntry is the pending entry in the shape the file format encodes.
func (e pendingEntry) fileEntry() changelogfile.Entry {
	return changelogfile.Entry{
		Seq: e.Seq, TS: e.TS, Actor: e.Actor, Principal: e.Principal,
		Op: e.Op, RecordID: e.RecordID, Kind: e.Kind,
		CausedBy: e.CausedBy, CausedByOK: e.CausedByOK,
		Payload: e.PayloadText,
	}
}

// settleChecksums stamps the checksum of every entry this transaction
// appended, in seq order. It runs at commit (inTx), after settleFold has
// made the last payload final, so the checksum covers the payload as stored.
// The pending entries stay on the transaction, each with its encoded line,
// for the segment writer that runs after commit.
//
// Every refusal the writer could give the line is given HERE, before commit:
// a line over changelogfile.MaxLineBytes rolls the transaction back, because
// the same refusal after commit would leave a row in the table that no boot
// can ever append to the file.
func (t *txn) settleChecksums() error {
	if len(t.pending) == 0 {
		return nil
	}
	// This transaction is appending, so it is the one that claims the dialect
	// its entries are written in (changelogdialect.go): the claim commits with
	// them, and a transaction that rolls back claims nothing.
	if err := t.stampChangelogDialect(); err != nil {
		return err
	}
	for i := range t.pending {
		e := &t.pending[i]
		line, sum, err := changelogfile.Encode(e.fileEntry())
		if err != nil {
			return fmt.Errorf("substrate/engine: checksum seq %d: %w", e.Seq, err)
		}
		if len(line) > changelogfile.MaxLineBytes {
			return fmt.Errorf("%w: %w: the entry for %s %s/%s is %d bytes as a changelog line and the cap is %d",
				substrate.ErrValidation, changelogfile.ErrLineTooLong, e.Op, e.Kind, e.RecordID, len(line), changelogfile.MaxLineBytes)
		}
		res, err := t.exec(`UPDATE changelog SET hash = $2 WHERE seq = $1`, e.Seq, sum[:])
		if err != nil {
			return fmt.Errorf("substrate/engine: stamp the checksum of seq %d: %w", e.Seq, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("substrate/engine: stamping the checksum of seq %d touched %d rows", e.Seq, n)
		}
		e.Line = line
	}
	return nil
}
