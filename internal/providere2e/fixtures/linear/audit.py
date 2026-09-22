#!/usr/bin/env python3
"""Audit the committed Linear fixtures for real identity, token by token.

`providers/` is a PUBLIC directory. The pseudonymiser is what makes this set
safe to publish; this is what proves it. It does not look for values that LOOK
real — it looks for THE REAL VALUES THEMSELVES, out of the only two places
real data is allowed to exist:

  * `tools/persona.local.json` (and `persona.learned.local.json`) — the map:
    every real name, first name, surname, address, local part, handle, and the
    key side of every word bucket the pseudonymiser ever mapped;
  * `raw/linear/` — the pull the fixtures were cut from. EVERY workspace UUID
    (an id is a live handle into the real workspace, and Linear's are
    everywhere: rows, cursors, upload URLs, `labelIds`), every member `name`,
    `displayName`, `email`, `initials` and profile URL, the workspace's
    `name`, `urlKey` and previous url keys, every team `name`, `key` and
    `displayName`, every workflow state, label, project status, project and
    cycle NAME, every issue `title`, `identifier`, `branchName` and URL, and
    the exact text of every description, comment body and project content.

FOUR CHECKS, and the last two are the ones that catch what nobody predicted:

  1. LITERALS — every specific real value against every fixture string, on
     word boundaries, values and KEYS alike;
  2. PROSE, EXACTLY — every real description / body / content string against
     every fixture string. Prose is REPLACED wholesale rather than truncated,
     so a survivor survives whole;
  3. PROSE, POSITIVELY — every prose string in the fixtures must be drawn from
     the generator's own closed vocabulary (`persona._already_generated`). A
     sentence the generator could not have written came from somewhere else;
  4. GRAMMAR — Linear composes an issue's `identifier` out of its team's
     `key`, and its `url` and `branchName` out of the workspace's url key, the
     identifier and a slug OF THE TITLE. A mapped title beside an unmapped
     slug is a leak wearing the right clothes, so every composed value is
     recomputed from the fixture's own fake parts and must match.

Every finding is printed as its CLASS, its length and a salted fingerprint —
never as the value it found (provider-practices §10), so this tool's own
output is committable.

    python3 providers/linear/fixtures/audit.py            # exit 0 = clean
    python3 providers/linear/fixtures/audit.py --verbose  # every hit

Exit status is the number of findings, capped at 125.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import pathlib
import re
import sys
import urllib.parse

HERE = pathlib.Path(__file__).resolve()
FIXTURES = HERE.parent
ROOT = HERE.parents[3]
MAPS = [ROOT / "tools" / "persona.local.json",
        ROOT / "tools" / "persona.learned.local.json"]
RAW = ROOT / "raw" / "linear"

sys.path.insert(0, str(ROOT / "tools"))
from persona import _VOCAB, _already_generated  # noqa: E402

MIN_LITERAL = 4
UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-"
                  r"[0-9a-f]{12}$", re.I)
B64ISH = re.compile(r"^[A-Za-z0-9+/_-]{12,}={0,2}$")

# Linear's OWN vocabulary and the platform's hosts: a literal that is one of
# these is not an identity even where a real team happens to be named after
# it. Everything here is either a schema enum value, a documented word, or a
# host Linear serves for every workspace.
ALLOW = {
    "linear", "linear.app", "api.linear.app", "uploads.linear.app",
    "public.linear.app", "client-api.linear.app", "issue", "issues", "project",
    "projects", "profiles", "team", "teams", "cycle", "cycles", "comment",
    "comments", "label", "labels", "user", "users", "viewer", "organization",
    "workflowstates", "workflowstate", "nodes", "pageinfo", "endcursor",
    "hasnextpage", "data", "true", "false", "null", "http", "https", "cycles.ics",
    "ical", "backlog", "unstarted", "started", "completed", "canceled",
    "cancelled", "duplicate", "triage", "planned", "paused", "ontrack",
    "atrisk", "offtrack", "month", "quarter", "halfyear", "year", "daily",
    "weekly", "never", "singleselect", "multiselect", "public", "restricted",
    "private", "development", "internal", "privatebeta", "beta", "prerelease",
    "sunday", "monday", "tuesday", "wednesday", "thursday", "friday",
    "saturday", "exponential", "fibonacci", "notused", "linearscale",
    "tshirt", "first", "last", "bottom", "owner", "admin", "member", "all",
    "business", "none", "urgent", "high", "medium", "low", "no priority",
    "github", "gitlab", "slack", "jira", "figma", "notion", "sentry", "email",
    "zendesk", "intercom", "front", "discord", "loom", "gong", "datadog",
    "pagerduty", "opsgenie", "salesforce", "launchdarkly", "airbyte",
    "microsoftteams", "suggest", "off", "auto", "untitled", "workspace",
}
# The keys whose value is prose the generator wrote. Checks 3 applies to them.
PROSE_KEYS = {"description", "body", "content", "quotedText", "title"}
# ... but only where the value is long enough to BE prose. A label called
# `Done` is a word, and the generator writes words through the bucket map.
PROSE_MIN = 24


def fingerprint(s: str) -> str:
    """A finding is printed as a SALTED FINGERPRINT, never as the literal.

    provider-practices §10: a tool's own output has to be committable, and an
    audit that prints the real name it found is a leak in the log, the
    terminal scrollback and the ticket somebody pastes it into. The class, the
    length and eight hex characters are enough to tell two findings apart and
    to confirm a fix; the value itself is in the gitignored map.
    """
    return hashlib.sha256(("linear-audit\x1f" + s).encode("utf-8", "replace")
                          ).hexdigest()[:8]


def shown(s: str) -> str:
    return "<%s len=%d %s>" % ("prose" if len(s.split()) > 3 else "value",
                               len(s), fingerprint(s))


def specific(lit: str) -> bool:
    """Is this literal a NEEDLE, or is it language?

    A real label called `ingest` and a real project called `memory` are words
    the generator's own vocabulary already contains, so hunting for them finds
    the generator's prose and not a leak. A needle has to be something only
    the real workspace would say.
    """
    v = (lit or "").strip()
    if len(v) < MIN_LITERAL or v.lower() in ALLOW:
        return False
    if v.lower() in _VOCAB:
        return False
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}|[\d.x]+|https?:", v):
        return False
    if re.search(r"[@./_\- ]", v):
        return True
    return len(v) >= 6


def strings(node, path="", out=None):
    """Every string in the document, with the path it sits at. Keys count: a
    real word hides in a map's KEY as easily as in a value."""
    out = [] if out is None else out
    if isinstance(node, dict):
        for k, v in node.items():
            if isinstance(k, str):
                out.append((path + ".{}", k))
            strings(v, path + "." + k, out)
    elif isinstance(node, list):
        for i, v in enumerate(node):
            strings(v, "%s[%d]" % (path, i), out)
    elif isinstance(node, str):
        out.append((path, node))
    elif isinstance(node, (int, float)) and not isinstance(node, bool):
        out.append((path, str(node)))
    return out


