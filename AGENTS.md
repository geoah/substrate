# Agent guide

How to work this repo: the substrate server (`cmd/substrated`), the console
(`web/console`, built into the server image and served by it at `/`), the
CLI (`cmd/substratectl`) and the Apple apps (`apps/`, one folder per app,
the console bundled in a native shell that only speaks the API; see
[its README](apps/README.md)). Provider auth and sync live IN the server — the OAuth
facility plus bundle functions.

**The model, in one paragraph.** One invite code gates who registers, where
one is set. Registering
creates a **repository** and its **user**. The user is an **identity** the
server holds beside the repositories, in a control-plane table and in no
repository: the name they sign in with, one password and a TOTP second
factor the user may skip and turn on later
([0237](docs/decisions/0237-a-second-factor-is-the-owners-choice-and-sign-in-asks-for-the-code-only-of-an-account-that-has-one.md)),
and the repositories it may open. A sign-in names the person, who picks the
repository; a person has one today, named after them unless the
registration named another
([0274](docs/decisions/0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)).
The repository's name IS its one **authority** (a bare label is
completed under the server's host,
[0046](docs/decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md)).
The kinds the user declares and the samples they add live in the
repository's **local namespace**, written `~` (`~/tasks/task`), so the name
never enters their data
([0259](docs/decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)).
Its storage is keyed by an **id** minted at registration (`rnd-…`): the
directory on disk and in the archive, the `repository` column of every table
and the webhook address the server gives out, so no table or path holds the
name
([0261](docs/decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)).
There is no username.
Everything the user has lives in it: an append-only,
strictly sequential **changelog**, checksummed lines in Postgres's
`changelog` table, is the truth and the **records** table is its fold.
Postgres alone confirms a write, and a background pass copies each
repository to its **archive** after the commits, a directory or an
S3-compatible bucket that is the backup and what a restore reads
([0262](docs/decisions/0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)). Tokens are themselves
records, each naming the identity it acts for, and a token
has full access to its repository unless it is an app's: a token an app got
through the authorization flow carries the **scopes** the owner approved and
reaches only those kinds, unless the owner approved it `full` access, which
is a token without scopes like a sign-in's
([0194](docs/decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
[0204](docs/decisions/0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md),
[apps](docs/apps.md)). There are no tenants, no user-managed keys, no sharing
and no roles.

There is no written contract document. **The code is the contract**; the tests
are what it promises. One will be written from the code, small, when someone
needs it.

## Build and run

Toolchain is [mise](https://mise.jdx.dev); `mise install` once, then
`mise tasks` to see everything. The ones that matter:

```bash
mise run build          # bin/substrate
mise run build:cli      # bin/substratectl
mise run test           # the whole Go suite, in its two halves
mise run test:short     # skips every suite that wants a database
mise run test:race      # the short suite under the race detector
mise run test:coverage  # both halves, with a profile -> coverage.out
mise run lint           # every linter: Go, YAML, shell, Python, the docs, the migrations, the sandbox gate, the toolchain pins, the CI scripts, the apps
mise run lint:go        # one of them; :yaml/:shell/:python/:docs/:migrations/:sandboxgate/:toolchain/:ci/:apps are the rest
mise run audit          # the vulnerability scans: govulncheck and pnpm audit
mise run fmt            # every formatter, in place: gofumpt/goimports, yamlfmt, and the apps' swift-format and ktfmt
mise run fmt:check      # the same, as a check — what CI runs
mise run console:dev    # the console on :5173, proxying /api to :8080
mise run console:render <url>  # a running substrate's console in headless Chrome: sign-in, register, four signed-in pages
mise run ci             # every CI job, locally. The whole pipeline.
```

**A bare task name is every check of its kind**, and a suffix narrows it to
one language: `lint` is all ten linters, `lint:go` is one of them; `fmt`
writes Go, YAML, Swift and Kotlin, `fmt:yaml` writes one; `fmt:check` is the
four as a check.
Nothing is reachable only through the aggregate. The console is not one of
those suffixes — it is a second toolchain with its own family (`console:lint`,
`console:fmt`, `console:test`), exactly as `test` is the Go suite and
`console:test` is not in it.

**`fmt` and `lint` cannot disagree.** `fmt:go` is `golangci-lint fmt`, not
bare gofmt: the formatters are configured once, in `.golangci.yml`'s
`formatters:` block (gofumpt with extra rules, goimports with a local prefix),
and both the writer and the checker are that same configuration. If `lint:go`
reports a formatting finding that `mise run fmt` will not fix, that is a bug in
the configuration, not something to hand-patch. The apps' two hold it the same
way (`.mise/appsfmt.sh`): `fmt:swift:check` and `fmt:kotlin:check` run the
writer over a copy and print the diff. swift-format is configured once, in
`apps/.swift-format`, and runs from the open-source Swift the `android:*`
tasks pin, never Xcode's, whose swift-format is another version; ktfmt is the
`:ktfmt` task of `apps/gradle` (kotlinlang style, its version in the Gradle
catalog, locked and verified), with its width and indent in
`apps/.editorconfig`. A file whose first line says `// Generated by …; do not
edit` is its generator's and neither formatter touches it. No CI job runs
either check: none installs that Swift or JDK, and no CI job builds the apps,
so a change to them runs `fmt:check` on a laptop.

**`audit` is its own family, not part of `lint`.** It is the one check whose
answer changes without the tree changing, because it asks a vulnerability
database what is known today: green this morning, red this afternoon, same
commit. It also needs the network, and `lint` is something a laptop can run on
a plane. It has its own CI workflow, `audit.yml`, for the same reason, and
because a red `ci` run on `main` holds back `latest` and every release.

**The YAML is formatted too.** `kinds/` and `samples/` are the contract as
files, so their shape is held the way Go's is: `mise run fmt` writes it, `mise run fmt:check` fails
on it, and the `lint` job runs both. Two rules are worth knowing before you
write a document by hand. Block mappings only — `metadata: {id: x}` is refused
by `lint:yaml`, `metadata:` with `id:` under it is the form; flow SEQUENCES
(`ops: [create, update]`) are fine. And folded scalars (`>-`) rewrap at 72
columns, so do not hand-align them. Settings live in `.yamlfmt` and
`.yamllint`, one comment per decision.

**A substrate to work against** — a Postgres container on `:5433`, a DATABASE
INSIDE IT NAMED AFTER THIS TREE, and the binary from the tree on `:8080`, no
invite code, pid, log, credential key and data root under `.dev/`.
`docker compose up` builds an image; this does not, so a change
is a restart. Every task is a subcommand of `.mise/dev.sh`.

```bash
mise run dev            # foreground; dev:up is the same in the background
mise run dev:totp       # dev, with the second factor ENFORCED
mise run dev:status     # container, this tree's database, server, console, URLs, which door
mise run dev:restart    # rebuild and restart; the data stays
mise run dev:stop       # stop THIS TREE'S server; the shared container keeps running
mise run dev:logs
mise run dev:wipe       # DROP this tree's database and DELETE .dev/ (data root and the archive in it, credential key, pid, log): the next start is a fresh substrate
mise run dev:wipe:all   # dev:wipe, then remove the container — EVERY tree's dev database
```

**One database per tree, inside the one container.** The container is shared by
every checkout and worktree on the box; the database inside it is
`substrate_<the tree directory's name>` — `substrate_substrate` for a checkout
at `src/substrate`, `substrate_issue_554` for a worktree named `issue-554`.
Every start and `dev:status` print the name. TWO WORKTREES WITH THE SAME
DIRECTORY NAME SHARE A DATABASE, so name worktrees distinctly or set
`SUBSTRATE_DEV_DB_NAME`; the shared database is what made three servers sweep,
upgrade and dispatch over one set of repositories, and the engine now refuses
the second of them at boot
([0083](docs/decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)).
`SUBSTRATE_DEV_DB_CONTAINER` and `SUBSTRATE_DEV_DB_PORT` move the container,
`SUBSTRATE_DEV_PORT` the server — the HTTP port is still `8080` for everyone,
and the start refuses rather than bind over another tree's server.

`dev:wipe` matters more than it looks: registration is one-shot per user, and
the only way back is deleting the repository (Settings, Danger zone, or
`POST /repository/delete` with both factors,
[0236](docs/decisions/0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md)),
so a change to the door is most simply tested by throwing the
database away — `dev:wipe` here, `docker compose down -v` on the compose path.
It drops THIS TREE'S database and leaves the container and every other tree's
database alone; `dev:wipe:all` is the one that reclaims the volume.
`bin/substratectl --dsn "$(mise run dev:dsn)" …` is the operator
hat against it, and `mise run console:build` puts the console at `/`.

**The dev door has no invite code and no second factor.** Every `dev*` task
leaves `SUBSTRATE_INVITE_CODE` unset, so the register door reads no code
(there is no closed state: a set code is the gate, an unset one is none), and
sets `SUBSTRATE_INSECURE_DISABLE_TOTP=true`, so registering and signing in
are a repository name and a password: typing a code printed beside the
command that starts the box, and enrolling an authenticator for a database
that gets wiped, are friction with nothing behind them. `compose.yaml` ships
the same two defaults. The engine still mints and seals a seed, and the
deployment says which door it runs at `GET /.well-known/substrate/server.json`
(`registration.inviteRequired`, `registration.totpRequired`), which is what
the console and `substratectl` read before they ask anybody for either code.
A change to the gate is tested with `SUBSTRATE_INVITE_CODE` set (`mise run
test:e2e` does), a change to the factor under `mise run dev:totp`, where it
is enforced, and NEVER by setting the TOTP variable outside this tree.

**The dev substrate has a data root.** Every `dev*` task exports
`SUBSTRATE_DATA_ROOT` as the absolute path of `.dev/data`, so each
repository's directory (blobs, webhook spool) and the archive
(`.dev/data/archive`) land under it, and `dev:wipe` removes it with this
tree's database. The two must go together: an archive outliving a wiped
database is restored at the next boot, and a database outliving its root has
lost every blob's bytes. The dev tasks
also mint `SUBSTRATE_CREDENTIAL_KEY` once into `.dev/credential.key` and
every start reuses it; it wraps each repository's DEK and each identity's, an
operator command that writes sealed material (`user reset`) reads the same file, and
`dev:status` prints both paths. There is no keyless mode: a host without the
key refuses to boot.

**Run the engine suite on its own.** `internal/engine`'s `*_db_test.go` files
share one pgvector testcontainer per test binary, and they starve under a
full-tree parallel run — `go test ./...` can fail there while `go test ./internal/engine/...` passes
clean. A `*_db_test.go` failure that looks arbitrary usually is; confirm it
alone before believing it. The full engine suite is `mise run test:db:engine`;
[docs/testing.md](docs/testing.md) says where its time goes, and
`mise run test:timing` reads a run's timings on this machine.

**One full database suite per machine at a time, never one per worktree.**
A full `test:db*` run uses the whole machine: sixteen parallel tests, a
Postgres container, hundreds of template clones. On 2026-09-26 a workflow ran
it in about twelve worktrees at once on one VM; load reached 50 on 16 vCPUs,
and the processes the agents themselves run in blocked on the disk. So:

- While working in a worktree, run the tests for what you changed with
  `-run`: `go test -count=1 -run '^TestX$|^TestY$' ./internal/engine/`.
- Run the full suite once, at the end, from one tree. An orchestrator that
  fans work out over several worktrees runs the full suites itself, one after
  another, or leaves them to CI; the agents it starts never do.
- `test:db`, `test:db:engine`, `test:db:rest` and `test:db:providers` wait for
  a machine-wide slot (`.mise/dblock.sh`, one slot unless
  `SUBSTRATE_TEST_DB_SLOTS` says more) and print `dblock: waiting for a
  database test slot` while another run holds it. That line means queued, not
  hung. The lock is a backstop for this rule, not a license to start ten.
- A killed run's containers go with the next run (every test container is
  labeled with its owner process, and a run removes the ones whose owner is
  gone); `mise run test:clean` does it by hand.

**`mise run test:llm` is the live suite** — the wire adapters and one whole
agent chain against the REAL OpenAI and Anthropic APIs. It runs when
`OPENAI_API_KEY` and `ANTHROPIC_API_KEY` are in the environment, which on a dev
box they are: they arrive from a gitignored `.mise.local.toml`. Without them
every case skips and the task exits 0, so CI is untouched, and `mise run test`
excludes the set by name either way. It SPENDS REAL MONEY — a pass is about a
cent, but run it once to confirm a change, never in a loop. Key
material is never committed, never echoed, and never written into a file in
this tree. [docs/testing.md](docs/testing.md) has the rest.

## Talk to a substrate

`substratectl` wears **two hats** and they do not meet. The user's hat speaks
HTTP and carries a token; the operator's hat speaks the DSN and the engine,
never HTTP, and refuses before touching anything without one.

```bash
# the user's hat — HTTP + a token
bin/substratectl register                    # invite code, your name (--repository names the repository otherwise), password, TOTP enroll (--skip-totp for none)
bin/substratectl login --identity <yours>    # password, then a TOTP code if the account has one; mints a token record in your repository (--repository picks one of several)
bin/substratectl kinds                       # every installed kind
bin/substratectl get substrate.reamde.dev/core/kind <ref> -o yaml  # one kind's definition
bin/substratectl get '~/tasks/task'          # list one kind: GET /api/v1/records with the kind in filter.kinds; quote `~`, or the shell expands it
bin/substratectl get '~/tasks/task' <id> -o yaml  # one record, apply-able envelope; a bare `task` is refused
bin/substratectl apply -f record.yaml        # put (merge, never prune)
bin/substratectl validate -f record.yaml     # the server's declaration checks, offline; --partial accepts what needs the repository
bin/substratectl watch                       # resumable change stream
bin/substratectl sync status                 # every connected account's sync state, joined with its triggers (the core `sync` trait)
bin/substratectl export                      # the recovery export: the repository's archive as one tar, as of one committed point

# the operator's hat — a DSN, no HTTP
bin/substratectl --dsn "$DATABASE_URL" repository list
bin/substratectl --dsn "$DATABASE_URL" repository verify <repository>  # walk the changelog table, hold the archive's every line to it, check sealed rows and blobs
bin/substratectl --dsn "$DATABASE_URL" repository rebuild <repository>  # replay the changelog table into a fresh fold
bin/substratectl --dsn "$DATABASE_URL" user reset <name>   # needs SUBSTRATE_CREDENTIAL_KEY; the server may keep running
```

**Registration seeds `core`, `llm`, `console` (record views,
[0166](docs/decisions/0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md))
and `observations` (measured values,
[0192](docs/decisions/0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md)),
`assistant` (Moss, the owner's assistant,
[0241](docs/decisions/0241-the-assistant-ships-with-the-binary-and-the-owner-shapes-it-through-instruction-records.md)),
three keyless provider rows, and the shipped units.** The agent runtime's
data kinds are the second seeded package, `substrate.reamde.dev/llm`
(`provider`, `model`, one model a provider offers and what it costs,
`thread`, `message`, `interaction`, `memory`, the owner's
standing facts an agent pins, and `instruction`, the owner's standing
direction to one agent), out of core so core is not
everything ([0077](docs/decisions/0077-the-llm-kinds-live-in-their-own-seeded-package.md));
the `agent` kind stays core's, because a manifest document is a record of a
core kind whatever package it describes. Creation also writes three keyless
`llm/provider` rows (`openai`, `anthropic`, `gemini`) and the `llm/model`
records of the models they offer, which the record seed keeps current; a
row's own deprecated `pricing` entry still wins for the model it names, and
an agent may name `runsOn`, model records best first, instead of a provider
and a model
([0242](docs/decisions/0242-a-model-a-provider-offers-is-a-record-and-an-agent-may-run-on-a-list-of-them.md)). The seeded assistant,
`substrate.reamde.dev/assistant/moss` with its `architect` and `librarian`,
names `anthropic` and refuses at dispatch until that row's `apiKey` is
written; the owner shapes it with `llm/instruction` records rather than
editing it. The LLM sample (the decision-loop lessons) is in the catalog and
nothing adds it.
Creation also writes the shipped unit and conversion records
(`substrate.reamde.dev/core/unit` and `…/core/conversion`, 216 units and 149
conversions from `internal/engine/unitseed`, edited by hand), and every open
fills what a repository lacks without touching a record its owner wrote or
deleted ([0186](docs/decisions/0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md),
[docs/units.md](docs/units.md)).
A seeded package's other data records sit beside its declarations, as a
bundle's do, and the record seed (`internal/engine/recordseed.go`) writes them
at registration and at every open by the same rule: today the console's
collection views of `llm/memory` and `core/contact`, the saved views a
collection shows as tabs
([0229](docs/decisions/0229-a-collections-shipped-saved-views-are-records-of-a-seeded-console-kind.md)).
No row sets a `reasoningEffort` default: the accepted set is the MODEL's, not
the row's (`gpt-5` refuses `none`, the gpt-5.6 family needs it for tools), so
a tool-using agent names the value itself. `tasks` is a SAMPLE
package a repository adds for itself, so a walkthrough
that reaches for any collection outside the seed takes one first. `tasks`
`requires:` `people` and `scheduling`, and adding one whose requirement is
missing is refused, so the order is `samples.substrate.reamde.dev/people`, then
`…/scheduling`, then `…/tasks`, each through `bin/substratectl add <sample>`
or `POST /api/v1/catalog/{id}/add` (the id is the package, so its slash is
percent-encoded: `samples.substrate.reamde.dev%2Ftasks`). The console's All
data → Add a collection → Start from a sample is the third door to the same
admission, and it adds the requirements first. Adding a sample COPIES the closure into the repository's local
namespace, so what lands is `~/tasks/task` in every repository, not the
shipped spelling (records 0048 and
[0259](docs/decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)). A PUBLISHED package (a provider such as
`providers.substrate.reamde.dev/google`, or a catalogue) takes the same route
and lands under the authority that publishes it: who keeps a package decides
where it lands
([0258](docs/decisions/0258-one-catalog-route-adds-a-package-and-keptby-says-who-keeps-it.md)). A
snippet that opens with `get '~/people/person'` on a fresh substrate
is wrong, and one that names a kind by a bare word is refused everywhere
([0101](docs/decisions/0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)).

`substrate.reamde.dev/observations` (`metric`, `observation`, `trace`,
`source`, `sourcelink`) ships no metrics. The metrics, and the domain kinds,
come from the well-known CATALOGUES, first-party packages under the same
authority that are NOT seeded: `substrate.reamde.dev/health` (sleep, workout,
`health/*` metrics), `…/labs` (lab reports and results, `labs/*` metrics,
`convertlab`; requires `health`), `…/environment` (`environment/*`
metrics) and `…/clinical` (the medical record as FHIR-shaped kinds; requires
`labs`, decision 0193). The catalog serves them as published packages, through
the add route (`bin/substratectl add substrate.reamde.dev/labs`), they
land published under their publisher's authority and never in `~`, and adding one first adds every
published package it requires, so adding
`providers.substrate.reamde.dev/applehealth` or `…/whoop` (each shipping its
own adapter) brings `health`. `keptBy` says only who keeps a package: each
catalog entry also says whether it is a PROVIDER (connects to an outside
service: every package under `providers.substrate.reamde.dev`, and any other
published one with an account kind), what it BRINGS, and which provider an
ADD-ON extends, so a catalogue is no provider and the console offers it as a
standard collection under Add a collection
([0238](docs/decisions/0238-the-catalog-tier-says-who-keeps-a-package-and-what-it-is-for-is-read-from-its-declarations.md)). A metric id
starts with the package that ships it (`health/heart_rate`,
`labs/ldl_cholesterol`, `whoop/strain`); an owner's own metrics use another
prefix.

Config is `~/.config/substratectl/config.yaml` (override with
`SUBSTRATECTL_CONFIG`): named contexts of `{name, server, repository,
identity, token, tokenId}` — no URL names the repository, because the token
implies it; the token id so `logout` can revoke the very token it forgets, the
repository (its authority, written by `register` and by `login`) because a
webhook URL is built from it, and the identity because `logout --everywhere`
names the person. `SUBSTRATE_SERVER` /
`SUBSTRATE_TOKEN` are canonical and override the file (`SS_*` is the one
accepted alias); flags override both. A password is NEVER an argument — every
prompt has a flag or a `--*-stdin` twin so the same command scripts headlessly.

## The envelope

Everything — vocabulary declarations and data records alike — is one YAML document
with **four** keys, `kind` / `metadata` / `data` / `status`. `kind` is the
record's kind REFERENCE (`samples.substrate.reamde.dev/people/person`; every
kind carries an authority and a package), `metadata.id` the record id,
`data.properties` the declared properties, references included (a reference
is a property,
[0044](docs/decisions/0044-a-reference-is-the-only-link-between-records.md)) —
properties are NOT written straight onto `data`, and the CLI refuses a document
that tries — and `status` is server-owned, ignored on input, so
`get -o yaml` output is directly `apply -f`-able.

