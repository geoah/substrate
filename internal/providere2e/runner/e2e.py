#!/usr/bin/env python3
"""Drive one provider end to end against a mock upstream.

`internal/providere2e` (the Go suite) owns the server and the repository;
this owns the run.

    fixtures/<p>  ->  a throwaway repository (e2e-<p>.localhost)

THE DIRECTORIES
===============

Four roots, each an environment variable when the layout is not the one here:

    SUBSTRATE_E2E_ROOT     this file's parent's parent, holding providers/,
                           fixtures/ and runner/
    SUBSTRATE_E2E_REPO     the substrate checkout, two levels above that
    SUBSTRATE_E2E_BUNDLES  the shipped bundles, kinds/providers.substrate.reamde.dev
    SUBSTRATE_E2E_VOCAB    the manifest tree scanned for record mappings that
                           read a provider's kinds, samples/

THE RUN
=======

 1. start `runner/mockserver.py` on a free loopback port over the recordings
 2. copy the bundle's `*.yaml` to a temp dir and REWIRE the copy: the
    bundle's trusted `oauth2:` endpoints and the sync body's API base point
    at the mock, and the mock's host joins `permissions.network`. The files
    in git stay pinned to the live provider — the substitution lands only in
    the temp copy, exactly as substrate's own provider tests do it
    (`rewriteOAuthEndpoints`). Loopback `http` is admitted by the engine for
    precisely this seam.
 3. `substratectl apply` the rewired closure into the target repository
 4. write the config record (client id/secret are arbitrary — the mock
    checks nothing) and the account record with every feature on
 5. the OAuth dance: POST …/oauth/start -> the consent URL -> the mock's
    /oauth/authorize -> 302 to the substrate callback -> `tokenStatus:
    connected`
 6. wait for the on-connect trigger's runs to settle (trigger status: no
    lag, nothing pending, nothing parked), then for the account's own
    `lastSyncedAt` stamp
 7. run `providers/<p>/scenario.py` with the run's facts in the
    environment
 8. print what happened: the recordings hit, the recordings MISSED (the ones
    still to capture, grouped by method so they read as a work list), the
    mirror rows by kind, the trigger runs
 -- `--scenario-only` runs 1, 7 and 9 and NOTHING between them: no rewire,
    no apply, no config or account write, no consent flow, no drive. It is
    for a repository an earlier run ALREADY POPULATED, where the rows took
    hours and the assertions are what is being iterated on (T-054). The mock
    still comes up on the fixed port, because what answers the scenario's own
    re-sync is the INSTALLED closure and that still dials it, through
    `config.apiBase`, which is a record and survives a later
    `substratectl apply` that put the live body back; the run says where it
    found the mock named, and warns when it is named nowhere. The switches the
    earlier run turned off go back on first (`unquiesce`) and off again after.
    THE CAVEAT IT CANNOT REMOVE: a fresh mock process has an EMPTY request
    log, so every "what did the sync ask for" assertion describes the
    SCENARIO's own re-sync, not the run that wrote the rows. That run's own
    log is the only place its requests can be read.

 9. QUIESCE (T-007): the mock dies with this run and the installed
    function body still points at it, so everything that could fire it again
    is switched off — the provider's triggers are disabled in the repository
    and the account's `syncFrequency` is set to its off value. Without this
    the hourly schedule and any pending re-drive stamp `erroring: Connection
    refused` on the account for the rest of the day. The next run's apply
    turns the kept triggers back on (a `substratectl apply` of a trigger
    document is a record PUT and carries `enabled: true` with it), and an
    apply of the files in git restores the whole closure pointed at the LIVE
    provider, so nothing here is a one-way door. A run that ENDS BADLY —
    a crash, a `die()`, a Ctrl-C — quiesces too, in every mode, because
    nobody is coming back to it and the drain it abandoned would dial the
    dead port for the rest of the day (T-050).

WHAT A PROVIDER AGENT OWNS
==========================

`providers/<p>/e2e.json`, all keys optional — everything here is
discovered from the bundle when it is absent, and named here when the guess
is wrong:

    {
      "configKind":  "providers.substrate.reamde.dev/github/config",
      "accountKind": "providers.substrate.reamde.dev/github/account",
      "accountId":   "e2e",
      "config":      {"clientId": "e2e-client", "clientSecret": "e2e-secret"},
      "account":     {"syncFrequency": "hourly", "backfillDepth": "last30d"},
      "hosts":       ["https://api.github.com", "https://github.com"],
      "keepHosts":   ["https://github.com/"],
      "triggers":    ["github-on-connect"],
      "settleSeconds": 120
    }

  * `hosts` — every live prefix the rewire replaces with the mock URL. When
    absent it is the set of scheme+host of the `oauth2:` endpoints, which is
    right for a bundle whose API base is the same host as its token endpoint
    and wrong for GitHub (api.github.com vs github.com) — so GitHub names it.
  * `keepHosts` — prefixes NOT to rewrite even though `hosts` covers them:
    an `html_url` a fixture asserts on, for instance.
  * `triggers` — the trigger ids to keep enabled. Every OTHER trigger in the
    closure is disabled for the run, because a SCHEDULE would race
    catch-up fires into the drain and prove nothing (substrate's own
    `installTriggers(t, dir, "github-on-connect")`). The rewired COPY carries
    that state, and the run also writes it to the trigger records themselves
    after the apply — an apply the engine decided was a no-op would otherwise
    leave yesterday's switch positions standing.
  * `quiesce` — what step 9 switches off, when the guesses are wrong:
    `{"syncFrequency": "off", "keepTriggers": [], "skip": false}`.
    `syncFrequency` defaults to the off-ish value of the account kind's own
    `syncFrequency` enum (`off` / `manual` / `never` / `none` / `paused`),
    read out of the bundle; `false` leaves the property alone.
    `keepTriggers` names triggers to leave enabled anyway — nothing needs it
    today, because every trigger in the closure calls a function body pinned
    to the mock that has just died.
  * `faults` — the file beside `e2e.json` holding the run's injected-failure rules
    (default `faults.json`, absent means none). The rule grammar is
    `runner/mockserver.py`'s module docstring: a glob per endpoint and a queue
    of statuses consumed in order, over the recordings the success path
    already uses, so a 429/500/401/404 matrix needs no hand-made recording.
    A scenario drives the same thing BETWEEN two syncs by POSTing
    `{"rules": […]}` to `MOCK_URL/__mock/faults`.

`providers/<p>/scenario.py` is yours too. It is run with:

    SUBSTRATE_SERVER     http://localhost:8081
    SUBSTRATE_TOKEN      the target repository's bearer
    SUBSTRATE_AUTHORITY  the repository (e2e-github.localhost)
    MOCK_URL             http://127.0.0.1:<port>
    MODE                 e2e
    PROVIDER             github
    ACCOUNT_KIND         providers.substrate.reamde.dev/github/account
    ACCOUNT_ID           e2e
    RECORDINGS           the directory being served

A non-zero exit fails the run. Assert the mirror rows, the references, the
temporal fields and the owner flags; then re-run the sync and assert
idempotence (no duplicate rows, the cursor advanced). `MOCK_URL/__mock/requests`
is the request log, which is how you assert the query windows the sync asked
for without guessing — and where an injected failure shows up, as
`"match": "fault"` with the rule that fired.
"""

from __future__ import annotations

import argparse
import contextlib
import io
import json
import os
import pathlib
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

# The suite's own directory: providers/<p>/e2e.json, providers/<p>/scenario.py,
# fixtures/<p> and runner/ are under it.
ROOT = pathlib.Path(os.environ.get("SUBSTRATE_E2E_ROOT")
                    or pathlib.Path(__file__).resolve().parent.parent)
# The substrate checkout. The run applies a REWIRED COPY of the bundle; the
# files in git are never edited.
REPO = pathlib.Path(os.environ.get("SUBSTRATE_E2E_REPO") or ROOT.parent.parent)
BUNDLES = pathlib.Path(os.environ.get("SUBSTRATE_E2E_BUNDLES")
                       or REPO / "kinds" / "providers.substrate.reamde.dev")
# The manifest tree scanned for record mappings that read a provider's kinds.
# Only a repository that ALREADY HOLDS the provider reaches it: a fresh one
# has nothing installed to hold back.
VOCAB = pathlib.Path(os.environ.get("SUBSTRATE_E2E_VOCAB") or REPO / "samples")
CORE = "substrate.reamde.dev/core"


def _rel(path) -> pathlib.Path:
    """A path said as shortly as it can be: relative to the checkout or to the
    suite when it is inside one of them, absolute otherwise."""
    path = pathlib.Path(path)
    for base in (REPO, ROOT):
        try:
            return path.relative_to(base)
        except ValueError:
            continue
    return path


def say(msg):
    print("\033[1m==>\033[0m %s" % msg, flush=True)


def warn(msg):
    print("\033[33m -- %s\033[0m" % msg, file=sys.stderr, flush=True)


def die(msg, code=1):
    print("\033[31m !! %s\033[0m" % msg, file=sys.stderr, flush=True)
    raise SystemExit(code)


# ------------------------------------------------------------------- the wire


# Seconds a synchronous trigger run may take to settle before the runner gives up.
RUN_TIMEOUT = 3600


class API:
    def __init__(self, server: str, token: str):
        self.server = server.rstrip("/")
        self.token = token

    # Four agents share one dev substrate and any of them may restart it
    # under the others. A refused connection or a 503 is therefore a NORMAL
    # transient here, not a failure: wait for it to come back rather than fail
    # a twenty-step run on somebody else's `dev:restart`.
    RETRY_SECONDS = 45

    def call(self, method: str, path: str, body=None, redirect=True, timeout=60):
        url = self.server + path if path.startswith("/") else path
        data = json.dumps(body).encode() if body is not None else None
        opener = urllib.request.build_opener() if redirect else \
            urllib.request.build_opener(_NoRedirect)
        deadline = time.time() + self.RETRY_SECONDS
        announced = False
        while True:
            req = urllib.request.Request(url, data=data, method=method)
            req.add_header("Authorization", "Bearer " + self.token)
            if data:
                req.add_header("Content-Type", "application/json")
            try:
                with opener.open(req, timeout=timeout) as r:
                    raw = r.read()
                    parsed = json.loads(raw) if raw.strip()[:1] in (b"{", b"[") \
                        else raw.decode()
                    return r.status, parsed, dict(r.headers)
            except urllib.error.HTTPError as e:
                if e.code == 503 and time.time() < deadline:
                    time.sleep(1.0)
                    continue
                raw = e.read()
                try:
                    return e.code, json.loads(raw), dict(e.headers)
                except json.JSONDecodeError:
                    return e.code, raw.decode()[:600], dict(e.headers)
            except urllib.error.URLError as e:
                if time.time() >= deadline:
                    die("%s %s: %s\n    (the substrate on %s is not "
                        "answering. It needs SUBSTRATE_OAUTH_CALLBACK_URL and "
                        "SUBSTRATE_OAUTH_STATE_KEY in its environment.)"
                        % (method, path, e.reason, self.server))
                if not announced:
                    warn("%s is not answering (%s) — waiting up to %ds for it "
                         "to come back" % (self.server, e.reason, self.RETRY_SECONDS))
                    announced = True
                time.sleep(1.0)

    def records(self, kind: str = "", first: int = 200, **filters):
        """THE list grammar: `filter` is a JSON object with `kinds`,
        `implements`, `ids`, `properties`, `labels`, `referencing`; the page
        size is `first`, not `limit`. A scenario imports this:

            import sys, os; sys.path.insert(0, os.environ["PYTHONPATH"])
            from e2e import API
            api = API(os.environ["SUBSTRATE_SERVER"], os.environ["SUBSTRATE_TOKEN"])
            rows = api.records("providers.substrate.reamde.dev/github/issue")
        """
        return self.page(kind, first, **filters)[0]

    def page(self, kind: str = "", first: int = 200, after: str = "",
             **filters) -> tuple[list[dict], str]:
        """One page and its continuation cursor ("" when exhausted).

        `first` is CAPPED AT 500 by the engine (substrate.Query: "default 50,
        max 500"), silently — a caller asking for 5,000 gets 500 and a cursor,
        which is how the summary table under-reported every large mirror as
        exactly 500. Everything that counts rows goes through `count()`.
        """
        f = dict(filters)
        if kind:
            f.setdefault("kinds", [kind])
        q = "/api/v1/records?first=%d" % first
        if after:
            q += "&after=" + urllib.parse.quote(after)
        if f:
            q += "&filter=" + urllib.parse.quote(json.dumps(f))
        st, body, _ = self.call("GET", q)
        if st != 200 or not isinstance(body, dict):
            return [], ""
        return (body.get("records") or []), (body.get("cursor") or "")

    PAGE = 500          # the engine's own ceiling; asking for more is ignored
    COUNT_CAP = 20000   # rows a summary count will walk before it says "N+"

    def count(self, kind: str = "", cap: int = 0, **filters) -> tuple[int, bool]:
        """(rows, capped) — the whole mirror, paged, not one page of it.

        There is no count endpoint (the console hits the same wall), so this
        walks pages of ids. `capped` is True when the walk stopped at `cap`
        with a cursor still in hand, which the caller prints as `N+` rather
        than as a number that is wrong.
        """
        cap = cap or self.COUNT_CAP
        total, after = 0, ""
        while True:
            rows, after = self.page(kind, self.PAGE, after, **filters)
            total += len(rows)
            if not after:
                return total, False
            if total >= cap:
                return total, True


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


