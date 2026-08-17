---
status: accepted
date: 2026-08-17
decision-makers: geoah
---

# 0031. Blob bytes outside Postgres are stored plaintext

## Context and Problem Statement

Blob bytes in the `blobs` column are plaintext, protected by the database. The
`fs` and `s3` backends ([#97](https://github.com/geoah/substrate/issues/97))
put them on a volume or in a bucket, where that protection does not follow, and
the issue asks for the stance to be decided here rather than inherited. The
repository already has a data-encryption key (`internal/engine/dek.go`) that
seals secret-typed properties, so sealing the bytes under it is available.

## Considered Options

- Store the bytes as they arrived.
- Seal every object under the repository DEK.
- Seal on `s3`, store plaintext on `fs`.

## Decision Outcome

Chosen: store the bytes as they arrived, on every backend, and say so in
`docs/operations.md` where an operator decides what the volume or the bucket
is.

A blob is content-addressed: the digest is the id, the manifest key, the
dedup rule and the s3 payload hash the endpoint checks. Sealing makes the
stored object ciphertext while the address stays a hash of the plaintext, so
the digest stops verifying what the backend holds and nothing but a re-read
and a re-hash can tell a corrupted object from a good one. AES-GCM is also
nondeterministic, so the same bytes stored twice are two different objects: a
re-put stops being a no-op, and healing a missing object stops being idempotent.

The trust boundary barely moves either way. The bytes' own manifest, the whole
changelog and every record folded from it stay plaintext in Postgres, so an
attacker who can read the database already has the interesting half; sealed
material is the exception the DEK exists for, and blobs are not it. And a
keyless host (`SUBSTRATE_CREDENTIAL_KEY` unset, which the insecure switch still
permits) could write blobs it could never read back.

The mixed option is worse than either: two on-disk formats, one of which is
whichever the operator last configured, and no way to tell them apart by
looking.

Where at-rest encryption is wanted, it belongs under the store: a LUKS volume
for `fs`, the bucket's own server-side encryption for `s3`. That is one
operator setting, it covers the whole store rather than the objects a
particular release remembered to seal, and it costs the substrate nothing.

### Consequences

- Good, because the digest keeps verifying the object, dedup stays exact, and
  a re-put stays a no-op.
- Good, because a keyless host and a host whose credential key is rotated both
  keep reading their blobs.
- Bad, because anything that can read the fs root or the bucket can read every
  repository's attachments, in the clear, with no key involved. The store is as
  trusted as the database, and the documentation says so.
- Bad, because the substrate offers nothing against a leaked bucket
  credential; that is entirely the operator's bucket policy.

### Confirmation

Held by review, plus `docs/operations.md`, which states the property as part of
the backup and isolation instructions for both external backends.

## More Information

Reopen this if blob bytes stop being content-addressed by their plaintext
digest, or if a substrate is ever hosted for someone who does not control the
bucket. Encrypting later needs a fresh object namespace or a per-object key id
in the key, since the digest cannot name a ciphertext, and a migration between
the two: that is the reversal cost, and it is why the choice is recorded rather
than left to whoever writes the next backend.
