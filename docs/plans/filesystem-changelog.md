# Plan: the changelog as segment files, one directory per repository

Status: in progress, September 2026. This page is the working spec for the
change; the decision records 0050 and 0051 hold the reasoning once the work
lands, and `docs/operations.md` the operator procedure.

## Why

The changelog today is a Postgres table whose every row carries a SHA-256
hash chained to the previous row and an Ed25519 signature over that hash.
The chain, the per-repository signing seed, the activation epochs, the
first-open backfill and the reseal rewrite are about 2,100 lines of engine
code and 1,300 lines of tests, and none of it makes a backup: a dump is still
the only copy, and on the `fs` and `s3` blob backends the dump is half of one.

V1 wants the opposite trade. No signatures and no chain. Checksums that catch
corruption. One directory per repository on disk that holds everything the
repository needs to come back, that an `rsync` cron can copy at any moment,
and that the server reads at boot to decide whether to resume, import or
refuse.

## The data root

```
$SUBSTRATE_DATA_ROOT/
  repositories/
    <repository id>/
      repository.json               # manifest, written atomically (temp + rename)
      changelog/
        000000000000001.ndjson      # named by first seq, 15 digits; the highest is the active segment
        000000000000001.ndjson.sha256
        000000000482113.ndjson
      blobs/
        blob-sha256-<hex>           # immutable, content addressed, as today
      sealed/
        secret-<hex>.json           # one file per sealed row; the ref with ':' replaced by '-'
        auth-<hex>.json
```

The directory is named by `repositories.id`, not the username. The username
is a login label that #341 wants renameable, the fs blob backend already
keys on the id, and the id is the AAD the DEK wrap is bound to, so an import
under the same id opens without re-wrapping. The manifest carries the
username so a person can find the directory with `grep`.

`repository.json`:

```json
{"format": 1,
 "id": "k3j9x2m41pfq",
 "username": "ada",
 "authority": "ada.example.com",
 "createdAt": "2026-09-05T10:00:00.000000Z",
 "changelogDialect": 2,
 "dek": "<base64 of the DEK wrapped under SUBSTRATE_CREDENTIAL_KEY, the repositories.dek bytes>"}
```

The host-wrapped DEK rides in the manifest so a restore onto a host that
holds the same `SUBSTRATE_CREDENTIAL_KEY` needs nothing else. It is
ciphertext under a key that is never in the directory, so the property the
recovery key exists for, that the directory plus the age identity is a
complete recovery, still holds. The manifest holds no head: the head is the
last line of the active segment.

## The changelog segment format

One JSON object per line, newline-delimited, UTF-8, no blank lines. Keys are
sorted and the encoding is `canonicalJSON` (the number and key rules from
the old `chain.go`, kept). The payload is the canonical form of what Postgres
stored (`payload::text`), so the file and the table agree by construction.

```json
{"actor":"api","causedBy":4188,"kind":"samples.substrate.reamde.dev/tasks/task","op":"put","payload":{...},"principal":"k7…","recordId":"kq3v9x2m41pf","seq":4190,"sum":"sha256:5f0c…","ts":"2026-08-04T10:00:00.183742Z"}
```

- `causedBy` is present only when set; `ts` is UTC with microseconds
  (`2006-01-02T15:04:05.000000Z`), the precision `timestamptz` stores.
- `sum` is `sha256:` plus the lowercase hex SHA-256 of the canonical
  encoding of the same object with the `sum` key absent. It detects
  corruption and lets the boot check compare file and table entry by entry.
  It does not chain to the previous entry. There is no signature.
- The `changelog.hash` column holds the same 32 bytes, stamped by the
  writing transaction where `settleChain` used to stamp the chain hash. The
  wire field `Change.Hash` is its hex and keeps its name.
- The active segment rotates when it passes `SUBSTRATE_CHANGELOG_SEGMENT_BYTES`
  (default 256 MiB): the writer fsyncs, writes `<name>.sha256` (the hex
  digest of the finished file, one line), and opens the next file named by
  its first seq. A segment with a sidecar is finished and never changes.
- A reader accepts exactly one kind of damage: a truncated final line in the
  active segment, which it discards. Anything else, a bad `sum`, a seq that
  does not follow the previous one, a finished segment whose sidecar digest
  does not match, is a named error.

Parquet was considered and rejected: its footer is written at close, so an
actively written file is unreadable and a cron copy of it is not a valid
file; it needs a third-party library; and a columnar layout pays nothing for
heterogeneous JSON payloads read sequentially.

## The write path

The Postgres transaction stays the commit point and the `changelog` table
stays the live index (the change feed filters by kind, op, actor, record id
and substring; triggers read `max(seq)` and walk `caused_by`). A row is
final only at commit: `settleFold` merges late effects into the last entry,
then the checksum is stamped. Right after `tx.Commit()` in `inTx`, the
dataset's one file writer appends the finalized lines and fsyncs before
watchers are signaled. The advisory lock that serializes appends already
guarantees a single writer per repository. A crash between commit and append
leaves the file one transaction behind, which the boot check heals.