# ----------------------------------------------------------- reading a bundle
#
# STDLIB ONLY, so no YAML parser: these read the handful of facts the run
# needs with regexes over the document text. That is enough because every fact
# below is a top-of-document, fixed-key line in a manifest we write ourselves —
# and when a guess is wrong, `e2e.json` names the answer instead.

OAUTH_KEYS = ("authorizationEndpoint", "tokenEndpoint", "revocationEndpoint",
              "emailEndpoint")


def documents(text: str) -> list[str]:
    return re.split(r"(?m)^---\s*$", text)


def doc_id(doc: str) -> str:
    """The document's `metadata.id`, in block form OR flow form
    (`metadata: {id: x, ...}`) — the reflection package writes the latter
    and used to hold itself back because only the block form was read."""
    m = re.search(r"(?m)^metadata:\s*$\n(?:\s+.*\n)*?\s+id:\s*(\S+)", doc)
    if not m:
        m = re.search(r"(?m)^metadata:\s*\{[^}]*\bid:\s*([^,}\s]+)", doc)
    return m.group(1).strip('"\'') if m else ""


def find_kind_with_trait(files: list[pathlib.Path], trait: str) -> str:
    for f in files:
        for doc in documents(f.read_text()):
            if re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/kind\s*$", doc) and \
                    re.search(r"(?m)^\s*traits:\s*\[[^\]]*\b%s\b" % re.escape(trait), doc):
                return doc_id(doc)
    return ""


def oauth_endpoints(files: list[pathlib.Path]) -> dict[str, str]:
    out = {}
    for f in files:
        for key in OAUTH_KEYS:
            m = re.search(r"(?m)^\s{4,}%s:\s*(\S+)\s*$" % key, f.read_text())
            if m:
                out[key] = m.group(1).strip('"\'')
    return out


def feature_toggles(files: list[pathlib.Path]) -> list[str]:
    """Every key under `featureScopes:` — the account's feature switches."""
    out = []
    for f in files:
        text = f.read_text()
        m = re.search(r"(?m)^(\s*)featureScopes:\s*$", text)
        if not m:
            continue
        indent = len(m.group(1))
        for line in text[m.end():].splitlines():
            if not line.strip():
                continue
            lead = len(line) - len(line.lstrip())
            if lead <= indent:
                break
            if lead == indent + 2 and line.rstrip().endswith(":"):
                out.append(line.strip().rstrip(":"))
    return out


def function_ids(files: list[pathlib.Path]) -> list[str]:
    out = []
    for f in files:
        for doc in documents(f.read_text()):
            if re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/function\s*$", doc):
                i = doc_id(doc)
                if i:
                    out.append(i)
    return out


def trigger_ids(files: list[pathlib.Path]) -> list[str]:
    out = []
    for f in files:
        for doc in documents(f.read_text()):
            if re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/trigger\s*$", doc):
                tid = doc_id(doc)
                if tid:
                    out.append(tid)
    return out


def package_of(files: list[pathlib.Path]) -> str:
    for f in files:
        for doc in documents(f.read_text()):
            if re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/package\s*$", doc):
                return doc_id(doc)
    return ""


# ------------------------------------------------------------- the rewiring


def rewire(src_files: list[pathlib.Path], dst: pathlib.Path, mock: str,
           hosts: list[str], keep: list[str], only_triggers: list[str]) -> list[pathlib.Path]:
    """Copy the closure, pointing every live provider prefix at the mock.

    Mirrors substrate's `rewriteOAuthEndpoints` and FAILS THE SAME WAY: if no
    endpoint moved, the run stops rather than quietly talking to the real
    provider with a real token.
    """
    out, moved, allowed = [], 0, 0
    mock_host = urllib.parse.urlsplit(mock).netloc
    for f in src_files:
        text = f.read_text()
        for host in sorted(hosts, key=len, reverse=True):
            if host in keep:
                continue
            if host in text:
                text = text.replace(host, mock)
                moved += 1
        # The function's network pin is a host allowlist; the mock is a host
        # the body must be allowed to reach.
        text, pinned = allow_mock_host(text, mock_host)
        allowed += pinned
        # Every trigger but the named ones is disabled: a schedule racing
        # catch-up fires into the drain proves nothing.
        if only_triggers:
            text = _disable_other_triggers(text, only_triggers)
        p = dst / f.name
        p.write_text(text)
        out.append(p)
    if not allowed:
        warn("no `permissions.network` block in the closure took the mock "
             "host — every function that dials %s will be refused by the "
             "engine's egress pin. (A closure whose bodies make no outbound "
             "call is the only honest case for this.)" % mock_host)
    if not moved:
        die("nothing in the closure named any of %s — the rewire found no "
            "live endpoint to point at the mock, so the run would have talked "
            "to the REAL provider with a REAL token. Name the API hosts in "
            "e2e/e2e.json under `hosts`. (A token bundle has no oauth2: block "
            "to derive them from, so it MUST name them.)" % (hosts,))
    return out


# The documents whose `permissions:` block is an EGRESS PIN. A `kind`
# document's properties are not: Beeper declares three kind properties called
# `network` (the guide forbids renaming an API field), and the old regex —
# `^\s*network:\s*$` over the whole file — turned each of them into a list
# (T-043). Beeper quoted its keys to hide from it; nothing should have to.
PINNED_DOC_KINDS = ("substrate.reamde.dev/core/function",
                    "substrate.reamde.dev/core/bundle",
                    "substrate.reamde.dev/core/agent")


def allow_mock_host(text: str, mock_host: str) -> tuple[str, int]:
    """Add the mock to every EGRESS allowlist in the closure, and to nothing
    else.

    Structural, not textual: only a document that declares one of
    `PINNED_DOC_KINDS` is considered, and within it only a `network:` key that
    is a direct child of a `permissions:` key. A property named `network` on a
    kind — the trap this replaces — is now invisible to it, quoted or not.
    """
    docs, added = documents(text), 0
    for i, doc in enumerate(docs):
        m = re.search(r"(?m)^kind:\s*(\S+)\s*$", doc)
        if not m or m.group(1).strip("\"'") not in PINNED_DOC_KINDS:
            continue
        lines, out, perms, here = doc.splitlines(), [], None, 0
        for line in lines:
            stripped, indent = line.strip(), len(line) - len(line.lstrip())
            # Leaving the permissions mapping: any line at or left of its key.
            if perms is not None and stripped and indent <= perms:
                perms = None
            if re.match(r'^"?permissions"?:\s*$', stripped):
                perms = indent
                out.append(line)
                continue
            out.append(line)
            if perms is not None and indent == perms + 2 and \
                    re.match(r'^"?network"?:\s*$', stripped):
                out.append('%s- "%s"' % (" " * (indent + 2), mock_host))
                here += 1
        if not here:
            continue
        added += here
        # splitlines() drops the document's own trailing newline, and the join
        # below is a bare `---`: putting it back is what keeps the file the
        # same file.
        docs[i] = "\n".join(out) + ("\n" if doc.endswith("\n") else "")
    # `documents()` splits on the separator LINE, so each document keeps the
    # newline before it and the one after: joining on a bare `---` puts the
    # file back byte for byte when nothing was inserted.
    return "---".join(docs), added


def _disable_other_triggers(text: str, keep: list[str]) -> str:
    docs = documents(text)
    for i, doc in enumerate(docs):
        if not re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/trigger\s*$", doc):
            continue
        if doc_id(doc) in keep:
            continue
        docs[i] = re.sub(r"(?m)^(\s*)enabled:\s*true\s*$", r"\1enabled: false", doc)
    return "---\n".join(docs)


# --------------------------------------------------- making room for our copy


def ctl(a, *args) -> subprocess.CompletedProcess:
    return subprocess.run([a.ctl, "--context", a.authority, "--server", a.server,
                           *args], capture_output=True, text=True)


MAPPING_KIND = "substrate.reamde.dev/core/recordmapping"
TRIGGER_KIND = "substrate.reamde.dev/core/trigger"


# A key whose value is a BLOCK SCALAR — a function's `source: |` is python,
# and the kind names inside it are not declarations.
BLOCK_SCALAR = re.compile(r'^(\s*)"?[\w.-]+"?:\s*[|>][-+0-9]*\s*$')
# `<authority>/<package>`: an authority is a dotted host, a package is one word.
# Every reference a manifest makes starts with those two segments, whatever
# follows — `providers.substrate.reamde.dev/google/contact` in a mapping's
# `from:`, in a function's `permissions.reads.kinds`, in a trigger's
# `source.record.kinds`, in a property's `kind:` referent or in a bundle's
# `requires:`.
# An authority has a letter in it: `0.0.0.0/0` in an egress block is a CIDR,
# not a package, and used to hold `enrichlink` back "waiting for 0.0.0.0/0".
PACKAGE_REF = re.compile(r"(?![0-9.]+/)([a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)+)/([a-z0-9][a-z0-9._-]*)")


def declarations_only(text: str) -> str:
    """The manifest with its block scalars and its comments removed, so a
    reference scan reads DECLARATIONS and not prose or python."""
    out, skip = [], None
    for line in text.splitlines():
        if skip is not None:
            if not line.strip() or (len(line) - len(line.lstrip())) > skip:
                continue
            skip = None
        m = BLOCK_SCALAR.match(line)
        if m:
            skip = len(m.group(1))
            continue
        if line.lstrip().startswith("#"):
            continue
        out.append(line.split(" #")[0])
    return "\n".join(out)


# The three PRUNABLE lists. An entry in one of them is a reference the batch
# may drop — that is what the engine's own catalogue import does to a
# suggested mapping whose provider is absent — where a reference anywhere else
# is one the document cannot be parsed without.
LIST_KEYS = ("installs", "requires", "requiresAtLeast")


def key_entries(text: str, key: str) -> list[str]:
    """The entries under one `<key>:` block, as names."""
    out, indent = [], None
    for line in declarations_only(text).splitlines():
        if indent is not None:
            if line.strip() and (len(line) - len(line.lstrip())) > indent:
                out.append(line.strip().lstrip("- ").split(":")[0].strip())
                continue
            indent = None
        m = re.match(r"^(\s*)%s:\s*$" % key, line)
        if m:
            indent = len(m.group(1))
    return [e for e in out if e]


def package_refs(f: pathlib.Path) -> set[str]:
    """Every package one manifest names in a way it cannot be parsed without.

    A mapping's `from:` was the first of these and is not the only one
    (T-052): a function's `permissions.reads.kinds`, the trigger that calls
    it and a kind property's `kind:` referent all name a kind by its full
    path, and each is refused while the package is missing — `unknown type`,
    `unknown referent kind`.

    A bundle's `requires:` is NOT one of them, and neither is `installs:`:
    those two are lists the batch prunes (`prune_lists`), the way the
    catalogue import prunes a suggested mapping. Counting them here would
    hold back a whole package — with the traits `kinds/tasks/task.yaml`
    binds — over a dependency the pruned copy no longer has.
    """
    return {"%s/%s" % m for m in PACKAGE_REF.findall(
        _without_lists(declarations_only(f.read_text())))}


