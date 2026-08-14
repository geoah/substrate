# The shared runner's Python host: one long-lived child process hosting
# every installed `runtime: python` function body. Speaks the runner's
# JSON-lines protocol, version 4 (one frame per line; functions/runner's
# protocol.go is the contract):
#
#   parent -> host   {"op": "register", "reqId": N, "id": <key>, "source": <inline body>}
#                    {"op": "deregister", "reqId": N, "id": <key>}
#                    {"op": "describe", "reqId": N}
#                    {"op": "invoke", "reqId": N, "id": <key>, "input": {...}}
#   host -> parent   {"kind": "response", "reqId": N, "ok": true, ...}
#                    {"kind": "response", "reqId": N, "ok": false, "error": "..."}
#   host -> parent   {"kind": "call", "reqId": N, "host": "get"|"list"|"search"|"call", "params": {...}}
#                    ... answered by the parent's next line:
#                    {"kind": "reply", "reqId": N, "ok": true, "result": {...}}
#
# The protocol stream is the ORIGINAL stdout, dup'd at startup and private to
# this file: sys.stdout is rebound so a body's print() lands in the current
# invocation's capped logs, never on the wire, and sys.stdin is rebound to
# /dev/null so a body's input() cannot eat protocol frames. Inline source is
# exec'd at registration into its own module namespace, keyed by the
# installation (repository + function + hash), so no state is shared across
# repositories or functions; `main(input, host)` is the entrypoint. A body error
# is an error frame the engine parks — never a host crash. Isolation is the
# process boundary (user code never loads into the substrate); tightening
# placement is a later dial on the same protocol.
import hashlib
import json
import os
import re
import sys
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
    """One invocation's capped log sink."""

    def __init__(self):
        self.lines = []
        self.dropped = 0

    def add(self, msg):
        s = str(msg)
        if len(s) > MAX_LOG_CHARS:
            s = s[:MAX_LOG_CHARS] + "...[truncated]"
        if len(self.lines) >= MAX_LOG_LINES:
            self.dropped += 1
            return
        self.lines.append(s)

    def out(self):
        if self.dropped:
            return self.lines + ["... %d more log lines dropped" % self.dropped]
        return self.lines


class StdoutCapture:
    """sys.stdout for the whole host: body prints land in the CURRENT
    invocation's logs (prefixed so they read as what they are), never on the
    protocol stream. Outside an invocation, writes are dropped."""

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
    PROTO_OUT.write(data + "\n")
    PROTO_OUT.flush()


class HostError(Exception):
    pass


# --- the SDK: deterministic ids, paging, typed reads, buffered effects -------
#
# The runner passes ONE `host` object to every body (`main(input, host)`). It
# carries the capability-scoped reads (get/list/search/call, unchanged) and,
# namespaced beside them, the SDK surface both runtimes share:
#
#   host.records.get/list/search   typed, type-scoped reads (committed state)
#   host.functions.call             function-to-function composition
#   host.effects.put/patch/delete/  the buffered-effects builder — each APPENDS
#     link/unlink/merge/split       a staged effect and returns a handle
#   host.ids.external/url           deterministic, hash-backed, URL-safe ids
#   host.page.resume / .more()      the paged-checkpoint wrapper
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
# A full type identity: `name.group`, dotted lowercase.
_RE_KIND = re.compile(r"^(?:[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+/)?[a-z][a-z0-9]*$")
# A relation / declared name: camelCase — schema.ValidCamel.
_RE_IDENT = re.compile(r"^[a-z][a-zA-Z0-9]*$")

# The kind a proposal lands as: effects.propose stages an ordinary put of one.
_KIND_RECORDPATCHREQUEST = "core.substrate.reamde.dev/recordpatchrequest"


