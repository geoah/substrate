# The shared runner's Python host: one long-lived child process hosting
# every installed `runtime: python` function body. Speaks the runner's
# JSON-lines protocol, version 6 (one frame per line; functions/runner's
# protocol.go is the contract):
#
#   parent -> host   {"op": "register", "reqId": N, "id": <key>, "source": <inline body>}
#                    {"op": "deregister", "reqId": N, "id": <key>}
#                    {"op": "describe", "reqId": N}
#                    {"op": "invoke", "reqId": N, "id": <key>, "input": {...}}
#   host -> parent   {"kind": "response", "reqId": N, "ok": true, ...}
#                    {"kind": "response", "reqId": N, "ok": false, "error": "..."}
#   host -> parent   {"kind": "call", "reqId": N, "host": "get"|"list"|"search"|"call"|"units.convert"|
#                     "units.canonical"|"units.measure", "params": {...}}
#                    ... answered by the parent's next line:
#                    {"kind": "reply", "reqId": N, "ok": true, "result": {...}}
#   host -> parent   {"kind": "log", "reqId": N, "line": "..."}   (no answer)
#
# The protocol stream is the ORIGINAL stdout, dup'd at startup and private to
# this file: sys.stdout is rebound so a body's print() lands in the current
# invocation's capped logs, never raw on the wire, and sys.stdin is rebound to
# /dev/null so a body's input() cannot eat protocol frames. Inline source is
# exec'd at registration into its own module namespace, keyed by the
# installation (repository + function + hash), so no state is shared across
# repositories or functions; `main(input, host)` is the entrypoint. A body error
# is an error frame the engine parks — never a host crash. Isolation is the
# process boundary (user code never loads into the substrate); tightening
# placement is a later dial on the same protocol.
import datetime
import decimal
import hashlib
import json
import os
import re
import sys
import threading
import traceback

MAX_LOG_LINES = 200
MAX_LOG_CHARS = 4096
MAX_FRAME_BYTES = 8 << 20  # below the parent's 16 MiB scanner ceiling

# The protocol stream is detached from user code by _bootstrap() before the
# loop runs — NOT at import. Keeping the fd dup, the sys.stdout/stdin rebind and
# the module-path append out of import time lets the pure helpers (the id
# hashing, slugging and builder validation) be imported by a test process
# without hijacking its stdio. The runner always execs this file as a script,
# where __main__ calls _bootstrap() first, so production is unchanged.
PROTO_OUT = None
PROTO_IN = None

FUNCS = {}


class Logs:
    """One invocation's capped log sink. Every line it keeps goes to the parent
    as a `log` frame the moment it is written, not with the response: the
    runner kills a body at its timeout, and the lines a killed body wrote are
    what says where its time went. `req_id` None keeps the lines here only."""

    def __init__(self, req_id=None):
        self.lines = []
        self.dropped = 0
        self.req_id = req_id

    def add(self, msg):
        s = str(msg)
        if len(s) > MAX_LOG_CHARS:
            s = s[:MAX_LOG_CHARS] + "...[truncated]"
        if len(self.lines) >= MAX_LOG_LINES:
            self.dropped += 1
            return
        self.lines.append(s)
        self._send(s)

    def close(self):
        """The count of the lines past the cap, as the invocation's last line."""
        if self.dropped:
            self._send("... %d more log lines dropped" % self.dropped)

    def _send(self, line):
        if self.req_id is not None:
            send(self.req_id, {"line": line}, kind="log")

    def out(self):
        if self.dropped:
            return self.lines + ["... %d more log lines dropped" % self.dropped]
        return self.lines


class StdoutCapture:
    """sys.stdout for the whole host: body prints land in the CURRENT
    invocation's logs (prefixed so they read as what they are), which frame
    them, never raw on the protocol stream. Outside an invocation, writes are
    dropped."""

    def __init__(self):
        self.logs = None
        self._buf = ""

    def write(self, s):
        self._buf += str(s)
        while "\n" in self._buf:
            line, self._buf = self._buf.split("\n", 1)
            if self.logs is not None and line:
                self.logs.add("[stdout] " + line)
        return len(s)

    def flush(self):
        pass


CAPTURE = StdoutCapture()


def _bootstrap():
    """Detach the protocol stream from user code and put the bundle's shared
    modules on the path — the side effects the loop needs, kept OUT of import so
    the pure helpers stay importable. The runner execs this file as a script, so
    __main__ runs this before the loop.

    The shared modules ride at the END of sys.path, appended AFTER the protocol
    FDs are detached and the stdlib is already resolvable — never on the startup
    PYTHONPATH. Two consequences: a `sitecustomize.py`/`usercustomize.py` in the
    module dir can NOT auto-run at interpreter startup (the dir is not on the
    path when the interpreter boots), and a module named `json.py` can NOT
    shadow the host's serializer (stdlib, earlier on the path, wins). Names are
    also validated at admission (schema/bundle.go rejects reserved and stdlib
    top-level names). isdir guards the dependency-free / no-module case."""
    global PROTO_OUT, PROTO_IN
    PROTO_OUT = os.fdopen(os.dup(1), "w")
    PROTO_IN = sys.stdin
    sys.stdout = CAPTURE
    sys.stdin = open(os.devnull)
    mod_dir = os.environ.get("SUBSTRATE_PY_MODULES")
    if mod_dir and os.path.isdir(mod_dir):
        sys.path.append(mod_dir)


# One frame per line, whole: a body that logs from a thread of its own while
# its main thread writes a host call must not interleave the two.
SEND_LOCK = threading.Lock()


def send(req_id, obj, kind="response"):
    obj["kind"] = kind
    obj["reqId"] = req_id
    try:
        data = json.dumps(obj)
    except (TypeError, ValueError) as e:
        # A user value the body handed back (a staged handle, a set, a bespoke
        # object) is NOT serializable. This runs outside the loop's exception
        # handler on the SHARED host, so a raw json.dumps failure here would
        # take the whole process — and every other repository's function — down.
        # Answer the frame with a clean ok:false instead.
        data = json.dumps({
            "kind": kind, "reqId": req_id, "ok": False,
            "error": "response is not JSON-serializable: %s (a staged handle or "
                     "the effects buffer is never a return value)" % e,
        })
    if len(data) > MAX_FRAME_BYTES:
        data = json.dumps({
            "kind": kind, "reqId": req_id, "ok": False,
            "error": "response frame of %d bytes exceeds the %d byte cap"
                     % (len(data), MAX_FRAME_BYTES),
        })
    with SEND_LOCK:
        PROTO_OUT.write(data + "\n")
        PROTO_OUT.flush()


class HostError(Exception):
    pass


