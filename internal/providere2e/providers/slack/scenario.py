#!/usr/bin/env python3
"""The Slack provider's end-to-end assertions.

Run by `tools/e2e.py` after it has installed the bundle, connected the account
and waited for the first sync to settle. Everything here is derived from THE
RECORDINGS the mock served, never from hard-coded ids, so the same file
asserts the e2e run (pseudonymised fixtures) and
a seed run over the owner\'s own pull (mneme tree only) (the owner's own pulls) without knowing
which it is looking at.

WHAT IT ASSERTS, and why each one is worth a run:

    1. account    the token's workspace and user arrived as REFERENCES that
                  resolve to mirrored rows — not as a teamId and a userId
    2. counts     every kind the recordings imply has its rows, and the ids
                  are the provider keys, distinct and derivable
    3. references user, channel, team, parent, mentions, reactions[].users
                  and files all resolve to rows of the kind they pin
    4. decode     `mentions` is the `<@U…>` tokens of the text, in order, as
                  references — recomputed here from the recording
    5. owner      the owner-relative facts are DATA, not flags: a message
                  whose `user` IS the account's user, and a message whose
                  `mentions` CONTAINS it, both found by reference
    6. time       every message carries `at`, and it is its ts
    7. state      the cursors live on `conversationsync`, and the
                  conversation row's own version did NOT move while a drain
                  wrote its history
    8. shape      no row carries `raw`, and no kind declares one
    9. again      a second sync adds no duplicate row, advances each cursor,
                  and lands the message only an INCREMENTAL recording carries
   10. failures   what the sync could NOT do, by class, with an example id —
                  and that a permanent failure on one conversation did not
                  stop the others
   11. mutations  an edit lands on the message it names, a delete removes its
                  target, a re-read with `reactions: []` clears
   12. requests   the windows and pages the sync actually asked for: every
                  incremental window names a stored cursor, both list calls
                  follow their cursor, and NOTHING was answered by a relaxed
                  or fallback match
   15. trait      the core `sync` trait (decision 0085): a finished walk is
                  `ok` with every stream acked against the request that drove
                  it, an injected 429 is `throttled`, an injected 401 is
                  `erroring` with the cause, and the next run recovers
   16. backlog    with a `replies` backlog bigger than a run's budget, a new
                  message in a mirrored channel lands within that one run
   17. write      `postmessage` posts through the config's token: the form it
                  sends, its output, no record written, Slack's refusal
                  surfaced, and an apiBase off slack.com refused unsent

Run against `raw/slack` (MODE=seed) two families of check relax, and only
two; both are commented "SEED:" at the site and described in
providers/slack/README.md under "The seed run, and what it may relax".
"""

from __future__ import annotations

import json
import os
import pathlib
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

SERVER = os.environ["SUBSTRATE_SERVER"].rstrip("/")
TOKEN = os.environ["SUBSTRATE_TOKEN"]
MODE = os.environ.get("MODE", "e2e")
MOCK = os.environ.get("MOCK_URL", "").rstrip("/")
ACCOUNT_KIND = os.environ.get("ACCOUNT_KIND",
                              "providers.substrate.reamde.dev/slack/account")
ACCOUNT_ID = os.environ.get("ACCOUNT_ID", "e2e")
RECORDINGS = pathlib.Path(os.environ["RECORDINGS"])
ROOT = pathlib.Path(os.environ.get("SUBSTRATE_E2E_ROOT")
                    or pathlib.Path(__file__).resolve().parents[2])

P = "providers.substrate.reamde.dev/slack"
TEAM, USER, BOT = P + "/team", P + "/user", P + "/bot"
CONV, MESSAGE, FILE = P + "/conversation", P + "/message", P + "/file"
SYNC = P + "/conversationsync"

MENTION_RE = re.compile(r"<@([UWB][A-Z0-9]+)(?:\|[^>]*)?>")
CHANNEL_RE = re.compile(r"<#([CDG][A-Z0-9]+)(?:\|[^>]*)?>")
# The scenario shared the sync's twenty-subtype denylist, so passing proved
# the two agreed on a reduced cut rather than that the cut was right. Only the
# MUTATION ENVELOPES are not messages — they carry no identity of their own
# and are applied to the message they speak about, which the scenario checks
# separately.
MUTATION_SUBTYPES = {"message_changed", "message_deleted", "message_replied"}

# the e2e run runs MODE=e2e against the pseudonymised cut;
# a seed run over the owner\'s own pull (mneme tree only) runs MODE=seed against raw/slack, which
# is the owner's own pull and is NOT a curated set. Two things differ, and
# only two:
#
#   * a raw pull is INCOMPLETE BY CONSTRUCTION. It holds what the puller
#     reached, so the mirror is compared against the conversations the sync
#     actually FINISHED rather than against every message the pull happens to
#     hold, and a call with no recording at all is the pull's gap rather than
#     the sync's bug.
#   * a raw pull carries no FABRICATED seams. The mutation envelopes, the
#     terminal empty page and the split roster page are cut by
#     fixtures/curate.py and cannot exist in a recording of the live API, so
#     the checks that need them assert only when they are present — and are
#     REQUIRED in e2e, so the coverage cannot quietly disappear.
#
# Nothing else is relaxed. Every relaxation below says so in a comment
# beginning "SEED:".
SEED = MODE != "e2e"

problems: list[str] = []
checks = 0
_section = "0. startup"


def ok(cond, msg):
    global checks
    checks += 1
    if not cond:
        problems.append((_section, msg))
    return bool(cond)


def seed_note(msg):
    """A check the seed run deliberately does not make. Printed either way, so
    the relaxation is visible in the log rather than implied by silence."""
    note(("SEED RELAXATION: " if SEED else "(e2e asserts this) ") + msg)


def section(name):
    global _section
    _section = name
    print("\n\033[1m--- %s\033[0m" % name, flush=True)


def note(msg):
    print("    %s" % msg, flush=True)


# ------------------------------------------------------------------ the API


def api(method, path, body=None):
    url = path if path.startswith("http") else SERVER + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={
        "Authorization": "Bearer " + TOKEN,
        "Content-Type": "application/json",
        "Accept": "application/json",
    })
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            raw = r.read()
            return r.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, {"raw": raw[:400].decode("utf-8", "replace")}


def records(kind, limit=5000):
    """Every row of a kind, paged."""
    out, after = [], ""
    while True:
        q = {"filter": json.dumps({"kinds": [kind]}), "first": "500"}
        if after:
            q["after"] = after
        st, body = api("GET", "/api/v1/records?" + urllib.parse.urlencode(q))
        if st >= 400:
            problems.append("records %s: HTTP %s %s"
                            % (kind, st, json.dumps(body)[:200]))
            return out
        items = (body or {}).get("records") or []
        out += items
        after = (body or {}).get("cursor") or ""
        if not items or not after or len(out) >= limit:
            return out


def props(row):
    return row.get("properties") or {}


def rid(row):
    return row.get("id") or (row.get("metadata") or {}).get("id")


def version(row):
    for src in (row, row.get("metadata") or {}):
        for key in ("version", "recordVersion", "seq"):
            if src.get(key) is not None:
                return src[key]
    return None


def writes_since(kind, record_id, after_version):
    """The property names each changelog write to one record touched after
    `after_version`, one list per write, or a string when the changelog could
    not be read. The engine lists only the properties a write changed."""
    req = urllib.request.Request(
        SERVER + "/api/v1/changes?" + urllib.parse.urlencode(
            {"recordId": record_id, "recordKind": kind}),
        headers={"Authorization": "Bearer " + TOKEN})
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            raw = r.read()
    except (urllib.error.URLError, OSError) as e:
        return "unreadable (%s)" % e
    out = []
    entries = []
    for line in raw.decode("utf-8", "replace").splitlines():
        try:
            doc = json.loads(line)
        except ValueError:
            continue
        # NDJSON is one change per line; the JSON form wraps a page of them.
        entries.extend(doc.get("changes") or [] if "changes" in doc else [doc])
    for entry in entries:
        mine = [a for a in entry.get("affected") or [] if a.get("id") == record_id]
        if mine and (mine[0].get("version") or 0) > (after_version or 0):
            out.append((entry.get("payload") or {}).get("properties"))
    return out


def ref_id(value):
    """A reference is `{"ref": "<kind>/<id>"}` on the wire when it carries
    link properties and a bare path otherwise; the id is its last segment."""
    if isinstance(value, dict) and value.get("ref"):
        return str(value["ref"]).rsplit("/", 1)[-1]
    if isinstance(value, str) and value:
        return value.rsplit("/", 1)[-1]
    return None


def ref_ids(value):
    if value is None:
        return []
    if not isinstance(value, list):
        value = [value]
    return [r for r in (ref_id(v) for v in value) if r]


# ------------------------------------------------------- the recordings, read


def load(name):
    f = RECORDINGS / name
    if not f.exists():
        return None
    doc = json.loads(f.read_text())
    if isinstance(doc, dict) and "__responses" in doc:
        doc = doc["__responses"][0]
    if isinstance(doc, dict) and "__response" in doc:
        doc = doc.get("body") or {}
    return doc


def each(pattern):
    for f in sorted(RECORDINGS.glob(pattern)):
        doc = load(f.name)
        if isinstance(doc, dict) and doc.get("ok"):
            yield f.name, doc


def keep(msg):
    if msg.get("type") not in (None, "message") or not msg.get("ts"):
        return False
    return (msg.get("subtype") or "") not in MUTATION_SUBTYPES


def _named_users(obj):
    """Every user id one payload names, at any depth this mirror follows."""
    out = set()
    if not isinstance(obj, dict):
        return out
    for key in ("user", "parent_user_id", "inviter", "creator",
                "guest_invited_by"):
        if isinstance(obj.get(key), str):
            out.add(obj[key])
    for holder in ("edited", "pinned_info", "bot_profile", "profile"):
        h = obj.get(holder)
        if isinstance(h, dict):
            for key in ("user", "pinned_by", "user_id", "guest_invited_by"):
                if isinstance(h.get(key), str):
                    out.add(h[key])
    for f in obj.get("files") or []:
        if isinstance(f, dict) and isinstance(f.get("user"), str):
            out.add(f["user"])
    for r in obj.get("reactions") or []:
        if isinstance(r, dict):
            out |= {u for u in (r.get("users") or []) if isinstance(u, str)}
    out |= {u for u in (obj.get("reply_users") or []) if isinstance(u, str)}
    out |= set(MENTION_RE.findall(obj.get("text") or ""))
    for nested in ("root", "latest", "message"):
        out |= _named_users(obj.get(nested))
    return {u for u in out if u and u[:1] in ("U", "W")}


