#!/usr/bin/env python3
"""Publication audit for the Beeper fixture set.

This tree is public. The pseudonymiser is the thing that makes it safe, and
this is the thing that proves it: every scalar leaf of every recording, every
MAP KEY, every recording NAME, every URL path and query component, every
base64 token that decodes to text, checked against every real literal we know
of. A finding is a real literal surviving anywhere in the published tree, and
zero is the only acceptable result.

    python3 providers/beeper/fixtures/audit.py            # -> exit 0 on zero
    python3 providers/beeper/fixtures/audit.py --verbose  # every occurrence

TWO SOURCES OF TRUTH FOR "REAL":

1.  `tools/persona.local.json` — the pinned cast's real names, addresses,
    logins, phone numbers and the real words behind every bucket, plus
    `tools/persona.learned.local.json`, whose KEYS are every real value the
    pseudonymiser ever mapped, plus this directory's `cast.local.json`, whose
    values are the owner's real chat ids. All three are gitignored.
2.  **the raw pull itself** (`raw/beeper/`), harvested for the classes a
    name-and-prose map never held: every MXID and every part of one (the
    bridge-scoped network id, the bare Beeper handle), every room id and its
    localpart, every phone number, every media handle and every path segment
    inside one, every contact's name and username, and the exact text of every
    message body, chat title, group description, draft and transcription.

WHY THE MAP KEYS GET THEIR OWN PASS. `tools/pseudonymise.py` walks VALUES, and
Beeper is the one provider here whose payload keys are identities:
`Message.seen` is keyed by participant id and `ChatDraft.attachments` by
attachment id. The first cut published 47 real MXIDs that way, every one of
them a dictionary key no value-walker could see. They are re-keyed by
`tools/rawpull/beeper.py --repair`, and audited here as leaves in their own
right so the repair cannot quietly stop running.

WHAT IS NOT A FINDING, and why each is named rather than left to silence:

  * a `sortKey`, an `oldestCursor`, a `newestCursor` and a numeric message
    `id`. They are per-install counters that name nobody, and the mirror's
    references, the read marker and every page boundary are keyed on them —
    moving them would break the fixtures without making them safer.
  * a bridge slug (`whatsapp`, `discordgo`, `local-signal`), a homeserver
    (`beeper.local`, `beeper.com`), a mime type, an enum value, an instant.
    Beeper's own vocabulary, which the bundle's enums and the e2e read.
  * a word `tools/persona.py` can itself INVENT. A fixture is made of those.

A finding is printed as a SALTED FINGERPRINT, never as the literal, so this
script's own output is committable.
"""

from __future__ import annotations

import argparse
import base64
import binascii
import collections
import hashlib
import json
import pathlib
import re
import sys
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parents[3]
RAW = ROOT / "raw" / "beeper"
FIXTURES = pathlib.Path(__file__).resolve().parent
MAPS = [ROOT / "tools" / "persona.local.json",
        ROOT / "tools" / "persona.learned.local.json",
        FIXTURES / "cast.local.json"]

# Beeper's identities, in the two shapes Matrix gives them.
MXID = re.compile(r"@([A-Za-z0-9._=/+-]+):([A-Za-z0-9.-]+)")
ROOMID = re.compile(r"!([A-Za-z0-9._=/+-]+):([A-Za-z0-9.-]+)")
UUID = re.compile(r"\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b", re.I)
HEXISH = re.compile(r"\b[0-9a-f]{8,}\b", re.I)
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
PHONE = re.compile(r"\+?\d[\d \-().]{7,}\d")
B64ISH = re.compile(r"^[A-Za-z0-9+/_-]{8,}={0,2}$")
_ENUMISH = re.compile(r"^[A-Z][A-Z0-9_]*$")
_ISO_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}([T ].*)?$")
_DIGITS = re.compile(r"^\d+$")

# Hosts whose NAME is not an identity: Beeper's own, and the open web's
# grammar. Their PATHS are still audited — a public host with a private
# resource id in its path is exactly the leak this exists to catch.
PUBLIC_HOSTS = {
    "beeper.com", "beeper.local", "local.beeper.com", "matrix.beeper.com",
    "matrix.org", "localhost", "example.com", "example.org", "example.net",
}