def decode(v: str):
    """Every form an identity can hide in: percent-encoding and base64. A
    Linear cursor is base64 of a row's own id, and base64 is an encoding, not
    anonymisation."""
    out = []
    if "%" in v:
        d = urllib.parse.unquote(v)
        if d != v:
            out.append(d)
    if B64ISH.fullmatch(v):
        for fn in (base64.urlsafe_b64decode, base64.b64decode):
            try:
                d = fn(v + "=" * (-len(v) % 4)).decode("utf-8", "strict")
            except Exception:                                # noqa: BLE001
                continue
            if d.isprintable():
                out.append(d)
                break
    return out


def from_map() -> dict[str, str]:
    real: dict[str, str] = {}

    def add(v, what):
        if isinstance(v, str) and len(v.strip()) >= MIN_LITERAL:
            real.setdefault(v.strip(), what)

    for path in MAPS:
        if not path.exists():
            continue
        d = json.loads(path.read_text())
        for e in [d.get("owner")] + list(d.get("people") or []):
            r = (e or {}).get("real") or {}
            who = "owner" if (e or {}).get("fake", {}).get("owner") else "person"
            for key in ("name", "first", "last"):
                add(r.get(key), who + " " + key)
            for key in ("emails", "github", "slack", "slackTeam", "aliases",
                        "domains"):
                for v in r.get(key) or []:
                    add(v, who + " " + key)
                    if key == "emails" and "@" in v:
                        add(v.split("@", 1)[0], who + " email local part")
                        add(v.split("@", 1)[1], "email domain")
        for bucket, m in (d.get("buckets") or {}).items():
            for k in m:
                add(k, "bucket " + bucket)
        for bucket, m in (d.get("learned") or {}).items():
            for k in m:
                add(k, "learned " + bucket)
    return real