def _without_lists(text: str) -> str:
    """The declarations with the prunable list blocks taken out."""
    out, indent = [], None
    for line in text.splitlines():
        if indent is not None:
            if line.strip() and (len(line) - len(line.lstrip())) > indent:
                continue
            indent = None
        m = re.match(r"^(\s*)(%s):\s*$" % "|".join(LIST_KEYS), line)
        if m:
            indent = len(m.group(1))
            continue
        out.append(line)
    return "\n".join(out)


def mapping_files_sourced_from(pkg: str) -> list[tuple[pathlib.Path, list[str]]]:
    """Every manifest declaring a record mapping whose `from` is inside
    `pkg`, with the mapping ids it declares."""
    out = []
    root = VOCAB
    if not root.is_dir() or not pkg:
        return out
    for f in sorted(root.rglob("*.yaml")):
        if f.name.endswith(".local.yaml") or f.name.startswith("."):
            continue
        ids = []
        for doc in documents(f.read_text()):
            if not re.search(r"(?m)^kind:\s*%s\s*$" % re.escape(MAPPING_KIND), doc):
                continue
            m = re.search(r"(?m)^\s+from:\s*(\S+)\s*$", doc)
            if m and m.group(1).strip("\"'").startswith(pkg + "/"):
                ids.append(doc_id(doc))
        if ids:
            out.append((f, [i for i in ids if i]))
    return out


def declared_ids(f: pathlib.Path) -> set[str]:
    """Every document id one manifest declares — what a bundle `installs:`
    names, plus the records (a trigger) that are not members at all."""
    return {i for i in (doc_id(doc) for doc in documents(f.read_text())) if i}


def files_referencing(pkg: str) -> list[tuple[pathlib.Path, list[str]]]:
    """Every manifest whose DECLARATIONS name `pkg`, with the ids it
    declares — a mapping's `from:`, a function's `permissions.reads.kinds`,
    the trigger that calls it, a property's referent (T-052).

    `mapping_files_sourced_from()` is the narrower question and the two are
    asked in different places: a mapping is what BLOCKS an uninstall, while
    any of these is what makes the re-apply that drops it refuse.
    """
    out = []
    root = VOCAB
    if not root.is_dir() or not pkg:
        return out
    for f in sorted(root.rglob("*.yaml")):
        if f.name.endswith(".local.yaml") or f.name.startswith("."):
            continue
        if pkg in package_refs(f):
            out.append((f, sorted(declared_ids(f))))
    return out


def retire_triggers(api: API, files) -> list[str]:
    """Delete the trigger RECORDS the held-back files declare, and say which.

    A trigger is a record, not a package member, so pruning it out of
    `installs:` does not take it with the function it calls — and the engine
    refuses to drop that function while it is there: "function
    <authority>/people/promotecontact is referenced by live trigger(s)
    [promotecontact-on-contact] — remove or rewire them first" (T-052).
    The apply that brings the file back creates the trigger again, because a
    trigger document IS a record write.
    """
    gone = []
    for f in files:
        for doc in documents(pathlib.Path(f).read_text()):
            if not re.search(r"(?m)^kind:\s*%s\s*$" % re.escape(TRIGGER_KIND), doc):
                continue
            tid = doc_id(doc)
            if not tid:
                continue
            st, _, _ = api.call("DELETE", "/api/v1/%s/%s" % (TRIGGER_KIND, tid))
            if st < 300:
                gone.append(tid)
    if gone:
        say("retired %d trigger record(s) whose function is held back: %s"
            % (len(gone), ", ".join(sorted(gone))))
    return gone


def reapply_without(a, package_dir: pathlib.Path, drop_files: set[pathlib.Path],
                    drop_ids: set[str], label: str) -> bool:
    """Apply one of OUR packages with some documents held out.

    A mapping record cannot be deleted on its own — its package's bundle
    document `installs:` it — so the way to drop one is to re-apply the
    package WITHOUT it: the member leaves `installs:`, and the record goes
    with it. The temp copy is what changes; the files in git never do.
    """
    files, bumped = [], False
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="substrate-e2e-hold-"))
    for f in sorted(package_dir.glob("*.yaml")):
        if f.name.endswith(".local.yaml") or f.name.startswith("."):
            continue
        if f in drop_files:
            continue
        text = f.read_text()
        for did in drop_ids:
            # the `installs:` entry naming a held-back member
            text = re.sub(r"(?m)^\s*-\s*%s\s*$\n" % re.escape(did), "", text)
        # A declaration that ships the same version reaches nobody, and this
        # closure genuinely differs from the one already installed, so its
        # package version has to move — twice, since it moves back after.
        text, n = re.subn(r"(?m)^(  version:\s*)(\d+)\s*$",
                          lambda m: m.group(1) + str(int(m.group(2)) + 1), text)
        bumped = bumped or bool(n)
        out = tmp / f.name
        out.write_text(text)
        files.append(out)
    r = ctl(a, "apply", *sum(([("-f"), str(f)] for f in files), []))
    shutil.rmtree(tmp, ignore_errors=True)
    if r.returncode != 0:
        warn("%s: %s" % (label, (r.stderr or r.stdout).strip()[:300]))
        return False
    return True


def closure_version(pkg: str):
    """The package version the SHIPPED bundle for `name` declares, or None
    when this checkout has no bundle for `pkg`."""
    name = pkg.rsplit("/", 1)[-1]
    f = BUNDLES / name / "bundle.yaml"
    if not pkg or not f.is_file():
        return None
    for doc in documents(f.read_text()):
        if re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/package\s*$", doc):
            m = re.search(r"(?m)^  version:\s*(\d+)\s*$", doc)
            return int(m.group(1)) if m else None
    return None


def installed_package(api: API, pkg: str) -> dict:
    st, body, _ = api.call("GET", "/api/v1/%s/package/%s"
                           % (CORE, urllib.parse.quote(pkg, safe="")))
    if st != 200 or not isinstance(body, dict):
        return {}
    return body.get("properties", {}) or {}


def packages_with_kinds(api: API) -> set[str]:
    """Every package this repository actually HOLDS, read off its kinds.

    `installed_package()` is NOT that test. The `core/package` row survives an
    uninstall — the declaration outlives the installation — so a package torn
    down a minute ago still answers with properties and reads as installed.
    Its KIND declarations go with it, so the repository holds `<pkg>` exactly
    when some `core/kind` id starts with `<pkg>/` (a kind id is
    `<authority>/<package>/<kind>`). T-049.
    """
    out, after = set(), ""
    while True:
        rows, after = api.page("%s/kind" % CORE, API.PAGE, after)
        for r in rows:
            parts = (r.get("id") or "").split("/")
            if len(parts) >= 3:
                out.add("/".join(parts[:2]))
        if not after:
            return out


def mapping_source_packages() -> set[str]:
    """Every package a mapping in the vocabulary tree reads FROM.

    Not the same set as the shipped bundles: a mapping can name a provider
    this tree has no bundle for at all, which is how `people` carried a Notion
    mapping into a repository with no Notion (T-028).
    """
    out = set()
    root = VOCAB
    if not root.is_dir():
        return out
    for f in sorted(root.rglob("*.yaml")):
        if f.name.endswith(".local.yaml") or f.name.startswith("."):
            continue
        for doc in documents(f.read_text()):
            if not re.search(r"(?m)^kind:\s*%s\s*$" % re.escape(MAPPING_KIND), doc):
                continue
            m = re.search(r"(?m)^\s+from:\s*(\S+)\s*$", doc)
            if not m:
                continue
            parts = m.group(1).strip("\"'").split("/")
            if len(parts) >= 3:
                out.add("/".join(parts[:2]))
    return out


STALE, ABSENT = "stale", "absent"


def stale_providers(api: API) -> dict[str, str]:
    """{package -> why} for every mapping source this repository cannot serve,
    and therefore every source whose mappings have to be held back.

    TWO reasons, handled identically (T-049):

      `stale`   the package IS installed, at a version older than the one the
                shipped bundle declares. Mappings sourced from it
                are written against OUR kinds' shapes and cannot be applied
                while the old provider is what the repository holds.
      `absent`  the repository does not hold the package AT ALL — a fresh
                repository with one provider in it, or a teardown that has
                already uninstalled one. A mapping whose `from` kind does not
                exist is refused at parse and takes every document batched
                with it down too; that is what made `people` unapplyable
                during Linear's teardown.

    OLDER than our closure is stale. NEWER is not: every apply whose rewired
    function body differs moves the installed version on (a new mock port did
    that six times in one day), so a higher number is our own shape, re-declared.
    """
    have = packages_with_kinds(api)
    candidates = set(mapping_source_packages())
    pdir = BUNDLES
    for d in sorted(pdir.iterdir()) if pdir.is_dir() else []:
        f = d / "bundle.yaml"
        if not f.is_file():
            continue
        pkg = package_of([f])
        if pkg:
            candidates.add(pkg)
    out: dict[str, str] = {}
    for pkg in sorted(candidates):
        if pkg not in have:
            out[pkg] = ABSENT
            continue
        ours = closure_version(pkg)
        try:
            installed = int(installed_package(api, pkg).get("version"))
        except (TypeError, ValueError):
            continue
        if ours is not None and installed < int(ours):
            out[pkg] = STALE
    return out


def why_held(src: str, state: str) -> str:
    """The reason one mapping file is held back, as the run says it out loud."""
    return ("its source %s is not installed here at all" % src
            if state == ABSENT else
            "its source %s is installed at an older shape" % src)


def unpublish(api: API, a, pkg: str, force: bool = False) -> list[pathlib.Path]:
    """Tear down a PUBLISHED provider so our copy can land.

    The catalogue's providers install with `source: published`, and after that
    only a substrate path may write their declarations — a hand `apply` of the
    same package answers 403. Our providers are those same packages, rewritten,
    so a repository that installed the catalogue's
    google/github/linear/notion/whoop/beeper has to give the package back
    first.

    FOUR obstacles, in the order the engine puts them:

      1. A MAPPING SOURCED FROM THE PACKAGE blocks the uninstall outright —
         "kind …/github/user is the source of mapping …/people/githubuserperson:
         delete that mapping first" — and the mapping cannot be deleted on its
         own either, because the people bundle `installs:` it. So OUR
         package is re-applied without that document first, which drops the
         record; the file is returned so the caller can put it back.
      2. `bundle purge` is refused while the bundle runs — disable first.
      3. `bundle uninstall` is refused while data lives — purge first.
      4. The teardown leaves the bundle DISABLED, and a disabled bundle's
         "configuration and accounts are frozen", so the very next step fails.
         An `enable` after the apply is part of the sequence, not a courtesy.

    A fresh e2e repository has none of this installed and every step is a
    no-op. `slack` is never published — nothing in the catalogue ships it.
    """
    if not pkg:
        return []
    props = installed_package(api, pkg)
    if not props:
        return []
    states = stale_providers(api)
    if states.get(pkg) == ABSENT:
        # The package ROW is still there — it survives an uninstall — but the
        # kinds are gone, so the package is not installed and there is nothing
        # to disable, purge or uninstall. Going on would `die` on an uninstall
        # the engine refuses for a package it does not hold (T-049).
        say("%s has a package row but no kinds — it is not installed here, so "
            "there is nothing to tear down" % pkg)
        return []
    ours = closure_version(pkg)
    if props.get("source") != "published":
        if not getattr(a, "replace", False):
            return []
        if not force:
            # Our own earlier copy. Its version number says little: every
            # apply whose rewired function body differs (a new mock port)
            # moves the installed version on, so a mismatch is not a shape
            # change. The apply itself is the test — it is refused only when
            # stored rows hold an old shape, and THEN the caller tears down
            # (force=True) and applies again. Skipping here also spares the
            # first sync from #576 (a stale function body after uninstall).
            say("%s is installed (ours, version %s; closure %s) — applying over "
                "it; a refusal for stored rows tears it down" % (pkg, props.get("version"), ours))
            return []
    say("%s is installed (%s, version %s; ours is %s) — tearing it down so our "
        "copy can land" % (pkg, props.get("source"), props.get("version"), ours))

    # Every mapping sourced from THIS package has to go (obstacle 1). And the
    # package that declares those mappings (kinds/people, say) may also carry
    # mappings sourced from ANOTHER provider we have rewritten but not yet
    # replaced — written against OUR shapes, they are refused against the old
    # kinds — or from one this repository does not hold at all, whose `from`
    # kind does not even parse. Both are held out here, or the re-apply below
    # is refused whole (T-049: that is what left `people` unapplyable during
    # Linear's teardown).
    sources = [pkg] + [s for s in states if s != pkg]
    by_dir: dict[pathlib.Path, tuple[set, set]] = {}
    for src in sources:
        # For the package being torn down, a MAPPING is the thing that blocks
        # the uninstall. For the others, ANY document naming them is a document
        # the re-apply below cannot parse — `people`'s promotecontact reads a
        # Google mirror, and with Google absent that refusal took the teardown
        # down with it (T-052).
        found = (mapping_files_sourced_from(src) if src == pkg
                 else files_referencing(src))
        for f, ids in found:
            files, dids = by_dir.setdefault(f.parent, (set(), set()))
            files.add(f); dids.update(ids)
            why = ("it names %s as a mapping source, which blocks the uninstall"
                   % pkg if src == pkg else why_held(src, states[src]))
            say("  holding back %s (%s) — %s" % (_rel(f), ", ".join(ids), why))
    held: list[pathlib.Path] = []
    for d, (files, dids) in sorted(by_dir.items()):
        retire_triggers(api, files)
        if reapply_without(a, d, files, dids, "re-applying %s without %s"
                           % (d.name, ", ".join(sorted(f.name for f in files)))):
            held.extend(sorted(files))

    ctl(a, "bundle", "disable", pkg)
    r = ctl(a, "bundle", "purge", pkg, "--yes")
    if r.returncode != 0 and "no data" not in (r.stderr + r.stdout).lower():
        warn("purge %s: %s" % (pkg, (r.stderr or r.stdout).strip()[:200]))
    r = ctl(a, "bundle", "uninstall", pkg)
    if r.returncode != 0:
        die("could not uninstall the published %s, so our copy cannot be "
            "applied:\n%s\n    (if it names a mapping, add that file's package "
            "to kinds/ so the hold-back above can reach it)"
            % (pkg, (r.stderr or r.stdout).strip()[:400]))
    say("%s uninstalled; the package is the repository's to declare again" % pkg)
    return held


