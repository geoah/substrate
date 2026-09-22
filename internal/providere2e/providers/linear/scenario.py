#!/usr/bin/env python3
"""What the Linear mirrors must be true of, once the runner has synced them.

`tools/e2e.py` owns the run — it serves the recordings, rewires the closure at
the mock, writes the config record and the account, and waits for the
on-connect trigger's first sync to settle. This file is what runs after that,
in both modes:

    the e2e run            MODE=e2e   providers/linear/fixtures
    a seed run (mneme tree only)  MODE=seed  raw/linear -> the owner's repository

THE EXPECTED INVENTORY IS READ OFF THE RECORDINGS, never hard-coded: the
fixture set is a CUT of the owner's real workspace (`tools/rawpull/linear.py
--cut`), so re-cutting it changes every count, and a scenario that hard-codes
them is a scenario that fails for the wrong reason. In `seed` mode the pull is
incomplete BY CONSTRUCTION (it holds what the puller reached), so the oracle
is what the sync actually finished, not every page the pull happens to hold
(`docs/provider-practices.md` §1).

WHAT IT PROVES

  1. every node the recordings carry is one row, at LINEAR's own UUID, once;
  2. NOTHING the guide forbids is declared or written: no `raw`, no `person` /
     `task` slot, no `assigneeEmail` or other scalar copy of a referent, no
     sync bookkeeping on a mirror — the account holds the watermark, the
     parked walk and the stamps, and nothing else does;
  3. every relation is a REFERENCE that resolves to a row of its pinned kind,
     including the ones inside an object (`sharedAccess.sharedWithUsers`) and
     the ones composed from an id scalar (`team.autoCloseState`);
  4. the cases the cut was built for, each found BY REFERENCE and not by
     string: an assigned issue, one in a project, one in a cycle, one with
     labels, subscribers, comments and reactions, a completed one, a canceled
     one, an archived one, an estimated one, one with a due date, one an
     integration created, one that moved team, a comment with a parent and a
     comment with reactions;
  5. every enum holds a value the declaration spells, and `at` is `createdAt`
     on every mirror but `cycle`, where `at`/`endsAt` are the cycle's span;
  6. a field the bundle declares and does NOT fetch is never written;
  7. a nested connection that came back capped left its property ALONE rather
     than writing a short list;
  8. a second, forced sync is idempotent — same rows, same ids, same content;
  9. the engine's part: a reference pinned at a mapping's SOURCE kind survives
     (#569) and `user` rows gain a `person` slot this bundle never declared
     (#570).
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

AUTHORITY = "providers.substrate.reamde.dev/linear"
MIRRORS = ("organization", "team", "user", "workflowstate", "issuelabel",
           "projectstatus", "project", "cycle", "issue", "comment", "reaction")
KINDS = {k: AUTHORITY + "/" + k for k in MIRRORS}
ID_PROP = {"organization": "organizationId", "team": "teamId",
           "user": "userId", "workflowstate": "workflowStateId",
           "issuelabel": "issueLabelId", "projectstatus": "projectStatusId",
           "project": "projectId", "cycle": "cycleId", "issue": "issueId",
           "comment": "commentId", "reaction": "reactionId"}
# The connection each stage's recordings carry, and the kind it feeds.
STAGE_FIELD = {"Teams": ("teams", "team"), "Users": ("users", "user"),
               "Workflowstates": ("workflowStates", "workflowstate"),
               "Labels": ("issueLabels", "issuelabel"),
               "Projects": ("projects", "project"),
               "Cycles": ("cycles", "cycle"),
               "Issues": ("issues", "issue")}
# Nothing the guide forbids may appear on any mirror row: a consumer slot, a
# scalar copy of a referent, a blob, or any of the account's bookkeeping.
FORBIDDEN = ("raw", "person", "task", "episode", "assigneeEmail",
             "assigneeName", "creatorEmail", "teamKey", "teamName",
             "stateName", "stateType", "projectName", "syncCursor",
             "syncCursors", "syncPending", "lastSyncedAt", "lastCompletedAt",
             "syncStatus", "syncRequestedAt", "syncRequestedAck", "isMine",
             "fromOwner", "mentionsOwner",
             # The core `sync` trait's twelve (substrate decision 0085) are
             # the ACCOUNT's and nothing else's: a mirror row carrying one
             # would mean the trait had been bound to the wrong kind.
             "syncState", "syncMessage", "lastSyncStartedAt",
             "lastSyncDurationMs", "syncPaused", "syncProgress", "syncError",
             "syncErrorAt", "syncStreams")

# `never | running | ok | erroring | throttled` — held in prose and in the
# engine's constants, because a trait contracts a datatype and not a value set.
SYNC_STATES = ("never", "running", "ok", "erroring", "throttled")
# Declared, NOT fetched (bundle.yaml says so, and says why). None may ever be
# written: the claim "every field is declared" is about the DECLARATION, and
# the claim "mirrored" is about the SYNC, and this is the second one.
NOT_FETCHED = {
    "user": ("inviteHash", "identityProviderId"),
    "team": ("inviteHash", "progressHistory", "currentProgress", "facetsIds",
             "postsIds", "resourceSectionsIds", "pinnedResourcesIds",
             "private", "draftWorkflowState", "protectedBy"),
    "organization": ("samlSettings", "scimSettings", "authSettings",
                     "themeSettings", "aiProviderConfiguration",
                     "linearAgentSettings", "codingAgentSettings",
                     "facetsIds", "subscriptionId", "allowedAuthServices"),
    "project": ("previousIdentifiers", "progressHistory", "currentProgress",
                "contentState", "syncedWith", "state", "facetsIds"),
    "issue": ("descriptionState", "activitySummary", "syncedWith",
              "boardOrder", "summaryId"),
    "comment": ("bodyData", "threadSummary", "syncedWith", "agentSessionId"),
    "cycle": ("progressHistory", "currentProgress"),
    "issuelabel": ("organization",),
}

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
        ok(prog.get("pending") == (1 if acct.get("syncPending") else 0),
           "%s: and its `pending` agrees with the parked walk on the account "
           "(%s)" % (where, prog.get("pending")))


def ref(value):
    if isinstance(value, dict):
        return value.get("ref") or ""
    return value if isinstance(value, str) else ""


def refs(value):
    if isinstance(value, list):
        return [r for r in (ref(v) for v in value) if r]
    r = ref(value)
    return [r] if r else []


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
        out += body.get("records") or []
        cursor = ((body.get("pageInfo") or {}).get("endCursor") or "")
        if not cursor or not (body.get("pageInfo") or {}).get("hasNextPage"):
            return out


def account(api, kind, aid):
    st, body, _ = api.call("GET", "/api/v1/%s/%s" % (kind, aid))
    if st != 200:
        raise SystemExit("reading the account failed: HTTP %s" % st)
    return props(body)


def recordings(d: pathlib.Path) -> dict:
    """What the recordings SAY the mirror must hold, by kind and by id."""
    want = {name: {} for name in MIRRORS}
    want["capped"] = []          # (issue id, connection) the API cut short
    for f in sorted(d.glob("POST_graphql__op-*.json")):
        stem = f.name.split("op-", 1)[1].rsplit("Page", 1)[0]
        doc = json.loads(f.read_text())
        data = doc.get("data") or {}
        if stem == "Viewer":
            v = data.get("viewer") or {}
            if v.get("id"):
                want["user"][v["id"]] = v
            continue
        if stem == "Organization":
            org = data.get("organization") or {}
            if org.get("id"):
                want["organization"][org["id"]] = org
            for s in org.get("projectStatuses") or []:
                want["projectstatus"][s["id"]] = s
            continue
        field, kind = STAGE_FIELD.get(stem, (None, None))
        if not field:
            continue
        for node in (data.get(field) or {}).get("nodes") or []:
            want[kind][node["id"]] = node
            if kind != "issue":
                continue
            for c in (node.get("comments") or {}).get("nodes") or []:
                want["comment"][c["id"]] = c
                for r in c.get("reactions") or []:
                    want["reaction"][r["id"]] = r
            for r in node.get("reactions") or []:
                want["reaction"][r["id"]] = r
            for conn in ("labels", "subscribers", "comments"):
                if ((node.get(conn) or {}).get("pageInfo") or {}).get("hasNextPage"):
                    want["capped"].append((node["id"], conn))
    return want


def force_sync(api, kind, aid, timeout=420, drained=True):
    """Ask for a sync and wait for a stamp newer than the one already there.

    `syncRequestedAt` is the only thing that can ask: the on-connect trigger is
    guarded to fire once per account, the schedule fires at most hourly, and a
    direct `function call` runs ONE invocation and hands the paged checkpoint
    back to its caller rather than draining it.

    `drained=False` waits for the STAMP alone. A run that fails PARKS its walk
    on purpose (the page it died on is where the next one resumes), so the
    failure matrix must not wait for `syncPending` to clear — it never will,
    and the wait would burn the whole timeout before every assertion.
    """
    before = account(api, kind, aid).get("lastSyncedAt") or ""
    stamp = time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime())
    st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (kind, aid),
                           {"properties": {"syncRequestedAt": stamp}})
    if st >= 400:
        raise SystemExit("could not request a sync: %s" % json.dumps(body)[:300])
    deadline = time.time() + timeout
    while time.time() < deadline:
        time.sleep(3)
        now = account(api, kind, aid)
        if (now.get("lastSyncedAt") or "") <= before:
            continue
        if drained and now.get("syncPending"):
            continue
        return now
    return account(api, kind, aid)


def set_faults(api, mock_url, rules):
    """Configure the RUNNER's mock with an injected-failure matrix.

    The statuses a failure test needs are a property of the RUN, not of the
    recordings (`docs/provider-practices.md` §3): a second directory of
    hand-written error bodies would drift from the success path's fixtures.
    Linear POSTs every request to one path, so a rule names the operation
    through `contains` — the body carries `"operationName": "<Stage>Page<N>"`,
    which is also what the recording is named for.
    """
    st, body, _ = api.call("POST", mock_url + "/__mock/faults",
                           {"rules": rules})
    if st >= 400:
        raise SystemExit("could not set the mock's faults: %s"
                         % json.dumps(body)[:300])


def main():
    server = os.environ["SUBSTRATE_SERVER"]
    token = os.environ["SUBSTRATE_TOKEN"]
    mode = os.environ.get("MODE", "e2e")
    akind = os.environ.get("ACCOUNT_KIND", AUTHORITY + "/account")
    aid = os.environ.get("ACCOUNT_ID", "e2e")
    recdir = pathlib.Path(os.environ["RECORDINGS"])
    api = API(server, token)
    want = recordings(recdir)

    print("--> the account carries the stamp, the cursors and nothing a mirror should")
    acct = account(api, akind, aid)
    ok(str(acct.get("syncStatus", "")).startswith("ok"),
       "the sync finished ok (%s)" % acct.get("syncStatus"))
    ok(bool(acct.get("lastSyncedAt")), "lastSyncedAt is stamped")
    ok(bool(acct.get("lastCompletedAt")),
       "lastCompletedAt is stamped — a walk ran out of work")
    ok(not acct.get("syncPending"),
       "no walk is parked (%s)" % json.dumps(acct.get("syncPending"))[:80])
    cursors = acct.get("syncCursors") or {}
    ok(isinstance(cursors, dict) and "issues" in cursors,
       "the issues stage stamped its watermark, in a keyed map: %s"
       % sorted(cursors))
    ok(set(cursors) <= {"issues"},
       "and only the INCREMENTAL stage has one — the workspace stages are "
       "walked whole (%s)" % sorted(cursors))

    # ---------------------------------- the core `sync` trait (T-084 / 0085)
    # The account kind binds [accountconfig, sync], so the Connections page,
    # `GET /api/v1/sync/status` and `substratectl sync status` read these
    # twelve without knowing anything about Linear. They are written BESIDE
    # the legacy `syncStatus` / `lastSyncedAt` strings, never instead of them.
    trait_ok(acct, "ok", "after the first sync")
    ok(str(acct.get("syncMessage") or "") == str(acct.get("syncStatus") or ""),
       "the trait's message and the legacy status are the same sentence on a "
       "good run (%r)" % str(acct.get("syncMessage"))[:60])
    ok(not acct.get("syncPaused"), "the account is not paused")
    ok("syncStreams" not in acct or not acct.get("syncStreams"),
       "no per-stream map: Linear is one stream and its stages are phases of "
       "it, reported in syncProgress (0085)")
    prog = acct.get("syncProgress") or {}
    ok(int(prog.get("total") or 0) > 0 and
       int(prog.get("done") or 0) == int(prog.get("total") or 0),
       "syncProgress counts the account's own enabled stages, and a walk "
       "that ran out of work finished all of them (%s/%s)"
       % (prog.get("done"), prog.get("total")))
    ok(prog.get("phase") == "idle",
       "and its phase is `idle`, not a stage it is still on (%r)"
       % prog.get("phase"))

    rows = {name: all_records(api, KINDS[name]) for name in MIRRORS}
    by_ref = {}
    for name in MIRRORS:
        for r in rows[name]:
            by_ref[KINDS[name] + "/" + r["id"]] = (name, r)
    # The ACCOUNT is a row every mirror points at, and it is not a mirror.
    by_ref[akind + "/" + aid] = ("account", None)

    print("--> the account's own identity resolves")
    user_ref, org_ref = ref(acct.get("user")), ref(acct.get("organization"))
    ok(user_ref in by_ref and by_ref[user_ref][0] == "user",
       "account.user resolves to a user row (%s)" % user_ref)
    ok(org_ref in by_ref and by_ref[org_ref][0] == "organization",
       "account.organization resolves to an organization row")
    copies = {k for k in acct
              if k in ("viewerName", "viewerEmail", "organizationName",
                       "urlKey", "teamKey", "userName", "userEmail")}
    ok(not copies, "the account copies neither the viewer's name nor the "
       "workspace's key — both are references (%s)" % sorted(copies))

    print("--> one row per node, at Linear's own UUID")
    for name in MIRRORS:
        have = {props(r).get(ID_PROP[name]) for r in rows[name]}
        have.discard(None)
        if mode == "e2e":
            missing = set(want[name]) - have
            extra = have - set(want[name])
            ok(not missing, "%s: %d rows, every recorded node mirrored "
               "(missing %d)" % (name, len(rows[name]), len(missing)))
            ok(not extra, "%s: no row the recordings do not describe (%d)"
               % (name, len(extra)))
        else:
            print("    --    %s: %d rows (%d in the pull)"
                  % (name, len(rows[name]), len(want[name])))
        ok(len(have) == len(rows[name]),
           "%s: every row has its own id, none shared" % name)

    print("--> nothing the guide forbids is written")
    for name in MIRRORS:
        carried = set()
        for r in rows[name]:
            carried |= set(props(r))
        stray = carried & set(FORBIDDEN)
        # A subject slot the ENGINE synthesised from an installed mapping is
        # the mapping owner's, not the provider's.
        ok(not stray, "%s: no forbidden property (%s)" % (name, sorted(stray)))
        leaks = {k for k in carried
                 if k.endswith("Email") or k.endswith("Login")
                 or k.endswith("Emails") or k == "raw"}
        ok(not leaks, "%s: no scalar copy of a referent (%s)"
           % (name, sorted(leaks)))
        never = set(NOT_FETCHED.get(name, ())) & carried
        ok(not never, "%s: nothing DECLARED-NOT-FETCHED was written (%s)"
           % (name, sorted(never)))

    print("--> every reference resolves to a row of its pinned kind")
    # SOME REFERENCES MAY LEGITIMATELY DANGLE, and each one is a row the
    # workspace holds that THIS account's walk cannot see: an issue's parent
    # updated before the window, a state or a label inherited from a team the
    # key cannot list, a sub-team the viewer is not a member of. The guide's
    # ruling for a target that may land later is "no mustExist, it heals on
    # the next walk" (kind-design §2's Slack `thread_ts` row,
    # provider-practices §6), so these are COUNTED and reported rather than
    # asserted away — and everything else must resolve.
    MAY_DANGLE = {("issue", "parent"), ("workflowstate", "team"),
                  ("workflowstate", "inheritedFrom"), ("team", "children"),
                  ("team", "ancestors"), ("team", "parent"),
                  ("team", "restrictedBy"), ("issuelabel", "inheritedFrom"),
                  ("issuelabel", "parent"), ("cycle", "inheritedFrom"),
                  ("issue", "sourceComment"), ("comment", "parent"),
                  ("comment", "resolvingComment"),
                  ("project", "convertedFromIssue")}
    dangling, checked, parents = [], 0, 0
    for name in MIRRORS:
        for r in rows[name]:
            for prop, value in props(r).items():
                for target in refs(value):
                    if not str(target).startswith(AUTHORITY + "/"):
                        continue
                    checked += 1
                    if target in by_ref:
                        continue
                    if (name, prop) in MAY_DANGLE:
                        parents += 1
                        continue
                    dangling.append("%s.%s -> %s" % (name, prop, target))
    ok(not dangling, "%d references resolve; %d dangle (%s)"
       % (checked, len(dangling), dangling[:4]))
    print("    --    %d references point outside this account's walk "
          "(issue.parent, an inherited state, a sub-team) — the case the "
          "guide admits" % parents)

    print("--> the relations the guide names, spelled as references")
    issues = rows["issue"]
    for prop, kind in (("team", "team"), ("state", "workflowstate"),
                       ("assignee", "user"), ("creator", "user"),
                       ("project", "project"), ("cycle", "cycle"),
                       ("labels", "issuelabel"), ("subscribers", "user")):
        hits = [t for r in issues for t in refs(props(r).get(prop))]
        good = [t for t in hits if by_ref.get(t, ("", None))[0] == kind]
        ok(bool(hits) and len(good) == len(hits),
           "issue.%s -> %s (%d references, %d resolve)"
           % (prop, kind, len(hits), len(good)))
    comments = rows["comment"]
    for prop, kind in (("issue", "issue"), ("user", "user")):
        hits = [t for r in comments for t in refs(props(r).get(prop))]
        ok(bool(hits), "comment.%s -> %s (%d references)" % (prop, kind, len(hits)))
    for prop, kind in (("status", "projectstatus"), ("teams", "team"),
                       ("members", "user")):
        hits = [t for r in rows["project"] for t in refs(props(r).get(prop))]
        ok(bool(hits), "project.%s -> %s (%d references)" % (prop, kind, len(hits)))
    ok(any(refs(props(r).get("team")) for r in rows["cycle"]),
       "cycle.team -> team")
    ok(any(refs(props(r).get("organization")) for r in rows["team"]),
       "team.organization -> organization")
    autoclose = [t for r in rows["team"] for t in refs(props(r).get("autoCloseState"))]
    ok(all(by_ref.get(t, ("", None))[0] == "workflowstate" for t in autoclose),
       "team.autoCloseState: an id SCALAR Linear ships is a reference here "
       "(%d)" % len(autoclose))

    print("--> the cases the cut was built for, found by reference")
    def anyissue(test, label):
        hits = [r for r in issues if test(props(r))]
        ok(bool(hits), "%s (%d)" % (label, len(hits)))
        return hits
    anyissue(lambda p: refs(p.get("assignee")), "an issue with an assignee")
    anyissue(lambda p: not p.get("assignee"), "an issue with no assignee")
    anyissue(lambda p: refs(p.get("project")), "an issue in a project")
    anyissue(lambda p: refs(p.get("cycle")), "an issue in a cycle")
    anyissue(lambda p: refs(p.get("labels")), "an issue with labels")
    anyissue(lambda p: refs(p.get("subscribers")), "an issue with subscribers")
    anyissue(lambda p: p.get("completedAt"), "a completed issue")
    anyissue(lambda p: p.get("estimate") is not None, "an estimated issue")
    anyissue(lambda p: p.get("dueDate"), "an issue with a due date")
    anyissue(lambda p: p.get("projectMilestoneId"),
             "an issue whose milestone is an id, the scope boundary the "
             "description names")
    ok(bool(rows["reaction"]), "reactions are their own kind (%d rows)"
       % len(rows["reaction"]))
    ok(any(refs(props(r).get("comment")) for r in rows["reaction"]),
       "a reaction on a comment, pointing at it")
    ok(any(refs(props(r).get("parent")) for r in comments),
       "a threaded comment, pointing at its parent")
    if mode == "e2e":
        anyissue(lambda p: p.get("canceledAt"), "a canceled issue")
        anyissue(lambda p: p.get("archivedAt"), "an archived issue")
        anyissue(lambda p: p.get("integrationSourceType"),
                 "an issue an integration created")
        anyissue(lambda p: p.get("previousIdentifiers"),
                 "an issue that moved team")

    print("--> the types the guide is strict about")
    states = rows["workflowstate"]
    ok(all(isinstance(props(r).get("type"), str) for r in states),
       "workflowstate.type is a STRING: Linear's schema types it String! and "
       "only its prose lists the words (§2)")
    ok(all(isinstance(props(r).get("number"), (int, float))
           for r in issues if props(r).get("number") is not None),
       "issue.number is a number, typed as the schema types it (Float)")
    enums = {"team": ("visibility",), "project": ("health", "frequencyResolution"),
             "projectstatus": ("type",), "issue": ("integrationSourceType",)}
    spelled = True
    for name, keys in enums.items():
        for r in rows[name]:
            for k in keys:
                v = props(r).get(k)
                if v is not None and (not isinstance(v, str)
                                      or not v.isalnum() or not v[0].isalpha()):
                    spelled = False
    ok(spelled, "every enum value is spelled [a-z][a-z0-9]* — the loader's rule")

    print("--> the temporal trait is fed by the API's own instant")
    def recorded(kind, rid):
        return want[kind].get(rid) or {}
    bad_at = []
    for name in MIRRORS:
        if name == "cycle":
            continue
        for r in rows[name]:
            node = recorded(name, props(r).get(ID_PROP[name]))
            if node.get("createdAt") and props(r).get("at") \
                    and props(r)["at"][:19] != node["createdAt"][:19]:
                bad_at.append("%s/%s" % (name, r["id"]))
    ok(not bad_at, "`at` is `createdAt` on every mirror but cycle (%d off)"
       % len(bad_at))
    if rows["cycle"]:
        c = props(rows["cycle"][0])
        node = recorded("cycle", c.get(ID_PROP["cycle"]))
        ok(not node or (c.get("at") or "")[:19] == (node.get("startsAt") or "")[:19],
           "cycle: `at` is `startsAt` and the cycle IS the span")
        ok(not node or (c.get("endsAt") or "")[:19] == (node.get("endsAt") or "")[:19],
           "cycle: `endsAt` is `endsAt`")
        ok("createdAt" in c, "cycle: `createdAt` rides beside the trait's slot")

    print("--> a capped nested connection was left alone, not written short")
    if want["capped"]:
        short, prefixes = [], []
        for rid, conn in want["capped"]:
            row = next((r for r in issues
                        if props(r).get("issueId") == rid), None)
            if row is None:
                continue
            if conn in ("labels", "subscribers") and conn in props(row):
                # A capped connection is a property this sync must NOT write:
                # a short list is a lie about the graph.
                short.append("%s.%s" % (rid, conn))
            if conn == "comments":
                # Comments are not a property — they ride as `comment.issue` —
                # so the capped page's PREFIX is written as rows, and the
                # account says so. That is the honest behaviour and the
                # README says it in those words.
                held = [c for c in comments
                        if ref(props(c).get("issue")).endswith("/" + row["id"])]
                prefixes.append(len(held))
        ok(not short, "%d capped connections; no short list was written (%s)"
           % (len(want["capped"]), short[:3]))
        if prefixes:
            ok(all(n > 0 for n in prefixes),
               "a capped comments page still wrote its prefix as rows (%s) "
               "and the account's status names it" % prefixes)
            ok("short:" in str(acct.get("syncStatus") or ""),
               "the run says which connection it could not finish (%s)"
               % acct.get("syncStatus"))
    else:
        print("    --    no connection in this set came back capped")

    print("--> a second, forced sync is idempotent")
    before = {name: len(rows[name]) for name in MIRRORS}
    content = {r["id"]: json.dumps(props(r), sort_keys=True)[:4000]
               for r in rows["issue"]}
    after_acct = force_sync(api, akind, aid)
    ok(str(after_acct.get("syncStatus", "")).startswith("ok"),
       "the second sync finished ok (%s)" % after_acct.get("syncStatus"))
    after = {name: all_records(api, KINDS[name]) for name in MIRRORS}
    same = all(len(after[name]) == before[name] for name in MIRRORS)
    ok(same, "no duplicate rows: %s"
       % {n: (before[n], len(after[n])) for n in MIRRORS if len(after[n]) != before[n]})
    changed = [r["id"] for r in after["issue"]
               if content.get(r["id"]) is not None
               and json.dumps(props(r), sort_keys=True)[:4000] != content[r["id"]]]
    ok(not changed, "issue content is unchanged across the re-sync (%d moved)"
       % len(changed))
    ok((after_acct.get("syncRequestedAck") or "") ==
       (after_acct.get("syncRequestedAt") or ""),
       "the sync acknowledged the request generation it acted on")
    trait_ok(after_acct, "ok", "after the requested sync")
    ok((after_acct.get("syncRequestedAck") or "") ==
       (after_acct.get("syncRequestedAt") or ""),
       "the trait's request pair agrees — a single-function provider answers "
       "with syncRequestedAck alone (0085)")
    ok((after_acct.get("lastCompletedAt") or "") >
       (acct.get("lastCompletedAt") or ""),
       "the second walk ran out of work too — `lastCompletedAt` moved")
    moved = (after_acct.get("syncCursors") or {}).get("issues") or ""
    ok(moved > ((acct.get("syncCursors") or {}).get("issues") or ""),
       "the issues watermark advanced (%s)" % moved[:19])
    ok(not after_acct.get("syncPending"),
       "and nothing is parked after it")

    if mode == "e2e":
        print("--> the engine's part: #569 and #570, against the real mapping")
        import yaml  # noqa: E402 — only the proof needs it
        root = pathlib.Path(os.environ.get("SUBSTRATE_E2E_REPO")
                            or HERE.parents[3])
        docs = []
        for f in (root / "samples/people/bundle.yaml",
                  root / "samples/people/person.yaml",
                  root / "samples/people/organization.yaml",
                  root / "samples/people/team.yaml",
                  root / "samples/people/mappings.linear.yaml"):
            if f.exists():
                docs += [d for d in yaml.safe_load_all(f.read_text()) if d]
        # `linearuserperson` ONLY. `linearissueperson` probes
        # `issue.assigneeEmail`, a denormalised scalar this rewrite removed
        # (§3: a reference is never doubled by a scalar) — it cannot type-check
        # against the new source kind and the mappings agent has the ask in
        # providers/linear/README.md. Every other provider's mapping names a
        # package this throwaway repository does not hold.
        keep = ("/person", "/organization", "/team", "/linearuserperson")
        docs = [d for d in docs
                if d.get("kind") != "substrate.reamde.dev/core/recordmapping"
                or (d.get("metadata") or {}).get("id", "").endswith("linearuserperson")]
        for d in docs:
            if d.get("kind") == "substrate.reamde.dev/core/bundle":
                d["data"]["installs"] = [i for i in d["data"].get("installs") or []
                                         if i.endswith(keep)]
        st, body, _ = api.call("POST", "/api/v1/vocabulary/apply",
                               {"documents": docs})
        ok(st < 300, "the people closure and linearuserperson applied (%s) %s"
           % (st, "" if st < 300 else json.dumps(body)[:300]))
        if st < 300:
            ok(True, "#569: references pinned at linear/user survived a "
                     "mapping declaring it as a source")
            force_sync(api, akind, aid)
            after_users = all_records(api, KINDS["user"])
            with_subject = [u for u in after_users if props(u).get("person")]
            ok(bool(with_subject),
               "#570: %d user rows carry a synthesised `person` slot the "
               "bundle never declared" % len(with_subject))
            issues_now = all_records(api, KINDS["issue"])
            assignees = [t for r in issues_now for t in refs(props(r).get("assignee"))]
            ok(bool(assignees),
               "and `issue.assignee -> user` still admits (%d references)"
               % len(assignees))

        # ------------------------------------- the failure matrix (T-084)
        # The trait's whole point is that a reader learns what happened
        # without reading a log, so the two failures that are NOT the same
        # thing have to read differently: a rate limit is work to resume
        # (`throttled`, with the instant Linear named on `retryNotBefore`)
        # and anything else is a break (`erroring`). The proof that neither
        # is a dead end is the clean run after them, over the same rows.
        mock_url = os.environ.get("MOCK_URL", "")
        if mock_url:
            print("--> the failure matrix, injected")
            rows_before = {name: {r["id"] for r in all_records(api, KINDS[name])}
                           for name in MIRRORS}

            set_faults(api, mock_url,
                       [{"match": "POST /graphql", "contains": "ViewerPage1",
                         "status": [429], "headers": {"Retry-After": "1"}}])
            limited = force_sync(api, akind, aid, drained=False)
            status = str(limited.get("syncStatus") or "")
            ok(status.startswith("erroring") and "rate limited" in status,
               "a 429 stops the walk and SAYS so (%s)" % status[:90])
            trait_ok(limited, "throttled", "after an injected 429")
            ok("rate limited" in str(limited.get("syncError") or ""),
               "syncError carries the cause (%s)"
               % str(limited.get("syncError"))[:70])
            ok(bool(limited.get("syncErrorAt")), "and syncErrorAt says when")
            ok(bool(limited.get("retryNotBefore")),
               "the instant Linear named survived onto the account (%s)"
               % limited.get("retryNotBefore"))
            ok(limited.get("syncRequestedAck") == limited.get("syncRequestedAt"),
               "the throttled run still acknowledged the request — an "
               "unacked one re-fires against the same failure forever")
            ok(bool(limited.get("syncPending")),
               "and the walk is parked where it stopped, not thrown away")

            set_faults(api, mock_url,
                       [{"match": "POST /graphql", "contains": "ViewerPage1",
                         "status": [500]}])
            broken = force_sync(api, akind, aid, drained=False)
            status = str(broken.get("syncStatus") or "")
            ok(status.startswith("erroring"),
               "a 500 is a break, not a wait (%s)" % status[:90])
            trait_ok(broken, "erroring", "after an injected 500")
            ok("500" in str(broken.get("syncError") or ""),
               "syncError names the status (%s)"
               % str(broken.get("syncError"))[:70])

            set_faults(api, mock_url, [])
            healed = force_sync(api, akind, aid)
            ok(str(healed.get("syncStatus") or "").startswith("ok"),
               "and the next clean run finishes ok (%s)"
               % healed.get("syncStatus"))
            trait_ok(healed, "ok", "after the run that healed it")
            ok((healed.get("syncErrorAt") or "") <=
               (healed.get("lastSyncedAt") or ""),
               "syncError is the LAST error, not a live one: syncErrorAt is "
               "older than the run that just finished ok")
            rows_after = {name: {r["id"] for r in all_records(api, KINDS[name])}
                          for name in MIRRORS}
            ok(rows_after == rows_before,
               "no row was lost or duplicated across two failing syncs")
        else:
            print("    --    no MOCK_URL: the failure matrix needs the "
                  "runner's mock")

    print()
    if FAILURES:
        print("!! %d checks failed" % len(FAILURES))
        for f in FAILURES:
            print("   - %s" % f)
        return 1
    print("PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
