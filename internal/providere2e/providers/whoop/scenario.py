#!/usr/bin/env python3
"""What the WHOOP mirrors must be true of, once the runner has synced them.

`tools/e2e.py` owns the run — it serves the recordings, rewires the closure at
the mock, writes the config and the account, completes the OAuth grant and
waits for the on-connect trigger's first sync to settle. This file is what runs
after that, in both modes:

    the e2e run            MODE=e2e   providers/whoop/fixtures
    a seed run (mneme tree only)  MODE=seed  raw/whoop  ->  the owner's repository

THE EXPECTED INVENTORY IS READ OFF THE RECORDINGS, not hard-coded: re-cutting
the fixture set changes the counts, and a scenario that hard-codes them is a
scenario that fails for the wrong reason. What is asserted is the SHAPE the
guide requires and the relations the cut was built to carry.

WHAT IT PROVES

  1. every mirror row the recordings describe exists, ONCE, at WHOOP's own id —
     the v2 UUID for a sleep and a workout, the cycle id for a recovery, which
     has no id of its own;
  2. NOTHING the guide forbids is declared or written: no `raw`, no derived
     `date` / `calories` / `asleepMs` / `sport` / `hrvMs` (every one of them a
     property substrate's version 9 carried), no `person` / `episode` slot, no
     sync bookkeeping on a mirror — the account holds every cursor;
  3. every relation is a REFERENCE that resolves to a row of the right kind:
     recovery -> cycle, recovery -> sleep, sleep -> cycle, and every mirror ->
     user. The ONE reference the cut deliberately leaves dangling is the
     recovery whose sleep fell outside the sleep window, which is what proves
     `recovery.sleep` carries no `mustExist`;
  4. the temporal rule (§4): `at` is `start` on a cycle, a sleep and a workout
     and `created_at` on a recovery, `endsAt` is `end`, the CURRENT cycle has
     no `endsAt` at all, and `updatedAt` — a mutation mark — is never the
     trait's instant;
  5. every `scoreState` is a spelled enum value, and `score` is present on
     exactly the SCORED rows;
  6. WHOOP's own units survive: kilojoules, metres, milliseconds, and no kcal;
  7. the `nextToken` walk actually ran — the second page's request is in the
     mock's log and its records landed;
  8. nothing MISSED the mock, and every request that fell back did so only by
     dropping the pagination pair (`limit`, `start`) the window rides in;
  9. a second, forced sync is idempotent — same rows, same ids, same values.
"""

import json
import os
import pathlib
import sys
import time
import urllib.parse

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from e2e import API  # noqa: E402  the runner's client, on PYTHONPATH

AUTHORITY = "providers.substrate.reamde.dev/whoop"
KINDS = {k: AUTHORITY + "/" + k
         for k in ("user", "cycle", "recovery", "sleep", "workout")}
MIRRORS = ("user", "cycle", "recovery", "sleep", "workout")

# Nothing the guide forbids may appear on any mirror row. The first five are
# substrate's version 9 of this bundle, property for property: a `raw` blob, a
# calendar day sliced out of `created_at`, kcal computed from kilojoules, a
# sum WHOOP does not publish, a sport name looked up from an id, and an HRV
# field whose name hides which statistic it is.
FORBIDDEN = ("raw", "date", "calories", "asleepMs", "sport", "hrvMs",
             "person", "episode", "task", "userId_", "syncCursor",
             "syncCursors", "lastSyncedAt", "lastCompletedAt", "syncStatus",
             "retryNotBefore", "cycleId_", "sleepId_", "start", "end")

FAILURES = []


def ok(condition, message):
    print(("    ok    " if condition else "    FAIL  ") + message)
    if not condition:
        FAILURES.append(message)


def props(rec):
    return (rec or {}).get("properties") or {}


def instant(value):
    """One RFC 3339 instant, normalised.

    The engine stores a datetime in its own canonical spelling, so comparing
    what came back against what WHOOP sent is a comparison of INSTANTS and
    never of strings — `2026-09-08T05:41:12.884Z` and
    `2026-09-08T05:41:12.884000+00:00` are the same moment and a string test
    calls them different.
    """
    if not value:
        return None
    import datetime as _dt
    try:
        return _dt.datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    except ValueError:
        return str(value)


def same(a, b):
    return instant(a) == instant(b)


def ref(value):
    """A reference value is `{ref: "<kind>/<id>"}`; a list of them is a list."""
    if isinstance(value, dict):
        return value.get("ref") or ""
    return value or ""


def all_records(api, kind):
    """Every row of a kind. A FAILED page raises: returning the rows read so
    far would turn a transport error into a smaller inventory, and every count
    below would then be compared against a number nobody measured."""
    out, cursor = [], ""
    while True:
        q = "/api/v1/records?first=500&filter=" + urllib.parse.quote(
            json.dumps({"kinds": [kind]}))
        if cursor:
            q += "&after=" + urllib.parse.quote(cursor)
        st, body, _ = api.call("GET", q)
        if st != 200 or not isinstance(body, dict):
            raise SystemExit("reading %s failed with HTTP %s — the inventory "
                             "below would be a lie" % (kind, st))
        rows = body.get("records") or []
        out.extend(rows)
        cursor = body.get("cursor") or ""
        if not rows or not cursor or len(rows) < 500:
            return out


# ------------------------------------------------------ the recordings' side


def _recs(d, pattern):
    """Recordings only — a raw pull keeps a `.headers.json` sidecar next to
    every body, and the globs must not read those as bodies."""
    return [f for f in d.glob(pattern) if not f.name.endswith(".headers.json")]