# The API's OWN vocabulary. Every one of these is a value the bundle's enum
# tables, the sync's dispatch or the e2e's assertions read, and the
# pseudonymiser keeps them on purpose.
BEEPER_VOCABULARY = {
    "beeper", "matrix", "whatsapp", "telegram", "signal", "discord",
    "discordgo", "facebook", "facebookgo", "instagram", "instagramgo",
    "slack", "slackgo", "twitter", "imessage", "googlechat", "linkedin",
    "local", "cloud", "self-hosted", "platform-sdk", "single", "group",
    "connected", "connecting", "backfilling", "disconnected", "disabled",
    "available", "unknown", "success", "pending", "room", "text", "notice",
    "image", "video", "voice", "audio", "file", "sticker", "location",
    "reaction", "users", "library", "application", "support", "beepertexts",
    "media", "desktop", "automattic", "darwin", "arm64", "whisper",
}

# The paths whose value is a per-install COUNTER: a sort key, a page cursor,
# a message id, the local chat id. The fixtures keep those deliberately, so a
# digit-only needle is not compared against them — and IS compared against
# everything else, because a Facebook user id and a WhatsApp phone-as-id are
# digit-only too (Codex review).
COUNTER_KEYS = {"sortKey", "oldestCursor", "newestCursor", "cursor",
                "lastReadMessageSortKey", "localChatID", "id", "linkedMessageID"}

# A word that is language, not identity: a real message body is prose and its
# individual words must not each become a needle.
_STOP = {
    "https", "http", "null", "false", "true", "message", "messages", "chat",
    "chats", "account", "accounts", "bridge", "bridges", "participant",
    "participants", "member", "members", "label", "labels", "attachment",
    "attachments", "download", "downloads", "avatar", "avatars", "profile",
    "index", "default", "standard", "everyone", "english", "example",
    "status", "online", "offline", "active", "thread", "threads", "emoji",
    "london", "europe", "america", "asia", "africa", "australia", "pacific",
    "atlantic", "indian", "antarctica", "mountain", "eastern", "western",
    "central", "summer", "daylight", "british",
} | BEEPER_VOCABULARY
_STOP = {w.casefold() for w in _STOP}

_SALT = hashlib.sha256(
    (MAPS[0].read_text() if MAPS[0].exists() else "beeper-audit")
    .encode("utf-8")).digest()


def fingerprint(v: str) -> str:
    """A finding, printed so this script's own output is committable."""
    return hashlib.blake2s(v.encode("utf-8"), key=_SALT[:32],
                           digest_size=5).hexdigest()


# ------------------------------------------------------------------- walking


def leaves(node, path=""):
    """Every scalar leaf with its path — AND every map KEY, which is where
    Beeper hides an identity no value-walker would see."""
    if isinstance(node, dict):
        for k, v in node.items():
            here = (path + "." + k).lstrip(".")
            if isinstance(k, str):
                yield here + "<key>", k
            yield from leaves(v, here)
    elif isinstance(node, list):
        for i, v in enumerate(node):
            yield from leaves(v, "%s[%d]" % (path, i))
    else:
        yield path, node


def decode_b64(s: str) -> str | None:
    """Base64 is an encoding, not anonymisation, so the audit decodes before
    it compares."""
    if not (isinstance(s, str) and len(s) >= 8 and B64ISH.fullmatch(s)):
        return None
    for fn in (base64.urlsafe_b64decode, base64.b64decode):
        try:
            out = fn(s + "=" * (-len(s) % 4))
        except (binascii.Error, ValueError):
            continue
        try:
            text = out.decode("utf-8")
        except UnicodeDecodeError:
            continue
        if text and all(32 <= ord(c) < 127 for c in text):
            return text
    return None


def expand(value: str) -> tuple[set[str], set[str]]:
    """Every form a leaf can hide an identity in.

    Returns `(fine, coarse)`. **coarse** is the value itself, its URL-decoded
    self, its base64 plaintext and its delimiter-separated components with
    their internal `-`/`_`/`.` intact. **fine** adds every sub-word of those,
    which is what catches a network id fused into a media filename or an MXID
    localpart. A weak (dictionary-shaped) needle is only ever compared against
    **coarse**, or every English word of a generated sentence is a finding."""
    fine, coarse, queue, seen = set(), set(), [value], set()
    while queue:
        s = queue.pop()
        if not isinstance(s, str) or not s or s in seen or len(seen) > 600:
            continue
        seen.add(s)
        coarse.add(s)
        fine.add(s)
        dec = decode_b64(s)
        if dec and dec != s:
            queue.append(dec)
        if "%" in s:
            try:
                unq = urllib.parse.unquote(s)
                if unq != s:
                    queue.append(unq)
            except Exception:  # noqa: BLE001 — a malformed escape is just text
                pass
        if len(s) > 3 and re.search(r"[^A-Za-z0-9]", s):
            for part in re.split(r"[^A-Za-z0-9_.+-]+", s):
                if part and part != s:
                    coarse.add(part)
                    fine.add(part)
                    if "_" in part or "." in part or "-" in part:
                        queue.append(part)
            for part in re.split(r"[^A-Za-z0-9]+", s):
                if part:
                    fine.add(part)
    return fine, coarse


