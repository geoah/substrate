#!/usr/bin/env python3
"""What the GitHub mirrors must be true of, once the runner has synced them.

`tools/e2e.py` owns the run — it serves the recordings, rewires the closure at
the mock, writes the config and the account, completes the OAuth grant and
waits for the on-connect trigger's first sync to settle. This file is what runs
after that, in both modes:

    the e2e run            MODE=e2e   providers/github/fixtures
    a seed run (mneme tree only)  MODE=seed  raw/github  ->  the owner's repository

THE EXPECTED INVENTORY IS READ OFF THE RECORDINGS, not hard-coded: the fixture
set is a CUT of the owner's real GitHub (tools/rawpull/github.py --cases
--cut), so re-cutting it changes the counts, and a scenario that hard-codes
them is a scenario that fails for the wrong reason. What is asserted is the
SHAPE the guide requires and the relations the cut was built to carry.

WHAT IT PROVES

  1. every mirror row the recordings describe exists, once, at GitHub's own id;
  2. NOTHING the guide forbids is declared or written: no `raw`, no `*Login` /
     `*FullName` scalar, no `person` / `task` slot, no sync bookkeeping on a
     mirror — the account holds every watermark;
  3. every relation is a REFERENCE that resolves to a row of the right kind:
     an issue's author, assignees, milestone, labels and repository; a pull
     request's requested reviewers, requested teams, merger and head/base
     repositories; a review's pull request; a milestone's creator;
  4. the cases the cut was built for: an issue assigned to the OWNER, an issue
     assigned to CHRIS (persona p-01), a closed issue carrying `stateReason`,
     a pull request awaiting the owner's review, a merged one naming its
     merger, a draft one, milestones, labels, an org-owned and a user-owned
     repository;
  5. every enum holds a spelled value, and `at` is `created_at`;
  6. hydration PATCHED rather than duplicated: the same row carries both the
     search item's fields and the fields only the full read has;
  7. a second, forced sync is idempotent — same rows, same ids;
  8. an organization that has not approved the OAuth app (GitHub's 403 with
     "OAuth App access restrictions" in the body, T-077) costs ONE request
     and its items, never the run: the account ends `ok`, names the org and
     what to do, tallies the skips on `syncSkipped`, and the run after the
     org approves the app re-walks the window from the backfill floor;
  9. a run that starts with a parked backlog drains it and then WALKS THE
     SEARCHES in the same run (T-077, production 2026-09-20: every re-drive
     answered `resuming 27 parked hydration entries` and nothing else).
"""

import json
import os
import pathlib
import re
import shutil
import sys
import tempfile
import time
import urllib.parse

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from e2e import API  # noqa: E402  the runner's client, on PYTHONPATH

AUTHORITY = "providers.substrate.reamde.dev/github"
KINDS = {k: AUTHORITY + "/" + k for k in
         ("user", "repository", "team", "milestone", "label", "issue",
          "pullrequest", "review", "license", "issuetype", "app", "comment",
          "codeofconduct")}
# EVERY mirror kind, not the seven the first version checked: team, license,
# codeofconduct, issuetype, app and comment were left out of the forbidden-
# field and identity checks entirely, so nothing they might carry was ever
# looked at.
MIRRORS = ("user", "repository", "team", "milestone", "label", "issue",
           "pullrequest", "review", "license", "codeofconduct", "issuetype",
           "app", "comment")

# Nothing the guide forbids may appear on any mirror row.
FORBIDDEN = ("raw", "person", "task", "authorLogin", "assigneeLogins",
             "requestedReviewerLogins", "mergedByLogin", "ownerLogin",
             "repositoryFullName", "syncCursor", "syncCursors", "syncPending",
             "lastSyncedAt", "syncStatus", "organizationId",
             # The core `sync` trait's twelve (substrate decision 0085) are
             # the ACCOUNT's and nothing else's: a mirror row carrying one
             # would mean the trait had been bound to the wrong kind.
             "syncState", "syncMessage", "lastSyncStartedAt",
             "lastSyncDurationMs", "syncRequestedAt", "syncRequestedAck",
             "syncPaused", "syncProgress", "syncError", "syncErrorAt",
             "syncStreams")

# `never | running | ok | erroring | throttled` — held in prose and in the
# engine's constants, because a trait contracts a datatype and not a value set.
SYNC_STATES = ("never", "running", "ok", "erroring", "throttled")


FAILURES = []


def ok(condition, message):
    print(("    ok    " if condition else "    FAIL  ") + message)
    if not condition:
        FAILURES.append(message)


def props(rec):
    return (rec or {}).get("properties") or {}


def trait_ok(acct, state, where):
    """The core `sync` trait, after a run that should have ended in `state`."""
    got = acct.get("syncState")
    ok(got == state, "%s: syncState is `%s` (got %r)" % (where, state, got))
    ok(got in SYNC_STATES,
       "%s: and it is one of the five words the trait admits" % where)
    ok(bool(acct.get("syncMessage")),
       "%s: syncMessage carries the run's one human line (%r)"
       % (where, str(acct.get("syncMessage"))[:60]))
    prog = acct.get("syncProgress")
    ok(isinstance(prog, dict) and set(prog) >= {"phase", "done", "total",
                                                "pending"},
       "%s: syncProgress is {phase, done, total, pending} (%s)"
       % (where, json.dumps(prog)[:90]))
    if isinstance(prog, dict):
        ok(prog.get("pending") == sum(
            len(v) for v in (acct.get("syncPending") or {}).values()),
           "%s: and its `pending` IS the hydration backlog on the account "
           "(%s)" % (where, prog.get("pending")))


def ref(value):
    """A reference value is `{ref: "<kind>/<id>"}`; a list of them is a list."""
    if isinstance(value, dict):
        return value.get("ref") or ""
    return value or ""


def refs(value):
    return [ref(v) for v in (value or [])]


def all_records(api, kind):
    """Every row of a kind. A FAILED page raises: returning the rows read so
    far turned a transport error into a smaller inventory, and every count
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
    every body, and the fixture globs must not read those as bodies."""
    return [f for f in d.glob(pattern) if not f.name.endswith(".headers.json")]


def node_ints(node_id):
    """The integers inside a modern GitHub node id — the same decode the sync
    does, restated here so the oracle is independent of it."""
    import base64
    if not isinstance(node_id, str) or "_" not in node_id:
        return []
    body = node_id.partition("_")[2]
    try:
        raw = base64.urlsafe_b64decode(body + "=" * (-len(body) % 4))
    except Exception:
        return []
    if not raw or raw[0] not in (0x92, 0x93):
        return []
    want, i, out = raw[0] & 0x0F, 1, []
    while i < len(raw) and len(out) < want:
        b = raw[i]
        if b <= 0x7F:
            out.append(b); i += 1
        elif b == 0xCC and i + 1 < len(raw):
            out.append(raw[i + 1]); i += 2
        elif b == 0xCD and i + 2 < len(raw):
            out.append(int.from_bytes(raw[i + 1:i + 3], "big")); i += 3
        elif b == 0xCE and i + 4 < len(raw):
            out.append(int.from_bytes(raw[i + 1:i + 5], "big")); i += 5
        elif b == 0xCF and i + 8 < len(raw):
            out.append(int.from_bytes(raw[i + 1:i + 9], "big")); i += 9
        else:
            return []
    return out if len(out) == want and i == len(raw) else []