def restore_mappings(api: API, a, held: list[pathlib.Path]) -> None:
    """Put the held-back mapping documents back, now that our copy of the
    provider is installed and their `from` kinds resolve again.

    Whatever STOPPED being stale-or-absent comes back. A document whose source
    is still at the old shape, or still not in this repository at all, stays
    held out and is told which of the two it is (T-049)."""
    if not held:
        return
    still: dict[pathlib.Path, set] = {}
    for src, state in sorted(stale_providers(api).items()):
        for f, ids in files_referencing(src):
            if f in held:
                still.setdefault(f, set()).update(ids)
                warn("%s stays held back — %s; %s"
                     % (_rel(f), why_held(src, state),
                        "it comes back when that package lands" if state == ABSENT
                        else "a run of %s restores it"
                        % src.rsplit("/", 1)[-1]))
    for d in sorted({f.parent for f in held}):
        drop = {f for f in still if f.parent == d}
        dids = set().union(*(still[f] for f in drop)) if drop else set()
        say("restoring %s (the held-back mappings%s)"
            % (_rel(d), "" if not drop else ", minus %d still held"
               % len(drop)))
        if not reapply_without(a, d, drop, dids, "restoring " + d.name):
            warn("could not restore %s — apply it by hand"
                 % _rel(d))



# ------------------------------------------------------------ auth and sync


def account_props(api: API, akind: str, aid: str) -> dict:
    _, b, _ = api.call("GET", "/api/v1/%s/%s" % (akind, aid))
    return (b or {}).get("properties", {}) if isinstance(b, dict) else {}


def account_visible(api: API, akind: str, aid: str, seconds: float = 30) -> dict:
    """The account record, waited for until the CREATE is visible (T-050).

    A PUT answers before the row is readable on every path: a Slack run on
    2026-09-17 read `{}` a moment after writing the account and died on it,
    while a straight re-run passed. An empty read is therefore
    retried rather than believed. It still returns `{}` in the end, so a
    caller reads a dict and never a None.
    """
    props = wait_for(lambda: account_props(api, akind, aid) or None, seconds)
    if not props:
        warn("%s/%s did not answer with any properties in %gs — carrying on "
             "with an empty account (the run's own checks say what that "
             "cost)" % (akind, aid, seconds))
    return props or {}


# A provider that bounded its own drain says so in `syncStatus`. Slack stamps
# `ok (3 pending, drain bounded)`; the word is what the run reads, because a
# structured "pending" field is not something every provider declares.
_PENDING_RE = re.compile(r"\b(\d+)\s+pending\b|\bpending\b", re.I)
# GitHub's grammar: `ok (capped: pullsFetch; deferred: pulls x1220, reviews x9)`
_DEFERRED_RE = re.compile(r"\bdeferred:([^;)]*)", re.I)


def pending_work(props: dict) -> int:
    st = str(props.get("syncStatus") or "")
    m = _PENDING_RE.search(st)
    if m:
        return int(m.group(1)) if m.group(1) else 1
    d = _DEFERRED_RE.search(st)
    if d:
        counts = [int(x) for x in re.findall(r"x(\d+)", d.group(1))]
        return sum(counts) or 1
    return 0


def connect_oauth(api: API, akind: str, account_id: str) -> None:
    """The host facility's round trip against the mock's stub."""
    st, body, _ = api.call("POST", "/api/v1/oauth/start", {"record": account_id})
    if st >= 400:
        die("oauth/start refused: %s\n"
            "    (the server needs SUBSTRATE_OAUTH_CALLBACK_URL and a state "
            "key. If this bundle has no oauth2: block it is a TOKEN bundle: "
            "put \"auth\": \"token\" in e2e.json.)" % json.dumps(body)[:400])
    consent = body["url"]
    print("    consent  %s" % consent[:110])
    st, _, headers = api.call("GET", consent, redirect=False)
    location = headers.get("Location") or headers.get("location")
    if st != 302 or not location:
        die("the mock did not redirect the consent (%s)" % st)
    st, _, _ = api.call("GET", location)
    if st >= 400:
        die("the substrate callback refused the code (%s) — the server log "
            "holds the correlation id" % st)

    props = wait_for(lambda: (lambda p: p if p.get("tokenStatus") == "connected"
                              else None)(account_props(api, akind, account_id)), 20)
    if not props:
        die("the account never reached tokenStatus: connected — %s"
            % json.dumps(account_props(api, akind, account_id))[:400])
    say("connected as %s, scopes: %s"
        % (props.get("email") or "(no email on the grant)",
           ", ".join(props.get("grantedScopes") or []) or "(none)"))


def drive_sync(api: API, cfg: dict, akind: str, account_id: str,
               triggers: list[str], a,
               ours: set[str] | None = None) -> tuple[list[dict], dict]:
    """Fire the sync, let it settle, and CONTINUE it while work is pending.

    Two things make this more than one wait. An on-connect trigger is guarded
    to fire ONCE per account — its `when` drops the moment the sync stamps
    `lastSyncedAt` — so an account connected by an earlier run would never
    sync again and the run would report yesterday's rows. And a drain the
    engine bounded (512 invocations, two minutes) legitimately finishes over
    several runs, leaving a queue on the account; calling that settled would
    report a half-walked workspace as done.
    """
    mode_cfg = cfg.get(a.mode) or {}
    settle = float(mode_cfg.get("settleSeconds", cfg.get("settleSeconds", 120)))
    cont = mode_cfg.get("continue", cfg.get("continue")) or {}
    # A cold walk of the OWNER's workspace is an order of magnitude more than
    # a fixture set's two days, so a larger corpus wants a larger budget.
    rounds = int(mode_cfg.get("continueRounds", cfg.get("continueRounds", 6)))

    # THE FIRST READ of the run, moments after the account was PUT. Retried:
    # a create that is not visible yet reads as "never synced", which is the
    # right answer for a fresh account and the wrong one for a re-run, and
    # the empty dict used to travel all the way to `props.get` below (T-050).
    if account_visible(api, akind, account_id).get("lastSyncedAt"):
        refired = False
        for tid in triggers:
            # A trigger run answers when the delivery settles; over a large
            # corpus (Notion: 23,360 recorded requests) that is many minutes,
            # and the 60 s default killed a large run on round 1 (T-051).
            _, b2, _ = api.call("POST", "/api/v1/%s/trigger/%s/run" % (CORE, tid),
                                {"kind": akind, "id": account_id},
                                timeout=RUN_TIMEOUT)
            if isinstance(b2, dict) and b2.get("ran"):
                say("account was already synced; fired %s by hand" % tid)
                refired = True
        if not refired and cont:
            value = cont.get("value", "now")
            value = _now_iso() if value == "now" else value
            say("account was already synced; re-driving it by stamping %s"
                % cont["property"])
            api.call("PATCH", "/api/v1/%s/%s" % (akind, account_id),
                     {"properties": {cont["property"]: value}})
            refired = True
        if not refired and not cont:
            warn("the account was already synced by an earlier run and no "
                 "trigger would re-fire — its `when` drops once lastSyncedAt "
                 "is stamped, which is correct. The rows below are the EARLIER "
                 "run's; a fresh repository is what re-syncs %s." % a.provider)

    statuses = settle_triggers(api, settle, ours)
    # `wait_for` hands back its LAST value, which is None when the wait timed
    # out — a sync still draining at 60 s is ordinary, so this must be a dict
    # either way. The caller reports the missing `lastSyncedAt` itself.
    props = wait_for(lambda: (lambda p: p if p.get("lastSyncedAt") else None)(
        account_props(api, akind, account_id)), 60) or \
        account_props(api, akind, account_id)

    # #576: the first invocation after an apply can run the PREVIOUS body —
    # pinned to a mock that is gone — and stamp `erroring: … Connection
    # refused`. The next invocation runs the body the record holds. Re-drive
    # an erroring first sync through the continuation rule, a few times.
    for i in range(3):
        st = str(props.get("syncStatus") or "")
        if not cont or not st.startswith("erroring"):
            break
        say("first sync erroring (%s) — re-driving (%d/3) by stamping %s"
            % (st[:90], i + 1, cont["property"]))
        value = cont.get("value", "now")
        value = _now_iso() if value == "now" else value
        api.call("PATCH", "/api/v1/%s/%s" % (akind, account_id),
                 {"properties": {cont["property"]: value}})
        statuses = settle_triggers(api, settle, ours)
        props = account_props(api, akind, account_id)

    for i in range(rounds):
        props = account_props(api, akind, account_id)
        left = pending_work(props)
        if not left or not cont:
            break
        value = cont.get("value", "now")
        if value == "now":
            value = _now_iso()
        say("the drain bounded itself with work pending (%s) — continuing "
            "(%d/%d) by stamping %s" % (props.get("syncStatus"), i + 1, rounds,
                                        cont["property"]))
        st, b, _ = api.call("PATCH", "/api/v1/%s/%s" % (akind, account_id),
                            {"properties": {cont["property"]: value}})
        if st >= 400:
            warn("could not stamp the continuation: %s" % json.dumps(b)[:200])
            break
        statuses = settle_triggers(api, settle, ours)
        props = account_props(api, akind, account_id)
    return statuses, props