# -------------------------------------------------------------------- shapes


def is_strong(v: str) -> bool:
    """An identifier: a shape no sentence produces. Compared against every
    expanded component of every leaf and every key."""
    if len(v) < 5 or _ISO_DATE.fullmatch(v):
        return False
    if v.casefold() in _STOP:
        return False
    # A bare run of digits is a sortKey, a cursor or a message id — a
    # per-install counter, which the fixtures keep deliberately.
    if _DIGITS.fullmatch(v):
        return False
    if EMAIL.fullmatch(v) or UUID.fullmatch(v):
        return True
    if HEXISH.fullmatch(v) and len(v) >= 10:
        return True
    if len(v) >= 12 and B64ISH.fullmatch(v) and re.search(r"\d", v) \
            and re.search(r"[A-Za-z]", v):
        return True
    if len(v) >= 6 and re.search(r"\d", v) and re.search(r"[A-Za-z]", v) \
            and not _ENUMISH.fullmatch(v):
        return True
    return False


SLUGISH = re.compile(r"^(?:local-)?[a-z][a-z0-9]*$")
HOSTNAME = re.compile(r"^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$", re.I)


def is_vocabulary(v: str, hard: set[str] | None = None) -> bool:
    """A value that is the PLATFORM's, not the owner's. Named as a class, so
    the exclusions are auditable rather than a growing stop-list:

      * a Beeper homeserver or media host (`beeper.local`, `local.beeper.com`)
        and anything under one;
      * a bridge slug (`whatsapp`, `local-signal`, `gmessages`) — Beeper ships
        these and the bundle's own descriptions name them;
      * `20Support`, which is what `Application%20Support` decomposes to and
        is a macOS directory, not a person.
    """
    s = v.casefold().strip()
    # A PINNED LITERAL IS NEVER PLATFORM VOCABULARY. The owner's own handle is
    # lowercase and alphanumeric, which is exactly the shape of a bridge slug —
    # so the shape tests below would have excused it as Beeper's own word and
    # taken it out of the needle set entirely (Codex review).
    if hard and v.strip() in hard:
        return False
    if s in _STOP or s in PUBLIC_HOSTS or s == "20support":
        return True
    if "." in s and s.split(".", 1)[-1] in PUBLIC_HOSTS:
        return True
    if SLUGISH.match(s):
        return True
    # The desktop app's own loopback endpoints, which `/v1/info` returns and
    # `config.apiBase` defaults to. Documented in the bundle; not the owner's.
    if s.startswith("http://localhost") or s.startswith("https://localhost") \
            or s.startswith("http://127.0.0.1"):
        return True
    # A BARE HOSTNAME is reported, not failed on. Hosts are the one class
    # where a blanket needle is useless — `github.com` is the open web and
    # `acme.example.com` is a tenant — so which hosts survived is printed by
    # `foreign_hosts` for a human to read, and only the PATH under one is
    # audited as identity (which is what `beeper_link` moves).
    if HOSTNAME.fullmatch(s):
        return True
    return False


def is_weak(v: str) -> bool:
    """A word that names somebody or something of the owner's — a person, a
    group, a handle, a host label. Compared only against whole leaves and
    whole delimiter-separated components."""
    if len(v) < 5 or _ENUMISH.fullmatch(v) or _ISO_DATE.fullmatch(v):
        return False
    if v.casefold() in _STOP:
        return False
    return bool(re.fullmatch(r"[A-Za-z][A-Za-z0-9 ._'-]{3,}", v))


# ------------------------------------------------------------------- needles


