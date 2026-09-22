#!/usr/bin/env python3
"""Audit the committed Notion fixtures for real identity, token by token.

`providers/` is a PUBLIC directory. The pseudonymiser's own safety net only
reads PROSE, so a bare user UUID, a title inside a URL path, a workspace name
or a property name the workspace chose is a leak it cannot see. This does see
them, because it does not look at the fixtures for things that LOOK real — it
looks for the REAL VALUES THEMSELVES, out of the two places real data is
allowed to exist:

  * `tools/persona.local.json` — the map. Every real name, alias, first name,
    last name, email address, local part, handle and the key side of every
    word bucket.
  * `raw/notion/` — the pull the fixtures were cut from. EVERY user `id`,
    `name`, `person.email`, `avatar_url`, `bot.workspace_name` and
    `workspace_id`; every `plain_text` and `text.content` span; every
    `child_page` / `child_database` title; every `properties` KEY and the
    `name` beside it; every select / multi-select option name; every `url`
    and `public_url` (whose path IS the page's title).

Every string in every fixture is scanned — object keys, values, URL paths,
query strings and recording FILE NAMES — and every one of them is also scanned
PERCENT-DECODED, because Notion spells a property id `%3C%3BXv` and a URL path
hides a title the same way.

THREE CHECKS, and the third is the one that would catch a leak nobody
predicted:

  1. LITERALS — every specific real value (a name, an address, a user UUID, a
     workspace name, a bucket word) against every fixture string, on word
     boundaries;
  2. PROSE, EXACTLY — every real `plain_text` / `text.content` / title string
     against every fixture string. A body is REPLACED wholesale rather than
     truncated, so a survivor survives whole;
  3. PROSE, POSITIVELY — every rich-text string in the fixtures must be drawn
     from the generator's own closed vocabulary (`persona._already_
     generated`). This is the check that does not need to know what the real
     value was: a sentence the generator could not have written is a sentence
     that came from somewhere else.

    python3 providers/notion/fixtures/audit.py            # exit 0 = clean
    python3 providers/notion/fixtures/audit.py --verbose  # every hit

Exit status is the number of findings, capped at 125.
"""

from __future__ import annotations

import argparse
import base64
import json
import pathlib
import re
import sys
import urllib.parse

HERE = pathlib.Path(__file__).resolve()
FIXTURES = HERE.parent
ROOT = HERE.parents[3]
MAP = ROOT / "tools" / "persona.local.json"
RAW = ROOT / "raw" / "notion"

sys.path.insert(0, str(ROOT / "tools"))
from persona import _already_generated  # noqa: E402


def fingerprint(value: str) -> str:
    """A finding, printed so the OUTPUT of this tool is committable.

    `provider-practices.md` §10: a tool never prints the literal. The
    fingerprint is salted with the persona map's own salt, so it is stable
    across runs, useless without the gitignored map, and enough to tell two
    findings apart.
    """
    import hashlib
    import hmac
    salt = b""
    if MAP.exists():
        try:
            salt = str(json.loads(MAP.read_text()).get("salt") or "").encode()
        except Exception:                                    # noqa: BLE001
            salt = b""
    digest = hmac.new(salt or b"notion-audit", value.encode("utf-8", "replace"),
                      hashlib.sha256).hexdigest()[:12]
    return "fp:%s/%d" % (digest, len(value))

# A literal is only a finding when it is SPECIFIC: three characters of a
# surname matching inside a UUID is noise, not a leak.
MIN_LITERAL = 4

