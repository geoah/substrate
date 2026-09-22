#!/usr/bin/env python3
"""Publication audit for the Slack fixture set.

This tree is public. The pseudonymiser is the thing that makes it safe, and
this is the thing that proves it: every scalar leaf of every recording, every
recording NAME, every URL path and query component, every base64 token that
decodes to text, checked against every real literal we know of.

Two sources of truth for "real":

1.  `tools/persona.local.json` — the pinned cast's real names, addresses,
    logins, Slack ids and the real words behind every bucket, plus
    `tools/persona.learned.local.json`, whose KEYS are every real value the
    pseudonymiser ever mapped. Both gitignored.
2.  **the raw pull itself** (`raw/slack/`), harvested for the classes a
    name-and-prose map never held: user/channel/team/bot/app/workflow/
    trigger/usergroup/call/file ids, avatar hashes, custom emoji names,
    non-public hosts, identifier-bearing URL path and query components,
    opaque cursors, client message ids, block ids, and the exact text of
    every message body, topic, purpose, status and channel name.

A finding is a real literal that survives ANYWHERE in the published tree.
Zero is the only acceptable result.

    python3 providers/slack/fixtures/audit.py            # -> exit 0 on zero
    python3 providers/slack/fixtures/audit.py --verbose  # every occurrence
"""

from __future__ import annotations

import argparse
import base64
import binascii
import collections
import json
import pathlib
import re
import sys
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parents[3]
RAW = ROOT / "raw" / "slack"
FIXTURES = pathlib.Path(__file__).resolve().parent
MAPS = [ROOT / "tools" / "persona.local.json",
        ROOT / "tools" / "persona.learned.local.json",
        FIXTURES / "cast.local.json"]

# A Slack identity in any of its namespaces. Slack's ids are `<prefix><body>`
# and the prefix is the namespace: U/W user, B bot, T team, C/D/G/E
# conversation, S usergroup, A app, F file, Ft trigger, Wf workflow, R call,
# Fl list, Q request.
# A Slack id always carries at least one DIGIT, which is the only thing
# that separates `U08Q3GYMYTZ` from `EVERYONE`, `DEFAULT` and `BROADCAST`.
SLACK_ID = re.compile(
    r"\b(?:Ft|Wf|Fl|[UWBTCDGSAFERQ])(?=[0-9A-Z]*\d)[0-9A-Z]{6,12}\b")
UUID = re.compile(r"\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b", re.I)
HEXISH = re.compile(r"\b[0-9a-f]{8,}\b", re.I)
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
URL = re.compile(r"https?://[^\s<>\"'|)\]]+")
B64ISH = re.compile(r"^[A-Za-z0-9+/_-]{8,}={0,2}$")

# Hosts whose NAME is not an identity: they are the platform, or the open
# web. Their PATHS are still audited — a public host with a private resource
# id in its path is exactly the recruiting-system leak.
PUBLIC_HOSTS = {
    "slack.com", "slack-edge.com", "slack-files.com", "slackb.com",
    "slack-imgs.com", "gravatar.com", "example.com", "example.org",
    "example.net", "localhost",
}


def _keep_hosts() -> set[str]:
    """The hosts `tools/persona.py` deliberately KEEPS.

    One source of truth for "this host's NAME is not an identity". The list
    lived here as well, shorter, so `docs.google.com` was kept by the
    pseudonymiser (rightly — it is the open web) and then read as a leak by
    the audit. If the judgement is wrong it should be wrong in one place."""
    sys.path.insert(0, str(ROOT / "tools"))
    try:
        import persona                                       # noqa: PLC0415
    except ImportError:
        return set()
    return {h.lower().lstrip(".") for h in getattr(persona, "KEEP_HOSTS", ())}


PUBLIC_HOSTS |= _keep_hosts()

# Path/query components that are grammar, not identity.
BORING_SEGMENT = re.compile(
    r"^(?:|api|files|files-pri|files-tmb|archives|team|messages|docs|x|v\d+|"
    r"download|services|apps|open|help|www|images|img|static|assets|"
    r"[a-z]{1,3}|\d{1,6}|[a-z]+\.(?:png|jpg|jpeg|gif|pdf|svg|webp|mp4))$", re.I)

