package engine_test

// An object field's `default:` (issue 248) fills a field the object value a
// write sends leaves out, and nothing else: never an object the writer did not
// send, never a stored object a patch does not touch. It is filled at the
// write, so the row and the changelog delta carry the same value and a
// rebuild replays it.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	fieldDefaultPackage = "fielddefault.example.substrate.reamde.dev/fielddefault"
	fieldDefaultProfile = fieldDefaultPackage + "/profile"
)

// fieldDefaultDocs declares one kind with a field default in each container:
// a single object (with a nested object the default must not invent), a
// repeated one and a keyed one. withDefaults false is the same shape with no
// default anywhere, for the rows a declaration change finds already stored.
func fieldDefaultDocs(withDefaults bool) []map[string]any {
	field := func(d map[string]any, value any) map[string]any {
		if withDefaults {
			d["default"] = value
		}
		return d
	}
	return []map[string]any{
		vocabulary.PackageManifest(fieldDefaultPackage, 0),
		vocabulary.KindManifest(fieldDefaultPackage,
			map[string]any{"singular": "profile"},
			map[string]any{"properties": map[string]any{
				"note": map[string]any{"type": "string"},
				"contact": map[string]any{"type": "object", "fields": map[string]any{
					"email":  map[string]any{"type": "email"},
					"locale": field(map[string]any{"type": "string"}, "en"),
					"home": map[string]any{"type": "object", "fields": map[string]any{
						"street":  map[string]any{"type": "string"},
						"country": field(map[string]any{"type": "string"}, "GR"),
					}},
				}},
				"visits": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
					"day":   map[string]any{"type": "string"},
					"count": field(map[string]any{"type": "int"}, 1),
				}},
				"channels": map[string]any{"type": "object", "keyed": true, "fields": map[string]any{
					"address": map[string]any{"type": "string"},
					"locale":  field(map[string]any{"type": "string"}, "en"),
				}},
			}}),
	}
}

func applyFieldDefaults(t *testing.T, ds substrate.Dataset, withDefaults bool) {
	t.Helper()
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, fieldDefaultDocs(withDefaults)); err != nil {
		t.Fatalf("apply the field default vocabulary (defaults %t): %v", withDefaults, err)
	}
}

// wantProps compares each named property as its JSON (sameJSON): a stored
// entry decodes a number as json.Number, and the row reads it as a Go number.
func wantProps(t *testing.T, what string, got, want map[string]any) {
	t.Helper()
	for name, v := range want {
		if !sameJSON(t, got[name], v) {
			t.Errorf("%s: %s = %s, want %s", what, name, jsonOf(t, got[name]), jsonOf(t, v))
		}
	}
}

// deltaOf is the `set` half of the one stored entry after seq that wrote the
// record.
func deltaOf(t *testing.T, svc substrate.Service, ds substrate.Dataset, after int64, id string) map[string]any {
	t.Helper()
	var set map[string]any
	for _, ch := range storedChangesSince(t, svc, ds, after) {
		if ch.RecordID != id {
			continue
		}
		if set != nil {
			t.Fatalf("more than one entry after seq %d wrote %s", after, id)
		}
		set = recordDeltaOf(t, ch)
	}
	if set == nil {
		t.Fatalf("no entry after seq %d wrote %s", after, id)
	}
	return set
}

func TestFieldDefaultFillsTheObjectsACreateSends(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newCoreDataset(t)
	applyFieldDefaults(t, ds, true)

	head := maxSeq(t, ds)
	created := mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "p1",
		Properties: map[string]any{
			"contact": map[string]any{"email": "a@example.com"},
			"visits": []any{
				map[string]any{"day": "monday"},
				map[string]any{"day": "tuesday", "count": 5},
			},
			"channels": map[string]any{
				"work": map[string]any{"address": "work@example.com"},
				"home": map[string]any{"address": "home@example.com", "locale": "el"},
			},
		},
	})
	// The nested `home` object was not sent, so its field default builds
	// nothing: `contact` holds exactly the field it was sent and the default.
	want := map[string]any{
		"contact": map[string]any{"email": "a@example.com", "locale": "en"},
		"visits": []any{
			map[string]any{"day": "monday", "count": 1},
			map[string]any{"day": "tuesday", "count": 5},
		},
		"channels": map[string]any{
			"work": map[string]any{"address": "work@example.com", "locale": "en"},
			"home": map[string]any{"address": "home@example.com", "locale": "el"},
		},
	}
	wantProps(t, "the create's answer", created.Properties, want)
	wantProps(t, "the stored row", mustGet(t, ds, fieldDefaultProfile, "p1").Properties, want)
	wantProps(t, "the changelog delta", deltaOf(t, svc, ds, head, "p1"), want)

	// A nested object the writer DID send gets its own field's default, and a
	// field the writer set to null keeps no value, as a null on a kind's own
	// property does.
	nested := mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "p2",
		Properties: map[string]any{
			"contact": map[string]any{"locale": nil, "home": map[string]any{"street": "Odos 1"}},
		},
	})
	wantProps(t, "a nested object", nested.Properties, map[string]any{
		"contact": map[string]any{"home": map[string]any{"street": "Odos 1", "country": "GR"}},
	})

	// The rebuild replays the entries, so the defaults survive it because
	// they are in the deltas, not recomputed on the way out.
	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
	wantProps(t, "the rebuilt row", mustGet(t, ds, fieldDefaultProfile, "p1").Properties, want)
}

