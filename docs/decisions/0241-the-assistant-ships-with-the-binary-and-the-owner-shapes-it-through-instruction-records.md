---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0241. The assistant ships with the binary, and the owner shapes it through instruction records

## Context and Problem Statement

Moss, the owner's assistant ([docs/plans/moss.md](../plans/moss.md)), was a
sample package imported onto the owner's authority, so the owner could
edit its prompts. Its prompts will change every week while its evals are
written, and a sample reaches an existing repository only through an
upgrade the owner accepts, which replaces whatever they edited. Owners
still need to shape it ("no bullet points", "go by Juniper", "give every
new collection a notes field"), and the assistant should change itself
when asked, without the change being a way for a record's content to
reprogram it.

## Considered Options

- Keep the assistant a sample the owner edits in place.
- Ship it, and let the owner change nothing but memories.
- Ship it, and add a record that names one agent and carries an
  instruction the engine renders after its prompt.
- Ship it, and let the agent rewrite its own prompt.

## Decision Outcome

Chosen: ship it, and shape it through instruction records, because the
binary keeps the prompt every repository runs current while the owner's
changes ride beside it, and a change the agent makes to itself passes
through the owner like every other reviewed write.

- **The assistant is a seeded package**, `substrate.reamde.dev/assistant`
  (`moss`, `architect`, `librarian`), written into every repository and
  upgraded at boot like `core` (record 0192's seeded list gains it). Its
  declarations are `builtin`, so no token and no agent writes them. An
  owner who wants a different prompt forks one under their own authority.
- **An instruction is a record of `substrate.reamde.dev/llm/instruction`:**
  `agent` (a reference to one agent), `text`, `active` and `source`, like
  a memory, and `primary`, so the owner lists, edits and retires them.
- **The engine renders an agent's active instructions into its system
  prompt**, after the engine's guidance and before what it pins, as an
  `<owner_instructions>` block: the owner's direction, not data. A later
  instruction wins over an earlier one and over the prompt where they
  differ. The block is read when a thread opens and kept on the thread
  with the pinned snapshot, so a thread's prompt stays one cached prefix.
  A run whose reply the engine parses, and an agent that turns the
  engine's guidance off, gets none.
- **An instruction never widens what an agent may do.** It changes how
  the agent works; its tools, its grants and the owner's policies decide
  what it may do.
- **A glob never reaches the instruction kind**, as it never reaches a
  declaration or a trigger (0239). An agent writes one only where its own
  writes name the kind. Moss, granted `*`, proposes one when the owner
  asks for a lasting change, and the owner's accept is what lands it; an
  agent granted `*` cannot accept a request either, because an accept is
  held to the exact kind. So a record or a page that talks Moss into
  "updating its instructions" produces a proposal the owner reads, never a
  change.

Memories stay what they are: facts about the owner, pinned as data for any
agent that pins them. An instruction is direction to one agent.

### Consequences

- Good, because a prompt fix reaches every repository at its next open.
- Good, because the owner's changes survive every upgrade: they are their
  records, not edits to a declaration the binary replaces.
- Good, because "change how you work" is the decision loop the owner
  already uses for memories and edits, and every change has an author and
  a history in the changelog.
- Bad, because an owner cannot rewrite the assistant's prompt in place;
  they add to it, or they fork it and maintain the fork.
- Bad, because the model chooses between the shipped prompt and an
  instruction that contradicts it; the block says the instruction wins,
  and nothing stronger holds it.
- Bad, because an instruction accepted mid-thread reaches the next thread,
  not the current one; the conversation itself carries it until then.
- Bad, because the owner cannot yet choose the shipped agents' model
  without forking; a bundle input as published agents use
  ([0217](0217-a-published-agent-takes-its-provider-through-a-bundle-input-and-declares-its-daily-cap.md))
  is the follow-up.

### Confirmation

`internal/engine/agentinstructions_db_test.go`: the active instructions of
one agent reach its prompt between the guidance and the pinned memories,
an inactive one or another agent's do not; Moss's `write` of an
instruction is refused while its proposal lands and, accepted, reaches the
next thread; and a repository born before the assistant receives it, with
the instruction kind, at its next open. `internal/vocabulary`'s seeded-tree
test names the fifth seeded package.

## More Information

Personal agent harnesses converged on the same layers: a persona or
"soul" the agent may revise, standing operating instructions, facts about
the user, and memory consulted on demand; the comparison is in
[docs/plans/moss.md](../plans/moss.md#how-the-owner-shapes-it). Revisit if
owners fork the assistant to change the prompt more often than they add
instructions, or when an instruction should apply to every agent rather
than one.
