#!/usr/bin/env python3
"""What the Notion mirrors must be true of, once the runner has synced them.

`tools/e2e.py` owns the run — it serves the recordings, rewires the closure at
the mock, writes the config and the account (token auth, so no consent flow)
and waits for the on-connect trigger's first sync to settle. This file is what
runs after that, in both modes:

    the e2e run            MODE=e2e   providers/notion/fixtures
    a seed run (mneme tree only)  MODE=seed  raw/notion -> the owner's repository

THE EXPECTED INVENTORY IS READ OFF THE RECORDINGS, never hard-coded: the
fixture set is a CUT of the owner's real workspace
(`tools/rawpull/notion.py --cut`), so re-cutting it changes the counts and a
scenario that hard-codes them fails for the wrong reason.

WHAT IT PROVES

  1. every mirror row the recordings describe exists, once, at Notion's own
     id — NORMALISED, so the dashed and undashed spellings of one UUID are
     one row and not two;
  2. NOTHING the guide forbids is declared or written: no `raw`, no `person`
     or `task` slot, no scalar copy of a referent, no sync bookkeeping on a
     mirror — the account holds every cursor and `notion/pagesync` holds
     every per-page one;
  3. every relation is a REFERENCE that resolves to a row of the right kind,
     including the ones INSIDE the `parent` object;
  4. the cases the cut was built for: a page that is a database ROW, a page
     under a page, a page at the top of the workspace, a data source with its
     schema, the database behind it, a block tree that PAGED;
  5. rich text is kept as Notion sent it AND decoded beside it: the json
     property and the plain-text decode agree span for span;
  6. every enum holds a spelled value, and `at` is `created_time`;
  7. a second, forced sync is idempotent — same rows, same ids, same values;
  8. the BACKFILL WINDOW is the account's own: a depth the owner changes
     reaches the walk, an unchanged one keeps the floor it anchored, and
     `all` means NO floor rather than the last floor anybody stored (T-045).

IT RUNS OVER A SEED TOO, AND A SEED IS A MILLION ROWS. `notion/block` streams
rather than materialising (one row is ~2.5 kB, and the owner's workspace holds
1,038,462 of them): every id lands in the reference map and identity is folded
over every row, while the per-row checks read `NOTION_BLOCK_SAMPLE` of them
(default 20,000) and the run says so. What a seed's second pass may do that a
fixture run may not — grow, because the continuation just reached trees nobody
had walked — is asserted as growth rather than as a duplicate (T-054).
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

AUTHORITY = "providers.substrate.reamde.dev/notion"
KINDS = {k: AUTHORITY + "/" + k for k in
         ("user", "database", "datasource", "page", "block", "pagesync")}
MIRRORS = ("user", "database", "datasource", "page", "block")

# Nothing the guide forbids may appear on any mirror row. `person` is NOT on
# this list for `user`: it is Notion's OWN field there (`{email,
# email_verified}`), and the consumer slot a mapping synthesises has the same
# name — so the check below is that `user.person` is an OBJECT, and that no
# OTHER mirror carries one at all.
FORBIDDEN = ("raw", "task", "episode", "workspaceName", "authorName",
             "createdByName", "pageTitle", "syncCursors", "syncPending",
             "lastSyncedAt", "lastCompletedAt", "syncStatus", "blocksCursor",
             "blocksSyncedAt", "isRow", "fromOwner")

FAILURES = []


def ok(condition, message):
    print(("    ok    " if condition else "    FAIL  ") + message)
    if not condition:
        FAILURES.append(message)


def props(rec):
    return (rec or {}).get("properties") or {}


def ref(value):
    if isinstance(value, dict):
        return value.get("ref") or ""
    return value or ""


def refs(value):
    return [ref(v) for v in (value or [])]


def instant(v):
    """The moment a datetime string names, whatever it is spelled like.

    The engine normalises an RFC 3339 instant on the way in — Notion sends
    `…T21:14:00.000Z` and the row reads back without the milliseconds — so a
    string comparison here would fail 582 of 582 rows over a difference that
    is not one.
    """
    import datetime as _dt
    s = str(v or "").strip()
    if not s:
        return None
    try:
        return _dt.datetime.fromisoformat(s.replace("Z", "+00:00"))
    except ValueError:
        return s


def ident(raw):
    s = str(raw or "").replace("-", "").lower()
    return s if len(s) == 32 else ""


def all_records(api, kind):
    """Every row of a kind. A FAILED page raises: returning the rows read so
    far would turn a transport error into a smaller inventory, and every count
    below would be compared against a number nobody measured."""
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


def scan_records(api, kind, on_row=None, keep=0):
    """Page a kind WITHOUT holding it.

    `all_records` returns the rows, which is right for a fixture set of 577
    blocks and impossible for the owner's workspace: one `notion/block` row is
    ~2.5 kB of JSON and there are 1,038,462 of them, so the list alone is
    2.6 GB before Python's own overhead (T-054). A seed-mode run therefore
    STREAMS the blocks — every row is folded as it arrives, `keep` of them are
    retained so the per-row assertions have real rows to read, and the count
    is the count of all of them.

    Returns `(kept rows, total)`.
    """
    kept, total, cursor = [], 0, ""
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
        for r in rows:
            total += 1
            if on_row:
                on_row(r)
            if keep and len(kept) < keep:
                kept.append(r)
        cursor = body.get("cursor") or ""
        if not rows or not cursor or len(rows) < 500:
            return kept, total


# ------------------------------------------------------ the recordings' side


def plain(rich):
    if not isinstance(rich, list):
        return ""
    return "".join(str(s.get("plain_text") or "") for s in rich
                   if isinstance(s, dict))


def recordings(directory):
    """What the fixture set says the mirrors should hold."""
    out = {"users": {}, "pages": {}, "datasources": {}, "databases": {},
           "blocks": {}, "me": "", "paged": set(), "parents": {},
           "blockTypes": set(), "propertyTypes": set()}
    d = pathlib.Path(directory)

    me = d / "GET_v1_users_me.json"
    if me.exists():
        body = json.loads(me.read_text())
        out["me"] = ident(body.get("id"))
        out["users"][out["me"]] = body
    for f in sorted(d.glob("GET_v1_users__*.json")):
        for u in json.loads(f.read_text()).get("results") or []:
            out["users"][ident(u.get("id"))] = u
    for f in sorted(d.glob("POST_v1_search__*.json")):
        for o in json.loads(f.read_text()).get("results") or []:
            key = ident(o.get("id"))
            if not key:
                continue
            if o.get("object") == "page":
                out["pages"][key] = o
                for v in (o.get("properties") or {}).values():
                    if isinstance(v, dict) and v.get("type"):
                        out["propertyTypes"].add(v["type"])
            elif o.get("object") == "data_source":
                out["datasources"][key] = o
            out["parents"][key] = o.get("parent") or {}
    for f in sorted(d.glob("GET_v1_databases_*.json")):
        body = json.loads(f.read_text())
        if ident(body.get("id")):
            out["databases"][ident(body["id"])] = body
    for f in sorted(d.glob("GET_v1_blocks_*_children__*.json")):
        body = json.loads(f.read_text())
        parent = ident(f.name.split("GET_v1_blocks_", 1)[1]
                       .split("_children", 1)[0])
        if body.get("has_more"):
            out["paged"].add(parent)
        for b in body.get("results") or []:
            key = ident(b.get("id"))
            if key:
                out["blocks"][key] = b
                out["blockTypes"].add(b.get("type"))
    return out


# ------------------------------------------------------------------ the sync


def account(api, kind, aid):
    st, body, _ = api.call("GET", "/api/v1/%s/%s" % (kind, aid))
    return props(body) if st == 200 and isinstance(body, dict) else {}


def pending_size(acct_props):
    """How much work the account is still holding, over every queue."""
    pending = acct_props.get("syncPending")
    if not isinstance(pending, dict):
        return 0
    return sum(len(v) if isinstance(v, list) else 1 for v in pending.values())


def force_sync(api, kind, aid, timeout=300, wait_pending=True):
    """Ask for a sync and wait for a stamp newer than the one already there.

    `syncRequestedAt` is the only thing that can ask: the on-connect trigger
    is guarded to fire once per account, the schedule fires at most hourly and
    is disabled for a run, and a direct `function call` runs ONE invocation
    and hands the paged checkpoint back rather than draining it.

    `wait_pending` is what a SEED needs off. One invocation of a bounded drain
    is not meant to end with an empty backlog — over the owner's workspace the
    re-sync discovers thousands of trees the first pass had not reached — so
    waiting for `syncPending` to be empty inside ONE round waits for something
    that only the CONTINUATION rule delivers, and times out on a run that is
    working perfectly (T-054). The caller drives the rounds instead.
    """
    before = account(api, kind, aid).get("lastSyncedAt") or ""
    # A REAL fractional stamp. `.000000Z` on a whole second is not sub-second
    # precision: two requests in the same second produce the same string, and
    # `syncRequestedAt > lastSyncedAt` closes (Codex 21, §4).
    import datetime as _dt
    stamp = _dt.datetime.now(_dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
    st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (kind, aid),
                           {"properties": {"syncRequestedAt": stamp}})
    if st >= 400:
        raise SystemExit("could not request a sync: %s" % json.dumps(body)[:300])
    deadline = time.time() + timeout
    was = account(api, kind, aid)
    last = (pending_size(was), was.get("syncStatus"))
    seen, quiet = False, 0
    while time.time() < deadline:
        p = account(api, kind, aid)
        moved = (p.get("lastSyncedAt") or "") > before
        if moved and not p.get("syncPending"):
            return p                      # a walk ran to the end
        if wait_pending:
            time.sleep(2)
            continue
        # A BOUNDED DRAIN NEVER STAMPS `lastSyncedAt`. `_stamp` runs when the
        # stage list runs out; a round the ENGINE stops on its own bounds
        # (512 invocations, two minutes) is abandoned mid-flight, so the only
        # evidence it ever ran is `syncPending` and `syncStatus`, which
        # `_save_pending` checkpoints as it goes. Waiting for a stamp waits
        # for something that will not come, which is what killed the
        # scenario-only run on the owner's repository (T-054).
        #
        # So the round is over when the account goes STILL: progress seen,
        # then nothing moving for QUIET polls. Re-stamping `syncRequestedAt`
        # while a drain is still running would put two drains on one cursor
        # (T-033), which is worth these few seconds of patience.
        now = (pending_size(p), p.get("syncStatus"))
        if now != last:
            seen, quiet, last = True, 0, now
        else:
            quiet += 1
        if (moved or seen) and quiet >= 8:
            return p
        time.sleep(2)
    # A CONTINUATION'S WINDOW EXPIRING IS NOT A FAILURE. Over the owner's
    # workspace the backlog moved for the WHOLE 900 s and never went still,
    # because the drain kept working the queue — so "it never went quiet"
    # means "it is still going", and raising here threw away 970 trees of
    # progress (T-054). The caller is a drain loop that measures the queue
    # round on round; hand it the account and let it decide. In e2e mode
    # (`wait_pending`) the raise stands: a fixture sync that does not finish
    # inside the window IS a failure.
    if not wait_pending:
        return account(api, kind, aid)
    raise SystemExit("the requested sync neither stamped the account nor "
                     "moved its backlog (last: %s)"
                     % account(api, kind, aid).get("syncStatus"))


def drain_backlog(api, kind, aid, rounds, timeout):
    """Drive the account until its backlog is empty or stops shrinking.

    THE SEED'S OWN CONTINUATION RULE, run from here: `e2e.json`'s `continue`
    stamps `syncRequestedAt` and waits again, and an account with pending work
    reports itself due whatever its cadence says. A bounded drain leaves work
    behind BY DESIGN, so one round proves nothing either way — what a run can
    assert is that the queue is strictly shrinking, and say what is left when
    it stops.

    Returns the backlog size after each round, first entry first.
    """
    sizes = [pending_size(account(api, kind, aid))]
    flat = 0
    for _ in range(max(rounds, 0)):
        if not sizes[-1]:
            break
        after = force_sync(api, kind, aid, timeout=timeout, wait_pending=False)
        sizes.append(pending_size(after))
        # ONE STANDING ROUND IS NOT A STALL. A single page's tree can be 152
        # pages of children deep and spend a whole round on its own, so the
        # queue stands still while real work happens. TWO in a row is the
        # signal that another round buys nothing.
        flat = flat + 1 if sizes[-1] >= sizes[-2] else 0
        if flat >= 2:
            break
    return sizes


def continuation_rule():
    """`continueRounds` / `settleSeconds` out of the provider's own e2e.json,
    so the scenario drives the backlog on the same terms the runner does
    rather than inventing a second set of numbers."""
    cfg = {}
    f = HERE / "e2e.json"
    if f.is_file():
        try:
            cfg = json.loads(f.read_text())
        except json.JSONDecodeError:
            cfg = {}
    seed = cfg.get("seed") or {}
    return (int(seed.get("continueRounds") or 20),
            int(seed.get("settleSeconds") or 300))


def parked_block_ids(pending):
    """The page ids still queued for a block walk.

    A `blocks` entry is a `[pageId, lastEditedTime]` pair — the sync carries
    the edit stamp so a tree whose page has not moved is skipped — so the id
    is READ out of the row rather than assumed to be the row.
    """
    out = set()
    for row in (pending or {}).get("blocks") or []:
        if isinstance(row, (list, tuple)) and row:
            out.add(ident(row[0]))
        elif isinstance(row, str):
            out.add(ident(row))
    return {i for i in out if i}


def mock_call(mock, path, method="GET", body=None):
    """The mock's own control surface — its fault rules and its request log.

    A failure is a property of the RUN, not of the recordings: the rules are
    posted between two syncs so the SAME fixture set serves the success path
    and the failure path, and the two cannot drift
    (`tools/mockserver.py`, "INJECTED FAILURES").
    """
    if not mock:
        return {}
    import urllib.request
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(mock + path, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        raw = resp.read().decode("utf-8")
    return json.loads(raw) if raw else {}


def mock_log(mock):
    """The mock's request log, or an empty one. Fetched ONCE — a 48-round
    seed's log is about a million entries."""
    if not mock:
        return []
    import urllib.request
    try:
        with urllib.request.urlopen(mock + "/__mock/requests", timeout=300) as r:
            log = json.loads(r.read())
    except Exception:
        return []
    return log if isinstance(log, list) else (log.get("requests") or [])


