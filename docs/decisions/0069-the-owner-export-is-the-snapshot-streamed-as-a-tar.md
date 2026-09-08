---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0069. The owner's export is the snapshot, streamed as a tar under the bearer token

## Context and Problem Statement

A repository's owner holds a recovery key
([0054](0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md))
but could not obtain the bytes it opens: the only copy of a repository was an
operator's `rsync` of the data root or an operator's `repository snapshot`
([0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)),
both taken on the server's box
([#380](https://github.com/geoah/substrate/issues/380)). The export has to
be a format that ships forever, taken from a running server, and complete
enough that `repository verify` passes on a restore of it.

## Considered Options

- A tar of the repository directory in the data root's layout, with 0065's
  `snapshot.json` as its last entry, at `GET /api/v1/export`.
- A new archive schema of its own (records as documents, attachments beside
  them).
- Hold the repository's writer mutex for the whole download, so nothing
  commits while the archive streams.
- Take the point under the writer mutex and stream the files afterwards,
  cutting the active segment at the length it had.
- Refuse the export under the `s3` blob store, or stream the objects.
- Require the password and TOTP code in the request, as `recovery enroll`
  does, or accept the bearer token alone.
- Drop the host-wrapped DEK from the exported `repository.json`, or keep it.

## Decision Outcome

Chosen: the tar of the directory in the snapshot format, the point taken
under the writer mutex and the files streamed as of it, the blob bytes
streamed whatever store the server runs, the bearer token as the whole
credential, and the manifest exported as the directory holds it, wrapped DEK
included.

The tar is the directory because the directory is already the backup unit
and every reader of it exists: `tar -x` under a data root is the restore
0051 documented, `repository rewrap` (0054) opens it on another host, and
`repository verify` reads the `snapshot.json` 0065 defined. A second archive
schema would be a second format to read forever, with no reader today.

The point is pinned, not held. Every commit holds the dataset's writer mutex
from its first staged byte to the newline that makes its lines history
([0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)),
so under that mutex the writer's head, the active segment's committed length,
the sealed files and the `records` table describe one state, and reading them
takes milliseconds. Everything streamed afterwards is as of that state: a
finished segment never changes, the active segment is cut at the length it
had (a later rotation adds a sidecar and stops the file growing, which changes
none of its first bytes), the sealed records were read under the mutex, and a
blob's bytes are content-addressed. Holding the mutex for the download would
stop every write for as long as the slowest client took, and refusing when a
segment rolled would make a busy repository unexportable.

The blob bytes ride in the archive under `s3` too, read through the same
`Store.Open` the fs path uses. Each blob is streamed from the store into the
tar under a header sized by its manifest, hashed on the way and held to its
digest once through, so an export holds one copy buffer per blob and never a
blob; a store that answers other bytes fails the export. 0065 lists the
objects instead of copying them because an operator has the bucket; an owner
does not, and an export that named objects they cannot fetch would not be a
recovery export. The archive's `snapshot.json` therefore records
`blobStore: fs` and no location: it describes the copy, which is laid out
for fs. One export streams per repository at a time: a second `Export` while
one streams is refused (`409 conflict`), because an export holds a blob open
and a tar half written for as long as the slowest client takes.

The pin takes its pool connection before the writer mutex. Every write holds
a connection from its start to the commit that runs under the mutex, and the
writers behind it wait on the changelog advisory lock inside their own
transactions, connections held; an export that took the mutex first and then
asked the pool would wait for a connection no writer can release until the
mutex is free.

The bearer token is enough. A token has full access to its repository: it
reads every record and every blob the archive carries, through `GET` and
the change feed, and the sealed files in the archive are ciphertext under
the DEK, which the archive holds only wrapped. `recovery enroll` demands both
factors because it claims a one-time slot and hands out a key that opens the
sealed store; the export hands out nothing a token could not already read.

The host-wrapped DEK stays in `repository.json`, as 0051 decided for the
directory and for the same reason: it is ciphertext under a key that is never
in the archive, so it discloses nothing, and it is what lets a restore onto a
host holding the same `SUBSTRATE_CREDENTIAL_KEY` boot with nothing else.
Dropping it would also break the archive on its own host: the import ties
`sealedDekOnly` to a DEK being present, and a manifest with neither makes the
first open mint a fresh DEK under which no sealed file opens. The recovery
key's copy in the `recoverykey` record is what opens the archive elsewhere.

The archive is written in the order a restore needs and `snapshot.json` is
its last entry, so an archive that ends early lacks the file that vouches
for it. A failure after the first byte aborts the HTTP response rather than
finishing a tar the server could not complete, and `substratectl export`
reads the archive back on the way to disk and removes one that ended before
`snapshot.json`.

### Consequences

- Good, because an owner recovers their repository with a token and their
  recovery key, on any host, with the operator's own tools.
- Good, because one format: the tar is the directory, and every existing
  reader of the directory reads it.
- Good, because writes go on during a download and the archive still holds
  one committed state, which `repository verify` proves on the restore.
- Bad, because the export streams every attachment through one HTTP
  response; a repository's export is as large as its attachments, and a
  client that goes away restarts from the beginning.
- Bad, because a blob whose manifest was `stored` at the point can be
  tombstoned and swept while the download runs; the export then fails and is
  taken again.
- Bad, because a leaked token now yields the whole repository in one
  request. It already yielded it in many.
- Bad, because one export per repository means a second client waits or
  retries, and a client that stalls holds the slot until its request ends.
- Bad, because the archive is not verified before it is taken, unlike
  `repository snapshot`: a live server cannot open every sealed file and hash
  every blob under the writer mutex, so a restore runs `repository verify`
  to learn what a snapshot knew before it copied.

### Confirmation

`TestExportOverTheAPIRestoresIntoAnEmptyDatabase` (internal/engine)
downloads the export from a running service, checks the archive's entries,
extracts it under a fresh data root, boots over it with an empty database
and asserts the fold, the blob, the secret, the token, `repository verify`
at the recorded point and a write past it.
`TestExportPinsAPointWhileWritesContinue` pins a point, commits writes
before and during `WriteTo` (the stream blocked at its first byte), and
asserts the archive's changelog ends at the pinned head and verifies there.
`TestExportPinsWithEveryPoolConnectionHeld` fills the repository pool with
writes waiting behind the pinned export and asserts it completes.
`TestExportStreamsBlobsOutOfS3` exports a repository whose bytes live in a
MinIO bucket and restores it onto an fs host. `TestExportStreamsTheArchiveUnderTheBearerToken`,
`TestExportRefusesWithAStatusBeforeTheFirstByte` and
`TestExportAbortsTheResponseWhenTheStreamFailsMidway` (internal/api) hold
the route; `TestExportRefusesAnArchiveThatEndedEarly`
(cmd/substratectl/commands) holds the client's refusal.

## More Information

[docs/operations.md](../operations.md#backups) has the owner's procedure
beside the operator's. Reopen if the export must verify before it streams
(it would need the operator snapshot's stopped-server checks under a live
writer), or if repositories outgrow one HTTP response (a resumable,
per-file download).
