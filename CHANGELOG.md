# Changelog

## [0.113.0](https://github.com/geoah/substrate/compare/v0.112.0...v0.113.0) (2026-09-29)


### ⚠ BREAKING CHANGES

* **vocabulary:** a function or agent whose `permissions.writes` names `substrate.reamde.dev/core/token`, `substrate.reamde.dev/core/credential` or `substrate.reamde.dev/core/recoverykey` is refused at `POST /api/v1/vocabulary/apply`, catalog import and install with `422`, and one already stored quarantines its package at the next open; `substratectl bundle status <id>` prints the reason. Delete the entry from `permissions.writes`, then apply the package's corrected documents, the whole package, with `substratectl apply -f`: a closure that admits clears the quarantine. Installed code no longer revokes a token: a function's `delete` effect, an agent's `write` and a policy judge's accept of a proposed token delete answer `403`, and the owner revokes with `substratectl token revoke <id>` or from the console. Globs never reached these kinds, so `writes: ["*"]` is unaffected, and no shipped bundle or sample names them.

### Added

* **agents:** count the child chain's writes on sub-agent tool rows ([#804](https://github.com/geoah/substrate/issues/804)) ([2433223](https://github.com/geoah/substrate/commit/2433223cb78942b685e5f6d5d7222a85488d62ee)), closes [#70](https://github.com/geoah/substrate/issues/70)
* **agents:** stamp the callable identity on llm/message tool rows ([#790](https://github.com/geoah/substrate/issues/790)) ([b614980](https://github.com/geoah/substrate/commit/b61498027966effdc9827cca0e68bf2965c02656))
* **api:** accept a list of targets in filter.referencing.refs ([#794](https://github.com/geoah/substrate/issues/794)) ([1fcef76](https://github.com/geoah/substrate/commit/1fcef7692412282c812eb2e67cec40d0e8c8aa1f))
* **console:** edit what an agent can see and change in the chat panel ([#814](https://github.com/geoah/substrate/issues/814)) ([1750fc2](https://github.com/geoah/substrate/commit/1750fc2f0b1b8bef4ed5ecef4fcf2622733df8c6))
* **console:** show an agent's spend by day, week and month ([#816](https://github.com/geoah/substrate/issues/816)) ([c00cc45](https://github.com/geoah/substrate/commit/c00cc451d1a81bea3a1e16c581729885495be613))
* **vocabulary:** store a field's default in each object a write sends ([#791](https://github.com/geoah/substrate/issues/791)) ([8eb7dcf](https://github.com/geoah/substrate/commit/8eb7dcf784ac9084f7e3df4c9457fba20e846860)), closes [#248](https://github.com/geoah/substrate/issues/248)
* **vocabulary:** title a trigger from its optional `label` ([#789](https://github.com/geoah/substrate/issues/789)) ([8c93530](https://github.com/geoah/substrate/commit/8c93530b3bf0a8ceaa6dc21734ffb31d97bc5dce)), closes [#118](https://github.com/geoah/substrate/issues/118)


### Fixed

* **console:** ask the last-chatted agent that can declare a kind ([#805](https://github.com/geoah/substrate/issues/805)) ([8a3887e](https://github.com/geoah/substrate/commit/8a3887e0a00effe55204405dd498c846ffe18fc9))
* **console:** match held copies to catalog entries by origin stamp ([#815](https://github.com/geoah/substrate/issues/815)) ([799b52f](https://github.com/geoah/substrate/commit/799b52fe887f817b67ca69f4a3dc955ec6aaea16)), closes [#448](https://github.com/geoah/substrate/issues/448)
* **console:** save changes to a view without keeping its old filters ([#811](https://github.com/geoah/substrate/issues/811)) ([dbbc106](https://github.com/geoah/substrate/commit/dbbc106acf0224e3f4ab9fe62c1d9217219bf5a0))
* **console:** say how many of a group's rows are on other pages ([#813](https://github.com/geoah/substrate/issues/813)) ([dc356af](https://github.com/geoah/substrate/commit/dc356af99761aafa04486ea13281088f9af6c389)), closes [#679](https://github.com/geoah/substrate/issues/679)
* **engine:** build indexes on records concurrently ([#788](https://github.com/geoah/substrate/issues/788)) ([37d42e4](https://github.com/geoah/substrate/commit/37d42e46ab3bbdda44bcd86d6e0f7f678515da85)), closes [#416](https://github.com/geoah/substrate/issues/416)
* **engine:** count and convert a kept kind whose propertytype moves ([#810](https://github.com/geoah/substrate/issues/810)) ([2842b7a](https://github.com/geoah/substrate/commit/2842b7ae1ace1de2ebb7aaa3a03e8c84d7294c9d))
* **engine:** index a parked package's rows under its stored declaration ([#793](https://github.com/geoah/substrate/issues/793)) ([275f572](https://github.com/geoah/substrate/commit/275f572cc51bbc96e20fb04a31c851cbd526dbd3))
* **vocabulary:** refuse core/token in function and agent writes ([#797](https://github.com/geoah/substrate/issues/797)) ([1ff1b5e](https://github.com/geoah/substrate/commit/1ff1b5edfcbddec51834d40b42de139ae687ade1)), closes [#135](https://github.com/geoah/substrate/issues/135)

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
