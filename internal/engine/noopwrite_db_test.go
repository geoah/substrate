package engine_test

// AN IDENTICAL WRITE MOVES NOTHING, WHATEVER THE SHAPE OF THE VALUE. The
// no-op suppression is old (core_db_test.go proves it on scalars), and a
// provider's sync leans on it hard: a Slack history page re-puts the
// conversation's own fields every run, and a version that moved would tell
// every trigger watching the mirror that something happened.
//
// What this file adds is the SHAPES. Suppression compares the stored row with
// the row the write produces, so any value the write path canonicalises,
// re-orders or re-derives on the way in is a way for an identical write to
// look different: a float, a repeated list, an object with fields, a
// reference, a datetime, the temporal trait's hot column, a title a
// displayTemplate derives. Each of those is a property a mirror kind actually
// declares.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const noopPackage = "mirrors.example.substrate.reamde.dev/mirrors"

// noopConversation is a mirror kind shaped like a provider's: the temporal
// trait's `at`, a derived title, and one property of every family a sync
// writes back unchanged on every run.
func noopConversation() map[string]any {
	return vocabulary.KindManifest(noopPackage,
		map[string]any{"singular": "conversation"},
		map[string]any{
			"traits":          []any{"temporal(point)"},
			"displayTemplate": "{name|conversationId}",
			"properties": map[string]any{
				"conversationId": map[string]any{"type": "string", "required": true},
				"name":           map[string]any{"type": "string"},
				"memberCount":    map[string]any{"type": "int"},
				"priority":       map[string]any{"type": "float"},
				"isPrivate":      map[string]any{"type": "bool"},
				"members":        map[string]any{"type": "string", "repeated": true},
				"lastReadAt":     map[string]any{"type": "datetime"},
				"purpose": map[string]any{
					"type": "object",
					"fields": map[string]any{
						"value":   map[string]any{"type": "string"},
						"creator": map[string]any{"type": "string"},
					},
				},
				"link":   map[string]any{"type": "url"},
				"labels": map[string]any{"type": "string", "keyed": true},
				"pins": map[string]any{
					"type":     "object",
					"repeated": true,
					"fields": map[string]any{
						"id": map[string]any{"type": "string"},
						"by": map[string]any{"type": "string"},
					},
				},
			},
		})
}

// noopProps is what the sync writes, every run, byte for byte.
func noopProps() map[string]any {
	return map[string]any{
		"conversationId": "C123",
		"name":           "general",
		"memberCount":    42,
		"priority":       0.0426,
		"isPrivate":      false,
		"members":        []any{"U1", "U2", "U3"},
		"lastReadAt":     "2026-09-16T10:11:12Z",
		"purpose":        map[string]any{"value": "the general channel", "creator": "U1"},
		"link":           "https://example.com/archives/C123",
		"labels":         map[string]any{"team": "core", "tier": "1"},
		"pins": []any{
			map[string]any{"id": "P1", "by": "U1"},
			map[string]any{"id": "P2", "by": "U2"},
		},
		"at": "2026-01-02T03:04:05Z",
	}
}

func TestAnIdenticalWriteMovesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	if err := enginetest.InstallAccountType(ctx, ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install the account kind: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(noopPackage, 0), noopConversation(),
	}); err != nil {
		t.Fatalf("declare the conversation kind: %v", err)
	}
	const conversation = noopPackage + "/conversation"

	first := mustPut(t, ds, slack, substrate.PutInput{
		Kind: conversation, ID: "C123", Properties: noopProps(),
	})

	// A trigger watching the mirror. Nothing here runs the dispatcher: a
	// delivery is a changelog row read afterwards, so the changelog assertion
	// below IS the trigger assertion — what is not in the log is never
	// delivered.
	head := maxSeq(t, ds)

	for _, round := range []struct {
		name  string
		write func() *substrate.Record
	}{
		{"a re-put of the same page", func() *substrate.Record {
			return mustPut(t, ds, slack, substrate.PutInput{
				Kind: conversation, ID: "C123", Properties: noopProps(),
			})
		}},
		{"a patch of the same page", func() *substrate.Record {
			return mustPatch(t, ds, slack, conversation, "C123", substrate.PatchInput{
				Properties: noopProps(),
			})
		}},
		{"a re-put whose timestamps carry another zone", func() *substrate.Record {
			// The same instants, spelled the way a second pass over the
			// same API payload can spell them. A mirror that churned on
			// this would churn on every sync.
			props := noopProps()
			props["lastReadAt"] = "2026-09-16T12:11:12+02:00"
			props["at"] = "2026-01-02T04:04:05+01:00"
			return mustPut(t, ds, slack, substrate.PutInput{
				Kind: conversation, ID: "C123", Properties: props,
			})
		}},
		{"a patch of one unchanged property", func() *substrate.Record {
			return mustPatch(t, ds, slack, conversation, "C123", substrate.PatchInput{
				Properties: map[string]any{"name": "general"},
			})
		}},
	} {
		t.Run(round.name, func(t *testing.T) {
			got := round.write()
			if got.Version != first.Version {
				t.Errorf("version %d → %d", first.Version, got.Version)
			}
			if !got.UpdatedAt.Equal(first.UpdatedAt) {
				t.Errorf("updatedAt %v → %v", first.UpdatedAt, got.UpdatedAt)
			}
			if rows := changesSince(t, ds, head); len(rows) != 0 {
				t.Errorf("wrote %d changelog rows, so every trigger watching the mirror fires: %+v", len(rows), rows)
			}
			for name, want := range noopProps() {
				if name == "at" {
					continue // the hot column, checked below
				}
				if !jsonSame(got.Properties[name], want) {
					t.Errorf("%s read back as %#v, want %#v", name, got.Properties[name], want)
				}
			}
			if at, _ := got.Properties["at"].(string); at != "" {
				if parsed, err := time.Parse(time.RFC3339, at); err != nil || !parsed.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
					t.Errorf("at = %v", got.Properties["at"])
				}
			}
		})
		if t.Failed() {
			return
		}
	}

	// And a real change still moves: one bump, one row.
	changed := mustPatch(t, ds, slack, conversation, "C123", substrate.PatchInput{
		Properties: map[string]any{"memberCount": 43},
	})
	if changed.Version != first.Version+1 {
		t.Fatalf("a changed property left the version at %d", changed.Version)
	}
	if rows := changesSince(t, ds, head); len(rows) != 1 {
		t.Fatalf("a changed property wrote %d changelog rows", len(rows))
	}
}

// jsonSame compares two decoded values the way the row comparison does.
func jsonSame(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