# --- the SDK: deterministic ids, paging, typed reads, buffered effects -------
#
# The runner passes ONE `host` object to every body (`main(input, host)`). It
# carries the capability-scoped reads (get/list/search/call, unchanged) and,
# namespaced beside them, the SDK surface:
#
#   host.records.get/list/search   typed, type-scoped reads (committed state)
#   host.records.aggregate         the aggregate read, budgeted like a list
#   host.functions.call             function-to-function composition
#   host.agents.call                run an agent to settlement, its reply back
#   host.effects.put/patch/delete/  the buffered-effects builder — each APPENDS
#     link/unlink/merge/split       a staged effect and returns a handle
#   host.ids.external/url           deterministic, hash-backed, URL-safe ids
#   host.page.resume / .more()      the paged-checkpoint wrapper
#   host.units.convert/canonical/   the engine's unit table, over the host call
#     measure
#   host.observed.trace/local_day/  pure-Python builders for observed values
#     period_id
#
# Writes STAY effects: the builder never writes mid-body, it accumulates the
# effect list the engine already applies atomically after the body returns.
# Reads never see the buffer — the engine's computed view would make a local
# overlay lie.


# The shared id/identifier alphabets — mirrors of the engine's schema/naming.go
# so the SDK rejects locally exactly what the engine rejects at admission.
_MAX_ID_LEN = 128
# reID: a record id is RFC 3986 unreserved plus ":" "@" and "/" (a
# the two extra pchars ":" and "@" — schema.ValidID.
_RE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$")
# A kind reference, in full: "<authority>/<package>/<name>". A bare name is
# refused here, before the call leaves the body, exactly as the engine refuses
# it at the door (decision record 0101).
_RE_KIND = re.compile(r"^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+/[a-z][a-z0-9]*/[a-z][a-z0-9]*$")
# A relation / declared name: camelCase — schema.ValidCamel.
_RE_IDENT = re.compile(r"^[a-z][a-zA-Z0-9]*$")

# The kind a proposal lands as: effects.propose stages an ordinary put of one.
_KIND_RECORDPATCHREQUEST = "substrate.reamde.dev/core/recordpatchrequest"


def _slugify(s):
    """Lowercase, then keep ONLY ASCII [a-z0-9] and collapse every other run to
    one dash. ASCII-only (no Unicode letters/digits, no case folding): a
    non-ASCII numeric like halfwidth ½, which Python's str.isalnum() once kept,
    is rejected by the engine's ASCII-only id alphabet."""
    out, prev_dash = [], False
    for ch in str(s):
        if "A" <= ch <= "Z":
            ch = chr(ord(ch) + 32)  # ASCII lowercase only
        if ("a" <= ch <= "z") or ("0" <= ch <= "9"):
            out.append(ch)
            prev_dash = False
        elif not prev_dash:
            out.append("-")
            prev_dash = True
    return "".join(out).strip("-")


def _id_hash(*parts):
    # Length-prefixed so ("ab","c") and ("a","bc") can never collide — the
    # foot-gun a truncated URL slug walked straight into.
    h = hashlib.sha256()
    for p in parts:
        p = "" if p is None else str(p)
        b = p.encode("utf-8")
        h.update(str(len(b)).encode() + b":" + b + b"|")
    return h.hexdigest()[:32]


def _need_component(fn, field, value):
    if not isinstance(value, str) or value == "":
        raise ValueError("ids.%s: %s is a required non-empty string" % (fn, field))
    return value


class Ids:
    """Deterministic, URL-safe, hash-backed id helpers. A function composes the
    ids of what it writes; hashing the provider key removes the truncate-a-URL
    collision foot-gun (the reading list's old `page-<slug>`). Every component is a
    required non-empty string, and the human slug is ASCII-folded. runner's
    ids.go recomputes these in Go for the engine's tests, so the two are held
    to one golden (testdata/id_vectors.json)."""

    def external(self, provider, account, external_id):
        """A stable id for one external record: provider + account + its id."""
        provider = _need_component("external", "provider", provider)
        account = _need_component("external", "account", account)
        external_id = _need_component("external", "external_id", external_id)
        slug = _slugify(provider)[:48]
        digest = _id_hash(provider, account, external_id)
        return (slug + "-" + digest) if slug else digest

    def url(self, url):
        """A stable id for one URL. SAFE V1: the exact URL is hashed with only
        surrounding-ASCII-whitespace trimming — NO canonicalization. Distinct
        spellings (case, default port, trailing slash, query/fragment bytes)
        are therefore DISTINCT ids by design: a lossy normalizer that merged
        `?next=/` with `?next=` would silently drop one page. A structural
        canonicalizer, when needed, arrives as a separate, clearly named
        helper — this one never guesses two URLs are the same page."""
        u = _need_component("url", "url", url).strip(" \t\n\r\f\v")
        if u == "":
            raise ValueError("ids.url: url is empty after trimming whitespace")
        slug = _slugify("url")[:48]
        digest = _id_hash("url", "", u)
        return (slug + "-" + digest) if slug else digest


class Page:
    """The paged-checkpoint wrapper. `resume()` returns the opaque cursor the
    previous page of this invocation chain returned (None on a fresh delivery).
    `more(cursor)` builds the continuation a paged body returns as its `more`,
    so bodies stop hand-building `{"cursor": ...}` dicts. `resume` is a METHOD,
    never an attribute, so a body that forgets the call gets an error rather
    than a bound method that is always truthy."""

    def __init__(self, resume):
        self._resume = resume

    def resume(self):
        return self._resume

    def more(self, cursor):
        if cursor is None:
            raise ValueError(
                "page.more: cursor is required — a None continuation commits a "
                "null cursor and replays page one; return no `more` to signal drained")
        return {"cursor": _json_copy("page.more", "cursor", cursor)}


class StagedEffect:
    """A handle to one buffered effect — NOT a record, and NEVER a return
    value: returning a handle (or the buffer) is refused, and would not
    serialize. Reads never reflect it; the engine applies the whole buffer
    atomically after the body returns."""

    __slots__ = ("_ef",)

    def __init__(self, ef):
        self._ef = ef

    @property
    def action(self):
        return self._ef.get("action")

    @property
    def kind(self):
        return self._ef.get("kind")

    @property
    def id(self):
        return self._ef.get("id")

    def __repr__(self):
        return "StagedEffect(%s %s %s)" % (self.action, self.kind, self.id)


def _json_copy(action, field, value):
    """Deep-copy a caller value through JSON so staging captures a SNAPSHOT (a
    body reusing one dict across a loop stages each call's value, not the last),
    and reject anything not JSON-compatible AT the offending builder call rather
    than at the far serialization boundary."""
    try:
        return json.loads(json.dumps(value))
    except (TypeError, ValueError) as e:
        raise ValueError(
            "effects.%s: %s is not JSON-serializable: %s" % (action, field, e)
        ) from e


