# The stored format

A repository's **archive** is the copy the server writes of it after its
writes commit ([the archive](operations.md#the-archive)), what the
[recovery export](operations.md#backups) hands out, and what a server reads
when it restores a repository. Its format is a contract, the most durable one
the project makes: an archive a release wrote is read by every later 1.x
release, and nothing in it changes shape without a conversion that the
server runs once ([conversions](#conversions)). This page is that contract.
[Stability](stability.md#stored-data) says what it promises across releases,
and decisions
[0260](decisions/0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md),
[0262](decisions/0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)
and
[0277](decisions/0277-the-archive-is-pieces-blobs-and-sealed-values-and-its-manifest-is-the-mark.md)
why.

Three rules hold everywhere below:

- **Every key set is closed.** A line, the manifest and every effect inside a
  line carry exactly the keys this page names. A reader refuses a key it does
  not know, naming it; it never drops one. A new key is a new format number
  or a new dialect.
- **A number names a shape.** The manifest's `format`, the line format and
  the two dialects only go up, and a server refuses an archive that names a
  number above its own.
- **The test vectors are the format written down**
  (`internal/changelogfile/testdata/vectors.json`). A vector never changes once
  it is there.

## The archive

An archive is objects under keys, because that is what every store holds: a
directory or a bucket, with four operations, put, get, list and delete.
Nothing in it is appended to, renamed or locked, and **no name ever holds two
contents**: every object's name carries the id of the pass that put it, and a
manifest's carries its epoch and the hash of its bytes. So a put a store
applies late writes bytes no other put writes under that name, and a delete
applied late removes nothing a later pass put. [The archive's
contract](operations.md#the-archives-contract) is what it holds against.

A repository's copy is one or more **archives**, each one life of it under an
id of its own: `a`, the time it was made in nanoseconds as 16 hex digits, and
8 random hex digits, so ids sort by when they were made. A repository is
created, imported or restored, or an operator resolves a disagreement, and a
new archive starts; an archive is never continued by anything but the
database life that started it.

```
repositories/
  rnd-0vrkvsgbs6bnz2sjy4196kccck/            # named by the repository's id
    manifests/                               # every archive's manifests and marks, apart from the objects
      a18e7b6f2c41d9a003b2c7e91/             # one archive
        00000000000000000007-<hash>.json     # a manifest: its epoch, and the SHA-256 of its bytes
      a18e7c0d55e01b7c2f9a01d44/
        deleted                              # a deletion mark, under an id of its own
    a18e7b6f2c41d9a003b2c7e91/               # one archive's objects
      changelog/
        1/                                   # the changelog dialect the lines are in
          000000000000001-000000000004190-<pass>-<sum>.ndjson  # a piece: seqs 1 to 4190
      blobs/
        blob-sha256-<hex>.<pass>             # a blob's bytes, named by their digest and the pass
      sealed/
        secret-<hex>.<sum>.<pass>.json       # one version of a sealed value
      spool/
        <ordinal>-<fire>.webhook.<pass>      # a webhook request answered and not yet in the changelog
```

`<pass>` is the 16 hex digits of the pass that put the object, and `<sum>`
the lowercase hex SHA-256 of its bytes (a piece's name carries the first 16
of them). The manifests and the marks sit under one prefix so a pass lists
them without listing every blob. [Running a
substrate](operations.md#the-archive) says where the server keeps it and how
often it writes it. An empty prefix may be absent.

### The manifest

A manifest is one JSON object, format 3, at
`manifests/<archive>/<epoch>-<hash>.json`: the epoch in 20 digits and the
lowercase hex SHA-256 of the manifest's own bytes. It is a pass's **one
commit point**: it describes the repository at one history position, as one
database snapshot at that position saw it, and names every piece, sealed
value and spooled request it selects. A reader takes the newest archive's
manifest of the highest epoch, and no other; two of the highest epoch are
reported and never chosen between. A manifest that a newer one replaced is
not a recovery point.

| Key | Holds |
| --- | --- |
| `format` | `3`. Any other value is refused here; 2 and 1 are [a 0.x directory's](#a-0x-directory). |
| `id` | The repository's id, `rnd-` and 26 characters, and the name of the prefix it sits under ([decision 0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)). |
| `archive` | The id of the archive the manifest is of. |
| `pass` | The id of the pass that published it. |
| `authority` | The repository's name: its authority, the namespace of its own kinds. |
| `createdAt` | When the repository was created, RFC 3339. |
| `changelogDialect` | The [changelog dialect](#dialects) the pieces are in, and the directory under `changelog/` they sit in. |
| `vocabularyDialect` | The [vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract) of the stored declaration records. |
| `dek` | The repository's data key, wrapped under the host's credential key and bound to the id, base64. |
| `dekKeyId` | 16 hex digits that name the credential key, never the key. |
| `head` | The **mark**: every line from seq 1 to it is in the pieces, and every blob and sealed value the records at it hold is in the archive but those `missingBlobs` and `pendingBlobs` name. `0` is an empty history. |
| `headSum` | `sha256:` and the lowercase hex of the line at `head`, the checksum the table stamped for it. Absent when `head` is 0. |
| `pieces` | The history from seq 1 to `head`, in order: each piece's `name` under the archive, the `sum` of its bytes (`sha256:` and hex) and their `size`. Absent when `head` is 0. |
| `sealed` | The version of each sealed value: its ref to the name of its object under the archive. Absent when there is none. |
| `spool` | The names of the objects holding the spooled requests. Absent when there is none. |
| `missingBlobs` | The digests of the blobs the records at `head` hold whose bytes the server had lost when the pass ran. Absent when none is. |
| `pendingBlobs` | The digests of the blobs the records at `head` hold that the pass had not copied yet: a pass copies a bounded amount, 1 GiB, and publishes. Absent when none is. |
| `epoch` | The manifest's place among its archive's manifests, the number its name carries; 1 or more. |

The archive is complete at its `head` only when both `missingBlobs` and
`pendingBlobs` are absent. A blob is selected by its digest: any object under
`blobs/<digest>.` holds its bytes, held to the digest as it is read.

A repository's deletion writes an empty object, `deleted`, under
`manifests/<mark>/`, where `<mark>` is an id taken as an archive's is, past
every archive the store shows anything of and the one the database records,
and leaves it there. **A mark covers every archive whose id sorts below
it**: no reader restores a covered archive, whatever lands in it later, and
no pass writes in one. A repository's id is never minted again; one whose
newest archive a mark covers comes back only by an explicit import
(`substratectl archive import`), which starts an archive past the mark.

### Pieces

A **piece** is a run of [lines](#the-line), one per entry and each ending in a
newline: whole transactions and consecutive seqs, written once and never
changed, named by the first and the last seq it holds (15 digits each), the
pass that put it and the first 16 hex digits of its bytes' SHA-256. A pass
cuts a piece at the first transaction boundary at or past 64 MiB, so a
transaction never crosses two. A last piece under 1 MiB is written again with
the next pass's lines, under a name of that pass, and the manifest that
selects the longer one drops it.

**The manifest is the index.** A reader reads the pieces `pieces` lists, in
order, each held to its `sum`, every line held to its own `sum` and its seq to
its place; a piece that is not the run its name says, or not the bytes the
manifest says, is damage, never a tail to cut. A piece the manifest does not
list is not read.

### Sealed values, blobs and the spool

- A **sealed value** holds one value of a `secret` property, or the login
  credential's, as `{"ref", "recordKind", "recordId", "payload",
  "expiresAt"?, "updatedAt"}` and a newline. `payload` is ciphertext under the
  repository's data key, base64. Its object is
  `sealed/<file>.<sum>.<pass>.json`, where `<file>` is the ref with every `:`
  as `-`; a reader takes the version `sealed` selects and refuses bytes that
  are not the version its name says. A line names the value by its `ref`,
  never by its content.
- A **blob** is its bytes, as they were uploaded, named by `blob-sha256-`,
  their digest and the pass that put them.
- A **spooled webhook request** is a request the door answered `202` and the
  changelog does not hold yet, sealed under the data key and bound to the
  repository's id and the fire
  ([decision 0167](decisions/0167-the-webhook-door-answers-after-a-sealed-spool-write.md)),
  named by its spool file and the pass.

### How a pass writes it

The database keeps a **record** of its archive (decision 0279): the archive
it writes, the manifest it last published (its name and its bytes), the
exact bytes of the one it is about to publish, and one row for every object
it has put or is deleting, written before the request is sent. One pass:

1. lists `manifests/` and holds what it shows to the record; anything but
   the archive as the database left it stops the pass, which writes and
   deletes nothing ([the contract's rule 4](operations.md#the-archives-contract));
2. puts the manifest the record says is about to be published, the same
   bytes, and records it published;
3. reads the webhook spool and holds every blob against collection, takes
   one repeatable-read snapshot, and puts what the manifest it started from
   does not select: blobs (1 GiB at most), spooled requests, sealed values
   that changed, and the pieces from the mark to the snapshot's head;
4. records the new manifest's bytes as about to be published, puts them, and
   records the publication, with every object it no longer selects marked
   dropped, in one transaction;
5. deletes what the record says the publication dropped, the manifest
   before included, each after its row says the delete is about to be sent.

A pass stopped at any step leaves the archive at its last publication: the
next pass finishes a publication recorded as about to be made, and deletes
what a process before put and no manifest selected. Nothing a published
manifest selects is deleted before a later publication drops it.

### A 0.x directory

A release before the archive wrote each repository as a directory under the
data root, `repositories/<id>/`: a format 2 manifest with no `head`, the
changelog in **segments** under `changelog/`, a file per sealed value under
`sealed/`, and `blobs/` and `spool/`. A segment is the same lines, named by
the seq of its first line in 15 digits (`000000000000001.ndjson`). A finished
segment has a sidecar, `<name>.sha256`, holding the lowercase hex SHA-256 of
the whole file and a newline; the highest has none and was the one that grew.
A transaction never crosses a segment, and a reader cuts an unfinished
transaction at the end of the highest segment whole.

A server reads a 0.x directory once, at its first boot, and takes its
history into the database
([what happens at boot](operations.md#what-happens-at-boot)), in this order:
every line both it and the table hold is compared, in the one dialect both
are in; the lines the table lacks and the sealed files go in; the directory
is set aside (`repository.json` renamed to `repository.json.adopted`, from
when no boot reads it again); the table is converted and folded; the
manifest, segments and sealed files are removed, the set-aside manifest last;
and a new archive is published by the first pass. The blobs and the spool
stay where they are. The [kept directories](#kept-directories) are in this
format.

A release before ids wrote format 1: the same keys without `id`, the
directory named by the authority and the data key bound to it. A server
converts such a directory at its boot, before anything reads it: it takes
the id the repository's row holds, or a new one for a directory being
restored, seals the spooled requests again under it, rewrites the manifest
in format 2 and renames the directory. Nothing else reads format 1.

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

**The canonical form** is what the checksum covers and what a piece holds:
no whitespace; object keys in bytewise order; strings as Go's
`encoding/json` writes them (`\"` and `\\`; `\b`, `\f`, `\n`, `\r` and `\t`;
every other control character and `<`, `>` and `&` as `\u00XX`; U+2028 and
U+2029 as ` ` and ` `; everything else as UTF-8);
`seq`, `causedBy` and `txn` as plain decimals; and every number inside
`payload` in one value-exact spelling, so `1.5`, `1.50` and `15e-1` are all
`1.5E0` and every zero is `0`. The spelling is computed on the digits and
never through a float. One vector, the first in the file:

```
{"actor":"api","kind":"ada.example.com/tasks/task","op":"put","payload":{"created":true,"fold":[{"delta":{"created":true,"kindVersion":3E0,"set":{"dueAt":"2026-08-06T09:00:00Z","estimate":1.5E0,"name":"Ship the fold"},"title":"Ship the fold"},"id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"record","ref":"ada.example.com/tasks/task","version":1E0},{"actor":"api","id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"manager","principal":"01jbx9k0token000000000000a","property":"name","ref":"ada.example.com/tasks/task","tier":"owner"}],"properties":["name","dueAt"]},"principal":"01jbx9k0token000000000000a","recordId":"01jbx9m2qz7k4c5v8n3w6t1hrd","seq":4190,"sum":"sha256:308ac73610024745a83e81276ac52f61d3b8f406156a231984a79199f8731e31","ts":"2026-08-04T10:00:00.183742Z","txn":4190}
```

[The changelog](changelog.md#the-checksum-and-the-archive) says what the
checksum proves and what it does not.

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
it, because a piece holds the canonical one and Postgres prints its own.

## Dialects

A repository's **changelog dialect** names the shapes its lines are in: the
ops, the effects and their keys. It is stamped in the database and in the
manifest, and it is 2 today. A change to what a line may hold raises it, and
a change to a shape history already holds also ships a
[conversion step](#conversions) to the new number, so a server reads one
dialect, its own. The line format (`LineFormat`) is 1 and the manifest's
`format` is 3, and a change to the frame of a line or of the manifest would
raise them. [The changelog](changelog.md#the-dialect-a-changelog-is-written-in)
says how the stamp is written and what refusing a newer one looks like.

## Conversions

A change to a stored shape lands as one **conversion step**, numbered like a
migration: step 2 converts a history in dialect 1 to dialect 2, and each
next step the one after. The chain is in `internal/engine/conversion.go`:

| Step | Name | What it changes |
| ---- | ---- | --------------- |
| 2 | `drop_recovery_wrap` | Each line of kind `substrate.reamde.dev/core/recoverykey`: `sealedKey` is dropped from what its `record` effects' `delta` sets, and the rest of the line stays, the property's name in the payload's `properties` and its `manager` effect included. The value was the repository's DEK wrapped to the owner's recovery identity, which opened every secret of every copy of the directory ([decision 0264](decisions/0264-a-repositorys-secrets-travel-with-its-host-key-or-are-left-behind-by-the-operators-choice.md)). |

A step declares how it rewrites one line in place: it may
not move the line's `seq` or `txn`, and the server recomputes the `sum`. A
step whose rewrite depends on the repository (a kind is the repository's own
only under its own authority) is handed the repository's id and name and
returns that repository's rewrite, and may carry rows outside history with
it, such as a sealed row bound to the kind its lines now name.

The history converted is the changelog table. At the boot check, before a
repository is folded or served, the server runs the steps it lacks, in
order, in **one transaction** under the changelog lock: each step walks the
whole table in seq order, so its `Line` sees every line once, as the steps
below it left it, and every row whose line it changed is rewritten; a step
that reads the repository's declarations reads them from the table those
walks wrote. Then each step's carry runs, the stamp is raised to the last
step's dialect and the repository is marked for rebuilding. The records are
then rebuilt from the table, which clears the mark. A server stopped before
that transaction commits leaves the history and every carried row as they
were, and the next boot converts them again; one stopped after it leaves the
mark, and the next boot rebuilds the records before anything is served.

A history from an archive or a 0.x directory is converted the same way,
between its lines going into the table and their fold. The archive follows on
its own: its next pass writes every piece again under the new dialect's
directory, moves the manifest to them, and deletes the old dialect's pieces
after the manifest. An archive restored from before a conversion names its
dialect in its manifest, and its lines are converted as they are imported. A
step leaves a line already in its dialect unchanged, so running it twice
changes nothing. A process that does not run the boot check, such as an
operator's read-only one, refuses to open a history below its dialect.

The stamp says which dialect a history's lines are in. A repository with no
stamp holds dialect 1, the history from before the stamp. A history below the
chain's dialect is claimed by its conversion at the boot check, whether or
not a step changed a line; an empty history is claimed by its first append,
as a new repository's is; an open claims nothing.

A conversion is not a [repository migration](operations.md#upgrading-the-binary).
A migration changes what records hold, within the current shapes, by
appending ordinary writes, and runs again over every import. A conversion
changes the shape history is written in, rewrites lines in place and appends
nothing, and the repository's own stamp records it. A change that needs both
lands both, and the conversion runs first. Dropping the recovery wrap is
one: after step 2, repository migration 8, `purge_the_recovery_key`, purges
the record those lines fold into, `substrate.reamde.dev/core/recoverykey/self`,
at the repository's first open. The kind stays declared and empty in a
repository registered before it, because the boot upgrade never prunes a
declaration; a repository registered since never declares it. Converting
closes the way back to an older server, as any raised stamp does
([going back](stability.md#going-back)).

The move to repository ids is neither: no line changed, because a line names
kinds and actors, and those stay the name's. What changed is the key the
rows and the directory are found by, so the boot gives a repository its id
before anything looks for it, and before the chain runs
([a 0.x directory](#a-0x-directory) above, and
[running a substrate](operations.md#what-happens-at-boot)).

## How old a copy may be

A 1.x server reads an archive any earlier 1.x release wrote, converting it
once where a shape changed, and 2.0 reads what the last 1.x release wrote. A
conversion step is carried for one major version: once the steps are dropped,
in a release after 1.0, an archive from before 1.0 is read by restoring it
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
A kept directory is never edited (`mise run frozen:check`). The directories
kept so far are [0.x directories](#a-0x-directory).

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
