---
status: accepted
date: 2026-09-05
decision-makers: George Antoniadis
---

# 0051. A repository directory under the data root is the backup unit

## Context and Problem Statement

A backup was "the changelog plus blobs plus sealed, as one unit", which under
the default `postgres` blob backend meant a database dump and under `fs` or
`s3` meant a dump plus a second artifact nobody's procedure named
([#216](https://github.com/geoah/substrate/issues/216)). With the changelog
moving to files ([0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md))
the question is what else must sit beside it so that one directory, copied
by a cron at any moment, brings a repository back.

## Considered Options

- One directory per repository under `SUBSTRATE_DATA_ROOT`, named by the
  repository id, holding the manifest, the changelog segments, the blob bytes
  and the sealed store; the host-wrapped DEK in the manifest.
- The same, named by the username.
- The same, without the host-wrapped DEK (restore needs the age identity).
- Keep the database dump as the backup and document the blob store beside it.

## Decision Outcome

Chosen: `<root>/repositories/<repository id>/` with `repository.json`,
`changelog/`, `blobs/` and `sealed/`. The manifest carries the id, the
username, the authority, the creation time, the changelog dialect and the DEK
wrapped under `SUBSTRATE_CREDENTIAL_KEY`, the same bytes as
`repositories.dek`. The fs blob backend lays its objects under the directory
and is the default; the `postgres` bytes backend is no longer a runtime
choice; `s3` stays selectable, and then the bucket is a second artifact. The
sealed store mirrors to one file per ref after each commit.

The directory is named by the id because the username is a login label that
[#341](https://github.com/geoah/substrate/issues/341) wants renameable, the
fs blob backend already keyed on the id, and the id is the additional data
the DEK wrap is bound to ([0023](0023-a-sealed-payload-is-bound-to-its-address.md)),
so an import under the same id opens without re-wrapping. The username is in
the manifest for a person with `grep`.

The host-wrapped DEK rides in the manifest so that a restore onto a host
holding the same credential key needs nothing else. It is ciphertext under a
key that is never in the directory, so the property the recovery key exists
for, that the directory plus the age identity is a complete recovery, still
holds. This narrows the earlier rule that nothing host-keyed sits in the
user plane: one wrap does, and it opens nothing without the host key.

At boot the server compares every directory with every `repositories` row.
Equal heads open; a table ahead of its file catches the file up; a file ahead
of its table, or a directory with no row, imports; a seq present in both with
different checksums, or a finished segment whose sidecar does not match,
refuses that repository and names the seq or the file. Import is the only
restore path, and `repository rebuild` replays from the files.

### Consequences

- Good, because the backup procedure is one sentence: copy the directory, and
  the credential key separately.
- Good, because finished segments and blobs never change and the manifest,
  sidecars and sealed files are replaced atomically, so a copy taken
  mid-write is either consistent or short by one torn line the importer
  discards.
- Bad, because the root is as private as its mode: anything that can read it
  can read every repository's blobs and every changelog in the clear
  ([0031](0031-blob-bytes-outside-postgres-are-stored-plaintext.md)). Encrypt
  the volume or the copy.
- Bad, because runtime state (trigger cursors, paged cursors, embeddings,
  OAuth flows in flight) is not in the directory and does not come back;
  triggers resume from the head after an import.
- Bad, because a restore onto a host with a different credential key needs
  the age identity and a tool that does not exist yet
  ([#137](https://github.com/geoah/substrate/issues/137)).

### Confirmation

`TestRoundTripDirectoryRestoresARepository` (internal/engine) registers,
writes records, a blob and a secret, copies the directory into a fresh
database and asserts the fold snapshot, the blob bytes and the secret read
back byte for byte. `TestBootImportsARepositoryDirectory` and
`TestBootRefusesADivergentChangelog` hold the import and the refusal. `Open`
refuses without a data root (`TestOpenRequiresADataRoot`).

## More Information

[The plan](../plans/filesystem-changelog.md) has the layout and the boot
cases in full. Revisit when repositories are meant to move between hosts
with different credential keys; the age-identity restore is the missing tool.
