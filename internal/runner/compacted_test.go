package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// host.Compacted is raised for a host call the parent marks refused because
// the substrate cannot resume its cursor (the reply's `compacted`), and for
// nothing else: the same words without the mark are a HostError, so no
// wording, the engine's or a body's, decides it.
func TestHostPythonRaisesCompactedForAReplyMarkedSo(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; host.py cannot be probed here")
	}
	const probe = `
import io, json, sys, importlib.util
spec = importlib.util.spec_from_file_location("host", "host.py")
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)
out = {}
for name, reply in json.load(sys.stdin).items():
    host.send = lambda *a, **k: None
    host.PROTO_IN = io.StringIO(json.dumps(dict(reply, kind="reply", reqId=1)) + "\n")
    h = host.Host({}, 1, None)
    try:
        h._call("list", {})
        out[name] = "answered"
    except host.Compacted:
        out[name] = "Compacted"
    except host.HostError:
        out[name] = "HostError"
print(json.dumps(out))
`
	words := fmt.Errorf("%w: cursor was minted against another history; list again", substrate.ErrStaleHistory).Error()
	cases := map[string]map[string]any{
		"marked":              {"ok": false, "error": words, "compacted": true},
		"marked, other words": {"ok": false, "error": "nothing like the engine's", "compacted": true},
		"the words, unmarked": {"ok": false, "error": words},
	}
	var got map[string]string
	runProbe(t, python, probe, cases, &got)
	want := map[string]string{"marked": "Compacted", "marked, other words": "Compacted", "the words, unmarked": "HostError"}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("a reply %s raised %s, want %s", name, got[name], w)
		}
	}
}

// A failed run carries the refusal of its cursor, and the exact cursor the
// refused list sent, when, and only when, a list of it was refused because
// the substrate cannot resume that cursor: the runner records both as it
// answers the call, so a body that caught the refusal and then failed
// carries them too, and no failure's words, a body's own host.Compacted
// among them, stand in for it.
func TestAFailedRunCarriesTheRefusalOfItsCursor(t *testing.T) {
	r := New()
	const list = `host.records.list("g.test/widgets/widget", after="stale")`
	for i, c := range []struct {
		name, body string
		refused    bool
	}{
		{"the host's refusal escapes", list, true},
		{"the refusal re-raised unchanged", "try:\n        " + list + "\n    except host.Compacted:\n        raise", true},
		{"the refusal wrapped", "try:\n        " + list + "\n    except host.Compacted as e:\n        raise RuntimeError(\"the sweep stopped\") from e", true},
		{"the refusal handled, another failure", "try:\n        " + list + "\n    except host.Compacted:\n        pass\n    raise ValueError(\"the outside service is gone\")", true},
		{"its own error in the refusal's words", `raise Exception("validation failed: cursor was minted against another history; list again")`, false},
		{"its own host.Compacted, no refusal", `raise host.Compacted("validation failed: the cursor cannot be resumed: list again")`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			backend := widgetBackend()
			// Words the engine never wrote: only the refusal's kind is read.
			backend.refuseAfter = "words no body could match"
			spec := Spec{
				Repository: "t1", Function: "compacted.g.test/" + string(rune('a'+i)),
				Runtime:   "python",
				Source:    "def main(input, host):\n    " + c.body + "\n",
				ReadTypes: []string{"g.test/widgets/widget"},
			}
			_, err := r.Invoke(context.Background(), spec, testInput(), backend)
			if err == nil {
				t.Fatal("the body succeeded")
			}
			if strings.Contains(err.Error(), "allowlist") {
				t.Fatalf("the body never reached the refused list: %v", err)
			}
			var refused *RefusedCursorError
			if errors.As(err, &refused) != c.refused {
				t.Fatalf("%q carries a refused cursor: %v, want %v", err, !c.refused, c.refused)
			}
			if !c.refused {
				return
			}
			if len(refused.Refused) != 1 {
				t.Fatalf("the run carries %d refusals, want the one", len(refused.Refused))
			}
			got := refused.Refused[0]
			if !errors.Is(got.Refusal, substrate.ErrStaleHistory) || !strings.Contains(got.Refusal.Error(), "words no body could match") {
				t.Fatalf("the refusal carried is %v, want the backend's", got.Refusal)
			}
			if got.Cursor != "stale" {
				t.Fatalf("the refused list sent %q, want the cursor it was refused for", got.Cursor)
			}
		})
	}
}

