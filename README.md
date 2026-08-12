# substrate

A personal data substrate: one place that holds everything you own, that other
software talks to instead of keeping its own copy.

One invite code admits a person. Registering creates a **user** — username,
password and TOTP — and that user's one **repository**. Everything they own
lives in it: an append-only changelog is the truth, and the records you read are its
fold, so what you see can never drift from what actually happened. Tokens and
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

# The invite code is the one door. This asks for a username and password,
# prints a TOTP enrollment once for your authenticator, and ends logged in:
# the token it mints is stored as a context in ~/.config/substratectl.
bin/substratectl register --server http://localhost:8080

# A new repository holds the CORE vocabulary and nothing else — there is no
# `tasks` collection to get yet. Install the bundle that ships one. This is
# the same closure the catalog serves, applied from the tree:
bin/substratectl apply \
  -f kinds/tasks.substrate.reamde.dev/bundle.yaml \
  -f kinds/tasks.substrate.reamde.dev/project.yaml \
  -f kinds/tasks.substrate.reamde.dev/task.yaml

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
`POST /api/v1/core.substrate.reamde.dev/catalog/{id}/install` — all three are
the same admission. [`docs/getting-started.md`](docs/getting-started.md) walks
the same path in full.

## Configuration

| Env                            | Default                         | Notes                                                         |
| ------------------------------ | ------------------------------- | ------------------------------------------------------------- |
| `DATABASE_URL`                 | required                        | the one Postgres                                               |
| `PORT`                         | `8080`                          |                                                                |
| `LOG_LEVEL`                    | `info`                          | debug/info/warn/error                                          |
| `WEB_DIR`                      | —                               | the built console, served at `/`; empty disables it            |
| `SUBSTRATE_INVITE_CODE`        | — (unset ⇒ registration closed) | the one door: `POST /register` needs it                        |
| `SUBSTRATE_CREDENTIAL_KEY`     | —                               | seals stored provider tokens; unset ⇒ plaintext and a warning  |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                               | the one redirect URI providers register                        |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                               | signs OAuth flow state                                         |
| `SUBSTRATE_CONSOLE_URL`        | —                               | postMessage origin for the OAuth return page                   |
| `LITELLM_BASE_URL`             | —                               | unset ⇒ no embedder and the embed queue idles                  |
| `LITELLM_API_KEY`              | —                               | falls back to `LITELLM_MASTER_KEY`                             |
| `LITELLM_EMBED_MODEL`          | `openai/text-embedding-3-small` | must be 1536-dim                                               |

## Development

Toolchain is [mise](https://mise.jdx.dev). `mise install` once, then:

```bash
mise run dev            # Postgres in a container + the server on :8080
mise run dev:up         # the same, in the background
mise run dev:status     # what is running, and on which URLs
mise run dev:logs       # follow the background server
mise run dev:restart    # rebuild and restart; the data stays
mise run dev:stop       # stop the server and its Postgres; the data stays
mise run dev:wipe       # DELETE the database — the next start is a fresh substrate
```

`docker compose up` builds an image; `mise run dev` runs the binary from the
tree, so a change is a restart rather than a rebuild. The invite code is
`let-me-in`, the database is a container of its own on `:5433`, and the pid and
log live in `.dev/`.

**`dev:wipe` is the only route back to a fresh substrate.** Registration is
one-shot per user and there is no unregister, so testing the door means
throwing the database away.

The server binds every interface, so the machine's own address reaches it from
another box on the same network — `dev:status` prints the addresses it finds,
including a tailnet one. Nothing is exposed publicly by that; it is a laptop
listening on a laptop's networks.

The console is served at `/` once it is built, and `dev:restart` picks it up:

```bash
mise run console:build  # -> web/console/dist, served by the substrate at /
mise run console:dev    # or the live one on :5173, proxying /api to :8080
```

The operator hat wants the DSN, never HTTP:

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
Postgres in a throwaway container.

## Docs

[`docs/`](docs/README.md) builds one thing — a to-do list — from registration
through to the API call that completes a task: the
[data model](docs/data-model.md), the [API](docs/api.md),
[extensions and functions](docs/extensions.md), and
[running a substrate](docs/operations.md).

`AGENTS.md` is the guide for anyone — person or agent — working on this code.
