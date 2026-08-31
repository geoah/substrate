# Bundles

A **bundle** is the unit of installation: a closure of declarations and
behavior that teaches the substrate something new, applied as one unit and
removable as one unit. Installing the URL harvester adds functions and agents;
installing a provider like Google adds account access and sync; installing a
vocabulary bundle adds kinds and rules. All three are bundles — there is no
second word for them, on the wire or anywhere else.

Two narrower words appear beside it, and neither is a synonym:

| Word            | Means                                                                  |
| --------------- | ---------------------------------------------------------------------- |
| **integration** | a bundle whose job includes an ongoing connection to an outside provider — a curated catalog facet, not a separate kind of thing |
| **account**     | one configured connection to such a provider: a record of an `accountconfig`-trait kind. The console lists these under **Connections** |

## What a bundle ships

A `bundle` document wears the ordinary envelope — `kind:`, `metadata:`,
`data:`, and the server-owned `status:` — and declares the one **authority it
owns**. A bundle that ships behavior may own any legal authority. The shipped
tree names them `<name>.bundles.substrate.reamde.dev` by convention, but the
loader does not require that shape. A vocabulary bundle owns a plain
organization-style label like `people.substrate.reamde.dev`, and that shape
is the one the loader does check, because it is what marks a vocabulary
[shipped](builtin-kinds.md) rather than installed. The authority's **first
label** is the bundle's name — its `metadata.id` suffix and the prefix an
installed kind's GraphQL name carries — so it must be one lowercase word. Two
bundles may share it: an install writes under `bundle:<authority>`, so they
stay two writers, and what is refused instead is one GraphQL name claimed
twice. `installs:` lists the exact references of everything the closure
ships: its [kinds](data-model.md#kinds-and-references),
[traits](data-model.md#traits),
[property types](data-model.md#property-types),
[record mappings](projection.md), [functions](functions.md), and
[agents](agents.md). The harvester's document, its description elided:

```yaml
kind: core.substrate.reamde.dev/bundle
metadata:
  id: web.bundles.substrate.reamde.dev/web
data:
  authority: web.bundles.substrate.reamde.dev
  inputs:
    connector:
      kind: web.bundles.substrate.reamde.dev/config
      inject: functions
  installs:
    - web.bundles.substrate.reamde.dev/config
    - web.bundles.substrate.reamde.dev/page
    - web.bundles.substrate.reamde.dev/findurls
    - web.bundles.substrate.reamde.dev/fetchpage
    - web.bundles.substrate.reamde.dev/setclass
    - web.bundles.substrate.reamde.dev/stampconfig
    - web.bundles.substrate.reamde.dev/pageclassifier
    - web.bundles.substrate.reamde.dev/readinglistagent
    - web.bundles.substrate.reamde.dev/weeklyrollup
```

The document's own id is the authority followed by its first label
(`web.bundles.substrate.reamde.dev/web`), and every entry in `installs:` is a reference of
the same shape, never a bare name. The bundle document never travels alone: the
[`authority`](vocabulary.md) document that brings the authority into being, and
every member `installs:` names, belong to the same apply. A closure applied
without that document is refused — `authority web.bundles.substrate.reamde.dev: no
authority manifest declares it` — and an authority wearing the shipped tree's
`*.bundles.substrate.reamde.dev` convention while declaring no bundle document is
refused too: a closure with no owner there is a forgotten document far more
often than a deliberate name.

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
`function:<authority>:<name>` for a function's effects,
`agent:<authority>:<name>` for an agent's, `bundle:<authority>` for the
declarations an install lays down ([actors](api.md#actors)).

## Install and lifecycle

Installing is one atomic apply of the whole closure: the same admission as any
[vocabulary batch](vocabulary.md#admission), every document admitted or none, active
on commit. The apply replaces the owned authority whole, and the loader holds
`installs:` equal to what the authority actually declares, both ways, so
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

The upgrade has a read-only **preview** beside it: the catalog compares the
shipped closure's declaration versions against the stored ones (the same diff
the boot upgrade runs for core, engine `PlanBundleUpgrade`) and attaches the
result to the catalog read as `upgrade`, with the same refuse-breakage guard
lines the install would refuse on as `blockers`. The console's Registry counts
these on the sidebar badge, offers Upgrade where nothing blocks, and states
the guard lines where something does; the button is the install verb,
unchanged. A changed declaration therefore **must** ship a changed version, or no
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
- **uninstall**: the bundle goes. The owned authority's declarations (the
  bundle, its kinds, functions, agents, actors, traits, property types, and
  mappings) are torn down through the same admission an apply uses, and the
  delivery wiring — every trigger referencing the authority's callables — is
  tombstoned in the same transaction, before the dropped-callable guard, so a
  full teardown never refuses on its own triggers. Afterwards the kinds leave
  the registry, the callables stop running, and a read of one is a 404.
  Uninstall is governed by the same refuse-with-instances rule a kind drop is:
  while live data records of the authority's kinds exist it refuses with a guard
  error carrying the count. Purge first. Uninstall is not reversible;
  re-applying the closure is a fresh install into an empty authority.
- **purge**: the explicit destructive verb, refused while the bundle is live
  (disable it first). It tombstones every live data record of the owned
  authority's kinds through the ordinary soft-delete path, connected accounts
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

Status is computed, never stored. `GET …/core.substrate.reamde.dev/bundle/status` answers
every installed bundle under an `{items}` envelope and
`…/core.substrate.reamde.dev/bundle/{id}/status` answers
one: `{id, name, authority, installed, enabled, inputs, setup, accounts,
functions, kinds, liveRecords}`, plus the quarantine pair when it applies.
`inputs` is each declared input's resolution: `{name, kind, record?, via?}`,
where `via` names the matching rule from
[the resolution order above](#what-a-bundle-ships) (`bound`, `default` or
`sole`). `setup` lists what stands between the
bundle and every runtime path it ships (`{code, input?, kind?, record?,
message}` — codes `missing`, `ambiguous`, `dangling`, `oauth-client`,
`provider`), mirrors only refusals dispatch would actually make, and is
empty when the bundle is ready. `POST …/core.substrate.reamde.dev/bundle/{id}/bind` with
`{input, record}` binds an input to a chosen record (empty `record` unbinds).
Disable, enable, uninstall and purge are runtime state the substrate owns
([decision 0033](decisions/0033-the-path-grammar-has-no-separators.md)), so each
is a `PATCH …/core.substrate.reamde.dev/bundle/{id}` carrying the state change:
`{"properties": {"disabled": true}}` disables (and `false` enables),
`{"properties": {"uninstalled": true}}` uninstalls, `{"properties": {"purging":
true}}` purges. Every path on this page hangs off `/api/v1`, and none of them
names a repository: the token does. The CLI faces are `substratectl bundle
list/status/disable/enable/uninstall/purge` (purge takes `--yes`), plus
`substratectl bundle connect` for the consent flow below; install and upgrade
stay `substratectl apply` of the closure.

## Integrations

An **integration** is a bundle whose job includes an ongoing connection to
an external provider. It is a facet, not the umbrella: the harvester and a
vocabulary package are bundles and not integrations, because network access
alone does not make one. Saying "install the Google integration" and "install
the harvester bundle" both stay true.

The facet is explicit catalog metadata, curated per bundle, not inferred: it
is not derived from the presence of an OAuth block, from account kinds, or from
the authority's name. A token or webhook integration may declare no OAuth, and
an account-shaped bundle is not necessarily a provider integration, so the
classification is stated rather than guessed.

A provider integration ships, on top of the usual closure, the pieces the
substrate's OAuth facility recognizes by [trait](data-model.md#traits): an
`accountconfig` kind (the Connection, one record per account, required to carry
`tokenRef`, `tokenStatus` and `grantedScopes`) and a client kind wearing the
`oauth2` trait — client id and secret, nothing else — named by the bundle's
`oauth2.clientInput`, plus the trusted `oauth2:` block on
the bundle. Every host check compares the resolved trait reference
(`core.substrate.reamde.dev/accountconfig` and its siblings), so a bundle's own trait
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
kind: google.bundles.substrate.reamde.dev/account
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
(`GET …/core.substrate.reamde.dev/trait/{id}/records`, with `…/traits/{id}/implementors`
for the kinds themselves), because implementing a trait is queryable.

## The catalog

The **catalog** lists everything shipped in the binary and ready to install —
the bundles, and the eleven **vocabulary bundles** (`people`, `tasks`,
`messaging`, `calendar`, and the mneme-ported `health`, `fitness`,
`routines`, `journal`, `places`, `food`, `commerce`) a repository
imports because creation seeds `core.substrate.reamde.dev` alone. A vocabulary bundle ships kinds and nothing else: no
inputs, no functions, no OAuth. Its entry carries `vocabulary: true`.

The catalog is a read model over the bundle closures baked in, parsed once at
boot: each entry carries `id`, `name`, `authority`, `description`, `version`,
`inputs`, `requires`, `vocabulary`, the curated `integration` and `example`
facets, and `closure`, which previews the `kinds` (each with its description),
`functions`, `agents` and `mappings` it declares plus the `records` the
install writes beside them (a bundle's triggers, the llm example's provider
rows), so the console can show what an install will add before it runs. Every one of those is a record: the
declarations are records of the core meta-kinds, and `records` are ordinary
rows the moment they land. A shipped directory carrying no bundle document is not an entry, and a
malformed one is dropped with a logged warning rather than failing the whole
catalog.

`requires` is the entry's declared vocabulary dependency: the authorities its
mappings, references and trigger subscriptions point at. Installing `google` into a
repository that has not imported `people` is **refused** by the ordinary
admission, before anything is touched, with a problem naming what to import
first. Nothing resolves the dependency for you — the order is yours.

`GET …/catalog` lists every shipped bundle under an `{items}`
envelope, each flagged `installed` for this repository;
`GET …/catalog/{id}` is one entry with its closure, and an
unknown id is a 404 `not_found`.

Installing from the catalog is a thin wrapper over the ordinary apply, never a
parallel path: `POST …/catalog/{id}/install` applies the entry's
closure exactly the way `substratectl apply -f bundle.yaml -f triggers.yaml` does —
the declarations through the batch [admission](vocabulary.md#admission), the
delivery wiring as ordinary records, both committing as one repository
transaction — and it is idempotent, so a second install changes nothing. It is
refused for any actor outside the three interactive clients (`api`, `console`,
`substratectl`) with a 403, before the closure is touched: installing bundle
code is a person's action. What lands is a copy —
the bundle's own declarations, written into this repository's changelog under
`bundle:<authority>` — so the catalog is the source and the changelog is the truth, and
nothing on the serving path reads the catalog again. The response is the
installed bundle's computed status. Uninstall, disable, enable, and purge
are the lifecycle verbs above; the catalog does not duplicate them.

The shipped bundles, one by one (what each declares, its functions, its
triggers, and which facet it carries), are the
[Bundles catalog](bundles-catalog.md).

Next: [functions](functions.md), the callables a bundle ships, and the
host SDK.
