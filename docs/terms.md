# Terms

One word per thing. If a word is not here, it is not a term — and where these
pages and the code disagree, the code is right.

Dead words, and what replaced them: **entity** → record, **group** → authority,
**type** → kind, **capability** → trait,
**schema** → vocabulary, **log** → changelog, **extension** → bundle,
**integration** → provider, **configType** and **singleton** → input,
**relationship** and **edge** → reference, **plural** → the kind's name, the
third segment of its reference (decision 0033), as an address; a plural
display form is the kind's declared `label` (decision 0117), **incoming** → the
`referencing` filter arm (decision 0079), **username** → the identity's name
(decision 0274), **login credential** → the identity, **measure** (and
`measures:`) → a unit's linear component, which a declaration names with
`unit:` (decision 0186), **tier** in the catalog's sense (an entry's `tier`)
→ who keeps the package, the entry's `keptBy` (decision 0258), **principal**
→ `token`, the token record a write stood behind (decision 0270), **tenant**
→ nothing, there are none.

## The shape of things

| Term | What it is |
| ---- | ---------- |
| **repository** | Everything one user has: one changelog, the records folded out of it, and the blob store and sealed store beside them. One user, one repository, no sharing. It owns one **authority**, any hostname its user controls, chosen at registration (`ada.example.com`; a bare label is completed under the server's host), which is the repository's name; registration names the user's identity after it unless told otherwise. The packages its user declares kinds in, and the samples they add, live in its **local namespace**, `~`, so the name never enters their data. Its storage is keyed by its **repository id**. |
| **identity** | A person's sign-in, held by the server beside the repositories and in none of them: the name a login presents (DNS-style, like a repository's), the password and the second factor, sealed under a key of its own, and the repositories it may open. It holds no data. A sign-in names the identity, which picks the repository; a token names the identity it acts for. Its id is `idn-` and 26 characters ([0274](decisions/0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)). |
| **repository id** | The key a repository's storage is under, minted at registration and never changed: `rnd-` and 26 characters (`rnd-0vrkvsgbs6bnz2sjy4196kccck`). It names the directory under the data root, fills the `repository` column of every table, and is the address the server gives out for a webhook. It holds no dot, so it is never an authority, and every door that names a repository takes either ([0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)). |
| **record** | One typed thing. Identity is `(kind, id)` within a repository. It is the only thing the substrate stores. |
| **kind** | What a record is, written `<authority>/<package>/<name>`; every kind carries both. The repository's own kinds are written `~/<package>/<name>` (`~/tasks/task`). A kind declares the properties its records may carry. |
| **authority** | The DNS-style label that publishes packages, or `~` for the repository's own. One path segment: `/api/v1/{authority}/{package}/{kind}/{id}`. |
| **local namespace** | `~`, the authority of the repository's own packages: the ones it declares and the samples it adds. `~/tasks/task` is this repository's tasks kind whatever the repository is called, so its name never enters the data and two repositories' copies of one sample are one kind. It is the one authority without a dot and the one short form of a kind reference. Every input also accepts the repository's full name in its place (`ada.example.com/tasks/task`) and folds it into `~`; every reply says `~`. Decision record [0259](decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md). |
| **package** | The set of kinds one authority versions, owns and quarantines together, named by a plain word: `samples.substrate.reamde.dev/tasks`. Two packages under one authority upgrade and fail independently. |
| **property** | A named, typed value on a record, declared by its kind. |
| **property type** | A named refinement of a base type plus its validations, declared in a package and reusable across its kinds. |
| **trait** | A contract a kind implements: a set of properties a kind promises to declare, so unrelated kinds can be treated alike. |
| **role** | What a person may read and write in a repository: a record of `core/role`, a name and a list of **rules**, each a list of verbs (`read`, `write`) over a list of kind patterns. Rules only add. A person's actor names the roles they hold; `admin`, `editor` and `viewer` ship ([0275](decisions/0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)). |
| **control kind** | A kind whose records decide what code runs, what it may do, what the vocabulary is or who may do what: triggers, policies, providers, settings, applets, declarations, actors and roles. A rule reaches one only by naming it, and `admin` reaches every one ([0276](decisions/0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md)). |
| **purpose** | Why a kind exists, declared on the kind: `primary` (browsed and opened directly), `supporting` (a detail reached from the records it belongs to) or `internal` (machinery); absent reads as `primary`. Clients read it to decide what they list, `filter.purposes` narrows a read by it, search ranks machinery lower, and an agent's kinds list leaves out the `internal` kinds its grant does not name. Decision records 0133, 0136 and 0172. |
| **series** | A temporal record with a repeat rule (`recurrence`, or `rdates`) under core's `recurring` trait. Its bound slot is the rule's first occurrence; the substrate stores the rule and never expands it into rows. In an aggregate read the word means something else: one group's rows across the buckets (with `groupBy` on `code`, one code's rows within a group), which `fill` makes dense and `maxBuckets` multiplies by the buckets. |
| **occurrence** | One instant a series' rule names. Computed by the window read, never stored: it is served in the record envelope as `<seriesId>_<slot>` with `computed: true`, and on a kind that binds `override` a put at that id turns it into one. |
| **override** | A temporal record standing in for one occurrence of a series, moved or edited: it names the series in `recurrenceOf` and the slot it replaces in `originalAt`, under core's `override` trait. iCalendar's `RECURRENCE-ID`. |
| **window read** | A records list whose filter bounds `at` on both ends: the rows in the window plus every series' computed occurrences, minus `exdates` and overridden slots, ordered by slot. |
| **reference** | A named, directed pointer at one record, declared as a property and stored as an object holding the target's `<kind>/<id>` path under `ref`. A write may spell the value as that object or as the bare path; every read serves the object. The only link between records; it may declare properties of its own, carried beside `ref`. |

