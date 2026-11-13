# The stored format

A repository's directory is what the [recovery export](operations.md#backups)
hands out, what a snapshot copies, and what a newer server reads when it
upgrades a repository or restores one. Its format is a contract, the most
durable one the project makes: a directory a release wrote is read by every
later 1.x release, and nothing in it changes shape without a conversion that
the server runs once ([conversions](#conversions)). This page is that
contract. [Stability](stability.md#stored-data) says what it promises across
releases, and [decision 0260](decisions/0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)
why.

Three rules hold everywhere below:

- **Every key set is closed.** A line, the manifest and every effect inside a
  line carry exactly the keys this page names. A reader refuses a key it does
  not know, naming it; it never drops one. A new key is a new format number
  or a new dialect.
- **A number names a shape.** The manifest's `format`, the line format and
  the two dialects only go up, and a server refuses a directory that names a
  number above its own.
- **The test vectors are the format written down**
  (`internal/changelogfile/testdata/vectors.json`). A vector never changes once
  it is there.

## The directory

```
repositories/
  rnd-0vrkvsgbs6bnz2sjy4196kccck/  # named by the repository's id
    repository.json           # the manifest
    changelog/
      000000000000001.ndjson  # segments, named by their first seq
      000000000000001.ndjson.sha256
      000000000482113.ndjson  # the highest is the active one
    blobs/
      blob-sha256-<hex>       # a blob's bytes, named by their digest
    sealed/
      secret-<hex>.json       # one file per sealed value
    spool/
      <ordinal>-<fire>.webhook  # a webhook request accepted and not yet in the changelog
```

[Running a substrate](operations.md#the-repository-directory) says how a
server keeps it and what else a data root holds. An empty subdirectory may be
absent. A snapshot adds `snapshot.json`
([decision 0065](decisions/0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)).

### The manifest

`repository.json` is one JSON object, format 2:

| Key | Holds |
| --- | --- |
| `format` | `2`. Any other value but 1, below, is refused. |
| `id` | The repository's id, `rnd-` and 26 characters, and the directory's name ([decision 0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)). |
| `authority` | The repository's name: its authority, the namespace of its own kinds. |
| `createdAt` | When the repository was created, RFC 3339. |
| `changelogDialect` | The [changelog dialect](#dialects) the segments are in. |
| `vocabularyDialect` | The [vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract) of the stored declaration records. |
| `dek` | The repository's data key, wrapped under the host's credential key and bound to the id, base64. |
| `dekKeyId` | 16 hex digits that name the credential key, never the key. |

A release before ids wrote format 1: the same keys without `id`, the
directory named by the authority and the data key bound to it. A server
converts such a directory at its boot, before anything reads it: it takes
the id the repository's row holds, or a new one for a directory being
restored, seals the spooled requests again under it, rewrites the manifest
in format 2 and renames the directory. Nothing else reads format 1.

### Segments

A segment is newline-delimited JSON, one [line](#the-line) per entry, named by
the seq of its first line in 15 digits. Seqs start at 1 and have no gap, and
each segment starts one past where the one before it ends. A finished segment
has a sidecar, `<name>.sha256`, holding the lowercase hex SHA-256 of the
whole file and a newline, and never changes again. The highest segment has no
sidecar and is the one that grows. A transaction never crosses a segment.

### Sealed files, blobs and the spool

- A **sealed file** holds one value of a `secret` property, or the login
  credential's, as `{"ref", "recordKind", "recordId", "payload",
  "expiresAt"?, "updatedAt"}`. `payload` is ciphertext under the
  repository's data key, base64. The file is named by the `ref` with every
  `:` as `-`, and a line names the value by its `ref`, never by its content.
- A **blob** is its bytes, as they were uploaded, named `blob-sha256-` and
  their digest.
- A **spooled webhook request** is a request the door answered `202` and the
  changelog does not hold yet, sealed under the data key and bound to the
  repository's id and the fire
  ([decision 0167](decisions/0167-the-webhook-door-answers-after-a-sealed-spool-write.md)).

## The line

A line is one JSON object:

| Key | Holds |
| --- | --- |
| `seq` | The entry's position, from 1. |
| `ts` | When the writing transaction ran: UTC, microseconds, `2006-01-02T15:04:05.000000Z`. |
| `actor` | Who wrote it ([the actor domain](api.md#actors)). |
| `principal` | The id of the token the write stood behind, or `""`. |
| `op` | `put`, `patch`, `delete`, `merge`, `split`, `gc` or `delivery`. |
| `kind`, `recordId` | The record the entry addresses. |
| `causedBy` | The seq of the entry that caused this one. Present only on an entry a delivery caused. |
| `txn` | The seq of the last entry of the transaction that appended this one: equal to `seq` on the line that ends a transaction, above it on every line before. |
| `payload` | What the write did: the keys a client reads on the wire, and `fold`, below. |
| `sum` | `sha256:` and the lowercase hex SHA-256 of the line's canonical form with `sum` left out. |

**The canonical form** is what the checksum covers and what the file holds:
no whitespace; object keys in bytewise order; strings as Go's
`encoding/json` writes them (`\"` and `\\`; `\b`, `\f`, `\n`, `\r` and `\t`;
every other control character and `<`, `>` and `&` as `\u00XX`; U+2028 and
U+2029 as `\u2028` and `\u2029`; everything else as UTF-8);
`seq`, `causedBy` and `txn` as plain decimals; and every number inside
`payload` in one value-exact spelling, so `1.5`, `1.50` and `15e-1` are all
`1.5E0` and every zero is `0`. The spelling is computed on the digits and
never through a float. One vector, the first in the file:

```
{"actor":"api","kind":"ada.example.com/tasks/task","op":"put","payload":{"created":true,"fold":[{"delta":{"created":true,"kindVersion":3E0,"set":{"dueAt":"2026-08-06T09:00:00Z","estimate":1.5E0,"name":"Ship the fold"},"title":"Ship the fold"},"id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"record","ref":"ada.example.com/tasks/task","version":1E0},{"actor":"api","id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"manager","principal":"01jbx9k0token000000000000a","property":"name","ref":"ada.example.com/tasks/task","tier":"owner"}],"properties":["name","dueAt"]},"principal":"01jbx9k0token000000000000a","recordId":"01jbx9m2qz7k4c5v8n3w6t1hrd","seq":4190,"sum":"sha256:308ac73610024745a83e81276ac52f61d3b8f406156a231984a79199f8731e31","ts":"2026-08-04T10:00:00.183742Z","txn":4190}
```

[The changelog](changelog.md#the-checksum-and-the-segment-files) says what a
reader does with a damaged line, and how a transaction is cut whole.

### The payload

The keys beside `fold` are what [the change feed](changelog.md) serves:
`created`, `restored`, `properties`, `states`, `finalizers`, `winner`,
`loser`, `moved`, `renamed`, `remapped` and a `gc` entry's `reason`. `fold`
is the list of **effects** the write applied, in order: replaying them is what
rebuilds the records from the history. No response carries `fold`.

### The effects

Every effect names its `kind`, and the record it lands on as `ref` (the
record's kind) and `id`. A trigger's bookkeeping lands on the trigger's
record. The keys are closed: an effect field a binary does not know refuses
the entry, naming the field.

| Effect | What replaying it does | Its other keys |
| --- | --- | --- |
| `record` | Applies a record's change, as values | `version`, the version the record reached; `delta`: `created`, `restored`, `force`, `set` (properties and their values), `del` (properties removed), `title`, `body`, `at`, `endsAt`, `dueAt` (`""` clears one), `states`, `labels`, `finalizers` (each whole, `{}` or `[]` clears), `kindVersion` |
| `tombstone` | Deletes the record, keeping it as a tombstone | `version`, `finalizer` (the hold a merge adds) |
| `purge` | Removes the record and everything on it | |
| `annotation` | Sets one annotation, or clears it when `value` is absent | `key`, `value` |
| `manager` | Sets who manages a property, or releases it when `actor` is empty | `property`, `actor`, `tier`, `principal`, `updatedAt` |
| `former` | Records an id the record was merged from | `formerId` |
| `resync` | States again, whole, every side row of the records a merge or split moved | `scope` (`kind`, `id` each); `rows`: `annotations` (`kind`, `id`, `key`, `value`, `updatedAt`), `managers` (`kind`, `id`, `property`, `actor`, `tier`, `principal`, `updatedAt`), `formerIds` (`kind`, `formerId`, `recordId`, `createdAt`) |
| `cursor` | Sets a record trigger's acknowledged position | `seq` |
| `schedule` | Sets a schedule trigger's last fired occurrence | `at` |
| `park` | Writes a failed delivery, whole | `failure`: `id`, `seq`, `fireId`, `recordId`, `attempts`, `lastError`, `parkedAt`, `payload` |
| `unpark` | Retires a parked delivery | `failure`: `id` |
| `page` | Writes a paged drain's resume row | `page`: `chain`, `cursor` or `cursorSha256` with `cursorBytes`, `version`, `pages`, `effects`, `bytes`, `startedAt`, `kind`, `identity` |
| `unpage` | Drops a paged drain's resume row | `page`: `chain` |
| `forget` | Drops every delivery row a deleted trigger owned | |

Times inside an effect are RFC 3339. A number reads from any JSON spelling of
it, because the file holds the canonical one and Postgres prints its own.

## Dialects

A repository's **changelog dialect** names the shapes its lines are in: the
ops, the effects and their keys. It is stamped in the database and in the
manifest, and it is 1 today. A change to what a line may hold raises it, and
a change to a shape history already holds also ships a
[conversion step](#conversions) to the new number, so a server reads one
dialect, its own. The line format (`LineFormat`) and the manifest's `format`
are 1, and a change to the frame of a line or of the manifest would raise
them. [The changelog](changelog.md#the-dialect-a-changelog-is-written-in)
says how the stamp is written and what refusing a newer one looks like.

## Conversions

A change to a stored shape lands as one **conversion step**, numbered like a
migration: step 2 converts a history in dialect 1 to dialect 2, and each
next step the one after. The chain is in `internal/engine/conversion.go`, and
it has no step yet. At the boot check, before a repository is folded or
served, the server runs the steps it lacks, in order and in one pass over its
history:

1. Every line is passed through each step and written, converted, into
   `changelog.converting-<dialect>` beside the live changelog. A step is
   handed one line and rewrites it in place; it may not move the line's
   `seq` or `txn`, and the server recomputes the `sum`.
2. One transaction rewrites the lines that changed in the database, raises
   the stamp and marks the repository's records for rebuilding.
3. The converted directory replaces the live one.
4. The records are rebuilt from the converted history, and the manifest is
   written with the new dialect.

A directory restored from an older release is converted the same way,
between its entries going in and their being folded. A server stopped at
any step finishes the conversion at its next boot: a converted directory the
database has not taken is discarded and the conversion runs again, one it has
taken is swapped in, and the records are rebuilt before anything is served.
A copy of the directory taken at any of those points and restored into
another database is one history too: no table there took the conversion, so
the copy keeps its unconverted lines, moved back to `changelog/` if the swap
had moved them to `changelog.previous`, and converts on import.
A step leaves a line already in its dialect unchanged, so running it twice
changes nothing. A process that does not run the boot check, such as an
operator's read-only one, refuses to open a history below its dialect.

A conversion is not a [repository migration](operations.md#upgrading-the-binary).
A migration changes what records hold, within the current shapes, by
appending ordinary writes, and runs again over every import. A conversion
changes the shape history is written in, rewrites lines in place and appends
nothing, and the repository's own stamp records it. A change that needs both
lands both, and the conversion runs first. Converting closes the way back to
an older server, as any raised stamp does
([going back](stability.md#going-back)).

The move to repository ids is neither: no line changed, because a line names
kinds and actors, and those stay the name's. What changed is the key the
rows and the directory are found by, so the boot gives a repository its id
before anything looks for it, and before the chain runs
([the directory](#the-manifest) above, and
[running a substrate](operations.md#what-happens-at-boot)).

## How old a directory may be

A 1.x server reads a directory any earlier 1.x release wrote, converting it
once where a shape changed, and 2.0 reads what the last 1.x release wrote. A
conversion step is carried for one major version: once the steps are dropped,
in a release after 1.0, a directory from before 1.0 is read by opening it
under 1.0 first. Below 1.0, what is read is what the
[kept directories](#kept-directories) prove: the oldest directory kept is the
oldest a release promises to read.

## Kept directories

Each release keeps one directory its own server wrote, under
`internal/testenv/testdata/kept/<tag>/`: the repository directory, every
record and tombstone the release served (`records.json`), each trigger's
state (`triggers.json`), and what the directory opens under
(`fixture.json`). It is made by `mise run release:keep <tag>`, which builds
the tag rather than the tree, runs it in Linux against a throwaway Postgres
and drives it through every op and every effect above
([releasing](releasing.md#cutting-a-release)). Nothing in it is anybody's:
`example.com` names, a password, a token and a credential key made for it.
A kept directory is never edited (`mise run frozen:check`).

`TestKeptDirectories` imports each one under the current code, as a restore
does, and compares every record with what the release served. The current
code may differ in two ways. A conversion step rewrites lines in place and
appends nothing, so what each step does to the records a release served is
written beside the test, one entry per step (`keptConversions`), from what
the step rewrites and never from what the current code serves; a step that
changes nothing a release served says that too, and the test refuses a
chain with a step it does not name. The release's records go through those
entries first. And the boot writes after the release's head: the seeded
packages brought to this release's versions. A record nothing after the
head wrote must be identical to the release's, converted, and one the boot
wrote must be that record with those writes applied, none of them taking a
value from the hand that set it. Any other difference fails, naming the
record. A trigger's acknowledged position, last fire and parked deliveries
are compared the same way. The records name what the comparison cannot
see, a secret served as `<redacted>` and a blob by its digest, so the test
also signs in with the release's password, reads the blob's bytes back, and
opens every sealed file under the data key.
