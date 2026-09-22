#!/usr/bin/env python3
"""The privacy audit for `providers/google/fixtures/`. It must return zero.

    python3 providers/google/fixtures/audit.py            # the committed set
    python3 providers/google/fixtures/audit.py --dir DIR  # any cut

A PLAIN GREP IS NOT AN AUDIT. Google encodes identity in at least four places
a text search cannot see, and the first pass over this set left the owner's
own address in 61 of them:

  * a Calendar `htmlLink` carries `eid=`, which is base64 of
    `"<eventId> <calendarId>"` — and a calendar id IS an address;
  * a Gmail body part is base64url, and a MIME header can be RFC 2047
    (`=?UTF-8?B?...?=`) or RFC 2231 (`name*=UTF-8''...`) encoded;
  * a conferencing join URL nests another whole URL, percent-encoded, in its
    query — so an address can hide two escapes deep;
  * a `tel:` / `sip:` entry point spells a dial-in without ever looking like
    a URL to a host-based rule.

So this DECODES FIRST and scans second, recursively, until nothing decodes
any further. It scans for TWO literal sets and reports every hit with its
file, its JSON path and the decoding chain that exposed it:

  1. every real literal in `tools/persona.local.json` — the owner's and the
     cast's names, first names, last names, aliases, addresses, logins, Slack
     ids, phone numbers, plus every real key of every word bucket (the real
     domains, companies, projects, channels, teams);
  2. every value the OWNER'S OWN RAW PULL holds — every address, every
     display name, every organisation name, every URL host and every PROSE
     phrase under `raw/google/*/`. The map only knows the cast; the raw pull
     knows everyone the owner ever mailed, and a fixture must not carry one
     of them either.

It also lists every URL host in the set that is neither Google's nor
reserved, because `<tenant>.zoom.us` was one and no rule was looking at it.

Both source files are gitignored. NOTHING REAL IS EVER PRINTED: a hit is
reported as its class, its length and a salted 8-hex fingerprint, so the
report is committable and a CI log is safe. `--reveal` prints the literal and
is for a human at a terminal, never for a file.

Exit status is the number of leaks, so `&& echo clean` means something.
"""

from __future__ import annotations

import argparse
import base64
import binascii
import hashlib
import json
import pathlib
import re
import sys
import urllib.parse
import zoneinfo

ROOT = pathlib.Path(__file__).resolve().parents[3]
FIXTURES = ROOT / "providers" / "google" / "fixtures"
MAP = ROOT / "tools" / "persona.local.json"
RAW = ROOT / "raw" / "google"

# A literal this short matches half the language; `ada`, `ops`, `uk` are not
# evidence of anything. The cast's real first names are checked at 4+, which
# is the same floor `pseudonymise.py`'s literal index uses.
MIN_LITERAL = 4

# A raw Gmail body is a megabyte of base64 in ONE string, and a regex like
# `[A-Za-z0-9._%+-]+@...` over it backtracks from every one of those
# positions: the first cut of this audit spent 200 seconds on a single file.
# Both scanners below are hand-written and LINEAR — they seek the `@` or the
# `://` and walk outwards — so the cost is the corpus's length, once.
LOCAL_CHARS = set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
                  "0123456789._%+-'")
DOMAIN_CHARS = set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
                   "0123456789.-")
SCHEMES = ("https://", "http://", "sip:", "sips:", "tel:", "wss://", "ws://")

B64_RE = re.compile(r"^[A-Za-z0-9_\-+/]{16,}={0,2}$")
RFC2047_RE = re.compile(r"=\?([^?]+)\?([BbQq])\?([^?]*)\?=")
RFC2231_RE = re.compile(r"[*]=\s*([^']*)'([^']*)'([^\s;]+)")
TOKEN_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._%+'@-]*")
WORD_RE = re.compile(r"[a-z][a-z']*")

# Hosts that are structurally Google's or reserved, and can never be a
# person. Everything else that appears as a host in a fixture is REPORTED as
# an unmapped host, because `<tenant>.zoom.us` was one and the plain grep
# never saw it.
PUBLIC_HOSTS = {
    "google.com", "www.google.com", "googleapis.com", "people.googleapis.com",
    "gmail.com", "calendar.google.com", "group.calendar.google.com",
    "mail.google.com", "drive.google.com", "docs.google.com",
    "accounts.google.com", "meet.google.com", "localhost", "127.0.0.1",
    "example.com", "example.org", "example.net", "example",
    "invalid", "schemas.microsoft.com", "www.w3.org",
}

