---
status: accepted
date: 2026-11-04
decision-makers: George Antoniadis
---

# 0227. Blob bytes may live in a bucket, and a migration moves them

## Context and Problem Statement

[0075](0075-v1-ships-the-fs-blob-backend-alone.md) removed the `s3` blob
backend because no deployment selected it, its SigV4 signer was written by
hand, and nothing moved bytes between backends. It said to reopen when a
deployment's attachments do not belong on its data root's disk, and named
what a second backend needs: the `Backend`/`Store` pair, a way for a snapshot
to say where the bytes are, and no hand-maintained signer. A deployment now
wants its attachments in an S3-compatible bucket.

## Considered Options

- An `s3` backend on the AWS SDK, beside `fs`, with an offline migration
  command, and snapshots that keep copying the bytes.
- The same, with snapshots that list the bucket's objects instead of copying
  them (0065's removed `blobLocation`).
- Keep `fs` alone, and put the data root on a network filesystem.

## Decision Outcome

Chosen: an `s3` backend on the AWS SDK, selected by `SUBSTRATE_BLOB_STORE`,
with `substratectl repository blobs status|migrate|prune` to move bytes
between the two stores, and snapshots unchanged.

The object key is the fs path relative to the data root
(`<prefix>/repositories/<authority>/blobs/<digest>`), so moving between the
stores is a copy of objects by digest and never a transform. The SDK signs;
this repository maintains no signer.

A snapshot keeps reading every blob out of the live store into its fs-shaped
copy, so `snapshot.json` still records `blobStore: fs` as the copy's layout
and needs no new key and no new format. Listing objects instead would keep a
snapshot small, but a snapshot would then be a pointer into a bucket that
moves on, not a copy, and 0065's restore would need a second branch again.
The cost is disk the size of a repository's attachments wherever a snapshot
is written.

The migration is offline and checked by hash on both sides. `migrate` copies
and never deletes; `prune` deletes an object only after reading and hashing
the other store's copy right before the delete, keeps an object the other
store lacks, stops a repository at the first copy that does not hash, and
refuses the live store. Both take the repository's writer lease and lock, as
a snapshot does. There is no marker file recording that a migration finished:
the only input to a delete is the read of the object itself, so there is no
marker to go stale or be copied along with a restored directory.

A server on `s3` refuses to boot while any repository directory holds blob
bytes, and names each one. Without that a directory restored from a snapshot
would serve stored manifests whose bytes the bucket does not have, and a 404
reads as a deletion.

A network filesystem under the data root stays a supported answer, and the
simpler one when the only problem is disk size. It does not give a deployment
an object store it already runs, nor bytes that outlive the host's mounts.

This supersedes 0075 and amends four accepted records, none reversed:
[0030](0030-a-blob-outside-postgres-settles-after-its-bytes.md) is again
about two backends outside Postgres;
[0031](0031-blob-bytes-outside-postgres-are-stored-plaintext.md)'s at-rest
answer is again disk encryption for `fs` and the bucket's own encryption for
`s3`; [0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)
and [0069](0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md) keep
copying the bytes on both backends, as 0075 left them.

### Consequences

- Good, because a deployment can keep attachments in a bucket and switch
  either way with a resumable, hash-checked command.
- Good, because the snapshot and export formats do not change, and a
  snapshot is a whole copy on either backend.
- Good, because no request signer is maintained here.
- Bad, because on `s3` the data root is not a whole backup: the bucket is a
  second artifact, and copies of the two taken at different moments disagree.
- Bad, because a snapshot of an `s3` repository downloads every attachment.
- Bad, because the AWS SDK is a dependency, and the backend's tests need a
  container (an S3 gateway), so `internal/blobbytes` is back in `test:db`.
- Bad, because the bucket's credentials reach every repository under the
  prefix, and two substrates sharing a prefix delete each other's bytes.

### Confirmation

`TestS3Store` holds the bucket backend to the same conformance suite as `fs`
against a versitygw container, with `TestS3ListPagesPastOneThousandKeys`,
`TestS3ListWalksPastKeysThatAreNotBlobs`, `TestS3AbsentBucketIsAnErrorNotAnAbsence`
and `TestS3OpenIsCanceledWithItsContext` (internal/blobbytes).
`TestBlobsMigrateThenPruneMovesEveryBlob`,
`TestBlobsPruneKeepsWhatTheOtherStoreLacks`,
`TestBlobsMigrateDeepRepairsAndRefusesDamagedSource`,
`TestBootRefusesLocalBlobsBesideAnExternalStore` and
`TestBlobsMigrateRefusesBesideARunningServer` hold the migration and the boot
refusal (internal/engine).

## More Information

[docs/operations.md](../operations.md#blobs-in-a-bucket) is the operator's
page. Reopen if snapshots of `s3` repositories become too large to write,
which is when 0065's listed-objects snapshot would be worth its second
restore branch.
