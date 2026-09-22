#!/usr/bin/env python3
"""Cut the fixture cast out of raw/beeper/.

`tools/pseudonymise.py` maps raw recordings to fixtures one for one, and the
owner's raw pull is the WHOLE inbox: 476 chats across six bridged networks,
900+ contacts, 17 chats' worth of history. A fixture set is meant to be a
readable handful, so this picks the cast first and the pseudonymiser runs over
the cut.

    python3 providers/beeper/fixtures/curate.py            # -> a staged dir
    python3 providers/beeper/fixtures/curate.py --out /tmp/beeper-cut
    python3 providers/beeper/fixtures/curate.py --survey   # what raw/ holds

What it produces, under the SAME recording names `tools/mockserver.py` serves
and `providers/beeper/src/functions/messagessync.py` asks for:

    /v1/info, /v1/accounts, /v1/bridges, /v1/labels  copied whole: they are
                                                     the connection's own
                                                     identity and they are
                                                     small
    /v1/accounts/{id}/contacts/list                  one page per bridge,
                                                     TRIMMED to the cast's own
                                                     people plus a few
    /v1/chats                                        TWO pages, split on a
                                                     REAL cursor, holding
                                                     exactly the cast
    /v1/chats/{id}?maxParticipantCount=-1            the authoritative read,
                                                     per cast chat
    /v1/chats/{id}/messages                          two pages per cast chat,
                                                     the second terminal
    /v1/chats/{id}/messages?cursor=…&direction=after the request only an
                                                     INCREMENTAL sync makes

THREE CURATED SEAMS, and they are seams because a recording of them cannot be
captured (`docs/provider-practices.md` §1 — a raw pull carries no fabricated
seams, so the ones a test needs are cut HERE and named):

  1. **the terminal page.** A real history page says `hasMore: true` forever;
     the cut's last page per chat says `false`, so the backfill ends inside the
     fixture set instead of asking for a page nobody recorded.
  2. **the forward page.** `direction=after` from a stored cursor is a request
     that did not exist when the pull ran, so its recording is cut from page
     one: the NEWEST message of each chat is moved out of the backfill and
     into a forward page keyed on the cursor the first page REALLY returned,
     and the chat list's `preview` moves back to the message behind it —
     which is what the preview WAS at the moment that list was read. No cursor
     is invented: the page's own are opaque values Beeper produced, and the
     forward page hands back the one it was given, so a third round asks the
     same question and stops. That makes the e2e's incremental assertion real:
     a message the first sync cannot have seen lands on the second.
  3. **the merged chat.** Beeper hides a merged chat's MEMBER chats from
     `/v1/chats`, so the pull holds no read of them and none of the merged
     chat itself. Its list entry carries a complete roster (`hasMore: false`),
     so the authoritative read is that entry with `preview` removed — a trim.
     Its history page is EMPTY, which is what the schema says that route
     returns ("a merged chat holds no messages of its own"); the preview rides
     on the list entry and is mirrored from there.

THE CAST IS NOT IN THIS FILE. It is a list of the owner's real chat ids, and a
real room id beside "the family group" in a public repository re-identifies
the chat whatever the pseudonymiser did to the payload. It lives in
`cast.local.json` beside this script, which is gitignored:

    {"cast": [{"id": "<real chat id>",
               "role": "dm|group|merged",
               "why": "what this one is here to prove"}, ...]}

`--survey` re-derives the candidates from `raw/` so the file can be rebuilt by
anyone holding the pull; `--write-cast` writes the top candidates out.

Every id, instant, cursor and body in the OUTPUT is real, which is why the
output is never committed: `tools/pseudonymise.py` is what produces the
committable set, and `audit.py` is what proves it.
"""

from __future__ import annotations

import argparse
import collections
import json
import pathlib
import sys
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "tools"))
from mockserver import recording_name  # noqa: E402

RAW = ROOT / "raw" / "beeper"
HERE = pathlib.Path(__file__).resolve().parent
CAST_PATH = HERE / "cast.local.json"

# What the SYNC sends. The recording's name is the query string, so the cut
# and the bundle have to agree on every one of these byte for byte.
CHAT_PAGE = 200
CONTACT_PAGE = 200
MESSAGE_PAGE = 20
WHOLE_PARAMS = {"maxParticipantCount": -1}
# How many contacts per bridge survive the cut. The owner's WhatsApp address
# book alone is 200 entries of real people; the cast's own participants plus a
# few strangers is what the mirror has to prove.
CONTACT_KEEP = 12


# ---------------------------------------------------------------- the cast