`apply` is `put`: it merges and never prunes. Deletion is only ever the
explicit `delete` verb. A document that still writes `apiVersion`, `group`,
`type` or `spec` is refused, naming the key that took its job.

## The words

The vocabulary is part of the contract, and a dead word is a bug wherever it
survives — in a column name, a route, a CEL binding or a comment. The live
words, and what each one replaced:

| Word          | Is                                                        | Not          |
| ------------- | --------------------------------------------------------- | ------------ |
| **record**    | the thing stored; `kind`/`id` in `records`, `record_kind`/`record_id` in the tables keyed by one record | entity       |
| **kind**      | what a record is; `{authority}/{package}/{name}`           | type, schema |
| **package**   | the kinds one authority versions, owns and quarantines together | namespace, module |
| **authority** | who publishes a package; one DNS-style name, or `~` for the repository's own | group        |
| **trait**     | a contract a kind implements                               | capability   |
| **vocabulary**| kinds, traits and property types together; `/vocabulary/apply` | schema   |
| **changelog** | the append-only sequence of deltas; the `changelog` table  | log          |
| **bundle**    | the install unit, named for the package it ships; a record of core's `bundle` kind, and the `bundle` tier | extension |
| **published** | a package its publisher keeps (`keptBy: publisher`), ADDED under the authority that publishes it; it says who keeps a package, never what it is for | provider, tier (as who keeps a package) |
| **provider**  | a package that connects the substrate to an outside service, as the catalog says on its entry: every package under `providers.substrate.reamde.dev`, and any other published one with an account kind ([0238](docs/decisions/0238-the-catalog-tier-says-who-keeps-a-package-and-what-it-is-for-is-read-from-its-declarations.md)) | integration |
| **sample**    | a package the repository keeps (`keptBy: repository`): a copy the user ADDS into the local namespace, `~/<package>` | example, vocabulary bundle |
| **input**     | a bundle's named configuration need; one record resolves per input (bound reference, the id `default`, then the sole record) | config, configType, singleton |
| **reference** | a typed pointer at one record, declared as a property and stored as `{ref: <kind>/<id>}` | relationship, edge |
| **repository**| everything one user has; its name IS its authority, and a registration names its person after it unless told otherwise; its id (`rnd-…`) keys its storage, and its own kinds live under `~` | username |
| **identity**  | a person's sign-in, held by the server beside the repositories: the name a login presents, the password and the second factor, and the repositories it may open; it holds no data | login credential |
| **local namespace** | `~`, the authority of the repository's own packages: `~/tasks/task` is this repository's tasks kind, whatever the repository is called; the full name is accepted for it on input and replies say `~` | the repository's name in a kind |