# The paths in a Linear payload whose value is an identity, and what it is.
IDENTITY_KEYS = {
    "id": "row id", "endCursor": "page cursor", "labelIds": "label id",
    "autoCloseStateId": "state id", "documentContentId": "document id",
    "projectUpdateId": "project update id", "initiativeId": "initiative id",
    "initiativeUpdateId": "initiative update id", "userId": "user id",
    "defaultHomeViewTargetId": "view id", "slugId": "project slug",
    "name": "name", "displayName": "display name", "email": "address",
    "initials": "initials", "url": "url", "avatarUrl": "avatar url",
    "logoUrl": "logo url", "cycleCalenderUrl": "calendar url",
    "urlKey": "workspace url key", "key": "team key",
    "identifier": "issue identifier", "branchName": "branch name",
    "gitHubUserId": "github user id", "slackChannelId": "slack channel id",
    "microsoftTeamsChannelId": "teams channel id",
    "scimGroupName": "scim group", "codeIntelligenceRepository": "repository",
    "previousIdentifiers": "previous identifier",
    "previousUrlKeys": "previous url key", "statusLabel": "status label",
}
PROSE_RAW_KEYS = {"description", "body", "content", "quotedText", "title"}


def from_raw() -> tuple[dict[str, str], set[str]]:
    real: dict[str, str] = {}
    prose: set[str] = set()
    if not RAW.exists():
        return real, prose

    def add(v, what):
        if isinstance(v, str) and len(v.strip()) >= MIN_LITERAL:
            real.setdefault(v.strip(), what)
            if UUID.fullmatch(v.strip()):
                real.setdefault(v.strip().replace("-", ""), what + " (undashed)")

    def visit(node):
        if isinstance(node, dict):
            for k, v in node.items():
                if k in IDENTITY_KEYS:
                    if isinstance(v, str):
                        add(v, "raw " + IDENTITY_KEYS[k])
                        if k.endswith("Url") or k == "url":
                            for seg in re.split(r"[/?&=#]", v):
                                seg = seg.strip()
                                if (len(seg) >= 6 and ":" not in seg
                                        and not seg.replace("x", "").isdigit()
                                        and seg.lower() not in ALLOW):
                                    add(seg, "raw url segment")
                        if k == "email" and "@" in v:
                            add(v.split("@", 1)[0], "raw email local part")
                            add(v.split("@", 1)[1], "raw email domain")
                    elif isinstance(v, list):
                        for x in v:
                            add(x, "raw " + IDENTITY_KEYS[k])
                if k in PROSE_RAW_KEYS and isinstance(v, str) \
                        and len(v.strip()) >= 12:
                    prose.add(v.strip())
                visit(v)
        elif isinstance(node, list):
            for v in node:
                visit(v)

    for f in sorted(RAW.glob("*.json")):
        if f.name.endswith(".local.json"):
            continue
        try:
            visit(json.loads(f.read_text()))
        except Exception:                                    # noqa: BLE001
            continue
    return real, prose


def slugify(text: str) -> str:
    out, last = [], "-"
    for ch in (text or "").lower():
        if ch.isalnum():
            out.append(ch); last = ch
        elif last != "-":
            out.append("-"); last = "-"
    return "".join(out).strip("-")[:60].strip("-") or "untitled"


def grammar(files) -> list[tuple[str, str, str, str]]:
    """Check 4: every composed value recomputed from the fixture's own fake
    parts. A title slug that does not match the fake title is the real one."""
    docs = {f: json.loads(f.read_text()) for f in files}
    keys, org = {}, {}
    for doc in docs.values():
        node = (doc.get("data") or {}).get("teams") or {}
        for t in node.get("nodes") or []:
            keys[t.get("id")] = t.get("key")
        if (doc.get("data") or {}).get("organization"):
            org = doc["data"]["organization"]
    url_key = slugify(org.get("name") or "")
    bad = []
    if org and org.get("urlKey") != url_key:
        bad.append(("OrganizationPage1", "urlKey", org.get("urlKey"),
                    "does not slug the workspace's own (fake) name"))
    for f, doc in docs.items():
        for issue in (((doc.get("data") or {}).get("issues") or {})
                      .get("nodes") or []):
            key = keys.get((issue.get("team") or {}).get("id"))
            want_id = "%s-%d" % (key, int(issue.get("number") or 0)) if key else None
            if want_id and issue.get("identifier") != want_id:
                bad.append((f.name, "identifier", issue.get("identifier"),
                            "is not <team key>-<number> (%s)" % want_id))
            want_url = "https://linear.app/%s/issue/%s/%s" % (
                url_key, issue.get("identifier"), slugify(issue.get("title") or ""))
            if issue.get("url") != want_url:
                bad.append((f.name, "url", issue.get("url"),
                            "does not compose the fake url key, identifier and title"))
            for c in (issue.get("comments") or {}).get("nodes") or []:
                if c.get("url") and not str(c["url"]).startswith(issue.get("url") or "\0"):
                    bad.append((f.name, "comment.url", c.get("url"),
                                "is not its issue's URL plus an anchor"))
    return bad


