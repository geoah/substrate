---
status: accepted
date: 2026-09-10
decision-makers: George Antoniadis
---

# 0075. v1 ships the `fs` blob backend alone, and `SUBSTRATE_BLOB_STORE` is gone

## Context and Problem Statement

The blob byte store had two backends, `fs` and `s3`, selected by
`SUBSTRATE_BLOB_STORE` ([#494](https://github.com/geoah/substrate/issues/494)).
No compose file, Dockerfile, mise task or deployment selected `s3`, and
nothing has been installed yet, so the backend had no user. It cost 1,039
lines: 834 in `internal/blobbytes`, 172 of them an AWS SigV4 signer written
by hand, 70 in `internal/config` for eight environment variables, and a
135-line MinIO harness in the engine's export suite. It also cost two MinIO
containers, one per test binary, which is why `internal/blobbytes` needed
Docker and a database at all. Every external-store decision, every operator
procedure and the snapshot file carried a second branch for it.

## Considered Options

- Remove the `s3` backend and ship `fs` alone, removing
  `SUBSTRATE_BLOB_STORE` with it.
- Keep the backend and drop only the MinIO suites, leaving it untested.
- Keep it and replace the hand-written signer with the AWS SDK.

## Decision Outcome

Chosen: remove the `s3` backend and ship `fs` alone. `SUBSTRATE_BLOB_STORE`
is removed rather than narrowed to one accepted value: a variable with one
legal setting is a question the operator should not be asked, and a host that
still sets it boots and ignores it, because the service reads the variables
it declares.

An untested SigV4 signer is worse than no backend: the one proof it had was
the MinIO suite, and MinIO is not AWS, so removing the suite would have left
a credential-signing path with nothing behind it. Adopting the AWS SDK would
have bought a dependency and a maintained surface for a feature no
deployment asked for. Nothing moves bytes between backends anyway, so the
choice was never reversible in place: an operator who wants a bucket wants a
migration path, which does not exist and was not going to be written for a
second backend nobody selected.

With one backend the seams collapse: `blobbytes.Locator` and the
`Backend.Name()` / `Store.Backend()` pair are gone, `snapshot.json` loses its
`blobLocation` key, and `config.Blobs` is gone with the eight variables it
loaded. `Backend` and `Store` both stay, because they are two roles (the
process's store, and one repository's) rather than two backends.

This amends four accepted records without reversing any of them:

- **[0030](0030-a-blob-outside-postgres-settles-after-its-bytes.md)**: the
  `pending` mint, the bytes, then the settle to `stored` is still exactly
  what an upload does. Only the plural is wrong: `fs` is the one backend
  outside Postgres, so the "listing per pass on s3" cost is now a directory
  read. Its confirmation now lives at
  `internal/engine/blobs_store_db_test.go`, renamed with the engine's
  `putBlobBytes`, because "external" had nothing left to be external to.
- **[0031](0031-blob-bytes-outside-postgres-are-stored-plaintext.md)**:
  bytes are still stored as they arrived. "On every backend" is now one
  backend, and the at-rest answer is disk encryption under the data root; the
  bucket's server-side encryption is no longer an option because there is no
  bucket.
- **[0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)**:
  a snapshot still copies the directory, verifies it and records its head.
  The `s3` branch it decided, listing objects at `blobLocation` instead of
  copying them, has no store to run against and is removed from the code and
  from the procedure; every snapshot now carries its blob bytes.
- **[0069](0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md)**: the
  export still streams the bytes into the archive and records `blobStore:
  fs`, which is now the only thing it could record. Its
  `TestExportStreamsBlobsOutOfS3` confirmation is removed with the backend.

0051's "`s3` stays selectable" needed no amendment here: that record is
already superseded by
[0052](0052-the-authority-is-the-repository-id.md).

### Consequences

- Good, because the repository directory is the whole backup unconditionally,
  so 0051's backup unit, 0065's snapshot and 0069's export each have one
  procedure instead of two.
- Good, because 1,039 lines and two MinIO containers leave the tree, and
  `internal/blobbytes` needs neither Docker nor a database, so it leaves
  `test:db` for the short suite.
- Good, because no hand-written request signer ships.
- Bad, because a deployment whose disk cannot hold its attachments has no
  answer in v1 beyond a bigger disk or a network filesystem under the data
  root.
- Bad, because a second backend later has to reintroduce the seams this
  removes, and a `snapshot.json` written by this release does not carry the
  key such a backend would need. The snapshot format's key set is closed, so
  that is a format change, not an added key.
- Bad, because `snapshot.json` loses `blobLocation` while `SnapshotFormat`
  stays 1, which the format's own rule would otherwise refuse. It is sound
  here only because nothing was installed, so no snapshot written before this
  release exists to be read. After v1 the same edit takes the next format
  number.

### Confirmation

No test starts an object-storage container: the only testcontainer image
left in the tree is pgvector, and `internal/blobbytes` needs neither Docker
nor a database. Its conformance suite still holds the whole `Store` contract
against the one backend.
`TestExportOverTheAPIRestoresIntoAnEmptyDatabase` and
`TestSnapshotRecordsThePointAndRestoresIntoAnEmptyDatabase`
(internal/engine), and `TestReleaseAcceptanceDrill` (internal/testenv), each
carry a blob's bytes through a copy and hash it on the far side.

## More Information

[docs/operations.md](../operations.md#the-blob-store) is the operator's page:
one store, nothing to configure. Reopen if a deployment appears whose
attachments do not fit its disk. A new backend needs, at minimum, the
`Backend`/`Store` pair it already has, a way for a snapshot to say where the
bytes are (0065's removed `blobLocation`, under a new snapshot format), and a
signer nobody in this repository maintains by hand.