def recordings(directory):
    """What the recordings say the mirrors should hold.

    Keyed by WHOOP's own identity: a cycle by its numeric `id`, a sleep and a
    workout by their v2 UUID, a RECOVERY by the cycle it scores — because a v2
    recovery has no id of its own and that is exactly the fact this scenario
    exists to hold onto.
    """
    out = {"profile": {}, "body": {}, "cycles": {}, "recoveries": {},
           "sleeps": {}, "workouts": {}, "pages": 0}
    d = pathlib.Path(directory)
    for f in _recs(d, "GET_v2_user_profile_basic*.json"):
        out["profile"] = json.loads(f.read_text())
    for f in _recs(d, "GET_v2_user_measurement_body*.json"):
        out["body"] = json.loads(f.read_text())
    for pattern, bucket, key in (
            ("GET_v2_cycle*.json", "cycles", "id"),
            ("GET_v2_recovery*.json", "recoveries", "cycle_id"),
            ("GET_v2_activity_sleep*.json", "sleeps", "id"),
            ("GET_v2_activity_workout*.json", "workouts", "id")):
        for f in _recs(d, pattern):
            body = json.loads(f.read_text())
            if not isinstance(body, dict) or "records" not in body:
                continue
            out["pages"] += 1
            for rec in body.get("records") or []:
                if rec.get(key) not in (None, ""):
                    out[bucket][rec[key]] = rec
    return out


# ------------------------------------------------------------------ the sync


def account(api, kind, aid):
    st, body, _ = api.call("GET", "/api/v1/%s/%s" % (kind, aid))
    return props(body) if st == 200 and isinstance(body, dict) else {}


def force_sync(api, kind, aid, timeout=300):
    """Ask for a sync and wait for a stamp newer than the one already there.

    `syncRequestedAt` is the only thing that can ask: the on-connect trigger is
    guarded to fire once per account, the schedule fires at most hourly, and a
    direct `function call` runs ONE invocation and hands the paged checkpoint
    back to its caller rather than draining it.
    """
    before = account(api, kind, aid).get("lastSyncedAt") or ""
    # SUB-SECOND, and really so: `.000000` appended to a whole second is a
    # whole second wearing a disguise, and the guard this drives compares
    # strings (Codex review, finding 10).
    import datetime as _dt
    stamp = _dt.datetime.now(_dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
    st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (kind, aid),
                           {"properties": {"syncRequestedAt": stamp}})
    if st >= 400:
        raise SystemExit("could not request a sync: %s" % json.dumps(body)[:300])
    deadline = time.time() + timeout
    while time.time() < deadline:
        p = account(api, kind, aid)
        if (p.get("lastSyncedAt") or "") > before:
            return p
        time.sleep(2)
    raise SystemExit("the requested sync never stamped the account (last: %s)"
                     % account(api, kind, aid).get("syncStatus"))