// refusedRun runs body against a backend that refuses every list sent with a
// cursor, and answers what the run carries.
func refusedRun(t *testing.T, name, body string, timeoutMs int) (*RefusedCursorError, error) {
	t.Helper()
	backend := widgetBackend()
	backend.refuseAfter = "the cursor cannot be resumed"
	spec := Spec{
		Repository: "t1", Function: "compacted.g.test/" + name,
		Runtime: "python", TimeoutMs: timeoutMs,
		Source:    "def main(input, host):\n" + body,
		ReadTypes: []string{"g.test/widgets/widget"},
	}
	_, err := New().Invoke(context.Background(), spec, testInput(), backend)
	if err == nil {
		t.Fatal("the body succeeded")
	}
	var refused *RefusedCursorError
	if !errors.As(err, &refused) {
		t.Fatalf("%q carries no refused cursor", err)
	}
	return refused, err
}

// Every list the substrate refused travels with the run, in order: a body
// that lists from a cursor it keeps in a record, catches that refusal, and
// then lists from its continuation's carries both, so the engine sees the
// one its continuation holds.
func TestAFailedRunCarriesEveryRefusedCursor(t *testing.T) {
	refused, _ := refusedRun(t, "every", `    try:
        host.records.list("g.test/widgets/widget", after="from-a-record")
    except host.Compacted:
        pass
    host.records.list("g.test/widgets/widget", after="from-the-continuation")
`, 0)
	var cursors []string
	for _, r := range refused.Refused {
		cursors = append(cursors, r.Cursor)
	}
	if strings.Join(cursors, ",") != "from-a-record,from-the-continuation" {
		t.Fatalf("the run carries the refused cursors %v, want both, in order", cursors)
	}
}

// The refusals travel with every way a run fails: a body that catches the
// refusal and then outlives its timeout fails as a timeout, temporary as a
// timeout is, carrying the refusal.
func TestARunThatHangsAfterARefusalCarriesIt(t *testing.T) {
	refused, err := refusedRun(t, "hangs", `    try:
        host.records.list("g.test/widgets/widget", after="stale")
    except host.Compacted:
        pass
    import time
    time.sleep(30)
`, 1500)
	if !Temporary(err) || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("the hung run failed as %q, want the timeout, temporary", err)
	}
	if len(refused.Refused) != 1 || refused.Refused[0].Cursor != "stale" {
		t.Fatalf("the hung run carries %+v, want the refused cursor", refused.Refused)
	}
}

// A run that caught a refusal and answers with the refused cursor as its
// continuation fails, carrying it: saved, the next run would send it again.
func TestARunThatAnswersWithARefusedCursorFails(t *testing.T) {
	refused, _ := refusedRun(t, "echo", `    try:
        host.records.list("g.test/widgets/widget", after="stale")
    except host.Compacted:
        pass
    return {"effects": [], "more": {"cursor": {"after": "stale"}}}
`, 0)
	if len(refused.Refused) != 1 || refused.Refused[0].Cursor != "stale" {
		t.Fatalf("the run carries %+v, want the refused cursor", refused.Refused)
	}
}

// A run that caught a refusal and answered carries it: what fails after the
// reply (its effects refused, its page's commit) is decided with it.
func TestARunThatCaughtARefusalAndAnsweredCarriesIt(t *testing.T) {
	backend := widgetBackend()
	backend.refuseAfter = "the cursor cannot be resumed"
	res, err := New().Invoke(context.Background(), Spec{
		Repository: "t1", Function: "compacted.g.test/answers", Runtime: "python",
		Source: `def main(input, host):
    try:
        host.records.list("g.test/widgets/widget", after="stale")
    except host.Compacted:
        pass
    return {"effects": []}
`,
		ReadTypes: []string{"g.test/widgets/widget"},
	}, testInput(), backend)
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Refused) != 1 || res.Refused[0].Cursor != "stale" {
		t.Fatalf("the answered run carries %+v, want the refusal it caught", res.Refused)
	}
}

