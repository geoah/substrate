---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0321. The repository migrations a repository ran travel with its archive

## Context and Problem Statement

A repository migration runs once per repository, and "once" was read from
the database's ledger (`repository_migrations`,
[0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md)).
A restore, an import or a take puts a repository into a database whose ledger
has nothing for it, so every migration ran again, each held to finding
nothing to do. That rule did not hold for every migration: a second run can
change what the repository wrote after the first one, such as the relay rows
migration 5 removes, which an older Google bundle still writes, or a bare
word migration 2 left and can resolve now
([#1362](https://github.com/geoah/substrate/issues/1362)).

## Considered Options

- The ledger travels with the archive: the manifest names it, and a restore
  records it before the first open.
- Each migration that can meet returning state leaves its own mark in the
  repository's history, as migration 7 does, and finds nothing where the
  mark is.

## Decision Outcome

Chosen: the ledger travels with the archive, because it closes every case at
once and for every migration written later, where a mark per migration
closes one case at a time and must be remembered by each author.

- Every manifest names the migrations the repository had run as of its
  `head` (`repositoryMigrations`: each one's version, name and the time it
  first ran), read in the snapshot the head is read in. A migration's writes
  and its ledger row commit together, so the list names exactly the
  migrations whose writes the history up to the head holds. An export is an
  archive and carries the same. A change of the ledger alone is enough for a
  pass to publish.
- A restore, an import and a take record the list in the transaction that
  inserts the repository's row, so the first open runs only what the
  repository had not run. A restore runs nothing the repository had run.
- The list is admitted with the dialects (`admitManifest`), on every door: a
  migration this binary does not carry refuses the restore before any row,
  naming it, and a take while the repository it would replace is whole and
  served. A list naming a migration under another name, or skipping one
  below its highest, is refused the same way as the open refuses such a
  ledger.
- A manifest that names no list, which is every archive written before this
  and a 0.x directory, restores as before: every migration runs again. A
  list says what the data had been through as of the manifest's head and
  dialect, so a manifest names one only from changelog dialect 8, the first
  whose archive could carry it: one below it that names a list is no
  manifest a binary wrote, and is refused as malformed.
- The key joins manifest format 3 with no new number, as `identities` did,
  because no release has carried format 3 yet. No line changes, so the
  changelog dialect stays.

### Consequences

- Good, because a migration no longer has to find nothing to do over state
  written after it ran, for any repository restored from an archive of this
  release or later.
- Good, because a newer binary's archive is refused before anything is
  written, as a newer dialect is, rather than at the first open after the
  rows are in.
- Bad, because an archive written before this names no list, and its restore
  still runs every migration again, with the cases above. A server of this
  release archives each repository once under it before an older copy is
  all there is.
- Bad, because a restore gains a refusal: an archive a newer binary wrote
  cannot be restored by this one even where the migration it ran changed
  nothing this binary reads.

### Confirmation

`TestARestoreRunsNoRepositoryMigrationItsArchiveHadRun` (restored, imported,
taken), `TestARestoreLeavesWhatTheRepositoryWroteAfterAMigrationRan`,
`TestAnArchiveThatNamesNoLedgerRunsEveryMigration`,
`TestAnArchiveNamingAMigrationThisBinaryLacksIsRefused`, and the manifest
vector in `internal/changelogfile/testdata/vectors.json`.

## More Information

The format is [docs/stored-format.md](../stored-format.md#the-manifest);
[upgrading the binary](../operations.md#upgrading-the-binary) says what an
operator sees.
