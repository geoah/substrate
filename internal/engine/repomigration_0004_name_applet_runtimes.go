package engine

// Repository migration 4: every applet names its runtime (decision record
// 0244).
//
// The applets package's first version had no runtime: every applet was A2UI
// screens. Its second requires one, with no default, so an applet says what
// draws it and a runtime added later is a new value rather than a guess about
// what an applet written before it meant. The shipped upgrade that makes it
// required is refused while a live applet lacks it, so this writes a2ui, what
// each of them was, into every live applet that names none, before the stored
// vocabulary loads and the upgrade runs.
//
// The rows are rewritten through the fold, as ordinary record writes, so a
// rebuild replays them. An applet that names a runtime is left as it is, and
// a second run finds nothing to do.

import (
	"fmt"
	"maps"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/substrate"
)

const appletRuntimeProp = "runtime"

func migrateNameAppletRuntimes(t *txn) (string, error) {
	ids, err := t.liveIDs(applet.Kind)
	if err != nil {
		return "", err
	}
	named := 0
	for _, id := range ids {
		row, err := t.loadRow(eref{Kind: applet.Kind, ID: id}, true)
		if err != nil {
			return "", err
		}
		if row == nil {
			continue
		}
		if runtime, _ := row.Props[appletRuntimeProp].(string); runtime != "" {
			continue
		}
		before := row.clone()
		// A copy, so the row loaded before the write still reads as it was
		// and the fold sees the difference.
		row.Props = maps.Clone(row.Props)
		row.Props[appletRuntimeProp] = applet.RuntimeA2UI
		res, err := t.foldRow(before, row, false, false)
		if err != nil {
			return "", err
		}
		if !res.changed {
			continue
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, id, applet.Kind,
			map[string]any{"properties": []string{appletRuntimeProp}}); err != nil {
			return "", err
		}
		named++
	}
	if named == 0 {
		return "every applet names its runtime", nil
	}
	return fmt.Sprintf("named the a2ui runtime on %d applets written before an applet named one", named), nil
}