# A domain EVERYBODY is at is not evidence that anybody is: `gmail.com` in a
# fixture says nothing, while `gmail.com` as a literal makes every address in
# the set a hit. The full ADDRESS at one of these is still a literal — only
# the bare domain is dropped.
PUBLIC_DOMAINS = PUBLIC_HOSTS | {
    "googlemail.com", "outlook.com", "hotmail.com", "icloud.com", "yahoo.com",
    "me.com", "live.com", "protonmail.com", "proton.me", "pm.me", "aol.com",
    "msn.com", "mac.com", "googlegroups.com", "resource.calendar.google.com",
}

# A VENDOR'S OWN HOST NAMES A SERVICE, NOT A PERSON. `zoom.us` in a fixture
# says only that somebody used Zoom; `<tenant>.zoom.us` names the
# organisation that pays for it, and a tenant subdomain is NOT in this set,
# so it is still harvested and still a leak. Same rule for a CDN: the host is
# Google's, the opaque path is the identity, and the path is what the
# pseudonymiser replaces.
VENDOR_HOSTS = {
    "zoom.us", "zoomcrc.com", "applications.zoom.us", "us02web.zoom.us",
    "facebook.com", "www.facebook.com", "m.facebook.com", "fb.com",
    "twitter.com", "x.com", "www.linkedin.com", "linkedin.com",
    "instagram.com", "www.instagram.com", "github.com", "www.github.com",
    "youtube.com", "www.youtube.com", "youtu.be", "teams.microsoft.com",
    "zoom.com", "webex.com", "gotomeeting.com", "whereby.com",
    "lh3.googleusercontent.com", "googleusercontent.com",
}

# The API's own vocabulary. A contact whose display name happens to be one of
# these words would be missed; every JSON key in the corpus would otherwise
# be a hit, which is worse. Keys are scanned against the MAP's literals only,
# for the same reason: a key is Google's word, not a person's.
API_WORDS = {
    "email", "name", "date", "start", "end", "value", "type", "photo", "url",
    "text", "html", "home", "work", "other", "mobile", "main", "person",
    "contact", "profile", "google", "calendar", "gmail", "event", "message",
    "thread", "label", "part", "body", "data", "size", "note", "notes",
    "title", "status", "phone", "address", "birthday", "nickname", "custom",
    # T-031. `people` is the People API's OWN path noun — every resourceName
    # in the corpus is `people/<id>` — and a Workspace directory has a team
    # called "People" and one called "Directory", so harvesting either as a
    # real literal makes EVERY resourceName in EVERY fixture a hit. It did:
    # 34 of them, all one fingerprint, all the six characters before the
    # slash. The trade is the one this set already makes for `person`,
    # `contact` and `profile`: a human whose display name is exactly one of
    # these words would be missed.
    "people", "directory",
    # T-023. The raw pull records `users.labels.list` and `contactGroups.list`
    # now, and a GMAIL SYSTEM LABEL and a SYSTEM CONTACT GROUP are Google's
    # own vocabulary in a `name` field — so the harvester took them for
    # people's names, and one five-letter system label then matched generated
    # fake prose in the fixtures four times over. Both sets are closed and
    # documented, so they are named rather than guessed at.
    "inbox", "sent", "draft", "drafts", "trash", "spam", "unread", "starred",
    "important", "chat", "chats", "scheduled", "snoozed", "forums", "updates",
    "promotions", "social", "reserved", "personal", "categorypersonal",
    "categorysocial", "categorypromotions", "categoryupdates",
    "categoryforums", "category_personal", "category_social",
    "category_promotions", "category_updates", "category_forums",
    "mycontacts", "chatbuddies", "friends", "family", "coworkers", "blocked",
    "all", "contacts",
}


# AN IANA ZONE NAME IS A PUBLIC VOCABULARY, NOT AN IDENTITY (T-055). A
# `timeZone` is Google's own closed list of tz database names; the
# pseudonymiser leaves it alone, and correctly — remapping it would change
# every instant on the record. The harvester, though, reads `New York` out of
# the owner's prose (an office location) and then matches it inside
# `America/New_York`, so the first fixture set whose specimens carried that
# zone reported six leaks that were the tz database. Skipped by KEY and only
# when the value LOADS as a zone, so a display name that happens to look like
# one is still scanned.
ZONE_KEYS = ("timezone",)


