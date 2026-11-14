# Changelog

## [0.124.0](https://github.com/geoah/substrate/compare/v0.123.1...v0.124.0) (2026-11-14)


### ⚠ BREAKING CHANGES

* **api:** a short repository name is completed under the server's configured public host, never the address a request reached; a new name under localhost or an IP address is refused outside development; a repository may be renamed, its old name resolving for 90 days; and the metrics label a repository by its id.
* **engine:** a repository's own kinds, packages, declarations and scopes are stored and answered as `~/…` instead of `<name>/…`.
* **auth:** what a hand may write is the one check's. 1. A function or an agent whose `permissions.writes` reaches a control    kind only through a glob (`*`, `<authority>/*`,    `<authority>/<package>/*`) is refused that write with `403    not_permitted`: list each control kind it writes by its reference    (`substrate.reamde.dev/core/trigger`,    `substrate.reamde.dev/llm/provider`, a package's account or input    kind). Every shipped package names its own. A policy    (`substrate.reamde.dev/core/recordpatchpolicy`) is no such kind:    installed code never writes one, whatever it names, and an admin    writes it. 2. An agent an app's token starts reads and writes only what the app's    scopes also reach; a write beyond them is refused `403    not_permitted`, and a read beyond them answers the agent the same.    Approve the app again with the scopes its agents' work needs. The    Workouts coach writes what the Workouts app writes and is unaffected. 3. An agent's `propose` of a `patch` or a `delete` of a kind its    `permissions.reads` does not list answers `<kind> is not in the reads    allowlist`: list the kind under its reads. The readinglist sample's    curator does from sample version 15; take the upgrade the catalog    offers. 4. An ask, an approval or a background job left waiting from before this    release is refused when it is answered, approved or settled, and its    conversation is told to ask again: nothing recorded whose rules held    it. Ask the agent again. 5. Upgrading needs no other step: every person of an existing repository    becomes an admin at the repository's first open under this release,    and every token keeps the access it had. A rollback restores the    backup taken before the upgrade: a server from before this release    does not carry the repository migration this one records.
* **auth:** a sign-in names a person. Upgrade the server before its clients: a client from before this release cannot sign in to a server after it, whose door refuses a body without `identity`. Of this release's clients, the console and the Swift kit's password door (Chat, Sync) send the older body to a server whose `auth` is below revision 3, and sign in to both; `substratectl` speaks the new door alone, so an older server takes an older `substratectl`. An app's sign-in through the authorization flow is unchanged on either side. 1. Stop the server, take the backup (`docs/operations.md`, Backups), and    start this release. The boot gives every repository its person, named    after the repository, with the same password and second factor; nobody    chooses anything. A rollback restores the backup taken before: a server    from before this release does not read the identity tables, and the    repositories' credential records are purged at their first open. 2. A script calling `POST /login`, `/register`, `/register/enroll`,    `/password`, `/totp/enroll`, `/totp` or `/signout/everywhere` renames the    body's `repository` to `identity` (the value stays the same: every    existing person signs in with their repository's name), and    `POST /repository/delete` sends `identity` beside `repository`. A    credential change answers `{"identity": …}` where it answered    `{"repository": …}`. 3. `substratectl`: `login --identity <name>` and `register --identity    <name>` (`--repository` alone still works and names the person after it);    `user reset <name>` runs with the server up. 4. An archive carries its repository's people from this release on. One    written before it (a manifest without `identities`) still holds the    sign-in record of a release before identities, and the boot makes the    person from it, with the same password and second factor, as it does for    a repository upgraded in place. A repository comes back without a person    only where its copy does not carry them: an export somebody else took    (an export carries the person who took it), a copy whose sign-ins do not    open under this host's key (refused unless the repository is listed, and    then its people are left out), or a person whose sign-in name another    person here holds. The tokens it carried for them are refused until    `substratectl user reset <repository>` (its id where its name is a    person's; `--identity <name>` to pick one of several) gives each back    under the identity their actor names, with a new password and second    factor. A build of `main` from before this one refuses a manifest that    carries `identities`. 5. A function body or app reading `core/credential` finds nothing; `GET    /identity` says whether the person has a second factor.
* **engine:** a record's `propertyMeta` is gone; a read serves `marks`. To upgrade: 1. Stop the server and copy the database (pg_dump) and the whole data    root, its archive included, before the new server starts: SQL migration    0026 replaces the `property_managers` table with `property_marks` and    re-keys `property_offers` by source record, and the history is converted    (each manager effect becomes a `mark` or an `unmark`), so going back is    restoring those copies under the older release. 2. Start the new server. The boot converts every history line and    rebuilds the marks from it: a stored `owner` or `bundle` tier pins, a    `machine` tier does not, and no converted mark has a `derivedFrom`.    A manager row on a value an earlier conversion took out (a package's    `actors`) becomes no mark, and one on a trigger run's `principal` a    mark on `token`, where that value moved. 3. Clients: read `marks.<property>` (`pinned`, `setBy.ref`,    `onBehalfOf.ref`, `derivedFrom[].ref`, `setAt`, and `alternatives[]`    with `derivedFrom.ref`, `value` and `updatedAt`) in place of    `propertyMeta.<property>` (`manager`, `tier`, `alternatives[].actor`).    The records feature is at revision 5. 4. To let sync replace a value, POST    /api/v1/<kind>/<id>/release with `{"properties": [...]}` (or    `substratectl release`) instead of writing null, which a required    property refuses; POST …/pin (`substratectl pin`) keeps the current    value. 5. A function body that read `record.propertyMeta` reads `record.marks`. 6. `substratectl get -o yaml` prints `status.properties` as marks; a    script that read `manager` or `tier` there reads `pinned` and `setBy`.
* **engine:** a repository's history converts to a new changelog dialect at the first boot of this release, and `body` is held to its declaration.
* **vocabulary:** the vocabulary dialect is 6, so a server from before this release refuses a repository this one has opened, and from this release the history of trigger runs and alerts is kept seven days.
* **vocabulary:** a property's `writer:` names types of actor, and the words `owner`, `connector` and `oauth` are refused; the vocabulary dialect is 5, so a server from before this release refuses a repository this one has opened.
* **engine:** a write names its hand by an actor record's id, never by a spelling, and no request names its actor. To upgrade: 1. Stop the server and copy the database (pg_dump) and the whole data    root, its archive included, before the new server starts: the release    before this one refuses a repository this one opened (the vocabulary    dialect is 4 and the history is converted to the next changelog    dialect), so going back is restoring those copies under the older    release. 2. Start the new server. At the boot check it converts every history line    (the spelling becomes the id of an actor record, `principal` becomes    `token`, an app's line gains `onBehalfOf`), and at the first open    repository migration `link_actors` tombstones core's four actor    declarations, clears a package's `actors`, and writes an actor record    for every hand the history names. 3. Clients: a change's `actor` is an actor record id, beside    `onBehalfOf` and `token` (the owner's reads alone); read    GET /api/v1/substrate.reamde.dev/core/actor/<id> for its `type` and    `name`. `actors=` and `excludeActors=` take actor record ids. A history    run carries `token` and `tokenName` in place of `principalName`.    Discovery's `grammar.actors` is `grammar.actorTypes`; an approval    page's `app.actor` is `app.appearsAs`. A property's `manager` and each    alternative's `actor` are actor record ids. A package's `declaredBy`, a    blob's `createdBy` and a call run's `caller` are references to actor    records; a call run's token is `token`. 4. Stop sending X-Substrate-Actor (it is ignored) and drop substratectl's    `--actor` flag from scripts: the command refuses it. 5. A package that ships a document of kind    substrate.reamde.dev/core/actor is refused: drop the document. 6. A function body that read `change.actor` as a spelling reads    `change.actorType` and `change.actorPackage`; a trigger guard that    compared `change.actor` to a spelling compares those instead (the    conversion rewrites a stored guard of the two shapes the shipped    providers used).
* **engine:** a damaged repository no longer stops the server while another repository is whole.
* **engine:** there is no recovery key. Registration answers no key, `recoveryPublicKey` is refused, and a repository moves to another host only with its host key or without its secrets.
* **engine:** the repository directory no longer holds the history, the snapshot command is gone and the export has a new layout. To upgrade: 1. Stop the server. Copy the database (pg_dump) and the whole data root:    that copy is the only way back. 2. Remove SUBSTRATE_DIGEST_BYTES_PER_SECOND and    SUBSTRATE_CHANGELOG_SEGMENT_BYTES from the environment; neither is read.    The archive's pace is SUBSTRATE_ARCHIVE_BYTES_PER_SECOND (default    8388608, 0 for no cap). 3. Choose where the archive goes. With SUBSTRATE_ARCHIVE unset it is    <data root>/archive, on the data root's own disk, which does not survive    losing that disk. To use a bucket, set SUBSTRATE_ARCHIVE=s3://<bucket>/<prefix>    with SUBSTRATE_ARCHIVE_S3_ENDPOINT and _REGION, the credentials in    _ACCESS_KEY_ID and _SECRET_ACCESS_KEY (or AWS_ACCESS_KEY_ID and    AWS_SECRET_ACCESS_KEY), and _PATH_STYLE=true for a store that needs it.    SUBSTRATE_ARCHIVE=off writes none. 4. Start the new binary. Its first boot reads each    <data root>/repositories/<id>/ the old server wrote, holds every line it    holds to the table (a line that differs refuses the boot, naming it),    takes in the lines the table lacks, renames repository.json to    repository.json.adopted, converts the table, and then removes    changelog/, sealed/ and the set-aside manifest, keeping blobs/ and    spool/; a boot cut short anywhere is finished by the next start. Each    repository's first archive pass then copies its whole history at the    pace above. Let it finish before a planned restart: a process stopped in    the middle of a first copy copies the history again from the start.    `substratectl repository inspect <name>` prints how far behind the head    each archive is. 5. Back up the archive and the database from now on. `substratectl    repository snapshot` is gone. A clean stop runs a last archive pass,    bounded at 30 seconds, and logs at error when it does not finish; before    copying the archive, check that `substratectl repository inspect <name>`    says "0 behind the head, 0 blob(s) missing" and no STOPPED line for each    repository, and that `substratectl repository verify <name>`, which    reads the archive back, passes and prints no "NOT VERIFIED" line. To    restore, put the archive in place, start with an empty database and    boot. After restoring the database from a backup beside an archive, with    the server stopped, run `substratectl --dsn … archive resolve <name>    --keep-database` for each repository, which starts a clean archive. A    snapshot or an export taken before this release still restores the old    way: put its directory under <data root>/repositories/ and boot. 6. Know what the archive does not hold against (docs/operations.md, "The    archive's contract"): never point two databases at one archive, which is    not safe; a store that lists stale or partial results, or loses an object    after answering, is not supported, and `repository verify` finds a loss    only when it runs; a write confirmed after the last published manifest is    lost with Postgres's storage; restore the database only with the server    stopped; a directory store on a network mount that hangs blocks in the    kernel; and bytes a late put leaves after a deletion may stay in the    store, unread and counted by `verify`. 7. A script that reads changelog/ or sealed/ under the data root, or that    unpacks an export expecting snapshot.json, reads the archive's layout    instead (docs/stored-format.md): repositories/<id>/manifests/<archive>/    <epoch>-<hash>.json (format 3; the newest archive's highest epoch is the    one to read, and an archive a `deleted` mark of a newer id covers is not),    and the objects the manifest names under repositories/<id>/<archive>/:    changelog/<dialect>/ pieces, sealed/, spool/ and blobs/<digest>.<pass>.    An export is an archive of its own; extract it into the store. Rollback: stop the server and restore the database and data root copied in step 1, with the old binary. An older binary refuses this database (migration 0022) and the archive's manifest format 3.
* **engine:** migration 0021 changes how every record kind and id, and every repository id, sorts. It runs only with no other server running against the database, changes the order of some listings, refuses every list and window cursor taken before it, starts the server's unfinished passes and drains again, and closes the rollback.
* **vocabulary:** a reference may no longer declare a link property named `repository`.
* **api:** `POST /api/v1/catalog/{id}/install` and `POST /api/v1/catalog/{id}/import` are gone, and a catalog entry's and an approval package row's `tier` is now `keptBy`.
* **catalog:** a kind a publisher published is permanent. `substratectl publish --check` refuses a release that declares `movedFrom` on a kind, or that drops a kind any version the authority document lists declared, with `--allow-narrowing` or without it. A server refuses to install a release a publisher's document serves that declares `movedFrom`, whether or not the repository holds the kind it names, or that drops a kind the version the repository holds declares.
* **vocabulary:** the vocabulary dialect is 3, and every repository is stamped with it at its first open under this release.
* **engine:** a schedule trigger that states no `missed` now fires only the newest occurrence it missed (the server off, the trigger disabled, a repository restored to an older fire state), where it fired each one. The new server's first dispatcher pass, seconds after it starts, reads every existing schedule trigger this way, and the release before this one refuses the `missed` key, so a trigger whose every occurrence must run is held disabled across the upgrade. A trigger's missed occurrences are counted from its fire state, which a disabled trigger keeps and an edit never moves.
* **engine:** requests are paced per person, and one refusal is worded per person.
* **api:** a list page and two keyed writes answer differently.
* **engine:** two writes that were admitted are refused.
* **engine:** a repository's storage is keyed by a minted id (`rnd-…`), not by its name. The first boot of this release converts every repository, and an older server cannot open one afterwards.
* **api,engine:** a record's `PUT` and `PATCH` (`/api/v1/{authority}/{package}/{kind}/{id}`) refuse any query parameter with `400 bad_request` naming it; installed code (a function, an agent, a provider's sync) can no longer write a change-request policy by any path; and an app's check plans only the packages its token's scopes name.
* **api:** the discovery file's `stability` value `alpha` is now `experimental`, and the `apps` feature reports `beta`.

### Added

