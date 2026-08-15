# Running a substrate

The substrate is one Go binary and one Postgres database. It serves the
[API](api.md) and the [console](console.md) on one port, runs its own
background loops in-process, and needs nothing else to be useful. This page is
how to stand one up and look after it.

## What it needs

- **Postgres**, with the `vector` and `pgcrypto` extensions available. The
  binary runs its own migration at boot, creates the two roles isolation rests
  on (`substrate_app`, bound by row level security, and `substrate_maint`,
  which bypasses it for registration and cross-repository lookups), and enables
  the extensions, so the DSN it starts with must be allowed to do those things.
- **One port**. The service serves the API under `/api`, the authentication
  endpoints beside it, and the console at `/`.
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
| `SUBSTRATE_INVITE_CODE`        | — (unset: registration is off)         | The one way in. See below.                                                                                  |
| `SUBSTRATE_CREDENTIAL_KEY`     | —                                      | Seals the sealed store, which holds every secret-typed property's material (AES-256-GCM), and the per-repository changelog signing seeds. Signing is mandatory, so a host without it refuses to boot (the insecure switch below is the local-testing exception); `repository reseal` upgrades unsealed payloads once it is set. |
| `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES` | `false`                 | **Local testing only.** Changelog signing is mandatory (an Ed25519 key per repository, sealed under the credential key, signs every entry's chain hash), so a host without `SUBSTRATE_CREDENTIAL_KEY` refuses to boot. This switch lets a keyless host run anyway: signing never activates, every entry carries the all-zero placeholder signature, and `repository verify` names the state as a finding. It does NOT weaken an activated repository — **activation is one-way**, and an activated repository whose key cannot open refuses to append regardless. Pre-v1 scaffolding ([#175](https://github.com/geoah/substrate/issues/175)). See [the chain](changelog.md#the-chain). |
| `SUBSTRATE_INSECURE_DISABLE_TOTP` | `false`                             | **Local development only.** Stops verifying the second factor, so a password is the whole credential: see [the local TOTP-off switch](auth.md#the-second-factor-can-be-switched-off-locally). Boots with a warning, and `GET /.well-known/substrate/server.json` says so. |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                                      | Signs OAuth flow state. Unset mints a random key per boot, with a warning: flows in progress break on restart. |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                                      | The one redirect URI every provider app registers.                                                        |
| `SUBSTRATE_CONSOLE_URL`        | —                                      | The console origin the OAuth return-page posts to and falls back to redirecting into. Empty is local dev. |
| `SUBSTRATE_LLM_BASE_URL`       | — (unset: no embedder)                 | The host's OpenAI-compatible gateway: it backs embeddings, and it is the endpoint an [`llmprovider`](agents.md#providers) row that names no `baseURL` resolves to. Nothing seeds such a row: the [LLM example bundle](bundles-catalog.md#llm-example) is what ships one. |
| `SUBSTRATE_LLM_API_KEY`        | —                                      | The bearer for that gateway, and the fallback key for a provider row that names neither a `baseURL` nor an `apiKey`. Absent means no embedder: the embed queue simply does not drain. |
| `SUBSTRATE_LLM_EMBED_MODEL`    | `text-embedding-3-small`               | Must be a 1536-dimension model.                                                                           |
| `SUBSTRATE_SANDBOX`            | `best-effort`                          | How hard to confine function bodies: `off`, `best-effort`, or `enforce` (refuse to run a body unconfined). |

`SUBSTRATE_CREDENTIAL_KEY` is the one that must be backed up beside the
database: without it, sealed material is unreadable.

`SUBSTRATE_LLM_API_KEY` travels to `SUBSTRATE_LLM_BASE_URL` and nowhere else:
an [`llmprovider`](agents.md#providers) row that names its own `baseURL` must
carry its own `apiKey`, so a repository-chosen endpoint can never be handed the
host's bearer.

## The function sandbox

Function bodies are third-party code, and the substrate confines them with
Landlock, seccomp and rlimits: see [the sandbox](functions.md#the-sandbox) for
what each layer closes. Two things an operator needs to know:

**Check the boot log.** The substrate reports the sandbox once at startup,
naming the kernel's actual Landlock ABI. If a layer is missing the line is an
ERROR, not a warning, because a confinement that silently does less than it
claims is worse than none. A real deployment should run `SUBSTRATE_SANDBOX=enforce`,
which turns that into a refusal to run bodies at all.

**Both layers work in a stock container**: Docker's and containerd's default
seccomp profiles permit the `landlock_*` and `seccomp` syscalls, and neither
needs a capability. What does **not** work in a stock container is anything built
on user namespaces or cgroup delegation: `CLONE_NEWUSER` is denied by the
default profile and `/sys/fs/cgroup` is mounted read-only, which is why the
sandbox has no memory or process-count ceiling. Do not add `--privileged` to
try to get one.

## The invite code

`SUBSTRATE_INVITE_CODE` is the only way a user gets created. Set it, register,
then unset it and restart: with it unset, registration is closed
(`501 unsupported`). Registration is rate-limited (paced, with no failure
lockout) whether or not the code is set ([users and tokens](auth.md)).

There is no admin user and no operator password. Everything privileged happens
on the box, through the DSN.

## What happens at boot

- The migration runs, under an advisory lock, and the roles and Postgres
  extensions are ensured.
- Each repository is opened the first time something touches it. Opening
  rebuilds its kind registry **from its own stored declaration records** —
  nothing on the serving path reads the binary's embedded tree.
- **The chain backfill runs first**: a repository whose changelog predates
  entry hashes gets them at its first open, oldest-first in bounded chunks,
  before anything else writes, and the backfill records a `backfill` chain
  epoch naming where attested history begins
  ([the chain](changelog.md#the-chain)). A large history takes proportional
  time, once, and the open logs progress.
- **Shipped vocabulary is upgraded, per repository, in one transaction**: the
  first open under a new binary appends the version diff to that repository's
  changelog under the `substrate` actor
  ([the boot-time upgrade](vocabulary.md#how-the-vocabulary-reaches-a-repository)).
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
scale. The chain leans on the same shape: one writer process per database, so
an older binary can never append unhashed entries behind a newer one's back
(`repository verify` reports exactly that state if it ever happens).

## Upgrading the binary

**Snapshot the database before you deploy.** A repository's first open under a
new binary may promote its stored
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract),
and the promotion this binary carries rewrites every declaration row the
repository holds. That is the one step of an upgrade a rollback cannot undo, so
the dump you take beforehand is the only way back.

**A dialect promotion is one transaction, and it is one-way.** Each repository
carries a monotonic dialect integer. When a binary's maximum is above the stored
one, the first open of that repository runs the promotion and stamps the new
number **inside the same transaction as the row rewrite**, indivisibly: a crash
leaves the store wholly on the old dialect and the next open tries again, and a
store can never hold new rows under an old stamp.

**Downgrading after that open is impossible without a restore.** An older binary
meeting the newer stamp refuses to open the repository. That is the named
refusal, which the API surfaces as `503 unavailable` with a `Retry-After`, never
as an invalid token, so a store the binary cannot serve is diagnosable rather
than mysterious. That refusal is the *good* outcome: it exists because the older
binary would otherwise misread the migrated rows. Rolling the image back is
therefore not a fix; restoring the pre-upgrade dump is. Rolling *forward* to a
binary whose maximum covers the stamp still is.

**The changelog carries a dialect of its own, and it refuses the same way.**
Beside the vocabulary stamp each repository carries a
[changelog dialect](changelog.md#the-dialect-a-changelog-is-written-in): what a
binary must understand to replay its entries. A binary claims it in the first
transaction it appends with, so an older binary meeting a newer stamp refuses
the open instead of serving a history it could not rebuild, while a new binary
that opened a repository and wrote nothing leaves the rollback open. Nothing is
rewritten and there is no promotion step: a changelog is append-only, so old
entries keep the spelling they were written in.

**The promotion refuses rather than guesses.** It translates every declaration
row this repository holds, and if one installed closure no longer parses under
the new binary it fails the open, logging the authority and the reason, instead
of migrating the rest: stamping a store with one un-migrated row would leave two
encodings in it, and no reader could tell which one it was holding. The repair is
to re-install that bundle (or open once under the binary that wrote it) before
the new binary migrates the repository. A repository whose only trouble is a
tightened contract is a different case, below.

[Quarantine](vocabulary.md#quarantine) is that other case: a binary that tightens
a contract quarantines each installed bundle whose stored closure no longer
admits, rather than bricking the repository, and re-installing the bundle (or a
later open under a binary that relaxed the contract) clears the marker.
Quarantine is a state a migrated repository may reach, never one it may be
migrated in.

## Backups

**A backup is the changelog plus blobs plus sealed, as one unit.** All three live in
the one Postgres database, so an ordinary consistent dump of that database is a
complete backup. The sealed rows encrypt under each repository's own
data-encryption key, which exists in two wraps: the control-plane
`repositories.dek` column holds it wrapped under `SUBSTRATE_CREDENTIAL_KEY`
(the host's half), and the repository's `recoverykey` record holds it
wrapped to the user's age recipient (the user's half). Either the host key
or the user's recovery identity opens a backup; losing both makes the
sealed rows inert.

What you do **not** have to back up separately is the fold. The records table
and its indexes are derived; the changelog is the truth.

## Operator recovery

Operator commands (the "operator hat" of
[substratectl](substratectl.md#two-hats)) speak to Postgres directly over the
DSN and hold no token. They need `--dsn` (or `DATABASE_URL`), and refuse
before touching anything without one.

```
DATABASE_URL=… substratectl repository list
DATABASE_URL=… substratectl repository inspect ada
DATABASE_URL=… substratectl repository verify ada
DATABASE_URL=… substratectl repository rebuild ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substratectl repository reseal ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substratectl user reset ada
```

- **`repository list`** reads the one control-plane table: one row per user.
- **`repository inspect <username>`** reports the repository id, the username,
  when it was created, the changelog head and entry count, live and tombstoned record
  counts, and the declaration versions per authority. It is the first thing to
  run when something looks wrong.
- **`repository verify <username>`** walks the whole chain in one read-only
  snapshot: it recomputes every entry's hash from the stored bytes, checks
  every signature the signing state requires, checks the chain epochs, and
  reports either the verified head `(seq, hash)` or every finding by seq and
  name. Entries below the activation seq carry the all-zero placeholder
  signature (history from before signing, which nothing sanctioned can sign
  after the fact); verify counts those in one line rather than naming each,
  while a placeholder at or after the activation seq, and a repository that
  never activated at all, are findings. It never backfills or repairs the repository it judges (opening the
  engine still applies pending schema migrations, as every operator command
  does), and run beside an in-flight first-open backfill it can report
  transient unhashed entries — re-run once the open settles. **Write the
  head down somewhere else and pass it back**: `--expect-head seq:hash`,
  `--expect-public-key` and `--expect-signed-from` turn your out-of-band
  knowledge into enforced findings, and a pinned head is the only way to
  catch a truncated tail. Run it before and after a Postgres major upgrade,
  and on every restored backup. Exits nonzero on any finding.
- **`repository rebuild <username>`** replays the whole changelog into a fresh fold,
  in one transaction, under that repository's own lock. It reproduces the fold
  bit for bit and appends nothing, so it is safe to run on a healthy
  repository. **It verifies the chain first** and refuses to install history
  that does not check out; `--force-unverified` overrides that, says so in its
  output, and is for the day you have decided the bytes are what you have.
  It does not touch blobs or sealed rows — those were never in the
  changelog — and it leaves runtime state (trigger cursors, OAuth flows) alone,
  because a cursor is a consumer's position in the changelog, not a fold of it.
- **`repository reseal <username>`** moves every legacy secret value into
  the sealed store and re-points record rows and the changelog's stored
  payloads at the refs; it also upgrades sealed-store payloads written while
  the server ran without `SUBSTRATE_CREDENTIAL_KEY`. Run it once after
  upgrading past the release that moved secrets into the store: the
  changelog is append-only, so plaintext it already carries can be removed by
  nothing else. Values-only and idempotent, and it refuses until the server
  has opened the repository once under the upgraded binary. Because it is the
  one sanctioned rewrite of history, it VERIFIES THE CHAIN FIRST and refuses
  over history that does not check out (a reseal would otherwise launder
  tampering into fresh hashes and signatures), then re-chains (and re-signs)
  every entry from the first rewritten seq and records a `reseal` chain
  epoch naming the old head and the new: a head you wrote down before a
  reseal will not match after, and the epoch is what explains it. On a
  signed repository it refuses without the signing key.
- **`user reset <username>`** is the answer to a user who has lost both
  factors. It writes fresh sealed material and a new credential record and
  prints a fresh TOTP enrollment. The data is untouched; the account gets new
  keys. There is no self-serve recovery, deliberately.

The signing seed has one copy outside the database: registration hands it to
the user, once ([the chain](changelog.md#the-chain)). With it the user can
derive the public key and verify a backup's signatures with no server, and
feed `verify --expect-public-key` from their own record of it. It does not
soften the credential-key rule: a lost credential key still stops writes on an
activated repository, and no path consumes a user-provided seed today (an
operator re-seal from that copy is a possible follow-up, not built).

Two rules keep operator commands honest, and they explain the output: the CLI
opens the engine with an empty registry, so an operator command can never
overwrite a repository's stored vocabulary with the declarations compiled into
the CLI's own build; and its reads assume the `substrate_app` role, because
row level security does
not bind a superuser and an operator DSN usually is one — without that,
`inspect` would count every repository's rows and report them as one user's.

## What it does not do

No sharing, no second user reading your repository, no cross-repository query.
No erasure, compaction, or retention policy: the changelog keeps everything, and the
horizon stays 0. The chain and its signatures are tamper EVIDENCE, not tamper
proofing, and never evidence against the host operator, who holds the database
and the credential key alike ([what the chain proves](changelog.md#the-chain)).
Each of those is a deliberate absence, not an oversight.

Next: [the live tests](testing.md), the one suite that talks to real LLM
providers.