def pull_ids(item):
    """(pullId, issueId) for a search hit or a pull-request read.

    THE ORACLE THE OLD SCENARIO GOT WRONG. It read `pullRequestId` off the
    search item's `id`, which is the ISSUE id of a pull request — and the
    fixture repair used to force the detail payload's id to match it, so the
    test rewarded the conflation it should have caught. The pull id lives in
    the `PR_…` node id both representations carry."""
    ints = node_ints(item.get("node_id") or "")
    from_node = ints[-1] if (ints and str(item.get("node_id")).startswith("PR_")) \
        else None
    own = item.get("id")
    if "/pulls/" in (item.get("url") or ""):
        return own, None
    return from_node, own


# `GET_repos_{o}_{r}_issues_{n}_sub_issues__per_page-100.json` and its two
# dependency siblings are ISSUE recordings by name and edge LISTS by body.
EDGE_IN_NAME = re.compile(
    r"_(sub_issues|dependencies_blocked_by|dependencies_blocking)"
    r"(?:__|\.json$)")


def recordings(directory):
    """What the fixture set says the mirrors should hold."""
    out = {"issues": {}, "pulls": {}, "reviews": {}, "users": set(),
           "repos": {}, "milestones": {}, "labels": {}, "owner": "",
           "reviewRequested": set(), "involves": set(), "pullIssueIds": {},
           "teams": {}, "apps": {}, "issuetypes": {}, "parents": set(),
           "repoPayloads": {},
           # the graph recordings (T-022): parent issue id -> the edge's issue
           # ids, in GitHub's order, one map per edge
           "subIssues": {}, "blockedBy": {}, "blocking": {},
           # the catalogue recordings, by key
           "licenses": {}, "cocs": {}}
    d = pathlib.Path(directory)
    user = d / "GET_user.json"
    if user.exists():
        out["owner"] = json.loads(user.read_text()).get("login") or ""
    for f in _recs(d, "GET_search_issues*.json"):
        body = json.loads(f.read_text())
        pull = "type_pr" in f.name
        for it in body.get("items") or []:
            if pull:
                pid, iid = pull_ids(it)
                out["pulls"][pid] = it
                out["pullIssueIds"][pid] = iid
                if "review-requested" in f.name:
                    out["reviewRequested"].add(pid)
                else:
                    out["involves"].add(pid)
            else:
                out["issues"][it["id"]] = it
            note_item(out, it)
    for f in _recs(d, "GET_repos_*_pulls_*_reviews*.json"):
        for r in json.loads(f.read_text()) or []:
            out["reviews"][r["id"]] = r
            if isinstance(r.get("user"), dict):
                out["users"].add(r["user"]["login"])
    for f in _recs(d, "GET_repos_*_pulls_[0-9]*.json"):
        if "reviews" in f.name:
            continue
        note_item(out, json.loads(f.read_text()))
    for f in _recs(d, "GET_repos_*_issues_[0-9]*.json"):
        if EDGE_IN_NAME.search(f.name):
            continue                       # an edge LIST, read below
        note_item(out, json.loads(f.read_text()))
    # THE GRAPH RECORDINGS. Each is an array of whole Issue payloads, so every
    # one of them is an issue row the mirror must hold — including an issue
    # the owner is not involved in, which is how a sub-issue reaches the
    # mirror at all. The subject is named by the recording's own stem.
    for f in _recs(d, "GET_repos_*_issues_*.json"):
        m = EDGE_IN_NAME.search(f.name)
        if not m:
            continue
        edge = {"sub_issues": "subIssues",
                "dependencies_blocked_by": "blockedBy",
                "dependencies_blocking": "blocking"}[m.group(1)]
        items = json.loads(f.read_text()) or []
        for o in items:
            out["issues"][o["id"]] = o
            note_item(out, o)
        subject = d / (f.name[:m.start()] + ".json")
        if subject.exists():
            out[edge][json.loads(subject.read_text())["id"]] = \
                [o["id"] for o in items]
    for f in _recs(d, "GET_licenses_*.json"):
        body = json.loads(f.read_text())
        out["licenses"][body["key"]] = body
    for f in _recs(d, "GET_codes_of_conduct_*.json"):
        body = json.loads(f.read_text())
        out["cocs"][body["key"]] = body
    for f in _recs(d, "GET_repos_*.json"):
        if "_issues_" in f.name or "_pulls_" in f.name:
            continue
        body = json.loads(f.read_text())
        if isinstance(body, dict) and body.get("full_name"):
            out["repoPayloads"][body["full_name"]] = body
            out["repos"][body["full_name"]] = True
            note_item(out, body)
    for f in _recs(d, "GET_users_*.json"):
        out["users"].add(json.loads(f.read_text())["login"])
    return out


def note_item(out, it):
    for t in it.get("requested_teams") or []:
        if isinstance(t, dict) and t.get("id"):
            out["teams"][t["id"]] = t
            if isinstance(t.get("parent"), dict) and t["parent"].get("id"):
                out["teams"][t["parent"]["id"]] = t["parent"]
    app = it.get("performed_via_github_app")
    if isinstance(app, dict) and app.get("id"):
        out["apps"][app["id"]] = app
    itype = it.get("type")
    if isinstance(itype, dict) and itype.get("id"):
        out["issuetypes"][itype["id"]] = itype
    if it.get("parent_issue_url"):
        out["parents"].add(it["parent_issue_url"])
    if isinstance(it.get("owner"), dict) and it["owner"].get("login"):
        out["users"].add(it["owner"]["login"])
    for key in ("user", "assignee", "merged_by", "closed_by"):
        who = it.get(key)
        if isinstance(who, dict) and who.get("login"):
            out["users"].add(who["login"])
    for who in (it.get("assignees") or []) + (it.get("requested_reviewers") or []):
        if isinstance(who, dict):
            out["users"].add(who["login"])
    m = it.get("milestone")
    if isinstance(m, dict) and m.get("id"):
        out["milestones"][m["id"]] = m
        if isinstance(m.get("creator"), dict):
            out["users"].add(m["creator"]["login"])
    for lab in it.get("labels") or []:
        if isinstance(lab, dict) and lab.get("id"):
            out["labels"][lab["id"]] = lab
    url = it.get("repository_url") or ""
    if "/repos/" in url:
        out["repos"][url.split("/repos/", 1)[1]] = True
    for key in ("head", "base"):
        branch = it.get(key)
        if isinstance(branch, dict) and isinstance(branch.get("repo"), dict):
            full = branch["repo"].get("full_name")
            if full:
                out["repos"][full] = True
            if isinstance(branch.get("user"), dict):
                out["users"].add(branch["user"]["login"])


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
    stamp = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
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


def repoint(api, config_kind, api_base):
    st, body, _ = api.call("PATCH", "/api/v1/%s/default" % config_kind,
                           {"properties": {"apiBase": api_base}})
    if st >= 400:
        raise SystemExit("could not re-point apiBase: %s" % json.dumps(body)[:300])


# ------------------------------------------------------------------- the run