def _need_kind(action, value):
    if not isinstance(value, str) or value == "":
        raise ValueError("effects.%s: kind is required" % action)
    if not _RE_KIND.match(value):
        raise ValueError("effects.%s: %r is not a kind reference (<authority>/<package>/<name>)" % (action, value))
    return value


def _need_id(action, field, value):
    if not isinstance(value, str) or value == "":
        raise ValueError("effects.%s: %s is required" % (action, field))
    if len(value) > _MAX_ID_LEN or not _RE_ID.match(value):
        raise ValueError(
            "effects.%s: %s %r is not a record id (URL-path-safe, at most %d characters)"
            % (action, field, value, _MAX_ID_LEN))
    return value


def _need_bool(action, field, value):
    if not isinstance(value, bool):
        raise ValueError("effects.%s: %s is a boolean, got %s"
                         % (action, field, type(value).__name__))
    return value


def _opt_map(action, field, value):
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ValueError("effects.%s: %s is a map, got %s" % (action, field, type(value).__name__))
    return _json_copy(action, field, value)


# The default for if_version: a private sentinel distinct from None, so an
# OMITTED precondition (unguarded write) is distinguishable from an explicit
# None. Passing None is a typo class (a missed .get("version")) that would
# otherwise silently drop the guard — it is refused.
_UNSET = object()


def _if_version(action, value):
    if value is _UNSET:
        return None  # omitted → an unguarded write
    if value is None:
        raise ValueError(
            "effects.%s: if_version=None is not a precondition — omit if_version for an "
            "unguarded write, or pass an integer version (e.g. host.version(record))" % action)
    # bool is an int subclass in python — reject it, an integer version is meant.
    if isinstance(value, bool) or not isinstance(value, int):
        raise ValueError("effects.%s: if_version is an integer version, got %s"
                         % (action, type(value).__name__))
    if value < 0:
        raise ValueError("effects.%s: if_version is a non-negative version, got %d" % (action, value))
    return value


# The lost-race policies a guarded write may declare. "park" is the default and
# need not be spelled; "yield" says losing is an ordinary outcome, so the whole
# delivery rolls back, writes nothing and settles as a skip instead of parking.
_ON_CONFLICT = ("park", "yield")


def _on_conflict(action, value, if_version):
    if value is None:
        return None
    if value not in _ON_CONFLICT:
        raise ValueError("effects.%s: on_conflict is one of %s, got %r"
                         % (action, ", ".join(repr(v) for v in _ON_CONFLICT), value))
    if if_version is None:
        raise ValueError(
            "effects.%s: on_conflict needs if_version — without a precondition there is "
            "no race to lose" % action)
    return value


class Effects:
    """The buffered-effects builder. Each method APPENDS a staged effect to a
    write-only buffer and returns a StagedEffect handle (never a record, never
    a return value); the runner returns the buffer as the delivery's effects.
    Because staging captures a JSON snapshot, one mode is used per invocation:
    a body that ALSO returns an explicit effect list while the buffer is
    non-empty is refused at return (the two apply orders are unrelated and can
    self-conflict under CAS). No flush() — the buffer IS the return. Shapes are
    validated here (a known action, a well-formed id and type, a boolean
    ifAbsent, a non-negative integer ifVersion, a known onConflict) so a
    mistake is a clear body error that parks once, not an engine park; the
    engine stays authoritative for the emit ceiling and type admission."""

    def __init__(self):
        self._staged = []

    def _add(self, ef):
        self._staged.append(ef)
        return StagedEffect(ef)

    def put(self, kind, id, properties=None, if_absent=False, if_version=_UNSET,
            on_conflict=None):
        ef = {"action": "put", "kind": _need_kind("put", kind),
              "id": _need_id("put", "id", id)}
        props = _opt_map("put", "properties", properties)
        if props is not None:
            ef["properties"] = props
        if _need_bool("put", "if_absent", if_absent):
            ef["ifAbsent"] = True
        v = _if_version("put", if_version)
        if v is not None:
            if if_absent:
                raise ValueError(
                    "effects.put: if_absent and if_version cannot combine — if_absent makes an "
                    "existing row a no-op before the version check; pick one")
            ef["ifVersion"] = v
        oc = _on_conflict("put", on_conflict, v)
        if oc is not None:
            ef["onConflict"] = oc
        return self._add(ef)

    def patch(self, kind, id, properties=None, if_version=_UNSET, on_conflict=None):
        ef = {"action": "patch", "kind": _need_kind("patch", kind),
              "id": _need_id("patch", "id", id)}
        props = _opt_map("patch", "properties", properties)
        if props is not None:
            ef["properties"] = props
        v = _if_version("patch", if_version)
        if v is not None:
            ef["ifVersion"] = v
        oc = _on_conflict("patch", on_conflict, v)
        if oc is not None:
            ef["onConflict"] = oc
        return self._add(ef)

    def delete(self, kind, id):
        return self._add({"action": "delete", "kind": _need_kind("delete", kind),
                          "id": _need_id("delete", "id", id)})

    def merge(self, kind, id, loser):
        winner = _need_id("merge", "id", id)
        loser = _need_id("merge", "loser", loser)
        if winner == loser:
            raise ValueError("effects.merge: winner and loser are the same id %r" % winner)
        return self._add({"action": "merge", "kind": _need_kind("merge", kind),
                          "id": winner, "loser": loser})

    def split(self, kind, merge):
        return self._add({"action": "split", "kind": _need_kind("split", kind),
                          "merge": _need_id("split", "merge", merge)})

    def propose(self, id, target_kind, target_id, diff=None, op="patch", rationale=None):
        """Stage a REVIEWED change instead of writing one: the put of a change
        request the owner decides on, so nothing lands until somebody accepts
        it. `id` is the request's own id (a replayed delivery re-proposes the
        same request, never a second one); op is patch (the default), create or
        delete; target_kind/target_id name the record the change is about — the
        existing target of a patch or delete, the record a create would mint;
        `diff` carries the proposed values, wrapped under "properties" or as a
        plain property map the engine wraps. Ordinary sugar: the effect is a put
        of the request kind, so a proposing function names that kind in its
        emit and needs nothing else."""
        if op not in ("patch", "create", "delete"):
            raise ValueError("effects.propose: op %r is not patch, create or delete" % op)
        rid = _need_id("propose", "id", id)
        if not isinstance(target_kind, str) or target_kind == "":
            raise ValueError(
                "effects.propose: target_kind is required — a proposal names the kind it is about")
        _need_kind("propose", target_kind)
        _need_id("propose", "targetId", target_id)
        if op == "delete":
            # Presence, not content: the engine's admission refuses ANY diff on a
            # delete request, an empty one included, so staging {} here would only
            # park the delivery later.
            if diff is not None:
                raise ValueError(
                    "effects.propose: op delete proposes no values — drop the diff or propose a patch")
        elif not diff:
            raise ValueError(
                "effects.propose: op %s needs a diff — name at least one property to change" % op)
        props = {"op": op}
        if rationale is not None:
            if not isinstance(rationale, str):
                raise ValueError("effects.propose: rationale is a string, got %s"
                                 % type(rationale).__name__)
            props["rationale"] = rationale
        d = _opt_map("propose", "diff", diff)
        if d is not None:
            props["diff"] = d
        ef = {"action": "put", "kind": _KIND_RECORDPATCHREQUEST, "id": rid,
              "properties": props}
        if op == "create":
            props["targetKind"], props["targetId"] = target_kind, target_id
        else:
            # The request names its target with an unpinned reference, so the
            # value is the referent's full "<kind>/<id>" path.
            props["target"] = "%s/%s" % (target_kind, target_id)
        return self._add(ef)


