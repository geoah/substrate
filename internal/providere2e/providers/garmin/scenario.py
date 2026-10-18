#!/usr/bin/env python3
"""What the Garmin closure must do once the runner has connected the account.

`runner/e2e.py` applied the closure rewired at the mock, connected the
account through the mock's OAuth stub (Garmin's `/oauth2Confirm`) and let
`garmin-on-connect` run `garminsync`. This file checks what that first sync
left, then plays Garmin: it sets the webhook trigger's key and posts every
push delivery under internal/providertest/testdata/garmin to the door, the
way Garmin's Endpoint Configuration would.

WHAT IT PROVES

  1. the first sync recorded the Garmin user id and permissions, and asked
     for every enabled type's backfill one day at a time over the last seven
     days: the mock answers each type's first window 202 and the rest 409,
     and every one counts as accepted;
  2. the door refuses a delivery without the key and accepts every summary
     type's push with it, the 738 KB activity detail included;
  3. each summary lands once at `<account>/<summaryId>` with every delivered
     field stored verbatim under Garmin's own name;
  4. a re-sent summaryId rewrites its row, and a sleep turns final in place;
  5. a ping is followed to its callback on the mock;
  6. a delivery for a user no account knows parks with an error naming the
     user id;
  7. a permission change and a disconnect land as notices and mark the
     account.
"""

import json
import os
import pathlib
import time
import urllib.error
import urllib.parse
import urllib.request

from e2e import API  # the runner's client, on PYTHONPATH

PKG = "providers.substrate.reamde.dev/garmin"
TRIGGER = "substrate.reamde.dev/core/trigger"
KEY = "e2e-garmin-webhook-key-0123456789"
USER = "7c1e5b9a0d2f4c3e8b6a1f0e9d8c7b6a"
KINDS = {
    "dailies": "daily", "epochs": "epoch", "sleeps": "sleep", "bodyComps": "bodycomp",
    "stressDetails": "stressdetail", "userMetrics": "usermetric", "pulseox": "pulseox",
    "allDayRespiration": "respiration", "healthSnapshot": "healthsnapshot", "hrv": "hrv",
    "bloodPressures": "bloodpressure", "skinTemp": "skintemp", "activities": "activity",
    "activityDetails": "activitydetail",
    "manuallyUpdatedActivities": "manuallyupdatedactivity", "moveIQActivities": "moveiq",
}
BACKFILL_PATHS = {
    "dailies", "epochs", "sleeps", "bodyComps", "stressDetails", "userMetrics", "pulseOx",
    "respiration", "healthSnapshot", "hrv", "bloodPressures", "skinTemp", "activities",
    "activityDetails", "moveiq",
}
PUSHES = [
    "push_dailies.json", "push_epochs.json", "push_sleeps.json", "push_bodyComps.json",
    "push_stressDetails.json", "push_userMetrics.json", "push_pulseox.json",
    "push_allDayRespiration.json", "push_healthSnapshot.json", "push_hrv.json",
    "push_bloodPressures.json", "push_skinTemp.json", "push_activities.json",
    "push_activityDetails_21004995534.json", "push_manuallyUpdatedActivities.json",
    "push_moveIQActivities.json",
]

FAILURES = []


def ok(condition, message):
    print(("    ok    " if condition else "    FAIL  ") + message)
    if not condition:
        FAILURES.append(message)


def mock_get(mock, path):
    with urllib.request.urlopen(mock + path, timeout=30) as r:
        return json.loads(r.read())


def post(server, authority, fixture, text=None, key=KEY):
    """One delivery to the door, as Garmin sends it: the fixture's body and
    its content-type and garmin-client-id headers."""
    req = fixture["request"]
    body = (text if text is not None else req["body"]["text"]).encode("utf-8")
    url = "%s/webhooks/%s/garmin-webhook%s" % (server, authority, "/" + key if key else "")
    r = urllib.request.Request(url, data=body, method="POST")
    for name in ("content-type", "garmin-client-id"):
        if req["headers"].get(name):
            r.add_header(name, req["headers"][name])
    try:
        with urllib.request.urlopen(r, timeout=60) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        return e.code


def all_records(api, kind):
    out, after = [], ""
    while True:
        rows, after = api.page(kind, 500, after)
        out.extend(rows)
        if not after:
            return out