# Words that are language, not identity: a real message body is prose and its
# individual words must not each become a needle.
MIN_WORD = 5

# A dict key that is an API FIELD NAME is grammar, not identity — `upload`,
# `priority`, `has_more`. Slack spells every one of them in lower snake case,
# and no identity it mints looks like that: an id carries a capital prefix and
# a digit, a hash is hex, an address has an `@`. So a key outside this shape
# is audited like any other leaf, and one inside it is skipped.
API_FIELD_NAME = re.compile(r"[a-z][a-z0-9_]*")

# Leaves whose value comes from a fixed PUBLIC vocabulary rather than from
# this workspace. They are still checked for an identity hiding inside them —
# a Slack id, a UUID, an address — but not against the WORD needles a real
# message body contributes, because a collision there says nothing.
#
#   * `tz` / `tz_label` are POOLED on purpose: `Rewriter.slack_tz` maps every
#     real zone into one of eight IANA names, so a fixture zone equalling a
#     real one is the mechanism working, not a leak. A rare zone naming one
#     person is exactly what the pool exists to prevent.
#   * the rest are Slack's own closed or near-closed vocabularies
#     (`application/pdf`, `complete`, `tombstone`), which are English words
#     and will match any sufficiently large prose harvest.
GRAMMAR_LEAF = re.compile(
    r"(?:^|\.)(tz|tz_label|mimetype|filetype|pretty_type|mode|subtype|"
    r"access|org_or_workspace_access|media_display_type|file_access|status|"
    r"locale|type|channel_type|restricted_user_type|two_factor_type|"
    r"external_type|presence|manual_presence|color)$")


_INDEX = re.compile(r"\[\d+\]")


def _indexless(path: str) -> str:
    """The path with every array index flattened, so a rule can name a field
    without naming which element it sat in."""
    return _INDEX.sub("[]", path or "")


def identity_shaped(form: str) -> bool:
    """Does this form look like an identity rather than a word?

    What a GRAMMAR_LEAF is still probed with. `application/pdf` may not be
    compared against the word `Application` out of somebody's message; it must
    still be compared against `U08Q3GYMYTZ`."""
    f = (form or "").strip()
    if not f:
        return False
    return bool(SLACK_ID.fullmatch(f) or UUID.fullmatch(f)
                or EMAIL.fullmatch(f) or HEXISH.fullmatch(f)
                or "://" in f or "@" in f)


# ------------------------------------------------------------------- walking


def leaves(node, path=""):
    """Every scalar leaf with its path — and every KEY, which is also a leaf.

    T-021: a dict KEY was never audited, and `files.info` answers `shares:
    {public: {<channel id>: [...]}}` while a profile's custom fields are keyed
    by the workspace's own `Xf…` ids. A map keyed by a real identifier
    publishes it just as surely as a value does, and the pseudonymiser had no
    key pass at all until this found the hole.
    """
    if isinstance(node, dict):
        for k, v in node.items():
            if isinstance(k, str) and not API_FIELD_NAME.fullmatch(k):
                yield (path + ".{}").lstrip("."), k
            yield from leaves(v, (path + "." + k).lstrip("."))
    elif isinstance(node, list):
        for i, v in enumerate(node):
            yield from leaves(v, "%s[%d]" % (path, i))
    else:
        yield path, node


