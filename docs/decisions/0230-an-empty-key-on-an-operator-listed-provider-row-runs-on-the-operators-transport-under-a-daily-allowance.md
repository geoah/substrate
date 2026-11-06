---
status: proposed
date: 2026-11-06
decision-makers: George Antoniadis
---

# 0230. An empty key on an operator-listed provider row runs on the operator's transport, under a daily allowance

## Context and Problem Statement

A person registering on a substrate somebody else runs cannot chat with an
agent or search by meaning until they paste a vendor key into an
`llm/provider` row. The operator, whoever runs the deployment, holds those
vendor accounts and may pay for the people it invites, but the engine
removed the server-wide LLM key so that no bearer the server holds could
reach a repository-chosen endpoint
(`kinds/substrate.reamde.dev/llm/provider.yaml`, `internal/llm/llm.go`
`WirePolicy`). [docs/plans/host-defaults.md](../plans/host-defaults.md)
proposes operator defaults, keys and OAuth clients the operator supplies and
a repository uses only where it holds none of its own; this record is the
part of it that binds the engine.

## Considered Options

- Keep every row on its own key (today).
- Write the operator's key into each repository's seeded rows.
- Fall back to the operator's key for an empty row and keep the row's own
  endpoint, headers and defaults.
- Serve an empty, operator-listed row on the operator's whole transport, from
  a file the operator owns, and charge it to a ledger the operator owns.

## Decision Outcome

Chosen: an empty key on an operator-listed row runs on the operator's whole
transport, and every such call is charged to the repository's allowance,
what the operator pays for one repository in one UTC day.

The operator's defaults file is the one `SUBSTRATE_OPERATOR_DEFAULTS` names.
Its keys are `{file: ...}` or `{env: ...}` references, never inline, read at
boot and held in the process; no record, sealed value, changelog line or
export carries them.

**Which rows the operator serves.** An `llm/provider` row resolves to the
operator's entry when all three hold: the row is live, its stored `apiKey`
is empty, and the file's `llm` section lists its id. Any key the person
wrote, valid or not, makes the row theirs, and clearing it returns the row
to the operator's entry. A keyless row whose id the file does not list
refuses as it does without a file, naming the site.

**Deletion.** A deleted row stays deleted: the seed is create-only, nothing
resolves the id, and an agent on it refuses as on any missing row. Creating
a row under the same id without a key returns it to the operator's entry.

**The whole transport.** For a row the operator serves, the entry supplies
the wire, base URL, key, headers, request defaults, cache, title model, model
list and prices. Nothing on the row except its id and its empty key is read,
so a person reaches an operator-paid request only with a prompt and a model
from the entry's list. A model off the list is refused before anything is
sent, naming the site, the list and the way out (the person's own key). The
entry's `maxOutputTokens` is sent where a request names no ceiling and caps
one that names more.

**Embeddings and the stamp.** Where no live row declares `embedModel` and
the file has its one `embeddings` entry, that entry is the repository's
embeddings provider, on the transport of the `llm` entry it is nested under,
and stamps its vectors with the provider id `(operator)`. The parentheses
are outside the record id alphabet, so no row's vectors can carry the same
pair, and moving between the operator's entry and a row always buys the
vectors again. A row that declares `embedModel` is the provider and must
carry its own key; a keyless one is refused rather than run on the
operator's key. Only `embedModel` moves embeddings: writing or clearing a
chat key on any row never does.

**Whether the operator offers a slot.** The operator defaults read
(`GET /api/v1/operator/defaults`) reports each slot's source (`operator`,
`own` or `none`) and `operatorOffers`. `operatorOffers` is true when the
file serves the slot to this repository: for an `llm` slot, the file lists
the row id; for embeddings, the file has an `embeddings` entry. It is the
same whatever the source, the readiness or the allowance, so it says whether
removing the person's key or claim would fall back to the operator's.

**The allowance.** Each operator-paid call first reserves a conservative
ceiling in `operator_spend_reservations` inside one transaction that sums the
repository's reservations for the UTC calendar day under an advisory lock,
and is refused when the sum would pass the file's `budget.centsPerDay`. The
call then settles to its reported cost; a call whose usage is missing or
partial keeps its ceiling unless it failed in a way the vendor cannot have
billed, and a reservation still open after an hour settles at its ceiling.
The ceiling covers one billable attempt, so an operator-paid call is not
retried after an attempt that may have been billed. The ledger is read only
by the gate and the operator defaults read, never by anything the person
writes, and it sits apart from the person's own spend cap
([0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md)).
At the allowance a triggered run is held as 0149 holds one at the person's
cap, and any other caller is refused with `403 guard`, reason `quota`, in
words that name the site and the way out: "today's allowance from Example is
used up; it resets at 00:00 UTC; add your own key to keep going".

Writing the key into the rows lost because it puts the operator's key in the
repository: the sealed store, the changelog and the recovery export would
carry it, and the person could point the row's `baseURL` at their own server
and read it. A fallback that keeps the row's endpoint lost for the reason the
server-wide key was removed: the operator's bearer would travel to whatever
endpoint, header set or gateway the row names. Keeping today's rule leaves
the problem unsolved.

### Consequences

- Good, because a new repository chats and searches by meaning on its first
  day with nothing to set up.
- Good, because the person's key always wins, and clearing it returns the row
  to the operator's.
- Good, because the operator's bearer still never reaches an endpoint a
  repository chose: the row's transport fields are ignored, and requests
  carrying a key refuse redirects for the person's keys and the operator's
  alike.
- Bad, because the ledger overshoots by however far a vendor's real bill
  exceeds the margined estimate, and a crash that loses the database loses up
  to one day of it.
- Bad, because moving embeddings between the operator's entry and a row
  changes the vectors' pair, so the repository's vectors are bought again.
- Bad, because the operator sees the requests whichever key pays; the
  defaults file changes who pays, not who operates.

### Confirmation

Engine tests in `internal/engine/operatordefaults_db_test.go` hold the
resolver (an edited `baseURL` or header never reaches an operator-paid
request, a deleted row is not revived, an off-list model is refused, the
read's sources, `operatorOffers` and embeddings target), and
`internal/engine/operatorspend_db_test.go` holds the ledger (admit, refuse at
the allowance, settle, expire at the ceiling, one repository apart from
another) and the stamp. The `internal/operatordefaults` tests hold the
file's refusals, and `internal/llm` tests hold the refused redirects and the
retry rule. A deployment without `SUBSTRATE_OPERATOR_DEFAULTS` behaves as
before, its keyless-row refusal naming "this server".

## More Information

The plan's later phases (an OAuth client fallback and function secrets for
named published consumers) follow the same two rules: the key stays in the
defaults file, and the spend goes through the same allowance.
