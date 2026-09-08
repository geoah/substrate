package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A stamped effect reads back from both stores. The table's jsonb spells the
// version `1`; the segment file's canonical JSON spells it `1E0`, and a
// rebuild decodes the file, so an effect whose version field refused that
// spelling would refuse every entry written since the stamp.
func TestAStampedEffectDecodesFromTheFileSpelling(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{`1`, `1E0`, `1.2E1`} {
		raw := `{"fold":[{"kind":"record","ref":"a.example/p/k","id":"x","version":` + spelling +
			`,"delta":{"created":true,"set":{"n":2E1}}}]}`
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		var payload map[string]any
		if err := dec.Decode(&payload); err != nil {
			t.Fatal(err)
		}
		ch := substrate.Change{Seq: 1, Op: substrate.OpPut, Kind: "a.example/p/k", RecordID: "x", Payload: payload}
		if foldRefuses(ch) {
			t.Fatalf("the fold refuses an effect whose version is spelled %s", spelling)
		}
		ops, err := foldOpsOf(ch)
		if err != nil || len(ops) != 1 || ops[0].Version != json.Number(spelling) {
			t.Fatalf("version %s decoded as %+v (%v)", spelling, ops, err)
		}
		want := int64(1)
		if spelling == `1.2E1` {
			want = 12
		}
		projectAffected(&ch)
		if len(ch.Affected) != 1 || ch.Affected[0].Version != want {
			t.Fatalf("version %s projected as %+v, want %d", spelling, ch.Affected, want)
		}
	}
}
