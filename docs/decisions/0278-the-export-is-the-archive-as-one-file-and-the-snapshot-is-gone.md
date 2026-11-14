---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0278. The export is the archive as one file, and the snapshot is gone

## Context and Problem Statement

The owner's export was the repository's directory streamed as a tar
([0069](0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md)), in the
format of the operator's stopped-server snapshot
([0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)),
pinned under the writer mutex of the synchronous copy. With Postgres alone
confirming a write
([0262](0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md))
there is no directory to stream and no mutex to pin under, and the archive
([0277](0277-the-archive-is-pieces-blobs-and-sealed-values-and-its-manifest-is-the-mark.md))
is already the copy a restore reads.

## Considered Options

- Keep a snapshot command and an export format of their own.
- The export is the archive as one file; the snapshot goes.

## Decision Outcome

Chosen: the export is the archive as one file, and the stopped-server
snapshot goes. `GET /api/v1/export` streams a tar of one archive of the
repository's own, under an id minted at the export
([0279](0279-the-archive-holds-against-a-written-contract.md)), in the
archive's layout, written from one repeatable-read snapshot of the table:
the blobs, the sealed values, the spooled webhook requests, the history in
pieces, and the manifest last, naming the head. A stream cut short
has no manifest, and nothing restores it. A restore extracts the tar into the
archive a stopped server reads and boots, which is the archive's own restore.
The operator's copy of a repository is its archive, which a cleanly stopped
server leaves at its head; a second format for it was rejected because two
formats are two readers.

### Consequences

- Good, because one format is written, read, documented and kept, whatever
  made the copy.
- Good, because the export no longer waits on a lock any write holds.
- Bad, because the operator loses a command that copied one repository with
  the server stopped; the copy is now the archive's directory, or the export.
- Bad, because an export holds a pool connection only for its pin and holds
  every blob against collection while it streams.

### Confirmation

`internal/engine/export_db_test.go` and `internal/api/export_db_test.go`: the
export restores into an empty database record for record, pins its point
while writes go on, and holds the blobs it listed;
`cmd/substratectl/commands/export_test.go` refuses an export with no manifest
at its end.

## More Information

Replaces [0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)
and [0069](0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md).
