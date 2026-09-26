---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-587 agent session)
---

# 0109. A decision number is checked against every live branch before the merge

## Context and Problem Statement

A record takes the next free number, and `lint:docs` refuses two records with
one number, but only inside one tree. Two branches off the same base that both
take the next number are each green until the second merges and renumbers. By
then the number is cited in commit messages, in other records and in other
repositories ([#587](https://github.com/geoah/substrate/issues/587): two
branches took `0086` on 2026-09-16, and the one that renumbered to `0088` had
been cited as `0086` in a consumer's tickets). On 2026-09-26, eleven open
branches held `0106`.

## Considered Options

- Reserve: a `docs/decisions/RESERVED` line merged to `main` before the record
  is written, and `lint:docs` refusing a record not reserved for its branch.
- Check wider: compare the records a branch adds against the numbers other
  branches on `origin` already add.
- Drop numbers from file names, keep the slug as the identity, and assign the
  number in the index at merge.

## Decision Outcome

Chosen: check wider, as `mise run decisions:check` in the `lint` CI job,
because it needs no extra merge per record and no change to how records are
named or cited. Reserving costs a round trip to `main` for every record, which
parallel agent branches would skip or race on. Dropping numbers rewrites how
102 records are cited.

A record a branch adds is refused when the base branch's tip already uses its
number, or when another branch added a different record with that number
first. "First" is the author date of the commit that added the file, which a
rebase keeps; ties fall to the file name, so both branches agree. A branch
whose tip is older than 30 days holds nothing, and a record whose title is on
`main` or in this tree under another number has merged or been renumbered. The
refusal names the next number nobody holds.

### Consequences

- Good, because a collision is red on the branch that took the number second,
  while nothing has cited it yet.
- Good, because it runs on the refs a full-history checkout already has: no
  new file, no network call beyond the fetch.
- Bad, because the check only sees pushed branches. Two branches pushed
  minutes apart can both be green until the later one's CI runs again.
- Bad, because the verdict can change without the tree changing: another
  branch pushed later with an earlier author date turns a green branch red
  on its next run.

### Confirmation

`.mise/decisionscheck.sh`, held by its scenarios in `.mise/cicheck.sh`
(`mise run lint:ci`): a later branch, a number taken on `main`, a stacked
branch, a merged-and-renumbered branch, an abandoned branch, an uncommitted
record, and an unresolvable base.

## More Information

The in-tree rule stays in `lint:docs`; this check is beside `kinds:check` and
`frozen:check` because, like them, it reads more than the files. Revisit if
records start being written outside this repository's branches (forks), which
this check cannot see.