## Truth and derivation

| Term | What it is |
| ---- | ---------- |
| **changelog** | The repository's one append-only, strictly sequential list of every change, the `changelog` table in Postgres. It is the truth. |
| **fold** | The records the changelog is replayed into. The only write path to them, so a live write and a rebuild cannot drift. |
| **archive** | The copy of each repository the server writes after its writes commit, into a directory or an S3-compatible bucket: its changelog in pieces, its blobs, its sealed values, its webhook spool and a manifest. The backup, what a restore reads and what the recovery export hands out. Not Postgres's WAL archive. |
| **piece** | One object of an archive's changelog, `changelog/<dialect>/<first seq>-<last seq>.ndjson`: whole transactions of consecutive entries as newline-delimited JSON, each line carrying its own SHA-256 checksum in `sum`. Written once and never changed. |
| **mark** | The `head` of an archive's manifest: every entry up to it is in the pieces, and every blob and sealed value the records at it hold is in the archive. The server repeats it on the repository's row, which `repository list` shows. |
| **data root** | `SUBSTRATE_DATA_ROOT`: the directory holding what Postgres does not, one subdirectory per repository, `repositories/<id>/`, with its blob bytes and its webhook spool, and, by default, the archive under `archive/`. |
| **checksum** | The SHA-256 of one entry's canonical line, stored as the entry's `hash` and returned on the wire under that name. It detects damage; it does not chain to the previous entry and nothing signs it. |
| **projection** | Recomputing a record's mapped properties from every live source record that maps onto it. |
| **recordmapping** | A declaration of how one kind's properties reach the record its subject reference points at. |
| **merge** | Joining two records of one kind so the winner absorbs the loser's place in the graph. |
| **split** | The undo of exactly one recorded merge. |
| **mark** | What a property carries beside its value: `pinned`, whether sync leaves it alone, stored when the value is written from the type of the hand that wrote it, and `setBy`, `onBehalfOf`, `derivedFrom` and `setAt`, where the value came from. `pin` and `release` move it. Decision record 0284. |
| **actor** | One hand that writes, as a record of `substrate.reamde.dev/core/actor` the engine writes: the repository's `person`, an `app` the person approved, an installed `function` or `agent`, a `package`'s own hand, or the `system`. A history line, a change event, a property's mark and every stored attribution link it by its id, beside `onBehalfOf`, the actor it acted for, and `token`, the token it stood behind. No package declares one and no request names one. Attribution, never authorization. Decision record 0270. |
| **writer** | A property's `writer:`: the types of **actor** that may write it, one or a list of `person`, `app`, `function`, `agent`, `package` and `system`; absent, any actor. The system may write any property. Not a class: the words `owner`, `connector` and `oauth` it took before are refused, each naming its replacement. Decision record 0290. |

## Declarations