* **api:** add a catalog package through one route, and say who keeps it in keptBy ([04e2f8e](https://github.com/geoah/substrate/commit/04e2f8e31346e742614718ec9be5d04f9c304f74))
* **api:** let a repository be renamed, its old name resolving for 90 days, and complete a short name under the public host ([691c441](https://github.com/geoah/substrate/commit/691c44101f170d9a4fbec5c2cf80f595d5f71276))
* **api:** put stability labels on packages and features, each feature with a revision ([e63a289](https://github.com/geoah/substrate/commit/e63a2898accf320cbc292f63a86703e09f2bc340))
* **api:** retry keys on put, patch and delete, a reason for a write in doubt, one list-again answer ([7054a39](https://github.com/geoah/substrate/commit/7054a396484ed674b00cf861dfef95ac0178c016))
* **auth:** a sign-in names a person, who picks the repository ([0639e42](https://github.com/geoah/substrate/commit/0639e424cecc0d9e866b8a8eb175898aa5b25060))
* **auth:** roles, one check for every hand that writes, and the list of control kinds ([346359d](https://github.com/geoah/substrate/commit/346359ddcd7706db9e740c6d5e52d51a9ad286a1))
* **auth:** sign out everywhere, and every stream ends with its token ([a4ff767](https://github.com/geoah/substrate/commit/a4ff767796dd1f63a4bfe805cedf1ca61d06830e))
* **catalog:** a publisher may not rename a kind, and code from outside publishers is experimental ([842b96a](https://github.com/geoah/substrate/commit/842b96aa5634b20c858f7b0785ce18a700f7d95c))
* **engine:** a property's mark says whether sync may replace it, apart from where its value came from ([c5b4e80](https://github.com/geoah/substrate/commit/c5b4e80ae010e1d9bfbaa1be26657c7cd0b525a1))
* **engine:** a schedule trigger runs only the latest of the runs it missed ([77d23ae](https://github.com/geoah/substrate/commit/77d23ae53a7e3b753eabb17ab32b7525841c28da))
* **engine:** confirm a write in Postgres alone and copy each repository to an archive after the commit ([6062b8d](https://github.com/geoah/substrate/commit/6062b8db9c14b8cbddc7d60385534d70432e97df))
* **engine:** document the stored format, keep v0.123.1's directory and add the conversion chain ([aa791fb](https://github.com/geoah/substrate/commit/aa791fba20f76844e21ae2d87801b6741ab588db))
* **engine:** every hand that writes is an actor record, and a write links three of them ([ba29ba9](https://github.com/geoah/substrate/commit/ba29ba97c02bca6f6cd197fb8170f41c95bf64e3))
* **engine:** keep a repository's own kinds in the local namespace ~, with its name another spelling of it ([7ddae89](https://github.com/geoah/substrate/commit/7ddae89a2567bb93ae9386644cf09d83db88626a))
* **engine:** key every repository by a minted id, and give existing ones theirs at boot ([7da04d5](https://github.com/geoah/substrate/commit/7da04d517fb2978b6153c1a26a0fb54e0f6ac95f))
* **engine:** make body an ordinary property, stored and checked like any other ([6cfc5c9](https://github.com/geoah/substrate/commit/6cfc5c92e2e0b42566961ae23f233eb009ae12da))
* **engine:** one reader per repository for its watchers, a token cache, and limits per person ([a6a2028](https://github.com/geoah/substrate/commit/a6a2028b2d93c82084793b5648f2a88aad4ecc4f))
* **engine:** remove the recovery key, and restore a repository without its secrets when the operator lists it ([63bd04e](https://github.com/geoah/substrate/commit/63bd04e9fdbd18046ae39bd506f0488f9875b595))
* **engine:** retry a temporary delivery failure over minutes ([69ba17b](https://github.com/geoah/substrate/commit/69ba17b71135e0cb953f4eceb56f8a7f6c396baa))
* **engine:** set a damaged repository aside at start-up and serve the others ([7658b08](https://github.com/geoah/substrate/commit/7658b0884e13082370ecf6680906917149a8a9aa))
* **vocabulary:** a new declaration key raises the dialect, and a package states the one it needs ([c10b9a2](https://github.com/geoah/substrate/commit/c10b9a2a381bd7a014f1184836f13b401eb8f4e8))
* **vocabulary:** a package names the function interface its code is written against ([225222b](https://github.com/geoah/substrate/commit/225222b9a6a48179755cab02ba747af128fd215a))
* **vocabulary:** a property's writer names types of actor, and its three old words are refused ([c8d778a](https://github.com/geoah/substrate/commit/c8d778abfe2131d894584110fd5621b4383a2503))
* **vocabulary:** let a kind declare how long its history is kept, and a repository override it ([03fbaca](https://github.com/geoah/substrate/commit/03fbaca0adacb25727f6f3b6bc68b240cd2a9b2f))
* **vocabulary:** reserve `repository` as a key of a reference value ([c0d2247](https://github.com/geoah/substrate/commit/c0d2247a83c97d4ef6ccb580581252442b281bb2))


### Fixed

* **api,engine:** refuse a query parameter on a record PUT or PATCH, restore the checks on a shipped kind move, keep vectors through a rebuild, keep functions off policies, and refresh only packages an app's token names ([1c10654](https://github.com/geoah/substrate/commit/1c106546da0ed22e6cd4080936d7c31579fea0f0))
* **build:** move to Go 1.27.4 for the net/http advisory ([3b6f8da](https://github.com/geoah/substrate/commit/3b6f8da023a6d28f560f5b7bca9db0759e7838bb))
* **engine:** a conversion rebuilds the fold only where a line changed or a step says so ([cf3e7e4](https://github.com/geoah/substrate/commit/cf3e7e4184e73675653962b07aa8598dd0cc7291))
* **engine:** count a string retyped to a reference by the values not yet links, so a release's repository takes the shipped upgrade ([cf52499](https://github.com/geoah/substrate/commit/cf52499af56b080e12465a3f6a32c4af06bff7f9))
* **engine:** refuse Local and the empty time zone, and count added allowed values as a narrowing ([b41b4c9](https://github.com/geoah/substrate/commit/b41b4c92ca2545a2507b77e86e06f580645f0139))
* **engine:** sort every record kind and id, and every repository id, in byte order, whatever the database's locale ([be8d33d](https://github.com/geoah/substrate/commit/be8d33dfd813d1b1e0966bdf5b8e3821c6f5f762))
* **engine:** write at an open the actor records a repository from before actors links and holds none of ([8cfd28d](https://github.com/geoah/substrate/commit/8cfd28d071367fcd0115cad070ce3aeb40083cc2))

## [0.123.1](https://github.com/geoah/substrate/compare/v0.123.0...v0.123.1) (2026-11-09)


### Fixed

* **engine:** refuse enabling a package's webhook trigger without a key ([#1304](https://github.com/geoah/substrate/issues/1304)) ([fdb34c8](https://github.com/geoah/substrate/commit/fdb34c84920cb5f5e2b7faa6628a3c5217f8e987))

## [0.123.0](https://github.com/geoah/substrate/compare/v0.122.0...v0.123.0) (2026-11-09)


### Added

* **metrics:** usage series per repository, anonymized unless SUBSTRATE_METRICS_ANONYMIZE=false ([#1294](https://github.com/geoah/substrate/issues/1294)) ([a92776f](https://github.com/geoah/substrate/commit/a92776f9fa4d84e63bb3b48a196cdfe0dd560de7))
* **providers:** let agents mail, schedule and search Drive through Google, each send waiting for the owner's approval in chat ([#1308](https://github.com/geoah/substrate/issues/1308)) ([e9c8f24](https://github.com/geoah/substrate/commit/e9c8f24b7dbbdccdfab8d33742b231605fb169c6))
* **tasks:** Tasks over any kind with a status, with suggestions and groups per collection ([#1296](https://github.com/geoah/substrate/issues/1296)) ([799e8a1](https://github.com/geoah/substrate/commit/799e8a1f7eee82047d5d3444a04e52de2e665c92))


### Fixed

* **notion:** skip the workspace roster when the token cannot list users ([#1297](https://github.com/geoah/substrate/issues/1297)) ([4a123a1](https://github.com/geoah/substrate/commit/4a123a195e4ef6ae380eda4fd421b4b8c4f7c55e))
* word every sync and sign-in failure as a sentence with the fix ([#1301](https://github.com/geoah/substrate/issues/1301)) ([5465543](https://github.com/geoah/substrate/commit/546554315123e0a92d5b7c2a2ff5ed53e28966a4))

## [0.122.0](https://github.com/geoah/substrate/compare/v0.121.0...v0.122.0) (2026-11-09)


### Added

* **api:** give a stale revision refusal the reason stale_revision ([#1278](https://github.com/geoah/substrate/issues/1278)) ([2cdbe31](https://github.com/geoah/substrate/commit/2cdbe311d2066634a5542a17244713c6cafbde8f))
* **apps:** build the web kit for npm and publish it with each release, once its packages are opened ([#1289](https://github.com/geoah/substrate/issues/1289)) ([2649868](https://github.com/geoah/substrate/commit/26498680f1d373e94da792c18206c0bcba25b185))
* **apps:** put Tasks on the web, on a web kit every app can share ([#1275](https://github.com/geoah/substrate/issues/1275)) ([9d92f81](https://github.com/geoah/substrate/commit/9d92f815b4877489cc1ebbd2787b931ccef0962b))


### Fixed

* **agents:** teach Moss to count a kind with a list, stop a resent refused read, and list a chat while it runs ([#1285](https://github.com/geoah/substrate/issues/1285)) ([da0f27d](https://github.com/geoah/substrate/commit/da0f27da0f15e5244096ec49658160781c6ea07c))
* **apps:** a retried create in the Swift kit reads its row instead of writing over another device's edit ([6d9783b](https://github.com/geoah/substrate/commit/6d9783b24488b781a82be883da67f3d00643d09c))
* **apps:** the Swift kit drops a write refused at its revision only where the stored record holds a later one ([1290871](https://github.com/geoah/substrate/commit/1290871a3dfee1b6af26f07cb9732b523c7445c1))
* **apps:** the Swift kit's outbox never loses an edit made after a create went, nor parks one behind the device's own change ([b09527d](https://github.com/geoah/substrate/commit/b09527d88d6fd12848ee73e1dfc50c952d76f2d0))
* **console:** list a provider's collections only once it is connected ([#1283](https://github.com/geoah/substrate/issues/1283)) ([5b46a82](https://github.com/geoah/substrate/commit/5b46a82ffa5432f952ed701a4ae3920582199f88))
* **console:** open each page at the top and each collection with its own filters ([#1284](https://github.com/geoah/substrate/issues/1284)) ([156ea46](https://github.com/geoah/substrate/commit/156ea46fc53f625c409abb76af58eefa258254ef))
* **engine:** remove the contacts GitHub's relay addresses made ([#1288](https://github.com/geoah/substrate/issues/1288)) ([42030e0](https://github.com/geoah/substrate/commit/42030e03fed252f5565d360ce87fa206d59f1a5b))
* **providers:** title Gmail threads by subject, and keep reply addresses out of contacts ([#1286](https://github.com/geoah/substrate/issues/1286)) ([52918ad](https://github.com/geoah/substrate/commit/52918ad8ebf2359a444f0d063a8f993e925e8f80))
* **runner:** end an invocation whose host call returns past its deadline ([682ffae](https://github.com/geoah/substrate/commit/682ffae5bf5d997fb9573f0d7c525a2538ad3ddc))

## [0.121.0](https://github.com/geoah/substrate/compare/v0.120.0...v0.121.0) (2026-11-08)


### ⚠ BREAKING CHANGES

* **agents:** an agent whose permissions.writes grants a glob (`*`, `<authority>/*` or `<authority>/<package>/*`) no longer writes through it declarations, triggers, `substrate.reamde.dev/llm/instruction` records or the records of `internal` kinds (threads, messages, provider rows, settings, requests and the rest of the substrate's machinery), and a function the agent runs is held to the same. An agent's declaration write that would convert live records is refused; the owner applies it. An agent meant to write one of those kinds names it in its permissions.writes, for example `substrate.reamde.dev/core/setting`, and its declaration is applied again. Rolling the binary back restores the old reach.
* **catalog:** `GET /api/v1/catalog`, `GET /api/v1/catalog/{id}` and the packages of an authorization plan answer `"tier": "published"` where they answered `"tier": "provider"`. A client that compares the tier compares against `published`, and one that needs to tell a provider from a catalogue reads the new `provider` field. The server needs nothing, and `substratectl catalog` prints `published` in its TIER column. Nothing stored changes, so a rollback answers `provider` again.

### Added

* **agents:** ship Moss, the owner's assistant, and let the owner shape it through instruction records ([#1257](https://github.com/geoah/substrate/issues/1257)) ([71b9560](https://github.com/geoah/substrate/commit/71b9560c106ad7e97b8492591b78c51b3a22ca11))
* applets, A2UI screens the substrate serves, built by Moss's designer ([#1261](https://github.com/geoah/substrate/issues/1261)) ([d79e44c](https://github.com/geoah/substrate/commit/d79e44c322b1c718fbbc03409164a4608808f196))
* **applets:** an applet names its runtime, and a2ui is the one the server runs ([#1269](https://github.com/geoah/substrate/issues/1269)) ([1f69fd5](https://github.com/geoah/substrate/commit/1f69fd5e360e39b4a14c56e623cde193bb57e65a))
* **apps:** bring Substrate Chat up to the console's agent chat ([#1260](https://github.com/geoah/substrate/issues/1260)) ([df8fb58](https://github.com/geoah/substrate/commit/df8fb58209108ec4aeab88808c34edc8156d1022))
* **apps:** open the browser sign-in once the server is found ([39305fa](https://github.com/geoah/substrate/commit/39305fa5a983b2ad9703c482818bcf2d3c3550c6))
* **auth:** let an owner skip the second factor at registration, and ask for a code only of an account that has one ([ab9ba5a](https://github.com/geoah/substrate/commit/ab9ba5a839ab047e2ed1c670bcde2489e26b039a))
* **catalog:** say what each package is for, so a catalogue is a standard collection and no account ([#1256](https://github.com/geoah/substrate/issues/1256)) ([7732e56](https://github.com/geoah/substrate/commit/7732e566cacf5218a7931e3f20958dc60e7b7064))
* **console:** ask an app's approval as one question and name its publisher on one line ([f786db6](https://github.com/geoah/substrate/commit/f786db64f5cd8ecb74773beab1aa93471c64de0a))
* **console:** list providers as rows, plainer welcome, and choose your own contact ([0845fb1](https://github.com/geoah/substrate/commit/0845fb16655fc646d13384f342b96a30c1414de5))
* **console:** make the recovery key in the browser, so a password manager saves it with the login ([ab9ba5a](https://github.com/geoah/substrate/commit/ab9ba5a839ab047e2ed1c670bcde2489e26b039a))
* **console:** mark Applets alpha and Agents, Observations and Providers beta ([#1268](https://github.com/geoah/substrate/issues/1268)) ([ebf2011](https://github.com/geoah/substrate/commit/ebf201178d012454ac4bf96733ea359232993dcf))
* **console:** ship a collection's saved views as records ([#1211](https://github.com/geoah/substrate/issues/1211)) ([9241530](https://github.com/geoah/substrate/commit/924153020fddd4a3d07195918a09f4f8fc618bc6))
* **llm:** make each provider's models records, and let an agent run on a list of them ([#1263](https://github.com/geoah/substrate/issues/1263)) ([2d355ca](https://github.com/geoah/substrate/commit/2d355caabbb061a47609e63f12fc2989703cef26))
* **mcp:** orient an assistant with an overview, a guide by topic, resources and prompts ([#1271](https://github.com/geoah/substrate/issues/1271)) ([647265d](https://github.com/geoah/substrate/commit/647265dc2d2ef58bac063c15e6b41b73be5494bb))
* Moss's builders ask before they build, and applets show what comes due ([#1266](https://github.com/geoah/substrate/issues/1266)) ([ba1e814](https://github.com/geoah/substrate/commit/ba1e8141567e392d68df1b0e6aea2858c91c52db))
* **server:** write the log as JSON when LOG_FORMAT is json ([#1247](https://github.com/geoah/substrate/issues/1247)) ([aa6e141](https://github.com/geoah/substrate/commit/aa6e141c5e9739df977156115eb141d2d986eda8))


### Fixed

* **apps:** link each app's client_uri to the site and name the publisher from SUBSTRATE_APPLE_SITE_NAME ([98100f4](https://github.com/geoah/substrate/commit/98100f4fbb4ebe436521139dd9834388dbba3d81))
* **apps:** open a reply's console links from Chat, and forward /site/ in the Console app ([#1270](https://github.com/geoah/substrate/issues/1270)) ([0bb2052](https://github.com/geoah/substrate/commit/0bb2052c545d5b18784e1067c1619fd9b74cf0ff))
* **apps:** sign Chat in with full access and read whose key each provider runs on ([#1251](https://github.com/geoah/substrate/issues/1251)) ([215740e](https://github.com/geoah/substrate/commit/215740ee5e4379c166bbdc237068bb40e500e341))
* **console:** center a table's empty state in the visible grid ([#1259](https://github.com/geoah/substrate/issues/1259)) ([66c08c3](https://github.com/geoah/substrate/commit/66c08c35c8a4950e00e6a619f4c54ba994011900))
* **console:** drop the landing bar's register button, which the landing already carries ([c42a871](https://github.com/geoah/substrate/commit/c42a8713fa73508327668d6004af9eaebfb81d9f))
* **console:** keep the shell in view under the iPad form bar, and stop zoom and overscroll ([e135f4d](https://github.com/geoah/substrate/commit/e135f4df266b4102df9b3a5bc9db1e9969aa89d2))
* **console:** mint ink ids without crypto.randomUUID, so the console loads over plain http ([#1258](https://github.com/geoah/substrate/issues/1258)) ([043165e](https://github.com/geoah/substrate/commit/043165e3d9108f27f57520d69901122e64b594a8))
* **logging:** quote every binary's lines from its first act and hold a log message constant ([#1249](https://github.com/geoah/substrate/issues/1249)) ([7c16bb3](https://github.com/geoah/substrate/commit/7c16bb38826fa61325f682086dbc4824c3efd765))

## [0.120.0](https://github.com/geoah/substrate/compare/v0.119.0...v0.120.0) (2026-11-06)


### Added

* **api:** delete a repository with both factors and free its name ([a209798](https://github.com/geoah/substrate/commit/a209798922eeaa4065a1c7134b603c97ba40a8ef))


### Fixed

* **api:** guard the console path on the value that reaches the join ([#1241](https://github.com/geoah/substrate/issues/1241)) ([754032d](https://github.com/geoah/substrate/commit/754032d082d3e78daeaba466e5ebcce0f77022fa))
* **console:** widen the door card and keep DNS record rows on one line with copy buttons ([a209798](https://github.com/geoah/substrate/commit/a209798922eeaa4065a1c7134b603c97ba40a8ef))
* **deps:** override the console's minimatch to 11 for the globparts DoS ([#1242](https://github.com/geoah/substrate/issues/1242)) ([508bad7](https://github.com/geoah/substrate/commit/508bad7e23c3acca29448b6ae11e79193b9797ab))
* **engine:** keep the credential ref and a webhook secret out of logs ([#1243](https://github.com/geoah/substrate/issues/1243)) ([b7bf6a0](https://github.com/geoah/substrate/commit/b7bf6a0fb12021df1d9cbb5dc340ed98b0a4e842))
* **engine:** refuse a spend cap past the platform's int instead of truncating it ([#1240](https://github.com/geoah/substrate/issues/1240)) ([c3c2fdd](https://github.com/geoah/substrate/commit/c3c2fdd8966fb790a5bad0bc824cc04e1475e4f8))
* **web:** clear three CodeQL findings in the console and the identity page ([#1244](https://github.com/geoah/substrate/issues/1244)) ([ad8bbee](https://github.com/geoah/substrate/commit/ad8bbeeb482e227552b620b088dbdb4d14f69110))

## [0.119.0](https://github.com/geoah/substrate/compare/v0.118.0...v0.119.0) (2026-11-06)


### ⚠ BREAKING CHANGES

* **sandbox:** a function body that declares network access can no longer open SCTP, MPTCP or raw internet sockets, send with TCP Fast Open, or connect a Unix socket (pathname or abstract). Every function body, with or without network access, now gets EACCES from setsockopt(SOL_SOCKET, SO_LINGER). In a body that declares network access, Python multiprocessing managers and the forkserver start method fail with EACCES. multiprocessing.set_start_method("spawn") or "fork" restores starting processes, but NOT managers: remove multiprocessing.Manager() and anything built on it from such a body, or move the work that uses it into a function that declares no network access. On a host whose seccomp policy refuses the main filter, a body that declares network access or runs outside code is now refused instead of starting with only the connect gate. Nothing else is needed on upgrade; rolling back to an older binary restores the old behaviour, including the isolation gaps this closes.
* **engine:** a PUT, a `substratectl apply` or an agent write that creates a record of a kind whose ids are minted (every kind that is not `internal` and does not declare `ids: {type: named}`) under an id not in the minted shape now answers `422 validation`. The minted shape is 26 lowercase Crockford base32 characters (the first `0` to `7`) or a UUID's canonical lowercase text. To upgrade, create such records with `POST /api/v1/records` (send an `Idempotency-Key` to retry safely) or `substratectl apply` of a document without `metadata.id`, and read the id from the reply; a client that must know the id first writes a UUIDv7 in either spelling, or derives one from its own key. Records already stored keep their ids, and a PUT at an existing id still updates it. An id minted before this change (12 characters of `a-z2-7`) cannot be used to create a record elsewhere: create the copy without an id. Metrics and the Apple provider's mirror kinds are named, so their writers are unaffected. Upgrade the Workouts app first (geoah/substrate `feat/workouts-minted-ids`). A rollback to an older binary quarantines the observations and apple packages, whose stored declarations now carry `ids`; the way back is this binary.

### Added

* **api:** declare the operator defaults wire shape ([#1209](https://github.com/geoah/substrate/issues/1209)) ([9bcb1e3](https://github.com/geoah/substrate/commit/9bcb1e3b81c1b78033df75779bc6148c2bdb2cfa))
* **api:** pace clients behind a listed proxy by X-Forwarded-For ([#1238](https://github.com/geoah/substrate/issues/1238)) ([1ba6225](https://github.com/geoah/substrate/commit/1ba6225df22fba564334caa9c762dd0b52bc556c)), closes [#1114](https://github.com/geoah/substrate/issues/1114)
* **console:** ask a new owner's name and save it as their contact ([#1229](https://github.com/geoah/substrate/issues/1229)) ([d38d547](https://github.com/geoah/substrate/commit/d38d547ed077d9ae61263e6ae2ea81c2af27f9d6))
* **console:** list AI models and operator keys on the Providers page ([#1212](https://github.com/geoah/substrate/issues/1212)) ([0311c59](https://github.com/geoah/substrate/commit/0311c599534ad196ce5952c3ac657847176ab021))
* **console:** read and edit objects and lists of objects in place ([#1198](https://github.com/geoah/substrate/issues/1198)) ([41847c5](https://github.com/geoah/substrate/commit/41847c5bbacd157b38f438574ec3537fbb5ce44b))
* **engine:** refuse chosen ids on new records unless the kind names them ([424751c](https://github.com/geoah/substrate/commit/424751caf53ce8616bfaee3a5ff25d430b32e807))
* **engine:** serve LLM and embeddings on operator keys under a daily allowance ([#1213](https://github.com/geoah/substrate/issues/1213)) ([d6db60c](https://github.com/geoah/substrate/commit/d6db60c694cf3af550196a0a99172bc51201df6d))
* **engine:** supply operator OAuth clients and install listed packages ([#1223](https://github.com/geoah/substrate/issues/1223)) ([b8c23c9](https://github.com/geoah/substrate/commit/b8c23c98d6c23b60da95363f716dc90f606e1a58))
* **kinds:** ship Firecrawl as a provider that can run on the operator's key ([#1221](https://github.com/geoah/substrate/issues/1221)) ([8e88583](https://github.com/geoah/substrate/commit/8e885836cb0b93f4d0b570772162f88619954bb7))
* **mcp:** let Claude Code, Codex and other assistants work a substrate over MCP ([#1214](https://github.com/geoah/substrate/issues/1214)) ([c08b022](https://github.com/geoah/substrate/commit/c08b0227819ae65ebbd2d561c8cb67c0d4af05e8))
* **site:** serve the site's icon as the console's favicon ([#1207](https://github.com/geoah/substrate/issues/1207)) ([9d6e751](https://github.com/geoah/substrate/commit/9d6e751cb61566a0890116627f24f60d467b73ab))
* **tasks:** a Needs you home screen with undoable completion ([#1216](https://github.com/geoah/substrate/issues/1216)) ([fdb7583](https://github.com/geoah/substrate/commit/fdb758336ad4d38c14f729215f1b4bb95e16c000))
* **vocabulary:** add the core contact kind ([#1210](https://github.com/geoah/substrate/issues/1210)) ([ea0fb82](https://github.com/geoah/substrate/commit/ea0fb82254276e9dbcdf0a0a923cb537ac81c3f4))
* **vocabulary:** map the shipped providers' people onto core contacts ([#1222](https://github.com/geoah/substrate/issues/1222)) ([399f91d](https://github.com/geoah/substrate/commit/399f91dbe405cfc5c09d1c978b2c31cf59f87c5a))
* **workouts:** menus grow out of glass buttons, and the island's after-line reads Next on one line ([b0602a3](https://github.com/geoah/substrate/commit/b0602a3ba74e34a9e97bb757773900f6a756ddcf))
* **workouts:** mint record ids instead of slugging names ([0cbcd74](https://github.com/geoah/substrate/commit/0cbcd7488d18e8c6b6915cb50f8be1d12f31def0))
* **workouts:** notes render Markdown and collapse past five lines with Show more ([d0992a1](https://github.com/geoah/substrate/commit/d0992a133ee2758a71baacd1dbb25db47438452b))
* **workouts:** offer the next load when two sessions reached the top of the range ([16dc751](https://github.com/geoah/substrate/commit/16dc751b27ea4e8486bdf7e4a60f267b903f2cc4))
* **workouts:** reps left replace RPE, with why the set ended beside them ([c720d5a](https://github.com/geoah/substrate/commit/c720d5ad31cb64d9b8283faeede5ebbf7e15706f))
* **workouts:** the open set names its exercise, and its contents sit flush with the panel's edge ([87e2203](https://github.com/geoah/substrate/commit/87e220384db7c7e84031ac1e542ea0762cec0482))
* **workouts:** the open set's … sits beside Log, so the header keeps its width for the title ([86402c9](https://github.com/geoah/substrate/commit/86402c925fbca6697cc733c070bc1301d55eaa9f))
* **workouts:** the sample routine shows a warm-up circuit, cues and reps left ([401e7e2](https://github.com/geoah/substrate/commit/401e7e20c67e8a502357942e3e14411d15562b17))
* **workouts:** the session's name is a large title over its length, and the bar keeps one … menu ([af134f4](https://github.com/geoah/substrate/commit/af134f49f10e0aa1decda75462cb638ee78214f9))
* **workouts:** the time the session should end, from today's sets and the plan's rests ([5409ec8](https://github.com/geoah/substrate/commit/5409ec81dbddaffe5a88a5a03182ee0e1bc5a896))
* **workouts:** training 6, with cues beside the set and warm-up circuits run in rounds ([ecb5a95](https://github.com/geoah/substrate/commit/ecb5a95de654cfaa59d851b0247d3245a89d2de3))
* **workouts:** training 7 and the review's fixes to reps left, stop reasons and the offer ([6965265](https://github.com/geoah/substrate/commit/69652657d695cae8c1251493959ca807da9e26b0))
* **workouts:** typed values apply as they are typed, and the open set's three values share one layout ([90b1c71](https://github.com/geoah/substrate/commit/90b1c71c32bff57c2821f4c6134f71707648b5b9))


### Fixed

* **api:** answer 200 to a PUT that changes nothing on a fresh record ([ff41aae](https://github.com/geoah/substrate/commit/ff41aaecdb4a36748f73f568837b6de68c72762d)), closes [#1220](https://github.com/geoah/substrate/issues/1220)
* **console:** draw the border on links styled as outline buttons ([#1230](https://github.com/geoah/substrate/issues/1230)) ([68446b0](https://github.com/geoah/substrate/commit/68446b0f1682d641c6270b1669ac7ffb18676291))
* **console:** keep typed secrets out of the mutation cache on every form ([#1232](https://github.com/geoah/substrate/issues/1232)) ([b5aac81](https://github.com/geoah/substrate/commit/b5aac812811268c2c1a0deb6a6a943678777e507))
* **console:** show provider brand names and plain setup text ([#1234](https://github.com/geoah/substrate/issues/1234)) ([c4a4e39](https://github.com/geoah/substrate/commit/c4a4e3987b6b2cb4340ee16ec1db2db651f9c5ae))
* **deps:** klauspost/compress v1.20.2, for GO-2026-6865 ([#1204](https://github.com/geoah/substrate/issues/1204)) ([fedb81f](https://github.com/geoah/substrate/commit/fedb81f7ba107eab91f846fc57c12306ad165c59))
* **engine:** make an open wait for a new repository's directory write ([#1236](https://github.com/geoah/substrate/issues/1236)) ([bd47b63](https://github.com/geoah/substrate/commit/bd47b634fb49f85719d26b61b20d975d08b163f2))
* **engine:** mark a refused key failing and say so plainly ([#1237](https://github.com/geoah/substrate/issues/1237)) ([1eda773](https://github.com/geoah/substrate/commit/1eda773595dc62d4b54ba4eae2ff3b7391411f4e))
* **llm:** count the usage a dropped Anthropic answer reported ([#1233](https://github.com/geoah/substrate/issues/1233)) ([d01f7aa](https://github.com/geoah/substrate/commit/d01f7aa3df0c4fb0b2f8d594e1d870423e3c5c5b))
* **llm:** seed the provider rows with their vendors' names as labels ([#1235](https://github.com/geoah/substrate/issues/1235)) ([35595df](https://github.com/geoah/substrate/commit/35595df579b4972e2f4e0fe19819d173ab116eba))
* **sandbox:** refuse unchecked connects and TCP Fast Open sends ([#1228](https://github.com/geoah/substrate/issues/1228)) ([cb4e372](https://github.com/geoah/substrate/commit/cb4e3729b3b1ef8192015668420c6a8ea4fb4925))
* **workouts:** a back-off set never takes the load the offer went up to ([d78a58b](https://github.com/geoah/substrate/commit/d78a58ba153e0e9a26dd488bddc96e6eaff51ab4))
* **workouts:** a rated reps left is drawn at full strength ([e449924](https://github.com/geoah/substrate/commit/e449924cda4927728e879536f03579109f7c673c))
* **workouts:** a warm-up in a group names itself, and a warm-up circuit's round says which ([b9cf1ff](https://github.com/geoah/substrate/commit/b9cf1ffc2654781b333932fe07f1d0944b53272d))
* **workouts:** line up the timers' text ([05a287b](https://github.com/geoah/substrate/commit/05a287b8258ef14caebf70db0bf46182a0fd9d30))
* **workouts:** Log's label is cut short at its end beside the …, not shrunk ([63aa8e7](https://github.com/geoah/substrate/commit/63aa8e7661b4cab125127700ecc8f140bd687672))
* **workouts:** name the held sample exercises when a signed-out session uploads ([5444340](https://github.com/geoah/substrate/commit/54443401d4e1c9a4b417df31485575705cd4bd2e)), closes [#1217](https://github.com/geoah/substrate/issues/1217)
* **workouts:** the open set's note sits as far below its title as the values sit below it ([5f2ac2b](https://github.com/geoah/substrate/commit/5f2ac2b11d71efa828ce6232b60723c661a28b7b))
* **workouts:** the reset sits next to the steps on every row, why the set ended before it ([785ba07](https://github.com/geoah/substrate/commit/785ba072763f4e410392773c9a5d09898e0ce9d5))
* **workouts:** the session asks before a load goes up, and a warm-up circuit says so ([1de3c6a](https://github.com/geoah/substrate/commit/1de3c6ab6e220afe63359659188fda8c1a0ddb0d))

## [0.118.0](https://github.com/geoah/substrate/compare/v0.117.0...v0.118.0) (2026-11-05)


### Added

* **tasks:** tasks as cards, + as the tab bar's prominent tab, one-tap list options, reminders off until asked for ([#1192](https://github.com/geoah/substrate/issues/1192)) ([1160166](https://github.com/geoah/substrate/commit/11601667c23b00eee61bddba41ece1e4da7f527b))


### Fixed

* **apps:** turn TestFlight's notify off while each build processes, not after every upload ([#1193](https://github.com/geoah/substrate/issues/1193)) ([36cba2c](https://github.com/geoah/substrate/commit/36cba2ca973556fca033b076a67c3c63952cb83c))

## [0.117.0](https://github.com/geoah/substrate/compare/v0.116.0...v0.117.0) (2026-11-05)


### Added

* **apps:** App Store screenshots for Tasks, Sync, Chat and Console ([#1188](https://github.com/geoah/substrate/issues/1188)) ([b7bbc71](https://github.com/geoah/substrate/commit/b7bbc713093bb19f07b35409240265201ed3e4de))


### Fixed

* **apple:** sign-in marks carry the stacked layers, as the app icons do ([#1190](https://github.com/geoah/substrate/issues/1190)) ([630b1b6](https://github.com/geoah/substrate/commit/630b1b6213072be319fce0b4a56c1dd05babca85))
* **apple:** the console from the App Store opens no registration ([#1189](https://github.com/geoah/substrate/issues/1189)) ([d650fd4](https://github.com/geoah/substrate/commit/d650fd462c74025ea5e622f0457231834a0e33d6))

## [0.116.0](https://github.com/geoah/substrate/compare/v0.115.0...v0.116.0) (2026-11-04)


### Added

* **apple:** mark the app icons with three stacked layers, not an infinity ([#1184](https://github.com/geoah/substrate/issues/1184)) ([17890da](https://github.com/geoah/substrate/commit/17890da58c1843cac289bcbb5486f9669c0e3860))
* **apps:** App Store listings in the tree, applied by mise run apps:store ([#1182](https://github.com/geoah/substrate/issues/1182)) ([aa1942c](https://github.com/geoah/substrate/commit/aa1942c961eb4f96ad8601c62b13f270d1754341))
* **apps:** link the publisher's privacy policy from every app's About ([#1178](https://github.com/geoah/substrate/issues/1178)) ([7fee677](https://github.com/geoah/substrate/commit/7fee6771d4adc93e2959c2659475e90448620169))
* **apps:** one app kit for every Substrate app, on Apple systems and Android ([#1169](https://github.com/geoah/substrate/issues/1169)) ([df67476](https://github.com/geoah/substrate/commit/df67476ae022946dde35f0956cb6c027e5c049fe))
* **apps:** send the iPhone and iPad apps to TestFlight on every release ([#1171](https://github.com/geoah/substrate/issues/1171)) ([8d81981](https://github.com/geoah/substrate/commit/8d81981c200c653642a51d9b10a6133713315afe))
* **console:** let signed-out visitors choose light, dark or system colors ([#1181](https://github.com/geoah/substrate/issues/1181)) ([c836cd0](https://github.com/geoah/substrate/commit/c836cd0e6bed6754ce9a5cfb527ff8322a90f0d2))
* **console:** take the reader's own domain at registration as an advanced option ([#1185](https://github.com/geoah/substrate/issues/1185)) ([f8c3606](https://github.com/geoah/substrate/commit/f8c3606a942f6b37e652950e846edcbaeed8b06a))
* let a deployment add a landing page and brand the sign-in pages ([#1173](https://github.com/geoah/substrate/issues/1173)) ([0bdd76d](https://github.com/geoah/substrate/commit/0bdd76d5f70c1c66fc1300b2a768b21f66df76ee))
* **scope:** an app's scope may name a trait ([#1170](https://github.com/geoah/substrate/issues/1170)) ([116b652](https://github.com/geoah/substrate/commit/116b65224f1c7c89cf67c3a50d3c06feb202c961))
* **vocabulary:** a kind's status binds core's lifecycle trait and names its done and dropped values ([#1176](https://github.com/geoah/substrate/issues/1176)) ([e7b4591](https://github.com/geoah/substrate/commit/e7b45912c53c454e9cfbafa099bf8ffba30c1578))


### Fixed

* **console:** register the render check's repository by label ([#1187](https://github.com/geoah/substrate/issues/1187)) ([dd42ca8](https://github.com/geoah/substrate/commit/dd42ca88c086a53c08bbeb83ccc11ffaa4456f1e))
* **engine:** a filter or order on a name that is a reference on some kinds reads each kind's rows as that kind declares it ([#1183](https://github.com/geoah/substrate/issues/1183)) ([84d7960](https://github.com/geoah/substrate/commit/84d79602fa90a3fe682be1224ee22b5375cc1c2d))
* **engine:** a filter or order on a name that is a state on some kinds reads each kind's own storage ([#1177](https://github.com/geoah/substrate/issues/1177)) ([73a6d31](https://github.com/geoah/substrate/commit/73a6d312c2ab2bf83b3e2b5febf783d560b3a24a))
* **engine:** an exists filter on a money property no longer fails to plan ([#1179](https://github.com/geoah/substrate/issues/1179)) ([f3007e7](https://github.com/geoah/substrate/commit/f3007e7cedca37fd34e231afb0a993957af52ea5))

## [0.115.0](https://github.com/geoah/substrate/compare/v0.114.1...v0.115.0) (2026-11-03)


### ⚠ BREAKING CHANGES

* **apple:** a repository holding Workouts' package under its own authority (`<authority>/training`) is not read or written by this build; the new build's approval installs `substrate.reamde.dev/training` from the authority document, and the owner takes the old package over and copies or deletes its records.
* **apple:** an app built on SubstrateKit no longer asks for `declare:` scopes or `agent` details; a package it brings is published under its authority document and asked for as a `package` detail at the version the build was written against.
* **appsetup:** an app whose approval installs a package with code (functions, agents, triggers, mappings) must ask for scopes that cover what that code reads and writes, or the package's row is blocked `reaches-past-scopes` on the approval page and in the app's set up check. Upgrading: update the app first so its request covers its packages' reach (Substrate Sync's new build asks for the apple and applehealth providers' kinds, health sleep and workouts, and observations; the owner approves it once), then roll the server. A token approved before keeps working for what it reaches; its app's next check reports the blocked row until it is approved again with the wider scopes. Rolling the server back lifts the check.
* **auth:** an app that asked for `declare:~/<package>` or sent an `agent` authorization detail is refused at `/oauth/authorize` and at `POST /api/v1/authorizations/check`; a token already carrying a `declare:` scope no longer authenticates, so the app signs in again. Publish the package under your authority's document and ask for it with a `package` detail instead; an agent ships in such a package. Send `software_version` with the authorization request and `softwareVersion` in the check body. A rollback refuses nothing: an older binary reads the new tokens as before, without the build facts.
* **vocabulary:** a token can no longer create a package under another authority. `POST /api/v1/vocabulary/apply` (and `substratectl apply -f`) of declarations that would create a package under any authority but the repository's own answers 403, and so does `substratectl apply -f` of a shipped provider's or catalogue's files. Instead: 1. Take a provider or a catalogue with `substratectl install <package>`    (`POST /api/v1/catalog/{id}/install`). 2. Take a sample with `substratectl import <sample>`, or rehome its files    with `substratectl apply --as-mine`. 3. Declare your own kinds under your repository's authority (the name you    log in with). Packages a repository already holds under another authority are unaffected and stay writable under their existing source rules. Nothing is stored or migrated, so a rollback to the previous binary restores the old rule with no other change.

### Added

* **agents:** resolve published agents through provider inputs ([c90b0d5](https://github.com/geoah/substrate/commit/c90b0d594d75bf5619269fb8651fc74da783da6b))
* **api:** open window, aggregate, semantic, blob, call and the feed's history to an app's token ([61ae414](https://github.com/geoah/substrate/commit/61ae414a950e50fe29cc153b1e7d08c049ed6649))
* **apple:** import Hevy into Substrate Workouts, seed an exercise library with movements and muscle shares, and add stats ([d72e00d](https://github.com/geoah/substrate/commit/d72e00d9fb2f3ec8fa680a81d86a46212eef7c44))
* **apple:** import Hevy into Substrate Workouts, seed an exercise library with movements and muscle shares, and add stats ([#1156](https://github.com/geoah/substrate/issues/1156)) ([d72e00d](https://github.com/geoah/substrate/commit/d72e00d9fb2f3ec8fa680a81d86a46212eef7c44))
* **apple:** mirror the call scope and the embedding clause in SubstrateKit's sentences ([72ba967](https://github.com/geoah/substrate/commit/72ba967eb7e2bbce12f52f2e6a0a5715b4d2a9ce))
* **apple:** publish Workouts' package closures on the site and keep every published version ([3041530](https://github.com/geoah/substrate/commit/30415307cf46f51be75306157448e1d62966b97a))
* **apple:** publish Workouts' training and trainingcoach packages under substrate.reamde.dev ([140c3f3](https://github.com/geoah/substrate/commit/140c3f30cd3679b73497fbae1abb77b8f5e39456))
* **apple:** retire the declare verb and the agent detail from SubstrateKit and send the build's version ([5f2eaa8](https://github.com/geoah/substrate/commit/5f2eaa8ef874d88f034959a102a0ef555157043a))
* **apple:** serve a page at each claimed callback and refuse a release signed without its associated domain ([66c294c](https://github.com/geoah/substrate/commit/66c294caa95850ba50a63f96b01d2a79a104534e))
* **apple:** show a routine's plan as a session draws it, edit it in place, and tidy its progress ([6ae32de](https://github.com/geoah/substrate/commit/6ae32de309de32071960cee574de8c716d9cc6a4))
* **apple:** sign in through a claimed https callback and find a server from a repository name ([5cb3902](https://github.com/geoah/substrate/commit/5cb39026910be6bcc622737d7fb723caf62a304c))
* **apple:** widen Substrate Sync's scopes to what its packages' code reaches ([8bee3c5](https://github.com/geoah/substrate/commit/8bee3c530885307177044ef6f578acbecd4aa913))
* **appsetup:** hold the code an approval installs to the scopes it approves ([fccea7a](https://github.com/geoah/substrate/commit/fccea7a71a28fd9d6d9b97d1052c578a241dcfb0))
* **auth:** install published packages at approval and retire declare and the agent detail ([dce37a4](https://github.com/geoah/substrate/commit/dce37a4381612e92142afecff5dde78a2f67665f))
* **auth:** name the add-ons a token keeps waiting for their provider ([132fb51](https://github.com/geoah/substrate/commit/132fb51c9479a7831530385ac74f9abbbe0001dd))
* **authorities:** confine and review document-published code ([a848e8f](https://github.com/geoah/substrate/commit/a848e8f04bf1cbe8a43ef868037bf699bb58c7aa)), closes [#1144](https://github.com/geoah/substrate/issues/1144)
* **authorize:** collect package settings and defer optional add-ons ([2b1ed55](https://github.com/geoah/substrate/commit/2b1ed55f42f3763c95c85384e3480c0f75c3120c))
* **auth:** verify an app's publisher through its domain's TXT record ([f4d4883](https://github.com/geoah/substrate/commit/f4d4883cd194a9194cdece27d5b24460c9c08bbf))
* **catalog:** install and refresh authority-published packages ([5e489ce](https://github.com/geoah/substrate/commit/5e489cec531dc3f20c25adf5990573a9652ca3c3))
* **catalog:** name the contracts of published packages and app builds ([6a99c3f](https://github.com/geoah/substrate/commit/6a99c3ff54d74e0a01e0f702a226259790a8de5b))
* **console:** group the collections by package ([#1158](https://github.com/geoah/substrate/issues/1158)) ([f334dd8](https://github.com/geoah/substrate/commit/f334dd8273b1ece7c530312bfe2a71d8df663d56))
* **console:** plan the approval again with the scopes left checked and show what a package's code reaches ([1a1a6e6](https://github.com/geoah/substrate/commit/1a1a6e61f5baecb6cacedf0080afb0f186834cd8))
* **console:** say on each app who publishes it, what it writes as, its build and the packages it needs ([d347760](https://github.com/geoah/substrate/commit/d347760876d6b2c8b9565ed973921cb4be656b04))
* **console:** say on the approval page when an app asked for another repository than the one signed in ([9492fc2](https://github.com/geoah/substrate/commit/9492fc2cac4950511cfb69469db29574e9f3a2b9))
* **console:** say what a call scope runs and what a read scope spends ([6bb5d3a](https://github.com/geoah/substrate/commit/6bb5d3a8b9aec16b6a40b31ebec11a406f772076))
* **console:** show who publishes a package on the approval page and drop the declare and agent rows ([574076a](https://github.com/geoah/substrate/commit/574076a4c9e37f05e08e3389383d5c037854bf98))
* **domains:** bound every _substrate lookup and let the operator name the resolver ([425a933](https://github.com/geoah/substrate/commit/425a9337220f24a836f509ec7365a8fd145a6fc0))
* **domains:** read the _substrate TXT record a domain publishes ([0e8d1d3](https://github.com/geoah/substrate/commit/0e8d1d30609703c716818d34c5f9f25653ac3af4))
* **repository:** verify a repository's name with its domain's TXT record ([e72a5e6](https://github.com/geoah/substrate/commit/e72a5e6cfadaf6a33d042e49a047a5ddbe62b980))
* **scope:** add the call verb and hold a function's reach to the grant ([10e8132](https://github.com/geoah/substrate/commit/10e813242d7be42e8179fbcfa30fd856174aa36d))
* **vocabulary:** declare new packages only under the repository's own authority ([7d23377](https://github.com/geoah/substrate/commit/7d23377e6f7b5ff8df77b905dd5d04e8b6ca8326))


### Fixed

* **api:** count every callee in an app's reach, and bind a published upgrade to the code reviewed ([79854b3](https://github.com/geoah/substrate/commit/79854b381257ed672c1b5b70004cf3e21e0988f7))
* **api:** grow a scoped history page as rows are kept, not from the size the caller asked for ([92899ed](https://github.com/geoah/substrate/commit/92899edea1e58b7eb2c82612a2ce3b30540ad5ba))
* **api:** keep a caller's package or request id to one log line ([feb00e3](https://github.com/geoah/substrate/commit/feb00e3f91c6b969e3fe10199786f266c25d4b0a))
* **api:** keep a refresh's error and reason to one log line ([8976502](https://github.com/geoah/substrate/commit/89765022da5d576d0fda7de1cdb1a36292f5b910))
* **api:** let an app read its own package's row, and keep a package the binary ships out of a document's refresh ([bdf7113](https://github.com/geoah/substrate/commit/bdf711373e9ee3ccbff2ce0a693125cb37e57d6f))
* **api:** list an app's three actor forms in the discovery document ([be662a4](https://github.com/geoah/substrate/commit/be662a4b5311723e7852870f9d5c14ec308bfd44))
* **apple:** check each closure the site publishes with the server's own parser, and keep every document entry whole ([5b8b049](https://github.com/geoah/substrate/commit/5b8b0499770df78c6b63fdc0e60c5778385703ac))
* **apple:** close Workouts' sign-in as soon as the session is kept ([7b2ab57](https://github.com/geoah/substrate/commit/7b2ab573591a0e8c4585e0e0fe44820f26b8430c))
* **apple:** find a server from a repository name typed as an https:// origin ([2f55d9d](https://github.com/geoah/substrate/commit/2f55d9d1d514f981775353a383978f39c9bdfeea))
* **apple:** keep the Console's rejected first sign-in where its retry looks ([8a4ecf5](https://github.com/geoah/substrate/commit/8a4ecf559f744208fe048d78297e649484e5b004))
* **apple:** keep Workouts users' data safe on upgrade and make the Hevy import lossless ([c7d9070](https://github.com/geoah/substrate/commit/c7d9070db11a3e2e53b939c484f19c7723030d37))
* **apple:** let a negative answer the plain resolver cannot confirm still find the parent server ([d653fba](https://github.com/geoah/substrate/commit/d653fba098bcdd9f17e434fc68a9703278a132a7))
* **apple:** make a routine's plan plainly read only and tidy its progress ([#1168](https://github.com/geoah/substrate/issues/1168)) ([7560c8a](https://github.com/geoah/substrate/commit/7560c8ac221a082bf656c90fe669f8e4daaa8da0))
* **apple:** name workout sets by exercise and number, mark done sets, fit the watch ([#1153](https://github.com/geoah/substrate/issues/1153)) ([a8ee65e](https://github.com/geoah/substrate/commit/a8ee65ee0ba09231e0d9ecb602c99980d366cc19))
* **apple:** read a _substrate record with any whitespace after its version ([b114cb1](https://github.com/geoah/substrate/commit/b114cb135eb63bdf3d05177c0c33c942e2b19d02))
* **apple:** say an enable row is set up, as the console does ([7d830b7](https://github.com/geoah/substrate/commit/7d830b72797f532329131f64b8d3d7071affd379))
* **apple:** say what the sign-in field takes in words a person reads ([64ce2f3](https://github.com/geoah/substrate/commit/64ce2f35b3f6efcbeaccc1f6bea909bb247a2e56))
* **apple:** stop discovery on a failed DNS lookup and keep a rejected token to revoke ([2cd0cb4](https://github.com/geoah/substrate/commit/2cd0cb4c6b7028b23c7e5517805ef113d6cf149a))
* **apple:** tell a SERVFAIL from a name without a _substrate record ([69e825d](https://github.com/geoah/substrate/commit/69e825d609843a4345bbafb7afd2facc3f59a3e1))
* **apple:** upload a phone's history after the update instead of deleting it ([0b82628](https://github.com/geoah/substrate/commit/0b8262866106abb0eb400fecf30ff01fa78c23c4))
* **appsetup:** bind an approval's set up to the closures it showed ([a064186](https://github.com/geoah/substrate/commit/a0641865d7023175351b774362c5d4ff29821a80))
* **appsetup:** bind deferred addons to checked candidates ([a0900e4](https://github.com/geoah/substrate/commit/a0900e49b14f37493d6cf4bc201d6c236535b315))
* **appsetup:** hold published agents to the owner's provider choice and deferred add-ons to live consent ([1e1082f](https://github.com/geoah/substrate/commit/1e1082f916066b94d8f55f9c66d2e07e8024186d))
* **appsetup:** install a deferred agent add-on only where its provider input resolves ([3116c23](https://github.com/geoah/substrate/commit/3116c239e2f9e967e4964c36386a6b7420bb4c05))
* **appsetup:** reconcile the approval with the catalog that reads authority documents ([8959286](https://github.com/geoah/substrate/commit/895928697be1b41e5bfedaaf07ef474f4d76c4c1))
* **auth:** distinguish and revoke retired app tokens ([b8873ae](https://github.com/geoah/substrate/commit/b8873ae5178c622370c161d350b983813014f03a))
* **authorities:** hold only outside function bodies to the sandbox's confinement ([1c558cd](https://github.com/geoah/substrate/commit/1c558cdfe2000350693c2f7a6b3262b51d319d13))
* **auth:** read an app's publisher only after the owner signs in, and end a request whose publisher moved ([d07d70e](https://github.com/geoah/substrate/commit/d07d70ec7a8b489fed4f907c7a99a7706e00d4af))
* **auth:** refuse an approval or exchange whose publisher moved ([5991928](https://github.com/geoah/substrate/commit/59919287d440bfbe5a3ad81da1eb99361e6fd582))
* **catalog:** bind installs to every approved dependency ([cdeca55](https://github.com/geoah/substrate/commit/cdeca55c57489ebbbdc07e7ae8cfd7bfb418ef4e))
* **catalog:** clear waiting add-ons after installation ([b27f373](https://github.com/geoah/substrate/commit/b27f37332d4ad70523e54cf57004cb535772fab7))
* **catalog:** count an asking agent's interactions as no reach ([b101249](https://github.com/geoah/substrate/commit/b101249e2b6ef06bf63959917e502634520af40d))
* **catalog:** expose add-on provenance and build hold reasons ([7979f46](https://github.com/geoah/substrate/commit/7979f468625bcef127986b1e9e04f96ab90103f5))
* **catalog:** hold a published package's upgrades when its document moves ([c6cb706](https://github.com/geoah/substrate/commit/c6cb70611a86b0e5570a52945693746f13dbe061))
* **catalog:** leave an unmet requirement to the engine's refusal outside an approval ([57cfa0f](https://github.com/geoah/substrate/commit/57cfa0f0bb4598a5915b6ab9b7dd41458f45d4de))
* **catalog:** never resolve or install a document's package over what the binary ships ([8fe01e3](https://github.com/geoah/substrate/commit/8fe01e331637441c050f61e1a4a372a958897afa))
* **catalog:** place a requirement the binary ships below its floor, as before ([1e8bd60](https://github.com/geoah/substrate/commit/1e8bd6052ef71562c17fcdc74f107ac2e1d54f45))
* **catalog:** send a published upgrade whose reach grows to review ([635d66c](https://github.com/geoah/substrate/commit/635d66c09ee1920e8abbe60c142de258311f6857))
* **console:** explain a held Approve, review a published upgrade before taking it, and say what waits ([d6f58e8](https://github.com/geoah/substrate/commit/d6f58e8b54c2ef5adc1157d0447c8dfa0b6a766e))
* **console:** head each app by its name and say when its token is retired ([fa3c18a](https://github.com/geoah/substrate/commit/fa3c18a39897c3dbcfac0ed9ee2b865fa65a0677))
* **console:** label an unverified app's heading and give its versions and holders their roles ([9fb737b](https://github.com/geoah/substrate/commit/9fb737bd59d48bbf1810c426c315fed02cfea471))
* **engine:** limit unchanged app check-ins to daily writes ([e6b0b7e](https://github.com/geoah/substrate/commit/e6b0b7e4183568c93ee130f79fcc121c2f528040))
* **engine:** list a token whose grant a later decision retired, instead of failing the owner's token list ([58d3733](https://github.com/geoah/substrate/commit/58d37338d99ed1554a87a359d853a34d8c5367ae))
* **repository:** bind a domain check to the proof it read, and keep what the name already publishes ([2bb3f6f](https://github.com/geoah/substrate/commit/2bb3f6f5a98a82b8e502caab3d79a933e32c039b))
* **repository:** read only the public URL when verifying a domain ([2787e50](https://github.com/geoah/substrate/commit/2787e5003ce368f59f6fe07640620d9d48fcf1af))
* **scope:** hold code globs to effective app access ([e33e675](https://github.com/geoah/substrate/commit/e33e6755a85569db3fc4d5b784044d2485be415e))
* **server:** clarify approval and authority wording ([96c7644](https://github.com/geoah/substrate/commit/96c7644b2d2282b8e8db75287af37f1feb17df03))
* **vocabulary:** let the owner repair a quarantined package of another authority ([35c10fb](https://github.com/geoah/substrate/commit/35c10fbd86a60fac6404142eb774f6ac4848aa23))

## [0.114.1](https://github.com/geoah/substrate/compare/v0.114.0...v0.114.1) (2026-11-02)


### Fixed

* **apple:** run the workout coach on Sonnet behind a LiteLLM gateway ([#1150](https://github.com/geoah/substrate/issues/1150)) ([1afe978](https://github.com/geoah/substrate/commit/1afe978ba566d658e6107d8967b0d4cf0c4eca0c))

## [0.114.0](https://github.com/geoah/substrate/compare/v0.113.0...v0.114.0) (2026-11-02)


### ⚠ BREAKING CHANGES

* **engine:** v0.114.0 needs pgvector 0.7.0 or newer (0.8 or newer for the semantic half of hybrid search). Before upgrading, connect to the substrate database (not `postgres`) and run `SELECT extversion FROM pg_extension WHERE extname='vector'`. If it is below 0.7.0: 1. get pgvector 0.7 or newer: with this repository's compose.yaml, run `docker compose pull` and recreate the database container (an image pulled long ago may still carry an older pgvector); on your own Postgres host, install the pgvector 0.7 or newer package; on a managed service, move to an engine version whose pgvector is 0.7 or newer; 2. run `ALTER EXTENSION vector UPDATE` in the substrate database; 3. start the new binary. If the check refuses, nothing is applied to the database, so v0.113.0 still boots on it unchanged and no restore is needed.
* **api:** `app:` is now a reserved actor prefix: an app's token writes under `app:<host>`, and nothing else may claim or declare it. Before upgrading: 1. If any client sends `X-Substrate-Actor` with a value that starts with    `app:`, give it another name. After the upgrade such a request    answers 403. 2. Run `substratectl get substrate.reamde.dev/core/actor` and look for an    id that starts with `app:`. Rename any you find in the package that    declares it and apply the package again. After the upgrade a stored    declaration of one quarantines its package at the next open. To roll back to a release before this one, first revoke every app token: Settings > Apps lists them, or `substratectl token list -o json` shows each token's `scopes`, and `substratectl token revoke <id>` revokes one. An older binary does not know `scopes` and reads an app's token as a full-access one.
* a rollback after this upgrade is a restore. This release carries SQL migrations 0012 to 0018, and v0.113.0 exits at boot on a database that applied them (`the database applied migrations this binary does not carry`). At each repository's first open, repository migration 3 (`name_units_by_unit_records`) runs, the boot upgrade moves core to version 44 and seeds the shipped unit records, and the search index is derived again behind the open. Before upgrading, take a database dump and a data root snapshot (docs/operations.md, Backups). To roll back, stop the server, restore both under the credential key they were taken with, and start v0.113.0.
* **engine:** an agent's query call with q or filter.search and no filter.kinds or kind now answers a tool error. To upgrade: 1. In every agent prompt you wrote that teaches a bare q or filter.search, tell the agent to name kinds from the "Kinds you can read" list in its prompt. 2. Accept the llm sample upgrade (version 24) in the console or through the catalog; an existing repository keeps version 23, whose substrate agent prompt does not mention the rule, until it accepts. A rollback restores the old behaviour with no data change.
* **engine:** `substrate.reamde.dev/llm/thread` gains the status `stopped`, which a turn settles with when the owner stops it (`POST /api/v1/substrate.reamde.dev/llm/thread/{id}/stop`) or the server shuts down mid-turn; the partial reply is kept. A client that switches over `running`, `ok`, `overbudget` and `error` must handle `stopped`. A chat turn no longer ends when its client disconnects: the run continues and is charged, and `GET /api/v1/substrate.reamde.dev/llm/thread/{id}/events` reattaches to it.
* **release:** the `stable` image tag is no longer moved. It stays at v0.113.0, so a deployment that pulls `ghcr.io/geoah/substrate:stable` receives no release after it. Pull `ghcr.io/geoah/substrate:v0` instead: it names the newest v0.x release (v0.113.0 today) and moves with each one, never to a prerelease or back to an older release. Once v1.0.0 ships, `v1` moves the same way and `v0` stays at the last v0.x. The bare `0` tag now moves under the same rule. In compose.yaml, replace `image: ghcr.io/geoah/substrate:stable` with `image: ghcr.io/geoah/substrate:v0`. A deployment that pins a version changes nothing. To roll a deployment back from what `v0` names, pin the version it ran, for example `ghcr.io/geoah/substrate:v0.113.0`; `stable` names that same v0.113.0 image and nothing newer.

### Added

* **agents:** cap daily spend per agent and repository, hold at the cap ([#911](https://github.com/geoah/substrate/issues/911)) ([a489383](https://github.com/geoah/substrate/commit/a48938367c875136a345f3e7a64001ee072d6ff2)), closes [#880](https://github.com/geoah/substrate/issues/880)
* **agents:** give every turn the time, the owner's zone and the records in view ([#1094](https://github.com/geoah/substrate/issues/1094)) ([95f1bac](https://github.com/geoah/substrate/commit/95f1bac1cebf753e36d2cc04c9c8eefdca65544d))
* **agents:** let a bundle offer its functions to the owner's assistant ([#1095](https://github.com/geoah/substrate/issues/1095)) ([fd7cac6](https://github.com/geoah/substrate/commit/fd7cac60d009a8e41e0e73c4397f47541a3db9dc))
* **agents:** link records as substrate URLs and record what a reply mentions ([#1090](https://github.com/geoah/substrate/issues/1090)) ([1884c19](https://github.com/geoah/substrate/commit/1884c198ad4d25886eb658a5d7612a54a013d425))
* **agents:** pin standing context into the cached prompt and seed llm/memory ([#1096](https://github.com/geoah/substrate/issues/1096)) ([2591cc1](https://github.com/geoah/substrate/commit/2591cc1b2d8e07a4398d76a10186a97a14f159c7))
* **agents:** replay a thread's tool exchanges on a follow-up ([#1089](https://github.com/geoah/substrate/issues/1089)) ([4040488](https://github.com/geoah/substrate/commit/4040488fdb3d98b89e3994f3bee680f2e5eabe82))
* **agents:** run a tool or sub-agent in the background and resume the thread with its result ([#1097](https://github.com/geoah/substrate/issues/1097)) ([f59a50f](https://github.com/geoah/substrate/commit/f59a50fa5dca9ccb6e22371284e5617d4124818b))
* **agents:** run independent tool calls concurrently ([#1091](https://github.com/geoah/substrate/issues/1091)) ([bdba923](https://github.com/geoah/substrate/commit/bdba923d877e63c4c0fd859e768d0244873c663e))
* **api:** give apps scoped tokens through a no-registration OAuth flow ([#1084](https://github.com/geoah/substrate/issues/1084)) ([079ff93](https://github.com/geoah/substrate/commit/079ff9318b5aeb6eda4fc751204cea40f218ec9f))
* **api:** let an app ask for full access, and sign the Console app in through it ([0815560](https://github.com/geoah/substrate/commit/08155606c934819ed3340dda32077cec15756a03))
* **api:** let an app declare one approved agent, and install the workout coach from Substrate Workouts ([#1140](https://github.com/geoah/substrate/issues/1140)) ([1a02537](https://github.com/geoah/substrate/commit/1a02537fd76adb26848ae49bf83e5b312770503a))
* **api:** let an app own the package it declares, and the owner take it over ([#1112](https://github.com/geoah/substrate/issues/1112)) ([4b2b105](https://github.com/geoah/substrate/commit/4b2b105407fbd624ebb2128eed1e40aeef332e04))
* **api:** let an app's approval set up its packages, account and webhook, and move the Apple apps onto it ([#1099](https://github.com/geoah/substrate/issues/1099)) ([b001925](https://github.com/geoah/substrate/commit/b001925b2ba46c03bc2925ee7ad0e8ce199d5913))
* **api:** let an app's token talk to agents, decide their requests and tail the change feed ([#1088](https://github.com/geoah/substrate/issues/1088)) ([bb11084](https://github.com/geoah/substrate/commit/bb1108447fa6420fea1e16e4900e006760c21327))
* **api:** let any origin call the server ([#1063](https://github.com/geoah/substrate/issues/1063)) ([aafafd5](https://github.com/geoah/substrate/commit/aafafd558e9a8f0cc9f1c6ea29a50838f54d5ddf))
* **api:** serve the aggregate read on the records route ([#976](https://github.com/geoah/substrate/issues/976)) ([2da28f8](https://github.com/geoah/substrate/commit/2da28f8d0c1463e637d0e819165e42664fd6fc55))
* **api:** show a refused sign-in on sync status outside the console ([#1007](https://github.com/geoah/substrate/issues/1007)) ([5c07c2b](https://github.com/geoah/substrate/commit/5c07c2bb1def30756f6a1a4bf0af5a4d8316e38b))
* **apple:** add a provider package for contacts a device pushes ([#956](https://github.com/geoah/substrate/issues/956)) ([ac85863](https://github.com/geoah/substrate/commit/ac85863ee90237d089bc59edc327a4a3553e2b87))
* **apple:** add macOS, iPadOS and iOS apps that bundle the console and update themselves ([#1023](https://github.com/geoah/substrate/issues/1023)) ([6281004](https://github.com/geoah/substrate/commit/62810042dbf328981b591f8c11bc1cffeec31b2a))
* **apple:** add Substrate Chat, the agent chat as a native iPhone, iPad and Mac app ([#1085](https://github.com/geoah/substrate/issues/1085)) ([734220e](https://github.com/geoah/substrate/commit/734220ec4babe0992c1306544f96757808347ec5))
* **apple:** add Substrate Sync, an iPhone and iPad app that pushes Health, Contacts and Calendar ([#1067](https://github.com/geoah/substrate/issues/1067)) ([77a3085](https://github.com/geoah/substrate/commit/77a30851b9d113c04e345fcb70f7ea33457ec9af))
* **apple:** add Substrate Workouts, strength sessions in a training package it keeps ([#1100](https://github.com/geoah/substrate/issues/1100)) ([d3642e8](https://github.com/geoah/substrate/commit/d3642e82b50129b04f322ddf77f76b7a175bef09))
* **apple:** check for updates at every launch and from the console's Settings ([f917918](https://github.com/geoah/substrate/commit/f91791844e23df768d0bd3c267cb9e7f9cf81182))
* **apple:** edit, reorder and duplicate routines in Substrate Workouts ([#1142](https://github.com/geoah/substrate/issues/1142)) ([ea0baed](https://github.com/geoah/substrate/commit/ea0baed8e5218e92350afaab17e28ee6b79d04d7))
* **apple:** give Substrate and Substrate Sync the family icon ([#1082](https://github.com/geoah/substrate/issues/1082)) ([41e4d39](https://github.com/geoah/substrate/commit/41e4d3910b4d2b8232b986faf6628a1cb8f12558))
* **apple:** give Substrate Workouts one sample routine that shows every feature, and let routines be deleted ([#1127](https://github.com/geoah/substrate/issues/1127)) ([98dd6da](https://github.com/geoah/substrate/commit/98dd6da09c84c937041d655d71072b80b4f12ff4))
* **apple:** keep a large sync running in the background with the system's progress ([#1074](https://github.com/geoah/substrate/issues/1074)) ([2d76048](https://github.com/geoah/substrate/commit/2d7604862ea4a023246830b95b699e17ae16cfc7))
* **apple:** sign the Console app in on the kit's connect screen, and keep its session in the Keychain ([2293afb](https://github.com/geoah/substrate/commit/2293afb85565d1e19d352dc8b69968d74607260e))
* **apple:** sync reminders, places and more Health, add Send to Substrate, and rework the app's screens and errors ([#1070](https://github.com/geoah/substrate/issues/1070)) ([9d72458](https://github.com/geoah/substrate/commit/9d724587f85526850cf1e6d5272246770e775ba1))
* **cli:** add validate -f, the server's declaration checks offline ([#899](https://github.com/geoah/substrate/issues/899)) ([20d57f1](https://github.com/geoah/substrate/commit/20d57f1e8669e8d7b99969e07df742112c573836))
* **cli:** print trigger, sync and bundle lists as JSON or YAML ([#894](https://github.com/geoah/substrate/issues/894)) ([0bba595](https://github.com/geoah/substrate/commit/0bba59562fa9ffa840defd893e0ba184c1b462b6)), closes [#889](https://github.com/geoah/substrate/issues/889)
* **console:** add an All range and connected lines to the timeline ([#1032](https://github.com/geoah/substrate/issues/1032)) ([f43e3c3](https://github.com/geoah/substrate/commit/f43e3c31542f7d5def48426c2bb24f5614836a7e))
* **console:** draw a table's results as tiles of one size ([#1014](https://github.com/geoah/substrate/issues/1014)) ([8ccfabb](https://github.com/geoah/substrate/commit/8ccfabb1127a54de90a57cbda2e82868c06a451e))
* **console:** draw tiles, trends and lab tables in record views ([#1004](https://github.com/geoah/substrate/issues/1004)) ([8310a7e](https://github.com/geoah/substrate/commit/8310a7ed52e8244dfd502daafbc84bbf8c5ec905))
* **console:** draw, import and show Inkidian ink and PDFs ([1caf7f7](https://github.com/geoah/substrate/commit/1caf7f792020630caa943932c4948358803d7cf8))
* **console:** list a metric once on Observations, its sources folded ([#1030](https://github.com/geoah/substrate/issues/1030)) ([a296a54](https://github.com/geoah/substrate/commit/a296a546a158294c540fb553e57fa768defd55d0))
* **console:** mark open-cycle tiles so far and draw lab ranges by type ([#1012](https://github.com/geoah/substrate/issues/1012)) ([4cb54d4](https://github.com/geoah/substrate/commit/4cb54d42e6a9c69043ebad705c991c44c331a0df))
* **console:** set the repository's daily spend cap from Settings ([#1054](https://github.com/geoah/substrate/issues/1054)) ([22889ed](https://github.com/geoah/substrate/commit/22889edeca6724a2bfa3dde567678d824e9f7a2f))
* **console:** show observations and draw record views ([#979](https://github.com/geoah/substrate/issues/979)) ([ee4fee3](https://github.com/geoah/substrate/commit/ee4fee39a65d3caf23c5725d49fbc46debde0b02))
* **core:** add an alert kind the engine raises and resolves ([#910](https://github.com/geoah/substrate/issues/910)) ([c87c12e](https://github.com/geoah/substrate/commit/c87c12e63df27e44445a3363f06cfe299637342e))
* **engine:** alert when a trigger's every run has failed for a window ([#912](https://github.com/geoah/substrate/issues/912)) ([74f1e4e](https://github.com/geoah/substrate/commit/74f1e4e0e9fe308ce8d49c2aeaa2f7750eb78fb8))
* **engine:** answer a webhook 202 after a sealed spool write ([#955](https://github.com/geoah/substrate/issues/955)) ([6a9bcc2](https://github.com/geoah/substrate/commit/6a9bcc2fd4cb1fd8c8b8bc9a0926064258163c12))
* **engine:** clear stale failing alerts and retire synced parks ([#1011](https://github.com/geoah/substrate/issues/1011)) ([a0d1941](https://github.com/geoah/substrate/commit/a0d194154d3187c8b76563eb9910b0b7cc7a6dcb))
* **engine:** derive the observed index from the fold ([#975](https://github.com/geoah/substrate/issues/975)) ([089c1bd](https://github.com/geoah/substrate/commit/089c1bdc56b44d61181e5670150d20669ae88d9f))
* **engine:** read and write quantities through the unit graph behind a switch ([#1017](https://github.com/geoah/substrate/issues/1017)) ([3a2a265](https://github.com/geoah/substrate/commit/3a2a265bca63b674de340e7e589672310096a819))
* **engine:** read semantic candidates from an HNSW index and bound the arm ([#1003](https://github.com/geoah/substrate/issues/1003)) ([b7628e9](https://github.com/geoah/substrate/commit/b7628e92db7e31fe71c7ab5bbde37b3df46db12c))
* **engine:** require kinds in agent searches, listed in the prompt ([#970](https://github.com/geoah/substrate/issues/970)) ([9617c77](https://github.com/geoah/substrate/commit/9617c77bb66c9f537da12cea8912ff163b10159b))
* **engine:** rerun an interrupted agent delivery once after a restart ([#900](https://github.com/geoah/substrate/issues/900)) ([ff151f2](https://github.com/geoah/substrate/commit/ff151f25840dca47be334b76c50be2880eeb2585))
* **engine:** run chat turns detached with thread attach and stop verbs ([#963](https://github.com/geoah/substrate/issues/963)) ([4fc3e59](https://github.com/geoah/substrate/commit/4fc3e59459aaf068a7c7b3708086cfab8031949f))
* **engine:** run due schedule fires in parallel, one per trigger ([#902](https://github.com/geoah/substrate/issues/902)) ([58a89c2](https://github.com/geoah/substrate/commit/58a89c2db46ae8ba15318697ecc2a6ec2e0a982a)), closes [#883](https://github.com/geoah/substrate/issues/883)
* **engine:** seed core unit and conversion records and guard their deletion ([#1016](https://github.com/geoah/substrate/issues/1016)) ([1df276d](https://github.com/geoah/substrate/commit/1df276dbca809d6ea4a26a5476723843f6226952))
* **llm:** cache the static prompt prefix on both wires ([#1092](https://github.com/geoah/substrate/issues/1092)) ([be7445f](https://github.com/geoah/substrate/commit/be7445f75da2418b2f08d1dcc3d2c0dd425e1b72))
* **llm:** keep a thread's last reply and order chats by last activity ([#961](https://github.com/geoah/substrate/issues/961)) ([95c63b7](https://github.com/geoah/substrate/commit/95c63b7f388e4aea7f92daf65850864822124451))
* **llm:** stream the model's reasoning and keep a summary on the turn ([#960](https://github.com/geoah/substrate/issues/960)) ([397ac37](https://github.com/geoah/substrate/commit/397ac372b4e6c2407a07e59164ed4ea630ef8951))
* **llm:** title a thread from its first message, then from its model ([#958](https://github.com/geoah/substrate/issues/958)) ([d1e100b](https://github.com/geoah/substrate/commit/d1e100bb897ffb76b4ce7d5080193d51d5b08356))
* **observations:** add the observed trait and the observation samples ([#974](https://github.com/geoah/substrate/issues/974)) ([51237fc](https://github.com/geoah/substrate/commit/51237fc9f61abc8effffe45343883df211b0cd2b))
* **observations:** read linked sources as one device under priority ([#1026](https://github.com/geoah/substrate/issues/1026)) ([c09250a](https://github.com/geoah/substrate/commit/c09250a2f7f43f503148e5ee280c8cd34496576a))
* **providers:** list WHOOP cycles in the console, titled by date ([#1013](https://github.com/geoah/substrate/issues/1013)) ([7d0a0cd](https://github.com/geoah/substrate/commit/7d0a0cdfcc369edafb88c7b057934ee57911ca67))
* **providers:** receive WHOOP webhooks verified by the door ([#981](https://github.com/geoah/substrate/issues/981)) ([c4ed1e3](https://github.com/geoah/substrate/commit/c4ed1e325e473de3bb710590ba97717d074e2253))
* read and write every quantity through the repository's units ([#1019](https://github.com/geoah/substrate/issues/1019)) ([b13d60f](https://github.com/geoah/substrate/commit/b13d60fe7286f7228b8c73f8a8ebcdf693629450))
* **release:** tag the newest v0.x release image `v0`, drop `stable` ([#877](https://github.com/geoah/substrate/issues/877)) ([08a3bd0](https://github.com/geoah/substrate/commit/08a3bd0cf90c4143c348c9122d7ace485ff7545d))
* **samples:** add an ink sample with note and markup kinds ([f46b536](https://github.com/geoah/substrate/commit/f46b536a51f7e3e59ecd29063c88038f4b02cfcd))
* **samples:** keep the 1h and 1d tiers for shared sensor metrics ([#1035](https://github.com/geoah/substrate/issues/1035)) ([16b788c](https://github.com/geoah/substrate/commit/16b788cbe49b22f1ff86bfb45005647b400cbc51))
* **samples:** make the default assistant a general personal assistant ([#1098](https://github.com/geoah/substrate/issues/1098)) ([c0e364a](https://github.com/geoah/substrate/commit/c0e364aec1fd3f6c27b3446ed56130105da31d20))
* **samples:** move WHOOP's score metrics from health to healthwhoop ([#1028](https://github.com/geoah/substrate/issues/1028)) ([47384f3](https://github.com/geoah/substrate/commit/47384f3adcbbe4b10ad0c78161a2359045a04926))
* **samples:** read meter glucose in mg/dL and mmol/L as one series ([#1031](https://github.com/geoah/substrate/issues/1031)) ([b329e1d](https://github.com/geoah/substrate/commit/b329e1d5c2c102cd0de621460657cc5c8c1dcf30))
* **samples:** turn WHOOP mirror rows into health records ([#977](https://github.com/geoah/substrate/issues/977)) ([4544613](https://github.com/geoah/substrate/commit/4544613c9b5894fd8bb2718a03e78fed1fcc780b))
* **slack:** apply live Socket Mode events from a webhook trigger ([#951](https://github.com/geoah/substrate/issues/951)) ([39ff180](https://github.com/geoah/substrate/commit/39ff180828c0244a624e5f8bec82f6ad60e989e4))
* **slacklive:** move Slack live events to an opt-in bundle and bridge ([#957](https://github.com/geoah/substrate/issues/957)) ([d9f3554](https://github.com/geoah/substrate/commit/d9f3554a8171bb19d5e7b367dd0c65cfe6d10199))
* **synth:** generate one invented person's WHOOP and manual data ([#980](https://github.com/geoah/substrate/issues/980)) ([e8f427e](https://github.com/geoah/substrate/commit/e8f427e1d7e6dbf44014b1cc47d9b7f63987d509))
* **vocabulary:** add a unit graph and generated unit records ([#1015](https://github.com/geoah/substrate/issues/1015)) ([ad1dd5c](https://github.com/geoah/substrate/commit/ad1dd5c0f42e14a88af6feb622d18ae3d26af058))
* **vocabulary:** add the clinical catalogue of FHIR-shaped record kinds ([cac5e2e](https://github.com/geoah/substrate/commit/cac5e2e449a120974462c7651ce39f3b3db1525c))
* **vocabulary:** add the quantity type and the unit table ([#973](https://github.com/geoah/substrate/issues/973)) ([2252162](https://github.com/geoah/substrate/commit/2252162c315efe773c6c4ed80705e25a413fa0f7))
* **vocabulary:** let a blobref declare the mediaTypes of its bytes ([66d9e97](https://github.com/geoah/substrate/commit/66d9e97272e61cbaaca646de75e7fccd1dd4289e))
* **vocabulary:** seed observations, and ship health, labs and environment as first-party catalogues ([#1069](https://github.com/geoah/substrate/issues/1069)) ([5040132](https://github.com/geoah/substrate/commit/5040132b6fbc524ddbae6976e85c0242b1d5d3ef))
* **vocabulary:** seed record page layouts in a console package ([#978](https://github.com/geoah/substrate/issues/978)) ([2e61089](https://github.com/geoah/substrate/commit/2e61089976c8b5eddc0f2d037f23ad80bd3b4ed4))
* **webhooks:** verify provider signatures and bound bodies per trigger ([#972](https://github.com/geoah/substrate/issues/972)) ([3186347](https://github.com/geoah/substrate/commit/3186347798ad04d6a5bdad205392f4a4b5c6d874))


### Fixed

* **agents:** cancel late jobs on stop, bound jobs by their own timeout ([#1131](https://github.com/geoah/substrate/issues/1131)) ([20b9668](https://github.com/geoah/substrate/commit/20b9668af8f35000bffb70f2c9aef5bb3e6268ff))
* **agents:** end a run at its deadline as overbudget, after a last turn ([#896](https://github.com/geoah/substrate/issues/896)) ([997967b](https://github.com/geoah/substrate/commit/997967b5f3fc70dad02595a5810b6a4a16ff4571)), closes [#882](https://github.com/geoah/substrate/issues/882)
* **aggregate:** start a calendar day at its first instant across DST ([#1128](https://github.com/geoah/substrate/issues/1128)) ([dc2cc8c](https://github.com/geoah/substrate/commit/dc2cc8c01a231b4955393fe5e276e1d4df20e7fd))
* **api:** cap login hashing, metric series and slow request bodies ([#925](https://github.com/geoah/substrate/issues/925)) ([dc0d436](https://github.com/geoah/substrate/commit/dc0d436b4cadc764dc99b6e0415fdfe1095fd486))
* **api:** refuse a chat context record an app's token cannot read ([#1129](https://github.com/geoah/substrate/issues/1129)) ([3415cd3](https://github.com/geoah/substrate/commit/3415cd37cfb1b349c4ce12454ac87487bb81260f))
* **api:** refuse scoped writes that map a reference to a hidden kind ([#1125](https://github.com/geoah/substrate/issues/1125)) ([741567b](https://github.com/geoah/substrate/commit/741567b1b6084b2c208b496cd38be21a72e61606))
* **apple:** close the workout coach gaps its first live run showed ([#1143](https://github.com/geoah/substrate/issues/1143)) ([b450583](https://github.com/geoah/substrate/commit/b45058309388791f3b52d62d08fc11acb3ef959c))
* **apple:** end the Console app's sign-in once, for the token that ended, and keep every server's ([2b62771](https://github.com/geoah/substrate/commit/2b6277119081d8f4a27860fef382997707f3571a))
* **apple:** keep a continued sync running in the background, with Health shown month by month ([#1083](https://github.com/geoah/substrate/issues/1083)) ([a3f6598](https://github.com/geoah/substrate/commit/a3f659897fc9f26707cf3632c3408b20dfd32b60))
* **apple:** keep the Health scan between launches, and show Health as behind while history is unsent ([d930fcd](https://github.com/geoah/substrate/commit/d930fcd02d9407ee939557053e4c926c92a470f9))
* **apple:** let a workout session be logged in any order, with a three-part island ([#1110](https://github.com/geoah/substrate/issues/1110)) ([9bc25b4](https://github.com/geoah/substrate/commit/9bc25b499dfa668f04dab750c1d25e42d9fc197a))
* **apple:** save a deleted set at once, pre-fill a rirstop set's reps, keep deletes to one server and never write over a changed package ([#1111](https://github.com/geoah/substrate/issues/1111)) ([c2fd68d](https://github.com/geoah/substrate/commit/c2fd68d67f134776ba63ae26b90342685054122e))
* **apple:** sign in on a Mac in the default browser, not a sheet over the window ([9e88cee](https://github.com/geoah/substrate/commit/9e88ceee9388398959d127873953ff9c83a3c236))
* **apple:** stop re-sending Health history from its first day, and stop flooding the server's spool ([37cd818](https://github.com/geoah/substrate/commit/37cd8185c909ab7643145347b9e2e32b19e0ded8))
* **apple:** teach the workout coach to encode a brought plan, grouped blocks and several medical conditions ([#1141](https://github.com/geoah/substrate/issues/1141)) ([bd7d3a2](https://github.com/geoah/substrate/commit/bd7d3a207537c6af1a63ea0232d205848e423292))
* **beeper:** pause the sync while Beeper Desktop is unreachable ([#1008](https://github.com/geoah/substrate/issues/1008)) ([d676493](https://github.com/geoah/substrate/commit/d676493ec7c24bab95db03b4c59ffdc959557b7f))
* **catalog:** list the catalog without counting every live row once per provider entry ([#1087](https://github.com/geoah/substrate/issues/1087)) ([7adb395](https://github.com/geoah/substrate/commit/7adb3957e8f9b60a79f6c8d7f0e8b60d821b00c7)), closes [#1086](https://github.com/geoah/substrate/issues/1086)
* clear the audit: pin undici and move to Go 1.27.3 ([#1093](https://github.com/geoah/substrate/issues/1093)) ([7fbb396](https://github.com/geoah/substrate/commit/7fbb396329526f3a50e9182a8b1423bf813b1b90))
* **console:** close the ⌘K palette on the first Escape ([a79cab7](https://github.com/geoah/substrate/commit/a79cab7b0fa997c7e88474d6c2f9e50b0750499f))
* **console:** draw a metric's summaries where a chart has no samples ([#1027](https://github.com/geoah/substrate/issues/1027)) ([265f58c](https://github.com/geoah/substrate/commit/265f58c14914a673bdea4333db74d08fe84fdb32))
* **console:** float the sidebar peek below the header row ([03be0fb](https://github.com/geoah/substrate/commit/03be0fbbc76f178c4cee9ddca1c07cb667907bea))
* **console:** keep digit-only search params and reload stale chunks ([#935](https://github.com/geoah/substrate/issues/935)) ([731bd96](https://github.com/geoah/substrate/commit/731bd96e3f7e7a187a146307b0185e9659858033))
* **console:** keep unsent chat messages and say why a send failed ([#1042](https://github.com/geoah/substrate/issues/1042)) ([aa9b254](https://github.com/geoah/substrate/commit/aa9b254cc458f5007b432dc49d6c5c319a6b9eff)), closes [#1038](https://github.com/geoah/substrate/issues/1038)
* **console:** list the catalogues' kinds with your data, not as machinery ([c1bb750](https://github.com/geoah/substrate/commit/c1bb750c344e30f6baece60f82f39a44033d2c5b))
* **console:** omit empty optional fields from a record create ([610092b](https://github.com/geoah/substrate/commit/610092bc79dcf13e989d726939482e1ba1c99ae9))
* **console:** render markdown links in chat replies ([#1041](https://github.com/geoah/substrate/issues/1041)) ([7b428b9](https://github.com/geoah/substrate/commit/7b428b92c753d9d06c3250fa35f5b027db5b998b))
* **console:** split the clinical kind names into known words ([d10034d](https://github.com/geoah/substrate/commit/d10034dfc425c758f19a9f0693976c9ffb4c1d90))
* **dev:** put the real uv first on every dev start and skip PEP 723 cases below python 3.11 ([#1050](https://github.com/geoah/substrate/issues/1050)) ([17decee](https://github.com/geoah/substrate/commit/17decee7a50842912c687a8a75a67131698a4642))
* **egress:** name the gate and its allowlist variable on a refusal ([#895](https://github.com/geoah/substrate/issues/895)) ([225492d](https://github.com/geoah/substrate/commit/225492d63415d0db67c91d8171a7e9e8f8443e92)), closes [#886](https://github.com/geoah/substrate/issues/886)
* **engine:** admit a webhook delivery in one changelog transaction ([#954](https://github.com/geoah/substrate/issues/954)) ([5c01301](https://github.com/geoah/substrate/commit/5c013012c4c4e1b25544a8c5389c538a5dc9fe20))
* **engine:** bound agent function calls by their declared timeout ([#965](https://github.com/geoah/substrate/issues/965)) ([5c177e0](https://github.com/geoah/substrate/commit/5c177e0137f22a5a074a76366f9b44ad45131b1c))
* **engine:** bound agent searches, tool calls and tool results ([#964](https://github.com/geoah/substrate/issues/964)) ([94ddccf](https://github.com/geoah/substrate/commit/94ddccfc5b44570be1771f202d7a602903d6e55c))
* **engine:** cache RRULE positions and read trigger runs via refs ([#931](https://github.com/geoah/substrate/issues/931)) ([19b1579](https://github.com/geoah/substrate/commit/19b1579ed72f14e98da33a332362754f4d0fabee)), closes [#874](https://github.com/geoah/substrate/issues/874) [#872](https://github.com/geoah/substrate/issues/872) [#835](https://github.com/geoah/substrate/issues/835)
* **engine:** clear a thread's lease when it settles, and leave a newer claim alone ([#1051](https://github.com/geoah/substrate/issues/1051)) ([576c99a](https://github.com/geoah/substrate/commit/576c99aaba3af7ebdcfea37da3af90a6e26d2a2f)), closes [#1038](https://github.com/geoah/substrate/issues/1038)
* **engine:** compare typed filters by value when filter.kinds is absent ([#1053](https://github.com/geoah/substrate/issues/1053)) ([a0cabbe](https://github.com/geoah/substrate/commit/a0cabbe12ea48e5b257b63296f978813cde3301e))
* **engine:** contain a panicking record trigger and stamp its last pass ([#908](https://github.com/geoah/substrate/issues/908)) ([6f86b41](https://github.com/geoah/substrate/commit/6f86b417df3564fb0bac99ec4021b42d2718a564))
* **engine:** decode a write tool input sent as a JSON string ([#903](https://github.com/geoah/substrate/issues/903)) ([993f02a](https://github.com/geoah/substrate/commit/993f02af4eb209a2f15c8559c7d5bc4771112bf3))
* **engine:** embed a declared body from its column ([#1001](https://github.com/geoah/substrate/issues/1001)) ([9054d7d](https://github.com/geoah/substrate/commit/9054d7d771470bccd413763a5d76061f3951c8e5))
* **engine:** erase sealed orphans when a repository opens as its writer ([#1049](https://github.com/geoah/substrate/issues/1049)) ([8991e71](https://github.com/geoah/substrate/commit/8991e7140859638242323c9413960688f2f653b0))
* **engine:** fall back to ts_rank order when BM25F scoring times out ([#967](https://github.com/geoah/substrate/issues/967)) ([725bdf3](https://github.com/geoah/substrate/commit/725bdf321a934c26c78f5c9d5e802c73c654eba0))
* **engine:** hold a shipped record any hand but its shipper wrote ([#1048](https://github.com/geoah/substrate/issues/1048)) ([62cc196](https://github.com/geoah/substrate/commit/62cc1960893e1e61e0f7479241f66f1a0149f3be)), closes [#950](https://github.com/geoah/substrate/issues/950)
* **engine:** index an observed point at 0001-01-01T00:00:00Z ([#1033](https://github.com/geoah/substrate/issues/1033)) ([b2a3b06](https://github.com/geoah/substrate/commit/b2a3b064baf5ed6bcafbef341731034fbb318924))
* **engine:** keep an embed queue row when its record read fails ([d9d45ee](https://github.com/geoah/substrate/commit/d9d45eea1a29b3c25e449ab72d686b272e43443b))
* **engine:** let an owner's write go ahead of waiting background writers ([#905](https://github.com/geoah/substrate/issues/905)) ([efbc1b4](https://github.com/geoah/substrate/commit/efbc1b45d09f5b6e5c7d404cc020800c1386ea08)), closes [#893](https://github.com/geoah/substrate/issues/893)
* **engine:** let webhook admission go ahead of waiting background writers ([#952](https://github.com/geoah/substrate/issues/952)) ([a50b39d](https://github.com/geoah/substrate/commit/a50b39d96b73950290466f1b6c50ed2ffa6d9539))
* **engine:** match the resolution sweep's threads to their messages through refs ([#1072](https://github.com/geoah/substrate/issues/1072)) ([17c355b](https://github.com/geoah/substrate/commit/17c355b928337b88f0715a548015b00f5b5f288c))
* **engine:** name the data root in rollback repair and stamp dialect 2 ([#1126](https://github.com/geoah/substrate/issues/1126)) ([963c535](https://github.com/geoah/substrate/commit/963c53581b116b07ca23bcedd8226df3f5ee78fb))
* **engine:** pack embed requests and move a refused value back ([#1002](https://github.com/geoah/substrate/issues/1002)) ([9153eaa](https://github.com/geoah/substrate/commit/9153eaada75ad61f0bd8d4d30b96f065c1c36721))
* **engine:** price cached prompt tokens at the row cache rates ([#1006](https://github.com/geoah/substrate/issues/1006)) ([818412a](https://github.com/geoah/substrate/commit/818412a43e27b0c6b95bf26cc5de2e7aa7e66f13))
* **engine:** re-derive a row the index pass skipped before a restart ([#876](https://github.com/geoah/substrate/issues/876)) ([df50f2c](https://github.com/geoah/substrate/commit/df50f2c2421d6a52c5d13d6e017458e2ab5d2e56)), closes [#868](https://github.com/geoah/substrate/issues/868)
* **engine:** re-derive only moved rows on apply, search index later ([#904](https://github.com/geoah/substrate/issues/904)) ([5c4b874](https://github.com/geoah/substrate/commit/5c4b87493bbaf682ba345fef2d8b38ff32485b27)), closes [#888](https://github.com/geoah/substrate/issues/888)
* **engine:** read exact mapping probes through the props index ([#953](https://github.com/geoah/substrate/issues/953)) ([c625022](https://github.com/geoah/substrate/commit/c6250227abc5d5f068937b24d26302be87503584))
* **engine:** read search statistics outside the search budget ([#969](https://github.com/geoah/substrate/issues/969)) ([83d344d](https://github.com/geoah/substrate/commit/83d344dd361d311ef830f2c332bf4c4dd60cbbb4))
* **engine:** read thread transcripts and reference filters via refs ([#930](https://github.com/geoah/substrate/issues/930)) ([36910b3](https://github.com/geoah/substrate/commit/36910b32ef677bb41841b88dee9928dc2cbd5691)), closes [#798](https://github.com/geoah/substrate/issues/798)
* **engine:** refuse to boot on pgvector below 0.7 before migrating ([#1130](https://github.com/geoah/substrate/issues/1130)) ([b724270](https://github.com/geoah/substrate/commit/b724270d97ea2342748c48fa738d2e3bd55c0328))
* **engine:** request a sync when an account reconnects ([#1005](https://github.com/geoah/substrate/issues/1005)) ([f155837](https://github.com/geoah/substrate/commit/f155837845cd342fb213c5688f6847dcff7604f8))
* **engine:** retake the writer lease from this process's own orphaned session, and reap it in seconds ([#1080](https://github.com/geoah/substrate/issues/1080)) ([4c928a1](https://github.com/geoah/substrate/commit/4c928a16b85f5fc99e5b47f0f3d364d265f6712a))
* **engine:** settle a sub-agent's thread when its parent's deadline ends mid-write ([#1046](https://github.com/geoah/substrate/issues/1046)) ([07d7ddd](https://github.com/geoah/substrate/commit/07d7ddd24d3076ee79e3be4e2b0560e9c3f07ecd)), closes [#819](https://github.com/geoah/substrate/issues/819)
* **engine:** stop a failed message write leaving its thread `running` ([#966](https://github.com/geoah/substrate/issues/966)) ([cc23bbc](https://github.com/geoah/substrate/commit/cc23bbc12351a8bf45607a8480ac2ce24634e3e1))
* **engine:** stop refreshing a credential refused with invalid_grant ([#909](https://github.com/geoah/substrate/issues/909)) ([b90f208](https://github.com/geoah/substrate/commit/b90f20854616cbebf00db5b7094076061ec85f82))
* **engine:** walk a record trigger's backlog one caller at a time ([#1010](https://github.com/geoah/substrate/issues/1010)) ([cc67fe2](https://github.com/geoah/substrate/commit/cc67fe227a44e77cc47c6b4e13bb5a5752796a00))
* **export:** keep listed blob bytes until the export stream ends ([#1025](https://github.com/geoah/substrate/issues/1025)) ([19614a3](https://github.com/geoah/substrate/commit/19614a3d3e89bbe8f3cd432c177e68209c7301e7))
* **google:** report an account erroring while any stream is broken ([#933](https://github.com/geoah/substrate/issues/933)) ([e905861](https://github.com/geoah/substrate/commit/e905861b00b3e5ec84fc8bab271e41230a20d1a4)), closes [#817](https://github.com/geoah/substrate/issues/817) [#818](https://github.com/geoah/substrate/issues/818)
* **llm:** head a new thread with the plain text of its first message ([#962](https://github.com/geoah/substrate/issues/962)) ([a0cd149](https://github.com/geoah/substrate/commit/a0cd149b1ea9b508be5be5e4c890560731a78740))
* **oauth:** back off refused refreshes and time out token calls ([#929](https://github.com/geoah/substrate/issues/929)) ([1b8f2de](https://github.com/geoah/substrate/commit/1b8f2de83a39f690ae99f4c19877432fef9abd7a))
* **oauth:** keep a rotated refresh token when its store fails ([#1009](https://github.com/geoah/substrate/issues/1009)) ([d253907](https://github.com/geoah/substrate/commit/d2539077a30b7311f0bd2f008872ef9b76087dae))
* **observations:** refuse a code over 255 characters with a 422 ([#1029](https://github.com/geoah/substrate/issues/1029)) ([d058043](https://github.com/geoah/substrate/commit/d05804371a2e84c52e42c4c3c15c3e4fed9d2c46))
* **providers:** pin requests 2.34.2 and lint every bundle's Python ([#932](https://github.com/geoah/substrate/issues/932)) ([4285b8f](https://github.com/geoah/substrate/commit/4285b8f4fa2d3846560e5cb24bb06471be60f72a))
* **sandbox:** list each missing layer and its reason when DEGRADED ([#898](https://github.com/geoah/substrate/issues/898)) ([5eb39de](https://github.com/geoah/substrate/commit/5eb39de7df60b3ac341617869e56e940c8be480b))
* **sandbox:** refuse body signals to the server's pid, group and pid 1 ([#927](https://github.com/geoah/substrate/issues/927)) ([0fca64c](https://github.com/geoah/substrate/commit/0fca64c7b0049bd36f2d2190ab04e1cfe306b561)), closes [#831](https://github.com/geoah/substrate/issues/831)
* **search:** keep deleted records from filling semantic candidates ([#1135](https://github.com/geoah/substrate/issues/1135)) ([9c47f74](https://github.com/geoah/substrate/commit/9c47f740ef4653ab8b2816ce87689cc2bd3fbcb2))
* **search:** rank a cold common word's newest matches instead of answering 503 ([#1052](https://github.com/geoah/substrate/issues/1052)) ([aca9780](https://github.com/geoah/substrate/commit/aca9780d9f005b269994886b3c21a35eeb66727d)), closes [#968](https://github.com/geoah/substrate/issues/968)
* **server:** drain watch streams at shutdown and log 503 causes ([#928](https://github.com/geoah/substrate/issues/928)) ([f5b661c](https://github.com/geoah/substrate/commit/f5b661c67728f75903449e589b6edbe8063f2fc9))
* **sync:** mark only the parked delivery's stream as erroring ([#901](https://github.com/geoah/substrate/issues/901)) ([9222afd](https://github.com/geoah/substrate/commit/9222afddf841a85c7ed2e9a0a8665a637f4a372c))
* **vocabulary:** align the clinical kinds with FHIR codes and GP Connect ([4f494cd](https://github.com/geoah/substrate/commit/4f494cd7794debd534d5b08aa54da1617c043477))
* **webhooks:** refuse a NUL part name and set aside refused spool files ([#1132](https://github.com/geoah/substrate/issues/1132)) ([16bf527](https://github.com/geoah/substrate/commit/16bf527e2f1177e121b6257204f229ee6b8ec82b))

## [0.113.0](https://github.com/geoah/substrate/compare/v0.112.1...v0.113.0) (2026-10-07)


### ⚠ BREAKING CHANGES

* **deps:** the server and `substratectl --dsn` now parse `DATABASE_URL` with pgx 5.11, which reads a connection string as libpq does. Most URLs are unaffected. Before upgrading, run `psql "$DATABASE_URL" -c 'select 1'`: libpq refuses the same URLs with the same messages, so a URL psql connects with boots the server, and one it refuses names the part to rewrite. The shapes 5.10 accepted and 5.11 refuses: a `+` meant as a space in a query value (write `%20`); an unencoded `@` or space in the password or any other part (write `%40` or `%20`); an unencoded `=` or `#` in a query value (write `%3D` or `%23`; `options=-c search_path=public` becomes `options=-c%20search_path%3Dpublic`); a stray `%` not followed by two hex digits (write `%25`); a bare IPv6 host (write `postgres://[::1]/db`); a keyword/value string with a backslash (double it). Two shapes parse differently rather than fail: a key repeated in the query now takes its last value, not its first, and in a multi-host URL each host keeps its own port instead of sharing the last one. A URL rewritten this way also works on v0.112.1. The URL never blocks a rollback; the schema migration this release carries does, see the vocabulary entry.
* **vocabulary:** a function or agent whose `permissions.writes` names `substrate.reamde.dev/core/token`, `substrate.reamde.dev/core/credential` or `substrate.reamde.dev/core/recoverykey` is refused at `POST /api/v1/vocabulary/apply`, catalog import and install with `422`. One already stored quarantines its whole package at the first boot: the repository opens and serves, but every kind of that package is unreadable and unwritable and its triggers skip until the corrected closure is applied, and the boot logs `quarantining a stored closure that no longer loads under this binary` at ERROR, twice on the first boot and once on every later one. `substratectl bundle list` names every quarantined package and `substratectl bundle status <id>` prints the reason (its "re-install the bundle" hint applies to a catalog package only). Delete the entry from `permissions.writes`, then apply the package's corrected documents, the whole package, with `substratectl apply -f`: a closure that admits clears the quarantine. Installed code no longer revokes a token: a function's `delete` effect and an agent's `write` of a token delete answer `403`, and a policy judge's accept of a proposed token delete leaves the proposal `proposed` and escalates it to the owner (its audit reads `escalated`). The owner revokes with `substratectl token revoke <id>`, from the console, or by accepting the escalated proposal. Globs never reached these kinds, so `writes: ["*"]` is unaffected, and no shipped bundle or sample names them. This release also carries SQL migration `0011_index_reprojections`, applied by the first boot before any repository opens; after it, v0.112.1 exits at boot with `the database applied migrations this binary does not carry`. Take a database dump and a data root snapshot before upgrading (docs/operations.md, Backups): a rollback is starting v0.112.1 on both copies, which loads such a package again and lets its code delete tokens again.

### Added

* **agents:** count the child chain's writes on sub-agent tool rows ([#804](https://github.com/geoah/substrate/issues/804)) ([2433223](https://github.com/geoah/substrate/commit/2433223cb78942b685e5f6d5d7222a85488d62ee)), closes [#70](https://github.com/geoah/substrate/issues/70)
* **agents:** stamp the callable identity on llm/message tool rows ([#790](https://github.com/geoah/substrate/issues/790)) ([b614980](https://github.com/geoah/substrate/commit/b61498027966effdc9827cca0e68bf2965c02656))
* **api:** accept a list of targets in filter.referencing.refs ([#794](https://github.com/geoah/substrate/issues/794)) ([1fcef76](https://github.com/geoah/substrate/commit/1fcef7692412282c812eb2e67cec40d0e8c8aa1f))
* **console:** edit what an agent can see and change in the chat panel ([#814](https://github.com/geoah/substrate/issues/814)) ([1750fc2](https://github.com/geoah/substrate/commit/1750fc2f0b1b8bef4ed5ecef4fcf2622733df8c6))
* **console:** show an agent's spend by day, week and month ([#816](https://github.com/geoah/substrate/issues/816)) ([c00cc45](https://github.com/geoah/substrate/commit/c00cc451d1a81bea3a1e16c581729885495be613))
* **engine:** snapshot with --base links unchanged segments ([#857](https://github.com/geoah/substrate/issues/857)) ([dfb8598](https://github.com/geoah/substrate/commit/dfb85986875f513d9d225cbd50faca633aeb50e7))
* **release:** tag the newest release image `stable` ([#848](https://github.com/geoah/substrate/issues/848)) ([7355382](https://github.com/geoah/substrate/commit/735538258a257b2a610715adde511e8232b34b0c))
* **vocabulary:** store a field's default in each object a write sends ([#791](https://github.com/geoah/substrate/issues/791)) ([8eb7dcf](https://github.com/geoah/substrate/commit/8eb7dcf784ac9084f7e3df4c9457fba20e846860)), closes [#248](https://github.com/geoah/substrate/issues/248)
* **vocabulary:** title a trigger from its optional `label` ([#789](https://github.com/geoah/substrate/issues/789)) ([8c93530](https://github.com/geoah/substrate/commit/8c93530b3bf0a8ceaa6dc21734ffb31d97bc5dce)), closes [#118](https://github.com/geoah/substrate/issues/118)


### Fixed

* **console:** ask the last-chatted agent that can declare a kind ([#805](https://github.com/geoah/substrate/issues/805)) ([8a3887e](https://github.com/geoah/substrate/commit/8a3887e0a00effe55204405dd498c846ffe18fc9))
* **console:** clear the npm audit advisories ([#865](https://github.com/geoah/substrate/issues/865)) ([e971db2](https://github.com/geoah/substrate/commit/e971db20f69ea17799d426a10e6911b56b279e4d))
* **console:** match held copies to catalog entries by origin stamp ([#815](https://github.com/geoah/substrate/issues/815)) ([799b52f](https://github.com/geoah/substrate/commit/799b52fe887f817b67ca69f4a3dc955ec6aaea16)), closes [#448](https://github.com/geoah/substrate/issues/448)
* **console:** mount the nuqs adapter inside the router ([#864](https://github.com/geoah/substrate/issues/864)) ([ced0db1](https://github.com/geoah/substrate/commit/ced0db1a9db5176be44a3daa34cd56619a0ee295))
* **console:** save changes to a view without keeping its old filters ([#811](https://github.com/geoah/substrate/issues/811)) ([dbbc106](https://github.com/geoah/substrate/commit/dbbc106acf0224e3f4ab9fe62c1d9217219bf5a0))
* **console:** say how many of a group's rows are on other pages ([#813](https://github.com/geoah/substrate/issues/813)) ([dc356af](https://github.com/geoah/substrate/commit/dc356af99761aafa04486ea13281088f9af6c389)), closes [#679](https://github.com/geoah/substrate/issues/679)
* **deps:** parse `DATABASE_URL` the way libpq does (pgx 5.11) ([a111f19](https://github.com/geoah/substrate/commit/a111f197aba84369ad4a14ee9a4ea3f80b004721))
* **engine:** answer a list by reference from the refs index ([#862](https://github.com/geoah/substrate/issues/862)) ([1901d57](https://github.com/geoah/substrate/commit/1901d5771d47c367053bfb9f6a62d41cbeb56a61))
* **engine:** count and convert a kept kind whose propertytype moves ([#810](https://github.com/geoah/substrate/issues/810)) ([2842b7a](https://github.com/geoah/substrate/commit/2842b7ae1ace1de2ebb7aaa3a03e8c84d7294c9d))
* **engine:** count due schedule fires as pending or in flight ([#866](https://github.com/geoah/substrate/issues/866)) ([f814e7b](https://github.com/geoah/substrate/commit/f814e7b5c3a3f93597fddbf2c87c18361e65e982))
* **engine:** index a parked package's rows under its stored declaration ([#793](https://github.com/geoah/substrate/issues/793)) ([275f572](https://github.com/geoah/substrate/commit/275f572cc51bbc96e20fb04a31c851cbd526dbd3))
* **engine:** log a function body's lines as it writes them ([#861](https://github.com/geoah/substrate/issues/861)) ([ee98bb3](https://github.com/geoah/substrate/commit/ee98bb37cde324e8bf17136c6d41dd1e1297cde1))
* **engine:** pace the changelog digest and pause it while a body runs ([#859](https://github.com/geoah/substrate/issues/859)) ([2c4ca44](https://github.com/geoah/substrate/commit/2c4ca444d004ac5c31c67015528c0b3e49c31c3a))
* **engine:** re-derive a reshaped kind's indexes behind the open ([#858](https://github.com/geoah/substrate/issues/858)) ([f4ebe5f](https://github.com/geoah/substrate/commit/f4ebe5f7deed4de2c92229ddd52785846f50bf88))
* **engine:** report a quarantined package's reason from bundle status ([#851](https://github.com/geoah/substrate/issues/851)) ([cfb654d](https://github.com/geoah/substrate/commit/cfb654dd1632dc443f44897cca6522ec9326cc78))
* **engine:** start a due schedule fire without waiting for the record triggers ([#867](https://github.com/geoah/substrate/issues/867)) ([32918ad](https://github.com/geoah/substrate/commit/32918ad71086218e1601c5b12bb022e959dd93fa))
* **image:** pull a registry ref before image:smoke boots it ([#856](https://github.com/geoah/substrate/issues/856)) ([5aeb8fc](https://github.com/geoah/substrate/commit/5aeb8fcb7bbc6c3e4e600e24627f20a6762f1d0a))
* **llm:** stop the Anthropic client reading the host environment ([#853](https://github.com/geoah/substrate/issues/853)) ([5f8e890](https://github.com/geoah/substrate/commit/5f8e89023d60ac15a6e7b3939aa56e4942901fdf))
* **providers:** bound every syncgmail phase by the invocation clock ([#863](https://github.com/geoah/substrate/issues/863)) ([c299fc9](https://github.com/geoah/substrate/commit/c299fc943b65cac4e9acac74014a0318884c9647))
* **providers:** stop a slow Gmail message from holding an account ([#860](https://github.com/geoah/substrate/issues/860)) ([a644b75](https://github.com/geoah/substrate/commit/a644b756e01b850fa0c7670644979e955d6e6a49))
* **release:** never move the `stable` image tag to an older release ([#855](https://github.com/geoah/substrate/issues/855)) ([d934bf6](https://github.com/geoah/substrate/commit/d934bf6341dcd2bdaf48638ca9d4f18182b15e1a))
* **release:** tag a release only after its own commit passed ci ([#849](https://github.com/geoah/substrate/issues/849)) ([8ed522b](https://github.com/geoah/substrate/commit/8ed522bccd4da955f267149c3ac74b721344687f))
* **slack:** fetch threads with new replies before watch re-checks ([#844](https://github.com/geoah/substrate/issues/844)) ([0240b9a](https://github.com/geoah/substrate/commit/0240b9a142e1406aed816ee472f27803f9d9537b))
* **vocabulary:** refuse core/token in function and agent writes ([1ff1b5e](https://github.com/geoah/substrate/commit/1ff1b5edfcbddec51834d40b42de139ae687ade1))

## [0.112.0](https://github.com/geoah/substrate/compare/v0.111.1...v0.112.0) (2026-09-29)

### ⚠ BREAKING CHANGES

* **vocabulary:** require trigger.source and recordpatchpolicy.action ([#787](https://github.com/geoah/substrate/issues/787)) ([9594691](https://github.com/geoah/substrate/commit/959469138dcf92908c2212de82ce3d01de1791f8))

* **Breaking:** `trigger.source` and `recordpatchpolicy.action` are required, and an older binary cannot open the upgraded core
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

### Fixed

* **vocabulary:** restore a tombstone in the kind's current shape ([#785](https://github.com/geoah/substrate/issues/785)) ([4e76a7c](https://github.com/geoah/substrate/commit/4e76a7cd9955f08a38c0ff94116e1e981fee4b63))
* **catalog:** reuse upgrade previews until the changelog head moves ([#786](https://github.com/geoah/substrate/issues/786)) ([ad8cc84](https://github.com/geoah/substrate/commit/ad8cc8486b553607ab21da77ea43f98014be67e3))
* **vocabulary:** require trigger.source and recordpatchpolicy.action ([#787](https://github.com/geoah/substrate/issues/787)) ([9594691](https://github.com/geoah/substrate/commit/959469138dcf92908c2212de82ce3d01de1791f8))

## [0.111.1](https://github.com/geoah/substrate/compare/v0.111.0...v0.111.1) (2026-09-29)

### Fixed

* **cli:** print apply progress to stderr during a vocabulary batch ([#780](https://github.com/geoah/substrate/issues/780)) ([9d231ee](https://github.com/geoah/substrate/commit/9d231ee2b15e96676eac55cd30e2b97aa9778ec4))
* **runner:** start a python body under a 5s floor, not its timeout ([#781](https://github.com/geoah/substrate/issues/781)) ([25991db](https://github.com/geoah/substrate/commit/25991db1aa789e7a4fdd24eadceab0caab3a33e4))
* **catalog:** offer the next shipped version over a higher stored one ([#667](https://github.com/geoah/substrate/issues/667)) ([6a2a655](https://github.com/geoah/substrate/commit/6a2a655af6ffe1dd10efbdfcfc4aeb16d43c958c))
* **engine:** count only purged rows in a GC pass ([#784](https://github.com/geoah/substrate/issues/784)) ([d62944a](https://github.com/geoah/substrate/commit/d62944ad812aec99965c64c9d5219ff6a9038a70))

## [0.111.0](https://github.com/geoah/substrate/compare/v0.110.6...v0.111.0) (2026-09-29)

### ⚠ BREAKING CHANGES

* **server:** bind 127.0.0.1 and refuse a non-loopback bind ([#778](https://github.com/geoah/substrate/issues/778)) ([513b750](https://github.com/geoah/substrate/commit/513b750985c983b0b6f98b6747f311622994e2ba))

* **Breaking:** The server listens on `127.0.0.1` and refuses a non-loopback bind
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

### Added

* **server:** bind 127.0.0.1 and refuse a non-loopback bind ([#778](https://github.com/geoah/substrate/issues/778)) ([513b750](https://github.com/geoah/substrate/commit/513b750985c983b0b6f98b6747f311622994e2ba))
* **agents:** gate function effects through the policy selector ([#779](https://github.com/geoah/substrate/issues/779)) ([28bd586](https://github.com/geoah/substrate/commit/28bd5861a8fcf707b184492cc2e183e4e8cd4b05))

## [0.110.6](https://github.com/geoah/substrate/compare/v0.110.5...v0.110.6) (2026-09-29)

### Fixed

* **api:** refuse unknown query parameters on a single-record GET ([#776](https://github.com/geoah/substrate/issues/776)) ([46c4d0f](https://github.com/geoah/substrate/commit/46c4d0f64d177df03a41c8764b4dc98afb6e6880))
* **providers:** name Slack's needed scope in a missing_scope refusal ([#777](https://github.com/geoah/substrate/issues/777)) ([e089c07](https://github.com/geoah/substrate/commit/e089c076a6fc8091004ed934d1d8a991396c3017))

## [0.110.5](https://github.com/geoah/substrate/compare/v0.110.4...v0.110.5) (2026-09-29)

### Fixed

* **vocabulary:** compare decimal and money bounds as declared numbers ([#772](https://github.com/geoah/substrate/issues/772)) ([1c60582](https://github.com/geoah/substrate/commit/1c6058259fe926f030793dc0a585ef6259004f0d))
* **vocabulary:** drop a state property as a confirmed lossy null step ([#771](https://github.com/geoah/substrate/issues/771)) ([33b2e39](https://github.com/geoah/substrate/commit/33b2e39ed17db2cdb36476a01727009bbf32bd2a))
* **engine:** erase a purged record's sealed rows and files ([#774](https://github.com/geoah/substrate/issues/774)) ([414e08f](https://github.com/geoah/substrate/commit/414e08f9a8544b9b555cf93d358c61093364584e))
* **server:** send security headers and no-store on credential routes ([#775](https://github.com/geoah/substrate/issues/775)) ([9b5a814](https://github.com/geoah/substrate/commit/9b5a814b3dfe534685560232d9c8be187a3157cd))

## [0.110.4](https://github.com/geoah/substrate/compare/v0.110.3...v0.110.4) (2026-09-29)

### Fixed

* **changelogfile:** verify a line without decoding it again ([#767](https://github.com/geoah/substrate/issues/767)) ([b8e6dfc](https://github.com/geoah/substrate/commit/b8e6dfcce55773cd4a824e606b64baaca890ed6e))
* **engine:** retire older parked fires when a schedule fire settles ([#768](https://github.com/geoah/substrate/issues/768)) ([94f2d7c](https://github.com/geoah/substrate/commit/94f2d7c14448b0c5639375f4a6d5dd0e9a092746))
* **engine:** apply no migration on a read-only open ([#769](https://github.com/geoah/substrate/issues/769)) ([b3ae620](https://github.com/geoah/substrate/commit/b3ae62079baed15572765f7daf08708bd9aa5c5c))
* **engine:** re-derive fts and refs for kinds a boot upgrade reshapes ([#770](https://github.com/geoah/substrate/issues/770)) ([d8195ab](https://github.com/geoah/substrate/commit/d8195abe4eb989c9cde78db31b8f00f09635f077))

## [0.110.3](https://github.com/geoah/substrate/compare/v0.110.2...v0.110.3) (2026-09-29)

### Fixed

* **engine:** re-derive the search index in the background after open ([#765](https://github.com/geoah/substrate/issues/765)) ([be4ee9b](https://github.com/geoah/substrate/commit/be4ee9ba1d0a2a885c36e586dab348c9b1f700bd))
* **engine:** record a paged drain's cursor by hash on each page entry ([#766](https://github.com/geoah/substrate/issues/766)) ([f113400](https://github.com/geoah/substrate/commit/f1134003051867da320ce64017206e46ba593029))

## [0.110.2](https://github.com/geoah/substrate/compare/v0.110.1...v0.110.2) (2026-09-29)

### Fixed

* **engine:** refuse a blob read whose bytes do not hash to the digest ([#763](https://github.com/geoah/substrate/issues/763)) ([06f1892](https://github.com/geoah/substrate/commit/06f1892e6293ee8d1e62f6f867bc10f64065c821))
* **engine:** stop a client disconnect from latching the repository ([#764](https://github.com/geoah/substrate/issues/764)) ([2485563](https://github.com/geoah/substrate/commit/2485563a9924841a9f6f527b687f4774d961e8cd))

## [0.110.1](https://github.com/geoah/substrate/compare/v0.110.0...v0.110.1) (2026-09-29)

### Fixed

* **engine:** reuse the boot check's segment digests at first open ([#762](https://github.com/geoah/substrate/issues/762)) ([63ebba8](https://github.com/geoah/substrate/commit/63ebba87472da6640871714c98845ce0708b11b9))

## [0.110.0](https://github.com/geoah/substrate/compare/v0.109.0...v0.110.0) (2026-09-29)

### Added

* **server:** add SUBSTRATE_TRIGGER_INTERVAL for the dispatcher tick ([#758](https://github.com/geoah/substrate/issues/758)) ([8dd2ad7](https://github.com/geoah/substrate/commit/8dd2ad77e0107d373df89602e45208c27379bd2b))

## [0.109.0](https://github.com/geoah/substrate/compare/v0.108.0...v0.109.0) (2026-09-28)

### Added

* **vocabulary:** add a money property type ([#751](https://github.com/geoah/substrate/issues/751)) ([cf45a78](https://github.com/geoah/substrate/commit/cf45a786612942cac539fa32eb5766f8f3321df5))

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

## [0.106.0](https://github.com/geoah/substrate/compare/v0.105.0...v0.106.0) (2026-09-27)

### Added

* **skills:** add the substrate-runbook-upgrade agent skill ([#722](https://github.com/geoah/substrate/issues/722)) ([f2dad01](https://github.com/geoah/substrate/commit/f2dad0177581a32302707779a3382bd66cce04e0))

## [0.105.0](https://github.com/geoah/substrate/compare/v0.104.3...v0.105.0) (2026-09-27)

### ⚠ BREAKING CHANGES

* **console:** redesign the console around your data, providers, agents and tools ([#648](https://github.com/geoah/substrate/issues/648)) ([9f7d300](https://github.com/geoah/substrate/commit/9f7d3006a4f071387db3a11bad3844172f511dd7))

* **Breaking:** A kind declares its purpose, and an older binary cannot read one that does
  1. Operators: upgrade every server that opens a repository before any of
     them boots this release, and do not roll back past it afterwards.
  2. Authors: add `purpose: supporting` to a kind a person would not browse
     as its own collection (a sync state, a cursor, a join row), and
     `purpose: internal` to machinery. Leave the key off a kind that is a
     thing a person keeps.
  3. Accept the upgrades offered for shipped providers and samples.

### Added

* **console:** redesign the console around your data, providers, agents and tools ([#648](https://github.com/geoah/substrate/issues/648)) ([9f7d300](https://github.com/geoah/substrate/commit/9f7d3006a4f071387db3a11bad3844172f511dd7))

## [0.104.3](https://github.com/geoah/substrate/compare/v0.104.2...v0.104.3) (2026-09-27)

### Fixed

* **slack:** read a day behind the history cursor to find first replies ([#717](https://github.com/geoah/substrate/issues/717)) ([1f94311](https://github.com/geoah/substrate/commit/1f943116f8c849ee9bf8d13a1f4605a2c0a86bb3))

## [0.104.2](https://github.com/geoah/substrate/compare/v0.104.1...v0.104.2) (2026-09-27)

### Fixed

* **github:** find pull requests the owner reviewed ([#715](https://github.com/geoah/substrate/issues/715)) ([956cf6a](https://github.com/geoah/substrate/commit/956cf6a56aef374deb3532e6ec00e26d120d0077))

## [0.104.1](https://github.com/geoah/substrate/compare/v0.104.0...v0.104.1) (2026-09-27)

### Fixed

* **vocabulary:** refuse an enum argument outside its values ([#714](https://github.com/geoah/substrate/issues/714)) ([b8bebd4](https://github.com/geoah/substrate/commit/b8bebd4762489b10ef75646d9e7819cf5a24c7c6))

## [0.104.0](https://github.com/geoah/substrate/compare/v0.103.0...v0.104.0) (2026-09-27)

### Added

* **engine:** let a function body run an agent it grants ([#692](https://github.com/geoah/substrate/issues/692)) ([e4a1155](https://github.com/geoah/substrate/commit/e4a1155b337999ef8a6f8524a1e122e55fee3b36))

## [0.103.0](https://github.com/geoah/substrate/compare/v0.102.0...v0.103.0) (2026-09-27)

### Added

* **engine:** write a triggerrun row per networked direct call ([#688](https://github.com/geoah/substrate/issues/688)) ([eb8e8d9](https://github.com/geoah/substrate/commit/eb8e8d9d156c898719d6345e13a3ef4fe04d2da8))
* **api:** return run summaries from the changes read with runs=1 ([#704](https://github.com/geoah/substrate/issues/704)) ([7de679a](https://github.com/geoah/substrate/commit/7de679a9eabd8266590ea1e629f4f4397b3105ac))
* **engine:** collect a record on `DELETE ?purge=true` ([#695](https://github.com/geoah/substrate/issues/695)) ([0087921](https://github.com/geoah/substrate/commit/0087921f7b041b8ec9a95b07103a55008d193d6c))

### Fixed

* **engine:** store a mapped mirror reference as its subject, once ([#690](https://github.com/geoah/substrate/issues/690)) ([f606839](https://github.com/geoah/substrate/commit/f606839eb6f21edd5b04aa34cc078bc32db72a6f))
* **engine:** read only a record trigger's kinds from the changelog ([#699](https://github.com/geoah/substrate/issues/699)) ([98e93cc](https://github.com/geoah/substrate/commit/98e93ccd979a46d16e22dac785d341c5b601d175))

## [0.102.0](https://github.com/geoah/substrate/compare/v0.101.0...v0.102.0) (2026-09-27)

### Added

* **providers:** ship Slack, Beeper and GitHub write functions ([#703](https://github.com/geoah/substrate/issues/703)) ([e942967](https://github.com/geoah/substrate/commit/e942967a1cb5fc1c4e62f4eb7faa8085b4393e7b))
* **engine:** project only the sources a recordmapping's where covers ([#687](https://github.com/geoah/substrate/issues/687)) ([0d0e049](https://github.com/geoah/substrate/commit/0d0e049245d13be15ffadfa5ac32a3ab2108f50a))

## [0.101.0](https://github.com/geoah/substrate/compare/v0.100.0...v0.101.0) (2026-09-27)

### ⚠ BREAKING CHANGES

* **engine:** compute occurrences in function and agent window lists ([#689](https://github.com/geoah/substrate/issues/689)) ([f4b9589](https://github.com/geoah/substrate/commit/f4b958972992e1f424bff3747f99ee2d00c7dda9))

* **Breaking:** A function's `host.records.list` and an agent's `query` bounded on `at` compute occurrences
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

### Added

* **engine:** pass a schedule trigger's arguments to its function ([#691](https://github.com/geoah/substrate/issues/691)) ([d3b7388](https://github.com/geoah/substrate/commit/d3b73880803c17b96b36cfc4c329b48051a71e1a))
* **engine:** match a probe in any case with fold: case ([#694](https://github.com/geoah/substrate/issues/694)) ([286ef71](https://github.com/geoah/substrate/commit/286ef71ec0c203c4edb3c5d7ed353a5e79cd89d5))
* **engine:** compute occurrences in function and agent window lists ([#689](https://github.com/geoah/substrate/issues/689)) ([f4b9589](https://github.com/geoah/substrate/commit/f4b958972992e1f424bff3747f99ee2d00c7dda9))
* **vocabulary:** let a kind declare its display label ([#707](https://github.com/geoah/substrate/issues/707)) ([567611e](https://github.com/geoah/substrate/commit/567611ef06f311acdf45c40ea227d24d8bd7ea90))

### Fixed

* **engine:** release an actor's holds when it is declared machine ([#696](https://github.com/geoah/substrate/issues/696)) ([871628c](https://github.com/geoah/substrate/commit/871628cbabf91e5319f9856112eda4afd32e1100))
* **providers:** fire sync now until the request is acknowledged ([#713](https://github.com/geoah/substrate/issues/713)) ([39d33ba](https://github.com/geoah/substrate/commit/39d33ba568777d784514d47627cdfdda2006239c))

## [0.100.0](https://github.com/geoah/substrate/compare/v0.99.0...v0.100.0) (2026-09-26)

### Added

* **engine:** let the owner adjust a change request on accept ([#706](https://github.com/geoah/substrate/issues/706)) ([85b3022](https://github.com/geoah/substrate/commit/85b30227f329c5ab802b31f814aba7166f3aeae2))

## [0.99.0](https://github.com/geoah/substrate/compare/v0.98.0...v0.99.0) (2026-09-26)

### Added

* **engine:** record the actor that first declares a package ([#705](https://github.com/geoah/substrate/issues/705)) ([0dc7bf3](https://github.com/geoah/substrate/commit/0dc7bf3d008d6d59f9f5815865bf1b41da734732))

## [0.98.0](https://github.com/geoah/substrate/compare/v0.97.1...v0.98.0) (2026-09-26)

### Added

* **engine:** let an allow policy lift the gate it names ([#702](https://github.com/geoah/substrate/issues/702)) ([e6f4695](https://github.com/geoah/substrate/commit/e6f46954b8dc7c4c7a7e2a0496237003590838ad))

## [0.97.1](https://github.com/geoah/substrate/compare/v0.97.0...v0.97.1) (2026-09-26)

### Fixed

* **engine:** link existing sources when a mapping is applied ([#697](https://github.com/geoah/substrate/issues/697)) ([f333f35](https://github.com/geoah/substrate/commit/f333f350f83557d1c8a63c978b4c221684326bff))

## [0.97.0](https://github.com/geoah/substrate/compare/v0.96.5...v0.97.0) (2026-09-26)

### Added

* **vocabulary:** let apply hold back mappings with no provider ([#693](https://github.com/geoah/substrate/issues/693)) ([9e93855](https://github.com/geoah/substrate/commit/9e938558af5ea86a401a3b7b4cb3a1cdbcf0b916))

### Fixed

* **engine:** render state, instant and body tokens in a displayTemplate ([#683](https://github.com/geoah/substrate/issues/683)) ([11bc7c0](https://github.com/geoah/substrate/commit/11bc7c05f33b351b9de3c953ccab68937bf9645c))
* **dev:** wait out a slow boot import instead of killing it ([#698](https://github.com/geoah/substrate/issues/698)) ([dc8220b](https://github.com/geoah/substrate/commit/dc8220bfed0bc03491fd84591328fa6a607c6ff7))

## [0.96.5](https://github.com/geoah/substrate/compare/v0.96.4...v0.96.5) (2026-09-26)

### Fixed

* **engine:** share one capped Postgres pool across all repositories ([#664](https://github.com/geoah/substrate/issues/664)) ([8b77be3](https://github.com/geoah/substrate/commit/8b77be3c3ab3cee94b2ba35ac604d0a753548d1d))

## [0.96.4](https://github.com/geoah/substrate/compare/v0.96.3...v0.96.4) (2026-09-26)

### Fixed

* **engine:** confirm a lossy apply while unrelated records are written ([#665](https://github.com/geoah/substrate/issues/665)) ([87f0c12](https://github.com/geoah/substrate/commit/87f0c12abb9f71a2483e3d771f40ccf718a55bd4))

## [0.96.3](https://github.com/geoah/substrate/compare/v0.96.2...v0.96.3) (2026-09-26)

### Fixed

* **catalog:** keep versions when an unchanged closure is reinstalled ([#666](https://github.com/geoah/substrate/issues/666)) ([0fa83f1](https://github.com/geoah/substrate/commit/0fa83f13550e6eb0fd221137133f15ca34d2060f))

## [0.96.2](https://github.com/geoah/substrate/compare/v0.96.1...v0.96.2) (2026-09-26)

### Fixed

* **engine:** dispatch each repository's triggers in its own lane ([#662](https://github.com/geoah/substrate/issues/662)) ([cebc1f4](https://github.com/geoah/substrate/commit/cebc1f4835d1d0bd8627839f77e66dd18f100e87))
* **engine:** cap each trigger's share of a dispatcher pass ([#663](https://github.com/geoah/substrate/issues/663)) ([3978cf7](https://github.com/geoah/substrate/commit/3978cf78d209935e277b94316fee23f50f7f73e6))

## [0.96.1](https://github.com/geoah/substrate/compare/v0.96.0...v0.96.1) (2026-09-26)

### Fixed

* **image:** ship the static upstream uv instead of Alpine's ([#660](https://github.com/geoah/substrate/issues/660)) ([c5f3b5a](https://github.com/geoah/substrate/commit/c5f3b5aa51baa2544d587d6a18b7d2739eea81db))
* **engine:** answer not found for a patch onto a tombstone ([#661](https://github.com/geoah/substrate/issues/661)) ([a6cde0b](https://github.com/geoah/substrate/commit/a6cde0b461dc97f157be4c2b9b8ee3f611575a72))

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

### ⚠ BREAKING CHANGES

* **Breaking:** A retry of a parked delivery that fails again answers `409` `parked`, not `500`
  1. Treat `409` `parked` from the retry route as "the delivery still fails":
     fix the callable, then retry or forget the row
     (`DELETE …/trigger/{id}/parked/{fid}`).
  2. Stop treating a `500` from this route as the failed-again outcome; a
     `500` now means a server fault.

### Added

* **metrics:** prometheus exposition at /metrics behind SUBSTRATE_METRICS ([#630](https://github.com/geoah/substrate/issues/630)) ([b90a07f](https://github.com/geoah/substrate/commit/b90a07f98bdfa8826ddc136d1294508ae4563457))

### Fixed

* **engine:** bind kind/id/actor lists as text[] and filter with = ANY instead of a jsonb_array_elements_text semi-join ([#596](https://github.com/geoah/substrate/issues/596)) ([6e5ee3a](https://github.com/geoah/substrate/commit/6e5ee3a74ee6f6eb36f647965c93bf858b2f7f1a))
* **engine:** a retry of a parked delivery that fails again answers 409 parked, not 500 ([#629](https://github.com/geoah/substrate/issues/629)) ([8b7043e](https://github.com/geoah/substrate/commit/8b7043ea989fddc856784fccda38c8e12566427c))

## [0.93.1](https://github.com/geoah/substrate/compare/v0.93.0...v0.93.1) (2026-09-24)

### Fixed

* **google:** a split series keeps one row per occurrence ([#626](https://github.com/geoah/substrate/issues/626)) ([970e7ae](https://github.com/geoah/substrate/commit/970e7ae92b01cf467136121590f80b80f3d76260))

## [0.93.0](https://github.com/geoah/substrate/compare/v0.92.1...v0.93.0) (2026-09-24)

### ⚠ BREAKING CHANGES

* **oauth:** refuse a bare account id on oauth/start ([0c02ddd](https://github.com/geoah/substrate/commit/0c02ddddbc7a9ab0f01162d7386280b3734cd22f))

* **Breaking:** `oauth/start` refuses a bare account id
  1. Send `record` as the full path:
     `{"record": "providers.substrate.reamde.dev/google/account/owner"}`.
  2. Pass the full path to `substratectl bundle connect`:
     `substratectl bundle connect providers.substrate.reamde.dev/google/account/owner`.
  3. If you listen for the `substrate-oauth` message or read `?connected=`,
     expect the full path there.
  4. Restart any consent that was in progress during the upgrade.

### Added

* **oauth:** refuse a bare account id on oauth/start ([0c02ddd](https://github.com/geoah/substrate/commit/0c02ddddbc7a9ab0f01162d7386280b3734cd22f))

## [0.92.1](https://github.com/geoah/substrate/compare/v0.92.0...v0.92.1) (2026-09-24)

### Fixed

* **console:** resolve a reference pin by its full identity only ([6dd5368](https://github.com/geoah/substrate/commit/6dd5368f82861c69a1ec9ed3b4816316c83a18e9))
* **engine:** qualify bare kinds in stored trigger and policy selectors ([7290771](https://github.com/geoah/substrate/commit/7290771605b1bd764b947022b3aa89b6c0603db3))

## [0.92.0](https://github.com/geoah/substrate/compare/v0.91.0...v0.92.0) (2026-09-24)

### ⚠ BREAKING CHANGES

* refuse a bare kind, trait or callable name on every surface ([1ae09f5](https://github.com/geoah/substrate/commit/1ae09f5c3c1b9c789c67944250363995f2eece7e))

* **Breaking:** A bare kind, trait, function or agent name is refused on every surface
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

### Added

* refuse a bare kind, trait or callable name on every surface ([1ae09f5](https://github.com/geoah/substrate/commit/1ae09f5c3c1b9c789c67944250363995f2eece7e))

## [0.91.0](https://github.com/geoah/substrate/compare/v0.90.0...v0.91.0) (2026-09-24)

### ⚠ BREAKING CHANGES

* **vocabulary:** refuse a bare kind or trait name in a declaration ([e3af50b](https://github.com/geoah/substrate/commit/e3af50b41c1eff59a4a615a87c334777d7612446))

* **Breaking:** A declaration naming a kind or trait by a bare word is refused
  1. Operators: nothing. The migration runs at boot.
  2. Authors: in every document you apply, replace each bare word in `kind:`,
     `trait:`, `traits:`, `writes` and `reads.kinds` with the full spelling the
     refusal lists. `substratectl kinds` prints every installed kind.
  3. Accept the upgrades offered for shipped providers and samples; their
     package versions were bumped for this change.

### Added

* **console:** filter a reference by picking its referents ([#612](https://github.com/geoah/substrate/issues/612)) ([10ee9b5](https://github.com/geoah/substrate/commit/10ee9b561b50183e373ddb4594b3f9466119b7f3))
* **vocabulary:** refuse a bare kind or trait name in a declaration ([e3af50b](https://github.com/geoah/substrate/commit/e3af50b41c1eff59a4a615a87c334777d7612446))

### Fixed

* **console:** list every problem of a refused import on its own line ([59d0913](https://github.com/geoah/substrate/commit/59d09137f9bf9c53952dc82fdb845f6eaadd3158))

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

### ⚠ BREAKING CHANGES

* **Breaking:** Six provider bundles are replaced with reshaped versions that rename kinds and functions
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

### Added

* **providers:** ship the seven mneme-tested bundles and their e2e suite ([#606](https://github.com/geoah/substrate/issues/606)) ([862094e](https://github.com/geoah/substrate/commit/862094e6e4e58dee3b8603314786d499a2e8f583))

## [0.87.0](https://github.com/geoah/substrate/compare/v0.86.0...v0.87.0) (2026-09-22)

### Added

* **console:** nest a kind's records under their parent reference ([53758d3](https://github.com/geoah/substrate/commit/53758d308703b889e1803cfe0bfef85f1907bfd1))

### Fixed

* **release:** create /var/lib/substrate and /keys in the published image ([#602](https://github.com/geoah/substrate/issues/602)) ([45f6028](https://github.com/geoah/substrate/commit/45f6028d466dfd0629519bef340f579d2fea4d61))
* **engine:** a put that resurrects a tombstone may name its state ([#604](https://github.com/geoah/substrate/issues/604)) ([9a91b44](https://github.com/geoah/substrate/commit/9a91b447e0818c9c7cd211991e44cd537786f051))

## [0.86.0](https://github.com/geoah/substrate/compare/v0.85.0...v0.86.0) (2026-09-22)

### ⚠ BREAKING CHANGES

* **webhooks:** a webhook trigger declares the headers its callable reads ([#601](https://github.com/geoah/substrate/issues/601)) ([c342277](https://github.com/geoah/substrate/commit/c342277f63c0a44d51c08e1e9a72cb004c2d4aa0))

* **Breaking:** A webhook fire carries only the headers its trigger lists in `source.webhook.headers`
  1. List the triggers: `substratectl get substrate.reamde.dev/core/trigger -o yaml`.
  2. For each record with `source.webhook`, find the headers its callable reads
     and add them under `source.webhook.headers`.
  3. Apply each edited record with `substratectl apply -f <file>`.
  4. For a trigger shipped by a sample (the Pebble sample declares its own
     eight names), re-importing the sample writes the list, but it discards a
     hand-set `key`, so set `key` again afterwards.

### Added

* **webhooks:** keep the Pebble Index app's headers; the sample reads its real contract ([#599](https://github.com/geoah/substrate/issues/599)) ([659b059](https://github.com/geoah/substrate/commit/659b059547cd8d5062d7cf0ab6bce46efde0c0b8))
* **webhooks:** a webhook trigger declares the headers its callable reads ([#601](https://github.com/geoah/substrate/issues/601)) ([c342277](https://github.com/geoah/substrate/commit/c342277f63c0a44d51c08e1e9a72cb004c2d4aa0))

## [0.85.0](https://github.com/geoah/substrate/compare/v0.84.0...v0.85.0) (2026-09-19)

### ⚠ BREAKING CHANGES

* **Breaking:** Provider mirror kinds stop declaring subject slots; the `recordmapping` adds them
  1. Read the hidden-address assignee from `assigneeUser` on `linear/issue`.
     v0.88.0 reshapes the Linear bundle again (see
     its note).
  2. Authors: stop declaring a `subject: true` reference on a source kind; the
     mapping adds it. A source kind that still declares one keeps working.
  3. To remove a mapping, delete the records linking through it first.

* **Breaking:** A mapping source with several candidates, or nothing to offer, is left unlinked
  1. Treat an empty subject slot on a mapping source as a normal state, not an
     error.
  2. On v0.95.0 or later, list waiting sources with `--ambiguous` and merge the
     candidates they name.
  3. To keep the old behavior for one mapping, set `onAmbiguous: mint` on it
     (v0.95.0 or later).

### Added

* **console:** a record's Provenance tab groups its sources by mapping and lets the owner pick a value, and propertyMeta names the source record behind each manager and alternative (record 0094) ([#594](https://github.com/geoah/substrate/issues/594)) ([c7479ea](https://github.com/geoah/substrate/commit/c7479eabfa5bf034433075b4288879254abbca69))

## [0.84.0](https://github.com/geoah/substrate/compare/v0.83.0...v0.84.0) (2026-09-19)

### ⚠ BREAKING CHANGES

* **Breaking:** The ranked read `q` parses the search grammar and refuses a query with no word
  1. Stop sending a `q` that is only stars, quotes or dashes; treat a `422`
     there as an empty query.
  2. Expect `lay*` to match words starting with `lay`, where the star used to
     be ignored.
  3. Quote a phrase you meant literally; other punctuation no longer splits a
     word.

### Added

* **search:** one search grammar filters, matches and ranks, with a Search page and a table search box ([#590](https://github.com/geoah/substrate/issues/590)) ([fbd2688](https://github.com/geoah/substrate/commit/fbd26882475a31c7e92b19e2fb458eb0770bba13))
* **api:** a records list pages by offset, and the console browse numbers its pages ([#591](https://github.com/geoah/substrate/issues/591)) ([36974a5](https://github.com/geoah/substrate/commit/36974a57d7349e4966c0085a0131581d5f7a60d7))
* **sync:** a core sync trait the dispatcher stamps, a sync status read, and the console's Connections page ([#592](https://github.com/geoah/substrate/issues/592)) ([fe3cfec](https://github.com/geoah/substrate/commit/fe3cfec1a17e3d92bd21a2996c56b032173dea4a))

## [0.83.0](https://github.com/geoah/substrate/compare/v0.82.0...v0.83.0) (2026-09-18)

### Added

* **console:** revamp record views and persist navigation preferences ([#589](https://github.com/geoah/substrate/issues/589)) ([279ffcc](https://github.com/geoah/substrate/commit/279ffcc838426f56e12a65f6c33a8cba7401a2b0))

## [0.82.0](https://github.com/geoah/substrate/compare/v0.81.0...v0.82.0) (2026-09-15)

### ⚠ BREAKING CHANGES

* **dev:** the dev database is per tree, and a second writer of a repository is refused at open ([#567](https://github.com/geoah/substrate/issues/567)) ([f2213cb](https://github.com/geoah/substrate/commit/f2213cbddc6b7086bac3a361b80a18bfb9769f44))

* **Breaking:** A second server on one database does not boot: each repository has one writer lease
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

### Added

* **dev:** the dev database is per tree, and a second writer of a repository is refused at open ([#567](https://github.com/geoah/substrate/issues/567)) ([f2213cb](https://github.com/geoah/substrate/commit/f2213cbddc6b7086bac3a361b80a18bfb9769f44))

## [0.81.0](https://github.com/geoah/substrate/compare/v0.80.0...v0.81.0) (2026-09-15)

### ⚠ BREAKING CHANGES

* **engine:** a dangling mustExist reference is a 422 validation problem naming the property ([#566](https://github.com/geoah/substrate/issues/566)) ([204d0fe](https://github.com/geoah/substrate/commit/204d0fe09707486c983874a88991a344e9d31288))

* **Breaking:** A write naming an absent `mustExist` referent answers `422 validation`, not `404`
  1. Handle a dangling referent under `422`: read `problemDetails[].path` to
     find the property, then fix the value or create the referent first.
  2. Keep `404` for "the record at this path does not exist".

### Fixed

* **engine:** a dangling mustExist reference is a 422 validation problem naming the property ([#566](https://github.com/geoah/substrate/issues/566)) ([204d0fe](https://github.com/geoah/substrate/commit/204d0fe09707486c983874a88991a344e9d31288))

## [0.80.0](https://github.com/geoah/substrate/compare/v0.79.1...v0.80.0) (2026-09-15)

### ⚠ BREAKING CHANGES

* **engine:** a failed accept of a patch request answers 409 conflict with the reason, never "version conflict" ([#564](https://github.com/geoah/substrate/issues/564)) ([5bf4861](https://github.com/geoah/substrate/commit/5bf4861389d297d9a47d0096cd31a8c9ffc18509))

* **Breaking:** A failed accept of a `recordpatchrequest` answers `409 conflict` naming its reason
  1. Treat `409 conflict` on an accept as "the change no longer applies":
     read the reason from `message` or from the request's conflict
     annotation, then reject or re-propose. Do not retry the same accept.
  2. Keep treating a `409` whose message says "version conflict" as a stale
     `ifVersion`: re-read the request and retry.

### Added

* **engine:** the judge is told its reply contract, reads a fenced verdict, and may expand the diff's referents ([#561](https://github.com/geoah/substrate/issues/561)) ([e3695cd](https://github.com/geoah/substrate/commit/e3695cd2ac194eff976e35b268c60fab169896e4))
* **vocabulary:** triggerrun carries the callable as a reference, and callable's description says what it stores ([#565](https://github.com/geoah/substrate/issues/565)) ([5fb5d10](https://github.com/geoah/substrate/commit/5fb5d10b297d9fed81b7a430696ebfc97a87c114))

### Fixed

* **engine:** a failed accept of a patch request answers 409 conflict with the reason, never "version conflict" ([#564](https://github.com/geoah/substrate/issues/564)) ([5bf4861](https://github.com/geoah/substrate/commit/5bf4861389d297d9a47d0096cd31a8c9ffc18509))
* **runner:** function bodies trust the system certificate bundle when the interpreter ships none ([#562](https://github.com/geoah/substrate/issues/562)) ([ddb6c2e](https://github.com/geoah/substrate/commit/ddb6c2e7ce77402719bf3c057d7abe8642914d5f))
* **engine:** a state property added to a kind with live records backfills its initial state at admission ([#563](https://github.com/geoah/substrate/issues/563)) ([2e4f48d](https://github.com/geoah/substrate/commit/2e4f48da21258a4ae6a59a8eeb24cd422b11286e))

## [0.79.1](https://github.com/geoah/substrate/compare/v0.79.0...v0.79.1) (2026-09-15)

### Fixed

* **cli:** apply renders every problem a 422 carries, details first ([#558](https://github.com/geoah/substrate/issues/558)) ([6c0238f](https://github.com/geoah/substrate/commit/6c0238fb6af40c6bcd7aa4d22c90224b83202d42))
* **runner:** the SDK's list accepts the REST string forms of orderBy ([#559](https://github.com/geoah/substrate/issues/559)) ([f24d12c](https://github.com/geoah/substrate/commit/f24d12c2ccf50592242e5459c6fdb96a9d92d0ca))
* **engine:** a refused label or annotation key names the rule it broke, and the docs say who may write which namespace ([#560](https://github.com/geoah/substrate/issues/560)) ([5877867](https://github.com/geoah/substrate/commit/5877867bb23d84843fb75cc8cb3c988efe28d8b6))

## [0.79.0](https://github.com/geoah/substrate/compare/v0.78.0...v0.79.0) (2026-09-14)

### ⚠ BREAKING CHANGES

* recurrence is core, the window read computes occurrences, and the Google mirror copies masters and exceptions ([#544](https://github.com/geoah/substrate/issues/544)) ([e31a5ac](https://github.com/geoah/substrate/commit/e31a5ac8a25d0a0fe7a5d4513601828fa6bc1452))

* **Breaking:** Mirror Google Calendar recurring events as one series row plus exceptions
  1. Read calendar ranges with both bounds on `at` and both kinds in
     `filter.kinds`, as above.
  2. Follow an exception to its series through its `recurrenceOf` reference.
  3. Nothing for stored data: the first sync after the upgrade converts it.

* **Breaking:** Remove `GET /api/v1/occurrences`; a two-sided `at` filter computes occurrences
  1. Replace calls to `/api/v1/occurrences` with the window read above, and
     page it with `first` and `after` instead of `limit`.
  2. Treat a row with `computed: true` as read-only unless its kind binds
     `override`; then a `PUT` at its id materializes that one occurrence.
  3. In your own kind declarations, bind `substrate.reamde.dev/core/recurring`
     instead of the scheduling sample's `recurring`. A bare trait name is
     refused in declarations from v0.91.0 (decision record 0098).

### Added

* recurrence is core, the window read computes occurrences, and the Google mirror copies masters and exceptions ([#544](https://github.com/geoah/substrate/issues/544)) ([e31a5ac](https://github.com/geoah/substrate/commit/e31a5ac8a25d0a0fe7a5d4513601828fa6bc1452))

### Fixed

* **engine:** a write appends the rows another process committed instead of latching until restart ([#542](https://github.com/geoah/substrate/issues/542)) ([cbaae3f](https://github.com/geoah/substrate/commit/cbaae3f0e3d0e768bd6c7381b44e257fab550875))
* **engine:** a snippet cuts on a rune boundary, and text no row stores is refused as validation ([#543](https://github.com/geoah/substrate/issues/543)) ([ea0dde1](https://github.com/geoah/substrate/commit/ea0dde138a560355d62694a2a0ae946800fd0fb3))

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

* **Breaking:** Remove `POST /api/v1/graphql` and read every list at `GET /api/v1/records`
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

* **Breaking:** Move the four `core/llm*` kinds to the seeded `substrate.reamde.dev/llm` package
  1. Nothing for stored data: the boot upgrade moves the rows and references.
  2. Replace the four old references in client code, saved filters and
     `apply -f` documents, including `provider:` references in agent
     manifests kept outside the server.
  3. Re-bookmark console pages for the old kinds.

* **Breaking:** Write trigger delivery rows as `substrate.reamde.dev/core/triggerrun`
  1. Replace `substrate.reamde.dev/core/run` with
     `substrate.reamde.dev/core/triggerrun` wherever a client lists or watches
     delivery attempts.
  2. To read attempts from before the upgrade, list the old kind as well; the
     engine no longer writes or prunes it.

* **Breaking:** Unset `SUBSTRATE_INVITE_CODE` opens registration instead of closing it
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

* **Breaking:** Rename the `web` sample to `samples.substrate.reamde.dev/readinglist`
  1. If you never imported `web`, nothing.
  2. If you did and want the maintained version, import `readinglist` and
     write the deny list into its `denyDomains` setting. The two packages have
     different kind references, so records of `<authority>/web/page` are not
     carried to `<authority>/readinglist/page`.

### Added

* the registry shows what a bundle adds, the llm kinds get their own package, and provider installs work in compose ([830f460](https://github.com/geoah/substrate/commit/830f460e4be52450f2ea281f936035047bc9d2c4))
* remove GraphQL and fold every record read into GET /api/v1/records ([#531](https://github.com/geoah/substrate/issues/531)) ([0c152b7](https://github.com/geoah/substrate/commit/0c152b7c54fb1ca1940046c9ea6786c8e3ef5480))

## [0.75.0](https://github.com/geoah/substrate/compare/v0.74.0...v0.75.0) (2026-09-10)

### ⚠ BREAKING CHANGES

* **runner:** remove the go function runtime ([121b4e9](https://github.com/geoah/substrate/commit/121b4e942f59575e405033ae09459c813dad1b1a))
* **samples:** remove the seven sample packages nothing imports ([f47c21f](https://github.com/geoah/substrate/commit/f47c21f5930406398536548ac30c85df2544549f))

* **Breaking:** The `go` function runtime is removed and `runtime: go` is refused
  1. Rewrite each Go body in Python. The entrypoint is `main(input, host)`;
     [functions](docs/functions.md) documents the `host` object.
  2. Set `runtime: python` on the declaration and apply it.
  3. Rewrite every `.go` entry under a bundle's `modules` as a `.py` module.
  4. Do steps 1 to 3 before the upgrade. If no declaration used `runtime: go`,
     there is nothing to do.

* **Breaking:** The catalog stops shipping seven sample packages
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

* **Breaking:** The `s3` blob backend and every `SUBSTRATE_BLOB_*` variable are removed
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

* **Breaking:** `substratectl edit`, `user password`, `user totp` and `POST /api/v1/embeddings/reembed` are removed
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

* **Breaking:** `POST /recovery/enroll` is removed and `upgrade.renames` leaves the wire
  1. Read renames from `upgrade.steps`, keeping the entries whose `step` is
     `rename`. `kind`, `from`, `to` and `records` hold the values `renames`
     held.
  2. Remove any call to `POST /recovery/enroll` or
     `substratectl recovery enroll`. There is no replacement.

## [0.70.0](https://github.com/geoah/substrate/compare/v0.69.0...v0.70.0) (2026-09-09)

### ⚠ BREAKING CHANGES

* **engine:** squash the migrations into one initial schema ([0b98b51](https://github.com/geoah/substrate/commit/0b98b516af369f54bf8959fc60928dda6528eeff))

* **Breaking:** The server refuses every database migrated before v0.70.0
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

## Before v0.70.0

A database migrated before v0.70.0 cannot be upgraded in place (the v0.70.0
entry above moves the data root onto an empty database instead), so the
releases from v0.1.0 to v0.69.0 are not listed here. Each is on the
[releases page](https://github.com/geoah/substrate/releases) with its commits.
