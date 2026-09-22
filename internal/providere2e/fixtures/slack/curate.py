#!/usr/bin/env python3
"""Cut the two-day fixture cast out of raw/slack/.

`tools/pseudonymise.py` maps raw recordings to fixtures one for one, and the
owner's raw pull is the WHOLE workspace: 3,865 conversations, 501 users, 900+
history pages. A fixture set is meant to be a readable two days, so this picks
the cast first and the pseudonymiser runs over the cut.

    python3 providers/slack/fixtures/curate.py            # -> a staged dir
    python3 providers/slack/fixtures/curate.py --out /tmp/slack-cut

What it produces, under the SAME recording names `tools/mockserver.py` serves:

    auth.test, team.info                    the account's identity
    users.list                              TWO pages, split on a real cursor,
                                            holding exactly the cast
    conversations.list                      TWO pages, same, holding the cast
    conversations.history                   one page per cast conversation
    conversations.replies                   every threaded parent in the cast
    conversations.info, .members, users.info  where the raw pull has them
    conversations.history?oldest=<cursor>   ONE forward page per cast
                                            conversation: the request only an
                                            INCREMENTAL sync makes, carrying
                                            the message the second sync must
                                            land. Hand-cut from the real page,
                                            because a recording of it cannot
                                            be captured — the cursor it names
                                            did not exist when the pull ran.

THE CAST IS NOT IN THIS FILE. It is a list of the owner's real conversation
ids, and a real id beside "the private hiring channel" in a public repository
re-identifies the channel whatever the pseudonymiser did to the payload. It
lives in `cast.local.json` beside this script, which is gitignored:

    {"cast": [{"id": "<real conversation id>",
               "role": "public|private|mpim|im",
               "why": "what this one is here to prove"}, ...]}

`--survey` re-derives the candidates from `raw/` so the file can be rebuilt
by anyone holding the pull; `--write-cast` writes the top candidates out.

Every id, ts, cursor and count in the OUTPUT is real too, which is why the
output is never committed: `tools/pseudonymise.py` is what produces the
committable set, and `audit.py` is what proves it.
"""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "tools"))
from mockserver import recording_name  # noqa: E402

RAW = ROOT / "raw" / "slack"
CAST_PATH = pathlib.Path(__file__).resolve().parent / "cast.local.json"
WINDOW_DAYS = 2
# What the sync sends on every history and replies call. `include_all_metadata`
# rides in the recording's NAME, so the cut and the pull have to agree on it.
HISTORY_PARAMS = {"limit": "200", "include_all_metadata": "true"}


def load_cast() -> list[tuple[str, str]]:
    """The cast, by real Slack id, out of the gitignored file beside this
    script. See the module docstring for why it is not here."""
    if not CAST_PATH.exists():
        sys.exit(
            "no %s.\n"
            "The cast is a list of the owner's REAL conversation ids and does "
            "not live in the public tree.\n"
            "Run `python3 %s --survey` against raw/ and write the file, or "
            "`--write-cast` to have it written for you."
            % (CAST_PATH, pathlib.Path(__file__).name))
    doc = json.loads(CAST_PATH.read_text())
    out = []
    for row in doc.get("cast") or []:
        cid = row.get("id")
        if cid:
            out.append((cid, "%-7s %s" % (row.get("role") or "",
                                          row.get("why") or "")))
    if not out:
        sys.exit("%s holds no cast" % CAST_PATH)
    return out


MENTION_RE = re.compile(r"<@([UWB][A-Z0-9]+)(?:\|[^>]*)?>")
# `keep` decides what the cut COUNTS — whose users it collects, which
# parents it walks for replies. It used to skip twenty persisted subtypes,
# which matched a sync that also dropped them; both were wrong, and the
# history pages carried the messages anyway. Only the mutation envelopes are
# not messages.
SKIP_SUBTYPES = {"message_changed", "message_deleted", "message_replied"}


# ---- the two things the cut must do before the pseudonymiser runs ----------

SLACK_INFRA_HOSTS = {
    "slack.com", "api.slack.com", "files.slack.com", "slack-files.com",
    "slack-edge.com", "a.slack-edge.com", "ca.slack-edge.com",
    "avatars.slack-edge.com", "emoji.slack-edge.com", "secure.gravatar.com",
    "slack-imgs.com", "downloads.slack-edge.com",
}
HOST_RE = re.compile(r"https?://([A-Za-z0-9.-]+)")
FILE_URL_KEYS = ("permalink", "permalink_public", "url_private",
                 "url_private_download")