// A nested call's refusals are the calling run's, at every depth: one the
// callee failed carrying, and one it caught and answered with, both join
// the caller's, so a drain that hands its cursor to a helper meets the
// helper's refusal as its own.
func TestANestedCallsRefusalsJoinTheCallersRun(t *testing.T) {
	stale := RefusedCursor{Refusal: fmt.Errorf("%w: the cursor cannot be resumed", substrate.ErrStaleHistory), Cursor: "handed-down"}
	spec := Spec{
		Repository: "t1", Function: "compacted.g.test/caller", Runtime: "python",
		CallTargets: []string{"callee.g.test"},
	}
	t.Run("the callee failed", func(t *testing.T) {
		backend := widgetBackend()
		backend.call = func(string, any) (any, error) {
			return nil, &RefusedCursorError{Refused: []RefusedCursor{stale}, Err: errors.New("runner: the helper's list was refused")}
		}
		spec := spec
		spec.Source = "def main(input, host):\n    host.call(\"callee.g.test\", {})\n"
		_, err := New().Invoke(context.Background(), spec, testInput(), backend)
		var refused *RefusedCursorError
		if !errors.As(err, &refused) || len(refused.Refused) != 1 || refused.Refused[0].Cursor != "handed-down" {
			t.Fatalf("the caller failed as %v, want it carrying the callee's refused cursor", err)
		}
	})
	t.Run("the callee answered", func(t *testing.T) {
		backend := widgetBackend()
		backend.call = func(string, any) (any, error) { return map[string]any{}, nil }
		backend.callRefused = []RefusedCursor{stale}
		spec := spec
		spec.Source = "def main(input, host):\n    host.call(\"callee.g.test\", {})\n    return {\"effects\": []}\n"
		res, err := New().Invoke(context.Background(), spec, testInput(), backend)
		if err != nil {
			t.Fatalf("the caller failed: %v", err)
		}
		if len(res.Refused) != 1 || res.Refused[0].Cursor != "handed-down" {
			t.Fatalf("the caller carries %+v, want the callee's refused cursor", res.Refused)
		}
	})
}

// The host's own walk (observed.reconcile's read of what an origin holds)
// starts again from the first page once when its cursor is refused, with
// what it read dropped; refused again, it raises.
func TestObservedReadsAgainFromTheFirstPageWhenACursorIsCompacted(t *testing.T) {
	var out struct {
		Once     []string `json:"once"`
		OnceAsks []string `json:"onceAsks"`
		Twice    string   `json:"twice"`
	}
	observedProbe(t, `
import json, importlib.util
spec = importlib.util.spec_from_file_location("host", "host.py")
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)
pages = [{"records": [{"id": "a"}], "cursor": "1"}, {"records": [{"id": "b"}]}]
class Fake:
    def __init__(self, refusals):
        self.refusals, self.asks = refusals, []
    def list(self, **query):
        after = query.get("after")
        self.asks.append(after or "")
        if after and self.refusals > 0:
            self.refusals -= 1
            raise host.Compacted("the cursor cannot be resumed: list again")
        return pages[int(after or 0)]
once = Fake(1)
got = [r["id"] for r in host.Observed(once)._pages({"kinds": ["k"]})]
try:
    host.Observed(Fake(2))._pages({"kinds": ["k"]})
    twice = "answered"
except host.Compacted:
    twice = "Compacted"
print(json.dumps({"once": got, "onceAsks": once.asks, "twice": twice}))
`, &out)
	if strings.Join(out.Once, ",") != "a,b" || strings.Join(out.OnceAsks, ",") != ",1,,1" {
		t.Errorf("the walk read %v asking %q, want a and b once, from the first page again", out.Once, out.OnceAsks)
	}
	if out.Twice != "Compacted" {
		t.Errorf("a walk refused again = %s, want the refusal raised", out.Twice)
	}
}

// runProbe runs probe against host.py with in as its stdin, as JSON, and
// decodes what it prints into out. -B: no bytecode beside a tracked file.
func runProbe(t *testing.T, python, probe string, in, out any) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-B", "-c", probe)
	cmd.Dir = "."
	cmd.Stdin = strings.NewReader(string(raw))
	answer, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run the probe against host.py: %v\n%s", err, answer)
	}
	if err := json.Unmarshal(answer, out); err != nil {
		t.Fatalf("decode the probe's answer %q: %v", answer, err)
	}
}