// A field default fills an object the writer sent. A create that sends no
// object stores none: not the single one, not a list, not a map.
func TestFieldDefaultNeverInventsAnObject(t *testing.T) {
	t.Parallel()
	svc, ds := newCoreDataset(t)
	applyFieldDefaults(t, ds, true)

	head := maxSeq(t, ds)
	created := mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "bare",
		Properties: map[string]any{"note": "no objects"},
	})
	for _, rec := range []map[string]any{
		created.Properties,
		mustGet(t, ds, fieldDefaultProfile, "bare").Properties,
		deltaOf(t, svc, ds, head, "bare"),
	} {
		for _, name := range []string{"contact", "visits", "channels"} {
			if v, held := rec[name]; held {
				t.Errorf("a create that sent no %s stored one: %s", name, jsonOf(t, v))
			}
		}
	}

	// An empty object is an object the writer sent, and so is an empty item
	// of a list; each takes its defaults. An empty list or map holds no
	// object to fill.
	filled := mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "empty",
		Properties: map[string]any{
			"contact":  map[string]any{},
			"visits":   []any{map[string]any{}},
			"channels": map[string]any{},
		},
	})
	wantProps(t, "empty objects", filled.Properties, map[string]any{
		"contact":  map[string]any{"locale": "en"},
		"visits":   []any{map[string]any{"count": 1}},
		"channels": map[string]any{},
	})
}

// A patch or a put that writes the object replaces it whole, so the object it
// sends takes the defaults exactly as a create's does. One that does not
// write the object leaves the stored object as it is, even an object stored
// before the default was declared.
func TestFieldDefaultFillsOnlyTheObjectsAWriteSends(t *testing.T) {
	t.Parallel()
	svc, ds := newCoreDataset(t)
	applyFieldDefaults(t, ds, false)

	// Stored before any default existed: this object lacks `locale`.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "old",
		Properties: map[string]any{
			"contact": map[string]any{"email": "old@example.com"},
			"visits":  []any{map[string]any{"day": "monday"}},
		},
	})
	applyFieldDefaults(t, ds, true)

	// A patch and a put that do not name the objects leave them alone, and
	// their deltas carry nothing for them.
	head := maxSeq(t, ds)
	patched := mustPatch(t, ds, owner, fieldDefaultProfile, "old", substrate.PatchInput{
		Properties: map[string]any{"note": "patched"},
	})
	unchanged := map[string]any{
		"contact": map[string]any{"email": "old@example.com"},
		"visits":  []any{map[string]any{"day": "monday"}},
	}
	wantProps(t, "a patch that does not touch the objects", patched.Properties, unchanged)
	if set := deltaOf(t, svc, ds, head, "old"); set["contact"] != nil || set["visits"] != nil {
		t.Fatalf("the patch's delta carries an object it did not write: %v", set)
	}
	head = maxSeq(t, ds)
	put := mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "old",
		Properties: map[string]any{"note": "put"},
	})
	wantProps(t, "a put that does not touch the objects", put.Properties, unchanged)
	if set := deltaOf(t, svc, ds, head, "old"); set["contact"] != nil || set["visits"] != nil {
		t.Fatalf("the put's delta carries an object it did not write: %v", set)
	}

	// A patch that writes `contact` replaces it, and the replacement takes the
	// default; `visits`, untouched, still lacks its count.
	head = maxSeq(t, ds)
	patched = mustPatch(t, ds, owner, fieldDefaultProfile, "old", substrate.PatchInput{
		Properties: map[string]any{"contact": map[string]any{"email": "new@example.com"}},
	})
	wantProps(t, "a patch that writes the object", patched.Properties, map[string]any{
		"contact": map[string]any{"email": "new@example.com", "locale": "en"},
		"visits":  []any{map[string]any{"day": "monday"}},
	})
	set := deltaOf(t, svc, ds, head, "old")
	wantProps(t, "the patch's delta", set, map[string]any{
		"contact": map[string]any{"email": "new@example.com", "locale": "en"},
	})
	if _, carried := set["visits"]; carried {
		t.Fatalf("the patch's delta carries the list it did not write: %v", set)
	}

	// A put that writes the list and the map fills each item and each value.
	put = mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "old",
		Properties: map[string]any{
			"visits":   []any{map[string]any{"day": "monday"}, map[string]any{"day": "friday", "count": 2}},
			"channels": map[string]any{"work": map[string]any{"address": "w@example.com"}},
		},
	})
	wantProps(t, "a put that writes the list and the map", put.Properties, map[string]any{
		"contact":  map[string]any{"email": "new@example.com", "locale": "en"},
		"visits":   []any{map[string]any{"day": "monday", "count": 1}, map[string]any{"day": "friday", "count": 2}},
		"channels": map[string]any{"work": map[string]any{"address": "w@example.com", "locale": "en"}},
	})
}

