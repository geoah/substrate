package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
)

// recoveryKeyLine is the history line registration wrote before decision
// 0264, as the kept v0.123.1 directory holds it, the wrap shortened.
const recoveryKeyLine = `{"created":true,"fold":[{"delta":{"created":true,"kindVersion":5.4E1,"set":{"algorithm":"age-x25519-v1","publicKey":"age1k8ddys8rfmyge6g525zffjvmzjgp9cth7aked27g5p0fanvme3jsfz2eky","sealedKey":"YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+"},"title":"age-x25519-v1"},"id":"self","kind":"record","ref":"substrate.reamde.dev/core/recoverykey","version":1.0},{"actor":"substrate","id":"self","kind":"manager","property":"sealedKey","ref":"substrate.reamde.dev/core/recoverykey","tier":"machine"}],"properties":["algorithm","publicKey","sealedKey"]}`

// Step 2 takes the wrap out of a recoverykey line and nothing else: the
// rest of what was set, the manager items and the names written stay, and
// so does every number's spelling. A line of another kind, and a line
// already converted, come back unchanged.
func TestConversionStepTwoDropsTheRecoveryWrap(t *testing.T) {
	t.Parallel()
	e := changelogfile.Entry{Seq: 480, Txn: 481, Kind: kindRecoveryKey, RecordID: recoveryKeyID, Op: "put", Payload: json.RawMessage(recoveryKeyLine)}
	if err := convertDropRecoveryWrap(&e); err != nil {
		t.Fatal(err)
	}
	got := string(e.Payload)
	if strings.Contains(got, `"sealedKey":"`) || strings.Contains(got, "YWdlLWVu") {
		t.Fatalf("the wrap is still in the line: %s", got)
	}
	for _, kept := range []string{`"algorithm":"age-x25519-v1"`, `"publicKey":"age1`, `"property":"sealedKey"`, `"sealedKey"]`, `5.4E1`, `"title":"age-x25519-v1"`} {
		if !strings.Contains(got, kept) {
			t.Errorf("the converted line lost %s: %s", kept, got)
		}
	}
	once := got
	if err := convertDropRecoveryWrap(&e); err != nil {
		t.Fatal(err)
	}
	if string(e.Payload) != once {
		t.Fatalf("a converted line converted again:\n%s\n%s", once, e.Payload)
	}

	other := changelogfile.Entry{
		Seq: 9, Txn: 9, Kind: "substrate.reamde.dev/llm/provider", RecordID: "openai", Op: "put",
		Payload: json.RawMessage(strings.ReplaceAll(recoveryKeyLine, "core/recoverykey", "llm/provider")),
	}
	before := string(other.Payload)
	if err := convertDropRecoveryWrap(&other); err != nil {
		t.Fatal(err)
	}
	if string(other.Payload) != before {
		t.Fatal("a line of another kind was rewritten")
	}
}