SEG_RE = re.compile(r"^(?P<stem>.*?)(?P<suffix>(?:_\d+)?)(?P<ext>\.[A-Za-z0-9]+)?$")


def neutralise_file_names(doc):
    """A file's NAME rides inside its own URLs, and the pseudonymiser cannot
    reach it there: `p.url` maps host and path TOKENS, and a filename like
    `20260914_candidate_feedback_<a real name>.pdf` is one token that no map
    knows. It is a real person's name in a fixture either way. So the curator
    renames every file after its own id before the payload is pseudonymised —
    the `name` and `title` fields are still replaced by the `title` rule, and
    the URLs now agree with an id rather than with a person.

    The platform fix is a path-token pass that splits on `_` and `-`; until it
    lands this is the cut's own business. See providers/slack/README.md."""
    n = 0
    for msg in doc.get("messages") or []:
        for f in msg.get("files") or []:
            fid = f.get("id")
            if not fid:
                continue
            stem = fid.lower()
            for key in list(f):
                v = f.get(key)
                if not (isinstance(v, str) and v.startswith("http")):
                    continue
                if key not in FILE_URL_KEYS and not key.startswith("thumb_"):
                    continue
                head, _, seg = v.rpartition("/")
                if not seg:
                    continue
                m = SEG_RE.match(seg)
                suffix, ext = m.group("suffix") or "", m.group("ext") or ""
                f[key] = "%s/%s%s%s" % (head, stem, suffix, ext)
                n += 1
            ext = ("." + f["filetype"]) if f.get("filetype") else ""
            if f.get("name"):
                f["name"] = stem + ext
            if f.get("title"):
                f["title"] = stem + ext
    return n


def teach_hosts(out):
    """Teach the pseudonym map every host in the cut that NAMES THE COMPANY.

    `Persona.scrub_host` maps a host only when its registrable domain is in
    the map, and a Slack workspace lives at `<workspace>.slack.com` — the
    registrable domain is Slack's, so nothing matches and the real workspace
    name rides out inside every permalink. The same goes for a company's SSO
    host. Teaching the learned map the whole host makes the ordinary `url`
    rule map it, consistently, everywhere it appears.

    ONLY a host whose leading label is a real identity the map already knows
    is taught. A public host (linkedin.com, platform.openai.com) is not an
    identity and is left exactly as it is, which is also what makes a fixture
    still read like the workspace it came from."""
    sys.path.insert(0, str(ROOT / "tools"))
    from persona import Persona, _slugify  # noqa: PLC0415

    def key(x):
        # Letters and digits only: `acme.io`, `acme-io` and
        # `acmeio` are one company written three ways.
        return re.sub(r"[^a-z0-9]", "", (x or "").lower())

    p = Persona.load()
    # The real words that mean "this company": every mapped domain's own stem,
    # every mapped company word, and the owner's own email domains.
    stems = {}
    for real, fake in (p.buckets.get("domain") or {}).items():
        stems[key(real)] = fake.split(".")[0]
        stems.setdefault(key(real.split(".")[0]), fake.split(".")[0])
    for real, fake in (p.buckets.get("company") or {}).items():
        stems.setdefault(key(real), _slugify(fake))

    hosts, taught = set(), {}
    for f in sorted(out.glob("GET_*.json")):
        for h in HOST_RE.findall(f.read_text()):
            h = h.lower().strip(".")
            if h and h not in SLACK_INFRA_HOSTS:
                hosts.add(h)
    for h in sorted(hosts):
        known = (p.buckets.get("domain", {}).get(h)
                 or p.learned.get("domain", {}).get(h))
        label, _, rest = h.partition(".")
        fake_label = stems.get(key(label))
        if not rest or not (known or fake_label):
            continue          # not this company's host: leave it alone
        fake = known or (fake_label + "." + rest)
        if not known:
            p._remember("domain", h, fake)
        # Reported whether it was learned now or in an earlier cut: the
        # rewrite below is what needs the list, and it must not depend on
        # which run first saw the host.
        taught[h] = fake
    rewrite_email_domains(out, p)
    p.flush()
    rewrite_hosts(out, taught)
    return taught


def rewrite_email_domains(out, p):
    """team.info's `email_domain` is a COMMA-JOINED LIST of domains, and no
    single-value rule can take it: the `domain` rule maps one domain, and the
    literal pass substitutes a company NAME — with the space in it — into a
    hostname. The cut splits the list and maps each domain through the map."""
    f = out / recording_name("GET", "/api/team.info", {})
    if not f.exists():
        return
    doc = json.loads(f.read_text())
    team = doc.get("team") or {}
    raw = team.get("email_domain")
    if not isinstance(raw, str) or not raw:
        return
    team["email_domain"] = ",".join(
        p.domain(d.strip()) for d in raw.split(",") if d.strip())
    f.write_text(json.dumps(doc, indent=2) + "\n")


