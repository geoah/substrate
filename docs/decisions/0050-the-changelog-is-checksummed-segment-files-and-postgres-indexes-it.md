---
status: accepted
date: 2026-09-05
decision-makers: George Antoniadis
---

# 0050. The changelog is checksummed segment files per repository, and Postgres indexes it

## Context and Problem Statement

Before v1 the changelog was a Postgres table whose every row carried a
SHA-256 hash chained to the previous row and an Ed25519 signature over it
([0009](0009-the-chain-hashes-what-postgres-stored.md),
[0010](0010-signing-is-per-repository-ed25519-one-way.md),
[0011](0011-chain-transitions-are-repository-scoped-epochs.md)), with a
first-open backfill and a sanctioned in-place rewrite
([0018](0018-reseal-rewrites-history-values-in-place.md)). That was about
2,100 lines of engine code and 1,300 lines of tests, it needed the host
credential key to append at all, and it produced no backup: a database dump
was still the only copy. V1 wants a copy on disk that a cron can take, and
corruption detection without a key
([the plan](../plans/filesystem-changelog.md)).

## Considered Options

- Keep the table, drop the signature, keep the chain.
- Newline-delimited JSON segment files per repository, one self-checksummed
  line per entry, with the Postgres table kept as the live index.
- Parquet files per repository.
- Make the files the only store and drop the table.

## Decision Outcome

Chosen: newline-delimited JSON segments, one object per line, each line
carrying `sum`, the SHA-256 of its own canonical encoding
(`internal/changelogfile`). Segments are named by their first seq and rotate
past `SUBSTRATE_CHANGELOG_SEGMENT_BYTES`; a finished segment gets a `.sha256`
sidecar and never changes. The `changelog` table stays, as the index the
change feed, the triggers and the causal walk query, and its `hash` column
holds the same checksum. The Postgres transaction is the commit point; the
dataset's single writer appends the committed lines after commit, and the
boot check heals the one-transaction gap a crash can leave.

Nothing chains to the previous entry and nothing is signed. A chain without a
signature defends against nothing a checksum does not, and it is what forced
the epochs, the backfill and the reseal re-chaining. Because the checksum
covers the file's own bytes, the "hash what Postgres stored" apparatus of
0009 is unnecessary: the payload is canonicalized once, from `payload::text`,
and the same bytes go to the file and into the checksum.

Parquet was rejected: its footer is written at close, so a file under active
append is unreadable and a cron copy of it is not a valid file; it needs a
third-party library; and a columnar layout pays nothing for heterogeneous
JSON payloads read sequentially. Dropping the table would move every feed
filter and cursor into a file scan for no gain at personal scale.

### Consequences

- Good, because a backup is a copy of a directory, and `repository rebuild`
  replaying from the files proves the directory alone reproduces the fold.
- Good, because a host without the credential key can still append; the key
  now protects the sealed store only.
- Bad, because an entry is verifiable as undamaged, not as authentic: an
  attacker with disk access can rewrite a line and its `sum` together. That
  is the trade v1 makes on purpose.
- Bad, because the changelog now lives in two places, and the boot check is
  the one thing keeping them equal. A write that reaches the table and not
  the file is caught at the next open, not before.
- Bad, because reseal is gone: a secret that ever reached a payload in the
  clear cannot be scrubbed from history. Secret-typed values are refs into
  the sealed store, so no shipped kind writes one.

### Confirmation

`internal/changelogfile` unit tests hold the encoding, the checksum, the
rotation, the sidecar and the torn-tail rule. `TestChecksumMatchesTheFileEncoding`
(internal/engine) holds the table's `hash` to `changelogfile.Encode`.
`TestBootCatchesUpTheFileFromTheTable`, `TestBootImportsARepositoryDirectory`
and `TestBootRefusesADivergentChangelog` hold the three boot outcomes.

## More Information

Supersedes 0009, 0010, 0011 and 0018.
[0017](0017-the-changelog-is-one-writers-total-order.md) stands: seq order is
still commit order and the one writer is what makes the file append safe.
Revisit if the substrate ever serves a repository whose owner does not trust
the host: that is the day a signature earns its keep again.