# Notion's own vocabulary, plus the hosts and words the pseudonymiser
# deliberately keeps. A literal that is one of these is not an identity even
# when a real page, property or option happens to be named after it.
ALLOW = {
    "notion", "notion.com", "app.notion.com", "notion.so", "notion.site",
    "api.notion.com", "object", "page", "block", "database", "data_source",
    "datasource", "user", "person", "bot", "list", "workspace", "parent",
    "properties", "results", "title", "type", "text", "mention", "equation",
    "rich_text", "plain_text", "annotations", "href", "color", "default",
    "bold", "italic", "strikethrough", "underline", "code", "emoji",
    "external", "file", "custom_emoji", "select", "multi_select", "status",
    "date", "checkbox", "number", "url", "email", "phone_number", "formula",
    "relation", "rollup", "people", "files", "created_time", "created_by",
    "last_edited_time", "last_edited_by", "unique_id", "verification",
    "button", "place", "paragraph", "heading_1", "heading_2", "heading_3",
    "quote", "callout", "toggle", "to_do", "bulleted_list_item",
    "numbered_list_item", "child_page", "child_database", "table", "table_row",
    "column", "column_list", "divider", "breadcrumb", "table_of_contents",
    "link_preview", "link_to_page", "synced_block", "template", "unsupported",
    "image", "video", "audio", "pdf", "bookmark", "embed", "has_more",
    "next_cursor", "start_cursor", "page_size", "request_id", "archived",
    "in_trash", "is_archived", "is_locked", "is_inline", "database_type",
    "has_children", "true", "false", "null", "http", "https", "json",
    "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink",
    "red", "descending", "ascending", "sort", "direction", "timestamp",
    # Vendor hosts, which the pseudonymiser deliberately keeps: they are the
    # same string in every workspace and name nobody.
    "lh3.googleusercontent.com", "googleusercontent.com",
    "public.notion-static.com", "notion-static.com", "s3.us-west-2.amazonaws.com",
    "prod-files-secure.s3.us-west-2.amazonaws.com", "amazonaws.com",
}

# Words THIS REPOSITORY'S OWN GENERATOR draws from. A fake option called
# `anchor` colliding with a real option called `anchor` is this repository
# quoting itself, not a leak — and without this the audit reports every such
# coincidence forever.
def _generator_words() -> set[str]:
    import persona as _p
    words: set[str] = set()
    for pool in (_p.SLUG_WORDS, _p.SLUG_TAILS, _p.PROJECT_WORDS,
                 _p.FIRST_SHORT, _p.FIRST_MEDIUM, _p.FIRST_LONG,
                 _p.SURNAMES, _p.COMPANIES):
        for w in pool:
            for part in str(w).replace("-", " ").split():
                words.add(part.lower())
    return words


GENERATED_WORDS = _generator_words()

# The vendor hosts `tools/persona.py` deliberately KEEPS. `google.com` is in
# the map as somebody's email DOMAIN and is also the host of
# `maps.google.com`, which the pseudonymiser keeps on purpose — the address
# `someone@google.com` is a longer literal and is still caught, because the
# scan is longest-first.
def _kept_hosts() -> set[str]:
    import persona as _p
    # `notion.com` is NOT in persona.KEEP_HOSTS (`notion.so` is), which is why
    # `tools/rawpull/notion.py --repair` has to put `app.notion.com` back; it
    # is named here so this audit does not report the vendor's own host.
    # Platform ask: providers/notion/README.md.
    return {h.lower() for h in _p.KEEP_HOSTS} | {"notion.com", "notion.site"}


ALLOW |= _kept_hosts()

# A kept vendor host is MASKED OUT of a value before the literal scan: a real
# company called Google is in the map, and `maps.google.com` is a host the
# pseudonymiser keeps on purpose, so every such URL reported the company
# forever. The mask is only for check 1 — the prose checks see the value
# whole.
KEPT_HOST_RE = re.compile(
    r"(?<![0-9A-Za-z.])(?:[0-9A-Za-z-]+\.)*(?:%s)(?![0-9A-Za-z])"
    % "|".join(sorted((re.escape(h) for h in _kept_hosts()), key=len,
                      reverse=True)), re.I)

# Every URL host in the fixtures that is neither a vendor's nor a `.example`
# fake is reported: a tenant subdomain names the organisation paying for it.
HOST_RE = re.compile(r"https?://([0-9A-Za-z.-]+)", re.I)

# The keys whose value is rich-text prose. Check 3 applies to these.
PROSE_KEYS = {"plain_text", "content"}


def specific(lit: str) -> bool:
    v = lit.strip()
    if len(v) < MIN_LITERAL or v.lower() in ALLOW:
        return False
    if v.lower() in GENERATED_WORDS:
        return False
    if re.search(r"[@./_\- ]", v):
        return True
    return len(v) >= 6