def rewrite_hosts(out, taught):
    """Rewrite the taught hosts in the cut itself, and the workspace slug with
    them.

    Teaching the map is not enough for a WORKSPACE host: the pseudonymiser
    keeps `slack.com` and everything under it verbatim (`KEEP_HOSTS`, shared
    code, and right for `files.slack.com`), so `<workspace>.slack.com` is
    protected along with the CDN. The cut is the one place that knows the
    difference, so it does the substitution — with the same fake the map
    teaches, so every later pass agrees with it."""
    # ONLY a workspace host. Every other host the map can map itself, and a
    # blind substitution of an ordinary company domain would rewrite the
    # domain inside every EMAIL ADDRESS before the pseudonymiser reads it —
    # which leaves the local part (a real person's name) standing and makes
    # the address look like one of ours. That bug cost a re-cut; the rule is
    # narrow on purpose.
    # A host with a SUBDOMAIN only: `<workspace>.slack.com` hides under
    # KEEP_HOSTS, and `<company>.okta.com` loses its first label to a
    # registrable-domain match that fires before the exact one. A BARE
    # company domain is never rewritten here — it lives inside every email
    # address, and replacing it would leave the local part (a real name)
    # standing behind an address that then looks like one of ours.
    taught = {h: f for h, f in taught.items()
              if h.count(".") >= 2 and h not in SLACK_INFRA_HOSTS}
    if not taught:
        return
    for f in sorted(out.glob("GET_*.json")):
        text = f.read_text()
        before = text
        for real, fake in taught.items():
            text = text.replace(real, fake)
            # team.info's `domain` is the workspace SLUG, the same identity
            # without the suffix; it must not disagree with the host.
            label, _, rest = real.partition(".")
            if rest.endswith("slack.com"):
                text = text.replace('"domain": "%s"' % label,
                                    '"domain": "%s"' % fake.partition(".")[0])
        if text != before:
            f.write_text(text)


def load(name):
    f = RAW / name
    return json.loads(f.read_text()) if f.exists() else None


def every(pattern):
    for f in sorted(RAW.glob(pattern)):
        yield f, json.loads(f.read_text())


def keep(m):
    if m.get("type") not in (None, "message") or not m.get("ts"):
        return False
    return (m.get("subtype") or "") not in SKIP_SUBTYPES


def all_users():
    out = {}
    for _, d in every("GET_api_users.list__*"):
        for u in d.get("members") or []:
            if u.get("id"):
                out[u["id"]] = u
    return out


def all_convs():
    out = {}
    for _, d in every("GET_api_conversations.list__*"):
        for c in d.get("channels") or []:
            if c.get("id"):
                out[c["id"]] = c
    return out


def first_cursor(pattern, fallback):
    """A REAL next_cursor out of the raw pull, so the fixture's pagination is
    a value Slack actually minted rather than a made-up token."""
    for _, d in every(pattern):
        c = ((d.get("response_metadata") or {}).get("next_cursor") or "")
        if c:
            return c
    return fallback


def rank():
    """The two-day activity, richest first, with the features each
    conversation carries. `survey` prints it; `write_cast` picks from it."""
    convs, floor = all_convs(), time.time() - WINDOW_DAYS * 86400
    auth = load("GET_api_auth.test.json") or {}
    owner = auth.get("user_id") or ""
    rows, ranked_cids = [], set()
    # NEWEST FIRST, and one row per conversation. A pull that predates
    # `include_all_metadata` and one that carries it both leave a
    # `…_limit-200.json` page on disk, so the same conversation would be
    # ranked twice; the one WITH the parameter is the one the sync can
    # replay, so it wins.
    pages = sorted(
        every("GET_api_conversations.history__channel-*_limit-200.json"),
        key=lambda fd: ("include_all_metadata" not in fd[0].name, fd[0].name))
    for f, d in pages:
        cid = re.search(r"channel-([A-Z0-9]+)", f.name).group(1)
        if cid in ranked_cids:
            continue
        ranked_cids.add(cid)
        ms = [m for m in (d.get("messages") or []) if float(m.get("ts") or 0) >= floor]
        if not ms:
            continue
        c = convs.get(cid, {})
        feats = set()
        for m in ms:
            if m.get("user") == owner:
                feats.add("owner")
            if ("<@%s>" % owner) in (m.get("text") or ""):
                feats.add("MENTIONS-OWNER")
            if MENTION_RE.search(m.get("text") or ""):
                feats.add("mention")
            if m.get("thread_ts") == m.get("ts") and (m.get("reply_count") or 0):
                feats.add("thread")
            if m.get("bot_id") or m.get("subtype") == "bot_message":
                feats.add("bot")
            if m.get("files"):
                feats.add("file")
            if m.get("reactions"):
                feats.add("reaction")
        kind = ("im" if c.get("is_im") else "mpim" if c.get("is_mpim")
                else "private" if c.get("is_private") else "public")
        rows.append((len(ms), kind, cid, c.get("name") or "(dm)", sorted(feats)))
    rows.sort(reverse=True)
    return rows


