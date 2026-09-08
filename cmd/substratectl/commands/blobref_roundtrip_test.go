package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A blob-ref reads as its manifest ({digest, name, mediaType, size, status})
// and is stored as the digest, and the server takes the digest out of the
// object on a write. The CLI's part is to hand the object back whole and
// typed: `get -o yaml | apply -f` on a record with an attachment must land the
// same document, with `size` still an integer rather than a string.
func TestBlobRefManifestSurvivesTheRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	digest := substrate.BlobDigestPrefix + strings.Repeat("a", 64)
	h.fake.seed(&substrate.Record{
		ID: "t9", Kind: "samples.substrate.reamde.dev/tasks/task",
		Properties: map[string]any{
			"title": "Send rack layout to Alex",
			"attachment": map[string]any{
				"digest": digest, "name": "layout.png", "mediaType": "image/png",
				"size": 2048, "status": "stored",
			},
		},
		Version:   3,
		CreatedAt: testNow.Add(-48 * time.Hour),
		UpdatedAt: testNow.Add(-2 * time.Hour),
	})

	first, _ := h.mustRun("get", "tasks", "t9")
	if !strings.Contains(first, "digest: "+digest+"\n") || !strings.Contains(first, "size: 2048\n") {
		t.Fatalf("get did not render the manifest with a typed size:\n%s", first)
	}

	h.stdin.WriteString(first)
	applied, _ := h.mustRun("apply", "-f", "-")
	if !strings.Contains(applied, "unchanged") {
		t.Fatalf("re-applying get output should be unchanged, got:\n%s", applied)
	}
	if got := h.lastRequest(); got != "PUT /api/v1/samples.substrate.reamde.dev/tasks/task/t9" {
		t.Fatalf("last request = %q, want the put", got)
	}
	var props map[string]any
	if err := json.Unmarshal(h.fake.lastBody["properties"], &props); err != nil {
		t.Fatalf("put body properties: %v", err)
	}
	sent, ok := props["attachment"].(map[string]any)
	if !ok {
		t.Fatalf("put body attachment = %#v, want the read shape carried whole", props["attachment"])
	}
	if sent["digest"] != digest {
		t.Fatalf("put body digest = %#v", sent["digest"])
	}
	if sent["size"] != float64(2048) {
		t.Fatalf("put body size = %#v (%T), want the integer 2048", sent["size"], sent["size"])
	}

	second, _ := h.mustRun("get", "tasks", "t9")
	if first != second {
		t.Fatalf("the document changed across get | apply | get:\n--- first\n%s\n--- second\n%s", first, second)
	}
}
