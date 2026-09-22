#!/usr/bin/env python3
"""Audit the committed WHOOP fixtures for real identity, token by token.

`providers/` is a PUBLIC directory, and WHOOP's payloads are HEALTH DATA about
one person: an address and a name on the profile, a height and a weight on the
body measurement, and — just as identifying — the exact minutes the owner fell
asleep and woke up on a named calendar night. This does not look at the
fixtures for things that look real; it looks for the REAL VALUES THEMSELVES,
out of the two places real data is allowed to exist:

  * `tools/persona.local.json` — the map. Every real name, alias, first name,
    last name, email address, local part and the key side of every word bucket.
  * `raw/whoop/` — the pull the fixtures were cut from. Every `email`,
    `first_name`, `last_name`, every member/cycle/sleep/workout id, every
    `v1_id`, every INSTANT and every body measurement the real payloads carry,
    whether or not the map ever heard of them.

THE AUDIT DECODES BEFORE IT SCANS (`docs/provider-practices.md` §2). A grep is
not an audit: Google's first check reported zero while the set carried the
owner's own address 61 times, base64'd inside a query parameter. So every
string and every number in every fixture — object keys, values and recording
FILE NAMES — is expanded into every form an identity can hide in before it is
matched:

  * URL- and percent-decoded, once and then again (a doubly-encoded value is
    still a value);
  * base64 and base64url decoded where the shape admits it;
  * split into its path and query components, and each query VALUE decoded and
    split again;
  * reduced to its calendar DAY where it is an instant, because a set whose
    dates were not shifted lines a published recording up with the owner's real
    week even when every id in it moved.

It also lists every URL HOST that is neither WHOOP's nor reserved, because a
host nobody expected is the shape a leak takes that no literal match catches.

    python3 providers/whoop/fixtures/audit.py            # exit 0 = clean
    python3 providers/whoop/fixtures/audit.py --verbose  # every hit

A finding is printed as a SALTED FINGERPRINT, never as the literal, so this
tool's own output is committable (`docs/provider-practices.md` §2).

Exit status is the number of findings, capped at 125.

TODAY'S CAVEAT, AND IT IS THE WHOLE OF T-017's GAP: `raw/whoop/` IS EMPTY. The
owner's stored WHOOP credential is a bare access token minted 2026-07-25 with
no refresh token beside it, so `tools/rawpull/whoop.py` answers 401 and no
pull exists. The committed fixtures are therefore SCHEMA-DERIVED, not a cut of
real payloads (see README.md in this directory) — there is no real corpus for
them to leak. This audit still runs against the persona map, and it is written
so that the day a pull lands it is the gate on the re-cut rather than something
that has to be invented then.
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
MAP = ROOT / "tools" / "persona.local.json"
RAW = ROOT / "raw" / "whoop"

# A literal is only a finding when it is SPECIFIC: three characters of a
# surname matching inside an opaque id is noise, not a leak. FOUR, and a bare
# word is specific at four too — a WHOOP corpus is a few hundred values, not
# GitHub's hundred thousand, so the noise a short name makes here is a handful
# of lines and the name it would otherwise miss is a person (Codex review,
# finding 12; GitHub's own audit needs six for the same reason reversed).
MIN_LITERAL = 4
BARE_WORD_MIN = 4
# Hosts a WHOOP fixture may name. Anything else is reported, whether or not it
# matches a literal: a host nobody expected names somebody.
KNOWN_HOSTS = {"api.prod.whoop.com", "prod.whoop.com", "whoop.com",
               "www.whoop.com", "support.whoop.com", "developer.whoop.com",
               "127.0.0.1", "localhost", "example.com", "zephyr-lane.example"}
# WHOOP's own payload vocabulary, plus the hosts and words the fixtures are
# allowed to carry. A literal that is one of these is not an identity even when
# a real word happens to collide with it.
ALLOW = {
    "whoop", "api.prod.whoop.com", "developer", "cycle", "cycles", "recovery",
    "sleep", "workout", "activity", "user", "profile", "basic", "measurement",
    "body", "records", "next_token", "nextToken", "limit", "start", "end",
    "score", "score_state", "scored", "pending_score", "unscorable", "true",
    "false", "null", "http", "https", "json", "com", "running", "cycling",
    "walking", "weightlifting", "padel", "other", "nap",
}


def fingerprint(value: str) -> str:
    """A finding, printed so the print itself is not a leak. Salted with the
    map's own salt, so the same real value fingerprints the same way in every
    tool's output and in none of them as itself."""
    salt = ""
    if MAP.exists():
        try:
            salt = str(json.loads(MAP.read_text()).get("salt") or "")
        except (OSError, ValueError):
            salt = ""
    return hashlib.sha256((salt + "|" + value).encode()).hexdigest()[:12]