def run_scenario(a, edir: pathlib.Path, mock: str, recordings: pathlib.Path,
                 akind: str, account_id: str, ckind: str) -> list[str]:
    """Step 7: the provider's own assertions, with the run's facts in the
    environment. The failures it found, as a list the caller extends."""
    scenario = edir / "scenario.py"
    if not scenario.exists():
        warn("no providers/%s/scenario.py — nothing asserted the mirrors"
             % a.provider)
        return []
    say("scenario %s" % _rel(scenario))
    env = dict(os.environ)
    env.update({
        "SUBSTRATE_SERVER": a.server, "SUBSTRATE_TOKEN": a.token,
        "SUBSTRATE_AUTHORITY": a.authority, "MOCK_URL": mock,
        "MODE": a.mode, "PROVIDER": a.provider,
        "ACCOUNT_KIND": akind, "ACCOUNT_ID": account_id,
        "CONFIG_KIND": ckind, "RECORDINGS": str(recordings),
        "PYTHONPATH": str(ROOT / "runner"),
        # The checkout, so a scenario that reads a manifest off disk (the
        # people mappings github and linear assert the engine against) finds
        # it without deriving the layout from its own path.
        "SUBSTRATE_E2E_ROOT": str(ROOT), "SUBSTRATE_E2E_REPO": str(REPO),
    })
    r = subprocess.run([sys.executable, str(scenario)], env=env)
    return [] if r.returncode == 0 else ["scenario.py exited %d" % r.returncode]


def _now_iso() -> str:
    import datetime
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


# ------------------------------------------- the switches, before and after
#
# T-007. A run's closure is pinned to a mock on 127.0.0.1 that dies with the
# run, so ANY later fire of it is an error by construction: the hourly
# schedule stamps `erroring: … Connection refused` on the account, and that
# stamp is the first thing a reviewer sees on the console. The fix is the
# boring one — switch the triggers off when the mock goes, and switch the ones
# this run needs back on when it starts. Both directions are explicit, because
# the apply cannot be relied on for either: `substratectl apply` of a trigger
# document IS a record put (it re-enables), but an apply the engine decides is
# unchanged writes nothing at all, and the declaration in git says
# `enabled: true` for the schedules that a run must have off.


# The `syncFrequency` enum values that mean "do not schedule anything". The
# three providers here all spell it `off`; a provider that spells it
# differently is covered by naming it in e2e.json's `quiesce.syncFrequency`.
OFF_VALUES = ("off", "manual", "never", "none", "paused", "disabled")


def set_trigger_enabled(api: API, tid: str, on: bool) -> bool:
    """Flip one trigger record. They are ordinary records ("get/apply/delete
    them like any other" — substratectl's own help), so this is a PATCH."""
    st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (TRIGGER_KIND, tid),
                           {"properties": {"enabled": on}})
    if st >= 400:
        warn("could not %s trigger %s: %s"
             % ("enable" if on else "disable", tid, json.dumps(body)[:200]))
        return False
    return True


def set_trigger_switches(api: API, files: list[pathlib.Path],
                         keep: list[str]) -> None:
    """The state the RUN needs: the named triggers on, every other trigger of
    this closure off.

    The rewired copy already says this and the apply usually lands it. Usually
    is not always: an apply whose documents are byte-identical to what the
    repository holds is a no-op, so a trigger left disabled by the previous
    run's step 9 would stay disabled and the sync would never fire."""
    ours = trigger_ids(files)
    if not ours:
        return
    wanted = {t: (t in keep) for t in ours}
    st, body, _ = api.call("GET", "/api/v1/%s/trigger/status" % CORE)
    live = {t.get("id"): bool(t.get("enabled"))
            for t in ((body or {}).get("items") or [])} if isinstance(body, dict) else {}
    moved = []
    for tid, on in wanted.items():
        if tid not in live or live[tid] == on:
            continue
        if set_trigger_enabled(api, tid, on):
            moved.append("%s %s" % (tid, "on" if on else "off"))
    if moved:
        say("trigger switches: %s" % ", ".join(moved))


def sync_frequency_off(files: list[pathlib.Path]) -> str:
    """The account kind's own off value for `syncFrequency`, out of the enum
    it declares. Empty when the kind declares no such property, which is not
    an error — a provider may not have a cadence at all."""
    for f in files:
        text = f.read_text()
        m = re.search(r"(?m)^(\s+)syncFrequency:\s*$", text)
        if not m:
            continue
        indent = len(m.group(1))
        block = []
        for line in text[m.end():].splitlines():
            if line.strip() and len(line) - len(line.lstrip()) <= indent:
                break
            block.append(line)
        for value in re.findall(r"(?m)^\s*-\s*value:\s*[\"']?([A-Za-z0-9_-]+)",
                                "\n".join(block)):
            if value.lower() in OFF_VALUES:
                return value
    return ""


def quiesce(api: API, cfg: dict, files: list[pathlib.Path], akind: str,
            account_id: str, provider: str) -> None:
    """Step 9: leave nothing behind that will dial the dead mock.

    ORDER MATTERS. The triggers go off FIRST and the account is patched
    second: the patch is itself a record change, and an on-request trigger
    whose generation guard is still open (a run that ended with work pending,
    or failed) would read it and fire one last invocation at the mock this
    function is called to bury."""
    spec = cfg.get("quiesce") or {}
    if spec.get("skip"):
        return
    keep = list(spec.get("keepTriggers") or [])
    left_off = []
    for tid in trigger_ids(files):
        if tid in keep:
            continue
        if set_trigger_enabled(api, tid, False):
            left_off.append(tid)

    freq = spec.get("syncFrequency", None)
    if freq is None:
        freq = sync_frequency_off(files)
    if freq:
        st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (akind, account_id),
                               {"properties": {"syncFrequency": freq}})
        if st >= 400:
            warn("could not set syncFrequency=%s on %s/%s: %s"
                 % (freq, akind, account_id, json.dumps(body)[:200]))
            freq = ""

    if not left_off and not freq:
        return
    say("quiesced — the mock this closure points at dies with this run, so "
        "nothing is left able to call it")
    if left_off:
        print("    triggers off   %s" % ", ".join(sorted(left_off)))
    if keep:
        print("    triggers left on %s (quiesce.keepTriggers)" % ", ".join(keep))
    if freq:
        print("    account        %s syncFrequency=%s" % (account_id, freq))
    print("    back on with   an apply of the files in git (the closure, "
          "pointed at the LIVE provider),\n"
          "                   or another run of %s, which re-enables what it "
          "needs" % provider)


def points_at_mock(api: API, files: list[pathlib.Path], mock: str,
                   ckind: str, cid: str, akind: str, aid: str) -> str:
    """Where this repository's INSTALLED closure names the mock — or "".

    A scenario-only run applies NOTHING, so what answers its sync is whatever
    the repository already holds, and that is not always the rewired copy an
    earlier run applied: an apply of the files in git puts the LIVE closure
    back (github's installed body reads `https://api.github.com` today). What
    keeps the account pointed at the mock is then the CONFIG record —
    the rewrite lands in `apiBase` — which survives an apply because it is a
    record and not a declaration. Both are checked, plus the account, because
    a token bundle carries its base there.
    """
    host = urllib.parse.urlsplit(mock).netloc
    for kind, rid, what in ((ckind, cid, "the config record %s/%s" % (ckind, cid)),
                            (akind, aid, "the account record %s/%s" % (akind, aid))):
        if not kind or not rid:
            continue
        _, b, _ = api.call("GET", "/api/v1/%s/%s" % (kind, rid))
        if isinstance(b, dict) and host in json.dumps(b.get("properties") or {}):
            return what
    for fid in function_ids(files):
        _, b, _ = api.call("GET", "/api/v1/%s/function/%s"
                           % (CORE, urllib.parse.quote(fid, safe="")))
        if isinstance(b, dict) and host in json.dumps(b.get("properties") or {}):
            return "the installed body of %s" % fid
    return ""


def unquiesce(api: API, cfg: dict, files: list[pathlib.Path], akind: str,
              account_id: str, keep: list[str], mode: str) -> None:
    """Step 9 undone, which is what a run over an ALREADY POPULATED
    repository has to do first (T-054).

    The earlier run left this provider's triggers off and the account's
    `syncFrequency` at its off value, on purpose. A scenario that drives a
    sync stamps `syncRequestedAt` and then waits — for ever, because the
    on-request trigger it is stamping for is disabled. So the switches go back
    to the positions a RUN needs: the kept triggers on, every other trigger of
    this closure off, and the cadence back to the value `e2e.json` declares
    for this mode. The quiesce on the way out puts both back.
    """
    set_trigger_switches(api, files, keep)
    want = (((cfg.get(mode) or {}).get("account") or {}).get("syncFrequency")
            or (cfg.get("account") or {}).get("syncFrequency"))
    if not want:
        warn("e2e.json declares no `account.syncFrequency`, so the account's "
             "cadence stays where the last quiesce left it. The scenario "
             "drives the sync itself, so nothing here needs the schedule, but "
             "the account reads `off` until the next run.")
    else:
        st, body, _ = api.call("PATCH", "/api/v1/%s/%s" % (akind, account_id),
                               {"properties": {"syncFrequency": want}})
        if st >= 400:
            warn("could not restore syncFrequency=%s on %s/%s: %s"
                 % (want, akind, account_id, json.dumps(body)[:200]))
            want = ""
    say("un-quiesced — triggers on: %s%s"
        % (", ".join(keep) or "(none named)",
           ", account %s syncFrequency=%s" % (account_id, want) if want else ""))


def stranded_work(props: dict) -> str:
    """What a quiesce strands: a bounded drain that still had a queue. Said
    out loud, because with the triggers off nothing will pick it up."""
    left = pending_work(props)
    return "" if not left else str(props.get("syncStatus") or "")


# ------------------------------------------------------------- the mock server


def free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def preferred_port(want: int) -> int:
    """The provider's FIXED mock port when it is free, else any free port.

    A stable port keeps the rewired function body byte-identical from run to
    run: the apply is then a no-op for the function, the installed package
    version stands still, and the engine's stale compiled body (#576 — it
    runs the PREVIOUS apply's body) is pinned to an endpoint that is alive."""
    if want:
        deadline = time.time() + 90
        reaped = False
        while True:
            s = socket.socket()
            try:
                s.bind(("127.0.0.1", int(want)))
                s.close()
                return int(want)
            except OSError:
                s.close()
                if not reaped:
                    reaped = True
                    if reap_orphan_mocks(int(want)):
                        time.sleep(1)
                        continue
                if time.time() > deadline:
                    die("mock port %s stayed busy for 90 s — a mock from an "
                        "earlier run is still alive AND STILL PARENTED, so it "
                        "belongs to a run in progress rather than to an "
                        "interrupted one (`pgrep -fl mockserver.py`; "
                        "`lsof -nP -iTCP:%s`). Falling back to another port "
                        "would pin the function body to a dead endpoint "
                        "(#576), so the run stops here." % (want, want))
                say("mock port %s is busy — waiting for it (another run of "
                    "this provider is holding it)" % want)
                time.sleep(3)
    return free_port()


def reap_orphan_mocks(port: int) -> bool:
    """Kill a mockserver left holding `port` by an INTERRUPTED run (T-024).

    The orphan test is the parent, not the port: `tools/e2e.py` starts its mock
    as a child and reaps it in a `finally`, so a mock whose parent is gone
    (PPID 1) is one whose runner died — Ctrl-C, a killed agent, a crashed
    scenario. A mock with a LIVE parent belongs to a run in progress, possibly
    another agent's, and killing that would break their run: those are waited
    for exactly as before.
    """
    try:
        out = subprocess.run(["lsof", "-nP", "-tiTCP:%d" % port, "-sTCP:LISTEN"],
                             capture_output=True, text=True, timeout=10).stdout
    except (OSError, subprocess.SubprocessError):
        return False
    killed = False
    for pid in [p for p in out.split() if p.isdigit()]:
        ps = subprocess.run(["ps", "-o", "ppid=,command=", "-p", pid],
                            capture_output=True, text=True).stdout.strip()
        if "mockserver.py" not in ps:
            continue
        ppid = ps.split(None, 1)[0] if ps.split() else ""
        if ppid != "1":
            say("mock port %d is held by a mockserver whose runner is still "
                "alive (pid %s, parent %s) — waiting rather than killing "
                "somebody else's run" % (port, pid, ppid))
            continue
        warn("killing orphaned mockserver pid %s on port %d (its runner is "
             "gone — an interrupted run)" % (pid, port))
        try:
            os.kill(int(pid), 15)
            killed = True
        except OSError as e:
            warn("could not kill %s: %s" % (pid, e))
    return killed


