# substrate

A personal data substrate: one place that holds everything you own, that other
software talks to instead of keeping its own copy.

One invite code admits a person. Registering creates a **user** — username,
password and TOTP — and that user's one **repository**. Everything they own
lives in it: an append-only changelog is the source of truth, and the records
you read are computed by replaying it, so what you see can never drift from
what actually happened. Tokens and
the login credential are records like any other.

Records have a **kind**, kinds are declared in YAML, and a **bundle** installs
a set of them — optionally with functions that sync a provider, so a calendar
or a mailbox becomes records you own rather than an API you rent.

## Quick start

```bash
docker compose up
```

That is the whole thing: Postgres, the API, and the console at
<http://localhost:8080>. Every setting has a working default, so there is
nothing to configure before the first run. Register with the invite code
`let-me-in`.

To talk to it from a terminal — register, teach the repository a vocabulary,
write a record, watch it land:

```bash
mise run build:cli

# Registration needs the invite code. This asks for a username and password,
# prints a TOTP enrollment once for your authenticator, and ends logged in:
# the token it mints is stored as a context in ~/.config/substratectl.
bin/substratectl register --server http://localhost:8080

# A new repository holds the core vocabulary and nothing else — there is no
# `tasks` collection to get yet. Install the bundle that ships one. This is
# the same closure the catalog serves, applied from the tree. Tasks name an
# assignee, so people comes first:
bin/substratectl apply \
  -f kinds/people.substrate.reamde.dev/bundle.yaml \
  -f kinds/people.substrate.reamde.dev/organization.yaml \
  -f kinds/people.substrate.reamde.dev/person.yaml \
  -f kinds/people.substrate.reamde.dev/team.yaml
bin/substratectl apply \
  -f kinds/tasks.substrate.reamde.dev/bundle.yaml \
  -f kinds/tasks.substrate.reamde.dev/project.yaml \
  -f kinds/tasks.substrate.reamde.dev/task.yaml \
  -f kinds/tasks.substrate.reamde.dev/tasklog.yaml

bin/substratectl kinds                   # what this repository knows now
bin/substratectl get tasks               # empty, but the collection is there

cat <<'EOF' | bin/substratectl apply -f -
kind: tasks.substrate.reamde.dev/task
data:
  properties:
    title: Buy milk
    dueAt: 2026-08-13T09:00:00Z
EOF

bin/substratectl get tasks
bin/substratectl patch tasks <id> --state status=done   # stamps completedAt
bin/substratectl watch                   # the changelog, streaming
```

