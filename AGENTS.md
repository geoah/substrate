# Agent guide

How to work this repo: the substrate server (`cmd/substrated`), the console
(`web/console`, built into the server image and served by it at `/`) and the
CLI (`cmd/substratectl`). Provider auth and sync live IN the server — the OAuth
facility plus bundle functions.

**The model, in one paragraph.** One invite code admits people. Registering
creates a **user** — username, password and TOTP, all three — and that user's
one **repository**, which owns one **authority** (`<username>.<server host>`
unless the registration names one; the home of the kinds the user declares,
[0046](docs/decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md)).
Everything the user has lives in it: an append-only,
strictly sequential, hash-chained and server-signed **changelog**
is the truth and the **records** table is
its fold. Tokens and the login credential are themselves records, and a token
has full access to its repository. There are no tenants, no identities, no
user-managed keys, no sharing, no scopes and no roles.

There is no written contract document. There was one — 3000 lines that
declared six absent documents its superior, described a tree that no longer
existed, and told you to send a trigger key the loader refuses — and it was
deleted rather than patched. **The code is the contract**; the tests are what
it promises. A replacement will be written from the code, small, when someone
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
mise run lint           # every linter: Go, YAML, shell, Python, the docs, the pins
mise run lint:go        # one of them; :yaml/:shell/:python/:docs/:toolchain are the rest
mise run audit          # the vulnerability scans: govulncheck and pnpm audit
mise run fmt            # every formatter, in place: gofumpt/goimports and yamlfmt
mise run fmt:check      # the same, as a check — what CI runs
mise run console:dev    # the console on :5173, proxying /api to :8080
mise run ci             # every CI job, locally. The whole pipeline.
```

**A bare task name is every check of its kind**, and a suffix narrows it to
one language: `lint` is all six linters, `lint:go` is one of them; `fmt`
writes Go and YAML, `fmt:yaml` writes one; `fmt:check` is the pair as a check.
Nothing is reachable only through the aggregate. The console is not one of
those suffixes — it is a second toolchain with its own family (`console:lint`,
`console:fmt`, `console:test`), exactly as `test` is the Go suite and
`console:test` is not in it.

**`fmt` and `lint` cannot disagree.** `fmt:go` is `golangci-lint fmt`, not
bare gofmt: the formatters are configured once, in `.golangci.yml`'s
`formatters:` block (gofumpt with extra rules, goimports with a local prefix),
and both the writer and the checker are that same configuration. If `lint:go`
reports a formatting finding that `mise run fmt` will not fix, that is a bug in
the configuration, not something to hand-patch.

**`audit` is its own family, not part of `lint`.** It is the one check whose
answer changes without the tree changing, because it asks a vulnerability
database what is known today: green this morning, red this afternoon, same
commit. It also needs the network, and `lint` is something a laptop can run on
a plane. It has its own CI job for the same reason.

**The YAML is formatted too.** `kinds/` is the contract as files, so its shape
is held the way Go's is: `mise run fmt` writes it, `mise run fmt:check` fails
on it, and the `lint` job runs both. Two rules are worth knowing before you
write a document by hand. Block mappings only — `metadata: {id: x}` is refused
by `lint:yaml`, `metadata:` with `id:` under it is the form; flow SEQUENCES
(`ops: [create, update]`) are fine. And folded scalars (`>-`) rewrap at 72
columns, so do not hand-align them. Settings live in `.yamlfmt` and
`.yamllint`, one comment per decision.

**A substrate to work against** — a Postgres container of its own on `:5433`
and the binary from the tree on `:8080`, invite code `let-me-in`, pid and log
under `.dev/`. `docker compose up` builds an image; this does not, so a change
is a restart. Every task is a subcommand of `.mise/dev.sh`.

```bash
mise run dev            # foreground; dev:up is the same in the background
mise run dev:totp       # dev, with the second factor ENFORCED
mise run dev:status     # database, server, console, URLs, which door
mise run dev:restart    # rebuild and restart; the data stays
mise run dev:logs
mise run dev:wipe       # DELETE the database: the next start is a fresh substrate
```

`dev:wipe` matters more than it looks: registration is one-shot per user and
there is no unregister, so any change to the door is tested by throwing the
database away — `dev:wipe` here, `docker compose down -v` on the compose path. `bin/substratectl --dsn "$(mise run dev:dsn)" …` is the operator
hat against it, and `mise run console:build` puts the console at `/`.

**The dev door has no second factor.** Every `dev*` task sets
`SUBSTRATE_INSECURE_DISABLE_TOTP=true`, so registering and signing in are a
username and a password: enrolling an authenticator for a database that gets
wiped is friction with nothing behind it. The engine still mints and seals a
seed, and the deployment says which door it runs at
`GET /.well-known/substrate/server.json` (`registration.totpRequired`), which
is what the console and `substratectl` read before they ask anybody for a
code. A change to the door is tested under
`mise run dev:totp`, where the factor is enforced, and NEVER by setting the
variable outside this tree.

**The dev substrate signs.** Changelog signing is mandatory and the signing
seed seals under `SUBSTRATE_CREDENTIAL_KEY`, so the dev tasks mint a key once
into `.dev/credential.key` and every start reuses it; `dev:wipe` removes it
with the database. An operator command that needs the key (`repository
reseal`) reads the same file — `dev:status` prints the export line. There is
no keyless mode to fall back to: a host without the key refuses to boot, and
a repository whose key cannot open refuses every write.

**Run the engine suite on its own.** `internal/engine`'s `*_db_test.go` files
each start a pgvector testcontainer, and they starve under a full-tree parallel
run — `go test ./...` can fail there while `go test ./internal/engine/...` passes
clean. A `*_db_test.go` failure that looks arbitrary usually is; confirm it
alone before believing it. The full engine suite takes ~4 minutes.

**`mise run test:llm` is the live suite** — the wire adapters and one whole
agent chain against the REAL OpenAI and Anthropic APIs. It runs when
`OPENAI_API_KEY` and `ANTHROPIC_API_KEY` are in the environment, which on a dev
box they are: they arrive from a gitignored `.mise.local.toml`. Without them
every case skips and the task exits 0, so CI is untouched, and `mise run test`
excludes the set by name either way. It SPENDS REAL MONEY — a pass is well
under a cent, but run it once to confirm a change, never in a loop. Key
material is never committed, never echoed, and never written into a file in
this tree. [docs/testing.md](docs/testing.md) has the rest.

## Talk to a substrate

`substratectl` wears **two hats** and they do not meet. The user's hat speaks
HTTP and carries a token; the operator's hat speaks the DSN and the engine,
never HTTP, and refuses before touching anything without one.

```bash
# the user's hat — HTTP + a token
bin/substratectl register                    # invite code, username, password, TOTP enroll, recovery key (auto-saved to 1Password when `op` is signed in)
bin/substratectl login --username <you>      # password + TOTP; mints a token record
bin/substratectl kinds                       # every installed kind
bin/substratectl get kind <ref> -o yaml      # one kind's definition
bin/substratectl get task <id> -o yaml       # one record, apply-able envelope
bin/substratectl apply -f record.yaml        # put (merge, never prune)
bin/substratectl watch                       # resumable change stream

