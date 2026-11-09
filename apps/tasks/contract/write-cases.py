#!/usr/bin/env python3
"""Writes the contract's cases. The expectations are worked out by hand from
README.md's rules, not computed: this script only spares the cases from
being typed as JSON. Run it from this folder after changing a case:

    python3 write-cases.py
"""

import json
import pathlib

HERE = pathlib.Path(__file__).parent
PACKAGE = json.loads((HERE / "../TasksKit/Sources/TasksCore/Resources/Package/tasks.json").read_text())


def own(name):
    for doc in PACKAGE["documents"]:
        if doc["metadata"]["id"] == f"substrate.reamde.dev/tasks/{name}":
            data = doc["data"]
            return {"identity": doc["metadata"]["id"], **({"label": data["label"]} if "label" in data else {}), "definition": data}
    raise KeyError(name)


TASK, LIST, COLLECTION, PREFERENCE = (f"substrate.reamde.dev/tasks/{n}" for n in ("task", "list", "collection", "preference"))
OWN = [own("task"), own("list"), own("collection"), own("preference")]
BASE_GRANT = ["write:substrate.reamde.dev/tasks/*", "read:implements(substrate.reamde.dev/core/lifecycle)"]
NOW, ZONE = "2026-11-09T09:41:00Z", "Europe/London"

CHORE, AREA, TICKET = "ada.example.com/work/chore", "ada.example.com/work/area", "ada.example.com/flow/ticket"
CHORE_DECL = {
    "identity": CHORE,
    "label": {"singular": "Chore", "plural": "Chores"},
    "definition": {
        "authority": "ada.example.com", "package": "work", "names": {"singular": "chore"},
        "traits": ["substrate.reamde.dev/core/temporal(point: due)", "substrate.reamde.dev/core/lifecycle(status: state)"],
        "displayTemplate": "{summary} · {code}",
        "properties": {
            "summary": {"type": "string", "required": True},
            "code": {"type": "string"},
            "due": {"type": "datetime"},
            "state": {"type": "enum", "values": ["suggested", "todo", "doing", "closed", "skipped"],
                      "done": ["closed"], "dropped": ["skipped"], "proposed": ["suggested"]},
            "priority": {"type": "enum", "values": ["low", "normal", "high"]},
            "area": {"type": "reference", "kind": AREA},
            "tags": {"type": "string", "repeated": True},
        },
    },
}
AREA_DECL = {
    "identity": AREA,
    "label": {"singular": "Area", "plural": "Areas"},
    "definition": {"authority": "ada.example.com", "package": "work", "names": {"singular": "area"},
                   "displayTemplate": "{name}", "properties": {"name": {"type": "string"}}},
}
TICKET_DECL = {
    "identity": TICKET,
    "definition": {
        "authority": "ada.example.com", "package": "flow", "names": {"singular": "ticket"},
        "traits": ["substrate.reamde.dev/core/lifecycle"],
        "displayTemplate": "{headline}",
        "properties": {
            "headline": {"type": "string", "required": True},
            "dueAt": {"type": "datetime"},
            "closedAt": {"type": "datetime"},
            "status": {
                "type": "state", "states": ["suggested", "backlog", "active", "resolved", "wontfix"], "initial": "backlog",
                "transitions": [
                    {"from": "suggested", "to": "backlog"}, {"from": "suggested", "to": "active"},
                    {"from": "suggested", "to": "wontfix"}, {"from": "backlog", "to": "active"},
                    {"from": "backlog", "to": "wontfix"},
                    {"from": "active", "to": "resolved", "stamps": {"closedAt": "now"}},
                    {"from": "resolved", "to": "active"},
                ],
                "done": ["resolved"], "dropped": ["wontfix"], "proposed": ["suggested"],
            },
        },
    },
}


def rec(record_kind, rid, created, version=1, **props):
    return {"kind": record_kind, "id": rid, "version": version, "createdAt": created, "properties": props}


def task(rid, created, name, **props):
    props.setdefault("status", "open")
    return rec(TASK, rid, created, name=name, title=name, **props)


def lst(rid, name, position):
    return rec(LIST, rid, "2026-10-01T08:00:00Z", name=name, title=name, position=position)


def acts(accept=(), dismiss=(), complete=(), wontDo=(), reopen=()):
    return {"accept": list(accept), "dismiss": list(dismiss), "complete": list(complete),
            "wontDo": list(wontDo), "reopen": list(reopen)}


OPEN_OWN = acts(complete=["done"], wontDo=["abandoned"])


