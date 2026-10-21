package engine

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/geoah/substrate/internal/engine/unitseed"
)

var updateConsoleUnits = flag.Bool("update-console-units", false, "rewrite the console's fixture of the shipped units")

// consoleShippedUnits is the shipped seed as GET /api/v1/units answers it for
// a fresh repository. The console's tests read units through it, so they run
// against the units a repository starts with; the console itself never
// imports it, because a repository's units are its own.
const consoleShippedUnits = "../../web/console/src/lib/unit-graph.shipped.json"

// TestConsoleFixtureMatchesTheSeed holds the console's fixture to the seed in
// internal/engine/unitseed. A seed change fails it until
//
//	go test ./internal/engine/ -run TestConsoleFixtureMatchesTheSeed -update-console-units
//
// rewrites the fixture.
func TestConsoleFixtureMatchesTheSeed(t *testing.T) {
	g, problems, err := unitseed.Graph()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("the shipped seed: %v", p)
	}
	tab := graphTable(g, 1)
	var b bytes.Buffer
	b.WriteString("{\n  \"revision\": 1,\n  \"units\": [\n")
	for i, u := range tab.Units {
		line, err := json.Marshal(u)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("    ")
		b.Write(line)
		if i < len(tab.Units)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ],\n  \"conversions\": [\n")
	for i, c := range tab.Conversions {
		line, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("    ")
		b.Write(line)
		if i < len(tab.Conversions)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]\n}\n")
	if *updateConsoleUnits {
		if err := os.WriteFile(consoleShippedUnits, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(consoleShippedUnits)
	if err != nil {
		t.Fatalf("read %s: %v (rewrite it with -update-console-units)", consoleShippedUnits, err)
	}
	if !bytes.Equal(got, b.Bytes()) {
		t.Errorf("%s differs from the shipped seed: rewrite it with -update-console-units, never by hand", consoleShippedUnits)
	}
}