def is_zone(key: str, value: str) -> bool:
    if key.casefold() not in ZONE_KEYS or not value or len(value) > 40:
        return False
    try:
        zoneinfo.ZoneInfo(value)
    except Exception:
        return False
    return True


def fingerprint(s: str) -> str:
    return hashlib.sha256(("audit\x1f" + s).encode("utf-8", "replace")).hexdigest()[:8]


# --------------------------------------------------------------- the scanners


def emails_in(text: str):
    """Every address in `text`, in linear time."""
    start = 0
    while True:
        i = text.find("@", start)
        if i < 0:
            return
        j = i - 1
        while j >= 0 and text[j] in LOCAL_CHARS:
            j -= 1
        k = i + 1
        while k < len(text) and text[k] in DOMAIN_CHARS:
            k += 1
        local, dom = text[j + 1:i], text[i + 1:k].rstrip(".")
        tld = dom.rsplit(".", 1)[-1] if "." in dom else ""
        if local and len(tld) >= 2 and tld.isalpha():
            yield local + "@" + dom
        start = max(k, i + 1)


def hosts_in(text: str):
    """Every URL host in `text`, in linear time, `tel:`/`sip:` included."""
    low = text.lower()
    for scheme in SCHEMES:
        start = 0
        while True:
            i = low.find(scheme, start)
            if i < 0:
                break
            k = i + len(scheme)
            while k < len(text) and text[k] not in "/?#\t\n\r \"'<>,;":
                k += 1
            host = text[i + len(scheme):k]
            start = max(k, i + 1)
            if scheme in ("tel:", "sip:", "sips:"):
                host = host.split("@")[-1]
                if not host or host[0].isdigit() or host.startswith("+"):
                    continue
            host = host.split("@")[-1].split(":")[0].lower().strip(".")
            if host and "." in host:
                yield host


# --------------------------------------------------------------- the decoders


def _b64(s: str) -> str | None:
    if not B64_RE.match(s.strip()):
        return None
    t = s.strip().replace("-", "+").replace("_", "/")
    try:
        raw = base64.b64decode(t + "=" * (-len(t) % 4))
    except (binascii.Error, ValueError):
        return None
    try:
        out = raw.decode("utf-8")
    except UnicodeDecodeError:
        out = raw.decode("latin-1", "replace")
    # A decode that yields mostly control bytes is binary, not text; scanning
    # it for names is noise. Anything printable is scanned.
    printable = sum(1 for c in out if c.isprintable() or c in "\r\n\t")
    if not out or printable / len(out) < 0.8:
        return None
    return out


def _one_level(value: str) -> list[tuple[str, str]]:
    """Every plaintext one decoding step turns `value` into."""
    out: list[tuple[str, str]] = []
    seen = {value}

    def emit(text: str, how: str):
        if text and text not in seen:
            seen.add(text)
            out.append((text, how))

    # percent-encoding, including a doubly-escaped nested URL
    if "%" in value:
        unq = urllib.parse.unquote(value)
        emit(unq, "urldecode")
        emit(urllib.parse.unquote(unq), "urldecode x2")

    # every query value of every URL in the string, and the `eid` decode
    if "?" in value and "://" in value:
        try:
            q = urllib.parse.urlsplit(value).query
        except ValueError:
            q = ""
        for key, vals in urllib.parse.parse_qs(q, keep_blank_values=True).items():
            for v in vals:
                emit(v, "query[%s]" % key)
                d = _b64(v)
                if d:
                    emit(d, "query[%s]+base64" % key)

    # a bare base64 / base64url blob (a Gmail body part, an `eid`)
    d = _b64(value)
    if d:
        emit(d, "base64")

    # RFC 2047 encoded-words in a MIME header, and RFC 2231 filenames
    if "=?" in value:
        for m in RFC2047_RE.finditer(value):
            charset, enc, payload = m.groups()
            try:
                if enc.lower() == "b":
                    text = base64.b64decode(payload + "=" * (-len(payload) % 4))
                else:
                    text = re.sub(
                        rb"=([0-9A-Fa-f]{2})",
                        lambda x: bytes([int(x.group(1), 16)]),
                        payload.replace("_", " ").encode("latin-1"))
                emit(text.decode(charset, "replace"), "rfc2047")
            except (binascii.Error, ValueError, LookupError, UnicodeDecodeError):
                continue
    if "*=" in value:
        for m in RFC2231_RE.finditer(value):
            charset, _lang, payload = m.groups()
            try:
                emit(urllib.parse.unquote(payload, encoding=charset or "utf-8"),
                     "rfc2231")
            except LookupError:
                continue
    return out