def item(standing, actions, group, band):
    return {"standing": standing, **actions, "group": group, "band": band}


def collection(kind, enabled, viewOnly, creatable, title, due, dueDay, priority, order, group):
    return {"kind": kind, "enabled": enabled, "viewOnly": viewOnly, "creatable": creatable, "title": title,
            "due": due, "dueDay": dueDay, "priority": priority, "order": order, "group": group}


OWN_COLLECTION = collection(TASK, True, False, True, "name", "dueAt", "dueDay", "priority", "position",
                            {"property": "list", "kind": LIST})


def patch(path, version, **props):
    return {"op": "patch", "path": path, "ifVersion": version, "properties": props}


T = lambda rid: f"{TASK}/{rid}"
L = lambda rid: f"{LIST}/{rid}"
C = lambda rid: f"{CHORE}/{rid}"
K = lambda rid: f"{TICKET}/{rid}"

CASES = {}

# 1. The app's own tasks: every band, a suggestion, a done task, a list that
#    is gone; another collection found but off.
CASES["own-tasks"] = {
    "name": "the app's own tasks in every band, with a suggestion and a missing list",
    "now": NOW, "timeZone": ZONE, "grant": BASE_GRANT,
    "declarations": OWN + [CHORE_DECL],
    "records": [
        lst("l1", "Home", "1"), lst("l2", "Groceries", "2"),
        task("t1", "2026-11-01T08:00:00Z", "Pay rent", dueDay="2026-11-05", priority="urgent", list=L("l1"), position="1"),
        task("t2", "2026-11-02T08:00:00Z", "Call the plumber", dueAt="2026-11-09T08:00:00Z", dueDay="2026-11-09",
             priority="high", list=L("l1"), position="2"),
        task("t3", "2026-11-03T08:00:00Z", "Renew the passport", priority="urgent"),
        task("t4", "2026-11-03T09:00:00Z", "Replace the shower head", priority="none", list=L("l1"), position="3"),
        task("t5", "2026-11-04T08:00:00Z", "Book the dentist", dueDay="2026-11-10", list=L("l1"), position="4"),
        task("t6", "2026-11-04T09:00:00Z", "Renew the insurance", dueDay="2026-11-30", list=L("l2"), position="1"),
        task("t7", "2026-11-09T07:00:00Z", "Reply to Maya Chen", status="proposed"),
        task("t8", "2026-11-05T08:00:00Z", "Water the plants", status="done", completedAt="2026-11-08T10:00:00Z", list=L("l2")),
        task("t9", "2026-11-06T08:00:00Z", "Pick up the parcel", list=L("l9")),
    ],
    "expect": {
        "collections": [
            OWN_COLLECTION,
            collection(CHORE, False, True, False, None, "due", None, "priority", None, None),
        ],
        "items": {
            T("t1"): item("open", OPEN_OWN, L("l1"), "lateOrUrgent"),
            T("t2"): item("open", OPEN_OWN, L("l1"), "lateOrUrgent"),
            T("t3"): item("open", OPEN_OWN, "none", "lateOrUrgent"),
            T("t4"): item("open", OPEN_OWN, L("l1"), "noDate"),
            T("t5"): item("open", OPEN_OWN, L("l1"), "comingUp"),
            T("t6"): item("open", OPEN_OWN, L("l2"), "later"),
            T("t7"): item("proposed", acts(accept=["open"], dismiss=["abandoned"]), "none", None),
            T("t8"): item("done", acts(reopen=["open"]), L("l2"), None),
            T("t9"): item("open", OPEN_OWN, "missing", "noDate"),
        },
        "needsYou": {"lateOrUrgent": [T("t1"), T("t2"), T("t3")], "comingUp": [T("t5")], "later": [T("t6")],
                     "noDate": 2, "suggestions": 1, "badge": 3},
        "suggestions": [T("t7")],
        "groups": [{"collection": TASK, "kind": LIST, "rows": [
            {"group": L("l2"), "open": 1}, {"group": L("l1"), "open": 4},
            {"group": "none", "open": 1}, {"group": "missing", "open": 1}]}],
    },
    "commands": [
        {"do": {"accept": T("t7")}, "writes": [patch(T("t7"), 1, status="open")]},
        {"do": {"accept": T("t7"), "edits": {"list": L("l1"), "priority": "high"}},
         "writes": [patch(T("t7"), 1, status="open", list=L("l1"), priority="high")]},
        {"do": {"dismiss": T("t7")}, "writes": [patch(T("t7"), 1, status="abandoned")]},
        {"do": {"complete": T("t1")}, "writes": [patch(T("t1"), 1, status="done")]},
        {"do": {"reopen": T("t8")}, "writes": [patch(T("t8"), 1, status="open", completedAt=None)]},
        {"do": {"accept": T("t1")}, "writes": []},
    ],
}