def start_mock(recordings: pathlib.Path, provider: str, log: pathlib.Path,
               want_port: int = 0, faults: pathlib.Path | None = None):
    port = preferred_port(want_port)
    fh = log.open("w")
    # `providers/<p>/e2e/faults.json`, when the provider agent wrote one, is
    # the run's failure matrix: statuses per endpoint, over the SAME
    # recordings the success path uses. A scenario that wants a failure only
    # for its second sync POSTs the rules to MOCK_URL/__mock/faults instead.
    extra = ["--faults", str(faults)] if faults and faults.is_file() else []
    proc = subprocess.Popen(
        [sys.executable, str(ROOT / "runner" / "mockserver.py"),
         "--dir", str(recordings), "--port", str(port), "--provider", provider,
         *extra],
        stdout=fh, stderr=fh)
    url = "http://127.0.0.1:%d" % port
    for _ in range(60):
        try:
            urllib.request.urlopen(url + "/healthz", timeout=1).read()
            return proc, url
        except Exception:
            if proc.poll() is not None:
                die("the mock server died at once:\n" + log.read_text())
            time.sleep(0.1)
    proc.terminate()
    die("the mock server never answered on " + url)


def mock_get(url: str, path: str):
    with urllib.request.urlopen(url + path, timeout=10) as r:
        return json.load(r)


# ------------------------------------------------------------------- the run


def wait_for(fn, seconds: float, every: float = 0.5):
    deadline = time.time() + seconds
    last = None
    while time.time() < deadline:
        last = fn()
        if last:
            return last
        time.sleep(every)
    return last


def settle_triggers(api: API, seconds: float,
                    only: set[str] | None = None) -> list[dict]:
    """Quiet means: nothing pending, no lag, twice in a row. A single quiet
    poll is a lie — the dispatcher's next pass has not started yet.

    `only` narrows to this provider's own triggers. `trigger status` is
    repository-wide, and a run may land beside other providers: without the
    filter, another repository's busy sync keeps this run waiting and its
    broken callable fails it."""
    quiet, last = 0, []
    deadline = time.time() + seconds
    while time.time() < deadline:
        st, body, _ = api.call("GET", "/api/v1/%s/trigger/status" % CORE)
        items = (body or {}).get("items", []) if isinstance(body, dict) else []
        if only:
            items = [t for t in items if t.get("id") in only]
        last = items
        busy = sum((t.get("lag") or 0) + (t.get("pending") or 0) for t in items)
        quiet = quiet + 1 if busy == 0 else 0
        if quiet >= 2:
            return items
        time.sleep(1.0)
    warn("the triggers did not go quiet in %ds" % seconds)
    return last


# ---------------------------------------------------- the summary a gap needs
#
# T-008's work list is the MISSING lines, and one per 404 is not a work list:
# a Slack run misses `conversations.history` 288 times and the reader has to
# count them by eye to learn that it is ONE gap in one pull. These fold the
# misses into endpoint CLASSES — method plus a path template — with a count and
# one example to record, and print the account's per-stream status beside them
# so "what did this run actually manage" is one screen.


def _path_template(path: str, trie: dict) -> str:
    """One path's template, read off the trie the whole miss set built."""
    node, out = trie, []
    for seg in [s for s in path.split("/") if s]:
        if node.get("collapse"):
            out.append("{*}")
            node = node["kids"]["{*}"]
            continue
        out.append(seg)
        node = node["kids"].get(seg) or {"kids": {}}
    return "/" + "/".join(out)


def _merge(into: dict, other: dict) -> None:
    for seg, sub in other["kids"].items():
        _merge(into["kids"].setdefault(seg, {"kids": {}}), sub)


def _path_trie(paths: list[str]) -> dict:
    """Fold a set of paths by looking at what VARIES at each position.

    A segment position collapses to `{*}` when its values are all numeric — an
    id — or when there are four or more of them, which no real endpoint name
    is; and the subtrees UNDER a collapsed position are merged, so the
    positions after it are judged across the whole class rather than per
    owner. A hundred misses on `/repos/<many>/<many>/pulls/<n>` become the one
    line `/repos/{*}/{*}/pulls/{*} x100`, while `/conversations.history` and
    `/users.info` stay the two separate gaps they are.
    """
    root: dict = {"kids": {}}
    for p in paths:
        node = root
        for seg in [s for s in p.split("/") if s]:
            node = node["kids"].setdefault(seg, {"kids": {}})
    _fold(root)
    return root


def _fold(node: dict) -> None:
    kids = node["kids"]
    node["collapse"] = len(kids) >= 4 or (
        len(kids) > 1 and all(k.isdigit() for k in kids))
    if node["collapse"]:
        merged: dict = {"kids": {}}
        for kid in kids.values():
            _merge(merged, kid)
        node["kids"] = {"{*}": merged}
    for kid in node["kids"].values():
        _fold(kid)


def print_missing_classes(misses: list[dict]) -> None:
    """The 404s as a work list: one line per endpoint class, with a count."""
    if not misses:
        return
    by_method: dict[str, list[dict]] = {}
    for m in misses:
        by_method.setdefault(m.get("method", "GET"), []).append(m)
    say("MISSING, by endpoint — this is the raw-pull work list (T-008)")
    for method in sorted(by_method):
        group = by_method[method]
        trie = _path_trie([m.get("path") or "" for m in group])
        classes: dict[str, list[dict]] = {}
        for m in group:
            classes.setdefault(_path_template(m.get("path") or "", trie), []).append(m)
        for template, rows in sorted(classes.items(), key=lambda kv: -len(kv[1])):
            print("    \033[31m%-6s %-46s\033[0m x%-5d e.g. %s"
                  % (method, template, len(rows),
                     (rows[0].get("looked_for") or [""])[0][:70]))


# The account properties worth a summary line: each stream's own status and
# watermark, whatever the provider calls them (`gmailLastSyncedAt`,
# `contactsSyncStatus`, `syncStatus`, `lastSyncedAt`).
_STREAM_PROP = re.compile(r"(?i)(syncstatus|lastsyncedat|syncerror)$")


def print_stream_status(props: dict) -> None:
    if not props:
        return
    rows = sorted((k, v) for k, v in props.items() if _STREAM_PROP.search(k))
    if not rows:
        return
    say("per-stream status")
    for k, v in rows:
        print("    %-30s %s" % (k, str(v)[:120]))
    for k, v in sorted(props.items()):
        if k.lower().endswith("cursors") and isinstance(v, dict):
            print("    %-30s %d stream%s: %s" % (k, len(v), "" if len(v) == 1 else "s",
                                                 ", ".join(sorted(v)[:8])))


