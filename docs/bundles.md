# Bundles

A **bundle** is the unit of installation: a closure of declarations and
behavior that teaches the substrate something new, applied as one unit and
removable as one unit. Installing the URL harvester adds functions and agents;
installing a provider like Google adds account access and sync; installing a
vocabulary bundle adds kinds and rules. All three are bundles — there is no
second word for them, on the wire or anywhere else.

The catalog it comes from has **two doors**, one per tier
([decision 0048](decisions/0048-providers-are-published-samples-are-copied.md)):

| Word            | Means                                                                  |
| --------------- | ---------------------------------------------------------------------- |
| **provider**    | a package a publisher owns (`providers.substrate.reamde.dev/google`). It INSTALLS under the authority that publishes it, and the publisher ships each change with a version bump the upgrade preview offers |
| **sample**      | a package the user copies (`samples/`). It IMPORTS under the repository's own authority (`samples.substrate.reamde.dev/tasks/task` lands as `ada.example.com/tasks/task`) and is the repository's afterwards: writable, never offered an upgrade |
| **account**     | one configured connection to a provider: a record of an `accountconfig`-trait kind. The console lists these under **Connections** |

## What a bundle ships

A `bundle` document wears the ordinary envelope — `kind:`, `metadata:`,
`data:`, and the server-owned `status:` — and declares the one **package it
owns**. Its `metadata.id` IS that package
([decision 0047](decisions/0047-a-kind-lives-in-a-package.md)): a closure and
the thing that installs it can never be spelled apart, and a document whose id
says anything else is refused naming the package. The package's own word is the
bundle's name and the prefix an installed kind's GraphQL name carries, so it is
one lowercase word. Two authorities may publish a package of the same word: an
install writes under `bundle:<authority>:<package>`, so they stay two writers,
and their GraphQL names take the authority's first label to stay apart.
`installs:` lists the exact references of everything the closure ships: its [kinds](data-model.md#kinds-and-references),
[traits](data-model.md#traits),
[property types](data-model.md#property-types),
[record mappings](projection.md), [functions](functions.md), and
[agents](agents.md). The harvester's document, its description elided:

```yaml
kind: substrate.reamde.dev/core/bundle
metadata:
  id: samples.substrate.reamde.dev/web
data:
  authority: samples.substrate.reamde.dev
  package: web
  inputs:
    connector:
      kind: samples.substrate.reamde.dev/web/config
      inject: functions
  installs:
    - samples.substrate.reamde.dev/web/config
    - samples.substrate.reamde.dev/web/page
    - samples.substrate.reamde.dev/web/findurls
    - samples.substrate.reamde.dev/web/fetchpage
    - samples.substrate.reamde.dev/web/setclass
    - samples.substrate.reamde.dev/web/stampconfig
    - samples.substrate.reamde.dev/web/pageclassifier
    - samples.substrate.reamde.dev/web/readinglistagent
    - samples.substrate.reamde.dev/web/weeklyrollup
```

The document's own id is the package it owns
(`samples.substrate.reamde.dev/web`), and every entry in `installs:` is a full
kind reference, never a bare name. The bundle document never travels alone: the
[`package`](vocabulary.md) document that heads the closure, and every member
`installs:` names, belong to the same apply. A closure applied without that
header is refused — `package samples.substrate.reamde.dev/web: no package
manifest declares it` — and `installs:` is held equal to what the package
actually declares, both ways, so a member left out and a name that is not there
are each refused by the same rule: the closure is the package.

Two more fields matter, both covered below. `inputs:` declares the bundle's
configuration needs by name, each naming a kind whose records satisfy it. No
cardinality is enforced on such a kind: any number of records may exist. The
engine resolves one record per input, in order: the bound record (a reference
the bind verb writes on the bundle's own row); else the record whose id is
`default`; else the sole live record of the kind; else nothing. An unresolved
input is a first-class state the bundle's status reports per input; the engine
never tie-breaks. An input with `inject: functions` crosses
into the bundle's function invocations under its name; one without is read by
a host facility alone (the OAuth client). A bundle that needs nothing
declares no inputs, and nothing anywhere implies configuration. The second
field is an optional `oauth2:` block of trusted provider endpoints and
scopes, whose `clientInput:` names the input carrying the client
credentials. A bundle may also
carry `modules:`, inline shared library source its functions import
([Functions](functions.md#shared-modules)); modules are sources rather than
closure members, so they never appear in `installs:`.

The installed callables react through the ordinary machinery: a bundle's
functions and agents run on the same [changelog](changelog.md) every other write
lands in, its [mappings](projection.md) fold provider records onto your people
and tasks, and its writes carry their own machine hands —
`function:<authority>:<package>:<name>` for a function's effects,
`agent:<authority>:<package>:<name>` for an agent's,
`bundle:<authority>:<package>` for the declarations an install lays down
([actors](api.md#actors)).

## Install and lifecycle

Installing is one atomic apply of the whole closure: the same admission as any
[vocabulary batch](vocabulary.md#admission), every document admitted or none, active
on commit. The apply replaces the owned package whole, and the loader holds
`installs:` equal to what the package actually declares, both ways, so
nothing is smuggled in or orphaned. **Upgrade is the same verb**, re-applying
the full new closure, and it refuses breakage inside that same transaction, all
problems at once: a dropped kind with live records, a narrowing change that
would strand them (a property dropped or given a new name, a changed property
kind, a removed enum value or state, a newly required property), or the removal
of a callable a live trigger still references. Additive changes (a new kind, a
new optional property, a new enum value or state) admit freely, so ship without
ceremony; [vocabulary evolution](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)
is the full contract a bundle author designs against. Accounts, triggers,
and cursors persist by reference across upgrades.

A **provider** is a bundle whose package a publisher owns: `google`, `github`,
`linear`, `notion`, `whoop` and `beeper`, all published under
`providers.substrate.reamde.dev`. Installing one from the catalog writes
`source: published` on its package, and from then on only an install or an
upgrade writes its declarations: a `POST …/vocabulary/apply` naming that
package is `403` for every token, because a mirror kind edited under the sync
that writes it breaks the next sync. Nothing else about the bundle changes.
Its records are the repository's to write, delete and purge, disable and
enable and uninstall work as they do on any bundle, and the upgrade offer is
the same preview and the same button
([decision 0048](decisions/0048-providers-are-published-samples-are-copied.md)).
Applying the same closure by hand from the shipped files lands `installed`
instead, and stays editable.

The upgrade has a read-only **preview** beside it: the catalog compares the
shipped closure's declaration versions against the stored ones (the same diff
the boot upgrade runs for core, engine `PlanBundleUpgrade`) and attaches the
result to the catalog read as `upgrade`, with the same refuse-breakage guard
lines the install would refuse on as `blockers`. The console's Registry counts
these on the sidebar badge, offers Upgrade where nothing blocks, and states
the guard lines where something does; the button is the install verb,
unchanged. Only a PROVIDER is previewed: a sample's closure landed under the
repository's own authority and belongs to it, so the catalog answers
not-available before the dataset is asked
([0048](decisions/0048-providers-are-published-samples-are-copied.md)). A changed declaration therefore **must** ship a changed version, or no
repository ever learns it moved; CI enforces that (`mise run kinds:check`,
AGENTS.md).

After that, an installed bundle carries runtime lifecycle **state** the
substrate owns: `disabled`, `uninstalled` and `purging`, each moved by a
`PATCH` of the bundle record
([decision 0033](decisions/0033-the-path-grammar-has-no-separators.md)). Each
transition is guarded by one repository-wide lock: in-flight function and agent
work finishes first, and no new work starts while it runs. A chain of nested
calls acquires the lock once, at its root, and holds it until its last effect
commits, so a nested or cross-bundle call can neither begin after the transition
has returned nor commit behind it.

- **disable** (reversible): the bundle's triggers stop delivering (cursors
  stand still, losing no position) and its functions and agents refuse to run on
  every path — dispatch, the call and chat APIs, host calls, function tools,
  sub-agents. Its config and account records freeze. Its declarations and its
  data otherwise stay untouched, so its kinds keep appearing in the kind
  registry and its other data stays readable and writable. **enable** reverses
  it, and the backlog delivers.
- **uninstall**: the bundle goes. The owned package's declarations (the
  bundle, its kinds, functions, agents, actors, traits, property types, and
  mappings) are torn down through the same admission an apply uses, and the
  delivery wiring — every trigger referencing the package's callables — is
  tombstoned in the same transaction, before the dropped-callable guard, so a
  full teardown never refuses on its own triggers. Afterwards the kinds leave
  the registry, the callables stop running, and a read of one is a 404.
  Uninstall is governed by the same refuse-with-instances rule a kind drop is:
  while live data records of the package's kinds exist it refuses with a guard
  error carrying the count. Purge first. Uninstall is not reversible;
  re-applying the closure is a fresh install into an empty package.
- **purge**: the explicit destructive verb, refused while the bundle is live
  (disable it first). It tombstones every live data record of the owned
  package's kinds through the ordinary soft-delete path, connected accounts
  first so their OAuth finalizers run to completion against the still-live
  config, config last. It never touches declarations; it clears the data so a
  following uninstall passes the refuse-with-instances guard. The destructive
  order is disable, then purge, then uninstall.

An installed closure that stops admitting under a newer binary is
[quarantined](vocabulary.md#quarantine), exactly as the shipped vocabulary is,
rather than allowed to brick the repository. On a bundle that means its status
reports `installed: false` beside the `quarantined`/`quarantineReason` pair;
re-installing it clears the marker, and uninstall still works on one, resolved
straight from its stored rows.

Status is computed, never stored. `GET …/substrate.reamde.dev/core/bundle/status` answers
every installed bundle under an `{items}` envelope and
`…/substrate.reamde.dev/core/bundle/{id}/status` answers
one: `{id, name, authority, package, installed, enabled, inputs, setup,
accounts, functions, kinds, liveRecords}`, plus the quarantine pair when it
applies. `id` is the package the bundle owns, and `name` and `package` are both
that package's own word.
`inputs` is each declared input's resolution: `{name, kind, record?, via?}`,
where `via` names the matching rule from
[the resolution order above](#what-a-bundle-ships) (`bound`, `default` or
`sole`). `setup` lists what stands between the
bundle and every runtime path it ships (`{code, input?, kind?, record?,
message}` — codes `missing`, `ambiguous`, `dangling`, `oauth-client`,
`provider`), mirrors only refusals dispatch would actually make, and is
empty when the bundle is ready. `POST …/substrate.reamde.dev/core/bundle/{id}/bind` with
`{input, record}` binds an input to a chosen record (empty `record` unbinds).
Disable, enable, uninstall and purge are runtime state the substrate owns
([decision 0033](decisions/0033-the-path-grammar-has-no-separators.md)), so each
is a `PATCH …/substrate.reamde.dev/core/bundle/{id}` carrying the state change:
`{"properties": {"disabled": true}}` disables (and `false` enables),
`{"properties": {"uninstalled": true}}` uninstalls, `{"properties": {"purging":
true}}` purges. Every path on this page hangs off `/api/v1`, and none of them
names a repository: the token does. The CLI faces are `substratectl bundle
list/status/disable/enable/uninstall/purge` (purge takes `--yes`), plus
`substratectl bundle connect` for the consent flow below; install and upgrade
stay `substratectl apply` of the closure.

## Providers

A **provider** is a package a publisher owns: the six shipped ones are
`providers.substrate.reamde.dev/google`, `/github`, `/linear`, `/notion`,
`/whoop` and `/beeper`. The tier is read from the tree the closure came from,
never guessed from an OAuth block, from account kinds, or from the package's
name: a token or webhook provider may declare no OAuth, and an account-shaped
package is not necessarily one.

A provider ships, on top of the usual closure, the pieces the
substrate's OAuth facility recognizes by [trait](data-model.md#traits): an
`accountconfig` kind (the Connection, one record per account, required to carry
`tokenRef`, `tokenStatus` and `grantedScopes`) and a client kind wearing the
`oauth2` trait — client id and secret, nothing else — named by the bundle's
`oauth2.clientInput`, plus the trusted `oauth2:` block on
the bundle. Every host check compares the resolved trait reference
(`substrate.reamde.dev/core/accountconfig` and its siblings), so a bundle's own trait
wearing a core name cannot counterfeit the interface.

A provider's records mirror in as ordinary records of the bundle's own kinds,
under ids composed from the provider's own identifiers
([`host.ids.external`](functions.md)), so a re-sync is an idempotent upsert
rather than a duplicate: syncing the same page twice writes the same rows.

## The OAuth facility

The substrate holds the OAuth engine and the credential store itself, as a host
facility, and bundles declare auth rather than implementing it. Two rules keep
a stored row from ever redirecting a credential.

**Provider endpoints and scopes are trusted manifest metadata, never
config-record properties.** A bundle's `oauth2:` block carries
`authorizationEndpoint`, `tokenEndpoint`, an optional `revocationEndpoint`,
`featureScopes` (a map from a declared boolean toggle to the scopes it
requests), and an optional `emailEndpoint`/`emailProperty` pair, the profile
call the facility reads the account's own address from. Start, callback,
refresh and revoke read endpoints only from this compiled metadata, so a
config-row edit can never point a token exchange at an attacker's server.
Endpoints are validated https (http only for a loopback test provider), and
every `featureScopes` key must name a toggle the bundle declares.

```yaml
oauth2:
  clientInput: client
  authorizationEndpoint: https://accounts.google.com/o/oauth2/v2/auth
  tokenEndpoint: https://oauth2.googleapis.com/token
  revocationEndpoint: https://oauth2.googleapis.com/revoke
  featureScopes:
    enabledContacts:
      scopes:
        - https://www.googleapis.com/auth/contacts.readonly
```

A toggle's entry is an object carrying `scopes:`, never a bare list: a keyed map
of lists is the one shape the property dialect cannot state, since `keyed` and
`repeated` are its two containers and a declaration is one or the other. So the
value takes a field, and a bare list is refused naming it.

**The requested scope set is derived per consent from the account's enabled
toggles**, unioned through `featureScopes`. A toggle that maps to no scope
requests nothing, which is exactly how a declared-but-unwired feature stays off
the wire. The scopes actually granted persist on the account as
`grantedScopes`, so enabling a further stream after the grant landed does not
widen it: the account needs a reconnect before that stream can succeed.

Per-property `writer:` ownership backs this on the row, in three declared
roles: `oauth`, only the facility's own actor, holding `tokenRef`,
`tokenStatus`, `grantedScopes` and the `email` it read from the grant;
`connector`, only installed bundle code, holding a sync's own state
(`syncToken`, `lastSyncedAt`, `syncStatus`); and `owner`, only an owner-tier
actor, holding the feature toggles, `syncFrequency` and `backfillDepth`. The
rule is enforced in the write path for REST, GraphQL, and CLI alike, not just
in the console.

The flow itself is two endpoints. `POST …/oauth/start` takes the
account record's id as `record` and answers the consent URL as `url`; it is
owner-tier only (the three interactive clients, never installed code) and refuses
while the client input does not resolve. The state is HMAC-signed over
the repository, the record, and a random nonce, expires in fifteen minutes, and
is persisted beside a sealed PKCE verifier, so a captured state cannot replay:
the callback consumes it exactly once. The provider redirects the browser to
`GET …/oauth/callback`, which is unauthenticated because that
signed one-time state is the whole authentication, and which exchanges the
code, seals the token in the credential store, and patches the account in one
transaction — a secret-typed `tokenRef`, never a raw token on a record or any
read surface.

The callback answers HTML rather than JSON: a small self-contained page that
posts the outcome back to the console that opened it and closes. Success posts
`{source: "substrate-oauth", ok: true, record}`; failure posts the same shape with
`ok: false` and a `correlation` id, the only thing a failure ever reflects,
joined against the server log — no provider detail reaches the browser. With no
opener, the page falls back to a redirect to the console's registry
(`/registry?connected=<id>`, or `?error=<correlation>` on failure).
`substratectl bundle connect` is the same start endpoint from the command line.

The facility then keeps the grant alive without the bundle: a service loop
trades the refresh token for every credential expiring within ten minutes,
marks `tokenStatus: erroring` when that fails, and skips disabled bundles;
deleting a connected account revokes best-effort against the declared
`revocationEndpoint`, deletes the stored credential, and only then releases the
record. A function never sees any of it — its injected config carries a
resolved `token`, never the client secret or the reference.

## Connections

A **Connection** is one configured provider account: a record of an
`accountconfig`-trait kind. Its operational health lives on the record, held
there by the OAuth facility rather than by hand: the token reference, a
`tokenStatus` (`pending`, `connected` or `erroring`), and the `grantedScopes`
the account actually holds. A single provider can back several accounts, so a
Connection is one account, not one provider. The client record is ordinary
too — several may exist, resolution picks one — and until the client input
resolves to a record carrying its credentials, the status says so and the
OAuth flow refuses.

A connected Google account reads back as:

```yaml
kind: providers.substrate.reamde.dev/google/account
metadata:
  id: george-work
data:
  properties:
    email: george@example.com
    tokenStatus: connected
    grantedScopes:
      - https://www.googleapis.com/auth/contacts.readonly
```

The **Connections** view in the console is a cross-bundle operational
surface over every such account, one row per account, read from the native
`accountconfig` records that integration bundles ship. It pages every
implementor of the `accountconfig` trait, which is a plain query
(`GET …/substrate.reamde.dev/core/trait/{id}/records`, with
`…/trait/{id}/implementors` for the kinds themselves), because implementing a
trait is queryable.

## The catalog

The **catalog** lists everything shipped in the binary, in the two tiers
[0048](decisions/0048-providers-are-published-samples-are-copied.md) draws:
the six **providers** under `kinds/providers.substrate.reamde.dev`, and the
seventeen **samples** under `samples/` (`people`, `tasks`, `messaging`,
`calendar`, `scheduling`, the mneme-ported `health`, `fitness`, `routines`,
`journal`, `places`, `food`, `commerce`, and the worked examples `llm`,
`notes`, `web`, `pebble`, `firecrawl`) a repository takes because creation
seeds `substrate.reamde.dev/core` alone.

The catalog is a read model over the bundle closures baked in, parsed once at
boot: each entry carries `id` (the package it ships), `name`, `authority`,
`package`, `description`, `version`, `tier`, `inputs`, `requires`,
`suggestedMappings` (each with the state it has here, and the ids it lands
under, below), and
`closure`, which previews the `kinds` (each with its description),
`functions`, `agents` and `mappings` it declares plus the `records` the
install writes beside them (a bundle's triggers, the llm example's provider
rows), so the console can show what an install will add before it runs. Every one of those is a record: the
declarations are records of the core meta-kinds, and `records` are ordinary
rows the moment they land. A shipped directory carrying no bundle document is not an entry, and a
malformed one is dropped with a logged warning rather than failing the whole
catalog.

`requires` is the entry's declared vocabulary dependency: the packages its
mappings, references and trigger subscriptions point at. Installing `google` into a
repository that has not imported `people` is **refused** by the ordinary
admission, before anything is touched, with a problem naming what to import
first. Nothing resolves the dependency for you — the order is yours.

`GET …/catalog` lists every shipped bundle under an `{items}`
envelope, each flagged `installed` for this repository;
`GET …/catalog/{id}` is one entry with its closure, and an
unknown id is a 404 `not_found`. The id is a package identity, so it carries a
`/` and a URL percent-encodes it once:
`…/catalog/samples.substrate.reamde.dev%2Ftasks`.

### The two doors

Both doors are thin wrappers over the ordinary apply, never a parallel path:
each applies the entry's closure exactly the way `substratectl apply -f
bundle.yaml -f triggers.yaml` does: the declarations through the batch
[admission](vocabulary.md#admission), the delivery wiring as ordinary records,
both committing as one repository transaction. Each is idempotent, so a
second call changes nothing. Both are refused for any actor outside the three
interactive clients (`api`, `console`, `substratectl`) with a 403, before the
closure is touched: taking bundle code is a person's action.

`POST …/catalog/{id}/install` is the **provider** door. The closure lands
verbatim, under the authority that publishes it, and the version bump the
publisher ships is what the upgrade preview above offers.

`POST …/catalog/{id}/import` is the **sample** door. The closure is REHOMED
first: every mention of `samples.substrate.reamde.dev` in the decoded documents
(the ids, the declared `authority`, the reference pins, `installs` and
`requires`, a function's `writes`, a trigger's selectors, a mapping's
`from`/`to`, and the authority a function's source spells inside its own text
text) becomes this repository's own authority, and only then does the closure meet
admission. A document that still mentions the placeholder afterwards is
refused. So `samples.substrate.reamde.dev/tasks/task` lands as
`ada.example.com/tasks/task`, `source: installed` and writable through the API,
and the bundle record it lands as is `ada.example.com/tasks`, not the id the
request named. A sample is never offered an upgrade: what it landed belongs to
the repository. `requires:` is rehomed with everything else, so importing
`tasks` before `people` is refused by the ordinary admission naming
`<your authority>/people`, the sample to import first.

`import` on a provider id is refused naming `install`. `install` on a sample id
still admits the closure verbatim, under the placeholder authority: nothing
needs it now that no provider names a sample package, but a repository that
wants the shipped vocabulary under the shipped authority may still ask for it.

**No provider requires a sample package, and every provider installs on a bare
repository.** A provider ships mirror kinds in its own shape and writes nothing
else: no `requires:`, no reference pinned at a sample kind, no core row. So the
console can install any of the six from a repository that has imported nothing,
and the Registry's Install button is never disabled for a missing requirement.

What reaches a `person`, an `emailmessage` or a `task` is the mapping the
repository declares, from a mirror onto a kind of its own. A mapping onto a
kind is the declaration of the package that owns that kind
([decision record 0049](decisions/0049-the-owner-of-a-mappings-target-declares-it.md)),
so every mirror ships an unpinned, empty subject slot and the repository fills
it. Until it does, the mirrors sync and the slots stay empty, which is the
whole of what an install delivers.

### Suggested mappings

A sample ships the mappings the repository would otherwise have to write. The
`people` sample declares five, onto its own `person`: from GitHub's `user`,
from Google's `contact` and `emailaddress`, and from Linear's `user` and an
issue's `assignee`. The `tasks` sample declares one, from Linear's `issue`
onto its own `task`. They are the sample's declarations, under its package, so
an import lands them as yours: edit them, delete them, version them like
everything else you own.

A suggested mapping names a kind in a package you may not have, and admission
refuses a mapping whose source kind is absent or shaped wrong. So **the import
is conditional**: a suggested mapping (and its `installs:` entry) is admitted
only where this repository can resolve it, and dropped otherwise. Importing
`people` onto a repository with no provider lands three kinds and no mapping,
rather than being refused for vocabulary you never asked for.

Every door and every surface says which of four states each mapping is in. The
state is the MAPPING RECORD's, not the provider's: installing GitHub lands
mirror kinds and nothing else, so "GitHub is installed" and "GitHub identities
reach my people" are two different answers.

| State     | What it means                                       | What lands it            |
| --------- | --------------------------------------------------- | ------------------------ |
| `landed`  | the declaration is here; the projection runs        | nothing left to do       |
| `ready`   | the provider is here and the mapping fits it        | import the sample again  |
| `waiting` | the provider package is absent                      | install it, then import again |
| `blocked` | the provider is older than the mapping needs        | upgrade it, then import again |

`blocked` carries the resolution problems with it (a subject slot or a mapped
property the installed version does not declare), so a mapping the shipped
sample outgrew names what to fix instead of failing the import.

**Installing a provider does not land a mapping.** Import the sample AGAIN,
which is what applies the closure with the mapping in it. That second import
REPLACES the package rather than merging into it, the cost every re-import
carries
([0048](decisions/0048-providers-are-published-samples-are-copied.md)): a kind
or a property you added since is dropped by it, or the narrowing guard refuses
it while live records hold the old shape. Every surface that offers the
re-import says so: the console's Registry puts an **Import again** action on a
held sample whose mapping is `ready` and states the replacement in the
confirmation, and `substratectl import` prints the same warning beside each
line.

**A landed mapping changes how you may write its target.** Nothing external
names a subject, so once a mapping points at your `person` or your `task` their
ids are server-assigned: `apply -f` of a new task carrying
`metadata.id: groceries` is refused from that moment on (0049, `checkCreateID`).
Addressing a record that already exists is not naming one, so an update by id
keeps working.

**No sample ships a mapping onto a message, a thread or a calendar event**, and
that is a limit rather than an omission. A mapping's only creator is a shell
mint, a bare subject with no references, and `emailmessage` declares a required
`thread` reference a shell cannot fill (0049 records it). A repository that
wants its own message or event rows writes them from a function of its own,
reading the mirrors.

What lands either way is a copy: the bundle's own declarations, written into
this repository's changelog under `bundle:<authority>:<package>`. So the
catalog is the source and the changelog is the truth, and nothing on the
serving path reads the catalog again. The response is the landed bundle's
computed status. Uninstall, disable, enable, and purge are the lifecycle verbs
above; the catalog does not duplicate them.

The CLI faces are `substratectl import <sample>` for the sample door, and
`substratectl apply -f <files> --as <authority>`, which runs the same rehoming
client-side over files on disk.

The shipped bundles, one by one (what each declares, its functions and its
triggers), are the [Bundles catalog](bundles-catalog.md).

Next: [functions](functions.md), the callables a bundle ships, and the
host SDK.
