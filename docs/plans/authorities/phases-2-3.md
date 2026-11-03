# Authorities phases 2 and 3: what is built

As of 2026-11-03. This page turns phases 2 and 3 of [the plan](../authorities.md)
into the rules the code holds. Where it and the plan disagree, this page
wins and the plan is amended when the work lands. Each section names the
decision record it lands as.

## Phase 2: published packages through approval

### The authority document reader

Decision 0212.

- `internal/authority` holds the authority document: its shape, the strict
  parse of the keys a server reads, the read through the `_substrate`
  record, the document cache and the development files. Phase 1's
  `authorize.AuthorityDocument`, `ParseAuthorityDocument` and the fetch in
  `authorize.Verifier` move there; `authorize.Verifier` keeps the app
  verdict and reads documents through it. `ConfinedClient` and the confined
  transport move to `internal/egress`, the package of the dial rules they
  apply.
- A document key this server does not read is ignored, never refused: a
  publisher may write a key for a newer server without becoming unverified
  to an older one. A known key of the wrong type, or a malformed value, is
  refused as before.
- `Read(ctx, authority, fresh)` resolves the TXT record on every call and
  serves the fetched document from the cache unless `fresh`. A package entry
  is held to its shape: a package name, versions that are unique positive
  integers, each with a closure URL and a 64-hex `sha256`, and an
  `addonFor` that is a package id.
- `Closure(ctx, source, version)` fetches one version's closure through the
  confined client (HTTPS, public addresses, no redirects, 4 MB cap) and
  refuses bytes that do not hash to the listed `sha256`. A development
  document's closures are paths relative to the document's own directory;
  an absolute path or one that leaves the directory is refused.

### Published packages in the catalog

Decision 0212.

- `catalog.Catalog` resolves a package id through two sources: the
  binary's shipped closures, then the authority documents
  (`Resolve(ctx, id, atLeast)`). The binary wins for every id it ships, and
  `providers.substrate.reamde.dev`, `samples.substrate.reamde.dev` and every
  package the binary ships under `substrate.reamde.dev` are never read from
  a document. A package under the repository's own authority is never
  installed from a document: the repository is its publisher and declares
  it.
- A document-published package resolves to the newest listed version at or
  above the floor. None listed, no document, or a document that cannot be
  read is `not-published`, with the reason in the owner's words.