def run(a) -> int:
    # Two directories, not one: the SUITE's (e2e.json, scenario.py, faults)
    # and the shipped BUNDLE's, which lives in the checkout under kinds/ and
    # is read, never written.
    edir = ROOT / "providers" / a.provider
    bdir = BUNDLES / a.provider
    if not edir.is_dir():
        die("no %s — that is the provider's own e2e.json and scenario.py"
            % _rel(edir))
    recordings = pathlib.Path(a.recordings) if a.recordings else (
        ROOT / "fixtures" / a.provider)
    if not recordings.is_dir():
        die("no recordings at %s" % recordings)

    cfg = {}
    cfgf = edir / "e2e.json"
    if cfgf.exists():
        cfg = json.loads(cfgf.read_text())

    files = sorted(bdir.glob("*.yaml"))
    if not files:
        die("no *.yaml under %s — that is the shipped bundle" % _rel(bdir))

    api = API(a.server, a.token)
    work = pathlib.Path(tempfile.mkdtemp(prefix="substrate-e2e-%s-" % a.provider))
    say("provider %s, mode %s, repository %s" % (a.provider, a.mode, a.authority))
    print("    recordings  %s (%d files)" % (recordings, len(list(recordings.glob("*.json")))))
    print("    bundle      %s (%s)" % (_rel(bdir), ", ".join(f.name for f in files)))
    print("    workdir     %s" % work)

    faults = edir / (cfg.get("faults") or "faults.json")
    proc, mock = start_mock(recordings, a.provider, work / "mock.log",
                            int(cfg.get("mockPort") or 0), faults)
    say("mock upstream on %s%s" % (mock, "" if not faults.is_file()
                                   else "  (failure matrix %s)"
                                   % _rel(faults)))

    failures = []
    # Bound before the try: the failure path below quiesces the account this
    # run connected, and it has to be able to say whether there IS one yet.
    akind = account_id = ""
    try:
        # -- SCENARIO ONLY (T-054): steps 1, 7 and 9, and nothing between ---
        # The rows are already there, written over hours by an earlier run, and the
        # assertions are what is being iterated on. So: no rewire, no apply,
        # no config write, no consent flow, no account write, no drive. The
        # mock is up because the INSTALLED closure still dials its fixed port,
        # the switches the earlier run turned off go back on for the scenario's own
        # sync, and the quiesce at the end puts them back.
        if getattr(a, "scenario_only", False):
            triggers = cfg.get("triggers") or [t for t in trigger_ids(files)
                                               if "connect" in t or "webhook" in t]
            mode_cfg = cfg.get(a.mode) or {}
            ckind = cfg.get("configKind") or find_kind_with_trait(files, "oauth2")
            akind = cfg.get("accountKind") or find_kind_with_trait(files, "accountconfig")
            if not akind:
                die("could not find the account kind (trait accountconfig) in "
                    "the bundle — name it as accountKind in e2e/e2e.json")
            cid = mode_cfg.get("configId", cfg.get("configId", "default"))
            aid = a.account_id or mode_cfg.get("accountId", cfg.get("accountId", a.mode))
            if not account_props(api, akind, aid):
                die("no account %s/%s in %s. --scenario-only asserts over a "
                    "repository that is ALREADY POPULATED: it applies nothing, "
                    "connects nothing and creates nothing. Populate it "
                    "first, or name the account with --account-id."
                    % (akind, aid, a.authority))
            account_id = aid
            say("scenario only: %s/%s in %s, asserted where it already is"
                % (akind, account_id, a.authority))
            where = points_at_mock(api, files, mock, ckind, cid, akind, account_id)
            if where:
                say("the installed closure reaches this mock through %s" % where)
            else:
                msg = ("NOTHING installed in %s names %s — not the config "
                       "record, not the account, not a function body. If the "
                       "scenario drives a sync it may reach the LIVE provider "
                       "with a real token. Re-run it, or set the config "
                       "record's apiBase at the mock, before asserting over "
                       "a sync." % (a.authority,
                                         urllib.parse.urlsplit(mock).netloc))
                # 2026-09-17: a scenario-only run over a freshly applied (live)
                # closure drove ~40 page trees against the real Notion API
                # with the owner's token before it was killed. A warning is
                # not a fence; this is.
                if not getattr(a, "allow_live", False):
                    die(msg + " (--allow-live overrides, on purpose only)")
                warn(msg)
            warn("this mock's request log starts EMPTY. What it holds at the "
                 "end describes the SCENARIO's own re-sync, not the run that "
                 "wrote these rows: that run's own log is the only place its "
                 "requests can be read (T-054).")
            unquiesce(api, cfg, files, akind, account_id, triggers, a.mode)
            failures += run_scenario(a, edir, mock, recordings, akind,
                                     account_id, ckind)
            final = account_props(api, akind, account_id)
            print_stream_status(final)
            print_missing_classes(mock_get(mock, "/__mock/misses")["misses"])
            stranded = stranded_work(final)
            quiesce(api, cfg, files, akind, account_id, a.provider)
            if stranded:
                warn("the account still had work queued (%s) and nothing will "
                     "pick it up now — the next run continues it" % stranded)
            return _verdict(a, failures)

        # -- 2/3: rewire and apply -----------------------------------------
        endpoints = oauth_endpoints(files)
        hosts = cfg.get("hosts") or sorted({
            "%s://%s" % (u.scheme, u.netloc)
            for u in (urllib.parse.urlsplit(v) for v in endpoints.values())
            if u.netloc})
        keep = cfg.get("keepHosts") or []
        triggers = cfg.get("triggers") or [t for t in trigger_ids(files)
                                           if "connect" in t or "webhook" in t]
        rewired = rewire(files, work, mock, hosts, keep, triggers)
        say("rewired %s -> %s; triggers kept: %s"
            % (", ".join(hosts), mock, ", ".join(triggers) or "(all)"))

        if not a.skip_apply:
            pkg = package_of(files)
            held = unpublish(api, a, pkg)
            r = ctl(a, "apply", *sum(([("-f"), str(f)] for f in rewired), []))
            if (r.returncode != 0 and getattr(a, "replace", False) and held == []
                    and "would break stored data" in (r.stderr + r.stdout)):
                # Same version number, different shape: the installed copy
                # was declared under our number by an earlier run (a hold-back
                # re-apply moves versions) or the closure moved without its
                # version. The refusal names the rows in the way, and the
                # answer to that is the teardown this run skipped.
                say("apply refused (stored rows hold the old shape) — tearing "
                    "the installed %s down after all" % pkg)
                held = unpublish(api, a, pkg, force=True)
                r = ctl(a, "apply", *sum(([("-f"), str(f)] for f in rewired), []))
            print(r.stdout.strip() or r.stderr.strip())
            if r.returncode != 0:
                die("apply refused the closure:\n" + (r.stderr or r.stdout) +
                    "\n    (a package already installed at a higher version "
                    "needs its `version:` moved; a PUBLISHED one is torn down "
                    "first — see unpublish() above)")
            # A teardown leaves the bundle disabled and a disabled bundle
            # freezes its configuration and accounts, so the next step would
            # fail. Enabling is part of the sequence.
            if held is not None:
                ctl(a, "bundle", "enable", pkg)
            say("applied %s" % (pkg or a.provider))
            restore_mappings(api, a, held)

        # The switch positions this run needs, written to the trigger records
        # themselves rather than assumed from the apply: the previous run's
        # step 9 left them all off, and an apply the engine reads as unchanged
        # writes nothing (T-007).
        set_trigger_switches(api, files, triggers)

        # -- 4: the two records --------------------------------------------
        # e2e.json's names WIN over the trait probe: a TOKEN bundle has no
        # kind wearing `oauth2`, so the probe cannot find its config at all.
        ckind = cfg.get("configKind") or find_kind_with_trait(files, "oauth2")
        akind = cfg.get("accountKind") or find_kind_with_trait(files, "accountconfig")
        if not akind:
            die("could not find the account kind (trait accountconfig) in the "
                "bundle — name it as accountKind in e2e/e2e.json")
        # The honest test is the MANIFEST: a bundle with no oauth2: block is a
        # token bundle. `auth` in e2e.json is the override, not the source.
        auth = cfg.get("auth") or ("oauth" if endpoints else "token")
        if not ckind:
            die("could not find the config kind — name it as configKind in "
                "e2e/e2e.json (a %s bundle has no kind wearing `oauth2`)" % auth)

        # `default` by NAME, and written with PUT: the bundle's `client` input
        # resolves the sole record, the one named `default`, or a bound one —
        # and a second run that POSTed would leave TWO and resolve neither.
        props = dict(cfg.get("config") or {"clientId": "e2e-client-id",
                                           "clientSecret": "e2e-client-secret"})
        props.update((cfg.get(a.mode) or {}).get("config") or {})
        mode_cfg = cfg.get(a.mode) or {}
        cid = mode_cfg.get("configId", cfg.get("configId", "default"))
        st, body, _ = api.call("PUT", "/api/v1/%s/%s" % (ckind, cid),
                               {"properties": props})
        if st >= 400:
            die("config record refused: %s" % json.dumps(body)[:400])
        say("config %s/%s (%s auth)" % (ckind, cid, auth))

        account = dict.fromkeys(cfg.get("toggles") or feature_toggles(files), True)
        account.update(cfg.get("account") or {})
        account.update((cfg.get(a.mode) or {}).get("account") or {})
        # PUT, not POST: the account is addressed by a NAMED id so a second
        # run updates the same account instead of minting a second one, and
        # POST refuses an id outright (the server assigns those).
        aid = a.account_id or mode_cfg.get("accountId", cfg.get("accountId", a.mode))
        if getattr(a, "replace", False):
            # `bundle purge` spares accountconfig rows, and a PUT cannot clear
            # what the connector wrote (cursors, stamps, deferred queues), so
            # a re-run would inherit the previous run's sync state. Start
            # the account over.
            st0, _, _ = api.call("DELETE", "/api/v1/%s/%s" % (akind, aid))
            if st0 < 300:
                say("account %s/%s from the earlier run deleted — starting over" % (akind, aid))
        st, body, _ = api.call("PUT", "/api/v1/%s/%s" % (akind, aid),
                               {"properties": account})
        if st >= 400:
            die("account record refused: %s" % json.dumps(body)[:400])
        account_id = (body or {}).get("id", aid) if isinstance(body, dict) else aid
        say("account %s/%s  toggles: %s"
            % (akind, account_id, ", ".join(sorted(k for k, v in account.items() if v is True))))

        # -- 5: the consent flow, for the bundles that have one -------------
        if auth == "token":
            say("token auth: no consent flow. The credential is on the config "
                "record and the on-connect trigger fires off the account — "
                "`lastSyncedAt` is the only completion marker there is.")
        else:
            connect_oauth(api, akind, account_id)

        # -- 6: the runs settle ---------------------------------------------
        ours = set(trigger_ids(files))
        statuses, stamped = drive_sync(api, cfg, akind, account_id, triggers, a, ours)
        # OUR triggers only. `trigger status` is repository-wide, and a run
        # may land beside other providers: another half-rewritten bundle would
        # otherwise fail this run with "callable does not resolve" for a
        # trigger this provider never installed.
        for t in statuses:
            print("    %-28s %-8s lag=%-4s pending=%-3s parked=%-3s %s" % (
                t.get("id"), t.get("kind"), t.get("lag", 0), t.get("pending", 0),
                t.get("parked", 0), t.get("error", "")))
            if t.get("parked") or t.get("error"):
                failures.append("trigger %s: parked=%s %s"
                                % (t.get("id"), t.get("parked"), t.get("error", "")))
        if stamped:
            say("synced at %s%s" % (stamped.get("lastSyncedAt", "(no lastSyncedAt on the account)"),
                                    ", status %s" % stamped["syncStatus"]
                                    if stamped.get("syncStatus") else ""))
            if pending_work(stamped):
                failures.append("the sync stopped with work still pending: %s "
                                "— the continuation did not converge"
                                % stamped.get("syncStatus"))
        else:
            failures.append("the account never stamped lastSyncedAt")
            warn("the account never stamped lastSyncedAt — the sync did not "
                 "complete; the mock's request log below says how far it got")


        # -- 8: what the run touched ---------------------------------------
        log = mock_get(mock, "/__mock/requests")["requests"]
        hits = [r for r in log if r.get("match") in ("exact", "~fallback", "~no-query")]
        misses = mock_get(mock, "/__mock/misses")["misses"]
        say("upstream: %d calls, %d served, %d missed"
            % (len(log), len(hits), len(misses)))
        for r in hits:
            print("    %-9s %s" % (r["match"], r.get("file")))
        for m in misses:
            print("    \033[31mMISSING\033[0m   %s  (record it as %s)"
                  % (m["path"], m["looked_for"][0]))
        print_missing_classes(misses)

        say("mirror rows")
        for kind in sorted(_declared_kinds(files)):
            n, capped = api.count(kind)
            print("    %-58s %6s" % (kind, ("%d+" % n) if capped else n))

        # -- 7: the provider's own assertions -------------------------------
        failures += run_scenario(a, edir, mock, recordings, akind, account_id, ckind)

        # -- the closing summary --------------------------------------------
        # Re-read both: the scenario's second sync asks for recordings the
        # first one did not, and it is the LAST state of the account that says
        # what this run left behind.
        final = account_props(api, akind, account_id)
        print_stream_status(final)
        print_missing_classes(mock_get(mock, "/__mock/misses")["misses"])

        # -- 9: quiesce (T-007) ---------------------------------------------
        if getattr(a, "quiesce", False):
            stranded = stranded_work(final)
            quiesce(api, cfg, files, akind, account_id, a.provider)
            if stranded:
                warn("the account still had work queued (%s) and nothing will "
                     "pick it up now — the next run continues it" % stranded)
    except BaseException as e:
        # ANY exit that is not the happy one — a crash, a `die()`, a Ctrl-C —
        # kills the mock in the `finally` below, and whatever the engine is
        # still draining then dials a dead port and stamps `erroring:
        # Connection refused` on the account until somebody notices (T-050).
        # So the switches go off on the way out, in every mode: this is the
        # one case where an e2e repository wants it too, because nobody is
        # coming back to that run.
        if akind and account_id:
            warn("the run is ending on %s — quiescing before the mock dies, "
                 "so no drain is left calling it"
                 % (e.__class__.__name__ if not isinstance(e, SystemExit)
                    else "a stop"))
            try:
                quiesce(api, cfg, files, akind, account_id, a.provider)
            except Exception as q:                       # never mask the real one
                warn("could not quiesce on the way out: %s" % q)
        raise
    finally:
        if a.keep:
            warn("mock still running (pid %d) on %s; workdir %s" % (proc.pid, mock, work))
        else:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            shutil.rmtree(work, ignore_errors=True)

    return _verdict(a, failures)


def _verdict(a, failures: list) -> int:
    if failures:
        for f in failures:
            print("\033[31m !! %s\033[0m" % f, file=sys.stderr)
        return 1
    say("%s %s%s: ok" % (a.mode, a.provider,
                         " (scenario only)" if getattr(a, "scenario_only", False)
                         else ""))
    return 0


def _declared_kinds(files: list[pathlib.Path]) -> set[str]:
    out = set()
    for f in files:
        for doc in documents(f.read_text()):
            if re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/kind\s*$", doc):
                i = doc_id(doc)
                if i:
                    out.add(i)
    return out


# ---------------------------------------------------------------- the self-test
#
# `python3 tools/e2e.py --self-test` — every assertion that needs no server and
# no provider directory of its own. It is the regression net under the three
# runner tickets: T-043 (what the rewire may touch), T-049 (stale vs absent)
# and T-050 (a run that ends badly quiesces first).


class _Stub:
    """A minimal stand-in for `API`: the two calls the package readers make,
    plus a catch-all for the record writes a run does."""

    PAGE = 500

    def __init__(self, kinds=(), versions=None, refuse=None, props=None):
        self.kinds, self.versions = list(kinds), dict(versions or {})
        self.refuse, self.seen, self.props = refuse, [], dict(props or {})

    def page(self, kind="", first=200, after="", **filters):
        if kind.endswith("/kind"):
            return [{"id": i} for i in self.kinds], ""
        return [], ""

    def records(self, *a, **k):
        return []

    def count(self, *a, **k):
        return 0, False

    def call(self, method, path, body=None, redirect=True):
        self.seen.append((method, path))
        if self.refuse and self.refuse(method, path):
            return 400, {"error": "refused by the self-test"}, {}
        if path.startswith("/api/v1/%s/package/" % CORE):
            pkg = urllib.parse.unquote(path.rsplit("/", 1)[-1])
            v = self.versions.get(pkg)
            return (200, {"properties": {"version": v, "source": "ours"}}, {}) \
                if v is not None else (404, {}, {})
        if path.endswith("/trigger/status"):
            return 200, {"items": []}, {}
        return 200, {"id": "selftest", "properties": dict(self.props)}, {}


def _check(ok: bool, what: str, fails: list) -> None:
    print("    %s  %s" % ("\033[32mok  \033[0m" if ok else "\033[31mFAIL\033[0m", what))
    if not ok:
        fails.append(what)