def specific(lit: str) -> bool:
    v = str(lit).strip()
    if len(v) < MIN_LITERAL or v.lower() in ALLOW:
        return False
    if re.search(r"[@./_\- ]", v):
        return True
    return len(v) >= BARE_WORD_MIN


B64 = re.compile(r"^[A-Za-z0-9_\-+/]{8,}={0,2}$")


def expand(value: str) -> list[str]:
    """Every form the value could be hiding an identity in.

    The point is that a fixture leak is rarely a literal. It is a literal
    inside a percent-encoded query value inside a URL, or base64'd inside an
    opaque token. Each expansion is cheap and each one has caught something in
    another provider's set.
    """
    out: list[str] = []
    seen = {value}

    def push(v):
        if isinstance(v, str) and v and v not in seen:
            seen.add(v)
            out.append(v)

    try:
        once = urllib.parse.unquote(value)
        push(once)
        push(urllib.parse.unquote(once))
    except Exception:                                            # noqa: BLE001
        pass
    if B64.match(value.strip()):
        for decoder in (base64.b64decode, base64.urlsafe_b64decode):
            try:
                raw = decoder(value.strip() + "=" * (-len(value.strip()) % 4))
                push(raw.decode("utf-8", "strict"))
            except Exception:                                    # noqa: BLE001
                continue
    if "://" in value or value.startswith("//"):
        try:
            u = urllib.parse.urlsplit(value)
            push(u.netloc)
            push(u.path)
            for part in (u.path or "").split("/"):
                push(part)
            for k, v in urllib.parse.parse_qsl(u.query, keep_blank_values=True):
                push(k)
                push(v)
                push(urllib.parse.unquote(v))
        except Exception:                                        # noqa: BLE001
            pass
    return out


HOST_RE = re.compile(r"https?://([A-Za-z0-9.\-]+)")


def hosts_in(value: str) -> list[str]:
    return [h.lower() for h in HOST_RE.findall(value or "")]


def strings(node, path="", out=None):
    """Every string and every number in the document, with the path it sits at.
    Keys count: a real word hides in a key as easily as in a value."""
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
        out.append((path, repr(node)))
    return out


ISO_DAY = re.compile(r"\b(\d{4}-\d{2}-\d{2})\b")