| Term | What it is |
| ---- | ---------- |
| **vocabulary** | Everything declarable, held as ordinary records: authorities, packages, kinds, property types, traits, recordmappings, functions, agents and bundles. Applied through `POST /api/v1/vocabulary/apply`. |
| **registry** | The live, in-memory vocabulary a repository rebuilds from its own stored declarations when it opens. |
| **dialect** | A monotonic integer stamped on each repository naming a shape it speaks. A binary older than the stamp refuses to open the repository. There are two, refused the same way and stamped differently: the **vocabulary dialect** over its stored declarations, stamped at the repository's first open, and the **changelog dialect** over the entries in its changelog, claimed by the transaction that appends them. |
| **managed** | A declared property the ENGINE stamps (`managed: true`): a declaration's `version`, its `source`, a package's `declaredBy`, the quarantine marks, a bundle's lifecycle bools, and the decision stamps `decidedAt` and `resolvedAt`. A write may echo the stored value, but a different one is refused rather than dropped, and a client renders it read-only. The one exception is a declaration's `version`, which the engine resolves itself: an incoming value is honored only when it moves past the stored one, and anything else (absent, echoed, lower) is stamped server-side. Not projection's *managed properties*, which is the ownership rule over a record's values. |

## Measured values

| Term | What it is |
| ---- | ---------- |
| **quantity** | A value of the `quantity` datatype: exact decimal digits and the id of a unit record, `{value, unit, comparator?}`. Decision records 0153 and 0186. |
| **unit** | A record of core's `unit` kind: what a quantity is written in (`kg`, `Cel`, `1/min`), with its name, symbol, UCUM `code` and aliases. Every repository is seeded with the shipped units and may add its own ([units](units.md)). |
| **conversion** | A record of core's `conversion` kind joining two units: an exact `factor` and `offset`, or a CEL expression and its inverse. The server chains conversions to convert a value; units joined by linear conversions form a **linear component**, the set a value converts and compares within. Decision record 0186. |
| **observed value** | A record of a kind that binds core's `observed` trait: one value (`observed(single)`, an observation) or many values of one metric over one fixed period (`observed(packed)`, a trace). Decision record 0154. |
| **metric** | A record of the seeded `substrate.reamde.dev/observations/metric` kind: what can be measured, its codes in other systems and the units its values are written in. Its id starts with the package that ships it (`health/heart_rate`). Decision record 0192. |
| **source** | On an observed value, the device, app, service or lab that produced it. The mirror row that delivered it is its `origin`. Not a declaration's managed `source`. |
| **form** | What an observed value's number means, written on the value: `gauge` (a level), `amount` (accrued over a span), `counter` (a running total) or `summary` (a statistic its source computed). It decides which reducers apply. Decision record 0156. |
| **hold** | How long a gauge's value stands after its point when no newer one arrives. Decision record 0158. |
| **stream** | One run of readings: the kind, metric, source, about and channel a value names, and a summary's statistic. The index keys it by a private id. Decision record 0160. |
| **point** | One value of an observed record: one per observation, one per point of a trace, derived from the record whenever the index or a read needs it. It is **available** when it holds a value or a code that is not empty; a `null` is an unavailable point and counts in nothing. |
| **segment** | The observed index's one row per observed record: its stream, the span of its points, what it holds and a fingerprint of what the index reads from it. |
| **rollup row** | One row of `observed_rollups`: one stream's points of one form in one bucket of one tier (`5m`, `15m`, `1h` or `1d`, as its metric's `rollups` name) and group, reduced to the statistics a read merges exactly. |
| **group** | In the observed index, the points of one stream, form and bucket that share a code, unit, `delta` and UTC offset: one rollup row each. In an aggregate read, the streams one combination of `groupBy` values selects: one row per bucket each. Never the old word for an authority. |
| **bucket** | One time window of an aggregate read: a fixed length, a calendar unit in a zone, a recorded-local day, or a matched record's span. |
| **recorded-local calendar** | The calendar time mode with no `timezone`: each value lands on the date its own UTC offset names, so a day spent abroad is that place's day. |
| **adapter** | A provider's function that turns its own mirror rows into observed values and health records, stamping `adapter` (`whoop@25`) on each. Also the code that speaks an `llm/provider`'s [wire](#installed-things). |

## Installed things

| Term | What it is |
| ---- | ---------- |
| **bundle** | The unit of installation: a closure of declarations and behavior, applied and removed as one unit. It owns one package, and its `metadata.id` is that package's identity. |
| **published** | One of the two sorts of catalog package: a package its publisher keeps (`providers.substrate.reamde.dev/google`, `substrate.reamde.dev/labs`, a package an authority document lists), `keptBy: publisher` on its entry. Adding it lands it under the authority that publishes it, and the publisher ships each change with a version bump the upgrade preview offers. `keptBy` says who keeps the declarations, never what the package is for. Decision records 0048, 0238 and 0258. |
| **provider** | A package that connects the substrate to an outside service, as the catalog says on its entry: every package under `providers.substrate.reamde.dev`, and any other published package with an account kind. The console's Providers page lists them, and its AI models beside them. Decision record 0238. |
| **add-on** | A package that extends a provider (Slack live events extends Slack): its publisher's document names the provider, or it brings no account of its own and requires one. The console lists it on that provider's page, never as a provider of its own. Decision records 0216 and 0238. |
| **catalogue** | A first-party package under `substrate.reamde.dev` holding one domain's shared metrics, kinds, units and functions (`health`, `labs`, `environment`, `clinical`). The catalog serves it as a published package: adding it lands it under its own authority, it upgrades from the catalog, and no seed writes it. It is no provider: the console calls it a standard collection and offers it under Add a collection. Not the **catalog** itself. Decision records 0192, 0193 and 0238. |
| **standard collection** | The console's word for a published package that is neither a provider nor an add-on and adds collections or metrics: a catalogue, or a package an app's approval installed. Added from Add a collection, and updated, paused and removed on its package page. Decision record 0238. |
| **sample** | The other sort: a package the repository keeps (`samples/`), `keptBy: repository` on its entry. Adding it COPIES it into the local namespace (`samples.substrate.reamde.dev/tasks/task` lands as `~/tasks/task`, the same in every repository), and the copy is the repository's afterwards: writable, and offered an upgrade through the origin stamp adding it left. Decision records 0048, 0070, 0258 and 0259. |
| **vocabulary bundle** | A description, not a sort of catalog package: a bundle that happens to ship only kinds and rules, with no functions and no provider. Nothing in the code reads it. |
| **input** | A bundle's named configuration need: it names a kind, and the engine resolves ONE record per input — the bound record, else the record whose id is `default`, else the sole live record, else nothing, surfaced per input on the bundle's status. No cardinality is enforced on the kind. |
| **setting** | A bundle's configuration as an ordinary record, a core `setting` or `secret` at `<bundle id>/<name>`, shipped in the closure and filled in by the user. Ownership is the id prefix alone; a bundle's functions read them as `config.settings.<name>`. Decision record 0076. |
| **bind** | The explicit step of input resolution: a reference on the bundle's own record row, named for the input, pointing it at a chosen record. `POST /api/v1/substrate.reamde.dev/core/bundle/{id}/bind`; an empty record unbinds. |
| **account** | One configured connection to a provider: a record of an `accountconfig`-trait kind. The console lists them under **Accounts** on each provider's page. |
| **sync** | The synchronization a provider's function drives on one account, as the core `sync` trait reports it: a state (`never`, `running`, `ok`, `erroring`, `throttled`), a message, the last run, the owner's request for the next one, progress and streams. The body writes it through its effects; the dispatcher stamps a delivery's start, finish and park. Decision record 0085. |
| **catalog** | The read-only bundle catalog: shipped closures, each a published package or a sample (`keptBy`), and published closures resolved through authority documents, each entry saying what it is for (`provider`, `brings`, `addonFor`), which each bundle status repeats. A source to add from, through one route, never a package. Decision record 0258. |
| **callable** | The union of function and agent — what a trigger binds and what dispatch invokes. |
| **function** | A callable whose body is inline Python, bounded by its declared `permissions`: `reads`, `writes`, `call`, `agents`, `network` and `mutations`, six grants in one object on the declaration. |
| **agent** | A callable whose body is an LLM loop. Experimental. |
| **llm/provider** | One place an agent buys completions: a wire, an endpoint and a key, as data. The kind is `provider` in the seeded `substrate.reamde.dev/llm` package, written here with its package because the bare word also names a provider package. Experimental. The console's Providers page lists these rows as AI models, beside the provider packages. |
| **operator** | Whoever runs the deployment: the server, its database and the vendor accounts behind it. `substratectl`'s operator hat is theirs. The console names the operator by the site's name (`site.yaml`), or "this server" where there is none. |
| **operator default** | A key or an OAuth client the operator supplies in the file `SUBSTRATE_OPERATOR_DEFAULTS` names, used only where the repository holds none of its own: an `llm/provider` row with an empty `apiKey` whose id the file lists runs on the operator's transport, and the operator's embeddings serve a repository whose rows declare no `embedModel`. It never enters a repository. Decision record 0235. |
| **allowance** | What the operator pays for one repository in one UTC day, the defaults file's `budget.centsPerDay`. Every call an operator default pays for reserves against it first; at the limit a trigger is held and any other call is refused with `403 guard`, reason `quota`. The person's own spend cap is separate. |
| **record view** | How the console lays out the record page of one kind: a record of the seeded `substrate.reamde.dev/console/recordview` kind, an ordered list of sections each holding one block. Not a **saved view**, which is a table's saved filters and columns. Decision record 0166. |
| **shipped view** | A **saved view** a package ships for one collection: a record of the seeded `substrate.reamde.dev/console/collectionview` kind, shown as a tab after All, and the tab a bare address opens on where it says `default`. Decision record 0229. |
| **applet** | A small app over the owner's records: a record of the seeded `substrate.reamde.dev/applets/applet` kind naming the runtime that draws its screens (`a2ui`, A2UI v1.0 components, is the one there is), with the sources that fill them and the actions their buttons run. The server renders it and runs its actions under the token of whoever opens it, at `/applets/{id}` in the console and `/a/{id}` full screen. Moss's `designer`, the owner and the MCP door write one; no app scope does. Not an **app**, which is a program granted access; not a **bundle**; not a **record view**. Experimental. Decision records 0243 and 0244. |
| **wire** | The protocol an `llm/provider`'s adapter speaks — `openai`, `anthropic` or `azure` — never a company: a gateway that speaks OpenAI's wire is an `openai` row. |
| **model** | The model id an agent sends on every completion, a plain string its provider understands. There is no model record and no tier. |
| **trigger** | One delivery binding: exactly one source (`record`, `schedule` or `webhook`) to one callable. |
| **run** | One settled delivery attempt of a trigger. |

## Sensitive values

| Term | What it is |
| ---- | ---------- |
| **sensitive** | The umbrella over `secret` and `digest`: redacted on every read surface, excluded from search, filtering, ordering, titles and change payloads. |
| **secret** | Confidential material as a property. The record and the changelog store only an opaque ref; the material lives encrypted in the sealed store, resolved only by the host reads that spend it. Rotation deletes the old material. |
| **digest** | A one-way SHA-256 the server minted to compare, never to reveal (a token's `hash`). Redacted like a secret but stored as the value itself: auth matches it in SQL. |
| **sealed store** | The `sealed` table, holding secret material (property secrets, OAuth tokens, the password hash, the TOTP seed) encrypted under the repository's DEK, addressed by refs. The archive keeps a copy of each row under `sealed/`, still ciphertext, which a restore reads back. |
| **DEK** | The repository's own data-encryption key. Wrapped under the host credential key, in the control plane and in the repository's manifest, and nowhere else: a repository's secrets move to another host only with that key. |

## The wire

| Term | What it is |
| ---- | ---------- |
| **token** | A bearer credential, itself a record. Without `scopes` it has full access to its repository; an app's token carries the scopes the owner approved. |
| **scope** | One entry of an app's token: `read:<pattern>` or `write:<pattern>` over kind references, `agents:<pattern>` or `decide:<pattern>` over agent ids, `call:<pattern>` over function ids, `webhook:<trigger id>` naming one webhook trigger; a pattern names the repository's own kinds under `~`, and the token keeps it as written ([apps](apps.md)). Not a provider's OAuth scopes, which an account's `grantedScopes` lists. |
| **app** | A program that asked for access through the authorization flow, named by its client id URL ([apps](apps.md)). Its writes are made by its actor record, on behalf of the person, whose `publisher` is its authority when its publisher is verified, `local` when its access went to a loopback address, and the host of its client id otherwise (decision records 0209 and 0270). Not a provider, and not a bundle. |
| **authority document** | The JSON file an authority publishes about itself at the URL its `_substrate` TXT record names (`doc=`): its name, the apps that speak for it by client id, and the packages it publishes. An app the document lists, whose callback is on the authority, is a verified app ([apps](apps.md#name-your-publisher), decision record 0209). |
| **domain** | A repository's or a publisher's authority as DNS holds it, where its `_substrate` TXT record lives. **Verify domain** proves a repository controls its name's DNS (`domainVerifiedAt`, decision record 0208); the word names the DNS side of an authority and nothing else, so a kind, a package and a route still name the authority. |
| **set up** | What approving an app's request runs on the owner's server, as the owner, before the code exists: the catalog packages its `authorization_details` name installed or upgraded, its account created, its webhook enabled ([apps](apps.md#ask-for-set-up), decision record 0202). Each detail is one row whose `action` is `install`, `upgrade`, `create`, `enable` or `none`, and which carries `blocked`, with a reason, where it cannot run. |
| **blob** | Content-addressed bytes whose digest is its id and whose manifest is an ordinary record. |
| **records route** | `GET /api/v1/records`: the one read of many records, in four modes told apart by their parameters: the list (`filter`, `orderBy`, `first`, `after`, `offset`, `expand`), the ranked read (`q`), the tail (`watch=1`) and the aggregate read (`aggregate`, decision record 0161). `POST` there creates one record under a server-assigned id. Decision record 0079. |
| **record path** | `/api/v1/{authority}/{package}/{kind}/{id}`: one record's URL, which is its reference value; a record of the repository's own kind is at `/api/v1/~/tasks/task/{id}`. Decision record 0033. |
| **referencing** | The filter arm that reads a reference backwards: the records pointing at one record (`ref`) or at any of several (`refs`), each matched by its canonical and former ids. The page carries `matches` naming the property each pointed from. |
| **expand** | The list parameter naming reference properties whose referents come back beside the page under `included`, keyed by record path, one hop. |
| **watch** | The ndjson tail of the records route, narrowed to a set of kinds, or of the changelog, resumable from a cursor. |
| **feature** | A named group of the API's routes: one entry of `features` in `GET /.well-known/substrate/server.json`, carrying its **stability**, a `revision` (a number that only goes up, raised when the feature gains something a client may want to check for), the surfaces that serve it (`rest` and `mcp` are the two there are) and, where the server announces a coming break, a `notice`. A client reads it instead of probing for failures. |
| **stability** | The promise a package or a **feature** makes about change, one of three labels: `stable` (it only gains things within 1.x), `beta` (supported, and a break is announced one minor release ahead) and `experimental` (it may change or go in any release). A package states it in its header, for its kinds, and one that states none reads as `experimental`; a feature carries it in discovery. Not a kind's display `label`. [Stability](stability.md) is the policy. Decision record 0256. |

## In an app

The apps' code is under `apps/`, and the plan for it is
[plans/app-kit.md](plans/app-kit.md). An app converted to the kit builds
each screen as one loop: a command, a controller that enriches it into an
event, then, in this order, an engine that applies the event, a journal
written before anything can suspend, a presenter, and the effects.
Workouts' session, Chat and Tasks are built so; Sync is not converted yet. These
words mean that loop's parts "in an app", and three of them are not the
server's: an app's **engine** is not `internal/engine`, its **effect** is
not a function's returned effects, and its **link** is not a reference.
The kit takes none of adapter, mirror, stream, sync, feature, group or
account for a name of its own.

| Term | What it is |
| ---- | ---------- |
| **command** | In an app: what the UI asks a controller for, as a value (tap Done on row 3). The UI sends nothing else. |
| **event** | In an app: a command enriched by its controller with what a pure function may not read (new ids, the current instant, lookups in caches), or the result of an effect; what an engine applies. Not a calendar event record. |
| **controller** | In an app: the one writer of a piece of state. It turns each command into an event, has its engine apply it, writes the journal where it keeps one, presents, and only then runs the effects. A child controller is keyed by an id the app minted, and reaches its parent only through an event. |
| **engine** | In an app: a controller's `apply(event, to: state) throws -> State`, pure, so folding the journal's events through it gives the same state every time. Not `internal/engine`, the server's implementation of the substrate. |
| **effect** | In an app: work an event asks for through the client, the local store or a device service, decided by a pure function of the event, the state (and the state before it, where what changed matters) and the presentation, and run only live, never on a replay. Its result comes back as an event. Not the effects a function body returns for the server to apply ([functions](functions.md)). |
| **presenter** | In an app: `present(state, inputs) -> Presentation`, pure, with every input it reads (catalogue, history, settings, the instant, the locale) declared, so a test pins each. |
| **presentation** | In an app: the value a view draws, made of strings, numbers, enums and ids, and the only thing a UI reads. Its `nextChange` instant has the runtime deliver a tick then, so no view keeps a timer. |
| **signal** | In an app: a value in a presentation the UI acts on once rather than draws (a haptic, a cover to open, a row to scroll to), with a serial its controller moves at each raise, so the same value raised twice acts twice and a presentation drawn again acts not at all. |
| **journal** | In an app: a controller's events kept on the device, written once the engine has applied the event and before the controller suspends, presents or runs an effect, and folded back through its engine at launch. Optional per controller: Workouts' session keeps one, Chat's conversation does not, since the server holds it. Not the **changelog**. |
| **link** | In an app: the root controller that owns the connection to one repository: the session and its revocations, the status check, the versions of the app's packages, connectivity and the outbox's state. It keeps the app's own root as its one child, tells every controller under it its snapshot, which carries no secret, and hears back when a request was refused. Not a **reference**, which is the only link between records. |
| **device service** | In an app: what the device does for the core behind one protocol (transport, secure store, browser sign-in, DNS lookups, connectivity, app folders, notifications, background work, ongoing activities), with an Apple implementation, an Android one where an Android app needs it, and a fake. A request and its answer are values, and an answer keeps each outcome apart. |
| **chunked answer** | In an app on Android: a device service's answer that crosses the bridge in parts (a line stream's lines, connectivity's changes), in order, into a bounded queue, ending once with its end, an error or a cancel. |
| **outbox** | In an app: the writes waiting to reach the server, kept on the device in order: puts, patches and deletes with their `ifVersion` chains, held back when refused until they are retried, discarded or resolved. |
| **batch** | In an app: the outbox's writes that stand or fall together (Workouts' session and its sets): discarding one drops the batch, and a write the server refuses holds the rest of it back. |
| **watch generation** | In an app: the `generation` a records page hands over with its `head`, which a **watch** resumed from them must match; a watch answered with another relists. |
| **keyed list** | In an app: a long collection in a presentation (a transcript, a task list) whose rows are keyed by id and which carries a revision, so the Android bridge sends it whole once and then only the rows that changed. |
| **handle generation** | In an app: the number an Android bridge handle carries, so a message for a closed handle, or one opened again, is dropped. |
| **controller generation** | In an app: the number closing or resetting a controller moves, so every result still outstanding for it is dropped, whatever its key. |
| **linked records** | In an app: the records of the kinds it shows, read once and then followed through the **watch**, with the outbox's waiting writes laid over them, kept by the app's root through the kit's `LinkedRecords`. |
| **record cache** | In an app: the records it last read and where its watch resumes, kept in the repository's folder on the device, so the app opens on them before the server answers; the server's, read again on demand. |
| **fold** | In an app: the records as the owner sees them, the server's as last read with the outbox's waiting writes applied in order over them; a refused write is not laid over. Not `internal/engine/fold.go`, the server's one path from the changelog to the records table. |
| **read again** | In an app: one record read from the server after the watch named it or a write of it left the outbox, so the fold holds the server's copy; one that fails waits for the network. |
| **shell** | In an app: what an Apple app runs its link and root under, the kit's `AppShell`: the session, the repository's folder, sign-in and Change Access, and Settings as one screen. The console's shell is another thing: the native window the bundled console runs in. |
| **App menu** | In an app: its top level as data, its sections drawn as a tab bar on a phone and as a sidebar on a tablet, a computer and a wide Android screen, which section is open its presentation's. |
| **link generation** | In an app: the number a sign-out moves, kept in the repository's folder on the device, which background work names when it is scheduled and checks before every write, so a job from before a sign-out writes nothing. |

## Words that mean something narrower than they look

- **capabilities** — only ever the name for a function's security envelope, and
  no longer a key: the grant is six keys on the declaration itself, and a
  document nesting them under `capabilities:` is refused. The substrate does not
  otherwise use the word; what a deployment offers is a *feature*, and what a
  kind promises is a *trait*.
- **schema** — only the JSON Schema a function's `arguments`/`returns`
  compile into, and Postgres. The substrate's declarations are its
  *vocabulary*.
- **log** — only logging. What holds the deltas is the *changelog*.
- **extension** — only a file extension or a Postgres extension. What you
  install is a *bundle*.
