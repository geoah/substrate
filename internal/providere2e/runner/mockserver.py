#!/usr/bin/env python3
"""Serve a directory of recordings as a provider's API, on loopback.

    python3 runner/mockserver.py --dir fixtures/google --port 8091

No per-provider code: a recording is named for the request it answers, so the
server is a file lookup. The naming rule is `recording_name()` below and the
the capture scripts that cut the fixtures import it, which is the whole
contract: a file they captured is a file this server can serve.

    from mockserver import recording_name
    recording_name("GET", "/calendar/v3/calendars/primary/events",
                   {"maxResults": "250", "singleEvents": "true"})
    -> 'GET_calendar_v3_calendars_primary_events__maxResults-250_singleEvents-true.json'

THE NAME
========

    <METHOD>_<path-slug>[__<query-slug>].json

`METHOD` is upper-case. `path-slug` is the URL path, percent-decoded, stripped
of its leading and trailing `/`, with every character outside `[A-Za-z0-9._-]`
replaced by `_` and every run of `_` collapsed to one. An empty path is
`root`.

`query-slug` is present only when something survives the filters. The query
string is parsed into (key, value) pairs, blank values kept;
**SECRET_PARAMS are dropped** (an access token must never reach a filename);
the rest are sorted by key then value and rendered `key-value`, sanitised the
same way as the path, joined by `_`. A slug longer than 80 characters becomes
its first 60 characters, a `-`, and the first 8 hex of the sha1 of the whole
slug — so a monstrous GraphQL query string still names one stable file.

A request BODY joins the query pairs:

  * `application/x-www-form-urlencoded` — the body's pairs are merged in
    (this is Slack's Web API: `POST /conversations.history` with a form).
  * JSON carrying `operationName` — one extra pair, `op-<operationName>`
    (GitHub's GraphQL).
  * any other non-empty body — one extra pair, `body-<sha1 of the body>[:8]`.

THE LOOKUP
==========

Three tries, in order, and the first file that exists wins:

  1. the exact name;
  2. the name with every PAGINATION_PARAMS pair dropped — so one recording
     answers page 1 and page 2 when a provider agent only captured one, and
     the server logs `~fallback` so nobody mistakes it for a real recording;
  3. the name with no query slug at all.

A miss is `404` with a JSON body naming all three files it looked for, so the
provider agent sees exactly what to record and under what name.

THE RECORDING
=============

Usually just the response body as JSON. Two envelopes exist for the cases a
bare body cannot express:

    {"__response": {"status": 429, "headers": {"Retry-After": "1"}},
     "body": {...}}

    {"__responses": [ <first call>, <second call>, … ]}   # each may itself
                                                          # be an envelope

`__responses` serves its entries in order, repeating the last one forever —
which is how a second sync gets a different answer from the first (an empty
page, a moved `nextPageToken`, a 410 on a stale cursor).

THE OAUTH STUB
==============

The facility's dance needs a provider, so the server is one. Any request whose
LAST PATH SEGMENT is one of these is handled before the recordings are
consulted:

  * `authorize` — 302 to the request's `redirect_uri` carrying `code` and the
    `state` unchanged. No consent page: the redirect IS the consent.
  * `token` / `access_token` — `{access_token, refresh_token, expires_in,
    scope, token_type}`. `grant_type=refresh_token` answers a new access
    token, which is what the facility's refresh loop expects.
  * `revoke` — `{}` and a 200.

The `/me`-style call the facility reads an address off (`emailEndpoint`) is
NOT special: it is an ordinary recording (`GET_user.json`,
`GET_oauth2_v2_userinfo.json`, `GET_users.identity.json`), and the address in
it is a fake one because it came through the pseudonymiser like everything
else.

INTROSPECTION
=============

    GET /__mock/requests      every request served, with the file it hit —
                              a scenario asserts the sync's query windows
                              against this instead of guessing
    GET /__mock/recordings    what is on disk
    GET /__mock/misses        the 404s, i.e. the recordings still to capture
    DELETE /__mock/requests   reset the log between phases of a scenario
    GET /healthz              ok

INJECTED FAILURES
=================

A failure matrix — 429, 500, 401, 404 per endpoint — is a property of the RUN,
not of the recordings: hand-writing a `{"__response": {"status": 429}}` file
per case makes the failure path a different directory from the success path,
and the two then drift. So the statuses are a RULE SET the run configures,
over the recordings it already has.

    --faults providers/<p>/faults.json        a matrix fixed for the whole run
    POST /__mock/faults  {"rules": [...]}     the same, set by a scenario
                                              between two syncs (PUT is the
                                              same thing; DELETE clears)
    GET  /__mock/faults                       the rules with what is left

One rule:

    {"match":   "GET /gmail/v1/users/*/messages*",   the request, METHOD and a
                                                    glob over the path; the
                                                    method is optional
     "contains": "channel-C123",                    optional: a substring that
                                                    must appear in the query or
                                                    the body, which is how a
                                                    form-POST provider (Slack)
                                                    names ONE conversation
     "status":  [429, 429, 0],                      consumed IN ORDER, one per
                                                    matching request; `0` (or
                                                    "pass") serves the recording
                                                    for that turn. When the list
                                                    runs out the rule is spent
                                                    and the endpoint behaves
                                                    normally — which is how a
                                                    retry test ends in success.
     "headers": {"Retry-After": "1"},               optional
     "body":    {"message": "rate limited"}}        optional; the default body
                                                    names the rule and the status

Rules are tried in order and the FIRST match wins. An injected response is
logged in `/__mock/requests` with `"match": "fault"` and the rule that fired,
so a scenario asserts the failure happened rather than assuming it.
"""