def decodings(value: str, depth: int = 0):
    """Every plaintext `value` can be turned into, with the chain that got
    there. Recursive: a query value inside a percent-encoded URL inside a
    base64 blob is reached."""
    if depth > 4 or not value:
        return
    for text, how in _one_level(value):
        yield text, how
        for sub, how2 in decodings(text, depth + 1):
            yield sub, how + " -> " + how2


# ------------------------------------------------------------- the literal set


def real_literals() -> dict[str, str]:
    """real literal -> its class. From the pseudonym map: the cast."""
    if not MAP.exists():
        sys.exit("audit: no %s — the map is what the audit scans FOR." % MAP)
    m = json.loads(MAP.read_text())
    out: dict[str, str] = {}

    def add(value, klass):
        if not isinstance(value, str):
            return
        v = value.strip()
        if len(v) < MIN_LITERAL:
            return
        out.setdefault(v.casefold(), klass)

    owner = m.get("owner")
    for person in [owner] + list(m.get("people") or []):
        if not person:
            continue
        real = person.get("real") or {}
        klass = "owner" if person is owner else "cast"
        for key in ("name", "first", "last"):
            add(real.get(key), klass + " name")
        for key, k2 in (("emails", "address"), ("aliases", "alias"),
                        ("slack", "slack id"), ("github", "login"),
                        ("phones", "phone"), ("slackTeam", "slack team")):
            for v in real.get(key) or []:
                add(v, klass + " " + k2)
                if k2 == "address" and "@" in v:
                    dom = v.split("@", 1)[1].lower()
                    if dom not in PUBLIC_DOMAINS and dom not in VENDOR_HOSTS:
                        add(dom, klass + " address domain")
    for bucket, table in (m.get("buckets") or {}).items():
        for real_key in table:
            if real_key.lower() in PUBLIC_DOMAINS or real_key.lower() in VENDOR_HOSTS:
                continue          # `google.com` is a bucket key AND a public host
            add(real_key, "bucket:" + bucket)
    for pin in (m.get("pins") or {}).values():
        add(pin.get("match"), "pin match")
        for key, vals in (pin.get("real") or {}).items():
            for v in (vals if isinstance(vals, list) else [vals]):
                add(v, "pin " + key)
    return out


