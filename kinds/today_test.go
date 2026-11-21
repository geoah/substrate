package kinds_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// THE TODAY PACKAGE'S TWO BODIES HOLD RULES ABOUT TIME. `fade` abandons a
// suggested task two weeks after it was written, and `prepare` asks for the
// morning once the owner's clock passes their hour, in their zone, once a
// day. A record's `createdAt` is the engine's and a schedule's fire is the
// clock's, so no install test can reach either rule. This runs the shipped
// sources, as the tree holds them, over a host that answers from a dictionary
// and a fire time the test names.
func TestTodayBodiesHoldTheirRules(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	dir := t.TempDir()
	want := map[string]string{
		"substrate.reamde.dev/today/fade":    "fade_body.py",
		"substrate.reamde.dev/today/prepare": "prepare_body.py",
	}
	for _, d := range readTreeDocuments(t) {
		name, ok := want[d.ID]
		if !ok || d.Kind != vocabulary.DocFunction {
			continue
		}
		source, _ := d.Data["source"].(string)
		if source == "" {
			t.Fatalf("%s carries no source", d.ID)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		delete(want, d.ID)
	}
	for id := range want {
		t.Fatalf("the tree declares no function %s", id)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.py"), []byte(todayBodiesCheck), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "check.py")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the today bodies: %v\n%s", err, out)
	}
}