from __future__ import annotations

import argparse
import fnmatch
import hashlib
import json
import os
import pathlib
import re
import sys
import threading
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Never in a filename. A recording is committed; a token is not.
SECRET_PARAMS = {
    "access_token", "oauth_token", "token", "key", "api_key", "apikey",
    "client_secret", "code", "refresh_token", "id_token", "signature", "sig",
}
# RELAXABLE TERMS INSIDE A QUERY VALUE. A provider whose incremental cursor is
# not a parameter but a TERM INSIDE one needs the same treatment pagination
# gets, one level down: GitHub's floor rides in `q`
# (`q=type:issue involves:ada updated:>=2026-09-15T20:14:31Z`) and the instant
# is a function of when the previous sync ran, so the exact name can only ever
# answer the request that captured it — and no fixture set could answer a
# SECOND sync. One relaxed candidate, with the window term removed, lets one
# recording answer every window, exactly as the pagination fallback lets one
# recording answer every page.
#
#   {provider or "*": {parameter: regex of the term to drop}}
QUERY_RELAXATIONS: dict[str, dict[str, re.Pattern]] = {
    "*": {
        # `updated:>=2026-09-15T20:14:31Z`, `created:>2026-09-01`, and the `<`
        # forms. GitHub's search grammar; harmless where nobody uses it.
        "q": re.compile(r"\s*(?:updated|created|pushed|merged|closed)"
                        r":[<>]=?[^\s]+"),
    },
}
# Dropped by the query-insensitive fallback: the same call, a different page.
PAGINATION_PARAMS = {
    "pageToken", "page_token", "nextPageToken", "cursor", "next_cursor",
    "page", "per_page", "offset", "after", "before", "startAt", "start",
    "maxResults", "max_results", "limit", "count", "oldest", "latest",
    "inclusive", "syncToken", "sync_token", "startIndex", "pageSize",
}
SLUG_MAX = 80


def _sanitise(s: str) -> str:
    return re.sub(r"_+", "_", re.sub(r"[^A-Za-z0-9._-]", "_", s)).strip("_")


def _pairs(query) -> list[tuple[str, str]]:
    """A query string, a dict (values may be lists), or an already-parsed list
    of pairs — all three, because the callers hold all three."""
    if query is None:
        return []
    if isinstance(query, (list, tuple)):
        return [(str(k), "" if v is None else str(v)) for k, v in query]
    if isinstance(query, dict):
        out = []
        for k, v in query.items():
            for item in (v if isinstance(v, (list, tuple)) else [v]):
                out.append((str(k), "" if item is None else str(item)))
        return out
    return urllib.parse.parse_qsl(str(query).lstrip("?"), keep_blank_values=True)


def query_slug(query, body_pairs: list[tuple[str, str]] | None = None) -> str:
    pairs = [(k, v) for k, v in _pairs(query) + (body_pairs or [])
             if k not in SECRET_PARAMS]
    if not pairs:
        return ""
    slug = "_".join("%s-%s" % (_sanitise(k), _sanitise(v))
                    for k, v in sorted(pairs))
    if len(slug) > SLUG_MAX:
        slug = slug[:60].rstrip("_") + "-" + hashlib.sha1(slug.encode()).hexdigest()[:8]
    return slug


