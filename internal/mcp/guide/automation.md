# Functions, triggers and policies

Declaring a Python function, running it with `call`, binding it to records, a schedule or a webhook with a trigger, driving syncs, and policies over writes.

## Functions

- A function is a declaration record of `substrate.reamde.dev/core/function`, id `<authority>/<package>/<name>`, declared with `apply` under `~` (the row IS the declaration: no `data.properties`). `runtime` is `python` (code in `source`) or `host` (built into the substrate, core's own; you cannot declare one).
- Required: `authority`, `description` (the tool card an agent sees), `runtime`; `source` on python. Optional: `package`, `arguments [{name, type, required?, repeated?, description?, values?}]` (`type`: `string`, `int`, `float`, `bool`, `enum`, `json`), `returns` (same shape), `timeout` (ISO 8601, default `PT5S`, capped at 60 s), `execution` (`foreground` | `background`), `effect` (`read`, `write`, `external`, `irreversible`), `confirmation: always` (every write it returns is held as a change request), `offer {agents: true, name?}` (offers it to agents whose `toolsFrom` matches), `stream` (the sync stream it keeps).
- `permissions`: `reads {kinds: [kind or glob], budgets {calls, rows}}`, `writes [kind or glob]`, `call [function refs]`, `agents [agent refs]`, `network [host, host:port or CIDR]` (no entry: no sockets; any entry: the public internet, never the deployment's own), `mutations [merge, split]`. A grant left out is refused at run time.
- The body is `main(input, host)` returning `{"output": …}`. `input["mode"]` is `record`, `schedule`, `webhook`, `manual` or `call`. A delivery carries `input["envelope"]`: `change {seq, op, kind, id, actor, changed}`, `record` (current state, `null` after a delete), `repository {authority}`; a schedule or webhook fire carries `fire {id, at}` instead, a webhook adds `request`. A direct `call` and a schedule's `arguments` arrive as `input["args"]`; `input["config"]` holds the bundle's `settings` and `inputs`.
- Writes are staged, not made: `host.effects.put(kind, id, properties=…, if_absent=…, if_version=…)`, `.patch`, `.delete`, `.propose(...)`, applied in one transaction under the function's actor. Reads: `host.records.get(kind, id)`, `.list(kinds, where=…)`, `.search(q, kinds)`; also `host.functions.call`, `host.agents.call`, `host.ids.minted(*parts)`, `host.units.convert`, `host.log`. Libraries ride a PEP 723 `# /// script` block at the top of `source`.
- Change a function later by sending its whole document again with `apply`, starting from the record `query` reads.

```json
{"kind": "substrate.reamde.dev/core/function",
 "metadata": {"id": "~/garden/markwatered"},
 "data": {
  "authority": "~", "package": "garden",
  "description": "Stamp a plant as watered. Call with {plant, at}.",
  "runtime": "python",
  "arguments": [{"name": "plant", "type": "string", "required": true, "description": "the plant's id"},
                {"name": "at", "type": "string", "required": true, "description": "when, RFC 3339"}],
  "permissions": {"reads": {"kinds": ["~/garden/plant"]}, "writes": ["~/garden/plant"]},
  "source": "def main(input, host):\n    args = input[\"args\"]\n    plant = host.records.get(\"~/garden/plant\", args[\"plant\"])\n    if not plant:\n        return {\"output\": {\"found\": False}}\n    host.effects.patch(\"~/garden/plant\", args[\"plant\"],\n        properties={\"lastWateredAt\": args[\"at\"]}, if_version=host.version(plant))\n    return {\"output\": {\"plant\": args[\"plant\"]}}\n"}}
```

## Running one: `call`

- `call {function, input, idempotencyKey?}` runs it now as the owner's client would; `input` arrives as `input["args"]`, checked against `arguments`. The answer is `{output, effects}` (the count of writes applied), landed under the function's actor; no trigger cursor moves.
- The call waits at most four minutes; the declaration's `timeout` bounds the body itself. Give a call you may repeat an `idempotencyKey`.
- Agents are not run by `call`: `chat` with one, or bind a trigger.

## Triggers

A `substrate.reamde.dev/core/trigger` is an ordinary data record (`write` it, or include it in an `apply` batch): one `source` bound to one `callable`, owning its cursor.

- `enabled` (false stops delivery and keeps the cursor), `label`.
- `source.record {kinds: [kind or glob], ops: [create, update, delete], when: "<CEL>", coalesce, fromStart}`: `when` is a boolean over `change`, `record` (`null` after a delete) and `repository`; `fromStart: true` delivers the matching history too.
- `source.schedule {recurrence: "FREQ=DAILY;BYHOUR=7", timezone: "Europe/London", startsAt: "2026-01-01T07:00:00Z"}` (an RRULE from `startsAt`; missed runs: the newest, or each with `missed: "all"`); the trigger's `arguments` reach the function as `input["args"]` on every fire.
- `source.webhook {key?, headers?, signature?, maxBody?}`: the public door `POST /webhooks/<authority>/<trigger id>`; `key` is a credential you may write but never read back (cut from every read); `signature {algorithm: "hmac-sha256", signatureHeader, secret: "substrate.reamde.dev/core/secret/<id>", …}` verifies the sender.
- `callable`: `"substrate.reamde.dev/core/function/<function id>"` or `"substrate.reamde.dev/core/agent/<agent id>"`; an agent delivery opens a thread whose first message is the envelope.

```json
{"op": "put", "kind": "substrate.reamde.dev/core/trigger", "id": "garden-water-on-due",
 "properties": {
  "enabled": true,
  "source": {"record": {"kinds": ["~/garden/plant"], "ops": ["create", "update"],
             "when": "record != null && record.properties.state == \"due\""}},
  "callable": "substrate.reamde.dev/core/agent/~/garden/waterer"}}
```

### Driving them: the `triggers` tool

- `status`: every trigger's `kind` (`record` | `schedule` | `webhook`), `callable`, `enabled`, `cursor`, `head`, `lag` (changes not yet delivered), `webhookPath`, `parked`, `pending`, `inFlight`, `lastPassAt`, `lastDeliveredAt`, `held` (an agent trigger waiting at its spend cap).
- `wake {id}`: run now; a record trigger drains to head, a schedule checks its due occurrence, a webhook fires once without a request. This is how a provider sync is run by hand.
- `run {id, kind, record}`: one synthetic delivery of that record's current state (guard applied, cursor untouched). `replay {id, from}`: move a record trigger's cursor back to that changelog seq.
- `parked {id}`: the deliveries it gave up on, `[{id, seq | fireId, recordId, attempts, lastError, parkedAt, running?, pending?}]`; a `pending: true` row is a webhook request not yet settled, not a failure. `retry {id, failure}` runs one again; `forget {id, failure}` drops it. Fix the cause before a retry; never loop on one.

## Provider syncs

- A connected account implements the core `sync` trait (`syncState`, `syncMessage`, `lastSyncedAt`, `syncRequestedAt`, `syncPaused`, `syncProgress`, `syncError`, `syncStreams`). A provider's bundle ships the sync function and the schedule trigger that fires it.
- `triggers {action: "sync"}` joins every account with its triggers: `{kind, id, title, state, health, paused, tokenStatus, lastSyncedAt, progress, error, streams, parked, triggers: [...]}`. A parked delivery of a stream function marks that stream erroring.
- Connecting an account is the owner's, on the console's Providers page (it needs their browser); adding the provider package is `packages {action: "add"}` (guide topic `packages`).

## Change-request policies

- A `substrate.reamde.dev/core/recordpatchpolicy` rules the writes of agents and functions, never yours: `selector {kinds, ops: [put, patch, delete], agents, functions}` (empty lists match all; `functions` empty means only writes an agent makes), `action` `allow` | `gate` | `refuse`, `disabled`. The most restrictive matching policy wins; no match lands the write.
- `gate` turns the write into a `substrate.reamde.dev/core/recordpatchrequest` the owner decides (`decide`). An optional `judge` (an agent reference) decides within `autoAccept` / `autoRefuse` confidences, reading `criteria` (prose) and `context` (`envelope` | `thread`); `mode: advise` only recommends. An `allow` with `overrides` lifts one gate for one agent, kind and op.
- Write one with `write`: `{op: "put", kind: "substrate.reamde.dev/core/recordpatchpolicy", id: "gate-deletes", properties: {selector: {ops: ["delete"]}, action: "gate"}}`. Pending requests: `query {kinds: ["substrate.reamde.dev/core/recordpatchrequest"], filter: {properties: {decision: {eq: "proposed"}}}}`.

## Bundles and inputs

A package shipping functions, agents and triggers together is a bundle (`substrate.reamde.dev/core/bundle`). Its declared `inputs` (an account, a provider row) resolve to one record each, bound with `packages {action: "bind", id, input, record}`, and reach the body under `input["config"]`. Guide topic `packages` has the rest.
