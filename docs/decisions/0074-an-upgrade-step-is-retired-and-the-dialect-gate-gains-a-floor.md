---
status: accepted
date: 2026-09-09
decision-makers: George Antoniadis (via the repo-cleanup agent session)
---

# 0074. An upgrade step is retired rather than carried forever, and the changelog dialect gate gains a floor

## Context and Problem Statement

The engine carried a compatibility path for every store shape any release had
ever written: the pre-authority directory migration (a directory named by the
random id a binary before
[0052](0052-the-authority-is-the-repository-id.md) minted, moved under its
authority with its DEK re-wrapped), the format-1 manifest v0.47.0 through
v0.53.0 wrote, the stray self-description record such a changelog folded, and
the delivery ledger's adoption of the trigger tables
([0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)).
Each ran at every boot of every repository, cost reads on the common path,
and — because none of them had an expiry — was going to be read, maintained
and reasoned about by everybody who touched the open ladder from here on. The
changelog dialect gate had a ceiling and no floor
([0057](0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md)
and after), so nothing in the code said which of those shapes was still
supported: the answer was "all of them, forever".

## Considered Options

- Retire the steps and give the gate a floor: refuse a store stamped below
  the oldest dialect this binary adopts, naming the release that still did
- Keep every step, and revisit at 1.0
- Keep the steps but mark them deprecated in comments
- Write a one-shot `substratectl` migration command an operator runs instead

## Decision Outcome

Chosen: retire the steps and give the gate a floor, because a compatibility
path with no expiry is not a compatibility path, it is a second
implementation of the open ladder that nobody may delete. Marking them
deprecated changes nothing a reader can rely on, and an operator-run
migration is the same code with a worse trigger: the boot is where the store
is already being read, and an upgrade nobody remembers to run is an outage
with extra steps.

**`minChangelogDialect` is the oldest stamp this binary opens, and today it
equals `maxChangelogDialect`.** A store stamped below it is refused at the
open and before a boot import writes any row, with an error naming both
numbers and `lastAdoptingRelease` — the release whose open still carried the
steps — because booting that release once, so its adoption commits and its
first append re-stamps the store, is the entire remedy. An unstamped store is
two things and the gate tells them apart by asking the changelog: nothing in
it is a fresh repository, which opens; entries in it are a history from before
the stamp existed, refused like any other store below the floor.

Retiring a step therefore has one price and it is paid in one place: raise
`minChangelogDialect`, set `lastAdoptingRelease` to the release being left
behind, and delete the step. The floor is not a version policy for the
project — it says nothing about REST or the vocabulary — it is the answer to
one question, which store shapes this binary's fold can complete.

**The floor does not replace the refusals that read the entries.** A stamp is
a claim about a store and the segment files are the store: a directory
written from the tables of a repository whose stamp ran ahead of its history
satisfies every dialect check and still holds a spelling with nothing to fold
into. So `foldRefuses` still refuses a `link` or `unlink` op by name, which
is what stops a rebuild of a current store whose files hold one, and
`refuseRetiredEntriesInFiles` still reads the entries an import is about to
insert, before any row exists. `ErrChangelogRetiredEntry` is a separate error
from `ErrChangelogDialectRetired` for exactly this reason.

### Consequences

- Good, because the supported set is a number in the source instead of an
  accumulation of untested branches, and the refusal tells an operator
  exactly which binary to boot rather than leaving them to discover the
  problem on the day they rebuild.
- Good, because the common path loses the reads those steps cost at every
  boot, and the open ladder is short enough to read.
- Good, because the pre-authority family goes with it: `changelogfile` reads
  one manifest format, `blobbytes` drops `LegacyRepositoryLister`, and
  `records` no longer holds a self-description under an id that is not the
  repository's.
- Bad, because a deployment that skips releases is now refused rather than
  silently carried: a repository written by v0.1.0 through v0.65.0 must boot
  v0.65.0 once before this binary. That is a real operational step, and it
  only works while that release is still runnable.
- Bad, because the floor equalling the maximum makes the dialect TRANSITION
  unobservable: a store between the floor and the maximum does not exist, so
  the tests that watch the stamp move and the manifest move with it
  (`TestManifestFollowsTheChangelogStampBeforeARestart` and the two
  manifest-ordering fault tests) skip until the next rung lands. They are
  written against the constants and re-arm themselves, rather than being
  deleted, because the ordering they hold is still the one the code
  implements.
- Bad, because a directory written by v0.47.0 through v0.53.0 can no longer
  be imported at all: the format-1 manifest reader is gone, and the manifest
  is what an import reads before it creates a row. The rewrap
  ([0054](0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md))
  reads the same one format.

### Confirmation

`TestAStoreBelowTheFloorIsRefused` holds the comparison, including that the
refusal names `lastAdoptingRelease`.
`TestOpenRefusesAStoreBelowTheDialectFloor` and
`TestAnUnstampedStoreIsJudgedByItsEntries` hold the open-time behaviour over
a real store for every stamp below the maximum, and
`TestImportRefusesARetiredDialectBeforeAnyRow` holds the import's half,
including that nothing is written. The entry-reading refusals keep the tests
they had, `TestRebuildRefusesARetiredLinkOp` and
`TestImportRefusesARetiredLinkEntryBeforeAnyRow`, which is what makes the
distinction between a stamp and a file checkable rather than argued.
`changelogdialect_internal_test.go`
already fails when an op or fold effect is added without moving the ceiling;
the floor is checked against it there, so a floor above the maximum cannot
land.

## More Information

This supersedes nothing: no earlier record claimed the steps were permanent.
It amends the gate described in
[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md),
whose adoption step is the first one retired under this rule.

Worth reopening if the project acquires deployments it does not control, where
"boot the older release once" stops being something the operator can be asked
to do. The answer then is a standalone migration binary per retired step, not
a step carried in the server forever.