def raw_literals() -> dict[str, str]:
    """real literal -> its class. From the OWNER'S RAW PULL: everyone.

    The map holds the cast; the pull holds every address the owner ever
    mailed, every contact's display name and every host they linked to. A
    fixture may carry none of them, whether or not the map ever heard of the
    person.
    """
    out: dict[str, str] = {}
    if not RAW.is_dir():
        return out
    # A key whose value IS a person's or an organisation's name. Every one of
    # these is harvested WHOLE, single word included: a surname is a leak.
    name_keys = {"displayName", "givenName", "familyName", "middleName",
                 "displayNameLastFirst", "unstructuredName", "phoneticFullName",
                 "phoneticGivenName", "phoneticFamilyName", "honorificPrefix",
                 "organizations.name", "organizations.department",
                 "nicknames.value", "name"}
    # A key whose value is PROSE. A single word out of a sentence is not
    # evidence — `review` and `cluster` are in the generated vocabulary too —
    # so prose is harvested as a PHRASE and matched only whole. That is what
    # "a fixture must not leak a sentence" actually means, and it caught a
    # real meeting title the single-word pass drowned in noise.
    phrase_keys = {"summary", "description", "subject", "snippet", "location",
                   "title", "notes", "comment", "label", "text", "value",
                   "biographies.value", "userDefined.value", "declineMessage",
                   "summaryOverride", "jobDescription", "filename"}

    def add(value: str, klass: str):
        v = (value or "").strip()
        if len(v) < MIN_LITERAL:
            return
        out.setdefault(v.casefold(), klass)

    def walk(node, key="", parent=""):
        if isinstance(node, dict):
            for k, v in node.items():
                walk(v, k, key)
        elif isinstance(node, list):
            for v in node:
                walk(v, key, parent)
        elif isinstance(node, str):
            for addr in emails_in(node):
                add(addr, "raw address")
                dom = addr.split("@", 1)[1].lower()
                if dom not in PUBLIC_DOMAINS and dom not in VENDOR_HOSTS:
                    add(dom, "raw address domain")
            for host in hosts_in(node):
                if host not in PUBLIC_HOSTS and host not in VENDOR_HOSTS \
                        and not host.endswith(
                            (".googleapis.com", ".google.com", ".example",
                             ".example.com", ".gstatic.com",
                             ".googleusercontent.com")):
                    add(host, "raw host")
            if parent == "headers":
                # a `headers[]` entry is `{name, value}`: the NAME is
                # `Content-Type` and the VALUE is MIME grammar. Harvesting
                # either makes every header in the corpus a literal.
                return
            if (key in name_keys or parent + "." + key in name_keys) \
                    and 5 <= len(node) <= 60 and "@" not in node \
                    and node.casefold() not in API_WORDS \
                    and (" " in node or not node.islower()) \
                    and not node.startswith(("http", "people/", "contactGroups/")):
                # a proper noun has a space or a capital; a bare lowercase
                # word is Google's vocabulary, not somebody's name
                add(node, "raw name")
            elif (key in phrase_keys or parent + "." + key in phrase_keys) \
                    and 8 <= len(node) <= 160 and "\n" not in node \
                    and "://" not in node and not node.startswith("http"):
                words = WORD_RE.findall(node.casefold())
                if 2 <= len(words) <= 12:
                    add(" ".join(words), "raw prose")
                elif len(words) > 12:
                    add(" ".join(words[:8]), "raw prose (opening)")

    for f in sorted(RAW.rglob("*.json")):
        if ".fixtures-src" in f.parts:
            continue                     # the cut, not the pull
        try:
            walk(json.loads(f.read_text()))
        except (json.JSONDecodeError, UnicodeDecodeError):
            continue
    return out


# -------------------------------------------------------------------- the scan


def candidates(text: str):
    """Every substring of `text` a real literal could be, cheaply.

    A literal alternation over a few thousand literals is a backtracking
    regex over every string in the set, which takes minutes. This flips it:
    the TEXT is tokenised and each token is looked up, so the cost is the
    text's length and nothing else. A token is also yielded split at its `@`
    and with each leading domain label stripped, so `acme.example` is
    found inside `ada@acme.example` and `zoom.example` inside
    `acme.zoom.example` — which is how a host hides from a set lookup.
    """
    for m in TOKEN_RE.finditer(text):
        tok = m.group(0).casefold().strip(".-_'")
        if len(tok) < MIN_LITERAL:
            continue
        yield tok
        if "@" in tok:
            local, _, dom = tok.partition("@")
            if len(local) >= MIN_LITERAL:
                yield local
            tok = dom
            if len(tok) >= MIN_LITERAL:
                yield tok
        while tok.count(".") > 1:
            tok = tok.split(".", 1)[1]
            if len(tok) >= MIN_LITERAL:
                yield tok