def decode_b64(s: str) -> str | None:
    """A Slack cursor is base64 of `<kind>:<id>`. Base64 is an encoding, not
    anonymisation, so the audit decodes before it compares."""
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

    Returns `(fine, coarse)`. **coarse** is the value itself, its
    URL-decoded self, its base64 plaintext and its delimiter-separated
    components with their internal `-`/`_`/`.` intact — a hyphenated channel
    name stays one word. **fine** adds every sub-word of those, which is what catches an
    id fused into a filename or a path token. A weak (dictionary-shaped)
    needle is only ever compared against **coarse**, or every English word of
    a generated sentence becomes a finding."""
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
            except Exception:  # noqa: BLE001 - a malformed escape is just text
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


# ------------------------------------------------------------------- shapes

_ENUMISH = re.compile(r"^[A-Z][A-Z0-9_]*$")
_ISO_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}([T ].*)?$")
_TS = re.compile(r"^\d+(\.\d+)?$")
# The grammar of the platform, of the file system and of English: a value
# that is one of these is not this workspace's identity.
_STOP = {
    "slack", "https", "http", "null", "false", "true", "image", "images",
    "member", "members", "unknown", "public", "private", "channel",
    "channels", "general", "random", "message", "messages", "users", "user",
    "team", "teams", "group", "groups", "email", "emails", "avatar",
    "avatars", "files", "download", "profile", "profiles", "photo", "photos",
    # The open web's own path grammar, which a third-party link is made of:
    # `docs.google.com/forms/d/e/<id>/viewform`. The WORD is the product, not
    # the resource — the resource is the id beside it, and that is still a
    # needle. A workspace that names a channel `forms` loses the catch, which
    # is the same trade `review` and `general` already make.
    "forms", "viewform", "document", "documents", "spreadsheet",
    "spreadsheets", "presentation", "slides", "folder", "folders", "drive",
    "calendar", "meet", "watch", "embed",
    "index", "default", "standard", "everyone", "changed", "review",
    "reviews", "draft", "drafts", "batch", "covers", "cluster", "dashboard",
    "capacity", "static", "assets", "attachment", "attachments", "thumb",
    "video", "audio", "canvas", "workflow", "workflows", "trigger", "list",
    "lists", "record", "records", "shared", "search", "help", "support",
    "about", "login", "signup", "account", "accounts", "settings", "admin",
    "apps", "docs", "blog", "home", "index.html", "english", "notifications",
    "example", "context", "question", "answer", "status", "online",
    "offline", "active", "away", "thread", "threads", "bookmarks",
    "bookmark", "huddle", "huddles", "reaction", "reactions", "emoji",
    # A TIMEZONE is a quasi-identifier and is remapped, but its vocabulary is
    # the IANA database's, not this workspace's: matching on `Mountain` or
    # `London` makes every remapped zone a false finding.
    "london", "europe", "america", "asia", "africa", "australia", "pacific",
    "atlantic", "indian", "antarctica", "mountain", "eastern", "western",
    "central", "summer", "daylight", "british", "korea", "turkey", "hawaii",
    "aleutian", "european", "standard", "chicago", "denver", "phoenix",
    "seoul", "istanbul", "brussels", "amsterdam", "casablanca", "honolulu",
}
# A subdomain label that is the open web's grammar, not a company's name.
_SUBDOMAIN_STOP = {
    "www", "api", "app", "apps", "jobs", "my", "go", "get", "id", "web",
    "cdn", "media", "content", "static", "assets", "img", "images", "link",
    "links", "share", "view", "edit", "download", "uploads", "files",
    "staging", "stage", "dev", "test", "prod", "secure", "portal", "auth",
    "sso", "login", "accounts", "mail", "calendar", "drive", "meet", "chat",
    "docs", "help", "support", "status", "blog", "store", "shop", "admin",
}
_STOP = {w.casefold() for w in _STOP}


def is_strong(v: str) -> bool:
    """An identifier: a shape no sentence produces. Compared against every
    sub-component of every leaf."""
    if len(v) < 5 or _TS.fullmatch(v) or _ISO_DATE.fullmatch(v):
        return False
    if EMAIL.fullmatch(v):
        return True
    if UUID.fullmatch(v) or SLACK_ID.fullmatch(v):
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


def is_handle(v: str) -> bool:
    """A component of a URL that is an OPAQUE HANDLE — a hash, a UUID, a
    Slack id, a long base64 token. `1f334.png` and `apple-large` are the
    platform's asset vocabulary and are not."""
    if UUID.fullmatch(v) or SLACK_ID.fullmatch(v):
        return True
    # A hyphenated lowercase WORD LIST is a public page's slug
    # (`some-article-about-a-thing`), not a handle. A real opaque token
    # carries mixed case or is hex, and a UUID was matched above.
    if re.fullmatch(r"[a-z0-9]+(?:[-_][a-z0-9]+){2,}", v):
        return False
    core = v.rsplit(".", 1)[0] if "." in v else v
    if HEXISH.fullmatch(core) and len(core) >= 12:
        return True
    if len(core) >= 16 and B64ISH.fullmatch(core) and re.search(r"\d", core) \
            and re.search(r"[A-Za-z]", core):
        return True
    return False