# the operator's hat — a DSN, no HTTP
bin/substratectl --dsn "$DATABASE_URL" repository list
bin/substratectl --dsn "$DATABASE_URL" repository verify <username>  # walk the changelog chain: every hash, every signature
bin/substratectl --dsn "$DATABASE_URL" repository rebuild <username>
bin/substratectl --dsn "$DATABASE_URL" repository reseal <username>  # migrate legacy secrets into the sealed store; needs SUBSTRATE_CREDENTIAL_KEY
bin/substratectl --dsn "$DATABASE_URL" user reset <username>   # needs SUBSTRATE_CREDENTIAL_KEY
```

**Registration seeds `core` and nothing else.** `tasks` above is a vocabulary
bundle the repository imports, so a walkthrough that reaches for any non-core
collection installs one first — `apply -f` of the closure's files, the
console's Registry page, or `POST
/api/v1/core.substrate.reamde.dev/catalog/{id}/install`, which are three doors
to the same admission. A snippet that opens with `get people` on a fresh
substrate is wrong, and was.

Config is `~/.config/substratectl/config.yaml` (override with
`SUBSTRATECTL_CONFIG`): named contexts of `{name, server, username, token,
tokenId}` — **no repository**, because the token implies it, and the token id
so `logout` can revoke the very token it forgets. `SUBSTRATE_SERVER` /
`SUBSTRATE_TOKEN` are canonical and override the file (`SS_*` is the one
accepted alias); flags override both. A password is NEVER an argument — every
prompt has a flag or a `--*-stdin` twin so the same command scripts headlessly.

## The envelope

Everything — vocabulary declarations and data records alike — is one YAML document
with **four** keys, `kind` / `metadata` / `data` / `status`. `kind` is the
record's kind REFERENCE (`people.substrate.reamde.dev/person`; every kind
carries an authority), `metadata.id` the record id,
`data.properties` the declared properties with `data.edges` beside them —
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
| **record**    | the thing stored; `record_kind`/`record_id` in every table | entity       |
| **kind**      | what a record is; `{authority}/{name}`                     | type, schema |
| **authority** | who publishes a kind; one DNS-style label                  | group        |
| **trait**     | a contract a kind implements                               | capability   |
| **vocabulary**| kinds, traits and property types together; `/vocabulary/apply` | schema   |
| **changelog** | the append-only sequence of deltas; the `changelog` table  | log          |
| **bundle**    | the install unit; `/bundles`, and the `bundle` tier         | extension    |
| **input**     | a bundle's named configuration need; one record resolves per input (bound edge, the id `default`, then the sole record) | config, configType, singleton |
| **edge**      | a named (`rel`), directed link between records              | relationship |

`docs/terms.md` is the full list, and it is the one the docs are held to.

Three words survive only in their honest senses. `schema`: GraphQL's own
`Schema`, JSON Schema for function IO, and Postgres (`schema_migrations`,
`current_schema`). `log`: logging alone — `slog`, the `log` field on the
service, a function body's stdout. `extension`: a file extension, or a Postgres
one. Anywhere else, each of them is a bug.

## Kind identity

A kind reference is `{authority}/{name}`: the authority is a single
dot-separated DNS-style label and the name never contains a slash, so the
reference splits on its one slash. The authority is one path segment in
`/api/v1/{authority}/{kind}` — the same shape Kubernetes uses to keep
`networking.k8s.io` addressable.

`substrate.reamde.dev` is a **placeholder**. Moving kind identity to URLs (so
anyone can publish kinds from a git repo without owning DNS) is real design
work and it has not happened. Do not half-do it. Its character budget is
reserved
([0014](docs/decisions/0014-authorities-widen-only-outside-the-id-alphabet.md)):
the record id alphabet is frozen and never gains `%`, an authority widens
only with characters the id alphabet excludes and never gains a raw `/`, and
first-label keying moves to the full authority or a hash of it before the
grammar widens. The actors moved
([0025](docs/decisions/0025-an-actor-carries-the-full-authority.md)):
`bundle:<authority>`, `function:<authority>:<name>`,
`agent:<authority>:<name>`, derived by the engine and never declared, with
`connector:` retired. The GraphQL prefix is the one keying left.

## House rules

- **Never leak the deployment.** This repo goes public. No cluster hostnames,
  no secret names, no internal service URLs — not in code, not in comments,
  not in test fixtures. A default that points at somebody's box is a bug;
  `example.com` or a localhost default is the answer.
- **`internal/api` never imports `internal/engine`.** The Go contract every
  consumer builds against is `internal/substrate`; the engine is one
  implementation of it, and the API is tested entirely against a hand-written
  fake (`internal/api/fake_test.go`).
- **The top level stays empty.** Every package is under `internal/` (or
  `cmd/`); `kinds/` is the vocabulary as files, with the one Go file that
  embeds it. No package is named for a language construct — no `types.go`, no
  `iface.go`, no `utils`. An interface lives with the subject it describes.
- **The changelog is the truth.** `internal/engine/fold.go` is the one path from changelog to
  `records`, so a live write and `RebuildRepository` cannot drift. Anything
  that writes `records` directly is wrong.
- **A changed declaration ships a changed version.** Every document under
  `kinds/` projects with a `version`, an incremental integer (a kind's own
  `data.version` where it pins one, else its authority's, else 1). The boot
  upgrade, the catalog's upgrade preview and the console's upgrade offer all
  key on it, so editing a declaration without bumping is an upgrade no
  repository ever receives. Through the API the engine maintains the version
  itself (a changed definition lands at stored+1), so hand-bumping is only
  for this tree, where the boot upgrade needs one total order across
  binaries. Bump the kind's own version for a one-kind change,
  the authority's (in its `bundle.yaml`) for a closure-wide one, and the
  authority's when a declaration is removed, so the prune reads as an upgrade.
  Additive changes (new kind, new optional property, new enum value, new
  state) upgrade cleanly; narrowings (drop, retype, remove a value, add
  `required`) are refused while live records hold the old shape, so prefer
  add-and-deprecate. `deprecated: true` is the marker, on a property, an
  edge or one enum value. CI runs `mise run kinds:check` and refuses the merge
  otherwise.
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
  `unique`, `deprecated` and `renamedFrom` are reserved today: stored,
  refused where they could not be honored, and acted on by nothing.
  `edges.<rel>.properties` was reserved with them and is now live: an edge
  write carrying a property the rel does not declare is refused
  ([0027](docs/decisions/0027-an-edge-outlives-a-tombstone-and-dies-with-a-purge.md)).
- **The console mirrors the wire by hand, and a golden file holds it to it.**
  `web/console/src/lib/api/types.ts` is written to match the structs in
  `internal/substrate`; nothing generates it. `wire.golden.json` is where the
  two meet: `internal/substrate/wire_test.go` writes the field names Go
  serializes, and a vitest asserts the TypeScript carries exactly those. Move a
  wire field and the Go test fails; regenerate with
  `go test ./internal/substrate/ -run TestWireGolden -update` and the console's
  test fails until `types.ts` follows. Do not edit the golden by hand.
- **Comments carry constraints, not narration.** Say why a thing must be so,
  or say nothing.
- **Every commit title is a conventional commit** —
  `type(scope): what changed`, with `!` before the colon for a break. The
  types in use here are `feat`, `fix`, `docs`, `refactor`, `test`, `chore`,
  `ci`. This is not style: **the title is the release**. A merge to `main`
  that passes CI is folded into a version by these titles (`fix:` the patch,
  `feat:` the minor, `!` the major, or the minor below 1.0.0), and that
  version is tagged, built and published without anybody deciding to. A title
  nothing can parse is a release that does not happen. A PR title is one too:
  the merge is a squash, so the PR title IS the commit that gets read.
  `mise run version:next` says what main would release right now.
- Keep `mise run lint` and `mise run fmt:check` at zero. Both are aggregates,
  and the `lint` job runs both: `lint` is Go, YAML, shell, Python, the docs,
  the migrations and the toolchain pins, `fmt:check` is Go and YAML. The console has its own pair
  (`console:lint`, `console:fmt:check`) inside `ci:console`.
- **A landed migration is never edited.** `internal/engine/migrations/` is
  append-only: the runner records each file's sha256 as it applies it and
  refuses a database whose recorded hash no longer matches, so editing a merged
  migration changes nobody's schema and locks every database that ran the old
  text out of every later binary. Add the next number instead, and make it
  idempotent where it may meet a schema that already has the change.
  `mise run frozen:check` refuses the edit, the delete and the rename;
  `mise run lint:migrations` holds the naming and numbering, because a file the
  runner cannot parse is a step that silently never runs. The one sanctioned
  exception, a migration corrected before it landed, is `supersededSHA256` in
  `internal/engine/migrate.go` together with the later migration that closes
  the gap.
- **`lint:docs` is the one docs linter**, and it holds two halves. What the
  pages point at: every Markdown link and `#anchor` resolves against the tree,
  offline, so renaming a doc or a heading means fixing what points at it, and
  external URLs are never fetched because somebody else's outage is not this
  repo's build failure. What they say: `.mise/docscheck.sh` refuses a dead
  word from [docs/terms.md](docs/terms.md), a retired envelope key in an
  example, a `mise run` name that no longer exists, and a page
  `docs/README.md` does not list. A mechanical docs rule worth enforcing goes
  in that script, so there is one place to run and one place to add to.
- **A decision that outlives its argument gets a record.**
  [docs/decisions/](docs/decisions/README.md) is one short, dated page per
  choice, frozen once accepted and superseded rather than edited, which
  `mise run frozen:check` holds: an accepted record's body may not change,
  while its `status:` and `superseded-by:` still may. The bar is
  all three of hard to reverse, shapes what other code may do, and reasoning
  not already written down; everything else is a commit. Read the index before
  proposing design work, so an option already rejected is not proposed again.
  A rule here may link its record.
