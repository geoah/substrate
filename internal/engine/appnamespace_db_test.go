package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A token minted before decision 0209 wrote its labels and annotations under
// `app:<client id host>`. Its actor is now `app:local` or `app:<authority>`,
// and the keys it wrote stay its to change.
func TestAnAppKeepsWritingTheKeysItsTokenWroteBeforeItsActorMoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	pkg := ds.Repository().Authority + "/notes"
	kind := pkg + "/note"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.BundleManifest(pkg, map[string]any{"installs": []any{kind}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{"properties": map[string]any{
			"text": map[string]any{"type": "text"},
		}}),
	}); err != nil {
		t.Fatalf("declare notes: %v", err)
	}
	for _, c := range []struct {
		name, clientID string
		before, now    substrate.Actor
	}{
		{"a loopback app, now app:local", "http://127.0.0.1:43123/cli", substrate.AppActor("127.0.0.1:43123"), substrate.AppActorLocal},
		{"a verified app, now app:<authority>", "https://cdn.example.net/workouts/client.json", substrate.AppActor("cdn.example.net"), substrate.AppActor("example.org")},
	} {
		t.Run(c.name, func(t *testing.T) {
			app := substrate.WithClientID(ctx, c.clientID)
			old := string(c.before) + "/seen"
			id := "n-" + string(c.now)[len("app:"):]
			rec, err := ds.Put(app, c.before, substrate.PutInput{
				Kind: kind, ID: fid(vocabularySafeID(id)), Properties: map[string]any{"text": "x"},
				Labels: map[string]any{old: "1"}, Annotations: map[string]any{old: "first"},
			})
			if err != nil {
				t.Fatalf("the earlier binary's write: %v", err)
			}
			if _, err := ds.Patch(app, c.now, kind, rec.ID, substrate.PatchInput{
				Labels: map[string]any{old: "2", string(c.now) + "/seen": "1"}, Annotations: map[string]any{old: "second"},
			}); err != nil {
				t.Fatalf("the same token under its new actor rewrites its old key: %v", err)
			}
			other := substrate.WithClientID(ctx, "https://notes.example.org/client.json")
			if _, err := ds.Patch(other, c.now, kind, rec.ID, substrate.PatchInput{
				Labels: map[string]any{old: "3"},
			}); !errors.Is(err, substrate.ErrForbidden) {
				t.Fatalf("another client writes the old key: %v, want forbidden", err)
			}
		})
	}
}

// vocabularySafeID keeps a test id inside the record id alphabet.
func vocabularySafeID(s string) string {
	out := []byte(s)
	for i, c := range out {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			out[i] = '-'
		}
	}
	return string(out)
}