def is_weak(v: str) -> bool:
    """A word that names somebody or something in this workspace — a person,
    a company, a channel, a custom emoji, a host label. Compared only against
    whole leaves and whole delimiter-separated components."""
    if len(v) < 5 or _ENUMISH.fullmatch(v) or _ISO_DATE.fullmatch(v):
        return False
    if v.casefold() in _STOP:
        return False
    return bool(re.fullmatch(r"[A-Za-z][A-Za-z0-9 ._'-]{3,}", v))


# ------------------------------------------------------------------- needles


def from_maps() -> tuple[set[str], set[str]]:
    """Every real literal the persona maps hold.

    Returns `(hard, soft)`. **hard** is the pinned cast's own real values —
    names, addresses, logins, Slack ids — which are real by construction and
    are never filtered. **soft** is every other map key: real words the
    pseudonymiser learned, but also, unavoidably, words the GENERATOR itself
    produced and then re-mapped idempotently. A soft needle that the
    generator can emit (`staging`, a surname from the surname pool) is
    dropped by `main`, because a fixture containing it is evidence of the
    generator, not of a leak.
    """
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
                real = e.get("real") or {}
                for k, v in real.items():
                    if isinstance(v, list):
                        for x in v:
                            add(x)
                    else:
                        add(v)
            for bucket in (d.get("buckets") or {}).values():
                for k in bucket:
                    add(k, soft)
            # A pin's KEY is the demo name the owner CHOSE (`chris`) — that
            # is the fake side. Only the `real` block under it is real.
            for pin in (d.get("pins") or {}).values():
                for v in (pin.get("real") or {}).values():
                    if isinstance(v, list):
                        for x in v:
                            add(x)
                    else:
                        add(v)
        elif "cast" in d:                                # fixtures/cast.local
            for row in d.get("cast") or []:
                add(row.get("id"))
                add(row.get("name"))
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
    """Every value the persona map DELIBERATELY produces — invented names,
    logins, addresses, Slack ids, and the demo cast's pinned spellings.

    These are the fixture's own vocabulary. One of them appearing is the
    pseudonymiser working, not a leak — and a pinned demo first name can
    collide with a real handle somewhere in a 501-person workspace, which
    would otherwise read as a finding forever."""
    out: set[str] = set()

    def add(v):
        if isinstance(v, str) and len(v.strip()) >= 3:
            out.add(v.strip().casefold())
            for w in re.split(r"[^A-Za-z0-9]+", v):
                if len(w) >= 4:
                    out.add(w.casefold())

    path = ROOT / "tools" / "persona.local.json"  # noqa: F841 (see below)
    if not path.exists():
        return out
    d = json.loads(path.read_text())
    for e in [d.get("owner") or {}] + list(d.get("people") or []):
        for v in (e.get("fake") or {}).values():
            if isinstance(v, list):
                for x in v:
                    add(x)
            else:
                add(v)
    for pin in (d.get("pins") or {}).values():
        for k, v in pin.items():
            if k != "real":
                add(v)
    for name, bucket in (d.get("buckets") or {}).items():
        for v in bucket.values():
            add(v)
    return out


def platform_vocabulary() -> set[str]:
    """Slack's OWN emoji shortcodes. The pseudonymiser keeps them on purpose
    (`STANDARD_EMOJI`), and an earlier over-broad pass left a few in the
    learned map as keys, so without this they read as leaks forever."""
    sys.path.insert(0, str(ROOT / "tools"))
    try:
        import pseudonymise                                  # noqa: PLC0415
    except ImportError:
        return set()
    return {e.casefold() for e in getattr(pseudonymise, "STANDARD_EMOJI", ())}