def from_maps() -> tuple[set[str], set[str]]:
    """Every real literal the persona maps and the cast file hold.

    Returns `(hard, soft)`. **hard** is the pinned cast's own real values,
    which are real by construction and never filtered. **soft** is every other
    map key: real words the pseudonymiser learned, but also, unavoidably,
    words the GENERATOR produced and then re-mapped idempotently."""
    hard, soft = set(), set()

    def add(v, target=None):
        if not isinstance(v, str):
            return
        v = v.strip()
        if len(v) >= 3:
            (hard if target is None else target).add(v)

    for path in MAPS:
        if not path.exists():
            continue
        d = json.loads(path.read_text())
        if "people" in d:                                # persona.local.json
            for e in [d.get("owner") or {}] + list(d.get("people") or []):
                for v in (e.get("real") or {}).values():
                    for x in (v if isinstance(v, list) else [v]):
                        add(x)
            for bucket in (d.get("buckets") or {}).values():
                for k in bucket:
                    add(k, soft)
            for pin in (d.get("pins") or {}).values():
                for v in (pin.get("real") or {}).values():
                    for x in (v if isinstance(v, list) else [v]):
                        add(x)
        elif "cast" in d:                                # fixtures/cast.local
            for row in d.get("cast") or []:
                cid = row.get("id") or ""
                add(cid)
                m = ROOMID.fullmatch(cid) or MXID.fullmatch(cid)
                if m:
                    add(m.group(1))
        else:                                            # persona.learned
            for name, bucket in d.items():
                if name == "person":
                    for k in bucket:
                        add(k.split(":", 1)[-1], soft)
                    continue
                for k in bucket:
                    add(str(k), soft)
    return hard, soft


def persona_output() -> set[str]:
    """Every value the persona map DELIBERATELY produces. These are the
    fixture's own vocabulary: one of them appearing is the pseudonymiser
    working, not a leak."""
    out: set[str] = set()

    def add(v):
        if isinstance(v, str) and len(v.strip()) >= 3:
            out.add(v.strip().casefold())
            for w in re.split(r"[^A-Za-z0-9]+", v):
                if len(w) >= 4:
                    out.add(w.casefold())

    path = ROOT / "tools" / "persona.local.json"
    if not path.exists():
        return out
    d = json.loads(path.read_text())
    for e in [d.get("owner") or {}] + list(d.get("people") or []):
        for v in (e.get("fake") or {}).values():
            for x in (v if isinstance(v, list) else [v]):
                add(x)
    for pin in (d.get("pins") or {}).values():
        for k, v in pin.items():
            if k != "real":
                add(v)
    for bucket in (d.get("buckets") or {}).values():
        for v in bucket.values():
            add(v)
    # Everything the LEARNED map produced is fixture vocabulary too: the
    # opaque stand-ins for network ids, room localparts and media handles are
    # this set's whole population.
    learned = ROOT / "tools" / "persona.learned.local.json"
    if learned.exists():
        for name, bucket in json.loads(learned.read_text()).items():
            for v in bucket.values():
                if isinstance(v, str):
                    add(v)
                elif isinstance(v, dict):
                    for x in v.values():
                        for y in (x if isinstance(x, list) else [x]):
                            add(y)
    return out


def is_generated(v: str) -> bool:
    """A value `tools/persona.py` can PROVE it produced: two or more words,
    every one of them from its own prose vocabulary. A generated two-word
    title will eventually collide with a real two-word string somewhere in a
    2,500-body corpus, and that collision is evidence of the generator, not of
    a leak."""
    sys.path.insert(0, str(ROOT / "tools"))
    try:
        import persona                                       # noqa: PLC0415
    except ImportError:
        return False
    fn = getattr(persona, "_already_generated", None)
    return bool(fn and fn(v))


def generated_vocabulary() -> set[str]:
    """Every word `tools/persona.py` can INVENT. A pseudonymised fixture is
    made of these, so one of them appearing is not a finding."""
    sys.path.insert(0, str(ROOT / "tools"))
    try:
        import persona                                       # noqa: PLC0415
    except ImportError:
        return set()
    out: set[str] = set()
    for attr in dir(persona):
        if not attr.isupper():
            continue
        val = getattr(persona, attr)
        if isinstance(val, (list, set, tuple)):
            for x in val:
                if isinstance(x, str):
                    for w in re.split(r"[^A-Za-z0-9]+", x):
                        if len(w) >= 4:
                            out.add(w.casefold())
    return out