def body_pairs(body: bytes | str | None, content_type: str = "") -> list[tuple[str, str]]:
    """What a request body contributes to the slug. See the module docstring."""
    if not body:
        return []
    raw = body if isinstance(body, str) else body.decode("utf-8", "replace")
    if not raw.strip():
        return []
    if "x-www-form-urlencoded" in (content_type or ""):
        return [(k, v) for k, v in urllib.parse.parse_qsl(raw, keep_blank_values=True)]
    try:
        doc = json.loads(raw)
        if isinstance(doc, dict) and doc.get("operationName"):
            return [("op", str(doc["operationName"]))]
    except (json.JSONDecodeError, UnicodeDecodeError):
        pass
    return [("body", hashlib.sha1(raw.encode("utf-8", "replace")).hexdigest()[:8])]


def path_slug(path: str) -> str:
    return _sanitise(urllib.parse.unquote(path or "").strip("/")) or "root"


def recording_name(method: str, path: str, query=None,
                   body: bytes | str | None = None, content_type: str = "") -> str:
    """THE contract between a capture script and this server."""
    slug = query_slug(query, body_pairs(body, content_type))
    return "%s_%s%s.json" % (method.upper(), path_slug(path),
                             "__" + slug if slug else "")


def relax(pairs: list[tuple[str, str]], provider: str = "") -> list[tuple[str, str]]:
    """Drop every relaxable TERM from inside a query value. See
    QUERY_RELAXATIONS: this is the pagination fallback, one level down."""
    rules = dict(QUERY_RELAXATIONS.get("*", {}))
    rules.update(QUERY_RELAXATIONS.get(provider, {}))
    if not rules:
        return pairs
    return [(k, re.sub(r"\s{2,}", " ", rules[k].sub("", v)).strip()
             if k in rules else v) for k, v in pairs]


def candidate_names(method: str, path: str, query=None,
                    body: bytes | str | None = None,
                    content_type: str = "", provider: str = "") -> list[str]:
    """The names the lookup tries, in order, deduplicated."""
    bp = body_pairs(body, content_type)
    qp = _pairs(query)
    p = path_slug(path)
    thin = lambda pairs: [(k, v) for k, v in pairs if k not in PAGINATION_PARAMS]
    names = [
        query_slug(qp, bp),                                   # exact
        query_slug(relax(qp, provider), relax(bp, provider)),  # window relaxed
        query_slug(thin(qp), thin(bp)),                       # page relaxed
        query_slug(thin(relax(qp, provider)), thin(relax(bp, provider))),
    ]
    out = []
    for slug in names + [""]:
        n = "%s_%s__%s.json" % (method.upper(), p, slug) if slug \
            else "%s_%s.json" % (method.upper(), p)
        if n not in out:
            out.append(n)
    return out


MATCH_WORDS = ["exact", "~relaxed", "~fallback", "~relaxed+fallback", "~no-query"]


# ------------------------------------------------------------------ the server


def _rule_matches(rule: dict, method: str, path: str, query: str,
                  body: bytes) -> bool:
    """`"<METHOD> <glob>"` or `"<glob>"` against the path, plus an optional
    substring that has to appear in the query or the body — which is the only
    way to name ONE conversation in a provider that POSTs a form (Slack sends
    `channel=` in the body, so the path alone cannot tell two apart)."""
    want = rule["match"].strip()
    head, _, tail = want.partition(" ")
    if head.upper() in HTTP_METHODS:
        if head.upper() != method.upper():
            return False
        want = tail.strip() or "*"
    if not fnmatch.fnmatch(path, want) and not fnmatch.fnmatch(path.lstrip("/"), want):
        return False
    if rule["contains"]:
        hay = query + " " + body.decode("utf-8", "replace")
        if rule["contains"] not in hay:
            return False
    return True