def load_cast() -> list[dict]:
    if not CAST_PATH.exists():
        sys.exit(
            "no %s.\n"
            "The cast is a list of the owner's REAL chat ids and does not live "
            "in the public tree.\n"
            "Run `python3 %s --survey` against raw/ and write the file, or "
            "`--write-cast` to have it written for you."
            % (CAST_PATH, pathlib.Path(__file__).name))
    doc = json.loads(CAST_PATH.read_text())
    rows = [r for r in (doc.get("cast") or []) if r.get("id")]
    if not rows:
        sys.exit("%s holds no cast" % CAST_PATH)
    return rows


# ---------------------------------------------------------------- reading

def load(name):
    f = RAW / name
    return json.loads(f.read_text()) if f.exists() else None


def chat_list_pages():
    """Every recorded page of /v1/chats, in the order the pull walked them."""
    first = load(recording_name("GET", "/v1/chats", {"limit": CHAT_PAGE}))
    pages = []
    if first:
        pages.append((None, first))
    cursor = (first or {}).get("oldestCursor")
    seen = set()
    while cursor and cursor not in seen:
        seen.add(cursor)
        page = load(recording_name("GET", "/v1/chats",
                                   {"limit": CHAT_PAGE, "cursor": cursor,
                                    "direction": "before"}))
        if not page:
            break
        pages.append((cursor, page))
        cursor = page.get("oldestCursor")
    return pages


def all_chats():
    out = {}
    for _cursor, page in chat_list_pages():
        for c in page.get("items") or []:
            out[c["id"]] = c
    return out


def message_pages(cid):
    """Every recorded page of one chat's history, oldest walk order."""
    q = urllib.parse.quote(cid, safe="")
    path = "/v1/chats/%s/messages" % q
    pages, cursor, seen = [], None, set()
    while True:
        params = {"limit": MESSAGE_PAGE}
        if cursor:
            params["cursor"] = cursor
            params["direction"] = "before"
        page = load(recording_name("GET", path, params))
        if not page:
            break
        pages.append((cursor, page))
        cursor = page.get("oldestCursor")
        if not cursor or cursor in seen or not page.get("hasMore"):
            break
        seen.add(cursor)
    return pages


def whole_read(cid):
    q = urllib.parse.quote(cid, safe="")
    return load(recording_name("GET", "/v1/chats/%s" % q, dict(WHOLE_PARAMS)))


# ---------------------------------------------------------------- the cut

class Cut:
    def __init__(self, out: pathlib.Path):
        self.out = out
        self.written = 0

    def write(self, method, path, params, payload):
        self.out.mkdir(parents=True, exist_ok=True)
        name = recording_name(method, path, params)
        (self.out / name).write_text(
            json.dumps(payload, indent=1, ensure_ascii=False))
        self.written += 1
        return name


def people_of(chat, messages):
    """Every Beeper user id the cast names, so the contact-list trim keeps the
    people the mirror will actually reference."""
    ids = set()
    for p in ((chat.get("participants") or {}).get("items") or []):
        if p.get("id"):
            ids.add(p["id"])
    for m in messages:
        for key in ("senderID",):
            if m.get(key):
                ids.add(m[key])
        for r in m.get("reactions") or []:
            if r.get("participantID"):
                ids.add(r["participantID"])
        for uid in m.get("mentions") or []:
            if uid and uid != "@room":
                ids.add(uid)
    return ids