def from_raw() -> tuple[set[str], set[str]]:
    """Harvest the raw pull for everything a prose map never held.

    Returns `(identifier needles, prose bodies)`. Identifiers are matched
    against every expanded component of every fixture leaf and key; prose
    bodies are matched WHOLE, which is what catches a message body copied
    verbatim into a chat's `preview`."""
    ids: set[str] = set()
    prose: set[str] = set()
    if not RAW.exists():
        return ids, prose

    # Keys whose value NAMES somebody or something of the owner's.
    ID_KEYS = {"id", "chatID", "senderID", "participantID", "loginID",
               "accountID", "localChatID", "imgURL", "srcURL", "posterImg",
               "favicon", "img", "filePath", "mergedIntoChatID",
               "defaultChatID", "stickerID"}
    PROSE_KEYS = {"text", "title", "description", "summary", "fullName",
                  "displayText", "senderName", "username", "email",
                  "phoneNumber", "transcription", "fileName", "name",
                  "statusText"}

    def note_id(v: str):
        if not isinstance(v, str) or len(v) < 4:
            return
        ids.add(v)
        for pat in (MXID, ROOMID):
            for m in pat.finditer(v):
                ids.add(m.group(0))
                local = m.group(1)
                ids.add(local)
                # `<bridge>_<network id>`: the network id is the identity, and
                # it is what survives a rename on the far network.
                head, sep, rest = local.partition("_")
                if sep and rest:
                    ids.add(rest)
        for m in EMAIL.finditer(v):
            ids.add(m.group(0))
        for m in PHONE.finditer(v):
            ids.add(re.sub(r"[ \-().]", "", m.group(0)))
        if "://" in v or v.startswith("/"):
            # a media handle: every path segment, and every sub-token of one
            for seg in re.split(r"[/?&=]+", urllib.parse.unquote(v)):
                if len(seg) >= 8:
                    ids.add(seg)
                    for bit in re.split(r"[^A-Za-z0-9]+", seg):
                        if len(bit) >= 8:
                            ids.add(bit)

    def visit(node, key=""):
        if isinstance(node, dict):
            for k, v in node.items():
                # Beeper's map KEYS are identities too — `seen` is keyed by
                # participant id and `draft.attachments` by attachment id — so
                # EVERY key of one of those blocks is harvested, whatever it
                # looks like, and any key anywhere that is MXID-, room- or
                # URI-shaped (Codex review).
                if isinstance(k, str) and k:
                    if key in ("seen", "attachments") or k[:1] in ("@", "!") \
                            or "://" in k:
                        note_id(k)
                visit(v, k)
            return
        if isinstance(node, list):
            for v in node:
                visit(v, key)
            return
        if not isinstance(node, str) or not node:
            return
        if key in ID_KEYS:
            note_id(node)
        if key in PROSE_KEYS and len(node.strip()) >= 5:
            prose.add(node.strip())
            if key in ("fullName", "displayText", "senderName", "username",
                       "name", "title"):
                ids.add(node.strip())
        # Wherever it sits, an MXID, a room id, an address or a media handle
        # is one: a body can quote an id nobody declared a key for.
        if node.startswith("@") or node.startswith("!") or "://" in node \
                or EMAIL.search(node):
            note_id(node)

    for f in sorted(RAW.glob("*.json")):
        try:
            visit(json.loads(f.read_text()))
        except (json.JSONDecodeError, UnicodeDecodeError):
            continue
        for bit in re.split(r"[^A-Za-z0-9]+", f.name):
            if len(bit) >= 10:
                ids.add(bit)

    return {v.strip() for v in ids if v and v.strip()}, prose


# --------------------------------------------------------------------- audit


