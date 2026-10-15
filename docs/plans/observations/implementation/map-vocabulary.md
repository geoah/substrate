# Codebase map: value types, traits, samples, docs, decisions

Snapshot of 2026-10-11, taken by a read-only agent for the observations
build. Paths are relative to the repository root. Line numbers drift; the
names are the point.

The money type (PR #751, commit cf45a786, 41 files) is the closest pattern
for `quantity`: an object-shaped value with its own lookup table. Its file
list is the checklist.

## 1. Property value types

Declaration (`internal/vocabulary`):

- `types.go:25-98` holds the `Datatype` constants. `builtinKinds`
  (`:128-135`) is the gate that admits a type name. It is used at
  `load.go:2097` (scalar type or refinement base), `load.go:490` (a trait's
  property types) and `load.go:639` (a trait variant's types). `quantity`
  must go in `builtinKinds`, or the `observed` trait cannot contract a
  `quantity` property.
- `types.go:139-148`: `shortStringKinds` and `IsLongText` decide full-text
  search and the `match` operator. Money is in neither set.
- `money.go` holds the value shape in one place: the member-name constants
  (`:20-23`), the currency table (`:39-73`), the lookups `CurrencyDecimals`
  and `Currencies`, the title renderer `FormatMoney` (`:112`) and `moneyInt`
  (`:132`). A unit table belongs in a similar `quantity.go`.
- `load.go:1924` is `parseProperty`; the scalar branch is `:2092-2189`; the
  allowed keys are `propKeys` at `:1785`. The money-only refusal at `:2140`:
  `a money property takes min and max, never a pattern or values`. A
  declared `default:` is checked in `parseDefault` (`:2281`); the money case
  is `:2316-2322`.
- Four maps refuse types in particular positions: link data
  (`linkPropForbiddenKinds`, `:1201`), `unique` (`:1823`), object fields
  (`:1905`) and keyed maps (`:1916`). Money is in none of them.
- Core meta-kind: `kinds/substrate.reamde.dev/core/propertytype.yaml:54-79`
  is the `base` enum, with `money` at `:64`. Adding a value changes the
  definition, so bump its `version: 13` at `:13`.

Write validation (`internal/engine/validate.go`):

- `coerceScalar` (`:450`) dispatches object-shaped and exact types before
  the string path (`DatatypeDecimal` to `coerceDecimal`, `DatatypeMoney` to
  `coerceMoney`).
- `coerceDecimal` is at `:650`. It refuses a bare JSON number and stores a
  canonical string via `canonicalDecimal` (`:713`); reuse that for
  `quantity.value`.
- `checkExactRange` (`:670`) and `boundDigits` (`:696`) compare `min` and
  `max` exactly.
- `coerceMoney` (`:742-779`): refuses any member other than the two,
  requires `amount` as an integer that is not a string, and checks the
  currency against the table.
- Titles: `titleResolver.Prop` `:919-922` and `First` `:1125-1130` call
  `FormatMoney`; an object otherwise renders as an empty string.
- Search bands are built in `ftsBands` (`:1233-1259`).

Storage and indexing:

- Values live in the `props` jsonb column. The generic indexes are
  `records_props_idx` (GIN, jsonb_path_ops) and `records_fts_idx` at
  `internal/engine/migrations/0001_init.up.sql:114,117`.
- A kind's declared `indices:` go through `indices.go:371`
  (`indexStatements`) and `:442-480` (`indexExpr`). Every property gets
  `props->>name`, except a single reference (`:476`). Money has no special
  expression, so a quantity index needs one here.

Filters and ordering (`internal/engine/query.go`):

- Routing happens at `:890-906`. A keyed money property falls back to the
  generic path (`:897`), and money goes to `condMoney` (`:903`).
- `moneyValueSQL` (`:915`) and `currencyDecimalsSQL` (`:925`) compute the
  exact number in SQL.
- `condMoney` (`:944`) refuses `prefix`, `match` and `contains`. Each
  operand is itself a money value run through `coerceMoney`, and comparisons
  only hold within the operand's currency.
- `matchRefusal` is at `:1004`. `condJSON` (`:1520`) applies per-type casts
  at `:1537-1577`.
- Ordering is at `:1690-1722`, using `numericProp` `:1347`, `moneyProp`
  `:1366` and `datetimeProp` `:1386`. All three use the rule that every kind
  declaring the name must agree on the type.

Narrowing guard when a declaration changes (`schemadiff.go`):
`patternApplies` `:1276-1284`, `boundsApply` `:1298-1301` and
`boundPredicate` `:1393-1413`, which has the money arm. The type-change
rules are at `:417-445`.

Every Go file that switches on the type name (and needs `quantity`):
`vocabulary/types.go`, `vocabulary/load.go`, `engine/validate.go`,
`engine/query.go`, `engine/schemadiff.go`, plus `engine/indices.go` if
quantity gets an index expression. `internal/api/fake_test.go:531,564`
switches on `"money"` as well.

Tests to copy: `vocabulary/money_test.go`; `currency_pin_test.go` (the table
may only grow); `currency_console_test.go` (it reads the console's
`money.ts` `CURRENCY_DECIMALS` and requires the two tables to match);
`engine/money_db_test.go` (`:81-255`); `validate_internal_test.go`;
`dialect_narrowing_db_test.go`.

Console. `web/console/src/lib/api/types.ts` does not list the types.
`PropSpec.kind` is a free string (`record-schema.ts:71-119`). The sites that
switch on `"money"` or `"decimal"`:

- `lib/record-schema.ts`: type sets `NUMERIC` `:154`, `OBJECT` `:166`;
  controls and seeds `controlFor` `:442`, `exampleFor` `:503`, `seedValue`
  `:543`; checks `:682` (money), `:732-787` (string-shaped types); text
  round trip `formatValue` `:864`, `parseScalarText` `:927`.
- `lib/money.ts` (the table plus parse and format helpers).
- `lib/format.ts:166` recognises money by the shape of the value (`isMoney`).
- `lib/filters.ts:80` and `lib/record-form.ts:121-127`.
- `components/data-table/property-icon.ts:36`;
  `property-sheet/sheet-model.ts:147,189,219`;
  `property-sheet/property-value.tsx:110`.
- `components/identity/money-value.tsx` and `components/record/money-input.tsx`.
- `record/property-field.tsx:502,682`; `property-sheet/inline-editor.tsx:77,271-320`;
  `property-sheet/list-editor.tsx:222,287`.
- `data-table/data-table-filters.tsx:221`; `pages/kind-browse-columns.tsx:163,176,326,568`;
  `changelog/value-moves.tsx:66`.

## 2. Traits

A full example, core's `sync` (`kinds/substrate.reamde.dev/core/core.yaml:263-284`):

```yaml
kind: substrate.reamde.dev/core/trait
metadata:
  id: substrate.reamde.dev/core/sync
data:
  authority: substrate.reamde.dev
  package: core
  description: "the synchronization a function drives on one connected account: ..."
  properties:
    syncState: string
    lastSyncedAt: datetime
    syncProgress: json
```

All six core traits are at `core.yaml:119-284`. The allowed keys are
`capabilityDataKeys` (`load.go:277`): `authority`, `package`, `oneOf`,
`properties`, `description`. A trait has no version of its own and moves
with core's package version (`core.yaml:49,58`, currently 38).

How a kind binds one: a `traits:` list of full identities, e.g.
`samples/tasks/task.yaml:20-23`,
`- "substrate.reamde.dev/core/temporal(point: dueAt)"`.

What the loader checks:

- Trait parsing is at `load.go:462-506`. A trait's property types must be in
  `builtinKinds` or be `reference` (`:490`).
- Bindings are parsed at `:961-987` (the binding grammar `reCapBinding` is
  at `:39`) and resolved by `bindCapability` (`:1564-1628`).
- The contract check runs after all packages load (`:2836-2851`): only
  presence and type are checked (`trait %s requires property %s`, `trait %s
  requires type %s, got %s`). `required` and `repeated` are not. A
  refinement passes because its `Datatype` is its base type. A bare trait
  name is refused (decision 0098).

Engine code that keys on traits:

- Constants are in `vocabulary/bundle.go:26-65`, in pairs: `TraitSync =
  "sync"` and `TraitSyncCore = PackageCore + "/" + TraitSync`. Add
  `TraitObserved` and `TraitObservedCore` the same way.
- Users: `engine/sync.go:73,174,218,290,345`; `engine/write.go:216,1575`;
  `engine/window.go:101,171,217,242` and `internal/window/window.go:467,529,553`;
  `engine/invocationconfig.go:117,226,229`; `engine/oauth.go:167,213`;
  `engine/bundles.go:195,227,674,1012`.
- Helpers: `Kind.Implements` (`types.go:787`), `Registry.Implementing`
  (`:1161`) and `ImplementingStrict` (`:1172`). Test pattern:
  `vocabulary/traitcore_test.go`.

`mise run kinds:check` (`.mise/kindscheck.sh`, `cmd/vocabularydiff`):

- It diffs `kinds/` and `samples/` against the merge base. A changed
  declaration (data minus `version`) must have a higher effective version:
  the kind's own `data.version`, else the package version from its
  `bundle.yaml` or package document (`main.go:246-279`).
- It does not check added declarations. Precedent still bumps core for an
  addition: version 38 came with the `alert` kind (`core.yaml:56-58`). Bump
  core and add a comment line there.
- Names gate (`names.go`): a new kind id may not contain a dead word from
  `terms.md`, and a name ending like a family member must start with the
  family's stem. Some dead words (`log`, `type`, `group`) are allowed inside
  a compound, but not alone or glued to a live word (`honestCompounds`, `:102`).

## 3. Samples

Layout, using `samples/tasks/`:

- `bundle.yaml` holds the package document (`version: 13`) and the bundle
  document, which has `requires:`, `requiresAtLeast:` (lowest version per
  required package) and `installs:` listing every identity (`:15-51`).
- One file per kind (`task.yaml`, `project.yaml`, `tasklog.yaml`) plus
  `mappings.<provider>.yaml`.
- A package that only declares a trait: `samples/scheduling/` (`occurrencelog.yaml`).

Kind patterns (`task.yaml`): `displayTemplate` `:30`, `indices` `:34-37`,
enum with `default` `:56-65`, `state` with transitions and `stamps`
`:94-114`, `reference` with `kind:` and `mustExist` `:119-133`; `onDelete:
cascade` is at `tasklog.yaml:44-50`.

Embedding: `samples/samples.go:31-32` (`//go:embed all:<dir>`). A new
directory must be added there.

Tests:

- `kinds/kinds_test.go`: `:27` every YAML on disk must be embedded; `:98`
  the seed and catalog both load; `:268` every kind needs a
  `displayTemplate`; `:308` `purpose` must be valid; `:457` everything
  installs in `requires` order; `:522` every sample re-homes onto a
  repository authority.
- `internal/vocabulary/vocabulary_test.go:3319`: `shippedVocabularyDirs`
  lists the kinds-only samples.
- `web/console/src/lib/shipped-kinds.test.ts:63-99`: each kind name must
  split into words listed in `kind-names.ts:39` (`WORDS`), and each
  description must open with a plain sentence.
- Catalog tests: `internal/catalog/catalog_test.go:140`.

## 4. Docs and the docs linter

- Type table: `docs/data-model.md:300-341`; the Money paragraph to copy is
  `:372-392`.
- Filters and ordering: `docs/api.md:536-540,597`. Narrowing bounds:
  `docs/vocabulary.md:394-397`.
- Traits: `docs/traits.md:201+` ("Where traits do work").
  `docs/builtin-kinds.md:124` says "the six shipped traits" and needs
  updating; its sample tables are `:21-86`, and `:10-12` lists the samples it
  does not table.
- `docs/terms.md` rows are `| **term** | ... |`; the "Dead words" paragraph
  is `:6-15`. `docs/README.md` must link every `docs/*.md` page.
- `.mise/docscheck.sh` (run by `lint:docs`) refuses: in docs prose
  `tenant(s)`, `relationship(s)`, `singleton(s)`, `configType`, `edge(s)`
  (so "edge case" fails), `entity/entities`, `integration(s)`; `terms.md`
  is exempt; in `description:` fields under `samples/` the same words; in
  seeded `kinds/substrate.reamde.dev` descriptions additionally `body`,
  `host`, `trip`, `touch`; text inside backticks is skipped. Also:
  `apiVersion:` and `spec:`, GraphQL, `mise run` names that do not exist,
  plural collection URLs, and the decision-record shape.

## 5. Decision records

- `docs/decisions/template.md`: frontmatter `status: proposed`, `date`,
  `decision-makers`; sections Context and Problem Statement, Considered
  Options, Decision Outcome (with Consequences: Good/Bad, and Confirmation),
  More Information.
- Index row in `README.md` (table from `:168`):
  `| [NNNN](NNNN-kebab.md) | Title | proposed |`. The status there must
  match the frontmatter.
- The highest number on 2026-10-11 is 0152. Run `git fetch --prune && mise
  run decisions:check` before using the next one.