HTTP_METHODS = {"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}


class Mock:
    """The state one served directory carries."""

    def __init__(self, directory: pathlib.Path, provider: str = ""):
        self.dir = directory
        self.provider = provider or directory.parent.name
        self.lock = threading.Lock()
        self.requests: list[dict] = []
        self.misses: list[dict] = []
        self.seq: dict[str, int] = {}     # per-file cursor into __responses
        self.codes: dict[str, str] = {}   # authorize code -> granted scope
        self.faults: list[dict] = []      # injected failures, in order

    # -- injected failures ---------------------------------------------------

    def set_faults(self, rules) -> list[dict]:
        """Replace the rule set. Each rule keeps its own queue of statuses, so
        setting them again is what resets a spent matrix."""
        out = []
        for r in (rules or []):
            if not isinstance(r, dict):
                continue
            status = r.get("status")
            if not isinstance(status, list):
                status = [status] if status is not None else []
            out.append({"match": str(r.get("match") or "*"),
                        "contains": r.get("contains") or "",
                        "status": [s for s in status],
                        "headers": dict(r.get("headers") or {}),
                        "body": r.get("body"),
                        "fired": 0})
        with self.lock:
            self.faults = out
        return out

    def fault_for(self, method: str, path: str, query: str, body: bytes):
        """The next injected response for this request, or None.

        Consumes one status from the FIRST rule that matches. A rule whose
        queue is empty matches nothing, so a matrix always ends with the
        endpoint working again — a retry test that could never succeed proves
        only that the mock can say 429."""
        with self.lock:
            for rule in self.faults:
                if not rule["status"]:
                    continue
                if not _rule_matches(rule, method, path, query, body):
                    continue
                status = rule["status"].pop(0)
                rule["fired"] += 1
                if status in (0, "pass", "", None):
                    return None
                out = rule["body"]
                if out is None:
                    out = {"error": "injected", "status": int(status),
                           "rule": rule["match"]}
                return int(status), dict(rule["headers"]), out, rule["match"]
        return None

    def read(self, name: str):
        f = self.dir / name
        try:
            return json.loads(f.read_text())
        except FileNotFoundError:
            return None
        except json.JSONDecodeError as e:
            return {"__response": {"status": 500},
                    "body": {"error": "recording %s is not JSON: %s" % (name, e)}}

    def resolve(self, doc, name: str):
        """Unwrap the envelopes. Returns (status, headers, body)."""
        if isinstance(doc, dict) and "__responses" in doc:
            seq = doc["__responses"]
            with self.lock:
                i = self.seq.get(name, 0)
                self.seq[name] = min(i + 1, len(seq) - 1)
            doc = seq[min(i, len(seq) - 1)]
        if isinstance(doc, dict) and "__response" in doc:
            meta = doc["__response"] or {}
            return int(meta.get("status", 200)), dict(meta.get("headers") or {}), \
                doc.get("body")
        return 200, {}, doc


class Handler(BaseHTTPRequestHandler):
    server_version = "substrate-mockserver/1"
    mock: Mock = None  # type: ignore

    # -- plumbing -----------------------------------------------------------

    def log_message(self, fmt, *a):  # the default logger is noisy and useless
        pass

    def note(self, verdict: str, name: str, status: int):
        sys.stderr.write("%-6s %-58s -> %-52s %s %d\n" % (
            self.command, self.path[:58], name[:52], verdict, status))
        sys.stderr.flush()

    def respond(self, status: int, body, headers: dict | None = None):
        # A STRING body is served as the TEXT it is, not JSON-quoted. A Drive
        # `files.export` answers text/plain, text/csv or text/markdown, and
        # the recording carries that body under a `__response` envelope
        # whose headers name the media type; quoting it would hand the sync
        # a JSON string literal where Google hands it bytes (T-088). A
        # Content-Type the envelope names WINS over the JSON default, so the
        # header is never sent twice.
        if isinstance(body, bytes):
            data = body
        elif isinstance(body, str):
            data = body.encode("utf-8")
        else:
            data = json.dumps(body, ensure_ascii=False).encode()
        self.send_response(status)
        named_type = False
        for k, v in (headers or {}).items():
            if str(k).lower() == "content-type":
                named_type = True
            self.send_header(k, str(v))
        if not named_type:
            self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def read_body(self) -> bytes:
        n = int(self.headers.get("Content-Length") or 0)
        return self.rfile.read(n) if n else b""

    # -- the dispatch -------------------------------------------------------

    def handle_any(self):
        url = urllib.parse.urlsplit(self.path)
        path, query = url.path, url.query
        body = self.read_body()
        ctype = self.headers.get("Content-Type", "")

        if path in ("/healthz", "/__mock/healthz"):
            return self.respond(200, {"ok": True, "dir": str(self.mock.dir)})
        if path.startswith("/__mock/"):
            return self.introspect(path, body)

        # An injected failure outranks everything below it — including the
        # OAuth stub, because "the token endpoint answered 401" is a case a
        # provider has to survive too.
        fault = self.mock.fault_for(self.command, path, query, body)
        if fault is not None:
            status, headers, out, rule = fault
            with self.mock.lock:
                self.mock.requests.append({
                    "method": self.command, "path": path, "query": query,
                    "match": "fault", "status": status, "rule": rule,
                })
            self.note("fault", rule, status)
            return self.respond(status, out, headers)

        last = path.rstrip("/").rsplit("/", 1)[-1].lower()
        if last in ("authorize", "auth", "oauth2"):
            return self.oauth_authorize(query)
        if last in ("token", "access_token"):
            return self.oauth_token(body, ctype)
        if last in ("revoke", "revocation"):
            self.note("oauth", "revoke", 200)
            return self.respond(200, {})

        names = candidate_names(self.command, path, query, body, ctype,
                                self.mock.provider)
        for i, name in enumerate(names):
            doc = self.mock.read(name)
            if doc is None:
                continue
            status, headers, out = self.mock.resolve(doc, name)
            verdict = MATCH_WORDS[i] if i < len(MATCH_WORDS) else "~fallback"
            with self.mock.lock:
                self.mock.requests.append({
                    "method": self.command, "path": path, "query": query,
                    "file": name, "match": verdict, "status": status,
                    "auth": (self.headers.get("Authorization") or "")[:24],
                })
            self.note(verdict, name, status)
            return self.respond(status, out, headers)

        miss = {"method": self.command, "path": path, "query": query,
                "looked_for": names, "dir": str(self.mock.dir)}
        with self.mock.lock:
            self.mock.misses.append(miss)
            self.mock.requests.append({**miss, "match": "miss", "status": 404})
        self.note("MISS", names[0], 404)
        return self.respond(404, {
            "error": "no recording",
            "message": "record one of these under %s" % self.mock.dir,
            **miss,
        })

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = do_HEAD = handle_any

    # -- oauth --------------------------------------------------------------

    def oauth_authorize(self, query: str):
        q = dict(urllib.parse.parse_qsl(query, keep_blank_values=True))
        redirect = q.get("redirect_uri")
        if not redirect:
            self.note("oauth", "authorize", 400)
            return self.respond(400, {"error": "invalid_request",
                                      "error_description": "no redirect_uri"})
        code = "mock-code-" + hashlib.sha1(
            (q.get("state", "") + redirect).encode()).hexdigest()[:12]
        with self.mock.lock:
            self.mock.codes[code] = q.get("scope", "")
            self.mock.requests.append({"method": "GET", "path": "/oauth/authorize",
                                       "query": query, "match": "oauth",
                                       "scope": q.get("scope", ""), "status": 302})
        sep = "&" if "?" in redirect else "?"
        location = "%s%scode=%s&state=%s" % (
            redirect, sep, urllib.parse.quote(code),
            urllib.parse.quote(q.get("state", "")))
        self.note("oauth", "authorize -> " + redirect[:40], 302)
        self.send_response(302)
        self.send_header("Location", location)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def oauth_token(self, body: bytes, ctype: str):
        form = dict(urllib.parse.parse_qsl(body.decode("utf-8", "replace"),
                                           keep_blank_values=True))
        if not form and body:
            try:
                form = json.loads(body)
            except json.JSONDecodeError:
                form = {}
        code = form.get("code", "")
        grant = form.get("grant_type", "authorization_code")
        scope = self.mock.codes.get(code) or form.get("scope", "")
        tag = hashlib.sha1((code or form.get("refresh_token", "") or "x").encode()
                           ).hexdigest()[:10]
        out = {
            "access_token": "mock-access-" + tag,
            "refresh_token": "mock-refresh-" + tag,
            "expires_in": 3600,
            "token_type": "Bearer",
            "scope": scope,
        }
        with self.mock.lock:
            self.mock.requests.append({"method": "POST", "path": "/oauth/token",
                                       "grant_type": grant, "match": "oauth",
                                       "scope": scope, "status": 200})
        self.note("oauth", "token (%s)" % grant, 200)
        self.respond(200, out)

    # -- introspection ------------------------------------------------------

    def introspect(self, path: str, body: bytes = b""):
        what = path.rsplit("/", 1)[-1]
        if what == "faults":
            if self.command in ("POST", "PUT"):
                try:
                    doc = json.loads(body or b"{}")
                except json.JSONDecodeError as e:
                    return self.respond(400, {"error": "not JSON: %s" % e})
                rules = doc.get("rules") if isinstance(doc, dict) else doc
                out = self.mock.set_faults(rules)
                self.note("faults", "%d rules" % len(out), 200)
                return self.respond(200, {"rules": out})
            if self.command == "DELETE":
                self.mock.set_faults([])
                return self.respond(200, {"rules": []})
            return self.respond(200, {"rules": self.mock.faults})
        if self.command == "DELETE" and what == "requests":
            with self.mock.lock:
                self.mock.requests.clear()
                self.mock.misses.clear()
            return self.respond(200, {"ok": True})
        if what == "requests":
            return self.respond(200, {"requests": self.mock.requests})
        if what == "misses":
            return self.respond(200, {"misses": self.mock.misses})
        if what == "recordings":
            return self.respond(200, {
                "dir": str(self.mock.dir),
                "recordings": sorted(f.name for f in self.mock.dir.glob("*.json")),
            })
        return self.respond(404, {"error": "no such introspection endpoint"})


def load_faults(path) -> list:
    """The rule set out of a JSON file: `{"rules": [...]}` or a bare list."""
    doc = json.loads(pathlib.Path(path).read_text())
    return doc.get("rules", []) if isinstance(doc, dict) else (doc or [])


def build(directory: pathlib.Path, port: int = 0, provider: str = "",
          faults=None):
    """One server over one directory. The handler is a SUBCLASS per server, so
    two mocks in one process do not share the `mock` class attribute — which
    a scenario running its own second-pass mock beside the runner's would."""
    mock = Mock(directory, provider)
    if faults:
        mock.set_faults(faults)
    handler = type("Handler_%s" % (provider or "mock"), (Handler,), {"mock": mock})
    httpd = ThreadingHTTPServer(("127.0.0.1", port), handler)
    return httpd, mock


def serve_bg(directory, port: int = 0, provider: str = ""):
    """Start the mock on a daemon thread. Returns (base_url, httpd, mock).

    For a scenario that needs to make assertions WHILE the server runs — a
    second sync pointed at a second recordings directory, say. `httpd.shutdown()`
    stops it.
    """
    directory = pathlib.Path(directory)
    httpd, mock = build(directory, port, provider)
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return "http://127.0.0.1:%d" % httpd.server_address[1], httpd, mock


def serve(directory: pathlib.Path, port: int, provider: str = "",
          faults=None) -> None:
    httpd, mock = build(directory, port, provider, faults)
    n = len(list(directory.glob("*.json"))) if directory.is_dir() else 0
    sys.stderr.write("mockserver: %s (%d recordings) on http://127.0.0.1:%d%s\n"
                     % (directory, n, httpd.server_address[1],
                        "" if not mock.faults
                        else " (%d injected-failure rules)" % len(mock.faults)))
    sys.stderr.flush()
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--dir", required=True, type=pathlib.Path)
    ap.add_argument("--port", type=int, default=0, help="0 picks a free one")
    ap.add_argument("--provider", default="")
    ap.add_argument("--faults", default="", type=str,
                    help="a JSON file of injected-failure rules (see the "
                         "module docstring); a scenario can also POST them to "
                         "/__mock/faults mid-run")
    ap.add_argument("--name", nargs="*", metavar="METHOD PATH [QUERY]",
                    help="print the recording name for a request and exit")
    a = ap.parse_args()
    if a.name:
        m, p = a.name[0], a.name[1]
        q = a.name[2] if len(a.name) > 2 else ""
        print(recording_name(m, p, q))
        return
    if not a.dir.is_dir():
        a.dir.mkdir(parents=True, exist_ok=True)
        sys.stderr.write("mockserver: %s did not exist; created it empty\n" % a.dir)
    serve(a.dir, a.port, a.provider,
          load_faults(a.faults) if a.faults else None)


if __name__ == "__main__":
    main()