const todayBodiesCheck = `
import datetime
import importlib.util

TASK = "substrate.reamde.dev/tasks/task"
PREFERENCE = "substrate.reamde.dev/today/preference"
ADVICE = "substrate.reamde.dev/today/advice"
PREPARATION = "substrate.reamde.dev/today/preparation"
UTC = datetime.timezone.utc


def load(name):
    spec = importlib.util.spec_from_file_location(name, name + "_body.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class Refused(Exception):
    pass


class Observed:
    def parse_instant(self, text):
        if not text:
            return None
        return datetime.datetime.fromisoformat(text.replace("Z", "+00:00"))


class Effects:
    def __init__(self):
        self.staged = []

    def put(self, kind, id, properties=None, if_absent=False, **rest):
        self.staged.append(("put", kind, id, properties or {}, {"if_absent": if_absent, **rest}))

    def patch(self, kind, id, properties=None, **rest):
        self.staged.append(("patch", kind, id, properties or {}, rest))

    def delete(self, kind, id, **rest):
        self.staged.append(("delete", kind, id, {}, rest))


class Records:
    def __init__(self, store):
        self.store = store

    def get(self, kind, id):
        if kind not in self.store:
            raise Refused("unknown type " + kind)
        return self.store[kind].get(id)

    def list(self, kinds, where=None, first=None, after=None, order=None):
        out = []
        for kind in kinds:
            if kind not in self.store:
                raise Refused("unknown type " + kind)
            for record in self.store[kind].values():
                if all(record["properties"].get(k) == v.get("eq") for k, v in (where or {}).items()):
                    out.append(record)
        return {"records": out}


class Host:
    Compacted = type("Compacted", (Exception,), {})

    def __init__(self, store):
        self.records, self.effects, self.observed, self.lines = Records(store), Effects(), Observed(), []

    def version(self, record):
        return int((record or {}).get("version") or 0)

    def log(self, line):
        self.lines.append(line)


def record(kind, id, created, **properties):
    return {"kind": kind, "id": id, "version": 3, "createdAt": created, "properties": properties}


def fire(at):
    return {"mode": "schedule", "envelope": {"fire": {"id": "f", "at": at}, "repository": {"authority": "ada.example.com"}}}


def delivery(task):
    return {"mode": "record", "envelope": {"change": {"op": "update", "kind": TASK, "id": task["id"]}, "record": task}}


fade, prepare = load("fade"), load("prepare")

# ---- fade: two weeks, by when the suggestion was written -----------------------
store = {
    TASK: {
        "old": record(TASK, "old", "2026-11-01T09:00:00Z", status="proposed", name="old"),
        "edge": record(TASK, "edge", "2026-11-07T03:10:00Z", status="proposed", name="exactly two weeks"),
        "new": record(TASK, "new", "2026-11-08T09:00:00Z", status="proposed", name="thirteen days"),
        "open": record(TASK, "open", "2026-10-01T09:00:00Z", status="open", name="an old task that is not a suggestion"),
    },
    PREPARATION: {
        "d20261101": record(PREPARATION, "d20261101", "2026-11-01T06:30:00Z", reason="morning"),
        "d20261120": record(PREPARATION, "d20261120", "2026-11-20T06:30:00Z", reason="morning"),
    },
}
host = Host(store)
out = fade.main(fire("2026-11-21T03:10:00Z"), host)
assert out == {"output": {"abandoned": 2, "deleted": 1}}, out
patches = [e for e in host.effects.staged if e[0] == "patch"]
assert [e[2] for e in patches] == ["old", "edge"], patches
for e in patches:
    # Under the version it read, and losing that race is a skip: a task the
    # owner answered meanwhile is never abandoned.
    assert e[1] == TASK and e[3] == {"status": "abandoned"} and e[4] == {"if_version": 3, "on_conflict": "yield"}, e
assert [e[1:3] for e in host.effects.staged if e[0] == "delete"] == [(PREPARATION, "d20261101")], host.effects.staged

# A repository without the tasks package: nothing read, nothing written, no failure.
host = Host({PREPARATION: {}})
assert fade.main(fire("2026-11-21T03:10:00Z"), host) == {"output": {"abandoned": 0, "deleted": 0}}
assert host.effects.staged == []

# ---- prepare: nothing until an app wrote the owner's preference ----------------
host = Host({PREFERENCE: {}, PREPARATION: {}, TASK: {}, ADVICE: {}})
assert prepare.main(fire("2026-11-21T09:00:00Z"), host) == {"output": {"skipped": "no day preference yet"}}
assert host.effects.staged == []


def day(preference, tasks=None, advice=None, asked=None):
    return {
        PREFERENCE: {"default": record(PREFERENCE, "default", "2026-11-01T00:00:00Z", **preference)},
        PREPARATION: dict(asked or {}), TASK: dict(tasks or {}), ADVICE: dict(advice or {}),
    }


# The owner's switch turns all of it off.
host = Host(day({"timezone": "Europe/London", "assistantPrepares": False}))
assert prepare.main(fire("2026-11-21T09:00:00Z"), host) == {"output": {"skipped": "the owner turned preparing off"}}

# The morning is at the owner's hour on the owner's clock.
host = Host(day({"timezone": "Europe/London", "prepareAt": "06:30"}))
assert prepare.main(fire("2026-11-21T06:00:00Z"), host) == {"output": {"skipped": "before the morning"}}
host = Host(day({"timezone": "Europe/London", "prepareAt": "06:30"}))
assert prepare.main(fire("2026-11-21T06:30:00Z"), host) == {"output": {"asked": ["d20261121"]}}
kind, id, props, how = host.effects.staged[0][1:]
assert (kind, id, how) == (PREPARATION, "d20261121", {"if_absent": True}), host.effects.staged
assert props["day"] == "2026-11-21" and props["reason"] == "morning"
assert "2026-11-21T00:00:00Z to 2026-11-22T00:00:00Z" in props["ask"], props["ask"]
# 06:00 UTC is 15:00 in Tokyo, and the day there began at 15:00 UTC the day before.
host = Host(day({"timezone": "Asia/Tokyo", "prepareAt": "06:30"}))
assert prepare.main(fire("2026-11-21T06:00:00Z"), host) == {"output": {"asked": ["d20261121"]}}
assert "2026-11-20T15:00:00Z to 2026-11-21T15:00:00Z" in host.effects.staged[0][3]["ask"]
# 06:00 UTC is 22:00 the evening before in Los Angeles: that day, already past its morning.
host = Host(day({"timezone": "America/Los_Angeles", "prepareAt": "06:30"}))
assert prepare.main(fire("2026-11-21T06:00:00Z"), host) == {"output": {"asked": ["d20261120"]}}
# A zone nobody knows reads as UTC and still prepares.
host = Host(day({"timezone": "Nowhere/Land"}))
assert prepare.main(fire("2026-11-21T07:00:00Z"), host) == {"output": {"asked": ["d20261121"]}}

# Once a day: the day's record is the mark.
asked = {"d20261121": record(PREPARATION, "d20261121", "2026-11-21T06:30:00Z", reason="morning")}
host = Host(day({"timezone": "Europe/London"}, asked=asked))
assert prepare.main(fire("2026-11-21T12:00:00Z"), host) == {"output": {"asked": []}}
assert host.effects.staged == []

# ---- prepare: advice for the suggestions that have none ------------------------
tasks = {
    "b": record(TASK, "b", "2026-11-20T10:00:00Z", status="proposed", name="Send the list"),
    "a": record(TASK, "a", "2026-11-20T09:00:00Z", status="proposed", name="Reply to Stefan"),
    "c": record(TASK, "c", "2026-11-20T08:00:00Z", status="proposed", name="Already advised"),
    "d": record(TASK, "d", "2026-11-20T08:00:00Z", status="open", name="Not a suggestion"),
}
advice = {"c": record(ADVICE, "c", "2026-11-20T08:01:00Z", answer="task")}
host = Host(day({"timezone": "Europe/London"}, tasks, advice, asked))
out = prepare.main(fire("2026-11-21T12:00:00Z"), host)
assert len(out["output"]["asked"]) == 1 and out["output"]["asked"][0].startswith("a"), out
props = host.effects.staged[0][3]
assert props["reason"] == "advice" and props["subjects"] == [TASK + "/a", TASK + "/b"], props
assert "2 suggested tasks wait" in props["ask"] and props["ask"].index("Reply to Stefan") < props["ask"].index("Send the list")
assert "Already advised" not in props["ask"] and "Not a suggestion" not in props["ask"]
first = out["output"]["asked"][0]
# The same tasks waiting is the same record, so nothing runs twice; a day later it is asked once more.
host = Host(day({"timezone": "Europe/London"}, tasks, advice, asked))
assert prepare.main(fire("2026-11-21T18:00:00Z"), host)["output"]["asked"] == [first]
host = Host(day({"timezone": "Europe/London"}, tasks, advice))
again = prepare.main(fire("2026-11-22T12:00:00Z"), host)["output"]["asked"]
assert again[0] == "d20261122" and again[1] != first, again
# One ask holds at most MAX_ADVICE tasks, the oldest first.
many = {"t%02d" % i: record(TASK, "t%02d" % i, "2026-11-20T%02d:00:00Z" % (i % 24), status="proposed", name="n%d" % i) for i in range(30)}
host = Host(day({"timezone": "Europe/London"}, many, asked=asked))
prepare.main(fire("2026-11-21T12:00:00Z"), host)
assert len(host.effects.staged[0][3]["subjects"]) == prepare.MAX_ADVICE

# A task arriving as a suggestion asks for the same thing.
host = Host(day({"timezone": "Europe/London"}, tasks, advice, asked))
assert prepare.main(delivery(tasks["a"]), host)["output"]["asked"] == [first]
# One that already has advice asks for nothing.
host = Host(day({"timezone": "Europe/London"}, {"c": tasks["c"]}, advice, asked))
assert prepare.main(delivery(tasks["c"]), host) == {"output": {"asked": []}}

# ---- prepare: an urgent mark nobody has checked --------------------------------
def urgent(**properties):
    base = {"status": "open", "name": "Confirm the drain list", "priority": "urgent"}
    base.update(properties)
    return record(TASK, "u", "2026-11-21T11:00:00Z", **base)


for properties, asks in [
    ({"prioritySetBy": "api", "priorityReason": "A drain waits."}, True),
    ({"prioritySetBy": "automation"}, True),
    ({"prioritySetBy": "assistant"}, False),
    ({"prioritySetBy": "owner"}, False),
    ({}, False),
    ({"prioritySetBy": "api", "urgentCheckedAt": "2026-11-21T11:01:00Z"}, False),
    ({"prioritySetBy": "automation", "urgentHeldAt": "2026-11-21T11:01:00Z"}, False),
]:
    task = urgent(**properties)
    host = Host(day({"timezone": "Europe/London"}, {"u": task}, asked=asked))
    got = prepare.main(delivery(task), host)["output"]["asked"]
    assert (len(got) == 1 and got[0].startswith("u")) == asks, (properties, got)
    if asks:
        props = host.effects.staged[0][3]
        assert props["reason"] == "urgent" and props["subjects"] == [TASK + "/u"], props
        assert "urgentCheckedAt" in props["ask"] and "urgentHeldReason" in props["ask"] and "never take the mark off" in props["ask"]
task = urgent(prioritySetBy="api", priorityWas="high")
host = Host(day({"timezone": "Europe/London"}, {"u": task}, asked=asked))
assert "with no reason given" in prepare.urgent_ask(task)

# A repository without the tasks package: the morning is still asked for, and nothing fails.
host = Host({PREFERENCE: {"default": record(PREFERENCE, "default", "2026-11-01T00:00:00Z", timezone="Europe/London")}, PREPARATION: {}, ADVICE: {}})
assert prepare.main(fire("2026-11-21T09:00:00Z"), host) == {"output": {"asked": ["d20261121"]}}
print("ok")
`
