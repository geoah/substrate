# Decision records

A **decision record** is one page about one choice: what forced it, what else
was on the table, what was chosen, and what that costs. It is dated, it is
short, and once it is accepted it does not change. This directory is the
corpus, and its point is that a choice already made and a choice already ruled
out can both be found before somebody proposes them again.

These are contributor-facing history, not reader documentation. That single
distinction decides most of what follows.

> The substrate has its own `decision`: the verdict on a proposed mutation,
> from the engine's decision loop. It is a different thing entirely. The word
> here always means the document.

## This is a repository-specific adaptation of MADR

The template is [MADR 4.0.0](https://adr.github.io/madr/) trimmed, not MADR.
The departures, so nobody reads the difference as a mistake:

- **Four statuses, no more**: `proposed`, `accepted`, `rejected`,
  `superseded`. MADR leaves the set open. `deprecated` is deliberately absent,
  because nothing here needs the distinction between deprecated and
  superseded, and a fifth value nobody can define is a lifecycle nobody can
  follow.
- **`superseded-by:`** is a frontmatter key here, checked by the linter.
- **Confirmation is expected**, not optional. A decision with nothing holding
  it is a decision that rots quietly, and writing "None: this is held by
  review only" is the honest version of that.

Nothing else is added: no tool, no generated site, no path governance, no
review schedule. The corpus is Markdown in git, and the one docs linter holds
its shape.

## What earns a record

A decision record is for a choice that is hard to reverse, shapes what other
code may do, and whose reasoning is not already written down somewhere it will
be found. All three, not any one.

Not a record, and these are the common mistakes:

- Anything the code already says. The code is the contract.
- A bug fix, a rename, a library bump, a refactor.
- A choice with one live option. That is not a decision, it is a consequence.
- A rule `AGENTS.md` already states along with its reason. A record is written
  only where the rule is there and the reasoning is not.

Those are commits. A good commit message carries them.

### The neighbours

Three overlapping documentation genres is how a practice dies, so the borders
are drawn here:

- **`docs/plans/*.md`** propose future work and change while the work is in
  flight. They already hold settled design, rejected alternatives and risks. A
  plan does not get a decision record for every choice inside it. One is
  written only for a choice that binds code beyond the plan's own work, and it
  links to the plan instead of restating its alternatives.
- **`AGENTS.md`** states the rule an agent must follow, in one line, and stays
  the operative text. A record explains a rule; it never replaces it. A house
  rule may link its record.
- **`docs/*.md`** describe the system as built. A record describes a moment.

## Names and numbers

Files are `NNNN-kebab-title.md`, four digits. `template.md` is the template
and `README.md` is this page; everything else in this directory is a record.

The number is the next free one. Numbers are permanent once merged to `main`:
if two branches take the same number, the one that merges second renumbers
before merging. There is no contiguity contract. Gaps are legal, so the linter
checks the format and uniqueness and nothing more, and nobody should read the
sequence as a count.

## Statuses and lifecycle

- `proposed` becomes `accepted` or `rejected`.
- `accepted` becomes `superseded`.
- `rejected` and `superseded` are terminal.
- `date` is the date of the last status change, and it is the only frontmatter
  field that changes when the status does.

## Superseding, not editing

Once a record is `accepted` or `rejected`, its Context, Considered Options,
Decision Outcome and Consequences are frozen. Reversing a decision means a NEW
record. The old one gets exactly two frontmatter edits, `status: superseded`
and `superseded-by: NNNN`, plus its `date`.

Only one direction of the link is stored: the superseded record points forward
to its successor. The successor names what it replaces in prose, in its
Context or its More Information. Storing both directions would be a second
source of truth with nothing keeping the two in sync, and one direction makes
a cycle impossible, because the linter requires the successor to be
`accepted`.

Mechanical repair is allowed. A link whose target was renamed may be fixed,
because `lint:docs` runs lychee over the whole tree and a dead link is a bug
wherever it lives. Repairing a link does not touch the decision, and git holds
the original either way.

## A decision record can rot, and that is expected

The code can move under an accepted record and leave it silently wrong. This
directory does not solve that and does not claim to. The code, the tests and
`AGENTS.md` govern what is true NOW; a record is historical and is never
authoritative about the present. Keeping the corpus honest means one thing
only: when a decision is reversed, the record that held it is superseded.

For the same reason these pages are not held to the live words in
[terms.md](../terms.md), and `docscheck.sh`'s vocabulary rule deliberately
does not reach them. A record written today used today's words. A vocabulary
change two years from now must not force an edit into a frozen document, and a
record that says what it said in 2026 is doing its job.

## What the linter holds

`mise run lint:docs` refuses:

1. A file here that is not `NNNN-kebab-title.md` (`README.md` and
   `template.md` excepted).
2. Two records with the same number.
3. A record without a frontmatter block at line 1 carrying a `status:` from
   the four and a `date:` as `YYYY-MM-DD`.
4. A record this index does not list.
5. A `superseded` record with no `superseded-by:`, or one naming a record that
   does not exist, is itself, or is not `accepted`. Also a record that carries
   `superseded-by:` without being superseded.
6. An index row whose status disagrees with the record's frontmatter.

Not held, on purpose: whether the record is under two pages, whether the
options were seriously considered, and whether Confirmation names a real test.
Those are a reviewer's, which is where `docscheck.sh` already draws its line.

## The index

| #                                              | Decision                                      | Status   |
| ---------------------------------------------- | --------------------------------------------- | -------- |
| [0001](0001-record-decisions-in-the-repo.md)   | Record decisions in the repo                  | accepted |
| [0002](0002-declaration-versions-are-integers.md) | Declaration versions are integers the API maintains | accepted |
| [0003](0003-notifies-is-the-one-resolution-primitive.md) | A transition's notifies marker is the one resolution primitive | accepted |
| [0004](0004-asks-are-records-the-owner-resolves.md) | Asks are batch records only the owner resolves | accepted |
| [0005](0005-the-policy-door-is-deterministic-and-the-judge-recommends.md) | The policy door is deterministic; the judge only ever recommends | accepted |
| [0006](0006-voluntary-proposals-stay-self-acceptable.md) | Voluntary proposals stay self-acceptable; only gated requests refuse bundle decisions | accepted |
| [0007](0007-the-audit-lives-on-the-records-it-concerns.md) | The audit lives on the records it concerns, not in an audit kind | accepted |
| [0008](0008-keep-the-loop-do-not-adopt-adk-go.md) | Keep the agent loop; do not adopt ADK for Go | accepted |
| [0012](0012-numbers-are-exact-or-refused.md)   | Numbers are exact or refused: string-carried decimal, safe-integer int, one stored duration grammar | accepted |
