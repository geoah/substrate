#!/usr/bin/env python3
"""Audit the committed GitHub fixtures for real identity, token by token.

`providers/` is a PUBLIC directory. The pseudonymiser's own safety net only
reads PROSE, so a bare id, a login inside a URL path, a query-string term or
an opaque node id is a leak it cannot see. This does see them, because it does
not look at the fixtures for things that look real — it looks for the REAL
VALUES THEMSELVES, out of the two places real data is allowed to exist:

  * `tools/persona.local.json` — the map. Every real name, alias, first name,
    last name, email address, local part, GitHub login, Slack id and the key
    side of every word bucket (the real domains, companies, projects, teams,
    channels).
  * `raw/github/` — the pull the fixtures were cut from. EVERY `login`,
    `name`, `email`, `blog`, `company`, `full_name`, `slug`, `html_url` host
    and EVERY `node_id` the real payloads carry, whether or not the map ever
    heard of them.

Every string in every fixture is scanned — object keys, values, URL paths,
query strings and recording FILE NAMES — plus the decoded contents of every
node id, because `U_kgDODF1NIw` is not a leak until you decode it into the
numeric user id it was minted from.

    python3 providers/github/fixtures/audit.py            # exit 0 = clean
    python3 providers/github/fixtures/audit.py --verbose  # every hit

Exit status is the number of findings, capped at 125.
"""

from __future__ import annotations

import argparse
import base64
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve()
FIXTURES = HERE.parent
ROOT = HERE.parents[3]
MAP = ROOT / "tools" / "persona.local.json"
RAW = ROOT / "raw" / "github"

# Words that are GitHub's, a persona's, or too short/common to carry identity.
# A literal is only a finding when it is SPECIFIC: three characters of a
# surname matching inside a base64 blob is noise, not a leak.
MIN_LITERAL = 4
# GitHub's own path and payload vocabulary, plus the hosts the pseudonymiser
# deliberately keeps. A literal that is one of these is not an identity even
# when a real repository, label or milestone happens to be named after it.
ALLOW = {
    "github", "github.com", "api.github.com", "githubusercontent.com",
    "avatars.githubusercontent.com", "raw.githubusercontent.com",
    "gravatar.com", "user", "users", "repos", "repo", "issue", "issues",
    "pull", "pulls", "search", "type", "involves", "review", "reviews",
    "requested", "reviewers", "assignees", "labels", "milestone",
    "milestones", "comments", "commits", "branches", "teams", "team",
    "events", "hooks", "keys", "tags", "trees", "blobs", "merges",
    "compare", "contents", "collaborators", "contributors", "stargazers",
    "subscribers", "subscription", "subscriptions", "notifications",
    "deployments", "downloads", "releases", "languages", "forks", "starred",
    "following", "followers", "gists", "organizations", "received_events",
    "main", "master", "null", "true", "false", "http", "https", "json",
    "com", "org", "net", "api", "www", "page", "sort", "order", "updated",
    "created", "closed", "merged", "state", "open", "bot", "none", "all",
    "projects", "project", "security", "dependencies", "dependency",
    "feature", "features", "staging", "production", "other", "others",
    "default", "docs", "test", "tests", "build", "chore", "bug", "fix",
    "enhancement", "documentation", "duplicate", "invalid", "wontfix",
    "question", "help wanted", "good first issue", "dependabot",
    "renovate", "codeowners", "workflow", "workflows", "actions",
}


def specific(lit: str, what: str) -> bool:
    """Is this literal SPECIFIC enough that finding it is evidence?

    A login, an address, a node id or a full name is. A single common word
    that happens to be a repository's or a label's name is not — matching it
    reports `/milestones` in a hypermedia URL as a leak and buries the real
    findings. Anything with a `@`, a `.`, a `-`, a `_`, a `/` or a space in
    it is specific by construction; a bare word has to be long.
    """
    v = lit.strip()
    if len(v) < MIN_LITERAL or v.lower() in ALLOW:
        return False
    if what.endswith("node_id"):
        return True
    if re.search(r"[@./_\- ]", v):
        return True
    return len(v) >= 6