def scan(directory: pathlib.Path, literals: dict[str, str], reveal: bool):
    hits, hosts, scanned, decoded = [], {}, 0, 0
    exact = {k: v for k, v in literals.items() if " " not in k}
    phrases = {k: v for k, v in literals.items() if " " in k}
    span = max((len(k.split()) for k in phrases), default=0)

    def check(text: str, where: str, how: str, keys_only: bool = False):
        found = {}
        for tok in candidates(text):
            if tok in exact:
                found[tok] = exact[tok]
        if phrases:
            words = WORD_RE.findall(text.casefold())
            for i in range(len(words)):
                for n in range(2, min(span, len(words) - i) + 1):
                    p = " ".join(words[i:i + n])
                    if p in phrases:
                        found[p] = phrases[p]
        for lit, klass in found.items():
            # A JSON KEY is Google's word, never a person's: scanning keys
            # against the raw pull's harvest turns every `name`/`value` in
            # the API into a hit. Keys are checked against the MAP only.
            if keys_only and klass.startswith("raw"):
                continue
            hits.append({"file": where.split("::")[0], "path": where.split("::")[1],
                         "via": how or "plain", "class": klass,
                         "len": len(lit), "fp": fingerprint(lit),
                         **({"literal": lit} if reveal else {})})

    def walk(node, path, where):
        nonlocal scanned, decoded
        if isinstance(node, dict):
            for k, v in node.items():
                check(k, "%s::%s" % (where, path), "key", keys_only=True)
                walk(v, "%s.%s" % (path, k) if path else k, where)
        elif isinstance(node, list):
            for i, v in enumerate(node):
                walk(v, "%s[%d]" % (path, i), where)
        elif isinstance(node, str):
            scanned += 1
            if is_zone(path.rsplit(".", 1)[-1], node):
                return
            check(node, "%s::%s" % (where, path), "")
            for text, how in decodings(node):
                decoded += 1
                check(text, "%s::%s" % (where, path), how)
            for host in hosts_in(node):
                if host in PUBLIC_HOSTS or host.endswith(
                        (".example", ".example.com", ".googleapis.com",
                         ".google.com", ".gstatic.com", ".googleusercontent.com")):
                    continue
                hosts.setdefault(host, []).append(where.split("::")[0])

    files = sorted(p for p in directory.glob("*.json") if p.name != "expectations.json")
    for f in files + [directory / "expectations.json"]:
        if not f.exists():
            continue
        # The recording's NAME is a request path and carries identity too.
        check(f.name, "%s::<filename>" % f.name, "filename")
        for text, how in decodings(f.name):
            check(text, "%s::<filename>" % f.name, "filename " + how)
        walk(json.loads(f.read_text()), "", f.name)
    return hits, hosts, scanned, decoded, len(files)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--dir", type=pathlib.Path, default=FIXTURES)
    ap.add_argument("--reveal", action="store_true",
                    help="print the literal itself. A TERMINAL ONLY: never "
                         "redirect this to a file in the repository.")
    ap.add_argument("--json", action="store_true")
    a = ap.parse_args()

    lit = real_literals()
    raw = raw_literals()
    merged = dict(raw)
    merged.update(lit)              # the map's class wins where both know one
    # Anything the COMMITTED cast already spells is fake by construction: an
    # invented persona's surname can collide with a real one, and a hit on a
    # value this repository publishes on purpose is noise, not a leak.
    public = ROOT / "providers" / "persona.public.json"
    if public.exists():
        fakes = set()

        def harvest(node):
            if isinstance(node, dict):
                for v in node.values():
                    harvest(v)
            elif isinstance(node, list):
                for v in node:
                    harvest(v)
            elif isinstance(node, str) and len(node) >= MIN_LITERAL:
                fakes.add(node.casefold())
        harvest(json.loads(public.read_text()))
        merged = {k: v for k, v in merged.items() if k not in fakes}

    hits, hosts, scanned, decoded, files = scan(a.dir, merged, a.reveal)

    if a.json:
        print(json.dumps({"leaks": hits, "unmappedHosts": hosts,
                          "files": files, "strings": scanned,
                          "decodings": decoded}, indent=1))
        return len(hits)

    print("fixture privacy audit: %s" % a.dir.relative_to(ROOT))
    print("  %d recordings, %d strings, %d decoded views "
          "(eid, base64url MIME, rfc2047/2231, nested percent-encoded URLs)"
          % (files, scanned, decoded))
    print("  scanned against %d real literals from tools/persona.local.json "
          "and %d from raw/google/*/" % (len(lit), len(raw)))
    if hosts:
        print("\n  %d host(s) that are neither Google's nor reserved — check "
              "each one is a mapped fake:" % len(hosts))
        for h, where in sorted(hosts.items()):
            print("    %-48s %d file(s)" % (h, len(set(where))))
    if not hits:
        print("\n  0 leaks. Clean.")
        return 0
    print("\n  %d LEAK(S):" % len(hits))
    for h in hits:
        print("    %-52s %-30s via %-24s %s len=%d fp=%s%s"
              % (h["file"][:52], h["path"][:30], h["via"][:24], h["class"],
                 h["len"], h["fp"], "  " + h.get("literal", "")))
    return len(hits)


if __name__ == "__main__":
    sys.exit(main())
