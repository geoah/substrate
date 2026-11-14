package engine

// Conversion step 4 on single lines: what a record effect's body below dialect 4
// becomes, what it leaves alone, and that a converted line converts to itself.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
)

func bodyLine(t *testing.T, payload string) changelogfile.Entry {
	t.Helper()
	canon, err := changelogfile.CanonicalJSON([]byte(payload))
	if err != nil {
		t.Fatalf("the fixture is not a payload: %v", err)
	}
	return changelogfile.Entry{Seq: 9, Txn: 9, Actor: "api", Op: "patch", Kind: "a.example.com/notes/note", RecordID: "n1", Payload: canon}
}

func convertBodyLine(t *testing.T, e changelogfile.Entry) string {
	t.Helper()
	if err := moveBodyIntoProperties(&e); err != nil {
		t.Fatalf("convert: %v", err)
	}
	canon, err := changelogfile.CanonicalJSON(e.Payload)
	if err != nil {
		t.Fatalf("the converted payload: %v", err)
	}
	again := e
	if err := moveBodyIntoProperties(&again); err != nil {
		t.Fatalf("convert the converted line: %v", err)
	}
	if string(again.Payload) != string(e.Payload) {
		t.Fatalf("a converted line converted again changed:\n%s\n%s", e.Payload, again.Payload)
	}
	return string(canon)
}

func TestTheBodyStepMovesTheFieldIntoTheProperties(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ in, want string }{
		"a value joins set": {
			`{"fold":[{"delta":{"body":"the falcon dives","set":{"name":"Falcon"},"title":"Falcon"},"id":"n1","kind":"record","ref":"a.example.com/notes/note","version":2E0}],"properties":["body","name"]}`,
			`{"fold":[{"delta":{"set":{"body":"the falcon dives","name":"Falcon"},"title":"Falcon"},"id":"n1","kind":"record","ref":"a.example.com/notes/note","version":2E0}],"properties":["body","name"]}`,
		},
		"a creation with no other property": {
			`{"created":true,"fold":[{"delta":{"body":"x","created":true,"kindVersion":3E0},"id":"n1","kind":"record","ref":"a.example.com/notes/note","version":1E0}],"properties":["body"]}`,
			`{"created":true,"fold":[{"delta":{"created":true,"kindVersion":3E0,"set":{"body":"x"}},"id":"n1","kind":"record","ref":"a.example.com/notes/note","version":1E0}],"properties":["body"]}`,
		},
		"a clear joins del, in order": {
			`{"fold":[{"delta":{"body":"","del":["url","alias"]},"id":"n1","kind":"record","ref":"a.example.com/notes/note","version":4E0}],"properties":["body","url","alias"]}`,
			`{"fold":[{"delta":{"del":["alias","body","url"]},"id":"n1","kind":"record","ref":"a.example.com/notes/note","version":4E0}],"properties":["body","url","alias"]}`,
		},
		"each record effect of a merge": {
			`{"fold":[{"delta":{"body":"kept"},"id":"w","kind":"record","ref":"a.example.com/notes/note","version":3E0},{"id":"l","kind":"tombstone","ref":"a.example.com/notes/note","version":2E0},{"delta":{"body":""},"id":"l","kind":"record","ref":"a.example.com/notes/note"}]}`,
			`{"fold":[{"delta":{"set":{"body":"kept"}},"id":"w","kind":"record","ref":"a.example.com/notes/note","version":3E0},{"id":"l","kind":"tombstone","ref":"a.example.com/notes/note","version":2E0},{"delta":{"del":["body"]},"id":"l","kind":"record","ref":"a.example.com/notes/note"}]}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := convertBodyLine(t, bodyLine(t, c.in)); got != c.want {
				t.Fatalf("converted to\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// A line with no record body is left byte for byte: another effect's `body`
// (a parked webhook's request) is not a record's, and the numbers keep the
// spelling the file wrote.
func TestTheBodyStepLeavesEveryOtherLine(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"a parked request's body": `{"fold":[{"failure":{"fireId":"f1","id":5.66E2,"payload":{"request":{"body":{"blob":"blob-sha256-ab","encoding":"text"}}}},"id":"hook","kind":"park","ref":"substrate.reamde.dev/core/trigger"}]}`,
		"a record with no body":   `{"fold":[{"delta":{"set":{"estimate":1.5E0,"name":"x"}},"id":"n1","kind":"record","ref":"a.example.com/notes/note","version":1E0}],"properties":["name","estimate"]}`,
		"no fold":                 `{"properties":["name"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := bodyLine(t, payload)
			before := string(e.Payload)
			if err := moveBodyIntoProperties(&e); err != nil {
				t.Fatalf("convert: %v", err)
			}
			if string(e.Payload) != before {
				t.Fatalf("an untouched line changed:\n%s\n%s", before, e.Payload)
			}
		})
	}
}

