package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// THE MARK NAMES NO TIER (decision 0284). Nothing a history line holds,
// nothing a resync restates and nothing a read serves names a tier, a
// manager or the token behind a value: a mark is pinned, setBy, onBehalfOf,
// derivedFrom and setAt.
func TestTheMarkNamesNoTierManagerOrToken(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[foldOp](), reflect.TypeFor[foldRows](), reflect.TypeFor[foldMarkRow](),
		reflect.TypeFor[substrate.Record](), reflect.TypeFor[substrate.PropertyMark](),
		reflect.TypeFor[substrate.PropertyAlternative](),
	} {
		for i := range typ.NumField() {
			key, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			switch key {
			case "tier", "manager", "managers", "principal", "propertyMeta", "actor":
				t.Errorf("%s.%s is on the wire as %q", typ.Name(), typ.Field(i).Name, key)
			}
		}
	}
}

// markWordsKept is where the manager rows' words may stay: what holds history
// as it was written, the frozen migrations that still name the table, and the
// pages that say what replaced them.
var markWordsKept = []string{
	"docs/decisions/",                  // accepted records are frozen
	"docs/plans/",                      // dated design notes, as written
	"CHANGELOG.md",                     // what each release said
	"internal/testenv/testdata/kept/",  // a release's directory, as it wrote it
	"internal/testenv/kept_db_test.go", // reads that directory's served shape
	"apps/workouts/WorkoutsKit/Tests/WorkoutsSyncTests/Fixtures/",               // responses an older server served
	"internal/engine/migrations/0001_init.up.sql",                               // frozen: created the table
	"internal/engine/migrations/0026_property_marks.up.sql",                     // drops it, and keeps its name for 0005
	"internal/engine/repomigration_0005_remove_reply_relay_contacts.go",         // frozen: reads it
	"internal/engine/conversion_0002_drop_recovery_wrap_test.go",                // the old lines it converts
	"internal/engine/conversion_0003_link_actors.go",                            // converts what old tool results echoed
	"internal/engine/actorlinks_test.go",                                        // the old lines it converts
	"internal/engine/actormarker_test.go",                                       // the old echoes it converts
	"internal/engine/conversion_0005_marks.go",                                  // converts the old manager effects
	"internal/engine/marklines_test.go",                                         // the old lines it converts
	"internal/engine/conversion_0005_marks_internal_db_test.go",                 // the old lines it adds to a history
	"internal/engine/marks.go",                                                  // setManager, for frozen migration 0006
	"internal/engine/repomigration_0002_qualify_bare_selector_kinds.go",         // frozen
	"internal/engine/repomigration_0006_key_the_repository_record_by_its_id.go", // frozen
	"internal/engine/repomigration_0009_link_actors.go",                         // reads the old lines' manager effects
	"internal/engine/migrations/0021_identity_columns_byte_order.up.sql",        // frozen: sorted the table
	"internal/engine/collation_db_test.go",                                      // holds the view 0025 keeps to byte order
	"internal/testenv/keptchecks_db_test.go",                                    // what each step does to that served shape
	"docs/stored-format.md",                                                     // the old dialects' lines
	"internal/changelogfile/testdata/vectors.json",                              // the old dialects' line vectors
	"internal/engine/migrations/0004_property_offers_source.up.sql",             // frozen
	"internal/testenv/testdata/kinds-before-llm-move/",                          // an old release's declarations
	"internal/engine/markwords_test.go",                                         // this list
	"internal/api/features.go",                                                  // says what revision 4 replaced
	"docs/api.md",                                                               // the same, in the feature table
}

// markWordsGone are the spellings of what a mark replaced: the table, the
// wire field, the row, the effect, and the tier a value was held at
// (`machine-held`, `owner-held`, `manager tier`). "Tier" stays in its other
// senses, each a word of its own and none of them a value's: the catalog's,
// the observations rollups', search's ranking, a provider's rate limits, and
// the writer's, which says what a hand may write and is step 16's.
var markWordsGone = []string{
	"property_managers", "propertyMeta", "PropertyMeta", "manager row",
	`"kind":"manager"`, `"kind": "manager"`, "foldManager",
	"machine-held", "owner-held", "bundle-held", "manager tier", "held at the machine tier",
}

// NO LIVE FILE NAMES THE MANAGER ROWS. The table, the wire field and the
// Go type are gone; a file outside markWordsKept that still names one is
// code or a page that did not move.
func TestNoLiveFileNamesTheManagerRows(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", ".dev", "bin", "build", ".build", ".gradle":
				return filepath.SkipDir
			}
			return nil
		}
		for _, kept := range markWordsKept {
			if strings.HasPrefix(rel, kept) {
				return nil
			}
		}
		switch filepath.Ext(path) {
		case ".go", ".ts", ".tsx", ".md", ".yaml", ".yml", ".sql", ".swift", ".kt", ".json", ".py", ".sh":
		default:
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, word := range markWordsGone {
			if strings.Contains(string(raw), word) {
				t.Errorf("%s names %s", rel, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
