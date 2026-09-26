#!/usr/bin/env python3
"""The Beeper provider's end-to-end assertions.

Run by `tools/e2e.py` after it has installed the bundle, written the token on
the config, connected the account and waited for the first sync to settle.
Everything here is derived from THE RECORDINGS the mock served, never from
hard-coded ids, so the same file asserts the e2e run (the
pseudonymised cut) and a seed run over the owner\'s own pull (mneme tree only) (the owner's own
pull) without knowing which it is looking at.

WHAT IT ASSERTS, and why each one is worth a run:

    1. account    the connection's own identity arrived as a REFERENCE that
                  resolves to a user row with `isSelf`, and the Beeper Desktop
                  build that answered is on the row
    2. counts     every kind the recordings imply has its rows, and the ids
                  are the API's own, distinct and derivable
    3. shape      no row carries `raw`, an owner flag, a consumer slot or the
                  API's `…ID` beside the reference that replaced it
    4. references chat, sender, chatAccount, bridge, participants, mentions,
                  reactions[].participant, merge.chats and linkedMessage all
                  resolve to rows of the kind they pin
    5. enums      every closed set is spelled by the guide's rule, and a
                  message the API sent with NO type is mirrored without one
                  rather than refused
    6. time       every message carries `at`, and it is its `timestamp`; a
                  CHAT carries `lastActivity` and NO `at`, because that
                  instant is a mutation mark and binds no trait
    7. roster     the authoritative read replaced the list's truncated one:
                  the big group holds every participant it says it has
    8. state      the cursors live on `chatsync`, and a chat row's own version
                  did NOT move while a drain wrote its history
    9. owner      the owner-relative facts are DATA, not flags: a message
                  whose `sender` IS the account's user, found by reference
   10. thin       a merged chat's member chats are minted thin, so
                  `merge.chats` resolves even though /v1/chats hides them
   11. again      a second sync adds no duplicate row, advances each cursor,
                  and lands the message only the FORWARD page carries
   12. requests   the pages and directions the sync actually asked for, and
                  that NOTHING was answered by a relaxed or fallback match
   13. trait      the core `sync` trait (decision 0085): a finished walk is
                  `ok` with both streams acked against the request that drove
                  it, an injected 429 is `throttled`, an injected 401 is
                  `erroring` with the cause, and the next run recovers
   14. write      `sendmessage` sends through the config's token: the body it
                  sends, its output, no record written, Beeper's refusal
                  surfaced, and a public apiBase refused unsent

Run against `raw/beeper` (MODE=seed) two families of check relax, and only
two; both are commented "SEED:" at the site and described in
providers/beeper/README.md under "The seed run, and what it may relax".
"""

from __future__ import annotations

import collections
import datetime as _dt
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
                              "providers.substrate.reamde.dev/beeper/account")
ACCOUNT_ID = os.environ.get("ACCOUNT_ID", "e2e")
RECORDINGS = pathlib.Path(os.environ["RECORDINGS"])

P = "providers.substrate.reamde.dev/beeper"
ACCOUNT, BRIDGE = P + "/account", P + "/bridge"
CHATACCOUNT, USER, LABEL = P + "/chataccount", P + "/user", P + "/label"
CHAT, MESSAGE, CHATSYNC = P + "/chat", P + "/message", P + "/chatsync"

# the e2e run runs MODE=e2e against the pseudonymised cut;
# a seed run over the owner\'s own pull (mneme tree only) runs MODE=seed against raw/beeper, which
# is the owner's own pull and is NOT a curated set. Two things differ:
#
#   * a raw pull is INCOMPLETE BY CONSTRUCTION. It holds what the puller
#     reached — 17 chats' history out of 476 listed — so the mirror is
#     compared against the chats the sync actually FINISHED rather than
#     against every chat the list happens to hold, and a call with no
#     recording at all is the pull's gap rather than the sync's bug.
#   * a raw pull carries no FABRICATED seams. The forward page, the terminal
#     page and the merged chat's synthesised read are cut by fixtures/
#     curate.py and cannot exist in a recording of the live API, so the checks
#     that need them assert only when they are present — and are REQUIRED in
#     e2e, so the coverage cannot quietly disappear.
#
# Nothing else is relaxed. Every relaxation below says so in a comment
# beginning "SEED:".
SEED = MODE != "e2e"

# The enum tables the bundle declares, API label -> declared value. The
# scenario carries them so a spelling that drifts in one place fails here.
MESSAGE_TYPES = {"TEXT": "text", "NOTICE": "notice", "IMAGE": "image",
                 "VIDEO": "video", "VOICE": "voice", "AUDIO": "audio",
                 "FILE": "file", "STICKER": "sticker", "LOCATION": "location",
                 "REACTION": "reaction"}
CHAT_TYPES = {"single": "single", "group": "group"}
PROVIDERS = {"cloud": "cloud", "self-hosted": "selfhosted", "local": "local",
             "platform-sdk": "platformsdk"}
ACCOUNT_STATUS = {"connected": "connected", "connecting": "connecting",
                  "backfilling": "backfilling",
                  "connection_required": "connectionrequired",
                  "reconnect_required": "reconnectrequired",
                  "attention_required": "attentionrequired",
                  "disconnected": "disconnected", "disabled": "disabled"}
BRIDGE_STATUS = {"available": "available", "connected": "connected",
                 "limit_reached": "limitreached",
                 "temporarily_unavailable": "temporarilyunavailable",
                 "disabled": "disabled"}

# The API's own names, which a mirror row must NOT carry beside the reference
# that replaced them — plus the two classes the guide forbids outright.
FORBIDDEN = {
    "raw", "person", "task", "episode",
    "chatID", "senderID", "participantID", "linkedMessageID",
    "mergedIntoChatID", "defaultChatID", "accountID", "userID",
    "fromOwner", "mentionsOwner", "isOwner", "isDirect", "isGroup",
    "messageCount", "participantCount", "reactionCount",
    # sync bookkeeping, which belongs on the account or on chatsync
    "oldestCursor", "newestCursor", "backfillComplete", "syncStatus",
}
# ...except where the kind legitimately declares one of those names.
FORBIDDEN_EXEMPT = {
    CHATSYNC: {"oldestCursor", "newestCursor", "backfillComplete"},
    ACCOUNT: {"syncStatus"},
}

problems: list[tuple[str, str]] = []
checks = 0
_section = "0. startup"


