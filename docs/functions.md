# Functions and the host SDK

A function is a **pure callable**: a named piece of real code, Python or Go,
carried inline on its manifest. It has no subscription and no schedule of its
own. What fires it is a separate [trigger](#triggers). When it runs it reads
its input, computes, and returns a list of **effects** the engine applies
through the ordinary write path under the function's own actor, plus an output
value for whoever called it. Writes are never blocked: a function is a
subscriber, not a gatekeeper. One reference, four ways in: a trigger delivery,
another function's host call, the HTTP call API, or a manual per-trigger run.

Functions ship inside an [bundle](bundles.md), beside the kinds they
read and write. Here is one shaped like the URL harvester's, which turns a
freshly minted `page` record into fetched markdown:

```yaml
kind: core.substrate.reamde.dev/function
metadata: {id: web.bundles.substrate.reamde.dev/fetchpage}
data:
  authority: web.bundles.substrate.reamde.dev
  description: Fetch one pending page as markdown and mark it fetched.
  runtime: python
  capabilities:
    emit: [web.bundles.substrate.reamde.dev/page]
  source: |
    def main(input, host):
        env = input.get("envelope") or {}
        page = env.get("record") or {}
        url = (page.get("properties") or {}).get("url", "")
        slug = url.rstrip("/").split("/")[-1] or url
        title = slug.replace("-", " ").strip() or url
        markdown = "# " + title + "\n\nfetched from " + url
        host.effects.patch(
            "web.bundles.substrate.reamde.dev/page", page.get("id"),
            properties={"title": title, "content": markdown,
                        "fetch": "fetched"})
        return {"output": {"page": page.get("id")}}
```

Every manifest document is the same four keys. `kind:` names the core kind the
document declares, `core.substrate.reamde.dev/function` here. `metadata.id` is the
reference this declaration is known by, `<authority>/<name>` — the string a
trigger, a host call and the call API all address it with. `data:` is the
declaration itself. `status:` is server-set, and no author writes it.

## The manifest

`data` carries the whole callable:

- **`authority`** is the authority that declares it, matching the authority
  half of `metadata.id`.
- **`description`** is model-facing and required: the function is its own tool
  card wherever it appears as a callable.
- **`runtime`** is `python` or `go`.
- **`source`** is the inline body (bounded, at most 256 KiB).
- **`timeoutMs`** bounds one invocation's wall clock, host calls included
  (default 5000, max 60000).
- Optional **`input:`** and **`output:`** shape schemas check a caller's
  arguments before the body runs and the returned value after. The schema
  dialect is deliberately tiny:
  `{type: object|array|string|number|boolean|any, properties, items, required,
  description}`, nothing else. An object schema with a `properties` key refuses
  undeclared keys (key presence closes the object); an object schema with no
  `properties` key is the open object. Trigger deliveries ignore both, because
  the envelope is the input.
- **`capabilities`** is the capability envelope, the whole security boundary:

```yaml
capabilities:
  emit:                            # required, non-empty: the write allowlist
    - tasks.substrate.reamde.dev/task
  reads:                           # the host-read allowlist and budget
    kinds:
      - people.substrate.reamde.dev/person
    budgets:
      calls: 16
      rows: 500
  call:                            # host-call allowlist (registered functions)
    - web.bundles.substrate.reamde.dev/setclass
  network:                         # any entry grants egress; none denies it
    - api.example.com
  mutations:                       # gates the merge / split effects
    - merge
```

`emit` is required and lists the kinds the effects may address: this function
may write those kinds and nothing else. `reads.kinds` is the host-read
allowlist and `reads.budgets` its budget (defaults 16 calls / 500 rows, raised
to at most 1000 calls / 10000 rows); a `reads:` block that declares no `kinds`
is a load error. `call` is the host-call allowlist; every target must be a
registered function. `network` is enforced as a BINARY gate: a function
declaring none is denied IPv4 and IPv6 sockets by the sandbox, while the host
patterns themselves are still only documentation (see [the sandbox](#the-sandbox)). `mutations` gates the `merge` and `split` effects, which are
refused without it. Every entry in `emit`, `reads.kinds` and `call` is a full
reference, `<authority>/<name>`, and none of them admit globs.

The body's entrypoint is `main(input, host)` in Python
(`Main(in, host)` in Go), and it returns `{effects, output}`. `input` names
the `mode` that woke the body (`trigger`, `schedule`, `webhook`, `manual` or
`call`) beside an `idempotencyKey`, a `causalDepth` and a `callDepth`, then
carries that mode's payload: a delivery puts the envelope under
`input["envelope"]`, while a direct call puts the caller's own JSON under
**`input["args"]`**.

## The delivery envelope

A record-triggered delivery arrives as `input["envelope"]`, three keys:

```yaml
change:
  seq: 412
  op: update
  kind: github.bundles.substrate.reamde.dev/account
  id: gh-acct-1
  actor: connector:github
  changed:
    - tokenStatus
record:
  id: gh-acct-1
  kind: github.bundles.substrate.reamde.dev/account
  properties:
    tokenStatus: connected
  edges: {}
repository:
  owner: geoah
```

`change` says what moved (`op` is `create`, `update` or `delete`, and
`changed` names the properties when the payload carries them). `record` is the
row's state **now**, not the old value, and is `null` after a delete. Its
`edges` map each `rel` to a list of targets, each target carrying the same `id`
and `kind` the record itself does, plus that target's `title`. A schedule or
webhook delivery has no changelog entry underneath it, so its envelope carries
`fire` (the fire's `id` and `at`) and `repository` in place of `change` and
`record`.

**One `kind`, everywhere.** A kind is named by a reference: `<authority>/<name>`
for a kind some authority publishes (`tasks.substrate.reamde.dev/task`,
`web.bundles.substrate.reamde.dev/page`), or a bare `<name>` for a kind local to this
repository. The envelope, the SDK's reads, the SDK's writes and an explicit
`effects` list all spell a kind exactly that one way, so an envelope's
`change` copies straight into a record reference:

```python
source = {"kind": change["kind"], "id": change["id"]}
```

That reference is what an edge target takes, and what a body compares against
to know whether a delivery is about the kind it cares about:

```python
PAGE = "web.bundles.substrate.reamde.dev/page"

def main(input, host):
    record = (input.get("envelope") or {}).get("record") or {}
    if record.get("kind") != PAGE:
        return {"output": {"skipped": True}}
    page = host.records.get(PAGE, record.get("id"))
    return {"output": {"fetch": (page or {}).get("properties", {}).get("fetch")}}
```

The manifest's allowlists are the one place a bare name is refused: `emit`,
`reads.kinds` and `call` all take full references, so a capability always
names an authority. A body may ask for either spelling — every gate runs on
what the reference resolves to, so `host.records.list(["task"])` against a
capability declaring `tasks.substrate.reamde.dev/task` is the same read, and a name the
registry cannot place is refused exactly as an undeclared kind is.

## How the body runs

A function's source is prepared **at install**, synchronously, and a body that
cannot run fails the install rather than the first delivery. Python source
registers into a shared runner host; Go source compiles to a cached binary the
runner supervises. Both speak the same JSON-lines protocol to that runner, a
child process of the substrate, never in-process user code. Preparation is
bounded so one apply cannot hold the vocabulary write path: at most 64 bodies warm
per batch, sequentially, under a five-minute aggregate deadline.

A dependency-free Python function and every Go function take the fast path. A
Python body that needs libraries declares them with a
[PEP 723](https://peps.python.org/pep-0723/) inline metadata block, and the
runner provisions a cached virtual environment with `uv` at registration:

```python
# /// script
# dependencies = ["google-api-python-client"]
# requires-python = ">=3.11"
# ///
```

Dependencies are resolved **at registration**, not at invocation: `uv sync
--script` provisions the environment while the network is still available, and
the interpreter uv built is then run directly. `uv` is never in the invocation
path.

## The sandbox

Every function body is arbitrary third-party code, and it runs **one process
per installation** — keyed by repository plus function plus content hash — with
that process confined by the kernel. There is no shared interpreter: two
functions never meet in one address space, whether they belong to one bundle,
one repository or two.

Three layers, all applied by the substrate to its own children, all
unprivileged, none requiring a container runtime:

- **Landlock** confines the filesystem. A body may read and execute its
  interpreter and the system libraries; it may read and write its **own** work
  directory and its own `TMPDIR`; and it may touch nothing else. Notably it
  gets no `/proc` at all, so it cannot read the substrate's own environment —
  which is what makes the environment allowlist below a boundary rather than a
  gesture — and the shared build and `uv` caches are read-and-execute, so one
  installation cannot plant an artifact another will run.
- **seccomp** removes the syscall classes a body has no use for — `ptrace` and
  the other reach-into-another-process calls, the mount APIs, `io_uring`,
  `bpf`, the kernel keyring, module loading — and enforces
  `capabilities.network`: **a function that declares no `network:` is denied
  `AF_INET` and `AF_INET6` sockets outright.** The enforcement is binary. A
  syscall filter cannot read the address behind a `connect(2)` pointer, so
  holding a body to the *specific hosts* it declared needs an egress proxy and
  is not done yet; what is enforced is the difference between "some egress" and
  "none".
- **rlimits** cap descriptors and file size, and disable core dumps.

The process environment is separately default-deny: every child starts from a
named allowlist of variables, never the substrate's own environment, so the
credential key, the database URL and the gateway keys are excluded by
construction rather than by filtering.

`SUBSTRATE_SANDBOX` chooses how hard the substrate insists — `off`,
`best-effort` (the default: apply every layer the kernel offers, and log
loudly about any it does not) or `enforce` (refuse to run a body at all unless
the filesystem and syscall layers both applied). The effective state is logged
once at boot; a degraded sandbox logs at ERROR, because a confinement that
quietly does less than it claims is worse than none.

**What it does not do.** It is not a container. A body still shares a uid and a
pid namespace with the substrate, so it can signal it. There is no memory or
process-count ceiling, because that needs a cgroup and `/sys/fs/cgroup` is
read-only in a stock container. A body that *is* granted network reaches
loopback, and therefore the substrate's own HTTP port — where it still needs a
token it does not have. On non-Linux hosts (a macOS laptop running
`mise run dev`) none of it applies, and the boot log says so.

### Shared modules

A bundle may ship library modules its functions import (`modules:` on the
bundle manifest, filename to inline source, at most 256 KiB each), so a
provider's functions dedupe a shared HTTP client or normalizers instead of
every body re-implementing them. `.py` files land on a per-installation module
path (appended after the interpreter boots, so no `sitecustomize.py` can
auto-run and no `json.py` can shadow the stdlib); `.go` files vendor into the
Go build as `substratefn.local/lib`. Modules are inline sources on the bundle
document, not closure members, so they never appear in `installs:`, and
changing one re-registers or rebuilds the function exactly like changing the
body.

## Effects

Effects are the ordinary [seven mutations](api.md#the-seven-mutations),
applied through the write path in the same transaction as the delivery's
cursor advance, every one held to `capabilities.emit` by the kind it names.
Every effect names its target the same way, a `kind` carrying a kind
reference:

- **`put`** `{action: put, kind, id, ifAbsent?, ifVersion?, properties?,
  edges?}`. `ifAbsent: true` is create-only: any existing row, live or
  tombstoned, is a no-op, so a minting function never resets state a later
  stage owns. `ifAbsent` must be a boolean, and it cannot combine with
  `ifVersion` on one put (the version check would be silently dropped).
- **`ifVersion`** (put and patch) is the optimistic-concurrency precondition: an
  integer the write applies against only if the stored version equals it (a
  non-existent record is version 0), else the whole delivery fails
  `conflict`. It is the safe read-then-conditional-write primitive.
- **`patch`** `{action: patch, kind, id, properties}`. A state value among the
  properties is a transition; re-asserting the current state is a no-op.
- **`delete`** `{action: delete, kind, id}` tombstones.
- **`link` / `unlink`** `{action, kind, id, rel, to}`, where `id` is the source
  record and `to` a bare id or a `{kind, id}` reference; `link` also takes
  the edge's own `properties`. Emit gates by the source kind.
- **`merge`** `{action: merge, kind, id, loser}` (`id` is the winner) and
  **`split`** `{action: split, kind, merge}`, both refused unless
  `capabilities.mutations` grants them; the `*request` records stay the polite
  default for agent chains.

A put or patch addressed to a former id resolves onto the canonical winner
instead of parking. Ids are required on every action but split (whose address
is the merge record), and because a function composes the ids of what it
writes, put/patch/delete/link/unlink replays are idempotent by construction.
Merge and split are replay-safe by verification instead: re-merging a loser
already former to the winner, or re-splitting an already-tombstoned merge, is
a verified no-op, and any other state is a conflict that parks.

## The SDK

The runner passes one `host` object to every body. It carries the same
namespaced surface in both runtimes, concept for concept; only the spelling
follows each language, so a multi-field call takes Python keyword arguments
where Go takes an option struct (`host.effects.put(kind, id, …)` against
`host.Effects.Put(substratefn.PutEffect{…})`) while the fixed-arity ones stay
positional in Go (`Get`, `Delete`, `Merge`, `Split`). Two names differ between
the two: Python's `order` is Go's `OrderBy`, and Python's `with_edges` is Go's
`WithEdges`. Go also adds typed read results Python has no need of.

**Reads.** `host.records.get(kind, id)`,
`host.records.list(kinds, where?, first?, after?, order?, with_edges?)`,
`host.records.search(q, kinds, k?, mode?)` (`mode` is `lexical`, `semantic`
or `hybrid`, and defaults to `hybrid`), and
`host.functions.call(function, input?)`. `get` addresses one record by its
full reference, the (kind, id) pair; a bare id names nothing and the frame is
refused. Reads see committed state, never this delivery's own staged effects,
so a local overlay can never lie. A forbidden kind answers exactly like an
absent id (same nil shape, same budget charge), so a disallowed `get` is never
an existence or kind oracle. Reads are held to `capabilities.reads`: with no
`reads:` block the allowlist is empty, so every `list` and `search` is refused
and every `get` answers absent. Calls are charged before they run, `first` and
`k` clamp to the remaining row budget, and returned rows charge on top. In Go
the typed read returns a `*ReadRecord` whose `Version` is an `int64`, so the
CAS idiom `IfVersion: substratefn.Version(e.Version)` is writable straight off a
read; in Python `host.version(record)` returns that integer.

**Writes, the buffered-effects builder.** `host.effects.put(kind, id,
properties?, edges?, if_absent?, if_version?)`, `.patch(kind, id, properties?,
if_version?)`, `.delete(kind, id)`, `.link(kind, id, rel, to, properties?)`,
`.unlink(kind, id, rel, to)`, `.merge(kind, id, loser)` and
`.split(kind, merge)` each append a staged effect to a write-only buffer and
return a staged-effect handle, never a record and never a value to inspect.
There is no `flush()`: the buffer is the return. The builder validates shape
locally against the engine's own alphabets (a URL-safe id, a kind reference, a
camelCase relation, a well-formed edge target, a boolean `if_absent`, a
non-negative integer `if_version`, no self-merge) and snapshot-copies caller
maps through JSON, so a mistake is a clear body error rather than an engine
park. The action needs no checking: it is the method you called. The engine
stays authoritative for the emit ceiling and kind admission.

**One mode per invocation.** A body **either** returns an explicit `effects`
list **or** stages on the builder, never both. The two apply orders are
unrelated (returned first, then staged) and could reverse writes or
self-conflict under the version check, so a result carrying explicit effects
while the buffer is non-empty is refused outright, naming both counts. The
example above stages on the builder and returns no `effects` key.

**Deterministic ids.** `host.ids.external(provider, account, external_id)` and
`host.ids.url(url)` produce stable, URL-safe, hash-backed ids, byte-identical
across the two runtimes. A deterministic id is scoped to one kind: the same
derived id used for two different kinds names two independent records, so the
writer names the kind on every put and get that uses it. `ids.url` hashes the
exact URL with only surrounding-whitespace trimming and no canonicalization, so
distinct spellings are distinct ids by design; a structural canonicalizer,
when needed, is a separate named helper.

**Paging.** `host.page.resume()` returns the opaque cursor the previous page
returned (absent on a fresh delivery), and `host.page.more(cursor)` builds the
continuation a paged body returns as its `more`. This is the first-class
wrapper over the paged-checkpoint protocol, so a body syncing a provider one
page per invocation stops hand-building cursor dicts. The host drains those
pages off the causal chain — every re-invocation carries the delivery's
original causal depth — and each page's effects commit with its cursor, so a
backfill of any length runs at constant depth and a crash mid-drain resumes
from the last committed page.

**Configuration and connected accounts.** `host.config()` (`host.Config()` in
Go) returns the callable's resolved configuration: the owning `bundle`
authority, the bundle's single `bundleconfig` record under `config`, and
every [connection](bundles.md#connections) the bundle declares under
`accounts`, each flattened to its id, kind and stored properties. For an OAuth
bundle the host resolves each account's credential itself and hands the body
a live `token` on the account entry, or a `tokenError` string when the grant is
dead, so one broken account never parks the whole delivery. The OAuth
facility's own secrets, the config's `clientSecret` and an account's
`tokenRef`, are never injected: a body gets the resolved token and nothing it
could exfiltrate a credential with. Every injected secret value is scrubbed out
of whatever crosses back over the runner boundary.

**Logging.** `host.log(msg)` in Python, `host.Logf(format, args...)` in Go,
records a line on the invocation's run record. Lines are truncated at 4096
characters and capped at 200 per invocation, with the remainder counted rather
than kept, so a chatty body cannot flood the record.

**Frames and ceilings.** One message between a body and the runner is a single
JSON line capped at **8 MiB**, in both runtimes. A response that would exceed
it is replaced by a clear error rather than a truncated frame, which is the
real reason a body that walks a provider pages instead of returning everything
at once.

## Host call

`host.functions.call(function, input)` runs another function to completion
inside the caller's invocation. The runner refuses a target outside the
caller's `capabilities.call` and charges the call budget before executing; the
engine refuses a target already on the call stack (direct and mutual recursion
both) and one that would exceed the causal-depth cap. The callee gets its own
fresh read budgets and its own timeout (bounded by the caller's remaining
deadline), and its output returns to the calling body. Its effects do not apply
on the spot: they accumulate, decoded against the callee's own capability
envelope, and land in the caller's delivery transaction, sub-call effects first
in call order, all under the delivery's actor. One delivery is one transaction,
so a caller that fails after a sub-call rolls the sub-call's writes back with
it.

## Triggers

A function does not watch anything. A **trigger** is a data record of
`core.substrate.reamde.dev/trigger`, console-editable and `substratectl apply`-able like
any other, that binds one source to one callable and owns the delivery. Here
is the trigger that drives the function above:

```yaml
kind: core.substrate.reamde.dev/trigger
metadata: {id: web-fetch-on-page}
data:
  properties:
    enabled: true
    source:
      record:
        kinds: [web.bundles.substrate.reamde.dev/page]
        ops: [create]              # create | update | delete
        when: 'record != null && record.properties.fetch == "pending"'
    callable: {kind: core.substrate.reamde.dev/function, id: web.bundles.substrate.reamde.dev/fetchpage}
```

`source` takes exactly one arm:

- An **`record`** arm subscribes to the [changelog](changelog.md): the trigger owns
  a cursor and, for every committed change to a matched kind and op whose
  `when:` guard passes, delivers the callable the record's **current** state.
  Each entry in `kinds:` is a kind reference, `<authority>/*` for everything
  one authority publishes, or `*` for everything; those three spellings are
  the whole glob grammar. `ops:` omitted means all three. Optional
  `coalesce: true` keeps only the latest matched change per record in a batch.
- A **`schedule`** arm fires the callable on an RRULE `recurrence` (with a
  `timezone` and optional `startsAt`), with no changelog entry underneath and no
  guard.
- A **`webhook`** arm has no scan of its own; an authenticated wake delivers one
  fire.

`callable` is a [reference](data-model.md#property-types) naming the function
or [agent](agents.md) to run: its `kind` is `core.substrate.reamde.dev/function` or
`core.substrate.reamde.dev/agent`, and its `id` is that callable's own reference.
`enabled` defaults to true, and setting it false stops delivery without losing
the cursor's position. Every trigger write is admitted: the guard must
compile, the recurrence and timezone must parse, and the callable must resolve
to a registered callable of its kind.

The `when:` guard is the one place [CEL](https://cel.dev) survives. It is a
boolean over three read-only bindings, `change`, `record` (null after a
delete), and `repository`. There is deliberately no clock and no way to fetch
other records: a guard is a cheap filter, not the computation. The computation
is the function body.

## What makes this safe to run unattended

- **Idempotent by construction.** A function composes its own ids, `put`
  upserts, and identical writes are suppressed. Replaying a trigger over the
  whole changelog is a no-op where it already ran. The dispatcher advances an
  record trigger's cursor in the same transaction as the effects, so
  substrate-side consequences are effectively-once; external consumers get an
  at-least-once floor, made safe by the same id composition.
- **No loops.** Every function-authored write records the change that caused
  it, and a trigger never delivers writes carrying its own callable's actor.
  That actor is `function:<name>` — the actor domain is flat, so two callables
  sharing a local name share one actor. A causal chain deeper than the engine's
  cap (16) parks instead of spinning.
- **No wedging.** A delivery that keeps failing is parked (3 attempts with
  backoff; a deterministic trip like an allowlist or budget violation parks on
  the first) and the trigger's cursor moves on. A false `when` is a skip, not a
  failure.

## Driving triggers

Delivery bookkeeping lives on the trigger, not on the function, and the verbs
that drive it live at the trigger resource, under
`/api/v1/core.substrate.reamde.dev/triggers/…` (a resource's verbs live at the
resource). No path carries a repository segment: the bearer token implies the
repository.

- `GET …/triggers/status` is the one collection-level verb: every trigger's
  kind, callable, cursor, head, lag, last fire and parked count in a single
  answer. There is no per-trigger `status`.
- `POST …/triggers/{id}/replay` takes `{"from": seq}` and resets an
  record-sourced trigger's cursor for a retrospective run.
- `POST …/triggers/{id}/run` takes `{"kind": …, "id": …}`, both required, and
  synthesizes one delivery of that record's current state (guard applied,
  source filter not, cursor untouched).
- `POST …/triggers/{id}/wake` scans now: a webhook fires once, a record
  trigger drains its backlog, a schedule checks its due occurrence.
- `GET …/triggers/{id}/parked` lists the deliveries the trigger gave up on,
  and `POST …/triggers/{id}/parked/{failureId}/retry` re-runs one.

`replay` answers the cursor it set; `run`, `wake` and `retry` answer
`{"ran": n}`, the number of deliveries that applied effects. Every settled
dispatched delivery writes a `core.substrate.reamde.dev/run` row under the
`substrate` actor: the trigger, the callable, the mode, the seq or fire id,
the status (`ok`, `skipped` or `parked`), the attempt count and the
applied-effects summary. Parked runs are kept; the newest twenty non-parked
runs per trigger stay and older ones tombstone. The direct invocations — a
manual run, a parked retry, a host call, the call API — mint nothing.

`substratectl function call <name> --input <json>` invokes one function directly,
applies its effects under the function's actor, and prints the effect count
beside the output. Over HTTP that is
`POST /api/v1/core.substrate.reamde.dev/functions/{name}/call` with `{"input": …}`,
answering `{"output": …, "effects": n}`; what you send arrives at the body as
`input["args"]`. A call moves no cursor, and its effects land under the
function's actor as a fresh causal root. The trigger rows are ordinary
records, so `get` / `apply` / `delete` edit them like anything else.

Next: [agents](agents.md), callables whose body is an LLM loop.