// A delta that carries body twice cannot be merged without losing one value,
// so the conversion stops on it, naming the line.
func TestTheBodyStepRefusesABodyCarriedTwice(t *testing.T) {
	t.Parallel()
	for _, delta := range []string{
		`{"body":"a","set":{"body":"b"}}`,
		`{"body":"a","del":["body"]}`,
	} {
		e := bodyLine(t, `{"fold":[{"delta":`+delta+`,"id":"n1","kind":"record","ref":"a.example.com/notes/note"}]}`)
		err := moveBodyIntoProperties(&e)
		if err == nil || !strings.Contains(err.Error(), "seq 9") {
			t.Fatalf("%s: %v, want a refusal naming the line", delta, err)
		}
	}
}

// A delta the step cannot read as every writer spelled one is refused, naming
// the line, before anything on it changes. A `null` set or del decodes
// without an error into a nil map or list, and a `null` body into the empty
// string, the cleared form; the old field was a pointer to a string, written
// only with a value (`""` for a clear) and omitted when nil, so no release
// wrote `null` there, and converting one would delete what an earlier line
// set. A number, an object and a boolean are no string.
func TestTheBodyStepRefusesADeltaItCannotRead(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ delta, says string }{
		"a null set":              {`{"body":"x","set":null}`, "delta.set is null"},
		"a null del":              {`{"body":"","del":null}`, "delta.del is null"},
		"a set that is a list":    {`{"body":"x","set":["name"]}`, "delta.set"},
		"a del that is an object": {`{"body":"","del":{"name":true}}`, "delta.del"},
		"a null body":             {`{"body":null,"set":{"name":"y"}}`, "delta.body is null"},
		"a null body alone":       {`{"body":null}`, "delta.body is null"},
		"a number body":           {`{"body":4E0}`, "delta.body"},
		"an object body":          {`{"body":{"text":"x"}}`, "delta.body"},
		"a boolean body":          {`{"body":true}`, "delta.body"},
	} {
		t.Run(name, func(t *testing.T) {
			e := bodyLine(t, `{"fold":[{"delta":`+c.delta+`,"id":"n1","kind":"record","ref":"a.example.com/notes/note"}]}`)
			before := string(e.Payload)
			err := moveBodyIntoProperties(&e)
			if err == nil || !strings.Contains(err.Error(), "seq 9") || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("%v, want a refusal naming seq 9 and saying %q", err, c.says)
			}
			if string(e.Payload) != before {
				t.Fatalf("a refused line changed:\n%s\n%s", before, e.Payload)
			}
		})
	}
}

// Every other `null` the step decodes is a place that holds no body: the
// line is left as it is, for the fold to judge, and nothing panics.
func TestTheBodyStepLeavesANullThatHoldsNoBody(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"a null fold":    `{"fold":null}`,
		"a null effect":  `{"fold":[null]}`,
		"a null delta":   `{"fold":[{"delta":null,"id":"n1","kind":"record","ref":"a.example.com/notes/note"}]}`,
		"a null payload": `null`,
	} {
		t.Run(name, func(t *testing.T) {
			e := bodyLine(t, payload)
			before := string(e.Payload)
			if err := moveBodyIntoProperties(&e); err != nil {
				t.Fatalf("convert: %v", err)
			}
			if string(e.Payload) != before {
				t.Fatalf("a line with no body changed:\n%s\n%s", before, e.Payload)
			}
		})
	}
}

// What the step writes is what the fold reads: a converted delta decodes
// under the strict effect decode, which refuses `body` as a field.
func TestAConvertedBodyFoldsAndAnUnconvertedOneIsRefused(t *testing.T) {
	t.Parallel()
	in := `[{"delta":{"body":"x","set":{"name":"y"}},"id":"n1","kind":"record","ref":"a.example.com/notes/note"}]`
	if _, err := foldOfJSON(t, in); err == nil || !strings.Contains(err.Error(), "body") {
		t.Fatalf("a delta below dialect 4 folded: %v", err)
	}
	e := bodyLine(t, `{"fold":`+in+`}`)
	if err := moveBodyIntoProperties(&e); err != nil {
		t.Fatal(err)
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatal(err)
	}
	ops, err := foldOfJSON(t, string(p[foldPayloadKey]))
	if err != nil {
		t.Fatalf("the converted delta does not fold: %v", err)
	}
	if got := ops[0].Delta.Set["body"]; got != "x" {
		t.Fatalf("the converted delta sets body to %v, want x", got)
	}
}