def days(value: str) -> list[str]:
    """The calendar DAYS an instant names.

    A WHOOP recording is a diary. Shifting every id and leaving the dates is
    the leak nobody sees: the owner's real week is still in the file, and a
    night's start and end minutes are a fingerprint of one person on one date.
    So the day is scanned as a literal of its own.
    """
    return ISO_DAY.findall(value or "")


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
        for key in ("emails", "aliases", "github", "slack"):
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
    """Every identity-bearing value the RAW pull carries.

    For WHOOP that is a short list and every item on it is a handle back to one
    person: the profile's name and address, the member id that rides on every
    record, each resource's own id (the v2 UUID and the retired `v1_id`), the
    body measurements, and every INSTANT — reduced to its day, because the day
    is what joins a published file to a real week.
    """
    real: dict[str, str] = {}
    if not RAW.exists():
        return real

    def add(v, what):
        if v is None:
            return
        s = str(v).strip()
        if len(s) >= MIN_LITERAL:
            real.setdefault(s, what)

    def visit(node):
        if isinstance(node, dict):
            for key in ("email", "first_name", "last_name"):
                v = node.get(key)
                if isinstance(v, str) and v.strip():
                    add(v, "raw " + key)
                    if key == "email" and "@" in v:
                        add(v.split("@", 1)[0], "raw email local part")
                        add(v.split("@", 1)[1], "raw email domain")
            for key in ("user_id", "id", "cycle_id", "sleep_id", "v1_id"):
                if key in node:
                    add(node.get(key), "raw " + key)
            for key in ("height_meter", "weight_kilogram", "max_heart_rate"):
                if key in node:
                    add(node.get(key), "raw body " + key)
            for key in ("created_at", "updated_at", "start", "end"):
                v = node.get(key)
                if isinstance(v, str):
                    add(v, "raw instant " + key)
                    for day in days(v):
                        add(day, "raw DAY of " + key)
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
        except Exception:                                        # noqa: BLE001
            continue
    return real


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    mapped = from_map()
    real = dict(mapped)
    real.update(from_raw())
    real = {k: v for k, v in real.items() if specific(k)}
    if not real:
        print("audit: no real corpus (tools/persona.local.json and raw/whoop "
              "are gitignored) — nothing to check against", file=sys.stderr)
        return 1

    # Longest first, so a full address is reported as the address and not as
    # the domain inside it. Word boundaries, so a four-letter surname does not
    # match inside an opaque id.
    literals = sorted(real, key=len, reverse=True)
    rx = re.compile("|".join(r"(?<![0-9A-Za-z])%s(?![0-9A-Za-z])" % re.escape(x)
                             for x in literals), re.I)

    files = sorted(FIXTURES.glob("*.json"))
    findings: list[tuple[str, str, str, str]] = []
    scanned = 0
    strangers: set[tuple[str, str]] = set()
    for f in files:
        doc = json.loads(f.read_text())
        pairs = strings(doc) + [("<filename>", f.name)]
        # A value is not one value. An instant is also the day inside it; a URL
        # is also its host, its path segments and every decoded query value; an
        # opaque token may be base64 of something that reads.
        extra = []
        for path, v in pairs:
            if not isinstance(v, str):
                continue
            for day in days(v):
                extra.append((path + " (day)", day))
            for form in expand(v):
                extra.append((path + " (decoded)", form))
            for host in hosts_in(v):
                if host not in KNOWN_HOSTS:
                    strangers.add((f.name, host))
        for path, v in pairs + extra:
            scanned += 1
            for m in rx.finditer(v):
                hit = m.group(0)
                what = real.get(hit) or real.get(
                    next((k for k in literals if k.lower() == hit.lower()), hit),
                    "?")
                findings.append((f.name, path, fingerprint(hit), what))
    for name, host in sorted(strangers):
        findings.append((name, "<url host>", fingerprint(host),
                         "a host that is neither WHOOP's nor reserved"))

    seen, uniq = set(), []
    for row in findings:
        key = (row[0], row[2])
        if key in seen:
            continue
        seen.add(key)
        uniq.append(row)

    raw_count = len(real) - len([k for k in mapped if specific(k)])
    print("audit: %d fixtures, %d values scanned, %d real literals "
          "(%d from the map, %d from raw/whoop)"
          % (len(files), scanned, len(real),
             len([k for k in mapped if specific(k)]), max(raw_count, 0)))
    if not RAW.exists() or not any(RAW.glob("*.json")):
        print("audit: raw/whoop holds no pull — the committed fixtures are "
              "schema-derived (see README.md here and T-017), so only the "
              "persona map is a corpus to check against")
    if not uniq:
        print("audit: 0 findings — no real name, address, domain, member id, "
              "resource id, body measurement, instant or calendar day survives "
              "in any fixture body, key or file name")
        return 0
    for name, path, fp, what in (uniq if args.verbose else uniq[:40]):
        print("  %-48s %-40s %-14s %s" % (name, path[:40], fp, what))
    if not args.verbose and len(uniq) > 40:
        print("  ... %d more (--verbose)" % (len(uniq) - 40))
    print("audit: %d findings" % len(uniq))
    return min(len(uniq), 125)


if __name__ == "__main__":
    sys.exit(main())