def _as_kinds(kinds):
    if isinstance(kinds, str):
        return [kinds]
    return list(kinds or [])


class SearchResult(list):
    """A search answer: iterable as its hits, with `pending`, the number of
    properties the drain has yet to buy vectors for. Non-zero beside a ranking
    means the semantic arm saw a partial index."""

    def __init__(self, hits, pending=0):
        super().__init__(hits)
        self.hits = self
        self.pending = pending


class Records:
    """Typed, type-scoped, budget-aware reads over the host's read calls. A
    forbidden type or an exhausted budget surfaces as a HostError carrying the
    engine's reason (read outside the reads allowlist / read budget exhausted).
    Reads see COMMITTED state — never this delivery's staged effects."""

    def __init__(self, host):
        self._host = host

    def get(self, kind, id):
        """One record by its FULL reference: kind + id (a bare id names
        nothing). None when absent — absence is a normal answer."""
        return self._host.get(kind, id)

    def list(self, kinds, where=None, first=None, after=None, order=None):
        """A page of records: the kinds to read, `where` the filter's property
        conditions, `first`/`after` the page and cursor, and `order` the sort.
        `order` takes either shape the records route takes — the compact string
        ("at", "at:desc,createdAt"; a missing direction means ascending) or a
        list of {"property": ..., "desc": bool}."""
        flt = {"kinds": _as_kinds(kinds)}
        if where:
            flt["properties"] = where
        q = {"filter": flt}
        if first is not None:
            q["first"] = first
        if after:
            q["after"] = after
        if order:
            q["orderBy"] = order
        return self._host.list(**q)

    def aggregate(self, body):
        """The aggregate read (docs/api.md): `body` is the records route's
        parameters as one document, {"filter": ..., "aggregate": ...,
        "downsample": ...}. `filter.kinds` names the observed kinds read (and
        `aggregate.bucket.records.filter.kinds` the records mode's), each held
        to the reads allowlist. Returns the result dict: `rows`, each with
        `start`, `end` and `values`. One call, its rows against the row
        budget."""
        return self._host.aggregate(body)

    def search(self, q, kinds, k=None, mode=None):
        params = {"q": q, "kinds": _as_kinds(kinds)}
        if k is not None:
            params["k"] = k
        if mode:
            params["mode"] = mode
        return self._host.search(**params)


class Functions:
    """Function-to-function composition, gated by permissions.call. The
    callee's effects accumulate into THIS delivery's transaction."""

    def __init__(self, host):
        self._host = host

    def call(self, function, input=None):
        return self._host.call(function, input)


class Agents:
    """Run an agent to settlement, gated by permissions.agents -> a dict of
    reply, thread and status. The agent commits its own writes as it runs,
    under its own actor, so they do not wait for this delivery and do not
    roll back with it."""

    def __init__(self, host):
        self._host = host

    def call(self, agent, input):
        return self._host.call(agent, input)


# --- units and observed values ------------------------------------------------
#
# host.units asks the ENGINE's unit table over the host call, so a body and the
# write path that holds its effects read one table; there is no Python copy of
# it. host.observed builds in pure Python the trace documents core's
# `observed(packed)` admits from a body's own points, the labels a trace id
# and a daily summary are composed from, and the stored spellings of numbers,
# durations and instants; its `reconcile` is the one call that reads and
# stages. Its rules mirror the engine's (vocabulary/observed.go); runner's
# observed tests hold the two together.

_RE_DECIMAL = re.compile(r"^[+-]?[0-9]+(\.[0-9]+)?$")
_RE_ISO_DURATION = re.compile(
    r"^P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$")
_EPOCH = datetime.datetime(1970, 1, 1, tzinfo=datetime.timezone.utc)
_US = {"W": 7 * 86400 * 10**6, "D": 86400 * 10**6, "H": 3600 * 10**6, "M": 60 * 10**6, "S": 10**6}
_MAX_UTC_OFFSET = 1080
_TRACE_FORMS = ("gauge", "amount", "counter")
_RESET_RULES = ("monotonic", "signed", "wrap")
# The write's limits on one trace (vocabulary.MaxTracePoints and
# MaxTraceBytes). The builder fills the byte limit less 64 KiB, which it
# leaves for the properties a writer adds to each document (origins,
# revision, adapter).
_MAX_TRACE_POINTS = 20000
_TRACE_BYTES_BUDGET = (1 << 20) - (64 << 10)


def _decimal_text(fn, value):
    """A number as the exact digits the engine stores. A str is held to the
    decimal grammar and an int or a Decimal is written out; a float is written
    as its shortest repr, which is the digits a JSON payload carried, so pass a
    str or a Decimal to keep digits a float cannot hold."""
    if isinstance(value, bool):
        raise ValueError("%s: %r is a bool, not a number" % (fn, value))
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        if value != value or value in (float("inf"), float("-inf")):
            raise ValueError("%s: %r is not a finite number" % (fn, value))
        value = decimal.Decimal(repr(value))
    if isinstance(value, decimal.Decimal):
        if not value.is_finite():
            raise ValueError("%s: %r is not a finite number" % (fn, value))
        return format(value, "f")
    if isinstance(value, str) and _RE_DECIMAL.match(value):
        return value
    raise ValueError('%s: %r is not a decimal: pass digits as a str ("72.4"), an int or a Decimal'
                     % (fn, value))


def _duration_us(fn, field, value):
    """An ISO 8601 duration (weeks, days and a time part, as the engine reads
    one) in whole microseconds."""
    m = _RE_ISO_DURATION.match(value) if isinstance(value, str) else None
    if not m or not any(m.groups()):
        raise ValueError("%s: %s %r is not an ISO 8601 duration (PT1H, PT5S, P1D)" % (fn, field, value))
    total = decimal.Decimal(0)
    for digits, unit in zip(m.groups(), "WDHMS"):
        if digits:
            total += decimal.Decimal(digits) * _US[unit]
    if total != total.to_integral_value():
        raise ValueError("%s: %s %r is finer than a microsecond" % (fn, field, value))
    return int(total)