def ok(cond, msg):
    global checks
    checks += 1
    if not cond:
        problems.append((_section, msg))
    return bool(cond)


def section(name):
    global _section
    _section = name
    print("\n\033[1m--- %s\033[0m" % name, flush=True)


def note(msg):
    print("    %s" % msg, flush=True)


def seed_note(msg):
    """A check the seed run deliberately does not make. Printed either way, so
    the relaxation is visible in the log rather than implied by silence."""
    note(("SEED RELAXATION: " if SEED else "(e2e asserts this) ") + msg)


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


def records(kind, limit=20000):
    out, after = [], ""
    while True:
        q = {"filter": json.dumps({"kinds": [kind]}), "first": "500"}
        if after:
            q["after"] = after
        st, body = api("GET", "/api/v1/records?" + urllib.parse.urlencode(q))
        if st >= 400:
            problems.append((_section, "records %s: HTTP %s %s"
                             % (kind, st, json.dumps(body)[:200])))
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
    for src in (row, row.get("metadata") or {}, row.get("status") or {}):
        for key in ("version", "recordVersion", "seq"):
            if src.get(key) is not None:
                return src[key]
    return None


def ref_id(value):
    """A reference is `{"ref": "<kind>/<id>"}` on the wire — with its link
    properties beside `ref` where it declares any — and a bare path where a
    document authored it short."""
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
        if doc is not None:
            yield f.name, doc


def expected():
    """What the recordings say the mirror must hold. Everything below is read
    off the served payloads, so the same code describes the cut and the
    owner's pull."""
    info = load("GET_v1_info.json") or {}
    accounts = load("GET_v1_accounts.json")
    accounts = accounts if isinstance(accounts, list) else \
        (accounts or {}).get("items") or []
    bridges = ((load("GET_v1_bridges.json") or {}).get("items")) or []
    labels = load("GET_v1_labels.json")
    labels = labels if isinstance(labels, list) else (labels or {}).get("items") or []

    users: dict[str, dict] = {}

    def see_user(u):
        if isinstance(u, dict) and u.get("id"):
            users.setdefault(u["id"], u)

    chataccounts: dict[str, dict] = {}
    for a in accounts:
        if a.get("accountID"):
            chataccounts[a["accountID"]] = a
        see_user(a.get("user"))
    for b in bridges:
        for a in b.get("accounts") or []:
            if a.get("accountID"):
                chataccounts.setdefault(a["accountID"], a)
            see_user(a.get("user"))
    for _n, doc in each("GET_v1_accounts_*_contacts_list__*"):
        for u in doc.get("items") or []:
            see_user(u)

    chats: dict[str, dict] = {}
    listed: dict[str, dict] = {}
    for _n, doc in each("GET_v1_chats__*"):
        for c in doc.get("items") or []:
            if c.get("id"):
                listed[c["id"]] = c
                chats[c["id"]] = c
    whole: dict[str, dict] = {}
    for _n, doc in each("GET_v1_chats_*__maxParticipantCount--1.json"):
        if isinstance(doc, dict) and doc.get("id"):
            whole[doc["id"]] = doc
            chats[doc["id"]] = doc
    for c in chats.values():
        for p in ((c.get("participants") or {}).get("items") or []):
            see_user(p)

    # A message arrives from three places, and they are ONE kind: a chat's
    # `preview`, the backfill pages, and the forward page.
    messages: dict[tuple[str, str], dict] = {}
    forward: dict[tuple[str, str], dict] = {}
    backfill_pages, forward_pages = 0, 0
    for c in listed.values():
        pv = c.get("preview")
        if isinstance(pv, dict) and pv.get("id"):
            messages[(pv.get("chatID") or c["id"], pv["id"])] = pv
    for name, doc in each("GET_v1_chats_*_messages*.json"):
        after = "direction-after" in name
        if after:
            forward_pages += 1
        else:
            backfill_pages += 1
        for m in doc.get("items") or []:
            if not m.get("id") or not m.get("chatID"):
                continue
            key = (m["chatID"], m["id"])
            messages[key] = m
            if after:
                forward[key] = m
            see_user({"id": m.get("senderID")})
            for r in m.get("reactions") or []:
                see_user({"id": r.get("participantID")})
            for uid in m.get("mentions") or []:
                if uid and uid != "@room":
                    see_user({"id": uid})
    return {
        "info": info, "chataccounts": chataccounts, "bridges": bridges,
        "labels": labels, "users": users, "chats": chats, "listed": listed,
        "whole": whole, "messages": messages, "forward": forward,
        "backfillPages": backfill_pages, "forwardPages": forward_pages,
    }


# ------------------------------------------------------------ driving a sync


def account_row():
    # The record door is `/api/v1/<kind>/<id>`, exactly as tools/e2e.py's own
    # `account_props` spells it; `/api/v1/records/...` is the LIST door and
    # 404s on a single record.
    st, body = api("GET", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID))
    return body if st < 400 else {}


def resync(label, timeout=240):
    """Ask for a sync now — the same `syncRequestedAt` door the console's
    button uses — and wait for the account's own stamp to move past it."""
    before = props(account_row()).get("lastSyncedAt") or ""
    stamp = time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime()) + ".000000Z"
    st, body = api("PATCH", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID),
                   {"properties": {"syncRequestedAt": stamp}})
    if st >= 400:
        problems.append((_section, "%s: syncRequestedAt refused: HTTP %s %s"
                         % (label, st, json.dumps(body)[:200])))
        return False
    deadline = time.time() + timeout
    while time.time() < deadline:
        time.sleep(4)
        p = props(account_row())
        after = p.get("lastSyncedAt") or ""
        if after and after > before and not str(p.get("syncStatus") or "") \
                .startswith("erroring"):
            if "pending" not in str(p.get("syncStatus") or ""):
                note("%s: %s" % (label, p.get("syncStatus")))
                return True
    note("%s: timed out waiting for the stamp to move (status %r)"
         % (label, props(account_row()).get("syncStatus")))
    return False


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


def request_sync():
    """Stamp `syncRequestedAt` and return it — the same door the console's
    Sync now button uses. SUB-SECOND, because the on-demand guard is
    `syncRequestedAt > lastSyncedAt` and the body stamps sub-second too."""
    stamp = _dt.datetime.now(_dt.timezone.utc).isoformat()
    api("PATCH", "/api/v1/%s/%s" % (ACCOUNT_KIND, ACCOUNT_ID),
        {"properties": {"syncRequestedAt": stamp}})
    return stamp


