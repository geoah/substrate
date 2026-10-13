#!/usr/bin/env python3
"""What the Google Fit mirrors must be true of once the runner has synced them.

The runner serves the recordings, rewires the closure at the mock, writes the
config and the account, completes the OAuth grant and waits for the first
sync to settle. This file runs after that.

Every expectation is read off the recordings and off the mock's request log,
never off the clock: the dataset recordings answer any day through the mock's
path relaxation, so which days were read depends on the run date, and the
scenario asks the log rather than guessing. On a run date far past the
recorded month the expected dataset rows are fewer; the rules stay the same.

WHAT IT PROVES

  1. nothing missed the mock, and every dataset read was answered by a
     path-relaxed recording;
  2. every data source the listing names has a row, raw ones included, and
     only derived sources were read;
  3. each derived source was read on every UTC day of one contiguous window,
     and each recorded point that started on a day the sync read is in that
     day's row, the row's span is the day, and no other dataset row exists;
  4. the session feed was walked to its empty page, every session ending on or
     after the window's first day is mirrored, and the account keeps the
     feed's last token;
  5. a second sync asked for by `syncRequestedAt` re-reads only the last three
     days, reads the feed once from the stored token, and changes no row.
"""

import datetime
import json
import os
import pathlib
import sys
import time
import urllib.parse
import urllib.request

from e2e import API  # the runner's client, on PYTHONPATH

AUTHORITY = "providers.substrate.reamde.dev/googlefit"
SOURCE = AUTHORITY + "/datasource"
DATASET = AUTHORITY + "/dataset"
SESSION = AUTHORITY + "/session"
LAST_TOKEN = "1791608658001"
READ_TYPES = {
    "com.google.step_count.delta", "com.google.calories.expended",
    "com.google.calories.bmr", "com.google.activity.segment",
    "com.google.active_minutes", "com.google.heart_minutes",
    "com.google.heart_rate.bpm", "com.google.weight", "com.google.height",
    "com.google.body.fat.percentage", "com.google.sleep.segment",
    "com.google.oxygen_saturation", "com.google.blood_pressure",
    "com.google.blood_glucose",
}
UTC = datetime.timezone.utc
DAY_NS = 86400 * 10**9

FAILURES = []


def ok(condition, message):
    print(("    ok    " if condition else "    FAIL  ") + message)
    if not condition:
        FAILURES.append(message)


def mock(path, method="GET"):
    req = urllib.request.Request(os.environ["MOCK_URL"] + path, method=method)
    with urllib.request.urlopen(req, timeout=30) as r:
        raw = r.read()
    return json.loads(raw) if raw.strip() else {}


def every(api, kind):
    out, after = [], ""
    while True:
        rows, after = api.page(kind, 500, after)
        out.extend(rows)
        if not after:
            return out


def props(row):
    return (row or {}).get("properties") or {}


def instant(value):
    return datetime.datetime.fromisoformat(str(value).replace("Z", "+00:00"))


def day_of_ns(ns):
    return datetime.datetime.fromtimestamp(int(ns) / 1e9, UTC).date()


def dataset_reads(log):
    """{data source id: [UTC day, ...]} in request order."""
    out = {}
    for r in log:
        path = urllib.parse.unquote(r.get("path") or "")
        if "/datasets/" not in path:
            continue
        head, _, window = path.rpartition("/datasets/")
        sid = head.rsplit("/dataSources/", 1)[1]
        out.setdefault(sid, []).append(day_of_ns(window.split("-")[0]))
    return out


def recorded_points(recordings):
    """Every recorded point, by data source, from the dataset recordings."""
    out = {}
    for f in sorted(recordings.glob("GET_*_datasets*.json")):
        doc = json.loads(f.read_text())
        out.setdefault(doc["dataSourceId"], []).extend(doc.get("point") or [])
    return out


def recorded_sessions(recordings):
    out, gone = [], []
    for f in sorted(recordings.glob("GET_fitness_v1_users_me_sessions__includeDeleted-true*.json")):
        doc = json.loads(f.read_text())
        out.extend(doc.get("session") or [])
        gone.extend(doc.get("deletedSession") or [])
    return out, gone


def wait_for_ack(api, kind, aid, stamp, seconds=300):
    deadline = time.time() + seconds
    while time.time() < deadline:
        st, row, _ = api.call("GET", "/api/v1/%s/%s" % (kind, aid))
        p = props(row) if st == 200 else {}
        if p.get("syncRequestedAck") and instant(p["syncRequestedAck"]) == instant(stamp):
            return p
        time.sleep(1.0)
    return None