def foreign_hosts(files) -> collections.Counter:
    """Every URL host that is neither Beeper's nor reserved. A host the owner
    shared a link to is the open web and is fine; a host nobody would have
    heard of names the organisation paying for it, so they are COUNTED and
    printed rather than failed on."""
    hosts = collections.Counter()
    for f in files:
        for m in re.finditer(r"(?:https?|mxc|localmxc)://([A-Za-z0-9.-]+)",
                             f.read_text()):
            host = m.group(1).lower()
            if host not in PUBLIC_HOSTS:
                hosts[host] += 1
    return hosts


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--verbose", action="store_true")
    ap.add_argument("--limit", type=int, default=25)
    a = ap.parse_args()

    if not RAW.exists():
        print("! raw/beeper is absent: auditing against the persona maps only")
    map_hard, map_soft = from_maps()
    raw_ids, raw_prose = from_raw()

    vocab = generated_vocabulary() | persona_output()
    strong: dict[str, str] = {}
    weak: dict[str, str] = {}
    digits: dict[str, str] = {}
    for source in (sorted(map_hard), sorted(raw_ids), sorted(map_soft)):
        for v in source:
            if len(v) >= 9 and _DIGITS.fullmatch(v):
                digits.setdefault(v, v)
                continue
            if is_vocabulary(v, map_hard) or v.casefold() in vocab:
                continue
            if is_strong(v):
                strong.setdefault(v.casefold(), v)
            elif is_weak(v):
                weak.setdefault(v.casefold(), v)
    weak.pop("", None)
    prose = {p.strip() for p in raw_prose if len(p.strip()) >= 12}

    files = sorted(FIXTURES.glob("GET_*.json"))
    hits: list[tuple[str, str, str, str]] = []      # file, path, needle, how
    counts = collections.Counter()
    leaf_n = 0

    def probe(fine: set[str], coarse: set[str],
              numeric: bool = False) -> str | None:
        for form in fine:
            hit = strong.get(form.casefold())
            if hit:
                return hit
            if numeric and len(form) >= 9 and _DIGITS.fullmatch(form):
                hit = digits.get(form)
                if hit:
                    return hit
        for form in coarse:
            hit = weak.get(form.casefold())
            if hit:
                return hit
        return None

    for f in files:
        # the recording NAME carries the request, and the request carries ids
        hit = probe(*expand(f.name))
        if hit:
            hits.append((f.name, "<filename>", hit, "filename"))
            counts["filename"] += 1
        doc = json.loads(f.read_text())
        for path, value in leaves(doc):
            leaf_n += 1
            if not isinstance(value, str) or not value:
                continue
            whole = value.strip()
            if whole in prose:
                hits.append((f.name, path, whole, "prose"))
                counts["prose"] += 1
                continue
            if is_generated(whole):
                continue
            is_key = path.endswith("<key>")
            leaf_key = path.rsplit(".", 1)[-1].replace("<key>", "")
            # A map KEY that LOOKS like an identity — an MXID, a room id, a
            # media handle — is probed against BOTH needle classes, because a
            # bare `@handle:beeper.com` carries no digit and would fall on the
            # weak side (Codex review). Every other key is a schema word
            # (`title`, `reminder`, `blockquote`) and is probed against
            # identifiers only, or the schema itself reads as a leak.
            identityish = value[:1] in ("@", "!") or "://" in value
            fine, coarse = expand(value)
            probe_coarse = coarse if (not is_key or identityish) else set()
            hit = probe(fine, probe_coarse,
                        numeric=leaf_key not in COUNTER_KEYS)
            if hit:
                # THE PATH ITSELF CAN CARRY THE LEAK: a map key is part of it,
                # so the last segment is fingerprinted rather than printed.
                shown = path
                if is_key:
                    shown = path.rsplit(".", 1)[0] + ".<key:%s>" % fingerprint(value)
                hits.append((f.name, shown, hit, "key" if is_key else "leaf"))
                counts["key" if is_key else "leaf"] += 1

    print("Beeper fixture publication audit")
    print("  %d recordings, %d scalar leaves and map keys" % (len(files), leaf_n))
    print("  %d real identifiers harvested from raw/beeper, %d prose bodies"
          % (len(raw_ids), len(prose)))
    print("  %d pinned-cast literals, %d learned-map literals"
          % (len(map_hard), len(map_soft)))
    print("  %d identifier needles, %d word needles" % (len(strong), len(weak)))
    hosts = foreign_hosts(files)
    if hosts:
        print("  URL hosts that are neither Beeper's nor reserved (%d):"
              % len(hosts))
        for host, n in hosts.most_common(10):
            print("      %-40s x%d" % (host, n))
    if not hits:
        print("  FINDINGS: 0  (filenames 0, leaves 0, map keys 0, prose 0)")
        return 0
    print("  FINDINGS: %d  (filenames %d, leaves %d, map keys %d, prose %d)"
          % (len(hits), counts["filename"], counts["leaf"], counts["key"],
             counts["prose"]))
    shown = hits if a.verbose else hits[:a.limit]
    for name, path, needle, how in shown:
        print("    %-8s %-62s %s  %s"
              % (how, path[:62], fingerprint(needle), name[:48]))
    if len(hits) > len(shown):
        print("    ... %d more (--verbose)" % (len(hits) - len(shown)))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