`docs/terms.md` is the full list, and it is the one the docs are held to.

Three words survive only in their honest senses. `schema`: JSON Schema for
function IO, and Postgres (`schema_migrations`, `current_schema`). `log`: logging alone — `slog`, the `log` field on the
service, a function body's stdout. `extension`: a file extension, or a Postgres
one. Anywhere else, each of them is a bug.

## Kind identity

A kind reference is `{authority}/{package}/{name}`
([0047](docs/decisions/0047-a-kind-lives-in-a-package.md)): the authority is a
dot-separated DNS-style name or `~`, the package and the name are single
lowercase words, so the reference splits on its two slashes without a
registry: the authority is the one segment carrying a dot, or `~`, the
repository's local namespace
([0259](docs/decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)). A stored reference value is that
plus the id, and a record's REST path is the reference under `/api/v1/`: four
segments, always. There is no per-kind list route: every list, ranked read and
tail is `GET /api/v1/records` with the kind in `filter.kinds`, and a create
under a server-assigned id is `POST /api/v1/records` with `kind` in the body
([0079](docs/decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md)).

The **package** is the unit. A kind may pin its own `version`, else it takes
its package's; a stored closure the loader refuses parks its package; and
`authorizeDeclarationWrite` reads the package row's `source`, so a
repository's token may not write a `builtin` package (the seeded `core`,
`llm`, `console` and `observations`) or a `published` one (an installed
provider or catalogue), and creates a new package only under `~`, the
repository's full name folded into it first
([0210](docs/decisions/0210-a-token-declares-new-packages-only-under-its-repositorys-authority.md),
[0259](docs/decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md));
its own packages and the
samples it added are `installed` and writable, whoever declared them: a
package an app once declared under the retired `declare:` scope is the
owner's like any other, and `declaredByClient` is a stamp no path writes
([0214](docs/decisions/0214-an-apps-approval-installs-published-packages-and-declare-and-the-agent-detail-are-retired.md)).
An app's own kinds arrive as a package its approval installs, published by
its authority's document. A bundle owns one package and its id IS that
package.

