---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0001. Record decisions in the repo

## Context and Problem Statement

This repository has nowhere to record why. `AGENTS.md` states the rules an
agent must follow, a commit title says what changed, and the reasoning behind
a choice lives in a pull request body that the squash merge folds away. The
one document that tried to hold reasoning was a 3000-line contract, and it was
deleted rather than patched, because it claimed to describe the system as it
is and the system moved out from under it.

The cost lands hardest on whoever arrives next, human or agent. An option that
was weighed and ruled out leaves no trace, so it comes back, and it comes back
with the same arguments and the same answer. Rules get followed without their
reasoning, which means they get followed past the point where they still make
sense.

## Considered Options

- Nothing: keep the reasoning in pull request bodies and issue threads.
- A contract document again, describing the system and the reasoning together.
- An ADR tool such as adr-tools, adrkit or log4brains, with a generated site.
- Plain Markdown records under `docs/decisions/`, a repository-specific
  adaptation of MADR 4.0.0, held by the docs linter that already exists.

## Decision Outcome

Chosen: plain Markdown records under `docs/decisions/`, adapting MADR 4.0.0,
with their shape held by a new section in `.mise/docscheck.sh`.

A record describes a moment, not the system, which is exactly the property the
deleted contract lacked: it cannot go stale by the system moving, because it
never claimed to describe the system now. It sits beside `docs/plans/`, which
is already the directory for work not yet landed, and it is read the same way
everything else here is read, with `git` and a text editor.

The tool was refused because the corpus is the value and the tool is not. A
generated site, a CLI to install and a second toolchain to pin buy indexing
that a table in a README does for a corpus this size. The rules that are worth
enforcing go where every other mechanical docs rule in this repository goes,
into the one docs linter, so `mise run lint:docs` and CI's lint job pick them
up with nothing to wire.

### Consequences

- Good, because an agent can read every prior decision, including the rejected
  and superseded ones, before proposing work. Not re-proposing something
  already ruled out is the whole point.
- Good, because `AGENTS.md` can stay one line per rule. The reasoning has
  somewhere to live that is not the rule itself.
- Good, because no new dependency, no new task and no generated artifact land
  with it.
- Bad, because it is another documentation genre next to `docs/plans/`,
  `AGENTS.md` and `docs/*.md`, and overlapping genres are how a practice dies.
  The borders are drawn in [README.md](README.md) for that reason, and the bar
  for writing a record at all is deliberately high.
- Bad, because a record can rot: the code can move under an accepted record
  and leave it silently wrong. Nothing detects that. The mitigation is that a
  record is never authoritative about the present, only about a moment.
- Bad, because the corpus only pays off if it is read. A record nobody opens
  before proposing work is a file, not a practice.

### Confirmation

The decision records section of `.mise/docscheck.sh`, which is run by
`mise run lint:docs` and gated by CI's lint job: names, unique numbers,
frontmatter, statuses, the index, and the supersede link. What it cannot decide,
whether a record was worth writing and whether it is honest, is held by review
and by the `adr` skill.

## More Information

- [MADR 4.0.0](https://adr.github.io/madr/) is the template this trims, and
  [README.md](README.md) lists the departures from it.
- The deleted contract document, and the rule that replaced it, are described
  at the top of [AGENTS.md](../../AGENTS.md).
- Revisit this if the index stops being readable at a glance, which is the
  point at which a real tool starts earning its keep, or if records are being
  written for choices that the bar in [README.md](README.md) excludes.
