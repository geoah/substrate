package engine

// The records table has no column for one property, and a write's history
// line carries `body` among its properties, as it carries any other.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestBodyHasNoColumnAndNoHistoryFieldOfItsOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	var columns int
	if err := rawDB(t, ds).QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'records' AND column_name = 'body'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Fatal("records still has a body column")
	}

	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(bodyPackage, 0),
		vocabulary.KindManifest(bodyPackage, map[string]any{"singular": "letter"}, map[string]any{
			"properties": map[string]any{"body": map[string]any{"type": "text"}},
		}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	rec, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: bodyLetter, Properties: map[string]any{"body": "the falcon dives"}})
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := rawDB(t, ds).QueryRowContext(ctx, `SELECT payload FROM changelog WHERE kind = $1 AND record_id = $2`,
		bodyLetter, rec.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Fold []struct {
			Kind  string                     `json:"kind"`
			Delta map[string]json.RawMessage `json:"delta"`
		} `json:"fold"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, eff := range payload.Fold {
		if eff.Kind != string(foldRecord) {
			continue
		}
		if _, own := eff.Delta["body"]; own {
			t.Fatalf("the delta carries body as a field of its own: %s", raw)
		}
		var set map[string]any
		_ = json.Unmarshal(eff.Delta["set"], &set)
		found = found || set["body"] == "the falcon dives"
	}
	if !found {
		t.Fatalf("the history line does not carry the body among its properties: %s", raw)
	}
}