# 2. A second collection: a renamed enum status, a template title, a group
#    kind with no status read under an exact scope, the same id in two kinds.
CASES["second-collection"] = {
    "name": "a second collection with a renamed enum status, its own group kind, and an id another kind shares",
    "now": NOW, "timeZone": ZONE,
    "grant": BASE_GRANT + [f"write:{CHORE}", f"read:{AREA}"],
    "declarations": OWN + [CHORE_DECL, AREA_DECL],
    "records": [
        rec(COLLECTION, "c-chore", "2026-11-01T08:00:00Z", kind=CHORE, enabled=True, title=CHORE),
        rec(AREA, "a1", "2026-10-01T08:00:00Z", name="Garden", title="Garden"),
        task("x1", "2026-11-01T08:00:00Z", "Pay rent", dueDay="2026-11-05"),
        rec(CHORE, "x1", "2026-11-02T08:00:00Z", summary="Clean the gutters", code="C-1", title="Clean the gutters · C-1",
            state="todo", due="2026-11-08T12:00:00Z", area=f"{AREA}/a1"),
        rec(CHORE, "x2", "2026-11-09T06:00:00Z", summary="Fix the fence", code="C-2", title="Fix the fence · C-2",
            state="suggested", area=f"{AREA}/a1"),
        rec(CHORE, "x3", "2026-11-03T08:00:00Z", summary="Sharpen the mower", code="C-3", title="Sharpen the mower · C-3",
            state="doing", priority="high", area=f"{AREA}/a9"),
        rec(CHORE, "x4", "2026-11-03T09:00:00Z", summary="Rake the leaves", code="C-4", title="Rake the leaves · C-4",
            state="closed", area=f"{AREA}/a1"),
        rec(CHORE, "x5", "2026-11-04T08:00:00Z", summary="Oil the hinges", code="C-5", title="Oil the hinges · C-5",
            state="todo", priority="normal"),
    ],
    "expect": {
        "collections": [
            OWN_COLLECTION,
            collection(CHORE, True, False, False, None, "due", None, "priority", None, {"property": "area", "kind": AREA}),
        ],
        "items": {
            T("x1"): item("open", OPEN_OWN, "none", "lateOrUrgent"),
            C("x1"): item("open", acts(complete=["closed"], wontDo=["skipped"]), f"{AREA}/a1", "lateOrUrgent"),
            C("x2"): item("proposed", acts(accept=["todo", "doing"], dismiss=["skipped"]), f"{AREA}/a1", None),
            C("x3"): item("open", acts(complete=["closed"], wontDo=["skipped"]), "missing", "lateOrUrgent"),
            C("x4"): item("done", acts(reopen=["todo", "doing"]), f"{AREA}/a1", None),
            C("x5"): item("open", acts(complete=["closed"], wontDo=["skipped"]), "none", "noDate"),
        },
        "needsYou": {"lateOrUrgent": [T("x1"), C("x1"), C("x3")], "comingUp": [], "later": [],
                     "noDate": 1, "suggestions": 1, "badge": 3},
        "suggestions": [C("x2")],
        "groups": [
            {"collection": TASK, "kind": LIST, "rows": [{"group": "none", "open": 1}]},
            {"collection": CHORE, "kind": AREA, "rows": [
                {"group": f"{AREA}/a1", "open": 1}, {"group": "none", "open": 1}, {"group": "missing", "open": 1}]},
        ],
    },
    "commands": [
        {"do": {"accept": C("x2")}, "choices": ["todo", "doing"], "writes": []},
        {"do": {"accept": C("x2"), "to": "doing"}, "writes": [patch(C("x2"), 1, state="doing")]},
        {"do": {"complete": C("x1")}, "writes": [patch(C("x1"), 1, state="closed")]},
        {"do": {"reopen": C("x4")}, "choices": ["todo", "doing"], "writes": []},
        {"do": {"complete": T("x1")}, "writes": [patch(T("x1"), 1, status="done")]},
    ],
}