def wait_for_state(want, seconds=120):
    """Wait for the account's `syncState` to reach a word.

    NOT `resync`: a failed run does not clear its queues, so waiting for the
    stamp to move past a clean status would wait for ever."""
    deadline = time.time() + seconds
    p = {}
    while time.time() < deadline:
        p = props(account_row())
        if p.get("syncState") == want:
            return p
        time.sleep(2)
    return p


# ------------------------------------------------------------------ the run


def main() -> int:
    exp = expected()
    note("recordings: %d chats listed, %d read whole, %d messages, "
         "%d users, %d chat accounts, %d bridges, %d labels"
         % (len(exp["listed"]), len(exp["whole"]), len(exp["messages"]),
            len(exp["users"]), len(exp["chataccounts"]), len(exp["bridges"]),
            len(exp["labels"])))

    # ---------------------------------------------------------------- 1
    section("1. the account: whose Beeper this is")
    acct = account_row()
    ap = props(acct)
    ok(ap.get("lastSyncedAt"), "the account carries no lastSyncedAt — the sync "
                               "never stamped itself")
    ok(not str(ap.get("syncStatus") or "").startswith("erroring"),
       "the account is erroring: %r" % ap.get("syncStatus"))
    ok(ap.get("serverVersion"),
       "the account carries no serverVersion — /v1/info was never read")
    if exp["info"]:
        want = ((exp["info"].get("app") or {}).get("version"))
        ok(ap.get("serverVersion") == want,
           "serverVersion is %r, /v1/info said %r"
           % (ap.get("serverVersion"), want))

    users = {rid(r): r for r in records(USER)}
    owner_ref = ref_id(ap.get("user"))
    ok(owner_ref, "the account carries no `user` reference — the connection's "
                  "own identity is a reference, never a copied id")
    owner = users.get(owner_ref) or {}
    ok(owner, "account.user points at %r, which is not a mirrored user row"
       % owner_ref)
    ok(props(owner).get("isSelf") is True,
       "account.user resolves to a row whose isSelf is not true: %r"
       % props(owner).get("isSelf"))

    # ---------------------------------------------------------------- 2
    section("2. one row per payload, keyed on the API's own id")
    rows = {
        CHAT: {rid(r): r for r in records(CHAT)},
        MESSAGE: {rid(r): r for r in records(MESSAGE)},
        USER: users,
        CHATACCOUNT: {rid(r): r for r in records(CHATACCOUNT)},
        BRIDGE: {rid(r): r for r in records(BRIDGE)},
        LABEL: {rid(r): r for r in records(LABEL)},
        CHATSYNC: {rid(r): r for r in records(CHATSYNC)},
    }
    for kind, got in rows.items():
        note("%-14s %d rows" % (kind.rsplit("/", 1)[-1], len(got)))

    by_chat_id = {props(r).get("chatId"): r for r in rows[CHAT].values()}
    by_msg = {(ref_id(props(r).get("chat")), props(r).get("messageId")): r
              for r in rows[MESSAGE].values()}
    by_user_id = {props(r).get("userId"): r for r in rows[USER].values()}
    by_ca_id = {props(r).get("chatAccountId"): r
                for r in rows[CHATACCOUNT].values()}
    by_bridge_id = {props(r).get("bridgeId"): r for r in rows[BRIDGE].values()}

    missing_chats = [c for c in exp["chats"] if c not in by_chat_id]
    ok(not missing_chats, "%d chats the recordings carry are not mirrored: %s"
       % (len(missing_chats), [c[:14] for c in missing_chats[:3]]))
    missing_ca = [c for c in exp["chataccounts"] if c not in by_ca_id]
    ok(not missing_ca, "%d chat accounts are not mirrored: %s"
       % (len(missing_ca), missing_ca[:3]))
    missing_bridges = [b.get("id") for b in exp["bridges"]
                       if b.get("id") not in by_bridge_id]
    ok(not missing_bridges, "%d bridges are not mirrored: %s"
       % (len(missing_bridges), missing_bridges[:3]))
    missing_labels = [x.get("id") for x in exp["labels"]
                      if x.get("id") not in
                      {props(r).get("labelId") for r in rows[LABEL].values()}]
    ok(not missing_labels, "%d labels are not mirrored: %s"
       % (len(missing_labels), missing_labels[:3]))
    ok(len(by_chat_id) == len(rows[CHAT]),
       "two chat rows share a chatId — the key is not the upstream's")
    ok(len(by_user_id) == len(rows[USER]),
       "two user rows share a userId — the key is not the upstream's")

    # T-047: a bridge hands the number over the way the contact saved it, so
    # `phoneNumber` is the API's own `string` and nothing may drop one — the
    # person mapping's `phoneNumber -> phones` is what a phone-only contact
    # has instead of a name.
    want_phone = {uid: u["phoneNumber"] for uid, u in exp["users"].items()
                  if u.get("phoneNumber")}
    lost = sorted(uid for uid, num in want_phone.items()
                  if not props(by_user_id.get(uid) or {}).get("phoneNumber"))
    ok(not lost, "%d of %d users the recordings give a phone number do not "
                 "carry one: %s" % (len(lost), len(want_phone),
                                    [u[:18] for u in lost[:3]]))
    verbatim = [uid for uid, num in want_phone.items()
                if props(by_user_id.get(uid) or {}).get("phoneNumber") == num]
    ok(len(verbatim) == len(want_phone) - len(lost),
       "a phone number was rewritten on the way in; it is a string and goes "
       "through as the API sent it")
    note("%d users carry a phone number, %d of them not in E.164 form"
         % (len(want_phone) - len(lost),
            sum(1 for n in want_phone.values()
                if any(c in n for c in " -()."))))

    # A message is keyed (chat, id), which is what makes a per-install counter
    # safe as an id: two chats may both hold a message called `1`.
    chat_of_row = {k: props(v).get("chatId") for k, v in rows[CHAT].items()}
    mirrored = {(chat_of_row.get(ref_id(props(r).get("chat"))),
                 props(r).get("messageId")) for r in rows[MESSAGE].values()}
    # A message that ONLY the forward page carries cannot be here yet: it is
    # what the second sync exists to discover (section 11).
    want_msgs = set(exp["messages"]) - set(exp["forward"])
    # SEED: a raw pull holds pages for the chats the puller reached, and the
    # sync walks every chat the list carries; the messages it FINISHED are the
    # ones the pull holds, so the comparison is one-directional there.
    missing_msgs = sorted(want_msgs - mirrored)
    ok(not missing_msgs, "%d messages the recordings carry are not mirrored: "
                         "%s" % (len(missing_msgs),
                                 [(c or "")[:12] + "/" + m
                                  for c, m in missing_msgs[:3]]))
    extra = sorted(mirrored - want_msgs)
    if SEED:
        seed_note("%d mirrored messages are not in the served pages" % len(extra))
    else:
        ok(not extra, "%d mirrored messages no recording carries: %s"
           % (len(extra), [(c or "")[:12] + "/" + m for c, m in extra[:3]]))

    # ---------------------------------------------------------------- 3
    section("3. shape: nothing raw, nothing owner-relative, nothing doubled")
    # A `json` property's interior is the API's payload, not this bundle's
    # declaration: Beeper's own `capabilities.reactionCount` says how many
    # reactions one person may leave, and it is not a count this mirror took.
    OPAQUE_PROPERTIES = {"capabilities", "seen"}

    def every_key(node):
        """Every property name at EVERY depth — an object field and a repeated
        object's field carry the same rules as a top-level property, and a
        top-level-only scan proved nothing about them."""
        if isinstance(node, dict):
            for k, v in node.items():
                if k != "ref":            # a reference's own envelope key
                    yield k
                if k not in OPAQUE_PROPERTIES:
                    yield from every_key(v)
        elif isinstance(node, list):
            for v in node:
                yield from every_key(v)

    for kind, got in rows.items():
        exempt = FORBIDDEN_EXEMPT.get(kind, set())
        bad = sorted({k for r in got.values() for k in every_key(props(r))
                      if k in FORBIDDEN and k not in exempt})
        ok(not bad, "%s rows carry forbidden properties at some depth: %s"
           % (kind.rsplit("/", 1)[-1], bad))
    # The declaration itself, not just the data.
    st, kinds = api("GET", "/api/v1/records?" + urllib.parse.urlencode(
        {"filter": json.dumps({"kinds": ["substrate.reamde.dev/core/kind"]}),
         "first": "500"}))
    declared_bad = []
    for k in ((kinds or {}).get("records") or []):
        kid = (k.get("metadata") or {}).get("id") or ""
        if not kid.startswith(P):
            continue
        exempt = FORBIDDEN_EXEMPT.get(kid, set())
        for name in (k.get("data") or {}).get("properties") or {}:
            if name in FORBIDDEN and name not in exempt:
                declared_bad.append("%s.%s" % (kid.rsplit("/", 1)[-1], name))
    ok(not declared_bad, "the bundle DECLARES forbidden properties: %s"
       % declared_bad[:5])

    # ---------------------------------------------------------------- 4
    section("4. every relation is a reference, and every reference resolves")
    dangling = []
    mispinned = []

    def ref_paths(value):
        if value is None:
            return []
        vals = value if isinstance(value, list) else [value]
        out = []
        for v in vals:
            if isinstance(v, dict) and v.get("ref"):
                out.append(str(v["ref"]))
            elif isinstance(v, str) and v:
                out.append(v)
        return out

    def resolves(value, table, where, kind=None):
        for r in ref_ids(value):
            if r not in table:
                dangling.append("%s -> %s" % (where, r[:16]))
        # A reference resolves to a row of the kind it PINS, and the wire
        # carries the kind in the path — so the check is on the path, not on
        # the id alone (Codex review).
        if kind:
            for path in ref_paths(value):
                if "/" in path and not path.startswith(kind + "/"):
                    mispinned.append("%s -> %s" % (where, path[:48]))

    for r in rows[MESSAGE].values():
        p = props(r)
        resolves(p.get("chat"), rows[CHAT], "message.chat", CHAT)
        resolves(p.get("sender"), rows[USER], "message.sender", USER)
        resolves(p.get("chatAccount"), rows[CHATACCOUNT], "message.chatAccount", CHATACCOUNT)
        resolves(p.get("mentions"), rows[USER], "message.mentions", USER)
        resolves(p.get("linkedMessage"), rows[MESSAGE], "message.linkedMessage", MESSAGE)
        for reaction in p.get("reactions") or []:
            resolves(reaction.get("participant"), rows[USER],
                     "message.reactions[].participant")
        for uid in ((p.get("sendStatus") or {}).get("deliveredToUsers") or []):
            resolves(uid, rows[USER], "message.sendStatus.deliveredToUsers")
    for r in rows[CHAT].values():
        p = props(r)
        resolves(p.get("chatAccount"), rows[CHATACCOUNT], "chat.chatAccount", CHATACCOUNT)
        resolves(p.get("participants"), rows[USER], "chat.participants", USER)
        resolves(p.get("labels"), rows[LABEL], "chat.labels", LABEL)
        resolves(p.get("mergedIntoChat"), rows[CHAT], "chat.mergedIntoChat", CHAT)
        merge = p.get("merge") or {}
        resolves(merge.get("chats"), rows[CHAT], "chat.merge.chats", CHAT)
        resolves(merge.get("defaultChat"), rows[CHAT], "chat.merge.defaultChat", CHAT)
    for r in rows[CHATACCOUNT].values():
        p = props(r)
        resolves(p.get("bridge"), rows[BRIDGE], "chataccount.bridge", BRIDGE)
        resolves(p.get("user"), rows[USER], "chataccount.user", USER)
    for r in rows[BRIDGE].values():
        resolves(props(r).get("accounts"), rows[CHATACCOUNT], "bridge.accounts", CHATACCOUNT)
    for r in rows[CHATSYNC].values():
        resolves(props(r).get("chat"), rows[CHAT], "chatsync.chat", CHAT)
    ok(not dangling, "%d references resolve to nothing: %s"
       % (len(dangling), dangling[:5]))
    ok(not mispinned, "%d references name a kind other than the one they pin: "
                      "%s" % (len(mispinned), mispinned[:5]))
    previews = [r for r in rows[CHAT].values() if props(r).get("preview")]
    resolves([props(r).get("preview") for r in previews], rows[MESSAGE],
             "chat.preview", MESSAGE)
    note("%d chats point at their newest message" % len(previews))

    # The membership's own flags ride on the LINK, not on the person.
    linked = [(rid(r), v) for r in rows[CHAT].values()
              for v in (props(r).get("participants") or [])
              if isinstance(v, dict)
              and any(k in v for k in ("isAdmin", "isPending", "isNetworkBot"))]
    person_flags = [rid(r) for r in rows[USER].values()
                    if any(k in props(r)
                           for k in ("isAdmin", "isPending", "isNetworkBot"))]
    ok(not person_flags,
       "%d user rows carry a MEMBERSHIP's flags: %s"
       % (len(person_flags), person_flags[:3]))
    note("%d participant links carry membership flags" % len(linked))

    # ---------------------------------------------------------------- 5
    section("5. enums, spelled by the rule")
    bad_enum = []
    for r in rows[MESSAGE].values():
        want = MESSAGE_TYPES.get((exp["messages"].get(
            (chat_of_row.get(ref_id(props(r).get("chat"))),
             props(r).get("messageId"))) or {}).get("type"))
        got = props(r).get("type")
        if want and got != want:
            bad_enum.append("message %s: %r, wanted %r"
                            % (props(r).get("messageId"), got, want))
    ok(not bad_enum, "%d messages are spelled wrong: %s"
       % (len(bad_enum), bad_enum[:3]))
    untyped_src = [k for k, m in exp["messages"].items() if m.get("type") is None]
    if untyped_src:
        untyped = [k for k in untyped_src
                   if "type" not in props(by_msg.get(k) or {})]
        ok(len(untyped) == len(untyped_src),
           "a message the API sent with NO type was given one: %s"
           % [k[1] for k in untyped_src[:3]])
        note("%d messages arrived with no `type` at all and are mirrored "
             "without one" % len(untyped_src))
    else:
        seed_note("no message in this set arrived without a `type`")
    for r in rows[CHAT].values():
        t = props(r).get("type")
        if t is not None:
            ok(t in CHAT_TYPES.values(), "chat.type is %r" % t)
    for r in rows[CHATACCOUNT].values():
        s = props(r).get("status")
        if s is not None:
            ok(s in ACCOUNT_STATUS.values(), "chataccount.status is %r" % s)
    for r in rows[BRIDGE].values():
        for name, table in (("provider", PROVIDERS), ("status", BRIDGE_STATUS)):
            v = props(r).get(name)
            if v is not None:
                ok(v in table.values(), "bridge.%s is %r" % (name, v))

    # ---------------------------------------------------------------- 6
    section("6. time: a message has an instant of its own, a chat does not")
    wrong_at, no_at = [], []
    for key, m in exp["messages"].items():
        row = by_msg.get(key)
        if not row:
            continue
        at = props(row).get("at") or (row.get("status") or {}).get("at")
        if not at:
            no_at.append(key[1])
            continue
        if m.get("timestamp") and at[:19] != m["timestamp"][:19]:
            wrong_at.append("%s: %s vs %s" % (key[1], at, m["timestamp"]))
    ok(not no_at, "%d messages carry no `at`: %s" % (len(no_at), no_at[:3]))
    ok(not wrong_at, "%d messages' `at` is not their timestamp: %s"
       % (len(wrong_at), wrong_at[:3]))
    # THE V4 RULING (§4): a chat's only instant is a mutation mark, so the
    # kind binds no temporal trait and the instant is an ordinary property.
    chat_with_at = [rid(r) for r in rows[CHAT].values() if props(r).get("at")]
    ok(not chat_with_at,
       "%d chat rows carry `at` — a chat's lastActivity is a MUTATION mark "
       "and binds no trait (kind-design v4 §4): %s"
       % (len(chat_with_at), chat_with_at[:3]))
    with_last = [r for r in rows[CHAT].values() if props(r).get("lastActivity")]
    want_last = [c for c in exp["chats"].values() if c.get("lastActivity")]
    ok(len(with_last) >= len(want_last) - 1 if want_last else True,
       "%d chats carry lastActivity, %d recordings have one"
       % (len(with_last), len(want_last)))

    # ---------------------------------------------------------------- 7
    section("7. the authoritative read replaced the list's truncated roster")
    truncated = {cid: c for cid, c in exp["listed"].items()
                 if ((c.get("participants") or {}).get("hasMore"))}
    checked = 0
    for cid, c in truncated.items():
        row = by_chat_id.get(cid)
        if not row:
            continue
        whole = exp["whole"].get(cid)
        if not whole:
            continue
        got = len(ref_ids(props(row).get("participants")))
        want = len({p.get("id") for p in
                    ((whole.get("participants") or {}).get("items") or [])
                    if p.get("id")})
        total = props(row).get("participantsTotal")
        ok(got == want, "chat %s holds %d participants, the authoritative "
                        "read carried %d" % (cid[:14], got, want))
        ok("participantsHasMore" not in props(row),
           "chat %s carries participants.hasMore — that is the embedded "
           "list's pagination flag, not a fact about the chat" % cid[:14])
        ok(total == want or total is None,
           "chat %s says participantsTotal %r but holds %d"
           % (cid[:14], total, got))
        checked += 1
    if checked:
        note("%d chats the LIST truncated were replaced by the whole read"
             % checked)
    else:
        seed_note("no chat in this set had a truncated roster in the list")

    # ---------------------------------------------------------------- 8
    section("8. the cursors live on chatsync, not on the chat")
    cursored = [r for r in rows[CHATSYNC].values()
                if props(r).get("newestCursor") or props(r).get("oldestCursor")]
    ok(cursored, "no chatsync row carries a cursor — the state kind is unused")
    note("%d chatsync rows carry a cursor, %d say the backfill is complete"
         % (len(cursored), sum(1 for r in rows[CHATSYNC].values()
                               if props(r).get("backfillComplete"))))
    chat_versions = {k: version(v) for k, v in rows[CHAT].items()}
    msg_before = len(rows[MESSAGE])
    msg_props_before = {(ref_id(props(r).get("chat")), props(r).get("messageId")):
                        props(r) for r in rows[MESSAGE].values()}

    # ---------------------------------------------------------------- 9
    section("9. owner-relative facts are DATA, not columns")
    mine = [r for r in rows[MESSAGE].values()
            if ref_id(props(r).get("sender")) == owner_ref]
    ok(mine, "no message resolves its sender to the account's own user — "
             "'from me' is a reference filter and nothing proves it here")
    note("%d messages were sent by the account's own identity" % len(mine))
    said_sender = [r for r in rows[MESSAGE].values()
                   if props(r).get("isSender") is True]
    note("%d messages carry Beeper's own isSender" % len(said_sender))
    mentions_me = [r for r in rows[MESSAGE].values()
                   if owner_ref in ref_ids(props(r).get("mentions"))]
    note("%d messages mention the account's own identity" % len(mentions_me))

    # ---------------------------------------------------------------- 10
    section("10. a merged chat's members are minted thin")
    merged = [r for r in rows[CHAT].values() if (props(r).get("merge") or {})]
    if merged:
        for r in merged:
            members = ref_ids((props(r).get("merge") or {}).get("chats"))
            ok(members, "a merged chat carries no merge.chats")
            for m in members:
                row = rows[CHAT].get(m)
                ok(row is not None,
                   "merge.chats names %s, which is not a chat row" % m[:16])
                if row is not None and props(row).get("chatId") not in exp["chats"]:
                    # `title` is the SERVER's rendering of displayTemplate,
                    # not a property this bundle wrote.
                    ok(set(props(row)) <= {"account", "chatId", "title"},
                       "a member chat no walk reached carries more than the "
                       "reference gave it: %s" % sorted(props(row)))
        note("%d merged chats, %d member chats minted thin"
             % (len(merged),
                sum(1 for r in merged
                    for m in ref_ids((props(r).get("merge") or {}).get("chats"))
                    if props(rows[CHAT].get(m) or {}).get("chatId")
                    not in exp["chats"])))
    else:
        seed_note("no merged chat in this set")

    # ---------------------------------------------------------------- 11
    section("11. the second sync: idempotent, incremental, and cursor-driven")
    ok(resync("second sync"), "the second sync did not complete")
    rows2 = {
        CHAT: {rid(r): r for r in records(CHAT)},
        MESSAGE: {rid(r): r for r in records(MESSAGE)},
        USER: {rid(r): r for r in records(USER)},
        CHATSYNC: {rid(r): r for r in records(CHATSYNC)},
    }
    msg_props_after = {(ref_id(props(r).get("chat")), props(r).get("messageId")):
                       props(r) for r in rows2[MESSAGE].values()}
    forward_keys = set(exp["forward"])
    landed = [k for k in forward_keys if k in
              {(chat_of_row.get(ref_id(props(r).get("chat"))),
                props(r).get("messageId")) for r in rows2[MESSAGE].values()}]
    if forward_keys:
        ok(len(landed) == len(forward_keys),
           "%d of %d messages that only the FORWARD page carries did not land "
           "on the second sync" % (len(forward_keys) - len(landed),
                                   len(forward_keys)))
        note("%d messages arrived only through `direction=after`"
             % len(forward_keys))
    else:
        # In e2e this is a hole in the cut, and fixtures/curate.py is what
        # fills it; in seed the forward page cannot be recorded at all.
        ok(SEED, "the cut carries no `direction=after` recording, so nothing "
                 "proves the incremental walk (fixtures/curate.py, seam 2)")
        seed_note("a raw pull cannot hold a forward page")

    # CONTENT EQUALITY, not just a count: a row the second sync re-read must
    # come back the same, or "idempotent" means only that nothing was added.
    changed = []
    for key, before in msg_props_before.items():
        after = msg_props_after.get(key)
        if after is None or after == before:
            continue
        moved = sorted(k for k in set(before) | set(after)
                       if before.get(k) != after.get(k))
        changed.append("%s: %s" % (key[1], moved[:4]))
    ok(not changed, "%d messages changed content across a second sync of the "
                    "same recordings: %s" % (len(changed), changed[:3]))

    grew = len(rows2[MESSAGE]) - msg_before
    ok(grew == len(forward_keys),
       "the second sync changed the message count by %d; only the %d forward "
       "messages should be new" % (grew, len(forward_keys)))
    dupes = [k for k, n in collections.Counter(
        (ref_id(props(r).get("chat")), props(r).get("messageId"))
        for r in rows2[MESSAGE].values()).items() if n > 1]
    ok(not dupes, "%d (chat, messageId) pairs have more than one row: %s"
       % (len(dupes), dupes[:3]))

    # A PAGE OF HISTORY MUST NOT BUMP THE CHAT. The cursors moved, the chats
    # did not — which is the whole reason `chatsync` exists.
    moved = [k for k, v in rows2[CHAT].items()
             if k in chat_versions and version(v) != chat_versions[k]]
    ok(not moved, "%d chat rows changed version across a sync that only wrote "
                  "messages and cursors: %s" % (len(moved), moved[:3]))

    # ---------------------------------------------------------------- 12
    section("12. the requests the sync actually made")
    if MOCK:
        reqs = None
        try:
            with urllib.request.urlopen(MOCK + "/__mock/requests", timeout=20) as r:
                log = json.loads(r.read())
            reqs = log if isinstance(log, list) else (log.get("items")
                                                      or log.get("requests") or [])
        except Exception as e:  # noqa: BLE001
            note("(mock request log unavailable: %s)" % e)
        if reqs is not None:
            def q(entry, key):
                raw = entry.get("query")
                if isinstance(raw, str):
                    raw = urllib.parse.parse_qs(raw.lstrip("?"))
                v = (raw or {}).get(key)
                return v[0] if isinstance(v, list) else v

            paths = [e.get("path", "") for e in reqs]
            note("%d upstream requests served" % len(reqs))
            for want in ("/v1/info", "/v1/accounts", "/v1/bridges",
                         "/v1/labels", "/v1/chats"):
                ok(any(p == want or p.startswith(want) for p in paths),
                   "the sync never called %s" % want)
            writes = [e for e in reqs
                      if (e.get("method") or "GET").upper() != "GET"]
            ok(not writes, "%d NON-GET requests were made — this bundle reads "
                           "only: %s" % (len(writes),
                                         [(e.get("method"), e.get("path"))
                                          for e in writes[:3]]))
            assets = [p for p in paths if "/v1/assets" in p]
            ok(not assets, "%d requests fetched MEDIA — this bundle mirrors "
                           "metadata and downloads nothing: %s"
               % (len(assets), assets[:3]))

            msg_calls = [e for e in reqs if e.get("path", "").endswith("/messages")]
            after = [e for e in msg_calls if q(e, "direction") == "after"]
            before = [e for e in msg_calls if q(e, "direction") == "before"]
            note("%d message pages: %d backfill (direction=before), %d "
                 "incremental (direction=after)"
                 % (len(msg_calls), len(before), len(after)))
            if exp["forwardPages"]:
                ok(after, "the second sync never sent `direction=after` — it "
                          "re-walked cold instead of using the stored cursor")
            # EXACT BOUNDS: every cursor the sync named must be one the mirror
            # actually stored for that chat, or the walk is asking from
            # somewhere it invented.
            stored = set()
            for st_row in list(rows[CHATSYNC].values()) + list(rows2[CHATSYNC].values()):
                for key in ("newestCursor", "oldestCursor"):
                    if props(st_row).get(key):
                        stored.add(props(st_row)[key])
            stray = [q(e, "cursor") for e in after
                     if q(e, "cursor") and q(e, "cursor") not in stored]
            ok(not stray, "%d incremental windows name a cursor the mirror "
                          "never stored: %s" % (len(stray), stray[:3]))
            chat_pages = [e for e in reqs if e.get("path") == "/v1/chats"]
            ok(any(q(e, "cursor") for e in chat_pages),
               "the sync never asked /v1/chats for its second page — the "
               "cursor the first page returned was not followed")

            # A RELAXED or FALLBACK match means a recording EXISTS and the mock
            # had to drop parameters to reach it: the sync asked the wrong
            # question and got an answer that looks right. Fatal in both modes.
            loose = [(e.get("path"), e.get("match"), e.get("file"))
                     for e in reqs
                     if e.get("match") not in ("exact", "miss", None)]
            ok(not loose, "%d requests were answered by a RELAXED or FALLBACK "
                          "match, so their page and window prove nothing: %s"
               % (len(loose), loose[:3]))

            ALWAYS_RECORDED = ("/v1/info", "/v1/accounts", "/v1/bridges",
                               "/v1/labels", "/v1/chats")
            misses = [e for e in reqs if e.get("match") == "miss"]
            if misses:
                by_path = {}
                for e in misses:
                    key = re.sub(r"/v1/chats/[^/]+", "/v1/chats/{id}",
                                 e.get("path", "?"))
                    by_path[key] = by_path.get(key, 0) + 1
                note("MISSING recordings, by route: %s"
                     % ", ".join("%s x%d" % (m, n)
                                 for m, n in sorted(by_path.items(),
                                                    key=lambda kv: -kv[1])))
            core = [e for e in misses if e.get("path") in ALWAYS_RECORDED]
            ok(not core, "%d requests to a call every pull records found no "
                         "recording: %s"
               % (len(core), [(e.get("path"), e.get("query")) for e in core[:3]]))
            ok(SEED or not misses, "%d requests found no recording at all: %s"
               % (len(misses),
                  [(e.get("path"), e.get("query")) for e in misses[:3]]))
            if SEED and misses:
                seed_note("%d of %d upstream requests had no recording; the "
                          "pull does not hold them and the sync treats each as "
                          "that chat's own permanent failure"
                          % (len(misses), len(reqs)))

    # --------------------------------------------------------------- 13
    section("13. the core `sync` trait")
    # T-084 / decision 0085. The trait is what the Connections page and
    # `substratectl sync status` read, and it is written BESIDE `syncStatus`
    # and `lastSyncedAt` — so every assertion here has a legacy twin above
    # that must still hold, and the two are checked for agreeing.
    final = props(account_row())
    requested = final.get("syncRequestedAt")
    want_streams = {"chats", "messages"}
    ok(final.get("syncState") == "ok",
       "syncState is %r after a completed walk" % final.get("syncState"))
    ok(final.get("syncMessage"), "syncMessage is empty after a completed walk")
    ok(final.get("syncMessage") == final.get("syncStatus"),
       "syncMessage %r and syncStatus %r disagree"
       % (final.get("syncMessage"), final.get("syncStatus")))
    ok(final.get("lastSyncedAt"), "the trait's lastSyncedAt is unset")
    ok(requested,
       "the scenario never stamped syncRequestedAt, so the ack is untested")
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
    ok(isinstance(streams, dict) and set(streams) == want_streams,
       "syncStreams names %r, not %r"
       % (sorted(streams) if isinstance(streams, dict) else streams,
          sorted(want_streams)))
    if isinstance(streams, dict):
        for name in sorted(want_streams):
            entry = streams.get(name) or {}
            ok(entry.get("state") == "ok",
               "stream %s is %r after a completed walk"
               % (name, entry.get("state")))
            ok(entry.get("pending") == 0,
               "stream %s reports %r pending" % (name, entry.get("pending")))
            ok(entry.get("lastAt"), "stream %s never stamped lastAt" % name)
            ok(entry.get("requestedAck") == requested,
               "stream %s acked %r, the request was %r"
               % (name, entry.get("requestedAck"), requested))
        # The breakdown is the whole of the total, or it is decoration.
        if isinstance(progress, dict):
            total = sum(int((streams.get(n) or {}).get("pending") or 0)
                        for n in want_streams)
            ok(total == progress.get("pending"),
               "the per-stream pending counts sum to %r, syncProgress says %r"
               % (total, progress.get("pending")))

    # THE TWO UNHAPPY STATES, injected rather than waited for. A 429 on
    # /v1/info is `throttled`; a 401 is `erroring` with the cause; the run
    # after each recovers, which is what makes them a test of the trait
    # rather than of the fixture.
    if MOCK and not SEED:
        faults([{"match": "GET /v1/info", "status": [429],
                 "headers": {"Retry-After": "1"},
                 "body": {"message": "rate limited", "code": "rate_limited"}}])
        request_sync()
        held = wait_for_state("throttled")
        ok(held.get("syncState") == "throttled",
           "an injected 429 left syncState %r, not throttled"
           % held.get("syncState"))
        ok(held.get("retryNotBefore"),
           "throttled, but no retryNotBefore deadline was stamped")
        ok(held.get("syncRequestedAck") != held.get("syncRequestedAt"),
           "a throttled run acknowledged the request it never served (%r)"
           % held.get("syncRequestedAck"))

        faults([{"match": "GET /v1/info", "status": [401],
                 "body": {"message": "invalid token", "code": "unauthorized"}}])
        # A rate-limit deadline binds the record door too on this bundle, so
        # the next request has to wait out the Retry-After the 429 named.
        time.sleep(3)
        request_sync()
        broke = wait_for_state("erroring")
        ok(broke.get("syncState") == "erroring",
           "an injected 401 left syncState %r, not erroring"
           % broke.get("syncState"))
        ok(broke.get("syncError"), "erroring, but syncError is empty")
        ok(broke.get("syncErrorAt"), "erroring, but syncErrorAt is unset")
        ok(str(broke.get("syncStatus") or "").startswith("erroring"),
           "the trait says erroring and syncStatus says %r"
           % broke.get("syncStatus"))
        broken = (broke.get("syncStreams") or {}).get("chats") or {}
        ok(broken.get("state") == "erroring",
           "the 401 landed in the `info` phase, which the chats stream owns, "
           "and that stream reports %r" % broken.get("state"))
        ok(broke.get("syncRequestedAck") != broke.get("syncRequestedAt"),
           "a failed run acknowledged the request it never served (%r)"
           % broke.get("syncRequestedAck"))

        # RECOVERY. Both rules are spent, so the next run is an ordinary one.
        faults([])
        if resync("after the injected failures"):
            back = props(account_row())
            ok(back.get("syncState") == "ok",
               "after the injected failures syncState is %r"
               % back.get("syncState"))
            ok(back.get("syncRequestedAck") == back.get("syncRequestedAt"),
               "the recovered run left syncRequestedAck %r against request %r"
               % (back.get("syncRequestedAck"), back.get("syncRequestedAt")))
            ok(back.get("syncError"),
               "syncError was cleared by a later success — it is the LAST "
               "error, and syncState is what says the run is over")

    section("14. sendmessage sends through the pasted token")
    sendmessage()

    # ------------------------------------------------------------- the report
    print("")
    if not problems:
        print("\033[32m== %d checks passed\033[0m" % checks)
        return 0
    print("\033[31m!! %d of %d checks failed\033[0m" % (len(problems), checks))
    last = None
    for sec, msg in problems:
        if sec != last:
            print("  %s" % sec)
            last = sec
        print("    - %s" % msg)
    return 1