- `catalog.ParseClosure` reads a fetched closure: the multi-document YAML a
  bundle directory ships, exactly one `bundle` document whose id is the
  package and whose version is the listed version, and every declaration
  and data document inside that package. The bundle's tier is `provider`
  (installed under its publisher's authority, `source: published`).
- Installing a document-published package stamps two managed properties on
  its package row beside `shippedVersion` (the publisher's version):
  `publishedFrom`, the document URL (or `file:` and the path for a
  development document), and `publishedDigest`, the closure's sha256.
  Installing a version the row already holds with another digest is
  refused (a published version is immutable), and so is installing a lower
  version than the row holds (the server never downgrades).
- The owner's door is the catalog's: `POST /api/v1/catalog/{id}/install`
  and `substratectl install <id>` resolve through both sources.
- **Take over…** (`POST /api/v1/vocabulary/takeover`) admits a
  document-published package: the row becomes `installed`, `takenOverFrom`
  names the publisher, `publishedFrom` is cleared, and upgrades stop.

### Upgrades from the publisher

Decisions 0212 and 0213.

- `Catalog.Refresh(ctx, ds)` reads, fresh, the document of every package the
  repository holds with `publishedFrom` set, and for a newer listed version
  plans the upgrade. A kinds-only, additive upgrade (no function, trigger,
  agent, mapping, `oauth2`, input or module added or changed, and nothing
  removed or renamed) is applied as the bundle. Any other waits for the
  owner as `upgrade-needs-review`, shown where the catalog already shows an
  upgrade. A publisher that no longer verifies leaves the package installed
  and not upgraded, and the upgrade offer says why.
- `cmd/substrated` runs it for every repository a minute after boot and
  every 24 hours, one repository at a time. An app's set up check that
  names a floor above the held version runs it for that package at once.

### Versions and build tracking

Decision 0213.

- The core `token` kind gains three managed properties: `appPackages` (the
  floor each package detail of the app's build asked for, by package id),
  `appSoftwareVersion` (the build's own version, at most 64 printable
  characters) and `appCheckedAt` (when the build last checked in).
- `/oauth/authorize` reads an optional `software_version` parameter (RFC
  7591's name). The token minted from the approval records it and the
  floors of the request's `package` details. The set up check
  (`POST /api/v1/authorization/check`) takes `softwareVersion` in its body
  and records the floors of its `package` details, the version and the time
  on the calling token (`Dataset.RecordAppBuild`).
- An upgrade from held version H to candidate C **narrows** when it removes
  or renames anything the held closure declares. A narrowing upgrade is held
  while any live token records a floor under C for the package; Refresh then
  takes the newest listed version above H that no live token holds back.
  The upgrade offer names each app holding it back: its name, client id,
  software version and when it last checked in.
- A published package's versions are its publisher's. `substratectl publish
  --check` compares a candidate closure with the document (a file, or the
  authority's live one): a version already listed with another digest, or a
  version under the newest listed, fails; a candidate that narrows the
  newest listed closure fails unless `--allow-narrowing`. It prints the
  version entry to add.

### The approval installs published packages

Decision 0214, superseding 0203, 0205 and 0207 and amending 0202.

- A `package` detail's `version` stays its floor. The planner resolves the
  package through `Catalog.Resolve`; a document-published package's row
  carries `publisher` (`authority`, the document's `name`, `documentUrl`,
  `fromFile`) and the version the approval would install. `not-published`
  joins the block reasons.
- **`declare:` is retired.** A request naming it is refused, pointing at a
  `package` detail, and a stored token carrying it fails closed (the app
  signs in again). No scoped token applies vocabulary. 0205's lock on an
  app-declared package is removed; `declaredByClient` is deprecated and no
  longer read for ownership, and takeover is for published packages.
- **The `agent` detail is retired.** An app's agent ships in a published
  package (decision 0217). The token's `agents` property is deprecated and
  no longer minted, and the engine's live-approval checks of 0207 go with
  it: a published package's agent runs as an agent of that package, bounded
  by its own declared reach (held to the approval's scopes, decision 0215)
  and its own daily cap, until the owner pauses or uninstalls the package.
- What an approval installed stays after the app is revoked, as 0202 says;
  the page says so and names the package page where it is paused.

### Workouts on `substrate.reamde.dev/training`

- The packages stay in `apps/apple/SubstrateWorkouts/Package/`, spelled
  under `substrate.reamde.dev`: `substrate.reamde.dev/training`, and
  `substrate.reamde.dev/trainingcoach` with the coach agent declared in its
  closure (`providerInput`, `models`, a daily cap; decision 0217).
- `mise run apple:site` builds each package's closure as
  `packages/<package>-<version>.yaml` on the site, computes its digest and
  lists it in `authority.json`, keeping every version already published: it
  reads the live document first and refuses to change a published
  version's digest.
- Workouts asks for `package` details on `substrate.reamde.dev/health`,
  `…/training` and `…/trainingcoach`, with
  `write:substrate.reamde.dev/training/*`, its health reads, the coach's
  reads and `agents:substrate.reamde.dev/trainingcoach/*`, and sends
  `software_version`. The coach is not optional: a server whose metadata
  lists no `agents` verb or no `package` detail is too old for the request
  (`WorkoutsManifest.requirement`), and the app says so. It applies no
  vocabulary and rehomes no training kind.

## Phase 3: full apps

### Installed code reaches no further than the approval

Decision 0215, amending 0202.

- The reach of a closure is its functions' `permissions.reads.kinds` and
  `writes`, its agents' reads and writes, its triggers' selector kinds and
  its mappings' sources (read) and targets (write). Every package an
  approval would install or upgrade, requirements included, binary or
  published, must reach only what the approved scopes cover (`write`
  covers `read`; `full` covers everything). A row reaching further is
  blocked `reaches-past-scopes`, naming each uncovered reach and the scope
  that would cover it. Unchecking a scope plans again.
- Each function's network is on the row: its declared hosts, or "any public
  host".
- An upgrade whose reach grows past the held version's waits for the owner,
  like any upgrade that changes code.

### Add-ons wait for their provider

Decision 0216.

- A document package with `addonFor` is optional on the page. Approved
  while the provider is held, it installs with the rest. Approved while it
  is not, the row's action is `wait` and the token records it
  (`appAddons`); when the owner later installs that provider, every live
  token's waiting add-ons for it are planned again against that token's
  scopes and installed as their bundles. A blocked add-on is reported in
  the install's answer, never dropped silently.

### An agent takes its provider through a bundle input

Decision 0217, superseding 0207's model choice.

- The core `agent` kind gains `providerInput`, the name of an input of the
  agent's own bundle whose kind is `substrate.reamde.dev/llm/provider`, and
  `models`, the model ids it runs on, best first. `provider` and `model`
  stop being required: an agent names exactly one of `provider` and
  `providerInput`, `model` beside `provider` and `models` beside
  `providerInput`.
- At dispatch the input resolves by the input rules (the bound record, the
  id `default`, the sole record), and the agent runs on the first of
  `models` the row prices, exactly or as a gateway's prefixed id
  (`*/<model>`). Otherwise the run is refused, naming the row and the
  models.
- A published package's agent declares `budgets.spendCentsPerDay`, at most
  1000; an install without one is refused.
- The approval page asks the owner to pick the provider row for each such
  input, offering only rows that price one of the agents' models, with the
  model each would run on and the agents' daily caps. The set up binds the
  input as the owner. No suitable row blocks the row (`no-model`).
- An app token reaches an agent only when its own reads cover every kind
  the agent reads, as 0207 held for app agents, now for every agent.

### Settings and secrets on the approval page

Decision 0218.

- The page lists a package's empty `setting` and `secret` records
  ([0076](../../decisions/0076-a-bundle-ships-its-settings-as-core-setting-and-secret-records.md))
  and the owner types their values. The approval carries them; the set up
  writes them as the owner after the package lands. They are never in the
  plan's hash, the token, an answer to the app or a log. A required one left
  empty blocks the approval (`needs-setting`).

### The wider scoped surface

Decision 0219, amending 0194 and 0195.

- Window and aggregate reads, and semantic search, over the kinds the token
  reads. Semantic search spends the owner's embedding budget, and the page
  says so.
- A blob read where the token reads a record of a kind whose `blobref`
  property names the blob.
- `call:<pattern>` over function ids, never a `core` or `llm` function: the
  token calls the matching functions, and only those whose own reach its
  read and write scopes cover.
- The change feed's history from a cursor, cut to the kinds the token
  reads.

### Code from outside authorities

Decision 0220, closing [#1144](https://github.com/geoah/substrate/issues/1144).

- Outside code is a function, trigger, agent, mapping or `oauth2` block of
  a package whose closure came from a document, not the binary.
- An outside function reaches only the hosts its `permissions.network`
  declares, enforced where the sandbox connects; one that declares none has
  no network.
- `SUBSTRATE_OUTSIDE_CODE` is `allow` (the default) or `refuse`. Where the
  sandbox cannot confine a body (any host but Linux), outside code is
  refused unless `SUBSTRATE_INSECURE_UNCONFINED_OUTSIDE_CODE` is set.
- An upgrade that adds or changes outside code waits for the owner, and the
  upgrade offer carries the diff of every function, trigger, agent, mapping
  and `oauth2` block it changes.
- An outside package's `oauth2` endpoints must be HTTPS and are shown on the
  page; they need not sit on the publisher's authority, because a
  publisher's add-on for another provider talks to that provider.

## First-party apps

Decision 0221. Sync, Chat, Workouts and the Console app publish under
`substrate.reamde.dev`: their client documents, the association file, the
authority document and the training closures are served from the
substrate.reamde.dev site, and the binary wins for every package it ships.
