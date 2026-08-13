# Agent guide

How to work this repo: the substrate server (`cmd/substrated`), the console
(`web/console`, built into the server image and served by it at `/`) and the
CLI (`cmd/substratectl`). Provider auth and sync live IN the server — the OAuth
facility plus bundle functions.

**The model, in one paragraph.** One invite code admits people. Registering
creates a **user** — username, password and TOTP, all three — and that user's
one **repository**. Everything the user has lives in it: an append-only,
strictly sequential, unsigned **changelog** is the truth and the **records** table is
its fold. Tokens and the login credential are themselves records, and a token
has full access to its repository. There are no tenants, no identities, no
keys, no signatures, no sharing, no scopes and no roles.

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
mise run lint           # golangci-lint; keep it at zero
mise run console:dev    # the console on :5173, proxying /api to :8080
```

**A substrate to work against** — a Postgres container of its own on `:5433`
and the binary from the tree on `:8080`, invite code `let-me-in`, pid and log
under `.dev/`. `docker compose up` builds an image; this does not, so a change
is a restart. Every task is a subcommand of `.mise/dev.sh`.

```bash
mise run dev            # foreground; dev:up is the same in the background
mise run dev:status     # database, server, console, URLs
mise run dev:restart    # rebuild and restart; the data stays
mise run dev:logs
mise run dev:wipe       # DELETE the database: the next start is a fresh substrate
```

`dev:wipe` matters more than it looks: registration is one-shot per user and
there is no unregister, so any change to the door is tested by throwing the
database away — `dev:wipe` here, `docker compose down -v` on the compose path. `bin/substratectl --dsn "$(mise run dev:dsn)" …` is the operator
hat against it, and `mise run console:build` puts the console at `/`.

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
bin/substratectl register                    # invite code, username, password, TOTP enroll
bin/substratectl login --username <you>      # password + TOTP; mints a token record
bin/substratectl kinds                       # every installed kind
bin/substratectl get kinds <ref> -o yaml     # one kind's definition
bin/substratectl get tasks <id> -o yaml      # one record, apply-able envelope
bin/substratectl apply -f record.yaml        # put (merge, never prune)
bin/substratectl watch                       # resumable change stream

# the operator's hat — a DSN, no HTTP
bin/substratectl --dsn "$DATABASE_URL" repository list
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
record's kind REFERENCE (`people.substrate.reamde.dev/person` for a published
kind, a bare `task` for a repository-local one), `metadata.id` the record id,
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
| **plural**    | the collection segment in a path                           | type         |
| **trait**     | a contract a kind implements                               | capability   |
| **vocabulary**| kinds, traits and property types together; `/vocabulary/apply` | schema   |
| **changelog** | the append-only sequence of deltas; the `changelog` table  | log          |
| **bundle**    | the install unit; `/bundles`, and the `bundle` tier         | extension    |
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
`/api/v1/{authority}/{plural}` — the same shape Kubernetes uses to keep
`networking.k8s.io` addressable.

`substrate.reamde.dev` is a **placeholder**. Moving kind identity to URLs (so
anyone can publish kinds from a git repo without owning DNS) is real design
work — routing needs a separator convention, references split on the last slash
instead of the first, the group validator widens — and it has not happened.
Do not half-do it.

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
- **Comments carry constraints, not narration.** Say why a thing must be so,
  or say nothing.
- **Every commit title is a conventional commit** —
  `type(scope): what changed`, with `!` before the colon for a break. The
  types in use here are `feat`, `fix`, `docs`, `refactor`, `test`, `chore`,
  `ci`. This is not style: release-please reads these titles off `main` to
  decide the version and write `CHANGELOG.md`, so a title it cannot parse is a
  release that does not happen. A PR title is one too — the merge is a squash,
  so the PR title IS the commit release-please reads.
- Keep `mise run lint` and `mise run fmt:check` at zero. CI runs both.