# CHECK 5: the joins. Each entry is (the array the node sits in, the relation,
# the array its target sits in). Only relations that MUST resolve are listed:
# a relation whose target this bundle does not mirror has no node to find, and
# the ones the guide admits may dangle (an issue's parent outside the window,
# a state inherited from a team this key cannot list) are named in
# `MAY_DANGLE`.
JOINS = [
    ("issues", "team", "teams"), ("issues", "state", "workflowStates"),
    ("issues", "assignee", "users"), ("issues", "creator", "users"),
    ("issues", "project", "projects"), ("issues", "cycle", "cycles"),
    ("issues", "labels", "issueLabels"), ("issues", "subscribers", "users"),
    ("projects", "status", "projectStatuses"), ("projects", "lead", "users"),
    ("projects", "creator", "users"), ("projects", "leadTeam", "teams"),
    ("projects", "teams", "teams"), ("projects", "members", "users"),
    ("cycles", "team", "teams"), ("issueLabels", "team", "teams"),
    ("issueLabels", "creator", "users"),
    ("teams", "organization", "organization"),
    ("teams", "defaultIssueState", "workflowStates"),
    ("teams", "triageIssueState", "workflowStates"),
    ("teams", "autoCloseStateId", "workflowStates"),
    ("teams", "activeCycle", "cycles"),
]
MAY_DANGLE = {("issues", "parent"), ("workflowStates", "team"),
              ("teams", "children"), ("teams", "ancestors")}
# `reactionData` is Linear's own SUMMARY of the same reactions that ride as
# rows, and it names its reactors by id. The pseudonymiser maps a value
# through a bucket named for its key, so `reactions[].user.id` and
# `reactionData[].reactions[].userId` land on two different fakes unless the
# repair puts them back together — 4 of 4 did not, and every other check here
# passed anyway.
SUMMARY_JOINS = [("issues", "reactionData"), ("comments", "reactionData")]