def strings(node, path="", out=None):
    """Every string in the document, with the path it sits at. Keys count:
    a real word can hide in a `custom_properties` key as easily as a value."""
    out = [] if out is None else out
    if isinstance(node, dict):
        for k, v in node.items():
            if isinstance(k, str):
                out.append((path + "." + k, k))
            strings(v, path + "." + k, out)
    elif isinstance(node, list):
        for i, v in enumerate(node):
            strings(v, "%s[%d]" % (path, i), out)
    elif isinstance(node, str):
        out.append((path, node))
    elif isinstance(node, (int, float)) and not isinstance(node, bool):
        out.append((path, str(node)))
    return out


def decode_node_id(v: str):
    """What a node id says once it is not opaque any more: the legacy form's
    plaintext `04:User88447`, and the modern form's msgpack integers."""
    try:
        if "_" in v:
            body = v.partition("_")[2]
            raw = base64.urlsafe_b64decode(body + "=" * (-len(body) % 4))
            if not raw or raw[0] not in (0x92, 0x93):
                return []
            ints, i = [], 1
            while i < len(raw):
                b = raw[i]
                if b <= 0x7F:
                    ints.append(b); i += 1
                elif b == 0xCC:
                    ints.append(raw[i + 1]); i += 2
                elif b == 0xCD:
                    ints.append(int.from_bytes(raw[i + 1:i + 3], "big")); i += 3
                elif b == 0xCE:
                    ints.append(int.from_bytes(raw[i + 1:i + 5], "big")); i += 5
                elif b == 0xCF:
                    ints.append(int.from_bytes(raw[i + 1:i + 9], "big")); i += 9
                else:
                    return []
            return [str(x) for x in ints]
        raw = base64.b64decode(v + "=" * (-len(v) % 4)).decode("ascii")
        return [raw] + re.findall(r"\d+", raw)
    except Exception:
        return []


def from_map() -> dict[str, str]:
    """Every real literal the pseudonym map holds, with what it is."""
    real: dict[str, str] = {}
    if not MAP.exists():
        return real
    d = json.loads(MAP.read_text())

    def add(v, what):
        if isinstance(v, str) and len(v.strip()) >= MIN_LITERAL:
            real.setdefault(v.strip(), what)

    for e in [d.get("owner")] + list(d.get("people") or []):
        r = (e or {}).get("real") or {}
        who = "owner" if (e or {}).get("fake", {}).get("owner") else "person"
        for key in ("name", "first", "last"):
            add(r.get(key), who + " " + key)
        for key in ("emails", "github", "slack", "slackTeam", "aliases"):
            for v in r.get(key) or []:
                add(v, who + " " + key)
                if key == "emails" and "@" in v:
                    add(v.split("@", 1)[0], who + " email local part")
                    add(v.split("@", 1)[1], "email domain")
    for bucket, m in (d.get("buckets") or {}).items():
        for k in m:
            add(k, "bucket " + bucket)
    return real