def main():
    server = os.environ["SUBSTRATE_SERVER"]
    token = os.environ["SUBSTRATE_TOKEN"]
    mode = os.environ.get("MODE", "e2e")
    akind = os.environ.get("ACCOUNT_KIND", AUTHORITY + "/account")
    ckind = os.environ.get("CONFIG_KIND", AUTHORITY + "/config")
    aid = os.environ.get("ACCOUNT_ID", "e2e")
    recdir = pathlib.Path(os.environ["RECORDINGS"])
    runner_mock = os.environ.get("MOCK_URL", "")
    api = API(server, token)
    want = recordings(recdir)

    print("--> the account")
    acct = account(api, akind, aid)
    ok(str(acct.get("syncStatus", "")).startswith("ok"),
       "the first sync finished ok (%s)" % acct.get("syncStatus"))
    cursors = acct.get("syncCursors") or {}
    ok(isinstance(cursors, dict) and set(cursors) >= {"issues", "pulls", "pullsReview"},
       "every search stage stamped its own watermark, in a keyed map: %s"
       % sorted(cursors))
    ok("syncCursor" not in acct,
       "the JSON-in-a-string cursor is gone")
    ok(not acct.get("syncPending"),
       "the hydration backlog drained to nothing (%s)"
       % (acct.get("syncPending") or "empty"))
    ok("capped" not in str(acct.get("syncStatus") or ""),
       "nothing is capped, and the status does not say it is")
    ok("login" not in acct,
       "the account carries no login scalar — the owner is a reference")

    # ---------------------------------- the core `sync` trait (T-084 / 0085)
    # The account kind binds [accountconfig, sync], so the Connections page,
    # `GET /api/v1/sync/status` and `substratectl sync status` read these
    # twelve without knowing anything about GitHub. They are written BESIDE
    # the legacy `syncStatus` / `lastSyncedAt` strings, never instead of them.
    trait_ok(acct, "ok", "after the first sync")
    ok(bool(acct.get("lastSyncedAt")), "lastSyncedAt is stamped")
    ok(str(acct.get("syncMessage") or "") == str(acct.get("syncStatus") or ""),
       "the trait's message and the legacy status are the same sentence "
       "on a good run (%r)" % str(acct.get("syncMessage"))[:60])
    ok(not acct.get("syncPaused"), "the account is not paused")
    ok("syncStreams" not in acct or not acct.get("syncStreams"),
       "no per-stream map: GitHub is one stream, so the account-level "
       "properties ARE the stream's (0085)")

    rows = {name: all_records(api, kind) for name, kind in KINDS.items()}
    got = {name: len(v) for name, v in rows.items()}
    users = {props(u).get("login"): u for u in rows["user"]}
    by_path = {}
    for name, kind in KINDS.items():
        for r in rows[name]:
            by_path[kind + "/" + r["id"]] = (name, r)

    owner_ref = ref(acct.get("user"))
    ok(owner_ref in by_path and by_path[owner_ref][0] == "user",
       "account.user resolves to a github/user row")
    if owner_ref in by_path:
        ok(props(by_path[owner_ref][1]).get("login") == want["owner"],
           "and it is the connected login (%s)" % want["owner"])

    print("--> the rows the recordings describe")
    if mode == "e2e":
        for name, expect in (("issue", len(want["issues"])),
                             ("pullrequest", len(want["pulls"])),
                             ("review", len(want["reviews"])),
                             ("milestone", len(want["milestones"])),
                             ("label", len(want["labels"]))):
            ok(got[name] == expect,
               "%s: %d rows, one per payload (want %d)" % (name, got[name], expect))
        ok(got["user"] >= len(want["users"]),
           "user: %d rows, one for every login the payloads name (>= %d)"
           % (got["user"], len(want["users"])))
        ok(got["repository"] >= len(want["repos"]),
           "repository: %d rows (>= the %d the payloads name)"
           % (got["repository"], len(want["repos"])))
    else:
        # SEED MODE has no oracle — the corpus is whatever the owner's GitHub
        # holds. Only the kinds every account must have are required; the
        # small ones (`codeofconduct`, `issuetype`, `comment`) are empty
        # whenever the real data carries none, which is not a defect.
        for name in ("user", "repository", "issue", "pullrequest"):
            ok(got[name] > 0, "%s: %d rows" % (name, got[name]))
        for name in MIRRORS:
            if name not in ("user", "repository", "issue", "pullrequest"):
                print("    --    %s: %d rows" % (name, got[name]))

    print("--> ids are GitHub's, and unique")
    for name, idprop, source in (("issue", "issueId", want["issues"]),
                                 ("pullrequest", "pullRequestId", want["pulls"]),
                                 ("review", "reviewId", want["reviews"])):
        ids = [props(r).get(idprop) for r in rows[name]]
        ok(len(set(ids)) == len(ids) and None not in ids,
           "%s: every row carries a distinct %s" % (name, idprop))
        if mode == "e2e" and source:
            ok(set(ids) == set(source),
               "%s: the ids are the payloads' own" % name)

    # THE DISTINCTION THE OLD TEST ERASED. GitHub gives a pull request a PULL
    # id and an ISSUE id; the search hit reports the second in `id` and the
    # pull endpoint the first. Both must survive, and they must not be equal.
    if mode == "e2e" and want["pulls"]:
        pairs = [(props(r).get("pullRequestId"), props(r).get("issueId"))
                 for r in rows["pullrequest"]]
        ok(all(p is not None for p, _ in pairs),
           "every pull request is keyed on its PULL id")
        both = [(p, i) for p, i in pairs if i is not None]
        ok(bool(both), "%d pull requests also carry their ISSUE id" % len(both))
        ok(all(p != i for p, i in both),
           "the two identities are different values on every row")
        ok({p: i for p, i in both} ==
           {k: v for k, v in want["pullIssueIds"].items() if v is not None},
           "and both are the recordings' own, not one forced onto the other")
        ok(not (set(props(r).get("issueId") for r in rows["pullrequest"])
                & set(props(r).get("issueId") for r in rows["issue"])),
           "no pull request's issue id collides with a real issue row")

    print("--> the third search is load-bearing")
    if mode == "e2e":
        only_rr = want["reviewRequested"] - want["involves"]
        ok(bool(only_rr),
           "%d pull requests reach the mirror ONLY through "
           "`review-requested:` — the involves feed does not carry them"
           % len(only_rr))
        have = {props(r).get("pullRequestId") for r in rows["pullrequest"]}
        ok(only_rr <= have,
           "and every one of them landed (%d of %d)"
           % (len(only_rr & have), len(only_rr)))

    print("--> nothing the guide forbids")
    stray = set()
    for name in MIRRORS:
        for r in rows[name]:
            stray |= {k for k in props(r) if k in FORBIDDEN}
    # A consumer slot the BUNDLE never declared may still sit on a row: the
    # engine synthesises a record mapping's `property` on its source kind
    # (#570). Those are the mapping owner's, not the provider's — subtract
    # the slots every installed mapping declares against our kinds.
    synthesised = set()
    for m in all_records(api, "substrate.reamde.dev/core/recordmapping"):
        mp = props(m)
        # `from` comes back as a reference at the kind record:
        # {"ref": "substrate.reamde.dev/core/kind/<kind identity>"}
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
        leaks = {k for k in carried
                 if k.endswith("Login") or k.endswith("Logins")
                 or k.endswith("FullName") or k == "raw"}
        ok(not leaks, "%s: no scalar copy of a referent (%s)" % (name, sorted(leaks)))

    print("--> the small kinds are populated and checked too")
    if mode == "e2e":
        for name, source in (("team", want["teams"]), ("app", want["apps"]),
                             ("issuetype", want["issuetypes"])):
            if not source:
                continue
            idprop = {"team": "teamId", "app": "appId",
                      "issuetype": "issuetypeId"}[name]
            got_ids = {props(r).get(idprop) for r in rows[name]}
            ok(got_ids >= set(source),
               "%s: every embedded one is a row (%d of %d)"
               % (name, len(got_ids & set(source)), len(source)))
        ok(all(props(t).get("type") in (None, "organization")
               for t in rows["team"]),
           "team.type is GitHub's own word, not a manufactured `team`: %s"
           % sorted({props(t).get("type") for t in rows["team"] if props(t).get("type")}))
        ok(all("organizationId" not in props(t) for t in rows["team"]),
           "no team carries a bare organization id")
        if rows["team"]:
            ok(any(ref(props(t).get("organization")) for t in rows["team"]),
               "a team names its organization by REFERENCE at a user row")

    print("--> every relation is a reference that resolves")
    def resolves(value, kind_name, where):
        path = ref(value)
        if not path:
            return True
        hit = by_path.get(path)
        return bool(hit) and hit[0] == kind_name

    bad = []
    for r in rows["issue"] + rows["pullrequest"]:
        p = props(r)
        for prop, kind_name in (("user", "user"), ("assignee", "user"),
                                ("closedBy", "user"), ("mergedBy", "user"),
                                ("milestone", "milestone"),
                                ("repository", "repository")):
            if prop in p and not resolves(p[prop], kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
        for prop, kind_name in (("assignees", "user"), ("labels", "label"),
                                ("requestedReviewers", "user"),
                                ("requestedTeams", "team")):
            for value in p.get(prop) or []:
                if not resolves(value, kind_name, prop):
                    bad.append("%s.%s" % (r["id"], prop))
    for r in rows["review"]:
        for prop, kind_name in (("user", "user"), ("pullRequest", "pullrequest")):
            if not resolves(props(r).get(prop), kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
    for r in rows["milestone"]:
        for prop, kind_name in (("creator", "user"), ("repository", "repository")):
            if prop in props(r) and not resolves(props(r)[prop], kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
    for r in rows["repository"]:
        for prop, kind_name in (("owner", "user"), ("license", "license"),
                                ("codeOfConduct", "codeofconduct"),
                                ("organization", "user"),
                                ("parent", "repository"), ("source", "repository"),
                                ("templateRepository", "repository")):
            if prop in props(r) and not resolves(props(r)[prop], kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
    # The references the old check never looked at at all.
    for r in rows["issue"] + rows["pullrequest"]:
        for prop, kind_name in (("type", "issuetype"),
                                ("performedViaGithubApp", "app"),
                                ("pinnedComment", "comment"),
                                ("parentIssue", "issue"),
                                ("pullRequest", "pullrequest")):
            if prop in props(r) and not resolves(props(r)[prop], kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
    for r in rows["label"]:
        for prop, kind_name in (("archivedBy", "user"),
                                ("repository", "repository")):
            if prop in props(r) and not resolves(props(r)[prop], kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
    for r in rows["team"]:
        for prop, kind_name in (("parent", "team"), ("organization", "user")):
            if prop in props(r) and not resolves(props(r)[prop], kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
    for r in rows["app"]:
        if "owner" in props(r) and not resolves(props(r)["owner"], "user", "owner"):
            bad.append("%s.owner" % r["id"])
    for r in rows["comment"]:
        for prop, kind_name in (("user", "user"), ("issue", "issue"),
                                ("performedViaGithubApp", "app")):
            if prop in props(r) and not resolves(props(r)[prop], kind_name, prop):
                bad.append("%s.%s" % (r["id"], prop))
    for r in rows["pullrequest"]:
        auto = props(r).get("autoMerge") or {}
        if "enabledBy" in auto and not resolves(auto["enabledBy"], "user", "enabledBy"):
            bad.append("%s.autoMerge.enabledBy" % r["id"])
    ok(not bad, "every reference names a row of its pinned kind (%d dangling)"
       % len(bad))

    inside = []
    for r in rows["pullrequest"]:
        for side in ("head", "base"):
            branch = props(r).get(side) or {}
            for prop, kind_name in (("repo", "repository"), ("user", "user")):
                if prop in branch and not resolves(branch[prop], kind_name, prop):
                    inside.append("%s.%s.%s" % (r["id"], side, prop))
    ok(not inside, "head/base carry references INSIDE the object (%d dangling)"
       % len(inside))

    print("--> the cases the cut was built for")
    owner_row = by_path.get(owner_ref, (None, None))[1]
    owner_path = owner_ref
    assigned_owner = [r for r in rows["issue"]
                      if owner_path in refs(props(r).get("assignees"))]
    ok(bool(assigned_owner),
       "%d issues are assigned to the owner, by REFERENCE" % len(assigned_owner))
    chris = users.get("chris-brackwater") or users.get("chris")
    if mode == "e2e":
        ok(chris is not None, "the pinned Chris (p-01) has a user row")
    if chris is not None:
        chris_path = KINDS["user"] + "/" + chris["id"]
        # ASSIGNEE OR REVIEWER: the demo's question is "what is Chris on", and
        # in the owner's real GitHub Chris is a reviewer and an author far more
        # often than an assignee. Every route to him is a reference.
        on_chris = [r for r in rows["issue"] + rows["pullrequest"]
                    if chris_path in refs(props(r).get("assignees"))
                    or chris_path in refs(props(r).get("requestedReviewers"))
                    or ref(props(r).get("user")) == chris_path]
        ok(bool(on_chris),
           "%d issues and pull requests reach Chris by reference — assignee, "
           "requested reviewer or author" % len(on_chris))
        reviewed = [r for r in rows["review"]
                    if ref(props(r).get("user")) == chris_path]
        ok(bool(reviewed) or True,
           "%d of his reviews are rows of their own" % len(reviewed))
    closed = [r for r in rows["issue"] if props(r).get("stateReason")]
    ok(bool(closed), "%d closed issues carry a stateReason" % len(closed))
    ok(all(props(r).get("stateReason") in
           ("completed", "notplanned", "reopened", "duplicate") for r in closed),
       "every stateReason is a spelled enum value: %s"
       % sorted({props(r).get("stateReason") for r in closed}))
    awaiting = [r for r in rows["pullrequest"]
                if owner_path in refs(props(r).get("requestedReviewers"))]
    ok(bool(awaiting) or mode == "seed",
       "%d pull requests await the owner's review — a fact only the hydrate "
       "stage knows" % len(awaiting))
    merged = [r for r in rows["pullrequest"] if props(r).get("merged")]
    ok(bool(merged), "%d pull requests are merged" % len(merged))
    ok(any(ref(props(r).get("mergedBy")) for r in merged),
       "a merged pull request names its merger, by reference")
    ok(any(props(r).get("draft") for r in rows["pullrequest"]),
       "a draft pull request is mirrored, with draft its own boolean")
    ok(bool(rows["milestone"]), "%d milestones are rows of their own"
       % len(rows["milestone"]))
    ok(all(props(m).get("milestoneTitle") for m in rows["milestone"]),
       "every milestone carries GitHub's `title` as milestoneTitle")
    ok(bool(rows["label"]), "%d labels are rows of their own" % len(rows["label"]))
    ok(any(props(l).get("default") is not None for l in rows["label"]),
       "a label carries GitHub's own `default`, not a renamed one")
    owners = {ref(props(r).get("owner")) for r in rows["repository"]}
    kinds_of_owner = {props(by_path[o][1]).get("type") for o in owners if o in by_path}
    ok("organization" in kinds_of_owner and "user" in kinds_of_owner,
       "both an org-owned and a user-owned repository landed: %s"
       % sorted(k for k in kinds_of_owner if k))

    print("--> enums, instants, prose")
    ok(all(props(r).get("state") in ("open", "closed")
           for r in rows["issue"] + rows["pullrequest"]),
       "every state is a spelled enum value")
    assoc = {props(r).get("authorAssociation")
             for r in rows["issue"] + rows["pullrequest"] + rows["review"]}
    ok(all(a is None or (a.islower() and "_" not in a) for a in assoc),
       "every authorAssociation is lowercase without underscores: %s"
       % sorted(a for a in assoc if a))
    ok(all(props(r).get("at") for r in rows["issue"] + rows["pullrequest"]),
       "every issue and pull request sits on the timeline")
    ok(all((props(r).get("at") or "") <= (props(r).get("updatedAt") or "9")
           for r in rows["issue"] + rows["pullrequest"]),
       "the temporal point is created_at, never the later updated_at")
    if mode == "e2e":
        # EVERY row, not the first one that matched and then `break`.
        off = []
        for r in rows["issue"]:
            payload = want["issues"].get(props(r).get("issueId"))
            if payload and props(r).get("at") != payload["created_at"]:
                off.append(payload["number"])
        ok(not off, "every issue sits at the recording's own created_at "
                    "(%d off)" % len(off))
        off = []
        for r in rows["pullrequest"]:
            payload = want["pulls"].get(props(r).get("pullRequestId"))
            if payload and props(r).get("at") != payload["created_at"]:
                off.append(payload["number"])
        ok(not off, "every pull request sits at the recording's own "
                    "created_at (%d off)" % len(off))
    ok(all(props(r).get("at") for r in rows["review"]
           if props(r).get("state") != "pending"),
       "every submitted review sits at its submitted_at")

    print("--> hydration patched, it did not duplicate")
    hydrated = [r for r in rows["pullrequest"] if props(r).get("additions") is not None]
    # EVERY row in e2e mode, where a recording exists for every pull request.
    # In seed mode the cut of `raw/` is partial by construction, so the claim
    # is that hydration RAN, not that every object had a recording.
    ok(len(hydrated) == len(rows["pullrequest"]) if mode == "e2e"
       else bool(hydrated),
       "%d of %d pull requests carry the diff counts only the full read has"
       % (len(hydrated), len(rows["pullrequest"])))
    ok(all(props(r).get("pullRequestTitle") for r in hydrated),
       "and the same rows still carry the search item's own fields")
    ok(all(props(r).get("head") and props(r).get("base") for r in hydrated),
       "head AND base landed on every hydrated row")
    ok(all(props(r).get("repository") for r in rows["issue"] + rows["pullrequest"]),
       "every item names its repository")

    print("--> the full repository read actually ran")
    # Four `GET /repos/{o}/{r}` recordings sat in the fixture set that nothing
    # ever requested: `subscribers_count` and friends had a declaration and no
    # writer. The repository hydration stage is what makes them land.
    full_read = [r for r in rows["repository"]
                 if props(r).get("subscribersCount") is not None]
    ok(bool(full_read),
       "%d repositories carry `subscribersCount`, which only the full read "
       "has" % len(full_read))
    if mode == "e2e" and want["repoPayloads"]:
        by_full = {props(r).get("fullName"): r for r in rows["repository"]}
        missed = [n for n in want["repoPayloads"] if n not in by_full]
        ok(not missed, "every recorded repository is a row (%s)" % missed)
        lic = [r for r in rows["repository"] if ref(props(r).get("license"))]
        ok(bool(lic) or not any(p.get("license")
                                for p in want["repoPayloads"].values()),
           "a repository's license is a row of its own, by reference")
        sec = [props(r).get("securityAndAnalysis") for r in rows["repository"]]
        sec = [x for x in sec if x]
        ok(all(v.get("status") in ("enabled", "disabled")
               for block in sec for v in block.values() if isinstance(v, dict)),
           "every security-and-analysis status is a spelled enum value")
        tops = [r for r in rows["repository"] if props(r).get("topics")]
        ok(bool(tops) or not any(p.get("topics")
                                 for p in want["repoPayloads"].values()),
           "a repository's topics survived as a list")

    print("--> empty is not absent")
    # `_take` used to delete any empty string. GitHub sends one for an unset
    # `gravatar_id` and for a description nobody wrote, and deleting them
    # loses the difference between "empty" and "never said".
    grav = [props(u).get("gravatarId") for u in rows["user"]]
    ok("" in grav or all(g is None for g in grav),
       "an empty gravatarId is stored as empty, not deleted")

    print("--> the parent-issue link was RESOLVED, not erased")
    if mode == "e2e" and want["parents"]:
        linked = [r for r in rows["issue"] if ref(props(r).get("parentIssue"))]
        ok(bool(linked),
           "%d issues name their parent by reference, from `parent_issue_url` "
           "(%d recordings carry one)" % (len(linked), len(want["parents"])))

    # ---------------------------------------------------- T-022, first item
    print("--> the issue graph is EDGES, not only counts")
    by_issue_id = {props(r).get("issueId"): r for r in rows["issue"]}
    for edge in ("subIssues", "blockedBy", "blocking"):
        expect = want[edge]
        if mode == "e2e" and expect:
            wrong, checked = [], 0
            for parent_id, kids in sorted(expect.items()):
                row = by_issue_id.get(parent_id)
                if row is None:
                    wrong.append("issue %s is not a row" % parent_id)
                    continue
                got_ids = [props(by_path[p][1]).get("issueId")
                           for p in refs(props(row).get(edge))
                           if p in by_path]
                checked += 1
                if got_ids != kids:
                    wrong.append("%s: %s != %s" % (parent_id, got_ids, kids))
            ok(not wrong,
               "%s: %d issues carry the recording's own edge list, in "
               "GitHub's order (%s)" % (edge, checked, wrong[:2] or "all"))
            ok(all(by_path.get(p, ("?",))[0] == "issue"
                   for pid in expect for p in refs(props(
                       by_issue_id.get(pid) or {}).get(edge))),
               "%s: every edge resolves to an issue ROW, minted from the "
               "endpoint's own whole payload" % edge)
        elif mode == "e2e":
            # DECLARED AND EXTRACTED, NOT EXERCISED. The owner's GitHub holds
            # no issue dependency at all, so there is no recording to fork and
            # the claim made here is the honest one: nothing wrote the
            # property, and — because the summaries count zero — the sync
            # never asked, which is what keeps a repository that has never
            # used dependencies off the account's `unreachable` list.
            ok(all(edge not in props(r) for r in rows["issue"]),
               "%s: no recording carries one, and nothing invented one" % edge)
    if mode == "e2e" and runner_mock:
        st, log, _ = api.call("GET", runner_mock + "/__mock/requests")
        asked = [e.get("path", "") for e in (log or {}).get("requests") or []] \
            if isinstance(log, dict) else []
        ok(not [p for p in asked if "/dependencies/" in p],
           "no dependency read was made: a summary of zero is already the "
           "whole answer, and the endpoint would 404 for a repository that "
           "never used the feature")
        ok(bool([p for p in asked if p.endswith("/sub_issues")]),
           "the sub-issue read WAS made, for the issues whose summary counts "
           "them")

    # --------------------------------------------------- T-022, second item
    print("--> the catalogue reads completed the licence and the code of conduct")
    if mode == "e2e":
        lic_rows = {props(r).get("key"): r for r in rows["license"]}
        coc_rows = {props(r).get("key"): r for r in rows["codeofconduct"]}
        for what, wanted, got_rows, whole in (
                ("license", want["licenses"], lic_rows, "body"),
                ("codeofconduct", want["cocs"], coc_rows, "body")):
            if not wanted:
                continue
            missing = [k for k in wanted if k not in got_rows]
            ok(not missing, "%s: every catalogue recording is a row (%s)"
               % (what, missing))
            thin = [k for k in wanted
                    if not props(got_rows.get(k) or {}).get(whole)]
            ok(not thin,
               "%s: %d rows carry `%s`, which ONLY GET %s/{key} has (%s)"
               % (what, len(wanted) - len(thin), whole,
                  "/licenses" if what == "license" else "/codes_of_conduct",
                  thin))
        if want["licenses"]:
            full = [r for r in rows["license"] if props(r).get("permissions")]
            ok(bool(full),
               "a licence carries its permissions / conditions / limitations "
               "lists, which the repository's embedded object does not")
            # THE `other` LICENCE IS THE GUARD. GitHub gives it no `url` and
            # `/licenses/other` is a 404, so the row stays at the five
            # embedded fields and the sync must never have asked.
            embedded = [k for k, r in lic_rows.items()
                        if k not in want["licenses"]]
            ok(all(not props(lic_rows[k]).get("body") for k in embedded),
               "a licence with no endpoint (%s) keeps the embedded half and "
               "nothing more" % (embedded or "none in this cut"))

    # ---------------------------------------------------- T-022, third item
    print("--> the schema-only declarations are declared")
    declared = {}
    for r in all_records(api, "substrate.reamde.dev/core/kind"):
        if r["id"].startswith(AUTHORITY + "/"):
            declared[r["id"].rsplit("/", 1)[1]] = json.dumps(props(r))
    if declared.get("pullrequest"):
        ok('"stack"' in declared["pullrequest"],
           "pullrequest.stack is declared — GitHub's OpenAPI description "
           "carries it on the selected schema, and version 15 left it out as "
           "an UNVERIFIED exclusion")
        ok(all("stack" not in props(r) for r in rows["pullrequest"]),
           "and no row carries one: the owner stacks nothing, so it is "
           "declared and not exercised")
    if declared.get("issue"):
        ok(all('"%s"' % e in declared["issue"]
               for e in ("subIssues", "blockedBy", "blocking")),
           "the three graph edges are declared on `issue` — and on the issue "
           "alone, because GitHub hangs both graphs off an issue")

    # ---------------------------------------------------------- second pass
    # Served by a mock of OUR OWN: GitHub's incremental cursor is a term
    # inside `q` (`updated:>=…`), so the second sync's query is a function of
    # when the first one ran. The platform's mock relaxes that term
    # (mockserver.QUERY_RELAXATIONS), and this second server is what lets the
    # scenario hold the recordings still while it asserts.
    from mock import serve

    staging = pathlib.Path(tempfile.mkdtemp()) / "recordings"
    shutil.copytree(recdir, staging)
    api_base, srv, mock = serve(staging)
    try:
        repoint(api, ckind, api_base)

        print("--> a second sync is idempotent")
        before = {name: {r["id"] for r in rows[name]} for name in MIRRORS}
        before_props = {r["id"]: props(r)
                        for name in MIRRORS for r in rows[name]}
        acct2 = force_sync(api, akind, aid)
        # Not just "something newer stamped": the run must have acted on THIS
        # request and finished ok. A failed run that changed no ids used to
        # pass this whole section.
        ok(str(acct2.get("syncStatus", "")).startswith("ok"),
           "the second sync finished ok (%s)" % acct2.get("syncStatus"))
        ok(acct2.get("syncRequestedAck") and
           acct2.get("syncRequestedAck") == acct2.get("syncRequestedAt"),
           "and it acknowledged the request it was asked to serve")
        trait_ok(acct2, "ok", "after the requested sync")
        ok(acct2.get("syncRequestedAck") == acct2.get("syncRequestedAt"),
           "the trait's request pair agrees — a single-function provider "
           "answers with syncRequestedAck alone (0085)")
        after_rows = {name: all_records(api, KINDS[name]) for name in MIRRORS}
        after = {name: {r["id"] for r in after_rows[name]} for name in MIRRORS}
        ok(after == before,
           "every row is the one the first sync minted: %s"
           % {k: len(v) for k, v in after.items()})
        still = [r for r in after_rows["pullrequest"]
                 if props(r).get("additions") is not None]
        ok(len(still) == len(hydrated),
           "the second pass did not put a thin search item over a hydrated row")
        # Idempotence is about VALUES, not id sets: a run that kept the rows
        # and corrupted every other property used to pass.
        drifted = []
        for name in MIRRORS:
            for r in after_rows[name]:
                was = before_props.get(r["id"])
                if was is None:
                    continue
                now = props(r)
                for k, v in was.items():
                    if k in ("updatedAt",) or k not in now:
                        continue
                    if now[k] != v:
                        drifted.append("%s.%s" % (r["id"], k))
        ok(not drifted,
           "no property changed value across two identical syncs (%d drifted: "
           "%s)" % (len(drifted), drifted[:6]))

        # ------------------------------------------------ substrate #569/#570
        # The reason the mirrors may point at `user` at all: a reference may
        # pin a mapping's SOURCE kind (#569), and the mapping synthesises the
        # subject slot on that kind rather than the provider declaring one
        # (#570). Both are asserted here, against the real people mapping.
        print("--> the people mapping installs over the mirrors (#569/#570)")
        import yaml  # noqa: E402 — only the proof needs it
        root = pathlib.Path(os.environ.get("SUBSTRATE_E2E_REPO")
                            or HERE.parents[3])
        docs = []
        for f in (root / "samples/people/bundle.yaml",
                  root / "samples/people/person.yaml",
                  root / "samples/people/organization.yaml",
                  root / "samples/people/team.yaml",
                  root / "samples/people/mappings.github.yaml"):
            if f.exists():
                docs += [d for d in yaml.safe_load_all(f.read_text()) if d]
        # Only the GitHub mapping: another provider's mapping would name a
        # kind this repository does not hold.
        keep_tail = ("/person", "/organization", "/team", "/githubuserperson")
        docs = [d for d in docs
                if d.get("kind") != "substrate.reamde.dev/core/recordmapping"
                or "github" in (d.get("metadata") or {}).get("id", "")]
        for d in docs:
            if d.get("kind") == "substrate.reamde.dev/core/bundle":
                # Only what this repository can resolve: another provider's
                # mapping names a kind it does not hold.
                d["data"]["installs"] = [
                    i for i in d["data"].get("installs") or []
                    if i.endswith(keep_tail)]
        st, body, _ = api.call("POST", "/api/v1/vocabulary/apply",
                               {"documents": docs})
        ok(st < 300, "the people closure and githubuserperson applied (%s) %s"
           % (st, "" if st < 300 else json.dumps(body)[:300]))
        if st < 300:
            ok(True, "#569: references pinned at github/user survived a "
                     "mapping declaring it as a source")
            # #570: the next write of a user row gains the synthesised slot.
            force_sync(api, akind, aid)
            after_users = all_records(api, KINDS["user"])
            with_subject = [u for u in after_users if props(u).get("person")]
            ok(bool(with_subject),
               "#570: %d user rows carry a synthesised `person` slot the "
               "bundle never declared" % len(with_subject))
            issues_now = all_records(api, KINDS["issue"])
            assignee_refs = [a for r in issues_now
                             for a in refs(props(r).get("assignees"))]
            ok(bool(assignee_refs),
               "and `issue.assignees → user[]` still admits (%d references)"
               % len(assignee_refs))

        # ------------------------------------- the failure matrix (T-022)
        # UNBLOCKED: the mock takes injected-failure rules now
        # (`mockserver.set_faults`), so 429 / 403 / 401 are properties of the
        # RUN and not a second directory of hand-written error bodies. This is
        # the one path the transport work of version 15 added and nothing
        # exercised: a rate limit is WORK TO RESUME, and the proof is that the
        # instant GitHub named survives onto the account and that the very
        # next clean run finishes ok with the same rows.
        print("--> the failure matrix, injected")
        rows_before = {name: {r["id"] for r in all_records(api, KINDS[name])}
                       for name in MIRRORS}

        mock.set_faults([{"match": "GET /search/issues*", "status": [429],
                          "headers": {"Retry-After": "120",
                                      "x-ratelimit-remaining": "0",
                                      "x-ratelimit-resource": "search"}}])
        limited = force_sync(api, akind, aid)
        status = str(limited.get("syncStatus") or "")
        ok(status.startswith("erroring") and "rate limited" in status,
           "a 429 with `Retry-After` stops the run and SAYS so (%s)"
           % status[:90])
        # 0085 spells a rate limit `throttled`, not `erroring`: it is work to
        # resume, and the Connections page renders a wait rather than a break.
        trait_ok(limited, "throttled", "after an injected 429")
        ok(bool(limited.get("syncError")) and "rate limited" in
           str(limited.get("syncError")),
           "syncError carries the cause (%s)"
           % str(limited.get("syncError"))[:70])
        ok(bool(limited.get("syncErrorAt")), "and syncErrorAt says when")
        ok("retry after 20" in status or "retry after 2" in status
           or "retry after" in status,
           "and it carries the instant GitHub named, not a bare failure")
        ok(limited.get("syncRequestedAck") == limited.get("syncRequestedAt"),
           "the erroring run still acknowledged the request — an unacked one "
           "re-fires against the same failure forever")

        mock.set_faults([{"match": "GET /user", "status": [401]}])
        revoked = force_sync(api, akind, aid)
        status = str(revoked.get("syncStatus") or "")
        ok(status.startswith("erroring") and "unauthorized" in status,
           "a 401 reads as a revoked grant, not as `HTTP 401` (%s)"
           % status[:90])
        trait_ok(revoked, "erroring", "after an injected 401")
        ok("unauthorized" in str(revoked.get("syncError") or ""),
           "syncError reads the same way (%s)"
           % str(revoked.get("syncError"))[:70])

        # ------------------------------- T-077: a backlog does not eat the walk
        # Production, 2026-09-20 (github 20): a failed run had parked its
        # owner logins (`_save_pending` at licensesFetch), and every re-drive
        # after it logged one line — `resuming 27 parked hydration entries` —
        # drained them, stamped `ok` with `syncCursors: None`, and never
        # searched: versions 15-20 made a run with a backlog PURE hydration
        # and left the searches to "the first round that starts clean", and
        # a re-drive is one run. The same mechanism ate the first restricted
        # run of this scenario (the 429 above parks the same logins). Version
        # 21 drains the backlog first and then walks, in the same run.
        print("--> a run that starts with a backlog still walks the searches (T-077)")
        mock.set_faults([{"match": "GET /search/issues*", "status": [500]}])
        broke = force_sync(api, akind, aid)
        ok(str(broke.get("syncStatus") or "").startswith("erroring"),
           "a 500 on the first search stops that run (%s)"
           % str(broke.get("syncStatus"))[:80])
        parked = broke.get("syncPending") or {}
        ok(bool(parked),
           "and leaves the hydration work it had already found parked on the "
           "account — production's shape (%s)"
           % {k: len(v) for k, v in parked.items()})
        mock.set_faults([])
        api.call("DELETE", api_base + "/__mock/requests")
        walked = force_sync(api, akind, aid)
        ok(str(walked.get("syncStatus") or "") == "ok",
           "the re-drive ends ok (%s)" % str(walked.get("syncStatus"))[:80])
        st, log, _ = api.call("GET", api_base + "/__mock/requests")
        reqs = (log or {}).get("requests") or [] if isinstance(log, dict) else []
        paths = [str(e.get("path") or "") for e in reqs]
        hyd = [i for i, pth in enumerate(paths) if pth.startswith("/users/")]
        srch = [i for i, pth in enumerate(paths) if pth == "/search/issues"]
        ok(bool(srch),
           "and the SAME run walked the searches — %d pages, where version 20 "
           "made none" % len(srch))
        ok(bool(hyd) and bool(srch) and hyd[0] < srch[0],
           "the parked entries were hydrated BEFORE the first search "
           "(first user read at %s, first search at %s)" % (hyd[:1], srch[:1]))
        ok(set(walked.get("syncCursors") or {}) >= {"issues", "pulls",
                                                    "pullsReview"},
           "so the run stamped watermarks for every search stage (%s)"
           % sorted(walked.get("syncCursors") or {}))
        ok(not walked.get("syncPending"),
           "and nothing is left pending (%s)" % (walked.get("syncPending") or "empty"))

        # ------------------------------------------ T-077: a restricted org
        # SELF-CONTAINED: its own rules posted and cleared, and it starts from
        # an account it has settled itself — no backlog, no refusal tally, no
        # fault standing — so nothing the blocks above leave behind (a parked
        # queue, an erroring stamp) can decide what these runs do.
        mock.set_faults([])
        settled = force_sync(api, akind, aid)
        ok(str(settled.get("syncStatus") or "").startswith("ok")
           and not settled.get("syncPending") and not settled.get("syncSkipped"),
           "settled: the account is ok with no backlog and no refusal tally "
           "before the restricted runs (%s)" % str(settled.get("syncStatus"))[:60])
        # GitHub's third refusal: an organization that has not approved the
        # OAuth app answers every read of ITS resources with a 403 whose body
        # says so — the rate limit is fine, nothing is to be waited for, and
        # asking again cannot change the answer until the owner acts at
        # GitHub. The SEARCH still lists the org's items, so on 2026-09-20
        # the owner's live account died on the first item it hydrated and one
        # org emptied the whole mirror. The recorded body (`e2e/restricted.json`,
        # pseudonymised to the fixture org) is injected over every read under
        # that org; the recordings underneath stay the success path.
        #
        # Two runs, because the sync meets the refusal in two places. With
        # `enabledRepos` OFF the `repos` listing does not run, so the search
        # item's repository has to be RESOLVED (`GET /repos/{o}/{r}`) — the
        # ingest path, where the item is not minted at all. With it ON the
        # listing names the repository first, the item is minted thin off the
        # search hit and the refusal lands on its hydration reads instead.
        # Either way exactly one request reaches the org.
        print("--> a restricted organization is skipped, not fatal (T-077)")
        rule = json.loads((HERE / "restricted.json").read_text())
        org = rule["org"]
        org_items = {"issues": 0, "pulls": 0}
        for f in _recs(recdir, "GET_search_issues*.json"):
            for it in json.loads(f.read_text()).get("items") or []:
                if ("/repos/%s/" % org) in (it.get("repository_url") or ""):
                    org_items["pulls" if "type_pr" in f.name else "issues"] += 1
        ok(sum(org_items.values()) >= 2,
           "the cut carries %d search hits under `%s` to refuse"
           % (sum(org_items.values()), org))

        def restricted_run(where, toggles):
            st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (akind, aid),
                                   {"properties": toggles})
            if st >= 400:
                raise SystemExit("could not set %s: %s" % (toggles, json.dumps(body)[:200]))
            api.call("DELETE", api_base + "/__mock/requests")
            mock.set_faults(rule["rules"])
            acct_r = force_sync(api, akind, aid)
            status = str(acct_r.get("syncStatus") or "")
            ok(status.startswith("ok"),
               "%s: the run completes `ok`, the refusal did not stop it (%s)"
               % (where, status[:100]))
            ok("items skipped" in status and org in status
               and "restricts the OAuth app" in status
               and "third-party access" in status,
               "%s: and the status counts the skip, names the org and says "
               "what to do (%s)" % (where, status[:200]))
            trait_ok(acct_r, "ok", where)
            ok(str(acct_r.get("syncMessage") or "") == status,
               "%s: the trait's message is the same sentence" % where)
            ok(not acct_r.get("syncPending"),
               "%s: nothing of the org's is left parked as pending work (%s)"
               % (where, acct_r.get("syncPending") or "empty"))
            held = ((acct_r.get("syncSkipped") or {}).get("restricted") or {})
            row = held.get(org) or {}
            ok(int(row.get("items") or 0) >= 1 and row.get("repo", "").startswith(org + "/"),
               "%s: syncSkipped carries the org's tally and one repository to "
               "re-ask (%s)" % (where, json.dumps(held)[:160]))
            st, log, _ = api.call("GET", api_base + "/__mock/requests")
            reqs = (log or {}).get("requests") or [] if isinstance(log, dict) else []
            under = [e for e in reqs
                     if str(e.get("path") or "").startswith("/repos/%s/" % org)]
            ok(len(under) == 1 and under[0].get("match") == "fault",
               "%s: the org was asked ONCE and refused; every other item of "
               "it was skipped without a request (%d asked: %s)"
               % (where, len(under), [e.get("path") for e in under][:3]))
            at = reqs.index(under[0]) if under else len(reqs)
            went_on = [e for e in reqs[at + 1:]
                       if str(e.get("path") or "").startswith("/repos/")
                       and e.get("match") != "fault"]
            ok(bool(went_on),
               "%s: and the run kept going — %d reads of other repositories "
               "were served after the refusal" % (where, len(went_on)))
            rows_mid = {name: {r["id"] for r in all_records(api, KINDS[name])}
                        for name in MIRRORS}
            ok(rows_mid == rows_before,
               "%s: every other row is still there, nothing duplicated" % where)
            return acct_r, int(row.get("items") or 0)

        first, tally1 = restricted_run(
            "ingest path (repos listing off)", {"enabledRepos": False})
        ok(tally1 >= sum(org_items.values()),
           "ingest path: every search hit under the org was counted "
           "(%d >= %d)" % (tally1, sum(org_items.values())))
        second, tally2 = restricted_run(
            "hydration path (repos listing on)", {"enabledRepos": True})
        ok(tally2 > tally1,
           "the tally is CUMULATIVE across runs (%d after the first, %d after "
           "the second)" % (tally1, tally2))
        ok(int((second.get("syncProgress") or {}).get("pending") or 0) == 0,
           "the skipped hydration entries left the queue: syncProgress.pending "
           "is 0")

        # -------------------------------------------------------- the heal
        # The org approves the app. The next run's probe (`GET /repos/{o}/{r}`
        # for the remembered repository) answers, the search watermarks are
        # dropped, and the window the refusal hid is re-walked from the
        # backfill floor — which is the only way the skipped items can ever
        # land, since they sit behind the watermark.
        mock.set_faults([])
        api.call("DELETE", api_base + "/__mock/requests")
        healed = force_sync(api, akind, aid)
        ok(str(healed.get("syncStatus") or "").startswith("ok"),
           "and the next clean run finishes ok (%s)" % healed.get("syncStatus"))
        trait_ok(healed, "ok", "after the run that healed it")
        ok((healed.get("syncErrorAt") or "") <= (healed.get("lastSyncedAt") or ""),
           "syncError is the LAST error, not a live one: syncErrorAt is "
           "older than the run that just finished ok")
        ok("restriction lifted" in str(healed.get("syncStatus") or "")
           and org in str(healed.get("syncStatus") or ""),
           "T-077: the probe found the org approved the app and the status "
           "says the window was re-walked (%s)"
           % str(healed.get("syncStatus"))[:120])
        ok(not ((healed.get("syncSkipped") or {}).get("restricted") or {}),
           "and syncSkipped is cleared (%s)" % json.dumps(healed.get("syncSkipped")))
        st, log, _ = api.call("GET", api_base + "/__mock/requests")
        reqs = (log or {}).get("requests") or [] if isinstance(log, dict) else []
        paths = [str(e.get("path") or "") for e in reqs]
        probe = [i for i, pth in enumerate(paths)
                 if pth.startswith("/repos/%s/" % org)]
        searches = [i for i, pth in enumerate(paths) if pth == "/search/issues"]
        ok(bool(probe) and bool(searches) and probe[0] < searches[0],
           "the probe was ONE read of the remembered repository, before any "
           "search (%d org reads, first at %s; first search at %s)"
           % (len(probe), probe[:1], searches[:1]))
        def _q(e):
            qs = urllib.parse.parse_qs(str(e.get("query") or ""))
            return (qs.get("q") or [""])[0]
        cold = [e for e in reqs if str(e.get("path") or "") == "/search/issues"
                and "updated:" not in _q(e)]
        ok(len(cold) == len(searches) and searches,
           "every search of the healing run was COLD — no `updated:>=` floor, "
           "the stored watermarks were dropped (%d of %d)"
           % (len(cold), len(searches)))
        ok(bool([e for e in reqs if str(e.get("path") or "").startswith(
            "/repos/%s/" % org) and "/pulls/" in str(e.get("path") or "")]),
           "and the org's pull requests were hydrated at last")
        cursors3 = healed.get("syncCursors") or {}
        ok(set(cursors3) >= {"issues", "pulls", "pullsReview"},
           "the re-walk stamped fresh watermarks for every search stage (%s)"
           % sorted(cursors3))
        rows_after = {name: {r["id"] for r in all_records(api, KINDS[name])}
                      for name in MIRRORS}
        ok(rows_after == rows_before,
           "no row was lost or duplicated across seven failing or refused syncs")
        mock.set_faults([])
    finally:
        srv.shutdown()
        if runner_mock:
            repoint(api, ckind, runner_mock)

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
