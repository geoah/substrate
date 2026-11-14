package engine

// THE ACTOR RECORDS A REPOSITORY LINKS (decision 0270). Every hand that
// writes is an actor record, and a line, a mark or a stored reference links
// it by id. A repository written by this binary holds the record of every
// hand it links, since a write ensures its own (ensureActors). One written
// before actors were records does not: the conversion turned each spelling
// in its history into an id, and repository migration 9 was to write the
// records those ids name, but a repository migration runs before the stored
// vocabulary loads, while the actor kind is still the declaration kind a
// release kept, and ensureActors writes nothing then. Its ledger says it
// ran, so it never runs again, and its log line counts records it did not
// write. Until a hand writes again, what links it answers 404.
//
// So the open fills them, after the stored vocabulary has loaded and the
// shipped upgrade has declared the actor kind this binary writes, and after
// the people pass has named the person (people.go settlePeople), the way
// the record seed fills what a repository lacks. It reads every actor id the
// repository links (each line's actor and the one it acted for, every mark
// in its history and in its fold, every reference to an actor record in a
// line or a row) and, for each that has no record at all, the hand behind
// it from what the repository has held (repositoryHands: its person, the
// system, every package, function and agent ever declared, live or
// tombstoned, and every app a token was approved for, from the rows and
// from history). It writes that hand's record as ordinary writes by the
// system, disabled where the hand's code or approval is gone, as migration
// 9 would have. A record that stands, a tombstone included, is left as it
// is. The person is the people pass's to write: its record names an
// identity this pass cannot know.
//
// An id no hand the repository held derives has no record written: nothing
// says what it is. Each is named in the log line.
//
// Once per repository: reading every line costs as much as the history is
// long, so the pass leaves a mark on the repository's own record
// (annActorRecordsFilled) in the transaction that writes what it found, and
// an open that finds the mark reads nothing. The mark is a fact of the
// repository, so an import or a restore carries it, and a repository this
// binary creates is born with it.

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// annActorRecordsFilled is the mark fillActorRecords leaves on the
// repository's record once it has written every actor record the repository
// links.
const annActorRecordsFilled = "substrate/actor-records-filled"

// fillActorRecords is the open's pass (above).
func (ds *dataset) fillActorRecords(ctx context.Context) error {
	if ds.svc.readOnly || ds.svc.noActorFill {
		return nil
	}
	self := eref{Kind: kindRepository, ID: ds.info.ID}
	var filled bool
	if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3)`,
		self.Kind, self.ID, annActorRecordsFilled).Scan(&filled); err != nil {
		return fmt.Errorf("substrate/engine: open repository %s: read whether its actor records are filled: %w", ds.info.ID, err)
	}
	if filled {
		return nil
	}
	var written, disabled int
	var unnamed, person []string
	var began time.Time
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		// A repository whose upgrade was refused still holds the declaration
		// kind an actor once was: nothing can be written yet, and the mark
		// waits for the open that can.
		if !t.actorKindDeclared() {
			return nil
		}
		// Said before the read, which takes as long as the history is long
		// and holds the open meanwhile: a server that looks hung here is
		// reading, and a restart rolls the read back and begins it again.
		began = time.Now()
		ds.svc.log.Info("substrate: reading the repository's history once for the actor records it links; the open waits for it, and a restart starts it over",
			"repository", ds.info.ID)
		linked, err := t.linkedActorIDs()
		if err != nil {
			return err
		}
		held, err := t.heldActorIDs()
		if err != nil {
			return err
		}
		hands, err := t.repositoryHands()
		if err != nil {
			return err
		}
		for _, id := range linked {
			if held[id] {
				continue
			}
			h, ok := hands[id]
			switch {
			case !ok:
				unnamed = append(unnamed, id)
				continue
			case h.key == substrate.ActorPerson:
				person = append(person, id)
				continue
			}
			if err := t.ensureActor(h.key); err != nil {
				return fmt.Errorf("write the actor record of %s: %w", h.key, err)
			}
			written++
			if !h.live {
				if err := t.disableActor(h.key); err != nil {
					return fmt.Errorf("disable the actor record of %s: %w", h.key, err)
				}
				disabled++
			}
		}
		if _, err := t.putAnnotation(self, annActorRecordsFilled, 1); err != nil {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpPatch, self.ID, self.Kind, nil)
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: open repository %s: fill its actor records: %w", ds.info.ID, err)
	}
	if began.IsZero() {
		return nil
	}
	// The line after the read, whatever it found, so the one before it is
	// never left open.
	level := slog.LevelInfo
	if written > 0 || len(unnamed) > 0 || len(person) > 0 {
		level = slog.LevelWarn
	}
	ds.svc.log.Log(ctx, level, "substrate: wrote the actor records the repository links and held none of",
		"repository", ds.info.ID, "written", written, "disabled", disabled,
		"unnamed", len(unnamed), "unnamedIDs", unnamed, "personWithoutRecord", person,
		"took", time.Since(began).Round(time.Millisecond))
	return nil
}

// linkedActorIDs is every actor id the repository links: each line's actor
// and the one it acted for, every mark's setBy and onBehalfOf in a line's
// payload and in the fold, and every reference to an actor record in a
// line's payload or a row's properties.
func (t *txn) linkedActorIDs() ([]string, error) {
	ref := `substrate\.reamde\.dev/core/actor/([0-9a-z]{26})`
	rows, err := t.query(`
		SELECT actor FROM changelog WHERE actor <> ''
		UNION SELECT on_behalf_of FROM changelog WHERE on_behalf_of <> ''
		UNION SELECT (regexp_matches(payload::text, '"(?:setBy|onBehalfOf)": "([0-9a-z]{26})"', 'g'))[1] FROM changelog
		UNION SELECT (regexp_matches(payload::text, $1, 'g'))[1] FROM changelog
		UNION SELECT (regexp_matches(props::text, $1, 'g'))[1] FROM records
		UNION SELECT set_by FROM property_marks WHERE set_by <> ''
		UNION SELECT on_behalf_of FROM property_marks WHERE on_behalf_of <> ''`, ref)
	if err != nil {
		return nil, fmt.Errorf("read the actors the repository links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if vocabulary.MintedID(id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), rows.Err()
}

// heldActorIDs is every actor record the repository holds, live or not.
func (t *txn) heldActorIDs() (map[string]bool, error) {
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1`, kindActor)
	if err != nil {
		return nil, fmt.Errorf("read the repository's actor records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	held := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		held[id] = true
	}
	return held, rows.Err()
}