def strings(node, path="", out=None):
    """Every string in the document, with the path it sits at. Keys count: a
    real word hides in a `properties` key as easily as in a value."""
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


def from_raw() -> tuple[dict[str, str], set[str]]:
    """Every identity-bearing value and every prose string the RAW pull
    carries — including people and words the map never heard of."""
    real: dict[str, str] = {}
    prose: set[str] = set()
    if not RAW.exists():
        return real, prose

    def add(v, what):
        if isinstance(v, str) and len(v.strip()) >= MIN_LITERAL:
            real.setdefault(v.strip(), what)

    def add_prose(v):
        if isinstance(v, str) and len(v.strip()) >= 12:
            prose.add(v.strip())

    def visit(node):
        if isinstance(node, dict):
            if node.get("object") == "user":
                # a user object: its UUID, its display name, its address and
                # its avatar are all identity
                add(node.get("id"), "raw user id")
                add(str(node.get("id") or "").replace("-", ""),
                    "raw user id (undashed)")
                add(node.get("name"), "raw user name")
                person = node.get("person")
                if isinstance(person, dict):
                    v = person.get("email")
                    if isinstance(v, str) and "@" in v:
                        add(v, "raw email")
                        add(v.split("@", 1)[0], "raw email local part")
                        add(v.split("@", 1)[1], "raw email domain")
                avatar = node.get("avatar_url")
                if isinstance(avatar, str) and avatar:
                    add(avatar, "raw avatar url")
                    for seg in re.split(r"[/?&=]", avatar):
                        if len(seg) >= 8:
                            add(seg, "raw avatar url segment")
                bot = node.get("bot")
                if isinstance(bot, dict):
                    add(bot.get("workspace_name"), "raw workspace name")
                    add(bot.get("workspace_id"), "raw workspace id")
            # The object's own address carries its TITLE in the path. The
            # WHOLE slug is the literal, never the words inside it: a real
            # page called "Rollback plan" decomposes into `rollback` and
            # `plan`, which are also the prose generator's own vocabulary, and
            # adding them reported 265 findings that were all this repository
            # quoting itself.
            for key in ("url", "public_url"):
                v = node.get(key)
                if isinstance(v, str) and "notion" in v:
                    add(v, "raw " + key)
                    tail = v.rstrip("/").rsplit("/", 1)[-1]
                    slug = tail.rsplit("-", 1)[0] if "-" in tail else ""
                    if len(slug) >= 12:
                        add(slug, "raw url title slug")
            # rich text, both halves
            for key in PROSE_KEYS:
                add_prose(node.get(key))
            # a block payload's bare title
            for key in ("child_page", "child_database"):
                sub = node.get(key)
                if isinstance(sub, dict):
                    add_prose(sub.get("title"))
                    add(sub.get("title"), "raw child title")
            # the workspace's own vocabulary: property names (keys AND the
            # `name` beside them) and option names
            props = node.get("properties")
            if isinstance(props, dict):
                for k, v in props.items():
                    add(k, "raw property name")
                    if isinstance(v, dict):
                        add(v.get("name"), "raw property name")
            if node.get("object") is None and isinstance(node.get("name"), str) \
                    and "color" in node:
                add(node["name"], "raw option name")
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
        except Exception:                                    # noqa: BLE001
            continue
    return real, prose


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    real = from_map()
    raw_real, prose = from_raw()
    real.update(raw_real)
    real = {k: v for k, v in real.items() if specific(k)}
    if not real:
        print("audit: no real corpus (tools/persona.local.json and raw/notion "
              "are gitignored) — nothing to check against", file=sys.stderr)
        return 1

    # Longest first, so an address is reported as the address and not as the
    # domain inside it. Word boundaries, so a four-letter surname does not
    # match inside a UUID.
    literals = sorted(real, key=len, reverse=True)
    rx = re.compile("|".join(r"(?<![0-9A-Za-z])%s(?![0-9A-Za-z])" % re.escape(x)
                             for x in literals), re.I)

    files = sorted(p for p in FIXTURES.glob("*.json"))
    findings: list[tuple[str, str, str, str]] = []
    scanned = 0
    hosts: dict[str, tuple[str, str]] = {}
    ungenerated: list[tuple[str, str, str]] = []
    for f in files:
        doc = json.loads(f.read_text())
        pairs = strings(doc) + [("<filename>", f.name)]
        # DECODED, and repeatedly: a property id is `%3C%3BXv`, a URL query
        # can carry a percent-encoded URL inside it, and a Notion file URL
        # nests both. One pass reached the first layer only (Codex 18).
        extra = []
        for pth, v in pairs:
            if not isinstance(v, str):
                continue
            seen_forms = {v}
            cur = v
            for _round in range(4):
                nxt = urllib.parse.unquote(cur)
                if nxt == cur or nxt in seen_forms:
                    break
                seen_forms.add(nxt)
                extra.append((pth + " (decoded)", nxt))
                cur = nxt
            for chunk in re.findall(r"[A-Za-z0-9_-]{16,}={0,2}", v):
                try:
                    raw = base64.urlsafe_b64decode(
                        chunk + "=" * (-len(chunk) % 4)).decode("utf-8")
                except Exception:                            # noqa: BLE001
                    continue
                if raw.isprintable() and len(raw) >= MIN_LITERAL:
                    extra.append((pth + " (base64)", raw))
        for path, v in pairs + extra:
            scanned += 1
            for m in HOST_RE.finditer(v):
                host = m.group(1).lower()
                if not host.endswith(".example") and not KEPT_HOST_RE.fullmatch(host):
                    hosts.setdefault(host, (f.name, path))
            # 1. the literals, with every kept vendor host masked out
            for m in rx.finditer(KEPT_HOST_RE.sub(" ", v)):
                hit = m.group(0)
                findings.append((f.name, path, hit, real.get(hit, next(
                    (real[k] for k in literals if k.lower() == hit.lower()),
                    "?"))))
            # 2. prose, exactly
            if v.strip() in prose:
                findings.append((f.name, path, v.strip(),
                                 "a whole real rich-text span"))
            # 3. prose, positively
            key = path.rsplit(".", 1)[-1]
            if key in PROSE_KEYS and len(v.strip()) >= 12 \
                    and not _already_generated(v):
                ungenerated.append((f.name, path, v.strip()[:60]))

    seen, uniq = set(), []
    for row in findings:
        key = (row[0], row[2])
        if key in seen:
            continue
        seen.add(key)
        uniq.append(row)

    print("audit: %d fixtures, %d values scanned, %d real literals "
          "(%d from the map, %d from raw/notion) and %d real prose spans"
          % (len(files), scanned, len(real), len(from_map()),
             len(real) - len(from_map()), len(prose)))
    if hosts:
        # COUNTED, not merely printed: a tenant subdomain names the
        # organisation paying for it, and an audit that returns zero beside a
        # list of suspect hosts is an audit nobody reads (Codex 18).
        print("audit: %d URL host(s) that are neither a vendor's nor a "
              "`.example` fake — each one is a finding until it is allowed:"
              % len(hosts))
        for host, (name, path) in sorted(hosts.items()):
            print("  %-40s %s %s" % (fingerprint(host), name, path[:34]))
    if ungenerated:
        print("audit: %d rich-text strings the generator could not have "
              "written — every one is a suspected leak:" % len(ungenerated))
        for name, path, v in ungenerated[:20]:
            print("  %-52s %-34s %s" % (name, path[:34], fingerprint(v)))
    if not uniq and not ungenerated and not hosts:
        print("audit: 0 findings — no real name, address, user id, workspace "
              "name, property name, option name, URL title or rich-text span "
              "survives in any fixture body, path, key or file name, decoded "
              "or not; and every rich-text string in the set is one this "
              "repository's own generator wrote")
        return 0
    for name, path, hit, what in (uniq if args.verbose else uniq[:40]):
        print("  %-52s %-34s %-22s %s"
              % (name, path[:34], fingerprint(hit), what))
    if not args.verbose and len(uniq) > 40:
        print("  ... %d more (--verbose)" % (len(uniq) - 40))
    total = len(uniq) + len(ungenerated) + len(hosts)
    print("audit: %d findings" % total)
    return min(total, 125)


if __name__ == "__main__":
    sys.exit(main())