def cut(out: pathlib.Path, cast: list[dict]) -> dict:
    cut = Cut(out)
    tally = collections.Counter()
    chats = all_chats()
    missing = [r["id"] for r in cast if r["id"] not in chats]
    if missing:
        sys.exit("cast chats absent from the recorded chat list: %d "
                 "(run --survey)" % len(missing))

    # 1. the connection's own identity, copied whole.
    for path in ("/v1/info", "/v1/accounts", "/v1/bridges", "/v1/labels"):
        payload = load(recording_name("GET", path))
        if payload is None:
            sys.exit("raw/beeper holds no recording of %s — re-run "
                     "tools/rawpull/beeper.py" % path)
        cut.write("GET", path, None, payload)
        tally["identity"] += 1

    # 2. per cast chat: the authoritative read, the history, and the seams.
    people = set()
    list_items = {}
    for row in cast:
        cid = row["id"]
        listed = dict(chats[cid])
        whole = whole_read(cid)
        pages = message_pages(cid)
        q = urllib.parse.quote(cid, safe="")
        mpath = "/v1/chats/%s/messages" % q

        if whole is None:
            # SEAM 3: a merged chat. Beeper hides its member chats from the
            # list, so nothing read it singly — but its list entry carries a
            # COMPLETE roster, so the authoritative read is that entry with
            # `preview` removed. A trim, not an invention.
            if (listed.get("participants") or {}).get("hasMore"):
                sys.exit("no whole read for %s and its listed roster is "
                         "truncated — cannot synthesise it" % cid[:12])
            whole = {k: v for k, v in listed.items() if k != "preview"}
        if not pages:
            # SEAM 3, and it is an EMPTY page rather than an invented one: the
            # schema says a merged chat "holds no messages of its own — read
            # messages from the member chats", so an empty history is what the
            # API's own contract says this route returns. Its `preview` still
            # rides on the list entry and is mirrored from there.
            pages = [(None, {"items": [], "hasMore": False,
                             "oldestCursor": None, "newestCursor": None})]
        cut.write("GET", "/v1/chats/%s" % q, dict(WHOLE_PARAMS), whole)
        tally["whole"] += 1

        pages = [(c, dict(p)) for c, p in pages[:2]]
        first = pages[0][1]
        items = [dict(m) for m in (first.get("items") or [])]

        # SEAM 2: the forward page. The newest message moves out of the
        # backfill, so a second sync is the only way it can arrive.
        forward = None
        if len(items) >= 2:
            forward = items.pop(0)
        first["items"] = items
        # THE PAGE'S CURSORS ARE LEFT ALONE. They are Beeper's, documented as
        # opaque, and the forward recording below is named with the REAL one
        # the first page returned — so nothing in this cut invents a cursor,
        # and the sync's `direction=after` from its stored cursor is answered
        # by a file it would genuinely have asked for.
        pages[0] = (pages[0][0], first)

        # SEAM 1: the terminal page. The cut's last page ends the backfill
        # inside the fixture set.
        last_cursor, last = pages[-1]
        last = dict(last)
        last["hasMore"] = False
        pages[-1] = (last_cursor, last)

        for cursor, page in pages:
            params = {"limit": MESSAGE_PAGE}
            if cursor:
                params["cursor"] = cursor
                params["direction"] = "before"
            cut.write("GET", mpath, params, page)
            tally["history"] += 1
            people |= people_of(whole, page.get("items") or [])

        newest = first.get("newestCursor")
        if forward and newest:
            # The forward page is keyed on the cursor the first page REALLY
            # returned, and hands back the cursor it was given — so a third
            # round asks the same question, gets the same answer, writes the
            # same row and stops. No cursor in this set is invented.
            cut.write("GET", mpath,
                      {"limit": MESSAGE_PAGE, "cursor": newest,
                       "direction": "after"},
                      {"items": [forward], "hasMore": False,
                       "oldestCursor": newest, "newestCursor": newest})
            tally["forward"] += 1
            people |= people_of(whole, [forward])
            # The list's `preview` is what was newest WHEN THE LIST WAS READ,
            # so it moves back with the page's cursor. Leaving it would hand
            # the first sync the very message the second has to discover.
            if items:
                listed["preview"] = dict(items[0])
        elif newest:
            cut.write("GET", mpath,
                      {"limit": MESSAGE_PAGE, "cursor": newest,
                       "direction": "after"},
                      {"items": [], "hasMore": False,
                       "oldestCursor": None, "newestCursor": newest})
            tally["forward"] += 1
        list_items[cid] = listed

    # 3. the chat list: two pages, split on the REAL cursor page two was
    #    recorded under, so list pagination is exercised by the cut.
    pages = chat_list_pages()
    split_cursor = (pages[0][1].get("oldestCursor") if pages else None)
    ordered = [list_items[r["id"]] for r in cast]
    half = max(1, len(ordered) // 2)
    page_one = {"items": ordered[:half], "hasMore": True,
                "oldestCursor": split_cursor,
                "newestCursor": pages[0][1].get("newestCursor") if pages else None}
    page_two = {"items": ordered[half:], "hasMore": False,
                "oldestCursor": None,
                "newestCursor": pages[1][1].get("newestCursor")
                if len(pages) > 1 else None}
    cut.write("GET", "/v1/chats", {"limit": CHAT_PAGE}, page_one)
    cut.write("GET", "/v1/chats",
              {"limit": CHAT_PAGE, "cursor": split_cursor,
               "direction": "before"}, page_two)
    tally["chatpages"] += 2

    # 4. the address books, trimmed to the cast's own people plus a few.
    accounts = load(recording_name("GET", "/v1/accounts")) or []
    for a in accounts if isinstance(accounts, list) else []:
        caid = a.get("accountID")
        if not caid:
            continue
        path = "/v1/accounts/%s/contacts/list" % urllib.parse.quote(caid, safe="")
        page = load(recording_name("GET", path, {"limit": CONTACT_PAGE}))
        if page is None:
            continue
        items = page.get("items") or []
        keep = [u for u in items if u.get("id") in people]
        rest = [u for u in items if u.get("id") not in people]
        keep = keep + rest[:max(0, CONTACT_KEEP - len(keep))]
        cut.write("GET", path, {"limit": CONTACT_PAGE},
                  {"items": keep, "hasMore": False,
                   "oldestCursor": None,
                   "newestCursor": page.get("newestCursor")})
        tally["contacts"] += 1
    return dict(tally, people=len(people))


# ---------------------------------------------------------------- surveying

def survey():
    """What raw/ can prove, so a cast can be chosen from evidence rather than
    from a hunch. Prints REAL ids: for the terminal, never for a file."""
    chats = all_chats()
    rows = []
    for cid, c in chats.items():
        if whole_read(cid) is None:
            continue
        pages = message_pages(cid)
        msgs = [m for _c, p in pages for m in (p.get("items") or [])]
        feats = []
        if any(m.get("attachments") for m in msgs):
            feats.append("attachments")
        if any(a.get("transcription") for m in msgs
               for a in (m.get("attachments") or [])):
            feats.append("transcription")
        if any(m.get("reactions") for m in msgs):
            feats.append("reactions")
        if any(m.get("mentions") for m in msgs):
            feats.append("mentions")
        if any(m.get("links") for m in msgs):
            feats.append("links")
        if any(m.get("linkedMessageID") for m in msgs):
            feats.append("reply")
        if any(m.get("editedTimestamp") for m in msgs):
            feats.append("edited")
        if any(m.get("isDeleted") for m in msgs):
            feats.append("deleted")
        if any(m.get("sendStatus") for m in msgs):
            feats.append("sendStatus")
        if any(m.get("type") is None for m in msgs):
            feats.append("no-type")
        if any(isinstance(m.get("seen"), dict) for m in msgs):
            feats.append("seenMap")
        if (c.get("participants") or {}).get("hasMore"):
            feats.append("rosterTruncated")
        if c.get("merge") or c.get("mergedIntoChatID"):
            feats.append("merge")
        rows.append((cid, c.get("accountID"), c.get("type"),
                     (c.get("participants") or {}).get("total"),
                     len(msgs), len(pages), feats))
    rows.sort(key=lambda r: -len(r[6]))
    for r in rows:
        print("%-38s %-11s %-6s p=%-4s m=%-3d pages=%d %s"
              % (r[0][:38], r[1], r[2], r[3], r[4], r[5], ",".join(r[6])))
    merged = [cid for cid, c in chats.items() if c.get("merge")]
    print("\n%d chats listed, %d with a whole read, %d merged"
          % (len(chats), len(rows), len(merged)))
    for cid in merged:
        print("  merged: %s" % cid)


def write_cast():
    """A starting cast, from the survey's own evidence. The file is
    gitignored; edit the `why` lines by hand afterwards."""
    chats = all_chats()
    picks, seen_networks = [], set()
    for cid, c in sorted(chats.items(),
                         key=lambda kv: -((kv[1].get("participants") or {})
                                          .get("total") or 0)):
        if whole_read(cid) is None or not message_pages(cid):
            continue
        net = c.get("accountID")
        if net in seen_networks and len(picks) >= 4:
            continue
        seen_networks.add(net)
        picks.append({"id": cid,
                      "role": "dm" if c.get("type") == "single" else "group",
                      "why": "TODO: say what this one proves"})
        if len(picks) >= 6:
            break
    for cid, c in chats.items():
        if c.get("merge"):
            picks.append({"id": cid, "role": "merged",
                          "why": "merge.chats and merge.defaultChat"})
            break
    CAST_PATH.write_text(json.dumps({"cast": picks}, indent=1))
    print("wrote %s with %d chats — edit the `why` lines"
          % (CAST_PATH, len(picks)))


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--out", default="/tmp/beeper-cut")
    ap.add_argument("--survey", action="store_true")
    ap.add_argument("--write-cast", action="store_true")
    args = ap.parse_args()
    if not RAW.exists():
        sys.exit("no %s — run tools/rawpull/beeper.py first" % RAW)
    if args.survey:
        return survey()
    if args.write_cast:
        return write_cast()
    out = pathlib.Path(args.out)
    tally = cut(out, load_cast())
    print("cut -> %s" % out)
    for k, v in sorted(tally.items()):
        print("  %-12s %s" % (k, v))
    print("\nnext:\n"
          "  python3 tools/pseudonymise.py %s providers/beeper/fixtures\n"
          "  python3 tools/rawpull/beeper.py --repair providers/beeper/fixtures\n"
          "  python3 providers/beeper/fixtures/audit.py" % out)


if __name__ == "__main__":
    main()
