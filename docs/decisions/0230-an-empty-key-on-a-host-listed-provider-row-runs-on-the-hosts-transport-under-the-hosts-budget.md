---
status: proposed
date: 2026-11-06
decision-makers: George Antoniadis
---

# 0230. An empty key on a host-listed provider row runs on the host's transport, under the host's budget

## Context and Problem Statement

A person registering on a substrate somebody else runs cannot chat with an
agent or search by meaning until they paste a vendor key into an
`llm/provider` row. The host holds those accounts and may pay for the people
it invites, but the engine removed the host-wide LLM key so that no host
bearer could reach a repository-chosen endpoint
(`kinds/substrate.reamde.dev/llm/provider.yaml`, `internal/llm/llm.go`
`WirePolicy`). [docs/plans/host-defaults.md](../plans/host-defaults.md)
proposes host defaults; this record is the part of it that binds the engine.

## Considered Options

- Keep every row on its own key (today).
- Write the host key into each repository's seeded rows.
- Fall back to the host key for an empty row and keep the row's own endpoint,
  headers and defaults.
- Serve an empty, host-listed row on the host's whole transport, from a file
  the host owns, and charge it to a ledger the host owns.

## Decision Outcome

Chosen: an empty key on a host-listed row runs on the host's whole
transport. A live `llm/provider` row with an empty `apiKey` whose id the file
`SUBSTRATE_HOST_DEFAULTS` lists is served by the host's entry: its wire, base
URL, key, headers, request defaults, cache, title model, model list and prices.
Nothing on the row except its id and its empty key is read for such a call, so
a person can reach a host-paid request only with a prompt and a model from the
host's list. A row with its own key behaves as before. When no row claims
`embedModel`, the host's one `embeddings` entry is the repository's embeddings
provider and stamps its vectors with the provider id `(host)`, which the record
id alphabet refuses, so no row's vectors can carry the same pair; a row that
claims `embedModel` must carry its own key.

Writing the key into the rows lost because it puts the host's credential in
the repository: the sealed store, the changelog and the recovery export would
carry it, and the person could point the row's `baseURL` at their own server
and read it. A fallback that keeps the row's endpoint lost for the reason the
host key was removed: a host bearer would travel to whatever endpoint, header
set or gateway the row names. Keeping today's rule leaves the problem
unsolved.

Host credentials never enter a repository. They are read from the host file
(`{file: ...}` or `{env: ...}`, never inline) at boot and held in the
process; no record, sealed value, changelog line or export carries them.

The host budget is the host's own ledger. Each host-paid call first reserves a
conservative ceiling in `host_spend_reservations` inside one transaction that
sums the repository's reservations for the UTC calendar day, and is refused
when the sum would pass the file's `budget.centsPerDay`. The call then settles
to its reported cost, a call whose usage is missing keeps its ceiling, and a
reservation still open after an hour settles at its ceiling. The ledger is
read only by the gate and the host services read, never by anything the
person writes, and it sits apart from the person's own spend cap
([0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md)).
A triggered run at the host limit is held as 0149 holds one at the person's
cap; any other caller is refused naming the limit and when it resets.

### Consequences

- Good, because a new repository chats and searches by meaning on its first
  day with nothing to set up.
- Good, because the person's key always wins, and clearing it returns the row
  to the host's.
- Good, because a host bearer still never reaches an endpoint a repository
  chose: the row's transport fields are ignored, and credential-bearing LLM
  requests refuse redirects for own and host keys alike.
- Bad, because the ledger overshoots by however far a vendor's real bill
  exceeds the margined estimate, and a crash that loses the database loses up
  to one day of it.
- Bad, because moving embeddings between the host's entry and a row changes
  the vectors' pair, so the repository's vectors are bought again.
- Bad, because the host operating the server sees the requests whichever key
  pays; the host file changes who pays, not who operates.

### Confirmation

Engine tests in `internal/engine/hostdefaults_db_test.go` hold the transport
rule (an edited `baseURL` or header never reaches a host-paid request, a
deleted row is not revived, an off-list model is refused) and the ledger
(admit, refuse at the limit, settle, expire at the ceiling). The
`internal/hostdefaults` tests hold the file's refusals, and
`internal/llm` tests hold the refused redirects. A deployment without
`SUBSTRATE_HOST_DEFAULTS` runs every existing test unchanged.

## More Information

The plan's later phases (an OAuth client fallback and function secrets for
named published consumers) follow the same two rules: the credential stays in
the host file, and the spend goes through the same ledger.