def block_ids_asked(log, status=None):
    """The page ids a request log asked the children of.

    `status=404` is the raw pull's gap made visible on the wire: the sync asked
    for a tree the pull never took. `status=None` is EVERY ask, which is the
    other half of the same question — an id that is in neither set was never
    requested at all, and that is a drain fault rather than a pull gap. The two
    were conflated once, and the message said "never asked for" about ids that
    had only failed to be 404s (T-054).
    """
    out = set()
    for row in log:
        if not isinstance(row, dict):
            continue
        if status is not None and row.get("status") != status:
            continue
        path = str(row.get("path") or "")
        if "/v1/blocks/" not in path or "/children" not in path:
            continue
        out.add(ident(path.split("/v1/blocks/", 1)[1].split("/children", 1)[0]))
    return {i for i in out if i}


def children_parents(log):
    """The block ids a request log asked for the children of and got an answer
    to — a 404 is a tree the recordings do not hold, and counting it as walked
    would let a re-walk of it pass unnoticed."""
    return block_ids_asked(log) - block_ids_asked(log, status=404)


def patched_sync(api, kind, aid, properties):
    """Change an owner-written setting, then sync, then read the account back.

    The patch and the `syncRequestedAt` stamp are two writes and the trigger
    fires off the second, so the setting is always already standing when the
    run starts — which is what makes "the account said `all` and the walk used
    a floor" an assertable statement (T-045).
    """
    st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (kind, aid),
                           {"properties": properties})
    if st >= 400:
        raise SystemExit("could not set %s: %s"
                         % (properties, json.dumps(body)[:300]))
    return force_sync(api, kind, aid)