def generated_words() -> set[str]:
    """Every word the generator can emit, down to `the` and `new`.

    `generated_vocabulary` keeps words of four letters or more, because a
    three-letter needle is noise. But the "is this phrase the generator's
    own?" test needs the SHORT words too: `the new` is two stopwords and one
    of them is three letters, so the test saw no words at all and every
    generated status line read as a finding."""
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
                        if w:
                            out.add(w.casefold())
    return out


def generated_vocabulary() -> set[str]:
    """Every word `tools/persona.py` can INVENT. A pseudonymised fixture is
    made of these, so one of them appearing is not a finding — and the
    learned map holds a few of them as keys, because a second pass re-maps
    its own output idempotently."""
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
                    # The WHOLE entry as well as its words: `_SUBJ` holds
                    # phrases ("the review", "the retry"), and the learned map
                    # keeps a few of them as KEYS because a second pass
                    # re-maps the generator's own output idempotently. Only
                    # the words were benign, so every generated title read as
                    # a finding.
                    out.add(x.strip().casefold())
                    for w in re.split(r"[^A-Za-z0-9]+", x):
                        if len(w) >= 4:
                            out.add(w.casefold())
    return out


def from_raw() -> tuple[set[str], set[str], set[str]]:
    """Harvest the raw pull for everything a prose map never held.

    Returns (identifier needles, prose bodies, benign words). Identifiers
    are matched
    against every expanded component of every fixture leaf; prose bodies are
    matched whole, which is what catches a message body copied verbatim into
    a nested `latest`/`root` representation.
    """
    ids: set[str] = set()
    prose: set[str] = set()
    # emoji_name -> True when the workspace UPLOADED it. A standard Unicode
    # emoji carries `unicode` in its display info and is Slack's vocabulary,
    # not this workspace's; a custom one is an organisational identifier.
    custom_emoji: dict[str, bool | None] = {}
    names: set[str] = set()
    if not RAW.exists():
        return ids, prose, set()

    ID_KEYS = {
        "id", "user", "bot_id", "app_id", "api_app_id", "team_id", "team",
        "channel", "channel_id", "creator", "inviter", "parent_user_id",
        "user_id", "author_id", "source_team", "user_team", "channel_team",
        "author_team", "context_team_id", "enterprise_id", "client_msg_id",
        "block_id", "avatar_hash", "last_avatar_image_hash", "trigger_id",
        "workflow_id", "usergroup_id", "huddle_state_call_id", "call_id",
        "next_cursor", "cursor", "file_id", "canvas_id", "shared_file_id",
        "pinned_by", "bot_user_id", "profile_id", "subteam_id",
        "external_id", "thread_id", "root_id", "tab_id", "list_id",
    }
    PROSE_KEYS = {
        "text", "value", "fallback", "pretext", "title", "name",
        "name_normalized", "real_name", "real_name_normalized",
        "display_name", "display_name_normalized", "first_name", "last_name",
        "status_text", "status_text_canonical", "preview", "plain_text",
        "preview_highlight", "alt_text", "author_name", "author_subname",
        "service_name", "email", "phone", "username", "label",
    }

    def visit(node, key=""):
        if isinstance(node, dict):
            if "emoji_name" in node and "display_url" in node:
                custom_emoji[node["emoji_name"]] = not node.get("unicode")
            # A CONVERSATION's name is this workspace's; a reaction's `name`
            # is Slack's emoji vocabulary and a file's is a filename. Only a
            # dict that is recognisably a conversation contributes one.
            if any(k in node for k in ("is_channel", "is_im", "is_mpim",
                                       "is_group", "is_private")):
                for k in ("name", "name_normalized"):
                    if isinstance(node.get(k), str) and len(node[k]) >= 4:
                        names.add(node[k])
                for n in node.get("previous_names") or []:
                    if isinstance(n, str) and len(n) >= 4:
                        names.add(n)
            # A USER's login.
            if "profile" in node and isinstance(node.get("name"), str):
                if len(node["name"]) >= 4:
                    names.add(node["name"])
            for k, v in node.items():
                visit(v, k)
            return
        if isinstance(node, list):
            for v in node:
                visit(v, key)
            return
        if not isinstance(node, str) or not node:
            return
        if key in ID_KEYS or key.startswith("previous_names"):
            if len(node) >= 4:
                ids.add(node)
        if key in ("status_emoji", "emoji") and node.startswith(":"):
            custom_emoji.setdefault(node.strip(":"), None)
        # Identity-shaped substrings anywhere, under any key.
        for m in SLACK_ID.finditer(node):
            ids.add(m.group(0))
        for m in UUID.finditer(node):
            ids.add(m.group(0))
        for m in EMAIL.finditer(node):
            ids.add(m.group(0))
        if key in ("avatar_hash", "last_avatar_image_hash"):
            ids.add(node)
        for m in URL.finditer(node):
            harvest_url(m.group(0), ids)
        if key in PROSE_KEYS and len(node) >= 12 and " " in node.strip():
            prose.add(node.strip())

    def harvest_url(u: str, out: set[str]):
        try:
            parts = urllib.parse.urlsplit(u)
        except ValueError:
            return
        host = (parts.hostname or "").lower()
        if host and not any(host == h or host.endswith("." + h)
                            for h in PUBLIC_HOSTS):
            out.add(host)
            # the registrable label is the identifying WORD of a company host
            labels = host.split(".")
            if len(labels) >= 2:
                for label in (labels[-2], labels[0]):
                    # A NUMBERED CDN shard (`media0`, `i2`, `cdn3`) is the
                    # same grammar as its unnumbered form: giphy serves from
                    # media0…media4 and the digit names a machine, not a
                    # workspace.
                    if re.sub(r"\d+$", "", label) not in _SUBDOMAIN_STOP:
                        out.add(label)
        # A path or query component is a needle only when it is
        # identifier-SHAPED. `/dashboard` is the open web's grammar; a
        # 32-hex asset name, a UUID or a Slack id is a handle.
        for seg in parts.path.split("/"):
            seg = urllib.parse.unquote(seg)
            if not seg or BORING_SEGMENT.fullmatch(seg):
                continue
            if is_handle(seg):
                out.add(seg)
            for bit in re.split(r"[^A-Za-z0-9]+", seg):
                if is_handle(bit):
                    out.add(bit)
        for vs in urllib.parse.parse_qs(parts.query).values():
            for v in vs:
                v = urllib.parse.unquote(v)
                if is_handle(v):
                    out.add(v)

    for f in sorted(RAW.glob("*.json")):
        try:
            visit(json.loads(f.read_text()))
        except (json.JSONDecodeError, UnicodeDecodeError):
            continue
        for bit in re.split(r"[^A-Za-z0-9]+", f.name):
            if SLACK_ID.fullmatch(bit):
                ids.add(bit)

    ids |= {name for name, custom in custom_emoji.items() if custom}
    ids |= names
    # A STANDARD Unicode emoji name is Slack's vocabulary. The learned map
    # holds a few as keys from an earlier over-broad rule; they must not be
    # needles, or every pseudonymised status reads as a leak.
    benign = {name.casefold() for name, custom in custom_emoji.items()
              if custom is False}
    return {v.strip() for v in ids if v and v.strip()}, prose, benign