def main():
    api = API(os.environ["SUBSTRATE_SERVER"], os.environ["SUBSTRATE_TOKEN"])
    recordings = pathlib.Path(os.environ["RECORDINGS"])
    akind, aid = os.environ["ACCOUNT_KIND"], os.environ["ACCOUNT_ID"]
    listing = json.loads((recordings / "GET_fitness_v1_users_me_dataSources.json").read_text())
    sources = {s["dataStreamId"]: s for s in listing["dataSource"]}
    derived = {sid for sid, s in sources.items()
               if s["type"] == "derived" and s["dataType"]["name"] in READ_TYPES}

    print("  1. the mock")
    log = mock("/__mock/requests").get("requests") or []
    misses = mock("/__mock/misses").get("misses") or []
    ok(not misses, "nothing missed the mock (%d misses)" % len(misses))
    words = {r.get("match") for r in log if "/datasets/" in (r.get("path") or "")}
    ok(words and all(str(w).startswith("~path") for w in words),
       "every dataset read was answered by a path-relaxed recording (%s)" % sorted(words))

    print("  2. data sources")
    rows = every(api, SOURCE)
    ok(len(rows) == len(sources), "%d data source rows for %d listed" % (len(rows), len(sources)))
    ok({props(r).get("dataStreamId") for r in rows} == set(sources),
       "every listed data source has its row")
    reads = dataset_reads(log)
    ok(set(reads) == derived, "only derived sources of the read data types were read (%d of %d)"
       % (len(reads), len(derived)))

    print("  3. datasets")
    windows = {sid: sorted(set(days)) for sid, days in reads.items()}
    first = min(d[0] for d in windows.values())
    last = max(d[-1] for d in windows.values())
    span = [first + datetime.timedelta(days=i) for i in range((last - first).days + 1)]
    ok(all(days == span for days in windows.values()),
       "every derived source was read on every day from %s to %s" % (first, last))
    want = {}
    for sid, points in recorded_points(recordings).items():
        for p in points:
            d = day_of_ns(p["startTimeNanos"])
            if first <= d <= last:
                want[(sid, d.isoformat())] = want.get((sid, d.isoformat()), 0) + 1
    rows = every(api, DATASET)
    have, strays = {}, []
    for r in rows:
        p = props(r)
        at = instant(p["at"])
        have[(p["dataSourceId"], at.date().isoformat())] = len(p.get("point") or [])
        lo = int(at.timestamp()) * 10**9
        inside = all(lo <= int(x["startTimeNanos"]) < lo + DAY_NS for x in p.get("point") or [])
        if not inside or instant(p["endsAt"]) - at != datetime.timedelta(days=1):
            strays.append(r.get("id"))
    ok(not strays, "every dataset row spans one UTC day and holds only points that "
       "started on it (%s)" % (strays[:3] or "all %d" % len(rows)))
    ok(have == want, "one dataset row per recorded source and day read, with every point "
       "(%d rows, %d expected)" % (len(have), len(want)))
    versions = {r["id"]: r.get("version") for r in rows}

    print("  4. sessions")
    sessions, gone = recorded_sessions(recordings)
    floor = datetime.datetime.combine(first, datetime.time(), UTC)
    expect = {s["id"] for s in sessions
              if datetime.datetime.fromtimestamp(int(s["endTimeMillis"]) / 1000, UTC) >= floor}
    rows = every(api, SESSION)
    ok({props(r).get("sessionId") for r in rows} == expect,
       "%d sessions mirrored, %d expected" % (len(rows), len(expect)))
    ok(not ({g["id"] for g in gone} & {props(r).get("sessionId") for r in rows}),
       "no session the feed reports deleted is mirrored")
    tokens = [urllib.parse.parse_qs(r.get("query") or "").get("pageToken", [""])[0]
              for r in log if (r.get("path") or "").endswith("/users/me/sessions")]
    ok(tokens == ["", "1790405742001", LAST_TOKEN],
       "the feed was walked page by page to its empty page (%s)" % tokens)
    acct = props(api.call("GET", "/api/v1/%s/%s" % (akind, aid))[1])
    ok((acct.get("syncTokens") or {}).get("sessions") == LAST_TOKEN,
       "the account keeps the feed's last token")
    ok(acct.get("syncState") == "ok" and acct.get("lastCompletedAt"),
       "the account reads ok and completed (%s)" % acct.get("syncMessage"))
    ok(len(acct.get("syncCursors") or {}) == 14, "one cursor per data type")
    ok(acct.get("email") == "ada@ada.example.com", "the address came from the userinfo call")

    print("  5. a second sync")
    mock("/__mock/requests", "DELETE")
    stamp = datetime.datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
    api.call("PATCH", "/api/v1/%s/%s" % (akind, aid), {"properties": {"syncRequestedAt": stamp}})
    acked = wait_for_ack(api, akind, aid, stamp)
    ok(acked is not None, "the second sync acknowledged the request")
    log = mock("/__mock/requests").get("requests") or []
    reads = dataset_reads(log)
    # The overlap is two days before the first run's start; a second run that
    # starts past midnight UTC adds the new day.
    tail = {last - datetime.timedelta(days=i) for i in (2, 1, 0, -1)}
    ok(reads and all(set(days) <= tail and len(set(days)) >= 3 for days in reads.values()),
       "the second sync re-read only the last days (%s)"
       % sorted({d.isoformat() for days in reads.values() for d in days}))
    tokens = [urllib.parse.parse_qs(r.get("query") or "").get("pageToken", [""])[0]
              for r in log if (r.get("path") or "").endswith("/users/me/sessions")]
    ok(tokens == [LAST_TOKEN], "the feed was read once, from the stored token (%s)" % tokens)
    after = {r["id"]: r.get("version") for r in every(api, DATASET)}
    ok(after == versions, "no dataset row changed and none appeared")

    if FAILURES:
        print("\n%d failure(s)" % len(FAILURES))
        sys.exit(1)
    print("\nall checks passed")


if __name__ == "__main__":
    main()