def expected_from_recordings():
    """What the recordings say the mirror must hold."""
    auth = load("GET_api_auth.test.json") or {}
    team = auth.get("team_id") or ""
    owner = auth.get("user_id") or ""

    users, listed = {}, set()
    for _, doc in each("GET_api_users.list__*"):
        for u in doc.get("members") or []:
            if u.get("id"):
                users[u["id"]] = u
                listed.add(u["id"])
    for _, doc in each("GET_api_users.info__*"):
        u = doc.get("user") or {}
        if u.get("id"):
            users.setdefault(u["id"], u)

    convs = {}
    for _, doc in each("GET_api_conversations.list__*"):
        for c in doc.get("channels") or []:
            if c.get("id"):
                convs[c["id"]] = c
    for _, doc in each("GET_api_conversations.info__*"):
        c = doc.get("channel") or {}
        if c.get("id"):
            convs.setdefault(c["id"], c)

    # Every history and replies page, grouped by the channel in its NAME —
    # the only place the channel survives, since a recording is a response
    # and a response does not name its request.
    messages, incremental, files, bots = {}, {}, {}, set()
    # The mutation envelopes, kept apart: an edit names the message it
    # changed, a delete names the message it removed, and neither is a row.
    changed, deleted = {}, {}
    for name, doc in each("GET_api_conversations.history__*"):
        m = re.search(r"channel-([A-Z0-9]+)", name)
        if not m:
            continue
        cid = m.group(1)
        bucket = incremental if "oldest-" in name else messages
        for msg in doc.get("messages") or []:
            sub_ = msg.get("subtype") or ""
            if sub_ == "message_deleted":
                gone = msg.get("deleted_ts") or (
                    msg.get("previous_message") or {}).get("ts")
                if gone:
                    deleted["%s/%s" % (cid, gone)] = msg
                continue
            if sub_ in ("message_changed", "message_replied"):
                inner = msg.get("message") or {}
                if inner.get("ts"):
                    changed["%s/%s" % (cid, inner["ts"])] = inner
                continue
            if keep(msg):
                bucket.setdefault(cid, {})[msg["ts"]] = msg
    for name, doc in each("GET_api_conversations.replies__*"):
        m = re.search(r"channel-([A-Z0-9]+)", name)
        if not m:
            continue
        for msg in doc.get("messages") or []:
            if keep(msg):
                messages.setdefault(m.group(1), {})[msg["ts"]] = msg
    # `latest` is a WHOLE MESSAGE embedded in the conversation payload, and
    # it can name a message no history page in the cut reaches. The mirror
    # ingests it, so the expectation has to hold it too.
    for cid, c in convs.items():
        latest = c.get("latest")
        if isinstance(latest, dict) and keep(latest):
            messages.setdefault(cid, {}).setdefault(latest["ts"], latest)
    # so can an embedded `root`, on a broadcast reply.
    for cid, by_ts in list(messages.items()):
        for msg in list(by_ts.values()):
            root = msg.get("root")
            if isinstance(root, dict) and root.get("ts") and keep(root):
                by_ts.setdefault(root["ts"], root)
    # Which MESSAGE each file and bot was seen on, so the expectation can be
    # restricted to the messages the sync was actually supposed to mirror —
    # a file shared below the backfill floor is not a missing file.
    file_convs, bot_convs = {}, {}
    named_users: set[str] = set()
    for bucket in (messages, incremental):
        for cid, by_ts in bucket.items():
            for msg in by_ts.values():
                key = "%s/%s" % (cid, msg.get("ts"))
                for f in msg.get("files") or []:
                    if isinstance(f, dict) and f.get("id"):
                        files[f["id"]] = f
                        file_convs.setdefault(f["id"], set()).add(key)
                # `x_files` names files by ID rather than embedding them, and
                # the mirror mints a thin row so the reference resolves. They
                # are files the recordings DO name, so they belong in the
                # expectation — without this the sync looked like it had
                # invented a file row.
                for fid in msg.get("x_files") or []:
                    if isinstance(fid, str) and fid:
                        files.setdefault(fid, {"id": fid})
                        file_convs.setdefault(fid, set()).add(key)
                if msg.get("bot_id"):
                    bots.add(msg["bot_id"])
                    bot_convs.setdefault(msg["bot_id"], set()).add(key)
                named_users |= _named_users(msg)
    # Every user id ANY recording names, anywhere — the set the mirror is
    # allowed to hold a thin row for.
    for c in convs.values():
        named_users |= _named_users(c)
        for key in ("topic", "purpose"):
            if isinstance(c.get(key), dict) and c[key].get("creator"):
                named_users.add(c[key]["creator"])
    for name, doc in each("GET_api_conversations.members__*"):
        named_users |= {m for m in (doc.get("members") or [])
                        if isinstance(m, str)}
    # T-021. `files.info` names a file's EDITORS, its canvas creator and the
    # people a DM grant reaches, and `bots.info` names the user a bot posts
    # as; each is a reference, so the mirror mints a thin row for it. They
    # are ids the recordings DO name — without this the sync looks like it
    # invented them, exactly as `x_files` did.
    share_convs = set()
    for _n, doc in each("GET_api_files.info__*"):
        f = doc.get("file") or {}
        named_users |= {u for u in (f.get("editors") or [])
                        if isinstance(u, str)}
        if isinstance(f.get("canvas_creator_id"), str):
            named_users.add(f["canvas_creator_id"])
        for g in f.get("dm_mpdm_users_with_file_access") or []:
            if isinstance(g, dict) and isinstance(g.get("user_id"), str):
                named_users.add(g["user_id"])
        for key in ("channels", "groups", "ims"):
            share_convs |= {c for c in (f.get(key) or []) if isinstance(c, str)}
    for _n, doc in each("GET_api_bots.info__*"):
        b = doc.get("bot") or {}
        if isinstance(b.get("user_id"), str):
            named_users.add(b["user_id"])
    named_users = {u for u in named_users if u and u[:1] in ("U", "W")}
    return {"team": team, "owner": owner, "users": users, "convs": convs,
            "share_convs": share_convs,
            "listed_users": listed, "named_users": named_users,
            "messages": messages, "incremental": incremental,
            "changed": changed, "deleted": deleted,
            "files": files, "bots": bots,
            "file_convs": file_convs, "bot_convs": bot_convs}


# ------------------------------------------------------------------- the run


def sync_now():
    """Fire the sync again the way the bundle says to: stamp the account's
    `syncRequestedAt`, which the on-demand trigger watches.

    NOT a function call. Call mode runs ONE invocation and does not drain a
    paged body, so a paged sync fired that way stops after its first page."""
    import datetime as dt
    # MICROSECONDS, deliberately. The trigger fires on `syncRequestedAt >
    # lastSyncedAt` and the sync stamps `lastSyncedAt` to the whole second;
    # a request stamped in the same second is NOT greater, so the guard
    # closes and the continuation deadlocks. A sub-second request is strictly
    # later than a whole-second completion in the same second.
    now = dt.datetime.now(dt.timezone.utc).isoformat()
    return api("PATCH", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID),
               {"properties": {"syncRequestedAt": now}})


def pending(p):
    """Unfinished work of EVERY class.

    This used to count four queues and call the account settled while a
    hydration queue, a half-walked history page, a half-read roster or a
    paused list cursor was outstanding — which is how a bounded drain read as
    a complete one.

    `retry` is NOT unfinished work, and this has to agree with the sync's own
    `_pending` or the two disagree about whether the drain is over. A retry
    entry is re-queued at the top of the NEXT walk; counting it here made the
    scenario fire an extra sync after a completed cold walk, and that walk —
    being incremental — consumed the hand-cut `oldest=` pages before section
    2 ever looked at the mirror."""
    cur = p.get("streamCursors") or {}
    if not isinstance(cur, dict):
        return False
    if any(cur.get(k) for k in ("queue", "info", "threads", "hydrate",
                                "files", "bots", "rosters")):
        return True
    if any(cur.get(k) for k in ("work", "reply", "roster")):
        return True
    return bool(cur.get("cursor")) or (cur.get("phase") or "done") != "done"


def wait_for_sync(previous_stamp, seconds=240):
    """Wait for a sync to settle, DRIVING it while it has work left.

    The on-demand trigger fires on `syncRequestedAt > lastSyncedAt`, and both
    are whole seconds: a drain that stamps its completion in the same second
    as the request closes its own guard, and nothing fires again. Re-stamping
    only when `lastSyncedAt` CHANGED therefore deadlocks — the owner's
    workspace stalled at 1,249 pending with the two equal to the second. So
    the driver re-stamps whenever the account still has pending work and the
    last stamp has gone quiet, whether or not it moved.
    """
    deadline = time.time() + seconds
    quiet_since, driven = time.time(), 0
    while time.time() < deadline:
        _st, body = api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))
        p = props(body or {})
        stamp = p.get("lastSyncedAt")
        if stamp and stamp != previous_stamp and not pending(p):
            return p
        if stamp != previous_stamp:
            quiet_since = time.time()
            previous_stamp = stamp
            if pending(p):
                sync_now()
                driven += 1
        elif pending(p) and time.time() - quiet_since > 20:
            # Nothing has run for twenty seconds and there is work left: the
            # guard is closed, so open it again.
            quiet_since = time.time()
            sync_now()
            driven += 1
        time.sleep(3)
    _st, body = api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))
    if driven:
        note("drove the sync %d times while it had work pending" % driven)
    return props(body or {})


def faults(rules):
    """Set (or clear) the mock's injected-failure rules between two syncs.

    A failure matrix is a property of the RUN, not of the recordings
    (provider-practices §3): the 401 and the 429 the trait's `erroring` and
    `throttled` states need are asked for here rather than hand-written as a
    second fixture set that would drift from the first."""
    if not MOCK:
        return
    if rules:
        api("POST", MOCK + "/__mock/faults", {"rules": rules})
    else:
        api("DELETE", MOCK + "/__mock/faults")


def _instant(v):
    import datetime as dt
    try:
        return dt.datetime.fromisoformat(str(v).replace("Z", "+00:00"))
    except ValueError:
        return None


def run_once(seconds=240):
    """Fire ONE run and wait for it to settle, without driving another.

    `wait_for_sync` re-fires while work is pending, which is right for
    draining and wrong for a check about what a single bounded run did. The
    run is recognised by the dispatcher's `lastSyncStartedAt` falling after
    the request, so a delivery still queued from an earlier drive is not
    mistaken for it."""
    import datetime as dt
    asked = dt.datetime.now(dt.timezone.utc)
    sync_now()
    deadline = time.time() + seconds
    p = {}
    while time.time() < deadline:
        p = props(api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))[1] or {})
        started = _instant(p.get("lastSyncStartedAt"))
        synced = _instant(p.get("lastSyncedAt"))
        if started and synced and started > asked and p.get("syncState") != "running" \
                and synced >= started.replace(microsecond=0):
            return p
        time.sleep(2)
    return p


def settle(quiet=20, seconds=240):
    """Wait until no run has stamped the account for `quiet` seconds, so a
    delivery an earlier drive left queued cannot land inside a check."""
    deadline = time.time() + seconds
    last, since = None, time.time()
    while time.time() < deadline:
        p = props(api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))[1] or {})
        mark = (p.get("lastSyncedAt"), p.get("syncState"))
        if mark != last:
            last, since = mark, time.time()
        elif p.get("syncState") != "running" and time.time() - since >= quiet:
            return p
        time.sleep(2)
    return p