# --------------------------------------------------------------------- audit


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--verbose", action="store_true")
    ap.add_argument("--limit", type=int, default=25)
    ap.add_argument("--paths", action="store_true",
                    help="group findings by path family, for triage")
    a = ap.parse_args()

    if not RAW.exists():
        print("! raw/slack is absent: auditing against the persona maps only")
    map_hard, map_soft = from_maps()
    raw_ids, raw_prose, benign = from_raw()

    vocab = (generated_vocabulary() | benign | persona_output()
             | platform_vocabulary())
    # The word-by-word test's vocabulary: the same plus the generator's
    # stopwords (see generated_words).
    words_vocab = vocab | generated_words()
    def invented(v: str) -> bool:
        """Is this needle a string the GENERATOR itself can produce?

        A whole entry of a word bank is in `vocab` already; a PHRASE the
        generator assembled out of them (`the retry`, cut short by a title's
        length bound) is not, and every generated title read as a finding
        because of it. If every word of a weak needle is the fixture's own
        vocabulary, the needle carries nothing real."""
        if v.casefold() in vocab:
            return True
        words = [w.casefold() for w in re.split(r"[^A-Za-z0-9]+", v) if w]
        return bool(words) and all(w in words_vocab for w in words)

    strong: dict[str, str] = {}
    weak: dict[str, str] = {}
    for v in sorted(map_hard):
        if is_strong(v):
            strong.setdefault(v.casefold(), v)
        elif is_weak(v) and not invented(v):
            weak.setdefault(v.casefold(), v)
    for v in sorted(raw_ids) + sorted(map_soft):
        if is_strong(v):
            strong.setdefault(v.casefold(), v)
        elif is_weak(v) and not invented(v):
            weak.setdefault(v.casefold(), v)
    weak.pop("", None)
    # A real prose body whose EVERY word the generator can invent is not
    # evidence of a leak: it is a string the generator itself produces, which
    # is why `persona._already_generated` refuses to rewrite it in the first
    # place (" After Friday, " is three words from `_SUBJ`/`_TAIL`). Keeping
    # it as a needle makes the generator's own output a finding forever.
    def all_generated(body: str) -> bool:
        words = [w.casefold() for w in re.split(r"[^A-Za-z0-9]+", body) if w]
        return bool(words) and all(w in words_vocab for w in words)

    prose = {p.strip(): p for p in raw_prose
             if len(p.strip()) >= 12 and not all_generated(p)}

    files = sorted(FIXTURES.glob("GET_*.json"))
    hits: list[tuple[str, str, str, str]] = []      # file, path, needle, how
    counts = collections.Counter()
    leaf_n = 0

    def probe(fine: set[str], coarse: set[str]) -> str | None:
        for form in fine:
            hit = strong.get(form.casefold())
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
                hits.append((f.name, path, whole[:40] + "…", "prose"))
                counts["prose"] += 1
                continue
            fine, coarse = expand(value)
            if GRAMMAR_LEAF.search(_indexless(path)):
                # A pooled zone or a closed API word. Still searched for an
                # identity; no longer compared with the dictionary.
                fine = {x for x in fine if identity_shaped(x)}
                coarse = set()
                if not fine:
                    continue
            hit = probe(fine, coarse)
            if hit:
                hits.append((f.name, path, hit, "leaf"))
                counts["leaf"] += 1

    print("Slack fixture publication audit")
    print("  %d recordings, %d scalar leaves" % (len(files), leaf_n))
    print("  %d real identifiers harvested from raw/slack, %d prose bodies"
          % (len(raw_ids), len(prose)))
    print("  %d pinned-cast literals, %d learned-map literals"
          % (len(map_hard), len(map_soft)))
    print("  %d identifier needles, %d word needles" % (len(strong), len(weak)))
    if not hits:
        print("  FINDINGS: 0  (filenames 0, leaves 0, prose bodies 0)")
        return 0
    print("  FINDINGS: %d  (filenames %d, leaves %d, prose bodies %d)"
          % (len(hits), counts["filename"], counts["leaf"], counts["prose"]))
    if a.paths:
        fam = collections.Counter(re.sub(r"\[\d+\]", "[]", h[1]) for h in hits)
        for p, n in fam.most_common():
            ex = next(h for h in hits if re.sub(r"\[\d+\]", "[]", h[1]) == p)
            print("    x%-5d %-58s %s" % (n, p[:58], ex[2][:34]))
        return 1
    by_needle = collections.Counter(h[2] for h in hits)
    for needle, n in by_needle.most_common(a.limit):
        where = next(h for h in hits if h[2] == needle)
        print("    x%-4d %-28s %s %s" % (n, needle[:28], where[0][:52], where[1]))
    if a.verbose:
        for h in hits:
            print("      %s  %s  %s" % (h[0], h[1], h[2]))
    elif len(by_needle) > a.limit:
        print("    … %d more distinct" % (len(by_needle) - a.limit))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
