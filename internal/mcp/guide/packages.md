# Packages and the catalog

Packages group declarations, govern who may change them, and bring collections, providers, tools and agents into the repository.

## Ownership and writes

A package is the unit an authority versions and owns, named `<authority>/<package>`. Its kinds are named `<authority>/<package>/<name>`. A kind can pin its own version; otherwise it inherits the package's. The repository's own packages live under `~`, its local namespace: `~/projects`, with kinds `~/projects/project`. The repository's full name is accepted in place of `~` on input, and every answer says `~`.

The stored package's `source` governs declaration writes:

- `builtin`: seeded by the server and changed by its shipped upgrades. An assistant cannot edit its declarations through `apply` or `write`.
- `published`: installed under the publisher's authority. Its declarations change by adding it again (its upgrade), not ordinary writes.
- `installed`: the owner's declarations, including added samples. An assistant may change them within its access. New packages go under `~`, for example `~/projects`.

These restrictions protect declarations, not every data record of their kinds. Ordinary data writes still follow the kind's rules and the assistant's access restrictions. A `substrate.reamde.dev/console/collectionview` record, for example, can be written even though its kind's declaration belongs to a builtin package. Secrets, credentials and tokens are not assistant write surfaces.

## Inspect before taking a package

Use `packages` with `{"action":"catalog"}` to list entries, then `{"action":"item","id":"substrate.reamde.dev/health"}` for one detail. The catalog's `keptBy` is `repository` for a sample, which is copied, or `publisher` for a published package; it is distinct from a stored package's `source`.

An entry describes:

- `id`, `authority`, `package`, `description`, `version` and `keptBy`.
- `provider`: whether it connects to an outside service. A published catalogue is not necessarily a provider.
- `brings`: any of `account`, `tools`, `collections`, `metrics`, `agents`, derived from its declarations and records.
- `addonFor`: the provider an add-on extends.
- `requires` and `requiresAtLeast`: required packages and minimum versions.
- `inputs`: named configuration needs, with `kind`, optional `inject` and `description`.
- `closure`: what taking it lands; `suggestedMappings`: conditional mappings and their state here.
- `installed`, and, where applicable, `upgrade`, `origin`, `originVersion`, `modified`.

Do not infer provider status from `keptBy`. Inspect the entry, especially its requirements and upgrade blockers.

## Samples: add a copy

`packages` action `add` on a sample copies its declarations and shipped data into `~`, rewriting references with them. The landed package is writable and remembers its catalog origin.

**Adding a sample does not add the samples it requires.** Add each required sample first, respecting dependency order. For the tasks sample, these are three separate `packages` calls:

```json
{"action":"add","id":"samples.substrate.reamde.dev/people"}
```

```json
{"action":"add","id":"samples.substrate.reamde.dev/scheduling"}
```

```json
{"action":"add","id":"samples.substrate.reamde.dev/tasks"}
```

The task kind then is `~/tasks/task`, and its installed bundle id is `~/tasks`, in every repository. Use that landed id for status, inputs and lifecycle actions. Continue using the catalog id for `item` and for adding it again. A fresh repository does not already have these sample collections.

## Published packages: add

`packages` with `{"action":"add","id":"substrate.reamde.dev/labs"}` on a published package keeps the publisher's authority. It adds available published requirements first, transitively, including requirements held below the requested floor. Required samples are not added for it. Missing or insufficient requirements still cause admission to refuse; dependencies may have committed before a later refusal.

The well-known catalogues are `substrate.reamde.dev/health`, `substrate.reamde.dev/labs`, `substrate.reamde.dev/environment` and `substrate.reamde.dev/clinical`. They are not seeded. The labs catalogue requires health; clinical requires labs. Provider packages can bring these catalogues as dependencies.

Connecting an account is the owner's work on the console's Providers page. Installing its vocabulary does not connect the account. Uninstalling and purging packages are also the owner's actions and are not `packages` tool actions.

## Bundles, inputs and status

A bundle is the install unit, represented by `substrate.reamde.dev/core/bundle`. Its id is the package it owns. Read `packages` action `status`, with an installed package `id` or without one for all statuses, before diagnosing configuration or execution.

Each declared input resolves one record in this order:

1. An explicitly bound reference.
2. A record of the input kind whose id is `default`.
3. The sole record of that kind.

Several candidates are not a tie the server arbitrarily breaks. Inspect the input's declared kind, write an appropriate record where permitted, then bind it explicitly. `packages` action `bind` takes `id` (the installed package), `input` (the declared input name), and `record` (the full `<authority>/<package>/<kind>/<id>` path). Do not invent an input name or put configuration directly on the bundle.

`enable` and `disable` take the installed package id. Disabling fences execution: its callables refuse invocation and its triggers stop advancing. Data stays; ordinary data remains readable and writable, while its input configuration and accounts are frozen. Enabling restores execution eligibility, not missing credentials or other unmet requirements.

A quarantined package has a stored declaration closure the loader could not admit at repository open. Its bundle status remains inspectable even though its declarations are absent from the live registry. Read the reported problem rather than repeatedly invoking its functions. Ownership protection still applies: repair a published closure by adding it again, not by hand-edited declarations.

## Upgrades and mappings

`packages` action `upgrades` previews shipped changes. A catalog `item` also reports what taking that entry would change and any blockers. A sample and a published package both take their offered update through `add` again. Check `modified` before replacing an owner's edited sample. Do not treat an upgrade offer as proof that admission will succeed, or work around a lossy-conversion refusal. An update whose preview lists `retentionShortens` keeps those kinds' history for less time: `add` refuses it, naming each kind, and the person takes it on the package's page in the console, where each is listed for review. Tell them; do not take it another way.

`substrate.reamde.dev/core/recordmapping` declares how source records map onto target records. Catalog suggested mappings can wait for a source package. For a hand-authored batch, `apply` with `holdWaitingMappings: true` holds back suggested mappings whose source kind is absent from both the repository and the batch, commits the rest, and reports `heldMappings`. Use the same flag with `plan: true` to preview. A present but incompatible source is not held back; it still refuses the batch. Without the flag, a missing source can refuse the whole batch.

The server seeds `substrate.reamde.dev/core`, `substrate.reamde.dev/llm`, `substrate.reamde.dev/console`, `substrate.reamde.dev/observations`, `substrate.reamde.dev/assistant` and `substrate.reamde.dev/applets`. The observations package provides shared kinds, not the catalogues' metric records. Read `guide` topics `vocabulary`, `automation`, `agents` and `observations` for the next layer.