`substrate.reamde.dev` is a served domain: its `_substrate` TXT record names
the authority document other servers read to verify the first-party apps
([0209](docs/decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)).
Kind identity is unchanged by that: an authority is still a DNS name, and
moving kind identity to URLs (so anyone can publish kinds from a git repo
without owning DNS) is real design work that has not happened. Do not
half-do it. The authority's character budget is reserved
([0014](docs/decisions/0014-authorities-widen-only-outside-the-id-alphabet.md)):
the record id alphabet is frozen and never gains `%`, and an authority widens
only with characters the id alphabet excludes and never gains a raw `/`; the
package segment is the third job for `/`
([0047](docs/decisions/0047-a-kind-lives-in-a-package.md)), and the rest of
0014 stands, first-label reservation included. The
actors are `bundle:<authority>:<package>`,
`function:<authority>:<package>:<name>`,
`agent:<authority>:<package>:<name>` and an app's token's actor, from what
its approval proved: `app:<authority>` for an app whose publisher was
verified, `app:local` for one whose access went to a loopback address, and
`app:<host>` of its client id otherwise. A package under `~` writes as `~`
(`bundle:~:tasks`), so its actor record is the same whatever the repository
is called. All of them are derived
by the substrate and never declared
([0025](docs/decisions/0025-an-actor-carries-the-full-authority.md),
[0194](docs/decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
[0209](docs/decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)). There
is no second spelling of a kind anywhere: no generated type name, no folded
authority, but one: an input may spell `~` as the repository's full name,
and the reply says `~`. The reference is the name on every surface, and nothing keys on a
first label.

## House rules

- **Give each UI fact a clear role.** Never place unrelated text fragments
  beside each other without labels or structure. Separate headings, labels,
  values, status, and actions with hierarchy and spacing. Show full kind and
  actor references where identity matters; wrap them instead of shortening
  them. Use the same field labels and descriptions in read and edit views.

- **Never leak the deployment.** This repo goes public. No cluster hostnames,
  no secret names, no internal service URLs — not in code, not in comments,
  not in test fixtures. A default that points at somebody's box is a bug;
  `example.com` or a localhost default is the answer.
- **`internal/api` never imports `internal/engine`.** The Go contract every
  consumer builds against is `internal/substrate`; the engine is one
  implementation of it. The exception is the API's tests of engine behavior:
  they run the handler on the real engine, in `internal/api`'s
  `*_db_test.go` files, the only files there that may import it, and its
  tests of the handler alone run on a stub that implements almost nothing
  (`internal/api/stub_test.go`,
  [0263](docs/decisions/0263-the-apis-tests-of-engine-behavior-run-on-the-real-engine-and-its-stand-in-is-deleted.md)).
- **The top level stays empty.** Every Go package is under `internal/` (or
  `cmd/`), and four directories are the exceptions. Two are the vocabulary as
  files: `kinds/` (the seeded `core`, `llm`, `console` and `observations`
  packages, the well-known catalogues and the shipped providers) and
  `samples/` (the sample packages a repository copies), each
  with the one Go file that embeds it. Two are for people building on a
  substrate: `skills/`, the agent skills they install (the upgrade runbook),
  and `.claude-plugin/`, the marketplace manifest Claude Code installs them
  from. A skill there speaks to a user of a substrate, never to this repo's
  developers, whose skills stay under `.claude/skills/`. No package is named
  for a language construct: no `types.go`, no `iface.go`, no `utils`. An
  interface lives with the subject it describes. Three known exceptions are
  not precedent: `web/console/src/lib/utils.ts` (the path shadcn's
  generated components import), `web/console/src/lib/api/types.ts` (the wire
  mirror below) and `internal/vocabulary/types.go`.
- **The changelog is the truth.** It is the `changelog` table, each line
  carrying its checksum, and Postgres alone confirms a write;
  `internal/engine/fold.go` is the one path from it to `records`, so a live
  write, an import and `RebuildRepository` fold from the same table and
  cannot drift. Anything that writes `records` directly, or a changelog row
  written outside `inTx`, is wrong. The exceptions are derived storage, a
  function of the fold and the declarations in force: each is written
  outside the fold without moving `version`, `updated_at` or the changelog,
  and `repository rebuild` derives it again. They are `fts`, the search
  index over each row (`rederiveFTS`, run by the background pass in
  `reprojection.go` behind a vocabulary apply or the boot upgrade and by the
  open-time reindex in `searchindex.go`, and `reprojectFTS` for the
  declaration rows inside the boot upgrade's transaction); `orphaned_at` and
  `ambiguous_at` on `records` (`orphans.go`, `ambiguous.go`); and the
  `property_offers` table (`mapping.go`). The **archive** is a copy, never a
  second truth: `internal/engine/archive.go` writes it after the commits,
  the blobs and sealed values before the pieces that name them and the
  manifest last, and nothing on the write path waits for it or reads it
  ([0262](docs/decisions/0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md),
  [0277](docs/decisions/0277-the-archive-is-pieces-blobs-and-sealed-values-and-its-manifest-is-the-mark.md)).
  History comes into the table from an archive or a 0.x directory only
  through the boot check's one import path (`history.go`).
- **A changed declaration ships a changed version.** Every document under
  `kinds/` and `samples/` projects with a `version`, an incremental integer (a
  kind's own `data.version` where it pins one, else its package's, else 1). The boot
  upgrade, the catalog's upgrade preview and the console's upgrade offer all
  key on it, so editing a declaration without bumping is an upgrade no
  repository ever receives. Through the API the engine maintains the version
  itself (a changed definition lands at stored+1), so hand-bumping is only
  for this tree, where the boot upgrade needs one total order across
  binaries. Bump the kind's own version for a one-kind change, the package's
  (in its `bundle.yaml`) for a closure-wide one, and the package's when a
  declaration is removed, so the prune reads as an upgrade.
  Additive changes (new kind, new optional property, new enum value, new
  state) upgrade cleanly; narrowings (drop, retype, remove a value, add
  `required`) are refused while live records hold the old shape, so prefer
  add-and-deprecate. `deprecated: true` is the marker, on a property, a
  reference or one enum value. CI runs `mise run kinds:check` and refuses the
  merge otherwise.
- **A changed stored shape ships a conversion step.** The archive is the
  export's contract
  ([docs/stored-format.md](docs/stored-format.md)): the manifest, a line's
  keys, every effect and its fields, each a closed set. A shape the history
  already holds changes only through a step,
  `internal/engine/conversion_NNNN_<name>.go`, that raises
  `maxChangelogDialect` and rewrites each row of the `changelog` table in
  place, in one transaction at the boot check: the one writer that rewrites
  a line, and the archive follows on its next pass. It lands with a test of what the lines
  become, and the server reads one shape. Each release keeps the directory
  its own server wrote (`mise run release:keep <tag>`), `TestKeptDirectories`
  imports every one, and `frozen:check` refuses an edit to a kept directory
  or a landed step
  ([0260](docs/decisions/0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)).
- **A kind titles itself from a property it declares.** The built-in `title`
  every record carries is derived storage, never a kind's input: a kind with a
  heading declares its own property (`name`, `summary`, `subject`) and renders
  the title with a `displayTemplate`. A written `title` is ignored on any kind
  that declares one, so a writer that means the heading writes the property
  ([0016](docs/decisions/0016-a-kind-titles-itself-from-a-declared-property.md)).
- **A dialect key is reserved by name, never tolerated by prefix.** A
  declaration's key set is closed, so an unknown key quarantines the authority
  and adding one is an upgrade of every binary that reads the closure. New
  keys therefore land inert and validated, in batches, and there is no `x-`
  escape hatch
  ([0020](docs/decisions/0020-dialect-keys-are-reserved-not-tolerated.md)).
  A key added, removed or moved raises `vocabulary.Dialect`, which discovery
  publishes and a package states under `dialect`:
  `internal/vocabulary/testdata/dialect.golden.json` records the key sets,
  `go test ./internal/vocabulary/ -run TestDialectKeysMatchTheGolden -update-dialect`
  refuses sets that moved under an unchanged number, and
  `mise run dialect:check` holds the same against the base branch. A step
  that adds a key also adds its line to `keyIntroducedAt`
  (`internal/vocabulary/dialect.go`) at the dialect it raises the number to,
  which is how a package header that states less than its keys need is
  refused; the golden holds the table, and both checks refuse a key added
  without its line, a line whose number moved and a line dropped while its
  key stays. A closed key set is a
  registered `…Keys` map, and the golden's test refuses one written inline
  in the shapes it can see (a ranged key compared with `!=` to a word, or
  switched over words with a `default`); an `==` chain or a lookup by name
  it does not see
  ([0268](docs/decisions/0268-a-new-declaration-key-raises-the-dialect-and-a-package-states-the-dialect-it-needs.md)).
  `unique` and `deprecated` are reserved today: stored, refused where they
  could not be honored, and acted on by no server path. A kind's `purpose`
  (`primary`, `supporting` or `internal`; absent reads as `primary`) is
  validated and stored, read by clients to decide what they list, by search
  to rank machinery lower, and by the engine to leave `internal` kinds out
  of an agent's kinds list unless its grant names them; every kind of
  `core`, `llm` and `console` is `internal` but `llm/memory` and
  `core/contact` (`vocabulary.SeededOwnerData`, which the merge guard and the
  console read too), while they and the
  seeded `observations` kinds are the owner's data and keep their own
  ([0133](docs/decisions/0133-a-kind-declares-its-purpose.md),
  [0231](docs/decisions/0231-a-contact-is-a-core-kind-of-the-owners-data-and-a-setting-names-the-owners-own.md),
  [0136](docs/decisions/0136-search-ranks-by-bm25f-and-a-kinds-purpose.md),
  [0172](docs/decisions/0172-an-agent-names-the-kinds-it-searches-and-its-prompt-lists-them.md)).
  A package header's `stability` (`stable`, `beta` or `experimental`; absent
  stores nothing and reads as `experimental`) is validated and stored the
  same way and refuses no write: it is the label the package states for its
  kinds, there is none on a kind, and a client reads it off the package
  record ([0256](docs/decisions/0256-a-stability-label-sits-on-a-package-and-on-a-feature-and-a-feature-carries-a-revision.md),
  [docs/stability.md](docs/stability.md)). `renamedFrom` is live, on a
  property and on an enum value entry alike:
  admitting the declaration moves every live record's value to the new name,
  or rewrites the old spelling to the new one, inside the apply's
  transaction, as ordinary record writes
  ([0063](docs/decisions/0063-a-property-rename-is-ordinary-record-writes.md),
  [0066](docs/decisions/0066-a-backfill-and-an-enum-remap-are-ordinary-record-writes.md)).
  `required` beside a `default` backfills the same way, and a remap onto a
  value the stored declaration still admits is refused as lossy. `movedFrom`
  is live on a KIND, and only on the SHIPPED boot upgrade: admitting a seeded
  declaration that names a kind the repository still declares carries every
  live row of it here, same id and same properties, repoints every live
  reference at it and tombstones the old rows, in the same transaction and as
  ordinary record writes
  ([0078](docs/decisions/0078-a-kind-move-is-ordinary-record-writes.md)). The
  old kind stays declared and empty; a second boot finds nothing to move. The
  apply door and the catalog store the key and REFUSE the move, because they
  publish a candidate registry before any rewrite could run and because a move
  reaches past the write path's admission rules.
  There is no `edges.<rel>.properties` and no `data.edges`:
  a reference is the only link between records, data on the link is the
  reference property's own `properties:` block, and a write carrying a link
  property the reference does not declare is refused
  ([0044](docs/decisions/0044-a-reference-is-the-only-link-between-records.md)).
  `retired` is live too: a package header lists the kind names it has spent
  and a kind lists its spent property names, enum values and states, the
  list is carried forward and never shrinks, and declaring a listed name
  again is refused on every door. A prune alone spends nothing
  ([0055](docs/decisions/0055-a-retired-name-is-declared-and-never-inferred-from-a-prune.md)).
- **The console mirrors the wire by hand, and a golden file holds it to it.**
  `web/console/src/lib/api/types.ts` is written to match the structs in
  `internal/substrate`; nothing generates it. `wire.golden.json` is where the
  two meet: `internal/substrate/wire_test.go` writes the field names Go
  serializes and whether each is always present (`omitempty` is optional),
  and a vitest asserts the TypeScript carries exactly those, `?` where the
  server may omit and nowhere else. Every wire interface the console's
  `lib/api` modules export is pinned, so an API reply is a named struct in
  `internal/substrate`, never a bare `map[string]any`. Move a wire field and the Go test fails; regenerate with
  `go test ./internal/substrate/ -run TestWireGolden -update` and the console's
  test fails until `types.ts` follows. Do not edit the golden by hand.
- **A route is placed in a feature, and a feature that gains something
  raises its revision.** The feature list in
  `GET /.well-known/substrate/server.json` is built from one table,
  `featureTable` in `internal/api/features.go`: each feature with its
  stability, its revision and the routes it holds
  ([0256](docs/decisions/0256-a-stability-label-sits-on-a-package-and-on-a-feature-and-a-feature-carries-a-revision.md),
  [docs/stability.md](docs/stability.md)). A route added to the router goes
  into the feature it belongs to, or into a new feature at revision 1, and
  `TestEveryRouteIsInOneFeature` fails until it does. A feature that gains
  a route, a parameter, a request field or a response field a client may
  want to check for goes up one revision, and a revision never comes down.
  `internal/api/testdata/features.golden.json` records each feature's
  routes and revision; regenerate it with
  `go test ./internal/api/ -run TestFeaturesMatchTheGolden -update`, which
  refuses a route set that changed under an unchanged revision, and do not
  edit it by hand. `mise run features:check` holds the same against the base
  branch, so two branches cannot each spend one number. Taking something
  away is declared, never inferred: a route a feature stops serving is
  listed in its `removed`, and a feature that goes in `removedFeatures`
  with the routes it held. Neither list shrinks, an address or a name on
  one is never used again, and the same guards refuse a route or a feature
  that left the table without being listed and a `stable` label that went
  down. A parameter, a request field and a response field are seen by no
  guard: raising the revision for one is held by review alone. The feature
  table in
  `docs/api.md` follows, and `TestDocsListTheFeatures` holds it. A new error code goes into `errorCodes` in
  `internal/api/errors.go`, then
  `go test ./internal/api/ -run TestErrorCodes -update`, the
  `The closed set:` line of `docs/api.md` and the console's
  `ServerErrorCode` union. A handler names a code constant where it writes
  one (`TestEveryEmittedCodeIsADeclaredConstant`, which cannot see an
  envelope spelled as raw JSON: do not write one). A new reason goes into
  `ErrorReasons` in `internal/substrate/errors.go`, then
  `go test ./internal/api/ -run TestErrorReasons -update`, the
  `The closed set of reasons:` line of `docs/api.md` and the console's
  `ErrorReason` union.
- **Comments carry constraints, not narration.** Say why a thing must be so,
  or say nothing.
- **Every commit title is a conventional commit** —
  `type(scope): what changed`, with `!` before the colon for a break. The
  types in use here are `feat`, `fix`, `docs`, `refactor`, `test`, `chore`,
  `ci`. This is not style: **the title is the changelog entry and the
  version bump**. release-please reads the titles merged since the last tag,
  writes the next `CHANGELOG.md` section from them (`feat:` under Added,
  `fix:` under Fixed, a break under BREAKING CHANGES) and picks the next
  version (`fix:` the patch, `feat:` the minor, a break the minor while
  below 1.0.0), and keeps that as one open release pull request,
  `chore(release): vX.Y.Z`. A title nothing can parse is a change the
  changelog never lists. `main` moves only through a pull request, merged by
  squash or by rebase: a squash lands the PR title as the one commit, a
  rebase lands every commit as it is, so the PR title AND every commit
  subject on the branch are held, by `mise run commits:check` (the
  `conventional commits` check). The ruleset is
  `.github/rulesets/main.json`, applied by an admin with
  `mise run repo:settings`.
- **A merge to `main` releases nothing; merging the release pull request
  does.** `latest` tracks `main`. A release is cut when somebody runs the
  checklist in [docs/releasing.md](docs/releasing.md) against the open
  `chore(release): vX.Y.Z` pull request and merges it: release-please tags
  the merge, creates the GitHub release with the section as its body, and
  `release.yml` builds and attaches the artifacts. Nothing is tagged by hand.
- **A break carries its upgrade steps in a `BREAKING CHANGE:` footer.** The
  footer's text is what release-please prints under BREAKING CHANGES, and
  it is what a person or an agent upgrading follows literally: the command
  or the request, in order, and what happens to a rollback. A `!` with no
  such footer on any commit of the branch is refused by `commits:check`; a
  feature or a fix whose title is not enough to use it says the rest in its
  body. The rest is held by the agent review
  (`.github/workflows/review.yml`, briefed by `.github/review.md`), which
  comments and never blocks. A wrong line in a merged change is fixed by
  editing the merged pull request's body (`BEGIN_COMMIT_OVERRIDE`, see
  [docs/releasing.md](docs/releasing.md)), never `CHANGELOG.md` by hand.
