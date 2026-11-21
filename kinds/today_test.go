package kinds_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// THE PREPARER WRITES ONLY ITS OWN RECORDS AND CHANGE REQUESTS. It is a
// model that reads what other people wrote (a suggested task's words come
// from a mail or a message), run by a trigger with nobody watching, so what
// it may write is the whole of what a crafted message could make it do. It
// holds no write on a task, a list or a glob: its verdict on an urgent mark
// is stamped on the task by a function. It is also not the owner's
// assistant: its spend is its own daily cap, on the bundle's provider input.
func TestThePreparerWritesOnlyItsOwnRecords(t *testing.T) {
	const id = "substrate.reamde.dev/today/preparer"
	want := []string{
		"substrate.reamde.dev/core/recordpatchrequest",
		"substrate.reamde.dev/today/advice",
		"substrate.reamde.dev/today/dossier",
		"substrate.reamde.dev/today/urgentcheck",
	}
	for _, d := range readTreeDocuments(t) {
		if d.Kind != vocabulary.DocAgent || d.ID != id {
			continue
		}
		permissions, _ := d.Data["permissions"].(map[string]any)
		var writes []string
		for _, w := range asList(permissions["writes"]) {
			kind, _ := w.(string)
			writes = append(writes, kind)
		}
		slices.Sort(writes)
		if !slices.Equal(writes, want) {
			t.Errorf("%s writes %v, want exactly %v", id, writes, want)
		}
		if d.Data["hiddenFromChat"] != true {
			t.Errorf("%s is offered in chat; it is run by a trigger alone", id)
		}
		if d.Data["providerInput"] != "model" {
			t.Errorf("%s takes its provider from %v, want the bundle's model input", id, d.Data["providerInput"])
		}
		budgets, _ := d.Data["budgets"].(map[string]any)
		if cap, _ := budgets["spendCentsPerDay"].(int); cap != 100 {
			t.Errorf("%s's daily cap is %v cents, want 100", id, budgets["spendCentsPerDay"])
		}
		return
	}
	t.Fatalf("the tree declares no agent %s", id)
}

