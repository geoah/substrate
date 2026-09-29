# Changelog

## [0.112.0](https://github.com/geoah/substrate/compare/v0.111.1...v0.112.0) (2026-09-29)

### ⚠ BREAKING CHANGES

* **vocabulary:** require trigger.source and recordpatchpolicy.action ([#787](https://github.com/geoah/substrate/issues/787)) ([9594691](https://github.com/geoah/substrate/commit/959469138dcf92908c2212de82ce3d01de1791f8))

### Fixed

* **vocabulary:** restore a tombstone in the kind's current shape ([#785](https://github.com/geoah/substrate/issues/785)) ([4e76a7c](https://github.com/geoah/substrate/commit/4e76a7cd9955f08a38c0ff94116e1e981fee4b63))
* **catalog:** reuse upgrade previews until the changelog head moves ([#786](https://github.com/geoah/substrate/issues/786)) ([ad8cc84](https://github.com/geoah/substrate/commit/ad8cc8486b553607ab21da77ea43f98014be67e3))
* **vocabulary:** require trigger.source and recordpatchpolicy.action ([#787](https://github.com/geoah/substrate/issues/787)) ([9594691](https://github.com/geoah/substrate/commit/959469138dcf92908c2212de82ce3d01de1791f8))

### Upgrade notes

#### `trigger.source` and `recordpatchpolicy.action` are required, and an older binary cannot open the upgraded core

Core's `substrate.reamde.dev/core/trigger` (version 20) now declares `source`
required, and `substrate.reamde.dev/core/recordpatchpolicy` (version 19)
declares `action` required. The engine already refused to run a trigger
without a source or a policy without an action. The declaration now says
so: a form marks both required, and a write that leaves one out answers
`422 validation` naming the property:

```
props.source: trigger requires a value
```

`source` is an `object`, and an `object` property now takes `required:`. A
declaration's key set is closed, and a binary from before this release
refuses that key on an object, so it refuses to open a repository whose core
took this upgrade. The way back is this release or a later one.

A repository still holding a row without the value, left by an older binary,
does not take this upgrade. The boot upgrade refuses the shipped vocabulary
as a whole (core and llm) while one such row lives, logs the count, and opens
the repository on its stored declarations:

```
kind substrate.reamde.dev/core/trigger: property "source" becomes required while 1 live records lack it: declare a default to backfill them, or write them first
```

##### What to do

1. Do not roll a server back past this release once it has opened a
   repository.
2. If the server logs the refusal above, or `substratectl catalog` lists it
   under "the upgrade is blocked", list the rows without the value:

   ```sh
   substratectl get substrate.reamde.dev/core/trigger \
     --filter '{"properties":{"source":{"exists":false}}}'
   substratectl get substrate.reamde.dev/core/recordpatchpolicy \
     --filter '{"properties":{"action":{"exists":false}}}'
   ```

3. Delete each one. The engine never ran these rows, so deleting one changes
   no behavior:

   ```sh
   substratectl delete substrate.reamde.dev/core/trigger <id>
   substratectl delete substrate.reamde.dev/core/recordpatchpolicy <id>
   ```

   To keep a policy instead, add `action: gate` (or `allow`, or `refuse`)
   under `data.properties` in the output of `substratectl get
   substrate.reamde.dev/core/recordpatchpolicy <id> -o yaml`, and apply the
   result with `substratectl apply -f`.

4. Restart the server. The boot upgrade runs at a repository's first open
   under a binary, so it lands at the next start.

#### A `put` restoring a tombstone rewrites it into the kind's current shape

A vocabulary apply that renames a property, respells an enum value with
`renamedFrom:`, adds `required:` with a `default:` or drops a property
rewrites live records only. A record deleted before that apply used to come
back from a restoring `put` in its old shape: holding `active` where the kind
now admits `working`, holding a property the kind no longer declares, or
refused outright with `props.mood: widget requires a value`. The restoring
`put` now rewrites the stored row first: a renamed name or spelling moves, a
value the kind no longer admits is removed, and a missing required value
receives its default. A property the `put` names keeps the `put`'s value.

```bash
# the kind declares `{value: working, renamedFrom: active}`; w1 was deleted holding `active`
substratectl apply -f w1.yaml   # restores w1, the document leaves `status` out
substratectl get convert.example.com/cv/widget w1 -o yaml
# data:
#   properties:
#     status: working
```

The restoring entry names each step under `renamed`, `remapped`,
`backfilled` or `nulled`, as a conversion's entry does, and
`GET /api/v1/changes?values=1` shows the removed values leaving there.

## [0.111.1](https://github.com/geoah/substrate/compare/v0.111.0...v0.111.1) (2026-09-29)

### Fixed

* **cli:** print apply progress to stderr during a vocabulary batch ([#780](https://github.com/geoah/substrate/issues/780)) ([9d231ee](https://github.com/geoah/substrate/commit/9d231ee2b15e96676eac55cd30e2b97aa9778ec4))
* **runner:** start a python body under a 5s floor, not its timeout ([#781](https://github.com/geoah/substrate/issues/781)) ([25991db](https://github.com/geoah/substrate/commit/25991db1aa789e7a4fdd24eadceab0caab3a33e4))
* **catalog:** offer the next shipped version over a higher stored one ([#667](https://github.com/geoah/substrate/issues/667)) ([6a2a655](https://github.com/geoah/substrate/commit/6a2a655af6ffe1dd10efbdfcfc4aeb16d43c958c))
* **engine:** count only purged rows in a GC pass ([#784](https://github.com/geoah/substrate/issues/784)) ([d62944a](https://github.com/geoah/substrate/commit/d62944ad812aec99965c64c9d5219ff6a9038a70))

### Upgrade notes

#### `substratectl apply` prints progress to stderr while a vocabulary batch runs

Before this release, `substratectl apply -f` printed nothing until the server
answered a vocabulary batch; a batch of 96 documents onto a repository with
336k records was ten minutes of silence. While the request is in flight,
`apply` now prints a line to stderr every 10 s, and stdout carries the same
summary as before. A script that treats any stderr output as a failure sees
these lines on a slow apply that succeeds.

```
$ substratectl apply -f kinds/capture/*.yaml -f kinds/tasks/*.yaml
applying 96 documents in 12 packages, 10s elapsed
applying 96 documents in 12 packages, 20s elapsed
package/example.com/capture applied
```

The server logs each step of the batch at info as the step starts, so an
operator tailing the log sees where the batch is while it holds the registry
lock. A step that can run long (waiting for the batch ahead, preparing
function bodies, building an index, a walk over stored records) logs only
when it has work:

```
time=2026-09-29T12:24:11.911Z level=INFO msg="substrate: vocabulary apply: holding the registry lock, checking the batch against the stored records" repository=ada.example.com documents=5 packages=2
time=2026-09-29T12:24:11.912Z level=INFO msg="substrate: vocabulary apply: writing the declarations of one package" repository=ada.example.com package=progress.example.com/depot declarations=2 index=1 packages=2
time=2026-09-29T12:24:11.929Z level=INFO msg="substrate: vocabulary apply: writing the declarations of one package" repository=ada.example.com package=progress.example.com/shop declarations=3 index=2 packages=2
time=2026-09-29T12:24:11.943Z level=INFO msg="substrate: vocabulary apply: re-deriving the search index" repository=ada.example.com kinds=3
time=2026-09-29T12:24:11.948Z level=INFO msg="substrate: vocabulary apply: committed" repository=ada.example.com took=42ms
```

#### A provider install records `shippedVersion`, and the upgrade preview measures from it

`POST /api/v1/catalog/{id}/install` of a provider stamps the shipped package
version it took on the package row as `shippedVersion`, and the catalog's
`upgrade` preview offers any shipped closure past that stamp. Before, the
preview compared against the stored package version, so a provider whose
stored version ran ahead of the shipped one (from hand applies before the
install) was never offered the next shipped versions: stored google 35 over
shipped 33 landed 36, and shipped 34 to 36 were never offered. Where the
stamp drives the offer, `upgrade.changes` lists each declaration the install
would change at the version it lands at (stored+1), the package header
included: a header edit (its `description`, its `retired` names) now moves
the package to stored+1 on every door, where before it kept the stored
version. A release past the stamp is offered even when `changes` is empty,
since it may change only a shipped trigger; taking it moves the stamp. A
sample installed verbatim through the same route stays editable and is not
stamped. The bundle status
(`GET /api/v1/substrate.reamde.dev/core/bundle/{id}/status`) carries the
stamp back as `shippedVersion` beside the stored `version`, and the console
shows both in technical mode.

A provider installed before this release carries no stamp and is measured
from its stored version until it is installed once more:

```bash
substratectl install providers.substrate.reamde.dev/google
```

## [0.111.0](https://github.com/geoah/substrate/compare/v0.110.6...v0.111.0) (2026-09-29)

### ⚠ BREAKING CHANGES

* **server:** bind 127.0.0.1 and refuse a non-loopback bind ([#778](https://github.com/geoah/substrate/issues/778)) ([513b750](https://github.com/geoah/substrate/commit/513b750985c983b0b6f98b6747f311622994e2ba))

### Added

* **server:** bind 127.0.0.1 and refuse a non-loopback bind ([#778](https://github.com/geoah/substrate/issues/778)) ([513b750](https://github.com/geoah/substrate/commit/513b750985c983b0b6f98b6747f311622994e2ba))
* **agents:** gate function effects through the policy selector ([#779](https://github.com/geoah/substrate/issues/779)) ([28bd586](https://github.com/geoah/substrate/commit/28bd5861a8fcf707b184492cc2e183e4e8cd4b05))

### Upgrade notes

#### The server listens on `127.0.0.1` and refuses a non-loopback bind

The server used to listen on every interface. It now listens on `127.0.0.1`
unless `SUBSTRATE_BIND_ADDRESS` names another address, and it refuses to
start on any address that is not loopback, `0.0.0.0` and empty included,
unless `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true` is set. The server speaks
plain HTTP, so the setting states that only a TLS terminator or a loopback
port mapping reaches the port. This hits a binary that other machines reach
directly or through a proxy on another host, and an image of your own built
around the binary. The published image and this repository's `compose.yaml`
set both variables. A refused boot exits with:

```
SUBSTRATE_BIND_ADDRESS is "0.0.0.0", so the server would listen on 0.0.0.0:8080, which is not loopback, and it speaks plain HTTP: passwords, TOTP codes and bearer tokens would reach the network unencrypted. ...
```

##### What to do

1. Compose: with this repository's `compose.yaml`, or any compose file that
   runs `ghcr.io/geoah/substrate`, nothing. A compose file that runs an image
   of your own adds, under the substrate service's `environment:`:

   ```yaml
   SUBSTRATE_BIND_ADDRESS: 0.0.0.0
   SUBSTRATE_INSECURE_ALLOW_CLEARTEXT: "true"
   ```

2. Kubernetes: with `ghcr.io/geoah/substrate`, nothing; the image sets both.
   A container built from an image of your own adds, under its `env:`:

   ```yaml
   - name: SUBSTRATE_BIND_ADDRESS
     value: 0.0.0.0
   - name: SUBSTRATE_INSECURE_ALLOW_CLEARTEXT
     value: "true"
   ```

3. A bare binary behind a proxy on the same host: nothing; point the proxy at
   `127.0.0.1:8080`. Behind a proxy on another host, add to the service's
   environment the interface the proxy reaches and the escape, and keep every
   other peer off the port:

   ```
   SUBSTRATE_BIND_ADDRESS=192.0.2.10 SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true
   ```

4. A binary that other machines reach with no TLS terminator in front is not
   a supported deployment. Put one in front
   ([TLS and the reverse proxy](docs/operations.md#tls-and-the-reverse-proxy)).

#### `recordpatchpolicy` gates function effects through `selector.functions`

An owner can now hold what a function writes for review. A
`recordpatchpolicy` whose `selector.functions` names a function gates each
put, patch or delete that function returns, wherever it runs: a trigger
delivery, a schedule or webhook fire, a drain page, a direct call, or an
agent's tool call. The effect lands as a `recordpatchrequest` written by the
function's actor and stamped with `function`; the target is untouched until
the owner accepts it.

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: gate-triage
data:
  properties:
    selector:
      functions:
        - crew.example.com/bots/triage
    action: gate
```

A policy without `functions` still speaks for agent writes alone, so an
existing `{}` gate does not start holding trigger writes.

A function's own `confirmation: always` now holds its effects the same way
on every path, not only when an agent runs it as a tool, and refuses a
`merge` or `split` it returns. Installed code can no longer accept a request
that carries `function`; the owner, or the governing policy's judge, decides
it. An effect outside `permissions.writes` is refused, never queued.

## [0.110.6](https://github.com/geoah/substrate/compare/v0.110.5...v0.110.6) (2026-09-29)

### Fixed

* **api:** refuse unknown query parameters on a single-record GET ([#776](https://github.com/geoah/substrate/issues/776)) ([46c4d0f](https://github.com/geoah/substrate/commit/46c4d0f64d177df03a41c8764b4dc98afb6e6880))
* **providers:** name Slack's needed scope in a missing_scope refusal ([#777](https://github.com/geoah/substrate/issues/777)) ([e089c07](https://github.com/geoah/substrate/commit/e089c076a6fc8091004ed934d1d8a991396c3017))

### Upgrade notes

#### A single-record `GET` refuses any query parameter

`GET /api/v1/{authority}/{package}/{kind}/{id}` honors no query parameter.
Before this release it ignored every one it was sent and answered `200`, so a
client sending `expand`, a stale `withEdges` or a typo got the bare record
back and could not tell. It now answers `400 bad_request` naming the
parameter, the same body `GET /api/v1/records` gives:

```http
GET /api/v1/samples.substrate.reamde.dev/people/person/9f2k?withEdges=1

→ 400 {"error": {"code": "bad_request",
                 "message": "unknown query parameter \"withEdges\""}}
```

A query string that does not parse, such as `?filter=%ZZ` or `?a=1;b=2`, is
now `400 bad_request` naming the parse error on the record read, the records
route, `DELETE` and `/changes`. Before, the unreadable pair was dropped, so
`GET /api/v1/records?filter=%ZZ` listed every record.

The console and `substratectl` send neither and are unaffected.

##### What to do

1. Drop every query parameter from a single-record `GET`. The read already
   carries `annotations`, so `withAnnotations=1` loses nothing.
2. For a record's referents, list it with
   `GET /api/v1/records?filter={"ids":[…],"kinds":[…]}&expand=…`.
3. Percent-encode `;` and `%` inside a query value (`%3B`, `%25`).

## [0.110.5](https://github.com/geoah/substrate/compare/v0.110.4...v0.110.5) (2026-09-29)

### Fixed

* **vocabulary:** compare decimal and money bounds as declared numbers ([#772](https://github.com/geoah/substrate/issues/772)) ([1c60582](https://github.com/geoah/substrate/commit/1c6058259fe926f030793dc0a585ef6259004f0d))
* **vocabulary:** drop a state property as a confirmed lossy null step ([#771](https://github.com/geoah/substrate/issues/771)) ([33b2e39](https://github.com/geoah/substrate/commit/33b2e39ed17db2cdb36476a01727009bbf32bd2a))
* **engine:** erase a purged record's sealed rows and files ([#774](https://github.com/geoah/substrate/issues/774)) ([414e08f](https://github.com/geoah/substrate/commit/414e08f9a8544b9b555cf93d358c61093364584e))
* **server:** send security headers and no-store on credential routes ([#775](https://github.com/geoah/substrate/issues/775)) ([9b5a814](https://github.com/geoah/substrate/commit/9b5a814b3dfe534685560232d9c8be187a3157cd))

### Upgrade notes

#### A `decimal` or `money` bound admits the value it names

Before this release the engine compared a `decimal` or `money` value against
the binary expansion of the bound's float64, so `min: 0.01` refused `"0.01"`
and `max: 0.3` refused `"0.3"`, both on a write and in the guard that refuses
a raised `min` or a lowered `max` over live records. Both now compare against
the number the declaration names.

The same change refuses, at its next write, a stored decimal that sat between
the bound and its float64. Under `max: 0.01`, `"0.0100000000000000001"` was
admitted and is now refused:

```
props.rate: must be <= 0.01
```

Only a decimal with 17 or more significant digits can sit there; a `money`
amount cannot. To find one, list the kind with a filter just past the bound:

```bash
substratectl get <authority>/billing/fee --filter '{"properties": {"rate": {"gt": "0.01"}}}'
```

#### A purge erases the record's sealed secrets, and `repository verify` names orphans

A record purged by the GC sweep or by `DELETE ?purge=true` used to leave its
rows in the `sealed` table and its files under `sealed/` behind, so every
backup kept ciphertext no record pointed at. The purge now erases them in
the same transaction. Each GC sweep also erases the sealed rows that no
record, live or tombstoned, holds the ref of, which clears what earlier
purges left.

`repository verify` counts those rows and files and names each one as a
finding, so on a repository that purged a record holding a secret-typed
property (an `llm/provider` with an `apiKey`, a connected account), verify
and `repository snapshot` fail until the first GC sweep has run, five minutes
after the server boots:

```
$ substratectl --dsn "$DATABASE_URL" repository verify ada.example.com
repository ada.example.com
  sealed:   4 rows, 4 files, 1 held by no record
  FINDING:  sealed secret:3f9a0c1d2e4b5a6978c0d1e2f3a4b5c6 (substrate.reamde.dev/llm/provider old): an orphan: no live or tombstoned record holds the ref, so nothing reads the material
```

Start the server on this release and let it run one sweep, then verify
again. The erasure reaches the live table and the directory only: backups
and snapshots taken before it still hold the ciphertext, so a leaked key
must also be revoked at its provider.

#### Dropping a `type: state` property clears the state after a data-loss confirmation

A vocabulary apply that removes a `type: state` property from a kind with
records used to be refused with `state property "status" dropped while 3 live
records hold a state`. Every record of such a kind holds
a state and no write clears one, so the refusal fired on every kind with data.
The drop is now the same lossy `null` step as dropping any other property: it
runs only with the previewed confirmation, and it removes the state from
every live record as one `patch` entry each. `createdAt`, the version history
and every other property stay. The drop is not a transition: it writes no
stamp and runs no `onEnter` effect or `notifies:` resume.

```bash
substratectl apply --allow-data-loss -f note.yaml
# confirming plan 3f9c… at changelog seq 812, which removes values:
#   drops status on notes.example.com/capture/note: its value leaves 3 live records (lossy: the values stay in the changelog only)
```

Without the flag the apply is refused with the `lossy` code, naming the step
and the `planHash` to confirm. On `GET /api/v1/changes?values=1` each
record's drop entry lists the state with its `before` and no `after`. The
shipped boot upgrade still refuses such a drop, because it never runs a lossy
step.

## [0.110.4](https://github.com/geoah/substrate/compare/v0.110.3...v0.110.4) (2026-09-29)

### Fixed

* **changelogfile:** verify a line without decoding it again ([#767](https://github.com/geoah/substrate/issues/767)) ([b8e6dfc](https://github.com/geoah/substrate/commit/b8e6dfcce55773cd4a824e606b64baaca890ed6e))
* **engine:** retire older parked fires when a schedule fire settles ([#768](https://github.com/geoah/substrate/issues/768)) ([94f2d7c](https://github.com/geoah/substrate/commit/94f2d7c14448b0c5639375f4a6d5dd0e9a092746))
* **engine:** apply no migration on a read-only open ([#769](https://github.com/geoah/substrate/issues/769)) ([b3ae620](https://github.com/geoah/substrate/commit/b3ae62079baed15572765f7daf08708bd9aa5c5c))
* **engine:** re-derive fts and refs for kinds a boot upgrade reshapes ([#770](https://github.com/geoah/substrate/issues/770)) ([d8195ab](https://github.com/geoah/substrate/commit/d8195abe4eb989c9cde78db31b8f00f09635f077))

### Upgrade notes

#### `repository verify` and `repository reembed` apply no migration

`substratectl repository verify` and `substratectl repository reembed` open
the engine read-only, beside a live server. Before, that open still applied
every schema migration the CLI carried, so a `substratectl` newer than the
server migrated the server's database and closed its rollback. Now the
read-only open applies nothing. It reads `schema_migrations` and refuses a
database missing a migration the CLI carries, naming each one:

```text
$ substratectl repository verify ada.example.com
error: open the substrate database: substrate/engine: the database has not
applied migrations this binary carries: 1 migration(s) pending, 10
(0010_records_matching), and this process opened the database read-only; open
it once with a process that writes (the server's boot applies them), or run
the substratectl of the release the server runs
```

A database holding a migration the CLI does not carry is refused as before.
Run both commands with the `substratectl` of the release the server runs.
`repository rebuild`, `rotate-generation`, `snapshot` and `user reset` need
the server stopped and still apply pending migrations, as the server's boot
would.

#### `repository verify` holds table checksums to the files; `--recanonicalize` recomputes them

`repository verify` and `repository snapshot` no longer recompute every
changelog table row's checksum from its stored columns. The table pass holds
each row's stamped checksum to the `sum` its line carries, and the file pass
checks each line without decoding it, several segments at a time. On a
history of 2 million entries the old walk took hours. A row edited in the
database with its checksum left alone is now found only with the new flag:

```
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify --recanonicalize ada.example.com
```

The snapshot's read-back hashes each copied finished segment against its
copied sidecar and the source's digest instead of walking its lines.

#### A settled schedule fire retires its trigger's older parked fires

When a fire of a schedule trigger settles, dispatched or retried by hand,
the engine deletes every parked fire of that trigger at or before the
settled occurrence, through the same unpark a retry writes. `GET /api/v1/sync/status`, the trigger status and
`GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked` then report 0
parked for it, and the console stops showing "N runs failed and are waiting
to be tried again" for a sync that has recovered. Rows parked before the
upgrade clear at the trigger's next settled fire.

Example: `github-scheduled` holds 110 parked fires from 2026-09-22 to
2026-09-28 13:30 UTC. Its 14:30 UTC fire settles `ok`, and the list is empty:

```console
$ substratectl trigger parked github-scheduled
ID  SEQ  FIRE  RECORD  ATTEMPTS  PARKED  RUNNING  ERROR
```

Three kinds of park stay until a person retries or forgets them: a record
trigger's (one record's change), a webhook request's, and an agent run a
server stop interrupted. Decision record 0142 has the reasoning.

## [0.110.3](https://github.com/geoah/substrate/compare/v0.110.2...v0.110.3) (2026-09-29)

### Fixed

* **engine:** re-derive the search index in the background after open ([#765](https://github.com/geoah/substrate/issues/765)) ([be4ee9b](https://github.com/geoah/substrate/commit/be4ee9ba1d0a2a885c36e586dab348c9b1f700bd))
* **engine:** record a paged drain's cursor by hash on each page entry ([#766](https://github.com/geoah/substrate/issues/766)) ([f113400](https://github.com/geoah/substrate/commit/f1134003051867da320ce64017206e46ba593029))

### Upgrade notes

#### A paged drain's page entry names its cursor by hash instead of copying it

Each page of a paged drain appended a `delivery` changelog entry carrying the
body's whole resume cursor. Slack's cursor is about 380 KB at 9,000 pending
items, so its triggers grew the segment files by that much on every page. A
page entry now records the cursor's `cursorSha256` and `cursorBytes`, the
SHA-256 and byte length of the stored cursor as Postgres prints it, and the
cursor stays in the `paged_cursors` table:

```
{"kind": "page", "ref": "substrate.reamde.dev/core/trigger", "id": "<trigger>",
 "page": {"chain": "<chain>", "cursorSha256": "<SHA-256 of cursor::text>",
          "cursorBytes": <its length>, "version": <n>, "pages": <n>,
          "effects": <n>, "bytes": <n>, "startedAt": "<first page>",
          "kind": "fire", "identity": "<fire id>"}}
```

A park still writes the cursor whole into its entry, so a parked drain
resumes from its last committed page after a restore, as before.
`substratectl repository rebuild` keeps every cursor the database holds.

One case behaves differently after an import of a repository directory into
an empty database (a restore from `substratectl export` or a copied data
root): a paged drain that stopped between pages without parking (the server
crashed or was stopped mid-drain) starts over from its first page, where it
used to resume. Its next delivery re-runs the pages it had committed.

Entries written before this release keep their cursors and replay as they
did; nothing rewrites them, so the space they take stays. Decision record
0141 has the design.

#### The search reindex after an upgrade no longer blocks the repository

When a new binary indexes text differently, the first open of each
repository re-derives every row's search index. That ran in one
transaction inside the open, so every request on the repository waited for
it: 24 minutes on a repository of 336k records, while `/healthz` answered.
It now runs in the background after the open, in transactions of 2000
rows. Reads and writes are served throughout, and a search finds each row
by its old index until the reindex reaches it. The log shows it advancing
kind by kind (a small repository):

```
INFO substrate: re-deriving the search index repository=ada.example.com from=1 to=2 kinds=9 rows=53
INFO substrate: re-derived the search index of one kind repository=ada.example.com kind=samples.substrate.reamde.dev/people/person rows=1 done=1 total=53
INFO substrate: re-derived the search index of one kind repository=ada.example.com kind=substrate.reamde.dev/core/kind rows=32 done=43 total=53
INFO substrate: re-derived the search index repository=ada.example.com from=1 to=2 rows=53 took=46ms
```

A shutdown before the last line leaves the old version recorded, and the
next open starts the reindex again.

## [0.110.2](https://github.com/geoah/substrate/compare/v0.110.1...v0.110.2) (2026-09-29)

### Fixed

* **engine:** refuse a blob read whose bytes do not hash to the digest ([#763](https://github.com/geoah/substrate/issues/763)) ([06f1892](https://github.com/geoah/substrate/commit/06f1892e6293ee8d1e62f6f867bc10f64065c821))
* **engine:** stop a client disconnect from latching the repository ([#764](https://github.com/geoah/substrate/issues/764)) ([2485563](https://github.com/geoah/substrate/commit/2485563a9924841a9f6f527b687f4774d961e8cd))

### Upgrade notes

#### `GET /api/v1/blobs/{digest}` answers `500` when the stored bytes do not hash to the digest

A blob read now hashes the stored bytes before it sends them. Bytes that do
not hash to the digest in the path, or are not the size the manifest
declares, were served with `200`; they now answer `500` with code `internal`
and a message naming the digest, and nothing of the blob is sent:

```http
GET /api/v1/blobs/blob-sha256-4f2a…

HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"error": {"code": "internal", "message": "substrate: stored data is corrupt: blobbytes: the stored bytes do not match their digest: blob-sha256-4f2a… hashes to blob-sha256-9c01…"}}
```

A client may rely on a `200` from this route: its body hashes to the digest.
The export (`GET /api/v1/export`) stops before a damaged blob's last byte and
cuts the connection. `substratectl repository verify` lists every damaged
blob; the repair is copying the file `blobs/<digest>` back from a backup.

#### A repository that needs a restart answers `503 unavailable`, not `500 internal`

A client that disconnected while its write committed could leave the
repository refusing every later write with `500 internal` until the server
restarted (#516). A write that is ready to commit now commits whatever the
client does, and a commit whose answer was lost after Postgres applied it is
appended to the directory by the next write, so neither case refuses
anything.

When that repair fails (the changelog writer or the directory is broken),
every write to the repository answers `503` with `Retry-After: 30`, and the
message names the restart:

```http
HTTP/1.1 503 Service Unavailable
Retry-After: 30

{"error":{"code":"unavailable","message":"substrate: refused until the server restarts: the repository directory is behind the tables after a failed write; restart the server so the boot check catches it up: repository alice.example.com: ..."}}
```

A client that waits `Retry-After` on a `503` and retries keeps working. The
operator restarts the server, and the boot check catches the directory up.

## [0.110.1](https://github.com/geoah/substrate/compare/v0.110.0...v0.110.1) (2026-09-29)

### Fixed

* **engine:** reuse the boot check's segment digests at first open ([#762](https://github.com/geoah/substrate/issues/762)) ([63ebba8](https://github.com/geoah/substrate/commit/63ebba87472da6640871714c98845ce0708b11b9))

## [0.110.0](https://github.com/geoah/substrate/compare/v0.109.0...v0.110.0) (2026-09-29)

### Added

* **server:** add SUBSTRATE_TRIGGER_INTERVAL for the dispatcher tick ([#758](https://github.com/geoah/substrate/issues/758)) ([8dd2ad7](https://github.com/geoah/substrate/commit/8dd2ad77e0107d373df89602e45208c27379bd2b))

### Upgrade notes

#### `SUBSTRATE_TRIGGER_INTERVAL` sets the trigger dispatcher's tick

The dispatcher that checks every repository for a trigger due to run ticked
every 5 seconds, fixed in the binary. `SUBSTRATE_TRIGGER_INTERVAL` now sets
that tick; unset, it is still `5s`. The tick bounds how long a record write
waits for the delivery it fires, so a test suite that writes a record and
polls for the run wants a short one, and a host with many repositories may
want a longer one. Zero or a negative duration refuses the boot, naming the
variable.

```sh
SUBSTRATE_TRIGGER_INTERVAL=1s bin/substrate
```

`mise run test:e2e` and the provider suite (`internal/providere2e`) start
their servers with `1s`. A deployment that sets nothing sees no change.

## [0.109.0](https://github.com/geoah/substrate/compare/v0.108.0...v0.109.0) (2026-09-28)

### Added

* **vocabulary:** add a money property type ([#751](https://github.com/geoah/substrate/issues/751)) ([cf45a78](https://github.com/geoah/substrate/commit/cf45a786612942cac539fa32eb5766f8f3321df5))

### Upgrade notes

#### A `money` property type holds an amount and its currency

A kind may declare `type: money`. The value is two members: `amount`, an
integer count of minor units, and `currency`, an ISO 4217 code whose
minor unit places the decimal point (2 for EUR, 0 for JPY). This record stores
a price of 19.99 EUR:

```yaml
kind: example.com/shop/item
metadata:
  id: coffee
data:
  properties:
    price:
      amount: 1999
      currency: EUR
```

A filter operand is a money value, and the comparison holds within its
currency: `{"price": {"lt": {"amount": 2000, "currency": "EUR"}}}` is every
EUR price under 20.00. `orderBy=price` sorts by the exact number. `min` and
`max` bound that number. The console shows the value in the reader's locale
(`€19.99`) and edits it as an amount beside a currency picker.

## [0.108.0](https://github.com/geoah/substrate/compare/v0.107.3...v0.108.0) (2026-09-28)

### Added

* **vocabulary:** let an agent declare its purpose ([f0fa95b](https://github.com/geoah/substrate/commit/f0fa95b656ef5c8f58d8a4c76cacabe9cd414b5e))
* **console:** list primary agents and put the rest behind show more ([e8942ff](https://github.com/geoah/substrate/commit/e8942ff891913fcc8b04eb741cc68ca48a7d218d))
* **agents:** compact long thread histories into a summary message ([#748](https://github.com/geoah/substrate/issues/748)) ([4061842](https://github.com/geoah/substrate/commit/40618422da560d8c67ea047098aebcd97a1b81b8))

### Fixed

* **api:** ask proxies not to buffer the chat and watch streams ([5f7ccda](https://github.com/geoah/substrate/commit/5f7ccda5e68889d7b7a2bc2e926c8a736eab08b9))
* **console:** show what each agent tool call returned when opened ([8e2f59c](https://github.com/geoah/substrate/commit/8e2f59c1123e46724a4bd9e0a15932bed04eb8c1))
* **agents:** skip internal kinds in a search that names none ([96be649](https://github.com/geoah/substrate/commit/96be6499a6d4f5a6b4765ede0ce9254c56d97871))
* **engine:** read the text and timestamp indexes under RLS ([#750](https://github.com/geoah/substrate/issues/750)) ([08ba84e](https://github.com/geoah/substrate/commit/08ba84e4e400d3513ea7e78d60900649a4733e1e))
* **cli:** apply a partial declaration over the stored one ([e1c59bf](https://github.com/geoah/substrate/commit/e1c59bf79d485e3a7849ff982d9141ba0a6ff1ea))
* **console:** search primary kinds alone in ⌘K and the @ picker ([#756](https://github.com/geoah/substrate/issues/756)) ([58f9ea2](https://github.com/geoah/substrate/commit/58f9ea2465c3ff7987ab1bdf8442b48d80acb483))

### Upgrade notes

#### A long agent thread compacts its older turns into a `summary` message

A chat thread or a resumed thread whose history nears its model's context
window now has its older turns summarized by the thread's own model. The
summary is a new `substrate.reamde.dev/llm/message` row with `role: summary`,
and later continuations replay it in place of the rows it covers. Those rows
stay in the thread unchanged, so the transcript still shows every turn.

Compaction runs only for a model whose `pricing` entry on its
`substrate.reamde.dev/llm/provider` row declares `contextWindow`, the model's
context window in tokens. A model without one never compacts. New
repositories get `contextWindow` on the seeded `openai`, `anthropic` and
`gemini` rows. The seed is create-only, so a repository created before this
release keeps its rows as they are: add `contextWindow` to each pricing entry
to turn compaction on.

```yaml
pricing:
  - {model: claude-opus-5, inputPer1M: "5", outputPer1M: "25", contextWindow: 200000}
```

An agent tunes or disables it with `data.compaction`. The defaults are shown:

```yaml
compaction:
  enabled: true           # false: this agent's threads never compact
  reserveTokens: 16384    # compact when the context passes window minus this
  keepRecentTokens: 20000 # the newest turns kept verbatim after the summary
```

The summary row carries `content`, `covers` (the ids of the first and last
message rows it replaces), `tokensBefore`, and the summarizer call's `model`,
`promptTokens` and `completionTokens`. The summarizer's tokens and cost are
added to the thread's tallies. The chat stream sends one event per
compaction:

```json
{"kind": "compacted", "thread": "k3v9qzr2xw1a", "tokensBefore": 184233, "covered": 42}
```

A reader that lists a thread's messages by `role` sees one more value. The
design is decision record 0138.

#### Word search and newest-first lists read their indexes

A lexical search or a list `search` filter for a word few records hold, and
a list ordered by `createdAt` or `updatedAt` (the default order), read an
index instead of every record in the repository. A word most records hold
is still matched record by record, which is the cheaper read for it. On a
repository of 389k records, this search spent 1.9 s counting the word's
documents and 2.3 s gathering candidates:

```sh
curl -H "Authorization: Bearer $TOKEN" \
  'https://substrate.example.com/api/v1/records?q=maruasa*&mode=lexical&first=8'
```

On a copy of that repository the count now takes 5 ms, and the query behind
`GET /api/v1/records?first=1` went from 190 ms to under 1 ms.

The first boot on this release runs migration 0010. It creates one SQL
function, `records_matching`, owned by `substrate_maint`, and builds no
index, so it finishes at once. A binary from before this release refuses the
database afterwards (`ErrDatabaseNewer`), so a rollback past this release
needs the database from before it.

A list continuation token minted before the upgrade under a `createdAt` or
`updatedAt` order is refused once with `422`; the client starts the walk
again.

## [0.107.3](https://github.com/geoah/substrate/compare/v0.107.2...v0.107.3) (2026-09-28)

### Fixed

* **operator:** read each segment once in verify, rebuild and import ([#747](https://github.com/geoah/substrate/issues/747)) ([bc89daf](https://github.com/geoah/substrate/commit/bc89dafc9c88bf766e7291296114de0581f852c0))

## [0.107.2](https://github.com/geoah/substrate/compare/v0.107.1...v0.107.2) (2026-09-28)

### Fixed

* **slack:** reuse one HTTPS connection across a drain ([#744](https://github.com/geoah/substrate/issues/744)) ([72f974a](https://github.com/geoah/substrate/commit/72f974ab769992b6e094bbedcde635a506a50ccd))

## [0.107.1](https://github.com/geoah/substrate/compare/v0.107.0...v0.107.1) (2026-09-28)

### Fixed

* **sync:** clear syncError when a scheduled run ends ok ([#741](https://github.com/geoah/substrate/issues/741)) ([2b919a0](https://github.com/geoah/substrate/commit/2b919a014fe01b540e04e5f8ed7eb5d12c1823d3))
* **agents:** settle a thread whose run died in a live process ([#742](https://github.com/geoah/substrate/issues/742)) ([56d63dc](https://github.com/geoah/substrate/commit/56d63dcda7958a3aba0eaba0dbf0fefa5333a7f3))
* **providers:** describe syncError as cleared by a good run ([#743](https://github.com/geoah/substrate/issues/743)) ([24375e7](https://github.com/geoah/substrate/commit/24375e77be4e05d636da47e0697700a6f49e0145))

## [0.107.0](https://github.com/geoah/substrate/compare/v0.106.1...v0.107.0) (2026-09-28)

### Added

* **console:** edit markdown properties as documents with record links ([#737](https://github.com/geoah/substrate/issues/737)) ([03597c0](https://github.com/geoah/substrate/commit/03597c0a91a024d820e7d2a68e6fb7ae4a22c860))

### Upgrade notes

#### The console edits `markdown` properties as documents with record links

The console renders every single `markdown` property as Markdown and edits
it in place: the record's body, a `markdown` row on the property sheet, and a
`markdown` field on the create and edit forms. `/` opens a block menu, and
`@`, ⌘K or a collection picked under `/` opens a record search. The value
stays plain Markdown. A record link is a Markdown link whose target is
`substrate://` followed by the record path (decision record 0137), and the
console renders it as the record's chip, linked to its page and titled with
its current title. A writer that means a record in prose (an agent, a script)
writes the same link.

```markdown
Agree the roadmap with [Ada Lovelace](substrate://ada.example.com/people/person/ada).
```

The link is prose, not a reference property: the substrate does not index it,
and the linked record's "Connected to" list does not show it. A repeated
`markdown` property still edits one plain-text item per line.

## [0.106.1](https://github.com/geoah/substrate/compare/v0.106.0...v0.106.1) (2026-09-28)

### Fixed

* **google:** log why a Drive plan failed ([#723](https://github.com/geoah/substrate/issues/723)) ([d904b3e](https://github.com/geoah/substrate/commit/d904b3ec1c8a6d7ad1fa21ffe59e68e9651a903d))
* **api:** stop logging client disconnects as errors, name the route ([#724](https://github.com/geoah/substrate/issues/724)) ([3f51bf8](https://github.com/geoah/substrate/commit/3f51bf8bbb80f3c2a9e37020242cba0cc4c66298))
* **sync:** stop stamping other packages' runs on sync accounts ([#725](https://github.com/geoah/substrate/issues/725)) ([61f2cd7](https://github.com/geoah/substrate/commit/61f2cd7ff3eb16f3a08654fa0c6e0a852e207e42))
* **sync:** clear syncError after a good run or a reconnect ([#726](https://github.com/geoah/substrate/issues/726)) ([2fcadbf](https://github.com/geoah/substrate/commit/2fcadbfe40652237834feb7abb81917729e2476e))
* **sync:** count parked schedule runs and return the latest reason ([#728](https://github.com/geoah/substrate/issues/728)) ([c694c1b](https://github.com/geoah/substrate/commit/c694c1bf49d07479653ceb7d56de11c7d9433642))
* **oauth:** store why a token refresh failed on the account ([#727](https://github.com/geoah/substrate/issues/727)) ([fe61a5b](https://github.com/geoah/substrate/commit/fe61a5b2a07e33773d2f739a83fb4686eff1f108))
* **github:** resume the pull request walk where the last run stopped ([#729](https://github.com/geoah/substrate/issues/729)) ([669ba85](https://github.com/geoah/substrate/commit/669ba8524389e589dfcc9a0659a82895e3166477))
* **agents:** settle threads a restart left running ([#730](https://github.com/geoah/substrate/issues/730)) ([2b0685e](https://github.com/geoah/substrate/commit/2b0685ea397ab60914af7b037b8985f075d6e4d9))
* **slack:** skip refused channels and items on later runs ([#731](https://github.com/geoah/substrate/issues/731)) ([bda093b](https://github.com/geoah/substrate/commit/bda093bc2f9185597aa73d51eef4c6aff1eaa9f5))
* **console:** show permission globs as patterns, not records ([#734](https://github.com/geoah/substrate/issues/734)) ([e1191ae](https://github.com/geoah/substrate/commit/e1191ae051349db2d94cd4e649d20645418c0ba7))
* **console:** let the owner change an agent's provider ([#735](https://github.com/geoah/substrate/issues/735)) ([34686c6](https://github.com/geoah/substrate/commit/34686c660a018936151a65043ad1a45e2a02b398))
* **console:** show an agent's package as read-only ([#736](https://github.com/geoah/substrate/issues/736)) ([2c72481](https://github.com/geoah/substrate/commit/2c724810841f7d1d2f2d06ed7c17b2549efcbca1))

### Upgrade notes

#### A restart settles the agent runs it interrupted, and a live run lists as running

An agent run that a server stop cut short used to stay `running` on its
`substrate.reamde.dev/llm/thread` forever, and its trigger delivery stayed
listed under `…/parked` with the error `delivery in flight: an agent run a
restart interrupted stays here, retried by hand`. A run that started after
the restart was listed with the same error.

At its first open of a repository, the server now:

- settles every `running` thread to `status: error` with `reason:
  interrupted: the server stopped during the run`;
- rewrites each interrupted delivery's error to `interrupted: the server
  stopped during this agent run, and nothing reruns it by itself; read its
  thread, then retry this delivery to run the agent again, or forget it`.

Nothing reruns an interrupted delivery by itself, as before (decision record
0064). A delivery the server is running right now lists with `running: true`
and the error `delivery in flight: an agent run is running it now`, and
`GET …/trigger/status` (`substratectl trigger status`) counts it under the new `inFlight` field
instead of `parked`:

```json
{"id": "reflect-daily", "parked": 1, "pending": 0, "inFlight": 1}
```

To finish an interrupted run, read its thread, then retry or forget the
parked row:

```
substratectl trigger parked <trigger>
substratectl trigger retry <trigger> <parked-id>
substratectl trigger forget <trigger> <parked-id>
```

## [0.106.0](https://github.com/geoah/substrate/compare/v0.105.0...v0.106.0) (2026-09-27)

### Added

* **skills:** add the substrate-runbook-upgrade agent skill ([#722](https://github.com/geoah/substrate/issues/722)) ([f2dad01](https://github.com/geoah/substrate/commit/f2dad0177581a32302707779a3382bd66cce04e0))

## [0.105.0](https://github.com/geoah/substrate/compare/v0.104.3...v0.105.0) (2026-09-27)

### ⚠ BREAKING CHANGES

* **console:** redesign the console around your data, providers, agents and tools ([#648](https://github.com/geoah/substrate/issues/648)) ([9f7d300](https://github.com/geoah/substrate/commit/9f7d3006a4f071387db3a11bad3844172f511dd7))

### Added

* **console:** redesign the console around your data, providers, agents and tools ([#648](https://github.com/geoah/substrate/issues/648)) ([9f7d300](https://github.com/geoah/substrate/commit/9f7d3006a4f071387db3a11bad3844172f511dd7))

### Upgrade notes

#### A kind declares its purpose, and an older binary cannot read one that does

A kind may now declare `purpose: primary | supporting | internal`: why it
exists, so a client can decide what its navigation lists. Absent reads as
primary, and the server acts on nothing but the value's validity
(decision record 0133). The console's sidebar lists primary kinds and puts
the rest behind Technical details. Core's `kind` kind declares the key
(version 19), and the shipped providers and samples now carry it, so their
package versions were bumped.

A declaration's key set is closed. A binary from before this release
quarantines a package whose stored declarations carry `purpose`, and refuses
to open a repository whose core does. Once a repository has booted under
this release, going back to an older binary is not possible.

##### What to do

1. Operators: upgrade every server that opens a repository before any of
   them boots this release, and do not roll back past it afterwards.
2. Authors: add `purpose: supporting` to a kind a person would not browse
   as its own collection (a sync state, a cursor, a join row), and
   `purpose: internal` to machinery. Leave the key off a kind that is a
   thing a person keeps.
3. Accept the upgrades offered for shipped providers and samples.

## [0.104.3](https://github.com/geoah/substrate/compare/v0.104.2...v0.104.3) (2026-09-27)

### Fixed

* **slack:** read a day behind the history cursor to find first replies ([#717](https://github.com/geoah/substrate/issues/717)) ([1f94311](https://github.com/geoah/substrate/commit/1f943116f8c849ee9bf8d13a1f4605a2c0a86bb3))

## [0.104.2](https://github.com/geoah/substrate/compare/v0.104.1...v0.104.2) (2026-09-27)

### Fixed

* **github:** find pull requests the owner reviewed ([#715](https://github.com/geoah/substrate/issues/715)) ([956cf6a](https://github.com/geoah/substrate/commit/956cf6a56aef374deb3532e6ec00e26d120d0077))

### Upgrade notes

#### GitHub mirrors the owner's review of a pull request they were asked to review

`githubsync` adds a fourth search, `type:pr reviewed-by:<login>`, as the
`pullsReviewed` stage with its own `syncCursors` entry. GitHub drops a user
from a pull request's requested reviewers once they review it, so before this
an approval sent through `submitreview` on a pull request the owner was only
asked to review never reached the `review` mirror (#710). It now lands on the
next sync, for example:

```http
GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/github/review"],"properties":{"state":{"eq":"approved"}}}
```

The first sync on version 26 walks the new search from the account's
`backfillDepth` floor, because the stage has no watermark yet.

## [0.104.1](https://github.com/geoah/substrate/compare/v0.104.0...v0.104.1) (2026-09-27)

### Fixed

* **vocabulary:** refuse an enum argument outside its values ([#714](https://github.com/geoah/substrate/issues/714)) ([b8bebd4](https://github.com/geoah/substrate/commit/b8bebd4762489b10ef75646d9e7819cf5a24c7c6))

### Upgrade notes

#### A function's `enum` argument refuses a value outside its `values`

Before this release, an argument declared `type: enum` checked only that the
value was a string, so a call passing `period: wekly` to
`values: [daily, weekly, monthly]` ran the body with the typo. The engine now
refuses it with `422 validation` on the call API, a host call, an agent's
function tool and a schedule trigger write:

```
.period: "wekly" is not one of the allowed values: daily, weekly, monthly
```

A schedule trigger written earlier with such a value is refused at its next
fire, which parks the occurrence after one attempt.

##### What to do

1. List parked deliveries on schedule triggers
   (`GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked`) and look for
   the message above.
2. Correct the trigger's `arguments` to one of the listed values, then retry
   the parked delivery.

## [0.104.0](https://github.com/geoah/substrate/compare/v0.103.0...v0.104.0) (2026-09-27)

### Added

* **engine:** let a function body run an agent it grants ([#692](https://github.com/geoah/substrate/issues/692)) ([e4a1155](https://github.com/geoah/substrate/commit/e4a1155b337999ef8a6f8524a1e122e55fee3b36))

### Upgrade notes

#### A function body runs an agent granted under `permissions.agents`

A function that names an agent under `permissions.agents` can run it with
`host.agents.call(agent, input)` and read `{reply, thread, status}`. A classifier
no longer needs a staging row and a record trigger to hand a candidate to an
agent. The agent's writes commit
as it runs and stay if the caller fails afterwards, so a delivery claims itself
when its agent opens a thread, parks instead of retrying when it fails after
that, and a keyed call binds its
`Idempotency-Key` to the agent's thread. The function is not delivered the
writes of the agents it grants. When an agent runs the function as a tool, the agent the body runs is
held to the calling agent's effective emit. The agent runs inside the
caller's `timeout` (at most 60s) and settles its thread when that passes.

```yaml
permissions:
  agents:
    - example.com/commerce/extractorder
timeout: PT60S
```

```python
def main(input, host):
    out = host.agents.call("example.com/commerce/extractorder", input["args"])
    return {"output": {"reply": out["reply"]}}
```

## [0.103.0](https://github.com/geoah/substrate/compare/v0.102.0...v0.103.0) (2026-09-27)

### Added

* **engine:** write a triggerrun row per networked direct call ([#688](https://github.com/geoah/substrate/issues/688)) ([eb8e8d9](https://github.com/geoah/substrate/commit/eb8e8d9d156c898719d6345e13a3ef4fe04d2da8))
* **api:** return run summaries from the changes read with runs=1 ([#704](https://github.com/geoah/substrate/issues/704)) ([7de679a](https://github.com/geoah/substrate/commit/7de679a9eabd8266590ea1e629f4f4397b3105ac))
* **engine:** collect a record on `DELETE ?purge=true` ([#695](https://github.com/geoah/substrate/issues/695)) ([0087921](https://github.com/geoah/substrate/commit/0087921f7b041b8ec9a95b07103a55008d193d6c))

### Fixed

* **engine:** store a mapped mirror reference as its subject, once ([#690](https://github.com/geoah/substrate/issues/690)) ([f606839](https://github.com/geoah/substrate/commit/f606839eb6f21edd5b04aa34cc078bc32db72a6f))
* **engine:** read only a record trigger's kinds from the changelog ([#699](https://github.com/geoah/substrate/issues/699)) ([98e93cc](https://github.com/geoah/substrate/commit/98e93ccd979a46d16e22dac785d341c5b601d175))

### Upgrade notes

#### `GET /api/v1/changes?runs=1` returns run summaries

The history page now has a run form: consecutive changes of the filtered feed
grouped by actor, kind and verb (`create`, `restore`, `update`, `delete`,
`merge`, `split`, `gc`), each with an exact `count`, its distinct `records`,
`newestSeq`/`oldestSeq` and `newestTs`/`oldestTs`. `first` counts runs, and a
page never ends inside one, so a client can say "60 tasks" from one read
instead of folding rows over several pages:

```
GET /api/v1/changes?runs=1&first=6
GET /api/v1/changes?runs=1&first=6&before=4128&generation=7f3a0c2e9b1d4e6f
```

`cursor` is the oldest run's `oldestSeq` when more rows lie below.
[docs/changelog.md](docs/changelog.md#run-summaries) has the rules.

#### `DELETE ?purge=true` collects a record now, so the next `put` is fresh

A plain `DELETE` leaves a tombstone until the garbage collector's next pass,
and a `put` at the same id in that window restores the row with every
property it held: a mirror's old subject, an account's `lastSyncedAt`,
cursors and `syncStatus`. `purge=true` collects the record in the delete
itself, so the next `put` at the id is a new record at version 1, and a
mapping source resolves its subject again through the probes. The old
subject is not deleted or re-pointed. A record a finalizer holds answers
`409 conflict` and nothing changes: delete it without `purge`, wait for the
finalizers to release, then purge. A purge through a merge loser's former id
answers `409 conflict` naming the canonical id, and a declaration record
answers `422 validation`.

A put that names the subject slot keeps that subject: the probes run only
when the put leaves the slot out. To resolve a Google contact mirror again
after the `googlecontactperson` mapping's probes improve, purge the mirror
and put it back without the `person` slot. Until the mirror is written
again, the person loses the names, emails and phones only this contact gave
it.

For a few contacts, put each one back yourself. Read the record first and
send its `properties` with `person` removed. `PUT` takes only
`{"properties": ...}`, so sending the whole document the `GET` returned
(with `version`, `createdAt` and `updatedAt`) answers `400 bad_request`:

```http
GET    /api/v1/providers.substrate.reamde.dev/google/contact/c1
DELETE /api/v1/providers.substrate.reamde.dev/google/contact/c1?purge=true
PUT    /api/v1/providers.substrate.reamde.dev/google/contact/c1
       {"properties": {"account": {"ref": "..."}, "resourceName": "people/c1", "emailAddresses": [...]}}
```

Waiting for the next scheduled sync does not bring the contact back: that
run reads only the People changes since the stored `contactsSyncToken`, so
it writes a purged contact again only once the contact changes upstream. To
make the connector write it, purge the mirror and then stamp
`syncRequestedAt` on the account the contact's `account` ref names:

```http
DELETE /api/v1/providers.substrate.reamde.dev/google/contact/c1?purge=true
PATCH  /api/v1/providers.substrate.reamde.dev/google/account/<account-id>
       {"properties": {"syncRequestedAt": "2026-09-26T12:00:00Z"}}
```

The request drives a full read on every enabled stream of that account
(contacts, Gmail, Calendar and Drive), not only contacts. The contacts read
writes each purged contact as a fresh record, and the probes resolve it.
Use it when many contacts were purged, and budget for the other streams'
full reads.

The CLI covers the delete only,
`substratectl delete --purge providers.substrate.reamde.dev/google/contact c1`.
A document from `substratectl get -o yaml` carries `data.properties.person`,
so remove that line before `substratectl apply -f`, or the contact points at
the old person again.

#### A direct call of a networked function writes a `substrate.reamde.dev/core/triggerrun` row

A `POST …/core/function/{name}/call` of a function that declares
`permissions.network`, or whose `permissions.call` grant reaches one that
does, now writes one `substrate.reamde.dev/core/triggerrun` row with
`mode: call` and no `trigger` (decision record 0119). The row holds the
`callableRef`, the `caller`, the `principal` (the token id), `startedAt`,
`finishedAt`, `status` (`ok`, or `failed` when the body ran and failed), the
applied `effects`, `outputBytes`, and `output` when it is at most 4096 bytes
of JSON and carries no NUL (U+0000). A `caller` whose `X-Substrate-Actor`
header is not valid UTF-8 is stored with each bad byte replaced by U+FFFD.
Call runs are never pruned.

A client that lists `triggerrun` rows and assumed `trigger` is always set
must allow for its absence. To read every direct call of one function:

```http
GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/triggerrun"],"referencing":{"ref":"substrate.reamde.dev/core/function/{name}","property":"callableRef"},"properties":{"mode":{"eq":"call"}}}
```

#### A map rule carries a mirror reference onto its subject

A map rule that copies a mirror reference into a slot pinned at the mirror's
subject kind stores the subject, and a read now shows the rule's source
record as the value's source, with no alternative. Two mirrors of one subject
land in a repeated target once. A mapping that wants a task's assignee copies
the issue's `github/user` reference, and a path through it
(`assignees[].person`) is refused, naming this spelling (decision record
0120):

```yaml
  map:
    assignee:
      path: assignee      # github/user on the issue, person on the task
```

##### What to do

A function that patches the assignee onto a mapped task after the fact can
be replaced by the map rule above.

#### `trigger replay` reads only the trigger's kinds from the changelog

A record trigger's drain, and a replay from seq 0, read only the entries of
the kinds the trigger's source names, at most 200 of each kind per batch,
and skip every other entry in one cursor step (decision record 0123). A
replay such as

```sh
substratectl trigger replay extractlinks-on-calendarevent
```

over one small kind in a large repository reads that kind's entries instead
of the whole changelog. A `*` source still reads every entry.

The first boot on this release runs migration 0007 when the engine opens,
before it serves any repository. It builds the `changelog (repository, kind,
seq)` index over the shared changelog table, every repository's entries at
once, and drops `changelog_kind_idx`. The new index holds one full tuple
per changelog entry, so it takes more disk than the index it replaces,
which deduplicated its repeated keys. The build holds writes to the
changelog while it runs, so on a host with millions of changelog entries in
total every repository starts later than usual on that boot. Nothing needs
doing.

## [0.102.0](https://github.com/geoah/substrate/compare/v0.101.0...v0.102.0) (2026-09-27)

### Added

* **providers:** ship Slack, Beeper and GitHub write functions ([#703](https://github.com/geoah/substrate/issues/703)) ([e942967](https://github.com/geoah/substrate/commit/e942967a1cb5fc1c4e62f4eb7faa8085b4393e7b))
* **engine:** project only the sources a recordmapping's where covers ([#687](https://github.com/geoah/substrate/issues/687)) ([0d0e049](https://github.com/geoah/substrate/commit/0d0e049245d13be15ffadfa5ac32a3ab2108f50a))

### Upgrade notes

#### A `recordmapping` takes `where` to project only some source records

A mapping may name which records of its source kind it covers, one filter
condition object per declared source property. A record outside the `where`
links no subject and mints none, and contributes nothing; one that leaves it
releases what it projected, and its subject is orphan-marked like one whose
source was deleted. A bare value (`state: open`) is refused: write the
condition object, as `filter.properties` takes it. A declared property named
`createdAt`, `updatedAt`, `deletedAt`, `id` or `version`, and a condition that
tests nothing (`eq: null`, `in: []`), are refused too.

```yaml
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: <authority>/tasks/pullrequesttask
data:
  authority: <authority>
  package: tasks
  from: providers.substrate.reamde.dev/github/pullrequest
  to: <authority>/tasks/task
  property: task
  where:
    state:
      eq: open
  map:
    name:
      path: title
```

#### Slack, Beeper and GitHub providers ship `postmessage`, `sendmessage` and `submitreview`

Each of the three providers ships one write function, callable through
`POST /api/v1/substrate.reamde.dev/core/function/{name}/call` or named as an
agent tool. No trigger fires them. Each spends the credential the provider's
sync already uses and writes no record; what it sent reaches the mirror only
when the sync reads it back. Two sends are never read back: a Slack first
reply in a thread that had no replies (#711), and a GitHub review by an owner
who was only asked to review (#710). A `200` answer is the confirmation: do
not retry a call because its message or review is missing from the mirror.

- `providers.substrate.reamde.dev/slack/postmessage` takes `channel`, `text`
  and an optional `threadTs`. The pasted user token needs Slack's
  `chat:write` user scope: a token minted for the read-only sync lacks it,
  and the call then fails with `missing_scope` until the token is reminted
  with the scope and pasted on the `config` record again.
- `providers.substrate.reamde.dev/beeper/sendmessage` takes `chat`, `text` and
  an optional `replyTo`.
- `providers.substrate.reamde.dev/github/submitreview` takes `repository`
  (`owner/name`), `number`, `event` (`approve` or `comment`), `body` and an
  optional `account`. It needs the `repo` scope, which the pull request,
  issue and repository toggles already ask for.

```http
POST /api/v1/substrate.reamde.dev/core/function/providers.substrate.reamde.dev%2Fslack%2Fpostmessage/call
Authorization: Bearer <token>
Content-Type: application/json

{"input": {"channel": "C0123456789", "text": "On it.", "threadTs": "1747674380.100249"}}
```

## [0.101.0](https://github.com/geoah/substrate/compare/v0.100.0...v0.101.0) (2026-09-27)

### ⚠ BREAKING CHANGES

* **engine:** compute occurrences in function and agent window lists ([#689](https://github.com/geoah/substrate/issues/689)) ([f4b9589](https://github.com/geoah/substrate/commit/f4b958972992e1f424bff3747f99ee2d00c7dda9))

### Added

* **engine:** pass a schedule trigger's arguments to its function ([#691](https://github.com/geoah/substrate/issues/691)) ([d3b7388](https://github.com/geoah/substrate/commit/d3b73880803c17b96b36cfc4c329b48051a71e1a))
* **engine:** match a probe in any case with fold: case ([#694](https://github.com/geoah/substrate/issues/694)) ([286ef71](https://github.com/geoah/substrate/commit/286ef71ec0c203c4edb3c5d7ed353a5e79cd89d5))
* **engine:** compute occurrences in function and agent window lists ([#689](https://github.com/geoah/substrate/issues/689)) ([f4b9589](https://github.com/geoah/substrate/commit/f4b958972992e1f424bff3747f99ee2d00c7dda9))
* **vocabulary:** let a kind declare its display label ([#707](https://github.com/geoah/substrate/issues/707)) ([567611e](https://github.com/geoah/substrate/commit/567611ef06f311acdf45c40ea227d24d8bd7ea90))

### Fixed

* **engine:** release an actor's holds when it is declared machine ([#696](https://github.com/geoah/substrate/issues/696)) ([871628c](https://github.com/geoah/substrate/commit/871628cbabf91e5319f9856112eda4afd32e1100))
* **providers:** fire sync now until the request is acknowledged ([#713](https://github.com/geoah/substrate/issues/713)) ([39d33ba](https://github.com/geoah/substrate/commit/39d33ba568777d784514d47627cdfdda2006239c))

### Upgrade notes

#### A function's `host.records.list` and an agent's `query` bounded on `at` compute occurrences

A function body's `host.records.list` (and `host.list`) and an agent's
`query` tool whose filter bounds `at` on both ends now answer the records
route's window read (decision record 0111). The page carries the plain
events and overrides in the window and, merged by slot, the occurrences
computed from every series among the kinds read, each with
`computed: true`, `version: 0` and the id `<seriesId>_<slot>`. Series rows
leave the page: a series whose own `at` falls in the window used to be
listed as a row, and now only its occurrences appear, without `recurrence`,
`rdates` or `exdates`.

```python
page = host.records.list(["providers.substrate.reamde.dev/google/calendarseries",
                          "providers.substrate.reamde.dev/google/calendarevent"],
                         where={"at": {"gte": "2026-09-24T00:00:00Z",
                                       "lt": "2026-09-25T00:00:00Z"}})
# before: stored events and any series anchored that day, newest created first
# after:  stored events plus {"id": "abc_20260924T130000Z", "computed": True, ...},
#         `at` ascending, no series rows
```

The window read's rules now apply to such a list:

- `order` is `at` alone (ascending or descending); with no `order` the page
  is `at` ascending, not newest created first.
- `offset` is refused.
- A window whose `gte` is not before its `lt` is refused.
- A list over kinds none of which binds `temporal` is refused; it used to
  return an empty page.
- More than 10,000 matching series is refused; narrow the filter.

Bounds take the same forms as on a plain list: an RFC 3339 instant, a
zone-less date-time or a bare date, the last two read as UTC.

##### What to do

1. Delete any RRULE expansion a body runs over a two-sided `at` list, or it
   will see each occurrence twice.
2. Treat a `computed: true` row as read-only. Skip it when writing back
   what you listed, or write the series under its own id. A patch at a
   computed id fails with `not found`, and a put at one creates a new
   record (an override where the kind binds `override`), so put at a
   computed id only to materialize that one occurrence on purpose.
3. Read series rows themselves with a one-sided `at` bound or by `ids`.
4. Drop an `order` other than `at` from such a list, and page it with
   `after` instead of `offset`.
5. A paged body whose stored continuation holds a list cursor from before
   the upgrade gets `bad cursor: not a window cursor`; restart that walk
   from the first page.
6. Lists bounded on one end of `at`, or not on `at` at all, are unchanged.

#### A kind declares a display label, and the vocabulary read carries it

A kind declaration may carry `label:` with `singular` and `plural`, the words
a client shows a person for one record and for the collection. The kind read
(`KindInfo`) returns it as `label`, absent when the kind declares none. The
shipped Slack `conversation` and `user`, Google `contactgroup` and
`calendarseries`, and the four `*sync` state kinds declare one (decision
record 0117).

A client reads the kind list with
`GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/kind"]}` and
finds the label on each declaration record at `properties.label`:

```json
{
  "id": "providers.substrate.reamde.dev/slack/conversation",
  "properties": {
    "label": {"singular": "Channel", "plural": "Channels"}
  }
}
```

`GET /api/v1/substrate.reamde.dev/core/trait/{id}/implementors` returns the
flat `KindInfo` shape, with the label at top-level `label`. A client shows
`label.plural` as the collection heading and `label.singular` for one record,
and falls back to humanizing the kind's name when `label` is absent.

#### A `recordmapping` probe matches in any case with `fold: case`

A probe compares exactly, so `Ada Example` and `ada example` are two people
and the second spelling mints a second subject. A probe that declares
`fold: case` lowercases and trims both the source value and the target's
stored value before comparing them (decision record 0116):

```yaml
data:
  match:
    - from: realName
      to: name
      fold: case
```

A probe without the key is unchanged. Turning the key on for an existing
mapping makes its probe ambiguous wherever case-variant duplicates already
exist, so under the default `onAmbiguous: park` new sources park until the
owner merges those duplicates. The stored side folds under the database's
locale, so a `C` locale folds only ASCII.

#### A schedule trigger's `arguments` reach its function as `input["args"]`

A `substrate.reamde.dev/core/trigger` with a `schedule` source and a
function callable may carry `arguments`, a map checked against the
function's declared `arguments:` at write time and at every fire. One
function can now serve several schedules instead of one entrypoint each.

```yaml
kind: substrate.reamde.dev/core/trigger
metadata:
  id: rollup-weekly
data:
  properties:
    source:
      schedule:
        recurrence: FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0;BYSECOND=0
        timezone: Europe/Athens
    arguments:
      period: weekly
    callable: substrate.reamde.dev/core/function/example.com/rollup/run
```

The body reads `input["args"]["period"]`. A record or webhook source, an
agent callable, or a function that declares no `arguments:` is refused with
`422`.

#### Declaring an actor at `tier: machine` releases what it already holds

A property written under an actor that held above the machine tier (an
undeclared `X-Substrate-Actor` name holds at the owner tier) stayed pinned
against mapping recompute after the actor was declared at `tier: machine`.
Now the apply that declares it, and every later apply of the package that
declares it, recomputes every mapped record the actor holds above machine.
`propertyMeta` reports such a row at the `machine` tier.

The release follows recompute's machine-tier rule, so it can delete data:

- A mapped property the actor wrote that no live source offers is deleted,
  unless the kind declares it required. A person the import wrote that no
  mirror links loses its imported mapped values.
- A `merge: union` property keeps only its sources' items: an imported
  address no mirror carries goes.
- A record with no live source whose only other rows are the actor's is
  marked orphaned, and with `SUBSTRATE_ORPHAN_GRACE` set the GC sweep may
  collect it, unmapped properties included.

To keep an import whatever its sources say, write it as a source kind
instead ([docs/projection.md](docs/projection.md#contributing-a-value)).

A repository where the actor was already declared at `tier: machine` before
this release releases nothing at boot: each record moves at its next source
write. To release everything now, apply the package that declares the actor
again, unchanged. The actor goes in a package you own, header document
included:

```yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: alice.example.com/imports
data:
  authority: alice.example.com
  package: imports
  version: 1
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: importer
data:
  authority: alice.example.com
  package: imports
  tier: machine
```

```
GET /api/v1/alice.example.com/people/person/<id>
  propertyMeta.emails = {manager: "<the winning source's actor>", tier: "machine"}
```

Declaring an actor above the machine tier still pins only what it writes
next. Decision record 0113 has the rule.

#### Slack, Beeper and Notion sync while `syncRequestedAt` is unacknowledged

The on-demand triggers of the Slack (package version 10), Beeper (17) and
Notion (15) bundles fire while the account's `syncRequestedAt` differs from its
`syncRequestedAck`, the rule the Google, GitHub and Linear bundles already
follow. They used to fire on `syncRequestedAt > lastSyncedAt`, which CEL
compares as strings. A Slack request stamped in the same second as the
whole-second `lastSyncedAt` sorted below it (`"…:30.84Z" < "…:30Z"`) and never
ran, so a Sync now pressed within a second of a finished run did nothing.

A request that a bounded or failed run did not acknowledge stays open: the next
write to the account by anyone other than the sync fires the trigger again.

```bash
substratectl patch providers.substrate.reamde.dev/slack/account/owner \
  --prop syncRequestedAt=2026-09-26T19:28:30.844008Z
# fires even when lastSyncedAt is 2026-09-26T19:28:30Z
```

## [0.100.0](https://github.com/geoah/substrate/compare/v0.99.0...v0.100.0) (2026-09-26)

### Added

* **engine:** let the owner adjust a change request on accept ([#706](https://github.com/geoah/substrate/issues/706)) ([85b3022](https://github.com/geoah/substrate/commit/85b30227f329c5ab802b31f814aba7166f3aeae2))

### Upgrade notes

#### A change request's accept takes `adjustedDiff`, the owner's values

The owner's accept of a `substrate.reamde.dev/core/recordpatchrequest` may
carry `adjustedDiff` beside `decision: accepted`. The accept applies it
instead of `diff`, with the same checks, and stores it on the request, where
`diff` keeps what was proposed. `adjustedDiff` replaces `diff` whole: name every
value to apply, since a proposed property it omits is not applied. An
`ifVersion` inside it is the accept's check on the target; without one the
accept checks the request's `targetVersion`. A patch or create request only;
installed code and the policy judge are refused with `403`.

```http
PATCH /api/v1/substrate.reamde.dev/core/recordpatchrequest/r41c
{"ifVersion": 1, "properties": {"decision": "accepted",
  "adjustedDiff": {"properties": {"priority": "urgent", "dueAt": "2026-10-01T17:00:00Z"}}}}
```

## [0.99.0](https://github.com/geoah/substrate/compare/v0.98.0...v0.99.0) (2026-09-26)

### Added

* **engine:** record the actor that first declares a package ([#705](https://github.com/geoah/substrate/issues/705)) ([0dc7bf3](https://github.com/geoah/substrate/commit/0dc7bf3d008d6d59f9f5815865bf1b41da734732))

### Upgrade notes

#### A `package` row names who declared it in `declaredBy`

The row the engine creates for a new package carries the managed
`declaredBy` property: the actor of the write that created it, for example
`bundle:core`, `bundle:<authority>:<package>`, `console`, `substratectl`,
`api`, `substrate` or `agent:<authority>:<package>:<name>`. Read it from the
package's record:

```
GET /api/v1/substrate.reamde.dev/core/package/<authority>%2F<package>
```

It is in `properties.declaredBy`. A later apply never changes it, and no
document or PUT may write it. A package created before this release carries
none (decision record 0109).

## [0.98.0](https://github.com/geoah/substrate/compare/v0.97.1...v0.98.0) (2026-09-26)

### Added

* **engine:** let an allow policy lift the gate it names ([#702](https://github.com/geoah/substrate/issues/702)) ([e6f4695](https://github.com/geoah/substrate/commit/e6f46954b8dc7c4c7a7e2a0496237003590838ad))

### Upgrade notes

#### A `recordpatchpolicy` allow can lift the gate it names with `overrides`

An `allow` with `overrides` naming a gate policy lands the writes both match
instead of gating them, for exactly one agent, one kind reference and one op.
A refuse still wins, and any other matching gate still holds the write.
Before this, an allow never outranked a gate, so "always allow this agent"
had nothing to write.

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: taskbot-may-put-tasks
data:
  properties:
    selector:
      kinds:
        - samples.substrate.reamde.dev/tasks/task
      ops:
        - put
      agents:
        - crew.example.com/bots/taskbot
    action: allow
    overrides: gate-tasks
```

The write is refused with `422` when the allow's selector is wider, or when
`overrides` names a missing policy, the allow itself, or a policy that is not
a gate. Delete the allow, or set `disabled: true`, to revoke it; disabling is
admitted even after the named gate is gone.

## [0.97.1](https://github.com/geoah/substrate/compare/v0.97.0...v0.97.1) (2026-09-26)

### Fixed

* **engine:** link existing sources when a mapping is applied ([#697](https://github.com/geoah/substrate/issues/697)) ([f333f35](https://github.com/geoah/substrate/commit/f333f350f83557d1c8a63c978b4c221684326bff))

### Upgrade notes

#### Applying a `recordmapping` links the sources that already exist

The vocabulary apply that admits, changes or re-applies a `recordmapping` now
links every live source whose subject slot is empty or names a deleted
record, in the same transaction:
one probe candidate links, none mints a subject, and a source that offers
nothing or parks on an ambiguous probe stays unlinked (decision record 0107).
Before, a mirror synced before its mapping existed kept an empty slot until the
provider wrote that row again.

To link sources an earlier release left unlinked, apply the mapping again:
the `recordmapping` document alone is enough, and for a package imported from
the catalog, so is re-importing the sample.

```bash
substratectl apply -f people.yaml    # any batch that names the recordmapping
```

A second apply with nothing left to link writes nothing.

## [0.97.0](https://github.com/geoah/substrate/compare/v0.96.5...v0.97.0) (2026-09-26)

### Added

* **vocabulary:** let apply hold back mappings with no provider ([#693](https://github.com/geoah/substrate/issues/693)) ([9e93855](https://github.com/geoah/substrate/commit/9e938558af5ea86a401a3b7b4cb3a1cdbcf0b916))

### Fixed

* **engine:** render state, instant and body tokens in a displayTemplate ([#683](https://github.com/geoah/substrate/issues/683)) ([11bc7c0](https://github.com/geoah/substrate/commit/11bc7c05f33b351b9de3c953ccab68937bf9645c))
* **dev:** wait out a slow boot import instead of killing it ([#698](https://github.com/geoah/substrate/issues/698)) ([dc8220b](https://github.com/geoah/substrate/commit/dc8220bfed0bc03491fd84591328fa6a607c6ff7))

### Upgrade notes

#### `vocabulary/apply` holds back a mapping whose provider is absent, on request

A batch whose `recordmapping` names a source kind this repository does not
have is refused whole. With `"holdWaitingMappings": true` in the body of
`POST /api/v1/vocabulary/apply`, the door holds back each suggested mapping
(onto the declaring package's own kind from another package's kind) whose
source kind is absent, applies the rest, and lists what it held in
`heldMappings` with state `waiting` (decision record 0106). `POST
/api/v1/vocabulary/plan` takes the same key and previews the batch without the
held mappings; its response does not list them. A mapping whose source is
present but does not fit it still refuses the batch.

```bash
substratectl apply -f people.yaml --hold-waiting-mappings
# recordmapping/ada.example.com/people/slackuserperson held: waits on providers.substrate.reamde.dev/slack/user from providers.substrate.reamde.dev/slack (install it, then apply again)
```

Apply the same files again after installing the provider to land the mapping.

## [0.96.5](https://github.com/geoah/substrate/compare/v0.96.4...v0.96.5) (2026-09-26)

### Fixed

* **engine:** share one capped Postgres pool across all repositories ([#664](https://github.com/geoah/substrate/issues/664)) ([8b77be3](https://github.com/geoah/substrate/commit/8b77be3c3ab3cee94b2ba35ac604d0a753548d1d))

### Upgrade notes

#### Repositories share one Postgres pool, capped by `SUBSTRATE_REPOSITORY_CONNECTIONS`

A server used to open a connection pool per repository and close none of
them, so a host with a few hundred repositories ran Postgres out of
connections and every read failed with `sorry, too many clients already`.
Every repository now draws from one pool of `SUBSTRATE_REPOSITORY_CONNECTIONS`
connections (default `16`, at least `4`), one repository takes at most half
of the pool and never more than eight, and a process holds at most the cap
plus 12: 4 for its admin pool, 5 for its maintenance pool, 2 for repository
migrations and 1 for a commit-time catch-up. The default holds a process to
28.

A deployment running up to three servers on a Postgres with the stock
`max_connections=100` fits them in 3 x (18 + 12) = 90, with 10 left for
operator sessions:

```
SUBSTRATE_REPOSITORY_CONNECTIONS=18
```

The shared pool publishes `substrate_db_pool_*` series under
`pool="repositories"` at `GET /metrics` (with `SUBSTRATE_METRICS=true`).

## [0.96.4](https://github.com/geoah/substrate/compare/v0.96.3...v0.96.4) (2026-09-26)

### Fixed

* **engine:** confirm a lossy apply while unrelated records are written ([#665](https://github.com/geoah/substrate/issues/665)) ([87f0c12](https://github.com/geoah/substrate/commit/87f0c12abb9f71a2483e3d771f40ccf718a55bd4))

## [0.96.3](https://github.com/geoah/substrate/compare/v0.96.2...v0.96.3) (2026-09-26)

### Fixed

* **catalog:** keep versions when an unchanged closure is reinstalled ([#666](https://github.com/geoah/substrate/issues/666)) ([0fa83f1](https://github.com/geoah/substrate/commit/0fa83f13550e6eb0fd221137133f15ca34d2060f))

### Upgrade notes

#### Re-installing an unchanged closure writes nothing and keeps every version

`POST /api/v1/catalog/{id}/install` (and a hand `POST
/api/v1/vocabulary/apply` of the same files) over a closure the repository
already holds unchanged appends no changelog entry and keeps the package's
and every declaration's version. Before, each re-install moved the package
and every kind up one (whoop 13, then 14, then 15), because a function's
`permissions.writes`, a bundle input's `kind` and a `timeout` compared unequal
to the spelling the stored row holds. A tool may install on every deploy:

```bash
substratectl install providers.substrate.reamde.dev/whoop
```

A closure that changes one of those values, a single reference or a single
duration, still lands at stored+1.

## [0.96.2](https://github.com/geoah/substrate/compare/v0.96.1...v0.96.2) (2026-09-26)

### Fixed

* **engine:** dispatch each repository's triggers in its own lane ([#662](https://github.com/geoah/substrate/issues/662)) ([cebc1f4](https://github.com/geoah/substrate/commit/cebc1f4835d1d0bd8627839f77e66dd18f100e87))
* **engine:** cap each trigger's share of a dispatcher pass ([#663](https://github.com/geoah/substrate/issues/663)) ([3978cf7](https://github.com/geoah/substrate/commit/3978cf78d209935e277b94316fee23f50f7f73e6))

## [0.96.1](https://github.com/geoah/substrate/compare/v0.96.0...v0.96.1) (2026-09-26)

### Fixed

* **image:** ship the static upstream uv instead of Alpine's ([#660](https://github.com/geoah/substrate/issues/660)) ([c5f3b5a](https://github.com/geoah/substrate/commit/c5f3b5aa51baa2544d587d6a18b7d2739eea81db))
* **engine:** answer not found for a patch onto a tombstone ([#661](https://github.com/geoah/substrate/issues/661)) ([a6cde0b](https://github.com/geoah/substrate/commit/a6cde0b461dc97f157be4c2b9b8ee3f611575a72))

### Upgrade notes

#### `patch` on a deleted record answers `404` and writes nothing

A `PATCH` addressed to a tombstoned record (deleted, not yet collected) used
to succeed: the version climbed and the properties changed while
`deletedAt` stayed set, so every list still hid the row, and a mapping
source written that way minted subjects from it. It now answers
`404 not found` and changes nothing. A bundle function's `patch` effect onto
a tombstone fails its delivery the same way, as a patch onto a collected
record already did.

With the tasks sample imported into a repository whose authority is
`alice.example.com`:

```http
DELETE /api/v1/alice.example.com/tasks/task/t1
PATCH  /api/v1/alice.example.com/tasks/task/t1
       {"properties": {"name": "Renamed"}}
-> 404, error.code "not_found": record t1 is deleted; a put restores it
```

A `put` to the same path still restores the record. A patch that only
releases finalizers (`{"removeFinalizers": [...]}`) is still accepted on a
tombstone, because that is how a teardown lets the collector take it.

An effect `put` with `ifAbsent: true` now treats a tombstone as absent and
restores it, where it used to skip it. A function that mints a mirror with
`if_absent` and then patches it (the Slack, Notion, Linear, Beeper, Whoop and
GitHub providers do) therefore writes a deleted mirror back on its next sync.

##### What to do

1. A client that patches a record it may have deleted: send `PUT` with the
   whole record instead, or expect `404` and follow it with a `PUT`.
2. A provider function that patches a mirror it may have lost: put the
   record first, then patch it. `if_absent=True` is enough:

   ```python
   host.effects.put(kind, eid, properties=props, if_absent=True)
   host.effects.patch(kind, eid, properties=props)
   ```

## [0.96.0](https://github.com/geoah/substrate/compare/v0.95.2...v0.96.0) (2026-09-26)

### Fixed

* **cli:** say search --kinds takes full kind references ([#656](https://github.com/geoah/substrate/issues/656)) ([efffad9](https://github.com/geoah/substrate/commit/efffad9456207f946c6422afb1f2dd6345733e46))
* **release:** run the release task under sh, and read breaks the way svu does ([#658](https://github.com/geoah/substrate/issues/658)) ([b931036](https://github.com/geoah/substrate/commit/b9310367ef8ff95ef8f6b8263c032e21a73680cd))

## [0.95.2](https://github.com/geoah/substrate/compare/v0.95.1...v0.95.2) (2026-09-26)

### Fixed

* **slack:** pull new history before resuming the backlog walk ([#654](https://github.com/geoah/substrate/issues/654)) ([1be9e23](https://github.com/geoah/substrate/commit/1be9e23e9e7b7198322356a53c672d98be1defe3))

## [0.95.1](https://github.com/geoah/substrate/compare/v0.95.0...v0.95.1) (2026-09-26)

### Fixed

* **github:** clear a lifted org from syncSkipped when dropping cursors ([#652](https://github.com/geoah/substrate/issues/652)) ([2453ef3](https://github.com/geoah/substrate/commit/2453ef3c21fcfbaed4b173db9c867b3b60c2e1a5))
* **google:** give every due Gmail account a share of each sync run ([#653](https://github.com/geoah/substrate/issues/653)) ([20c23cb](https://github.com/geoah/substrate/commit/20c23cb345c468335423c0f9462e8148d1e83773))

## [0.95.0](https://github.com/geoah/substrate/compare/v0.94.1...v0.95.0) (2026-09-25)

### Added

* **engine:** add onAmbiguous and withhold probed values others hold ([#631](https://github.com/geoah/substrate/issues/631)) ([da41d5e](https://github.com/geoah/substrate/commit/da41d5e1bbac01b8a66eeca646a4979de9599c57))

## [0.94.1](https://github.com/geoah/substrate/compare/v0.94.0...v0.94.1) (2026-09-25)

### Fixed

* **engine:** a scalar reference filter is one indexed equality, and every scalar reference gets its index ([#628](https://github.com/geoah/substrate/issues/628)) ([8aba124](https://github.com/geoah/substrate/commit/8aba12458a40c9bd5288d2c21ac6ef4243b8a975))

## [0.94.0](https://github.com/geoah/substrate/compare/v0.93.1...v0.94.0) (2026-09-25)

### Added

* **metrics:** prometheus exposition at /metrics behind SUBSTRATE_METRICS ([#630](https://github.com/geoah/substrate/issues/630)) ([b90a07f](https://github.com/geoah/substrate/commit/b90a07f98bdfa8826ddc136d1294508ae4563457))

### Fixed

* **engine:** bind kind/id/actor lists as text[] and filter with = ANY instead of a jsonb_array_elements_text semi-join ([#596](https://github.com/geoah/substrate/issues/596)) ([6e5ee3a](https://github.com/geoah/substrate/commit/6e5ee3a74ee6f6eb36f647965c93bf858b2f7f1a))
* **engine:** a retry of a parked delivery that fails again answers 409 parked, not 500 ([#629](https://github.com/geoah/substrate/issues/629)) ([8b7043e](https://github.com/geoah/substrate/commit/8b7043ea989fddc856784fccda38c8e12566427c))

### Upgrade notes

#### A retry of a parked delivery that fails again answers `409` `parked`, not `500`

Clients and scripts retrying parked trigger deliveries are hit, including
`substratectl trigger retry`. When the retried delivery runs and fails
again, `POST /api/v1/substrate.reamde.dev/core/trigger/{id}/parked/{fid}/retry`
answered `500` `internal`. From v0.94.0 it answers `409` with code `parked`,
and the message names the new error's first line:

```
trigger <id>: parked delivery <fid> ran again and failed, it stays parked at attempt <n>: <first line of the error>
```

The row stays parked, one attempt older. A retry that succeeds still answers
`200` with `{"ran": …}` and deletes the row.

##### What to do

1. Treat `409` `parked` from the retry route as "the delivery still fails":
   fix the callable, then retry or forget the row
   (`DELETE …/trigger/{id}/parked/{fid}`).
2. Stop treating a `500` from this route as the failed-again outcome; a
   `500` now means a server fault.

## [0.93.1](https://github.com/geoah/substrate/compare/v0.93.0...v0.93.1) (2026-09-24)

### Fixed

* **google:** a split series keeps one row per occurrence ([#626](https://github.com/geoah/substrate/issues/626)) ([970e7ae](https://github.com/geoah/substrate/commit/970e7ae92b01cf467136121590f80b80f3d76260))

## [0.93.0](https://github.com/geoah/substrate/compare/v0.92.1...v0.93.0) (2026-09-24)

### ⚠ BREAKING CHANGES

* **oauth:** refuse a bare account id on oauth/start ([0c02ddd](https://github.com/geoah/substrate/commit/0c02ddddbc7a9ab0f01162d7386280b3734cd22f))

### Added

* **oauth:** refuse a bare account id on oauth/start ([0c02ddd](https://github.com/geoah/substrate/commit/0c02ddddbc7a9ab0f01162d7386280b3734cd22f))

### Upgrade notes

#### `oauth/start` refuses a bare account id

Clients starting an OAuth connect are hit, including scripts calling
`substratectl bundle connect`. A `POST /api/v1/oauth/start` whose `record`
is an account's bare id is refused with `422` `validation`, and the message
lists the account paths the repository holds under that id. Before v0.93.0
the bare id resolved when one account kind held it. The full record path
has been accepted since v0.85.0 (decision record 0090).

```json
{"record": "owner"}
```

answers:

```
"owner" is a bare record id, and an account is named in full as <authority>/<package>/<kind>/<id>; this repository holds providers.substrate.reamde.dev/google/account/owner
```

The consent callback now carries the path too: the return page's
`postMessage` `record` and the console fallback redirect
`/registry?connected=<path>` hold `<authority>/<package>/<kind>/<id>`, not
the bare id. A consent started under the previous binary fails at the
callback and has to be started again.

##### What to do

1. Send `record` as the full path:
   `{"record": "providers.substrate.reamde.dev/google/account/owner"}`.
2. Pass the full path to `substratectl bundle connect`:
   `substratectl bundle connect providers.substrate.reamde.dev/google/account/owner`.
3. If you listen for the `substrate-oauth` message or read `?connected=`,
   expect the full path there.
4. Restart any consent that was in progress during the upgrade.

## [0.92.1](https://github.com/geoah/substrate/compare/v0.92.0...v0.92.1) (2026-09-24)

### Fixed

* **console:** resolve a reference pin by its full identity only ([6dd5368](https://github.com/geoah/substrate/commit/6dd5368f82861c69a1ec9ed3b4816316c83a18e9))
* **engine:** qualify bare kinds in stored trigger and policy selectors ([7290771](https://github.com/geoah/substrate/commit/7290771605b1bd764b947022b3aa89b6c0603db3))

## [0.92.0](https://github.com/geoah/substrate/compare/v0.91.0...v0.92.0) (2026-09-24)

### ⚠ BREAKING CHANGES

* refuse a bare kind, trait or callable name on every surface ([1ae09f5](https://github.com/geoah/substrate/commit/1ae09f5c3c1b9c789c67944250363995f2eece7e))

### Added

* refuse a bare kind, trait or callable name on every surface ([1ae09f5](https://github.com/geoah/substrate/commit/1ae09f5c3c1b9c789c67944250363995f2eece7e))

### Upgrade notes

#### A bare kind, trait, function or agent name is refused on every surface

API clients, function bodies, trigger and policy authors, and
`substratectl` users are hit. Every place that took a bare word now takes
the full `<authority>/<package>/<name>` and refuses a bare one with `422`
`validation`, listing the spellings the repository declares. An unknown full
name stays `404`.

- `filter.kinds` on `GET /api/v1/records` (all three modes) and `kind` on
  `POST /api/v1/records`: a bare word was `404 unknown kind task`, now `422`.
- `filter.implements` takes a full trait: `substrate.reamde.dev/core/temporal`.
- `POST /api/v1/substrate.reamde.dev/core/function/{name}/call`,
  `…/core/agent/{name}/call` and `…/core/agent/{name}/chat`: `{name}` is the
  full identity, percent-encoded.
- A trigger's `source.record.kinds` and a policy's `selector.kinds` refuse a
  bare entry at write time.
- `host.records.list(["task"])` in a function body is refused.
- `substratectl get`, `patch`, `delete` and `search --kinds` refuse a bare
  kind, and `get`, `patch` and `delete` lose `--package`.

Before and after:

```
substratectl get task
substratectl get example.com/tasks/task
```

##### What to do

1. Upgrade to v0.92.1 or later, not v0.92.0. On v0.92.0 a stored trigger
   with a bare `source.record.kinds` entry is skipped and a stored policy
   with a bare `selector.kinds` entry gates nothing; repository migration
   `0002_qualify_bare_selector_kinds` in v0.92.1 rewrites both at first open.
2. Replace every bare kind, trait, function and agent name in scripts,
   function bodies and agent tool calls with the full spelling the refusal
   lists. `substratectl kinds` prints every kind.
3. Drop `--package` from `substratectl` invocations and pass the full kind.
4. Treat `422` `validation` on the records route as a spelling error, not a
   missing kind.

## [0.91.0](https://github.com/geoah/substrate/compare/v0.90.0...v0.91.0) (2026-09-24)

### ⚠ BREAKING CHANGES

* **vocabulary:** refuse a bare kind or trait name in a declaration ([e3af50b](https://github.com/geoah/substrate/commit/e3af50b41c1eff59a4a615a87c334777d7612446))

### Added

* **console:** filter a reference by picking its referents ([#612](https://github.com/geoah/substrate/issues/612)) ([10ee9b5](https://github.com/geoah/substrate/commit/10ee9b561b50183e373ddb4594b3f9466119b7f3))
* **vocabulary:** refuse a bare kind or trait name in a declaration ([e3af50b](https://github.com/geoah/substrate/commit/e3af50b41c1eff59a4a615a87c334777d7612446))

### Fixed

* **console:** list every problem of a refused import on its own line ([59d0913](https://github.com/geoah/substrate/commit/59d09137f9bf9c53952dc82fdb845f6eaadd3158))

### Upgrade notes

#### A declaration naming a kind or trait by a bare word is refused

Vocabulary authors are hit: anyone applying kind, function or bundle
documents through `POST /api/v1/vocabulary/apply`, `substratectl apply` or a
catalog import. Five places now take `<authority>/<package>/<name>` only:
a reference's `kind:` pin, a reference's `trait:` pin, a `traits:` entry, and
a function's `permissions.writes` and `permissions.reads.kinds`. Nothing
resolves a bare word any more, not even to the declaring package's own kind.

Before, this property was admitted:

```yaml
owner:
  type: reference
  kind: person
```

From v0.91.0 the apply answers `422` `validation` with a problem like:

```
kind example.com/tasks/task: data.properties.owner.kind: "person" is a bare name, and a kind pin is named in full as <authority>/<package>/<name>; this repository declares samples.substrate.reamde.dev/people/person
```

A `traits:` entry keeps its variant after the identity:
`substrate.reamde.dev/core/temporal(point: dueAt)`.

Stored declarations are not affected. Repository migration
`0001_qualify_bare_declaration_names` rewrites every stored bare name at the
repository's first open under v0.91.0 or later.

##### What to do

1. Operators: nothing. The migration runs at boot.
2. Authors: in every document you apply, replace each bare word in `kind:`,
   `trait:`, `traits:`, `writes` and `reads.kinds` with the full spelling the
   refusal lists. `substratectl kinds` prints every installed kind.
3. Accept the upgrades offered for shipped providers and samples; their
   package versions were bumped for this change.

## [0.90.0](https://github.com/geoah/substrate/compare/v0.89.1...v0.90.0) (2026-09-23)

### Added

* **console:** walk a first-time user through connecting a provider account ([#610](https://github.com/geoah/substrate/issues/610)) ([f5ac583](https://github.com/geoah/substrate/commit/f5ac58349dda23da790c6d535822a1dc1db572b7))

### Fixed

* **console:** a provider without an account kind offers nothing to add, and the Registry warns on a missing token ([#611](https://github.com/geoah/substrate/issues/611)) ([2c72f58](https://github.com/geoah/substrate/commit/2c72f58f5f4aa643c6b358b8dd033027c30294de))

## [0.89.1](https://github.com/geoah/substrate/compare/v0.89.0...v0.89.1) (2026-09-22)

### Fixed

* **engine:** name the temporal binding in the unbound dueAt refusal ([#608](https://github.com/geoah/substrate/issues/608)) ([99d5b09](https://github.com/geoah/substrate/commit/99d5b09fc9cfb3b8dedfc5dc291bad908d8528ec))

## [0.89.0](https://github.com/geoah/substrate/compare/v0.88.0...v0.89.0) (2026-09-22)

### Added

* **compose:** pass SUBSTRATE_CONSOLE_URL through ([#607](https://github.com/geoah/substrate/issues/607)) ([e3d1066](https://github.com/geoah/substrate/commit/e3d10669ed5977599b21676352e805b04d9cef28))

## [0.88.0](https://github.com/geoah/substrate/compare/v0.87.0...v0.88.0) (2026-09-22)

### Added

* **providers:** ship the seven mneme-tested bundles and their e2e suite ([#606](https://github.com/geoah/substrate/issues/606)) ([862094e](https://github.com/geoah/substrate/commit/862094e6e4e58dee3b8603314786d499a2e8f583))

### Upgrade notes

#### Six provider bundles are replaced with reshaped versions that rename kinds and functions

Anyone with a shipped provider installed is hit. v0.88.0 replaces beeper 10,
github 12, google 18, linear 14, notion 9 and whoop 9 with versions 13, 21,
30, 15, 11 and 11, and adds slack 5. Clients reading these mirrors break on
the renames and removals. The per-provider "Upgrading from version N"
paragraphs in [the bundles catalog](docs/bundles-catalog.md) list every change.
The kind and function renames are:

- [Google](docs/bundles-catalog.md#google): kinds `event` to `calendarevent`,
  `series` to `calendarseries`, `thread` to `gmailthread`, `message` to
  `gmailmessage`; functions `contactssync` to `synccontacts`, `gmailsync` to
  `syncgmail`, `calendarsync` to `synccalendar`.
- [GitHub](docs/bundles-catalog.md#github): `account.syncCursor` to
  `syncCursors`, `account.login` to `user`, `htmlURL` to `htmlUrl`, `title` to
  `issueTitle` and `pullRequestTitle`; `raw` is gone.
- [Linear](docs/bundles-catalog.md#linear): function `issuessync` to
  `linearsync`; triggers to `linear-on-connect` and `linear-scheduled`;
  `issue.assignee` is a reference at `linear/user`; `config` holds `apiKey`.
- [WHOOP](docs/bundles-catalog.md#whoop): `recovery.cycleId` to `recovery.cycle`,
  `workout.sport` to `sportName` and `sportId`.
- [Notion](docs/bundles-catalog.md#notion): function `workspacesync` to
  `notionsync`; rows move from `database` to `datasource`.
- [Beeper](docs/bundles-catalog.md#beeper): kind `room` is removed; `chat`
  replaces it.

The `people` sample drops the `linearissueperson` mapping.

##### What to do

1. Run `substratectl catalog` to preview each provider's upgrade. An upgrade
   that drops a kind with live rows is refused with
   `kind <ref> has <n> live records: delete or migrate them first`.
2. Delete those rows, or run `substratectl bundle disable <id>` and
   `substratectl bundle purge <id> --yes`, which also deletes the accounts.
   The next sync writes the new kinds.
3. Upgrade with `substratectl install providers.substrate.reamde.dev/<name>`,
   and reconnect any purged account.
4. Import the `people` and `tasks` samples again so their mappings read the
   new kinds.
5. Rewrite clients against the new names.

## [0.87.0](https://github.com/geoah/substrate/compare/v0.86.0...v0.87.0) (2026-09-22)

### Added

* **console:** nest a kind's records under their parent reference ([53758d3](https://github.com/geoah/substrate/commit/53758d308703b889e1803cfe0bfef85f1907bfd1))

### Fixed

* **release:** create /var/lib/substrate and /keys in the published image ([#602](https://github.com/geoah/substrate/issues/602)) ([45f6028](https://github.com/geoah/substrate/commit/45f6028d466dfd0629519bef340f579d2fea4d61))
* **engine:** a put that resurrects a tombstone may name its state ([#604](https://github.com/geoah/substrate/issues/604)) ([9a91b44](https://github.com/geoah/substrate/commit/9a91b447e0818c9c7cd211991e44cd537786f051))

## [0.86.0](https://github.com/geoah/substrate/compare/v0.85.0...v0.86.0) (2026-09-22)

### ⚠ BREAKING CHANGES

* **webhooks:** a webhook trigger declares the headers its callable reads ([#601](https://github.com/geoah/substrate/issues/601)) ([c342277](https://github.com/geoah/substrate/commit/c342277f63c0a44d51c08e1e9a72cb004c2d4aa0))

### Added

* **webhooks:** keep the Pebble Index app's headers; the sample reads its real contract ([#599](https://github.com/geoah/substrate/issues/599)) ([659b059](https://github.com/geoah/substrate/commit/659b059547cd8d5062d7cf0ab6bce46efde0c0b8))
* **webhooks:** a webhook trigger declares the headers its callable reads ([#601](https://github.com/geoah/substrate/issues/601)) ([c342277](https://github.com/geoah/substrate/commit/c342277f63c0a44d51c08e1e9a72cb004c2d4aa0))

### Upgrade notes

#### A webhook fire carries only the headers its trigger lists in `source.webhook.headers`

Anyone with a `substrate.reamde.dev/core/trigger` record whose source is a
webhook is hit. Before v0.86.0 the engine forwarded a fixed provider list
(`x-github-event`, `stripe-signature`, `x-slack-signature`, the Pebble app's
`x-index-*` and others). From v0.86.0 a fire, and its parked copy, carries
`content-type`, `content-length`, `content-encoding`, `user-agent`, `date`
and the names the trigger declares, nothing else. An undeclared header reads
as absent in the callable's `request.headers`. The trigger still fires.

Before, a callable reading `x-github-event` worked with:

```yaml
source:
  webhook: {}
```

After, it needs:

```yaml
source:
  webhook:
    headers:
      - x-github-event
      - x-hub-signature-256
```

Names match case-insensitively, at most 32. Declaring `authorization`,
`proxy-authorization`, `cookie`, `set-cookie`, `host` or
`transfer-encoding` is refused at write time.

##### What to do

1. List the triggers: `substratectl get substrate.reamde.dev/core/trigger -o yaml`.
2. For each record with `source.webhook`, find the headers its callable reads
   and add them under `source.webhook.headers`.
3. Apply each edited record with `substratectl apply -f <file>`.
4. For a trigger shipped by a sample (the Pebble sample declares its own
   eight names), re-importing the sample writes the list, but it discards a
   hand-set `key`, so set `key` again afterwards.

## [0.85.0](https://github.com/geoah/substrate/compare/v0.84.0...v0.85.0) (2026-09-19)

### Added

* **console:** a record's Provenance tab groups its sources by mapping and lets the owner pick a value, and propertyMeta names the source record behind each manager and alternative (record 0094) ([#594](https://github.com/geoah/substrate/issues/594)) ([c7479ea](https://github.com/geoah/substrate/commit/c7479eabfa5bf034433075b4288879254abbca69))

### Upgrade notes

#### Provider mirror kinds stop declaring subject slots; the `recordmapping` adds them

Clients reading provider mirrors and authors of source kinds are hit. The
`github` (12), `google` (18) and `linear` (14) packages no longer declare
these references:

- `person` on `github/user`, `google/contact`, `google/emailaddress` and
  `linear/user`
- `assignee` and `task` on `linear/issue`

A `substrate.reamde.dev/core/recordmapping` now adds its `property` to the
source kind when the mapping installs (decision record 0096). With the
`people` and `tasks` samples imported, those samples' mappings add the same
names back, so the values stay where they were. Without a mapping the
property is absent.
A kind read (`GET /api/v1/substrate.reamde.dev/core/kind/<ref>`) serves an
added slot with `managed: true` and `mappedBy`. The stored declaration
does not carry it.

Linear's sync wrote the viewer's own user mirror into `linear/issue.assignee`
when the viewer's address was hidden. It now writes it to `assigneeUser`, a
reference at `linear/user`:

```python
props["assignee"] = USER_TYPE + "/" + host.ids.external("linear", aid, "user:" + vid)      # linear 13
props["assigneeUser"] = USER_TYPE + "/" + host.ids.external("linear", aid, "user:" + vid)  # linear 14
```

Removing a mapping is refused while live records still link through its
slot.

##### What to do

1. Read the hidden-address assignee from `assigneeUser` on `linear/issue`.
   v0.88.0 reshapes the Linear bundle again (see
   its note).
2. Authors: stop declaring a `subject: true` reference on a source kind; the
   mapping adds it. A source kind that still declares one keeps working.
3. To remove a mapping, delete the records linking through it first.

#### A mapping source with several candidates, or nothing to offer, is left unlinked

Consumers of mapping targets (a `person` behind every `google/contact`, for
example) are hit. Before v0.85.0 a source record whose probes found no single
match always got a new, empty subject record. From v0.85.0 the engine leaves
the source's subject slot unset in two cases (decision record 0087):

- the probes find several candidates;
- the source offers nothing: no probe value and no mapped value, where empty
  lists and blank strings count as nothing.

A source with something to offer and no candidate still gets a new subject.
Two callers still always get one: a write that names the mirror in a slot
pinned at the subject kind, and a source kind that declares its slot
`required:`.

An unset slot is resolved again on the source's next write, so after the
owner merges the two candidates, the next sync links it.

v0.95.0 adds the `recordmapping` key `onAmbiguous` (`park`, the default, is
this behavior; `oldest`; `mint`). It also adds `filter.ambiguous` and
`substratectl get <kind> --ambiguous`, which list sources left unlinked by
several candidates:

```
substratectl get providers.substrate.reamde.dev/google/contact --ambiguous
```

##### What to do

1. Treat an empty subject slot on a mapping source as a normal state, not an
   error.
2. On v0.95.0 or later, list waiting sources with `--ambiguous` and merge the
   candidates they name.
3. To keep the old behavior for one mapping, set `onAmbiguous: mint` on it
   (v0.95.0 or later).

## [0.84.0](https://github.com/geoah/substrate/compare/v0.83.0...v0.84.0) (2026-09-19)

### Added

* **search:** one search grammar filters, matches and ranks, with a Search page and a table search box ([#590](https://github.com/geoah/substrate/issues/590)) ([fbd2688](https://github.com/geoah/substrate/commit/fbd26882475a31c7e92b19e2fb458eb0770bba13))
* **api:** a records list pages by offset, and the console browse numbers its pages ([#591](https://github.com/geoah/substrate/issues/591)) ([36974a5](https://github.com/geoah/substrate/commit/36974a57d7349e4966c0085a0131581d5f7a60d7))
* **sync:** a core sync trait the dispatcher stamps, a sync status read, and the console's Connections page ([#592](https://github.com/geoah/substrate/issues/592)) ([fe3cfec](https://github.com/geoah/substrate/commit/fe3cfec1a17e3d92bd21a2996c56b032173dea4a))

### Upgrade notes

#### The ranked read `q` parses the search grammar and refuses a query with no word

API clients and agents calling the ranked read are hit. Before v0.84.0 the
lexical arm of `GET /api/v1/records?q=` parsed `q` with Postgres's
`websearch_to_tsquery`. From v0.84.0 it uses the search grammar: bare words
conjoin and stem, a `*` in a word marks a prefix, quotes make a phrase, `-`
excludes, `OR` in capitals disjoins, and no other character is an operator
(`a&b` is one word). The grammar applies in every `mode`, and
`substratectl search` sends its query the same way.

A query with no word left in it is now refused instead of ranked:

```
GET /api/v1/records?q=*
```

answers `422` `validation` with the message

```
q: "*" has no word to match
```

##### What to do

1. Stop sending a `q` that is only stars, quotes or dashes; treat a `422`
   there as an empty query.
2. Expect `lay*` to match words starting with `lay`, where the star used to
   be ignored.
3. Quote a phrase you meant literally; other punctuation no longer splits a
   word.

## [0.83.0](https://github.com/geoah/substrate/compare/v0.82.0...v0.83.0) (2026-09-18)

### Added

* **console:** revamp record views and persist navigation preferences ([#589](https://github.com/geoah/substrate/issues/589)) ([279ffcc](https://github.com/geoah/substrate/commit/279ffcc838426f56e12a65f6c33a8cba7401a2b0))

## [0.82.0](https://github.com/geoah/substrate/compare/v0.81.0...v0.82.0) (2026-09-15)

### ⚠ BREAKING CHANGES

* **dev:** the dev database is per tree, and a second writer of a repository is refused at open ([#567](https://github.com/geoah/substrate/issues/567)) ([f2213cb](https://github.com/geoah/substrate/commit/f2213cbddc6b7086bac3a361b80a18bfb9769f44))

### Added

* **dev:** the dev database is per tree, and a second writer of a repository is refused at open ([#567](https://github.com/geoah/substrate/issues/567)) ([f2213cb](https://github.com/geoah/substrate/commit/f2213cbddc6b7086bac3a361b80a18bfb9769f44))

### Upgrade notes

#### A second server on one database does not boot: each repository has one writer lease

From v0.82.0 a server takes a Postgres session-level advisory lock per
repository when it opens it, on one connection pinned for the life of the
process (decision record 0083). A second server pointed at the same
database cannot take the lease and exits at boot, whether or not it shares
the first one's `SUBSTRATE_DATA_ROOT`. Before, two servers under two data
roots both ran and repaired each other's changelog rows at their next write.

This hits operators who run two servers against one database, including a
rolling update that starts the new server before the old one stops. The
second server fails with:

```text
substrate/engine: boot check: repository ada.example.com: substrate/engine: another
process is this repository's writer: repository ada.example.com. One server per
database: ...
```

If the pinned connection drops, the server refuses every write with
`503 unavailable` until its heartbeat takes the lease back.
`repository verify` and `repository reembed` take no lease and still run
beside a live server; every other operator command meets the lease.

##### What to do

1. Run one server per database, and roll out so the old server stops
   before the new one starts (on Kubernetes, the `Recreate` strategy).
2. If a server refuses to boot with no other server running, find the
   holder in `pg_locks` with the query in `docs/operations.md` and end it
   with `pg_terminate_backend(pid)`.
3. Stop the server before an operator command that writes, such as
   `substratectl --dsn … user reset`. It is refused while the server holds
   the lease, even when run against a data root of its own.
4. Contributors: `mise run dev:stop` no longer stops Postgres, each tree gets
   its own database `substrate_<tree directory name>`, and `dev:wipe:all`
   removes the shared container.

## [0.81.0](https://github.com/geoah/substrate/compare/v0.80.0...v0.81.0) (2026-09-15)

### ⚠ BREAKING CHANGES

* **engine:** a dangling mustExist reference is a 422 validation problem naming the property ([#566](https://github.com/geoah/substrate/issues/566)) ([204d0fe](https://github.com/geoah/substrate/commit/204d0fe09707486c983874a88991a344e9d31288))

### Fixed

* **engine:** a dangling mustExist reference is a 422 validation problem naming the property ([#566](https://github.com/geoah/substrate/issues/566)) ([204d0fe](https://github.com/geoah/substrate/commit/204d0fe09707486c983874a88991a344e9d31288))

### Upgrade notes

#### A write naming an absent `mustExist` referent answers `422 validation`, not `404`

Before v0.81.0, a write whose body held a `mustExist: true` reference to a
record that does not exist answered `404 not_found`, the status a client
reads as "the record I addressed is gone". From v0.81.0 it answers
`422 validation`, with one `problemDetails` entry per dangling reference,
addressed to the property that holds it and listed beside every other
problem in the same write. The message text is unchanged. A missing source
row on a subject hop follows the same rule.

This hits clients and agents that branch on the status of a `PUT`, `PATCH`
or `POST /api/v1/records`:

```http
POST /api/v1/records
{"kind": "ada.example.com/people/team",
 "properties": {"name": "Nobodies",
                "members": ["ada.example.com/people/person/nobody",
                            "ada.example.com/people/person/nobody-either"]}}

before: 404 {"error": {"code": "not_found", "message": "... reference names ada.example.com/people/person/nobody, which does not exist"}}
after:  422 {"error": {"code": "validation", "message": "...",
             "problemDetails": [{"path": "props.members[0]", "message": "..."},
                                {"path": "props.members[1]", "message": "..."}]}}
```

##### What to do

1. Handle a dangling referent under `422`: read `problemDetails[].path` to
   find the property, then fix the value or create the referent first.
2. Keep `404` for "the record at this path does not exist".

## [0.80.0](https://github.com/geoah/substrate/compare/v0.79.1...v0.80.0) (2026-09-15)

### ⚠ BREAKING CHANGES

* **engine:** a failed accept of a patch request answers 409 conflict with the reason, never "version conflict" ([#564](https://github.com/geoah/substrate/issues/564)) ([5bf4861](https://github.com/geoah/substrate/commit/5bf4861389d297d9a47d0096cd31a8c9ffc18509))

### Added

* **engine:** the judge is told its reply contract, reads a fenced verdict, and may expand the diff's referents ([#561](https://github.com/geoah/substrate/issues/561)) ([e3695cd](https://github.com/geoah/substrate/commit/e3695cd2ac194eff976e35b268c60fab169896e4))
* **vocabulary:** triggerrun carries the callable as a reference, and callable's description says what it stores ([#565](https://github.com/geoah/substrate/issues/565)) ([5fb5d10](https://github.com/geoah/substrate/commit/5fb5d10b297d9fed81b7a430696ebfc97a87c114))

### Fixed

* **engine:** a failed accept of a patch request answers 409 conflict with the reason, never "version conflict" ([#564](https://github.com/geoah/substrate/issues/564)) ([5bf4861](https://github.com/geoah/substrate/commit/5bf4861389d297d9a47d0096cd31a8c9ffc18509))
* **runner:** function bodies trust the system certificate bundle when the interpreter ships none ([#562](https://github.com/geoah/substrate/issues/562)) ([ddb6c2e](https://github.com/geoah/substrate/commit/ddb6c2e7ce77402719bf3c057d7abe8642914d5f))
* **engine:** a state property added to a kind with live records backfills its initial state at admission ([#563](https://github.com/geoah/substrate/issues/563)) ([2e4f48d](https://github.com/geoah/substrate/commit/2e4f48da21258a4ae6a59a8eeb24cd422b11286e))

### Upgrade notes

#### A failed accept of a `recordpatchrequest` answers `409 conflict` naming its reason

Accepting a change request whose change no longer applies used to answer
with the status of the inner cause, and the message claimed a version
conflict the caller's `ifVersion` never lost. A diff that applied no change,
for example, answered `422 validation` with `substrate: version conflict:
substrate: validation failed: the diff applied no change …`. From v0.80.0
every failed accept (a no-op diff, a target that moved or vanished, a guard,
an emit ceiling refusal) answers one `409 conflict` whose message names the
reason and never says "version conflict". The request stays `proposed` and
carries the same reason as its conflict annotation.

```http
PATCH /api/v1/substrate.reamde.dev/core/recordpatchrequest/noop
{"ifVersion": 1, "properties": {"decision": "accepted"}}

before: 422 {"error": {"code": "validation", "message": "substrate: version conflict: substrate: validation failed: the diff applied no change …"}}
after:  409 {"error": {"code": "conflict", "message": "substrate: the accepted diff did not apply: the diff applied no change …"}}
```

This hits clients and agents that decide change requests.

##### What to do

1. Treat `409 conflict` on an accept as "the change no longer applies":
   read the reason from `message` or from the request's conflict
   annotation, then reject or re-propose. Do not retry the same accept.
2. Keep treating a `409` whose message says "version conflict" as a stale
   `ifVersion`: re-read the request and retry.

## [0.79.1](https://github.com/geoah/substrate/compare/v0.79.0...v0.79.1) (2026-09-15)

### Fixed

* **cli:** apply renders every problem a 422 carries, details first ([#558](https://github.com/geoah/substrate/issues/558)) ([6c0238f](https://github.com/geoah/substrate/commit/6c0238fb6af40c6bcd7aa4d22c90224b83202d42))
* **runner:** the SDK's list accepts the REST string forms of orderBy ([#559](https://github.com/geoah/substrate/issues/559)) ([f24d12c](https://github.com/geoah/substrate/commit/f24d12c2ccf50592242e5459c6fdb96a9d92d0ca))
* **engine:** a refused label or annotation key names the rule it broke, and the docs say who may write which namespace ([#560](https://github.com/geoah/substrate/issues/560)) ([5877867](https://github.com/geoah/substrate/commit/5877867bb23d84843fb75cc8cb3c988efe28d8b6))

## [0.79.0](https://github.com/geoah/substrate/compare/v0.78.0...v0.79.0) (2026-09-14)

### ⚠ BREAKING CHANGES

* recurrence is core, the window read computes occurrences, and the Google mirror copies masters and exceptions ([#544](https://github.com/geoah/substrate/issues/544)) ([e31a5ac](https://github.com/geoah/substrate/commit/e31a5ac8a25d0a0fe7a5d4513601828fa6bc1452))

### Added

* recurrence is core, the window read computes occurrences, and the Google mirror copies masters and exceptions ([#544](https://github.com/geoah/substrate/issues/544)) ([e31a5ac](https://github.com/geoah/substrate/commit/e31a5ac8a25d0a0fe7a5d4513601828fa6bc1452))

### Fixed

* **engine:** a write appends the rows another process committed instead of latching until restart ([#542](https://github.com/geoah/substrate/issues/542)) ([cbaae3f](https://github.com/geoah/substrate/commit/cbaae3f0e3d0e768bd6c7381b44e257fab550875))
* **engine:** a snippet cuts on a rune boundary, and text no row stores is refused as validation ([#543](https://github.com/geoah/substrate/issues/543)) ([ea0dde1](https://github.com/geoah/substrate/commit/ea0dde138a560355d62694a2a0ae946800fd0fb3))

### Upgrade notes

#### Mirror Google Calendar recurring events as one series row plus exceptions

Before v0.79.0 the Google provider's calendar sync wrote one
`providers.substrate.reamde.dev/google/event` row per occurrence of a
recurring event, up to 365 days ahead. From v0.79.0 it writes a recurring
master as one `providers.substrate.reamde.dev/google/series` row carrying
the rule (`recurrence`, `rdates`, `exdates`), a modified occurrence as an
`event` row with `recurrenceOf` and `originalAt`, and never a row for an
unmodified occurrence. The first sync after the upgrade re-reads each
calendar in full and deletes every legacy per-occurrence row.

This hits a client that lists Google events for a date range as a plain
list: recurring meetings disappear from it. Ask for a window instead, which
computes the occurrences from the series rows:

```http
before: GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/google/event"],
            "properties":{"at":{"gte":"2026-07-01T00:00:00Z"}}}
after:  GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/google/event",
            "providers.substrate.reamde.dev/google/series"],
            "properties":{"at":{"gte":"2026-07-01T00:00:00Z","lt":"2026-07-08T00:00:00Z"}}}
```

The two kinds were renamed `calendarevent` and `calendarseries` in v0.88.0.

##### What to do

1. Read calendar ranges with both bounds on `at` and both kinds in
   `filter.kinds`, as above.
2. Follow an exception to its series through its `recurrenceOf` reference.
3. Nothing for stored data: the first sync after the upgrade converts it.

#### Remove `GET /api/v1/occurrences`; a two-sided `at` filter computes occurrences

From v0.79.0 `GET /api/v1/occurrences` answers `404`. Recurring series are
expanded by the window read instead: a `GET /api/v1/records` list whose
filter bounds `at` on both ends returns the stored rows in the window and,
merged in slot order, one computed occurrence per slot of every series among
the kinds in play (decision record 0081). The recurrence traits moved to
core as `substrate.reamde.dev/core/recurring` and
`substrate.reamde.dev/core/override`; the scheduling sample no longer
declares `recurring`.

```http
before: GET /api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-08T00:00:00Z&limit=1000
        -> {"occurrences": [{"kind", "id", "title", "at", "log"}], "truncated": false}

after:  GET /api/v1/records?filter={"implements":"substrate.reamde.dev/core/temporal",
            "properties":{"at":{"gte":"2026-07-01T00:00:00Z","lt":"2026-07-08T00:00:00Z"}}}&orderBy=at
        -> {"records": [...], "cursor": "...", "head": 4211, "generation": "..."}
```

A computed occurrence is a record envelope with `"computed": true`,
`"version": 0` and the id `<seriesId>_<slot>` (slot in UTC,
`YYYYMMDDTHHMMSSZ`). `orderBy` on a window read is `at` alone.

##### What to do

1. Replace calls to `/api/v1/occurrences` with the window read above, and
   page it with `first` and `after` instead of `limit`.
2. Treat a row with `computed: true` as read-only unless its kind binds
   `override`; then a `PUT` at its id materializes that one occurrence.
3. In your own kind declarations, bind `substrate.reamde.dev/core/recurring`
   instead of the scheduling sample's `recurring`. A bare trait name is
   refused in declarations from v0.91.0 (decision record 0098).

## [0.78.0](https://github.com/geoah/substrate/compare/v0.77.1...v0.78.0) (2026-09-14)

### Added

* **vocabulary:** a kind grant may glob, and a glob never reaches auth material ([#541](https://github.com/geoah/substrate/issues/541)) ([c4a381a](https://github.com/geoah/substrate/commit/c4a381ace3cebe082e40dbca09e86229ec7aa9d8))

## [0.77.1](https://github.com/geoah/substrate/compare/v0.77.0...v0.77.1) (2026-09-14)

### Fixed

* **console:** a reference in a table cell is the referent's pill, not {ref} ([#538](https://github.com/geoah/substrate/issues/538)) ([db139fa](https://github.com/geoah/substrate/commit/db139fa3812bd4576a37e94ddb122d977ef02fd4))

## [0.77.0](https://github.com/geoah/substrate/compare/v0.76.0...v0.77.0) (2026-09-14)

### Added

* **console:** read a declared object as its fields, not as a JSON blob ([#537](https://github.com/geoah/substrate/issues/537)) ([f3bfc88](https://github.com/geoah/substrate/commit/f3bfc8871b82040618f40b77c1cbdcf8cc4c44d0))

## [0.76.0](https://github.com/geoah/substrate/compare/v0.75.0...v0.76.0) (2026-09-12)

### ⚠ BREAKING CHANGES

* the registry shows what a bundle adds, the llm kinds get their own package, and provider installs work in compose ([830f460](https://github.com/geoah/substrate/commit/830f460e4be52450f2ea281f936035047bc9d2c4))
* remove GraphQL and fold every record read into GET /api/v1/records ([#531](https://github.com/geoah/substrate/issues/531)) ([0c152b7](https://github.com/geoah/substrate/commit/0c152b7c54fb1ca1940046c9ea6786c8e3ef5480))

### Added

* the registry shows what a bundle adds, the llm kinds get their own package, and provider installs work in compose ([830f460](https://github.com/geoah/substrate/commit/830f460e4be52450f2ea281f936035047bc9d2c4))
* remove GraphQL and fold every record read into GET /api/v1/records ([#531](https://github.com/geoah/substrate/issues/531)) ([0c152b7](https://github.com/geoah/substrate/commit/0c152b7c54fb1ca1940046c9ea6786c8e3ef5480))

### Upgrade notes

#### Remove `POST /api/v1/graphql` and read every list at `GET /api/v1/records`

Any client that posted to `POST /api/v1/graphql`, listed a kind at its
three-segment path, or read `…/{id}/incoming` or `…/trait/{id}/records`
gets `404` from v0.76.0. Agents lose the `substrate.reamde.dev/core/graphql`
and `substrate.reamde.dev/core/mutate` tools. The record path
(`GET`/`PUT`/`PATCH`/`DELETE /api/v1/{authority}/{package}/{kind}/{id}`) is
unchanged. Every filter below is URL-encoded JSON in `?filter=`.

| Read | Before | After |
| --- | --- | --- |
| List a kind | `GET /api/v1/ada.example.com/tasks/task?filter={"properties":{"status":{"eq":"open"}}}&orderBy=dueAt`, or GraphQL `records(filter: {kinds: [...]}, first: 20)` | `GET /api/v1/records?filter={"kinds":["ada.example.com/tasks/task"],"properties":{"status":{"eq":"open"}}}&orderBy=dueAt&first=20` |
| Get one | GraphQL `record(kind, id)` | `GET /api/v1/ada.example.com/tasks/task/kq3v9x2m41pf` |
| Search | GraphQL `search(q: "quarterly review", mode: "hybrid", kinds: [...], k: 20) { hits { record lexical semantic } pending }` | `GET /api/v1/records?q=quarterly+review&mode=hybrid&filter={"kinds":["ada.example.com/notes/note"]}&first=20`, answering `{records, scores, pending}` with `scores` keyed by `<kind>/<id>` |
| Who points here | `GET /api/v1/ada.example.com/tasks/project/p1/incoming?property=project&fromKind=ada.example.com/tasks/task` | `GET /api/v1/records?filter={"referencing":{"ref":"ada.example.com/tasks/project/p1","property":"project"},"kinds":["ada.example.com/tasks/task"]}`, pointing sites in `matches` |
| Trait implementors' records | `GET /api/v1/substrate.reamde.dev/core/trait/{id}/records` | `GET /api/v1/records?filter={"implements":"substrate.reamde.dev/core/accountconfig"}` |
| Tail one kind | `GET /api/v1/ada.example.com/tasks/task?watch=1` | `GET /api/v1/records?watch=1&filter={"kinds":["ada.example.com/tasks/task"]}` |
| Create, server id | `POST /api/v1/ada.example.com/tasks/task` | `POST /api/v1/records` with `"kind"` in the body; a body `id` is `422` |

##### What to do

1. Rewrite each call with the table. Put every kind in `filter.kinds`; a
   GraphQL inline fragment is not needed, because each record carries all
   its `properties`.
2. Replace a GraphQL join with `expand=<reference property>` on the list and
   read referents from `included`, keyed by record path.
3. Discard list cursors saved before the upgrade. A cursor binds its filter,
   and one from a replaced history answers `410 compacted`.
4. In each agent's `tools:`, replace `substrate.reamde.dev/core/graphql` with
   `substrate.reamde.dev/core/query`, and `substrate.reamde.dev/core/mutate`
   with `substrate.reamde.dev/core/write` (`{op, kind, id, input,
   ifVersion}`). An agent naming a removed tool is quarantined until it does.
5. Upgrade `substratectl` to 0.76.0 or later. Ranked search is
   `substratectl search <query>`; `get` takes `--expand` and `--referencing`.

#### Move the four `core/llm*` kinds to the seeded `substrate.reamde.dev/llm` package

The agent runtime's kinds changed reference in v0.76.0 (decision record
0077):

| Before | After |
| --- | --- |
| `substrate.reamde.dev/core/llmprovider` | `substrate.reamde.dev/llm/provider` |
| `substrate.reamde.dev/core/llmthread` | `substrate.reamde.dev/llm/thread` |
| `substrate.reamde.dev/core/llmmessage` | `substrate.reamde.dev/llm/message` |
| `substrate.reamde.dev/core/llminteraction` | `substrate.reamde.dev/llm/interaction` |

The first boot of v0.76.0 moves every live row to the new kind with the same
id and properties, and repoints every reference at it (decision record 0078).
The old kinds stay declared and empty. This hits clients, scripts and YAML
files that name the old references, for example a provider key written as:

```http
before: PATCH /api/v1/substrate.reamde.dev/core/llmprovider/openai
after:  PATCH /api/v1/substrate.reamde.dev/llm/provider/openai
        {"properties": {"apiKey": "sk-..."}}
```

A write to an old kind is not refused: it lands in a kind the agent runtime
no longer reads.

##### What to do

1. Nothing for stored data: the boot upgrade moves the rows and references.
2. Replace the four old references in client code, saved filters and
   `apply -f` documents, including `provider:` references in agent
   manifests kept outside the server.
3. Re-bookmark console pages for the old kinds.

#### Write trigger delivery rows as `substrate.reamde.dev/core/triggerrun`

From v0.76.0 the engine records each trigger delivery attempt as a
`substrate.reamde.dev/core/triggerrun` record. Before, it wrote
`substrate.reamde.dev/core/run`. The properties are the same kind of row
(`trigger`, `status`, `callable` and the rest), but nothing moves the old
rows: a repository created before v0.76.0 keeps a dormant `core/run` kind
holding the history it had, and a fresh repository never has it.

This hits any client or console bookmark that lists delivery attempts by
kind:

```http
before: GET /api/v1/substrate.reamde.dev/core/run?filter={"properties":{"status":{"eq":"parked"}}}
after:  GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/triggerrun"],"properties":{"status":{"eq":"parked"}}}
```

The route change in that example is the GraphQL removal of the same release.

##### What to do

1. Replace `substrate.reamde.dev/core/run` with
   `substrate.reamde.dev/core/triggerrun` wherever a client lists or watches
   delivery attempts.
2. To read attempts from before the upgrade, list the old kind as well; the
   engine no longer writes or prunes it.

#### Unset `SUBSTRATE_INVITE_CODE` opens registration instead of closing it

Before v0.76.0, a server with `SUBSTRATE_INVITE_CODE` unset or empty refused
`/register` and `/register/enroll` with `501 unsupported`. From v0.76.0 the
same server registers anyone who can reach the port and logs a `WARN` at
boot. There is no closed state any more. This hits every operator who
closed registration by unsetting the code after creating their user.

Discovery changes with it. `GET /.well-known/substrate/server.json` drops
`registration.open` and reports `registration.inviteRequired`:

```json
before: {"registration": {"open": false, "totpRequired": true}}
after:  {"registration": {"inviteRequired": false, "totpRequired": true}}
```

`compose.yaml` also changed its defaults: `SUBSTRATE_INVITE_CODE` now
defaults to empty (it was `let-me-in`), and
`SUBSTRATE_INSECURE_DISABLE_TOTP` defaults to `true`, so login takes a
password alone.

##### What to do

1. On any server reachable by someone other than you, set
   `SUBSTRATE_INVITE_CODE` to a long random value nobody is given, then
   restart. An empty value is the same as unset.
2. On a compose deployment that is not a laptop, also set
   `SUBSTRATE_INSECURE_DISABLE_TOTP=false` so the second factor is verified
   again. A user who enrolled an authenticator before the upgrade keeps
   using it: the server still holds the sealed seed.
3. In a client, read `registration.inviteRequired` instead of
   `registration.open`, and stop treating `501 unsupported` from the
   register door as "closed".

#### Rename the `web` sample to `samples.substrate.reamde.dev/readinglist`

The catalog no longer lists `samples.substrate.reamde.dev/web`. Its
successor is `samples.substrate.reamde.dev/readinglist`, which reads its deny
list from a core `setting` record (`<authority>/readinglist/denyDomains`,
decision record 0076) instead of the `web/config` kind and its `connector`
input. This hits a repository that imported `web` before v0.76.0: its copy
keeps working, but no catalog entry offers it an upgrade.

```bash
before: substratectl import samples.substrate.reamde.dev/web
after:  substratectl import samples.substrate.reamde.dev/readinglist
```

##### What to do

1. If you never imported `web`, nothing.
2. If you did and want the maintained version, import `readinglist` and
   write the deny list into its `denyDomains` setting. The two packages have
   different kind references, so records of `<authority>/web/page` are not
   carried to `<authority>/readinglist/page`.

## [0.75.0](https://github.com/geoah/substrate/compare/v0.74.0...v0.75.0) (2026-09-10)

### ⚠ BREAKING CHANGES

* **runner:** remove the go function runtime ([121b4e9](https://github.com/geoah/substrate/commit/121b4e942f59575e405033ae09459c813dad1b1a))
* **samples:** remove the seven sample packages nothing imports ([f47c21f](https://github.com/geoah/substrate/commit/f47c21f5930406398536548ac30c85df2544549f))

### Upgrade notes

#### The `go` function runtime is removed and `runtime: go` is refused

This hits vocabulary authors who wrote a function body in Go. A function
declaration with `runtime: go` is refused on every door:

```
data.runtime: "go" is retired; write the body in python
```

The core `function` kind (version 14) lists `go` under
`retired.values.runtime`, so the value can never be declared again. A bundle
module whose filename ends in `.go` is refused too
(`a module filename ends in .py, the extension the runtime imports`). The
runtime image no longer contains a Go toolchain at `/usr/local/go`. The
runtimes left are `python` and `host`.

##### What to do

1. Rewrite each Go body in Python. The entrypoint is `main(input, host)`;
   [functions](docs/functions.md) documents the `host` object.
2. Set `runtime: python` on the declaration and apply it.
3. Rewrite every `.go` entry under a bundle's `modules` as a `.py` module.
4. Do steps 1 to 3 before the upgrade. If no declaration used `runtime: go`,
   there is nothing to do.

#### The catalog stops shipping seven sample packages

The catalog no longer lists these packages, and importing one answers `404`:

- `samples.substrate.reamde.dev/health`
- `samples.substrate.reamde.dev/fitness`
- `samples.substrate.reamde.dev/commerce`
- `samples.substrate.reamde.dev/routines`
- `samples.substrate.reamde.dev/journal`
- `samples.substrate.reamde.dev/food`
- `samples.substrate.reamde.dev/places`

```
POST /api/v1/catalog/samples.substrate.reamde.dev%2Fhealth/import
→ 404 {"error": {"code": "not_found", "message": "substrate: not found: bundle \"samples.substrate.reamde.dev/health\""}}
```

`substratectl import samples.substrate.reamde.dev/health` fails the same
way. `samples.substrate.reamde.dev/scheduling` stays.

A repository that imported one of them keeps its copy under its own
authority (for example `<authority>/health/medication`) and every record of
it. The catalog no longer offers an upgrade for that copy.

##### What to do

1. For a repository that already holds a copy, nothing.
2. To declare one of these packages on a new repository, take its files from
   `samples/<name>/` at tag `v0.74.0` and declare them by hand. No catalog
   door serves them.

## [0.74.0](https://github.com/geoah/substrate/compare/v0.73.0...v0.74.0) (2026-09-10)

### ⚠ BREAKING CHANGES

* **substrate:** fold the optional seams into Dataset and Service ([f3c7a13](https://github.com/geoah/substrate/commit/f3c7a130dee9462c179dda58ac4b88fe16cc3552))

## [0.73.0](https://github.com/geoah/substrate/compare/v0.72.0...v0.73.0) (2026-09-10)

### ⚠ BREAKING CHANGES

* **blobs:** remove the S3 backend and ship fs alone ([4fcea33](https://github.com/geoah/substrate/commit/4fcea33a80480113d748ac73bad7d8d509f3fcf5))

### Upgrade notes

#### The `s3` blob backend and every `SUBSTRATE_BLOB_*` variable are removed

Blob bytes live in one place now: `$SUBSTRATE_DATA_ROOT/repositories/<authority>/blobs/<digest>`.
`SUBSTRATE_BLOB_STORE` and every `SUBSTRATE_BLOB_S3_*` variable are no
longer read, so a host that still sets them boots and ignores them. A
deployment that ran `SUBSTRATE_BLOB_STORE=s3` boots without its blob bytes,
because nothing reads the bucket (decision record 0075).

`snapshot.json` loses its `blobLocation` key, and its key set is closed. A
snapshot or export written by v0.72.0 or earlier carries the key, so
`repository verify` reports a `snapshot:` finding on it, and a v0.73.0
`substratectl export` refuses an archive from an older server:

```
the archive's snapshot.json does not read: json: unknown field "blobLocation"
```

The boot ignores `snapshot.json`, so restoring such a copy still imports it.

##### What to do

1. With `SUBSTRATE_BLOB_STORE` unset or `fs`, remove the variable. Nothing
   else changes.
2. With `s3`, stop the server first. Copy each object
   `<prefix><authority>/<digest>` from the bucket to
   `$SUBSTRATE_DATA_ROOT/repositories/<authority>/blobs/<digest>`. Remove
   every `SUBSTRATE_BLOB_*` variable. Start v0.73.0 and run
   `substratectl repository verify <repository>` for each repository.
3. After the upgrade, take a new `substratectl export` or
   `substratectl repository snapshot` to replace any copy written before it.

## [0.72.0](https://github.com/geoah/substrate/compare/v0.71.0...v0.72.0) (2026-09-10)

### ⚠ BREAKING CHANGES

* **cli,api:** remove edit, user password, user totp, reembed route ([2a4112a](https://github.com/geoah/substrate/commit/2a4112ac5f0e905bb341390d4bbef3596e60f133))
* **cli,api:** remove edit, user password, user totp, reembed route ([dc52087](https://github.com/geoah/substrate/commit/dc52087ab83b248cd6297585dcc4f34e6afb8e46))

### Upgrade notes

#### `substratectl edit`, `user password`, `user totp` and `POST /api/v1/embeddings/reembed` are removed

Three `substratectl` commands and one REST route are gone. `substratectl
edit`, `substratectl user password` and `substratectl user totp` are unknown
commands. `POST /api/v1/embeddings/reembed` answers `404`. The auth routes the
two `user` commands called (`POST /password`, `POST /totp/enroll`,
`POST /totp`) are still served; the console's account page uses them.

With the route gone, the `embeddings` entry in
`GET /.well-known/substrate/server.json` lists `"surfaces": ["graphql"]`
instead of `["rest", "graphql"]`. (GraphQL was removed later, and on current
releases the entry lists `["rest"]`.)

##### What to do

1. Replace `substratectl edit <kind> <id>` with a get, an edit and an apply:

   ```
   substratectl get ada.example.com/tasks/task t1 -o yaml > t1.yaml
   $EDITOR t1.yaml
   substratectl apply -f t1.yaml
   ```

2. Change a password or a second factor on the console's account page.
3. Replace a call to `POST /api/v1/embeddings/reembed` with the operator
   command on the server's host. It runs beside a live server:

   ```
   substratectl --dsn "$DATABASE_URL" repository reembed <repository>
   ```

   `--all` takes the place of the route's `{"all": true}` body.

## [0.71.0](https://github.com/geoah/substrate/compare/v0.70.0...v0.71.0) (2026-09-09)

### ⚠ BREAKING CHANGES

* remove recovery enroll and the deprecated upgrade renames ([244df02](https://github.com/geoah/substrate/commit/244df022c8a755109274ca82857ed44d32162ac9))

### Upgrade notes

#### `POST /recovery/enroll` is removed and `upgrade.renames` leaves the wire

Two removals, both for clients older than the server.

`POST /recovery/enroll` and `substratectl recovery enroll` are gone. The
server no longer routes the path. Registration is the only writer of the
`recoverykey` record, and nothing adds one later.

The `upgrade` object no longer carries `renames`. It appears on a catalog
entry (`GET /api/v1/catalog`, `GET /api/v1/catalog/{id}`) and in
`GET /api/v1/vocabulary/upgrade`. Each rename was always listed in `steps`
too, as `"step": "rename"`:

```json
{"available": true, "from": 3, "to": 4,
 "renames": [{"kind": "ada.example.com/tasks/task", "from": "due", "to": "dueAt", "records": 12}],
 "steps": [{"step": "rename", "kind": "ada.example.com/tasks/task", "property": "dueAt",
            "from": "due", "to": "dueAt", "records": 12}],
 "work": 12, "lossy": false}
```

From v0.71.0 the same object has no `renames` key.

##### What to do

1. Read renames from `upgrade.steps`, keeping the entries whose `step` is
   `rename`. `kind`, `from`, `to` and `records` hold the values `renames`
   held.
2. Remove any call to `POST /recovery/enroll` or
   `substratectl recovery enroll`. There is no replacement.

## [0.70.0](https://github.com/geoah/substrate/compare/v0.69.0...v0.70.0) (2026-09-09)

### ⚠ BREAKING CHANGES

* **engine:** squash the migrations into one initial schema ([0b98b51](https://github.com/geoah/substrate/commit/0b98b516af369f54bf8959fc60928dda6528eeff))

### Upgrade notes

#### The server refuses every database migrated before v0.70.0

v0.70.0 replaces the 25 files in `internal/engine/migrations/` with one
`0001_init.up.sql` that builds the final schema. A database an earlier binary
migrated records migrations 2 to 25 and an older hash for 1, so the boot
refuses it before applying or serving anything:

```
substrate/engine: the database applied migrations this binary does not carry: 24 migration(s) recorded that this binary does not carry (it carries up to 1), ...
substrate/engine: 1 migration(s) this database applied are not the ones this binary carries. ...
```

There is no in-place upgrade. The data root carries over instead: a boot
against an empty database imports every repository directory under
`SUBSTRATE_DATA_ROOT`. That works only for a data root v0.69.0 wrote.
v0.69.0 removed the reads of older stores, so a `repository.json` with
`"format": 3` (written by v0.68.0 and earlier) is refused by v0.69.0 and
v0.70.0 alike.

##### What to do

1. Stop the v0.69.0 server. Back up `$SUBSTRATE_DATA_ROOT` and
   `SUBSTRATE_CREDENTIAL_KEY`.
2. Create an empty database and point `DATABASE_URL` at it.
3. Start v0.70.0 with the same `SUBSTRATE_DATA_ROOT` and
   `SUBSTRATE_CREDENTIAL_KEY`. The boot creates each repository's row from
   its directory and replays its changelog.
4. For each repository, run
   `SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify <repository>`.
5. Expect clients to re-list once: the import mints a new history
   generation, so saved change cursors no longer resume.
6. On v0.68.0 or earlier, no path exists: register again on v0.70.0 and
   write the data back.

## [0.69.0](https://github.com/geoah/substrate/compare/v0.68.0...v0.69.0) (2026-09-09)

### ⚠ BREAKING CHANGES

* **engine:** drop the dialect ladders and every older-store read path ([fd6ddf4](https://github.com/geoah/substrate/commit/fd6ddf4c653ed28ea5fc4e83c46b9aa3093bed05))

## [0.68.0](https://github.com/geoah/substrate/compare/v0.67.0...v0.68.0) (2026-09-09)

### ⚠ BREAKING CHANGES

* retire plural from the wire, the CLI and the console ([efdf680](https://github.com/geoah/substrate/commit/efdf6808be5a985e364ebbeb5e17a281fdf0a202))

## [0.67.0](https://github.com/geoah/substrate/compare/v0.66.0...v0.67.0) (2026-09-09)

### ⚠ BREAKING CHANGES

* **api:** remove the OpenAPI document and its well-known route ([f4e19bc](https://github.com/geoah/substrate/commit/f4e19bc3678ad892e4d2b5ce537ab202a53db7a6))

## [0.66.0](https://github.com/geoah/substrate/compare/v0.65.0...v0.66.0) (2026-09-09)

### ⚠ BREAKING CHANGES

* log in with the repository name and drop the username ([#473](https://github.com/geoah/substrate/issues/473)) ([d13dedb](https://github.com/geoah/substrate/commit/d13dedbde502231e2ed1681e9cdf1e52f60ca120))

### Added

* log in with the repository name and drop the username ([#473](https://github.com/geoah/substrate/issues/473)) ([d13dedb](https://github.com/geoah/substrate/commit/d13dedbde502231e2ed1681e9cdf1e52f60ca120))

## [0.65.0](https://github.com/geoah/substrate/compare/v0.64.2...v0.65.0) (2026-09-09)

### Added

* **api:** honor Idempotency-Key on creates, calls, merge and split ([#458](https://github.com/geoah/substrate/issues/458)) ([0bf90b6](https://github.com/geoah/substrate/commit/0bf90b65fab41dd00240fae8bd6789f7828b6ae3))

## [0.64.2](https://github.com/geoah/substrate/compare/v0.64.1...v0.64.2) (2026-09-09)

### Fixed

* **engine:** restore the former-id conflict message the #456 squash reverted ([#460](https://github.com/geoah/substrate/issues/460)) ([f56c9bc](https://github.com/geoah/substrate/commit/f56c9bc74441451c2fe526177585a51ecb1582df))

## [0.64.1](https://github.com/geoah/substrate/compare/v0.64.0...v0.64.1) (2026-09-09)

### Fixed

* **engine:** point a former-id conflict at the canonical id ([#457](https://github.com/geoah/substrate/issues/457)) ([796130c](https://github.com/geoah/substrate/commit/796130c04bf28527d8b8d2071adf2c9215507901))

## [0.64.0](https://github.com/geoah/substrate/compare/v0.63.0...v0.64.0) (2026-09-08)

### Added

* **catalog:** preview an imported sample's upgrade and pin its requires ([#444](https://github.com/geoah/substrate/issues/444)) ([a0fec3d](https://github.com/geoah/substrate/commit/a0fec3dce9cd5fdb898cd7aa6ac8a44eae604018))

## [0.63.0](https://github.com/geoah/substrate/compare/v0.62.1...v0.63.0) (2026-09-08)

### Added

* **api:** preview conversion steps and confirm a lossy upgrade ([#438](https://github.com/geoah/substrate/issues/438)) ([bf682f7](https://github.com/geoah/substrate/commit/bf682f73f3a5b75aba96ddca23001741fea83895))

## [0.62.1](https://github.com/geoah/substrate/compare/v0.62.0...v0.62.1) (2026-09-08)

### Fixed

* **engine:** record an accepted webhook in the ledger before its 202 ([#437](https://github.com/geoah/substrate/issues/437)) ([89e0231](https://github.com/geoah/substrate/commit/89e02317829c4681b2339ef757a4d83bd9d7a783))

## [0.62.0](https://github.com/geoah/substrate/compare/v0.61.0...v0.62.0) (2026-09-08)

### Added

* **api:** export a repository's recovery snapshot over the API ([#436](https://github.com/geoah/substrate/issues/436)) ([fc99162](https://github.com/geoah/substrate/commit/fc991629f4076150a980e2253d139d56f0e341ab))

## [0.61.0](https://github.com/geoah/substrate/compare/v0.60.0...v0.61.0) (2026-09-08)

### ⚠ BREAKING CHANGES

* **engine:** fold trigger bookkeeping from delivery changelog entries ([#426](https://github.com/geoah/substrate/issues/426)) ([6d09b11](https://github.com/geoah/substrate/commit/6d09b111f0bf1adcf6c52e896f6abac70e8e4707))

### Added

* **vocabulary:** backfill a required default and remap an enum value on live records ([#433](https://github.com/geoah/substrate/issues/433)) ([6df3e11](https://github.com/geoah/substrate/commit/6df3e11681fc28271dc015b75cef5007e62ba2ff))
* **engine:** fold trigger bookkeeping from delivery changelog entries ([#426](https://github.com/geoah/substrate/issues/426)) ([6d09b11](https://github.com/geoah/substrate/commit/6d09b111f0bf1adcf6c52e896f6abac70e8e4707))

## [0.60.0](https://github.com/geoah/substrate/compare/v0.59.0...v0.60.0) (2026-09-08)

### Added

* **cli:** snapshot a stopped repository and verify its blobs and secrets ([#430](https://github.com/geoah/substrate/issues/430)) ([71a8f4c](https://github.com/geoah/substrate/commit/71a8f4c799e4f2493076cd5aa6b9b2be469b0576))

### Fixed

* **runner:** restart a child killed between lookup and roundtrip ([#431](https://github.com/geoah/substrate/issues/431)) ([7d18408](https://github.com/geoah/substrate/commit/7d1840893c7c6ffcb784182b373c7a253215dce6))

## [0.59.0](https://github.com/geoah/substrate/compare/v0.58.0...v0.59.0) (2026-09-08)

### Added

* **api:** publish an OpenAPI document for the REST surface ([#429](https://github.com/geoah/substrate/issues/429)) ([1e16a29](https://github.com/geoah/substrate/commit/1e16a2912fd9e13abf0a65eb829433c5a2bc9796))

## [0.58.0](https://github.com/geoah/substrate/compare/v0.57.0...v0.58.0) (2026-09-08)

### Added

* **vocabulary:** rename a property with live records inside the apply ([#424](https://github.com/geoah/substrate/issues/424)) ([71a2212](https://github.com/geoah/substrate/commit/71a22127814cb263f1ab959a5cadc6ad277944be))

## [0.57.0](https://github.com/geoah/substrate/compare/v0.56.0...v0.57.0) (2026-09-08)

### ⚠ BREAKING CHANGES

* **engine:** acknowledge a write only once its lines are on disk ([#423](https://github.com/geoah/substrate/issues/423)) ([88cfc15](https://github.com/geoah/substrate/commit/88cfc1515988810f9db5726e5b993b7553afeea9))

### Added

* **api:** mark the supported REST features stable ([#428](https://github.com/geoah/substrate/issues/428)) ([6e28479](https://github.com/geoah/substrate/commit/6e2847984a21408501b7699232ea8c79c7671e0d))

### Fixed

* **engine:** derive property_offers again on rebuild and import ([#410](https://github.com/geoah/substrate/issues/410)) ([9258c3e](https://github.com/geoah/substrate/commit/9258c3e672b59ba49141aea97f4d46c8973ce50c))
* **engine:** acknowledge a write only once its lines are on disk ([#423](https://github.com/geoah/substrate/issues/423)) ([88cfc15](https://github.com/geoah/substrate/commit/88cfc1515988810f9db5726e5b993b7553afeea9))

## [0.56.0](https://github.com/geoah/substrate/compare/v0.55.0...v0.56.0) (2026-09-08)

### ⚠ BREAKING CHANGES

* **api:** name each affected record on a change row and drop fold ([#418](https://github.com/geoah/substrate/issues/418)) ([6334a31](https://github.com/geoah/substrate/commit/6334a31830a64dcb7e81b4961f367065238c8db4))

### Added

* **api:** name each affected record on a change row and drop fold ([#418](https://github.com/geoah/substrate/issues/418)) ([6334a31](https://github.com/geoah/substrate/commit/6334a31830a64dcb7e81b4961f367065238c8db4))

## [0.55.0](https://github.com/geoah/substrate/compare/v0.54.0...v0.55.0) (2026-09-08)

### Added

* **engine:** stamp each record with the kind version that wrote it ([#412](https://github.com/geoah/substrate/issues/412)) ([49bc598](https://github.com/geoah/substrate/commit/49bc598179c1c70159efe02a48d122d1175f84e9))

## [0.54.0](https://github.com/geoah/substrate/compare/v0.53.0...v0.54.0) (2026-09-08)

### ⚠ BREAKING CHANGES

* **engine:** write both dialects into repository.json when stamped ([#419](https://github.com/geoah/substrate/issues/419)) ([4ffb51a](https://github.com/geoah/substrate/commit/4ffb51a72c90044822cf7559cef72d16d1e424ad))

### Added

* **api:** take a version precondition on delete, merge and split ([#420](https://github.com/geoah/substrate/issues/420)) ([aa2c10b](https://github.com/geoah/substrate/commit/aa2c10bac1fa4ca897999f992b3f1090b0f9391a))

### Fixed

* **engine:** re-derive fts when a kind edit changes what it indexes ([#421](https://github.com/geoah/substrate/issues/421)) ([3d7c2e0](https://github.com/geoah/substrate/commit/3d7c2e002da4c345dfacb1da46da4be6870e7457))
* **engine:** write both dialects into repository.json when stamped ([#419](https://github.com/geoah/substrate/issues/419)) ([4ffb51a](https://github.com/geoah/substrate/commit/4ffb51a72c90044822cf7559cef72d16d1e424ad))

## [0.53.0](https://github.com/geoah/substrate/compare/v0.52.1...v0.53.0) (2026-09-08)

### ⚠ BREAKING CHANGES

* **engine:** re-key each repository once, then refuse legacy payloads ([#413](https://github.com/geoah/substrate/issues/413)) ([94b9c11](https://github.com/geoah/substrate/commit/94b9c111942fb07f7abbb8d9f2107e026d3e0a1e))

### Added

* **engine:** re-key each repository once, then refuse legacy payloads ([#413](https://github.com/geoah/substrate/issues/413)) ([94b9c11](https://github.com/geoah/substrate/commit/94b9c111942fb07f7abbb8d9f2107e026d3e0a1e))

## [0.52.1](https://github.com/geoah/substrate/compare/v0.52.0...v0.52.1) (2026-09-08)

### Fixed

* **engine:** queue every embeddable property on import ([#409](https://github.com/geoah/substrate/issues/409)) ([7d222a5](https://github.com/geoah/substrate/commit/7d222a51e6bae43e0b93f1f0c2662365d3f9bfc7))

## [0.52.0](https://github.com/geoah/substrate/compare/v0.51.0...v0.52.0) (2026-09-08)

### ⚠ BREAKING CHANGES

* **changelog:** frame each line by transaction, cut a torn one whole ([#405](https://github.com/geoah/substrate/issues/405)) ([fa4acb3](https://github.com/geoah/substrate/commit/fa4acb3009683c6ee038f25af967f9347e502e12))

### Added

* **changelog:** frame each line by transaction, cut a torn one whole ([#405](https://github.com/geoah/substrate/issues/405)) ([fa4acb3](https://github.com/geoah/substrate/commit/fa4acb3009683c6ee038f25af967f9347e502e12))
* **engine:** hold a vocabulary batch's writes to its candidate registry ([#414](https://github.com/geoah/substrate/issues/414)) ([c103e01](https://github.com/geoah/substrate/commit/c103e014ffa743f071479c481dcd92717d3a89e9))
* **api:** serve the refused core boot upgrade at /vocabulary/upgrade ([#411](https://github.com/geoah/substrate/issues/411)) ([59e42a2](https://github.com/geoah/substrate/commit/59e42a29a613fe251d34b60fd30a34c0b1e29f76))

## [0.51.0](https://github.com/geoah/substrate/compare/v0.50.0...v0.51.0) (2026-09-08)

### Added

* **gql:** name every non-core kind with its full authority ([#408](https://github.com/geoah/substrate/issues/408)) ([df0f356](https://github.com/geoah/substrate/commit/df0f3561eaf553170c42040c21a780266696ae77))
* **vocabulary:** refuse a retired name on every door ([#400](https://github.com/geoah/substrate/issues/400)) ([8e56dc4](https://github.com/geoah/substrate/commit/8e56dc4470e54bee966cf61588dc7dc0be1c2d3c))

### Fixed

* **engine:** match merge and split entries under both records' ids ([#387](https://github.com/geoah/substrate/issues/387)) ([320651c](https://github.com/geoah/substrate/commit/320651c5afef599d14d4fbee07f9f06637fddb00))

## [0.50.0](https://github.com/geoah/substrate/compare/v0.49.1...v0.50.0) (2026-09-08)

### ⚠ BREAKING CHANGES

* **api:** bind change cursors to a history generation ([#403](https://github.com/geoah/substrate/issues/403)) ([84c0914](https://github.com/geoah/substrate/commit/84c09142635d6dca3871ea987a780e77b00bf70d))

### Added

* **api:** bind change cursors to a history generation ([#403](https://github.com/geoah/substrate/issues/403)) ([84c0914](https://github.com/geoah/substrate/commit/84c09142635d6dca3871ea987a780e77b00bf70d))

## [0.49.1](https://github.com/geoah/substrate/compare/v0.49.0...v0.49.1) (2026-09-08)

### Fixed

* **engine:** publish the registry before the head signal ([#391](https://github.com/geoah/substrate/issues/391)) ([edef55c](https://github.com/geoah/substrate/commit/edef55c590bcfdfaad875b3b6617066747283285))

## [0.49.0](https://github.com/geoah/substrate/compare/v0.48.0...v0.49.0) (2026-09-08)

### Added

* **substratectl:** rewrap a repository directory with its recovery key ([#393](https://github.com/geoah/substrate/issues/393)) ([f496710](https://github.com/geoah/substrate/commit/f496710b964d7896e650fe88d58dd85c8087be2a))
* **catalog:** stamp an imported sample's origin and shipped version ([#399](https://github.com/geoah/substrate/issues/399)) ([c1677a2](https://github.com/geoah/substrate/commit/c1677a27dd5be951fa45bf2cef1ead480f2a5d61))

### Fixed

* **engine:** resume a boot import that died before its fold completed ([#397](https://github.com/geoah/substrate/issues/397)) ([4402b7c](https://github.com/geoah/substrate/commit/4402b7c771d528be21bafa686d604382af5881b7))
* **engine:** refuse a database a newer binary migrated ([#389](https://github.com/geoah/substrate/issues/389)) ([cd9a08f](https://github.com/geoah/substrate/commit/cd9a08f498b6de5bc733ad87e74acbad0ea6f869))
* **graphql:** serve int properties as Long in the preview schema ([#392](https://github.com/geoah/substrate/issues/392)) ([9129bb9](https://github.com/geoah/substrate/commit/9129bb9e9dce7af89434e1759666364a3173ed7e))
* **engine:** refuse a tightened pattern or bound with live rows ([#396](https://github.com/geoah/substrate/issues/396)) ([998a3e9](https://github.com/geoah/substrate/commit/998a3e969ddc7fb45903d74b934ff7186fcc71ec))
* **engine:** replay the removal of a record's last label ([#388](https://github.com/geoah/substrate/issues/388)) ([c2daa38](https://github.com/geoah/substrate/commit/c2daa38a20beb6eedb01af333ce2c23577ec147f))

## [0.48.0](https://github.com/geoah/substrate/compare/v0.47.0...v0.48.0) (2026-09-08)

### Added

* **api:** report REST as supported and GraphQL as preview in discovery ([#390](https://github.com/geoah/substrate/issues/390)) ([64363c5](https://github.com/geoah/substrate/commit/64363c553a93e8146f5a5500967ca0316014d2ff))

### Fixed

* **engine:** accept a blobref manifest on write, store its digest ([#395](https://github.com/geoah/substrate/issues/395)) ([5c79db0](https://github.com/geoah/substrate/commit/5c79db015b048ec50f1f603786ad49569edce13e))

## [0.47.0](https://github.com/geoah/substrate/compare/v0.46.0...v0.47.0) (2026-09-06)

### ⚠ BREAKING CHANGES

* **engine:** make the authority the repository id ([#356](https://github.com/geoah/substrate/issues/356)) ([44404c3](https://github.com/geoah/substrate/commit/44404c33f1b497de83e7f3733e4cb91d84b5cf41))

### Added

* **engine:** make the authority the repository id ([#356](https://github.com/geoah/substrate/issues/356)) ([44404c3](https://github.com/geoah/substrate/commit/44404c33f1b497de83e7f3733e4cb91d84b5cf41))

## [0.46.0](https://github.com/geoah/substrate/compare/v0.45.0...v0.46.0) (2026-09-05)

### ⚠ BREAKING CHANGES

* **engine:** keep the changelog in checksummed files per repository ([#355](https://github.com/geoah/substrate/issues/355)) ([57a6ae9](https://github.com/geoah/substrate/commit/57a6ae991038a8b3f3d17f58c908a9a94cb7f6ee))

### Added

* **engine:** keep the changelog in checksummed files per repository ([#355](https://github.com/geoah/substrate/issues/355)) ([57a6ae9](https://github.com/geoah/substrate/commit/57a6ae991038a8b3f3d17f58c908a9a94cb7f6ee))

## [0.45.0](https://github.com/geoah/substrate/compare/v0.44.0...v0.45.0) (2026-09-05)

### Added

* **catalog:** a sample's mappings land when its provider is installed ([#354](https://github.com/geoah/substrate/issues/354)) ([50804f7](https://github.com/geoah/substrate/commit/50804f7e79949926745c7292630278e4b2693c8d))

## [0.44.0](https://github.com/geoah/substrate/compare/v0.43.0...v0.44.0) (2026-09-05)

### ⚠ BREAKING CHANGES

* **engine:** declare a mapping in the package that owns its target ([#351](https://github.com/geoah/substrate/issues/351)) ([b803da7](https://github.com/geoah/substrate/commit/b803da73c94c8f5cfccebf381d461224a3003919))

### Added

* **engine:** declare a mapping in the package that owns its target ([#351](https://github.com/geoah/substrate/issues/351)) ([b803da7](https://github.com/geoah/substrate/commit/b803da73c94c8f5cfccebf381d461224a3003919))

## [0.43.0](https://github.com/geoah/substrate/compare/v0.42.0...v0.43.0) (2026-09-05)

### ⚠ BREAKING CHANGES

* **catalog:** two tiers, and a sample imports under your authority ([#352](https://github.com/geoah/substrate/issues/352)) ([9a546ee](https://github.com/geoah/substrate/commit/9a546eeaa6daf7964242ea5dc067d98ecf02f429))

### Added

* **catalog:** two tiers, and a sample imports under your authority ([#352](https://github.com/geoah/substrate/issues/352)) ([9a546ee](https://github.com/geoah/substrate/commit/9a546eeaa6daf7964242ea5dc067d98ecf02f429))

## [0.42.0](https://github.com/geoah/substrate/compare/v0.41.0...v0.42.0) (2026-09-05)

### ⚠ BREAKING CHANGES

* **engine:** a provider package is published, not the token's to write ([#350](https://github.com/geoah/substrate/issues/350)) ([e28cc01](https://github.com/geoah/substrate/commit/e28cc0114bc0a54d6c71376e2af49ae6c6781c31))

### Added

* **engine:** a provider package is published, not the token's to write ([#350](https://github.com/geoah/substrate/issues/350)) ([e28cc01](https://github.com/geoah/substrate/commit/e28cc0114bc0a54d6c71376e2af49ae6c6781c31))

## [0.41.0](https://github.com/geoah/substrate/compare/v0.40.0...v0.41.0) (2026-09-05)

### ⚠ BREAKING CHANGES

* **vocabulary:** a kind reference is {authority}/{package}/{name} ([#348](https://github.com/geoah/substrate/issues/348)) ([fb3d51e](https://github.com/geoah/substrate/commit/fb3d51e7c957d4c730c97008e951f4ffd43e929a))

## [0.40.0](https://github.com/geoah/substrate/compare/v0.39.1...v0.40.0) (2026-09-05)

### ⚠ BREAKING CHANGES

* **api:** webhook URLs name the repository by its authority ([#345](https://github.com/geoah/substrate/issues/345)) ([e513ace](https://github.com/geoah/substrate/commit/e513aceecd0753a1dacd4547fdb17d3a0956070c))

### Added

* **api:** webhook URLs name the repository by its authority ([#345](https://github.com/geoah/substrate/issues/345)) ([e513ace](https://github.com/geoah/substrate/commit/e513aceecd0753a1dacd4547fdb17d3a0956070c))

## [0.39.1](https://github.com/geoah/substrate/compare/v0.39.0...v0.39.1) (2026-09-05)

### Fixed

* **auth:** registration copy says any hostname is the authority ([#342](https://github.com/geoah/substrate/issues/342)) ([33b0916](https://github.com/geoah/substrate/commit/33b0916f5870c253f8a468504a43bf3ccb4d7dc0))
* **console:** install jsdom storage over node's global localStorage ([#346](https://github.com/geoah/substrate/issues/346)) ([27a8392](https://github.com/geoah/substrate/commit/27a83921373e5ef8d293ed3e8519fdc7b8951a54))

## [0.39.0](https://github.com/geoah/substrate/compare/v0.38.0...v0.39.0) (2026-09-02)

### Added

* **auth:** register takes an authority, default <username>.<server host> ([#340](https://github.com/geoah/substrate/issues/340)) ([9dd4b68](https://github.com/geoah/substrate/commit/9dd4b68d33d210fa33fc21a790252d7d5b2c6c0e))

## [0.38.0](https://github.com/geoah/substrate/compare/v0.37.1...v0.38.0) (2026-09-02)

### Added

* public webhook endpoints fire triggers with the request payload ([#338](https://github.com/geoah/substrate/issues/338)) ([934d112](https://github.com/geoah/substrate/commit/934d1124dc37e8f7fc3d4f81f3c0c8798a182ece))

## [0.37.1](https://github.com/geoah/substrate/compare/v0.37.0...v0.37.1) (2026-09-01)

### Fixed

* **console:** route record reads by kind name, not the retired plural ([#337](https://github.com/geoah/substrate/issues/337)) ([c28b391](https://github.com/geoah/substrate/commit/c28b3914b721d79cb932832a62a556e14af4e358))

## [0.37.0](https://github.com/geoah/substrate/compare/v0.36.0...v0.37.0) (2026-09-01)

### ⚠ BREAKING CHANGES

* **api:** incoming drops createdAt and pages by the refs index ([#333](https://github.com/geoah/substrate/issues/333)) ([dd62a47](https://github.com/geoah/substrate/commit/dd62a4772d813766f91ba6ffd1bb7a2fedf095d5))

### Added

* **api:** incoming drops createdAt and pages by the refs index ([#333](https://github.com/geoah/substrate/issues/333)) ([dd62a47](https://github.com/geoah/substrate/commit/dd62a4772d813766f91ba6ffd1bb7a2fedf095d5))

## [0.36.0](https://github.com/geoah/substrate/compare/v0.35.0...v0.36.0) (2026-09-01)

### ⚠ BREAKING CHANGES

* replace edges with reference properties ([#320](https://github.com/geoah/substrate/issues/320)) ([13c0b38](https://github.com/geoah/substrate/commit/13c0b3848cbd0838b95012c1a37f7a99dd1265b0))

### Added

* replace edges with reference properties ([#320](https://github.com/geoah/substrate/issues/320)) ([13c0b38](https://github.com/geoah/substrate/commit/13c0b3848cbd0838b95012c1a37f7a99dd1265b0))

## [0.35.0](https://github.com/geoah/substrate/compare/v0.34.0...v0.35.0) (2026-08-31)

### Added

* sync recurring series and compute occurrences at read ([9d49d4f](https://github.com/geoah/substrate/commit/9d49d4f55e6e0498092910b561addf10617d9608))

## [0.34.0](https://github.com/geoah/substrate/compare/v0.33.3...v0.34.0) (2026-08-19)

### Added

* **engine:** refuse to open a changelog this binary cannot replay (#104) ([#301](https://github.com/geoah/substrate/issues/301)) ([ebc7a6d](https://github.com/geoah/substrate/commit/ebc7a6df3647be5e089a0792d895948fed859d3e))

## [0.33.3](https://github.com/geoah/substrate/compare/v0.33.2...v0.33.3) (2026-08-19)

### Fixed

* **google:** an inline photo past the source cap no longer replaces the mail body (#191) ([#300](https://github.com/geoah/substrate/issues/300)) ([29e856d](https://github.com/geoah/substrate/commit/29e856dc2e01ababe1df484b2a21ad198f2210ee))

## [0.33.2](https://github.com/geoah/substrate/compare/v0.33.1...v0.33.2) (2026-08-19)

### Fixed

* **engine:** confine the OAuth token-exchange dial to public egress (#241) ([#299](https://github.com/geoah/substrate/issues/299)) ([ae90497](https://github.com/geoah/substrate/commit/ae904975b27b8bb9f373d5cbdc2050caceea3685))

## [0.33.1](https://github.com/geoah/substrate/compare/v0.33.0...v0.33.1) (2026-08-19)

### Fixed

* **engine:** confine repository-chosen provider dials to public egress (#241) ([#297](https://github.com/geoah/substrate/issues/297)) ([212ad26](https://github.com/geoah/substrate/commit/212ad260e1e52052a6f597587f07e20c19ed4e29))

## [0.33.0](https://github.com/geoah/substrate/compare/v0.32.2...v0.33.0) (2026-08-19)

### ⚠ BREAKING CHANGES

* **engine:** a secret property may not alias another's sealed row (#233) ([#295](https://github.com/geoah/substrate/issues/295)) ([52ea542](https://github.com/geoah/substrate/commit/52ea542aa85dae48677d16242d5d5a4b5a7adcc2))

### Fixed

* **engine:** reseal and verified rebuild check the epoch anchor (#252) ([#296](https://github.com/geoah/substrate/issues/296)) ([b2f1e06](https://github.com/geoah/substrate/commit/b2f1e0627a1723d59380277290fdb8d3a486a076))
* **engine:** a secret property may not alias another's sealed row (#233) ([#295](https://github.com/geoah/substrate/issues/295)) ([52ea542](https://github.com/geoah/substrate/commit/52ea542aa85dae48677d16242d5d5a4b5a7adcc2))

## [0.32.2](https://github.com/geoah/substrate/compare/v0.32.1...v0.32.2) (2026-08-19)

### Fixed

* **engine:** a referenceProp title skips a sensitive referent property (#232) ([#294](https://github.com/geoah/substrate/issues/294)) ([bced4d7](https://github.com/geoah/substrate/commit/bced4d779f3362bec963d6d9e7225510fee3b38d))

## [0.32.1](https://github.com/geoah/substrate/compare/v0.32.0...v0.32.1) (2026-08-19)

### Fixed

* **ci:** console typecheck actually typechecks the source (#254) ([#292](https://github.com/geoah/substrate/issues/292)) ([d03bf1b](https://github.com/geoah/substrate/commit/d03bf1bb011f684f6b75a722e3ce8ffea37582eb))

## [0.32.0](https://github.com/geoah/substrate/compare/v0.31.0...v0.32.0) (2026-08-19)

### ⚠ BREAKING CHANGES

* **api:** every kind carries an authority, drop the URL dot-rule (#131) ([#288](https://github.com/geoah/substrate/issues/288)) ([3c705bd](https://github.com/geoah/substrate/commit/3c705bd38462cbfadb9fa50fd7c465365f53cc3a))

## [0.31.0](https://github.com/geoah/substrate/compare/v0.30.0...v0.31.0) (2026-08-19)

### Added

* **api:** field-addressable problems and a body-fault error code (#128) ([#284](https://github.com/geoah/substrate/issues/284)) ([014ecf7](https://github.com/geoah/substrate/commit/014ecf754df8231780e89df17a656054d5227234))

## [0.30.0](https://github.com/geoah/substrate/compare/v0.29.0...v0.30.0) (2026-08-19)

### ⚠ BREAKING CHANGES

* **vocabulary:** rename kind/subject/weight properties before they freeze (#125) ([#283](https://github.com/geoah/substrate/issues/283)) ([1fff215](https://github.com/geoah/substrate/commit/1fff215c04098ca515c49debcd459fc542f1ed4a))

## [0.29.0](https://github.com/geoah/substrate/compare/v0.28.0...v0.29.0) (2026-08-19)

### ⚠ BREAKING CHANGES

* **vocabulary:** retype run provenance fields to references (#51) ([#282](https://github.com/geoah/substrate/issues/282)) ([c400428](https://github.com/geoah/substrate/commit/c400428db07c441235bf5aad07b91c9a00db8f5b))

## [0.28.0](https://github.com/geoah/substrate/compare/v0.27.0...v0.28.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **vocabulary:** retype function timeout to the duration datatype (#51) ([#281](https://github.com/geoah/substrate/issues/281)) ([4df651f](https://github.com/geoah/substrate/commit/4df651f6f5d1c62ece08ef7ea083d503bdf8dad2))

## [0.27.0](https://github.com/geoah/substrate/compare/v0.26.0...v0.27.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **vocabulary:** make body a declarable property, index it per-property (#68) ([#278](https://github.com/geoah/substrate/issues/278)) ([f705617](https://github.com/geoah/substrate/commit/f705617234e3c2044a9ec691d9f03cd25aa44d90))

### Added

* **vocabulary:** make body a declarable property, index it per-property (#68) ([#278](https://github.com/geoah/substrate/issues/278)) ([f705617](https://github.com/geoah/substrate/commit/f705617234e3c2044a9ec691d9f03cd25aa44d90))

## [0.26.0](https://github.com/geoah/substrate/compare/v0.25.0...v0.26.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **vocabulary:** rename core kind properties to their settled names (#51) ([#277](https://github.com/geoah/substrate/issues/277)) ([e75d6d9](https://github.com/geoah/substrate/commit/e75d6d968c6ea205c21634866796f8976c0de9b8))

## [0.25.0](https://github.com/geoah/substrate/compare/v0.24.0...v0.25.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **vocabulary:** retire `data.names.plural` from the dialect ([#276](https://github.com/geoah/substrate/issues/276)) ([b232aca](https://github.com/geoah/substrate/commit/b232aca667fc51d893f36fc90e509b6516ea8865))

### Added

* **vocabulary:** retire `data.names.plural` from the dialect ([#276](https://github.com/geoah/substrate/issues/276)) ([b232aca](https://github.com/geoah/substrate/commit/b232aca667fc51d893f36fc90e509b6516ea8865))

## [0.24.0](https://github.com/geoah/substrate/compare/v0.23.0...v0.24.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **vocabulary:** declare core kind invariants, titles and run `reason` ([#273](https://github.com/geoah/substrate/issues/273)) ([27eb051](https://github.com/geoah/substrate/commit/27eb051d8a3cfe031a63cce2e62ea061985568c2))

### Added

* **vocabulary:** declare core kind invariants, titles and run `reason` ([#273](https://github.com/geoah/substrate/issues/273)) ([27eb051](https://github.com/geoah/substrate/commit/27eb051d8a3cfe031a63cce2e62ea061985568c2))

## [0.23.0](https://github.com/geoah/substrate/compare/v0.22.2...v0.23.0) (2026-08-18)

### Added

* **vocabulary:** extract occurrencelog and recurring traits from the copied log kinds ([#274](https://github.com/geoah/substrate/issues/274)) ([dc0a150](https://github.com/geoah/substrate/commit/dc0a150bb63a7ef9c812e8f68b6c958c5c80bc9f))

## [0.22.2](https://github.com/geoah/substrate/compare/v0.22.1...v0.22.2) (2026-08-18)

### Fixed

* **engine:** bind a sealed payload to its address with GCM AAD ([#272](https://github.com/geoah/substrate/issues/272)) ([f68405e](https://github.com/geoah/substrate/commit/f68405e109120ae7176d994d7a2bcac81ff066b0))

## [0.22.1](https://github.com/geoah/substrate/compare/v0.22.0...v0.22.1) (2026-08-18)

### Fixed

* **vocabulary:** validate a `permissions.network` entry's grammar ([#271](https://github.com/geoah/substrate/issues/271)) ([63f8754](https://github.com/geoah/substrate/commit/63f875477f347bdb023fd08cab2f8c59b72c2369))

## [0.22.0](https://github.com/geoah/substrate/compare/v0.21.0...v0.22.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **api:** operational lists answer `{items, cursor?}`, not a bare array ([#270](https://github.com/geoah/substrate/issues/270)) ([4b2ea3a](https://github.com/geoah/substrate/commit/4b2ea3a71ac6cc5cb33e2326db7a7d2cbc060379))

### Added

* **api:** operational lists answer `{items, cursor?}`, not a bare array ([#270](https://github.com/geoah/substrate/issues/270)) ([4b2ea3a](https://github.com/geoah/substrate/commit/4b2ea3a71ac6cc5cb33e2326db7a7d2cbc060379))

## [0.21.0](https://github.com/geoah/substrate/compare/v0.20.1...v0.21.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **api:** remove `gated` from the closed error-code set ([#269](https://github.com/geoah/substrate/issues/269)) ([1cd8d45](https://github.com/geoah/substrate/commit/1cd8d45df0cb357a4699b3cae0092993eac4962f))

### Fixed

* **api:** remove `gated` from the closed error-code set ([#269](https://github.com/geoah/substrate/issues/269)) ([1cd8d45](https://github.com/geoah/substrate/commit/1cd8d45df0cb357a4699b3cae0092993eac4962f))

## [0.20.1](https://github.com/geoah/substrate/compare/v0.20.0...v0.20.1) (2026-08-18)

### Fixed

* **sandbox:** refuse a network body's connect to private ranges ([#265](https://github.com/geoah/substrate/issues/265)) ([59582a4](https://github.com/geoah/substrate/commit/59582a4d6a860b48be7e8596f6e19ff7ad8618bc))

## [0.20.0](https://github.com/geoah/substrate/compare/v0.19.0...v0.20.0) (2026-08-18)

### ⚠ BREAKING CHANGES

* **engine:** require SUBSTRATE_CREDENTIAL_KEY be base64 of 32 bytes ([#264](https://github.com/geoah/substrate/issues/264)) ([381e809](https://github.com/geoah/substrate/commit/381e809338e6c76ca0eb19a5009fece03da449ed))

### Fixed

* **engine:** require SUBSTRATE_CREDENTIAL_KEY be base64 of 32 bytes ([#264](https://github.com/geoah/substrate/issues/264)) ([381e809](https://github.com/geoah/substrate/commit/381e809338e6c76ca0eb19a5009fece03da449ed))
* **llm:** scrub the provider key from a completions 401 error ([#263](https://github.com/geoah/substrate/issues/263)) ([a0ca942](https://github.com/geoah/substrate/commit/a0ca942cf9edc79193c38eee1aef435e104c6358))

## [0.19.0](https://github.com/geoah/substrate/compare/v0.18.0...v0.19.0) (2026-08-17)

### ⚠ BREAKING CHANGES

* **api:** drop plurals and separators from the path grammar ([#260](https://github.com/geoah/substrate/issues/260)) ([5faff66](https://github.com/geoah/substrate/commit/5faff66232ff8152cdc3d0de27f23a823fc2515c))

### Added

* **api:** drop plurals and separators from the path grammar ([#260](https://github.com/geoah/substrate/issues/260)) ([5faff66](https://github.com/geoah/substrate/commit/5faff66232ff8152cdc3d0de27f23a823fc2515c))

## [0.18.0](https://github.com/geoah/substrate/compare/v0.17.0...v0.18.0) (2026-08-17)

### ⚠ BREAKING CHANGES

* **engine:** a reference may pin a trait, so a shared kind owns any account ([#261](https://github.com/geoah/substrate/issues/261)) ([3e0bd89](https://github.com/geoah/substrate/commit/3e0bd8980dab7f8e80ef9d23844303358baf1eca))
* **vocabulary:** refuse edge properties a rel does not declare ([#243](https://github.com/geoah/substrate/issues/243)) ([854f7f6](https://github.com/geoah/substrate/commit/854f7f66ca3ffdb92538972c4548f9e9d8f8c591))

### Added

* **engine:** a reference may pin a trait, so a shared kind owns any account ([#261](https://github.com/geoah/substrate/issues/261)) ([3e0bd89](https://github.com/geoah/substrate/commit/3e0bd8980dab7f8e80ef9d23844303358baf1eca))
* **vocabulary:** refuse edge properties a rel does not declare ([#243](https://github.com/geoah/substrate/issues/243)) ([854f7f6](https://github.com/geoah/substrate/commit/854f7f66ca3ffdb92538972c4548f9e9d8f8c591))

## [0.17.0](https://github.com/geoah/substrate/compare/v0.16.0...v0.17.0) (2026-08-17)

### ⚠ BREAKING CHANGES

* **engine:** the GC cascade follows an `ownerRef` reference ([#256](https://github.com/geoah/substrate/issues/256)) ([b63b5ae](https://github.com/geoah/substrate/commit/b63b5ae602ed8ff2fdab3b4b61eb1a0db47ec3af))

### Added

* **engine:** the GC cascade follows an `ownerRef` reference ([#256](https://github.com/geoah/substrate/issues/256)) ([b63b5ae](https://github.com/geoah/substrate/commit/b63b5ae602ed8ff2fdab3b4b61eb1a0db47ec3af))

## [0.16.0](https://github.com/geoah/substrate/compare/v0.15.0...v0.16.0) (2026-08-17)

### ⚠ BREAKING CHANGES

* delete kinds:gen, the dialect-1 rung, the registration seed ([#245](https://github.com/geoah/substrate/issues/245)) ([2a18054](https://github.com/geoah/substrate/commit/2a18054d502146fcc4036c95b930c68403490759))

### Added

* delete kinds:gen, the dialect-1 rung, the registration seed ([#245](https://github.com/geoah/substrate/issues/245)) ([2a18054](https://github.com/geoah/substrate/commit/2a18054d502146fcc4036c95b930c68403490759))

## [0.15.0](https://github.com/geoah/substrate/compare/v0.14.0...v0.15.0) (2026-08-17)

### ⚠ BREAKING CHANGES

* **api:** stamp unfrozen discovery features `beta`, derive the list ([#244](https://github.com/geoah/substrate/issues/244)) ([cd84a73](https://github.com/geoah/substrate/commit/cd84a73c153a48a04460cbfe9eb08987f9bc45c0))

### Added

* **api:** stamp unfrozen discovery features `beta`, derive the list ([#244](https://github.com/geoah/substrate/issues/244)) ([cd84a73](https://github.com/geoah/substrate/commit/cd84a73c153a48a04460cbfe9eb08987f9bc45c0))

## [0.14.0](https://github.com/geoah/substrate/compare/v0.13.0...v0.14.0) (2026-08-17)

### Added

* **engine:** admit `<authority>/*` in a policy selector's kinds ([#224](https://github.com/geoah/substrate/issues/224)) ([4e086d0](https://github.com/geoah/substrate/commit/4e086d0d20b4f00d7d7ca137bc15f9dec4a20d09))

### Fixed

* **release:** ship substratectl in both runtime images ([#218](https://github.com/geoah/substrate/issues/218)) ([b71a964](https://github.com/geoah/substrate/commit/b71a96434a4fec72ecff15341bb533c45397382d))

## [0.13.0](https://github.com/geoah/substrate/compare/v0.12.0...v0.13.0) (2026-08-17)

### Added

* **engine:** store blob bytes on fs or s3, not only in `bytea` ([#238](https://github.com/geoah/substrate/issues/238)) ([75e74de](https://github.com/geoah/substrate/commit/75e74de1ce4a802c36acf9fe6a5111fe718d67d5))

## [0.12.0](https://github.com/geoah/substrate/compare/v0.11.0...v0.12.0) (2026-08-17)

### ⚠ BREAKING CHANGES

* **engine:** buy embeddings from an llmprovider row, not SUBSTRATE_LLM_* ([#227](https://github.com/geoah/substrate/issues/227)) ([1e70268](https://github.com/geoah/substrate/commit/1e70268efbc3e352011dda2a942541c1ddb4b253))

### Added

* **engine:** buy embeddings from an llmprovider row, not SUBSTRATE_LLM_* ([#227](https://github.com/geoah/substrate/issues/227)) ([1e70268](https://github.com/geoah/substrate/commit/1e70268efbc3e352011dda2a942541c1ddb4b253))

## [0.11.0](https://github.com/geoah/substrate/compare/v0.10.0...v0.11.0) (2026-08-17)

### ⚠ BREAKING CHANGES

* **engine:** enforce `required` and materialize `default` on writes ([#225](https://github.com/geoah/substrate/issues/225)) ([722cfd7](https://github.com/geoah/substrate/commit/722cfd76027e5b4b6dda2675307e561d352ce62e))
* **api:** mark each discovery feature's surfaces; drop unserved embeddings ([#187](https://github.com/geoah/substrate/issues/187)) ([18ff5a7](https://github.com/geoah/substrate/commit/18ff5a776a63857495853d3f9ef6bf2cdaf87c3c))
* **vocabulary:** actors carry the full authority, retiring connector: ([#226](https://github.com/geoah/substrate/issues/226)) ([1ed3470](https://github.com/geoah/substrate/commit/1ed3470c1985ca5600b2330cf0b4a52055aed367))
* **engine:** delete the keyless changelog signing escape hatch ([#228](https://github.com/geoah/substrate/issues/228)) ([a21f57e](https://github.com/geoah/substrate/commit/a21f57ea6c6b27db8da7d8cb7425c735e4a40cee))

### Added

* **engine:** enforce `required` and materialize `default` on writes ([#225](https://github.com/geoah/substrate/issues/225)) ([722cfd7](https://github.com/geoah/substrate/commit/722cfd76027e5b4b6dda2675307e561d352ce62e))
* **api:** mark each discovery feature's surfaces; drop unserved embeddings ([#187](https://github.com/geoah/substrate/issues/187)) ([18ff5a7](https://github.com/geoah/substrate/commit/18ff5a776a63857495853d3f9ef6bf2cdaf87c3c))
* **vocabulary:** actors carry the full authority, retiring connector: ([#226](https://github.com/geoah/substrate/issues/226)) ([1ed3470](https://github.com/geoah/substrate/commit/1ed3470c1985ca5600b2330cf0b4a52055aed367))
* **engine:** delete the keyless changelog signing escape hatch ([#228](https://github.com/geoah/substrate/issues/228)) ([a21f57e](https://github.com/geoah/substrate/commit/a21f57ea6c6b27db8da7d8cb7425c735e4a40cee))

## [0.10.0](https://github.com/geoah/substrate/compare/v0.9.2...v0.10.0) (2026-08-17)

### Added

* **vocabulary:** reserve `unique`, `deprecated` and edge properties ([#184](https://github.com/geoah/substrate/issues/184)) ([5edd97f](https://github.com/geoah/substrate/commit/5edd97f838c4e4400e756043859bd78f0080c197))

## [0.9.2](https://github.com/geoah/substrate/compare/v0.9.1...v0.9.2) (2026-08-17)

### Fixed

* **engine:** detached tasks are counted, canceled and recovered ([#223](https://github.com/geoah/substrate/issues/223)) ([d092b77](https://github.com/geoah/substrate/commit/d092b7763f80eb0ba33e21750b8ee907de0f85e7))

## [0.9.1](https://github.com/geoah/substrate/compare/v0.9.0...v0.9.1) (2026-08-16)

### Fixed

* **engine:** a database an unmerged build migrated boots and catches up ([#195](https://github.com/geoah/substrate/issues/195)) ([0931fe2](https://github.com/geoah/substrate/commit/0931fe24c29654e9dae53c91d92106b7d9259948))

## [0.9.0](https://github.com/geoah/substrate/compare/v0.8.0...v0.9.0) (2026-08-16)

### ⚠ BREAKING CHANGES

* **vocabulary:** declare policy selector ops as put/patch/delete ([#189](https://github.com/geoah/substrate/issues/189)) ([cad0458](https://github.com/geoah/substrate/commit/cad0458fad5ffbe944b8ca119777782821ce7131))
* **vocabulary:** task and transcript declare `name` and derive their title ([#185](https://github.com/geoah/substrate/issues/185)) ([8a25d5e](https://github.com/geoah/substrate/commit/8a25d5efdb4092a1f0a1ed72820af068eb6472a0))

### Added

* **engine:** stamp the verified token id on every changelog entry ([#186](https://github.com/geoah/substrate/issues/186)) ([1b83ebf](https://github.com/geoah/substrate/commit/1b83ebf46a8db2b023d1147db7e915d39664d9f4))
* **vocabulary:** declare policy selector ops as put/patch/delete ([#189](https://github.com/geoah/substrate/issues/189)) ([cad0458](https://github.com/geoah/substrate/commit/cad0458fad5ffbe944b8ca119777782821ce7131))
* **vocabulary:** task and transcript declare `name` and derive their title ([#185](https://github.com/geoah/substrate/issues/185)) ([8a25d5e](https://github.com/geoah/substrate/commit/8a25d5efdb4092a1f0a1ed72820af068eb6472a0))

### Fixed

* **google:** the gmail sync keeps an html body's links ([#190](https://github.com/geoah/substrate/issues/190)) ([8ac7b1a](https://github.com/geoah/substrate/commit/8ac7b1aaf456378794c8378d45871b9f944ddad0))

## [0.8.0](https://github.com/geoah/substrate/compare/v0.7.0...v0.8.0) (2026-08-15)

### ⚠ BREAKING CHANGES

* **vocabulary:** remove the unproven media and memory bundles ([#180](https://github.com/geoah/substrate/issues/180)) ([c5f40cf](https://github.com/geoah/substrate/commit/c5f40cfb542f267a4ac678bf0da5ebabfac2faad))

### Added

* **vocabulary:** remove the unproven media and memory bundles ([#180](https://github.com/geoah/substrate/issues/180)) ([c5f40cf](https://github.com/geoah/substrate/commit/c5f40cfb542f267a4ac678bf0da5ebabfac2faad))

### Fixed

* prepare bodies on module-only edits, render declarations apply-able ([#61](https://github.com/geoah/substrate/issues/61)) ([baef48c](https://github.com/geoah/substrate/commit/baef48cf3fcc78cd826e6d95f0fb1b92d8691555))

## [0.7.0](https://github.com/geoah/substrate/compare/v0.6.0...v0.7.0) (2026-08-15)

### Added

* **auth:** registration returns the repository's signing seed, once ([#178](https://github.com/geoah/substrate/issues/178)) ([60287a0](https://github.com/geoah/substrate/commit/60287a01d93f7b4e32acf424906c260121384ffa))

## [0.6.0](https://github.com/geoah/substrate/compare/v0.5.0...v0.6.0) (2026-08-15)

### ⚠ BREAKING CHANGES

* **engine:** every changelog entry is hash-chained and signed ([#89](https://github.com/geoah/substrate/issues/89)) ([25dc53c](https://github.com/geoah/substrate/commit/25dc53c4917805680523653babd6ba985138e40f))

### Added

* **engine:** every changelog entry is hash-chained and signed ([#89](https://github.com/geoah/substrate/issues/89)) ([25dc53c](https://github.com/geoah/substrate/commit/25dc53c4917805680523653babd6ba985138e40f))

## [0.5.0](https://github.com/geoah/substrate/compare/v0.4.1...v0.5.0) (2026-08-15)

### ⚠ BREAKING CHANGES

* **vocabulary:** done replaces completed in the occurrence logs, abandoned replaces dropped on task ([#171](https://github.com/geoah/substrate/issues/171)) ([54c2127](https://github.com/geoah/substrate/commit/54c212723e63bb1640de7b7f89f48185ee496662))

### Added

* **console:** add a Properties tab to the record page and make it the default ([#174](https://github.com/geoah/substrate/issues/174)) ([f35e66c](https://github.com/geoah/substrate/commit/f35e66caa4105a9ebd96b820586ad70284fe5064))
* **vocabulary:** done replaces completed in the occurrence logs, abandoned replaces dropped on task ([#171](https://github.com/geoah/substrate/issues/171)) ([54c2127](https://github.com/geoah/substrate/commit/54c212723e63bb1640de7b7f89f48185ee496662))
* **vocabulary:** stamp targets are declared datetime properties, and managed refuses a client's value ([#176](https://github.com/geoah/substrate/issues/176)) ([580b292](https://github.com/geoah/substrate/commit/580b29218ce92f03b0bea93ef0d810fe3c034fb8))
* **vocabulary:** run, llmthread and llmmessage declare their closed sets as enums ([#173](https://github.com/geoah/substrate/issues/173)) ([58d4a29](https://github.com/geoah/substrate/commit/58d4a295277f7926c1f818741cd5acb2cd0b9abc))

### Fixed

* **engine:** the rebuild replay decodes numbers exactly, never through float64 ([#172](https://github.com/geoah/substrate/issues/172)) ([5788b65](https://github.com/geoah/substrate/commit/5788b65025e30c71193d86710bbcf00c629eedb3))

## [0.4.1](https://github.com/geoah/substrate/compare/v0.4.0...v0.4.1) (2026-08-15)

### Fixed

* **engine:** order by a declared datetime property casts to timestamptz, not text ([#169](https://github.com/geoah/substrate/issues/169)) ([c007620](https://github.com/geoah/substrate/commit/c00762004d49eff5f5f23fcdc8d1f271c390ee58))

## [0.4.0](https://github.com/geoah/substrate/compare/v0.3.4...v0.4.0) (2026-08-15)

### ⚠ BREAKING CHANGES

* **vocabulary:** decimal money, safe-integer int, ISO 8601 durations ([#161](https://github.com/geoah/substrate/issues/161)) ([1e56d76](https://github.com/geoah/substrate/commit/1e56d7612a0b780d06b3117d86d43dc0e3d05748))

### Added

* **vocabulary:** decimal money, safe-integer int, ISO 8601 durations ([#161](https://github.com/geoah/substrate/issues/161)) ([1e56d76](https://github.com/geoah/substrate/commit/1e56d7612a0b780d06b3117d86d43dc0e3d05748))

## [0.3.4](https://github.com/geoah/substrate/compare/v0.3.3...v0.3.4) (2026-08-15)

### Fixed

* **gql:** the filter grammar is discoverable, and a bad key says so ([#167](https://github.com/geoah/substrate/issues/167)) ([9d23775](https://github.com/geoah/substrate/commit/9d23775a38181d87ebecdda31aa6333f44030b8d))

## [0.3.3](https://github.com/geoah/substrate/compare/v0.3.2...v0.3.3) (2026-08-15)

### Fixed

* **api:** answer bare /api with the JSON 404, not the console ([#140](https://github.com/geoah/substrate/issues/140)) ([8601d9b](https://github.com/geoah/substrate/commit/8601d9b9d8387f9f92b45daab250932bcbbb8aa7))
* **compose:** pass the OAuth callback URL and state key through ([#141](https://github.com/geoah/substrate/issues/141)) ([4b28378](https://github.com/geoah/substrate/commit/4b2837897480aa904b74be535940b4086cef1685))

## [0.3.2](https://github.com/geoah/substrate/compare/v0.3.1...v0.3.2) (2026-08-15)

### Fixed

* **compose:** an empty invite code closes registration ([#96](https://github.com/geoah/substrate/issues/96)) ([9884c0a](https://github.com/geoah/substrate/commit/9884c0a57c27d70f1dfb9acef3befd30986a54f4))

## [0.3.1](https://github.com/geoah/substrate/compare/v0.3.0...v0.3.1) (2026-08-15)

### Fixed

* **compose:** containers restart unless stopped, and the port binds to localhost by default ([#95](https://github.com/geoah/substrate/issues/95)) ([0ff9d72](https://github.com/geoah/substrate/commit/0ff9d722345f3ce08f92e54cf5cd4667cce58d5d))

## [0.3.0](https://github.com/geoah/substrate/compare/v0.2.0...v0.3.0) (2026-08-15)

### Added

* **console:** the proposal card shows the change itself, inline in the thread ([#93](https://github.com/geoah/substrate/issues/93)) ([f427297](https://github.com/geoah/substrate/commit/f427297d9f4a5091c8ec32a8ef9cdbee85a15a04))

## [0.2.0](https://github.com/geoah/substrate/compare/v0.1.0...v0.2.0) (2026-08-15)

### ⚠ BREAKING CHANGES

* **api:** remove the failure lockout, which was a denial-of-service lever ([1c134b4](https://github.com/geoah/substrate/commit/1c134b4b1cee69d71de5d3b4762b8d7df469d76a))
* **tokens:** drop the last-used stamp, which wrote a changelog entry a minute ([261802c](https://github.com/geoah/substrate/commit/261802c73a3f4b21b56c9cbc5b719255f10f04dd))
* **llm:** providers as records; agents name a provider and a model ([#7](https://github.com/geoah/substrate/issues/7)) ([b9e1023](https://github.com/geoah/substrate/commit/b9e1023ea003d9e6f4bd38675ec6e5d0622b1667))
* **runner:** confine function bodies with landlock, seccomp and one process each ([#8](https://github.com/geoah/substrate/issues/8)) ([fbf434c](https://github.com/geoah/substrate/commit/fbf434c5cad36e3b97449d612a820d058f2ede31))
* **engine:** move every secret-typed value into the sealed store ([#12](https://github.com/geoah/substrate/issues/12)) ([fd1b8d9](https://github.com/geoah/substrate/commit/fd1b8d914ef13c66f5e7879973f955b2cc20640b))
* **engine:** per-repository encryption keys and an age recovery key ([#13](https://github.com/geoah/substrate/issues/13)) ([8a6ada2](https://github.com/geoah/substrate/commit/8a6ada2cf6308b2ebb9f532b2849987aea5744ab))
* **vocabulary:** references are first-class, and the graph reads them ([1d37952](https://github.com/geoah/substrate/commit/1d37952e666d524f8d17162a6fa3eb87db619640))
* **core:** nothing seeds an llmprovider; an example ships two instead ([b8d4537](https://github.com/geoah/substrate/commit/b8d453768deeebff1446fa17d607bed367ad22f6))
* a record editor that knows the schema, and an llmprovider that is declared ([#18](https://github.com/geoah/substrate/issues/18)) ([b37caa5](https://github.com/geoah/substrate/commit/b37caa5683bbb92ac4f9f8671ecf92a620ae1b74))
* **core:** bundle inputs replace the config singleton ([#19](https://github.com/geoah/substrate/issues/19)) ([2958676](https://github.com/geoah/substrate/commit/295867697839de6867c03983c759b4cb65fcac24))
* **vocabulary:** eight mneme bundles join the registry and the shipped kinds unify with them ([#20](https://github.com/geoah/substrate/issues/20)) ([a67da82](https://github.com/geoah/substrate/commit/a67da826a39bb58aaf863a1905eb29747fce9fce))
* **core:** agents speak graphql, and the llm example becomes the substrate assistant ([#21](https://github.com/geoah/substrate/issues/21)) ([6fcde52](https://github.com/geoah/substrate/commit/6fcde524fc82c58bac3b2073ee5f434a728353f8))
* **bundles:** any authority owns a bundle, and its shipped records are previewed ([#25](https://github.com/geoah/substrate/issues/25)) ([d4510a5](https://github.com/geoah/substrate/commit/d4510a576f5b7364431d0e45c0e90b228003a593))
* **ci:** main builds push :latest instead of :edge ([#29](https://github.com/geoah/substrate/issues/29)) ([b740bcb](https://github.com/geoah/substrate/commit/b740bcbc828bad1c4142ef0a53b6c3a448127357))
* **api:** discovery moves to /.well-known/substrate/server.json, and names the register door's own shape ([#27](https://github.com/geoah/substrate/issues/27)) ([6c90460](https://github.com/geoah/substrate/commit/6c9046041193d37b5d4ce2f708bd98cc42a5bbd6))
* **core:** declarations are typed properties, generated from the kinds tree ([#43](https://github.com/geoah/substrate/issues/43)) ([eeb2620](https://github.com/geoah/substrate/commit/eeb2620d8941d6e1e9f15e8842dae8b0794209e6))
* **engine:** proposals validate at every door, and a judge agent can accept ([#44](https://github.com/geoah/substrate/issues/44)) ([05a6a5a](https://github.com/geoah/substrate/commit/05a6a5aea4d0fde1934856c29b2940ea46091163))
* **core:** declaration versions are incremental integers the API maintains ([#63](https://github.com/geoah/substrate/issues/63)) ([33d86d6](https://github.com/geoah/substrate/commit/33d86d6d573aa8cfe1c3ddbdb4c68d3ccaefdb49))

### Added

* **tokens:** drop the last-used stamp, which wrote a changelog entry a minute ([261802c](https://github.com/geoah/substrate/commit/261802c73a3f4b21b56c9cbc5b719255f10f04dd))
* **blobs:** a name beside the optional mime type ([e5f0826](https://github.com/geoah/substrate/commit/e5f08268aaff11e15900eb131261882fa225d46d))
* **kinds:** a description on every kind, read above its collection ([#9](https://github.com/geoah/substrate/issues/9)) ([e85f394](https://github.com/geoah/substrate/commit/e85f394ae65ae9c979184aaa6f0440d9dfbf3e31))
* **llm:** providers as records; agents name a provider and a model ([#7](https://github.com/geoah/substrate/issues/7)) ([b9e1023](https://github.com/geoah/substrate/commit/b9e1023ea003d9e6f4bd38675ec6e5d0622b1667))
* **runner:** confine function bodies with landlock, seccomp and one process each ([#8](https://github.com/geoah/substrate/issues/8)) ([fbf434c](https://github.com/geoah/substrate/commit/fbf434c5cad36e3b97449d612a820d058f2ede31))
* **engine:** move every secret-typed value into the sealed store ([#12](https://github.com/geoah/substrate/issues/12)) ([fd1b8d9](https://github.com/geoah/substrate/commit/fd1b8d914ef13c66f5e7879973f955b2cc20640b))
* **engine:** per-repository encryption keys and an age recovery key ([#13](https://github.com/geoah/substrate/issues/13)) ([8a6ada2](https://github.com/geoah/substrate/commit/8a6ada2cf6308b2ebb9f532b2849987aea5744ab))
* **vocabulary:** references are first-class, and the graph reads them ([1d37952](https://github.com/geoah/substrate/commit/1d37952e666d524f8d17162a6fa3eb87db619640))
* **console:** agent threads, tool-call detail, and the record graph ([1224f4f](https://github.com/geoah/substrate/commit/1224f4f4af80f94875ad40d2bb639a2d6f50ce58))
* **core:** nothing seeds an llmprovider; an example ships two instead ([b8d4537](https://github.com/geoah/substrate/commit/b8d453768deeebff1446fa17d607bed367ad22f6))
* a record editor that knows the schema, and an llmprovider that is declared ([#18](https://github.com/geoah/substrate/issues/18)) ([b37caa5](https://github.com/geoah/substrate/commit/b37caa5683bbb92ac4f9f8671ecf92a620ae1b74))
* **core:** bundle inputs replace the config singleton ([#19](https://github.com/geoah/substrate/issues/19)) ([2958676](https://github.com/geoah/substrate/commit/295867697839de6867c03983c759b4cb65fcac24))
* **vocabulary:** eight mneme bundles join the registry and the shipped kinds unify with them ([#20](https://github.com/geoah/substrate/issues/20)) ([a67da82](https://github.com/geoah/substrate/commit/a67da826a39bb58aaf863a1905eb29747fce9fce))
* **core:** agents speak graphql, and the llm example becomes the substrate assistant ([#21](https://github.com/geoah/substrate/issues/21)) ([6fcde52](https://github.com/geoah/substrate/commit/6fcde524fc82c58bac3b2073ee5f434a728353f8))
* **auth:** the second factor can be switched off, and local dev switches it off ([#24](https://github.com/geoah/substrate/issues/24)) ([a8c25fe](https://github.com/geoah/substrate/commit/a8c25fe4cab98307bf72b9deef7efbdb5f0df71d))
* **core:** bundle upgrades are previewed, offered and version-gated ([#23](https://github.com/geoah/substrate/issues/23)) ([8ce02a1](https://github.com/geoah/substrate/commit/8ce02a11d29d8b163e3225e20fc1436c825533e9))
* **bundles:** any authority owns a bundle, and its shipped records are previewed ([#25](https://github.com/geoah/substrate/issues/25)) ([d4510a5](https://github.com/geoah/substrate/commit/d4510a576f5b7364431d0e45c0e90b228003a593))
* **ci:** main builds push :latest instead of :edge ([#29](https://github.com/geoah/substrate/issues/29)) ([b740bcb](https://github.com/geoah/substrate/commit/b740bcbc828bad1c4142ef0a53b6c3a448127357))
* **api:** discovery moves to /.well-known/substrate/server.json, and names the register door's own shape ([#27](https://github.com/geoah/substrate/issues/27)) ([6c90460](https://github.com/geoah/substrate/commit/6c9046041193d37b5d4ce2f708bd98cc42a5bbd6))
* **core:** declarations are typed properties, generated from the kinds tree ([#43](https://github.com/geoah/substrate/issues/43)) ([eeb2620](https://github.com/geoah/substrate/commit/eeb2620d8941d6e1e9f15e8842dae8b0794209e6))
* **engine:** proposals validate at every door, and a judge agent can accept ([#44](https://github.com/geoah/substrate/issues/44)) ([05a6a5a](https://github.com/geoah/substrate/commit/05a6a5aea4d0fde1934856c29b2940ea46091163))
* **console:** the review inbox, and the SPA serves assets honestly ([#46](https://github.com/geoah/substrate/issues/46)) ([5b2ac4e](https://github.com/geoah/substrate/commit/5b2ac4e8fe4c4ba3f364dd47a9d62cf8f6fbba5f))
* **console:** the record form speaks the schema, and every pointer gets a picker ([#52](https://github.com/geoah/substrate/issues/52)) ([060465c](https://github.com/geoah/substrate/commit/060465c9d5d5526a4af64138302caea7f427b54e))
* **compose:** Configure LLM embedding gateway environment variables ([#56](https://github.com/geoah/substrate/issues/56)) ([b4d987c](https://github.com/geoah/substrate/commit/b4d987c64122a64aa0eb5b71a031e01306865386))
* **core:** declaration versions are incremental integers the API maintains ([#63](https://github.com/geoah/substrate/issues/63)) ([33d86d6](https://github.com/geoah/substrate/commit/33d86d6d573aa8cfe1c3ddbdb4c68d3ccaefdb49))
* **engine:** tool rows carry their changes, and a decision resumes the proposing thread ([#72](https://github.com/geoah/substrate/issues/72)) ([8eb2ad6](https://github.com/geoah/substrate/commit/8eb2ad61f8cbb58c99b736a8a4aeee4c80a3d9dd))
* **engine:** a transition declares what it notifies, and resolutions always arrive ([#78](https://github.com/geoah/substrate/issues/78)) ([5a54224](https://github.com/geoah/substrate/commit/5a54224f90e4ac866befb4350b83043245c83fe8))
* **engine:** an agent asks, the owner answers, and the thread hears it ([#80](https://github.com/geoah/substrate/issues/80)) ([821d0e8](https://github.com/geoah/substrate/commit/821d0e8649977ad34839f377a35bcc4cb1f3cfc6))
* **engine:** the policy door — owner rules gate, refuse or allow agent writes ([#81](https://github.com/geoah/substrate/issues/81)) ([303e7e1](https://github.com/geoah/substrate/commit/303e7e10f1cba9a41c1367d1a3616ef553986166))
* **engine:** the judge — a policy's agent decides gated requests within the owner's thresholds ([#83](https://github.com/geoah/substrate/issues/83)) ([c7f9049](https://github.com/geoah/substrate/commit/c7f9049031e22bb1a44047488f204e030733b02f))
* **release:** main releases itself on merge, and every build says which version it is ([#91](https://github.com/geoah/substrate/issues/91)) ([bf15fcc](https://github.com/geoah/substrate/commit/bf15fcc155325d86d8452ba515178494b52b51b5))

### Fixed

* **ci:** the first release could not push its image ([4f8662f](https://github.com/geoah/substrate/commit/4f8662f3c803015c31c8de8f6696fc7fed2e224b))
* **api:** remove the failure lockout, which was a denial-of-service lever ([1c134b4](https://github.com/geoah/substrate/commit/1c134b4b1cee69d71de5d3b4762b8d7df469d76a))
* **engine:** the vocabulary upgrade refuses a narrowing instead of projecting it ([656121e](https://github.com/geoah/substrate/commit/656121e6c22fa22c4b5371501eb8213e1cc5a81c))
* **engine:** the runner keys processes by repository id, and the suite runs in parallel ([#26](https://github.com/geoah/substrate/issues/26)) ([7113bfa](https://github.com/geoah/substrate/commit/7113bfaf1dbca8acceacf1a68d3a473cd2845d8a))
* **security:** the known advisories are cleared, and the scans that found them run in CI ([#28](https://github.com/geoah/substrate/issues/28)) ([38fa144](https://github.com/geoah/substrate/commit/38fa1449f05b0ec70b8d4197f2b3ffee9349db90))
* **api:** a GraphQL request's extensions key is spelled extensions again ([#39](https://github.com/geoah/substrate/issues/39)) ([9407946](https://github.com/geoah/substrate/commit/940794665821ee3437355bc32c073554e729b983))
* **llm:** the go dependencies move, and the openai wire caps completions with max_completion_tokens ([#31](https://github.com/geoah/substrate/issues/31)) ([a368e44](https://github.com/geoah/substrate/commit/a368e44de527f4e9b610b8bfe2833aa02c79dee8))
* **console:** react-resizable-panels moves to v4, and the resizable shell rides the Group/Separator API ([#35](https://github.com/geoah/substrate/issues/35)) ([8eb84bb](https://github.com/geoah/substrate/commit/8eb84bb07c02fd3751f75e398bc53d1b23660260))

## [0.1.0](https://github.com/geoah/substrate/commits/v0.1.0) (2026-08-12)

### Added

* the substrate ([e7301a7](https://github.com/geoah/substrate/commit/e7301a7244cfc3ea9d01af6ae64912b973770e5c))
