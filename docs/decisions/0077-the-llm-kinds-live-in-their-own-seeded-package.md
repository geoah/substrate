---
status: accepted
date: 2026-09-12
decision-makers: George Antoniadis
---

# 0077. The LLM kinds live in their own seeded package

## Context and Problem Statement

`substrate.reamde.dev/core` holds everything: the repository, tokens, the
vocabulary's own meta-model, the changelog's envelopes, the delivery plumbing,
a bundle's settings, and the agent runtime's four data kinds
(`llmprovider`, `llmthread`, `llmmessage`, `llminteraction`). A package that
holds everything says nothing about what belongs in it, and the four agent
kinds are the clearest thing in there that is a subsystem rather than the
substrate itself: they are alpha and unfrozen (ruling A12) while the rest of
core is what a repository cannot exist without. Their local names carry the
prefix `llm` precisely because they had no package to carry it for them.

## Considered Options

- Leave them in core and accept that core is the only package the binary ships.
- A second SEEDED package, `substrate.reamde.dev/llm`, written into every new
  repository beside core.
- Ship them as an installable provider bundle a repository opts into.

## Decision Outcome

Chosen: a second seeded package. The agent runtime is part of the substrate:
the engine writes threads and messages from Go constants, and `/agents` is a
first-party wire, so making it optional would mean the engine could address
kinds a repository does not hold. Seeding it costs nothing that core does not
already cost, and it gives the four kinds a home whose name says what they are.

- The package is `substrate.reamde.dev/llm` at version 1, and the four kinds
  are renamed as they move: `core/llmprovider` → `llm/provider`,
  `core/llmthread` → `llm/thread`, `core/llmmessage` → `llm/message`,
  `core/llminteraction` → `llm/interaction`.
- The rename is REQUIRED, not cosmetic. A seeded kind's GraphQL name is its
  bare singular (record 0058), and the boot upgrade never prunes (record
  0055), so a repository that predates this move keeps its `core/llmprovider`
  kind forever. `llm/llmprovider` would claim `Llmprovider` beside it and
  `graphqlNameProblems` would refuse the upgrade permanently. `Provider`,
  `Thread`, `Message` and `Interaction` are each free among the seeded kinds.
- The old names are NOT retired in core's header: retiring a name a live
  repository still declares is refused whole (`heldRetirementGuards`), so the
  four old kinds stay declared and dormant forever, exactly as `run` stayed
  when it became `triggerrun`.
- The old ROWS, unlike `run`'s, are MOVED. Nothing referenced a `run`; four
  kinds of agent state reference each other and core's `agent` references the
  provider, so leaving the rows behind would strand every repository that ever
  ran an agent. The move is its own decision, record
  [0078](0078-a-kind-move-is-ordinary-record-writes.md): the declarations here
  carry `movedFrom:`, and admitting them carries the rows and repoints the
  references in the same transaction.
- Nothing in the seed, the boot upgrade or `authorizeDeclarationWrite` changes:
  all three key on the AUTHORITY and on `source: builtin`, and
  `shippedPackages` was already plural.
- The `agent` kind stays in core. It is a manifest document kind, and
  `vocabulary/document.go` pins every manifest document to core; its REST
  routes (`/agent/{name}/call`, `/chat`) are frozen there. So do `setting` and
  `secret` (record 0076), `token`, `credential` and `trigger`.

### Consequences

- Good, because core stops being the place everything lands by default, and
  the next subsystem has a precedent to follow.
- Good, because the four kinds read as what they are: `llm/provider`, not
  `core/llmprovider`.
- A repository upgraded across this change ends up holding the four new kinds
  with every row it ever wrote, and the four old kinds declared and empty. The
  boot does it unattended; nothing is asked of an operator, and nothing is left
  pointing at a kind with no rows in it. Record 0078 is what makes that true,
  and without it the upgrade would be refused forever on the first `agent` row.
- Anything that spelled one of the old references BY HAND still breaks: a CLI
  script naming `substrate.reamde.dev/core/llmprovider`, a GraphQL query asking
  for `Llmprovider`, a bookmarked console URL. The records moved; the spellings
  did not follow.
- Four invariants that read "core" now read wider, and each picked its test
  deliberately:
  - `guardMergeType` (engine/merge.go) keys on `Source == builtin` rather than
    the package name, so every seeded package's rows stay out of the generic
    merge surface, llm threads included.
  - The stored closure is rebuilt and admitted with the seeded packages as ONE
    unit, and a failure of that unit is an error rather than a quarantine:
    `buildPackagesSeparately` buckets them together, `admissibleSubset`
    installs them with one `InstallAll`, and `loadStoredVocabulary` refuses
    rather than serving a repository "without" core or llm.
  - A bundle's `inputs` may name a kind in `core`, in `llm`, in its own
    package, or in a package it `requires:`. The seeded packages are always
    present, so no `requires:` line is needed for either.
  - `notifies:` stays keyed on the two package NAMES rather than on the source,
    because it runs inside the loader and the loader stamps whatever source its
    caller hands it (`LoadFS` builds any tree as `builtin`). Keying on source
    there would admit a bundle kind under the one loader that cannot tell.
- The same check accepts the dormant `core/llmthread` pin beside the new one.
  Without that, this binary could not FINALIZE a closure an older one seeded:
  the dormant `core/llminteraction` and `core/recordpatchrequest` declarations
  it inherited both carry `notifies:` against the old thread kind, and the
  repository would refuse to open. Nothing can write into core, so the legacy
  pin is reachable only from what an older binary seeded.
- CORE AND LLM ARE ONE UNIT to the closure rebuild, and this is the part that
  is easy to get wrong. The two reference each other, core's `agent` pointing
  at `llm/provider` and `llm/thread` pointing back at core's `agent`, which is
  a dependency CYCLE; the rebuild had two places that assumed a package stands
  alone, and either one alone would have quarantined both packages, which is
  the whole repository. A third seeded package joins the same unit: the seed
  admits as a whole or not at all.

### Confirmation

`internal/vocabulary/vocabulary_test.go` holds the seeded tree to exactly core
and llm and to the four new identities.
`internal/testenv/llmmove_db_test.go` seeds a repository from a FIXED COPY of
the previous shipped tree (`internal/testenv/testdata/kinds-before-llm-move`)
holding a provider, an agent that references it, a thread with two messages, an
interaction and a change request, boots this binary over the same database, and
asserts every row arrived under its new kind with every reference following it.
`mise run kinds:check` holds core's version bump, which is what makes the prune
read as an upgrade.

## More Information

Record 0055 (a retired name is declared, never inferred from a prune) is why
nothing is retired here. Record 0058 (a GraphQL name always carries the
authority) is why the local names had to change; its body says "non-core"
where the implementation says `source == builtin`, and the two agree today
because the seed was one package. From here on, `source == builtin` is the
rule, and 0058 stands otherwise unchanged.