def _format_duration(us):
    """The one stored spelling of a duration, as the engine writes it: days,
    then a time part, zero components omitted, PT0S for zero."""
    days, us = divmod(us, 86400 * 10**6)
    h, us = divmod(us, 3600 * 10**6)
    mi, us = divmod(us, 60 * 10**6)
    sec, frac = divmod(us, 10**6)
    out = "P" + ("%dD" % days if days else "")
    if h or mi or sec or frac or not days:
        out += "T"
        if h:
            out += "%dH" % h
        if mi:
            out += "%dM" % mi
        if frac:
            out += "%d.%sS" % (sec, ("%06d" % frac).rstrip("0"))
        elif sec or not (h or mi or days):
            out += "%dS" % sec
    return out


def _aware(fn, field, at):
    if not isinstance(at, datetime.datetime) or at.tzinfo is None or at.utcoffset() is None:
        raise ValueError("%s: %s is a timezone-aware datetime, got %r" % (fn, field, at))
    return at


def _offset_minutes(fn, field, offset):
    seconds = offset.total_seconds()
    if seconds % 60 or abs(seconds) > _MAX_UTC_OFFSET * 60:
        raise ValueError("%s: %s's UTC offset is whole minutes within %d either way, got %s"
                         % (fn, field, _MAX_UTC_OFFSET, offset))
    return int(seconds // 60)


def _epoch_us(at):
    delta = at.astimezone(datetime.timezone.utc) - _EPOCH
    return (delta.days * 86400 + delta.seconds) * 10**6 + delta.microseconds


def _instant(us):
    """An instant in microseconds since the epoch as RFC 3339 UTC."""
    t = _EPOCH + datetime.timedelta(microseconds=us)
    if t.microsecond:
        return t.strftime("%Y-%m-%dT%H:%M:%S.") + ("%06d" % t.microsecond).rstrip("0") + "Z"
    return t.strftime("%Y-%m-%dT%H:%M:%SZ")


class Converted(str):
    """A converted value's decimal digits, a str, with `exact`: False where the
    result has no finite decimal expansion and was rounded to 20 significant
    digits."""

    exact = True


class Units:
    """The engine's unit table, asked over the host call: every code, alias and
    factor is the one the write path holds an effect to. Errors are the
    engine's, raised as HostError."""

    def __init__(self, host):
        self._host = host

    def convert(self, value, from_unit, to_unit, measure=None):
        """Convert exact digits between two units of one measure -> a decimal
        str whose `exact` says whether it is every digit. Aliases are read
        (`°C`, `kWh`, `mmHg`). Every digit comes back when the result has a
        finite decimal expansion; one without is rounded to 20 significant
        digits and `exact` is False. The value holds at most 1000 digits.
        `measure` names the measure to convert in where the units' own reading
        is not meant: a deviation in Cel converts to [degF] in
        `temperature-difference`, without the offset."""
        params = {"value": _decimal_text("units.convert", value),
                  "from": from_unit, "to": to_unit}
        if measure is not None:
            params["measure"] = measure
        out = self._host._call("units.convert", params)
        converted = Converted(out.get("value"))
        converted.exact = out.get("exact") is not False
        return converted

    def canonical(self, code):
        """The code the engine stores for a unit spelling: `bpm` is `/min`,
        `°C` is `Cel`, `kWh` is `kW.h`."""
        return self._host._call("units.canonical", {"code": code}).get("code")

    def measure(self, code):
        """The name of the measure a unit belongs to: `mass` for `kg`, `rate`
        for `/min`. An ambiguous or unknown code raises."""
        return self._host._call("units.measure", {"code": code}).get("measure")


def _ref_path(value):
    """A stored reference's record path, in either spelling ({"ref": path} or
    the bare path)."""
    if isinstance(value, dict):
        value = value.get("ref")
    return value if isinstance(value, str) else ""


def _same_value(a, b):
    """Whether a stored property value is the one a write would store: a
    reference in either spelling is its path, and a None member of a map is
    absent."""
    if isinstance(a, dict) and set(a) == {"ref"} and isinstance(b, str):
        return a["ref"] == b
    if isinstance(b, dict) and set(b) == {"ref"} and isinstance(a, str):
        return b["ref"] == a
    if isinstance(a, dict) and isinstance(b, dict):
        keys = {k for k, v in a.items() if v is not None} | {k for k, v in b.items() if v is not None}
        return all(_same_value(a.get(k), b.get(k)) for k in keys)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(_same_value(x, y) for x, y in zip(a, b))
    if isinstance(a, bool) or isinstance(b, bool):
        return a is b
    return a == b


class Observed:
    """Builders for observed values (docs/observations.md). `trace`,
    `local_day`, `period_id`, `decimal`, `duration`, `instant` and
    `record_id` are pure Python: they compute and never write. `reconcile`
    reads an origin's stored outputs and stages the writes and deletes that
    bring them to what the origin implies now."""

    def __init__(self, host=None):
        self._host = host

    def trace(self, points, *, metric, source, unit, form, period="PT1H", hold=None,
              about=None, channel=None, reset_rule=None, wrap_at=None, delta=None,
              circular=None, start=None, end=None):
        """Pack points into the trace documents a kind binding
        `observed(packed)` admits -> a list of property maps, oldest first,
        each ready for host.effects.put(<trace kind>, <id>, properties).

        `points` is [(at, value)] for numbers or [(at, code)] for codes, `at` a
        timezone-aware datetime and a value or code None where the source had
        nothing (unavailable from that point on). Each document covers one
        fixed UTC period (`period`, aligned to the epoch: PT1H is the UTC
        hour) and is split where the points' UTC offset changes, so every
        document has one offset; a later part of a split period starts at its
        first point. A part is split again before it passes 20,000 points or
        the byte budget, so the write never refuses it for its size. Points
        regularly spaced from the document's start are written with `every`,
        anything else with `offsets` in milliseconds.

        `start` and `end`, timezone-aware datetimes read to the millisecond,
        bound the span this delivery covers: each document claims only the
        part of its period inside [start, end), so two deliveries that share
        an hour write two documents side by side. A point outside them
        raises.

        Each document names both keys of every exclusive pair, the one it
        does not use as None (`every` or `offsets`, `values` or `codes`, and
        `unit` on a trace of codes), so a put of it over a stored trace of the
        other shape replaces that shape whole.

        `reset_rule="wrap"` needs `wrap_at`, the value a counter wraps to zero
        at; `delta` and `circular` mark temperature differences and angles.
        `metric`, `source` and `about` are reference values (a full record
        path or {"ref": path}). Ids are the caller's: compose each from
        `period_id(document["at"], period)`."""
        fn = "observed.trace"
        if form not in _TRACE_FORMS:
            if form == "summary":
                raise ValueError("%s: a summary is one value per record: write an observation" % fn)
            raise ValueError("%s: form is one of %s, got %r" % (fn, ", ".join(_TRACE_FORMS), form))
        p_us = _duration_us(fn, "period", period)
        if p_us <= 0:
            raise ValueError("%s: period is longer than zero" % fn)
        if hold is not None:
            if form != "gauge":
                raise ValueError("%s: only a gauge holds its value; a %s does not" % (fn, form))
            _duration_us(fn, "hold", hold)
        if reset_rule is not None:
            if form != "counter":
                raise ValueError("%s: only a counter resets; a %s does not" % (fn, form))
            if reset_rule not in _RESET_RULES:
                raise ValueError("%s: reset_rule is one of %s, got %r" % (fn, ", ".join(_RESET_RULES), reset_rule))
        if wrap_at is not None:
            if reset_rule != "wrap":
                raise ValueError("%s: wrap_at is the value a wrapping counter returns to zero at: "
                                 "pass reset_rule=\"wrap\"" % fn)
            wrap_at = _decimal_text(fn, wrap_at)
            if decimal.Decimal(wrap_at) <= 0:
                raise ValueError("%s: wrap_at is above zero, got %s" % (fn, wrap_at))
        elif reset_rule == "wrap":
            raise ValueError("%s: a wrapping counter names the value it wraps at: pass wrap_at" % fn)
        for name, flag in (("delta", delta), ("circular", circular)):
            if flag is not None and not isinstance(flag, bool):
                raise ValueError("%s: %s is True, False or None, got %r" % (fn, name, flag))
        lo = hi = None
        if start is not None:
            lo = _epoch_us(_aware(fn, "start", start))
            lo -= lo % 1000
        if end is not None:
            hi = _epoch_us(_aware(fn, "end", end))
            hi += -hi % 1000
        if lo is not None and hi is not None and hi <= lo:
            raise ValueError("%s: end is after start" % fn)
        coded, pts = None, []
        for i, point in enumerate(points):
            if not isinstance(point, (tuple, list)) or len(point) != 2:
                raise ValueError("%s: point %d is an (at, value) pair" % (fn, i))
            at, v = point
            _aware(fn, "point %d's at" % i, at)
            offset = _offset_minutes(fn, "point %d" % i, at.utcoffset())
            if v is not None:
                is_code = isinstance(v, str) and not _RE_DECIMAL.match(v)
                if coded is None:
                    coded = is_code
                elif coded != is_code:
                    raise ValueError("%s: point %d mixes a %s into a trace of %s"
                                     % (fn, i, "code" if is_code else "number", "codes" if coded else "numbers"))
                if is_code:
                    if v == "":
                        raise ValueError("%s: point %d's code is empty" % (fn, i))
                else:
                    v = _decimal_text(fn, v)
            t = _epoch_us(at)
            if (lo is not None and t < lo) or (hi is not None and t >= hi):
                raise ValueError("%s: point %d at %s is outside [start, end)" % (fn, i, _instant(t)))
            pts.append((t, i, offset, v))
        if not pts:
            return []
        coded = bool(coded)
        if coded and unit is not None:
            raise ValueError("%s: a trace of codes carries no unit: pass unit=None" % fn)
        if not coded and not unit:
            raise ValueError("%s: a trace of numbers names the unit every value is in" % fn)
        if coded and form == "counter":
            raise ValueError("%s: a counter is a running total, a number, never a code" % fn)
        if coded and (delta or circular):
            raise ValueError("%s: delta and circular describe numbers; a trace of codes has none" % fn)
        pts.sort()
        # Pieces: one per fixed period, split where the offset changes and
        # before a piece passes the point limit. A later piece of a split
        # period starts at its first point, floored to the millisecond its
        # offsets count in, and a split never falls inside one millisecond.
        pieces = []
        for t, _, offset, v in pts:
            period_start = t - t % p_us
            last = pieces[-1] if pieces else None
            if last is None or last["period"] != period_start:
                first = period_start if lo is None else max(period_start, lo)
                pieces.append({"period": period_start, "start": first, "offset": offset, "points": []})
            elif t // 1000 > last["points"][-1][0] // 1000 and (
                    last["offset"] != offset or len(last["points"]) >= _MAX_TRACE_POINTS):
                pieces.append({"period": period_start, "start": t - t % 1000, "offset": offset, "points": []})
            pieces[-1]["points"].append((t, v))
        common = {"metric": metric, "form": form, "source": source, "about": about, "channel": channel,
                  "unit": None if coded else unit, "hold": hold, "resetRule": reset_rule,
                  "wrapAt": wrap_at, "delta": delta, "circular": circular}
        keep = ("metric", "form", "unit")  # written as given, None included

        def build(piece_start, piece_end, offset, piece_points):
            props = {"metric": metric, "form": form, "at": _instant(piece_start), "endsAt": _instant(piece_end),
                     "utcOffsetStart": offset, "utcOffsetEnd": offset}
            for key, value in common.items():
                if value is not None or key in keep:
                    props[key] = value
            times = [t for t, _ in piece_points]
            steps = {b - a for a, b in zip(times, times[1:])}
            every = steps.pop() if len(steps) == 1 else 0
            if len(times) > 1 and times[0] == piece_start and every > 0 \
                    and every * len(times) <= piece_end - piece_start:
                props["every"], props["offsets"] = _format_duration(every), None
            else:
                props["every"], props["offsets"] = None, [(t - piece_start) // 1000 for t in times]
            listed = [v for _, v in piece_points]
            props["codes"], props["values"] = (listed, None) if coded else (None, listed)
            return props

        docs = []

        def emit(piece_start, piece_end, offset, piece_points):
            doc = build(piece_start, piece_end, offset, piece_points)
            if len(json.dumps(doc, separators=(",", ":"))) > _TRACE_BYTES_BUDGET:
                # Halve at a millisecond boundary nearest the middle.
                cuts = [k for k in range(1, len(piece_points))
                        if piece_points[k][0] // 1000 > piece_points[k - 1][0] // 1000]
                if cuts:
                    k = min(cuts, key=lambda c: abs(c - len(piece_points) // 2))
                    cut = piece_points[k][0] - piece_points[k][0] % 1000
                    emit(piece_start, cut, offset, piece_points[:k])
                    emit(cut, piece_end, offset, piece_points[k:])
                    return
            docs.append(doc)

        for i, piece in enumerate(pieces):
            nxt = pieces[i + 1] if i + 1 < len(pieces) else None
            if nxt and nxt["period"] == piece["period"]:
                piece_end = nxt["start"]
            else:
                piece_end = piece["period"] + p_us
                if hi is not None:
                    piece_end = min(piece_end, hi)
            emit(piece["start"], piece_end, piece["offset"], piece["points"])
        return docs

    def local_day(self, at, utc_offset_minutes):
        """The civil date at `at` where the clock was `utc_offset_minutes`
        east of UTC, as YYYY-MM-DD: the day a daily summary or a recorded-local
        reading belongs to."""
        fn = "observed.local_day"
        _aware(fn, "at", at)
        if isinstance(utc_offset_minutes, bool) or not isinstance(utc_offset_minutes, int) \
                or abs(utc_offset_minutes) > _MAX_UTC_OFFSET:
            raise ValueError("%s: utc_offset_minutes is whole minutes within %d either way, got %r"
                             % (fn, _MAX_UTC_OFFSET, utc_offset_minutes))
        local = at.astimezone(datetime.timezone.utc) + datetime.timedelta(minutes=utc_offset_minutes)
        return local.date().isoformat()

    def decimal(self, value):
        """A number as the digits the engine stores, never with an exponent:
        an int as itself, a float as its shortest repr written out (1e-05 is
        "0.00001", the digits a JSON payload carried), a Decimal or a digit
        string as it is. None is None; a bool, NaN, an infinity or any other
        value raises."""
        if value is None:
            return None
        return _decimal_text("observed.decimal", value)

    def duration(self, seconds):
        """A length of time, as seconds (an int, a float, a Decimal or digits)
        or a timedelta, in the one ISO 8601 spelling the engine stores
        ("PT7H54M49S", "P1DT2H"), rounded to the microsecond. A negative
        length raises."""
        fn = "observed.duration"
        if isinstance(seconds, datetime.timedelta):
            us = (seconds.days * 86400 + seconds.seconds) * 10**6 + seconds.microseconds
        else:
            d = decimal.Decimal(_decimal_text(fn, seconds))
            us = int((d * 10**6).to_integral_value(rounding=decimal.ROUND_HALF_EVEN))
        if us < 0:
            raise ValueError("%s: %r is a negative length of time" % (fn, seconds))
        return _format_duration(us)

    def instant(self, at):
        """A timezone-aware datetime as the RFC 3339 UTC text the engine
        stores ("2026-10-07T06:00:00Z"), to the microsecond."""
        return _instant(_epoch_us(_aware("observed.instant", "at", at)))

    def record_id(self, provider, account, *parts):
        """An output's id composed from upstream data: `<provider>/<part>/...`
        as written where it is a record id (the id alphabet, at most 128
        bytes), else host.ids.external(provider, account, that text), so an
        upstream identifier outside the alphabet or past the length never
        refuses the write."""
        readable = "/".join([str(provider)] + [str(p) for p in parts])
        if len(readable.encode("utf-8")) <= _MAX_ID_LEN and _RE_ID.match(readable):
            return readable
        return Ids().external(provider, account, readable)

    def reconcile(self, origins, outputs, *, owned, adapter=None, stored=None):
        """Write what an origin implies now and delete what it no longer
        implies -> {"written": n, "unchanged": n, "deleted": n}.

        `origins` is the record path of the row the outputs are built from, or
        a list of paths where one delivery answers for several rows.
        `outputs` maps (kind, id) to the properties the rows imply. `owned`
        maps every output kind to each property the body sets on it: a write
        names them all, None for one the rows no longer imply, and a put's
        None deletes the key, so one put replaces what an earlier write set.
        `adapter` is stamped on every output where given.

        The stored side is every record of an owned kind whose `origin`, or
        an entry of whose `origins`, names one of the paths (another
        reference to the path, such as `derivedFrom`, does not count), read
        page by page; `stored` passes records the body already read instead.
        An output whose stored properties already hold what it would be
        written with, `revision` aside, is not written. A written output's
        `revision` is the higher of the one given and the one stored: the
        write path refuses a falling revision, and a delivery reads its rows
        as they stand now, so a lower one means a row the output was built
        from went away, never that the delivery is stale. Every stored output
        `outputs` does not name is deleted."""
        fn = "observed.reconcile"
        if self._host is None:
            raise ValueError("%s: reads the origin's outputs through the host; call host.observed" % fn)
        paths = [origins] if isinstance(origins, str) else list(origins)
        if not paths or not all(isinstance(p, str) and p for p in paths):
            raise ValueError("%s: origins is a record path or a list of them, got %r" % (fn, origins))
        if not isinstance(owned, dict) or not owned:
            raise ValueError("%s: owned maps each output kind to the properties the body sets" % fn)
        if stored is None:
            stored = []
            for path in paths:
                after = None
                while True:
                    query = {"filter": {"kinds": list(owned), "referencing": {"ref": path}}, "first": 500}
                    if after:
                        query["after"] = after
                    page = self._host.list(**query)
                    stored.extend(page.get("records") or [])
                    after = page.get("cursor")
                    if not after:
                        break
        held = {}
        for rec in stored:
            props = rec.get("properties") or {}
            named = [_ref_path(props.get("origin"))] + [_ref_path(o) for o in props.get("origins") or []]
            if rec.get("kind") in owned and any(p in paths for p in named):
                held[(rec.get("kind"), rec.get("id"))] = rec
        counts = {"written": 0, "unchanged": 0, "deleted": 0}
        for kind, rid in sorted(outputs):
            if kind not in owned:
                raise ValueError("%s: %s is not a kind owned names" % (fn, kind))
            full = {name: None for name in owned[kind]}
            full.update(outputs[(kind, rid)])
            if adapter is not None:
                full["adapter"] = adapter
            old = held.pop((kind, rid), None)
            have = (old or {}).get("properties") or {}
            if old is not None and all(_same_value(have.get(k), v) for k, v in full.items() if k != "revision"):
                counts["unchanged"] += 1
                continue
            rev = have.get("revision")
            if isinstance(rev, (int, float)) and not isinstance(rev, bool) and \
                    (full.get("revision") is None or rev > full["revision"]):
                full["revision"] = int(rev)
            self._host.effects.put(kind, rid, properties=full)
            counts["written"] += 1
        for kind, rid in sorted(held):
            self._host.effects.delete(kind, rid)
            counts["deleted"] += 1
        return counts

    def period_id(self, at, period):
        """The label a trace id ends with: `at` in UTC at the period's
        resolution, so PT1H gives "2026-10-07T06", PT15M "2026-10-07T06:15"
        and P1D "2026-10-07". An `at` off the period's grid (the later part of
        a period split by an offset change) adds the finer fields it needs
        ("2026-10-07T06:20:31"), so no two parts share a label."""
        fn = "observed.period_id"
        _aware(fn, "at", at)
        p_us = _duration_us(fn, "period", period)
        t = at.astimezone(datetime.timezone.utc)
        us = _epoch_us(at)
        fields = 1  # date, hour, minute, second, fraction
        for step, n in ((86400 * 10**6, 1), (3600 * 10**6, 2), (60 * 10**6, 3), (10**6, 4)):
            if p_us % step == 0:
                fields = n
                break
        else:
            fields = 5
        for step, n in ((10**6, 5), (60 * 10**6, 4), (3600 * 10**6, 3), (86400 * 10**6, 2)):
            if us % step:
                fields = max(fields, n)
                break
        out = t.strftime("%Y-%m-%d")
        if fields >= 2:
            out += t.strftime("T%H")
        if fields >= 3:
            out += t.strftime(":%M")
        if fields >= 4:
            out += t.strftime(":%S")
        if fields >= 5 and t.microsecond:
            out += "." + ("%06d" % t.microsecond).rstrip("0")
        return out


class Host:
    """The body's host API: logging plus the capability-scoped reads, and the
    SDK surface (records/functions/agents/effects/ids/page/units/observed)
    namespaced beside them."""

    def __init__(self, inp, req_id, logs):
        self._config = inp.get("config")
        self._req_id = req_id
        self._logs = logs
        self.effects = Effects()
        self.records = Records(self)
        self.functions = Functions(self)
        self.agents = Agents(self)
        self.ids = Ids()
        self.page = Page(inp.get("resume"))
        self.units = Units(self)
        self.observed = Observed(self)

    @property
    def logs(self):
        return self._logs.out()

    def config(self):
        return self._config

    def version(self, record):
        """The integer version of a record read (0 for an absent read) — the
        value to pass as `if_version` for a guarded write. It keeps the CAS
        idiom off raw dict access, which would hand back a float and defeat the
        precondition."""
        if not record:
            return 0
        v = record.get("version")
        if isinstance(v, bool) or not isinstance(v, (int, float)):
            return 0
        return int(v)

    def log(self, msg):
        self._logs.add(msg)

    def _call(self, method, params):
        send(self._req_id, {"host": method, "params": params}, kind="call")
        line = PROTO_IN.readline()
        if not line:
            raise HostError("host went away during " + method)
        reply = json.loads(line)
        if reply.get("kind") != "reply" or reply.get("reqId") != self._req_id:
            # The parent kills a desynchronized child; failing the body is
            # all that is left to do from this side.
            raise HostError("protocol desync during " + method)
        if not reply.get("ok"):
            raise HostError(reply.get("error") or method + " failed")
        return reply.get("result") or {}

    def get(self, kind, id):
        """One record by its full (kind, id) reference; None when absent —
        absence is a normal answer."""
        return self._call("get", {"kind": kind, "id": id}).get("record")

    def list(self, **query):
        """Records by query (filter, orderBy, first, after) -> page dict.
        `orderBy` is either the compact string ("at:desc,createdAt") or a list
        of {"property": ..., "desc": bool}."""
        return self._call("list", query).get("page") or {}

    def aggregate(self, body):
        """An aggregate read ({filter, aggregate, downsample?}) -> the result
        dict."""
        return self._call("aggregate", body).get("result") or {}

    def search(self, **params):
        """Lexical/semantic search (q, mode, kinds, k) -> SearchResult: the hits
        list, with `.pending` (properties the drain has yet to buy vectors
        for) beside it."""
        raw = self._call("search", params)
        return SearchResult(raw.get("hits") or [], raw.get("pending") or 0)

    def call(self, function, input=None):
        """Invoke another function (permissions.call gated) -> its output.
        Its effects apply in THIS delivery's transaction. An agent granted
        under permissions.agents runs the same way (host.agents.call)."""
        return self._call("call", {"function": function, "input": input}).get("output")


def register(req):
    ns = {}
    exec(compile(req["source"], req["id"], "exec"), ns)
    fn = ns.get("main")
    if not callable(fn):
        raise ValueError("source defines no main(input, host)")
    FUNCS[req["id"]] = fn
    return {"ok": True}


def deregister(req):
    FUNCS.pop(req.get("id"), None)
    return {"ok": True}


def invoke(req, req_id):
    fn = FUNCS.get(req["id"])
    if fn is None:
        return {"ok": False, "error": "unregistered function " + str(req["id"])}
    inp = req.get("input") or {}
    logs = Logs(req_id)
    host = Host(inp, req_id, logs)
    CAPTURE.logs = logs
    try:
        out = fn(inp, host) or {}
    except HostError as e:
        return {"ok": False, "error": str(e)}
    except Exception:
        tb = traceback.format_exc(limit=3)
        return {"ok": False, "error": tb.strip().splitlines()[-1] + "\n" + tb}
    finally:
        CAPTURE.logs = None
        logs.close()
    # The return-path rule: ONE mode per invocation. A body either returns an
    # explicit effect list OR stages on the builder — never both. The two apply
    # orders are unrelated (returned-first, then staged), so mixing can reverse
    # writes, duplicate them, or self-conflict under CAS. Reject the ambiguous
    # case with a clear error that parks once (never a return of the buffer or a
    # staged handle, which are not return values). One of the two lists is
    # always empty here, so their concatenation is the single chosen plan.
    returned = out.get("effects")
    if not isinstance(returned, (list, type(None))):
        return {"ok": False,
                "error": "the returned `effects` is a list of effects, got %s"
                         % type(returned).__name__}
    returned = list(returned or [])
    staged = host.effects._staged
    if returned and staged:
        return {"ok": False,
                "error": "a body returns an explicit `effects` list OR stages on "
                         "host.effects — not both (%d returned, %d staged); the two "
                         "apply orders are unrelated and can self-conflict under CAS"
                         % (len(returned), len(staged))}
    effects = returned + staged
    resp = {"ok": True, "effects": effects, "output": out.get("output")}
    # The paged-checkpoint continuation: a body returns {"more": {"cursor": X}}
    # to mean "commit this page, then re-invoke me with X on input['resume']".
    # Absent (or None) means drained — the ordinary single-shot completion.
    if out.get("more") is not None:
        resp["more"] = out["more"]
    return resp


def main():
    for line in PROTO_IN:
        line = line.strip()
        if not line:
            continue
        req_id = 0
        try:
            req = json.loads(line)
            req_id = req.get("reqId") or 0
            op = req.get("op")
            if op == "register":
                resp = register(req)
            elif op == "deregister":
                resp = deregister(req)
            elif op == "describe":
                resp = {"ok": True, "functions": sorted(FUNCS), "protocol": 6}
            elif op == "invoke":
                resp = invoke(req, req_id)
            else:
                resp = {"ok": False, "error": "unknown op %r" % (op,)}
        except Exception as e:  # a malformed frame answers, never kills the host
            resp = {"ok": False, "error": "%s: %s" % (type(e).__name__, e)}
        send(req_id, resp)


if __name__ == "__main__":
    _bootstrap()
    main()