- Keep `mise run lint` and `mise run fmt:check` at zero. Both are aggregates,
  and the `lint` job runs both but the apps' half: `lint` is Go, YAML, shell, Python, the docs,
  the migrations, the sandbox gate, the toolchain pins, the CI scripts and the apps' layering, `fmt:check` is Go, YAML, Swift and Kotlin, and
  no CI job runs the Swift and Kotlin checks. The console has its own pair
  (`console:lint`, `console:fmt:check`) inside `ci:console`.
- **A landed migration is never edited.** `internal/engine/migrations/` is
  append-only: the runner records each file's sha256 as it applies it and
  refuses a database whose recorded hash no longer matches, so editing a merged
  migration changes nobody's schema and locks every database that ran the old
  text out of every later binary. Add the next number instead, and make it
  idempotent where it may meet a schema that already has the change.
  `mise run frozen:check` refuses the edit, the delete and the rename;
  `mise run lint:migrations` holds the naming and numbering, because a file the
  runner cannot parse is a step that silently never runs. A **repository
  migration** (`internal/engine/repomigration_NNNN_name.go`) is the same
  contract for code: it runs once per repository at the repository's first
  open under a binary that carries it, before the stored vocabulary loads,
  writes through the changelog as ordinary record writes, is idempotent, and
  leaves its row in `repository_migrations`
  ([0099](docs/decisions/0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md)).
  Reach for one only where a SQL migration cannot decide the answer from the
  rows alone. A SQL migration never writes a row of a fold table (every table
  `repository rebuild` clears and fills again from the changelog): it changes
  their schema alone, and a change to what a row holds is a repository
  migration, so a rebuild replays it.
  `TestEveryMigrationLeavesAFoldTheRebuildReproduces` holds every embedded
  migration to this
  ([upgrading the binary](docs/operations.md#upgrading-the-binary)).
- **`lint:docs` is the one docs linter**, and it holds two halves. What the
  pages point at: every Markdown link and `#anchor` resolves against the tree,
  offline, so renaming a doc or a heading means fixing what points at it, and
  external URLs are never fetched because somebody else's outage is not this
  repo's build failure. What they say: `.mise/docscheck.sh`, whose header
  says which files its rules read and how to list the rules. A mechanical
  docs rule worth enforcing goes in that script, so there is one place to run
  and one place to add to.
- **A decision that outlives its argument gets a record.**
  [docs/decisions/](docs/decisions/README.md) is one short, dated page per
  choice, frozen once accepted and superseded rather than edited, which
  `mise run frozen:check` holds: an accepted record's body may not change,
  while its `status:` and `superseded-by:` still may. The bar is
  all three of hard to reverse, shapes what other code may do, and reasoning
  not already written down; everything else is a commit. Read the index before
  proposing design work, so an option already rejected is not proposed again.
  A rule here may link its record. Before citing a new record's number,
  `git fetch --prune` and run `mise run decisions:check`: it refuses a number `main`
  or another live branch already took, and names a free one
  ([0124](docs/decisions/0124-a-decision-number-is-checked-against-every-live-branch-before-the-merge.md)).
