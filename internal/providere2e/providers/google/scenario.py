#!/usr/bin/env python3
"""The google provider's end-to-end scenario.

the e2e run runs this AFTER the platform's runner has:

  1. installed `providers/google/bundle.yaml` + `triggers.yaml` into a fresh
     repository,
  2. written a `config` record whose `apiBase` points at the mock server
     serving `providers/google/fixtures/`,
  3. created an `account` record with all three streams enabled and
     `backfillDepth: all` (the one depth whose request parameters do not move
     with the clock, and so the one a recording can answer),
  4. connected that account against the OAuth stub, and
  5. let the three on-connect syncs drain.

a seed run over the owner\'s own pull (mneme tree only) runs the SAME scenario against
`raw/google/<account>/` instead, which is why nothing here is hard-coded: the
expectations are COMPUTED FROM THE RECORDINGS the run was served, so one
scenario checks a pseudonymised fixture set and the owner's real mailbox
alike. What is fixture-specific (the moved instance, the cancelled slot, the
instance moved to another DAY, the ordinal `BYDAY` master, the event updated
between two sync tokens) is declared in `fixtures/expectations.json` and
skipped, loudly, when a recording set does not carry it.

Environment — the names `tools/e2e.py` sets, each with a fallback so the
scenario also runs by hand:

    SUBSTRATE_SERVER    default http://localhost:8081
    SUBSTRATE_TOKEN     the repository's bearer token         (required)
    ACCOUNT_ID          the account record id                 default e2e
    ACCOUNT_KIND        default providers.substrate.reamde.dev/google/account
    RECORDINGS          the directory the mock served
                        default providers/google/fixtures
    MODE                e2e | seed — only ever reported, never branched on
    MOCK_URL            the mock's base, for its request log
    E2E_SKIP_SECOND     set to skip the second-sync half

Exits non-zero on the FIRST failure, with a message naming what was expected,
what the repository holds, and which recording said so.
"""

from __future__ import annotations

import base64
import datetime as dt
import hashlib
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