def mock_call(mock_url, path, method="GET", body=None):
    """The mock's own control surface — its fault rules and its request log.

    A failure is a property of the RUN, not of the recordings: the rules are
    posted between two syncs so the SAME fixture set serves the success path
    and the failure path, and the two cannot drift
    (`tools/mockserver.py`, "INJECTED FAILURES").
    """
    if not mock_url:
        return {}
    import urllib.request
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(mock_url + path, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        raw = resp.read().decode("utf-8")
    return json.loads(raw) if raw else {}


def mock_requests(mock_url):
    if not mock_url:
        return []
    try:
        import urllib.request
        with urllib.request.urlopen(mock_url + "/__mock/requests", timeout=20) as r:
            body = json.loads(r.read().decode())
    except Exception:                                            # noqa: BLE001
        return []
    if isinstance(body, dict):
        return body.get("requests") or []
    return body if isinstance(body, list) else []


# ------------------------------------------------------------------- the run


def main():
    server = os.environ["SUBSTRATE_SERVER"]
    token = os.environ["SUBSTRATE_TOKEN"]
    mode = os.environ.get("MODE", "e2e")
    akind = os.environ.get("ACCOUNT_KIND", AUTHORITY + "/account")
    aid = os.environ.get("ACCOUNT_ID", "e2e")
    recdir = pathlib.Path(os.environ["RECORDINGS"])
    mock_url = os.environ.get("MOCK_URL", "")
    api = API(server, token)
    want = recordings(recdir)

    print("--> the account")
    acct = account(api, akind, aid)
    ok(str(acct.get("syncStatus", "")).startswith("ok"),
       "the first sync finished ok (%s)" % acct.get("syncStatus"))
    ok(bool(acct.get("lastCompletedAt")),
       "a walk RAN OUT OF WORK — lastCompletedAt is the only evidence a sync "
       "finished, as opposed to being attempted (%s)"
       % acct.get("lastCompletedAt"))
    cursors = acct.get("syncCursors") or {}
    ok(isinstance(cursors, dict)
       and set(cursors) >= {"cycle", "recovery", "sleep", "workout"},
       "every stream banked its own floor, in a keyed map: %s" % sorted(cursors))
    ok("." in str(acct.get("lastSyncedAt") or ""),
       "the sync stamp is SUB-SECOND (%s) — a request landing in the same "
       "whole second would otherwise close the request guard forever"
       % acct.get("lastSyncedAt"))
    ok("userId" not in acct and "memberId" not in acct,
       "the account carries no member id scalar — the member is a reference")
    ok(not acct.get("retryNotBefore"),
       "nothing was rate limited (retryNotBefore: %s)"
       % (acct.get("retryNotBefore") or "unset"))

    # ---- the core `sync` trait (substrate decision 0085, T-084) ----------
    # The account binds `[accountconfig, sync]`, so the Connections page,
    # `GET /api/v1/sync/status` and `substratectl sync status` read it without
    # knowing what WHOOP is. What is asserted here is what the TRAIT
    # promises, not what this bundle's own `syncStatus` string says: the two
    # are written by the same patch and a scenario that only read the old one
    # could not tell a bound kind from an unbound one.
    print("--> the core `sync` trait")
    ok(acct.get("syncState") == "ok",
       "syncState is `ok` after the first sync — which means the walk ran OUT "
       "OF WORK, not merely that nothing failed (%s)" % acct.get("syncState"))
    ok(bool(acct.get("syncMessage")) and "erroring" not in str(
        acct.get("syncMessage") or ""),
       "syncMessage carries the one human line the run left (%s)"
       % acct.get("syncMessage"))
    ok(not acct.get("syncError") and not acct.get("syncErrorAt"),
       "and no syncError — a clean run files none (%s)"
       % (acct.get("syncError") or "unset"))
    prog = acct.get("syncProgress") if isinstance(
        acct.get("syncProgress"), dict) else {}
    ok(set(prog) >= {"phase", "done", "total", "pending"},
       "syncProgress is {phase, done, total, pending} (%s)" % (prog or "unset"))
    ok(prog.get("phase") == "done" and prog.get("pending") == 0,
       "and it says every enabled collection drained (%s)" % prog)
    ok(prog.get("done") == prog.get("total") == 4,
       "done == total == the four enabled collections — cycle, recovery, "
       "sleep and workout (%s of %s)" % (prog.get("done"), prog.get("total")))
    ok(not acct.get("syncStreams"),
       "syncStreams is UNWRITTEN: the four collections are one function, one "
       "queue and one outcome, which decision 0085 answers with "
       "syncRequestedAck alone (%s)" % (acct.get("syncStreams") or "unset"))
    ok(not acct.get("syncPaused"),
       "and the account is not paused (%s)" % (acct.get("syncPaused"),))
    # THE DISPATCHER'S OWN HALF. The first sync is the on-connect trigger, a
    # RECORD-sourced delivery, so the engine stamps the start and the duration
    # around it (decision 0085) and the body stamps neither.
    started, took = acct.get("lastSyncStartedAt"), acct.get("lastSyncDurationMs")
    ok(bool(started),
       "lastSyncStartedAt is stamped by the DISPATCHER around the "
       "record-sourced first delivery (%s)" % (started or "unset"))
    ok(isinstance(took, int) and took >= 0,
       "and lastSyncDurationMs is the duration it measured (%s)" % took)

    rows = {name: all_records(api, kind) for name, kind in KINDS.items()}
    got = {name: len(v) for name, v in rows.items()}
    by_path = {}
    for name, kind in KINDS.items():
        for r in rows[name]:
            by_path[kind + "/" + r["id"]] = (name, r)

    member_ref = ref(acct.get("user"))
    ok(member_ref in by_path and by_path[member_ref][0] == "user",
       "account.user resolves to a whoop/user row")
    if member_ref in by_path and want["profile"]:
        ok(props(by_path[member_ref][1]).get("userId") == want["profile"].get("user_id"),
           "and it is the member the basic profile names")

    print("--> the rows the recordings describe")
    if mode == "e2e":
        for name, expect in (("cycle", len(want["cycles"])),
                             ("recovery", len(want["recoveries"])),
                             ("sleep", len(want["sleeps"])),
                             ("workout", len(want["workouts"]))):
            ok(got[name] == expect,
               "%s: %d rows, one per record (want %d)" % (name, got[name], expect))
        ok(got["user"] == 1,
           "user: exactly one member row — every record names the same "
           "`user_id` and the two sibling calls patch that same row (got %d)"
           % got["user"])
    else:
        for name in MIRRORS:
            print("    --    %s: %d rows" % (name, got[name]))
        ok(got["user"] >= 1, "user: %d rows" % got["user"])

    print("--> the member is ONE row out of TWO sibling calls")
    for r in rows["user"]:
        p = props(r)
        if want["profile"]:
            ok(p.get("firstName") == want["profile"].get("first_name"),
               "the member carries the profile call's `first_name`")
        if want["body"]:
            ok(p.get("maxHeartRate") == want["body"].get("max_heart_rate"),
               "and the body measurement's `max_heart_rate` on the SAME row "
               "— two calls, one kind")
            ok(p.get("weightKilogram") == want["body"].get("weight_kilogram"),
               "with WHOOP's own unit: kilograms, unconverted")

    print("--> ids are WHOOP's own, and unique")
    for name, idprop in (("cycle", "cycleId"), ("sleep", "sleepId"),
                         ("workout", "workoutId")):
        ids = [props(r).get(idprop) for r in rows[name]]
        ok(len(set(ids)) == len(ids) and None not in ids,
           "%s: every row carries a distinct %s" % (name, idprop))
    if mode == "e2e":
        ok({props(r).get("cycleId") for r in rows["cycle"]} == set(want["cycles"]),
           "cycle: the ids are the recordings' own")
        ok({props(r).get("sleepId") for r in rows["sleep"]} == set(want["sleeps"]),
           "sleep: the ids are the recordings' own v2 UUIDs")
        ok({props(r).get("workoutId") for r in rows["workout"]} == set(want["workouts"]),
           "workout: the ids are the recordings' own v2 UUIDs")

    # THE TWO IDENTITIES (§3). v2 keys a sleep and a workout on a UUID and
    # keeps the retired v1 number beside it as `v1_id`. Both must survive, the
    # key must be the UUID, and a record that carries no `v1_id` must carry
    # none — WHOOP documents it as gone after 09/01/2025, so a null would be
    # this mirror inventing a fact.
    for name, idprop, source in (("sleep", "sleepId", want["sleeps"]),
                                 ("workout", "workoutId", want["workouts"])):
        ok(all(isinstance(props(r).get(idprop), str) for r in rows[name]),
           "%s: the key is the v2 UUID string, not the v1 number" % name)
        if mode == "e2e" and source:
            with_v1 = {k for k, v in source.items() if v.get("v1_id") is not None}
            carried = {props(r).get(idprop) for r in rows[name]
                       if props(r).get("v1Id") is not None}
            ok(carried == with_v1,
               "%s: `v1Id` is on exactly the %d record(s) that carry one, and "
               "absent on the rest" % (name, len(with_v1)))
            wrong = [props(r).get(idprop) for r in rows[name]
                     if props(r).get("v1Id") is not None
                     and props(r).get("v1Id") != (source.get(props(r).get(idprop))
                                                  or {}).get("v1_id")]
            ok(not wrong,
               "%s: `v1Id` is the recording's own NUMBER, not merely present "
               "(%d wrong)" % (name, len(wrong)))

    print("--> a recovery is keyed by the cycle it scores")
    # It has no id of its own, so the cycle reference IS the identity. Two
    # recoveries of one cycle would be the defect this checks for.
    cycle_refs = [ref(props(r).get("cycle")) for r in rows["recovery"]]
    ok(len(set(cycle_refs)) == len(cycle_refs) and "" not in cycle_refs,
       "every recovery names a distinct cycle (%d rows)" % len(cycle_refs))
    ok(all("recoveryId" not in props(r) for r in rows["recovery"]),
       "and no row carries an invented recovery id")

    print("--> nothing the guide forbids, in the DECLARATIONS")
    # Checking the stored rows alone cannot see a property nothing happened to
    # write. The declaration is where a `raw: json` or a consumer slot would
    # live, so it is read (Codex review, finding 10).
    declared = {}
    for name, kind in KINDS.items():
        st, body, _ = api.call("GET", "/api/v1/substrate.reamde.dev/core/kind/"
                                      + urllib.parse.quote(kind, safe=""))
        if st == 200 and isinstance(body, dict):
            declared[name] = ((body.get("properties") or {}).get("properties")
                              or {})
    ok(len(declared) == len(KINDS),
       "every mirror kind's declaration reads back (%d of %d)"
       % (len(declared), len(KINDS)))
    bad_decl = []
    for name, decl in declared.items():
        for prop, d in decl.items():
            if prop in FORBIDDEN:
                bad_decl.append("%s.%s declared" % (name, prop))
            if isinstance(d, dict) and d.get("type") == "json":
                bad_decl.append("%s.%s is json" % (name, prop))
    ok(not bad_decl,
       "no mirror kind DECLARES a forbidden or json property (%s)" % bad_decl[:4])
    if declared:
        for name, field in (("cycle", "score"), ("recovery", "score"),
                            ("sleep", "score"), ("workout", "score")):
            d = (declared.get(name) or {}).get(field) or {}
            ok(d.get("type") == "object" and bool(d.get("fields")),
               "%s.%s is a DECLARED object with typed fields, not a blob"
               % (name, field))

    print("--> nothing the guide forbids, in the ROWS")
    stray = set()
    for name in MIRRORS:
        for r in rows[name]:
            stray |= {k for k in props(r) if k in FORBIDDEN}
    # A consumer slot the BUNDLE never declared may still sit on a row: the
    # engine synthesises a record mapping's `property` on its source kind
    # (#570). Those are the mapping owner's, not the provider's.
    synthesised = set()
    for m in all_records(api, "substrate.reamde.dev/core/recordmapping"):
        mp = props(m)
        src = mp.get("from")
        src = src.get("ref", "") if isinstance(src, dict) else str(src or "")
        if (AUTHORITY + "/") in src and mp.get("property"):
            synthesised.add(mp["property"])
    stray -= synthesised
    ok(not stray, "no forbidden property on any mirror row (found: %s)"
       % sorted(stray))
    for name in MIRRORS:
        carried = set()
        for r in rows[name]:
            carried |= set(props(r))
        leaks = {k for k in carried if k == "raw" or k.endswith("Kcal")
                 or k.endswith("Calories")}
        ok(not leaks, "%s: nothing raw and nothing converted (%s)"
           % (name, sorted(leaks)))

    print("--> every relation is a reference that resolves")
    def resolves(value, kind_name):
        path = ref(value)
        if not path:
            return True, ""
        hit = by_path.get(path)
        if hit and hit[0] == kind_name:
            return True, ""
        return False, path

    # A recovery row is keyed by its cycle, so this is how a row is traced back
    # to the record it came from.
    cycle_id_by_path = {KINDS["cycle"] + "/" + r["id"]: props(r).get("cycleId")
                        for r in rows["cycle"]}
    cycle_id_of = {r["id"]: cycle_id_by_path.get(ref(props(r).get("cycle")))
                   for r in rows["recovery"]}

    bad, dangling_sleep = [], []
    for name, pairs in (("cycle", (("user", "user"),)),
                        ("recovery", (("user", "user"), ("cycle", "cycle"))),
                        ("sleep", (("user", "user"), ("cycle", "cycle"))),
                        ("workout", (("user", "user"),))):
        for r in rows[name]:
            for prop, kind_name in pairs:
                good, path = resolves(props(r).get(prop), kind_name)
                if not good:
                    bad.append("%s.%s -> %s" % (r["id"], prop, path))
    for r in rows["recovery"]:
        good, path = resolves(props(r).get("sleep"), "sleep")
        if not good:
            dangling_sleep.append(path)
    ok(not bad, "every reference names a row of its pinned kind (%d dangling: "
                "%s)" % (len(bad), bad[:3]))
    ok(all(ref(props(r).get("user")) == member_ref for name in MIRRORS
           for r in rows[name] if name != "user"),
       "every mirror row points at the ONE member — 'mine' is a reference "
       "filter, not a flag")
    if mode == "e2e":
        # The cut carries exactly one recovery whose `sleep_id` names a sleep
        # the cut does not hold — a deliberate coverage gap, not a window
        # exclusion (the two streams share a floor). The reference is written
        # and does NOT resolve, which is the whole point: `recovery.sleep` has
        # no `mustExist`, so the row is written anyway and heals on a walk that
        # reaches the sleep, instead of minting a sleep from a reference.
        expected = {r["sleep_id"] for r in want["recoveries"].values()
                    if r.get("sleep_id") not in want["sleeps"]}
        ok(len(dangling_sleep) == len(expected),
           "%d recovery reference(s) name a sleep the cut does not hold, "
           "which is what proves `recovery.sleep` carries no mustExist"
           % len(dangling_sleep))
        ok(bool(want["sleeps"]) and len(dangling_sleep) < len(rows["recovery"]),
           "and the rest resolve to real sleep rows")

        # A REFERENCE THAT RESOLVES TO THE RIGHT KIND IS NOT ENOUGH. The
        # question is whether it names the row the PAYLOAD named, and a check
        # that stops at the kind would pass a sync that pointed every recovery
        # at the same cycle (Codex review, finding 10).
        by_cycle_id = {props(r).get("cycleId"): KINDS["cycle"] + "/" + r["id"]
                       for r in rows["cycle"]}
        by_sleep_id = {props(r).get("sleepId"): KINDS["sleep"] + "/" + r["id"]
                       for r in rows["sleep"]}
        wrong = []
        for r in rows["recovery"]:
            cid = cycle_id_of.get(r["id"])
            rec = want["recoveries"].get(cid)
            if not rec:
                continue
            if ref(props(r).get("cycle")) != by_cycle_id.get(rec["cycle_id"], ""):
                wrong.append("recovery %s.cycle" % cid)
            want_sleep = by_sleep_id.get(rec.get("sleep_id"))
            if want_sleep and ref(props(r).get("sleep")) != want_sleep:
                wrong.append("recovery %s.sleep" % cid)
        for r in rows["sleep"]:
            rec = want["sleeps"].get(props(r).get("sleepId"))
            if rec and ref(props(r).get("cycle")) != by_cycle_id.get(rec["cycle_id"], ""):
                wrong.append("sleep %s.cycle" % rec["id"])
        ok(not wrong,
           "and every reference names the row the PAYLOAD named, not merely a "
           "row of the right kind (%d wrong: %s)" % (len(wrong), wrong[:3]))

    print("--> the temporal trait is bound where the kind has an instant of "
          "its own")
    if mode == "e2e":
        off = []
        for r in rows["cycle"]:
            rec = want["cycles"].get(props(r).get("cycleId"))
            if not rec:
                continue
            if not same(props(r).get("at"), rec.get("start")):
                off.append(("cycle at", r["id"]))
            if rec.get("end") and not same(props(r).get("endsAt"), rec["end"]):
                off.append(("cycle endsAt", r["id"]))
            if not rec.get("end") and props(r).get("endsAt"):
                off.append(("cycle endsAt on a current cycle", r["id"]))
        for r in rows["sleep"]:
            rec = want["sleeps"].get(props(r).get("sleepId"))
            if rec and not (same(props(r).get("at"), rec.get("start"))
                            and same(props(r).get("endsAt"), rec.get("end"))):
                off.append(("sleep span", r["id"]))
        for r in rows["workout"]:
            rec = want["workouts"].get(props(r).get("workoutId"))
            if rec and not (same(props(r).get("at"), rec.get("start"))
                            and same(props(r).get("endsAt"), rec.get("end"))):
                off.append(("workout span", r["id"]))
        ok(not off, "`at` is `start` and `endsAt` is `end` on every cycle, "
                    "sleep and workout (%d off: %s)" % (len(off), off[:3]))
        current = [r for r in rows["cycle"]
                   if not (want["cycles"].get(props(r).get("cycleId")) or {}).get("end")]
        ok(bool(current) and all(not props(r).get("endsAt") for r in current),
           "the CURRENT cycle has no `endsAt` at all — WHOOP omits `end` until "
           "the member wakes, and an absent end is not a null one (%d)"
           % len(current))
        # A recovery's `at` is its OWN `created_at` — the instant WHOOP
        # recorded the verdict. The row is keyed by its cycle, so the record it
        # came from is found by the cycle the row points at.
        off = []
        for r in rows["recovery"]:
            rec = want["recoveries"].get(cycle_id_of.get(r["id"]))
            if rec and not same(props(r).get("at"), rec.get("created_at")):
                off.append(r["id"])
        ok(not off, "a recovery's `at` is its own `created_at` (%d off)"
           % len(off))
    # The rule the ticket names: `temporal` binds a kind's OWN instant, never a
    # mutation mark. `updated_at` is one on all four kinds, so it is a declared
    # property and never the trait's slot.
    mutation_as_at = []
    for name in ("cycle", "sleep", "workout", "recovery"):
        for r in rows[name]:
            p = props(r)
            if p.get("updatedAt") and p.get("at") == p.get("updatedAt") \
                    and p.get("createdAt") and p.get("createdAt") != p.get("at"):
                mutation_as_at.append(r["id"])
    ok(not mutation_as_at,
       "no row put `updated_at` — a mutation mark — in the trait's slot (%d)"
       % len(mutation_as_at))
    ok(all(props(r).get("createdAt") for r in rows["cycle"]),
       "a cycle keeps `created_at` as its own declared property, beside the "
       "trait's span")
    ok(all("createdAt" not in props(r) for r in rows["recovery"]),
       "a recovery does NOT redeclare `created_at` — it IS the trait's `at`")

    print("--> every mapped value, record by record")
    if mode == "e2e":
        # Presence is not equality. This walks the API field -> property map
        # the sync carries and compares the VALUE, so a sync that wrote the
        # right shape with the wrong numbers fails here rather than passing
        # nine "is present" assertions (Codex review, finding 10).
        SCALARS = {
            "cycle": (("timezone_offset", "timezoneOffset"),
                      ("created_at", "createdAt"), ("updated_at", "updatedAt")),
            "sleep": (("timezone_offset", "timezoneOffset"), ("nap", "nap"),
                      ("v1_id", "v1Id"), ("created_at", "createdAt"),
                      ("updated_at", "updatedAt")),
            "workout": (("timezone_offset", "timezoneOffset"),
                        ("sport_name", "sportName"), ("sport_id", "sportId"),
                        ("v1_id", "v1Id"), ("created_at", "createdAt"),
                        ("updated_at", "updatedAt")),
            "recovery": (("updated_at", "updatedAt"),),
        }
        SCORE = {
            "cycle": (("strain", "strain"), ("kilojoule", "kilojoule"),
                      ("average_heart_rate", "averageHeartRate"),
                      ("max_heart_rate", "maxHeartRate")),
            "recovery": (("user_calibrating", "userCalibrating"),
                         ("recovery_score", "recoveryScore"),
                         ("resting_heart_rate", "restingHeartRate"),
                         ("hrv_rmssd_milli", "hrvRmssdMilli"),
                         ("spo2_percentage", "spo2Percentage"),
                         ("skin_temp_celsius", "skinTempCelsius")),
            "sleep": (("respiratory_rate", "respiratoryRate"),
                      ("sleep_performance_percentage", "sleepPerformancePercentage"),
                      ("sleep_consistency_percentage", "sleepConsistencyPercentage"),
                      ("sleep_efficiency_percentage", "sleepEfficiencyPercentage")),
            "workout": (("strain", "strain"),
                        ("average_heart_rate", "averageHeartRate"),
                        ("max_heart_rate", "maxHeartRate"),
                        ("kilojoule", "kilojoule"),
                        ("percent_recorded", "percentRecorded"),
                        ("distance_meter", "distanceMeter"),
                        ("altitude_gain_meter", "altitudeGainMeter"),
                        ("altitude_change_meter", "altitudeChangeMeter")),
        }
        NESTED = {
            "sleep": (("stage_summary", "stageSummary",
                       (("total_in_bed_time_milli", "totalInBedTimeMilli"),
                        ("total_awake_time_milli", "totalAwakeTimeMilli"),
                        ("total_no_data_time_milli", "totalNoDataTimeMilli"),
                        ("total_light_sleep_time_milli", "totalLightSleepTimeMilli"),
                        ("total_slow_wave_sleep_time_milli", "totalSlowWaveSleepTimeMilli"),
                        ("total_rem_sleep_time_milli", "totalRemSleepTimeMilli"),
                        ("sleep_cycle_count", "sleepCycleCount"),
                        ("disturbance_count", "disturbanceCount"))),
                      ("sleep_needed", "sleepNeeded",
                       (("baseline_milli", "baselineMilli"),
                        ("need_from_sleep_debt_milli", "needFromSleepDebtMilli"),
                        ("need_from_recent_strain_milli", "needFromRecentStrainMilli"),
                        ("need_from_recent_nap_milli", "needFromRecentNapMilli")))),
            "workout": (("zone_durations", "zoneDurations",
                         (("zone_zero_milli", "zoneZeroMilli"),
                          ("zone_one_milli", "zoneOneMilli"),
                          ("zone_two_milli", "zoneTwoMilli"),
                          ("zone_three_milli", "zoneThreeMilli"),
                          ("zone_four_milli", "zoneFourMilli"),
                          ("zone_five_milli", "zoneFiveMilli"))),),
        }
        SOURCE = {"cycle": ("cycles", "cycleId"), "sleep": ("sleeps", "sleepId"),
                  "workout": ("workouts", "workoutId")}
        wrong, compared = [], 0
        for name in ("cycle", "sleep", "workout", "recovery"):
            for r in rows[name]:
                if name == "recovery":
                    rec = want["recoveries"].get(cycle_id_of.get(r["id"]))
                else:
                    bucket, idprop = SOURCE[name]
                    rec = want[bucket].get(props(r).get(idprop))
                if not rec:
                    continue
                p = props(r)
                for api_key, prop in SCALARS[name]:
                    compared += 1
                    # An INSTANT is compared as an instant. The engine stores a
                    # datetime in its own canonical spelling and drops a
                    # trailing zero from the fraction, so
                    # `2026-09-10T08:47:19.640Z` reads back as `…19.64Z` — the
                    # same moment, a different string, and a string test calls
                    # one row in five wrong.
                    if api_key.endswith("_at"):
                        if not same(rec.get(api_key), p.get(prop)):
                            wrong.append("%s.%s" % (name, prop))
                    elif rec.get(api_key) != p.get(prop):
                        wrong.append("%s.%s" % (name, prop))
                got_score = p.get("score") or {}
                raw_score = rec.get("score") or {}
                for api_key, prop in SCORE[name]:
                    compared += 1
                    if raw_score.get(api_key) != got_score.get(prop):
                        wrong.append("%s.score.%s" % (name, prop))
                for api_key, prop, fields in NESTED.get(name, ()):
                    got_sub = got_score.get(prop) or {}
                    raw_sub = raw_score.get(api_key) or {}
                    for sub_api, sub_prop in fields:
                        compared += 1
                        if raw_sub.get(sub_api) != got_sub.get(sub_prop):
                            wrong.append("%s.score.%s.%s" % (name, prop, sub_prop))
        ok(not wrong,
           "%d values compared field by field against the recordings, %d wrong "
           "(%s)" % (compared, len(wrong), sorted(set(wrong))[:4]))

    print("--> enums, scores and units")
    states = {props(r).get("scoreState")
              for name in ("cycle", "recovery", "sleep", "workout")
              for r in rows[name]}
    ok(states <= {"scored", "pendingscore", "unscorable", None},
       "every scoreState is a spelled enum value: %s" % sorted(s for s in states if s))
    if mode == "e2e":
        ok("unscorable" in states and "pendingscore" in states,
           "the cut exercises the two states that carry NO score, not only "
           "the happy one: %s" % sorted(s for s in states if s))
    mismatched = []
    for name in ("cycle", "recovery", "sleep", "workout"):
        for r in rows[name]:
            p = props(r)
            if p.get("scoreState") == "scored" and not p.get("score"):
                mismatched.append(("scored without a score", r["id"]))
            if p.get("scoreState") in ("pendingscore", "unscorable") and p.get("score"):
                mismatched.append(("unscored with a score", r["id"]))
    ok(not mismatched,
       "`score` is present on exactly the SCORED rows (%d off: %s)"
       % (len(mismatched), mismatched[:3]))
    scored = [props(r).get("score") or {} for r in rows["workout"]
              if props(r).get("scoreState") == "scored"]
    ok(all("kilojoule" in s for s in scored),
       "a workout's energy is in WHOOP's own kilojoules")
    ok(any(s.get("zoneDurations") for s in scored),
       "and the heart-rate zones landed as a declared object, not json")
    ok(any(s.get("distanceMeter") is not None for s in scored)
       and any(s.get("distanceMeter") is None for s in scored),
       "distance is present on the activities WHOOP measures it for and "
       "ABSENT on the rest — not zero")
    sleeps = [props(r).get("score") or {} for r in rows["sleep"]
              if props(r).get("scoreState") == "scored"]
    ok(all(s.get("stageSummary") for s in sleeps),
       "every scored sleep carries the whole stage summary, declared")
    ok(all("asleepMs" not in (s.get("stageSummary") or {}) for s in sleeps),
       "and nothing summed the stages into a total WHOOP does not publish")
    ok(all(s.get("sleepNeeded") for s in sleeps),
       "and the four terms of `sleep_needed`, none of them added up")
    if mode == "e2e":
        naps = [r for r in rows["sleep"] if props(r).get("nap") is True]
        nights = [r for r in rows["sleep"] if props(r).get("nap") is False]
        ok(bool(naps) and bool(nights),
           "a nap and a night are ONE kind, told apart by WHOOP's own `nap` "
           "flag (%d naps, %d nights)" % (len(naps), len(nights)))
        older = [r for r in rows["recovery"]
                 if (props(r).get("score") or {}).get("recoveryScore") is not None
                 and (props(r).get("score") or {}).get("spo2Percentage") is None]
        ok(bool(older),
           "a recovery from hardware with no pulse oximeter carries no "
           "`spo2Percentage` — absent, not zero (%d)" % len(older))
        sports = {props(r).get("sportName") for r in rows["workout"]}
        ok(None not in sports and sports,
           "every workout carries WHOOP's own `sport_name`: %s"
           % sorted(s for s in sports if s))
        with_id = [r for r in rows["workout"] if props(r).get("sportId") is not None]
        ok(len(with_id) < len(rows["workout"]),
           "and the deprecated `sport_id` is on only the records that still "
           "carry one (%d of %d)" % (len(with_id), len(rows["workout"])))

    print("--> the paged walk, and what the mock was actually asked")
    log = mock_requests(mock_url)
    asked = [r for r in log if r.get("method") == "GET"]
    missed = [r for r in asked if r.get("match") == "miss"]
    ok(not missed, "no request MISSED a recording (%d: %s)"
       % (len(missed), [r.get("path") for r in missed[:3]]))
    with_end = [r for r in asked if "end=" in (r.get("query") or "")]
    ok(not with_end,
       "the sync sends no `end` — WHOOP defaults it to now, and one fewer "
       "clock reading in the request is one fewer in the recording's name")
    paged = [r for r in asked if "nextToken=" in (r.get("query") or "")]
    # The cut holds more collection pages than it holds streams exactly when a
    # stream was recorded over two pages, and that is the only thing that makes
    # a `nextToken` request obligatory.
    if mode == "e2e" and want["pages"] > 4:
        ok(bool(paged),
           "the `nextToken` walk ran — %d request(s) carried a page token"
           % len(paged))
        ok(len(rows["cycle"]) == len(want["cycles"]),
           "and BOTH pages of the paged stream landed (%d cycle rows over %d "
           "recorded pages)" % (len(rows["cycle"]), want["pages"]))
    # The VERDICT WORD is not the assertion. `tools/mockserver.py` labels a
    # match by its position in the DEDUPLICATED candidate list, and this
    # provider has no query relaxation, so its page fallback dedups into the
    # slot the word "~relaxed" names. What matters is which FILE answered, so
    # that is what is checked.
    on_disk = {f.name for f in _recs(recdir, "*.json")} - {"expectations.json"}
    served = {r.get("file") for r in asked if r.get("match") != "oauth"}
    stray = {f for f in served if f and f not in on_disk}
    ok(not stray, "every request was answered by a committed recording (%s)"
       % sorted(stray))
    if mode == "e2e":
        unused = on_disk - served - {"audit.py"}
        ok(not unused,
           "and every recording in the cut was ASKED FOR — a file nothing "
           "requests proves nothing (%s)" % sorted(unused))

    # ---------------------------------------------------------- second pass
    print("--> a second sync is idempotent")
    before = {name: {r["id"] for r in rows[name]} for name in MIRRORS}
    before_props = {r["id"]: props(r) for name in MIRRORS for r in rows[name]}
    acct2 = force_sync(api, akind, aid)
    ok(str(acct2.get("syncStatus", "")).startswith("ok"),
       "the second sync finished ok (%s)" % acct2.get("syncStatus"))
    ok(acct2.get("syncRequestedAck")
       and str(acct2["syncRequestedAck"]) == str(acct2.get("syncRequestedAt")),
       "and it acknowledged the request it was asked to serve")
    # ONE PROPERTY, ONE MEANING. `syncRequestedAck` is both the on-request
    # trigger's guard and the core `sync` trait's acknowledgement, so the
    # console's "Sync now" and this scenario's forced pass read the same stamp
    # (decision 0085, T-084).
    ok(acct2.get("syncState") == "ok",
       "and the trait agrees the second run finished (%s)"
       % acct2.get("syncState"))
    after_rows = {name: all_records(api, KINDS[name]) for name in MIRRORS}
    after = {name: {r["id"] for r in after_rows[name]} for name in MIRRORS}
    ok(after == before,
       "every row is the one the first sync minted: %s"
       % {k: len(v) for k, v in after.items()})
    # WHOLE MAPS, BOTH WAYS. A comparison that only walks the BEFORE map
    # passes a second sync that ADDED a property, which is exactly what a
    # thin-over-fat write or a newly-derived field looks like (Codex review,
    # finding 10).
    drifted = []
    for name in MIRRORS:
        for r in after_rows[name]:
            was = before_props.get(r["id"])
            if was is None:
                continue
            now = props(r)
            for k in set(was) | set(now):
                if k not in now:
                    drifted.append("%s.%s (gone)" % (r["id"], k))
                elif k not in was:
                    drifted.append("%s.%s (appeared)" % (r["id"], k))
                elif now[k] != was[k]:
                    drifted.append("%s.%s" % (r["id"], k))
    ok(not drifted,
       "not one property appeared, vanished or changed across two identical "
       "syncs (%d drifted: %s)" % (len(drifted), drifted[:6]))
    cursors2 = acct2.get("syncCursors") or {}
    ok(all(cursors2.get(k, "") >= cursors.get(k, "") for k in cursors),
       "and every stream's floor moved forward or stood still, never back")

    # ------------------------------------------- the trait under failure
    # E2E ONLY, and LAST: it deliberately breaks two syncs, and everything
    # after it would read a broken account. A failure is never injected into a
    # seed — the owner's repository is not a test bench.
    if mode == "e2e" and mock_url:
        print("--> a 429 is `throttled` and a 500 is `erroring` (T-084)")
        # BOTH RULES SIT ON THE PROFILE READ, the first call of every run
        # whatever the cursors say — a rule on a stream endpoint would depend
        # on which collection the run happens to reach first.
        mock_call(mock_url, "/__mock/faults", "POST", {"rules": [
            {"match": "GET /v2/user/profile/basic", "status": [429],
             "headers": {"Retry-After": "1"},
             "body": {"message": "rate limited"}}]})
        held = force_sync(api, akind, aid)
        ok(held.get("syncState") == "throttled",
           "a 429 is `throttled`, NOT `erroring`: WHOOP named the instant to "
           "come back and the run is work to resume (%s)"
           % held.get("syncState"))
        ok(bool(held.get("retryNotBefore")),
           "and retryNotBefore carries that instant, which holds every path "
           "until it passes (%s)" % held.get("retryNotBefore"))
        ok(not held.get("syncError"),
           "a throttled run files NO syncError — nothing failed (%s)"
           % (held.get("syncError") or "unset"))
        ok("rate limited" in str(held.get("syncMessage") or ""),
           "the message says what happened (%s)" % held.get("syncMessage"))
        # WAIT THE HOLD OUT, deliberately and from the account's own stamp:
        # `_held` refuses every way in while it stands, so a request stamped
        # inside the window would be skipped without a trace and the wait
        # below would time out against a sync that never ran.
        until = instant(held.get("retryNotBefore"))
        if until is not None and not isinstance(until, str):
            import datetime as _dt2
            wait = (until - _dt2.datetime.now(_dt2.timezone.utc)).total_seconds()
            if wait > 0:
                time.sleep(min(wait + 1.0, 90.0))

        mock_call(mock_url, "/__mock/faults", "POST", {"rules": [
            # A 500 is TRANSIENT: `_get` raises it as an HTTPError, the body's
            # own failure path stamps the account, and the delivery still
            # settles — so the trait's `erroring` is the body's word and not a
            # park's.
            {"match": "GET /v2/user/profile/basic", "status": [500] * 6,
             "body": {"message": "the mock is standing in for an outage"}}]})
        bad = force_sync(api, akind, aid)
        ok(bad.get("syncState") == "erroring",
           "after an injected 500 the state is `erroring` (%s)"
           % bad.get("syncState"))
        ok(bool(bad.get("syncError")) and bool(bad.get("syncErrorAt")),
           "and syncError carries the text with the instant it was written "
           "(%s @ %s)" % (bad.get("syncError"), bad.get("syncErrorAt")))
        ok(str(bad.get("syncError") or "") == str(bad.get("syncMessage") or ""),
           "the message and the error are the same sentence — a console shows "
           "one line and it is this one (%s)" % bad.get("syncMessage"))
        ok(str(bad.get("syncStatus") or "").startswith("erroring:"),
           "and the legacy syncStatus string says it too, unchanged: the "
           "trait is ADDITIVE and a reader of the old shape keeps working "
           "(%s)" % bad.get("syncStatus"))
        ok(str(bad.get("syncRequestedAck") or "")
           == str(bad.get("syncRequestedAt") or ""),
           "a FAILED run still acknowledges the request it served — an "
           "unacknowledged one would re-fire the trigger against the same "
           "failure forever (%s)" % bad.get("syncRequestedAck"))
        # AND IT RECOVERS. An account that cannot come back from a transient
        # failure is the same bug as one that never reported it.
        mock_call(mock_url, "/__mock/faults", "DELETE")
        good = force_sync(api, akind, aid)
        ok(good.get("syncState") == "ok",
           "with the rules cleared the next run raises the state back to `ok` "
           "(%s)" % good.get("syncState"))
        ok(str(good.get("syncError") or "") == str(bad.get("syncError") or ""),
           "while syncError stays as the RECORD of the last failure — the "
           "state is what says the account is healthy again, not the absence "
           "of a past error (%s)" % good.get("syncError"))
        final = {name: {r["id"] for r in all_records(api, KINDS[name])}
                 for name in MIRRORS}
        ok(final == before,
           "and every row survived the outage: %s"
           % {k: len(v) for k, v in final.items()})

    print()
    summary = ", ".join("%s %d" % (k, got[k]) for k in sorted(got) if got[k])
    if mode == "seed":
        print("SEEDED  " + summary)
    if FAILURES:
        print("FAIL  %d assertion(s):" % len(FAILURES))
        for f in FAILURES:
            print("  - " + f)
        return 1
    print("PASS  " + summary)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