def _test_rewire(fails: list) -> None:
    """T-043: the mock host lands in an egress pin and NOWHERE else."""
    host = "127.0.0.1:48111"
    kind_doc = ("kind: substrate.reamde.dev/core/kind\n"
                "id: providers.substrate.reamde.dev/beeper/user\n"
                "spec:\n"
                "  properties:\n"
                "    network:\n"                 # UNQUOTED, the trap T-043 removed
                "      type: string\n")
    out, n = allow_mock_host(kind_doc, host)
    _check((out, n) == (kind_doc, 0),
           "a kind property named `network`, UNQUOTED, survives the rewire "
           "untouched (the workaround in providers/beeper is not what saves it)",
           fails)
    out, n = allow_mock_host(kind_doc.replace("    network:", '    "network":'), host)
    _check(n == 0, "the same property quoted survives too", fails)

    fn = ("kind: substrate.reamde.dev/core/function\n"
          "id: providers.substrate.reamde.dev/x/sync\n"
          "spec:\n"
          "  inputs:\n"
          "    network:\n"
          "      type: string\n"
          "  permissions:\n"
          "    network:\n"
          "      - api.example.com\n"
          "    secrets:\n"
          "      - token\n")
    out, n = allow_mock_host(fn, host)
    _check(n == 1 and out.count(host) == 1, "one egress pin takes the mock host", fails)
    _check('    network:\n      type: string' in out,
           "a `network:` key that is not a child of `permissions:` is left alone", fails)
    _check(out.index(host) > out.index("  permissions:"),
           "the host lands inside the permissions block", fails)

    both = kind_doc + "---\n" + fn
    out, n = allow_mock_host(both, host)
    _check(n == 1 and kind_doc in out,
           "in one file, the bundle document is pinned and the kind document "
           "is returned byte for byte", fails)
    nothing = "kind: substrate.reamde.dev/core/trigger\nid: x/y/z\nspec: {}\n"
    _check(allow_mock_host(nothing, host) == (nothing, 0),
           "a document with nothing to pin round-trips byte for byte", fails)

    for f in sorted(BUNDLES.glob("*/bundle.yaml")):
        # UNQUOTED, always: `"network":` was the workaround a provider had to
        # write against the old regex, and the point of T-043 is that nobody
        # needs it. The COPY read here is unquoted whatever the file says: the
        # shipped bundle is not edited by this test.
        text = f.read_text().replace('"network":', "network:")
        out, n = allow_mock_host(text, host)
        props = len(re.findall(r"(?m)^\s*network:\s*$", "\n".join(_kind_docs(text))))
        _check(n >= 1 and _kind_docs(text) == _kind_docs(out),
               "%s: %d egress pin(s); %d kind propert%s named `network`, "
               "unquoted, untouched" % (_rel(f), n, props,
                                        "y" if props == 1 else "ies"), fails)


def _kind_docs(text: str) -> list[str]:
    return [d for d in documents(text)
            if re.search(r"(?m)^kind:\s*substrate\.reamde\.dev/core/kind\s*$", d)]


def _test_stale_or_absent(fails: list) -> None:
    """T-049: a source package that is ABSENT is held back like a stale one."""
    gh = "providers.substrate.reamde.dev/github"
    ours = closure_version(gh)
    sources = mapping_source_packages()
    _check(gh in sources and len(sources) > 1,
           "the mapping sources are read off the manifests (%d: %s)"
           % (len(sources), ", ".join(sorted(s.rsplit("/", 1)[-1] for s in sources))),
           fails)

    # A fresh repository holding ONE provider: github's kinds are there, and
    # nothing else's are. The `core/package` row is present for ALL of them —
    # that is the lie T-049 is about — so a version-only test would call every
    # one of them current.
    kinds = ["%s/user" % gh, "%s/issue" % gh,
             "samples.substrate.reamde.dev/people/person"]
    versions = {s: ours for s in sources}
    states = stale_providers(_Stub(kinds, versions))
    _check(states.get(gh) is None,
           "the one provider the repository holds is neither stale nor absent", fails)
    absent = {s for s, w in states.items() if w == ABSENT}
    _check(absent >= (sources - {gh}),
           "every OTHER mapping source is absent (%s) even though each still "
           "has a core/package row"
           % ", ".join(sorted(s.rsplit("/", 1)[-1] for s in sorted(absent))), fails)
    _check("not installed here at all" in why_held(gh, ABSENT)
           and "older shape" in why_held(gh, STALE),
           "the hold-back reason names absence and staleness apart", fails)

    if ours:
        older = stale_providers(_Stub(kinds, dict(versions, **{gh: ours - 1})))
        _check(older.get(gh) == STALE, "an OLDER installed version is stale", fails)
        newer = stale_providers(_Stub(kinds, dict(versions, **{gh: ours + 1})))
        _check(newer.get(gh) is None,
               "a NEWER one is not — that is our own shape, re-declared", fails)


class _FakeProc:
    pid = -1

    def terminate(self):
        pass

    def wait(self, timeout=0):
        return 0

    def kill(self):
        pass


def _test_quiesce_on_exit(fails: list) -> None:
    """T-050: ANY exit that is not the happy one quiesces first — but only
    once there is an account to quiesce."""
    if not (ROOT / "fixtures" / "github").is_dir():
        warn("no fixtures/github — skipping the quiesce test")
        return
    g = globals()
    names = ("API", "start_mock", "connect_oauth", "drive_sync", "quiesce")
    saved = {k: g[k] for k in names}
    quiesced = []

    class _Args:
        provider, mode = "github", "e2e"
        server, token = "http://127.0.0.1:1", "self-test"
        authority, ctl = "self-test.localhost", "/nonexistent/substratectl"
        recordings, account_id = "", "selftest"
        keep = skip_apply_done = False
        skip_apply, replace, quiesce = True, False, False

    try:
        g["start_mock"] = lambda *a, **k: (_FakeProc(), "http://127.0.0.1:1")
        g["connect_oauth"] = lambda *a, **k: None
        g["quiesce"] = lambda *a, **k: quiesced.append(a[3:5])

        g["drive_sync"] = lambda *a, **k: (_ for _ in ()).throw(RuntimeError("forced"))
        g["API"] = lambda *a, **k: _Stub()
        out = io.StringIO()
        try:
            with contextlib.redirect_stdout(out):
                run(_Args())
            raised = None
        except BaseException as e:                      # noqa: BLE001 — that is the test
            raised = e
        _check(isinstance(raised, RuntimeError) and str(raised) == "forced",
               "an exception inside the run reaches the caller unchanged", fails)
        _check(len(quiesced) == 1,
               "and the run quiesced exactly once on the way out, before the "
               "mock died", fails)

        quiesced.clear()
        g["API"] = lambda *a, **k: _Stub(
            refuse=lambda m, p: m == "PUT" and "/config/" in p)
        try:
            with contextlib.redirect_stdout(out):
                run(_Args())
            code = 0
        except SystemExit as e:
            code = int(e.code or 0)
        except BaseException as e:                      # noqa: BLE001
            code = repr(e)
        _check(code == 1 and not quiesced,
               "a refusal BEFORE the account exists still exits 1 and quiesces "
               "nothing (there is no account to quiesce)", fails)
    finally:
        g.update(saved)


def _test_scenario_only(fails: list) -> None:
    """T-054: the mode that applies nothing, connects nothing, creates
    nothing — and still quiesces."""
    if not (ROOT / "fixtures" / "github").is_dir():
        warn("no fixtures/github — skipping the scenario-only test")
        return
    g = globals()
    names = ("API", "start_mock", "connect_oauth", "drive_sync", "quiesce",
             "unquiesce", "run_scenario", "mock_get", "ctl", "points_at_mock")
    saved = {k: g[k] for k in names}
    seen: list = []

    class _Args:
        provider, mode = "github", "e2e"
        server, token = "http://127.0.0.1:1", "self-test"
        authority, ctl = "self-test.localhost", "/nonexistent/substratectl"
        recordings, account_id = "", "e2e"
        keep = skip_apply = replace = False
        quiesce, scenario_only = False, True

    def _record(name, ret=None):
        def f(*a, **k):
            seen.append(name)
            return ret
        return f

    try:
        g["start_mock"] = lambda *a, **k: (_FakeProc(), "http://127.0.0.1:48111")
        g["mock_get"] = lambda *a, **k: {"misses": [], "requests": []}
        g["points_at_mock"] = lambda *a, **k: "the config record (stub)"
        g["unquiesce"] = _record("unquiesce")
        g["quiesce"] = _record("quiesce")
        g["run_scenario"] = _record("scenario", [])
        for forbidden in ("connect_oauth", "drive_sync", "ctl"):
            g[forbidden] = _record(forbidden)

        g["API"] = lambda *a, **k: _Stub(props={"lastSyncedAt": "2026-09-17T00:00:00Z"})
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = run(_Args())
        _check(code == 0 and seen == ["unquiesce", "scenario", "quiesce"],
               "scenario-only runs un-quiesce, the scenario and the quiesce, "
               "in that order and nothing else (%s)" % ", ".join(seen), fails)
        _check(not {"connect_oauth", "drive_sync", "ctl"} & set(seen),
               "and never applies, connects or drives a sync of its own", fails)

        seen.clear()
        g["API"] = lambda *a, **k: _Stub()          # an account that reads {}
        try:
            with contextlib.redirect_stdout(out):
                run(_Args())
            code = 0
        except SystemExit as e:
            code = int(e.code or 0)
        _check(code == 1 and "scenario" not in seen,
               "an account that is not there stops the run before the "
               "scenario, rather than creating one", fails)
    finally:
        g.update(saved)


def self_test() -> int:
    fails: list = []
    say("T-043  the rewire touches egress pins and nothing else")
    _test_rewire(fails)
    say("T-049  a source package that is absent is held back like a stale one")
    _test_stale_or_absent(fails)
    say("T-050  a run that ends badly quiesces before the mock dies")
    _test_quiesce_on_exit(fails)
    say("T-054  scenario-only asserts over a populated repository and touches "
        "nothing else")
    _test_scenario_only(fails)
    if fails:
        warn("%d self-test failure(s)" % len(fails))
        return 1
    say("self-test: ok")
    return 0


def main() -> None:
    # The server-free assertions, and a positional-free way in: `--self-test`
    # takes no provider and talks to nothing.
    if "--self-test" in sys.argv[1:]:
        raise SystemExit(self_test())
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("provider")
    # One mode, and a flag rather than a constant because the scenarios read
    # MODE out of their environment and branch on it.
    ap.add_argument("--mode", choices=["e2e"], default="e2e")
    ap.add_argument("--server", default=os.environ.get("SUBSTRATE_SERVER",
                                                       "http://localhost:8081"))
    ap.add_argument("--authority", required=True)
    # SUBSTRATE_TOKEN, not a flag: argv is world-readable through `ps`.
    ap.add_argument("--token", default=os.environ.get("SUBSTRATE_TOKEN", ""),
                    help="prefer the SUBSTRATE_TOKEN environment variable")
    ap.add_argument("--ctl", default=os.environ.get("SUBSTRATECTL", "substratectl"))
    ap.add_argument("--recordings", default="",
                    help="serve these recordings instead of fixtures/<p>")
    ap.add_argument("--account-id", default="",
                    help="the account record id, overriding e2e.json")
    ap.add_argument("--keep", action="store_true",
                    help="leave the mock server and the rewired copy behind")
    ap.add_argument("--skip-apply", action="store_true")
    ap.add_argument("--scenario-only", action="store_true",
                    help="assert over a repository that is ALREADY SEEDED: "
                         "start the mock on the provider's fixed port, put the "
                         "triggers and the cadence the last quiesce took off "
                         "back on, "
                         "run e2e/scenario.py, quiesce again. Applies nothing, "
                         "connects nothing, creates no account and drives no "
                         "sync of its own (T-054)")
    ap.add_argument("--allow-live", action="store_true",
                    help="scenario-only: proceed even when nothing installed "
                         "names the mock (the sync may reach the real provider)")
    ap.add_argument("--replace", action="store_true",
                    help="tear down an INSTALLED copy of the package too, not only a "
                         "published one: a rewritten closure is a narrowing over the "
                         "old rows, so a re-run over a populated repository replaces "
                         "the package whole (disable, purge, uninstall, apply, enable)")
    # Step 9, T-007. Off by default: the repository is a throwaway and the
    # next run wants its triggers exactly as the apply leaves them. A run that
    # ends badly quiesces anyway, whatever this says.
    ap.add_argument("--quiesce", action=argparse.BooleanOptionalAction,
                    default=None,
                    help="after the run, disable this provider's triggers and "
                         "set the account's syncFrequency off, so nothing "
                         "calls the mock that dies with the run")
    a = ap.parse_args()
    if not a.token:
        die("no token: SUBSTRATE_TOKEN, or --token")
    if a.quiesce is None:
        a.quiesce = False
    raise SystemExit(run(a))


if __name__ == "__main__":
    main()