# 3. A machine with two ways to accept, a stamp a reopen clears, and a move
#    the machine does not declare.
CASES["two-destinations"] = {
    "name": "a state machine that accepts into two states and stamps its close",
    "now": NOW, "timeZone": ZONE,
    "grant": BASE_GRANT + [f"write:{TICKET}"],
    "declarations": OWN + [TICKET_DECL],
    "records": [
        rec(COLLECTION, "c-ticket", "2026-11-01T08:00:00Z", kind=TICKET, enabled=True, title=TICKET),
        rec(TICKET, "k1", "2026-11-08T08:00:00Z", headline="Rotate the keys", title="Rotate the keys", status="suggested"),
        rec(TICKET, "k2", "2026-11-02T08:00:00Z", version=3, headline="Patch the router", title="Patch the router",
            status="resolved", closedAt="2026-11-07T08:00:00Z"),
        rec(TICKET, "k3", "2026-11-03T08:00:00Z", headline="Renew the certificate", title="Renew the certificate",
            status="backlog", dueAt="2026-11-12T10:00:00Z"),
    ],
    "expect": {
        "collections": [
            OWN_COLLECTION,
            collection(TICKET, True, False, False, "headline", "dueAt", None, None, None, None),
        ],
        "items": {
            K("k1"): item("proposed", acts(accept=["backlog", "active"], dismiss=["wontfix"]), None, None),
            K("k2"): item("done", acts(reopen=["active"]), None, None),
            K("k3"): item("open", acts(wontDo=["wontfix"]), None, "comingUp"),
        },
        "needsYou": {"lateOrUrgent": [], "comingUp": [K("k3")], "later": [], "noDate": 0, "suggestions": 1, "badge": 0},
        "suggestions": [K("k1")],
        "groups": [{"collection": TASK, "kind": LIST, "rows": []}],
    },
    "commands": [
        {"do": {"accept": K("k1")}, "choices": ["backlog", "active"], "writes": []},
        {"do": {"accept": K("k1"), "to": "active", "edits": {"dueAt": "2026-11-20T09:00:00Z"}},
         "writes": [patch(K("k1"), 1, status="active", dueAt="2026-11-20T09:00:00Z")]},
        {"do": {"reopen": K("k2")}, "writes": [patch(K("k2"), 3, status="active", closedAt=None)]},
        {"do": {"complete": K("k3")}, "writes": []},
        {"do": {"accept": K("k1"), "to": "resolved"}, "writes": []},
    ],
}

# 4. A collection the grant only reads, the app's group kind out of reach for
#    the other, and pins that are gone or out of reach.
CASES["view-only-and-pins"] = {
    "name": "a view-only collection, and pins that are deleted or out of reach",
    "now": NOW, "timeZone": ZONE, "grant": BASE_GRANT,
    "declarations": OWN + [CHORE_DECL],
    "records": [
        rec(COLLECTION, "c-chore", "2026-11-01T08:00:00Z", kind=CHORE, enabled=True, title=CHORE),
        rec(PREFERENCE, "p", "2026-11-01T08:00:00Z", pins=[L("l1"), L("l4"), f"{AREA}/a1"]),
        lst("l1", "Home", "1"),
        {**lst("l4", "Old", "2"), "deletedAt": "2026-11-05T08:00:00Z"},
        task("t1", "2026-11-01T08:00:00Z", "Replace the shower head", list=L("l1")),
        rec(CHORE, "y1", "2026-11-09T06:00:00Z", summary="Fix the fence", code="C-2", title="Fix the fence · C-2",
            state="suggested", area=f"{AREA}/a1"),
    ],
    "expect": {
        "collections": [
            OWN_COLLECTION,
            collection(CHORE, True, True, False, None, "due", None, "priority", None, None),
        ],
        "items": {
            T("t1"): item("open", OPEN_OWN, L("l1"), "noDate"),
            C("y1"): item("proposed", acts(), None, None),
        },
        "needsYou": {"lateOrUrgent": [], "comingUp": [], "later": [], "noDate": 1, "suggestions": 1, "badge": 0},
        "suggestions": [C("y1")],
        "groups": [{"collection": TASK, "kind": LIST, "rows": [{"group": L("l1"), "open": 1}]}],
        "pins": [L("l1")],
    },
    "commands": [
        {"do": {"accept": C("y1")}, "writes": []},
        {"do": {"dismiss": C("y1")}, "writes": []},
    ],
}

for name, case in CASES.items():
    (HERE / f"{name}.json").write_text(json.dumps(case, indent=2, ensure_ascii=False) + "\n")
print("\n".join(sorted(CASES)))
