---
status: accepted
date: 2026-09-14
decision-makers: George Antoniadis
---

# 0080. A kind grant may glob, and a glob never reaches auth material

## Context and Problem Statement

A `permissions.reads.kinds` or `permissions.writes` entry has to be a full
kind identity: the loader refuses a `*` outright ("writes names them, no
globs", `internal/vocabulary/agent.go` and `function.go`), and
`resolvePackageAgents` refuses a grant naming a kind the registry cannot
resolve. So a grant is a closed set, fixed when the declaration loads. That
makes a general-purpose agent impossible to ship: an agent that works over
the owner's data has to name every kind, the shipped sample cannot name kinds
the owner imports later, and adding a bundle means hand-editing every agent
that should see it.

Trigger selectors already have the vocabulary for this. `source.record.kinds`
takes `*`, `<authority>/*` and `<authority>/<package>/*`, matched by
`MatchTypeGlob` in `internal/vocabulary/function.go`.

## Considered Options

- Keep grants closed; an owner names each kind after importing it.
- Accept the trigger-selector globs in kind grants, matching every kind.
- Accept them, but never let a glob reach the auth kinds.
- Accept them in `reads` only, leaving `writes` closed.

## Decision Outcome

Chosen: accept the trigger-selector globs in every kind grant — agent and
function, `reads.kinds` and `writes` alike — with the auth kinds carved out of
what a glob can match. It beat the closed-set option because the reach an
owner wants is real and the alternative is hand-editing a grant per import; it
beat the plain version because `*` spans `substrate.reamde.dev/core`, where
`token`, `credential`, `secret` and `recoverykey` live, and a `*` write grant
would otherwise hand an agent loop the repository's own auth material; and it
beat reads-only because a read grant already exposes everything a write grant
could corrupt, so splitting them buys less than it costs in explanation.

This passes the test [0038](0038-a-network-entry-is-a-bare-host-or-cidr-destination.md)
set for `permissions.network`. That record refused a wildcard because the
egress confinement filters by resolved destination and "a wildcard would
promise a match nothing evaluates". Here something does evaluate it:
`MatchTypeGlob` is the same matcher trigger selectors are held to, and the
grant gates reuse it rather than growing a second grammar. 0038 is untouched —
a network entry stays a bare destination, because that runtime still has no
per-host gate.

Four rules make it precise:

- **The grammar is the selector's.** `*`, `<authority>/*`,
  `<authority>/<package>/*`, or an exact reference. `ValidTypeGlob` already
  decides it. `permissions.call` and `permissions.network` are NOT in scope:
  `call` names functions rather than kinds, and `network` is 0038's.
- **A glob never matches an auth kind.** `substrate.reamde.dev/core/token`,
  `/credential`, `/secret` and `/recoverykey` are reachable only by an entry
  that spells one out. So `*` is "everything the owner has", not "everything,
  including the keys to the substrate".
- **A glob resolves to nothing at load.** The existence check that refuses an
  unknown kind applies to exact entries alone, because a glob's whole purpose
  is to cover kinds that are not there yet. A grant is therefore no longer a
  closed set: importing a bundle widens an existing glob grant, by design.
- **Intersection is by subsumption.** `effectiveEmit` narrows a sub-agent's
  emit to its caller's ceiling, and two patterns here form a prefix lattice,
  so the intersection of `p` and `q` is whichever subsumes the other, and
  empty when neither does. A sub-agent can still never write what its chain
  could not.

### Consequences

- Good, because one agent can be granted the owner's whole authority
  (`ada.example.com/*`) and keeps working as bundles are imported.
- Good, because the grant grammar and the selector grammar are one matcher, so
  a reader learns it once and a typo fails the same way in both.
- Good, because the auth kinds become unreachable by accident: reaching one is
  now a deliberate, greppable line in a declaration.
- Bad, because a grant is no longer a closed set read off the declaration: to
  know what an agent may write you need the declaration AND the repository's
  current kinds, and importing a bundle silently widens an existing grant.
- Bad, because the load-time existence check no longer catches a typo in a
  globbed entry — `ada.example.com/tsaks/*` is well-formed and matches
  nothing, where the exact spelling would have been refused.
- Bad, because the auth carve-out is a rule with no syntax: a reader of
  `writes: ["*"]` cannot see the exception without knowing it exists, which is
  why it is documented on the kind and in `docs/agents.md`.

### Confirmation

`TestGrantGlobGrammar` and `TestGrantGlobNeverMatchesAuthKinds` in
`internal/vocabulary/` cover the admitted and refused spellings and the
carve-out on every gate; `TestEffectiveEmitIntersectsGlobsBySubsumption` in
`internal/engine/` covers the ceiling lattice, including a globbed ceiling
over a globbed sub-agent. `mise run kinds:check` holds the shipped closures to
the grammar.

## More Information

Builds on [0038](0038-a-network-entry-is-a-bare-host-or-cidr-destination.md),
which refused a wildcard in `permissions.network` and states the test this
passes. Reopen if a grant needs to exclude a kind it otherwise matches: a
subtraction (`ada.example.com/*` minus one kind) is deliberately not part of
this, and adding one is a second dialect change rather than a reading of what
lands here.
