---
name: adr
description: Write, supersede and read the decision records in docs/decisions/. Use when asked to record a decision or write an ADR, when a choice that binds future code is about to be made, and BEFORE proposing design work, so an option already ruled out is not proposed again.
---

# Decision records

One record is one choice made at one time: what forced it, what else was on
the table, what was chosen, and what that costs. The corpus is
[docs/decisions/](../../../docs/decisions/README.md), and that README is the
rules. This skill is the procedure, and it does not restate them: where the
two could disagree, the README is right.

## 1. Does this need one?

Most of the time, no, and stopping a record from being written is the most
valuable thing this skill does. The bar is all three at once:

- the choice is hard to reverse, and
- it shapes what other code may do, and
- its reasoning is not already written down somewhere it will be found.

These are commits, not records: anything the code already says, a bug fix, a
rename, a library bump, a choice with one live option, and a rule `AGENTS.md`
already states along with its reason. A choice inside a plan is the plan's;
one is written only when the choice binds code beyond that plan's own work.

If the answer is no, say so and stop. Do not write a record to be thorough.

## 2. Read the corpus first

Before proposing design work of any kind, read the index table in
`docs/decisions/README.md`, including the rejected and superseded rows, and
open anything that touches the area you are about to work in. Re-proposing
something already ruled out is the failure this corpus exists to prevent, and
a rejected record is worth more here than an accepted one.

Say what you found. "0002 already settled this, and here is why it still
holds" is a complete answer to a design question.

## 3. Write it

Copy `docs/decisions/template.md` to `docs/decisions/NNNN-kebab-title.md`,
taking the next free number, and fill it in:

- Outcome first. Name the chosen option in the first sentence of Decision
  Outcome, then say why it beat the others.
- Under two pages. Link out to the plan, the issue or the code instead of
  restating them.
- Both halves of Consequences. A record with no Bad bullets was not written
  honestly.
- Confirmation names what keeps this true, or says "None: this is held by
  review only".
- House style: short sentences, no invented history. If the record is written
  after the fact, say so in the first sentence of Context and cite what it was
  reconstructed from. An option you cannot evidence from the tree is left out.

## 4. Supersede, never edit

An accepted or rejected record is frozen. Reversing a decision is two files:
the new record, which names what it replaces in prose, and the old one, which
gets exactly `status: superseded`, `superseded-by: NNNN` and a new `date`.
Nothing else in the old record moves. The link is stored forward only, and the
successor must be `accepted`, which the linter checks.

## 5. Land it

Add the row to the index table in `docs/decisions/README.md`, with a status
that matches the frontmatter, then:

```bash
mise run lint:docs
```

Commit as `docs(decisions): what was decided`.
