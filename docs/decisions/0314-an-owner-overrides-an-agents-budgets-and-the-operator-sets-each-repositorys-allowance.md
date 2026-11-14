---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0314. An owner overrides an agent's budgets, and the operator sets each repository's allowance

## Context and Problem Statement

Two daily limits stop agent runs. One is the agent's declared
`budgets.spendCentsPerDay`, beside the repository's own setting
([0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md),
[0217](0217-a-published-agent-takes-its-provider-through-a-bundle-input-and-declares-its-daily-cap.md)).
The other is the operator's allowance
([0235](0235-an-empty-key-on-an-operator-listed-provider-row-runs-on-the-operators-transport-under-a-daily-allowance.md)).

Nobody can change the first for an agent they did not declare. The seeded
assistant Moss declares 300 cents in the `assistant` package, which no
token may write, so an owner cannot give Moss 10 USD a day. The allowance is
one number for every repository, so an operator cannot give one person more
of its money than another.

Owners will want to change more of a shipped agent's limits than this one,
so the shape chosen here is the one those changes take too.

## Considered Options

- A general override record: a reference to the record it changes, a path
  into its fields, and a value.
- A record per value, such as one that holds the daily cap alone.
- One record per agent that carries the numeric limits of its `budgets`.
- Letting the declared cap lapse where the operator pays, so the allowance
  alone bounds those runs.
- For the operator's values: the defaults file, a control-plane table set
  through the operator's `substratectl` commands, or records.

## Decision Outcome

Chosen: one record per agent for the owner, the declared cap holding on
every key, and the operator's per-repository values in the defaults file.

**The owner's override** is a record of a new seeded kind,
`substrate.reamde.dev/llm/agentbudget`:

```yaml
kind: substrate.reamde.dev/llm/agentbudget
metadata:
  id: moss
data:
  properties:
    agent:
      ref: substrate.reamde.dev/core/agent/substrate.reamde.dev/assistant/moss
    spendCentsPerDay: 1000
```

- `agent` is a required reference to the `core/agent` it overrides.
- `spendCentsPerDay` is an optional whole number of US cents, from 0 to
  2^53-1. Each limit the record sets replaces the declared limit of the same
  name, on whichever key the runs go. A limit it leaves out keeps the
  declared value.
- An agent has one live record. The write of a second is refused with
  `conflict`, naming the first, so the record the owner reads is the one
  that holds.
- Only numeric limits of `budgets` may join this kind later. What an agent
  may read, write or call, its prompt and its provider each have their own
  door, and none is reachable here: grants, a bundle input
  ([0217](0217-a-published-agent-takes-its-provider-through-a-bundle-input-and-declares-its-daily-cap.md)),
  or an instruction
  ([0241](0241-the-assistant-ships-with-the-binary-and-the-owner-shapes-it-through-instruction-records.md)).
- The kind is `internal`, so a glob in an agent's `writes` never reaches it.
  Moss writes `*` and still cannot raise its own cap.
- It is a control kind of the declarations class
  ([0276](0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md)),
  as `core/retention` is, because it stands in for a key of a declaration and
  raising it spends more. A role reaches it only by naming it, so an editor's
  rules do not. Code reaches it only where its declaration names the kind,
  so a function whose `writes` are a glob does not. `admin` writes it.
- An import folds history in without the write's checks, so a repository
  may still hold two overrides of one agent. The lowest cap holds, the lower
  id on a tie, and the console shows that one and leaves one when the owner
  next changes it.

**The declared cap holds on every key**, as 0149 says. The repository's own
setting is unchanged.

**The operator's allowance is set per repository** under `budget.repositories`
in the defaults file, keyed by the repository's id or its full name:

```yaml
budget:
  centsPerDay: 300
  repositories:
    rnd-0abcdefghjkmnpqrstvwxyz012:
      centsPerDay: 1000
```

