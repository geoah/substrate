# Running a substrate

The substrate is one Go binary and one Postgres database. It serves the
[API](api.md) and the [console](console.md) on one port, runs its own
background loops in-process, and needs nothing else to be useful. This page is
how to stand one up and look after it.

## What it needs

- **Postgres**, with the `vector` and `pgcrypto` bundles available. The
  binary runs its own migration at boot, creates the two roles isolation rests
  on (`substrate_app`, bound by row level security, and `substrate_maint`,
  which bypasses it for registration and cross-repository lookups), and enables
  the bundles, so the DSN it starts with must be allowed to do those things.
- **One port**. The service serves the API under `/api`, the door endpoints
  beside it, and the console at `/`.
- **Nothing else.** Search, the change feed, the function runner, and the OAuth
  facility are all in the one process; the image also carries `python3`, the Go
  toolchain, and `uv`, because [functions](functions.md) run as child
  processes of the substrate.

Everything lives in one Postgres schema. Repositories are separated by a
`repository` column plus `FORCE ROW LEVEL SECURITY` keyed on the authenticated
token's repository — enforced by the database, not by discipline in the query
layer, and it fails closed.

## Configuration

There is no settings surface: configuration is the environment, read once at
boot.

| Variable                       | Default                                | What it does                                                                                              |
| ------------------------------ | -------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `DATABASE_URL`                 | required                               | The one Postgres holding every repository.                                                                |
| `PORT`                         | `8080`                                 | The port served.                                                                                          |
| `LOG_LEVEL`                    | `info`                                 | `debug`, `info`, `warn`, `error`.                                                                         |
| `WEB_DIR`                      | —                                      | The built console, served at `/`. Empty disables static serving.                                          |
| `SUBSTRATE_INVITE_CODE`        | — (unset: registration is off)         | The one door. See below.                                                                                  |
| `SUBSTRATE_CREDENTIAL_KEY`     | —                                      | Seals the credential store (AES-256-GCM). Unset stores provider tokens unsealed, with a boot warning.     |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                                      | Signs OAuth flow state. Unset mints a random key per boot, with a warning: flows in progress break on restart. |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                                      | The one redirect URI every provider app registers.                                                        |
| `SUBSTRATE_CONSOLE_URL`        | —                                      | The console origin the OAuth return-page posts to and falls back to redirecting into. Empty is local dev. |
| `LITELLM_BASE_URL`             | — (unset: no embedder)                 | The OpenAI-compatible gateway backing embeddings and the agent loop.                                      |
| `LITELLM_API_KEY`              | — (falls back to `LITELLM_MASTER_KEY`) | Absent means no embedder: the embed queue simply does not drain.                                          |
| `LITELLM_EMBED_MODEL`          | `openai/text-embedding-3-small`        | Must be a 1536-dimension model.                                                                           |

`SUBSTRATE_CREDENTIAL_KEY` is the one that must be backed up beside the
database: without it, sealed material is unreadable.

## The invite code

`SUBSTRATE_INVITE_CODE` is the only way a user gets created. Set it, register,
then unset it and restart — with it unset the registration endpoints answer
`501 unsupported`, which is the right resting state for a substrate that
already has its user. Registration is rate-limited and lockout-guarded whether
or not the code is set ([users and tokens](auth.md)).

There is no admin user and no operator password. Everything privileged happens
on the box, through the DSN.

## What happens at boot

- The migration runs, under an advisory lock, and the roles and bundles are
  ensured.
- Each repository is opened the first time something touches it. Opening
  rebuilds its kind registry **from its own stored declaration records** —
  nothing on the serving path reads the binary's embedded tree.
- **Shipped vocabulary is upgraded, per repository, in one transaction.** Every
  declaration carries a version; the first open under a new binary diffs the
  binary's shipped declarations against the stored ones and appends the
  difference to that repository's changelog as explicit entries under the `substrate`
  actor. Same-or-newer only: never a downgrade, never a prune. An unchanged
  tree writes nothing at all, and a repository nobody opens is never touched.