SEND_FN = P + "/sendmessage"


def sendmessage():
    """#644. The bundle's one write: a callable, fired by nobody but its
    caller, that spends the config's token on `POST /v1/chats/{id}/messages`
    and writes no record. The recording names the chat; the message replied
    to is one the sync mirrored in it."""
    if not MOCK or SEED:
        seed_note("sendmessage is driven against the mock only")
        return
    from writecall import Writes, body_json, error_text
    rec_name = next((n for n, _ in each("POST_v1_chats_*_messages.json")), None)
    rec = load(rec_name) if rec_name else {}
    chat = (rec or {}).get("chatID")
    if not ok(chat, "no sendmessage recording names a chatID"):
        return
    chats = {props(r).get("chatId"): rid(r) for r in records(CHAT)}
    ok(chat in chats, "the recording's chat %s is not mirrored" % chat)
    mine = [props(m).get("messageId") for m in records(MESSAGE)
            if ref_id(props(m).get("chat")) == chats.get(chat)
            and props(m).get("messageId")]
    if not ok(mine, "no mirrored message in %s to reply to" % chat):
        return
    route = "/v1/chats/%s/messages" % chat
    w = Writes(SERVER, TOKEN, MOCK)
    w.reset()

    text = "On my way."
    st, reply = w.call(SEND_FN, {"chat": chat, "text": text, "replyTo": mine[0]})
    ok(st == 200, "a reply answered %s: %s" % (st, error_text(reply)[:300]))
    out = (reply or {}).get("output") or {} if st == 200 else {}
    ok(out == {"chat": chat, "pendingMessageId": rec.get("pendingMessageID")},
       "the reply's output is %r, want the recording's chat and pending id" % out)
    ok((reply or {}).get("effects") == 0,
       "sendmessage wrote %r records; it writes none" % (reply or {}).get("effects"))
    sent = w.requests("POST", route)
    ok(len(sent) == 1, "%d sends reached Beeper, want 1" % len(sent))
    if sent:
        ok(body_json(sent[0]) == {"text": text, "replyToMessageID": mine[0]},
           "the body sent was %r" % sent[0].get("body"))
        ok(str(sent[0].get("auth") or "").startswith("Bearer bdapi-"),
           "the token rode the Authorization header (%r)" % sent[0].get("auth"))

    st, reply = w.call(SEND_FN, {"chat": chat, "text": text})
    sent = w.requests("POST", route)
    ok(st == 200 and len(sent) == 2 and body_json(sent[-1]) == {"text": text},
       "a plain message answered %s and sent %r" % (st, sent[-1:] or None))

    w.faults([{"match": "POST /v1/chats/*/messages", "status": [404],
               "body": {"message": "Chat not found", "code": "NOT_FOUND"}}])
    st, reply = w.call(SEND_FN, {"chat": chat, "text": text})
    ok(st >= 400 and "Chat not found" in error_text(reply),
       "Beeper's refusal did not reach the caller: %s %s"
       % (st, error_text(reply)[:300]))
    w.faults([])

    before = len(w.requests("POST", route))
    st, reply = w.call(SEND_FN, {"chat": chat})
    ok(400 <= st < 500, "a call with no text answered %s, want a 4xx" % st)
    ok(len(w.requests("POST", route)) == before,
       "a call with no text still reached Beeper")

    # The origin pin: a public apiBase refuses before any request. The value
    # it had is put back, not cleared: a scenario-only run reaches the mock
    # through it.
    cfg = "/api/v1/%s/default" % os.environ.get("CONFIG_KIND", P + "/config")
    had = props(api("GET", cfg)[1] or {}).get("apiBase")
    st, _ = api("PATCH", cfg, {"properties": {"apiBase": "https://beeper.example.com"}})
    if ok(st < 400, "could not point apiBase at a public host: %s" % st):
        st, reply = w.call(SEND_FN, {"chat": chat, "text": text})
        ok(st >= 400 and "not a private origin" in error_text(reply),
           "a public apiBase did not refuse: %s %s"
           % (st, error_text(reply)[:200]))
        ok(len(w.requests("POST", route)) == before,
           "the refused call still reached the mock")
        api("PATCH", cfg, {"properties": {"apiBase": had}})
    note("sendmessage: %d sends reached the mock" % len(w.requests("POST", route)))


if __name__ == "__main__":
    sys.exit(main())