def by_summary(api, kind):
    return {(r.get("properties") or {}).get("summaryId"): r
            for r in all_records(api, PKG + "/" + kind)}


def wait(what, fn, seconds=120):
    deadline = time.time() + seconds
    value = None
    while time.time() < deadline:
        value = fn()
        if value:
            return value
        time.sleep(1.0)
    ok(False, "%s within %ds" % (what, seconds))
    return value


def at_least(api, kind, n):
    """A kind's rows by summaryId once there are n of them, else None."""
    rows = by_summary(api, kind)
    return rows if len(rows) >= n else None


def webhook_status(api):
    _, body, _ = api.call("GET", "/api/v1/substrate.reamde.dev/core/trigger/status")
    for t in (body or {}).get("items") or []:
        if t.get("id") == "garmin-webhook":
            return t
    return {}


def main():
    api = API(os.environ["SUBSTRATE_SERVER"], os.environ["SUBSTRATE_TOKEN"])
    server = os.environ["SUBSTRATE_SERVER"].rstrip("/")
    authority = os.environ["SUBSTRATE_AUTHORITY"]
    mock = os.environ["MOCK_URL"].rstrip("/")
    akind, aid = os.environ["ACCOUNT_KIND"], os.environ["ACCOUNT_ID"]
    data = pathlib.Path(os.environ["SUBSTRATE_E2E_REPO"]) / "internal/providertest/testdata/garmin"

    def load(name):
        return json.loads((data / name).read_text())

    def account():
        _, body, _ = api.call("GET", "/api/v1/%s/%s" % (akind, aid))
        return (body or {}).get("properties") or {}

    # -- 1. the first sync ------------------------------------------------
    print("  the first sync")
    props = account()
    ok(props.get("userId") == USER, "userId is the Garmin user id (%s)" % props.get("userId"))
    ok("HEALTH_EXPORT" in (props.get("permissions") or []),
       "permissions came from GET /rest/user/permissions (%s)" % props.get("permissions"))
    ok(props.get("syncState") == "ok" and props.get("lastCompletedAt"),
       "the walk ended ok and complete (%s: %s)" % (props.get("syncState"), props.get("syncMessage")))
    ok(len(props.get("syncCursors") or {}) == len(BACKFILL_PATHS),
       "one backfill cursor per backfilled type (%d)" % len(props.get("syncCursors") or {}))
    log = mock_get(mock, "/__mock/requests")["requests"]
    backfills = {}
    for r in log:
        if r.get("path", "").startswith("/wellness-api/rest/backfill/"):
            backfills.setdefault(r["path"].rsplit("/", 1)[1], []).append(r)
    ok(set(backfills) == BACKFILL_PATHS, "every backfilled type was asked for (%s)" % sorted(backfills))
    now = time.time()
    for path, reqs in sorted(backfills.items()):
        statuses = [r.get("status") for r in reqs]
        ok(len(reqs) == 7 and statuses[0] == 202 and set(statuses[1:]) == {409},
           "%s: seven windows, 202 then 409 (%s)" % (path, statuses))
        for r in reqs:
            q = dict(urllib.parse.parse_qsl(r.get("query") or ""))
            start, end = int(q["summaryStartTimeInSeconds"]), int(q["summaryEndTimeInSeconds"])
            if not (0 < end - start <= 86400 and start >= now - 8 * 86400):
                ok(False, "%s: window %d..%d is a day inside the last week" % (path, start, end))

    # -- 2. the door ------------------------------------------------------
    print("  the webhook door")
    st, body, _ = api.call("PATCH", "/api/v1/%s/garmin-webhook" % TRIGGER, {"properties": {
        "enabled": True,
        "source": {"webhook": {"key": KEY, "headers": ["garmin-client-id"]}}}})
    ok(st < 300, "the webhook trigger takes a key (%s %s)" % (st, json.dumps(body)[:160]))
    first = load(PUSHES[0])
    ok(post(server, authority, first, key="") == 404, "a delivery without the key is refused 404")
    expected = {}
    for name in PUSHES:
        f = load(name)
        size = len(f["request"]["body"]["text"])
        st = post(server, authority, f)
        ok(st == 202, "%s (%d bytes) accepted: %s" % (name, size, st))
        for key, entries in json.loads(f["request"]["body"]["text"]).items():
            for e in entries:
                expected.setdefault(KINDS[key], {})[e["summaryId"]] = e

    # -- 3. the rows ------------------------------------------------------
    print("  the mirrors")
    for kind, entries in sorted(expected.items()):
        want = len(entries)
        rows = wait("%d %s rows" % (want, kind),
                    lambda k=kind, n=want: at_least(api, k, n)) or {}
        ok(len(rows) == len(entries), "%s: %d rows for %d summaries" % (kind, len(rows), len(entries)))
        wrong = []
        for sid, entry in entries.items():
            row = rows.get(sid) or {}
            stored = row.get("properties") or {}
            if row.get("id") != "%s/%s" % (aid, sid):
                wrong.append("%s id %s" % (sid, row.get("id")))
            for field, value in entry.items():
                if stored.get(field) != value:
                    wrong.append("%s.%s" % (sid, field))
        ok(not wrong, "%s: every delivered field stored verbatim%s"
           % (kind, "" if not wrong else " (differs: %s)" % ", ".join(wrong[:5])))

    # -- 4. re-sends ------------------------------------------------------
    print("  re-sends")
    st = post(server, authority, load("push_dailies_2026-10-07_resend.json"))
    full = wait("the re-sent daily", lambda: (lambda r: r if len(
        (r.get("properties") or {}).get("timeOffsetHeartRateSamples") or {}) == 5660 else None)(
        by_summary(api, "daily").get("x4b2c9e1-6ac56150") or {}))
    ok(st == 202 and bool(full), "the re-sent 2026-10-07 daily replaced the partial one in place")
    st = post(server, authority, load("push_sleeps_2026-10-07_final.json"))
    final = wait("the final sleep", lambda: (lambda r: r if (r.get("properties") or {}).get(
        "validation") == "ENHANCED_FINAL" else None)(by_summary(api, "sleep").get("x4b2c9e1-6ac54ecc") or {}))
    ok(st == 202 and bool(final), "the 2026-10-07 sleep turned final in place")

    # -- 5. a ping --------------------------------------------------------
    print("  a ping")
    ping = load("ping_dailies.json")
    text = ping["request"]["body"]["text"].replace("https://apis.garmin.com", mock)
    st = post(server, authority, ping, text=text)
    pulled = wait("the ping's callback", lambda: [
        r for r in mock_get(mock, "/__mock/requests")["requests"]
        if r.get("path") == "/wellness-api/rest/dailies" and r.get("status") == 200])
    ok(st == 202 and bool(pulled), "the ping's callback was fetched from the mock")

    # -- 6. an unknown user -----------------------------------------------
    print("  an unknown user")
    stranger = load("push_bloodPressures.json")
    text = stranger["request"]["body"]["text"].replace(USER, "0000000000000000000000000000ffff")
    st = post(server, authority, stranger, text=text)
    parked = wait("the stranger's delivery to park",
                  lambda: (lambda t: t if (t.get("parked") or 0) >= 1 else None)(webhook_status(api)))
    ok(st == 202 and "0000000000000000000000000000ffff" in json.dumps(parked or {}),
       "it parked naming the user id (%s)" % json.dumps(parked or {})[:200])

    # -- 7. lifecycle notices ---------------------------------------------
    print("  lifecycle notices")
    post(server, authority, load("push_userPermissionsChange.json"))
    post(server, authority, load("push_deregistrations.json"))
    marked = wait("the disconnect mark", lambda: (lambda p: p if p.get("deregisteredAt") else None)(account()))
    marked = marked or {}
    ok(marked.get("syncState") == "erroring" and marked.get("permissions") == ["ACTIVITY_EXPORT", "HEALTH_EXPORT"],
       "the account is marked disconnected with the permissions the user left (%s, %s)"
       % (marked.get("syncState"), marked.get("permissions")))
    notices = sorted((r.get("properties") or {}).get("noticeType") for r in all_records(api, PKG + "/notice"))
    ok(notices == ["deregistration", "permissionchange"], "two notices (%s)" % notices)

    print()
    if FAILURES:
        print("FAIL  %d assertion(s):" % len(FAILURES))
        for f in FAILURES:
            print("  - " + f)
        return 1
    print("PASS  %s" % ", ".join("%s %d" % (k, len(v)) for k, v in sorted(expected.items())))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
