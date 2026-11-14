package engine

// THE TITLES AFTER THE LOCAL NAMESPACE (decision record 0259). Conversion
// step 6 never edits a title's text: a title a writer gave is theirs, and one
// the engine rendered from a display template is what it rendered then,
// which the line keeps. A rendered title is derived, so a record's current
// one is rendered again from the record as it reads after the conversion, by
// the engine's own renderer (deriveTitle), once, at the open after the
// stored vocabulary loads: a repository migration runs before it loads and
// could not render a kind of the repository's own. It reads only the rows
// whose title spells one of the repository's names, renders a row only where its
// kind renders titles from a template, writes only where the title moves,
// as ordinary record writes by the system, and goes round again while a
// title moved, since one title may render another record's (a trigger's
// renders its callable's).

import (
	"context"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// annTitlesRenderedLocal is the mark renderLocalTitles leaves on the
// repository's own record once it has rendered them; a repository registered
// by this binary is born with it.
const annTitlesRenderedLocal = "substrate/titles-rendered-local"

// localTitlePasses bounds the rounds: a title renders another record's
// title through a reference, and no shipped template chains further than
// that twice.
const localTitlePasses = 4

// renderLocalTitles is the open's pass (above).
func (ds *dataset) renderLocalTitles(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	self := eref{Kind: kindRepository, ID: ds.info.ID}
	var done bool
	if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3)`,
		self.Kind, self.ID, annTitlesRenderedLocal).Scan(&done); err != nil {
		return fmt.Errorf("substrate/engine: open repository %s: read whether its titles are rendered: %w", ds.info.ID, err)
	}
	if done {
		return nil
	}
	names := ds.names()
	rendered := 0
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		for range localTitlePasses {
			moved := 0
			for _, name := range names {
				n, err := t.renderTitlesSpelling(name)
				if err != nil {
					return err
				}
				moved += n
			}
			rendered += moved
			if moved == 0 {
				break
			}
		}
		if _, err := t.putAnnotation(self, annTitlesRenderedLocal, 1); err != nil {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpPatch, self.ID, self.Kind, nil)
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: open repository %s: render its titles under ~: %w", ds.info.ID, err)
	}
	if rendered > 0 {
		ds.svc.log.Info("substrate: rendered again the titles that spelled the repository's name",
			"repository", logSafeID(ds.info.ID), "titles", rendered)
	}
	return nil
}

// renderTitlesSpelling renders again the title of every live row whose title
// spells name and whose kind renders titles from a template, and says how
// many moved.
func (t *txn) renderTitlesSpelling(name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	rows, err := t.query(`SELECT kind, id FROM records WHERE deleted_at IS NULL AND strpos(title, $1) > 0 ORDER BY kind, id`, name)
	if err != nil {
		return 0, fmt.Errorf("read the titles that spell the name: %w", err)
	}
	var held []eref
	for rows.Next() {
		var r eref
		if err := rows.Scan(&r.Kind, &r.ID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		held = append(held, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	moved := 0
	for _, ref := range held {
		ty, _ := t.derivationView().ByIdentity(ref.Kind)
		if ty == nil || ty.Template == nil {
			continue
		}
		row, err := t.loadRow(ref, true)
		if err != nil {
			return 0, err
		}
		if row == nil || row.DeletedAt != nil {
			continue
		}
		title, err := t.deriveTitle(ty, row)
		if err != nil {
			return 0, err
		}
		if title == row.Title {
			continue
		}
		before := row.clone()
		row.Title = title
		res, err := t.foldRow(before, row, false, false)
		if err != nil {
			return 0, err
		}
		if !res.changed {
			continue
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, ref.ID, ref.Kind, nil); err != nil {
			return 0, err
		}
		moved++
	}
	return moved, nil
}