def survey():
    for n, kind, cid, name, feats in rank()[:40]:
        print("%-8s %-13s %-34s %3d  %s" % (kind, cid, name[:34], n, ",".join(feats)))


def write_cast():
    """Write `cast.local.json` from the ranking: the richest conversation of
    each kind, plus a second public one and a second DM, which is the coverage
    `docs/providers-plan.md` asks for. Never writes into the public tree."""
    want = [("public", 2), ("private", 1), ("mpim", 1), ("im", 2)]
    rows, out = rank(), []
    for kind, n in want:
        for row in [r for r in rows if r[1] == kind][:n]:
            out.append({"id": row[2], "role": kind,
                        "why": ", ".join(row[4]) or "traffic"})
    CAST_PATH.write_text(json.dumps({"cast": out}, indent=2) + "\n")
    print("%d conversations -> %s (gitignored)" % (len(out), CAST_PATH))
    for row in out:
        print("    %-13s %-8s %s" % (row["id"], row["role"], row["why"]))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="/tmp/slack-fixture-cut")
    ap.add_argument("--write-cast", action="store_true",
                    help="write cast.local.json from the --survey ranking")
    ap.add_argument("--survey", action="store_true",
                    help="print the two-day activity the cast was chosen from")
    a = ap.parse_args()
    if a.survey:
        return survey()
    if a.write_cast:
        return write_cast()
    if not RAW.exists():
        sys.exit("no %s — run tools/rawpull/slack.py first" % RAW)

    out = pathlib.Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    for old in out.glob("GET_*.json"):
        old.unlink()

    # THE REQUEST MANIFEST. A recording's name is `recording_name(method,
    # path, params)`, and a long slug is TRUNCATED AND HASHED — so a
    # pseudonymiser that rewrites an id inside the visible part of such a
    # name produces a name the mock will never compute again. Worse, a
    # cursor and a `ts` inside a name are identities in their own right and
    # have to move with the body. Neither is recoverable from the name.
    # So the cut records what it asked for, the pseudonymiser maps the
    # PARAMETERS and recomputes the name. Gitignored: it holds real ids.
    manifest = {}

    def write(path, params, body):
        name = recording_name("GET", path, params)
        manifest[name] = {"method": "GET", "path": path, "params": params}
        (out / name).write_text(json.dumps(body, indent=2) + "\n")

    auth = load("GET_api_auth.test.json")
    team = load("GET_api_team.info.json")
    if not (auth and team):
        sys.exit("raw/slack is missing auth.test or team.info")
    write("/api/auth.test", {}, auth)
    write("/api/team.info", {}, team)

    convs, users = all_convs(), all_users()
    CAST = load_cast()
    cast = [cid for cid, _ in CAST if cid in convs]
    missing = [cid for cid, _ in CAST if cid not in convs]
    if missing:
        print("  (not in raw/: %s)" % ", ".join(missing))

    # The history pages, and everyone they name.
    seen_users = {auth.get("user_id") or ""}
    histories, threads = {}, []
    stale_metadata = []
    # T-021: every file and bot the cut NAMES, so the hydration recordings
    # the cut ships are exactly the ones the sync will ask for.
    cut_files, cut_bots = set(), set()
    # Which conversation's history and which thread get split across a real
    # cursor (below). The busiest history page is the one with messages to
    # spare; the first long-enough thread takes the replies split.
    ranked = [cid for _n, _k, cid, _nm, _f in rank() if cid in cast]
    paged_history_cid = ranked[0] if ranked else ""
    paged_replies, paged = "", []
    for cid in cast:
        # T-021: the sync asks for `include_all_metadata`, and the parameter
        # is part of the recording's NAME. A page pulled before the sync
        # asked for it answers a question nobody puts any more, so the cut
        # prefers the re-pulled recording and SAYS SO when it has to fall
        # back — a body captured without the parameter published under a name
        # that claims it is a lie about the data.
        d = load(recording_name("GET", "/api/conversations.history",
                                dict(HISTORY_PARAMS, channel=cid)))
        if not d:
            d = load(recording_name("GET", "/api/conversations.history",
                                    {"channel": cid, "limit": "200"}))
            if d:
                stale_metadata.append(cid)
        if not d:
            print("  (no history recording for %s)" % cid)
            continue
        # A raw history page is the newest 200 messages, whatever the window,
        # and it advertises the history BEHIND it. A curated page holds the
        # two days and nothing behind them, so it must say so: an honest
        # recording never promises a page the set does not hold.
        floor = time.time() - WINDOW_DAYS * 86400
        d = dict(d)
        d["messages"] = [m for m in (d.get("messages") or [])
                         if float(m.get("ts") or 0) >= floor]
        d["has_more"] = False
        d["response_metadata"] = {"next_cursor": ""}
        msgs = [m for m in d["messages"] if keep(m)]
        neutralise_file_names(d)
        histories[cid] = d
        for m in msgs:
            cut_files |= {f["id"] for f in (m.get("files") or [])
                          if isinstance(f, dict) and f.get("id")}
            cut_files |= {f for f in (m.get("x_files") or []) if f}
            if m.get("bot_id"):
                cut_bots.add(m["bot_id"])
            cut_bots |= {b for b in (m.get("reply_users") or [])
                         if isinstance(b, str) and b[:1] == "B"}
            for k in ("user", "parent_user_id"):
                if m.get(k):
                    seen_users.add(m[k])
            seen_users |= set(MENTION_RE.findall(m.get("text") or ""))
            for r in m.get("reactions") or []:
                seen_users |= set(r.get("users") or [])
            for f in m.get("files") or []:
                if f.get("user"):
                    seen_users.add(f["user"])
            if m.get("thread_ts") == m.get("ts") and (m.get("reply_count") or 0):
                threads.append((cid, m["ts"]))
        # T-021, A HISTORY PAGE THAT PAGES. The cut was one page per
        # conversation, so the cold walk's cursor branch — the one that
        # decides whether a 916-conversation backfill ever reaches the second
        # page — was never replayed. The busiest conversation's page is SPLIT
        # across a cursor Slack actually minted, newest half first, exactly
        # as the roster is split below: the set proves the union of two pages
        # rather than pretending the question does not arise.
        if cid == paged_history_cid and len(d.get("messages") or []) >= 4:
            hcur = first_cursor("GET_api_conversations.history__*",
                                "bmV4dF90czoxNzQ3NjgwMDAwMDAwMDAw")
            ms = list(d["messages"])
            half = max(1, len(ms) // 2)
            write("/api/conversations.history",
                  dict(HISTORY_PARAMS, channel=cid),
                  {"ok": True, "messages": ms[:half], "has_more": True,
                   "response_metadata": {"next_cursor": hcur}})
            write("/api/conversations.history",
                  dict(HISTORY_PARAMS, channel=cid, cursor=hcur),
                  {"ok": True, "messages": ms[half:], "has_more": False,
                   "response_metadata": {"next_cursor": ""}})
            paged.append("history %s over %d + %d messages"
                         % (cid, half, len(ms) - half))
            continue
        write("/api/conversations.history", dict(HISTORY_PARAMS, channel=cid), d)

    # The threaded replies, the per-conversation detail and the rosters —
    # whatever the raw pull reached.
    for cid, ts in threads:
        params = dict(HISTORY_PARAMS, channel=cid, ts=ts)
        d = load(recording_name("GET", "/api/conversations.replies", params))
        if not d:
            d = load(recording_name("GET", "/api/conversations.replies",
                                    {"channel": cid, "ts": ts, "limit": "200"}))
            if d:
                stale_metadata.append("%s/%s" % (cid, ts))
        if d:
            d = dict(d, has_more=False, response_metadata={"next_cursor": ""})
            neutralise_file_names(d)
            # T-021, A REPLIES PAGE THAT PAGES. The same gap one level down:
            # a thread longer than a page walks its own cursor, and no
            # recording exercised it. The FIRST thread with enough replies is
            # split; the parent stays on page one, because
            # conversations.replies answers with the parent first and the
            # sync mints it before anything references it.
            rms = list(d.get("messages") or [])
            if not paged_replies and len(rms) >= 4:
                rcur = first_cursor("GET_api_conversations.replies__*",
                                    "bmV4dF90czoxNzQ3NjgwMDAwMDAwMDAw")
                half = max(2, len(rms) // 2)
                write("/api/conversations.replies", params,
                      {"ok": True, "messages": rms[:half], "has_more": True,
                       "response_metadata": {"next_cursor": rcur}})
                write("/api/conversations.replies", dict(params, cursor=rcur),
                      {"ok": True, "messages": rms[half:], "has_more": False,
                       "response_metadata": {"next_cursor": ""}})
                paged_replies = "%s/%s over %d + %d messages" % (
                    cid, ts, half, len(rms) - half)
                paged.append("replies " + paged_replies)
            else:
                write("/api/conversations.replies", params, d)
            for m in d.get("messages") or []:
                if m.get("user"):
                    seen_users.add(m["user"])
                cut_files |= {f["id"] for f in (m.get("files") or [])
                              if isinstance(f, dict) and f.get("id")}
                cut_files |= {f for f in (m.get("x_files") or []) if f}
                if m.get("bot_id"):
                    cut_bots.add(m["bot_id"])
                seen_users |= set(MENTION_RE.findall(m.get("text") or ""))
    for cid in cast:
        d = load(recording_name("GET", "/api/conversations.info",
                                {"channel": cid}))
        if d:
            write("/api/conversations.info", {"channel": cid}, d)
        params = {"channel": cid, "limit": "200"}
        d = load(recording_name("GET", "/api/conversations.members", params))
        if not d:
            continue
        seen_users |= set(d.get("members") or [])
        cur = ((d.get("response_metadata") or {}).get("next_cursor") or "")
        nxt = load(recording_name("GET", "/api/conversations.members",
                                  dict(params, cursor=cur))) if cur else None
        if not cur:
            write("/api/conversations.members", params, d)
            continue
        # A ROSTER THAT PAGES. The raw pull caught the first page, its real
        # cursor and — for most channels — nothing behind it, so the cut used
        # to publish a page that PROMISES a second one and does not hold it:
        # the sync asks for it, the mock has no recording, and the roster is
        # dropped. A recording must never advertise a page the set cannot
        # serve. Where raw has the continuation it is used; where it does not,
        # the one page is SPLIT across the cursor Slack actually minted, so
        # the set proves the union of two pages rather than pretending the
        # question never arises.
        if nxt:
            seen_users |= set(nxt.get("members") or [])
            write("/api/conversations.members", params, d)
            write("/api/conversations.members", dict(params, cursor=cur), nxt)
            continue
        members = list(d.get("members") or [])
        half = max(1, len(members) // 2)
        write("/api/conversations.members", params,
              {"ok": True, "members": members[:half],
               "response_metadata": {"next_cursor": cur}})
        write("/api/conversations.members", dict(params, cursor=cur),
              {"ok": True, "members": members[half:],
               "response_metadata": {"next_cursor": ""}})
    for uid in sorted(u for u in seen_users if u):
        d = load(recording_name("GET", "/api/users.info", {"user": uid}))
        if d:
            write("/api/users.info", {"user": uid}, d)

    # users.list and conversations.list, each cut to the cast and split across
    # TWO pages on a cursor the real pull actually returned.
    members = [users[u] for u in sorted(seen_users) if u in users]
    ucur = first_cursor("GET_api_users.list__*", "dXNlcjpVMDYxTkZUVDI=")
    half = max(1, len(members) // 2)
    write("/api/users.list", {"limit": "200"},
          {"ok": True, "members": members[:half],
           "response_metadata": {"next_cursor": ucur}})
    write("/api/users.list", {"limit": "200", "cursor": ucur},
          {"ok": True, "members": members[half:],
           "response_metadata": {"next_cursor": ""}})

    channels = [convs[c] for c in cast]
    ccur = first_cursor("GET_api_conversations.list__*", "dGVhbTpDMDYxTkZUVDI=")
    fixed = {"limit": "200", "exclude_archived": "false",
             "types": "public_channel,private_channel,mpim,im"}
    chalf = max(1, len(channels) // 2)
    write("/api/conversations.list", fixed,
          {"ok": True, "channels": channels[:chalf],
           "response_metadata": {"next_cursor": ccur}})
    write("/api/conversations.list", dict(fixed, cursor=ccur),
          {"ok": True, "channels": channels[chalf:],
           "response_metadata": {"next_cursor": ""}})

    # THE INCREMENTAL PAGE. The second sync asks each conversation for
    # `oldest=<the ts the first sync stored>`; no pull can have recorded that
    # request, because the cursor did not exist yet. So it is cut here, from
    # the conversation's own newest message: the same payload one second
    # later, which is a message the first sync provably did not see.
    incremental, mutations = 0, 0
    for cid, d in histories.items():
        msgs = [m for m in (d.get("messages") or []) if keep(m)]
        if not msgs:
            continue
        newest = max(msgs, key=lambda m: float(m["ts"]))
        fresh = dict(newest)
        fresh["ts"] = "%.6f" % (float(newest["ts"]) + 1)
        fresh.pop("thread_ts", None)
        fresh.pop("reply_count", None)
        fresh.pop("latest_reply", None)
        fresh.pop("reactions", None)
        if fresh.get("client_msg_id"):
            fresh["client_msg_id"] = fresh["client_msg_id"][:-1] + "0"
        page = [fresh]
        # MUTATION ENVELOPES, in the one conversation that can spare three
        # ordinary messages. A mirror that never sees an edit or a delete is
        # a mirror that goes stale silently, and the whole set was `ok` and
        # append-only. Same seam as the appended message: hand-cut, because
        # the pull happened before any of it.
        spare = [m for m in msgs
                 if m["ts"] != newest["ts"] and not m.get("thread_ts")
                 and not m.get("reactions") and not m.get("files")]
        # NOT a threaded message: the thread watch re-reads every watched
        # thread on an incremental walk, and `conversations.replies` would
        # hand the reactions straight back — which is correct of the sync and
        # makes the fixture self-contradictory.
        reacted = [m for m in msgs
                   if m["ts"] != newest["ts"] and m.get("reactions")
                   and not m.get("thread_ts") and not m.get("reply_count")]
        if not mutations and len(spare) >= 2 and reacted:
            edited, removed, dereacted = spare[-1], spare[-2], reacted[-1]
            after = dict(edited)
            after["text"] = (edited.get("text") or "") + " (edited)"
            after["edited"] = {"user": edited.get("user") or "",
                               "ts": "%.6f" % (float(newest["ts"]) + 2)}
            page.append({"type": "message", "subtype": "message_changed",
                         "channel": cid, "hidden": True,
                         "ts": "%.6f" % (float(newest["ts"]) + 2),
                         "event_ts": "%.6f" % (float(newest["ts"]) + 2),
                         "message": after, "previous_message": dict(edited)})
            page.append({"type": "message", "subtype": "message_deleted",
                         "channel": cid, "hidden": True,
                         "ts": "%.6f" % (float(newest["ts"]) + 3),
                         "event_ts": "%.6f" % (float(newest["ts"]) + 3),
                         "deleted_ts": removed["ts"],
                         "previous_message": dict(removed)})
            # A REACTION REMOVED. The message comes back with an EMPTY
            # reactions array, which is Slack saying "there are none now" —
            # the mirror has to clear, and an emptiness the sync drops is a
            # reaction that stays on the row forever.
            cleared = dict(dereacted)
            cleared["reactions"] = []
            page.append(cleared)
            mutations = 1
        write("/api/conversations.history",
              dict(HISTORY_PARAMS, channel=cid, oldest=newest["ts"]),
              {"ok": True, "messages": page, "has_more": False,
               "response_metadata": {"next_cursor": ""}})
        # AND THE UNCHANGED PAGE. A third sync asks with the cursor the
        # second one stored — the newest ts the incremental page carried,
        # envelopes included — and there was no recording for it, so the mock
        # answered by DROPPING `oldest` and handing back the cold page. A
        # replay that falls back proves nothing about idempotence, and the
        # empty page is the honest answer: nothing has happened since.
        top = max(m["ts"] for m in page)
        write("/api/conversations.history",
              dict(HISTORY_PARAMS, channel=cid, oldest=top),
              {"ok": True, "messages": [], "has_more": False,
               "response_metadata": {"next_cursor": ""}})
        incremental += 1

    # ---- T-021: the HYDRATION recordings ---------------------------------
    # `files.info` for every file the cut's messages name and `bots.info` for
    # every bot they were posted under. Both are real responses — the pull
    # captures them (tools/rawpull/slack.py --only-hydrate) — and both are
    # the only source of facts no embed carries: where a file has been
    # SHARED, and the identity behind a bot_id that arrived without a
    # bot_profile.
    hydrated_files, hydrated_bots = [], []
    for fid in sorted(cut_files):
        d = load(recording_name("GET", "/api/files.info", {"file": fid}))
        if d:
            write("/api/files.info", {"file": fid}, d)
            hydrated_files.append(fid)
    for bid in sorted(cut_bots):
        d = load(recording_name("GET", "/api/bots.info", {"bot": bid}))
        if d:
            write("/api/bots.info", {"bot": bid}, d)
            hydrated_bots.append(bid)

    # ---- T-021: THE FAILURE MATRIX ---------------------------------------
    # The whole set was `ok: true`, so the rate-limit, the 5xx-retry and the
    # expired-cursor paths were exercised by reading the code. Each seam
    # below is a `__responses` envelope (tools/mockserver.py): the FIRST call
    # gets the failure, every later one gets the real answer, so the mirror
    # has to recover rather than the set simply being broken. Fabricated, and
    # labelled — the same contract the mutation envelopes above are under.
    seams = []
    # A 429 WITH Retry-After, on one conversation's HISTORY page. It goes on
    # history and not on `users.info` because the sync only ever hydrates a
    # user the published `users.list` does not carry, and this cut's list
    # carries every user it names — a seam nobody reaches is a path that
    # looks tested and is not. History is also the better test: the rate
    # limit lands mid-walk, so what the run proves is that the drain stops
    # POLITELY, stamps `retryNotBefore`, and RESUMES on the same conversation
    # rather than restarting the walk.
    rated = [c for c in cast if c in histories and c != paged_history_cid][-1:]
    for cid in rated:
        real = histories[cid]
        write("/api/conversations.history", dict(HISTORY_PARAMS, channel=cid),
              {"__responses": [
                  {"__response": {"status": 429,
                                  "headers": {"Retry-After": "1"}},
                   "body": {"ok": False, "error": "ratelimited"}},
                  real]})
        seams.append("429 + Retry-After on one conversations.history page")
    # A 5xx, on one conversations.info. A transient failure is KEPT and
    # retried at the top of the next walk, so the fields only that call
    # carries land on the second sync. The conversation is chosen so its
    # `latest` is a message the history page holds anyway — a seam must test
    # the retry, not remove a message from the cut.
    for cid in cast:
        d = load(recording_name("GET", "/api/conversations.info",
                                {"channel": cid}))
        c = (d or {}).get("channel") or {}
        latest = c.get("latest") or {}
        page = histories.get(cid) or {}
        holds = {m.get("ts") for m in (page.get("messages") or [])}
        if not d or (latest.get("ts") and latest["ts"] not in holds):
            continue
        write("/api/conversations.info", {"channel": cid}, {"__responses": [
            {"__response": {"status": 500},
             "body": {"ok": False, "error": "internal_error"}},
            d]})
        seams.append("500 on conversations.info for one conversation")
        break
    # An EXPIRED CURSOR, on the second page of one roster. Slack has already
    # answered, so the sync must record it as PERMANENT and move on rather
    # than retrying a token that can never work again — and the next WALK
    # re-reads the roster from page one, where the real second page is
    # waiting.
    for name in sorted(out.glob("GET_api_conversations.members__*cursor-*")):
        d = json.loads(name.read_text())
        if not d.get("ok"):
            continue
        entry = manifest.get(name.name)
        if not entry:
            continue
        write(entry["path"], entry["params"], {"__responses": [
            {"ok": False, "error": "invalid_cursor"}, d]})
        seams.append("invalid_cursor on a roster's second page")
        break

    (out / "_requests.local.json").write_text(
        json.dumps(manifest, indent=1, sort_keys=True) + "\n")
    taught = teach_hosts(out)
    for real, fake in sorted(taught.items()):
        print("  host taught: %s -> %s" % (real, fake))
    n = len(list(out.glob("GET_*.json")))
    print("%d recordings -> %s" % (n, out))
    print("  %d conversations, %d users, %d threads, %d incremental pages"
          % (len(cast), len(members), len(threads), incremental))
    print("  %d files.info, %d bots.info (of %d files and %d bots the cut names)"
          % (len(hydrated_files), len(hydrated_bots),
             len(cut_files), len(cut_bots)))
    if len(hydrated_files) < len(cut_files) or len(hydrated_bots) < len(cut_bots):
        print("  !! a file or bot the cut names has NO hydration recording — "
              "run `python3 tools/rawpull/slack.py --only-hydrate`")
    for split in paged:
        print("  paged across a real cursor: %s" % split)
    for seam in seams:
        print("  failure seam (fabricated, labelled in fixtures/README.md): %s"
              % seam)
    if stale_metadata:
        print("  !! %d history/replies recordings fell back to a page pulled "
              "WITHOUT include_all_metadata: %s. Re-run "
              "`python3 tools/rawpull/slack.py` and cut again."
              % (len(stale_metadata), ", ".join(stale_metadata[:4])))
    for cid, why in CAST:
        print("    %-13s %s" % (cid, why))
    print("  (the ids above are REAL; %s is gitignored and so is this "
          "output directory's content until the pseudonymiser runs)"
          % CAST_PATH.name)


if __name__ == "__main__":
    raise SystemExit(main())
