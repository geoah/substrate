package engine

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aaApp is the app the coach tests declare under: an app's actor declares a
// package like any other hand, and nothing locks the package to it
// (decision 0214).
var aaApp = substrate.AppActor("workouts.example.org")

// The coach the Workouts app ships loads as an ordinary agent of its
// package: nothing beyond the agent loader admits or refuses it.
func TestTheShippedCoachLoads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	home := ds.Repository().Authority
	training, coachPkg := home+"/training", home+"/trainingcoach"
	memory, routine := training+"/memory", training+"/routine"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "anthropic", Properties: map[string]any{
		"wire": "anthropic", "pricing": []any{map[string]any{"model": "claude-sonnet-5-5", "inputPer1M": "2", "outputPer1M": "10"}},
	}}); err != nil {
		t.Fatal(err)
	}
	str := map[string]any{"type": "string"}
	if _, err := ds.ApplyVocabularyDocuments(ctx, aaApp, []map[string]any{
		vocabulary.PackageManifest(training, 0),
		vocabulary.BundleManifest(training, map[string]any{"installs": []any{routine, memory}}),
		vocabulary.KindManifest(training, map[string]any{"singular": "routine"}, map[string]any{"properties": map[string]any{"name": str}}),
		vocabulary.KindManifest(training, map[string]any{"singular": "memory"}, map[string]any{"properties": map[string]any{
			"text": map[string]any{"type": "text"}, "topic": str, "source": str, "until": map[string]any{"type": "date"},
			"exercise": str, "active": map[string]any{"type": "bool", "default": true},
		}}),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, aaApp, []map[string]any{
		vocabulary.PackageManifest(coachPkg, 0),
		vocabulary.BundleManifest(coachPkg, map[string]any{"installs": []any{coachPkg + "/workoutCoach"}}),
		shippedCoach(t, home, "claude-sonnet-5-5", []string{routine, memory}, []string{routine, memory}),
	}); err != nil {
		t.Fatalf("the shipped coach does not load: %v", err)
	}
}