- A repository the file does not list takes `budget.centsPerDay`.
- Keys go through the one name lookup, so a former name within its 90 days
  still finds the repository
  ([0287](0287-a-repositorys-name-is-an-alias-and-a-former-name-holds-for-90-days.md)).
  An id never changes, so it is the key the docs recommend.
- A repository resolves its allowance at its first read and keeps it while
  it stays open. Where two keys find one repository, the lower holds.
- The start logs every key that finds no repository, finds one by a former
  name, or finds one that another key found too.
- Like every other key in the file, a change takes effect at the next start.

**Why the others lost:**
- **The general override record** puts a free-string path into a declaration
  where a typed reference belongs, holds an untyped value, and needs a
  second validator beside the kind system. Its list of overridable paths
  would be a kind per field in another form. The retention override chose a
  typed record per target for the same reasons
  ([0282](0282-a-kind-declares-how-long-its-history-is-kept-and-a-repository-may-override-it.md)).
- **A record per value** makes every further limit a new kind, and a
  migration to merge them later.
- **Letting the declared cap lapse on the operator's key** changes the
  meaning of every declared cap: an owner's own agent and a publisher's cap
  shown at approval would stop holding there. It would also remove the
  per-agent stop on a runaway run. The cap check runs at the root run, so a
  root run on the operator's key whose sub-agents run on the owner's key
  would spend the owner's money under no cap at all. The allowance bounds
  what the operator pays for a repository; an agent's cap bounds what one
  agent may consume. They answer different questions.
- **A control-plane table** would apply without a restart. The file already
  holds the operator's funding policy, and a restart is how it changes
  today.
- **Records** would put the operator's money in what the owner can write
  and export, which 0235 rules out.

### Consequences

- Good, because an owner raises or lowers any agent's daily cap, a shipped
  or published one included, without writing a declaration they cannot
  write.
- Good, because a further budget limit joins the same kind as one more
  optional property with a version bump.
- Good, because the operator gives one repository more or less of its money
  with one line in its file.
- Good, because an agent cannot raise its own budgets: only a grant that
  names the kind reaches it.
- Bad, because the operator cannot raise a person's agent cap for them: a
  person whose agent's declared cap sits under the allowance raises it
  themselves.
- Bad, because a change to the file needs a restart. A name key also stops
  finding the repository 90 days after a rename, which is why the docs
  recommend ids.
- Bad, because an older binary refuses `budget.repositories`, so a rollback
  means removing it from the file first.

### Confirmation

`internal/engine/spend_db_test.go`:
`TestAnOwnersAgentBudgetReplacesTheDeclaredCap`,
`TestAnAgentHasOneBudgetOverride` (a second create, a patch that moves one
onto another agent's, and the bounds), `TestTwoOverridesOfOneAgentAtOnceLandOne`
and `TestAnOwnersBudgetNamesALocalAgentByEitherSpelling`.
`TestAnAgentCannotWriteItsOwnBudgetsThroughAGlob`
(`internal/engine/agentnamedonly_db_test.go`),
`TestAnAgentBudgetIsWrittenByAnAdminAndNoGlob`
(`internal/engine/roles_internal_db_test.go`) and
`TestAnAgentBudgetIsAControlKind` (`internal/scope`) hold the control
kind.
`internal/engine/operatordefaults_db_test.go`:
`TestOnTheOperatorsKeyTheOwnersBudgetReplacesTheDeclaredCap`, whose run is
held to its repository's own line, and
`TestTheFileSetsOneRepositorysAllowance`, which covers a listed repository,
an unlisted one, a key that finds none, a repository registered after the
start, and one listed by its id and its name. The `internal/operatordefaults` tests hold the file's refusals of
`budget.repositories`.

## More Information

This amends 0149 (an owner may override an agent's cap), 0217 (an owner's
override replaces a published agent's declared cap) and 0235 (the allowance
per repository). Revisit the operator's values if live administration
without a restart is needed, and the operator's reach if operators need to
raise people's agent caps without them.
