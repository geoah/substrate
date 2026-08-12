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
owns**. An integration or capability bundle owns a categorized authority,
`<name>.bundles.substrate.reamde.dev`; a vocabulary bundle owns a plain
organization-style label like `people.substrate.reamde.dev`. Either way the
authority is what marks a vocabulary installed rather than
[shipped](builtin-kinds.md). `installs:` lists the exact references of everything the closure
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
  configType: web.bundles.substrate.reamde.dev/config
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
authority manifest declares it` — and the rule cuts both ways, because a
`*.bundles.substrate.reamde.dev` authority declaring no bundle document is a closure with
no owner and is refused too.

Two more fields matter, both covered below: `configType`, the bundle's one
configuration kind — declared in its own authority, implementing the
`bundleconfig` trait, and the only such kind it ships — and an optional
`oauth2:` block of trusted provider endpoints and scopes. A bundle may also
carry `modules:`, inline shared library source its functions import
([Functions](functions.md#shared-modules)); modules are sources rather than
closure members, so they never appear in `installs:`.

The installed callables react through the ordinary machinery: a bundle's
functions and agents run on the same [changelog](changelog.md) every other write
lands in, its [mappings](projection.md) fold provider records onto your people
and tasks, and its writes carry their own machine hands — `function:<name>` for
a callable's effects, `bundle:<name>` for the declarations an install lays down
([actors](api.md#actors)).

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

After that, three lifecycle verbs act on an installed bundle, each guarded
by one dataset-wide fence so in-flight work drains first and nothing new admits
after. An invocation tree takes the fence once at its root and holds it through
its last effect, so a nested or cross-bundle call can neither begin after a
verb has returned nor commit behind it.

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
**quarantined** rather than allowed to brick the repository. Repository open
installs the maximal admissible subset, leaves the rest out of the live registry
— so their kinds refuse writes and their callables do not run — and marks each
with `quarantined: true` plus the admission error as `quarantineReason`. A
quarantined bundle reports `installed: false`; re-installing it clears the
marker, and uninstall still works on one, resolved straight from its stored
rows. It is the same [quarantine](vocabulary.md#quarantine) the shipped vocabulary
gets.

Status is computed, never stored. `GET …/core.substrate.reamde.dev/bundles/status` answers
every installed bundle and `…/core.substrate.reamde.dev/bundles/{id}/status` answers
one: `{id, name, authority, configType, installed, enabled, configured,
accounts, functions, kinds, liveRecords}`, plus the quarantine pair when it
applies. The verbs are `POST …/core.substrate.reamde.dev/bundles/{id}/disable` and its
`enable`, `uninstall` and `purge` siblings. Every path on this page hangs off
`/api/v1`, and none of them names a repository: the token does. The CLI faces
are `substratectl bundle list/status/disable/enable/uninstall/purge` (purge takes
`--yes`), plus `substratectl bundle connect` for the consent flow below; install and
upgrade stay `substratectl apply` of the closure.

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
`tokenRef`, `tokenStatus` and `grantedScopes`), the `bundleconfig` kind named
by `configType`, and, when it speaks OAuth, the `oauth2` trait on that config
kind — client id and secret, nothing else — plus the trusted `oauth2:` block on
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
  authorizationEndpoint: https://accounts.google.com/o/oauth2/v2/auth
  tokenEndpoint: https://oauth2.googleapis.com/token
  revocationEndpoint: https://oauth2.googleapis.com/revoke
  featureScopes:
    enabledContacts:
      - https://www.googleapis.com/auth/contacts.readonly
```

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

The flow itself is two endpoints. `POST …/core.substrate.reamde.dev/oauth/start` takes the
account record's id as `record` and answers the consent URL as `url`; it is
owner-tier only — the three human doors, never installed code — and refuses
while the bundle still needs configuration. The state is HMAC-signed over
the repository, the record, and a random nonce, expires in fifteen minutes, and
is persisted beside a sealed PKCE verifier, so a captured state cannot replay:
the callback consumes it exactly once. The provider redirects the browser to
`GET …/core.substrate.reamde.dev/oauth/callback`, which is unauthenticated because that
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
Connection is one account, not one provider. Creating a second live record of a
`bundleconfig`-trait kind is refused with a guard error: there is exactly one
configuration record per bundle, and until it exists the bundle reads
"needs configuration" and the OAuth flow refuses.

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
(`GET …/core.substrate.reamde.dev/traits/{id}/records`, with `…/traits/{id}/implementors`
for the kinds themselves), because implementing a trait is queryable.

## The catalog

The **catalog** lists everything shipped in the binary and ready to install —
the bundles, and the five **vocabulary bundles** (`people`, `tasks`,
`messaging`, `calendar`, `media`) a repository imports because creation seeds
`core.substrate.reamde.dev` alone. A vocabulary bundle ships kinds and nothing else: no
config type, no functions, no OAuth. Its entry carries `vocabulary: true`.

The catalog is a read model over the bundle closures baked in, parsed once at
boot: each entry carries `id`, `name`, `authority`, `description`, `version`,
`configType`, `requires`, `vocabulary`, the `integration` facet above, and
`resources`, which previews the `kinds`, `functions`, `agents`, and `triggers`
the closure installs, so the console can show what an install will add before
it runs. A shipped directory carrying no bundle document is not an entry, and a
malformed one is dropped with a logged warning rather than failing the whole
catalog.

`requires` is the entry's declared vocabulary dependency: the authorities its
mappings, edges and trigger subscriptions point at. Installing `google` into a
repository that has not imported `people` is **refused** by the ordinary
admission, before anything is touched, with a problem naming what to import
first. Nothing resolves the dependency for you — the order is yours.

`GET …/core.substrate.reamde.dev/catalog` lists every shipped bundle under a `catalog`
key, each flagged `installed` for this repository;
`GET …/core.substrate.reamde.dev/catalog/{id}` is one entry with its resources, and an
unknown id is a 404 `not_found`.

Installing from the catalog is a thin wrapper over the ordinary apply, never a
parallel path: `POST …/core.substrate.reamde.dev/catalog/{id}/install` applies the entry's
closure exactly the way `substratectl apply -f bundle.yaml -f triggers.yaml` does —
the declarations through the batch admission, the delivery wiring as ordinary
records, both committing as one repository transaction — and it is idempotent,
so a second install changes nothing. It is refused for any actor outside the
three human doors (`api`, `console`, `substratectl`) with a 403, before the closure is
touched: installing bundle code is a person's action. What lands is a copy —
the bundle's own declarations, written into this repository's changelog under
`bundle:<name>` — so the catalog is the source and the changelog is the truth, and
nothing on the serving path reads the catalog again. The response is the
installed bundle's computed status. Uninstall, disable, enable, and purge
are the lifecycle verbs above; the catalog does not duplicate them.

The shipped bundles, one by one (what each declares, its functions, its
triggers, and whether it is an integration or a capability bundle), are the
[Bundles catalog](bundles-catalog.md).

Next: [functions](functions.md), the callables a bundle ships, and the
host SDK.