The sealed store mirrors the same way: every insert, update and delete on
the `sealed` table writes or removes `sealed/<ref>.json` after commit. The
file holds `ref`, `recordKind`, `recordId`, `payload` (base64 ciphertext
under the DEK), `expiresAt` and `updatedAt`.

Blob bytes go through the existing fs backend, rooted at
`<root>/repositories/<id>/blobs/`. `fs` is the default and the only backend
under which the directory is the whole backup; `s3` stays selectable and the
docs say the bucket is then a second artifact; `postgres` is no longer a
runtime choice (`blobs migrate --from postgres` still reads the column, so an
existing store moves out).

## The boot check

At `Open`, for every directory under `repositories/` and every row in
`repositories`:

1. Row and directory, table head equals file head, the last entries'
   checksums match: open.
2. Table head ahead of the file: append the missing entries to the file
   (crash between commit and append). Sealed files are rewritten from the
   table.
3. File head ahead of the table, or directory with no row: import. Create
   the row from the manifest if missing, load `sealed/` into the table,
   insert the missing entries into `changelog` with their checksums and
   fold them through `fold.go`. This is the restore path, and the only one.
4. Same seq in both with different checksums, a line whose `sum` does not
   verify, or a finished segment whose sidecar does not match: refuse to
   open that repository with an error naming the seq or the file. No
   automatic repair.
5. Row with no directory: write the directory out from the tables. This is
   the one-time migration from today's store.

`repository rebuild` replays from the files, not the table, after running
the check above, so the existing fold-snapshot test proves the directory
alone reproduces the fold. `repository verify` becomes the file walk: every
line's `sum`, every sidecar, both heads. `repository inspect` reports both
heads and the segment count.

## What goes away

- `signing.go`, `backfill.go`, `reseal.go`, `verify.go`'s chain walk, the
  chain half of `chain.go` (`entryHash`, `epochHash`, the frames,
  `settleChain`'s signing), `chain_epochs`, and the three `signing_*`
  columns. Migration 0014 drops `sig`, the sig constraints, `chain_epochs`
  and the signing columns; `hash` stays as the checksum.
- The signing public key on the register response, in the CLI
  (`signingpublickey.go`, `verify --expect-public-key`, `--expect-head`),
  in the console (register page, session store, `auth.ts`, `types.ts`) and
  in `wire.golden.json`.
- `repository reseal`, `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES` where
  any mention survives, and the "the dev substrate signs" paragraph.
- `SUBSTRATE_BLOB_FS_ROOT`: the blob root is derived from the data root.
- Decision records 0009, 0010, 0011 and 0018 are superseded by 0050 and
  0051. 0017 (one writer's total order) stands. 0030 and 0031 stand.

`SUBSTRATE_CREDENTIAL_KEY` stays: it wraps the DEK.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `SUBSTRATE_DATA_ROOT` | required | Absolute path. The directory above; one subdirectory per repository. |
| `SUBSTRATE_CHANGELOG_SEGMENT_BYTES` | `268435456` | Rotate the active segment past this many bytes. |
| `SUBSTRATE_BLOB_STORE` | `fs` | `fs` (under the data root) or `s3`. |

The dev tasks use `.dev/data`, which `dev:wipe` removes. Compose mounts a
`substrate-data` volume at `/var/lib/substrate`. Tests pass a `t.TempDir()`.

## Tasks

Phase 1, in parallel:

1. Remove signing, the chain, backfill and reseal (engine, migration 0014,
   CLI, API, console, golden file, tests). Keep `canonicalJSON`. Stamp the
   checksum where the chain hash was stamped.
2. The `internal/changelogfile` package: line encoding, checksum, segment
   writer with rotation and sidecars, reader with torn-tail handling,
   manifest read and write, sealed file read and write. Unit tests for every
   crash point. No database.
3. `SUBSTRATE_DATA_ROOT` in config and `engine.WithDataRoot`; the fs blob
   backend under `<root>/repositories/<id>/blobs`; `fs` the default;
   `postgres` retired as a runtime backend; every `engine.Open` call site
   and test helper passes a root.

Phase 2, after phase 1:

4. Wire the writer into `inTx`, write the manifest at repository creation,
   mirror the sealed store, implement the boot check, replay rebuild from
   the files, rewrite `repository verify` and extend `inspect`. Tests for
   each boot case and one round trip: register, write, upload a blob, store
   a secret, copy the directory, wipe, boot, compare.
5. Decision records 0050 and 0051; `docs/changelog.md`,
   `docs/operations.md`, `docs/auth.md`, `docs/substratectl.md`,
   `SECURITY.md`, `README.md`, `CLAUDE.md`, `docs/terms.md`; `.mise/dev.sh`
   and `compose.yaml`.

Phase 3: review, fix, full CI, smoke against the dev substrate, merge. Then
close #147, #148, #153, #159 and #166 as superseded, retitle #285, #339 and
#341 without the signing-key premise, and point #216 and #137 at the import
path.