def wait_for_state(want, seconds=90):
    """Wait for the account's `syncState` to reach a word.

    NOT `wait_for_sync`: a failed run leaves `lastSyncedAt` where it was (the
    walk did not complete), so waiting on the stamp to move would wait for
    ever."""
    deadline = time.time() + seconds
    p = {}
    while time.time() < deadline:
        p = props(api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))[1] or {})
        if p.get("syncState") == want:
            return p
        time.sleep(2)
    return p


def main():
    want = expected_from_recordings()
    team, owner = want["team"], want["owner"]
    note("recordings: %s" % RECORDINGS)
    note("team %s, owner %s, %d users, %d conversations"
         % (team, owner, len(want["users"]), len(want["convs"])))
    if not ok(team and owner, "auth.test recording is missing or has no team/user"):
        return finish()

    account = props(api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))[1] or {})
    if pending(account):
        note("the first drain was bounded; driving it to completion")
        sync_now()
        account = wait_for_sync(account.get("lastSyncedAt"))

    teams = records(TEAM)
    users = records(USER)
    convs = records(CONV)
    msgs = records(MESSAGE)
    files = records(FILE)
    bots = records(BOT)
    states = records(SYNC)
    by_user = {props(r).get("userId"): r for r in users}
    by_conv = {props(r).get("conversationId"): r for r in convs}
    by_id = {rid(r): props(r) for r in msgs}
    user_ids = {rid(r) for r in users}
    conv_ids = {rid(r) for r in convs}
    file_ids = {rid(r) for r in files}
    conv_of_row = {rid(r): props(r).get("conversationId") for r in convs}
    user_of_row = {rid(r): props(r).get("userId") for r in users}
    by_key = {}
    for r in msgs:
        p = props(r)
        by_key[(conv_of_row.get(ref_id(p.get("channel"))), p.get("ts"))] = rid(r)
    note("mirror: %d team, %d user, %d conversation, %d message, %d file, "
         "%d bot, %d sync state" % (len(teams), len(users), len(convs),
                                    len(msgs), len(files), len(bots), len(states)))

    # ---------------------------------------------------------------- 1
    section("1. the account names its workspace and its user by REFERENCE")
    ok(account.get("lastSyncedAt"), "the account never stamped lastSyncedAt")
    ok(str(account.get("syncStatus") or "").startswith("ok"),
       "syncStatus is %r" % account.get("syncStatus"))
    ok("teamId" not in account and "userId" not in account,
       "the account still carries a bare teamId/userId beside its references")
    team_ref = ref_id(account.get("team"))
    user_ref = ref_id(account.get("user"))
    ok(team_ref, "account.team is not a reference: %r" % account.get("team"))
    ok(user_ref, "account.user is not a reference: %r" % account.get("user"))
    ok(teams and rid(teams[0]) == team_ref,
       "account.team does not resolve to the mirrored team row")
    ok(props(teams[0] if teams else {}).get("teamId") == team,
       "the team row's teamId is not what auth.test said")
    owner_row = by_user.get(owner)
    ok(owner_row is not None and rid(owner_row) == user_ref,
       "account.user does not resolve to the mirrored user auth.test named")
    note("account.team -> %s, account.user -> %s" % (team_ref, user_ref))

    # ---------------------------------------------------------------- 2
    section("2. counts, and ids that are the provider's keys")
    ok(len(teams) == 1, "expected exactly 1 team row, got %d" % len(teams))
    ok(len(convs) == len(want["convs"]),
       "expected %d conversation rows, got %d" % (len(want["convs"]), len(convs)))
    # USERS. The mirror mints a thin row for every user id a payload NAMES —
    # an editor, a reactor, a roster entry, a Slack Connect sender — because
    # a reference needs a row to land on. So the recordings' users.list is a
    # LOWER bound, not an equality: what must hold is that nothing users.list
    # carried is missing, and that every extra row is an id some recording
    # actually named rather than one the sync invented.
    named = want["named_users"]
    missing_users = sorted(set(want["listed_users"]) - set(by_user))
    ok(not missing_users,
       "%d users that users.list returned are not mirrored: %s"
       % (len(missing_users), missing_users[:5]))
    invented = sorted(set(by_user) - set(want["users"]) - named)
    ok(not invented,
       "%d mirrored user rows are ids NO recording names — the sync invented "
       "them: %s" % (len(invented), invented[:5]))
    extra = sorted(set(by_user) - set(want["users"]))
    if extra:
        note("%d user rows are thin, minted from an id a payload named and "
             "never returned by users.list (e.g. %s)" % (len(extra), extra[:3]))
    unhydrated = sorted(set(want["users"]) - set(by_user))
    if unhydrated:
        # A users.info recording for an id no mirrored message, roster or
        # profile ever names: the sync has no reason to fetch it, and a pull
        # that happens to hold it does not make that a defect.
        seed_note("%d users.info recordings name ids nothing in the mirror "
                  "references, so the sync never fetched them: %s"
                  % (len(unhydrated), unhydrated[:3]))
        ok(SEED or not unhydrated,
           "%d users the recordings carry are not mirrored: %s"
           % (len(unhydrated), unhydrated[:5]))
    ok(set(by_conv) == set(want["convs"]),
       "conversation ids differ: %d missing, %d unexpected"
       % (len(set(want["convs"]) - set(by_conv)),
          len(set(by_conv) - set(want["convs"]))))
    # MESSAGES and FILES are compared against the conversations the sync
    # FINISHED, not against every page the recordings hold. A drain that
    # stopped on its own bounds has not walked the rest yet, and asserting
    # the whole pull against a bounded drain measures the bound, not the
    # sync. `done` is the set of conversations whose state row says the walk
    # completed; in e2e every conversation is in it, so this is identical to
    # the old equality there.
    # A conversation the walk REACHED, which is two cases and not one:
    # it wrote a cursor (it found messages), or its status says the walk
    # finished (it found none in the window). Requiring BOTH matched nothing
    # in the seed: every conversation with a cursor had its `historyStatus`
    # overwritten by a LATER incremental `oldest=` request that no raw pull
    # can hold, and every conversation still reading `ok` is one with nothing
    # in the backfill window. The check was silently asserting nothing.
    # …and NOT a conversation whose history the sync could not read in this
    # run. The account's failure list names them; a conversation that 404'd
    # keeps whatever an earlier walk left behind, and counting it as covered
    # turns the pull's gap into 555 phantom "missing messages".
    hurt_now = {str(r[1]) for r in (account.get("streamCursors") or {}).get("failed") or []
                if len(r) > 1 and r[0] in ("history", "info")}
    done = set()
    for st in states:
        cid = conv_of_row.get(ref_id(props(st).get("conversation")))
        if not cid or cid in hurt_now:
            continue
        if props(st).get("latestTs") or props(st).get("historyStatus") == "ok":
            done.add(cid)
    if hurt_now:
        seed_note("%d conversations could not be read in this run and are "
                  "excluded from the message expectation" % len(hurt_now))
    # …and within a finished conversation, from the BACKFILL FLOOR forward.
    # `backfillDepth` bounds the cold walk (the seed runs `last2d` against a
    # pull that holds months), so the assertion is "no HOLE in the window the
    # mirror actually covers": every recorded message at or after the oldest
    # ts mirrored for that conversation must be there. In e2e the depth is
    # `all`, the oldest mirrored ts is the oldest recorded one, and this is
    # the same full equality it has always been.
    have_by_conv = {}
    for r in msgs:
        cid = conv_of_row.get(ref_id(props(r).get("channel")))
        if cid and props(r).get("ts"):
            have_by_conv.setdefault(cid, set()).add(props(r)["ts"])
    want_msgs, floors = set(), {}
    for cid, by_ts in want["messages"].items():
        if cid not in done or not have_by_conv.get(cid):
            continue
        # The floor comes from the TOP-LEVEL messages only. A thread is
        # walked whole whatever its age (the documented backfill policy:
        # backfillDepth scopes roots), so one old reply in the mirror must
        # not drag the floor down and make every unwalked message between it
        # and the window read as a hole.
        def top_level(ts):
            m = by_ts.get(ts) or {}
            return not m.get("thread_ts") or m.get("thread_ts") == ts
        tops = [ts for ts in have_by_conv[cid] if top_level(ts)]
        floor_ts = min(tops or have_by_conv[cid], key=float)
        floors[cid] = floor_ts
        for ts, m in by_ts.items():
            parent = m.get("thread_ts") or ts
            # a message at or after the floor, or a reply in a thread whose
            # ROOT is at or after it — both are inside the walked window
            if float(ts) >= float(floor_ts) or float(parent) >= float(floor_ts):
                want_msgs.add("%s/%s" % (cid, ts))
    have_msgs = {"%s/%s" % (cid, ts)
                 for cid, tss in have_by_conv.items() for ts in tss}
    absent_msgs = sorted(want_msgs - have_msgs)
    ok(not absent_msgs,
       "%d messages are MISSING from inside the window the mirror covers — a "
       "hole, not a bound (%d conversations finished, %d messages expected in "
       "their windows, %d mirrored in total): %s"
       % (len(absent_msgs), len(done), len(want_msgs), len(msgs),
          absent_msgs[:3]))
    if MODE != "e2e":
        seed_note("messages are asserted from each finished conversation's "
                  "oldest mirrored ts forward (%d conversations), because "
                  "backfillDepth bounds the cold walk" % len(floors))
    if len(done) < len(want["messages"]):
        seed_note("%d of %d conversations with recorded history were not "
                  "walked in this run, so their %d messages are not expected "
                  "yet" % (len(want["messages"]) - len(done),
                           len(want["messages"]),
                           sum(len(v) for cid, v in want["messages"].items()
                               if cid not in done)))
    ok(len({rid(r) for r in msgs}) == len(msgs),
       "duplicate message row ids: %d rows, %d ids"
       % (len(msgs), len({rid(r) for r in msgs})))
    if want["files"]:
        want_files = {fid for fid, keys in want["file_convs"].items()
                      if keys & want_msgs}
        have_files = {props(r).get("fileId") for r in files}
        absent_files = sorted(want_files - have_files)
        ok(not absent_files,
           "%d files shared on messages the mirror holds are not mirrored "
           "(%d expected, %d mirrored): %s"
           % (len(absent_files), len(want_files), len(files), absent_files[:5]))
        stray_files = sorted(have_files - set(want["files"]))
        ok(not stray_files,
           "%d mirrored file ids appear in no recording: %s"
           % (len(stray_files), stray_files[:5]))
    if want["bots"]:
        have_bots = {props(r).get("botId") for r in bots}
        want_bots = {b for b, keys in want["bot_convs"].items()
                     if keys & want_msgs}
        absent_bots = sorted(want_bots - have_bots)
        ok(not absent_bots,
           "%d bot ids named by messages the mirror holds have no mirrored "
           "bot row: %s"
           % (len(absent_bots), absent_bots[:5]))
    note("%d users, %d conversations, %d messages, %d files, %d bots"
         % (len(users), len(convs), len(msgs), len(files), len(bots)))

    # ---------------------------------------------------------------- 3
    section("3. every relation is a reference, and every reference resolves")
    counted = {"user": 0, "channel": 0, "team": 0, "parent": 0, "mentions": 0,
               "reactions": 0, "files": 0, "channelMentions": 0}
    # A reference whose target is outside the cut is the DESIGN (no mustExist
    # on a pointer whose row may land later), not a failure. Counted apart.
    outside = {"parent": 0, "channelMentions": 0, "mentions": 0}
    dangling = []
    for mid, p in by_id.items():
        for scalar in ("conversationId", "teamId", "userId", "threadTs",
                       "mentionIds", "memberIds"):
            if scalar in p:
                dangling.append("message %s still carries %s" % (mid, scalar))
        cid = ref_id(p.get("channel"))
        if not cid or cid not in conv_ids:
            dangling.append("message %s: channel %r is no mirrored conversation"
                            % (mid, p.get("channel")))
        else:
            counted["channel"] += 1
        if p.get("team"):
            # A message's `team` is the workspace it was POSTED in, and on a
            # Slack Connect channel that is legitimately not this one. The
            # assertion was "it equals the local team", which rejects valid
            # provenance; what matters is that the reference RESOLVES to a
            # mirrored team row of the right kind.
            tref = ref_id(p["team"])
            if tref not in {rid(t) for t in teams}:
                dangling.append("message %s: team %s is no mirrored team"
                                % (mid, tref))
            else:
                counted["team"] += 1
        uid = ref_id(p.get("user"))
        if uid:
            counted["user"] += 1
            if uid not in user_ids:
                dangling.append("message %s: user %s is no mirrored user" % (mid, uid))
        pid = ref_id(p.get("parent"))
        if pid:
            counted["parent"] += 1
            parent = by_id.get(pid)
            if parent is None:
                # A thread parent older than the backfill window is a
                # reference at a row that has not landed — which is why the
                # declaration carries no mustExist (§2). Counted, not failed.
                outside["parent"] += 1
            elif ref_id(parent.get("channel")) != cid:
                dangling.append("message %s: parent is in another conversation" % mid)
        for one in ref_ids(p.get("mentions")):
            counted["mentions"] += 1
            if one not in user_ids:
                outside["mentions"] += 1
        for one in ref_ids(p.get("channelMentions")):
            counted["channelMentions"] += 1
            if one not in conv_ids:
                # A `<#C…>` link to a channel outside this cut: the decode is
                # lossless, so the reference is written and the row may land
                # later. Counted, not failed.
                outside["channelMentions"] += 1
        for one in ref_ids(p.get("files")):
            counted["files"] += 1
            if one not in file_ids:
                dangling.append("message %s: file %s is no mirrored file" % (mid, one))
        for r in p.get("reactions") or []:
            for one in ref_ids((r or {}).get("users")):
                counted["reactions"] += 1
                if one not in user_ids:
                    dangling.append("message %s: reaction %s by %s is no user"
                                    % (mid, r.get("name"), one))
    ok(not dangling, "%d reference problems, first 5: %s"
       % (len(dangling), dangling[:5]))
    for name in ("user", "channel", "parent", "mentions", "reactions", "files"):
        ok(counted[name], "not one message carries a resolving `%s` reference" % name)
    note("resolved: " + ", ".join("%s=%d" % (k, v) for k, v in sorted(counted.items())))
    note("pointing outside the cut (by design, no mustExist): "
         + ", ".join("%s=%d" % (k, v) for k, v in sorted(outside.items())))
    ok(not outside["mentions"], "%d mentions name a user the recordings hold "
       "no row for — users.info hydration missed a Slack Connect sender"
       % outside["mentions"])
    # The conversation's own relations, and the roster from the sibling call.
    with_members = [p for p in map(props, convs) if p.get("members")]
    ok(with_members, "no conversation carries `members` from conversations.members")
    bad_members = [m for p in with_members for m in ref_ids(p["members"])
                   if m not in user_ids]
    ok(not bad_members,
       "%d conversation members are not mirrored users (first: %s)"
       % (len(bad_members), bad_members[:3]))
    dm = [p for p in map(props, convs) if p.get("isIm") and p.get("user")]
    ok(dm, "no 1:1 DM carries its partner as a reference")
    ok(all(ref_id(p["user"]) in user_ids for p in dm),
       "a DM partner reference does not resolve")

    # ---------------------------------------------------------------- 4
    section("4. mentions are the text's own tokens, decoded")
    wrong, compared = [], 0
    for cid, by_ts in want["messages"].items():
        for ts, msg in by_ts.items():
            row = by_id.get(by_key.get((cid, ts)))
            if row is None:
                continue
            compared += 1
            seen, expect = set(), []
            for uid in MENTION_RE.findall(msg.get("text") or ""):
                if uid not in seen:
                    seen.add(uid)
                    expect.append(uid)
            got = ref_ids(row.get("mentions"))
            if len(got) != len(expect):
                wrong.append("%s/%s: %d mention refs for %d tokens"
                             % (cid, ts, len(got), len(expect)))
            elif [user_of_row.get(i) for i in got if user_of_row.get(i)] != \
                    [u for u in expect if u in by_user]:
                wrong.append("%s/%s: %r != %r"
                             % (cid, ts, [user_of_row.get(i) for i in got], expect))
            want_ch = []
            seen = set()
            for ch in CHANNEL_RE.findall(msg.get("text") or ""):
                if ch not in seen:
                    seen.add(ch)
                    want_ch.append(ch)
            got_ch = ref_ids(row.get("channelMentions"))
            if len(got_ch) != len(want_ch):
                wrong.append("%s/%s: %d channel refs for %d tokens"
                             % (cid, ts, len(got_ch), len(want_ch)))
            elif [conv_of_row.get(i) for i in got_ch if conv_of_row.get(i)] != \
                    [c for c in want_ch if c in by_conv]:
                wrong.append("%s/%s channels: %r != %r"
                             % (cid, ts, [conv_of_row.get(i) for i in got_ch], want_ch))
    ok(not wrong, "%d decode disagreements, first 3: %s" % (len(wrong), wrong[:3]))
    note("mentions and channel links recomputed for %d recorded messages"
         % compared)

    # ---------------------------------------------------------------- 5
    section("5. the owner-relative cases are DATA, found by reference")
    from_owner = [m for m, p in by_id.items() if ref_id(p.get("user")) == user_ref]
    mentions_owner = [m for m, p in by_id.items()
                      if user_ref in ref_ids(p.get("mentions"))]
    ok(from_owner, "no message's `user` is the account's own user — the owner's "
                   "own messages are missing from the cut")
    ok(mentions_owner, "no message's `mentions` contains the account's user")
    for flag in ("fromOwner", "mentionsOwner", "directedAtOwner", "fromBot",
                 "fileCount", "reactionCount"):
        ok(not any(flag in p for p in by_id.values()),
           "a message still carries the computed property `%s`" % flag)
    note("%d messages from the owner, %d mentioning them — both by reference"
         % (len(from_owner), len(mentions_owner)))
    bot_msgs = [m for m, p in by_id.items() if ref_id(p.get("bot"))]
    ok(bot_msgs, "no message points at a bot identity")
    note("%d messages carry a `bot` reference" % len(bot_msgs))

    # ---------------------------------------------------------------- 6
    section("6. every message is on the timeline")
    import datetime as dt
    no_at = [m for m, p in by_id.items() if not p.get("at")]
    ok(not no_at, "%d messages carry no `at`" % len(no_at))
    bad_at = []
    for m, p in by_id.items():
        at, ts = p.get("at"), p.get("ts")
        if not (at and ts):
            continue
        # EXACT, to the microsecond. A one-second tolerance was here, and it
        # was permitting the defect it was meant to catch: the sync parsed
        # the ts through a float and then zeroed the microseconds, so every
        # instant in the mirror could be up to a second away from the ts
        # printed beside it. The ts is `<seconds>.<6 digits>`, so the two
        # halves are compared as integers and never meet a float.
        whole, _, frac = str(ts).partition(".")
        want_at = dt.datetime.fromtimestamp(
            int(whole), dt.timezone.utc).replace(
                microsecond=int((frac or "")[:6].ljust(6, "0") or 0))
        got_at = dt.datetime.fromisoformat(str(at).replace("Z", "+00:00"))
        if got_at != want_at:
            bad_at.append("%s: at=%s want=%s" % (ts, at, want_at.isoformat()))
    ok(not bad_at, "%d messages whose `at` is not EXACTLY their ts: %s"
       % (len(bad_at), bad_at[:3]))
    perma = [p for p in by_id.values() if p.get("permalink")]
    ok(perma, "no message carries a composed permalink")
    typed = [p for p in by_id.values() if p.get("type") == "message"]
    ok(len(typed) == len(by_id),
       "%d messages do not carry the one-value discriminator `type`"
       % (len(by_id) - len(typed)))
    if files:
        # A file row minted from an `x_files` ID ALONE carries identity and
        # nothing else — there is no `created` to feed the trait until an
        # embedded object or files.info fills it in. Only a row that carries
        # the upload metadata must carry the instant.
        fat = [props(f).get("fileId") for f in files
               if not props(f).get("at") and props(f).get("name")]
        ok(not fat, "%d file rows carry upload metadata but no `at` — their "
                    "`created` never reached the trait: %s" % (len(fat), fat[:5]))
        thin = [f for f in files if not props(f).get("at")]
        if thin:
            note("%d file rows are identity-only (named by id, never embedded)"
                 % len(thin))

    # ---------------------------------------------------------------- 7
    section("7. the cursors live on the state kind, not on the mirror")
    ok(states, "not one conversationsync row was written")
    walked = [props(s) for s in states if props(s).get("latestTs")]
    ok(walked, "not one conversationsync row carries a history cursor")
    for s in states:
        p = props(s)
        cref = ref_id(p.get("conversation"))
        ok(cref in conv_ids,
           "a conversationsync row points at %r, which is no conversation" % cref)
        break
    leaked = [c for c in convs for k in ("latestTs", "repliesTs",
                                         "membersSyncedAt", "historyStatus")
              if k in props(c)]
    ok(not leaked, "%d conversation rows still carry a sync cursor" % len(leaked))
    note("%d state rows, %d with a history cursor" % (len(states), len(walked)))

    # ---------------------------------------------------------------- 8
    section("8. no row hides a payload")
    with_raw = [k for k, rows in (("user", users), ("conversation", convs),
                                  ("message", msgs), ("team", teams),
                                  ("file", files))
                for r in rows if "raw" in props(r)]
    ok(not with_raw, "%d rows carry `raw`" % len(with_raw))
    st, kinds = api("GET", "/api/v1/kinds")
    declared = json.dumps(kinds or {})
    ok('"raw"' not in declared or "slack" not in declared.split('"raw"')[0][-400:],
       "a slack kind still declares a `raw` property")
    # The display name lives in the profile, once.
    lifted = [r for r in users if "displayName" in props(r) or "email" in props(r)]
    ok(not lifted, "%d user rows carry a lifted displayName/email beside the "
                   "profile" % len(lifted))
    with_profile = [props(r) for r in users if props(r).get("profile")]
    ok(with_profile, "no user row carries a profile object")
    ok(any((p["profile"] or {}).get("displayName") for p in with_profile),
       "not one profile carries displayName — the name the API puts there")
    ok(any((p["profile"] or {}).get("email") for p in with_profile),
       "not one profile carries an email — a person mapping has nothing to "
       "match on")
    if MODE == "e2e":
        cast = json.loads((ROOT / "providers" / "persona.public.json").read_text())
        fake = {e.lower() for p in [cast["owner"]] + cast["people"]
                for e in (p.get("emails") or [])}
        real = [(p["profile"] or {}).get("email") for p in with_profile
                if (p["profile"] or {}).get("email")
                and (p["profile"]["email"]).lower() not in fake]
        ok(not real, "%d mirrored users carry an email the public persona cast "
                     "does not own — a fixture is leaking: %s"
           % (len(real), real[:3]))

    # ---------------------------------------------------------------- 9
    section("9. a second sync is idempotent, and picks up what is new")
    before = {"user": len(users), "conversation": len(convs), "message": len(msgs)}
    conv_versions = {rid(c): version(c) for c in convs}
    cursors_before = {ref_id(props(s).get("conversation")): props(s).get("latestTs")
                      for s in states}
    stamp = account.get("lastSyncedAt")
    st, _ = sync_now()
    ok(st < 400, "the second sync could not be fired: HTTP %s" % st)
    after_props = wait_for_sync(stamp)
    users2, convs2, msgs2 = records(USER), records(CONV), records(MESSAGE)
    states2 = records(SYNC)
    note("after the second sync: %d user, %d conversation, %d message"
         % (len(users2), len(convs2), len(msgs2)))
    ok(len(users2) == before["user"],
       "the second sync changed the user count: %d -> %d"
       % (before["user"], len(users2)))
    ok(len(convs2) == before["conversation"],
       "the second sync changed the conversation count: %d -> %d"
       % (before["conversation"], len(convs2)))
    ok(len({rid(r) for r in msgs2}) == len(msgs2),
       "the second sync produced duplicate message ids")

    # THE POINT OF THE STATE KIND: a sync that only moves HISTORY must not
    # bump a conversation's version. It is asserted over a THIRD sync, not the
    # second: a bounded first drain can leave rosters unwalked, and a roster
    # landing IS a change to the conversation (conversations.members is the
    # conversation's own field, §1) — so the second sync may legitimately move
    # a row. By the third, every sibling call has landed and only cursors and
    # messages can move.
    settled = {rid(c): (version(c), props(c)) for c in convs2}
    state_before = {rid(s2): version(s2) for s2 in states2}
    stamp2 = after_props.get("lastSyncedAt")
    st, _ = sync_now()
    ok(st < 400, "the third sync could not be fired: HTTP %s" % st)
    third = wait_for_sync(stamp2)
    convs3, msgs3, states3 = records(CONV), records(MESSAGE), records(SYNC)
    # A conversation row may still move for ONE reason: its roster landing.
    # conversations.members is one Tier-4 call per channel and the drain walks
    # one per invocation, so a four-channel workspace takes a few syncs to
    # cover them — and `members` IS the conversation's own field (§1, a
    # sibling call keyed by the object's id). Anything ELSE moving is the bug
    # the state kind exists to prevent.
    # Two assertions. CONTENT: nothing but the properties in MOVING may
    # change. WRITES: every write the row took since the second sync touched
    # only MOVING properties. The engine suppresses a write whose values equal
    # the stored ones (internal/engine/noopwrite_db_test.go), so a write that
    # names `purpose` on a row whose `purpose` ends where it began is one of
    # two writes that cancel out: two calls disagreeing about one field, every
    # sync. That is real trigger churn, and it is what substrate#575 was (the
    # recordings of conversations.list and conversations.info carried
    # different purpose and topic text for three channels). A version with no
    # changelog write behind it would be the engine bug #575 first suspected.
    MOVING = ("members", "priority")
    unexplained, bumped, rostered = [], [], 0
    for c in convs3:
        was = settled.get(rid(c))
        if was is None:
            continue
        before_p, now_p = was[1], props(c)
        changed = sorted(k for k in set(before_p) | set(now_p)
                         if json.dumps(before_p.get(k), sort_keys=True)
                         != json.dumps(now_p.get(k), sort_keys=True))
        # `members` is the roster, refreshed by a sibling call the drain
        # walks over several runs. `priority` is Slack's per-CALLER relevance
        # score and is genuinely volatile: the owner's own pull holds 0.0426
        # for a DM from conversations.list and 0.0458 for the SAME DM from
        # conversations.info, because the two calls were made moments apart.
        # A mirror that reflects it is correct; a test that calls it drift is
        # not. Neither is sync state on the row.
        moved = [k for k in changed if k in MOVING]
        changed = [k for k in changed if k not in MOVING]
        if changed:
            unexplained.append("%s: %s" % (props(c).get("conversationId"), changed))
            continue
        if moved:
            rostered += 1
        if version(c) == was[0]:
            continue
        writes = writes_since(CONV, rid(c), was[0])
        touched = sorted({k for w in (writes if isinstance(writes, list) else [])
                          for k in (w or []) if k not in MOVING})
        if not isinstance(writes, list) or not writes or touched:
            bumped.append("%s: v%s -> v%s, net change %s, writes %s"
                          % (props(c).get("conversationId"), was[0], version(c),
                             moved or "none", writes))
    ok(not unexplained,
       "%d conversation rows CHANGED during a sync that only advanced cursors "
       "and history — the mirror is carrying sync state: %s"
       % (len(unexplained), unexplained[:3]))
    note("third sync: %d conversation rows moved only %s" % (rostered, "/".join(MOVING)))
    ok(all(version(c) is not None for c in convs3),
       "a conversation row carries no version, so the version check proves "
       "nothing")
    ok(not bumped,
       "%d conversation rows took a write to a property that ended where it "
       "began (two calls disagree and cancel out), or a version with no write "
       "behind it: %s" % (len(bumped), bumped[:3]))
    ok(len(msgs3) == len(msgs2),
       "the third sync changed the message count: %d -> %d"
       % (len(msgs2), len(msgs3)))
    ok(str(third.get("syncStatus") or "").startswith("ok"),
       "after the third sync syncStatus is %r" % third.get("syncStatus"))
    note("third sync: %d conversation rows unmoved, %d messages unchanged, "
         "%d state rows" % (len(convs3), len(msgs3), len(states3)))
    _ = state_before
    advanced = [s for s in states2
                if props(s).get("latestTs")
                and props(s).get("latestTs") >= (cursors_before.get(
                    ref_id(props(s).get("conversation"))) or "")]
    ok(len(advanced) == len([s for s in states2 if props(s).get("latestTs")]),
       "a conversationsync cursor went backwards")

    new = {"%s/%s" % (cid, ts): msg
           for cid, by_ts in want["incremental"].items()
           for ts, msg in by_ts.items() if (cid, ts) not in by_key}
    seen = {"%s/%s" % (conv_of_row.get(ref_id(props(r).get("channel"))),
                       props(r).get("ts")) for r in msgs2}
    all_incremental = {"%s/%s" % (cid, ts)
                       for cid, by_ts in want["incremental"].items() for ts in by_ts}
    if all_incremental:
        absent = sorted(all_incremental - seen)
        ok(not absent, "%d of %d messages that ONLY an incremental fetch can "
                       "reach are not in the mirror: %s"
           % (len(absent), len(all_incremental), absent[:3]))
        gone = [k for k in want["deleted"]
                if tuple(k.rsplit("/", 1)) in by_key]
        ok(len(msgs2) == before["message"] + len(new) - len(gone),
           "expected %d messages after the second sync (%d + %d new - %d "
           "deleted), got %d"
           % (before["message"] + len(new) - len(gone), before["message"],
              len(new), len(gone), len(msgs2)))
        note("%d incremental message(s) in the mirror, %d of them new this run"
             % (len(all_incremental), len(new)))
    else:
        ok(len(msgs2) == before["message"],
           "the second sync changed the message count with no incremental "
           "recording to explain it: %d -> %d" % (before["message"], len(msgs2)))
    ok(str(after_props.get("syncStatus") or "").startswith("ok"),
       "after the second sync syncStatus is %r" % after_props.get("syncStatus"))

    # ---------------------------------------------------------------- 10
    section("10. the work the sync could not do")
    cur = (after_props.get("streamCursors") or {})
    failed = cur.get("failed") or []
    if not failed:
        note("no unit of work failed")
    else:
        # `streamCursors.failed` is [kind, id, attempts, why]. Printed here so
        # "[200 failed]" on the status is answerable without reading the
        # server log: what failed, how often it was tried, and why.
        by_class = {}
        for row in failed:
            kind = row[0] if len(row) > 0 else "?"
            why = str(row[3] if len(row) > 3 else "").strip()
            by_class.setdefault((kind, why[:60]), []).append(row)
        note("%d failed units of work, by class:" % len(failed))
        for (kind, why), rows in sorted(by_class.items(),
                                        key=lambda kv: -len(kv[1])):
            note("    %-9s x%-5d %s   (e.g. %s)"
                 % (kind, len(rows), why or "(no reason)", rows[0][1]))
        permanent = [r for r in failed
                     if str(r[3] if len(r) > 3 else "").startswith("permanent")]
        note("%d permanent (never retried), %d transient"
             % (len(permanent), len(failed) - len(permanent)))
        # A PERMANENT failure is an answer, not an outage: Slack said the
        # channel is gone or the caller cannot read it. A TRANSIENT one that
        # has exhausted its retries is a real problem in both modes.
        stuck = [r for r in failed
                 if not str(r[3] if len(r) > 3 else "").startswith("permanent")
                 and (r[2] if len(r) > 2 else 0) > 3]
        ok(not stuck,
           "%d units of work failed TRANSIENTLY and exhausted their retries: "
           "%s" % (len(stuck), [(r[0], r[1], r[3]) for r in stuck[:3]]))
        # ONE CONVERSATION'S 404 MAY NOT STOP THE OTHERS. The failures name
        # conversations; the assertion is that conversations which did NOT
        # fail were still walked to completion in the same run.
        hurt = {str(r[1]) for r in failed if r[0] in ("history", "info",
                                                      "roster", "thread")}
        states_now = records(SYNC)
        walked = {conv_of_row.get(ref_id(props(st).get("conversation")))
                  for st in states_now if props(st).get("historyStatus") == "ok"}
        clean = {c for c in walked if c and c not in hurt}
        ok(clean,
           "%d units of work failed and NOT ONE conversation completed its "
           "history walk — a failure is blocking the drain" % len(failed))
        note("%d conversations completed their history walk alongside %d "
             "failures" % (len(clean), len(failed)))

    # ---------------------------------------------------------------- 11
    section("11. an edit, a delete and a reaction removed")
    # SEED: a raw pull is a recording of `conversations.history`, and history
    # returns the CURRENT state of each message — an edit shows up as an
    # `edited` object on the message itself, never as a `message_changed`
    # envelope, and a deleted message simply is not there. The envelopes are
    # Events API shapes, so `fixtures/curate.py` cuts them by hand and no raw
    # recording can contain them. Assert them where they exist; REQUIRE them
    # in e2e, so the coverage cannot quietly disappear from the cut.
    ok(SEED or want["changed"],
       "the fixture set carries no message_changed envelope — edit handling "
       "is untested")
    ok(SEED or want["deleted"],
       "the fixture set carries no message_deleted envelope — delete handling "
       "is untested")
    if not (want["changed"] or want["deleted"]):
        seed_note("the recordings hold no mutation envelope, so edit and "
                  "delete handling is not exercised by this run")
    now_by_key = {"%s/%s" % (conv_of_row.get(ref_id(props(r).get("channel"))),
                             props(r).get("ts")): props(r) for r in msgs2}
    for key, inner in sorted(want["changed"].items()):
        row = now_by_key.get(key)
        if not ok(row, "a message_changed envelope named %s and the mirror "
                       "has no such message" % key):
            continue
        ok(row.get("text") == inner.get("text"),
           "%s was edited upstream and the mirror still holds the old text"
           % key)
        ok(row.get("edited"), "%s was edited upstream and carries no `edited`"
                              % key)
    for key in sorted(want["deleted"]):
        ok(key not in now_by_key,
           "%s was DELETED upstream and is still in the mirror" % key)
    # A REACTION REMOVED. The incremental page re-states a message with an
    # empty `reactions`, which is Slack saying there are none now.
    emptied = [("%s/%s" % (cid, ts))
               for cid, by_ts in want["incremental"].items()
               for ts, msg in by_ts.items()
               if isinstance(msg.get("reactions"), list) and not msg["reactions"]]
    ok(SEED or emptied,
       "the fixture set carries no message re-stated with `reactions: []` — "
       "reaction clearing is untested")
    if not emptied:
        seed_note("no recording re-states a message with an empty "
                  "`reactions`, so clearing is not exercised by this run")
    for key in emptied:
        row = now_by_key.get(key)
        if ok(row, "a cleared-reaction message %s is not in the mirror" % key):
            ok(not row.get("reactions"),
               "%s came back with no reactions and the mirror still holds "
               "them — an empty collection was dropped instead of clearing"
               % key)
    note("%d edits, %d deletes, %d reaction clears asserted"
         % (len(want["changed"]), len(want["deleted"]), len(emptied)))

    # ---------------------------------------------------------------- 12
    section("12. the requests themselves: windows, pages and fallbacks")
    reqs = None
    if MOCK:
        try:
            with urllib.request.urlopen(MOCK + "/__mock/requests", timeout=20) as r:
                log = json.loads(r.read())
            reqs = log if isinstance(log, list) else (log.get("items")
                                                     or log.get("requests") or [])
        except Exception as e:  # noqa: BLE001
            reqs = None
            note("(mock request log unavailable: %s)" % e)
        if reqs is not None:
            def q(entry, key):
                # the mock logs `query` as the RAW query string on some
                # paths and as a parsed mapping on others
                raw = entry.get("query")
                if isinstance(raw, str):
                    raw = urllib.parse.parse_qs(raw.lstrip("?"))
                v = (raw or {}).get(key)
                return v[0] if isinstance(v, list) else v

            hist = [e for e in reqs if e.get("path", "").endswith(
                "/conversations.history")]
            inc = [e for e in hist if q(e, "oldest")]
            note("%d upstream requests served, %d history, %d of those "
                 "incremental" % (len(reqs), len(hist), len(inc)))
            ok(inc, "the second sync never sent an `oldest` — it re-walked cold "
                    "instead of using the stored cursor")

            # EXACT BOUNDS, not "a request containing oldest". Every
            # incremental window must name a cursor the mirror actually
            # stored for that conversation, or the sync is walking from
            # somewhere it invented.
            # Every cursor the mirror has EVER held, because the third
            # sync asks with the cursor the second one stored and `states2`
            # is the snapshot before that.
            stored = {}
            for st in list(states) + list(states2) + list(states3):
                cid = conv_of_row.get(ref_id(props(st).get("conversation")))
                if cid and props(st).get("latestTs"):
                    stored.setdefault(cid, set()).add(props(st)["latestTs"])
                for ts in props(st).get("threadWatch") or []:
                    stored.setdefault(cid, set()).add(ts)
            stray = [(q(e, "channel"), q(e, "oldest")) for e in inc
                     if q(e, "oldest") not in stored.get(q(e, "channel"), set())]
            ok(not stray, "%d incremental history windows name a cursor the "
                          "mirror never stored: %s" % (len(stray), stray[:3]))

            # PAGINATION. The set splits users.list and conversations.list
            # across two pages on a real cursor; a sync that stopped at page
            # one would still pass every count check, because page two's rows
            # would simply be absent from both sides.
            for method in ("users.list", "conversations.list"):
                calls = [e for e in reqs if e.get("path", "").endswith(method)]
                paged = [e for e in calls if q(e, "cursor")]
                ok(paged, "the sync never asked %s for its second page — the "
                          "cursor the first page returned was not followed"
                   % method)

            # TWO DIFFERENT THINGS, and only one of them is ever the
            # sync's fault.
            #
            # A RELAXED or FALLBACK match means a recording EXISTS and the
            # mock had to drop the page or window parameters to reach it —
            # the sync asked the wrong question and got an answer that looks
            # right. Always fatal, in both modes.
            loose = [(e.get("path"), e.get("match"), e.get("file"))
                     for e in reqs
                     if e.get("match") not in ("exact", "miss", None)]
            ok(not loose, "%d requests were answered by a RELAXED or FALLBACK "
                          "match, so their page and window prove nothing: %s"
               % (len(loose), loose[:3]))

            # A MISS means there is no recording at all. In e2e that is a hole
            # in a curated set and is fatal. SEED: the raw pull is the owner's
            # own capture of a 3,865-conversation workspace and is incomplete
            # by construction — it cannot hold `conversations.info` for a
            # channel the puller never reached. A miss on one of those is the
            # PULL's gap, reported by method and count. It is still fatal for
            # the calls a pull always records, because a miss there means the
            # sync asked for something that should have been captured.
            ALWAYS_RECORDED = ("auth.test", "team.info", "users.list",
                               "conversations.list")
            misses = [e for e in reqs if e.get("match") == "miss"]
            by_method = {}
            for e in misses:
                by_method[e.get("path", "?")] = by_method.get(e.get("path", "?"), 0) + 1
            if misses:
                note("MISSING recordings, by method: %s"
                     % ", ".join("%s x%d" % (m.rsplit("/", 1)[-1], n)
                                 for m, n in sorted(by_method.items(),
                                                    key=lambda kv: -kv[1])))
            core = [e for e in misses
                    if e.get("path", "").endswith(ALWAYS_RECORDED)]
            ok(not core,
               "%d requests to a call every pull records found no recording: "
               "%s" % (len(core),
                       [(e.get("path"), e.get("query")) for e in core[:3]]))
            ok(SEED or not misses, "%d requests found no recording at all: %s"
               % (len(misses),
                  [(e.get("path"), e.get("query")) for e in misses[:3]]))
            if SEED and misses:
                seed_note("%d of %d upstream requests had no recording; the "
                          "pull does not hold them and the sync treats each "
                          "as that conversation's own permanent failure"
                          % (len(misses), len(reqs)))

    # ---------------------------------------------------------------- 13
    section("13. the objects' own endpoints: files.info and bots.info")
    # T-021. A file row was an upload's metadata and its thumbnails: measured
    # against the owner's pull, `files.info` adds exactly `shares`,
    # `channels`, `groups`, `ims`, `comments_count` and `has_more_shares` to
    # an embedded file object — six declared properties that were ALWAYS
    # EMPTY because nothing ever called the endpoint. A bot row was an id
    # plus whatever a `bot_profile` happened to carry, and 37 of the owner's
    # 91 bots never got one.
    infos = {(d.get("file") or {}).get("id"): d.get("file") or {}
             for _n, d in each("GET_api_files.info__*")}
    infos.pop(None, None)
    binfos = {(d.get("bot") or {}).get("id"): d.get("bot") or {}
              for _n, d in each("GET_api_bots.info__*")}
    binfos.pop(None, None)
    ok(SEED or infos, "the fixture set carries no files.info response, so "
                      "file hydration is untested")
    ok(SEED or binfos, "the fixture set carries no bots.info response, so "
                       "bot hydration is untested")
    files3 = records(FILE)
    bots3 = records(BOT)
    convs3 = records(CONV)
    conv3_ids = {rid(r) for r in convs3}
    by_file3 = {props(r).get("fileId"): props(r) for r in files3}
    by_bot3 = {props(r).get("botId"): props(r) for r in bots3}
    hydrated, share_outside = 0, 0
    for fid, f in sorted(infos.items()):
        row = by_file3.get(fid)
        if not ok(row is not None,
                  "files.info answered for %s and the mirror has no file row"
                  % fid):
            continue
        hydrated += 1
        ok(row.get("commentsCount") is not None,
           "%s was read from files.info and the row carries no "
           "`commentsCount` — the hydration did not land" % fid)
        for prop, api_key in (("channels", "channels"), ("groups", "groups"),
                              ("ims", "ims")):
            want_n = len({c for c in (f.get(api_key) or []) if c})
            got = ref_ids(row.get(prop))
            ok(len(got) == want_n,
               "%s.%s: files.info named %d conversation(s) and the row holds "
               "%d" % (fid, prop, want_n, len(got)))
            # A share names a conversation the walk may never touch — a
            # channel the token's user is not in. Pointing outside the cut is
            # the DESIGN here, as it is for `channelMentions`: no `mustExist`,
            # and nothing is minted to make a pointer tidy.
            share_outside += len([r for r in got if r not in conv3_ids])
        if isinstance(f.get("shares"), dict) and f["shares"]:
            ok(isinstance(row.get("shares"), dict) and row["shares"],
               "%s: files.info carried `shares` and the row does not" % fid)
        if "has_more_shares" in f:
            ok(row.get("hasMoreShares") == f["has_more_shares"],
               "%s.hasMoreShares is %r, files.info said %r"
               % (fid, row.get("hasMoreShares"), f["has_more_shares"]))
    for bid, b in sorted(binfos.items()):
        row = by_bot3.get(bid)
        if not ok(row is not None,
                  "bots.info answered for %s and the mirror has no bot row"
                  % bid):
            continue
        ok(row.get("name") == b.get("name"),
           "%s.name is %r, bots.info said %r" % (bid, row.get("name"),
                                                 b.get("name")))
        if b.get("user_id"):
            ok(ref_id(row.get("user")),
               "%s: bots.info named the bot's user and the row has no `user` "
               "reference" % bid)
    note("%d file(s) and %d bot(s) hydrated from their own endpoint; %d share "
         "reference(s) point outside the cut, by design (%d conversation(s) "
         "named by a share in total)"
         % (hydrated, len(binfos), share_outside, len(want["share_convs"])))

    # HYDRATED ONCE. The phase steps over a row that already carries the
    # field only its endpoint supplies, so three syncs must not ask for one
    # file three times — this workspace has 2,699 of them.
    if reqs is not None:
        for method in ("files.info", "bots.info"):
            calls = [e for e in reqs if e.get("path", "").endswith(method)]
            keys = [json.dumps(e.get("query"), sort_keys=True) for e in calls]
            ok(len(keys) == len(set(keys)),
               "%s was asked for the same object more than once across three "
               "syncs (%d calls, %d distinct) — hydration is not converging"
               % (method, len(keys), len(set(keys))))
            note("%s: %d call(s), all distinct" % (method, len(calls)))

    # `profile.botId` was a string beside a bot kind we already mirror.
    linked = 0
    for uid, u in sorted(want["users"].items()):
        bid = (u.get("profile") or {}).get("bot_id")
        if not bid:
            continue
        row = props(by_user.get(uid) or {})
        prof = row.get("profile") or {}
        ok("botId" not in prof,
           "%s.profile still carries a bare `botId` beside the bot kind" % uid)
        ref = ref_id(prof.get("bot"))
        ok(ref and ref in {rid(r) for r in bots3},
           "%s.profile.bot does not resolve to a bot row (%r)"
           % (uid, prof.get("bot")))
        linked += 1
    note("%d app user(s) whose profile points at a bot row" % linked)

    # AMENDMENT 6, the thin object. `user_profile` is nine of a profile's ~50
    # fields, inlined on a message beside its author. It was dropped
    # entirely; writing it as the profile would DELETE the other forty-one,
    # because a patch replaces an object whole. So: the row must hold the
    # thin payload's fields AND still hold what the full read gave it.
    thin = {}
    for bucket in ("messages", "incremental"):
        for _cid, by_ts in (want[bucket] or {}).items():
            for msg in by_ts.values():
                prof = msg.get("user_profile")
                if isinstance(prof, dict) and prof and msg.get("user"):
                    thin.setdefault(msg["user"], prof)
    users3 = records(USER)
    by_user3 = {props(r).get("userId"): props(r) for r in users3}
    for uid, prof in sorted(thin.items()):
        row = (by_user3.get(uid) or {}).get("profile") or {}
        if not ok(row, "%s was named by an inlined user_profile and the "
                       "mirror's row carries no profile at all" % uid):
            continue
        if prof.get("display_name"):
            ok(row.get("displayName") == prof["display_name"],
               "%s.profile.displayName is %r, the inlined user_profile said %r"
               % (uid, row.get("displayName"), prof["display_name"]))
        if uid in want["listed_users"]:
            full = (want["users"][uid].get("profile") or {})
            for key, api_key in (("email", "email"), ("updated", "updated"),
                                 ("realName", "real_name")):
                if full.get(api_key) and key not in row:
                    ok(False, "%s: the thin `user_profile` REPLACED the "
                              "profile users.list filled in — %s is gone"
                       % (uid, key))
    note("%d author(s) carried an inlined user_profile" % len(thin))
    if not thin:
        note("(no recording inlines a user_profile, so the merge is not "
             "exercised by this run)")

    # ---------------------------------------------------------------- 14
    section("14. the failure matrix, and the pages behind a cursor")
    # T-021. The whole set used to be `ok: true`, so the rate-limit, the
    # 5xx-retry and the expired-cursor paths were exercised by reading the
    # code. Each seam is a `__responses` envelope: the first call fails, the
    # rest answer, and what is asserted is that the mirror RECOVERED.
    # A TRUNCATED SLUG would be silent. `tools/mockserver.py` caps a query
    # slug at 80 characters and replaces the tail with a sha1, and this
    # scenario buckets a history page as incremental by looking for `oldest-`
    # IN ITS NAME. Adding `include_all_metadata` took the longest incremental
    # name to exactly 80, so one more parameter would start hashing the
    # window away and every incremental page would read as a cold one. Fail
    # loudly instead.
    truncated = [f.name for f in RECORDINGS.glob("GET_api_conversations.*.json")
                 if re.search(r"-[0-9a-f]{8}\.json$", f.name)
                 and "oldest-" not in f.name and "cursor-" not in f.name]
    ok(not truncated,
       "%d recording name(s) had their query slug truncated and hashed, so a "
       "window or a page is no longer readable from the name: %s"
       % (len(truncated), truncated[:3]))

    seams = {}
    for f in sorted(RECORDINGS.glob("GET_*.json")):
        doc = json.loads(f.read_text())
        if not isinstance(doc, dict) or "__responses" not in doc:
            continue
        first = doc["__responses"][0]
        if isinstance(first, dict) and "__response" in first:
            seams[f.name] = int((first["__response"] or {}).get("status", 200))
        elif isinstance(first, dict) and not first.get("ok"):
            seams[f.name] = first.get("error") or "not-ok"
    ok(SEED or seams,
       "the fixture set carries no failure seam at all — the 429, the 5xx "
       "and the expired cursor are still tested by reading the code")
    for name, why in sorted(seams.items()):
        note("failure seam: %s -> %s" % (name[:62], why))
    if reqs is not None:
        served = {e.get("status") for e in reqs}
        for code, what in ((429, "a rate limit"), (500, "a 5xx")):
            if not any(v == code for v in seams.values()):
                continue
            if ok(code in served,
                  "the set holds a %d seam and the mock never served one — "
                  "the sync did not reach it" % code):
                note("%s was served and the drain carried on" % what)
        # EVERY history and replies call asks for the metadata. The parameter
        # is part of the recording's name, so a call without it would be a
        # miss rather than a silent loss — but a call that stopped sending it
        # would quietly stop mirroring `metadata`, which is why it is checked
        # here and not only in the recordings.
        for method in ("conversations.history", "conversations.replies"):
            calls = [e for e in reqs if e.get("path", "").endswith(method)]
            bare = [e for e in calls if q(e, "include_all_metadata") != "true"]
            ok(not bare,
               "%d %s call(s) went without `include_all_metadata`: %s"
               % (len(bare), method, [e.get("query") for e in bare[:2]]))
            paged = [e for e in calls if q(e, "cursor")]
            ok(SEED or paged,
               "no %s call followed a cursor — the set holds no page behind "
               "one, so the cold walk's paging is untested" % method)
            note("%s: %d call(s), %d behind a cursor" % (method, len(calls),
                                                         len(paged)))
    # RECOVERY. A seam that broke the mirror rather than testing it would
    # show up here: the account is `ok`, and nothing is still pending.
    final = props(api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))[1] or {})
    ok(str(final.get("syncStatus") or "").startswith("ok"),
       "after the seams the account's syncStatus is %r"
       % final.get("syncStatus"))
    ok(not final.get("retryNotBefore"),
       "the account is still holding a rate-limit deadline: %r"
       % final.get("retryNotBefore"))

    # --------------------------------------------------------------- 15
    section("15. the core `sync` trait")
    # T-084 / decision 0085. The trait is what the Connections page and
    # `substratectl sync status` read, and it is written BESIDE `syncStatus`
    # and `lastSyncedAt` — so every assertion here has a legacy twin above
    # that must still hold, and the two are checked for agreeing.
    requested = final.get("syncRequestedAt")
    ok(final.get("syncState") == "ok",
       "syncState is %r after a completed walk" % final.get("syncState"))
    ok(final.get("syncMessage"), "syncMessage is empty after a completed walk")
    ok(final.get("syncMessage") == final.get("syncStatus"),
       "syncMessage %r and syncStatus %r disagree"
       % (final.get("syncMessage"), final.get("syncStatus")))
    ok(final.get("lastSyncedAt"), "the trait's lastSyncedAt is unset")
    ok(requested, "the scenario never stamped syncRequestedAt, so the ack is "
                  "untested")
    ok(final.get("syncRequestedAck") == requested,
       "syncRequestedAck is %r and the request was %r"
       % (final.get("syncRequestedAck"), requested))
    progress = final.get("syncProgress")
    ok(isinstance(progress, dict)
       and {"phase", "done", "total", "pending"} <= set(progress),
       "syncProgress is %r, not {phase, done, total, pending}" % (progress,))
    if isinstance(progress, dict):
        ok(progress.get("phase") == "done" and progress.get("pending") == 0,
           "syncProgress says %r after a walk that ran out of work"
           % (progress,))
        ok(progress.get("total") == progress.get("done"),
           "syncProgress total %r != done %r with nothing pending"
           % (progress.get("total"), progress.get("done")))
    streams = final.get("syncStreams")
    want_streams = {"users", "conversations", "messages"}
    ok(isinstance(streams, dict) and set(streams) == want_streams,
       "syncStreams names %r, not %r"
       % (sorted(streams) if isinstance(streams, dict) else streams,
          sorted(want_streams)))
    if isinstance(streams, dict):
        for name in sorted(want_streams):
            entry = streams.get(name) or {}
            ok(entry.get("state") == "ok",
               "stream %s is %r after a completed walk" % (name, entry.get("state")))
            ok(entry.get("pending") == 0,
               "stream %s reports %r pending" % (name, entry.get("pending")))
            ok(entry.get("lastAt"), "stream %s never stamped lastAt" % name)
            ok(entry.get("requestedAck") == requested,
               "stream %s acked %r, the request was %r"
               % (name, entry.get("requestedAck"), requested))
        # The breakdown is the whole of the total, or it is decoration.
        if isinstance(progress, dict):
            ok(sum(int((streams.get(n) or {}).get("pending") or 0)
                   for n in want_streams) == progress.get("pending"),
               "the per-stream pending counts sum to %r, syncProgress says %r"
               % (sum(int((streams.get(n) or {}).get("pending") or 0)
                      for n in want_streams), progress.get("pending")))

    # THE TWO UNHAPPY STATES, injected rather than waited for. A 429 on
    # auth.test is `throttled`; a 401 is `erroring` with the cause; the run
    # after each recovers, which is what makes them a test of the trait
    # rather than of the fixture.
    if MOCK and not SEED:
        faults([{"match": "GET /api/auth.test", "status": [429],
                 "headers": {"Retry-After": "1"},
                 "body": {"ok": False, "error": "ratelimited"}}])
        sync_now()
        held = wait_for_state("throttled")
        ok(held.get("syncState") == "throttled",
           "an injected 429 left syncState %r, not throttled"
           % held.get("syncState"))
        ok(held.get("retryNotBefore"),
           "throttled, but no retryNotBefore deadline was stamped")
        ok(held.get("syncRequestedAck") != held.get("syncRequestedAt"),
           "a throttled run acknowledged the request it never served (%r)"
           % held.get("syncRequestedAck"))

        faults([{"match": "GET /api/auth.test", "status": [401],
                 "body": {"ok": False, "error": "invalid_auth"}}])
        time.sleep(2)   # the Retry-After the 429 named was one second
        sync_now()
        broke = wait_for_state("erroring")
        ok(broke.get("syncState") == "erroring",
           "an injected 401 left syncState %r, not erroring"
           % broke.get("syncState"))
        ok(broke.get("syncError"), "erroring, but syncError is empty")
        ok(broke.get("syncErrorAt"), "erroring, but syncErrorAt is unset")
        ok(str(broke.get("syncStatus") or "").startswith("erroring"),
           "the trait says erroring and syncStatus says %r"
           % broke.get("syncStatus"))
        broken_streams = broke.get("syncStreams") or {}
        ok(all((broken_streams.get(n) or {}).get("state") == "erroring"
               for n in want_streams),
           "a failure before any phase ran left the streams at %r"
           % {n: (broken_streams.get(n) or {}).get("state")
              for n in sorted(want_streams)})
        ok(broke.get("syncRequestedAck") != broke.get("syncRequestedAt"),
           "a failed run acknowledged the request it never served (%r)"
           % broke.get("syncRequestedAck"))

        # RECOVERY. Both rules are spent, so the next run is an ordinary one.
        faults([])
        sync_now()
        back = wait_for_sync(broke.get("lastSyncedAt"))
        ok(back.get("syncState") == "ok",
           "after the injected failures syncState is %r" % back.get("syncState"))
        ok(back.get("syncRequestedAck") == back.get("syncRequestedAt"),
           "the recovered run left syncRequestedAck %r against request %r"
           % (back.get("syncRequestedAck"), back.get("syncRequestedAt")))
        ok(back.get("syncError"),
           "syncError was cleared by a later success — it is the LAST error, "
           "and syncState is what says the run is over")

    # --------------------------------------------------------------- 16
    section("16. new history lands while a backlog drains")
    # substrate#650. A bounded run resumes its phase, and a walk past
    # `history` used to reach it again only after the whole backlog drained:
    # 11,912 queued threads and files held new messages back for days. The
    # mock builds the backlog: one injected history page names THREADS thread
    # parents, more than two runs' call budget (300 each) can walk, and every
    # conversations.replies call answers an empty page. The run after that
    # meets a new message in the same channel. On the unfixed body that run
    # spends its whole budget in `replies` and never asks for history.
    if MOCK and not SEED:
        THREADS = 1000
        base = settle()
        conv_cid = {rid(r): props(r).get("conversationId") for r in records(CONV)}
        cursors16 = {conv_cid.get(ref_id(props(s).get("conversation"))):
                     props(s).get("latestTs") for s in records(SYNC)}
        target = sorted(c for c, ts in cursors16.items() if c and ts)
        ok(not pending(base), "the account still had work pending before the "
                              "backlog was built: %r" % base.get("syncStatus"))
        ok(target, "no conversation has a stored latestTs to pull new history from")
        if target:
            cid = target[0]
            start = float(cursors16[cid]) + 10
            parents = []
            for i in range(THREADS):
                ts = "%.6f" % (start + i * 0.001)
                parents.append({"type": "message", "user": owner, "ts": ts,
                                "text": "backlog thread %d" % i, "thread_ts": ts,
                                "reply_count": 1,
                                "latest_reply": "%.6f" % (start + i * 0.001 + 0.0005)})
            new_ts = "%.6f" % (start + THREADS)
            text = "posted while the replies backlog drains"
            replies = {"match": "GET /api/conversations.replies",
                       "status": [200] * (THREADS + 200),
                       "body": {"ok": True, "has_more": False, "messages": []}}

            def history(messages):
                return {"match": "GET /api/conversations.history",
                        "contains": "channel=" + cid, "status": [200],
                        "body": {"ok": True, "has_more": False,
                                 "messages": messages}}

            # Run one: a new walk takes the parents and stops in `replies`.
            faults([history(parents), replies])
            one = run_once()
            first = one.get("streamCursors") or {}
            ok(first.get("phase") == "replies"
               and len(first.get("threads") or []) > 300,
               "the first run did not leave a replies backlog bigger than one "
               "run (phase %r, %d threads), so this proves nothing"
               % (first.get("phase"), len(first.get("threads") or [])))
            # Run two: resumes the backlog, with a new message waiting.
            faults([history([{"type": "message", "user": owner, "text": text,
                              "ts": new_ts}]), replies])
            two = run_once()
            left = two.get("streamCursors") or {}
            ok(left.get("phase") == "replies" and left.get("threads"),
               "the second run did not stop in the backlog (phase %r, %d "
               "threads left), so this proves nothing"
               % (left.get("phase"), len(left.get("threads") or [])))
            ok(len(left.get("threads") or []) < len(first.get("threads") or []),
               "the second run spent nothing on the backlog: %d threads before, "
               "%d after" % (len(first.get("threads") or []),
                             len(left.get("threads") or [])))
            conv_row = {v: k for k, v in conv_cid.items()}.get(cid)
            landed = [r for r in records(MESSAGE)
                      if props(r).get("ts") == new_ts
                      and ref_id(props(r).get("channel")) == conv_row]
            ok(landed, "the message posted at %s in %s is not in the mirror "
                       "after one run with a replies backlog (%s)"
               % (new_ts, cid, two.get("syncStatus")))
            note("backlog: %d threads after run one, %d after run two; new "
                 "message %s" % (len(first.get("threads") or []),
                                 len(left.get("threads") or []),
                                 "landed" if landed else "MISSING"))
            faults([replies])
            drained = wait_for_sync(two.get("lastSyncedAt"), seconds=600)
            ok(not pending(drained), "the backlog did not drain afterwards: %r"
               % drained.get("syncStatus"))
            faults([])

    section("17. postmessage posts through the pasted token")
    postmessage()
    return finish()