// A restoring put holds every value the tombstone kept to what a put naming it
// would store (decision 0144), and a put naming an object fills its field
// defaults, so the restored object carries them. The delta carries the filled
// value, and a rebuild replays it.
func TestRestoreFillsAFieldDefaultTheTombstonedObjectLacks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newCoreDataset(t)
	applyFieldDefaults(t, ds, false)
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "gone",
		Properties: map[string]any{"contact": map[string]any{"email": "gone@example.com"}},
	})
	if _, err := ds.Delete(ctx, owner, fieldDefaultProfile, "gone", substrate.DeleteInput{}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	applyFieldDefaults(t, ds, true)

	head := maxSeq(t, ds)
	restored := mustPut(t, ds, owner, substrate.PutInput{
		Kind: fieldDefaultProfile, ID: "gone",
		Properties: map[string]any{"note": "back"},
	})
	want := map[string]any{"contact": map[string]any{"email": "gone@example.com", "locale": "en"}}
	wantProps(t, "the restored record", restored.Properties, want)
	wantProps(t, "the restoring delta", deltaOf(t, svc, ds, head, "gone"), want)

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
}

// `required` beside a field's `default` is satisfied by the fill on every
// write that sends the object. On a declaration change the default backfills
// nothing, so a field turning required while stored objects lack it is still
// refused, and the refusal says the default will not fill them.
func TestRequiredFieldWithADefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	pkg := fieldDefaultPackage + "req"
	docs := func(required, withDefault bool) []map[string]any {
		tier := map[string]any{"type": "enum", "values": []any{"free", "paid"}}
		if required {
			tier["required"] = true
		}
		if withDefault {
			tier["default"] = "free"
		}
		return []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "plan"},
				map[string]any{"properties": map[string]any{
					"billing": map[string]any{"type": "object", "fields": map[string]any{
						"tier":  tier,
						"email": map[string]any{"type": "email"},
					}},
				}}),
		}
	}
	kind := pkg + "/plan"
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs(false, false)); err != nil {
		t.Fatalf("apply the optional field: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "stored",
		Properties: map[string]any{"billing": map[string]any{"email": "a@example.com"}},
	})

	_, err := ds.ApplyVocabularyDocuments(ctx, owner, docs(true, true))
	wantNarrowingGuard(t, err, `field "tier" becomes required`, "1 live records", "backfills no stored one")
	if got := mustGet(t, ds, kind, "stored").Properties["billing"]; !sameJSON(t, got, map[string]any{"email": "a@example.com"}) {
		t.Fatalf("a refused declaration touched a stored object: %s", jsonOf(t, got))
	}

	// Written with a value, the stored object no longer blocks the pair.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "stored",
		Properties: map[string]any{"billing": map[string]any{"email": "a@example.com", "tier": "paid"}},
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs(true, true)); err != nil {
		t.Fatalf("the pair must land once no stored object lacks the field: %v", err)
	}
	created := mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "new",
		Properties: map[string]any{"billing": map[string]any{"email": "b@example.com"}},
	})
	wantProps(t, "a required field left out", created.Properties, map[string]any{
		"billing": map[string]any{"email": "b@example.com", "tier": "free"},
	})
	// An explicit null is the writer saying the object holds no value, which
	// the required field then refuses rather than refilling.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: kind, ID: "nulled",
		Properties: map[string]any{"billing": map[string]any{"tier": nil}},
	})
	wantProblem(t, err, "props.billing: .tier: this field is required")
}

// A field default no write could store is refused at admission, as a
// property's is: the value's own rules (a pattern here) are the write path's,
// and a required field's default may not be the empty value `required`
// refuses.
func TestFieldDefaultMustBeStorable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	pkg := fieldDefaultPackage + "bad"
	for name, tc := range map[string]struct {
		field map[string]any
		want  string
	}{
		"a default the pattern refuses": {
			field: map[string]any{"type": "string", "pattern": "^[a-z]{2}$", "default": "ENG"},
			want:  `property "prefs" field "code": default ENG`,
		},
		"an empty default on a required field": {
			field: map[string]any{"type": "string", "required": true, "default": ""},
			want:  `property "prefs" field "code": default "": a required field's default holds a value`,
		},
	} {
		_, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "prefs"},
				map[string]any{"properties": map[string]any{
					"prefs": map[string]any{"type": "object", "fields": map[string]any{"code": tc.field}},
				}}),
		})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want it to name %q", name, err, tc.want)
		}
	}
}