The console's Registry page installs the same bundle from the catalog compiled
into the binary, and so does
`POST /api/v1/core.substrate.reamde.dev/catalog/{id}/install`: all three run
the same install path and validation, which
[docs/vocabulary.md](docs/vocabulary.md#admission) calls admission.
[`docs/getting-started.md`](docs/getting-started.md) walks
the same path in full.

### Running an agent

A fresh repository holds no LLM provider: a provider row carries the wire, the
endpoint and the key, and a substrate cannot invent a key. Registry →
**Examples** → the LLM example installs two keyless rows (`anthropic`,
`openai`) and an agent chain that uses them, or apply the same closure from the
tree:

```bash
bin/substratectl apply \
  -f kinds/llm.examples.substrate.reamde.dev/bundle.yaml \
  -f kinds/llm.examples.substrate.reamde.dev/providers.yaml
```

Then put a key on the row — it is an ordinary record write, and `apiKey` is
secret-typed, so it reads back redacted ever after:

```bash
cat <<'EOF' | bin/substratectl apply -f -
kind: core.substrate.reamde.dev/llmprovider
metadata: {id: anthropic}
data:
  properties: {apiKey: sk-ant-…}
EOF
```

The console's **Agents** page then has something to chat with: a thread per
run in the left rail, the transcript rebuilt from records, and every tool call
expandable to its request and response.
[`docs/agents.md`](docs/agents.md) has the rest — wires, pricing, sub-agents,
budgets.

## Configuration

| Env                            | Default                         | Notes                                                         |
| ------------------------------ | ------------------------------- | ------------------------------------------------------------- |
| `DATABASE_URL`                 | required                        | the one Postgres                                               |
| `PORT`                         | `8080`                          |                                                                |
| `LOG_LEVEL`                    | `info`                          | debug/info/warn/error                                          |
| `WEB_DIR`                      | —                               | the built console, served at `/`; empty disables it            |
| `SUBSTRATE_INVITE_CODE`        | — (unset ⇒ registration closed) | required by `POST /register`                                   |
| `SUBSTRATE_CREDENTIAL_KEY`     | —                               | seals the sealed store (provider tokens, every secret property's material); unset ⇒ plaintext and a warning |
| `SUBSTRATE_INSECURE_DISABLE_TOTP` | `false`                      | **local development only**: stops verifying the second factor, so a password is the whole credential |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                               | the one redirect URI providers register                        |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                               | signs OAuth flow state                                         |
| `SUBSTRATE_CONSOLE_URL`        | —                               | postMessage origin for the OAuth return page                   |
| `SUBSTRATE_LLM_BASE_URL`       | —                               | the host gateway: embeddings, and any provider row naming no `baseURL` |
| `SUBSTRATE_LLM_API_KEY`        | —                               | its bearer; unset ⇒ no embedder and the embed queue idles      |
| `SUBSTRATE_LLM_EMBED_MODEL`    | `text-embedding-3-small`        | must be 1536-dim                                               |

Those three configure the **host gateway** and nothing else; every other place
an agent buys completions is a provider record, as
[docs/agents.md](docs/agents.md#providers) explains.

## Development

Toolchain is [mise](https://mise.jdx.dev). `mise install` once, then:

```bash
mise run dev            # Postgres in a container + the server on :8080
mise run dev:totp       # the same, with the second factor enforced
mise run dev:up         # dev, in the background
mise run dev:status     # what is running, on which URLs, whether TOTP is enforced
mise run dev:logs       # follow the background server
mise run dev:restart    # rebuild and restart; the data stays
mise run dev:stop       # stop the server and its Postgres; the data stays
mise run dev:wipe       # delete the database — the next start is a fresh substrate
```

`docker compose up` builds an image; `mise run dev` runs the binary from the
tree, so a change is a restart rather than a rebuild. The invite code is
`let-me-in`, the database is a container of its own on `:5433`, and the pid and
log live in `.dev/`.

**A fresh substrate means deleting the database.** Registration is one-shot per
user and there is no unregister, so testing registration twice means throwing
the database away: `mise run dev:wipe` on this path, `docker compose down -v` on the compose
one.

**The second factor is off here, and nowhere else.** Every `mise run dev*`
task except `dev:totp` sets `SUBSTRATE_INSECURE_DISABLE_TOTP=true`, so local
registering and signing in take only a username and a password, and no
authenticator entry is enrolled for a repository `dev:wipe` will delete
tomorrow. Test a change to auth under `mise run dev:totp`, which runs the same
substrate with the factor enforced. Never set the variable anywhere a
substrate is reachable, where it would make a leaked password the account.
[docs/auth.md](docs/auth.md#the-second-factor-can-be-switched-off-locally)
says exactly what the switch does and does not change, including the reset a
user registered without an authenticator needs.

The server binds every interface, so anything on the same LAN or tailnet
reaches it — `dev:status` prints the addresses it finds. That is the point when
you are testing from a phone or a second laptop, and it also means the invite
code is reachable by everyone on those networks: registration is open for as
long as one is set. Set `SUBSTRATE_INVITE_CODE` to something of your own, or
`mise run dev:stop` when you are done. None of it reaches the internet.

The console is served at `/` once it is built, and `dev:restart` picks it up:

```bash
mise run console:build  # -> web/console/dist, served by the substrate at /
mise run console:dev    # or the live one on :5173, proxying /api to :8080
```

Operator commands talk to Postgres directly over the DSN, never over HTTP:

```bash
bin/substratectl --dsn "$(mise run dev:dsn)" repository list
mise run dev:psql       # or a psql shell straight into it
```

The rest:

```bash
mise run ci             # the whole pipeline — exactly what CI runs
mise run test           # the Go suite
mise tasks              # everything else
```

Docker is needed for the engine suite, which runs each case against a real
Postgres in a throwaway container. `mise run test:llm` is the one suite that
talks to real LLM providers; it skips itself without keys, and
[docs/testing.md](docs/testing.md) says how to give it some.

## Changing a kind

Every declaration under `kinds/` carries a `version` (Kubernetes-style:
`v1alpha1`, `v1beta2`, `v1`), either its own `data.version` on a kind or the
authority's in `bundle.yaml`. That version is the entire upgrade signal: a
repository picks up a changed declaration only when its version moved, at boot
for core and through the console's Registry for installed bundles. So the rule
is one sentence: **change a declaration's `data`, bump its version.** Adding a
property or an enum value is a bump; removing or retyping one is a breaking
change the server refuses while live records hold the old shape, so keep the
old property and add a new one instead.

CI enforces the rule (`mise run kinds:check`): a PR that edits a declaration
without moving its version, or removes one without bumping its authority, does
not merge.

A core declaration is also read by a generator: `internal/corekinds` and the
console's `corekinds.ts` are produced from `kinds/` and checked in, so an edit
under `kinds/core.substrate.reamde.dev/` is followed by `mise run kinds:gen`.
CI enforces that too (`mise run kinds:gen:check`).

## Docs

[`docs/`](docs/README.md) builds one thing — a to-do list — from registration
through to the API call that completes a task: the
[data model](docs/data-model.md), the [API](docs/api.md),
[bundles](docs/bundles.md), [functions](docs/functions.md), and
[running a substrate](docs/operations.md).

`AGENTS.md` is the guide for anyone — person or agent — working on this code.