POST_FN = P + "/postmessage"
POST_ROUTE = "/api/chat.postMessage"


def postmessage():
    """#644. The bundle's one write: a callable, fired by nobody but its
    caller, that spends the config's token on `chat.postMessage` and writes
    no record. The recording names the conversation and the thread parent,
    so the call is built from it and checked against the mirror first."""
    if not MOCK or SEED:
        seed_note("postmessage is driven against the mock only")
        return
    from writecall import Writes, error_text, form
    rec = load("POST_api_chat.postMessage.json") or {}
    channel = rec.get("channel")
    parent = (rec.get("message") or {}).get("thread_ts")
    if not ok(channel and parent, "the postMessage recording names no channel "
                                  "and thread parent"):
        return
    convs = {props(r).get("conversationId"): rid(r) for r in records(CONV)}
    ok(channel in convs, "the recording's conversation %s is not mirrored" % channel)
    ok(any(props(m).get("ts") == parent
           and ref_id(props(m).get("channel")) == convs.get(channel)
           for m in records(MESSAGE)),
       "the thread parent %s is not a mirrored message in %s" % (parent, channel))
    w = Writes(SERVER, TOKEN, MOCK)
    w.reset()

    text = "On it, thanks."
    st, reply = w.call(POST_FN, {"channel": channel, "text": text,
                                 "threadTs": parent})
    ok(st == 200, "a threaded reply answered %s: %s" % (st, error_text(reply)[:300]))
    out = (reply or {}).get("output") or {} if st == 200 else {}
    ok(out == {"channel": channel, "ts": rec.get("ts")},
       "the reply's output is %r, want the recording's channel and ts" % out)
    ok((reply or {}).get("effects") == 0,
       "postmessage wrote %r records; it writes none" % (reply or {}).get("effects"))
    sent = w.requests("POST", POST_ROUTE)
    ok(len(sent) == 1, "%d chat.postMessage requests reached Slack, want 1" % len(sent))
    if sent:
        ok(form(sent[0]) == {"channel": channel, "text": text, "thread_ts": parent},
           "the form sent was %r" % form(sent[0]))
        ok(str(sent[0].get("auth") or "").startswith("Bearer xoxp-"),
           "the token rode the Authorization header (%r)" % sent[0].get("auth"))
        ok("token" not in form(sent[0]), "the token was sent in the form")

    st, reply = w.call(POST_FN, {"channel": channel,
                                 "text": "Posted to the channel."})
    ok(st == 200, "a top-level post answered %s: %s" % (st, error_text(reply)[:300]))
    sent = w.requests("POST", POST_ROUTE)
    ok(len(sent) == 2 and "thread_ts" not in form(sent[-1]),
       "a post with no threadTs sent a thread_ts (%r)" % (sent[-1:] or None))

    w.faults([{"match": "POST " + POST_ROUTE, "status": [200],
               "body": {"ok": False, "error": "not_in_channel"}}])
    st, reply = w.call(POST_FN, {"channel": channel, "text": text})
    ok(st >= 400 and "not_in_channel" in error_text(reply),
       "Slack's refusal did not reach the caller: %s %s"
       % (st, error_text(reply)[:300]))
    w.faults([])

    before = len(w.requests("POST", POST_ROUTE))
    st, reply = w.call(POST_FN, {"channel": channel})
    ok(400 <= st < 500, "a call with no text answered %s, want a 4xx" % st)
    ok(len(w.requests("POST", POST_ROUTE)) == before,
       "a call with no text still reached Slack")

    # The origin pin: an apiBase off slack.com refuses before any request.
    # The value it had is put back, not cleared: a scenario-only run reaches
    # the mock through it.
    cfg = "/api/v1/%s/default" % os.environ.get("CONFIG_KIND", P + "/config")
    had = props(api("GET", cfg)[1] or {}).get("apiBase")
    st, _ = api("PATCH", cfg, {"properties": {"apiBase": "https://slack.example.com"}})
    if ok(st < 400, "could not point apiBase off Slack: %s" % st):
        st, reply = w.call(POST_FN, {"channel": channel, "text": text})
        ok(st >= 400 and "not a pinned origin" in error_text(reply),
           "an apiBase off slack.com did not refuse: %s %s"
           % (st, error_text(reply)[:200]))
        ok(len(w.requests("POST", POST_ROUTE)) == before,
           "the refused call still reached the mock")
        api("PATCH", cfg, {"properties": {"apiBase": had}})
    note("postmessage: %d calls reached the mock"
         % len(w.requests("POST", POST_ROUTE)))


def finish():
    print()
    if problems:
        # Grouped by the section that raised them, and every message carries
        # its MEASURED value. A bare "!! 10 of 73 checks failed" at the end of
        # a 12,000-line seed log tells whoever reads it nothing at all.
        print("\033[31m !! %d of %d checks failed (mode %s)\033[0m"
              % (len(problems), checks, MODE))
        last = None
        for sect, msg in problems:
            if sect != last:
                print("   \033[1m%s\033[0m" % sect)
                last = sect
            print("     - %s" % msg)
        print("\n   grep this log for the section heading above each failure "
              "to see what it measured.")
        return 1
    print("\033[32m == all %d checks passed (mode %s)\033[0m" % (checks, MODE))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