def _slugify(s):
    """Lowercase, then keep ONLY ASCII [a-z0-9] and collapse every other run to
    one dash. ASCII-only (no Unicode letters/digits, no case folding) so the
    slug is byte-identical to the Go SDK's — a non-ASCII numeric like ½ that
    Python's str.isalnum() once kept diverged across runtimes AND was rejected
    by the engine's ASCII-only id alphabet."""
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
    collision foot-gun (the harvester's old `page-<slug>`). Every component is a
    required non-empty string, and the human slug is ASCII-folded, so the two
    runtimes' outputs are byte-identical."""

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
    so bodies stop hand-building `{"cursor": ...}` dicts. `resume` is a METHOD
    to match the Go SDK's `host.Page.Resume()`."""

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
        raise ValueError("effects.%s: %r is not a kind reference (<authority>/<name>)" % (action, value))
    return value


def _need_id(action, field, value):
    if not isinstance(value, str) or value == "":
        raise ValueError("effects.%s: %s is required" % (action, field))
    if len(value) > _MAX_ID_LEN or not _RE_ID.match(value):
        raise ValueError(
            "effects.%s: %s %r is not a record id (URL-path-safe, at most %d characters)"
            % (action, field, value, _MAX_ID_LEN))
    return value


def _need_rel(action, value):
    if not isinstance(value, str) or value == "":
        raise ValueError("effects.%s: rel is required" % action)
    if not _RE_IDENT.match(value):
        raise ValueError("effects.%s: %r is not a relation name" % (action, value))
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


def _edge_target(action, label, to):
    """Validate one edge/link target: a bare non-empty id string, OR a full
    {kind, id} record reference (both required — the partial ref the engine
    would later reject on a polymorphic edge is caught here)."""
    if isinstance(to, str):
        if to == "":
            raise ValueError("effects.%s: %s id is empty" % (action, label))
        return
    if isinstance(to, dict):
        for k in ("kind", "id"):
            v = to.get(k)
            if not isinstance(v, str) or v == "":
                raise ValueError(
                    "effects.%s: %s reference needs a kind and an id" % (action, label))
        return
    raise ValueError(
        "effects.%s: %s is an id or a {kind, id} reference" % (action, label))


def _need_to(action, to):
    _edge_target(action, "to", to)
    return _json_copy(action, "to", to)


def _need_edges(action, edges):
    if edges is None:
        return None
    if not isinstance(edges, dict):
        raise ValueError("effects.%s: edges is a map of rel -> target" % action)
    for rel, targets in edges.items():
        _need_rel(action, rel)
        items = targets if isinstance(targets, list) else [targets]
        for t in items:
            _edge_target(action, "edges." + rel, t)
    return _json_copy(action, "edges", edges)


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


class Effects:
    """The buffered-effects builder. Each method APPENDS a staged effect to a
    write-only buffer and returns a StagedEffect handle (never a record, never
    a return value); the runner returns the buffer as the delivery's effects.
    Because staging captures a JSON snapshot, one mode is used per invocation:
    a body that ALSO returns an explicit effect list while the buffer is
    non-empty is refused at return (the two apply orders are unrelated and can
    self-conflict under CAS). No flush() — the buffer IS the return. Shapes are
    validated here (a known action, a well-formed id/type/rel/edge, a boolean
    ifAbsent, a non-negative integer ifVersion) so a mistake is a clear
    body error that parks once, not an engine park; the engine stays
    authoritative for the emit ceiling and type admission."""

    def __init__(self):
        self._staged = []

    def _add(self, ef):
        self._staged.append(ef)
        return StagedEffect(ef)

    def put(self, kind, id, properties=None, edges=None, if_absent=False, if_version=_UNSET):
        ef = {"action": "put", "kind": _need_kind("put", kind),
              "id": _need_id("put", "id", id)}
        props = _opt_map("put", "properties", properties)
        if props is not None:
            ef["properties"] = props
        eg = _need_edges("put", edges)
        if eg is not None:
            ef["edges"] = eg
        if _need_bool("put", "if_absent", if_absent):
            ef["ifAbsent"] = True
        v = _if_version("put", if_version)
        if v is not None:
            if if_absent:
                raise ValueError(
                    "effects.put: if_absent and if_version cannot combine — if_absent makes an "
                    "existing row a no-op before the version check; pick one")
            ef["ifVersion"] = v
        return self._add(ef)

    def patch(self, kind, id, properties=None, if_version=_UNSET):
        ef = {"action": "patch", "kind": _need_kind("patch", kind),
              "id": _need_id("patch", "id", id)}
        props = _opt_map("patch", "properties", properties)
        if props is not None:
            ef["properties"] = props
        v = _if_version("patch", if_version)
        if v is not None:
            ef["ifVersion"] = v
        return self._add(ef)

    def delete(self, kind, id):
        return self._add({"action": "delete", "kind": _need_kind("delete", kind),
                          "id": _need_id("delete", "id", id)})

    def link(self, kind, id, rel, to, properties=None):
        ef = {"action": "link", "kind": _need_kind("link", kind),
              "id": _need_id("link", "id", id), "rel": _need_rel("link", rel),
              "to": _need_to("link", to)}
        props = _opt_map("link", "properties", properties)
        if props is not None:
            ef["properties"] = props
        return self._add(ef)

    def unlink(self, kind, id, rel, to):
        return self._add({"action": "unlink", "kind": _need_kind("unlink", kind),
                          "id": _need_id("unlink", "id", id),
                          "rel": _need_rel("unlink", rel), "to": _need_to("unlink", to)})

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
            ef["edges"] = {"target": {"kind": target_kind, "id": target_id}}
        return self._add(ef)


def _as_kinds(kinds):
    if isinstance(kinds, str):
        return [kinds]
    return list(kinds or [])


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

    def list(self, kinds, where=None, first=None, after=None, order=None, with_edges=False):
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
        if with_edges:
            q["withEdges"] = True
        return self._host.list(**q)

    def search(self, q, kinds, k=None, mode=None):
        params = {"q": q, "kinds": _as_kinds(kinds)}
        if k is not None:
            params["k"] = k
        if mode:
            params["mode"] = mode
        return self._host.search(**params)


class Functions:
    """Function-to-function composition, gated by capabilities.call. The
    callee's effects accumulate into THIS delivery's transaction."""

    def __init__(self, host):
        self._host = host

    def call(self, function, input=None):
        return self._host.call(function, input)


class Host:
    """The body's host API: logging plus the capability-scoped reads, and the
    SDK surface (records/functions/effects/ids/page) namespaced beside them."""

    def __init__(self, inp, req_id, logs):
        self._config = inp.get("config")
        self._req_id = req_id
        self._logs = logs
        self.effects = Effects()
        self.records = Records(self)
        self.functions = Functions(self)
        self.ids = Ids()
        self.page = Page(inp.get("resume"))

    @property
    def logs(self):
        return self._logs.out()

    def config(self):
        return self._config

    def version(self, record):
        """The integer version of a record read (0 for an absent read) — the
        value to pass as `if_version` for a guarded write. The mirror of Go's
        `substratefn.Version(e.Version)`; keeps the CAS idiom off raw dict access,
        which would hand back a float and defeat the precondition."""
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
        """Records by query (filter, orderBy, first, after) -> page dict."""
        return self._call("list", query).get("page") or {}

    def search(self, **params):
        """Lexical/semantic search (q, mode, kinds, k) -> hits list."""
        return self._call("search", params).get("hits") or []

    def call(self, function, input=None):
        """Invoke another function (capabilities.call gated) -> its output.
        Its effects apply in THIS delivery's transaction."""
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
    logs = Logs()
    host = Host(inp, req_id, logs)
    CAPTURE.logs = logs
    try:
        out = fn(inp, host) or {}
    except HostError as e:
        return {"ok": False, "error": str(e), "logs": logs.out()}
    except Exception:
        tb = traceback.format_exc(limit=3)
        return {"ok": False, "error": tb.strip().splitlines()[-1] + "\n" + tb,
                "logs": logs.out()}
    finally:
        CAPTURE.logs = None
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
                         % type(returned).__name__,
                "logs": logs.out()}
    returned = list(returned or [])
    staged = host.effects._staged
    if returned and staged:
        return {"ok": False,
                "error": "a body returns an explicit `effects` list OR stages on "
                         "host.effects — not both (%d returned, %d staged); the two "
                         "apply orders are unrelated and can self-conflict under CAS"
                         % (len(returned), len(staged)),
                "logs": logs.out()}
    effects = returned + staged
    resp = {"ok": True, "effects": effects,
            "output": out.get("output"), "logs": logs.out()}
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
                resp = {"ok": True, "functions": sorted(FUNCS), "protocol": 4}
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
