package engine

// THE TITLES AFTER THE LOCAL NAMESPACE (decision record 0259). Conversion
// step 6 never edits a title's text: a title a writer gave is theirs, and one
// the engine rendered from a display template is what it rendered then,
// which the line keeps. A record's current title moves once, at the open
// after the stored vocabulary loads (a repository migration runs before it
// loads and could not render a kind of the repository's own), and only where
// the stored title is the OLD SPELLING of what the engine renders now: the
// same text, with one of the repository's names where the rendering has `~`.
// That equality is the proof that the engine wrote the title and that only
// the spelling moved. Every other title stays exactly as stored, one that
// spells a name included, and one a template change or a renamed referent
// left stale.
//
// One title may render another's (a trigger's renders its callable's), so
// the pass goes round, rendering against the titles it has moved so far,
// until a round moves nothing, and then writes each title it moved once, as
// an ordinary record write by the system (decision record 0099): a trigger
// watching one of those kinds runs for it once.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// annTitlesRenderedLocal is the mark renderLocalTitles leaves on the
// repository's own record once it has rendered them; a repository registered
// by this binary is born with it.
const annTitlesRenderedLocal = "substrate/titles-rendered-local"

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
	rendered := 0
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		for _, name := range ds.names() {
			moved, err := t.renderTitlesSpelling(name)
			if err != nil {
				return err
			}
			rendered += moved
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

// spelledTitle is one live row whose stored title spells the name, of a kind
// that renders its title from a template: its address, its title as read,
// and the title the pass has moved it to so far. The pass holds no more of a
// row than that, and locks a row only to write it.
type spelledTitle struct {
	ref    eref
	ty     *vocabulary.Kind
	stored string
	title  string
}

// renderTitlesSpelling moves every title that is the old spelling of its
// rendering under name, and says how many moved.
func (t *txn) renderTitlesSpelling(name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	rows, err := t.query(`SELECT kind, id, title FROM records WHERE deleted_at IS NULL AND strpos(title, $1) > 0 ORDER BY kind, id`, name)
	if err != nil {
		return 0, fmt.Errorf("read the titles that spell the name: %w", err)
	}
	var read []*spelledTitle
	for rows.Next() {
		st := &spelledTitle{}
		if err := rows.Scan(&st.ref.Kind, &st.ref.ID, &st.stored); err != nil {
			_ = rows.Close()
			return 0, err
		}
		read = append(read, st)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	var titles []*spelledTitle
	spellings := 0
	for _, st := range read {
		if st.ty, _ = t.derivationView().ByIdentity(st.ref.Kind); st.ty == nil || st.ty.Template == nil {
			continue
		}
		st.title = st.stored
		titles = append(titles, st)
		spellings += strings.Count(st.stored, name)
	}
	// Nothing is written until the rounds end, so a title renders the titles
	// it reads through as moved so far from this map.
	moving := map[eref]string{}
	// A move takes at least one spelling of the name out of one title and
	// adds none, so the rounds that move cannot outnumber the spellings. A
	// pass that reaches the bound fails the open rather than leave the mark
	// over titles still moving.
	for round := 0; ; round++ {
		if round > spellings {
			return 0, fmt.Errorf("the titles that spell the name still moved after %d rounds", round)
		}
		moved := 0
		for _, st := range titles {
			row, err := t.loadRow(st.ref, false)
			if err != nil {
				return 0, err
			}
			if row == nil || row.DeletedAt != nil {
				continue
			}
			row.Title = st.title
			now := st.ty.Template.Render(&titleResolver{t: t, ty: st.ty, row: row, titles: moving})
			if !spelledBefore(st.title, now, name) {
				continue
			}
			st.title = now
			moving[st.ref] = now
			moved++
		}
		if moved == 0 {
			break
		}
	}
	if t.ds.svc.testAfterTitleRounds != nil {
		t.ds.svc.testAfterTitleRounds()
	}
	written := 0
	for _, st := range titles {
		if st.title == st.stored {
			continue
		}
		row, err := t.loadRow(st.ref, true)
		if err != nil {
			return 0, err
		}
		// A title that changed since it was read was written since, and
		// that write rendered it.
		if row == nil || row.DeletedAt != nil || row.Title != st.stored {
			continue
		}
		before := row.clone()
		row.Title = st.title
		res, err := t.foldRow(before, row, false, false)
		if err != nil {
			return 0, err
		}
		if !res.changed {
			continue
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, row.ID, row.Kind, nil); err != nil {
			return 0, err
		}
		written++
	}
	return written, nil
}

// spelledBefore reports whether stored is rendering as the engine spelled it
// before the local namespace: the same text, with name in place of one or
// more of the `~` that stand where a name stood (tildeStands), and `~` at the
// others. A title a round already moved in part holds both.
func spelledBefore(stored, rendering, name string) bool {
	i, j, respelled := 0, 0, false
	for i < len(rendering) {
		if rendering[i] == '~' && tildeStands(rendering, i) && strings.HasPrefix(stored[j:], name) {
			i, j, respelled = i+1, j+len(name), true
			continue
		}
		if j == len(stored) || stored[j] != rendering[i] {
			return false
		}
		i, j = i+1, j+1
	}
	return respelled && j == len(stored)
}

// tildeStands reports whether the `~` at i stands where a name stood: one
// followed by `/`, as in `~/tasks/task`, whatever precedes it, since what
// precedes it is the same text in the title and its rendering; and one not
// inside a longer word, as in `bundle:~:tasks` or a bare `~`.
func tildeStands(s string, i int) bool {
	if i+1 < len(s) && s[i+1] == '/' {
		return true
	}
	return (i == 0 || !nameByte(s[i-1])) && (i+1 == len(s) || !nameByte(s[i+1]))
}

// nameByte is a byte a repository name, or a word around one, is spelled
// with.
func nameByte(b byte) bool {
	return b == '.' || b == '-' || b == '_' || b == '~' ||
		'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || '0' <= b && b <= '9'
}