# ------------------------------------------------------------------- the run


def main():
    server = os.environ["SUBSTRATE_SERVER"]
    token = os.environ["SUBSTRATE_TOKEN"]
    mode = os.environ.get("MODE", "e2e")
    akind = os.environ.get("ACCOUNT_KIND", AUTHORITY + "/account")
    aid = os.environ.get("ACCOUNT_ID", "e2e")
    recdir = pathlib.Path(os.environ["RECORDINGS"])
    api = API(server, token)
    want = recordings(recdir)

    print("--> the account")
    acct = account(api, akind, aid)
    ok(str(acct.get("syncStatus", "")).startswith("ok"),
       "the first sync finished ok (%s)" % acct.get("syncStatus"))
    # A PINNED BACKLOG HAS TWO CAUSES AND THEY LOOK IDENTICAL FROM HERE: work
    # the drain has not reached yet, and work the RECORDINGS CANNOT ANSWER. The
    # owner's seed ran 120 continuation rounds against a deferred set that
    # never moved off 12,721, and the assertion said only "the backlog drained
    # to nothing ({'blocks': [[…]]})" — the whole queue, dumped, saying nothing
    # about why. So when it cannot converge the failure NAMES the mock's own
    # 404s: a tree the pull never recorded can never drain, and that is a pull
    # gap (T-051) rather than a drain fault.
    pending = acct.get("syncPending") if isinstance(
        acct.get("syncPending"), dict) else {}
    pend = ", ".join("%s x%d" % (k, len(v) if isinstance(v, list) else 1)
                     for k, v in sorted(pending.items()))
    if pending and mode == "seed":
        parked = parked_block_ids(pending)
        log = mock_log(os.environ.get("MOCK_URL", ""))
        # AN EMPTY LOG PROVES NOTHING, AND IN A SCENARIO-ONLY RUN IT IS EMPTY
        # BY CONSTRUCTION: the mock process was started by THIS run, minutes
        # ago, and the sync that filled the repository ran against a different
        # one hours or days back. Classifying the parked ids against it said
        # "3,132 of 3,132 NEVER REQUESTED" about a queue whose requests were
        # simply made before this log existed (T-054).
        gap = parked & block_ids_asked(log, status=404) if log else set()
        never = parked - block_ids_asked(log) if log else set()
        if not log:
            pend += ("; this run's mock log is EMPTY, so whether those ids "
                     "were ever asked for cannot be read here — a "
                     "scenario-only run's log covers only what THIS run "
                     "drives")
        if gap:
            pend += ("; %d of the %d parked block trees are ids the mock "
                     "answered 404 for — recordings `tools/rawpull/notion.py` "
                     "never took, so no number of rounds can drain them "
                     "(T-051: `--fill-blocks`, then `--coverage` must say 0 "
                     "MISSING). e.g. %s"
                     % (len(gap), len(parked), ", ".join(sorted(gap)[:3])))
        if never:
            pend += ("; %d of the %d parked block trees were NEVER REQUESTED — "
                     "not a 404, not a skip, no ask at all, which is a DRAIN "
                     "fault and not a pull gap (T-054): %s"
                     % (len(never), len(parked), ", ".join(sorted(never)[:3])))
        if log and parked and not gap and not never:
            pend += ("; all %d parked trees were asked for and answered, so "
                     "the queue is mid-flight rather than stuck" % len(parked))
    ok(not pending, "the backlog drained to nothing (%s)" % (pend or "empty"))
    ok(bool(acct.get("lastCompletedAt")),
       "a walk ran to the end, so lastCompletedAt is stamped (%s)"
       % acct.get("lastCompletedAt"))
    ok("." in str(acct.get("lastSyncedAt") or ""),
       "lastSyncedAt carries sub-second precision, so a same-second "
       "syncRequestedAt can still drive the account (%s)"
       % acct.get("lastSyncedAt"))
    # THE WINDOW THE WALK ACTUALLY USED, checked in BOTH modes (T-045). The
    # owner's seed ran an account that said `backfillDepth: all` and walked to
    # a 90-day floor a previous run had stored, stopped at the first item of
    # the first search page and reported `ok`. Both halves are assertable from
    # the account alone: an `all` account may carry NO floor, and no run may
    # end with its window having admitted nothing.
    cursors = acct.get("syncCursors") if isinstance(
        acct.get("syncCursors"), dict) else {}
    if str(acct.get("backfillDepth") or "") == "all":
        ok(not (cursors.get("floor") or ""),
           "backfillDepth `all` walked with NO floor (stored floor: %s)"
           % (cursors.get("floor") or "none"))
    ok("backfill window admitted nothing" not in str(acct.get("syncStatus") or ""),
       "the backfill window admitted something — the walk was not stopped at "
       "its first item by a floor newer than every object (%s)"
       % acct.get("syncStatus"))
    ok("integrationToken" not in acct and "token" not in acct,
       "the account carries no token — it lives on the config record")
    ok("workspaceName" not in acct,
       "the account carries no copy of the workspace's name — `user` is a "
       "reference and the name is the user row's")

    # ---- the core `sync` trait (substrate decision 0085, T-084) ----------
    # The account binds `[accountconfig, sync]`, so the Connections page,
    # `GET /api/v1/sync/status` and `substratectl sync status` read it without
    # knowing what Notion is. What is asserted here is what the TRAIT
    # promises, not what this bundle's own `syncStatus` string says: the two
    # are written by the same patch and a scenario that only read the old one
    # could not tell a bound kind from an unbound one.
    print("--> the core `sync` trait")
    ok(acct.get("syncState") == "ok",
       "syncState is `ok` after the first sync — which means the drain ran "
       "OUT OF WORK, not merely that nothing failed (%s)"
       % acct.get("syncState"))
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
    ok(prog.get("pending") == 0 and prog.get("phase") == "done",
       "and it says the drain is done with nothing pending — the same fact "
       "syncPending states, in the shape a progress bar reads (%s)" % prog)
    ok(prog.get("done") == prog.get("total"),
       "done == total when the queue is empty (%s of %s)"
       % (prog.get("done"), prog.get("total")))
    ok(not acct.get("syncStreams"),
       "syncStreams is UNWRITTEN: one function walks one workspace as one "
       "stream, which decision 0085 answers with syncRequestedAck alone (%s)"
       % (acct.get("syncStreams") or "unset"))
    ok(not acct.get("syncPaused"),
       "and the account is not paused (%s)" % (acct.get("syncPaused"),))
    # THE DISPATCHER'S OWN HALF. The first sync is the on-connect trigger, a
    # RECORD-sourced delivery, so the engine stamps the start and the
    # duration around it (decision 0085) and the body stamps neither.
    started, took = acct.get("lastSyncStartedAt"), acct.get("lastSyncDurationMs")
    ok(bool(started),
       "lastSyncStartedAt is stamped by the DISPATCHER around the "
       "record-sourced first delivery (%s)" % (started or "unset"))
    ok(isinstance(took, int) and took >= 0,
       "and lastSyncDurationMs is the duration it measured (%s)" % took)

    # `by_path` maps a record path to its KIND NAME and does not keep the row:
    # resolving a reference only ever asks what kind is at the other end, and
    # a million retained block payloads is 2.6 GB for an answer that is one
    # word (T-054).
    by_path = {}
    block_ids = set()      # every block row id, whether or not its row is kept
    block_keys = set()     # every blockId VALUE, folded as the rows stream
    block_types = set()    # and every `type`, which is a bounded set
    block_dupes, block_id_faults = [], []
    rows, got = {}, {}
    sample = int(os.environ.get("NOTION_BLOCK_SAMPLE") or 20000)
    for name, kind in KINDS.items():
        if name == "block":
            # THE ONE KIND THAT DOES NOT FIT IN MEMORY, streamed in BOTH modes
            # so the fixture run exercises the path the seed depends on: every
            # id lands in `by_path`, so every reference still resolves against
            # the WHOLE corpus, and `sample` rows are kept for the per-row
            # checks. The cut's 577 blocks are all kept; the owner's million
            # are not.
            def seen(r):
                # FOLDED OVER EVERY ROW, not over the sample: identity is the
                # one block check that must cover the whole corpus, and it
                # costs a set of ids rather than a million payloads.
                block_ids.add(r["id"])
                by_path[KINDS["block"] + "/" + r["id"]] = "block"
                block_types.add(props(r).get("type") or "")
                bid = props(r).get("blockId")
                if not (isinstance(bid, str) and len(bid) == 32
                        and "-" not in bid):
                    block_id_faults.append(r["id"])
                elif bid in block_keys:
                    block_dupes.append(bid)
                else:
                    block_keys.add(bid)

            kept, total = scan_records(api, kind, on_row=seen, keep=sample)
            rows[name], got[name] = kept, total
            continue
        rows[name] = all_records(api, kind)
        got[name] = len(rows[name])
        for r in rows[name]:
            by_path[kind + "/" + r["id"]] = name
    if got["block"] > len(rows["block"]):
        print("    --    block: %d rows, the per-row checks below read a "
              "SAMPLE of %d (NOTION_BLOCK_SAMPLE); every id is still in the "
              "reference map" % (got["block"], len(rows["block"])))

    owner_ref = ref(acct.get("user"))
    owner_row = next((r for r in rows["user"]
                      if KINDS["user"] + "/" + r["id"] == owner_ref), None)
    ok(by_path.get(owner_ref) == "user",
       "account.user resolves to a notion/user row")
    if owner_row and want["me"]:
        ok(props(owner_row).get("userId") == want["me"],
           "and it is the integration user GET /v1/users/me named")
        ok(props(owner_row).get("type") == "bot",
           "which is a bot, as an internal integration always is")

    print("--> the rows the recordings describe")
    if mode == "e2e":
        for name, expect in (("user", len(want["users"])),
                             ("page", len(want["pages"])),
                             ("datasource", len(want["datasources"])),
                             ("database", len(want["databases"])),
                             ("block", len(want["blocks"]))):
            ok(got[name] == expect,
               "%s: %d rows, one per payload (want %d)" % (name, got[name], expect))
        ok(got["pagesync"] == len(want["pages"]),
           "pagesync: %d state rows, one per page the block walk visited "
           "(want %d)" % (got["pagesync"], len(want["pages"])))
    else:
        # SEED MODE has no oracle — the corpus is whatever the owner's Notion
        # holds. What it CAN say is why a zero is a zero: the first owner seed
        # landed 9 users and 0 pages with the status `ok`, and the cause was
        # the backfill floor rather than a missing recording (T-045).
        for name in ("user", "page"):
            ok(got[name] > 0, "%s: %d rows%s" % (
                name, got[name],
                "" if got[name] else
                "  <-- zero. Check the backfill window against the age of the "
                "pull: `python3 tools/rawpull/notion.py --coverage "
                "--coverage-days <N>` replays the walk offline and says "
                "whether the floor or a missing recording is the cause"))
        for name in ("datasource", "database", "block", "pagesync"):
            print("    --    %s: %d rows" % (name, got[name]))

    print("--> ids are Notion's, normalised, and unique")
    # `block` is not in this loop: its rows are a SAMPLE and identity is the
    # one check that may not be sampled, so it was folded over every row as
    # the kind streamed past.
    ok(not block_dupes,
       "block: every row carries a distinct blockId (%d repeat(s)%s)"
       % (len(block_dupes),
          (": " + ", ".join(sorted(set(block_dupes))[:3])) if block_dupes else ""))
    ok(not block_id_faults,
       "block: every blockId is the normalised 32-hex form, so the dashed and "
       "undashed spellings of one UUID are ONE row (%d off%s)"
       % (len(block_id_faults),
          (": " + ", ".join(block_id_faults[:3])) if block_id_faults else ""))
    if mode == "e2e" and want["blocks"]:
        ok(block_keys == set(want["blocks"]),
           "block: the ids are the recordings' own")
    for name, idprop, source in (("page", "pageId", want["pages"]),
                                 ("datasource", "dataSourceId", want["datasources"]),
                                 ("database", "databaseId", want["databases"]),
                                 ("user", "userId", want["users"])):
        ids = [props(r).get(idprop) for r in rows[name]]
        ok(len(set(ids)) == len(ids) and None not in ids,
           "%s: every row carries a distinct %s" % (name, idprop))
        ok(all(isinstance(i, str) and len(i) == 32 and "-" not in i
               for i in ids if i),
           "%s: every %s is the normalised 32-hex form, so the dashed and "
           "undashed spellings of one UUID are ONE row" % (name, idprop))
        if mode == "e2e" and source:
            ok(set(ids) == set(source),
               "%s: the ids are the recordings' own" % name)

    print("--> nothing the guide forbids")
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
    # `person` is Notion's own object on `user` and nothing else's.
    elsewhere = {name for name in MIRRORS if name != "user"
                 for r in rows[name] if "person" in props(r)}
    ok(not elsewhere - synthesised,
       "no mirror but `user` carries a `person` at all (%s)" % sorted(elsewhere))
    people = [props(r).get("person") for r in rows["user"]
              if props(r).get("person") is not None]
    ok(all(isinstance(v, dict) and "ref" not in v for v in people),
       "%d user rows carry Notion's OWN `person` object, not a consumer slot "
       "(the mapping's would be a reference)" % len(people))
    ok(any((v or {}).get("email") for v in people),
       "and at least one carries the address a person mapping probes")
    for name in MIRRORS:
        carried = set()
        for r in rows[name]:
            carried |= set(props(r))
        leaks = {k for k in carried
                 if k == "raw" or k.endswith("Login") or k.endswith("FullName")}
        ok(not leaks, "%s: no scalar copy of a referent (%s)" % (name, sorted(leaks)))

    print("--> every relation is a reference that resolves")

    def resolves(value, kind_name):
        """A reference the row CARRIES must name a row of its pinned kind.

        This used to return True for an empty value, so a property present and
        empty passed silently — the check was only ever run on properties the
        row carries, which made the exemption pure noise (Codex 20).
        """
        path = ref(value)
        if not path:
            return False
        return by_path.get(path) == kind_name

    bad = []
    for name in ("page", "datasource", "database", "block"):
        for r in rows[name]:
            p = props(r)
            for prop in ("createdBy", "lastEditedBy"):
                if prop in p and not resolves(p[prop], "user"):
                    bad.append("%s.%s" % (r["id"], prop))
            for obj_name in ("parent", "databaseParent"):
                obj = p.get(obj_name) or {}
                for field, kind_name in (("pageId", "page"),
                                         ("dataSourceId", "datasource"),
                                         ("databaseId", "database"),
                                         ("blockId", "block")):
                    if field in obj and not resolves(obj[field], kind_name):
                        bad.append("%s.%s.%s" % (r["id"], obj_name, field))
    for r in rows["database"]:
        for value in props(r).get("dataSources") or []:
            if not resolves(value, "datasource"):
                bad.append("%s.dataSources" % r["id"])
    for r in rows["pagesync"]:
        if not resolves(props(r).get("page"), "page"):
            bad.append("%s.page" % r["id"])
    for r in rows["user"]:
        owner = (props(r).get("bot") or {}).get("owner") or {}
        if "user" in owner and not resolves(owner["user"], "user"):
            bad.append("%s.bot.owner.user" % r["id"])
    ok(not bad, "every reference names a row of its pinned kind, including "
                "the ones INSIDE `parent` (%d dangling)" % len(bad))
    accountless = [r["id"] for name in MIRRORS for r in rows[name]
                   if not ref(props(r).get("account"))]
    ok(not accountless,
       "every mirror row names its account, which is the one property the "
       "guide ADDS to an API object (%d without)" % len(accountless))

    inside = sum(1 for r in rows["page"] + rows["block"]
                 for f in ("pageId", "dataSourceId", "databaseId", "blockId")
                 if (props(r).get("parent") or {}).get(f))
    ok(inside > 0,
       "%d parent references live INSIDE the discriminated object, which is "
       "what §2 asks of a value object holding an id we mirror" % inside)

    print("--> the cases the cut was built for")
    # `parent.type` is an ENUM, so the stored value is the loader's spelling
    # (`data_source_id` -> `datasourceid`) and NOT the API's word. Counting
    # the API's spelling here reported zero of everything while the rows were
    # perfectly right, which is what an enum landing looks like from the
    # outside.
    parents = {}
    for r in rows["page"]:
        key = (props(r).get("parent") or {}).get("type") or "?"
        parents[key] = parents.get(key, 0) + 1
    ok(set(parents) <= {"workspace", "pageid", "databaseid", "datasourceid",
                        "blockid", "?"},
       "every parent.type is a spelled enum value: %s" % sorted(parents))
    ok(parents.get("datasourceid", 0) > 0,
       "%d pages are ROWS of a data source, and the discriminator is the "
       "payload's own `parent.type` — there is no isRow scalar"
       % parents.get("datasourceid", 0))
    ok(parents.get("pageid", 0) > 0,
       "%d pages hang off another page" % parents.get("pageid", 0))
    if mode == "e2e":
        ok(parents.get("workspace", 0) > 0,
           "%d pages sit at the top of the workspace" % parents.get("workspace", 0))
        ok(bool(rows["datasource"]) and all(props(r).get("properties")
                                            for r in rows["datasource"]),
           "every data source carries its property SCHEMA")
        ok(bool(rows["database"]),
           "%d databases landed — and search returns none, so each one is an "
           "addressed read a data source's parent asked for" % len(rows["database"]))
        ok(any(refs(props(r).get("dataSources")) for r in rows["database"]),
           "a database names its data sources BY REFERENCE")
        ok(bool(want["paged"]),
           "the cut carries a block list that PAGES (%d of them)"
           % len(want["paged"]))
    statuses = {}
    for r in rows["pagesync"]:
        key = str(props(r).get("blocksStatus") or "?")
        statuses[key] = statuses.get(key, 0) + 1
    if mode == "e2e":
        ok(all(props(r).get("blocksStatus") == "ok" for r in rows["pagesync"]),
           "every page's block walk finished ok on its own state row")
    else:
        # A RAW PULL IS INCOMPLETE BY CONSTRUCTION (`provider-practices.md`
        # §1): the pull walks the block trees of its first `--pages` pages, so
        # a seed over every page in the workspace meets recordings that were
        # never captured. That is a BOUND with a ticket, not a defect — what
        # matters is that one missing tree never stopped another.
        ok(statuses.get("ok", 0) > 0,
           "the block walks that had a recording finished ok: %s" % statuses)
        print("    --    the rest are the raw pull's --pages bound, named in "
              "providers/notion/fixtures/README.md")
    ok(all("blocksCursor" not in props(r) for r in rows["pagesync"]),
       "and none is left holding a cursor, so every tree drained")

    print("--> the thin stub was PATCHED, not duplicated")
    # An embedded `created_by` is `{object, id}` and nothing else; the roster
    # walk is the only thing that knows a name. A row carrying BOTH is a row
    # the two writes coalesced onto rather than duplicating (Codex 20).
    stub_ids = set()
    for o in list(want["pages"].values()) + list(want["datasources"].values()):
        for key in ("created_by", "last_edited_by"):
            who = o.get(key)
            if isinstance(who, dict):
                stub_ids.add(ident(who.get("id")))
    hydrated = [r for r in rows["user"]
                if props(r).get("userId") in stub_ids and props(r).get("name")]
    ok(bool(hydrated) or not stub_ids,
       "%d of the %d users first seen as a bare {object, id} stub carry the "
       "name only the roster has" % (len(hydrated), len(stub_ids)))
    ok(len({props(r).get("userId") for r in rows["user"]}) == len(rows["user"]),
       "and no stub minted a second row beside the roster's")

    print("--> rich text is kept whole, and the decode agrees with it")
    checked = 0
    off = []
    for r in rows["page"]:
        p = props(r)
        if not isinstance(p.get("properties"), dict):
            continue
        want_title = ""
        for v in p["properties"].values():
            if isinstance(v, dict) and v.get("type") == "title":
                want_title = plain(v.get("title"))
        checked += 1
        if (p.get("titlePlainText") or "") != want_title:
            off.append(r["id"])
    ok(checked > 0 and not off,
       "%d pages carry `properties` as Notion sent it, and titlePlainText is "
       "its own title spans concatenated (%d off)" % (checked, len(off)))
    ds_off = [r["id"] for r in rows["datasource"]
              if (props(r).get("titlePlainText") or "")
              != plain(props(r).get("dataSourceTitle"))]
    ok(not ds_off, "every data source's decode agrees with its rich title")
    db_off = [r["id"] for r in rows["database"]
              if (props(r).get("titlePlainText") or "")
              != plain(props(r).get("databaseTitle"))]
    ok(not db_off, "every database's decode agrees with its rich title")

    print("--> the block union, and its one DERIVED index")
    payload_names = {
        "paragraph": "paragraph", "heading_1": "heading1",
        "heading_2": "heading2", "heading_3": "heading3", "quote": "quote",
        "child_page": "childPage", "child_database": "childDatabase",
        "to_do": "toDo", "bulleted_list_item": "bulletedListItem",
        "numbered_list_item": "numberedListItem", "table_row": "tableRow",
        "code": "code", "callout": "callout", "toggle": "toggle",
    }
    missing = []
    for r in rows["block"]:
        p = props(r)
        name = payload_names.get(p.get("type") or "")
        if name and name not in p:
            missing.append("%s (%s)" % (r["id"], p.get("type")))
    ok(not missing,
       "every block carries its payload under the property named for its own "
       "`type` (%d without)" % len(missing))
    if mode == "e2e":
        # THE TYPE SET IS FOLDED, NOT SAMPLED. "every type the recordings hold
        # is mirrored" is a claim about the whole corpus, and a sample that
        # happened to miss one `quote` would fail a mirror that is perfect —
        # the tiny-sample probe of the streaming path found exactly that. The
        # set is bounded at 34, so folding it costs nothing.
        seen_types = block_types
        ok(seen_types >= {t for t in want["blockTypes"] if t},
           "every block type the cut holds is mirrored: %s"
           % sorted(t for t in seen_types if t))
    texty = [r for r in rows["block"] if props(r).get("plainText")]
    ok(bool(texty) or (mode == "seed" and not rows["block"]),
       "%d blocks carry the DERIVED plain-text index, which is the one "
       "`embed: true` property in this bundle" % len(texty))

    print("--> enums, instants")
    types = {props(r).get("type") for r in rows["user"]}
    ok(types <= {"person", "bot", None},
       "every user.type is a spelled enum value: %s" % sorted(t for t in types if t))
    objects = set()
    for name, expect in (("user", "user"), ("page", "page"),
                         ("database", "database"), ("block", "block")):
        objects |= {(name, props(r).get("object")) for r in rows[name]}
    ok(all(v in (k, None) for k, v in objects),
       "every `object` discriminator is the one-value enum of its kind: %s"
       % sorted("%s=%s" % kv for kv in objects))
    ok(all(props(r).get("object") == "datasource" for r in rows["datasource"])
       or not rows["datasource"],
       "a data source's `object` is spelled `datasource` by the loader's rule, "
       "with `data_source` as the label")
    for name in ("page", "datasource", "database", "block"):
        ok(all(props(r).get("at") for r in rows[name]),
           "%s: every row sits on the timeline" % name)
    if mode == "e2e":
        off = []
        for r in rows["page"]:
            payload = want["pages"].get(props(r).get("pageId"))
            if payload and instant(props(r).get("at")) != \
                    instant(payload["created_time"]):
                off.append(r["id"])
        ok(not off, "every page sits at the recording's own created_time "
                    "(%d off)" % len(off))
        off = []
        for r in rows["block"]:
            payload = want["blocks"].get(props(r).get("blockId"))
            if payload and instant(props(r).get("at")) != \
                    instant(payload["created_time"]):
                off.append(r["id"])
        ok(not off, "every block sits at the recording's own created_time "
                    "(%d off)" % len(off))
    if mode == "e2e":
        off = []
        for name, idprop, source in (("datasource", "dataSourceId",
                                      want["datasources"]),
                                     ("database", "databaseId",
                                      want["databases"])):
            for r in rows[name]:
                payload = source.get(props(r).get(idprop))
                if payload and instant(props(r).get("at")) != \
                        instant(payload.get("created_time")):
                    off.append(r["id"])
        ok(not off, "every data source and database sits at the recording's "
                    "own created_time (%d off)" % len(off))
    ok(all((props(r).get("at") or "") <= (props(r).get("lastEditedTime") or "9")
           for r in rows["page"]),
       "the temporal point is created_time, never the later last_edited_time")

    print("--> the sync asked for what the bundle says it asks for")
    mock = os.environ.get("MOCK_URL", "")
    if mock:
        # The mock is not the substrate: it takes no bearer and answers
        # `{"requests": [...]}`, which one earlier cut read as a bare list and
        # so asserted nothing at all while printing four `ok`s.
        import urllib.request
        with urllib.request.urlopen(mock + "/__mock/requests", timeout=20) as r:
            log = json.loads(r.read())
        log = log if isinstance(log, list) else (log.get("requests") or [])
        asked = [r for r in log if isinstance(r, dict)]
        paths = [str(r.get("path") or "") for r in asked]
        if not asked and mode == "seed":
            # Scenario-only again: this mock came up with the run and nothing
            # has driven a sync through it YET — the second pass below is the
            # first thing that will. Asserting over it here is asserting over
            # an empty list.
            print("    --    this run's mock has served nothing yet; what the "
                  "SEED asked for is only in the seed run's own log")
        else:
            ok(bool(asked), "the mock served %d requests" % len(asked))
            ok(any(p.endswith("/v1/users/me") for p in paths),
               "it read GET /v1/users/me")
        # SEARCH RAN, OR ITS CURSOR WAS ALREADY AT THE END. A cold run walks
        # the search; a run that RESUMES does not — the walk's cursor is spent
        # and popped, and a round that starts with a backlog skips the search
        # stage outright so a bounded drain does not re-derive the queue it is
        # already holding. The owner's seed asked for no search at all and was
        # marked wrong for it (T-054): correct incremental behaviour asserted
        # as though every run were cold.
        searched = any(p.endswith("/v1/search") for p in paths)
        spent = not str((cursors or {}).get("search") or "")
        ok(searched or (spent and got["page"] > 0),
           "search ran, or its stored cursor was already at the end: "
           "searched=%s, stored search cursor=%s, %d pages already mirrored"
           % (searched, (cursors or {}).get("search") or "none (spent)",
              got["page"]))
        ok(not any("/v1/pages/" in p for p in paths),
           "and it NEVER read GET /v1/pages/{id}: search is authoritative, "
           "which is 17,000 calls this sync does not make")
        ok(not any("/v1/data_sources/" in p for p in paths),
           "nor GET /v1/data_sources/{id}, for the same reason")
        misses = [r for r in asked if r.get("status") == 404]
        ok(not misses, "no recording was missed (%d misses)" % len(misses))
        # A RELAXED OR FALLBACK MATCH IS NEVER TOLERATED (§1): it means a
        # recording existed and the sync asked for the wrong page. Only a 404
        # was checked before, which let every relaxation through (Codex 21).
        relaxed = sorted({str(r.get("match")) for r in asked
                          if str(r.get("match") or "exact") != "exact"})
        ok(not relaxed,
           "every request hit its EXACT recording — no page or window was "
           "relaxed (%s)" % (relaxed or "none"))

    # ---------------------------------------------------------- second pass
    print("--> a second sync is idempotent")
    blocks_before, walked_before = 0, set()
    if mock:
        import urllib.request as _u
        with _u.urlopen(mock + "/__mock/requests", timeout=300) as r:
            log0 = json.loads(r.read())
        log0 = log0 if isinstance(log0, list) else (log0.get("requests") or [])
        before_count = len(log0)
        blocks_before = len([x for x in log0
                             if "/v1/blocks/" in str(x.get("path") or "")])
        walked_before = children_parents(log0)
    before = {name: {r["id"] for r in rows[name]} for name in MIRRORS}
    before["block"] = set(block_ids)       # all of them, not just the sample
    before_props = {r["id"]: props(r) for name in MIRRORS for r in rows[name]}
    # A SEED'S RE-SYNC IS NOT A SINGLE ROUND. Over the owner's workspace the
    # second sync discovers thousands of block trees the first pass had not
    # reached (~4,000 on the run that filed T-054) and ends its round holding
    # them — `ok (deferred: blocks x3962)`, which is a bounded drain doing
    # exactly what it says. So the backlog is driven on the SEED'S OWN
    # continuation rule and the assertion is that it drains, or failing that
    # that it is strictly shrinking and the remainder is named.
    rounds, settle = continuation_rule()
    acct2 = force_sync(api, akind, aid, wait_pending=(mode != "seed"))
    if mode == "seed" and pending_size(acct2):
        sizes = drain_backlog(api, akind, aid, rounds, settle)
        moved = " -> ".join(str(v) for v in sizes[:6]) + \
            (" -> …" if len(sizes) > 6 else "")
        if not sizes[-1]:
            ok(True, "the re-sync's late-discovered backlog drained in %d "
                     "continuation round(s): %s" % (len(sizes) - 1, moved))
        else:
            ok(len(sizes) > 1 and all(b < a for a, b in zip(sizes, sizes[1:])),
               "the re-sync's backlog is strictly shrinking round on round "
               "(%s); %d item(s) left after %d round(s) — the drain is "
               "bounded, not stuck" % (moved, sizes[-1], len(sizes) - 1))
        acct2 = account(api, akind, aid)
    ok(str(acct2.get("syncStatus", "")).startswith("ok"),
       "the second sync finished ok (%s)" % acct2.get("syncStatus"))
    ok(acct2.get("syncRequestedAck")
       and acct2.get("syncRequestedAck") == acct2.get("syncRequestedAt"),
       "and it acknowledged the request it was asked to serve")
    # ONE PROPERTY, ONE MEANING. `syncRequestedAck` is both the on-request
    # trigger's guard and the core `sync` trait's acknowledgement, so the
    # console's "Sync now" and the drain's continuation read the same stamp
    # (decision 0085, T-084).
    ok(acct2.get("syncState") == "ok",
       "and the trait agrees the second run finished (%s)"
       % acct2.get("syncState"))
    after_rows, after = {}, {}
    for name in MIRRORS:
        if name == "block":
            # Streamed again, and for the same reason: the ids are what the
            # lost/grew comparison needs, and only the rows the first pass
            # SAMPLED can be compared property by property anyway.
            seen_ids, kept = set(), []

            def take(r, seen_ids=seen_ids, kept=kept):
                seen_ids.add(r["id"])
                if r["id"] in before_props:
                    kept.append(r)

            scan_records(api, KINDS[name], on_row=take)
            after_rows[name], after[name] = kept, seen_ids
            continue
        after_rows[name] = all_records(api, KINDS[name])
        after[name] = {r["id"] for r in after_rows[name]}
    # NOTHING IS EVER LOST. In e2e mode nothing is gained either — the
    # recordings are a closed set and a second sync over them must mint
    # nothing. A SEED's second sync legitimately GROWS: the late-discovered
    # trees the continuation just drained are blocks that were never walked
    # before, and calling that a duplicate row would be calling the drain's
    # own progress a fault (T-054).
    lost = {name: sorted(before[name] - after[name])[:3] for name in MIRRORS
            if before[name] - after[name]}
    grew = {name: len(after[name] - before[name]) for name in MIRRORS
            if after[name] - before[name]}
    if mode == "e2e":
        ok(after == before,
           "every row is the one the first sync minted: %s"
           % {k: len(v) for k, v in after.items()})
    else:
        ok(not lost,
           "no row the first pass minted was lost, and the %s the second pass "
           "added are the trees the continuation reached for the first time "
           "(%s)" % (grew or "0 rows", {k: len(v) for k, v in after.items()}))
    if lost:
        print("    --    lost: %s" % lost)
    # THE WHOLE MAP, both ways. Comparing only the keys the first run wrote
    # let a destructive clear and a newly invented property both pass
    # (Codex 21); `provider-practices.md` §8 asks for content equality.
    drifted = []
    for name in MIRRORS:
        for r in after_rows[name]:
            was = before_props.get(r["id"])
            if was is None:
                continue
            now = props(r)
            for k in set(was) | set(now):
                if k not in was:
                    drifted.append("%s.+%s" % (r["id"], k))
                elif k not in now:
                    drifted.append("%s.-%s" % (r["id"], k))
                elif was[k] != now[k]:
                    drifted.append("%s.%s" % (r["id"], k))
    ok(not drifted,
       "no property was added, removed or changed across two identical syncs "
       "(%d drifted: %s)" % (len(drifted), drifted[:6]))
    if mock:
        import urllib.request as _u2
        with _u2.urlopen(mock + "/__mock/requests", timeout=300) as r:
            log1 = json.loads(r.read())
        log1 = log1 if isinstance(log1, list) else (log1.get("requests") or [])
        blocks_after = len([x for x in log1
                            if "/v1/blocks/" in str(x.get("path") or "")])
        # THE INCREMENTAL SKIP, proved rather than described. `pagesync`
        # carries the `last_edited_time` each tree was walked against, and a
        # page Notion has not touched since has nothing to re-read — so the
        # second sync must ask for NO page's children at all. The declaration
        # promised this and nothing read it until Codex finding 16.
        if mode == "e2e":
            ok(blocks_after == blocks_before,
               "the second sync re-walked NO page's block tree: %d children "
               "requests before, %d after" % (blocks_before, blocks_after))
        else:
            # A SEED'S SECOND PASS MAY WALK — but only trees it had never
            # walked. Counting requests would call the continuation's own
            # progress a re-walk, so the check is on the IDS: every tree the
            # second pass asked for must be one the first pass never
            # completed. That is the same promise, stated over a corpus that
            # is still growing (T-054).
            again = children_parents(log1[before_count:]) & walked_before
            ok(not again,
               "the second sync re-walked no tree it had already walked: "
               "%d children request(s), %d new tree(s), %d repeat(s)%s"
               % (blocks_after - blocks_before,
                  len(children_parents(log1[before_count:]) - walked_before),
                  len(again),
                  (" e.g. " + ", ".join(sorted(again)[:3])) if again else ""))

    # ------------------------------------------------ the backfill window
    # E2E ONLY. This drives four syncs and narrows the account's window on the
    # way; against the owner's workspace that is hours of drain, and a run
    # that died between two of them would leave the seed account on a 90-day
    # floor — the exact fault this proves is gone.
    if mode == "e2e":
        print("--> the backfill window, and `all` meaning NO floor (T-045)")
        newest = max([str(o.get("last_edited_time") or "")
                      for o in list(want["pages"].values())
                      + list(want["datasources"].values())] or [""])
        # NARROW IT, to a window that excludes every object the recordings
        # hold. `none` is used rather than the seed's own `last90d` because
        # its floor is the RUN'S OWN START and so is newer than any recording
        # BY CONSTRUCTION — `last90d` only excludes this cut while the cut is
        # more than 90 days old, and a re-cut would quietly disarm the check.
        # What it proves is the seed's fault either way: a floor newer than
        # the newest object stops the descending walk at item one.
        acct3 = patched_sync(api, akind, aid, {"backfillDepth": "none"})
        cur3 = acct3.get("syncCursors") if isinstance(
            acct3.get("syncCursors"), dict) else {}
        ok(cur3.get("floorDepth") == "none",
           "a changed backfillDepth reaches the walk: the floor is stored "
           "beside the depth that produced it (%s)" % cur3.get("floorDepth"))
        ok(bool(cur3.get("floor")) and str(cur3.get("floor")) > newest,
           "and the floor it computed is newer than the newest recording, so "
           "the window excludes every object (floor %s, newest %s)"
           % (cur3.get("floor"), newest))
        ok("backfill window admitted nothing" in str(acct3.get("syncStatus") or ""),
           "the status SAYS the window is what stopped the walk rather than "
           "reporting a bare `ok` over 0 rows (%s)" % acct3.get("syncStatus"))
        # ANCHORED. A relative window recomputed from a later clock on every
        # run abandons whatever was edited in between (§4, Codex 16), so an
        # UNCHANGED depth must reuse the floor it already stored.
        acct4 = force_sync(api, akind, aid)
        cur4 = acct4.get("syncCursors") if isinstance(
            acct4.get("syncCursors"), dict) else {}
        ok(cur4.get("floor") == cur3.get("floor"),
           "an unchanged depth re-walks the floor it anchored, never a fresh "
           "one off a later clock — which is what `none` (\"from here "
           "forward\", §9 open question 3) means (%s)" % cur4.get("floor"))
        # WIDEN IT BACK. This is the assertion the owner's seed needed: `all`
        # has to CLEAR the floor a narrower depth left behind. Before the fix
        # the stored floor won whatever the account said, and this account —
        # reading `backfillDepth: all` — kept walking to the 90-day floor.
        acct5 = patched_sync(api, akind, aid, {"backfillDepth": "all"})
        cur5 = acct5.get("syncCursors") if isinstance(
            acct5.get("syncCursors"), dict) else {}
        ok(not (cur5.get("floor") or ""),
           "`all` CLEARS the floor a narrower depth stored — it means no "
           "bound, not the last bound (%s)" % (cur5.get("floor") or "none"))
        ok("backfill window admitted nothing" not in str(acct5.get("syncStatus") or ""),
           "and the walk admits the whole cut again (%s)"
           % acct5.get("syncStatus"))
        final_rows = {name: {r["id"] for r in all_records(api, KINDS[name])}
                      for name in MIRRORS}
        ok(final_rows == before,
           "every row survived the round trip: %s"
           % {k: len(v) for k, v in final_rows.items()})

    # ------------------------------------------- the trait under failure
    # E2E ONLY, and LAST: it deliberately breaks a sync, and everything after
    # it would read a broken account. A failure is never injected into a seed
    # — the owner's repository is not a test bench.
    if mode == "e2e" and mock:
        print("--> the trait says `erroring` when a run fails (T-084)")
        before_state = acct2.get("syncState")
        mock_call(mock, "/__mock/faults", "POST", {"rules": [
            # ON `users/me`, which is the FIRST call of EVERY run whatever
            # stage the drain resumes in — a rule on a later endpoint would
            # depend on which stage this account happens to walk next. A 500
            # is TRANSIENT: `_call` raises it as an HTTPError, the body's own
            # failure path stamps the account, and the delivery still settles,
            # so the trait's `erroring` is the body's word and not a park's.
            {"match": "GET /v1/users/me", "status": [500] * 6,
             "body": {"object": "error", "status": 500,
                      "code": "internal_server_error",
                      "message": "the mock is standing in for an outage"}}]})
        bad = force_sync(api, akind, aid)
        ok(bad.get("syncState") == "erroring",
           "after an injected 500 the state is `erroring` (%s, was %s)"
           % (bad.get("syncState"), before_state))
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
        ok(bad.get("syncRequestedAck") == bad.get("syncRequestedAt"),
           "a FAILED run still acknowledges the request it served — an "
           "unacknowledged one would re-fire the trigger against the same "
           "failure forever (%s)" % bad.get("syncRequestedAck"))
        # AND IT RECOVERS. An account that cannot come back from a transient
        # failure is the same bug as one that never reported it.
        mock_call(mock, "/__mock/faults", "DELETE")
        good = force_sync(api, akind, aid)
        ok(good.get("syncState") == "ok",
           "with the rules cleared the next run raises the state back to `ok` "
           "(%s)" % good.get("syncState"))
        ok(good.get("syncError") == bad.get("syncError"),
           "while syncError stays as the RECORD of the last failure — the "
           "state is what says the account is healthy again, not the absence "
           "of a past error (%s)" % good.get("syncError"))
        final_rows = {name: {r["id"] for r in all_records(api, KINDS[name])}
                      for name in MIRRORS if name != "block"}
        ok(all(final_rows[name] == before[name]
               for name in final_rows),
           "and every row survived the outage: %s"
           % {k: len(v) for k, v in final_rows.items()})

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