// THE TODAY PACKAGE'S BODIES HOLD RULES ABOUT TIME. `fade` abandons a
// suggested task two weeks after it was written, `prepare` asks for the
// morning once the owner's clock passes their hour, in their zone, once a
// day, and for advice at most every two hours, and `stampurgent` stamps a
// task only while its urgent mark is unchecked. A record's `createdAt` is
// the engine's and a schedule's fire is the clock's, so no install test can
// reach these rules. This runs the shipped sources, as the tree holds them,
// over a host that answers from a dictionary and a fire time the test names.
func TestTodayBodiesHoldTheirRules(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	dir := t.TempDir()
	want := map[string]string{
		"substrate.reamde.dev/today/fade":        "fade_body.py",
		"substrate.reamde.dev/today/prepare":     "prepare_body.py",
		"substrate.reamde.dev/today/stampurgent": "stampurgent_body.py",
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
URGENTCHECK = "substrate.reamde.dev/today/urgentcheck"
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

    def instant(self, at):
        return at.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")

    def ref_id(self, value, kind):
        path = value.get("ref") if isinstance(value, dict) else (value or "")
        prefix = kind + "/"
        return path[len(prefix):] if path.startswith(prefix) else ""


class Effects:
    def __init__(self):
        self.staged = []

    def put(self, kind, id, properties=None, if_absent=False, **rest):
        self.staged.append(("put", kind, id, properties or {}, {"if_absent": if_absent, **rest}))

    def patch(self, kind, id, properties=None, **rest):
        self.staged.append(("patch", kind, id, properties or {}, rest))

    def delete(self, kind, id, **rest):
        self.staged.append(("delete", kind, id, {}, rest))


def holds(record, where):
    """The filter a body's read carries: eq on any property, in on a
    reference, which is compared by the path it points at."""
    for name, condition in (where or {}).items():
        value = record["properties"].get(name)
        if isinstance(value, dict):
            value = value.get("ref")
        if "eq" in condition and value != condition["eq"]:
            return False
        if "in" in condition and value not in condition["in"]:
            return False
    return True


class Records:
    def __init__(self, store):
        self.store, self.reads = store, []

    def get(self, kind, id):
        if kind not in self.store:
            raise Refused("unknown type " + kind)
        return self.store[kind].get(id)

    def list(self, kinds, where=None, first=None, after=None, order=None):
        self.reads.append((tuple(kinds), where, first, order))
        out = []
        for kind in kinds:
            if kind not in self.store:
                raise Refused("unknown type " + kind)
            out += [r for r in self.store[kind].values() if holds(r, where)]
        if order and order.startswith("createdAt:desc"):
            out.sort(key=lambda r: (r.get("createdAt") or "", r.get("id") or ""), reverse=True)
        return {"records": out[:first] if first else out}


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


def delivery(changed):
    return {"mode": "record", "envelope": {"change": {"op": "update", "kind": changed["kind"], "id": changed["id"]}, "record": changed}}


fade, prepare, stampurgent = load("fade"), load("prepare"), load("stampurgent")
SET_UP = {"default": record(PREFERENCE, "default", "2026-11-01T00:00:00Z", timezone="Europe/London")}

# ---- fade: two weeks, by when the suggestion was written -----------------------
store = {
    PREFERENCE: SET_UP,
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

# Adding the package alone abandons nothing: the same old suggestions, and no
# preference record because no app was ever opened here.
host = Host({PREFERENCE: {}, TASK: store[TASK], PREPARATION: store[PREPARATION]})
assert fade.main(fire("2026-11-21T03:10:00Z"), host) == {"output": {"skipped": "no day preference yet"}}
assert host.effects.staged == []

# A repository without the tasks package: nothing read, nothing written, no failure.
host = Host({PREFERENCE: SET_UP, PREPARATION: {}})
assert fade.main(fire("2026-11-21T03:10:00Z"), host) == {"output": {"abandoned": 0, "deleted": 0}}
assert host.effects.staged == []

# ---- prepare: nothing until an app wrote the owner's preference ----------------
host = Host({PREFERENCE: {}, PREPARATION: {}, TASK: {}, ADVICE: {}, URGENTCHECK: {}})
assert prepare.main(fire("2026-11-21T09:00:00Z"), host) == {"output": {"skipped": "no day preference yet"}}
assert host.effects.staged == []


def day(preference, tasks=None, advice=None, asked=None, checks=None):
    return {
        PREFERENCE: {"default": record(PREFERENCE, "default", "2026-11-01T00:00:00Z", **preference)},
        PREPARATION: dict(asked or {}), TASK: dict(tasks or {}), ADVICE: dict(advice or {}),
        URGENTCHECK: dict(checks or {}),
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
advice = {"c": record(ADVICE, "c", "2026-11-20T08:01:00Z", answer="task", task={"ref": TASK + "/c"})}
host = Host(day({"timezone": "Europe/London"}, tasks, advice, asked))
out = prepare.main(fire("2026-11-21T12:00:00Z"), host)
assert len(out["output"]["asked"]) == 1 and out["output"]["asked"][0].startswith("a"), out
props = host.effects.staged[0][3]
assert props["reason"] == "advice" and props["subjects"] == [TASK + "/a", TASK + "/b"], props
# The newest first: the oldest are the ones about to fade.
assert "2 suggested tasks wait" in props["ask"] and props["ask"].index("Send the list") < props["ask"].index("Reply to Stefan")
assert "Already advised" not in props["ask"] and "Not a suggestion" not in props["ask"]
# The advice is read by the waiting tasks' own paths, never as the whole
# kind under a limit: advice is kept for good, and a limited read of all of
# it would one day miss some and ask for it again.
reads = [r for r in host.records.reads if r[0] == (ADVICE,)]
assert reads and all(set(r[1]) == {"task"} and "in" in r[1]["task"] for r in reads), reads
assert sorted(reads[0][1]["task"]["in"]) == [TASK + "/a", TASK + "/b", TASK + "/c"], reads
first = out["output"]["asked"][0]

# Advice is asked for at most every two hours, however many suggestions
# arrive in between; then the same tasks waiting are the same record, so
# nothing runs twice; and a day later they are asked for once more.
was = dict(asked)
was[first] = record(PREPARATION, first, "2026-11-21T12:00:00Z", reason="advice")
more = dict(tasks)
more["e"] = record(TASK, "e", "2026-11-21T12:30:00Z", status="proposed", name="Heard a moment ago")
host = Host(day({"timezone": "Europe/London"}, more, advice, was))
assert prepare.main(fire("2026-11-21T13:30:00Z"), host) == {"output": {"asked": []}}
assert host.effects.staged == [] and not [r for r in host.records.reads if r[0] == (TASK,)], host.records.reads
host = Host(day({"timezone": "Europe/London"}, more, advice, was))
later = prepare.main(fire("2026-11-21T14:00:00Z"), host)["output"]["asked"]
assert len(later) == 1 and later[0].startswith("a") and later[0] != first, later
assert host.effects.staged[0][3]["subjects"] == [TASK + "/a", TASK + "/b", TASK + "/e"]
host = Host(day({"timezone": "Europe/London"}, tasks, advice, was))
assert prepare.main(fire("2026-11-21T18:00:00Z"), host)["output"]["asked"] == [first]
assert host.effects.staged[0][4] == {"if_absent": True}
host = Host(day({"timezone": "Europe/London"}, tasks, advice))
again = prepare.main(fire("2026-11-22T12:00:00Z"), host)["output"]["asked"]
assert again[0] == "d20261122" and again[1] != first, again
# One ask holds at most MAX_ADVICE tasks, the newest.
many = {"t%02d" % i: record(TASK, "t%02d" % i, "2026-11-20T%02d:00:00Z" % i, status="proposed", name="n%d" % i) for i in range(20)}
host = Host(day({"timezone": "Europe/London"}, many, asked=asked))
prepare.main(fire("2026-11-21T12:00:00Z"), host)
assert prepare.MAX_ADVICE == 8
assert host.effects.staged[0][3]["subjects"] == [TASK + "/t%02d" % i for i in range(12, 20)], host.effects.staged[0][3]["subjects"]

# A suggested task arriving asks for nothing by itself: the half-hourly pass does.
host = Host(day({"timezone": "Europe/London"}, tasks, advice, asked))
assert prepare.main(delivery(tasks["a"]), host) == {"output": {"asked": []}}
assert host.effects.staged == []

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
        assert URGENTCHECK in props["ask"] and "cannotwait" in props["ask"] and "canwait" in props["ask"], props["ask"]
        # The preparer holds no write on a task, so nothing asks it for one.
        assert "patch" not in props["ask"] and "urgentCheckedAt" not in props["ask"], props["ask"]
task = urgent(prioritySetBy="api", priorityWas="high")
assert "with no reason given" in prepare.urgent_ask(task)
# A mark set again after it was checked is asked about again: the check that
# was there is part of what is asked.
host = Host(day({"timezone": "Europe/London"}, {"u": task}, asked=asked))
once = prepare.main(delivery(task), host)["output"]["asked"]
checks = {"u": record(URGENTCHECK, "u", "2026-11-21T11:02:00Z", verdict="canwait", task={"ref": TASK + "/u"})}
host = Host(day({"timezone": "Europe/London"}, {"u": task}, asked=asked, checks=checks))
twice = prepare.main(delivery(task), host)["output"]["asked"]
assert once != twice and twice[0].startswith("u"), (once, twice)

# A repository without the tasks package: the morning is still asked for, and nothing fails.
host = Host({PREFERENCE: SET_UP, PREPARATION: {}, ADVICE: {}, URGENTCHECK: {}})
assert prepare.main(fire("2026-11-21T09:00:00Z"), host) == {"output": {"asked": ["d20261121"]}}

# ---- stampurgent: the verdict becomes the task's stamp, and nothing else --------
def check(verdict, reason=None, task=TASK + "/u"):
    properties = {"task": {"ref": task}, "verdict": verdict}
    if reason:
        properties["reason"] = reason
    return record(URGENTCHECK, "u", "2026-11-21T11:02:00Z", **properties)


waiting = urgent(prioritySetBy="automation", priorityReason="A drain waits.")
host = Host({TASK: {"u": waiting}})
assert stampurgent.main(delivery(check("cannotwait", "The drain starts at noon.")), host) == {"output": {"stamped": ["urgentCheckedAt"]}}
op, kind, id, props, how = host.effects.staged[0]
assert (op, kind, id, how) == ("patch", TASK, "u", {"if_version": 3, "on_conflict": "yield"}), host.effects.staged
assert set(props) == {"urgentCheckedAt"} and props["urgentCheckedAt"].endswith("Z"), props
host = Host({TASK: {"u": waiting}})
out = stampurgent.main(delivery(check("canwait", "  It is due  on Friday.\n")), host)
assert out == {"output": {"stamped": ["urgentHeldAt", "urgentHeldReason"]}}, out
props = host.effects.staged[0][3]
assert set(props) == {"urgentHeldAt", "urgentHeldReason"} and props["urgentHeldReason"] == "It is due on Friday.", props
# Nothing is written for a mark the owner took off, a mark the owner or the
# assistant set, a task already stamped, a record that is not a task, a task
# that is gone, or a verdict nobody declared; and never anything but stamps.
for task, verdict, target in [
    (urgent(prioritySetBy="owner", priority="high"), "cannotwait", TASK + "/u"),
    (urgent(prioritySetBy="assistant"), "cannotwait", TASK + "/u"),
    (urgent(prioritySetBy="api", urgentHeldAt="2026-11-21T11:01:00Z"), "cannotwait", TASK + "/u"),
    (waiting, "cannotwait", "substrate.reamde.dev/tasks/list/u"),
    (None, "canwait", TASK + "/u"),
    (waiting, "done", TASK + "/u"),
]:
    host = Host({TASK: {"u": task} if task else {}})
    out = stampurgent.main(delivery(check(verdict, "Because.", target)), host)
    assert "skipped" in out["output"] and host.effects.staged == [], (out, host.effects.staged)
# A repository without the tasks package: nothing read, nothing written, no failure.
host = Host({})
assert stampurgent.main(delivery(check("canwait")), host) == {"output": {"skipped": "no task"}}
print("ok")
`