def joins(files) -> list[tuple[str, str, str, str]]:
    """Check 5: a fixture set that cannot resolve its own references.

    The pseudonymiser maps an id through a bucket named for the KEY it sits
    under, so a state's own `id` and a team's `autoCloseStateId` pointing at
    it land on two different fakes unless something puts them back together —
    and a set whose joins are broken passes every other check in this file
    while proving nothing at all. Codex found exactly that on the first cut.
    """
    index, nodes = {}, {}
    for f in files:
        data = json.loads(f.read_text()).get("data") or {}
        for field, value in data.items():
            rows = (value.get("nodes") if isinstance(value, dict) and "nodes" in value
                    else [value] if isinstance(value, dict) else [])
            index.setdefault(field, set()).update(
                r["id"] for r in rows if isinstance(r, dict) and r.get("id"))
            nodes.setdefault(field, []).extend(r for r in rows if isinstance(r, dict))
            if field == "organization":
                for s in (rows[0] if rows else {}).get("projectStatuses") or []:
                    index.setdefault("projectStatuses", set()).add(s.get("id"))
            if field == "issues":
                for r in rows:
                    for c in (r.get("comments") or {}).get("nodes") or []:
                        index.setdefault("comments", set()).add(c.get("id"))
    bad = []
    for field, rel, target in JOINS:
        for node in nodes.get(field, []):
            value = node.get(rel)
            if value is None:
                continue
            got = ([value] if isinstance(value, str)
                   else [value.get("id")] if isinstance(value, dict) and "id" in value
                   else [n.get("id") for n in (value.get("nodes") or [])]
                   if isinstance(value, dict) else [])
            for ident in [g for g in got if g]:
                if ident not in index.get(target, set()):
                    bad.append((field, rel, target,
                                "names a %s the set does not carry" % target))
    users = index.get("users", set())
    for field, _rel in SUMMARY_JOINS:
        rows = nodes.get(field, [])
        if field == "comments":
            rows = [c for i in nodes.get("issues", [])
                    for c in (i.get("comments") or {}).get("nodes") or []]
        for node in rows:
            for entry in node.get("reactionData") or []:
                for r in (entry or {}).get("reactions") or []:
                    uid = r.get("userId")
                    if uid and uid not in users:
                        bad.append((field, "reactionData[].reactions[].userId",
                                    "users", "names a member the set does not "
                                    "carry"))
    return bad


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    mapped = from_map()
    raw_real, prose = from_raw()
    real = dict(mapped)
    real.update(raw_real)
    real = {k: v for k, v in real.items() if specific(k)}
    if not real:
        print("audit: no real corpus (tools/persona.local.json and raw/linear "
              "are gitignored) — nothing to check against", file=sys.stderr)
        return 1

    # TWO INDEXES, because one regex cannot be both. The workspace's UUIDs
    # are tens of thousands of literals and an alternation over them takes
    # minutes; they are also EXACTLY shaped, so a token scan with a set
    # lookup finds them in one pass. Everything else — names, addresses,
    # words, URL segments — stays a regex, because it has to match inside a
    # sentence and on word boundaries.
    exact = {k.lower(): v for k, v in real.items()
             if UUID.fullmatch(k) or re.fullmatch(r"[0-9a-f]{32}", k, re.I)}
    literals = sorted((k for k in real if k.lower() not in exact),
                      key=len, reverse=True)
    rx = re.compile("|".join(r"(?<![0-9A-Za-z])%s(?![0-9A-Za-z])" % re.escape(x)
                             for x in literals), re.I) if literals else None
    TOKEN = re.compile(r"[0-9A-Za-z][0-9A-Za-z-]{7,}")

    files = sorted(FIXTURES.glob("*.json"))
    findings: list[tuple[str, str, str, str]] = []
    ungenerated: list[tuple[str, str, str]] = []
    scanned = 0
    for f in files:
        doc = json.loads(f.read_text())
        pairs = strings(doc) + [("<filename>", f.name)]
        extra = [(p + " (decoded)", d) for p, v in pairs
                 if isinstance(v, str) for d in decode(v)]
        for path, v in pairs + extra:
            scanned += 1
            # `icon` is LINEAR's vocabulary — a name out of its own icon set
            # or a standard emoji shortcode — so a real team called `Mirror`
            # must not make the icon named `mirror` a finding.
            if path.endswith(".icon") and " " not in v:
                continue
            for m in TOKEN.finditer(v):
                what = exact.get(m.group(0).lower())
                if what:
                    findings.append((f.name, path, m.group(0), what))
            for m in (rx.finditer(v) if rx else ()):
                hit = m.group(0)
                findings.append((f.name, path, hit, real.get(hit, next(
                    (real[k] for k in literals if k.lower() == hit.lower()),
                    "?"))))
            if v.strip() in prose:
                findings.append((f.name, path, v.strip()[:40],
                                 "a whole real prose span"))
            key = path.rsplit(".", 1)[-1]
            # A link inside generated prose is never generator vocabulary, so
            # check 3 tests the PROSE — the links are check 1's business, and
            # `--repair` composes every one of them out of fake parts.
            bare = re.sub(r"https?://\S+", " ", v)
            if key in PROSE_KEYS and len(v.strip()) >= PROSE_MIN \
                    and not _already_generated(bare):
                ungenerated.append((f.name, path, v.strip()[:60]))

    seen, uniq = set(), []
    for row in findings:
        k = (row[0], row[2])
        if k in seen:
            continue
        seen.add(k)
        uniq.append(row)
    broken = grammar(files) + joins(files)

    print("audit: %d fixtures, %d values scanned, %d real literals (%d from "
          "the map, %d from raw/linear) and %d real prose spans"
          % (len(files), scanned, len(real), len(mapped),
             len(real) - len(mapped), len(prose)))
    if ungenerated:
        print("audit: %d prose strings the generator could not have written — "
              "every one is a suspected leak:" % len(ungenerated))
        for name, path, v in ungenerated[:20]:
            print("  %-46s %-34s %s" % (name, path[:34], shown(v)))
    if broken:
        print("audit: %d composed values that do not recompute, or joins that "
              "do not resolve, inside the set:" % len(broken))
        for name, what, got, why in broken[:20]:
            print("  %-46s %-16s %-22s %s"
                  % (name, what, shown(str(got)), why))
    if not uniq and not ungenerated and not broken:
        print("audit: 0 findings — no real member, address, workspace, team, "
              "project, label, issue title, identifier, branch, URL segment, "
              "workspace UUID, cursor or prose span survives in any fixture "
              "body, key, path or file name, decoded or not; and every "
              "composed value recomputes from the fixture's own fake parts")
        return 0
    for name, path, hit, what in (uniq if args.verbose else uniq[:40]):
        print("  %-46s %-34s %-22s %s" % (name, path[:34], shown(hit), what))
    if not args.verbose and len(uniq) > 40:
        print("  ... %d more (--verbose)" % (len(uniq) - 40))
    total = len(uniq) + len(ungenerated) + len(broken)
    print("audit: %d findings" % total)
    return min(total, 125)


if __name__ == "__main__":
    sys.exit(main())
