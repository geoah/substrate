---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
amended-by: 0075
---

# 0065. A snapshot is a stopped-server copy that records its head and lists what it needs

## Context and Problem Statement

[0051](0051-a-repository-directory-is-the-backup-unit.md) made a backup "copy
the directory at any moment, verify the copy, retake one that fails", and
`repository verify` checked the changelog files against the table and the
sealed files against the sealed rows byte for byte. It read no blob, opened
no sealed file and walked no live record's secret reference, so a copy short
of a `stored` blob's bytes or a live secret's file passed, and after an import
into an empty database (which loads whatever files it finds) the sealed half
of the check was true by construction. Nothing in the copy said which point
it held: the manifest carries no head
([#216](https://github.com/geoah/substrate/issues/216)). The owner's
conditions: a stopped-server capture is enough, the point must be recorded,
secrets are validated by decrypting them, blobs by hashing them, and under
`s3` the required objects are part of the procedure.

## Considered Options

- A `repository snapshot` operator command: verify the repository whole,
  copy the directory into a destination root, verify the copy, write a
  `snapshot.json` naming the committed head and its checksum, and make
  `verify` check blobs, live secret references and, under the key, every
  sealed file.
- A documented copy order (`changelog/` before the side stores) plus the
  stronger `verify`, with no command and no recorded point.
- Record the history generation of [0056](0056-a-change-cursor-is-a-seq-under-a-history-generation.md)
  as the point instead of the head and its checksum.
- Have the server write the head into the directory on every commit, so any
  copy carries its point.
- Under `s3`, download the objects into the copy's `blobs/` so the directory
  is self-contained.

## Decision Outcome

Chosen: the command, with the head seq and that entry's checksum as the
recorded point, and the stronger `verify` beside it. `SnapshotRepository`
opens the repository as its writer, so a running server refuses it
(`ErrChangelogLocked`); it needs `SUBSTRATE_CREDENTIAL_KEY`, because it
proves every sealed file opens before it copies; it runs the whole
verification and refuses on any finding; it copies the manifest, every
segment and sidecar, every committed sealed file and, under `fs`, the bytes
of every `stored` manifest, each hashed against its digest; it verifies the
copy's changelog and sealed files; and it writes `snapshot.json` last, so a
directory carrying one is a copy that finished. The copy is laid out as a
data root (`<destination>/repositories/<authority>/`), so the restore is the
one 0051 documented. `verify` now also reads every `stored` blob out of the
configured store and hashes it, holds every secret reference a live record
holds (by the repository's own declarations) to the sealed files, opens every
sealed file under the DEK when the key is present, and, when `snapshot.json`
is there, checks that the entry at the recorded head is in the files with the
recorded checksum. A head past the point is not a finding. Files the fold
does not name are not findings either: a blob nobody references and a note at
the top of the directory pass, a missing required file fails.

The head and its checksum were chosen over the generation because they are a
property of the files alone, checkable in the copy without the database that
wrote it, and the checksum names the exact entry, so a copy of another
history reaching the same seq is told apart. The generation is minted anew at
import (0056), so a restored repository never carries the one a snapshot
would record. A per-commit head file was rejected in
[0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md):
it is a second commit point and a second fsync per write; the snapshot file is
written once, offline, by a process that holds the lock. The copy order alone
was rejected because it cannot record a point and cannot refuse a copy taken
beside a live writer. Under `s3` the snapshot lists the objects
(`blobLocation` plus each digest) rather than downloading them, because `s3`
exists for deployments whose disk cannot hold the attachments; the restore
copies the listed objects and `verify` hashes them after the import.

0051's copy-at-any-moment procedure stands beside this: a cron copy is still
a backup once `verify` passes on it, and copies taken before this landed get
no promise beyond 0051's. 0051 is already superseded by
[0052](0052-the-authority-is-the-repository-id.md) for the directory's name;
nothing here changes what it decided.

### Consequences

- Good, because a snapshot is one command, refuses beside a live server, and
  carries its own point: `verify` prints it after the restore.
- Good, because `verify` on a restored copy now fails on the three damages an
  import cannot tell from a healthy directory: a `stored` manifest with no
  bytes or other bytes, a live secret with no file, a sealed file the DEK
  does not open.
- Good, because the snapshot holds exactly what the fold needs: pending
  uploads, tombstoned blobs and staged sealed files are not copied.
- Bad, because the snapshot needs the server stopped, so it is a maintenance
  window; the cron copy of 0051 remains the live option.
- Bad, because `verify` reads and hashes every stored blob, so it costs the
  size of the repository's attachments, over the network under `s3`, and
  beside a live server a blob the sweep collected a moment ago can be a false
  finding until the next run.
- Bad, because an `s3` snapshot is not self-contained: the directory names
  the objects and the operator copies them; a restore that skips that step
  comes back with `stored` manifests and no bytes, which `verify` names.
- Bad, because a restored directory keeps a `snapshot.json` that names a
  point the repository has grown past; a later snapshot overwrites it.

### Confirmation

`TestVerifyReportsMissingAndDamagedSideStoreFiles` (internal/engine) deletes
one stored blob's bytes and one live secret's file from a stopped
repository's copy, damages a second blob in place, adds an unreferenced blob
and a stray file, imports the copy and asserts exactly the three findings.
`TestVerifyOpensEverySealedFileUnderTheKey` corrupts one payload in the file
and the row alike, asserts the finding under the key and none without it.
`TestSnapshotRecordsThePointAndRestoresIntoAnEmptyDatabase` snapshots,
refuses a second snapshot over the first, restores the destination as a data
root, asserts the fold, the blob, the secret, the recorded point and that a
write past it is not a finding. `TestSnapshotRefusesTheLockAndADamagedRepository`
holds the refusals. `TestSnapshotRoundTrip` and
`TestSnapshotRefusesAnotherFormatAndUnknownKeys` (internal/changelogfile)
hold the file's closed key set. `TestRoundTripDirectoryRestoresARepository`
still holds 0051's copy.

## More Information

[docs/operations.md](../operations.md#backups) has the procedure, the `s3`
objects and the two copy windows (sealed files and blob bytes) a live copy
has. Reopen if live snapshots are needed: they would need either a per-commit
head marker, which 0062 rejected, or a copy taken under the writer lock while
the server runs, which the engine does not offer.