- Persisted function bodies re-warm in the background. One that no longer
  prepares logs an error naming the function, and its deliveries park rather
  than the repository failing.

Four loops then run in-process: the trigger dispatcher every 5 seconds, garbage
collection every 5 minutes, OAuth refresh and finalizer processing every
minute, and — when an embedder is configured — the embed-queue drain every
minute. Each enumerates repositories and opens each one through the same
row-level-security-bound pool a request uses.

Keep it to **one replica**. The watch signal and the trigger dispatcher are
in-process, and two dispatchers would serialize on compare-and-swap rather than
scale.

## Upgrading the binary

Two safety rails matter when you roll a new image forward.

**The vocabulary dialect** is a monotonic integer stamped on each repository,
describing the shape its stored declarations are written in. A binary whose
maximum is **below** a repository's stored dialect refuses to open it, with a
named error, rather than misreading rows a newer shape wrote. The API surfaces
that refusal as `503 unavailable` with a `Retry-After` — never as an invalid
token, so a store the binary cannot serve is diagnosable. Rolling back to the
older binary, or forward to a newer one, is the fix.

**Quarantine** covers the other direction. A binary that tightens a contract
can make an already-installed bundle's stored closure fail admission. The
repository is not bricked: it installs the maximal admissible subset and
quarantines the rest, logging each failed authority with its reason, leaving it
out of the live registry (its kinds refuse writes, its callables do not run)
and marking it on its `authority` record so the console shows "needs
re-install". Re-installing that bundle clears the marker, and so does a
later open under a binary that relaxed the contract again.

## Backups

**A backup is the changelog plus blobs plus sealed, as one unit.** All three live in
the one Postgres database, so an ordinary consistent dump of that database is a
complete backup — plus `SUBSTRATE_CREDENTIAL_KEY`, without which the sealed
rows are inert.

What you do **not** have to back up separately is the fold. The records table
and its indexes are derived; the changelog is the truth.

## Operator recovery

The operator's hat on [substratectl](substratectl.md) speaks to Postgres directly and holds
no token. It needs `--dsn` (or `DATABASE_URL`), and refuses before touching
anything without one.

```
DATABASE_URL=… substratectl repository list
DATABASE_URL=… substratectl repository inspect ada
DATABASE_URL=… substratectl repository rebuild ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substratectl user reset ada
```

- **`repository list`** reads the one control-plane table: one row per user.
- **`repository inspect <username>`** reports the repository id, the username,
  when it was created, the changelog head and entry count, live and tombstoned record
  counts, and the declaration versions per authority. It is the first thing to
  run when something looks wrong.
- **`repository rebuild <username>`** replays the whole changelog into a fresh fold,
  in one transaction, under that repository's own lock. It reproduces the fold
  bit for bit and appends nothing, so it is safe to run on a healthy
  repository. It does not touch blobs or sealed rows — those were never in the
  changelog — and it leaves runtime state (trigger cursors, OAuth flows) alone,
  because a cursor is a consumer's position in the changelog, not a fold of it.
- **`user reset <username>`** is the answer to a user who has lost both
  factors. It writes fresh sealed material and a new credential record and
  prints a fresh TOTP enrollment. The data is untouched; the account gets new
  keys. There is no self-serve recovery, deliberately.

Two disciplines keep the operator hat honest, and are worth knowing because
they explain its output: it opens the engine with an empty registry, so an
operator command can never re-vocabulary a repository from the CLI's own build;
and its reads assume the `substrate_app` role, because row level security does
not bind a superuser and an operator DSN usually is one — without that,
`inspect` would count every repository's rows and report them as one user's.

## What it does not do

No sharing, no second user reading your repository, no cross-repository query.
No erasure, compaction, or retention policy: the changelog keeps everything, and the
horizon stays 0. The changelog is unsigned — trusted storage, not evidence. Each of
those is a deliberate absence, not an oversight.

Next: the [built-in kinds](builtin-kinds.md), the vocabulary every repository
starts with.