# The suite root, `internal/providere2e`: this file is
# `providers/google/scenario.py`, so three hops. The runner also passes it as
# SUBSTRATE_E2E_ROOT, which wins when the suite is laid out somewhere else.
ROOT = os.environ.get("SUBSTRATE_E2E_ROOT") or os.path.dirname(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PACKAGE = "providers.substrate.reamde.dev/google"
KIND = {name: PACKAGE + "/" + name for name in (
    "config", "account", "emailaddress", "contact", "contactgroup",
    "gmaillabel", "gmailthread", "gmailmessage", "gmailattachment",
    "calendar", "calendarsync", "calendarseries", "calendarevent",
    "drivefile")}
KIND["account"] = os.environ.get("ACCOUNT_KIND") or KIND["account"]

# THE STREAMS THIS RUN ASSERTS OVER. Four since T-088 — and trimmed to three
# in `main` when the recording set carries no Drive stream at all (a raw
# pull taken before the Drive pull existed), because a stream the mock
# cannot answer errors rather than proving anything. Every per-stream loop
# below reads this tuple and never a literal.
STREAMS = ("contacts", "gmail", "calendar", "drive")
# The four "sync now" triggers, one per stream, in the same order.
ON_REQUEST = tuple("google-%s-on-request" % s for s in STREAMS)

SERVER = os.environ.get("SUBSTRATE_SERVER", "http://localhost:8081").rstrip("/")
TOKEN = os.environ.get("SUBSTRATE_TOKEN", "")
ACCOUNT = os.environ.get("ACCOUNT_ID") or os.environ.get("E2E_ACCOUNT") or "e2e"
RECORDINGS = os.environ.get("RECORDINGS") or os.environ.get(
    "E2E_RECORDINGS") or os.path.join(ROOT, "fixtures", "google")
MODE = os.environ.get("MODE", "e2e")
MOCK_URL = os.environ.get("MOCK_URL", "")
SKIP_SECOND = bool(os.environ.get("E2E_SKIP_SECOND"))

# The shapes the fixture privacy gate reads. Deliberately simple: this is a
# tripwire, not a parser, and it runs on every end-to-end run.
_ADDRESS_RE = re.compile(r"[A-Za-z0-9._%+'-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
_HOST_RE = re.compile(r"(?:https?|wss?|sips?)://([^/?#\s\"']+)", re.I)
_B64_ONLY = re.compile(r"^[A-Za-z0-9_-]{16,}={0,2}$")

# The triggers the second sync is fired through, in the order a fresh connect
# fires them.
TRIGGERS = ("google-contacts-scheduled", "google-gmail-scheduled",
            "google-calendar-scheduled", "google-drive-scheduled")


# --------------------------------------------------------------------------
# failing
# --------------------------------------------------------------------------

class Failed(Exception):
    pass


def check(condition, message):
    if not condition:
        raise Failed(message)


def note(message):
    print("   .. " + message)


def step(message):
    print("== " + message)


# --------------------------------------------------------------------------
# the repository, over REST
# --------------------------------------------------------------------------

def api(path, method="GET", body=None):
    url = SERVER + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={
        "Authorization": "Bearer " + TOKEN,
        "Accept": "application/json",
        "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read().decode("utf-8")
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", "replace")[:400]
        raise Failed("%s %s -> HTTP %d: %s" % (method, path, e.code, detail))


def rows(kind, where=None, expand=None, scoped=True):
    """Every live record of one kind THIS ACCOUNT owns, paged to exhaustion.

    THE SCOPE IS THE ACCOUNT, not the kind. A seed run puts two Google
    accounts in one repository — the owner has two — and the second account's
    scenario counted the first account's rows too: *"calendar: the repository
    holds 16 rows, the recordings imply 7"*. Every mirror kind carries an
    `account` reference, so the filter is exact; `calendarsync` does not, and
    is scoped through the calendar it names (below). Filtering here rather
    than at each call site means a count, an inventory and the idempotence
    comparison cannot disagree about what "the rows" are.
    """
    out, after = [], ""
    while True:
        params = {"filter": json.dumps(
            {"kinds": [kind], "properties": where} if where else {"kinds": [kind]}),
            "first": "200"}
        if after:
            params["after"] = after
        if expand:
            params["expand"] = expand
        page = api("/api/v1/records?" + urllib.parse.urlencode(params))
        got = page.get("records") or page.get("items") or []
        out.extend(got)
        nxt = page.get("cursor") or ""
        # A CURSOR THAT DOES NOT MOVE IS A FAILURE, not a stop condition. The
        # old guard only checked for an empty page or an absent cursor, so a
        # server handing back the same cursor forever read as "done" on the
        # first page and every count assertion below it was measuring one
        # page of a longer kind.
        if nxt and nxt == after:
            raise Failed("%s: the records cursor repeated (%r) after %d rows "
                         "— pagination is not advancing" % (kind, nxt, len(out)))
        after = nxt
        if not after or not got:
            if not scoped:
                return out
            mine, foreign = [], 0
            for row in out:
                ref = props(row).get("account")
                if ref is None:
                    mine.append(row)
                    continue
                if ref_id(ref) == ACCOUNT:
                    mine.append(row)
                else:
                    foreign += 1
            if foreign:
                _FOREIGN[kind] = foreign
            return mine
        


# How many rows of each kind belong to ANOTHER account in this repository.
# Reported once, because "the repository holds more than this run wrote" is
# the normal state of a seeded repository and reads like a bug otherwise.
_FOREIGN = {}


def one(kind, rid):
    got = api("/api/v1/records?" + urllib.parse.urlencode(
        {"filter": json.dumps({"kinds": [kind], "ids": [rid]}), "first": "1"}))
    found = got.get("records") or got.get("items") or []
    return found[0] if found else None


def window(lo, hi):
    """THE WINDOW READ, paged: both bounds on `at`, over the TRAIT.

    Bounding `at` on both ends is what makes the server COMPUTE a recurring
    series' occurrences inside the window (decision 0081), and it is the one
    read `tools/agenda.py` makes — so an assertion over it is an assertion
    about what the owner's agenda shows, not about a property in isolation.
    Every kind binding `temporal` answers, which is why the callers below
    filter by row id rather than trusting the page.
    """
    out, after = [], ""
    while True:
        params = {"filter": json.dumps(
            {"implements": "substrate.reamde.dev/core/temporal",
             "properties": {"at": {"gte": lo, "lt": hi}}}),
            "orderBy": "at", "first": "200"}
        if after:
            params["after"] = after
        page = api("/api/v1/records?" + urllib.parse.urlencode(params))
        got = page.get("records") or []
        out.extend(got)
        nxt = page.get("cursor") or ""
        if nxt and nxt == after:
            raise Failed("the window read's cursor repeated (%r)" % nxt)
        after = nxt
        if not after or not got:
            return out


def props(row):
    return (row or {}).get("properties") or {}


def ref_id(value):
    """The id inside a stored reference value, whichever end it is read from."""
    if isinstance(value, dict):
        return str(value.get("ref") or "").rsplit("/", 1)[-1]
    if isinstance(value, str):
        return value.rsplit("/", 1)[-1]
    return ""


def ref_ids(value):
    if isinstance(value, list):
        return [ref_id(v) for v in value]
    rid = ref_id(value)
    return [rid] if rid else []


# --------------------------------------------------------------------------
# the ids the bundle composes — `host.ids.external`, reproduced
# --------------------------------------------------------------------------

def _slugify(s):
    out, prev_dash = [], False
    for ch in str(s):
        if "A" <= ch <= "Z":
            ch = chr(ord(ch) + 32)
        if ("a" <= ch <= "z") or ("0" <= ch <= "9"):
            out.append(ch)
            prev_dash = False
        elif not prev_dash:
            out.append("-")
            prev_dash = True
    return "".join(out).strip("-")


def _id_hash(*parts):
    h = hashlib.sha256()
    for p in parts:
        b = ("" if p is None else str(p)).encode("utf-8")
        h.update(str(len(b)).encode() + b":" + b + b"|")
    return h.hexdigest()[:32]


def external(provider, account, external_id):
    slug = _slugify(provider)[:48]
    digest = _id_hash(provider, account, external_id)
    return (slug + "-" + digest) if slug else digest


def address_id(addr):
    return external("google-address", ACCOUNT, addr)


def calendar_id(cid):
    return external("gcal-calendar", ACCOUNT, cid)


def series_id(cref, mid):
    return external("gcal-series", cref, mid)


def event_id(cref, eid):
    return external("gcal-event", cref, eid)


def exception_id(sref, slot_iso):
    stamp = dt.datetime.fromisoformat(slot_iso.replace("Z", "+00:00"))
    return "%s_%s" % (sref, stamp.astimezone(dt.timezone.utc)
                      .strftime("%Y%m%dT%H%M%SZ"))


def message_id(mid):
    return external("gmail-message", ACCOUNT, mid)


def thread_id(tid):
    return external("gmail-thread", ACCOUNT, tid)


def contact_id(resource):
    return external("google-contacts", ACCOUNT, resource)


def group_id(resource):
    return external("google-contactgroup", ACCOUNT, resource)


def label_id(lid):
    return external("gmail-label", ACCOUNT, lid)


def file_id(fid):
    return external("gdrive-file", ACCOUNT, fid)


def _drive_constants():
    """The Drive stream's OWN request parameters — the fields mask, the
    query, the ordering, the export table — read from `drive_requests.py`
    beside this file rather than re-spelled: the mask is part of the request,
    so it is part of the recording's name, and two copies of it drift the
    first time one is tuned. The bundle's `syncdrive` pins the same strings
    byte for byte."""
    import importlib.util as ilu
    spec = ilu.spec_from_file_location(
        "_gpull_scenario", os.path.join(os.path.dirname(os.path.abspath(__file__)), "drive_requests.py"))
    mod = ilu.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


# --------------------------------------------------------------------------
# what the recordings say should be there
# --------------------------------------------------------------------------

def recordings():
    if not os.path.isdir(RECORDINGS):
        raise Failed("no recordings directory at %s" % RECORDINGS)
    out = {}
    for name in sorted(os.listdir(RECORDINGS)):
        if not name.endswith(".json") or name in ("expectations.json", "index.json"):
            continue
        with open(os.path.join(RECORDINGS, name), "r", encoding="utf-8") as fh:
            try:
                out[name] = json.load(fh)
            except ValueError:
                raise Failed("recording %s is not JSON" % name)
    if not out:
        raise Failed("no recordings under %s" % RECORDINGS)
    return out


class Expected:
    """The rows the served recordings imply, derived exactly the way the sync
    derives them.

    Not a count of items: a SET OF COMPOSED ROW IDS, because that is what the
    sync writes. A recurring master is one `calendarseries` id; a modified
    exception is a `calendarevent` id at `<series id>_<slot>`, the slot
    resolved in the SERIES' zone the way the sync resolves it, so two exceptions whose spellings
    differ but whose instants agree collapse into one row here exactly as they
    do there; a cancelled entry REMOVES an id, because a cancellation is a
    retraction.
    """

    def __init__(self, recs):
        self.calendars = {}       # calendarId -> entry
        self.series = {}          # series row id -> item
        self.events = {}          # event row id -> item
        self.cancelled = set()    # row ids that must NOT be there
        self._deltas = {}         # the syncToken recordings the SECOND sync reads
        self._threads = {}        # threadId -> the `threads.get` recording
        self._pages = {}          # events recordings, before the chain walk
        self.orphans = 0          # page recordings no live chain reaches
        self.tombstoned = set()   # exception rows Google cancelled: rows, with
                                  # `status: cancelled`, not absences
        self.cancelled_series = set()   # series ids Google tombstoned
        self.order_dependent = set()    # exceptions of a cancelled master
        self.listed = set()             # message ids a LISTING recording names
        self._msg_addresses = {}        # message id -> the addresses it names
        self.messages = {}        # messageId -> payload
        self.threads = set()
        self.contacts = {}        # resourceName -> person (BOTH endpoints)
        self.connections = set()  # resourceNames people/me/connections named
        self.directory = set()    # resourceNames listDirectoryPeople named
        self.groups = {}          # contactGroups resourceName -> group
        self.labels = {}          # Gmail label id -> label
        self.addresses = set()
        self.owner_addresses = set()
        # THE DRIVE STREAM (T-088).
        self.drive_files = {}         # fileId -> the File item the listing carried
        self.drive_exports = {}       # fileId -> (status, body text)
        self.drive_changed = {}       # fileId -> the File item the delta moves forward
        self.drive_removed = set()    # fileIds the delta retracts
        self.drive_start_token = ""   # what changes.getStartPageToken answers
        self.drive_next_token = ""    # what the delta hands back
        self._read(recs)
        self._read_drive(recs)

    def _read(self, recs):
        for name, payload in recs.items():
            if name.startswith("GET_calendar_v3_users_me_calendarList"):
                self._calendar_list(payload)
        self._pages = {}
        for name, payload in recs.items():
            # ONLY the walk the FIRST sync makes: no `timeMin` (that is the
            # plan's 14-day window pull, which the sync never asks for under
            # `backfillDepth: all`) and no `syncToken` (that is the delta a
            # SECOND sync reads). Counting either would expect rows the run
            # never fetched, or order a cancellation against a page the sync
            # read in the other order.
            # `timeMax` AS WELL AS `timeMin`. The raw pull also captures a
            # WINDOWED walk (`events_window`, timeMin+timeMax) that the sync
            # never asks for — and `recording_name` hashes a query slug past
            # 80 characters, so `timeMin` vanishes from the NAME while
            # `timeMax` survives. Counting those recordings expected two
            # masters the sync had no call that could have read.
            if "_events__" in name and "_instances" not in name \
                    and "timeMin" not in name and "timeMax" not in name:
                if "syncToken" in name:
                    self._deltas[name] = payload
                    continue
                self._pages[name] = payload
                continue
            if name == "GET_gmail_v1_users_me_messages.json" or \
                    name.startswith("GET_gmail_v1_users_me_messages__"):
                for entry in payload.get("messages") or []:
                    if entry.get("id"):
                        self.listed.add(str(entry["id"]))
                continue
            if name.startswith("GET_gmail_v1_users_me_threads_"):
                tid = str(payload.get("id") or "")
                if tid:
                    self._threads[tid] = payload
            elif name.startswith("GET_gmail_v1_users_me_messages_"):
                self._message(payload)
            elif name.startswith("GET_v1_people_me_connections"):
                self._connections(payload)
            elif name.startswith("GET_v1_contactGroups"):
                for group in payload.get("contactGroups") or []:
                    if group.get("resourceName"):
                        self.groups[str(group["resourceName"])] = group
            elif name == "GET_gmail_v1_users_me_labels.json":
                for label in payload.get("labels") or []:
                    if label.get("id"):
                        self.labels[str(label["id"])] = label
            elif name.startswith("GET_v1_people_listDirectoryPeople"):
                # THE WORKSPACE DIRECTORY (T-031), into the SAME expected set.
                # A directory person is a `contact`, so it is counted as one;
                # a person the connections page ALSO carries is counted ONCE,
                # because `self.contacts` is keyed by resourceName and one
                # resourceName composes one row id. That identity IS the
                # assertion — see `assert_directory`.
                self._directory(payload)
        # A CANCELLED MASTER TAKES ITS EXCEPTIONS WITH IT. The sync deletes
        # every event pointing at a retracted series (the cascade would
        # collect them at the next GC, and the explicit delete keeps the
        # timeline right in between), so an exception whose master was
        # tombstoned on any page is not a row — whatever order the pages
        # arrived in.
        # A CANCELLED MASTER TAKES ITS EXCEPTIONS WITH IT — the ones that
        # were already COMMITTED when its tombstone was read. The sync's
        # `_exceptions_of` reads committed state, so an exception that landed
        # on a LATER page than its master's tombstone survives, and which
        # side of that line an exception falls on is a function of the page
        # order the provider chose. These ids are therefore ORDER-DEPENDENT:
        # present or absent, both correct, and the scenario asserts the
        # boundary rather than guessing which.
        self._walk_pages()
        # ONE LIFECYCLE, NOT TWO. Version 18's scenario put every exception
        # of a tombstoned master into `order_dependent` — "present or absent,
        # both correct" — which is a test accepting a defect. The rule now:
        # EVERY RESOURCE THE WALK READ IS A ROW, cancelled or not, and the
        # only thing a tombstone removes is the resource it names. So an
        # exception under a tombstoned master stays a row (carrying
        # `status: cancelled` when Google cancelled it too), its master
        # survives as an identity-only shell, and `order_dependent` is empty
        # — there is nothing left for it to hold.
        self.order_dependent = set()
        # A hydrate recording the LISTING does not name is one an earlier pull
        # captured and a later one dropped: the sync never asks for it, so it
        # is not an expected row. When a recording set carries no listing at
        # all, every hydrate counts.
        if self.listed:
            for mid in [m for m in self.messages if m not in self.listed]:
                self.messages.pop(mid, None)
            self.threads = {str((self.messages[m].get("threadId") or m))
                            for m in self.messages}
        # The header addresses of the messages that SURVIVED the prune, and
        # only those. A thin thread member carries no headers and so names no
        # address, which is why this is keyed on the hydrates.
        for mid in self.messages:
            self.addresses |= self._msg_addresses.get(mid, set())
        # A THREAD'S MEMBERS ARE ROWS TOO, even the ones this window never
        # hydrated: `threads.get` returns the whole thread, and the sync mints
        # a THIN message row for every member so the Thread's `messages`
        # reference has a target. They are what a fuller read later patches,
        # and leaving them out of the expectation made a correct mirror look
        # like two rows too many.
        for tid, payload in sorted(self._threads.items()):
            if tid not in self.threads:
                continue
            for member in payload.get("messages") or []:
                mid = str(member.get("id") or "")
                if mid and mid not in self.messages:
                    self.messages[mid] = {"id": mid, "threadId": tid,
                                          "__thin": True}

    def _read_drive(self, recs):
        """The Drive rows the served recordings imply, derived the way the
        SYNC derives them: the `files.list` page chain followed by
        `nextPageToken` under the sync's own parameters (a page nothing
        names is not part of the walk), one export recording per document,
        the start token, and the delta a SECOND sync reads from it.
        """
        if not any(n.startswith("GET_drive_v3_") for n in recs):
            return
        try:
            from mockserver import recording_name
        except ImportError:
            sys.path.insert(0, os.path.join(ROOT, "runner"))
            from mockserver import recording_name
        gpull = _drive_constants()
        params = dict(gpull.SYNC_DRIVE_PARAMS)
        name, hops, seen = recording_name("GET", "/drive/v3/files", params), 0, set()
        while name in recs and hops < 50 and name not in seen:
            seen.add(name)
            page = recs[name]
            for item in page.get("files") or []:
                if item.get("id"):
                    self.drive_files[str(item["id"])] = item
            token = str(page.get("nextPageToken") or "")
            hops += 1
            if not token:
                break
            name = recording_name("GET", "/drive/v3/files",
                                  dict(params, pageToken=token))
        for n, payload in recs.items():
            if n.startswith("GET_drive_v3_files_") and "_export__" in n:
                fid = n[len("GET_drive_v3_files_"):].split("_export__")[0]
                meta = payload.get("__response") or {} if isinstance(payload, dict) else {}
                status = int(meta.get("status") or 200)
                body = payload.get("body") if isinstance(payload, dict) else payload
                self.drive_exports[fid] = (status, body if isinstance(body, str) else "")
        start = recs.get("GET_drive_v3_changes_startPageToken.json") or {}
        self.drive_start_token = str(start.get("startPageToken") or "")
        if self.drive_start_token:
            base = {"pageToken": self.drive_start_token, "pageSize": 100,
                    "fields": gpull.DRIVE_CHANGES_FIELDS}
            delta = recs.get(recording_name("GET", "/drive/v3/changes", base)) or {}
            self.drive_next_token = str(delta.get("newStartPageToken") or "")
            for change in delta.get("changes") or []:
                fid = str(change.get("fileId") or "")
                if change.get("removed") is True:
                    self.drive_removed.add(fid)
                elif isinstance(change.get("file"), dict) and fid:
                    self.drive_changed[fid] = change["file"]
        for item in self.drive_files.values():
            for user in (item.get("owners") or []) + [item.get("lastModifyingUser") or {}]:
                addr = str((user or {}).get("emailAddress") or "").strip()
                if addr and "@" in addr:
                    local, _, domain = addr.rpartition("@")
                    self.addresses.add(local + "@" + domain.lower())

    def delta_rows(self):
        """Every row id a `syncToken` recording MEANS to move.

        The second drain reads each calendar's delta, and the cut writes one
        per calendar — so more than one event legitimately changes, and a
        whole-property idempotence check has to know exactly which. Read off
        the recordings rather than off `expectations.json`, which names one.
        """
        out = set()
        for name, payload in self._deltas.items():
            cid = self._match_calendar(self._calendar_of(name))
            if cid is None:
                continue
            cref = calendar_id(cid)
            for item in payload.get("items") or []:
                eid = str(item.get("id") or "")
                if not eid:
                    continue
                out.add(event_id(cref, eid))
                out.add(series_id(cref, eid))
        # The Drive delta's ONE moved file (T-088): its `modifiedTime`,
        # `version` and — because the text was fetched again — `exportedAt`
        # are supposed to move, and nothing else's is.
        for fid in self.drive_changed:
            out.add(file_id(fid))
        return out

    def recurrence_lines(self):
        """series row id -> the EXDATE instants the MASTER'S OWN lines spell.

        The master's decode is the only thing `exdates` may hold; anything
        beyond it is another resource's lifecycle leaking into the master.
        """
        out = {}
        for rid, item in self.series.items():
            spelled = []
            for line in item.get("recurrence") or []:
                if str(line).upper().startswith("EXDATE"):
                    spelled += [x for x in str(line).split(":", 1)[-1].split(",") if x]
            out[rid] = spelled
        return out

    # The sync's own events query, parameter for parameter (`EVENT_PARAMS` in
    # the calendarsync body). A recording is addressed by its (method, path,
    # query) slug, so these three decide which file answers.
    EVENT_PARAMS = {"singleEvents": "false", "showDeleted": "true",
                    "maxResults": 250}

    def _walk_pages(self):
        """Follow each calendar's page chain the way the SYNC follows it.

        A recordings DIRECTORY is not a walk. `raw/google/<account>` is
        appended to by every pull, so it holds page recordings from earlier
        pulls whose `pageToken` nothing points at any more — orphans the sync
        can never ask for, and which made the scenario require 241 event rows
        no call in the run could have produced. The chain starts at the base
        query and follows `nextPageToken`, exactly as the drain does; a page
        nothing names is not part of this walk and is not expected.
        """
        try:
            from mockserver import recording_name
        except ImportError:
            sys.path.insert(0, os.path.join(ROOT, "runner"))
            from mockserver import recording_name
        for cid in self.calendars:
            path = "/calendar/v3/calendars/%s/events" % urllib.parse.quote(
                str(cid), safe="")
            name = recording_name("GET", path, dict(self.EVENT_PARAMS))
            seen, hops = set(), 0
            while name and name in self._pages and hops < 200:
                payload = self._pages[name]
                seen.add(name)
                self._events(name, payload)
                token = str(payload.get("nextPageToken") or "")
                hops += 1
                if not token:
                    break
                name = recording_name("GET", path,
                                      dict(self.EVENT_PARAMS, pageToken=token))
                if name in seen:
                    break
            self.orphans += len([n for n in self._pages
                                 if _slug_path(cid) in n and n not in seen])

    def _calendar_list(self, payload):
        for entry in payload.get("items") or []:
            cid = str(entry.get("id") or "")
            if not cid or entry.get("deleted"):
                continue
            # A FREE/BUSY CALENDAR IS STILL A CalendarListEntry, and the
            # mirror is of the LIST. Skipping the row here (and in the sync)
            # meant a calendar downgraded to free/busy kept its old detailed
            # row and its old events forever, with nothing in the set even
            # showing the case. The entry is mirrored; its events are not
            # readable, which is a fact about the access role, not a reason
            # to lose the row.
            self.calendars[cid] = entry

    @staticmethod
    def _calendar_of(name):
        head = name.split("_events__")[0]
        return head[len("GET_calendar_v3_calendars_"):]

    def _match_calendar(self, slug):
        for cid in self.calendars:
            if _slug_path(cid) == slug:
                return cid
        return None

    def _slot_zone(self, cid, item, master):
        """The zone ONE exception's slot resolves in, in the sync's order.

        `originalStartTime.timeZone` FIRST: it belongs to the value, so no
        lookup and no page order can move it. Then the master on this page,
        then the calendar, then UTC. Version 18 resolved against the master
        when the master happened to be on the page and against the calendar
        when it was not, which re-keyed every exception under a master that
        landed a page later — the scenario reproduced that and called the
        result order-dependent.
        """
        import zoneinfo
        own = ((item.get("originalStartTime") or {}).get("timeZone")
               or (item.get("start") or {}).get("timeZone"))
        for name in (own,
                     (master.get("start") or {}).get("timeZone"),
                     (master.get("end") or {}).get("timeZone"),
                     (self.calendars.get(cid) or {}).get("timeZone")):
            if not name:
                continue
            try:
                return zoneinfo.ZoneInfo(str(name))
            except Exception:
                continue
        return dt.timezone.utc

    def _zone(self, cid, item):
        """The one zone a series resolves in: the master's own, else the
        calendar's, else UTC — the same order the sync uses."""
        import zoneinfo
        for name in ((item.get("start") or {}).get("timeZone"),
                     (item.get("end") or {}).get("timeZone"),
                     (self.calendars.get(cid) or {}).get("timeZone")):
            if not name:
                continue
            try:
                return zoneinfo.ZoneInfo(str(name))
            except Exception:
                continue
        return dt.timezone.utc

    @staticmethod
    def _instant(value, zone):
        value = value or {}
        if value.get("dateTime"):
            try:
                ts = dt.datetime.fromisoformat(
                    str(value["dateTime"]).replace("Z", "+00:00"))
            except ValueError:
                return ""
            if ts.tzinfo is None:
                ts = ts.replace(tzinfo=zone)
            return ts.astimezone(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        if value.get("date"):
            try:
                day = dt.date.fromisoformat(str(value["date"]))
            except ValueError:
                return ""
            return dt.datetime(day.year, day.month, day.day, tzinfo=zone)\
                .astimezone(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        return ""

    def _events(self, name, payload):
        cid = self._match_calendar(self._calendar_of(name))
        if cid is None:
            return
        cref = calendar_id(cid)
        items = payload.get("items") or []
        # The masters this page carries, so an exception on the same page
        # resolves in its master's own zone rather than the calendar's.
        masters = {str(i.get("id")): i for i in items if i.get("recurrence")}
        for item in items:
            eid = str(item.get("id") or "")
            if not eid:
                continue
            dead = str(item.get("status") or "") == "cancelled"
            master_id = str(item.get("recurringEventId") or "")
            original = item.get("originalStartTime") or {}
            if master_id and original:
                sref = series_id(cref, master_id)
                zone = self._slot_zone(cid, item, masters.get(master_id) or {})
                slot = self._instant(original, zone)
                if not slot:
                    continue
                rid = "%s_%s" % (sref, slot)
                # A CANCELLED EXCEPTION IS A ROW NOW, not a slot the master
                # spends. Version 18 turned it into `calendarseries.cancelledSlots` —
                # a derived list of instants that lost the exception's id, its
                # status and its `originalStartTime`, unioned forever, and
                # unable to represent a REINSTATED occurrence at all. The
                # resource is preserved with `status: cancelled`, and the
                # overlay is a join the reader computes.
                self.events[rid] = item
                if dead:
                    self.tombstoned.add(rid)
                continue
            if dead:
                # A BARE TOMBSTONE — no `recurringEventId`, no
                # `originalStartTime` — is Google retracting the resource
                # itself, and it does not say whether that resource was a
                # single event or a recurring master. The single event's row
                # must be gone. The MASTER's row may survive as an
                # identity-only shell, because the exceptions under it are
                # resources in their own right and their references need a
                # target; what must not survive is the master's CONTENT,
                # which `assert_counts` proves by asserting the shell shape.
                self.cancelled.add(event_id(cref, eid))
                self.cancelled_series.add(series_id(cref, eid))
                self.events.pop(event_id(cref, eid), None)
                self.series.pop(series_id(cref, eid), None)
                continue
            if item.get("recurrence"):
                self.series[series_id(cref, eid)] = item
            else:
                self.events[event_id(cref, eid)] = item
            self._parties(item)

    def _parties(self, item):
        # NOT FROM A TOMBSTONE (T-048). The sync harvests its hub rows from
        # the masters, the exceptions and the singles — never from `gone`,
        # the bare tombstones — because an address seen only on a resource
        # Google has just retracted is an identity with no live record behind
        # it. The expectation matches the mirror.
        if str(item.get("status") or "") == "cancelled" \
                and not item.get("recurringEventId"):
            return
        for entry in [item.get("creator"), item.get("organizer")] + list(
                item.get("attendees") or []):
            addr = str((entry or {}).get("email") or "").strip().lower()
            if addr:
                self.addresses.add(addr)
                if (entry or {}).get("self"):
                    self.owner_addresses.add(addr)

    def _message(self, payload):
        mid = str(payload.get("id") or "")
        if not mid:
            return
        self.messages[mid] = payload
        self.threads.add(str(payload.get("threadId") or mid))
        # HELD PER MESSAGE, not unioned here (T-048). A hydrate recording the
        # listing does not name is one an earlier pull captured and a later
        # one dropped — `_read` prunes those from `self.messages` below, and
        # the sync never asks for them. Their headers' addresses were being
        # counted anyway, so the owner's seed expected up to 39 hub rows the
        # sync had no call that could have written: a 15-row shortfall that
        # was the expectation's, not the mirror's.
        held = self._msg_addresses.setdefault(mid, set())
        for h in (payload.get("payload") or {}).get("headers") or []:
            if str(h.get("name") or "").lower() in ("from", "to", "cc", "bcc",
                                                    "reply-to"):
                held |= set(_addresses_in(h.get("value") or ""))

    def _connections(self, payload):
        self._people(payload.get("connections") or [], "connections")

    def _directory(self, payload):
        self._people(payload.get("people") or [], "directory")

    def _people(self, people, source):
        """One `Person` list from either endpoint, into the same expectation.

        The two endpoints differ by ONE key in the envelope — `connections[]`
        against `people[]` — and by nothing else: the same resource, the same
        fields, the same `people/<id>` namespace. So they merge here exactly
        as the mirror merges them, and `self.directory` remembers which
        resource names the directory named so the dual case can be asserted.
        """
        for person in people:
            resource = str(person.get("resourceName") or "")
            if not resource:
                continue
            if ((person.get("metadata") or {}).get("deleted")):
                continue
            if source == "directory":
                self.directory.add(resource)
            else:
                self.connections.add(resource)
            held = self.contacts.get(resource) or {}
            # The fuller payload wins the expectation, the way a second write
            # patches the row: neither endpoint is authoritative over the
            # other and both carry the whole resource.
            self.contacts[resource] = person if len(person) >= len(held) else held
            for entry in person.get("emailAddresses") or []:
                addr = str(entry.get("value") or "").strip().lower()
                if addr:
                    self.addresses.add(addr)


def _slug_path(cid):
    """A calendar id as `tools/mockserver.path_slug` spells it inside a
    recording's name: the path is percent-DECODED first, and `.`, `-` and `_`
    survive, so `ada@zephyr-lane.example` is `ada_zephyr-lane.example` and not
    `ada_40zephyr-lane_example`."""
    import re
    return re.sub(r"_+", "_", re.sub(r"[^A-Za-z0-9._-]", "_", cid)).strip("_")


def _addresses_in(header):
    from email.utils import getaddresses
    out = []
    for _, addr in getaddresses([header]):
        addr = (addr or "").strip().lower()
        if addr and "@" in addr:
            out.append(addr)
    return out


def expectations_file():
    path = os.path.join(RECORDINGS, "expectations.json")
    if not os.path.exists(path):
        return {}
    with open(path, "r", encoding="utf-8") as fh:
        return json.load(fh)


# --------------------------------------------------------------------------
# the assertions
# --------------------------------------------------------------------------

def account_props_now():
    """The account row's properties as they stand NOW — read again rather than
    carried, because the second sync moves the stream cursors."""
    return props(one(KIND["account"], ACCOUNT) or {})


def assert_account():
    step("the account connected and every stream stamped")
    acct = one(KIND["account"], ACCOUNT)
    check(acct is not None, "no account record %r — did the runner create it?" % ACCOUNT)
    p = props(acct)
    check(p.get("tokenStatus") == "connected",
          "account tokenStatus is %r, want connected" % p.get("tokenStatus"))
    check(p.get("email"),
          "the account carries no email: the OAuth stub's profile endpoint did "
          "not answer, so no owner attribution can be computed")
    for stream in STREAMS:
        status = str(p.get(stream + "SyncStatus") or "")
        check(status.startswith("ok"),
              "%s stream is %r, want ok — the sync did not complete"
              % (stream, status or "unrun"))
        check(p.get(stream + "LastSyncedAt"),
              "%s stream stamped no cadence anchor" % stream)
    assert_sync_trait(p)
    note("email %s, %d streams ok" % (p.get("email"), len(STREAMS)))
    return p


def _streams(acct):
    """The account's `syncStreams`, however the API hands the json back."""
    raw = acct.get("syncStreams") or {}
    if isinstance(raw, str):
        try:
            raw = json.loads(raw)
        except ValueError:
            raw = {}
    return raw if isinstance(raw, dict) else {}


def assert_sync_trait(acct):
    """T-084: the account carries the core `sync` trait after the first sync.

    The trait is what the Connections page, `GET /api/v1/sync/status` and
    `substratectl sync status` read off ANY provider that binds it, so these
    assertions are deliberately spelled in the SUBSTRATE's vocabulary and not
    in this bundle's: a reader that knows nothing about Google must be able to
    say what this account is doing. The legacy `<stream>SyncStatus` sentences
    are asserted beside them by the caller — both shapes ship this version.
    """
    state = str(acct.get("syncState") or "")
    check(state == "ok",
          "the account's syncState is %r after three completed streams, want "
          "`ok` — one of the five words the trait admits (never, running, ok, "
          "erroring, throttled)" % (state or "unset"))
    check(acct.get("lastSyncedAt"),
          "the account carries no lastSyncedAt: the trait's own rollup is "
          "what the Connections page reads for `last synced`")
    check(str(acct.get("syncMessage") or ""),
          "the account carries no syncMessage — the trait's one human line is "
          "unset while syncStatus says %r" % acct.get("syncStatus"))
    streams = _streams(acct)
    for stream in STREAMS:
        entry = streams.get(stream) or {}
        check(entry, "syncStreams carries no %s entry: a three-stream "
                     "provider reports per stream, and %r is what the page "
                     "would render" % (stream, streams))
        check(str(entry.get("state") or "") == "ok",
              "syncStreams.%s.state is %r after a completed run"
              % (stream, entry.get("state")))
        check(entry.get("lastAt"),
              "syncStreams.%s carries no lastAt, so nothing finished by the "
              "trait's account (the stream's own anchor says otherwise)"
              % stream)
    progress = acct.get("syncProgress") or {}
    if isinstance(progress, str):
        try:
            progress = json.loads(progress)
        except ValueError:
            progress = {}
    check(isinstance(progress, dict) and "phase" in progress
          and "done" in progress and "total" in progress
          and "pending" in progress,
          "syncProgress is %r, want the trait's {phase, done, total, pending}"
          % (progress,))
    check(int(progress.get("pending") or 0) == 0,
          "syncProgress says %s still pending while every stream stamped its "
          "anchor" % progress.get("pending"))
    note("trait: syncState %s, streams %s, progress %s"
         % (state,
            ", ".join("%s=%s" % (s, (streams.get(s) or {}).get("state"))
                      for s in STREAMS),
            "%s %s/%s" % (progress.get("phase"), progress.get("done"),
                          progress.get("total"))))


def assert_counts(exp):
    step("the row counts the recordings imply")
    got = {name: rows(KIND[name]) for name in
           ("calendar", "calendarseries", "calendarevent", "gmailthread",
            "gmailmessage", "contact", "contactgroup", "gmaillabel",
            "emailaddress", "gmailattachment", "drivefile")}
    want = {
        "calendar": len(exp.calendars),
        "gmailmessage": len(exp.messages),
        "gmailthread": len(exp.threads),
        "contact": len(exp.contacts),
    }
    if "drive" in STREAMS:
        want["drivefile"] = len(exp.drive_files)
    for name, count in want.items():
        check(len(got[name]) == count,
              "%s: the repository holds %d rows, the recordings imply %d"
              % (name, len(got[name]), count))
        note("%-15s %d" % (name, count))

    # `calendarseries` is a BOUNDARY too. Every master the recordings carry
    # must be a row; a row BEYOND them is an IDENTITY-ONLY SHELL — the series an
    # exception named on a page its master was not on, minted so the
    # exception's reference has a target and completed by the master's own
    # write when it lands (a put merges). A shell carries the identity and
    # NOTHING ELSE: no rule, no recurrence lines, no summary. Asserting that
    # shape is stronger than a count, and it is the shape that says the sync
    # invented no content it did not read.
    held = {row["id"] for row in got["calendarseries"]}
    missing = set(exp.series) - held
    check(not missing,
          "series: %d masters the recordings carry are absent, e.g. %s"
          % (len(missing), sorted(missing)[:3]))
    shells = 0
    for row in got["calendarseries"]:
        if row["id"] in exp.series:
            continue
        p = props(row)
        invented = [k for k in ("recurrence", "recurrenceLines", "summary",
                                "description", "start", "end", "status",
                                "cancelledSlots")
                    if p.get(k) not in (None, "", [], {})]
        check(not invented,
              "calendarseries %s is a shell for an exception whose master this walk "
              "never read, and it carries %s — a shell holds the identity "
              "and nothing the sync did not read"
              % (row["id"], ", ".join(invented)))
        check(p.get("calendar") and p.get("account"),
              "calendarseries shell %s carries neither its calendar nor its account"
              % row["id"])
        shells += 1
    note("%-15s %d (%d masters, %d identity-only shells)"
         % ("calendarseries", len(held), len(exp.series), shells))

    # `calendarevent` is an EQUALITY now. It used to be a boundary with an
    # `order_dependent` escape hatch, because an exception of a cancelled
    # master survived or vanished depending on page order and the scenario
    # accepted both. One lifecycle, one answer: a cancelled EXCEPTION is a
    # live row carrying `status: cancelled`, and a cancelled MASTER takes its
    # exceptions with it wherever its tombstone landed.
    held = {row["id"] for row in got["calendarevent"]}
    missing = set(exp.events) - held
    check(not missing,
          "event: %d rows the recordings require are absent, e.g. %s"
          % (len(missing), sorted(missing)[:3]))
    unexpected = held - set(exp.events)
    check(not unexpected,
          "event: %d rows no recording accounts for, e.g. %s"
          % (len(unexpected), sorted(unexpected)[:3]))
    gone = held & exp.cancelled
    check(not gone,
          "event: %d rows belong to a master Google tombstoned and are still "
          "live, e.g. %s" % (len(gone), sorted(gone)[:3]))
    note("%-15s %d (exactly the recordings' set)" % ("calendarevent", len(held)))
    # Addresses are a FLOOR, not an equality: the mirror mints one per address
    # any stream saw, and a recording set the scenario only partly parses (a
    # header this file does not read) can only ever add.
    check(len(got["emailaddress"]) >= len(exp.addresses),
          "emailaddress: the repository holds %d rows, fewer than the %d "
          "addresses the recordings name"
          % (len(got["emailaddress"]), len(exp.addresses)))
    note("%-15s %d (>= %d named)" % ("emailaddress", len(got["emailaddress"]),
                                     len(exp.addresses)))
    note("%-15s %d" % ("gmailattachment", len(got["gmailattachment"])))
    return got


def assert_ids(exp, got):
    step("every id is composed from Google's own")
    held = {row["id"] for row in got["calendar"]}
    for cid in exp.calendars:
        check(calendar_id(cid) in held,
              "calendar %s is not at its composed id %s" % (cid, calendar_id(cid)))
    held = {row["id"] for row in got["gmailmessage"]}
    for mid in exp.messages:
        check(message_id(mid) in held,
              "gmailmessage %s is not at its composed id %s" % (mid, message_id(mid)))
    held = {row["id"] for row in got["gmailthread"]}
    for tid in exp.threads:
        check(thread_id(tid) in held,
              "gmailthread %s is not at its composed id %s" % (tid, thread_id(tid)))
    held = {row["id"] for row in got["contact"]}
    for resource in exp.contacts:
        check(contact_id(resource) in held,
              "contact %s is not at its composed id %s"
              % (resource, contact_id(resource)))
    note("%d calendars, %d messages, %d threads, %d contacts at composed ids"
         % (len(exp.calendars), len(exp.messages), len(exp.threads),
            len(exp.contacts)))


def synthesised_slots():
    """The consumer slots an installed mapping mints on OUR kinds, per kind.

    `docs/kind-design.md` §3: a provider carries no consumer slot — no
    `person`, no `task`. That is a rule about what this BUNDLE declares and
    what this SYNC writes, and it still holds. It is not a rule about what is
    on the row: since #570 the engine synthesises a `recordmapping`'s
    `property` on its source kind, so a repository with `kinds/people`
    installed has a `person` on every `contact` and every `emailaddress`.
    Asserting the row is bare is therefore only correct where nothing maps
    from it — true in a throwaway e2e repository, false in the owner's, which
    is exactly where the seed-mode scenario failed (T-048).

    Returns `{local kind name: {property, …}}`, empty when nothing maps.
    """
    out = {}
    try:
        maps = rows("substrate.reamde.dev/core/recordmapping", scoped=False)
    except Exception as e:
        note("could not read the record mappings (%s) — no slot is subtracted"
             % str(e)[:120])
        return out
    for m in maps:
        p = props(m)
        src = p.get("from")
        src = src.get("ref", "") if isinstance(src, dict) else str(src or "")
        prop = str(p.get("property") or "")
        if not prop or (PACKAGE + "/") not in src:
            continue
        out.setdefault(src.rsplit("/", 1)[-1], set()).add(prop)
    return out


def assert_relations(exp, got):
    """Every relation the mirror states resolves.

    ONE shape now, and it is a reference: `thread`, `calendar`,
    `recurrenceOf`, `message` and `account` are references outright, and
    every ADDRESS is a reference FIELD inside an occurrence object —
    `attendees[].address`, `from[].address`, `contact.emailAddresses[].address`
    — because the same address can occur twice and each occurrence carries
    more than link data holds. Version 17's `attendeeEmails`, `fromEmail` and
    `emails` scalar duplicates are gone; the `value` beside each reference is
    the entry's OWN field, not a copy of the referent's key.
    """
    step("every relation resolves")
    addresses = {row["id"] for row in got["emailaddress"]}
    threads = {row["id"] for row in got["gmailthread"]}
    messages = {row["id"] for row in got["gmailmessage"]}
    calendars = {row["id"] for row in got["calendar"]}
    series = {row["id"] for row in got["calendarseries"]}

    seen, statuses = 0, 0
    for row in got["calendarevent"] + got["calendarseries"]:
        p = props(row)
        check(ref_id(p.get("account")) == ACCOUNT,
              "%s %s points at account %r" % (row.get("kind"), row["id"],
                                              ref_id(p.get("account"))))
        cal = ref_id(p.get("calendar"))
        check(not cal or cal in calendars,
              "%s %s names calendar %s, which is not a row"
              % (row.get("kind"), row["id"], cal))
        check("calendarId" not in p,
              "%s %s still carries a `calendarId` scalar beside its calendar "
              "reference" % (row.get("kind"), row["id"]))
        for name in ("creator", "organizer"):
            entry = p.get(name)
            if not entry or not entry.get("address"):
                continue
            check(ref_id(entry["address"]) in addresses,
                  "%s %s: %s %s has no emailaddress row"
                  % (row.get("kind"), row["id"], name, entry.get("value")))
            seen += 1
        for entry in p.get("attendees") or []:
            if entry.get("address"):
                check(ref_id(entry["address"]) in addresses,
                      "%s %s: attendee %s has no emailaddress row"
                      % (row.get("kind"), row["id"], entry.get("value")))
                seen += 1
            # The address the entry names and the row it points at must be
            # the same one, or the occurrence object and the hub disagree.
            if entry.get("value") and entry.get("address"):
                check(ref_id(entry["address"]) == address_id(entry["value"]),
                      "%s %s: attendee `value` %s and `address` %s are not "
                      "the same address" % (row.get("kind"), row["id"],
                                            entry["value"],
                                            ref_id(entry["address"])))
            if entry.get("responseStatus"):
                check(entry["responseStatus"] in
                      ("needsaction", "declined", "tentative", "accepted"),
                      "%s %s: attendee responseStatus %r is not one of the "
                      "enum's four" % (row.get("kind"), row["id"],
                                       entry["responseStatus"]))
                statuses += 1
    check(seen > 0,
          "not one event or series names an attendee or an organizer — the "
          "people half of the calendar mirror did not write")
    check(statuses > 0,
          "not one attendee carries a `responseStatus` — the per-occurrence "
          "half of the attendee entry did not write")
    note("%d attendee/organizer addresses resolve, %d carry a responseStatus"
         % (seen, statuses))

    for row in got["gmailmessage"]:
        p = props(row)
        tid = ref_id(p.get("thread"))
        check(tid in threads,
              "gmailmessage %s names thread %s, which is not a row" % (row["id"], tid))
        check("threadId" not in p,
              "gmailmessage %s still carries a `threadId` scalar beside its thread "
              "reference" % row["id"])
        for name in ("from", "to", "cc", "bcc", "replyTo"):
            for entry in p.get(name) or []:
                check(entry.get("address") is not None,
                      "gmailmessage %s: %s entry %r names no address reference"
                      % (row["id"], name, entry.get("value")))
                check(ref_id(entry["address"]) in addresses,
                      "gmailmessage %s: %s %s has no emailaddress row"
                      % (row["id"], name, entry.get("value")))
    note("every message's thread and header addresses resolve")

    for row in got["gmailthread"]:
        for value in props(row).get("messages") or []:
            check(ref_id(value) in messages,
                  "gmailthread %s names a message that is not a row" % row["id"])
    note("every thread's `messages` resolve")

    for row in got["gmailattachment"]:
        check(ref_id(props(row).get("message")) in messages,
              "gmailattachment %s names a message that is not a row" % row["id"])

    probes = 0
    for row in got["contact"]:
        p = props(row)
        check("emails" not in p,
              "contact %s still carries the `emails` scalar probe" % row["id"])
        for entry in p.get("emailAddresses") or []:
            if not entry.get("value"):
                continue
            check(entry.get("address") is not None,
                  "contact %s: address entry %s names no reference"
                  % (row["id"], entry["value"]))
            check(ref_id(entry["address"]) in addresses,
                  "contact %s names %s, which has no emailaddress row"
                  % (row["id"], entry["value"]))
            probes += 1
    check(probes > 0,
          "not one contact carries an address entry — the People half of the "
          "mirror did not write")
    note("%d contact address entries resolve to hub rows" % probes)

    # The HUB carries the identity alone: a `displayName` here would be
    # last-writer-wins across every header and attendee list that ever named
    # the address, which is the bug the occurrence object exists to fix.
    #
    # EXCEPT THE SLOT THE ENGINE SYNTHESISES, which is not the provider's
    # (T-048). With #570 a `recordmapping` gets its `property` minted on its
    # SOURCE kind, so in the owner's repository `googleaddressperson` puts a
    # `person` on every hub row — and this assertion failed the seed by
    # design, calling the mapping's own slot a leak. What the bundle must not
    # declare and the sync must not write is still asserted; what an
    # installed mapping targets is subtracted, per kind, the way the GitHub
    # scenario subtracts it.
    minted = synthesised_slots()
    for row in got["emailaddress"]:
        p = props(row)
        check(p.get("address"), "emailaddress %s carries no address" % row["id"])
        for gone in ("displayName", "isOwner", "person"):
            if gone in minted.get("emailaddress", set()):
                continue
            check(gone not in p,
                  "emailaddress %s still carries `%s` — a hub carries the "
                  "identity alone" % (row["id"], gone))
    on_hub = sorted(minted.get("emailaddress", set()))
    note("every emailaddress row carries the address and nothing else%s"
         % (" (besides %s, synthesised by an installed mapping)"
            % ", ".join(on_hub) if on_hub else ""))

    # `recurrenceOf` does not `mustExist`, and deliberately: Google sends an
    # exception whose master the walk never returns (a master deleted long
    # ago, or living in another calendar), so a DANGLING pointer is real
    # data rather than a bug. What must hold is that the mirror invented
    # nothing: a pointer dangles only where the recordings carry no master
    # for it either.
    dangling = 0
    for row in got["calendarevent"]:
        sref = ref_id(props(row).get("recurrenceOf"))
        if not sref or sref in series:
            continue
        dangling += 1
        check(sref not in exp.series,
              "calendarevent %s overrides series %s, which the recordings carry and "
              "the repository does not" % (row["id"], sref))
    if dangling:
        note("%d events override a master the walk never returned — the "
             "pointer dangles, as the mirror declares it may" % dangling)


# --------------------------------------------------------------------------
# the Drive stream (T-088)
# --------------------------------------------------------------------------

def _normal_address(addr):
    """The bundle's mailbox normal form: the DOMAIN lowercased, the local
    part exactly as spelled."""
    text = str(addr or "").strip()
    if "@" not in text:
        return ""
    local, _, domain = text.rpartition("@")
    return local + "@" + domain.lower()


def _export_requests():
    """How many times the sync asked the mock for each file's export, off the
    request log — the proof of ONE network fetch per document."""
    out = {}
    for entry in (mock("/__mock/requests") or {}).get("requests") or []:
        path = str(entry.get("path") or "")
        m = re.match(r"^/drive/v3/files/([^/]+)/export$", path)
        if m:
            fid = urllib.parse.unquote(m.group(1))
            out[fid] = out.get(fid, 0) + 1
    return out


def _first_drive_walk_logged():
    """Did THIS mock serve the first sync's `files.list`? A `--scenario-only`
    run starts a fresh mock over a repository an earlier run synced, so its
    log holds none of the first walk's fetches and a count of one per
    document would be a count of the wrong run (T-054's lesson, applied)."""
    if not MOCK_URL:
        return False
    return any(str(e.get("path") or "") == "/drive/v3/files"
               and str(e.get("match") or "") != "miss"
               for e in (mock("/__mock/requests") or {}).get("requests") or [])


def assert_drive(exp, got, account_props):
    """T-088: the Drive mirror, row for row against the recordings.

    Asserted in the guide's order: one row per File the two listing pages
    carry at its composed id and no row beyond them; every field the mask
    asked for present as the API spelled it (`fileId`, `name`, `mimeType`,
    `modifiedTime`, `size` as an int) and `at` equal to `createdTime` — the
    API's instant in the trait slot and NOT redeclared; every owner an
    occurrence object whose `address` resolves to an `emailaddress` row at
    the normalised address; every `parents` pointer resolving to a row, or
    dangling only where the recordings carry no such file; then the
    SIBLING CALL — every Doc, Sheet and Slides deck the listing named
    carries the export recording's text, `exportedAt`, the media type it
    was exported as and no `exportError`; the deck Drive REFUSED carries the
    403 on `exportError` and no text; a binary carries none of the four; and
    the mock's request log shows exactly ONE export call per document.
    Finally the account: `driveStartPageToken` is the token the start-token
    recording answered, stamped only because the walk completed.
    """
    step("the Drive mirror: files, owners, parents and exports")
    if "drive" not in STREAMS:
        note("SKIPPED: this recording set carries no Drive stream")
        return
    gpull = _drive_constants()
    rows_by_id = {row["id"]: row for row in got["drivefile"]}
    missing = [fid for fid in exp.drive_files if file_id(fid) not in rows_by_id]
    check(not missing, "drivefile: %d files the recordings carry are absent, e.g. %s"
          % (len(missing), missing[:3]))
    unexpected = set(rows_by_id) - {file_id(f) for f in exp.drive_files}
    check(not unexpected, "drivefile: %d rows no recording accounts for, e.g. %s"
          % (len(unexpected), sorted(unexpected)[:3]))
    addresses = {row["id"] for row in got["emailaddress"]}
    owners_seen, parents_resolved, parents_dangling = 0, 0, 0
    for fid, item in sorted(exp.drive_files.items()):
        row = rows_by_id[file_id(fid)]
        p = props(row)
        check(ref_id(p.get("account")) == ACCOUNT,
              "drivefile %s points at account %r" % (fid, ref_id(p.get("account"))))
        check(p.get("fileId") == fid,
              "drivefile %s carries fileId %r" % (fid, p.get("fileId")))
        for key in ("name", "mimeType"):
            check(p.get(key) == item.get(key),
                  "drivefile %s.%s is %r, the recording says %r"
                  % (fid, key, p.get(key), item.get(key)))
        check(str(p.get("at") or "")[:19] == str(item.get("createdTime") or "")[:19],
              "drivefile %s: at=%r, createdTime=%r — the trait's instant is the "
              "API's createdTime" % (fid, p.get("at"), item.get("createdTime")))
        check("createdTime" not in p,
              "drivefile %s redeclares `createdTime` beside the trait's `at`" % fid)
        check(str(p.get("modifiedTime") or "")[:19] == str(item.get("modifiedTime") or "")[:19],
              "drivefile %s: modifiedTime %r, recording %r"
              % (fid, p.get("modifiedTime"), item.get("modifiedTime")))
        if item.get("size") is not None:
            check(p.get("size") == int(item["size"]),
                  "drivefile %s: size %r, the recording's decimal string is %r"
                  % (fid, p.get("size"), item["size"]))
        for gone in ("raw", "ownerEmails", "parentIds", "createdTime"):
            check(gone not in p, "drivefile %s carries `%s`" % (fid, gone))
        for entry in p.get("owners") or []:
            if not entry.get("emailAddress"):
                continue
            check(entry.get("address") is not None,
                  "drivefile %s: owner %s names no address reference"
                  % (fid, entry["emailAddress"]))
            check(ref_id(entry["address"]) in addresses,
                  "drivefile %s: owner %s has no emailaddress row"
                  % (fid, entry["emailAddress"]))
            check(ref_id(entry["address"]) == address_id(_normal_address(entry["emailAddress"])),
                  "drivefile %s: owner `emailAddress` %s and `address` %s disagree"
                  % (fid, entry["emailAddress"], ref_id(entry["address"])))
            owners_seen += 1
        for want in item.get("parents") or []:
            refs = ref_ids(p.get("parents"))
            check(file_id(want) in refs,
                  "drivefile %s: parent %s is not among its `parents` references %s"
                  % (fid, want, refs))
            if file_id(want) in rows_by_id:
                parents_resolved += 1
            else:
                check(want not in exp.drive_files,
                      "drivefile %s: parent %s is in the recordings and not a row" % (fid, want))
                parents_dangling += 1
    check(owners_seen > 0, "not one Drive file names an owner with an address — the "
                           "people half of the Drive mirror did not write")
    note("%d files at composed ids; %d owner addresses resolve to hub rows; %d "
         "parents resolve, %d dangle where the window holds no such folder"
         % (len(exp.drive_files), owners_seen, parents_resolved, parents_dangling))

    # THE SIBLING CALL.
    exported, refused, binaries = 0, 0, 0
    counted = _first_drive_walk_logged()
    calls = _export_requests() if counted else {}
    if MOCK_URL and not counted:
        note("this mock did not serve the first walk (a scenario-only run) — "
             "the one-fetch-per-document counts are not asserted")
    for fid, item in sorted(exp.drive_files.items()):
        p = props(rows_by_id[file_id(fid)])
        export_as = gpull.DRIVE_EXPORTS.get(str(item.get("mimeType") or ""))
        held = exp.drive_exports.get(fid)
        if not export_as:
            for key in ("text", "exportedAt", "exportError", "exportMimeType"):
                check(key not in p, "drivefile %s (%s) carries `%s` — only a Doc, a "
                      "Sheet or a Slides deck is exported"
                      % (fid, item.get("mimeType"), key))
            check(fid not in calls, "the sync asked to export %s, which is %s"
                  % (fid, item.get("mimeType")))
            binaries += 1
            continue
        check(held is not None, "no export recording for %s (%s) — a pull gap, not "
              "a bug in the sync" % (fid, item.get("mimeType")))
        status, body = held
        if counted:
            check(calls.get(fid) == 1,
                  "the sync asked for %s's export %d time(s); one network fetch per "
                  "record per page" % (fid, calls.get(fid, 0)))
        if status == 200:
            want = body[1:] if body.startswith("\ufeff") else body
            check(p.get("text") == want.replace("\x00", ""),
                  "drivefile %s: `text` differs from the export recording (%d vs %d chars)"
                  % (fid, len(p.get("text") or ""), len(want)))
            check(p.get("exportMimeType") == export_as,
                  "drivefile %s exported as %r, want %r" % (fid, p.get("exportMimeType"), export_as))
            check(p.get("exportedAt"), "drivefile %s carries text and no exportedAt" % fid)
            check(not p.get("exportError"),
                  "drivefile %s carries text AND exportError %r" % (fid, p.get("exportError")))
            check(p.get("exportTruncated") is False,
                  "drivefile %s: exportTruncated is %r for a %d-char body under the cap"
                  % (fid, p.get("exportTruncated"), len(want)))
            exported += 1
        else:
            check("403" in str(p.get("exportError") or "") or str(status) in str(p.get("exportError") or ""),
                  "drivefile %s: Drive refused the export with %d and the row says %r"
                  % (fid, status, p.get("exportError")))
            check("text" not in p and "exportedAt" not in p,
                  "drivefile %s: a refused export still wrote text or exportedAt" % fid)
            refused += 1
    check(exported > 0, "not one exported document landed with its text")
    note("%d documents carry their exported text, %d export refused and recorded, "
         "%d files have no export by type" % (exported, refused, binaries))

    check(str(account_props.get("driveStartPageToken") or "") == exp.drive_start_token,
          "account.driveStartPageToken is %r after a completed walk; the start-token "
          "recording answered %r" % (account_props.get("driveStartPageToken"),
                                      exp.drive_start_token))
    check(account_props.get("driveSyncState") == "ok",
          "driveSyncState is %r, want ok" % account_props.get("driveSyncState"))
    note("driveStartPageToken %s stamped on completion" % exp.drive_start_token)


def assert_drive_delta(exp, after):
    """T-088: the SECOND sync read the delta, not the listing.

    The delta moves ONE file forward (its `modifiedTime`, its `version`), and
    names ONE removal of a file this set never held. So: the moved file's
    row carries the newer stamps and was RE-EXPORTED (its `modifiedTime`
    passed its `exportedAt`, and the request log shows the second fetch);
    every other document was NOT fetched again; the removal retracted
    nothing and failed nothing; and the account's cursor advanced to the
    delta's `newStartPageToken`.
    """
    step("the second Drive sync is a delta: one file moved, one fetch, the cursor advanced")
    if "drive" not in STREAMS:
        note("SKIPPED: this recording set carries no Drive stream")
        return
    if not exp.drive_changed:
        note("SKIPPED: the delta recording moves no file")
        return
    rows_by_id = {row["id"]: row for row in after["drivefile"]}
    gpull = _drive_constants()
    calls = _export_requests() if MOCK_URL else {}
    # One fetch per document in the first walk, where THIS mock served it; a
    # scenario-only run's mock saw only the delta, so its base is zero.
    base = 1 if _first_drive_walk_logged() else 0
    for fid, item in sorted(exp.drive_changed.items()):
        row = rows_by_id.get(file_id(fid))
        check(row is not None, "the delta's moved file %s is not a row" % fid)
        p = props(row)
        check(str(p.get("modifiedTime") or "")[:19] == str(item.get("modifiedTime") or "")[:19],
              "drivefile %s: modifiedTime %r after the delta, the delta says %r"
              % (fid, p.get("modifiedTime"), item.get("modifiedTime")))
        check(p.get("version") == str(item.get("version")),
              "drivefile %s: version %r after the delta, the delta says %r"
              % (fid, p.get("version"), item.get("version")))
        if gpull.DRIVE_EXPORTS.get(str(item.get("mimeType") or "")) and MOCK_URL:
            check(calls.get(fid, 0) == base + 1,
                  "the moved document %s was fetched %d time(s) in total (%d in the "
                  "first walk); a file whose modifiedTime moved past the stored one "
                  "is exported ONCE more" % (fid, calls.get(fid, 0), base))
            check(p.get("text"), "drivefile %s lost its text on re-export" % fid)
    if MOCK_URL:
        for fid, item in exp.drive_files.items():
            if fid in exp.drive_changed or not gpull.DRIVE_EXPORTS.get(str(item.get("mimeType") or "")):
                continue
            if fid in exp.drive_exports and exp.drive_exports[fid][0] == 200:
                check(calls.get(fid, 0) == base,
                      "an unchanged document %s was fetched %d time(s) across two syncs "
                      "(%d in the first walk) — the delta re-exports only what moved"
                      % (fid, calls.get(fid, 0), base))
    for fid in exp.drive_removed:
        check(file_id(fid) not in rows_by_id,
              "the delta removed %s and its row is still live" % fid)
    acct = account_props_now()
    check(str(acct.get("driveStartPageToken") or "") == exp.drive_next_token,
          "driveStartPageToken is %r after the delta; the delta handed back %r"
          % (acct.get("driveStartPageToken"), exp.drive_next_token))
    check(str(acct.get("driveSyncStatus") or "").startswith("ok"),
          "driveSyncStatus is %r after the delta" % acct.get("driveSyncStatus"))
    note("moved file re-read and re-exported once, %d removal(s) tolerated, cursor "
         "%s -> %s" % (len(exp.drive_removed), exp.drive_start_token, exp.drive_next_token))


def assert_temporal(got):
    step("every time-bearing row sits on the timeline")
    all_day = 0
    for row in got["calendarevent"] + got["calendarseries"]:
        p = props(row)
        # An IDENTITY-ONLY SHELL is a series row Google never sent a master
        # for on any page this walk read: it exists so an exception's
        # reference has a target, it carries the identity and nothing else,
        # and it therefore has no instants to carry either. The master's own
        # write completes it, because a put merges.
        if not p.get("start") and not p.get("recurrence"):
            continue
        check(p.get("at"), "%s %s has no `at`" % (row.get("kind"), row["id"]))
        check(p.get("endsAt"),
              "%s %s has no `endsAt` — temporal(range) is unfilled"
              % (row.get("kind"), row["id"]))
        # The API's own `start` rides BESIDE the trait, typed: `at` cannot
        # say whether Google sent a civil date or an instant, which is
        # exactly why `allDay` is not a property.
        start = p.get("start") or {}
        check(start.get("date") or start.get("dateTime"),
              "%s %s carries no `start.date` or `start.dateTime` beside `at`"
              % (row.get("kind"), row["id"]))
        check("allDay" not in p,
              "%s %s still carries the derived `allDay`"
              % (row.get("kind"), row["id"]))
        if start.get("date"):
            all_day += 1
    check(all_day > 0,
          "no event or series carries a `start.date` — the all-day specimen "
          "is missing, so the typed start object is unproven")
    for row in got["gmailmessage"]:
        p = props(row)
        check(p.get("at"),
              "gmailmessage %s has no `at` — temporal(point) is unfilled" % row["id"])
        check("internalDate" not in p,
              "gmailmessage %s redeclares `internalDate` beside the trait's `at`"
              % row["id"])
    # A Thread carries no instant of its own, so it binds no temporal trait.
    for row in got["gmailthread"]:
        check("at" not in props(row),
              "gmailthread %s carries an `at` — a Thread resource has no instant, "
              "and a derived one is what version 17 got wrong" % row["id"])
    note("%d events + series carry at/endsAt (%d all-day), %d messages carry at"
         % (len(got["calendarevent"]) + len(got["calendarseries"]), all_day,
            len(got["gmailmessage"])))


def assert_recurrence(exp, got):
    step("recurrence: a master, its moved instance, its cancelled slot")
    if not exp.series:
        note("SKIPPED: the recordings carry no recurring master")
        return
    by_id = {row["id"]: row for row in got["calendarevent"]}
    moved = 0
    for row in got["calendarevent"]:
        p = props(row)
        if not p.get("recurrenceOf"):
            continue
        moved += 1
        sref = ref_id(p["recurrenceOf"])
        check(p.get("originalAt"),
              "calendarevent %s overrides a series but names no original slot" % row["id"])
        check(row["id"] == exception_id(sref, p["originalAt"]),
              "calendarevent %s is not at `<series>_<slot>` (%s)"
              % (row["id"], exception_id(sref, p["originalAt"])))
    note("%d moved instances carry recurrenceOf + originalAt at the slot id"
         % moved)

    # A CANCELLATION IS A ROW, NOT A SLOT. `calendarseries.cancelledSlots` is gone:
    # it was a derived list of instants that threw away the cancelled
    # exception's id, its status and its own `originalStartTime`, unioned
    # forever, and could not represent a REINSTATED occurrence at all. The
    # cancelled resource is mirrored like any other, and the overlay a reader
    # wants is a join over `recurrenceOf` + `status`.
    cancelled_rows = [r for r in got["calendarevent"]
                      if str(props(r).get("status") or "") == "cancelled"]
    for row in got["calendarseries"]:
        check("cancelledSlots" not in props(row),
              "calendarseries %s still carries `cancelledSlots` — a derived list of "
              "instants standing in for rows it threw away" % row["id"])
    for rid in sorted(exp.cancelled):
        check(rid not in by_id,
              "%s is a resource Google tombstoned and is still a row" % rid)
    for rid in sorted(exp.tombstoned):
        row = by_id.get(rid)
        check(row is not None,
              "%s is a cancelled EXCEPTION and has no row — a cancellation is "
              "a resource with a status, not an absence and not a slot" % rid)
        check(str(props(row).get("status") or "") == "cancelled",
              "%s is the row for a cancelled exception but carries status %r"
              % (rid, props(row).get("status")))
        check(props(row).get("recurrenceOf") and props(row).get("originalAt"),
              "%s is a cancelled exception with no `recurrenceOf`/`originalAt` "
              "— the two facts that make it reinstatable" % rid)
    note("%d cancelled exceptions are live rows carrying `status: cancelled`, "
         "%d of them named by the recordings; %d tombstoned resources are gone"
         % (len(cancelled_rows), len(exp.tombstoned), len(exp.cancelled)))

    # EXDATE IS THE MASTER'S OWN LINE, and only that. Version 18 unioned
    # cancellations into it, so the master's decode and other resources'
    # lifecycles were mixed in one property that no re-read could ever clean.
    for name_, payload in exp.recurrence_lines().items():
        row = next((r for r in got["calendarseries"] if r["id"] == name_), None)
        if row is None:
            continue
        stored = [str(x) for x in (props(row).get("exdates") or [])]
        check(len(stored) <= len(payload),
              "calendarseries %s stores %d EXDATE instants and its own recurrence "
              "lines carry %d — cancellations are being unioned into the "
              "master's decode again" % (name_, len(stored), len(payload)))

    for row in got["calendarseries"]:
        p = props(row)
        if row["id"] not in exp.series:
            continue        # an identity-only shell, asserted in assert_counts
        check(p.get("recurrence") or p.get("rdates"),
              "calendarseries %s carries neither a rule nor an RDATE" % row["id"])
        check(p.get("recurrenceLines"),
              "calendarseries %s kept no verbatim recurrence lines beside the trait's "
              "rule" % row["id"])
        lines = [x for x in (p.get("recurrenceLines") or [])
                 if str(x).upper().startswith("RRULE")]
        check(len(lines) <= 1 or p.get("recurrence"),
              "calendarseries %s carries %d RRULE lines and no projected rule — an "
              "unprojectable rule set must still be visible as its lines"
              % (row["id"], len(lines)))


# --------------------------------------------------------------------------
# T-055: the two cases the owner's calendar was missing
# --------------------------------------------------------------------------

def _utc(value):
    """An instant the repository served, as RFC 3339 UTC at second grain."""
    if not value:
        return ""
    try:
        stamp = dt.datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    except ValueError:
        return ""
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=dt.timezone.utc)
    return stamp.astimezone(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _google_instant(value, zone_name=""):
    """One Google `start` / `originalStartTime` object as RFC 3339 UTC."""
    import zoneinfo
    zone = dt.timezone.utc
    if zone_name:
        try:
            zone = zoneinfo.ZoneInfo(str(zone_name))
        except Exception:
            zone = dt.timezone.utc
    value = value or {}
    if value.get("dateTime"):
        stamp = dt.datetime.fromisoformat(
            str(value["dateTime"]).replace("Z", "+00:00"))
        if stamp.tzinfo is None:
            stamp = stamp.replace(tzinfo=zone)
        return stamp.astimezone(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    if value.get("date"):
        day = dt.date.fromisoformat(str(value["date"]))
        return dt.datetime(day.year, day.month, day.day, tzinfo=zone)\
            .astimezone(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return ""


def _day_window(instant, zone_name=""):
    """The [midnight, next midnight) window around an instant, IN A ZONE —
    which is how a reader files a row under a day, so it is how the day an
    occurrence lands on is asserted."""
    import zoneinfo
    zone = dt.timezone.utc
    if zone_name:
        try:
            zone = zoneinfo.ZoneInfo(str(zone_name))
        except Exception:
            zone = dt.timezone.utc
    local = dt.datetime.fromisoformat(instant.replace("Z", "+00:00"))\
        .astimezone(zone)
    floor = local.replace(hour=0, minute=0, second=0, microsecond=0)
    return (floor.astimezone(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            (floor + dt.timedelta(days=1)).astimezone(dt.timezone.utc)
            .strftime("%Y-%m-%dT%H:%M:%SZ"))


_WEEKDAYS = {"MO": 0, "TU": 1, "WE": 2, "TH": 3, "FR": 4, "SA": 5, "SU": 6}


def _ordinal_byday(rule):
    """`BYDAY=3FR` -> (3, 4); `BYDAY=-1TH` -> (-1, 3). ("", "") when the rule
    names no ordinal."""
    match = re.search(r"BYDAY=([+-]?\d+)([A-Z]{2})", rule.upper())
    if not match or match.group(2) not in _WEEKDAYS:
        return None, None
    return int(match.group(1)), _WEEKDAYS[match.group(2)]


def _is_nth_weekday(day, nth, weekday):
    """Is `day` the nth (or -nth) `weekday` of its own month?"""
    if day.weekday() != weekday:
        return False
    if nth > 0:
        return (day.day - 1) // 7 + 1 == nth
    last = day
    while True:
        nxt = last + dt.timedelta(days=7)
        if nxt.month != day.month:
            break
        last = nxt
    return (last - day).days // 7 == -nth - 1


def assert_moved_instance(expect):
    """AN INSTANCE MOVED TO ANOTHER DAY (T-055).

    The owner's agenda was missing instances of recurring events that had
    been dragged to a different day. Two things have to hold for one of them
    to land on the day it was moved TO, and both are asserted against the
    window read itself rather than against a property in isolation:

      * the row's `at` is the exception's OWN start — the instant Google now
        holds it at — and NOT `originalAt`, the slot it left. An `at` on the
        slot files the instance under the day it moved off, which is a miss
        on one day and a phantom on the other.
      * the slot it left is CLAIMED, so the series produces no computed
        occurrence there. The exception row and the occurrence it replaces
        share one id (`<series>_<slot>`), so a slot the read does not treat
        as claimed shows up as that same id in the old day's window.
    """
    step("T-055: an instance moved to another day lands on the new day")
    moved = (expect or {}).get("movedInstance") or {}
    if not (moved.get("eventId") and moved.get("recurringEventId")):
        note("SKIPPED: the recordings carry no instance moved across a day "
             "boundary, so nothing here is exercised")
        return
    cref = calendar_id(moved["calendarId"])
    sref = series_id(cref, moved["recurringEventId"])
    series = one(KIND["calendarseries"], sref)
    check(series is not None,
          "the moved instance's master %s is not a series row — an exception "
          "whose `recurrenceOf` points at nothing is a hole in the timeline"
          % sref)
    zone = str(props(series).get("timezone") or "")
    slot = _google_instant(moved["originalStartTime"], zone)
    start = _google_instant(moved["start"], zone)
    check(slot and start, "the declared moved instance names no instants")
    check(slot[:10] != start[:10],
          "the declared moved instance no longer crosses a day boundary "
          "(slot %s, start %s) — re-cut the fixtures" % (slot, start))
    rid = exception_id(sref, slot)
    row = one(KIND["calendarevent"], rid)
    check(row is not None,
          "the moved instance %s is not a row: it is an exception at "
          "`<series>_<slot>` and the slot is %s" % (rid, slot))
    p = props(row)
    check(_utc(p.get("originalAt")) == slot,
          "calendarevent %s carries originalAt=%r; the slot it replaces is %s"
          % (rid, p.get("originalAt"), slot))
    check(_utc(p.get("at")) == start,
          "calendarevent %s carries at=%r — a moved instance's `at` is its "
          "NEW start (%s), never the slot it left (%s), or every reader files "
          "it under the day it moved off"
          % (rid, p.get("at"), start, slot))
    lo, hi = _day_window(start, zone)
    check(rid in {r["id"] for r in window(lo, hi)},
          "the window read over %s..%s (the day it moved TO) does not carry "
          "%s" % (lo, hi, rid))
    lo, hi = _day_window(slot, zone)
    check(rid not in {r["id"] for r in window(lo, hi)},
          "the window read over %s..%s (the day it moved OFF) still carries "
          "%s — the slot an override claims must not also be computed"
          % (lo, hi, rid))
    note("the moved instance is on %s and not on %s, by `at` and not by its "
         "slot" % (start[:10], slot[:10]))


def assert_ordinal_rule(expect):
    """AN ORDINAL `BYDAY` RULE, EXPANDED (T-055).

    "The third Friday of the month" is the rule form the diagnosis had to
    verify by hand, because nothing in the set carried one. Two halves: the
    mirror must project the master's line VERBATIM into the trait's single
    `recurrence` (a rule the provider mangles is a series the read cannot
    walk), and the window read must then compute the occurrences it names —
    each on the ordinal weekday-of-month the rule spells, in the series' own
    zone.
    """
    step("T-055: an ordinal BYDAY rule survives and expands")
    ordinal = (expect or {}).get("ordinalRule") or {}
    rule = str(ordinal.get("rule") or "")
    nth, weekday = _ordinal_byday(rule)
    if not ordinal.get("eventId") or nth is None:
        note("SKIPPED: the recordings carry no RRULE with an ordinal BYDAY, "
             "so the read-side expansion of one is not exercised")
        return
    cref = calendar_id(ordinal["calendarId"])
    sref = series_id(cref, ordinal["eventId"])
    row = one(KIND["calendarseries"], sref)
    check(row is not None,
          "the ordinal-rule master %s is not a series row" % sref)
    p = props(row)
    check(str(p.get("recurrence") or "") == rule,
          "calendarseries %s projects recurrence=%r; the master's own line is "
          "%r — an ordinal BYDAY has to survive verbatim or the read cannot "
          "walk it" % (sref, p.get("recurrence"), rule))
    check(rule in [str(x) for x in (p.get("recurrenceLines") or [])],
          "calendarseries %s kept no verbatim %r among its recurrence lines"
          % (sref, rule))
    zone = str(p.get("timezone") or "")
    anchor = _utc(p.get("at"))
    check(anchor, "calendarseries %s has no `at` to anchor its rule" % sref)
    lo = dt.datetime.fromisoformat(anchor.replace("Z", "+00:00"))
    hi = lo + dt.timedelta(days=400)
    got = [r for r in window(lo.strftime("%Y-%m-%dT%H:%M:%SZ"),
                             hi.strftime("%Y-%m-%dT%H:%M:%SZ"))
           if r["id"] == sref or r["id"].startswith(sref + "_")]
    slots = sorted({_utc(props(r).get("at")) for r in got if props(r).get("at")})
    check(len(slots) >= 3,
          "the window read computes %d occurrence(s) of %s over 400 days from "
          "its anchor; %r names one a month — an ordinal BYDAY the expander "
          "drops is exactly the miss T-055 went looking for"
          % (len(slots), sref, rule))
    import zoneinfo
    try:
        tz = zoneinfo.ZoneInfo(zone) if zone else dt.timezone.utc
    except Exception:
        tz = dt.timezone.utc
    for slot in slots:
        day = dt.datetime.fromisoformat(slot.replace("Z", "+00:00"))\
            .astimezone(tz).date()
        check(_is_nth_weekday(day, nth, weekday),
              "%s computed an occurrence on %s, which is not the %d%s of its "
              "month that %r names" % (sref, day, nth,
                                       " weekday-of-month", rule))
    note("%d occurrences computed from %r, every one on the ordinal it names"
         % (len(slots), rule))


def assert_owner_identity(account_props, got):
    """The account's own identity is a RELATION, not a flag on a mirror.

    Version 17 computed `fromOwner`, `directedAtOwner` and `mentionsOwner` on
    every message and `isOwner` on every address. All four are gone: the
    owner is `account.address`, and "mail I sent" is a filter on the `from`
    entry's reference.
    """
    step("the account's identity is a reference, and no mirror carries a flag")
    owner = str(account_props.get("email") or "").lower()
    ref = ref_id(account_props.get("address"))
    check(ref, "the account carries no `address` reference — the connection's "
               "own identity is not a relation")
    check(ref == address_id(owner),
          "account.address names %s, and the account's email %s composes to %s"
          % (ref, owner, address_id(owner)))
    check(one(KIND["emailaddress"], ref) is not None,
          "account.address names %s, which is not an emailaddress row" % ref)
    for row in got["gmailmessage"]:
        for gone in ("fromOwner", "directedAtOwner", "mentionsOwner"):
            check(gone not in props(row),
                  "gmailmessage %s still carries the owner-relative `%s`"
                  % (row["id"], gone))
    note("account.address -> %s, and no message carries an owner flag" % owner)


def assert_no_raw(got):
    """NOT ONE `raw` in the closure.

    `gmailmessage.payload` is the one `json` this package declares, and it is the
    recursive MIME tree the guide admits by name — not a copy of the row.
    Everything else is typed.
    """
    step("no kind hides the payload under `raw`")
    for name in ("calendar", "calendarseries", "calendarevent",
                 "gmailmessage", "contact", "contactgroup", "gmaillabel",
                 "gmailthread", "gmailattachment", "emailaddress", "drivefile"):
        for row in got[name]:
            check("raw" not in props(row),
                  "%s %s still carries a `raw` payload" % (name, row["id"]))
    payloads = len([r for r in got["gmailmessage"] if props(r).get("payload")])
    check(payloads > 0,
          "not one message kept its `payload` — the MIME tree is declared "
          "beside its decode, not instead of it")
    note("no row carries `raw`; %d messages keep the payload tree beside "
         "their decoded headers" % payloads)


def assert_mime_fidelity(exp, got, recs):
    """THE BYTES, NOT A COUNT OF THEM.

    `assert_no_raw` checked that no key is literally named `raw` and that
    something called `payload` exists — which the version that deleted every
    `data` key from the MIME tree passed. This one takes the base64url a
    recording carries and looks for THAT STRING in the stored payload, and
    takes the header lines the recording carries and looks for each name and
    value in the stored `headers`. A lossy decode cannot pass it.

    OVER THE HYDRATES ONLY, and that is not a softening (T-048). A row can
    exist for a message this run never fetched: `threads.get` returns the
    whole thread and the sync mints a THIN member row for every message in
    it, carrying the identity the Thread embedded and nothing else — no
    `payload`, no `headers`, by design. A `raw/<account>` directory is
    appended to by every pull, so it also holds `messages.get` recordings no
    live listing names any more; comparing one of those against the thin row
    its thread minted asks the sync to have stored bytes it had no call that
    could have read them. The owner's seed failed exactly there, on a
    message whose row carries `account, at, historyId, labels, messageId,
    sizeEstimate, snippet, thread` — a thread member, complete for what it
    is. What a HYDRATE stored is still compared byte for byte.
    """
    step("the stored MIME tree is the recording's bytes")
    by_id = {}
    for row in got["gmailmessage"]:
        upstream = props(row).get("messageId")
        if upstream:
            by_id[str(upstream)] = row
    hydrated = {mid for mid, payload in exp.messages.items()
                if not (payload or {}).get("__thin")}

    def parts(node):
        yield node
        for child in node.get("parts") or []:
            for sub in parts(child):
                yield sub

    checked_bytes, checked_headers, thin = 0, 0, 0
    for name, doc in sorted(recs.items()):
        if not name.startswith("GET_gmail_v1_users_me_messages_") \
                or "format-full" not in name:
            continue
        mid = str(doc.get("id") or "")
        if mid not in hydrated:
            thin += 1
            continue
        row = by_id.get(mid)
        if row is None:
            continue
        stored = json.dumps(props(row).get("payload") or {}, ensure_ascii=False)
        for part in parts(doc.get("payload") or {}):
            data = ((part.get("body") or {}).get("data") or "")
            if len(data) < 24:
                continue
            check(data[:200] in stored,
                  "gmailmessage %s: part %r's base64url body is not in the stored "
                  "payload — the MIME tree was stripped of its content"
                  % (doc.get("id"), part.get("partId")))
            checked_bytes += 1
        headers = props(row).get("headers") or []
        spelled = {(str(h.get("name") or "").lower(), str(h.get("value") or ""))
                   for h in headers if isinstance(h, dict)}
        for header in (doc.get("payload") or {}).get("headers") or []:
            want = (str(header.get("name") or "").lower(),
                    str(header.get("value") or ""))
            check(want in spelled,
                  "gmailmessage %s: the %r header the recording carries is not in "
                  "the stored headers — a header list that keeps only the "
                  "first occurrence of a name is not the source"
                  % (doc.get("id"), header.get("name")))
            checked_headers += 1
    check(checked_bytes,
          "not one message part with a body was compared — either no fixture "
          "carries inline content or `payload` is not stored")
    note("%d inline part bodies and %d header lines are byte-identical to the "
         "recordings%s" % (checked_bytes, checked_headers,
                           ", %d hydrate recordings no live listing names "
                           "were skipped (their rows are thread members, "
                           "which carry no payload)" % thin if thin else ""))


def assert_threads_authoritative(got, recs):
    """A thread's own fields come from the Thread resource or not at all."""
    step("gmailthread rows carry the Thread resource's own fields")
    reads = {name: doc for name, doc in recs.items()
             if name.startswith("GET_gmail_v1_users_me_threads_")}
    if not reads:
        note("SKIPPED: this recording set has no `users.threads.get` — every "
             "gmailthread is legitimately thin (identity only)")
        return
    by_id = {str(props(r).get("threadId") or ""): r for r in got["gmailthread"]}
    checked = 0
    for name, doc in sorted(reads.items()):
        tid = str(doc.get("id") or "")
        row = by_id.get(tid)
        if row is None:
            continue
        stored = props(row)
        check(str(stored.get("historyId") or "") == str(doc.get("historyId") or ""),
              "gmailthread %s carries historyId %r, `threads.get` says %r — the "
              "thread's own field is not a message's"
              % (tid, stored.get("historyId"), doc.get("historyId")))
        want = [message_id(str(m.get("id"))) for m in doc.get("messages") or []
                if m.get("id")]
        check(ref_ids(stored.get("messages")) == want,
              "gmailthread %s membership is %r, the Thread resource says %r — "
              "membership is the resource's array, in its order, not a union "
              "of what a window happened to see"
              % (tid, ref_ids(stored.get("messages")), want))
        checked += 1
    check(checked, "no thread row matched a `threads.get` recording")
    note("%d thread rows carry the Thread resource's own historyId and its "
         "membership, in its order" % checked)


def plant_stale_row(got):
    """A row NO recording names, planted before the second drain.

    An incremental run must delete NOTHING it did not see: deletion is the
    sweep's job, and the sweep only runs behind a full re-read. Nothing in
    this scenario proved that, so a bug that deleted every row a delta did
    not mention would have passed every assertion here. The row is built by
    copying a real one, so it satisfies whatever the kind requires, and it is
    removed again at the end.
    """
    if not got["gmailmessage"]:
        return None
    source = props(got["gmailmessage"][0])
    rid = "e2e-stale-" + _id_hash("stale", ACCOUNT)[:16]
    # Everything the kind REQUIRES is kept (`account`, `thread`), the bulky
    # decoded parts are not, and the upstream id is one no recording names —
    # which is what makes the row invisible to every read the sync makes.
    body = {k: v for k, v in source.items()
            if k not in ("messageId", "payload", "headers", "body")}
    body["messageId"] = "stale" + rid.replace("-", "")
    # PUT at the RECORD PATH, which is the record's reference and not a
    # collection under `/records`: `/api/v1/{authority}/{package}/{kind}/{id}`.
    # `POST /records` mints a SERVER-ASSIGNED id and refuses a body that names
    # one, and a row planted under an id the scenario chose is the whole point
    # — the sync must not touch it.
    try:
        api("/api/v1/" + KIND["gmailmessage"] + "/" + rid, "PUT",
            {"kind": KIND["gmailmessage"], "properties": body})
    except Failed as e:
        note("could not plant the stale row (%s) — the no-delete assertion is "
             "skipped" % str(e)[:200])
        return None
    note("planted %s, which no recording names" % rid)
    return rid


def assert_stale_row_survived(rid):
    step("an incremental drain deletes nothing it did not see")
    if rid is None:
        note("SKIPPED: no stale row was planted")
        return
    row = one(KIND["gmailmessage"], rid)
    check(row is not None,
          "the planted row %s was deleted by a drain that never read a full "
          "listing — only a sweep behind a complete re-read may delete, and "
          "only rows in ITS scope" % rid)
    api("/api/v1/" + KIND["gmailmessage"] + "/" + rid, "DELETE")
    note("the planted row survived the second drain, and is removed again")


def assert_restart_sweeps_a_stale_row(got):
    """T-003: a REQUESTED RESTART retracts what its listing no longer names.

    The mirror-image of `assert_stale_row_survived`, and the two together are
    the whole contract. An INCREMENTAL drain reads a delta and may delete
    nothing it did not see. A REQUESTED RESTART is the other path — the owner
    (or the seed runner) stamps `syncRequestedAt`, the stream drops to the
    windowed re-read, and the walk is then the AUTHORITY for its window: a row
    the listing no longer names is gone, and must be retracted.

    Nothing proved that half, and it was broken. The sweep fenced off every
    row whose `historyId` was above the RUN-START PROFILE READ — which is not
    the top of the mailbox, so ordinary rows sat above it (47 of 147 hydrated
    messages in the owner's own pull) and could never be swept. The fence is
    the walk's own high water mark now, and this is the assertion that keeps
    it that way.
    """
    step("a requested restart retracts a row its listing no longer names")
    rid = plant_stale_row(got)
    if rid is None:
        note("SKIPPED: no stale row could be planted")
        return
    api("/api/v1/" + KIND["account"] + "/" + ACCOUNT, "PATCH",
        {"properties": {"syncRequestedAt": _now_iso()}})
    note("stamped syncRequestedAt — the gmail stream must restart its window")
    # GMAIL'S TRIGGERS ONLY, and this assertion runs LAST for the same
    # reason: `syncRequestedAt` is account-wide, so waking every stream would
    # restart the CALENDAR walk too — and a calendar restart re-reads the base
    # pages, which puts the pre-delta `updated` back on the event the fixture
    # set moves forward between two sync tokens. That is correct behaviour and
    # a wrecked assertion, so the request is spent on gmail alone.
    ran, tried = 0, {}
    for trigger in ("google-gmail-on-request", "google-gmail-scheduled"):
        try:
            answer = api("/api/v1/substrate.reamde.dev/core/trigger/%s/run"
                         % urllib.parse.quote(trigger, safe=""), method="POST",
                         body={"kind": KIND["account"], "id": ACCOUNT})
        except Failed as e:
            tried[trigger] = "refused: %s" % str(e)[:300]
            continue
        got = int(answer.get("ran") or 0)
        tried[trigger] = "ran=%d" % got
        ran += got
    note("restart deliveries: %s"
         % "; ".join("%s %s" % (k, v) for k, v in sorted(tried.items())))
    check(ran > 0, "no gmail delivery ran for the requested restart (%s)"
          % "; ".join("%s %s" % (k, v) for k, v in sorted(tried.items())))
    # The restart re-reads the window, then sweeps, and both are paged off the
    # causal chain: wait for the row to go, and for the ack to catch up.
    gone = False
    for _ in range(72):
        if one(KIND["gmailmessage"], rid) is None:
            gone = True
            break
        time.sleep(5)
    if not gone:
        acct = props(one(KIND["account"], ACCOUNT))
        api("/api/v1/" + KIND["gmailmessage"] + "/" + rid, "DELETE")
        raise Failed(
            "the planted row %s survived a REQUESTED RESTART — the restart "
            "re-reads the window and the walk is the authority for it, so a "
            "row its listing no longer names must be retracted. gmail status "
            "%r, ack %r, request %r"
            % (rid, acct.get("gmailSyncStatus"),
               acct.get("gmailSyncRequestedAck"), acct.get("syncRequestedAt")))
    note("the requested restart swept the planted row")


def _now_iso():
    """A request stamp, with SUB-SECOND precision.

    It used to be whole seconds, and `syncRequestedAt` is a GENERATION every
    on-request guard compares for equality: two requests inside one second are
    the SAME generation, so the second one fires nothing at all — the streams
    have already acknowledged it. That is the hazard
    `docs/provider-practices.md` § "Sub-second sync stamps" names, and it only
    became reachable here when the bounded-drain case started handing back in
    milliseconds: its drive and the resumption's landed in the same second,
    and no delivery ran.
    """
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def assert_fixtures_are_pseudonymous(recs):
    """THE RECORDINGS THEMSELVES, DECODED, BEFORE ANY CLAIM ABOUT THEM.

    A green scenario used to say nothing about whether the set it ran on was
    publishable, and the first Google cut passed every assertion here while
    carrying the owner's own address 61 times — base64, inside a calendar
    `htmlLink`'s `eid`. This is the cheap, self-contained half of
    `fixtures/audit.py`: it holds no real values (it cannot, in a committed
    file), so it asserts the INVARIANT instead — every address in the set,
    after decoding, is at a reserved domain or in the committed cast, and
    every URL host is Google's, a loopback, or a `.example`.

    The full audit (`python3 providers/google/fixtures/audit.py`) is the one
    that scans against the real literals in the gitignored map and the raw
    pull. This one runs on every e2e, in under a second, and would have
    caught the leak.
    """
    step("the recordings are pseudonymous, after decoding")
    if MODE == "seed":
        note("SKIPPED: a seed run is served the owner's REAL pulls by design")
        return
    cast = set()
    public = os.path.join(ROOT, "providers", "persona.public.json")
    if os.path.exists(public):
        with open(public, "r", encoding="utf-8") as fh:
            def harvest(node):
                if isinstance(node, dict):
                    for v in node.values():
                        harvest(v)
                elif isinstance(node, list):
                    for v in node:
                        harvest(v)
                elif isinstance(node, str):
                    cast.add(node.casefold())
            harvest(json.load(fh))
    ok_domains = (".example", ".example.com", "google.com", "gmail.com",
                  "googleapis.com", "googleusercontent.com", "googlegroups.com",
                  "group.calendar.google.com", "localhost",
                  # Google's static CDN (`fonts.gstatic.com` in a mail's html,
                  # `ssl.gstatic.com` behind a Drive icon): a vendor host with
                  # no tenant in it, the same allowance the audit makes (T-088).
                  "gstatic.com",
                  # T-065: a VENDOR'S TRANSACTIONAL SENDER NAMES NO PERSON.
                  # `fred@fireflies.ai` and `app.fireflies.ai/view/<id>` are
                  # exactly what `capture/detecttranscript` keys on, so the
                  # recap fixture cannot pseudonymise them and stay a test.
                  # Same rule `google.com` is already here under.
                  "fireflies.ai")
    ok_hosts = ok_domains + ("127.0.0.1",)
    bad_addresses, bad_hosts, decoded = set(), set(), 0

    def plaintexts(value):
        """`value`, plus everything one decode turns it into: percent-escapes,
        a base64url body part, and a Calendar `eid`."""
        yield value
        seen = {value}
        queue = [value]
        while queue:
            cur = queue.pop()
            more = []
            if "%" in cur:
                more.append(urllib.parse.unquote(cur))
            if "eid=" in cur:
                blob = cur.split("eid=", 1)[1].split("&")[0]
                try:
                    more.append(base64.b64decode(
                        blob + "=" * (-len(blob) % 4)).decode("utf-8"))
                except Exception:
                    pass
            elif len(cur) >= 16 and _B64_ONLY.match(cur):
                try:
                    more.append(base64.urlsafe_b64decode(
                        cur + "=" * (-len(cur) % 4)).decode("utf-8"))
                except Exception:
                    pass
            for text in more:
                if text and text not in seen and len(seen) < 8:
                    seen.add(text)
                    queue.append(text)
                    yield text

    def walk(node):
        nonlocal decoded
        if isinstance(node, dict):
            for v in node.values():
                walk(v)
        elif isinstance(node, list):
            for v in node:
                walk(v)
        elif isinstance(node, str):
            for text in plaintexts(node):
                decoded += 1
                for addr in _ADDRESS_RE.findall(text):
                    low = addr.casefold()
                    if low in cast or low.endswith(ok_domains):
                        continue
                    bad_addresses.add(low)
                for host in _HOST_RE.findall(text):
                    low = host.casefold().split(":")[0]
                    if low.endswith(ok_hosts) or low in cast:
                        continue
                    bad_hosts.add(low)

    for name, doc in sorted(recs.items()):
        walk(name)
        walk(doc)
    check(not bad_addresses,
          "%d address(es) in the recordings are neither at a reserved domain "
          "nor in the committed cast — a fixture may carry no real mailbox: %s"
          % (len(bad_addresses), ", ".join(sorted(bad_addresses)[:6])))
    check(not bad_hosts,
          "%d URL host(s) in the recordings are neither Google's nor reserved "
          "— a tenant subdomain names an organisation: %s"
          % (len(bad_hosts), ", ".join(sorted(bad_hosts)[:6])))
    note("%d decoded views of %d recordings: every address is cast or "
         "reserved, every host is Google's or `.example`"
         % (decoded, len(recs)))


def wake_and_wait():
    """Run every stream a SECOND time, against the same recordings.

    `wake` alone proves little here: each stream's `_due` check reads its own
    cadence anchor, and a stream that finished a minute ago is not due for an
    hour, so a woken schedule correctly does nothing. So the second sync goes
    through `run` on each SCHEDULED trigger, carrying the account record: a
    schedule has no guard, the body sees a record envelope, and the
    record-triggered path syncs that one account whatever its cadence says.
    That is the same delivery a connect makes, which is exactly the one whose
    idempotence is worth proving.
    """
    step("a second sync, one per stream, against the same recordings")
    ran = 0
    for trigger in TRIGGERS:
        answer = api("/api/v1/substrate.reamde.dev/core/trigger/%s/run"
                     % urllib.parse.quote(trigger, safe=""), method="POST",
                     body={"kind": KIND["account"], "id": ACCOUNT})
        ran += int(answer.get("ran") or 0)
    note("%d deliveries applied effects" % ran)
    check(ran > 0,
          "not one of %s re-delivered — the second sync never ran, so "
          "idempotence is unproven" % ", ".join(TRIGGERS))
    # The drain is paged off the causal chain, so the run returns before the
    # last page commits. Wait for every stream to stamp a FRESH anchor.
    before = {}
    for _ in range(60):
        acct = props(one(KIND["account"], ACCOUNT))
        stamps = tuple(acct.get(s + "LastSyncedAt")
                       for s in STREAMS)
        if before and stamps == before:
            break
        before = stamps
        time.sleep(5)
    return ran


def _short(value, width=60):
    text = json.dumps(value, ensure_ascii=False, sort_keys=True) \
        if not isinstance(value, str) else value
    return text if len(text) <= width else text[:width] + "..."


def calendar_versions():
    """Every calendar row's version, by id — what a drain must not move."""
    return {r["id"]: (r.get("status") or {}).get("version")
            for r in rows(KIND["calendar"])}


def assert_versions_held(before, after):
    """THE POINT OF THE STATE KIND, asserted.

    A second sync re-reads every calendar's events and commits a fresh sync
    token. If the token were a property of the calendar mirror — as it was in
    version 17 — that write would bump the calendar row's version and fire
    every trigger watching a calendar, once per page of history. It is a
    `calendarsync` row now, so the calendar's version must stand still.
    """
    step("a drain does not move the calendar rows' versions")
    for rid, was in sorted(before.items()):
        now = after.get(rid)
        check(now == was,
              "calendar %s moved from version %r to %r across a sync — the "
              "per-calendar cursor is back on the mirror" % (rid, was, now))
    note("%d calendar rows held their version across a full second sync"
         % len(before))


def assert_idempotence(before, exp, planted=None):
    step("idempotence: the same recordings write the same rows")
    # The row this scenario planted is not the sync's, and it is deliberately
    # still there — it is the no-delete assertion's evidence, not a duplicate.
    after = {name: [r for r in rows(KIND[name]) if r["id"] != planted]
             for name in before}
    for name in before:
        check(len(after[name]) == len(before[name]),
              "%s: %d rows before the second sync, %d after — the sync is not "
              "an upsert" % (name, len(before[name]), len(after[name])))
    note("row counts unchanged across a second sync")

    # COUNTS ARE NOT IDEMPOTENCE. A second drain that rewrites a property,
    # nulls a field a thin payload did not carry, or re-derives a body keeps
    # the count identical and changes the row. Every property of every mirror
    # row is compared, and the only properties allowed to move are the ones
    # the second run is SUPPOSED to move: the account's cursors and stamps
    # (not a mirror), and the events the fixture set deliberately updates
    # between the two sync tokens.
    # The one row the recordings MEAN to move: the event whose delta the
    # second sync token carries. Everything else is a re-read of the same
    # bytes and must land on the same row, byte for byte.
    moved = exp.delta_rows()
    drift = []
    for name in before:
        was = {r["id"]: props(r) for r in before[name]}
        now = {r["id"]: props(r) for r in after[name]}
        for rid, old in sorted(was.items()):
            if rid in moved:
                continue
            new = now.get(rid)
            if new is None:
                drift.append("%s %s vanished on the second sync" % (name, rid))
                continue
            for key in sorted(set(old) | set(new)):
                if old.get(key) != new.get(key):
                    drift.append(
                        "%s %s.%s changed across an identical re-read (%s -> %s)"
                        % (name, rid, key, _short(old.get(key)),
                           _short(new.get(key))))
    check(not drift, "the second sync rewrote %d propert%s:\n    %s"
          % (len(drift), "y" if len(drift) == 1 else "ies",
             "\n    ".join(drift[:12])))
    note("every property of every mirror row is byte-identical after a second "
         "drain over the same recordings")

    # The cursor advanced — AND IT IS NOT ON THE MIRROR. The per-calendar
    # token lives in a `calendarsync` state row, so a page of events cannot
    # bump the calendar's version; the version check below is what proves it.
    acct = props(one(KIND["account"], ACCOUNT))
    check(acct.get("gmailHistoryId"),
          "the gmail stream stamped no historyId — no cursor advanced")
    for row in after["calendar"]:
        for gone in ("syncToken", "syncWalk", "syncGeneration"):
            check(gone not in props(row),
                  "calendar %s still carries `%s` — sync bookkeeping on a "
                  "mirror" % (row["id"], gone))
    # `calendarsync` is the one kind with no `account` of its own, so it is
    # scoped through the calendar it names — which is scoped already. In a
    # seeded repository holding two Google accounts, the unscoped read is the
    # OTHER account's drain state and asserting over it means nothing.
    held = {r["id"] for r in after["calendar"]}
    state = [r for r in rows(KIND["calendarsync"], scoped=False)
             if ref_id(props(r).get("calendar")) in held]
    tokened = [r for r in state if props(r).get("syncToken")]
    check(tokened,
          "no calendarsync row holds a sync token — the incremental cursor "
          "never committed, so every run is a full re-read")
    note("gmail historyId %s, %d of %d calendars hold a sync token in "
         "calendarsync" % (acct["gmailHistoryId"], len(tokened),
                           len(after["calendar"])))
    if acct.get("contactsSyncToken"):
        note("contacts sync token held")
    return after


def assert_directory(exp, got, account_props):
    """T-031: the Workspace directory's people are contacts too, and a person
    who is in BOTH lists is ONE record.

    This is the assertion the ticket exists for. A Workspace account keeps its
    colleagues in the domain directory, not in `myContacts` — so if the
    directory stream did not run, or ran into a kind of its own, a colleague
    would either be missing or be a duplicate of the contact he already is.
    """
    step("the Workspace directory's people are contacts too")
    if not exp.directory:
        note("SKIPPED: this recording set carries no listDirectoryPeople page "
             "— the directory stream is not exercised")
        return
    held = {row["id"] for row in got["contact"]}
    missing = sorted(r for r in exp.directory if contact_id(r) not in held)
    check(not missing,
          "%d directory people are not contact rows, e.g. %s — the directory "
          "stream did not write them"
          % (len(missing), missing[:3]))

    # ONE RESOURCE NAME, ONE ROW. The whole reason the directory is mirrored
    # into `contact` rather than into a kind of its own: a colleague who is
    # also a saved contact must not be two records.
    dual = sorted(exp.directory & exp.connections)
    if dual:
        for resource in dual:
            rows = [r for r in got["contact"] if r["id"] == contact_id(resource)]
            check(len(rows) == 1,
                  "%s is in the directory AND in the address book and the "
                  "mirror holds %d rows for it — one resourceName is one row"
                  % (resource, len(rows)))
            sources = {str(s.get("type") or "") for s in
                       ((props(rows[0]).get("metadata") or {}).get("sources") or [])}
            check(sources,
                  "the dual contact %s carries no metadata.sources[] — that "
                  "array is the ONLY discriminator saying where it came from, "
                  "and no `isDirectory` flag replaces it" % resource)
        note("%d person(s) in both lists are one row each, sources declared"
             % len(dual))
    else:
        note("no person is in both lists in this set, so the one-record case "
             "is not exercised")

    # The directory's own cursor, beside the address book's and never merged.
    state = str(account_props.get("contactsDirectoryState") or "")
    check(state == "ok",
          "the account's contactsDirectoryState is %r, not `ok` — the "
          "directory walk did not complete (status: %s)"
          % (state, account_props.get("contactsDirectoryStatus")))
    check(account_props.get("contactsDirectorySyncToken"),
          "the directory walk finished with no contactsDirectorySyncToken — "
          "the next delta has no boundary to resume from")
    check(str(account_props.get("contactsDirectorySyncToken") or "")
          != str(account_props.get("contactsSyncToken") or ""),
          "the directory and the address book stamped the SAME sync token — "
          "two endpoints, two walks, two tokens")
    note("%d directory people are contacts; the directory keeps its own "
         "cursor (state %s)" % (len(exp.directory), state))


def assert_containers(exp, got):
    """T-023: the two listings that were unrecorded, as kinds and references.

    `contactgroup` and `gmaillabel` both pass §3's two-part test — their own
    endpoint AND a second kind's payload — and both were `string` fields for
    exactly one reason: no recording answered the list call, so a kind built
    from a message's or a membership's id would have been an identity with no
    fields. The recordings exist now, so this is what changed:
    `contact.memberships[].contactGroupMembership.contactGroup` and
    `gmailmessage.labels` are references at whole rows.
    """
    step("the contact groups and the Gmail labels are kinds, and referenced")
    if not exp.groups and not exp.labels:
        note("SKIPPED: this recording set answers neither list call")
        return

    if exp.groups:
        held = {row["id"] for row in got["contactgroup"]}
        missing = sorted(r for r in exp.groups if group_id(r) not in held)
        check(not missing, "%d contact groups the listing names are not rows, "
                           "e.g. %s" % (len(missing), missing[:3]))
        check(len(held) == len(exp.groups),
              "the repository holds %d contactgroup rows and the listing names "
              "%d — the walk is complete, so it is an equality"
              % (len(held), len(exp.groups)))
        kinds = {str(props(r).get("groupType") or "") for r in got["contactgroup"]}
        check("systemcontactgroup" in kinds,
              "no contactgroup row carries groupType systemcontactgroup — the "
              "enum is unexercised (got %s)" % sorted(kinds))
        for row in got["contactgroup"]:
            p = props(row)
            check(p.get("resourceName"),
                  "contactgroup %s carries no resourceName" % row["id"])
        note("%-15s %d rows, groupTypes %s"
             % ("contactgroup", len(held), ", ".join(sorted(k for k in kinds if k))))

    if exp.labels:
        held = {row["id"] for row in got["gmaillabel"]}
        missing = sorted(l for l in exp.labels if label_id(l) not in held)
        check(not missing, "%d Gmail labels the listing names are not rows, "
                           "e.g. %s" % (len(missing), missing[:3]))
        check(len(held) == len(exp.labels),
              "the repository holds %d gmaillabel rows and the listing names %d "
              "— users.labels.list is complete, so it is an equality"
              % (len(held), len(exp.labels)))
        # THE TYPE EACH LABEL'S OWN LISTING ENTRY GAVE IT, label by label.
        # This used to demand a `system` AND a `user` row, which is a fact
        # about the FIXTURE SET and not about a Gmail account: the owner's
        # work mailbox lists 15 labels and every one of them is Google's own
        # (T-048). "Both types exist" was never the property worth asserting
        # anyway — that the row carries the type the listing spelled is, and
        # it subsumes the old check wherever the listing does carry both.
        types = {str(props(r).get("type") or "") for r in got["gmaillabel"]}
        by_row = {row["id"]: row for row in got["gmaillabel"]}
        for lid, label in sorted(exp.labels.items()):
            row = by_row.get(label_id(lid))
            if row is None:
                continue
            want = str(label.get("type") or "").lower()
            check(str(props(row).get("type") or "") == want,
                  "gmaillabel %s carries type %r and the listing spells it %r"
                  % (row["id"], props(row).get("type"), want))
        named = {str(l.get("type") or "").lower() for l in exp.labels.values()}
        named.discard("")
        check(named <= types,
              "the gmaillabel rows cover %s and the listing names %s — every "
              "label the listing names lands, with its own type"
              % (sorted(types), sorted(named)))
        # THE COUNTS ARE ABSENT, NOT ZERO. Only users.labels.get returns them.
        for row in got["gmaillabel"]:
            p = props(row)
            for key in ("messagesTotal", "messagesUnread", "threadsTotal",
                        "threadsUnread"):
                check(p.get(key) is None,
                      "gmaillabel %s carries %s=%r — only users.labels.get "
                      "returns it and this bundle calls list, so it must be "
                      "ABSENT rather than written zero"
                      % (row["id"], key, p.get(key)))
        note("%-15s %d rows, types %s (every one the listing's own), no count "
             "invented" % ("gmaillabel", len(held),
                           ", ".join(sorted(t for t in types if t))))

        # EVERY LABEL A MESSAGE NAMES RESOLVES, and no scalar rides beside it.
        labels_seen = 0
        for row in got["gmailmessage"]:
            p = props(row)
            check("labelIds" not in p,
                  "gmailmessage %s still carries a `labelIds` scalar beside "
                  "its `labels` reference" % row["id"])
            for entry in p.get("labels") or []:
                rid = ref_id(entry)
                check(rid in {r["id"] for r in got["gmaillabel"]},
                      "gmailmessage %s names label %s, which is not a row"
                      % (row["id"], rid))
                labels_seen += 1
        check(labels_seen,
              "not one message carries a `labels` reference — the id list was "
              "a string array until T-023 and this is what proves it is not")
        note("%d message->label references resolve" % labels_seen)


# --------------------------------------------------------------------------
# the failure matrix (T-023)
# --------------------------------------------------------------------------

def mock(path, method="GET", body=None):
    """The mock's own control surface: its request log and its fault rules."""
    if not MOCK_URL:
        return {}
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(MOCK_URL + path, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode("utf-8")
            return json.loads(raw) if raw else {}
    except Exception as e:
        raise Failed("mock %s %s: %s" % (method, path, e))


def faults_fired():
    """Every injected response the mock served, by the rule that fired.

    The mock logs an injected response with `"match": "fault"`, which is what
    lets a scenario assert that the failure HAPPENED rather than assume it —
    a matrix whose rules never matched would otherwise pass in silence.
    """
    out = {}
    for entry in (mock("/__mock/requests") or {}).get("requests") or []:
        if str(entry.get("match") or "") != "fault":
            continue
        key = "%s %s" % (entry.get("status"), entry.get("rule")
                         or entry.get("path") or "?")
        out[key] = out.get(key, 0) + 1
    return out


def _fired_404(mid):
    """Has the mock served THIS message's 404 yet?

    Read off the fault log rather than off the request log: `faults_fired`
    keys on the RULE that matched, so "the 404 for this message fired" is a
    fact the mock states, not one the scenario infers from a path.
    """
    return any(key.startswith("404 ") and mid in key
               for key in faults_fired())


def _drive(triggers, note_as):
    """Fire these triggers with the account record and say what ran."""
    api("/api/v1/" + KIND["account"] + "/" + ACCOUNT, "PATCH",
        {"properties": {"syncRequestedAt": _now_iso()}})
    ran = 0
    for trigger in triggers:
        try:
            answer = api("/api/v1/substrate.reamde.dev/core/trigger/%s/run"
                         % urllib.parse.quote(trigger, safe=""), method="POST",
                         body={"kind": KIND["account"], "id": ACCOUNT})
        except Failed:
            continue
        ran += int(answer.get("ran") or 0)
    check(ran > 0, "no delivery ran for %s" % note_as)
    return ran


def wait_until_quiet(seconds=180):
    """Wait for every stream's cadence anchor to stop moving.

    A drain pages off the causal chain, so a trigger run returns long before
    the work stops. Anything that stamps `syncRequestedAt` while a drain is
    live has its request acknowledged by that drain and never re-plans.
    """
    last, still = None, 0
    for _ in range(int(seconds / 5)):
        acct = props(one(KIND["account"], ACCOUNT))
        now = tuple(acct.get(s + "LastSyncedAt")
                    for s in STREAMS)
        still = still + 1 if now == last else 0
        last = now
        if still >= 3:
            return "anchors still for %d polls" % still
        time.sleep(5)
    return "gave up waiting; the anchors were still moving"


def assert_failure_matrix(got):
    """T-023: the TRANSIENT failures, injected, and the recovery after each.

    THE MATRIX IS THE SCENARIO'S, NOT A FILE'S, and that is deliberate.
    `tools/e2e.py` loads `providers/<p>/e2e/faults.json` whenever the FILE
    EXISTS — it is not gated on `--mode` — so a committed matrix would inject
    429s, 500s and 404s into the owner's SEED as well as into the e2e. Posting
    the rules from here is gated on `MODE == "e2e"`, and it buys the better
    test as well: each case runs against an account that is already `ok`, so
    what the status says afterwards is THIS failure's doing.

    EVERY RULE ENDS IN `0` — the recording — so each case ends in the
    RECOVERY. A matrix that only proved the failure would prove half of it:
    the question is not whether a drain can fail, it is whether the mailbox is
    whole afterwards.
    """
    step("the injected failure matrix, and what survived it")
    if MODE != "e2e":
        note("SKIPPED: MODE is %r — failures are never injected into a seed"
             % MODE)
        return
    if not MOCK_URL:
        note("SKIPPED: no MOCK_URL, so no rules can be set")
        return

    vanished, vanished_thread = None, ""
    for row in got["gmailmessage"]:
        mid = str(props(row).get("messageId") or "")
        if mid:
            vanished = mid
            vanished_thread = ref_id(props(row).get("thread"))
            break
    check(vanished, "no message row to 404 — the matrix needs one")

    # GMAIL MUST BE QUIET FIRST. The assertion before this one drove a gmail
    # restart and its wait ends as soon as the planted row is gone — but the
    # drain pages off the causal chain, so it can still be in flight. A
    # `syncRequestedAt` stamped under a live drain is ACKNOWLEDGED by it
    # without re-planning, and the stream then never re-lists: the first run
    # of this matrix fired its 403 and its 500 and neither of its two gmail
    # rules, because gmail made no call at all.
    quiet = wait_until_quiet()
    note("gmail settled before the matrix (%s)" % quiet)

    mock("/__mock/requests", "DELETE")
    mock("/__mock/faults", "POST", {"rules": [
        # 429 WITH Retry-After on the Gmail listing: two refusals, then the
        # recording. The stream must set `account.retryNotBefore` from the
        # header and still finish the window.
        # ON THE PROFILE READ, which is the FIRST call of every gmail run (the
        # watermark, one quota unit) and therefore the only gmail endpoint a
        # run is guaranteed to touch whichever path it takes. Putting the 429
        # on the LISTING made the case depend on the stream choosing its
        # backfill, which it does not always do.
        {"match": "GET /gmail/v1/users/*/profile", "status": [429, 429, 0],
         "headers": {"Retry-After": "1"},
         "body": {"error": {"code": 429, "status": "RESOURCE_EXHAUSTED",
                            "message": "User-rate limit exceeded."}}},
        # 500 on the calendar list, once: transient, retried, not permanent.
        {"match": "GET /calendar/v3/users/me/calendarList*", "status": [500, 0],
         "body": {"error": {"code": 500, "status": "INTERNAL",
                            "message": "Backend Error"}}},
        # 404 on ONE message: Gmail answers this for a message deleted between
        # the listing and the fetch. That row goes; the page carries on.
        # PERMANENT FOR THE RUN, unlike every other rule here. The others end
        # in `0` so the drain recovers; a 404 means the message is GONE
        # UPSTREAM, and a recovery tick that served the recording again would
        # write the row back and make the assertion below contradict the
        # retraction it is checking. Deleted stays deleted.
        {"match": "GET /gmail/v1/users/*/messages/%s*" % vanished,
         "status": [404] * 40,
         "body": {"error": {"code": 404, "status": "NOT_FOUND",
                            "message": "Requested entity was not found."}}},
        # 403 on the Workspace DIRECTORY alone: a consent fact, not an outage.
        # The address book must still land and the stream must still stamp ok.
        {"match": "GET /v1/people:listDirectoryPeople*", "status": [403, 403, 0],
         "body": {"error": {"code": 403, "status": "PERMISSION_DENIED",
                            "message": "Request had insufficient authentication scopes.",
                            "details": [{"reason": "ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}},
    ]})
    note("rules set: 429+Retry-After on the gmail listing, 500 on the "
         "calendar list, 404 on message %s, 403 on the directory" % vanished)
    called = False
    for attempt in range(3):
        _drive(("google-gmail-on-request", "google-gmail-scheduled",
                "google-calendar-on-request", "google-calendar-scheduled",
                "google-contacts-on-request", "google-contacts-scheduled"),
               "the transient matrix")
        for _ in range(24):
            paths = [str(e.get("path") or "") for e in
                     (mock("/__mock/requests") or {}).get("requests") or []]
            if any("/gmail/v1/" in x for x in paths):
                called = True
                break
            time.sleep(5)
        if called:
            break
        note("gmail made no call on attempt %d — stamping again" % (attempt + 1))
    check(called,
          "the gmail stream made no call at all across three requested "
          "restarts, so its half of the matrix could not fire. A drain still "
          "in flight acknowledges a new syncRequestedAt without re-planning; "
          "if that is not it, the trigger guards are.")

    # THE RECOVERY IS THE ASSERTION. Wait for the three streams to come back
    # to `ok` — a drain that took a 429 or a 500 for a permanent failure would
    # sit at `erroring` for ever.
    # THE RETRY IS THE TICK. A transient failure leaves the stream `erroring`
    # and stamps nothing — that is the contract, so the cadence comes back for
    # it — and `_erroring` ACKNOWLEDGES the request on purpose, so the
    # on-request trigger cannot spin against the same failure. Nothing in a
    # scenario is the hourly schedule, so this IS the schedule: re-drive until
    # the stream recovers, which is what proves the failure was transient
    # rather than assuming it. Every rule is spent by now, so each re-drive
    # gets the recording.
    healthy, acct = False, {}
    for attempt in range(12):
        acct = props(one(KIND["account"], ACCOUNT))
        healthy = all(str(acct.get(s + "SyncStatus") or "").startswith("ok")
                      for s in STREAMS)
        if healthy:
            break
        wall = acct.get("retryNotBefore")
        _drive(("google-gmail-on-request", "google-gmail-scheduled",
                "google-calendar-on-request", "google-calendar-scheduled",
                "google-contacts-on-request", "google-contacts-scheduled"),
               "the recovery after the matrix")
        note("recovery tick %d (retryNotBefore was %s)"
             % (attempt + 1, wall or "clear"))
        wait_until_quiet(90)
    acct = props(one(KIND["account"], ACCOUNT))

    # THE 404 IS DRIVEN UNTIL IT FIRES (T-048), not skipped when it does not.
    # It is the one rule in the matrix that needs the stream to take a
    # particular path: only a walk that HYDRATES asks `messages.get`, and a
    # restart that goes down the history path asks for nothing. The old check
    # read the request log and said "SKIPPED" when it found no hydrate, which
    # is a green assertion testing nothing — on the runs that mattered it was
    # the only thing standing between a broken retraction and a PASS. Every
    # `_drive` stamps `syncRequestedAt`, and an unspent request plans a
    # BACKFILL (`_plan_account`), so asking again is asking for a hydrate.
    for _ in range(4):
        if _fired_404(vanished):
            break
        _drive(("google-gmail-on-request", "google-gmail-scheduled"),
               "the 404 hydrate")
        wait_until_quiet(90)
    check(_fired_404(vanished),
          "the mock never served the 404 for message %s across four requested "
          "restarts, so the vanished-message path is unexercised — the gmail "
          "stream never hydrated (a history-path restart asks `messages.get` "
          "for nothing)" % vanished)
    # The drives above are drains: let them land before the status below is
    # read, or a run still in flight reads as a stream that never recovered.
    for _ in range(18):
        acct = props(one(KIND["account"], ACCOUNT))
        if all(str(acct.get(s + "SyncStatus") or "").startswith("ok")
               for s in STREAMS):
            break
        time.sleep(5)
    acct = props(one(KIND["account"], ACCOUNT))

    fired = faults_fired()
    for key, n in sorted(fired.items()):
        code, rule = (key.split(" ", 1) + [""])[:2]
        note("injected %-4s x%-2d %s" % (code, n, rule[:70]))
    codes = {int(k.split(" ", 1)[0]) for k in fired
             if k.split(" ", 1)[0].isdigit()}
    for want, why in ((429, "the Retry-After path"),
                      (500, "the transient-retry path"),
                      (404, "the vanished-message path"),
                      (403, "the stream-not-available path")):
        check(want in codes,
              "no %d was injected — %s is unexercised (fired: %s)"
              % (want, why, sorted(codes)))

    for stream in STREAMS:
        status = str(acct.get(stream + "SyncStatus") or "")
        check(status.startswith("ok"),
              "%s is %r after the matrix — a 429, a 500, a 404 on one item and "
              "a 403 on one sub-stream are all survivable, and a drain that "
              "ends `erroring` on them has taken a retryable failure for a "
              "permanent one" % (stream, status))

    check(one(KIND["gmailmessage"], message_id(vanished)) is None,
          "the message the mock 404'd (%s) is still a row — Gmail answers "
          "404 for a message deleted between the listing and the fetch, "
          "and the sync must retract that one" % vanished)
    # AND THE THREAD LET GO OF IT. The retraction used to be undone in the
    # same batch: `threads.get` still named the member (it is the older
    # snapshot of the two), so `_stage_threads` minted the row back as a thin
    # member and the delete and the put of one id in one delivery left the row
    # standing. A thread still naming a retracted message would be a dangling
    # reference as well as a resurrection, so both ends are asserted.
    if vanished_thread:
        trow = one(KIND["gmailthread"], vanished_thread)
        if trow is not None:
            named = ref_ids(props(trow).get("messages"))
            check(message_id(vanished) not in named,
                  "gmailthread %s still names the message the mock 404'd — "
                  "the row is retracted and the membership that pointed at it "
                  "is a dangling reference" % vanished_thread)
    left = [r for r in rows(KIND["gmailmessage"])]
    check(len(left) >= 1,
          "the 404 took the whole page with it — %d message rows left"
          % len(left))
    note("the 404'd message is gone (and its thread no longer names it), "
         "%d message rows survived, every stream ok" % len(left))

    dstate = str(acct.get("contactsDirectoryState") or "")
    check(dstate in ("ok", "needsreconsent", "unavailable"),
          "contactsDirectoryState is %r after a 403 on the directory" % dstate)
    note("the directory was refused and contacts still finished (%s); "
         "retryNotBefore is %s"
         % (dstate or "unrun", acct.get("retryNotBefore") or "clear"))
    mock("/__mock/faults", "DELETE")


def _google_triggers():
    """Our triggers, with the engine's own view of each: lag, pending, parked.

    Repository-wide (`trigger status` is), so it is filtered to this bundle's:
    a seed repository holds every other provider's triggers beside ours.
    """
    body = api("/api/v1/substrate.reamde.dev/core/trigger/status")
    items = body.get("triggers") or body.get("items") or body.get("records") or []
    return [t for t in items if str(t.get("id") or "").startswith("google-")]


def _pending_now(acct):
    """How much work the account says is still owed, in either vocabulary."""
    progress = acct.get("syncProgress") or {}
    if isinstance(progress, str):
        try:
            progress = json.loads(progress)
        except ValueError:
            progress = {}
    owed = int((progress or {}).get("pending") or 0)
    for entry in _streams(acct).values():
        owed += int((entry or {}).get("pending") or 0)
    for stream in STREAMS:
        if "pending" in str(acct.get(stream + "SyncStatus") or ""):
            owed += 1
    return owed


def assert_drain_bounds_itself():
    """The parked-drain fix: a fire STOPS ITSELF and says what is left.

    The engine gives a continuation chain two minutes, cumulatively, and parks
    it when it runs past them — with its effects already committed, so the
    ledger records a failure for work that actually landed. That is what every
    hourly `google-gmail-scheduled` delivery did in production. Each stream
    now watches the chain's own clock, hands back with the remainder on the
    account, and the next fire resumes.

    PROVEN, NOT TRUSTED. `config.drainBudgetMs` shrinks the budget to ONE
    MILLISECOND for this case — the same test seam `apiBase` is — so the first
    budget check hands back instead of a minute of real work happening first.
    A budget of one SECOND is not small enough: a whole drain over these
    fixtures fits inside it, the bound never trips, and the assertion cannot
    tell a drain that stopped itself from one that simply finished. What is
    asserted is the SHAPE of the stop: work still pending, the account still
    `ok` (a bounded drain is not a failure), and NOTHING PARKED.
    """
    step("a bounded fire hands back instead of parking")
    if MODE != "e2e" or not MOCK_URL:
        note("SKIPPED: MODE is %r — the budget is never shrunk under a seed"
             % MODE)
        return
    configs = rows(KIND["config"], scoped=False)
    check(configs, "no config record to shrink the drain budget on")
    cid = configs[0]["id"]
    api("/api/v1/" + KIND["config"] + "/" + cid, "PATCH",
        {"properties": {"drainBudgetMs": 1}})
    note("config.drainBudgetMs = 1 for this case")
    try:
        _drive(ON_REQUEST, "the bounded-drain case")
        owed, acct = 0, {}
        for _ in range(36):
            acct = props(one(KIND["account"], ACCOUNT))
            owed = _pending_now(acct)
            if owed:
                break
            time.sleep(5)
        check(owed > 0,
              "no stream reported pending work with a one-millisecond drain "
              "budget: syncProgress is %r and the three statuses are %s. A "
              "fire that never hands back is the one that gets parked."
              % (acct.get("syncProgress"),
                 [acct.get(s + "SyncStatus") for s in
                  STREAMS]))
        state = str(acct.get("syncState") or "")
        check(state in ("ok", "throttled"),
              "the account says %r while it is merely bounded — a drain that "
              "stopped on its own budget is `ok` with work pending (or "
              "`throttled` behind a rate limit), never `erroring`" % state)
        parked = [t for t in _google_triggers() if int(t.get("parked") or 0)]
        check(not parked,
              "%d google trigger(s) hold parked deliveries after a bounded "
              "fire: %s — the whole point of the self-limit is that the chain "
              "stops itself before the engine has to"
              % (len(parked), [(t.get("id"), t.get("parked")) for t in parked]))
        note("bounded: %d unit(s) of work owed, syncState %s, nothing parked"
             % (owed, state))
    finally:
        api("/api/v1/" + KIND["config"] + "/" + cid, "PATCH",
            {"properties": {"drainBudgetMs": None}})
        note("config.drainBudgetMs cleared")

    # AND THE REMAINDER IS PICKED UP. A hand-back is only honest if the next
    # fire finishes the work, so the account is driven back to a whole mirror
    # before anything downstream reads it.
    for attempt in range(8):
        acct = props(one(KIND["account"], ACCOUNT))
        if not _pending_now(acct):
            break
        _drive(ON_REQUEST, "the resumption after the bound")
        wait_until_quiet(120)
    acct = props(one(KIND["account"], ACCOUNT))
    check(not _pending_now(acct),
          "the streams still owe work after eight resumed fires: %s — a "
          "hand-back that is never picked up is a half-synced mailbox"
          % [acct.get(s + "SyncStatus") for s in
             STREAMS])
    note("the remainder was picked up: %s"
         % ", ".join("%s %s" % (s, acct.get(s + "SyncStatus"))
                     for s in STREAMS))


def assert_sync_request_is_acknowledged():
    """T-084: ONE request, answered by every enabled stream before the
    account says it was served.

    The trait's request pair is how "Sync now" works: the owner stamps
    `syncRequestedAt`, each `google-<stream>-on-request` trigger fires while
    its own stream has not answered, and the account-level `syncRequestedAck`
    equals the stamp only once ALL THREE have. That last rule is the one worth
    asserting — a single ack spent by whichever stream ran first would tell
    the Connections page a request was served while two streams had not
    moved — and it is asserted on the same generation the per-stream acks
    carry, so the two halves are proven in step rather than separately.
    """
    step("a sync request is acknowledged by every enabled stream")
    if MODE != "e2e":
        note("SKIPPED: MODE is %r — a seed drives its own requests" % MODE)
        return
    stamped = _now_iso()
    api("/api/v1/" + KIND["account"] + "/" + ACCOUNT, "PATCH",
        {"properties": {"syncRequestedAt": stamped}})
    ran = 0
    for trigger in ON_REQUEST:
        try:
            answer = api("/api/v1/substrate.reamde.dev/core/trigger/%s/run"
                         % urllib.parse.quote(trigger, safe=""), method="POST",
                         body={"kind": KIND["account"], "id": ACCOUNT})
        except Failed:
            continue
        ran += int(answer.get("ran") or 0)
    check(ran > 0, "no on-request delivery ran for the acknowledgement case")

    def _same(value):
        # The stamp comes back as the engine stored it, which may spell the
        # zone or the sub-second digits differently; the SECOND is the
        # generation, and that is what the guards compare on.
        return str(value or "")[:19] == stamped[:19]

    acct, served = {}, False
    for _ in range(60):
        acct = props(one(KIND["account"], ACCOUNT))
        streams = _streams(acct)
        served = _same(acct.get("syncRequestedAck")) and all(
            _same((streams.get(s) or {}).get("requestedAck"))
            for s in STREAMS)
        if served:
            break
        time.sleep(5)
    streams = _streams(acct)
    for stream in STREAMS:
        check(_same(acct.get(stream + "SyncRequestedAck")),
              "%s answered %r, not the request %r that was stamped — the "
              "legacy per-stream ack is what its on-request guard reads, so a "
              "stale one re-fires the trigger against the same work for ever"
              % (stream, acct.get(stream + "SyncRequestedAck"), stamped))
        check(_same((streams.get(stream) or {}).get("requestedAck")),
              "syncStreams.%s.requestedAck is %r, not the request %r — the "
              "trait's half and the legacy ack must move together"
              % (stream, (streams.get(stream) or {}).get("requestedAck"),
                 stamped))
    check(_same(acct.get("syncRequestedAck")),
          "the account-level syncRequestedAck is %r after all three streams "
          "answered %r — the trait's account-level acknowledgement is what a "
          "console waits on" % (acct.get("syncRequestedAck"), stamped))
    note("one request %s answered by contacts, gmail and calendar, and only "
         "then by the account" % stamped[:19])
    wait_until_quiet(90)


def assert_permanent_failure_is_named():
    """T-023: a 401 is PERMANENT until the owner reconnects, and says so.

    The one class the fixed matrix cannot carry, because it must be injected
    BETWEEN two syncs: the account has to be `ok` first, so that what the
    status says afterwards is this failure and not the first sync's. 403 on a
    whole stream endpoint rides with it — same shape, different sentence.

    It runs LAST and puts the rules back, because everything after it would
    otherwise read a deliberately broken account.
    """
    step("a 401 is a reconnect, a 403 is a stream that is not available")
    if MODE != "e2e" or not MOCK_URL:
        note("SKIPPED: MODE is %r — failures are never injected into a seed"
             % MODE)
        return
    before = props(one(KIND["account"], ACCOUNT))
    check(str(before.get("contactsSyncStatus") or "").startswith("ok"),
          "the account is not `ok` before the permanent-failure case, so what "
          "it says afterwards would not be this failure's doing")

    mock("/__mock/faults", "POST", {"rules": [
        {"match": "GET /v1/people/me/connections*", "status": [401, 401, 401],
         "body": {"error": {"code": 401, "status": "UNAUTHENTICATED",
                            "message": "Invalid Credentials"}}},
        {"match": "GET /calendar/v3/users/me/calendarList*",
         "status": [403, 403, 403],
         "body": {"error": {"code": 403, "status": "PERMISSION_DENIED",
                            "message": "Insufficient Permission"}}}]})
    _drive(("google-contacts-on-request", "google-contacts-scheduled",
            "google-calendar-on-request", "google-calendar-scheduled"),
           "the permanent-failure case")

    got = {}
    for _ in range(48):
        acct = props(one(KIND["account"], ACCOUNT))
        got = {s: str(acct.get(s + "SyncStatus") or "")
               for s in ("contacts", "calendar")}
        if "reconnect needed" in got["contacts"] \
                and "not available" in got["calendar"]:
            break
        time.sleep(5)
    check("reconnect needed" in got.get("contacts", ""),
          "after a 401 the contacts status is %r — a 401 is PERMANENT until "
          "the owner reconnects and the status has to say so, because it is "
          "what the console shows and the only thing that tells a human what "
          "to do" % got.get("contacts"))
    check("not available" in got.get("calendar", ""),
          "after a 403 the calendar status is %r — a 403 is this grant being "
          "refused the stream, not an outage" % got.get("calendar"))
    note("401 -> %s" % got["contacts"][:72])
    note("403 -> %s" % got["calendar"][:72])

    # T-084: THE SAME FAILURE, IN THE TRAIT'S WORDS. A page that knows
    # nothing about Google must be able to say this account is broken and
    # why, which is `syncState: erroring` plus the two error fields — and the
    # per-stream entry, because it is the contacts and calendar streams that
    # failed while gmail is untouched.
    acct = props(one(KIND["account"], ACCOUNT))
    streams = _streams(acct)
    check(str(acct.get("syncState") or "") in ("erroring", "throttled"),
          "the account's syncState is %r after a 401 on contacts and a 403 on "
          "calendar — the trait's account-level state is raised by a failure, "
          "never left at the last success" % acct.get("syncState"))
    check(str(acct.get("syncError") or ""),
          "the account carries no syncError after two failing streams, so "
          "nothing but this bundle's own status string says what broke")
    check(acct.get("syncErrorAt"),
          "syncError is set and syncErrorAt is not — a failure with no "
          "instant cannot be ordered against the last success")
    for stream in ("contacts", "calendar"):
        entry = streams.get(stream) or {}
        check(str(entry.get("state") or "") in ("erroring", "throttled"),
              "syncStreams.%s.state is %r after its injected permanent "
              "failure" % (stream, entry.get("state")))
        check(str(entry.get("message") or ""),
              "syncStreams.%s carries no message for the failure" % stream)
    note("trait: syncState %s, syncError %s"
         % (acct.get("syncState"), str(acct.get("syncError"))[:60]))
    mock("/__mock/faults", "DELETE")
    note("the fault rules are cleared again")


def assert_disabled_stream_is_not_walked():
    """T-023: a stream the owner turned off is not walked, and says `disabled`.

    Asserted by the REQUEST LOG rather than by the absence of rows: rows from
    an earlier sync would still be there, so "no new rows" proves nothing. The
    only proof is that the endpoint was not called at all.
    """
    step("a disabled stream is not walked")
    if MODE != "e2e" or not MOCK_URL:
        note("SKIPPED: MODE is %r" % MODE)
        return
    api("/api/v1/" + KIND["account"] + "/" + ACCOUNT, "PATCH",
        {"properties": {"enabledGmail": False,
                        "syncRequestedAt": _now_iso()}})
    mock("/__mock/requests", "DELETE")
    ran = 0
    for trigger in ("google-gmail-on-request", "google-gmail-scheduled"):
        try:
            answer = api("/api/v1/substrate.reamde.dev/core/trigger/%s/run"
                         % urllib.parse.quote(trigger, safe=""), method="POST",
                         body={"kind": KIND["account"], "id": ACCOUNT})
        except Failed:
            continue
        ran += int(answer.get("ran") or 0)
    time.sleep(20)
    hits = [e for e in (mock("/__mock/requests") or {}).get("requests") or []
            if "/gmail/v1/" in str(e.get("path") or "")]
    check(not hits,
          "gmail is disabled and the drain still called %d gmail endpoints, "
          "e.g. %s — a toggle the owner turned off is not a slow sync, it is "
          "no sync" % (len(hits), [str(h.get("path"))[:60] for h in hits[:2]]))
    note("gmail disabled: %d deliveries ran, 0 gmail calls made" % ran)
    api("/api/v1/" + KIND["account"] + "/" + ACCOUNT, "PATCH",
        {"properties": {"enabledGmail": True}})


def assert_declared(expect, after):
    """The fixture-specific facts, when the recording set declares them."""
    if not expect:
        step("declared expectations")
        note("SKIPPED: this recording set ships no expectations.json — the "
             "updated-between-two-sync-tokens assertion is not exercised")
        return
    step("declared expectations")
    updated = expect.get("updatedEvent") or {}
    if updated.get("calendarId") and updated.get("eventId"):
        cref = calendar_id(updated["calendarId"])
        rid = updated.get("rowId") or event_id(cref, updated["eventId"])
        row = one(KIND["calendarevent"], rid)
        check(row is not None,
              "the declared updated event %s is not a row" % rid)
        want = updated.get("updated")
        check(not want or str(props(row).get("updated") or "").startswith(want[:19]),
              "calendarevent %s carries updated=%r, the fixture's second sync says %r"
              % (rid, props(row).get("updated"), want))
        note("the event updated between two sync tokens carries the newer stamp")
    for addr in expect.get("castAddresses") or []:
        row = one(KIND["emailaddress"], address_id(addr))
        check(row is not None,
              "the cast address %s has no emailaddress row (the shared persona "
              "is what the person mapping demo joins on)" % addr)
    if expect.get("castAddresses"):
        note("%d cast addresses are rows" % len(expect["castAddresses"]))


# --------------------------------------------------------------------------

def main():
    if not TOKEN:
        print("!! SUBSTRATE_TOKEN is unset", file=sys.stderr)
        return 2
    print("google %s: %s, account %s, recordings %s"
          % (MODE, SERVER, ACCOUNT, RECORDINGS))
    try:
        recs = recordings()
        if not any(n.startswith("GET_drive_v3_") for n in recs):
            global STREAMS, ON_REQUEST
            STREAMS = tuple(s for s in STREAMS if s != "drive")
            ON_REQUEST = tuple("google-%s-on-request" % s for s in STREAMS)
            note("this recording set carries NO Drive stream (a pull taken before "
                 "T-088) — the drive assertions are skipped and the account is "
                 "asserted over three streams")
        exp = Expected(recs)
        note("%d recordings read" % len(recs))
        account_props = assert_account()
        got = assert_counts(exp)
        if exp.orphans:
            note("%d event page recording(s) are ORPHANS — no live "
                 "`nextPageToken` chain reaches them, so the sync cannot ask "
                 "for them and they are not expected (an appended-to raw pull "
                 "keeps every earlier pull's pages)" % exp.orphans)
        if _FOREIGN:
            note("this repository also holds %s belonging to ANOTHER account; "
                 "every count, inventory and comparison below is scoped to %s"
                 % (", ".join("%d %s" % (n, k.rsplit("/", 1)[1])
                              for k, n in sorted(_FOREIGN.items())), ACCOUNT))
        assert_ids(exp, got)
        assert_relations(exp, got)
        assert_drive(exp, got, account_props)
        assert_temporal(got)
        assert_owner_identity(account_props, got)
        assert_recurrence(exp, got)
        assert_no_raw(got)
        assert_mime_fidelity(exp, got, recs)
        assert_threads_authoritative(got, recs)
        assert_fixtures_are_pseudonymous(recs)
        if SKIP_SECOND:
            note("E2E_SKIP_SECOND set: the idempotence half is skipped")
        else:
            versions = calendar_versions()
            stale = plant_stale_row(got)
            wake_and_wait()
            got = assert_idempotence(got, exp, planted=stale)
            assert_versions_held(versions, calendar_versions())
            assert_stale_row_survived(stale)
            assert_drive_delta(exp, got)
        assert_directory(exp, got, account_props_now())
        assert_containers(exp, got)
        assert_declared(expectations_file(), got)
        assert_moved_instance(expectations_file())
        assert_ordinal_rule(expectations_file())
        # LAST, and deliberately: it spends the account's `syncRequestedAt`
        # and restarts a stream's window, so anything asserting on the state
        # the SECOND sync left has to have run already.
        if not SKIP_SECOND:
            assert_restart_sweeps_a_stale_row(got)
        # THE FAILURE MATRIX IS LAST, and inside it the PERMANENT cases are
        # last again: they leave the account deliberately broken between two
        # syncs, so anything reading a healthy account has to have run first.
        # The trait's request pair, before the matrix breaks the account on
        # purpose: an acknowledgement is only legible while every stream can
        # still answer one.
        if not SKIP_SECOND:
            assert_drain_bounds_itself()
            assert_sync_request_is_acknowledged()
        assert_failure_matrix(got)
        if not SKIP_SECOND:
            assert_disabled_stream_is_not_walked()
            assert_permanent_failure_is_named()
    except Failed as e:
        print("\nFAILED: %s" % e, file=sys.stderr)
        return 1
    print("\ngoogle %s: PASS" % MODE)
    return 0


if __name__ == "__main__":
    sys.exit(main())
