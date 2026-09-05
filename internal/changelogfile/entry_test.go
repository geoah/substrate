package changelogfile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func sample() Entry {
	return Entry{
		Seq: 4190, TS: time.Date(2026, 8, 4, 10, 0, 0, 183742000, time.UTC),
		Actor: "api", Principal: "k7abc", Op: "put",
		RecordID: "kq3v9x2m41pf", Kind: "samples.substrate.reamde.dev/tasks/task",
		CausedBy: 4188, CausedByOK: true,
		Payload: json.RawMessage(`{"properties": ["name", "dueAt"], "created": true, "n": 1.50}`),
	}
}

func TestEncodeIsCanonicalAndRoundTrips(t *testing.T) {
	line, sum, err := Encode(sample())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"actor":"api","causedBy":4188,"kind":"samples.substrate.reamde.dev/tasks/task","op":"put","payload":{"created":true,"n":1.5E0,"properties":["name","dueAt"]},"principal":"k7abc","recordId":"kq3v9x2m41pf","seq":4190,"sum":"sha256:`
	if !strings.HasPrefix(string(line), want) {
		t.Fatalf("line = %s\nwant prefix %s", line, want)
	}
	if strings.Contains(string(line), "\n") {
		t.Fatal("a line must not contain a newline")
	}
	got, gotSum, err := Decode(line)
	if err != nil {
		t.Fatal(err)
	}
	if gotSum != sum {
		t.Fatal("decode returned a different sum than encode")
	}
	e := sample()
	if got.Seq != e.Seq || !got.TS.Equal(e.TS) || got.Actor != e.Actor || got.Principal != e.Principal ||
		got.Op != e.Op || got.RecordID != e.RecordID || got.Kind != e.Kind ||
		got.CausedBy != e.CausedBy || !got.CausedByOK {
		t.Fatalf("round trip lost a field: %+v", got)
	}
	if string(got.Payload) != `{"created":true,"n":1.5E0,"properties":["name","dueAt"]}` {
		t.Fatalf("payload = %s", got.Payload)
	}
}

func TestPayloadSpellingDoesNotMoveTheSum(t *testing.T) {
	a := sample()
	b := sample()
	b.Payload = json.RawMessage(`{"n":15e-1,"created":true,"properties":["name","dueAt"]}`)
	_, sa, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	_, sb, err := Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	if sa != sb {
		t.Fatal("two spellings of one payload value must checksum alike")
	}
}

func TestEveryFieldMovesTheSum(t *testing.T) {
	base := sample()
	_, s0, err := Encode(base)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*Entry){
		"seq":       func(e *Entry) { e.Seq++ },
		"ts":        func(e *Entry) { e.TS = e.TS.Add(time.Microsecond) },
		"actor":     func(e *Entry) { e.Actor = "system" },
		"principal": func(e *Entry) { e.Principal = "" },
		"op":        func(e *Entry) { e.Op = "patch" },
		"recordId":  func(e *Entry) { e.RecordID = "other" },
		"kind":      func(e *Entry) { e.Kind = "x.example/p/k" },
		"causedBy":  func(e *Entry) { e.CausedBy++ },
		"causedNil": func(e *Entry) { e.CausedByOK = false },
		"payload":   func(e *Entry) { e.Payload = json.RawMessage(`{"created":false}`) },
	}
	for name, mutate := range mutations {
		e := sample()
		mutate(&e)
		_, s, err := Encode(e)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s == s0 {
			t.Fatalf("changing %s did not move the sum", name)
		}
	}
}

func TestDecodeRefusesDamage(t *testing.T) {
	line, _, err := Encode(sample())
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(line), `"actor":"api"`, `"actor":"apj"`, 1)
	if _, _, err := Decode([]byte(edited)); !errors.Is(err, ErrBadSum) {
		t.Fatalf("edited line: err = %v, want ErrBadSum", err)
	}
	if _, _, err := Decode(line[:len(line)-3]); err == nil {
		t.Fatal("a truncated line must not decode")
	}
	if _, _, err := Decode([]byte(strings.Replace(string(line), `"sum":`, `"sig":`, 1))); err == nil {
		t.Fatal("an unknown key must be refused")
	}
	if _, _, err := Decode([]byte(string(line) + `{}`)); err == nil {
		t.Fatal("trailing data must be refused")
	}
}

func TestEncodeRefusesAnIncompleteEntry(t *testing.T) {
	e := sample()
	e.TS = time.Time{}
	if _, _, err := Encode(e); err == nil {
		t.Fatal("an entry without a timestamp must not encode")
	}
	e = sample()
	e.Payload = json.RawMessage(`{"a":1} {"b":2}`)
	if _, _, err := Encode(e); err == nil {
		t.Fatal("a payload with trailing data must not encode")
	}
}

func TestCanonicalNumberValueExact(t *testing.T) {
	cases := map[string]string{
		"0": "0", "0.00": "0", "-0": "0", "0e9": "0",
		"1": "1E0", "1.5": "1.5E0", "1.50": "1.5E0", "15e-1": "1.5E0", "0.15E1": "1.5E0",
		"-42": "-4.2E1", "1000": "1E3", "0.001": "1E-3",
		"123456789012345678901234567890": "1.2345678901234567890123456789E29",
	}
	for lex, want := range cases {
		got, err := canonicalNumber(lex)
		if err != nil {
			t.Fatalf("%s: %v", lex, err)
		}
		if got != want {
			t.Fatalf("%s: got %s want %s", lex, got, want)
		}
	}
	for _, bad := range []string{"", "-", "1e", "abc", "1.2.3"} {
		if _, err := canonicalNumber(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}

func TestCanonicalJSONSortsKeysAndNormalizes(t *testing.T) {
	got, err := CanonicalJSON([]byte(`{"b": [1.0, 2.50, {"z": null, "a": "xé"}], "a": true}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":true,"b":[1E0,2.5E0,{"a":"xé","z":null}]}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if _, err := CanonicalJSON([]byte(`{} {}`)); err == nil {
		t.Fatal("trailing data must be refused")
	}
}
