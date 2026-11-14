---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
amended-by: 0279, 0309
---

# 0260. The stored format is a contract, each release keeps a directory, and history converts forward once

## Context and Problem Statement

A repository's directory (its manifest, its changelog segments, its sealed
files and blobs) is what the recovery export hands out and what a newer
server reads when it upgrades or restores. The guide called the effects
inside a line storage that was free to change (`docs/changelog.md`), no
directory any release wrote was kept, and the test meant to cover old data
wrote its history with the current code. An effect field a binary did not
know was dropped without a word (`internal/engine/fold.go`, `foldOpsOf`).
The plan in [pull request 1313](https://github.com/geoah/substrate/pull/1313)
(F8, and "The conversion at 1.0") asks for the format to be the contract
and for every later change of a stored shape to ship a conversion that a
directory kept from a release proves.

## Considered Options

- Keep the format internal, and keep testing the import with history the
  current code writes.
- Read old shapes where they are met: a decoder per dialect, chosen per line.
- One conversion chain, numbered like migrations and keyed on the changelog
  dialect stamp, run once per repository before anything folds its history;
  the format documented with test vectors; a directory kept from each
  release and imported under the current code.

## Decision Outcome

Chosen: the conversion chain, the documented format and the kept
directories, because they are the only option under which the server reads
one shape of each thing and a release's real output is what proves it.

What this sets:

- **The format is the export's contract.** The line's keys, the canonical
  form and its checksum, every effect with its fields, the manifest and the
  directory layout are written down in
  [docs/stored-format.md](../stored-format.md), with test vectors under
  `internal/changelogfile/testdata`. The keys of a line, of the manifest and
  of every effect are closed sets: a key a binary does not know is refused,
  never dropped. Before the effects turned strict, every effect field each
  released tag declared was read back; the only ones gone are the edge
  fields v0.1.0 to v0.35.0 wrote, on effects a history refuses since
  v0.36.0.
- **Each release keeps a directory.** `mise run release:keep <tag>` builds
  the tag, runs it, drives it through every op and every effect, and keeps
  the repository directory with every record and trigger state the release
  served, under `internal/testenv/testdata/kept/<tag>`. Nothing in it is
  real: `example.com` names and a credential key made for it.
  `TestKeptDirectories` imports each one under the current code and holds
  every record to the release's; the only differences it admits are the
  ones the entries the boot appends after the release's head carry, and
  none of those may move a value an owner, an app, a function or an agent
  set.
- **History converts forward once.** A change to a stored shape lands as a
  step of the chain in `internal/engine/conversion.go`, step N converting
  dialect N-1 to N, and raises the dialect. A step is handed one line and
  rewrites it in place; the runner recomputes the checksum. The boot check
  runs the steps a repository lacks, in one pass, on a repository whose
  table and files agree and on an import between its rows going in and
  their fold: the converted lines are written beside the live ones, the
  table takes them in one transaction with the new stamp and the
  import-progress marker, the converted directory replaces the live one,
  and the fold is rebuilt. A crash at any step leaves a state the next boot
  finishes. A step is idempotent. A process that does not run the boot
  check refuses an unconverted history.
- **How old a directory may be.** A 1.x server reads what any earlier 1.x
  release wrote. Before 1.0, the kept directories are the promise: the
  oldest one kept is the oldest a release reads. A step is carried for one
  major version, and once the steps are dropped after 1.0, a 0.x directory
  passes through 1.0 first.

Keeping the format internal is what F8 found broken: the first
incompatibility would be met by someone restoring a backup. Reading old
shapes where they are met makes every reader of history (the fold, the
change event's values, the webhook signature replay) carry every old shape
for as long as any directory might hold it, which is two sets of bugs for
every change.

A conversion is not a repository migration
([0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md)),
and both stay. A migration changes what records hold, within the current
shapes, by appending ordinary writes, and its ledger is the database's, so
it runs again over every import. A conversion changes the shape history is
written in, by rewriting lines in place and appending nothing, and its
record is the repository's own stamp, which travels in the manifest. A
change that needs both lands both, the step first.

### Consequences

- Good, because the export a person downloads has a written, tested shape,
  and an older release's directory is evidence, not an assumption.
- Good, because a later change of a stored shape (the plan's steps 11, 15
  and 21) adds one step and one test, and nothing that reads history
  changes.
- Good, because an effect field a binary does not know now refuses the
  entry by name instead of folding a record short.
- Bad, because rewriting history in place moves every checksum it touches,
  so a checksum proves a line undamaged since its last conversion, not
  since it was first written.
- Bad, because the conversion runs inside the boot and its time grows with
  history; today the table side is one transaction. It is timed at 100 GB
  before 1.0 (the plan's step 38).
- Bad, because each release spends about six minutes making its kept
  directory, most of it waiting for the release's GC sweep, and the tree
  grows by about 1.5 MB a release.

### Confirmation

`TestKeptDirectories` (`internal/testenv/kept_db_test.go`, the `db rest`
job) imports every kept directory and compares every record.
`TestEveryEffectFieldReadsBack` and `TestAnUnknownEffectFieldIsRefused` hold
the closed effects; `TestTheLineVectors` holds the canonical form;
`TestTheConversionChainIsNumberedAndFiled` holds the chain to its numbering
and its files; `TestAConversionCutShortIsFinishedByTheNextBoot` kills the
boot at each durable step of a conversion. `mise run frozen:check` refuses
an edit to a kept directory or to a landed conversion step.

## More Information

The step 7 of the plan moves the changelog into Postgres with the files as
an archive, and replaces
[0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md),
[0051](0051-a-repository-directory-is-the-backup-unit.md),
[0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)
and [0069](0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md).
What a step is handed (one line) does not change with it; the runner does.