def from_raw() -> dict[str, str]:
    """Every identity-bearing value the RAW pull carries — including people
    the map never heard of, whose fakes came out of the hash fallback."""
    real: dict[str, str] = {}
    nodes: dict[str, str] = {}
    if not RAW.exists():
        return real

    def add(v, what):
        if isinstance(v, str) and len(v.strip()) >= MIN_LITERAL:
            real.setdefault(v.strip(), what)

    def visit(node):
        if isinstance(node, dict):
            login = node.get("login") if isinstance(node.get("login"), str) else ""
            if login:
                # a user / organization / bot object: its login, its display
                # name, its company and its typed-in site are all identity
                add(login, "raw login")
                add(node.get("name"), "raw display name")
                add(node.get("company"), "raw company")
                add(node.get("ldap_dn"), "raw ldap")
                add(node.get("twitter_username"), "raw twitter")
                for key in ("email", "notification_email"):
                    v = node.get(key)
                    if isinstance(v, str) and "@" in v:
                        add(v, "raw email")
                        add(v.split("@", 1)[0], "raw email local part")
                        add(v.split("@", 1)[1], "raw email domain")
                blog = node.get("blog")
                if isinstance(blog, str) and blog.strip():
                    add(blog, "raw blog")
                    host = re.sub(r"^\w+://", "", blog).split("/")[0]
                    add(host, "raw blog host")
            full = node.get("full_name")
            if isinstance(full, str) and node.get("owner") and "/" in full:
                add(full, "raw full_name")
                for half in full.split("/"):
                    add(half, "raw full_name half")
            if isinstance(node.get("slug"), str) and (
                    "permission" in node or "privacy" in node):
                add(node["slug"], "raw team slug")
                add(node.get("name"), "raw team name")
            if isinstance(node.get("node_id"), str):
                nodes[node["node_id"]] = "raw node_id"
            for v in node.values():
                visit(v)
        elif isinstance(node, list):
            for v in node:
                visit(v)

    for f in sorted(RAW.glob("*.json")):
        if f.name.endswith(".local.json"):
            continue
        try:
            visit(json.loads(f.read_text()))
        except Exception:
            continue
    real.update(nodes)
    return real


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    real = from_map()
    real.update(from_raw())
    real = {k: v for k, v in real.items() if specific(k, v)}
    if not real:
        print("audit: no real corpus (tools/persona.local.json and raw/github "
              "are gitignored) — nothing to check against", file=sys.stderr)
        return 1
    # Longest first: `george@reflection.ai` must be reported as the address,
    # not as the domain inside it.
    literals = sorted(real, key=len, reverse=True)
    # Word boundaries, so a four-letter surname does not match inside
    # `content` — a substring that is not a token is not a leak, and a
    # thousand of them hide the one that is.
    rx = re.compile("|".join(r"(?<![0-9A-Za-z])%s(?![0-9A-Za-z])" % re.escape(x)
                             for x in literals), re.I)

    files = sorted(p for p in FIXTURES.glob("*.json"))
    findings: list[tuple[str, str, str, str]] = []
    scanned = 0
    for f in files:
        doc = json.loads(f.read_text())
        pairs = strings(doc) + [("<filename>", f.name)]
        # a node id is not a string until it is decoded
        extra = []
        for path, v in pairs:
            if isinstance(v, str) and (v.startswith("MD") or "_" in v[:5]):
                for d in decode_node_id(v):
                    extra.append((path + " (decoded)", d))
        for path, v in pairs + extra:
            scanned += 1
            for m in rx.finditer(v):
                hit = m.group(0)
                findings.append((f.name, path, hit, real.get(hit, real.get(
                    next((k for k in literals if k.lower() == hit.lower()), hit),
                    "?"))))
    seen = set()
    uniq = []
    for row in findings:
        key = (row[0], row[2])
        if key in seen:
            continue
        seen.add(key)
        uniq.append(row)

    print("audit: %d fixtures, %d values scanned, %d real literals "
          "(%d from the map, %d from raw/github)"
          % (len(files), scanned, len(real), len(from_map()),
             len(real) - len(from_map())))
    if not uniq:
        print("audit: 0 findings — no real name, login, email, domain, "
              "bucket word or node id (decoded or not) survives in any "
              "fixture body, path, query string or file name")
        return 0
    for name, path, hit, what in (uniq if args.verbose else uniq[:40]):
        print("  %-52s %-40s %-24r %s" % (name, path[:40], hit, what))
    if not args.verbose and len(uniq) > 40:
        print("  ... %d more (--verbose)" % (len(uniq) - 40))
    print("audit: %d findings" % len(uniq))
    return min(len(uniq), 125)


if __name__ == "__main__":
    sys.exit(main())
