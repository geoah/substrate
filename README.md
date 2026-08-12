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

To talk to it from a terminal:

```bash
mise run build:cli
bin/substratectl register --server http://localhost:8080
bin/substratectl get people
```

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
mise run ci             # the whole pipeline — exactly what CI runs
mise run test           # the Go suite
mise run console:dev    # the console on :5173, proxying to :8080
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
