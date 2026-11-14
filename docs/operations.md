# Running a substrate

The substrate is one Go binary and one Postgres database. It serves the
[API](api.md) and the [console](console.md) on one port, runs its own
background loops in-process, and needs nothing else to be useful. This page is
the deployment: what it needs, how it is configured, what a boot does, and the
backup and recovery procedures. A substrate on your own machine is
[running one locally](running-locally.md), which is shorter.

## What it needs

- **Postgres 16 or later**, with the `vector` and `pgcrypto` extensions
  available. A typed filter or order asks `pg_input_is_valid`, new in 16,
  whether a stored value casts before it casts it. The
  binary runs its own migration at boot, creates the two roles isolation rests
  on (`substrate_app`, bound by row level security, and `substrate_maint`,
  which bypasses it for registration and cross-repository lookups), and enables
  the extensions, so the DSN it starts with must be allowed to do those things.
- **pgvector 0.7 or later**, and 0.8 or later for the semantic half of
  hybrid search. Migration 0015 indexes `halfvec`, which pgvector added in
  0.7, so the boot reads the installed version first and, below 0.7, refuses
  with the version it found before it applies any migration. `compose.yaml`
  and the dev and test tasks run `pgvector/pgvector:pg16`, which carries 0.8;
  a distribution package may be older (Ubuntu 24.04's
  `postgresql-16-pgvector` is 0.6), and a host that pulled the image long
  ago may still run an older one. `SELECT extversion FROM pg_extension
  WHERE extname = 'vector'`, run in the substrate database, shows the
  version (no row there means the extension is not installed in it). To
  upgrade: with compose, `docker compose pull` and recreate the database
  container; on your own host, install pgvector 0.7 or later; on a managed
  service, move to an engine version whose pgvector is 0.7 or later. Then
  run `ALTER EXTENSION vector UPDATE` in the substrate database.
- **One port**. The service serves the API under `/api`, the authentication
  endpoints beside it, and the console at `/`. Two routes are live streams:
  an agent chat (`POST …/core/agent/{name}/chat`) and the change feed with
  `watch=1`. A proxy in front of the service must pass their lines through as
  they are written. Both send `X-Accel-Buffering: no`, which nginx and its
  ingress honor, and `Cache-Control: no-store, no-transform`, which stops an
  intermediary from compressing them. A proxy that ignores both turns a
  streamed chat reply into one block at the end.
- **A TLS terminator** in front of it for anyone beyond loopback. The server
  speaks plain HTTP only, listens on `127.0.0.1` unless told otherwise, and
  exposing it remotely without TLS is unsupported: see
  [TLS and the reverse proxy](#tls-and-the-reverse-proxy).
- **Nothing else.** Search, the change feed, the function runner, and the OAuth
  facility are all in the one process; the image also carries `python3` and
  `uv`, because [functions](functions.md) run as child processes of the
  substrate. It carries `substratectl` too, so the operator commands below run
  inside the container.

Everything lives in one Postgres schema. Repositories are separated by a
`repository` column plus `FORCE ROW LEVEL SECURITY` keyed on the authenticated
token's repository — enforced by the database, not by discipline in the query
layer, and it fails closed.

## The published image

`ghcr.io/geoah/substrate` is one image for `linux/amd64` and `linux/arm64`,
built by the tree's one `Dockerfile` on both paths. A commit on `main` is
tagged `main-<sha12>`, the first twelve hex digits of the commit
(`main-1a2b3c4d5e6f`), once its whole push `ci` run has succeeded. That tag
is pushed once and never moved, so a deployment that tracks `main` pins it
and pulls the same build every time. `latest` is the tip of `main`: the same
image as the tip's `main-<sha12>`, under a second name. A commit whose `ci`
run failed any job, the jobs a pull request does not require included, gets
no `main-<sha12>`, and while it is the tip `latest` stays on the previous
one. Both report their version the way
`git describe` spells it (`v0.85.0-4-g1a2b3c4`) and carry the full commit in
the `org.opencontainers.image.revision` label, which
`docker buildx imagetools inspect <ref> --format '{{json .Image}}'` shows
without a pull. A release is tagged `v0.85.0`, `0.85.0`,
`0.85`, `v0` and `0`, and reports `v0.85.0`. `v0` is the newest v0.x
release: the release workflow moves it to each new version, and never to a
prerelease or back to an older one, and once v1.0.0 ships `v1` moves the same
way while `v0` stays at the last v0.x. `stable` is a tag an earlier release
left at v0.113.0; it is not moved again. `GET
/.well-known/substrate/server.json` says which one is running. `compose.yaml` builds from the tree; to run a
published image instead, replace its `build: .` with
`image: ghcr.io/geoah/substrate:0.85.0`.

`ghcr.io/geoah/substrate-slack-socket` is the Slack Socket Mode bridge
([`cmd/slack-socket`](../cmd/slack-socket/README.md)), the Dockerfile's
`slack-socket` stage, for the same two platforms. It gets `main-<sha12>` and
`latest` from the same workflow and under the same rules as the server, so a
deployment pins the bridge and the server at the same commit. Releases do not
publish it yet: it has no version tags.

The server runs as uid 65532 and writes two paths: the data root
(`SUBSTRATE_DATA_ROOT`, `/var/lib/substrate` in `compose.yaml`) and `/keys`,
where the compose entrypoint mints the credential key. The image creates both,
owned by that uid, and a fresh named volume mounted over either inherits the
ownership. A bind mount does not: Docker leaves a host directory's owner
alone, so a host path mounted at either must be owned by uid 65532 before the
first start, or boot fails with
`mkdir /var/lib/substrate/repositories: permission denied` and the container
restarts until it is.

```
install -d -o 65532 -g 65532 /srv/substrate/data /srv/substrate/keys
```

`mise run image:smoke ghcr.io/geoah/substrate:0.85.0` boots an image under
`compose.yaml` against a throwaway Postgres and checks exactly this: the
process answers `/healthz`, runs as uid 65532, owns its data root, minted
its key and names a version at `GET /.well-known/substrate/server.json`, and
the console it serves renders in headless Chrome (it registers a throwaway
repository to reach the signed-in pages). CI runs it over every image it
builds.

**Container settings.** `compose.yaml` runs the substrate service with
`cap_drop: [ALL]` and `cap_add: [SYS_PTRACE]` (the connect gate needs that one
capability, see [the function sandbox](#the-function-sandbox)),
`security_opt: [no-new-privileges:true]`, `pids_limit: 4096` and
`stop_grace_period: 30s`, with a commented `mem_limit` to size to the host.
The server runs as uid 65532 with no effective capability, so the first two
bound what a setuid binary or a file capability could reach if one ever
landed in the image. The limits are the only process and memory ceiling a
function body has, because the sandbox cannot write a cgroup, and they bound
the substrate together with its bodies: a runaway body cannot exhaust the
host or Postgres, and a body that reaches a limit takes the substrate down
until the restart policy brings it back. The Postgres service keeps Docker's
defaults, because its entrypoint needs `CHOWN` and `SETUID` to initialize a
volume.

The Kubernetes equivalent, on the substrate's container:

```yaml
securityContext:
  runAsNonRoot: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
    add: [SYS_PTRACE]
  seccompProfile:
    type: RuntimeDefault
resources:
  limits:
    memory: 4Gi
```

The image's user is numeric (`USER 65532:65532`), so `runAsNonRoot` needs no
`runAsUser`. A container has no process limit of its own in Kubernetes: the
kubelet's `podPidsLimit` sets one per pod. `readOnlyRootFilesystem: true`
works only with writable volumes at `/tmp`, where the server stages a blob
upload, and at `$HOME` (`/home/substrate`), where the runner keeps every
installation's work directory and uv's cache.

**Stopping takes up to about 30 s.** On `SIGTERM` the server waits up to 10 s
for requests in flight, then for the background passes in flight, then up to
10 s for the engine's detached tasks and up to 10 s more for the repository
pool to close. Docker's default stop timeout is 10 s, after which it sends
`SIGKILL`, so `compose.yaml` sets `stop_grace_period: 30s`. Kubernetes'
default `terminationGracePeriodSeconds` is 30.

## Configuration

There is no settings surface: configuration is the environment, read once at
boot.

| Variable                       | Default                                | What it does                                                                                              |
| ------------------------------ | -------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `DATABASE_URL`                 | required                               | The one Postgres holding every repository.                                                                |
| `PORT`                         | `8080`                                 | The port served.                                                                                          |
| `SUBSTRATE_BIND_ADDRESS`       | `127.0.0.1`                            | The interface the port is served on. Any address that is not loopback (`0.0.0.0`, empty, a LAN address) refuses the boot, naming the address, unless `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT` is set. The image sets `0.0.0.0`. See [TLS and the reverse proxy](#tls-and-the-reverse-proxy). |
| `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT` | `false`                          | Admits a non-loopback `SUBSTRATE_BIND_ADDRESS`. It states that only a TLS terminator or a loopback port mapping reaches the port; the server serves plain HTTP either way, and the boot warns with the address. The image and `compose.yaml` set it. |
| `LOG_LEVEL`                    | `info`                                 | `debug`, `info`, `warn`, `error`.                                                                         |
| `LOG_FORMAT`                   | `text`                                 | `text` or `json`, in any case: how each line is written to stderr. `text` is `key=value` for a person at a terminal; `json` is one object per line, its attributes as keys, for a collector that parses fields. A deployment that ships its logs to a collector sets `json`. Any other value refuses the boot, naming the variable. |
| `WEB_DIR`                      | —                                      | The built console, served at `/`. Empty disables static serving.                                          |
| `SUBSTRATE_SITE_DIR`           | unset (no site)                        | An absolute path to a site directory: a `site.yaml` naming the brand the console's sign-in, register and landing pages show, and files served at `/site/`. An invalid `site.yaml` refuses the boot, naming the problem. See [a site for the deployment](#a-site-for-the-deployment). |
| `SUBSTRATE_OPERATOR_DEFAULTS`  | unset (no operator defaults)           | An absolute path to the operator defaults file: the LLM providers, embeddings and provider function keys the operator pays for on behalf of every repository, and the daily allowance bounding them. It may not sit inside `SUBSTRATE_SITE_DIR`. An invalid file refuses the boot, naming the problem. See [operator defaults](#operator-defaults). |
| `SUBSTRATE_INVITE_CODE`        | — (unset: registration asks for no code) | Gates registration. **Set it before anyone else can reach the port.** See below.                            |
| `SUBSTRATE_DATA_ROOT`          | required                               | The directory that holds what Postgres does not: `repositories/<id>/` with each repository's blob bytes and webhook spool, and, unless `SUBSTRATE_ARCHIVE` names another place, the archive under `archive/`. See [the repository directory](#the-repository-directory). It must be an absolute path, it must outlive the container, and a host without one refuses to boot, naming the variable. |
| `SUBSTRATE_ARCHIVE`            | unset: `<SUBSTRATE_DATA_ROOT>/archive` | Where each repository is copied after its writes commit ([the archive](#the-archive)): unset is a directory under the data root; an absolute path is that directory; `s3://<bucket>[/<prefix>]` is a bucket of an S3-compatible service, reached with the `SUBSTRATE_ARCHIVE_S3_*` settings below; `off` writes no archive, so back Postgres up. Any other value refuses the boot, naming the variable. No request is made at boot: a store that cannot be reached is a delay the server reports, never a boot that fails. |
| `SUBSTRATE_ARCHIVE_INTERVAL`   | `10s`                                  | How often each repository's archive pass runs: with the store answering, the longest a confirmed write waits before it is in the archive. A positive duration. |
| `SUBSTRATE_COMPACTION_INTERVAL` | `1h`                                 | How often each repository's [compaction pass](#the-compaction-pass) runs, a duration of a minute or more, or `off`, when history only grows. |
| `SUBSTRATE_DELETIONS_KEPT`     | `720h`                                 | How long a deletion is remembered before the compaction pass forgets the record and moves the repository's horizon; `0` keeps every deletion. |
| `SUBSTRATE_ARCHIVE_S3_ENDPOINT` | unset: Amazon's                       | The URL of an S3-compatible service that is not Amazon's own, such as `https://s3.example.com`. |
| `SUBSTRATE_ARCHIVE_S3_REGION`  | `us-east-1`                            | The region the requests are signed for. |
| `SUBSTRATE_ARCHIVE_S3_PATH_STYLE` | `false`                             | Names the bucket in the path rather than the host name, which most services other than Amazon's need. |
| `SUBSTRATE_ARCHIVE_S3_ACCESS_KEY_ID` | unset: `AWS_ACCESS_KEY_ID`       | The access key id the requests are signed with. Unset together with the secret, both are read from `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` (and `AWS_SESSION_TOKEN`). An S3 archive with neither refuses the boot. |
| `SUBSTRATE_ARCHIVE_S3_SECRET_ACCESS_KEY` | unset: `AWS_SECRET_ACCESS_KEY` | The secret access key. Never logged: the boot line and the metrics name the store by its endpoint, bucket and prefix. |
| `SUBSTRATE_REPOSITORY_CONNECTIONS` | `16`                            | The Postgres connections every repository of the process shares, however many repositories it has opened. One repository takes at most half of them, and never more than eight, so two busy repositories cannot take the whole pool. A process holds at most this cap plus 12 more: 4 for the admin pool, 5 for the maintenance pool, 2 for repository migrations running at once (each dials a connection of its own) and 1 for a commit-time catch-up after a foreign writer. The default therefore holds a process to 16 + 12 = 28. Keep the cap plus 12, for every process on the database, under the cluster's `max_connections`. At least 4; a lower value refuses the boot, naming the variable. |
| `SUBSTRATE_ARCHIVE_BYTES_PER_SECOND` | `8388608`                       | The most bytes per second the process's archive passes copy, over every repository together ([the archive](#the-archive)). A pass encodes and checksums every line it copies, one core busy for as long as the bytes last, so the default, 8 MiB, keeps it under half a core on a four-core arm64 box, where the first pass over a 27 GB history takes about an hour; the passes also pause while any function or agent runs, whatever the cap, for at most 30 s at a time. `0` removes the cap; a negative value, or one above `0` and below `65536`, refuses the boot, naming the variable. |
| `SUBSTRATE_CONVERSION_CEILING` | `10000`                                | The most live records one declaration change (a vocabulary apply, a provider upgrade, the boot upgrade) may rewrite in its transaction ([vocabulary evolution](vocabulary.md#backfilling-and-remapping)). A plan above it is refused and the previews list the refusal; `0` removes the ceiling. |
| `SUBSTRATE_ORPHAN_GRACE`       | — (unset: nothing is collected)        | Turns the GC sweep's **orphan collection** on, and sets the window a marked record waits out first (`168h`, `720h`). A mapping target with no live source, nothing above the machine tier holding a property, and nothing live pointing at it is tombstoned once its mark is older than this. Unset or `0` collects nothing, which is the default: the mark is derived either way and `filter.orphaned` lists it. See [collecting orphaned mapping targets](#collecting-orphaned-mapping-targets). |
| `SUBSTRATE_TRIGGER_INTERVAL`   | `5s`                                   | How often the trigger dispatcher checks every repository for a delivery due, so the longest a record write waits for the trigger it fires. Each tick lists the repositories and runs one pass per idle repository, at most eight passes at once; a host with many repositories may want a slower tick, a test suite that waits on deliveries a faster one. Zero or negative refuses the boot, naming the variable. |
| `SUBSTRATE_TRIGGER_LANE_WORKERS` | `4`                                  | How many due schedule fires one repository's dispatcher pass runs at once, each of a different trigger, so syncs due at the same minute start together instead of one after another. Every delivery the dispatcher runs also holds one of 16 process-wide slots, so raising this does not raise what the process runs at once ([what happens at boot](#what-happens-at-boot)). `1` fires one schedule at a time; below `1` or above `16` refuses the boot, naming the variable. |
| `SUBSTRATE_REQUESTS_PER_PERSON` | `100`                                | How many requests a second one person may send once their token is read, sustained; past it, and past the burst below, a request answers `429 rate_limited` with a `Retry-After`. A person is a repository's sign-in. The default sits far above what a person in the console or an app syncing a device sends, and bounds a script left running in a loop. A watch counts once, when it opens, and a call to the [MCP door](mcp.md) once, whatever its tool asks of the API. A person's requests to manage their tokens (`/tokens`) with a token of their own are paced by a second allowance of the same size, which no app's token reaches, so an app that spent its person's requests cannot keep its person from revoking it. The sign-in door, and every route that takes the factors instead of a token, has its own pacing. `0` paces nothing; a negative value refuses the boot, naming the variable. |
| `SUBSTRATE_REQUEST_BURST_PER_PERSON` | `500`                           | How many requests one person may send at once before `SUBSTRATE_REQUESTS_PER_PERSON` paces them: five seconds of the default rate, so a page that loads many things at once is not paced. At least `1` beside a positive rate; a lower value refuses the boot, naming the variable. |
| `SUBSTRATE_CHAT_TURNS_PER_PERSON` | `4`                                | How many chat turns with an agent one person runs at once. Each holds a model call and keeps running after its client leaves; one more is refused `409 conflict` until one finishes or is stopped. `0` is the default; a negative value refuses the boot, naming the variable. |
| `SUBSTRATE_HEALTH_FAILING_AFTER` | `1h`                              | How long every delivery of a trigger must have parked, since its newest `ok` run, before the dispatcher opens its `trigger.failing/<trigger id>` alert and `trigger status` and `sync status` read `failing`; a parked delivery of the trigger must still stand. The next `ok` delivery, dispatched or retried by hand, resolves the alert whatever the window, and so do the end of its last standing park and the next pass after it is disabled or deleted. Zero or negative refuses the boot, naming the variable. See [driving triggers](functions.md#driving-triggers). |
| `SUBSTRATE_CREDENTIAL_KEY`     | required                               | Wraps each repository's data-encryption key (DEK), which encrypts the sealed store: every secret-typed property's material and stored provider tokens (AES-256-GCM). It wraps each person's own key the same way, which encrypts their password hash and TOTP seed. It is key material, not a passphrase: base64 of exactly 32 bytes, the AES-256 key itself. Generate one with `openssl rand -base64 32`; a host whose key is empty or any other shape refuses to boot, naming the variable (ADR [0024](decisions/0024-the-credential-key-is-key-material-not-a-passphrase.md)). A host whose key does not open the wrapped DEKs the store already holds refuses to boot too, naming each repository, the id of the key its wrap was written under and the id of the key this host holds (`repositories.dek_key_id`: 16 hex digits of a one-way hash over the key, never the key): that is a wrong key or a store from somewhere else. No command re-wraps a repository's DEK under another host key: the key is the one thing that keeps a repository's secrets across hosts, and an archive or an export brought to a host without it restores only without them ([restore without the credential key](#restore-without-the-credential-key)). |
| `SUBSTRATE_IMPORT_WITHOUT_SECRETS` | — (empty: a copy this host's key does not open is set aside) | The repositories, each by its name or its id and separated by commas, that the boot restores without their secrets when this host's `SUBSTRATE_CREDENTIAL_KEY` does not open their copy's DEK: the operator's acceptance of the loss, after the boot set the copy aside naming the key it was written under ([a repository set aside at start-up](#a-repository-set-aside-at-start-up)). It reaches an archive the database has no row of, an archive the operator takes (`archive resolve --take-archive`) and a 0.x directory. A listed repository restores under a new DEK wrapped under this host's key, and every record and every line of history arrives, its sealed values that do not open kept until its next open takes each one out (a directory from before repository ids takes its id at the same boot, and the listing reaches that step too); its next open deletes the webhook requests it had spooled, clears every secret its records named, whatever state their package is in, and raises an alert for each, so its owner reconnects each account and enters each API key again, and each person whose sign-in does not open under this host's key signs in only after `substratectl user reset`. A listed repository this host's key does open restores with its secrets, as if it were not listed. It reaches only a copy being restored: a repository the database holds whose DEK this host's key does not open still refuses the boot. The operator's commands that open as the writer (`archive resolve`, `archive import`, `repository rebuild` and the others) read it too, split the same way, since their start restores and takes as the server's does. Boots with a warning naming the list. See [restore without the credential key](#restore-without-the-credential-key). |
| `SUBSTRATE_INSECURE_DISABLE_TOTP` | `false`                             | **Local development only.** Stops verifying the second factor, so a password is the whole credential: see [the local TOTP-off switch](auth.md#the-second-factor-can-be-switched-off-locally). Boots with a warning, and `GET /.well-known/substrate/server.json` says so. |
| `SUBSTRATE_DNS_RESOLVER` | the system's resolver | The DNS server (`host:port`) every `_substrate` TXT lookup asks: Verify domain ([decision 0208](decisions/0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md)), an app's publisher (0209) and a published package's document (0212). A TXT answer is only as trustworthy as the resolver that fetched it and few zones are signed, so point this at a validating resolver for DNSSEC. Each lookup is bounded at 5 seconds. A value that is not `host:port` refuses the boot. |
| `SUBSTRATE_INSECURE_AUTHORITY_DOCS` | unset | **For testing an unpublished authority document.** A comma-separated list of `authority=path`: each named authority's document is read from the file in place of its `_substrate` TXT record when the server decides whether an app is verified (see [name your publisher](apps.md#name-your-publisher)). Nothing limits it to development: whoever writes the file decides which apps read as that authority's. The approval page says a file on this server verified the publisher, not DNS, and the token records `appAuthorityFromFile`. Boots with a warning naming each authority, and refuses to boot when a file cannot be read. |
| `SUBSTRATE_INSECURE_ALLOW_FOREIGN_PACKAGES` | `false`                | **Local development only.** Lets a token create a package under any authority, not only its repository's own ([decision 0210](decisions/0210-a-token-declares-new-packages-only-under-its-repositorys-authority.md)). The provider end-to-end suite and `mise run test:e2e` set it, because they apply kinds under fixed authorities and shipped provider files rewired at a local mock. Boots with a warning. Never set it where the substrate is reachable: any token may then declare packages under names its repository does not own. |
| `SUBSTRATE_METRICS`            | `false`                                | Serves the Prometheus exposition at `GET /metrics`, unauthenticated and DB-free like `/healthz`: `substrate_http_request_duration_seconds` (by chi route pattern, method and status class), `substrate_http_requests_in_flight`, the pools' `go_sql_*` stats (`db_name` is `admin`, `maint` or `repository:<authority>`; a repository's series count only the connections it has checked out of the shared pool and its waits at its own cap of half the pool, never the shared pool's state), the shared repository pool's own `substrate_db_pool_*` series under `pool="repositories"` (cap, open, acquired and idle connections, acquisitions, waits and wait time: acquired at the cap with waits climbing is an exhausted pool), `substrate_trigger_pass_seconds`, `substrate_trigger_deliveries_total{trigger}`, the webhook spool's `substrate_webhook_spool_entries`, `substrate_webhook_spool_bytes` and `substrate_webhook_spool_oldest_age_seconds` gauges and its `substrate_webhook_spool_drain_failures_total` and `substrate_webhook_spool_refusals_total` counters (all by `repository`: a growing age or a rising failure count is a drain that cannot record, and refusals are the door answering `503`), the archive's `substrate_archive_pending_entries`, `substrate_archive_delay_seconds`, `substrate_archive_last_pass_timestamp_seconds`, `substrate_archive_failed_passes`, `substrate_archive_missing_blobs`, `substrate_archive_pending_blobs` and `substrate_archive_stopped` gauges (by `repository`; see [the archive](#the-archive)), `substrate_repositories_set_aside` (the repositories the start [set aside](#a-repository-set-aside-at-start-up); above zero is one waiting on its operator), and the Go runtime. The usage series say what each repository does: `substrate_changes_total{repository,kind,op,actor}` (every committed changelog entry; `actor` is the type of the actor record that wrote it, `person`, `app`, `function`, `agent`, `package`, `system` or `other`), `substrate_function_invocations_total{repository,function,mode,outcome}` and `substrate_function_invocation_seconds{function}`, `substrate_agent_runs_total{repository,agent,mode,status}`, `substrate_llm_tokens_total{repository,agent,provider,model,type}`, `substrate_llm_cost_usd_total{repository,agent,provider,model,payer}` (`payer` is `operator` or `owner`), `substrate_auth_events_total{repository,event}` (`registered`, `login`, `login_failed`, `repository_deleted`), `substrate_mcp_tool_calls_total{repository,tool,outcome}`, and three gauges read from each open repository at most every five minutes, in the background and only while something scrapes: `substrate_repository_records{repository,kind}` (live records), `substrate_repository_package_kinds{repository,package,origin}` (kinds per declared package; `origin` is `builtin`, `published`, `sample`, named by the shipped sample it came from, or `own`) and `substrate_repository_first_change_timestamp_seconds{repository}`. `SUBSTRATE_METRICS_ANONYMIZE` decides how they name things. The instruments record either way; this only opens the door. **Keep the path off the ingress**: scrape the pod on its own network and have the proxy in front of it refuse `/metrics`. |
| `SUBSTRATE_METRICS_ANONYMIZE`  | `true`                                 | Hashes the names the usage series carry: the repository becomes `repo:<hash>`, a kind, package, function or agent under the repository's own authority `own:<hash>` of what follows the authority (so one sample imported by two people reads as one name), any other authority's `ext:<hash>`, and a provider row or a model the owner named `own:<hash>`. What the substrate ships (`substrate.reamde.dev` and its subdomains, the seeded provider rows and the models their shipped `llm/model` records name) stays in clear, those names being the features, and so does a model an operator entry serves. The hash is keyed by `SUBSTRATE_CREDENTIAL_KEY`, so it is stable across restarts and unreadable to a dashboard viewer without the key. `false` labels everything with its name, for a box whose operator should read what each person does. Either way a repository puts at most 1000 distinct names of its own on the usage series, and every name past that reads `overflow`; deleting a repository drops its usage series but its account events. The operational series above keep the repository's name either way, since acting on them needs it. |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                                      | Signs OAuth flow state. With a callback URL set and this unset, the server mints a random key per boot and warns: flows in progress break on restart. |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                                      | The one redirect URI every provider app registers. Unset, the OAuth facility is off and no flow can start. |
| `SUBSTRATE_CONSOLE_URL`        | —                                      | The console origin the OAuth return-page posts to and falls back to redirecting into. Empty is local dev. |
| `SUBSTRATE_TRUSTED_PROXIES`   | —                                      | The reverse proxies in front of the server, as comma-separated CIDRs or addresses (`10.42.0.0/16`). A request that arrives from one of them is paced by the client its `X-Forwarded-For` names, read from the right past every listed hop, so login, webhook token lookups and app sign-in pace each client rather than the proxy. Empty reads no forwarded header. List only proxies that write `X-Forwarded-For` themselves and drop or append to the one a client sends; see [Behind a reverse proxy](#tls-and-the-reverse-proxy). |
| `SUBSTRATE_PUBLIC_URL`         | —                                      | This server's origin as apps reach it (`https://` and a host, no path): the issuer of [app authorization](apps.md). Empty derives it from each request's `Host`, `https` unless the host is loopback. Set it when the server answers under more than one name. |
| `SUBSTRATE_OUTSIDE_CODE` | `allow` | `allow` or `refuse`: admission and execution of code from document-published packages. A refusal also stops code installed before the setting changed. |
| `SUBSTRATE_INSECURE_UNCONFINED_OUTSIDE_CODE` | `false` | Permits an outside function body ([decision 0220](decisions/0220-outside-code-is-confined-and-reviewed.md)) where the sandbox cannot confine it: macOS, sandbox mode `off`, or a kernel missing a layer such as Landlock. An outside agent, trigger, mapping or OAuth block needs no sandbox and is admitted without it. Boots with a warning. It does not override `SUBSTRATE_OUTSIDE_CODE=refuse`. |
| `SUBSTRATE_SANDBOX`            | `best-effort`                          | How hard to confine function bodies: `off`, `best-effort`, or `enforce` (refuse to run a body unconfined). |
| `SUBSTRATE_SANDBOX_EGRESS_ALLOW` | —                                   | A comma-separated list of CIDRs (or bare addresses) a network body may reach despite the private-range block. A binary-shipped body that declares `permissions.network` reaches the public internet but not the deployment's own loopback, link-local or RFC1918 ranges, so a local provider (a loopback Ollama) needs its address listed here. The Beeper provider is one: Beeper Desktop serves its API on a private address, so every Beeper sync is refused until that address is listed ([its setup](../kinds/providers.substrate.reamde.dev/beeper/README.md#operator-setup)). Empty blocks every private range. The body sees `EACCES`; the failed call's error names the gate and the value to set (`the sandbox's connect gate refused 10.0.0.7:5432; allow it with SUBSTRATE_SANDBOX_EGRESS_ALLOW=10.0.0.7`), and the server logs `egress blocked by the sandbox's connect gate` at `WARN` at most once per destination per body process. An entry that is neither a CIDR nor an address is ignored, and the boot warns naming it. |
| `SUBSTRATE_EGRESS_ALLOW`       | —                                      | A comma-separated list of CIDRs (or bare addresses) the SERVER may dial for a repository-chosen URL despite the private-range block. An `llm/provider` row's `baseURL` is written by the repository owner, so the engine confines its completion and embedding dials to public destinations, refusing the deployment's own loopback, link-local, RFC1918 and CGNAT ranges at connect time (issue #241). A local provider (a loopback Ollama) needs its address listed here. Empty blocks every private range. This is the server's own dials; `SUBSTRATE_SANDBOX_EGRESS_ALLOW` is the separate escape for a function body's dials. A refused dial fails with `egress blocked by the server's dial gate: 10.0.0.7:4000; allow it with SUBSTRATE_EGRESS_ALLOW=10.0.0.7`. These dials honor `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY`: with a proxy set, the server dials the proxy, so the gate checks the proxy's address and never the destination's. A proxy on a private address has to be listed here, and from then on the proxy alone decides what a repository-chosen URL reaches. An entry that is neither a CIDR nor an address is ignored, and the boot warns naming it. |

`SUBSTRATE_CREDENTIAL_KEY` is the one that must be backed up apart from the
archive: without it, sealed material is unreadable
([backups](#backups)).

## The repository directory

The `changelog` table is a repository's history, and Postgres alone confirms
a write
([decision 0262](decisions/0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)).
The data root holds what Postgres does not: one directory per repository,
named by its id
([decision 0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)),
with its blob bytes and its webhook spool, and, by default, the
[archive](#the-archive):

```
$SUBSTRATE_DATA_ROOT/
  instance                          # this server's instance id, minted at its first boot
  repositories/
    rnd-0vrkvsgbs6bnz2sjy4196kccck/ # one per repository, named by its id
      blobs/
        blob-sha256-<hex>           # the bytes, content addressed
      spool/
        <ordinal>-hook-<id>.webhook # a webhook request answered 202 and not yet in the changelog, sealed under the DEK
  archive/                          # the archive, unless SUBSTRATE_ARCHIVE names another place
    repositories/
      rnd-0vrkvsgbs6bnz2sjy4196kccck/
        manifests/<epoch>.json      # the manifest and the mark: format 3, the highest epoch read
        changelog/1/                # the history in pieces, under its changelog dialect
        blobs/
        sealed/
        spool/
```

`instance` names this server to a domain's `_substrate` record
([decision 0208](decisions/0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md)):
`GET /.well-known/substrate/server.json` serves it, unauthenticated, as
`server.instance`, and without `SUBSTRATE_PUBLIC_URL` Verify domain reads it
back from the origin a record's `server=` names. It is public by design and
stable for the data root's life, so anyone can tell that two hostnames reach
one server; a lost data root mints a new one, and stamps already written
stay.

The id is minted at registration (`rnd-` and 26 characters) and never
changes; the name is the repository's authority. `substratectl repository
list` prints both, so a person finds a repository's directory by its name
there. The repository's data-encryption key (DEK) is in its `repositories`
row, wrapped under `SUBSTRATE_CREDENTIAL_KEY`, and the archive's manifest
carries the same bytes, so a copy restored onto a host with the same key
opens without anything else. The credential key itself is in neither;
`dekKeyId` names it (16 hex digits of a one-way hash over the key), so a host holding
another key is told which key a repository wants. Every sealed value is
ciphertext under that DEK and nothing else: the server refuses a plain
payload and never tries the host key on one
([decision 0059](decisions/0059-a-marked-repository-refuses-plain-and-host-key-sealed-payloads.md)).

A release before the archive kept each repository's history here too: a
manifest, the changelog's segment files and a file per sealed value beside
the blobs (a 0.x directory). The boot takes such a directory's history into
the database and removes those files, keeping the blobs and the spool
([what happens at boot](#what-happens-at-boot)).

The root is as private as its mode. The server creates directories `0700` and
files `0600`; give the root to the substrate's user alone, because anything
that can read it reads every repository's blobs, and the history of an
archive kept under it, in the clear
([0031](decisions/0031-blob-bytes-outside-postgres-are-stored-plaintext.md)).
Only the sealed values and the spool are ciphertext. Encrypt the volume for
encryption at rest; the substrate does not.

## The archive

Each repository's **archive** is a copy of it the server writes after its
writes commit: its history in pieces, every blob its records hold, its sealed
values (ciphertext under its DEK), its webhook spool, and a manifest whose
`head`, the **mark**, says how far the copy reaches
([decision 0262](decisions/0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md),
[decision 0277](decisions/0277-the-archive-is-pieces-blobs-and-sealed-values-and-its-manifest-is-the-mark.md)).
It is the backup, what a restore reads and what the owner's export hands out
([backups](#backups)). [The stored format](stored-format.md#the-archive) is its
layout, object by object.

**Where it goes.** `SUBSTRATE_ARCHIVE` names the store
([configuration](#configuration)):

- **Unset**: a directory, `<SUBSTRATE_DATA_ROOT>/archive`. A blob is
  hard-linked from `repositories/<id>/blobs/` where the two are on one file
  system, so its bytes take no second copy of the disk. That copy shares
  the data root's disk, and a linked blob is the same file as the live one,
  so it survives the loss of the database but not of that disk: copy it
  elsewhere ([backups](#backups)), or point the archive elsewhere.
- **An absolute path**: that directory. A path on another disk keeps the
  copy when the data root's disk is lost.
- **`s3://<bucket>[/<prefix>]`**: a bucket of an S3-compatible service,
  reached with the `SUBSTRATE_ARCHIVE_S3_*` settings. The prefix lets one
  bucket hold several servers' archives.
- **`off`**: no archive. The backup is then Postgres's own, a dump or a
  continuous copy of its WAL, taken with the data root's `repositories/`,
  where the blob bytes are and nowhere else.

**No call to the store waits forever.** A list, a delete, or the wait for a
get's response is given 2 minutes; a put's or a get's body fails once a minute
passes with no byte moving, and a transfer that keeps moving is never cut. The
boot lists the archive once, to find what it holds that the database does
not: a store that cannot be reached, or that takes the connection and never
answers, delays the boot by at most that bound, and the boot goes on without
it, restoring nothing from it until a later boot. A restore that has begun is
the exception: its repository is marked, and the boot refuses until the
archive answers rather than serve a repository whose history is half in.

**A pass runs every ten seconds.** While the server has a repository open,
it runs a pass over it every `SUBSTRATE_ARCHIVE_INTERVAL` (`10s`). A pass
holds the store to what the database recorded of its last publication, then
reads the repository from one repeatable-read snapshot and writes the blobs
and spooled requests the store lacks, the sealed values that changed and the
pieces from the mark to the snapshot's head, then records the manifest's
bytes as about to be published, puts it (the pass's one commit point),
records the publication, and last deletes what the publication dropped
([how a pass writes it](stored-format.md#how-a-pass-writes-it),
[the archive's contract](#the-archives-contract)). A pass that finds nothing
new writes nothing. With the store answering, a confirmed
write waits at most an interval before it is in the archive. A server stopped
cleanly runs one last pass over each repository it has open, bounded at 30 s,
so its archive is at the head it stopped at.

**The passes yield to real work.** A pass encodes and checksums every line it
copies, so the process's passes together copy at most
`SUBSTRATE_ARCHIVE_BYTES_PER_SECOND` (8 MiB by default), and pause while any
function body or agent loop runs in the process, for at most 30 s at a time.
The first pass over a long history copies all of it: about an hour for 27 GB
at the default, after an upgrade from a release before the archive, a new
archive, or a move to a new store. A process stopped in the middle of it
copies the history again from the start ([the first copy and a
restart](#the-archives-contract)).

**What a lost store costs.** A write confirmed and not yet archived is in
Postgres alone, and is lost if Postgres's storage is lost before the next
pass finishes. A crash or a restart of Postgres loses nothing. Zero loss is a
synchronous replica of Postgres, which the substrate does not set up.

**A store that fails delays the archive and nothing else.** A pass that
fails leaves the archive at its last manifest; the server goes on serving,
the next interval tries again, and the delay grows. The server logs
`substrate: an archive pass failed; the server goes on serving, and the
archive falls behind until a pass finishes` with the error when passes start
failing, and `substrate: the archive is written again` with `failedPasses`
when one finishes. How far behind is too far is not decided, and nothing
stops the server for it
([#1314](https://github.com/geoah/substrate/issues/1314)); alert on the delay
yourself.

**A row that does not verify stops the pass at its piece.** A pass holds
every line it copies to the checksum the `changelog` table stores beside it,
so a row whose columns are not what that checksum was stamped over fails it
with `two histories of the repository disagree: seq N: the table's checksum
is not the checksum of the row it holds`. The archive stays at its last
manifest and every pass fails at the same seq, as when the store fails, and
the pass notes the row, so a compaction stops below it rather than stamp it
again and one such row holds back the compaction of everything written after
it ([while a line is known not to verify](#the-compaction-pass), [`repository
compact`](#operator-recovery)). The repair is the row: write its columns in
the `changelog` table back from a whole copy of its line, which is the
archive's piece that holds the seq where a pass copied it before the damage
(the piece's name carries its first and last seq; the line is one JSON line
with its `sum`), or restore the database from a copy taken before the
damage, then check the table with `substratectl --dsn … repository verify
--recanonicalize`. The next pass goes on from the mark.

With `SUBSTRATE_METRICS` on, each open repository reports seven
gauges, labeled `repository` with its name:

| Series | What it says |
| --- | --- |
| `substrate_archive_pending_entries` | Changelog entries committed and not yet in the archive. |
| `substrate_archive_delay_seconds` | The age of the oldest entry not yet in the archive; 0 when the archive holds every entry. |
| `substrate_archive_last_pass_timestamp_seconds` | When the last pass that finished ended; 0 before the first. |
| `substrate_archive_failed_passes` | Passes in a row that did not finish; 0 after one that did. |
| `substrate_archive_missing_blobs` | Blobs the archived records hold whose bytes the archive lacks, because this server had lost them; every pass tries them again. |
| `substrate_archive_pending_blobs` | Blobs the archived records hold that the archive has not copied yet: a pass copies 1 GiB of them and publishes, and the next copies more. |
| `substrate_archive_stopped` | 1 while the archive is not as the database left it ([the contract's rule 4](#the-archives-contract)): the passes write nothing until an operator resolves it. |

`substratectl repository list` shows each repository's mark, the seq its
archive holds every line up to and how long ago it was written, and
`repository inspect` how far that is behind the changelog's head and how
many blobs it lacks ([operator recovery](#operator-recovery)). An archive is
a complete copy at its head only with `0 behind the head` and `0 blob(s)
missing`: a blob whose bytes the server lost is named in the manifest
(`missingBlobs`) rather than held up the archive. Both are what the last pass
reported, read from the database; `inspect` reads nothing of the store and
says so (`as last published`). `repository verify` reads the archive back:
every line of the newest manifest's pieces against the table's checksums,
every sealed value it selects, and every blob it holds, hashed against its
name, and, with the archive at the table's head, that every blob the records
hold is in it or named missing. What it could not prove it prints as `NOT
VERIFIED` with the reason, and each such reason is a finding but one: no
pass has published a copy to the store yet. A manifest gone from a store a
pass published to, a store that cannot be read, and history in an earlier
changelog dialect than the table's (until the next pass writes it again)
fail the check. Before counting on a copy, check both.

### The archive's contract

What the archive holds against, and what it does not, written down
([decision 0279](decisions/0279-the-archive-holds-against-a-written-contract.md)).
A review of the archive holds the code to this; a case this section excludes
by name is not a defect, and a rule that does not hold is.

**What the archive is.** A copy of what one database holds, made after the
commit. Postgres decides what happened and answers every read. The archive
may be behind, and how far is measured and reported. While the database
holds a repository's rows, nothing automatic takes the archive's word over
the database's.

**What the store must do.** Four operations: put, get, list, delete.

- A put writes a whole object or nothing. A get returns a whole object,
  "not there", or an error, and the three can be told apart. A list returns
  every name under a prefix or an error, never a part of them; the S3 store
  follows every page to the end, and an error in any page fails the whole
  listing.
- After a put or a delete has succeeded, a later get or list sees it.
- A put or a delete that failed or timed out has an unknown outcome: it may
  be applied later, at any time, whole.
- Nothing else is assumed: no conditional write, no order between requests
  in flight.

A local directory and S3-class object stores do this. The directory store
writes under another name and renames, inside its put.

**Words.** An **archive** is one life of a repository's copy in a store,
under its own id (`repositories/<repository id>/<archive id>/…`, its
manifests under `repositories/<repository id>/manifests/<archive id>/`).
An archive id sorts by when it was made. A new archive starts when a
repository is created, when it is imported (from an archive, an export or a
directory), and when an operator resolves a disagreement; an archive is never
continued by anything but the database life that started it. A **manifest**
is the one commit point of a pass: it carries an epoch and lists every
history piece, sealed value and spooled request it selects; blobs are
selected by the records, by digest. The **record** is what the database
keeps per repository about its archive (the `archive_records` and
`archive_objects` tables): the archive id; the manifest it last published,
its name and bytes; the exact bytes of the manifest it is about to publish,
if any; and one row for every object it has put or is deleting, written
before the request is sent.

**The rules.**

1. **A name never holds two contents, and a pass never selects an object it
   cannot vouch for.** Every object's name carries the id of the pass that
   put it (and a checksum of its bytes where its content does not name it
   already); a manifest's name carries its epoch and the hash of its bytes.
   A manifest selects only objects the pass put itself, or that the manifest
   it started from selected. An object no manifest selects is reused only by
   the process that put it; after a restart it is only ever deleted. Once a
   manifest has dropped an object, no later manifest selects that name.
2. **One snapshot.** A manifest describes the repository at one history
   position: every line up to it and none after, and the sealed values and
   the blobs as one database snapshot at that position saw them. Everything
   it selects was put, and the put succeeded, before the manifest is sent.
3. **One publication at a time, finished exactly.** Before a manifest is
   sent, its exact bytes are recorded as about to be published. A pass that
   finds one recorded puts those same bytes again and records it published;
   it never builds a different manifest in its place. Publication, and what
   may now be deleted, are recorded in one database transaction.
4. **The database goes on only in an archive that is as it left it.** Every
   pass, an idle one included, starts by listing the repository's manifests
   and marks (one list request per open repository per interval, so a
   manifest that lands late is met at the next tick whether or not anything
   changed):
   - the recorded archive has published nothing, and no manifest of it or
     of a newer archive is listed: go on (a recorded first manifest is put
     again; otherwise a first pass with names of its own);
   - the recorded archive is the newest listed, no deletion mark covers it,
     and its newest manifest is the recorded one, or the one about to be
     published: go on;
   - something published, and nothing of the repository listed at all: the
     store was emptied or replaced. A new archive starts, logged at error;
   - anything else (a newer or a different manifest than recorded, two
     manifests of the highest epoch, a newer archive, a deletion mark over
     the recorded archive, the published manifest gone): **stop**. The pass
     writes nothing and deletes nothing. The server keeps serving and says
     so: the log, `substrate_archive_stopped`, `repository inspect`. The
     operator decides (below). Nothing is ever merged.

   **Taking the archive never removes anything of the database until the
   whole snapshot it will install has been read, checked and is on local
   disk**: every piece against its checksum and every line against its own,
   every sealed value against its version, every spooled request, and every
   blob of the archive against its digest,
   copied to `<data root>/archive-take/<id>/<stage>/` with `staged.json`
   written last. **A staged copy has no authority of its own.** Only a take
   recorded in the database starts or continues a replacement, and it
   installs only the copy staged for exactly that choice: the repository,
   the archive and the manifest, by name and by the hash the manifest's
   name carries. **The staged copy is read as its restore will read it,
   twice**, to its end and writing nothing: every history piece against
   its hash and every line against its own, the sealed values, the spooled
   requests and the blobs, by the code the restore reads them with, and with
   them the restore's own checks of the manifest (its name, its dialects,
   its data key under this host's `SUBSTRATE_CREDENTIAL_KEY` or the
   operator's listing in `SUBSTRATE_IMPORT_WITHOUT_SECRETS` on a host that
   has a key, its sealed values under that key:
   [restore without the credential key](#restore-without-the-credential-key)).
   Once before the choice is recorded, and again at the boot before
   anything of the repository goes, with the key and the listing as they
   are then. A refusal at the boot leaves the repository whole and served
   and its choice recorded, and logs why; the next boot tries again. A take
   reads its copy twice for this, so it costs one more reading of the
   history than a restore. After the removal, what can still fail is the
   database, the staged copy changed between the two readings, and the
   conversion and fold of what was read; the choice stays recorded and the
   store keeps the archive it names, so after the repair (for a changed
   copy, removing `archive-take/<id>/<stage>/`, which the next boot stages
   again from the store) a boot restores the repository. `archive resolve --take-archive` stages,
   in a directory of its own, before it records the choice; a read or a
   check that fails records nothing and changes nothing, an earlier choice's
   copy included, says why, and the choice is still the operator's. A second choice is
   staged beside the first and replaces it only once it is recorded. The
   record is the control plane's (`archive_takes`), so it outlives the
   repository's rows: the boot that carries the choice out stages the
   recorded manifest again only if its copy is not there whole, then drops
   the repository's rows and files and restores from that copy alone,
   never reading the store again, and the restore's last transaction ends
   the take. A crash between the staging and the replacement leaves the
   repository whole and the copy staged, and once the choice is recorded
   the next boot replaces it; a copy staged with no choice recorded is
   removed by an ordinary boot, never installed. A crash after the rows
   went leaves no row, the record and the copy, which the next boot
   restores from. A deletion of the repository ends its take in the
   transaction that begins the deletion. The boot reads `archive-take/`
   only to find the copy a record names, and removes every other.
5. **A pass deletes only what the record accounts for**, and only after the
   publication that drops it is recorded: what the manifest it started from
   selected and the new one does not, the manifest before itself, and its
   own database's objects from earlier processes that no manifest selected.
   It never deletes an object the record does not hold. A row says a delete
   is about to be sent before it is sent.
6. **Lines are compared in one place only**: the first boot over a directory
   a release before the archive wrote. Every line both hold is compared,
   before the history is converted, so both are in one dialect; then the
   directory is set aside, the history converted, and a new archive
   published. No per-line record of a conversion is kept.
7. **A deleted repository stays deleted.** A deletion records on the
   repository's row that it began (from then on nothing serves it, no pass
   runs on it, and a boot finishes it), writes a mark under an id of its own
   past every archive the store shows and the one recorded, then removes
   every other object of the repository. A mark covers every archive older
   than it: no boot restores one, whatever lands later, and no pass writes
   in one. Bringing it back is an explicit import, which starts an archive
   past the mark.
8. **Readers.** A restore with no rows takes the newest archive's newest
   manifest, when no mark covers it, checks every object it selects as it
   reads it, installs that one snapshot and its record together, and starts a
   new archive. Two manifests of the highest epoch are reported and not
   chosen between. A manifest a newer one replaced is not a recovery point.
   `repository verify` and `repository inspect` say what they proved and what
   they did not: the recorded manifest is there; every object it selects is
   there with its bytes; every line's checksum; objects the record does not
   hold, as a count, which a listing that fails makes unknown and a finding,
   never zero. An archive in an older dialect than the table, a store
   that could not be read, and an object that went while it was read (read
   again once) are reported as not verified, never as fine.

**What it does not hold against.**

- **Two databases publishing into one archive.** Not supported and not
  safe: each stops when it sees the other's manifest, and by then either may
  have deleted what the other still selects.
- **A store that shows an old or a partial listing, or loses an object after
  answering.** Not supported. `repository verify` finds a loss when it is
  run and can read the store; it cannot prove the store hides no other
  manifest.
- **Postgres's storage lost.** What was confirmed after the last published
  manifest is missing from the repository rebuilt from the copy.
- **The database restored from a backup while the server runs.** Stop the
  server first.
- **A directory store on a network mount that hangs in the kernel**, where
  no timeout reaches.
- **Bytes after deletion.** A put applied late can leave an object no
  manifest selects. Nothing reads it, and `verify` counts it; the archive
  does not promise that deleted bytes are gone from the store.

**Why a database restored from a backup is safe without being declared.** A
restored database has forgotten what it did after the backup. If that
forgotten future published a manifest that is in the store, rule 4 sees a
manifest the record does not hold and stops. If its manifest was still in
flight, the restored database goes on, and when the manifest lands it is
either below the current epoch (ignored, reported, and left) or at or above
it (rule 4 stops at the next pass). A delete from the forgotten future
targets only an object a publication it recorded had dropped, which rule 4
saw, or an object no manifest selected, which rule 1 never selects after a
restart. A put from it lands under a name nothing else has. So nothing the
current manifest selects is removed by a request the database has
forgotten. After restoring Postgres, still run `archive resolve
--keep-database`, which starts a clean archive at once.

**When the passes stop.** `substratectl --dsn … archive report <repository>`
lists every archive and mark the store holds of the repository and says
what the database and the archive's newest manifest each hold that the
other lacks: the lines (compared only when both are in one changelog
dialect), the sealed values by ref, the blobs by digest. Then, with the
server stopped:

- `archive resolve <repository> --keep-database` starts a new archive the
  next pass writes from the database whole. The old archive stays in the
  store; `archive remove <repository> <archive id>` removes it, and refuses
  the archive the database writes and a deletion mark.
- `archive resolve <repository> --take-archive` stages the newest archive's
  newest manifest whole on local disk, checked (above), records the choice,
  and has the next boot replace the repository by that copy: every row and
  file the server holds of it goes, the archive's snapshot comes in, and a
  new archive starts. What the database held that the archive lacks is
  lost. A snapshot that does not read or check whole is refused when the
  command runs, and nothing changes. Run again, it records the later
  choice in place of the earlier; `--keep-database` after it ends the take.
- `archive import <repository id> <archive id>` restores a repository the
  database does not hold from one archive, past a deletion mark.

**The first copy of a long history, and a restart.** An object no manifest
selected is only ever deleted after a restart (rule 1), so a process stopped
in the middle of a pass loses what that pass put. A pass copies blobs 1 GiB
at a time and publishes between, so a restart loses at most a step of them.
The history cannot be cut that way, because a manifest is one snapshot: a
process stopped in the middle of the first copy of a long history (after an
upgrade, or a new archive) copies it again from the start. Let the first pass
finish before a planned restart: `repository inspect` shows the archive
behind the head while it runs, and `0 behind the head` when it is done.

## The compaction pass

Each repository runs a compaction pass every `SUBSTRATE_COMPACTION_INTERVAL`
([decision 0300](decisions/0300-a-paced-pass-compacts-what-is-past-each-kinds-retention-and-forgets-old-deletions.md)).
It replaces a record's entries past its kind's retention with one
[state entry](stored-format.md#state-entries), the record's whole state as of
the newest entry it replaces: the latest state of every record stays, and so
does every entry inside the retention. A kind that keeps everything is left
alone. An entry's age is its own time. The pass keeps, per kind, the seq it
has compacted up to, and each pass works the stretch between that and the new
cutoff, reading only the records with a line in it (and the effects other
records' lines carry on them, from an index the write path keeps), never the
whole history.

It works in batches of 25 records, each one transaction that holds the
changelog lock a write waits for, sleeps as long as each batch took, and
stops after two minutes, leaving the rest to the next pass. A pass stopped
anywhere (a stop, a crash) leaves the history whole: the next one does its
stretch again, and a record already compacted is not written again.

It also **forgets a deletion** older than `SUBSTRATE_DELETIONS_KEPT`: every
entry of a purged record goes. A client that resumed from before it would
never hear of the deletion, so the repository's **horizon** moves to the line
the deletion was on, and a `from=` below it is answered `410 compacted`; the
change page names the horizon. No deletion past an enabled record trigger's
cursor is forgotten, so a trigger that stands still holds the horizon back,
and `repository history` names it; one that is not enabled holds nothing,
and the pass starts it over from the beginning, once, before it forgets past
it. A purged record with a line that must stay, such as one a parked failure
names, keeps every line until it goes. So does any record such a line, a
drain's or the webhook spool holds part of, even one whose every line is
held: each pass retries one batch of a kind's held records, in turn, so a
hold that lasts takes no more of a pass than that, and `repository history`
counts the ones that wait, how many passes a round of their retries takes,
and whether the last pass finished or ran out of its budget.

**While a line of the history is known not to verify, nothing is compacted
or erased at or above it, and the repair comes first.** A line is known once
a reader that writes anyway meets it: `repository compact`, the pass's own
reads, the pass's first derivation of its index, an erasure, and the archive
pass where it refuses the row (`repository verify` reads and writes nothing,
and notes nothing). The note outlives the failure that found it. So one such
line holds back the compaction of everything written after it, for every
record, and a permanent delete made meanwhile deletes the record and holds
its history's erasure, intent kept, with the reply naming the seq.
`repository history` names every noted seq, and each
pass held by one logs `substrate: a line of the history does not verify`
once, with the seqs. Each pass reads each noted row once, and the first pass
after the row is [written back](#the-archive) from a whole copy drops the
note and goes on by itself, the held erasures included.

What stays open is damage no reader has met: a batch of the pass finds a
record's lines by their `kind` and `record_id` columns and by the index of
the effects a line carries for other records, so a line whose damage
changed those is not among the lines of the record it was written for.
Until some reader meets such a line, a batch can place a state above it
without the effect it was written with, and an erasure can leave a value in
it. `repository verify --recanonicalize` reads every line and names it.

**A permanent delete erases the record's history** (a forget, the history
before a live record's current state), in a transaction that follows the
delete's own ([api](api.md#the-records-route),
[decision 0304](decisions/0304-a-permanent-delete-erases-the-records-history-and-the-intent-is-in-the-history.md)).
What remains to erase is written into the history, so a crash, a rebuild or
a restore between the two leaves it there, and each pass finishes what
waits: a crash's leftover, a trigger that has delivered the deletion, a
line written back. `repository history` names each waiting erasure and
since when. The table holds no value of the record once it is done, and the
archive once its next pass has published; a store whose versioning or
retention lock keeps replaced objects keeps them, by its own policy. A
restore starts an archive life of its own, and the life it read keeps what
it held, as an export or a database backup taken before the delete does.

**What a deleted record owns goes in batches of 100** (decision 0305). A
batch is one transaction under the changelog lock a write waits for: it
purges up to 100 records the deleted one owns directly, each as a record
deleted directly is purged, and erases their histories. The delete runs one
batch in all, so a record that owns fewer than 100, none of which owns
anything in turn, is done in the request; the compaction pass runs the
rest, and every deeper level, sleeping as long as each
batch took and stopping at its two-minute budget, and while a closure waits
the next pass comes a minute after the last one ends instead of at
`SUBSTRATE_COMPACTION_INTERVAL`. The size is not a setting: a smaller batch
leaves a large closure waiting more passes, and a larger one holds the lock
longer. With the interval `off` no pass runs, and a closure moves only
through the forget door below. A record a finalizer holds is tombstoned by
the batch that meets it, and the closure waits for its release; the
collector's sweep leaves a tombstone a standing closure has still to
erase. `repository history` names each closure in
progress with the record that owned it, how many went and how many it still
owns directly, a floor, since what those own is counted only as the batches
reach them.

A record created again at the deleted id waits for its batches: an account
connected again after its permanent delete, before the pass has taken its
mirrors, is the case to expect. Its create answers `503 unavailable` with
the reason `erasing` and a one-minute `Retry-After`, logged at INFO as
`request refused: the record at its id is still being erased`, and a sync's
delivery that writes it parks as a temporary failure, `temporary, retried
automatically at …`, with no alert, and gets through on a retry once the
batches are done. Nothing needs a person, but the owner can move it along:
`POST <record path>/forget` on the deleted record runs one batch a call, and
`{"finish": true}` also passes the triggers the erasure waits for. A
delivery whose retries run out before the closure is done, about an hour,
is set aside with the parked alert, as any other is, and so at once is one
whose function may run an agent or call another, which is never run twice
unattended: its parked error names the erasure and when to retry.

The first archive pass that starts after a compaction writes again only the
pieces holding a line it changed; one already running publishes the cut it
read first. `substratectl repository history <repository>` measures
a repository's history by kind, entries and bytes, from the table, beside a
live server; `repository compact` runs a whole compaction by hand.

## There is no LLM configuration

Without an [operator defaults](#operator-defaults) file the server takes no
LLM endpoint, no key and no embedding model. Completions
and embeddings alike are bought through a repository's own
[`llm/provider`](agents.md#providers) records, which carry the wire, the
endpoint, the key and (for embeddings) the model. The process holds no bearer,
so no host-wide key can reach a repository-chosen endpoint. The provider
clients read nothing from the host either: `ANTHROPIC_API_KEY`,
`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` and
Anthropic profile files are ignored, so setting one on the server changes no
request.

Three consequences an operator meets:

- A fresh repository has three keyless provider rows (`openai`, `anthropic`,
  `gemini`) and the shipped assistant, Moss, which names `anthropic`. It
  refuses at dispatch until that row's `apiKey` is written.
  [Registering a provider](agents.md#registering-a-provider) is the write.
- Hybrid search returns its lexical arm alone until one row declares
  `embedModel`, and the vectors it buys name that row and that model, so
  re-pointing either stops the older vectors being scored. `substratectl
  --dsn … repository reembed <repository>` queues their replacement and the
  drain loop buys the vectors a batch at a time, so an interrupted re-embed
  resumes by itself. A value the endpoint refuses (HTTP 400, 413 or 422) moves
  to the back of the queue with a warning in the log rather than stopping it,
  unless the endpoint refuses a known-good input too, which stops the pass
  with an error and moves nothing
  ([0173](decisions/0173-the-embed-drain-packs-values-into-capped-requests-and-sets-a-refused-one-aside.md)).
  A refused value is tried again, and warned about again, each time the queue
  reaches it; editing its text, or dropping `embed` from its property, ends
  that. There is no REST verb for it: it is an operator command,
  run on the box ([operator recovery](#operator-recovery)). A gateway swapped behind an unchanged row and model name
  is invisible to the provenance columns, so that case takes `reembed --all`.
- A repository restored from its archive queues every embeddable property
  by itself, because the vectors were never in the archive
  ([backups](#backups)).

## Operator defaults

An operator who pays for the people it invites names one file in
`SUBSTRATE_OPERATOR_DEFAULTS` ([decision 0235](decisions/0235-an-empty-key-on-an-operator-listed-provider-row-runs-on-the-operators-transport-under-a-daily-allowance.md),
[the plan](plans/host-defaults.md)). An operator default is a key the file
supplies, used only where a repository holds none of its own. Without the
file the server behaves as the section above says. With it:

- A live `llm/provider` row with an empty `apiKey` whose id the file lists
  runs on the operator's entry whole: its wire, base URL, key, headers,
  request defaults, cache, title model, models and prices. Nothing on the row
  but its id reaches the request. A row with its own key is the person's, and
  a deleted row stays deleted until the person creates it again without a
  key.
- Where no row declares `embedModel`, the file's one `embeddings` entry buys
  the repository's vectors, stamped with the provider id `(operator)`, which
  no row id can be.
- A function the file lists under `functionSecrets` (Firecrawl's `websearch`
  and `webscrape`), of a package installed as the published provider, gets
  the file's key as `config.settings.apiKey` while the repository's own
  `apiKey` secret for that package is empty, and none of the repository's
  settings beside it, so a `baseUrl` the person wrote never reaches a call the
  operator pays for. The key never becomes a record. A copy of the package
  under the repository's own authority, such as an earlier copy of the
  Firecrawl sample, never gets it
  ([decision 0234](decisions/0234-firecrawl-is-a-provider-whose-tools-are-offered-to-agents.md)).
- Every operator-paid call first reserves its worst-case cost against the
  allowance, `budget.centsPerDay`, per repository and per UTC calendar day,
  and settles to its reported cost after; a function call reserves its
  `pricePerCall` and settles at it once its body may have started, or at
  nothing when the runner never started it. A call that reports no
  usage, or only part of it, counts at that worst case, unless it reported
  nothing and failed before the vendor could bill it: it never left, or the
  vendor answered a redirect or a 4xx. The reservation covers one attempt, so an
  operator-paid Anthropic call is not retried after a 5xx, a dropped answer
  or a timeout; a 429 is still retried. At the limit a trigger is held, and a
  chat, a call or a search's semantic arm is refused with `403 guard`, reason
  `quota`, naming the site: "today's allowance from Example is used up; it
  resets at 00:00 UTC; add your own key to keep going". The site's name comes
  from `site.yaml` ([a site for the deployment](#a-site-for-the-deployment)),
  and is "this server" without one.
- A key the vendor refuses, the operator's or a person's own, reads
  `failing` on the operator defaults read with the vendor and the status
  ("OpenAI refused the key (401)") until a call the vendor accepts or a new
  key is written. A refusal is a `401`, or a `403` whose error names the key
  (`authentication_error`, `invalid_api_key`); a `403` about one request (a
  region, a model, a missing permission, moderation) fails that request
  alone. A refusal of an operator key reads in every repository the key
  serves; a person's own key's reads in their repository alone. An agent
  call on a refused key answers `503 unavailable` with the reason
  `key_refused`, and a chat turn ends with an `error` event, both with the
  plain refusal, and the vendor's own words go to the log. The embed drain
  stops calling a refused key until a backoff ends: a minute after the
  refusal, doubling with each refusal after it, up to an hour, and one drain
  asks per step however many repositories the key serves: the first that
  has a request to send. The server holds this in memory, so a restart
  forgets it: the slots read ready until the next call meets the refusal
  again. It holds at most 4096 provider rows across repositories, and past
  that drops the row touched longest ago.
- A published provider listed under `oauthClients` signs in through the
  operator's OAuth client wherever its client input resolves to no record of
  the person's
  ([the operator's OAuth client](bundles.md#the-operators-oauth-client)).
  Each account records which client its grant came from; after the client
  changes, the account asks for a reconnect instead of refreshing. Removing a
  provider from `oauthClients` is such a change for the accounts connected
  through your client: they ask for a reconnect at their next refresh.
- Each package under `install` is installed through the catalog at
  registration, and at a repository's first open under a file that lists a
  package the repository has never held. A package whose bundle record its
  changelog ever held, including one the owner since uninstalled, is not
  installed again, and that survives a rebuild and an export restored
  elsewhere, because the changelog travels with it. A failed install is
  logged, tried again at the next open (in practice, the next restart), and
  shown as `install-failed` with the error on the package's `oauth` item and
  its `function` item, whichever the file lists.
- `GET /api/v1/operator/defaults` answers each slot, its source, whether the
  operator offers it, and today's allowance, and discovery lists the
  `operatorDefaults` feature ([operator defaults](api.md#operator-defaults)).

```yaml
install:                           # published providers every repository gets once
  - providers.substrate.reamde.dev/google
oauthClients:
  providers.substrate.reamde.dev/google:
    clientId: 1234.apps.googleusercontent.com
    clientSecret: {file: /run/secrets/google-client-secret}
    notice: >-                     # optional; shown beside the provider's sign-in
      Example's Google sign-in is in testing. Ask Example to add your Google
      address before you connect, and expect to reconnect once a week.
llm:
  openai:                          # the llm/provider row id it serves
    wire: openai
    baseURL: https://api.openai.com/v1
    apiKey: {file: /run/secrets/openai}
    models:                        # the only models it serves, each priced in USD per 1M tokens
      - {model: gpt-5, inputPer1M: "1.25", outputPer1M: "10", contextWindow: 400000}
      - {model: gpt-5-mini, inputPer1M: "0.25", outputPer1M: "2"}
    maxOutputTokens: 16000         # sent where a request names none, caps one that names more
    titleModel: gpt-5-mini         # optional; one of models
    defaults: {reasoningEffort: low}   # optional; request params under the agent's own
    cache: {key: true}             # optional; ttl, key and breakpoints as on a row
    headers: []                    # optional; [{name, value}]; Idempotency-Key and X-Idempotency-Key are refused
    embeddings: {model: text-embedding-3-small, per1M: "0.02"}   # at most one entry
  anthropic:
    wire: anthropic
    apiKey: {env: OPERATOR_ANTHROPIC_KEY}
    models:
      - {model: claude-sonnet-5-5, inputPer1M: "3", outputPer1M: "15"}
    maxOutputTokens: 8000
functionSecrets:
  providers.substrate.reamde.dev/firecrawl:
    apiKey: {file: /run/secrets/firecrawl}
    consumers: [websearch, webscrape]          # the functions that receive the key
    pricePerCall: {websearch: "0.1", webscrape: "0.1"}   # cents, one per consumer
budget:
  centsPerDay: 200                 # the allowance: per repository, everything operator-paid
```

Key material is never written in the file: every key is `{file: /path}`, an
absolute path read once at boot, or `{env: NAME}`. Each package `install`,
`oauthClients` and `functionSecrets` name must be one this binary ships as a
provider.

An OAuth client the operator registers lists the server's one callback,
`SUBSTRATE_OAUTH_CALLBACK_URL` (`https://<your-substrate-host>/api/v1/oauth/callback`),
as its redirect URI. A Google client in Testing mode lets only the test users listed
on its consent screen connect, at most 100, so the operator adds each
person's Google address by hand, and the `notice` says so. Google expires a
Testing client's refresh tokens after 7 days: the refresh then fails with
`invalid_grant`, and the account asks for a reconnect. Moving to a production
client is a client id change, so every account connected through the old
client reconnects once. Google's verification of a production client
(sensitive scopes, and a security assessment for `gmail.readonly`) is the
operator's own work.

The boot refuses a file that is not absolute or sits inside
`SUBSTRATE_SITE_DIR`, a key file that is missing or empty, an unset or empty
variable, a key written inline, a base URL that is not `https`, a model with
no `inputPer1M` or `outputPer1M`, an entry without `maxOutputTokens`, more than
one `embeddings` entry, a missing `budget.centsPerDay`, and any key the format
does not know. Rotating a key is a file change and a restart; removing an
entry withdraws it from every repository at the next start.

The ledger, `operator_spend_reservations`, is the operator's: no changelog
entry writes it, so a rebuild, an import and a restore leave the day's
allowance as it was, and no export carries it. A reservation still open an
hour after it opened (a crash, a lost settle) is settled at its ceiling. The
reserved ceiling prices the prompt at a token per three bytes plus a
quarter, at the higher of the input and cache-write rates, and
`maxOutputTokens` at the output rate, so the ledger overcounts rather than
under; a vendor whose bill exceeds that estimate is the overshoot to watch
on the vendor's own dashboard. The person's own spend cap
([0149](decisions/0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md))
is separate and unchanged.

Every request carrying a key, the operator's or a row's, follows no
redirect: a `3xx` answer fails the call.

## The blob store

A blob is two halves: a **manifest**, which is an ordinary record keyed by the
content digest, and the **bytes**. The manifest is always in Postgres and is
always the truth. The bytes are always at
`$SUBSTRATE_DATA_ROOT/repositories/<id>/blobs/<digest>`, inside the
repository directory beside the spool, and the
[archive](#the-archive) holds a copy of every blob a record holds.

There is nothing to configure: one store, no variable
([0075](decisions/0075-v1-ships-the-fs-blob-backend-alone.md)). A host that
still sets `SUBSTRATE_BLOB_STORE`, or any `SUBSTRATE_BLOB_S3_*` variable,
boots and ignores it, because the server reads no such variable.

**Isolation is not the database's job here.** The repository is half of every
key, and it comes from the authenticated token's repository, never from the
request: a read resolves the manifest first, under row level security, and only
then fetches bytes. So a caller cannot reach another repository's blob by
guessing a digest. But anything that can read the data root can read every
repository's blobs: the store is as trusted as the database. The directory and
the files in it are mode `0700` and `0600`, so on a shared box the mode is what
keeps another local account out.

**Blob bytes are never sealed.** The sealed store covers secret-typed
properties; a blob object is stored exactly as it arrived, and no credential
key is involved in reading it
([0031](decisions/0031-blob-bytes-outside-postgres-are-stored-plaintext.md)).
Whoever holds the directory holds every attachment in the clear. For encryption
at rest, put it under the store: disk encryption for the data root.

**An upload becomes two steps, and a crash between them is cheap.** Outside
Postgres the bytes cannot commit with the manifest, so the manifest is written
`pending` first, then the bytes, then `stored`
([0030](decisions/0030-a-blob-outside-postgres-settles-after-its-bytes.md)). A
manifest only ever says `stored` once the store confirms the bytes, so no read
ever meets a blob whose bytes are missing. What a crash leaves is a `pending`
manifest, which the sweep collects with anything else nobody references.
Deleting works the same way in reverse: the manifest is tombstoned, then the
object is deleted, and an object left behind by a failure is reaped by a later
sweep that lists the store.

**A read hashes the bytes it serves.** `GET /api/v1/blobs/{digest}` and the
export check every blob's SHA-256 against its digest, so a blob damaged on
disk (a flipped bit, a truncated or replaced file) is refused, never served:
the read answers `500` naming the digest and logs `request failed` with the
same message ([the API](api.md#blobs)). `repository verify` finds every
damaged blob without waiting for a read. The digest is the file name, so the
repair is copying that one file back from a backup.

**The bytes follow the data root.** A server pointed at a data root the bytes
are not in serves a 404 for every blob, and a 404 reads like a deletion. Move
`<data root>/repositories/` whole, with the server stopped.

The 64 MiB cap on one upload and the absence of range reads are the contract,
not the store.

## The function sandbox

Function bodies are third-party code, and the substrate confines them with
Landlock, seccomp and rlimits: see [the sandbox](functions.md#the-sandbox) for
what each layer closes. Three things an operator needs to know:

**Check the boot log.** The substrate reports the sandbox once at startup,
naming the kernel's actual Landlock ABI. If a layer is missing the line is an
ERROR, not a warning, because a confinement that silently does less than it
claims is worse than none. A real deployment should run `SUBSTRATE_SANDBOX=enforce`,
which turns that into a refusal to run bodies at all.

**The DEGRADED line names each missing layer.** The message and its `missing`
attribute list every layer the kernel did not give, with the reason the probe
saw and the fix, for example
`filesystem (Landlock): not supported by this kernel: build it with CONFIG_SECURITY_LANDLOCK=y and add landlock to its lsm= list`.
[What each layer needs](#what-each-layer-needs-from-the-kernel) says how to
check a host before booting the substrate on it.

**The boot log also says what bodies trust.** A second line beside the sandbox
report names the certificate store a body verifies a TLS peer against: the
interpreter's own where it has one, else the system bundle the runner names in
`SSL_CERT_FILE` because the interpreter's compiled-in store does not exist —
the python.org framework build on macOS is that case, and without the line
every function declaring `network:` failed `CERTIFICATE_VERIFY_FAILED` with
nothing at boot to say why. A host with no certificate bundle at all gets a
WARN naming the paths that were looked for; install the platform's CA
certificates package.

**Landlock and seccomp work in a stock container**: Docker's and containerd's
default seccomp profiles permit the `landlock_*` and `seccomp` syscalls, and
neither needs a capability.

**The connect gate needs `CAP_SYS_PTRACE`.** It is the third layer, and the only
one whose work happens in the substrate rather than in the body: the filter
routes a body's `connect(2)` to a supervisor here, which reads the destination
with `process_vm_readv(2)` and duplicates the body's socket with
`pidfd_getfd(2)`. The default seccomp profile permits those two only for a
container carrying `CAP_SYS_PTRACE` in its bounding set, so without it the gate
cannot answer a single notification. `compose.yaml` ships
`cap_add: [SYS_PTRACE]` for exactly this (`securityContext.capabilities.add` is
the Kubernetes spelling). It is a bounded trade: the capability unblocks the
profile's ptrace group for the whole container, and what bounds it is that the
container holds only the substrate and the function children it spawns, all at
one unprivileged uid and each started under `no_new_privs` inside its own
Landlock domain.

**Without the capability, network bodies are refused, never unfiltered.**
Filtering the destination is the contract a network grant is issued under
([0035](decisions/0035-a-network-body-connect-is-filtered-by-destination.md)),
so a gate that cannot run refuses every function that declares `network:` in
`best-effort` as well as in `enforce` — reaching the deployment's own Postgres
is not a degradation of that contract, it is its opposite. The boot line is an
ERROR naming the syscall that refused, and the refusal a caller sees names it
too, so a failing provider install reads as the missing capability instead of
as uv's "Permission denied". A function that declares no network is unaffected.

What does **not** work in a stock container is anything built
on user namespaces or cgroup delegation: `CLONE_NEWUSER` is denied by the
default profile and `/sys/fs/cgroup` is mounted read-only, which is why the
sandbox has no memory or process-count ceiling. Do not add `--privileged` to
try to get one.

### What each layer needs from the kernel

The first column is the name the DEGRADED line uses. Run the checks on the
host: a container shares the host's kernel.

| Layer | What it confines | What the kernel needs | How to check on the host |
| ----- | ---------------- | --------------------- | ------------------------ |
| `filesystem (Landlock)` | The paths a body may read, write and execute. It closes `/proc`, so a body cannot read the substrate's environment. | `CONFIG_SECURITY_LANDLOCK=y`, and `landlock` in the active LSM list (`CONFIG_LSM`, or the `lsm=` boot parameter, which replaces it). ABI 3 (Linux 6.2) or newer: below it `truncate(2)` is not mediated, and the layer counts as missing. | `cat /sys/kernel/security/lsm` lists `landlock`; `uname -r` is 6.2 or newer. The boot line names the ABI it got. |
| `syscall filter (seccomp)` | The syscall classes a body has no use for (`ptrace`, the mount APIs, `bpf`, `io_uring`, module loading), and `AF_INET`/`AF_INET6` sockets for a body that declares no network. | `CONFIG_SECCOMP_FILTER=y`, and a substrate build with a syscall table for the host's architecture: `linux/amd64` or `linux/arm64`. | `grep Seccomp /proc/self/status` prints a `Seccomp_filters:` line, which the kernel prints since Linux 5.9 and only with `CONFIG_SECCOMP_FILTER`. On an older kernel, use the configuration check below the table. |
| `connect gate` | Where a body that declares network may connect: the public internet, not loopback, link-local or the RFC1918 ranges. | `SECCOMP_RET_USER_NOTIF` with `SECCOMP_IOCTL_NOTIF_ADDFD`, `pidfd_open(2)`, `pidfd_getfd(2)` and `process_vm_readv(2)`: about Linux 5.9. In a container, `CAP_SYS_PTRACE` in its bounding set (above). | `uname -r` is 5.9 or newer. The boot line reports the probe, which calls `pidfd_open`, `pidfd_getfd` and `process_vm_readv` against a child. |

The kernel's own configuration settles all three where it is readable:
`zgrep -E 'LANDLOCK|CONFIG_LSM=|SECCOMP' /proc/config.gz`, or the same pattern
over `/boot/config-$(uname -r)` on a distribution that installs it.

### Raspberry Pi OS

Run a 64-bit Raspberry Pi OS. The image ships `linux/amd64` and `linux/arm64`
only, and the server does not compile for `GOARCH=arm` today. A 32-bit arm
build would also have no syscall filter: the sandbox carries no seccomp
syscall table for 32-bit arm.

A kernel built from the Raspberry Pi defconfigs has no Landlock, so the
filesystem layer is missing and the boot line says
`filesystem (Landlock): not supported by this kernel`. The syscall filter and
the connect gate's kernel side are there. Checked in
[raspberrypi/linux](https://github.com/raspberrypi/linux) at the head of its
default branch, `rpi-6.18.y`, on 2026-10-08 (commit `4103a989`):

- None of the three defconfigs below sets `CONFIG_SECURITY_LANDLOCK`, and the
  option has no default
  ([`security/landlock/Kconfig` lines 3 to 22](https://github.com/raspberrypi/linux/blob/4103a989a46196239b830bd07693277e42b8f992/security/landlock/Kconfig#L3-L22)),
  so it is off.
- Each empties the LSM list: `CONFIG_LSM=""` at
  [`arch/arm64/configs/bcm2711_defconfig` line 1747](https://github.com/raspberrypi/linux/blob/4103a989a46196239b830bd07693277e42b8f992/arch/arm64/configs/bcm2711_defconfig#L1747)
  (the 64-bit kernel for the Pi 3, 4, 400, CM4, Zero 2 W and the Pi 5
  family),
  [`arch/arm64/configs/bcm2712_defconfig` line 1749](https://github.com/raspberrypi/linux/blob/4103a989a46196239b830bd07693277e42b8f992/arch/arm64/configs/bcm2712_defconfig#L1749)
  (the optimised kernel for the Pi 5 family) and
  [`arch/arm/configs/bcm2709_defconfig` line 1612](https://github.com/raspberrypi/linux/blob/4103a989a46196239b830bd07693277e42b8f992/arch/arm/configs/bcm2709_defconfig#L1612)
  (32-bit).
- None sets `CONFIG_SECCOMP` or `CONFIG_SECCOMP_FILTER`, and both default to
  on for arm64 and 32-bit arm
  ([`arch/Kconfig` lines 646 to 665](https://github.com/raspberrypi/linux/blob/4103a989a46196239b830bd07693277e42b8f992/arch/Kconfig#L646-L665)).

The `rpi-6.12.y` branch (commit `43c132e8`) reads the same: `CONFIG_LSM=""`
at
[`arch/arm64/configs/bcm2711_defconfig` line 1733](https://github.com/raspberrypi/linux/blob/43c132e8863c3bff3647033b6a7d2bf87b15501c/arch/arm64/configs/bcm2711_defconfig#L1733)
and no `CONFIG_SECURITY_LANDLOCK`. On a running Pi, `sudo modprobe configs`
and then the `zgrep` above over `/proc/config.gz` shows what the installed
kernel was built with.

To get the filesystem layer, build the Raspberry Pi kernel with
`CONFIG_SECURITY_LANDLOCK=y` and turn Landlock on at boot: put `landlock` in
`CONFIG_LSM`, or add an `lsm=` parameter to `/boot/firmware/cmdline.txt`. An
`lsm=` value replaces the whole list, so name every LSM the host should run,
for example `lsm=landlock,apparmor` to keep AppArmor. A kernel built with
Landlock but booted without it in the list makes the boot line say
`built in but disabled` instead.

## The invite code

`SUBSTRATE_INVITE_CODE` gates the one way a user gets created. Set,
registration admits only a request that presents it. Unset, registration asks
for no code and anyone who reaches the port may register: the laptop default,
which `compose.yaml` ships and the boot log warns about. There is no closed
state: once the box has its user, keep strangers out with a code nobody is
given. Registration is rate-limited (paced, with no failure lockout) whether
or not the code is set ([users and tokens](auth.md)).

There is no admin user and no operator password. Everything privileged happens
on the box, through the DSN.

## Who may share a server

A server is for one household, or for people who trust each other and you,
its operator. Running one for people who do not know its operator is not
supported in 1.x ([stability](stability.md#who-may-share-a-server)). Each
person registers a repository of their own and no request reads across two,
but three things assume that trust:

- **The sandbox default.** `SUBSTRATE_SANDBOX` defaults to `best-effort`,
  which runs a function body with whatever layers the host's kernel gives
  and without the rest ([the function sandbox](#the-function-sandbox)), and
  a function one person writes runs on the machine everyone shares.
- **Names.** Registration takes a repository's name on the registrant's
  word, so anyone admitted can register a domain they do not hold until
  [Verify domain](auth.md#verify-domain) says otherwise.
- **Start-up.** One repository's damaged changelog file is set aside at the
  start and the others serve, but bringing it back is a restart, which
  everyone's repositories wait through
  ([a repository set aside at start-up](#a-repository-set-aside-at-start-up)).

You also hold the database, the data root and the credential key, so you can
read every repository on the server. Give the invite code only to people
who accept that.

## A site for the deployment

A deployment can carry its own name and a landing page for signed-out
visitors. Point `SUBSTRATE_SITE_DIR` at a directory holding a `site.yaml`:

```yaml
name: Example               # required: the brand the console shows
tagline: A place for our photos and plans   # optional: one line under it
logo: logo.svg              # optional: a file in the directory
icon: icon.svg              # optional: the browser tab's icon, a file in the directory
landing: landing.html       # optional: a file in the directory
```

`name` is the brand on the sign-in and register pages and on the landing.
Under it the sign-in and register pages say "A Substrate provider": a site
names who hosts the Substrates on this server, and the doors keep calling
what a visitor signs in to "your Substrate". `tagline` is the line under the
name on a landing whose fragment cannot load, and the second half of the
landing's page title. `logo` is an image shown beside the name. `icon` is
the browser tab's icon (`.svg`, `.png`, `.ico`, `.webp`, `.gif`, `.jpg` or
`.jpeg`); without one the logo is the tab's icon. The server writes that icon
and the site's `name` into the console's HTML, so the tab shows them before
any script runs, and answers `/favicon.ico` with the same file.
`landing` is an HTML fragment the console renders at `/` for a visitor who is
not signed in, in place of sending them straight to sign in. The console
frames it with a bar carrying the brand and a sign-in link and nothing else,
so the fragment carries its own link to `/register`. Without
`SUBSTRATE_SITE_DIR` none of this exists: `/site/` is the console's like
any other path and discovery carries no `site`.

The server reads `site.yaml` once, at boot, and refuses to boot when it is
wrong: no `name`, a key it does not know, more than one YAML document, an
`icon` of another extension, or a `logo`, `icon` or `landing` that is not a
regular file inside the directory (an
absolute path, a `..`, a segment starting with a dot, or a symlink that leads
out of the directory). A change to `site.yaml` needs a restart; the files it
names are read on every request, so an edited `landing.html` shows on the
next load.

Every file in the directory is served at `GET /site/<path>` (and `HEAD`),
`site.yaml` included, so keep nothing there that is not public. Only regular
files are served: a directory, a dotfile or dot directory, a path with `..`
or an empty segment, and a symlink out of the directory are all a `404`, and
so is any other method. A file's `Content-Type` comes from its extension
alone (`application/octet-stream` without one), and each answer carries
`Cache-Control: no-cache` with an `ETag` and a `Last-Modified`, so an edit is
seen at once and an unchanged file answers `304`. A site file is served under
its own `Content-Security-Policy` with `sandbox`: opened at its own URL, a
page in the directory runs no script and cannot read the console's storage.

Discovery announces the site, so a client finds it before anyone signs in.
`GET /.well-known/substrate/server.json` gains a `site` object; `tagline`,
`logo` and `landing` are omitted when `site.yaml` does not set them, and the
two files are named by their URLs under `/site/`:

```json
"site": {
  "name": "Example",
  "tagline": "A place for our photos and plans",
  "logo": "/site/logo.svg",
  "landing": "/site/landing.html"
}
```

A minimal setup, with the image:

```bash
mkdir -p /srv/site
printf 'name: Example\nlanding: landing.html\n' > /srv/site/site.yaml
printf '<h1>Example</h1><p>Ask for an invite at hello@example.com.</p>\n' > /srv/site/landing.html
docker run -v /srv/site:/srv/site:ro -e SUBSTRATE_SITE_DIR=/srv/site ...
```

## TLS and the reverse proxy

The server speaks plain HTTP and never terminates TLS. Login and registration
send the password and the TOTP code in the request body and answer with a
bearer secret and the TOTP enrollment seed; every other API request carries
the bearer token. Serving the port to anyone beyond loopback
without TLS in front of it is unsupported.

The binary listens on `127.0.0.1` (`SUBSTRATE_BIND_ADDRESS`) and refuses to
start on any other address, every interface included, unless
`SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true` says that only a TLS terminator or a
loopback port mapping reaches it:

```
SUBSTRATE_BIND_ADDRESS is "0.0.0.0", so the server would listen on 0.0.0.0:8080, which is not loopback, and it speaks plain HTTP: ...
```

The published image and `compose.yaml` set both, `SUBSTRATE_BIND_ADDRESS=0.0.0.0`
and `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true`, because a container's own
loopback is out of reach of its port mapping, and the mapping is what decides
who reaches the port: `compose.yaml` publishes it on the host's `127.0.0.1`
unless `BIND_ADDRESS` says otherwise, and a Kubernetes Service answers inside
the cluster. A `docker run` of the image publishes what `-p` says:
`-p 127.0.0.1:8080:8080` keeps the port on the host's loopback, and a bare
`-p 8080:8080` publishes it on every host interface in cleartext, which is
the unsupported case the image cannot see. The boot warns every time the setting admits a non-loopback
address, naming the address, because the server cannot see what is in front
of it. A proxy on another host is the same case: bind the interface it
reaches, set the escape, and keep every other peer off the port.

The supported path is [Caddy](https://caddyserver.com) on the same host,
proxying to the substrate on loopback:

```
substrate.example.com {
	respond /metrics 404
	reverse_proxy 127.0.0.1:8080
}
```

Caddy obtains and renews the certificate for `substrate.example.com`,
redirects plain HTTP to HTTPS, refuses `/metrics` from outside, and passes
every other request through. What the substrate needs from the proxy, and what
it trusts:

- **The original `Host` header.** At registration and login a bare repository
  label is completed under the host the request reached (`ada` becomes
  `ada.substrate.example.com`), so a proxy that rewrites `Host` to its
  upstream's address registers and looks up the wrong repository. Caddy passes
  `Host` through unchanged; nginx rewrites it unless the location sets
  `proxy_set_header Host $host;`. Caddy routes only requests for
  `substrate.example.com` to this block, so the completion always uses the
  deployment's own name.
- **Unbuffered streams.** The agent chat and `watch=1` answer with no
  `Content-Length` and write lines as they happen; Caddy flushes a response
  with no `Content-Length` to the client as it arrives, so nothing needs
  setting.
- **The client's address, if you list the proxy.** By default the server
  reads no `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Real-IP` or `Forwarded`
  header, because a header anyone can send cannot name the caller. The
  per-address paces (login, webhook token lookups, `GET /oauth/authorize`)
  then key on the transport peer, which behind a proxy is the proxy for every
  caller: two people signing in to an app within a few seconds of each other
  share one pace, and one client sending made-up webhook tokens pauses every
  app's deliveries. Set `SUBSTRATE_TRUSTED_PROXIES` to the address the
  server sees the proxy connect from (`127.0.0.1` for the binary behind the
  Caddyfile above; a container's port mapping shows the bridge gateway
  instead) and the server reads the client from `X-Forwarded-For`, from the
  right, past every listed hop. Caddy writes the header and ignores one a
  client sent unless told to trust it. The proxy itself must see the client's
  address: a Kubernetes `LoadBalancer` Service with the default
  `externalTrafficPolicy: Cluster` rewrites it to a node's, so set the
  proxy's Service to `Local`.
- **HTTPS in the settings that name the deployment.** `SUBSTRATE_CONSOLE_URL`
  is `https://substrate.example.com` and `SUBSTRATE_OAUTH_CALLBACK_URL` is
  `https://substrate.example.com/api/v1/oauth/callback`. The OAuth return page
  posts its result only to the console URL's origin, and an `http://` origin
  never matches a console served over HTTPS.
- **The browser headers pass through untouched.** Every response carries
  `Strict-Transport-Security: max-age=31536000; includeSubDomains`, a
  `Content-Security-Policy` with `frame-ancestors 'none'`,
  `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff` and
  `Referrer-Policy: no-referrer`; every response of the authentication
  routes, the token routes, the OAuth start and callback and `GET
  /api/v1/export` carries `Cache-Control: no-store`. The server sets them
  itself, so the proxy adds nothing and must replace none. A browser honors
  HSTS only when it arrives over HTTPS, which is why the header is harmless on
  a loopback laptop.

## What happens at boot

- The migration runs, under an advisory lock, and the roles and Postgres
  extensions are ensured.
- Each repository is opened the first time something touches it. Opening
  rebuilds its kind registry **from its own stored declaration records** —
  nothing on the serving path reads the binary's embedded tree.
- **The sealed store has one key.** Every payload is bound-framed ciphertext
  under the repository's DEK, from its first write, so a read refuses a plain
  payload and never tries the host key, naming the framing it found and the
  key it expected
  ([decision 0059](decisions/0059-a-marked-repository-refuses-plain-and-host-key-sealed-payloads.md)).
  `repository inspect` shows the id of the host key the DEK is wrapped under.
  A payload that does not open under the DEK refuses that open, naming the
  ref: nothing can recover its material, so delete the row from `sealed`
  and have the user re-enter the secret it held (a provider token: reconnect
  the account), then open again.
- **A repository registered before identities gets its person**
  ([decision 0274](decisions/0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)).
  Its sign-in was its `core/credential` record; the boot makes an identity
  of it, named after the repository, with the same password and the same
  second factor, and the repository's first open purges the record and names
  the person of every token it holds. A name another person took since,
  which only a restore can arrange, becomes `<repository id>.<name>`, and the
  boot logs it. A record whose material does not open costs that repository
  its person and no other's: the boot logs it, the repository refuses to
  open, and `user reset <repository>` gives it one.
- **A repository a release before ids left keyed by its name takes its id
  first** ([decision 0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)).
  One transaction per repository moves every row of every table from the
  name to a minted id and rewraps the data key under it; then the spooled
  webhook requests are sealed again under the id, `repository.json` is
  rewritten in format 2 and the directory renamed from the name to the id.
  A 0.x directory from before ids with no row, being restored, takes a new
  id the same way before its import. One whose name a repository in the database
  holds is that repository's only when both hold the same data key; one whose
  name another repository took since the copy was made is
  [set aside](#a-repository-set-aside-at-start-up) and left exactly as it
  was. The conversion holds the writer lease under the
  name until the boot check has published the repository under its id, so a
  second server over the same database, with the same directory or a copy of
  it, is refused rather than giving it a second id. The boot logs `substrate: giving a repository
  an id: rewriting its rows` before each and `substrate: gave a repository an
  id` after, with the rows and `took`: on a laptop's Postgres about 600,000
  rows took 6 seconds, and the boot serves nothing until every repository is
  done. A boot stopped between the rows and the directory is finished by the
  next. An older server cannot open a converted repository; the way back is
  the copy taken before the upgrade.
- **The boot check takes in every history the database lacks**, before
  anything else writes, in three passes
  ([decision 0262](decisions/0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)).
  A repository a pass cannot bring to serve is **set aside** and the check
  goes on to the next ([a repository set aside at start-up](#a-repository-set-aside-at-start-up)).
  It logs `substrate: boot check started` with the number of repositories
  and of 0.x directories, and `substrate: repository checked` with the
  `action` for each repository it changed.
  0. **A deletion that began is finished**, and a repository its operator
     chose to replace by its archive (`archive resolve --take-archive`) loses
     its rows and its files once the copy staged for the recorded choice is
     whole, so step 2 restores it from that copy. A deletion that cannot
     finish (an archive store that does not answer) is the next start's to
     try again, the repository served by no one meanwhile; it sets nothing
     aside.
  1. **A 0.x directory**, the manifest, segment files and sealed files a
     release before the archive wrote under the data root, gives the table
     what the table lacks and then gives its history up
     ([the contract's rule 6](#the-archives-contract)). One with no row is
     **imported** whole: the row is created from its `repository.json`,
     stamped with the `changelogDialect` and `vocabularyDialect` the
     manifest recorded, its sealed files loaded into the table and its lines
     inserted with their checksums. One with a row is held to the table
     **line for line**, every seq both hold, in the one dialect both are in:
     a line that differs, a row of the table its own checksum does not
     reproduce, or a seq either side skips, sets the repository aside, its
     directory as it was, and names the seq. Past the shorter one's head the longer one's lines stand: the
     table's, which a release before the archive left ahead when it died
     between its commit and the append to the file, and the directory's,
     which a database restored from a backup older than the data root leaves
     behind it, and which are imported. The directory is then set aside
     (`repository.json` renamed to `repository.json.adopted`) before the
     history is converted, so no boot ever compares a converted table with
     it. The boot opens the directory's history read-only: an unfinished
     transaction at its end is read past and left on disk, since it may
     hold a whole line of a transaction the table holds. Its manifest,
     segments and sealed files are removed after, and only
     once the table stands at the binary's changelog dialect with no import
     marked incomplete: a start that finds the conversion refused, or still
     to run, leaves them as they are and logs why; its blobs and spool stay
     where they are. A directory under `repositories/`
     named by an id, or by an authority (`ada.example.com`) as a release
     before ids named it, with no row and no `repository.json` is logged and
     skipped: nothing says whose it is, so it is neither imported nor
     deleted. A dot-prefixed entry is passed over: `.keying/` holds the
     keying's records of directories still named by their authority. Any
     other entry under `repositories/` (a `tmp`, a `Backup-2026`, anything
     whose name is neither) refuses the boot and names the entry; move it
     out of the data root.
  2. **The archive**, where the server writes one: a repository it holds and
     the database does not is **restored** from the newest archive's newest
     manifest, the row created from it as above, into a new archive
     ([the contract's rule 8](#the-archives-contract)). An archive a deletion
     mark covers is not restored (`archive import` brings it back), and one
     holding two manifests of its highest epoch is logged at error and not
     chosen from. A repository the database holds is never taken from the
     archive at the boot: its passes hold the archive to the record and stop
     at anything they do not recognize, and the operator decides. A store
     that cannot be listed, or a repository's archive that cannot be read,
     is logged at error and skipped, and the next boot reads it again.
  3. **Every row**: an import or a conversion a previous boot began is
     finished, and a history below this binary's changelog dialect is
     converted ([conversions](stored-format.md#conversions)), before anything
     opens it.

  No copy of a history is ever compared with the table across a
  conversion: a 0.x directory is compared before it, and whether an archive
  is this database's is decided by the record, never by its lines.

  Every import is one path: the lines past the table's head go in with
  their checksums, the history is converted to this binary's dialect, and
  the records are rebuilt from the table through `fold.go`. The import
  writes an `import_progress` row before its first batch of lines commits
  and deletes it in the transaction that commits the last fold pass. A boot
  that dies in between leaves the row, and the next boot check resumes the
  import from the table's head: no line is inserted twice and nothing is
  appended. Until then an open of the repository refuses. A manifest whose
  `changelogDialect` or `vocabularyDialect` is above the binary's maximum
  **sets the repository aside** with the same named error the open gives ("the
  changelog speaks a newer dialect than this binary can replay", "the store
  speaks a newer schema dialect than this binary"); so does a manifest
  carrying no wrapped DEK, one whose DEK this host's
  `SUBSTRATE_CREDENTIAL_KEY` does not open unless
  `SUBSTRATE_IMPORT_WITHOUT_SECRETS` lists it
  ([restore without the credential key](#restore-without-the-credential-key)),
  one whose DEK does not open every sealed value unless the same variable
  lists it, and one naming a repository whose name another repository in
  the database holds. Each refusal comes before the row is
  created, and while the copy is set aside no registration takes its name.
  In a restore and in an import that has begun, a line whose `sum` does not
  verify, a piece that is not the run its name says, and an archive whose
  pieces do not reach its head **set the repository aside** too, and so does
  a 0.x directory that disagrees with its table at a seq. Each names the
  repository and the seq or the object, and repairs nothing: the repository
  is not opened at all, rather than half-open beside the others. To bring it
  back, repair what its warning names and restart; to start without it, move
  `repositories/<id>/` out of the archive, or the directory out of the data
  root.
- **Shipped vocabulary is upgraded, per repository, in one transaction**: the
  first open under a new binary appends the version diff to that repository's
  changelog by the `system` [actor](api.md#actors)
  ([the boot-time upgrade](vocabulary.md#how-the-vocabulary-reaches-a-repository)).
  It logs `substrate: upgrading a repository's shipped vocabulary from the
  embedded tree` once its guards have passed, with the packages and the
  kinds whose indexes it reshapes, and `substrate: upgraded a repository's
  shipped vocabulary from the embedded tree` after the transaction, with
  `took`; a refused upgrade logs `REFUSED` instead. Every request on the
  repository waits for the open, so the transaction re-derives no stored
  record's indexes: a shipped kind whose reference sites or `fts` flags
  moved is re-derived behind the open, below. The conversions a shipped
  declaration declares (a rename, a backfill, a remap) still rewrite the
  records they name inside the transaction, under
  `SUBSTRATE_CONVERSION_CEILING`.
- **A reshaped kind's indexes are re-derived in the background** when a
  boot upgrade moves a reference site or an `fts` flag of a shipped kind.
  The upgrade's transaction records the kind and the properties that moved;
  once the open has published the repository, a pass re-derives the refs
  rows and `fts` of the rows that carry one of those properties, in
  transactions of 500 rows, and leaves every other row alone, since a row
  carrying none of them derives the same rows under either declaration. It
  logs `substrate: re-deriving the indexes of the kinds a declaration change
  reshaped` with the kinds, `substrate: progress` with the `walk`
  `re-deriving the indexes of a reshaped kind`, `rows` and `elapsed`,
  `substrate: re-derived the indexes of one kind` after each kind, and
  `substrate: re-derived the indexes of the kinds a declaration change
  reshaped` with `rows` and `took` at the end. A transaction that fails logs
  `substrate: a background pass step failed; trying it again` with the
  `pass` `background index re-derivation` and the `step`, and is tried
  again after a pause of up to a minute. Each page records the id it ended
  at, so a pass a shutdown interrupts resumes there at the next open, and
  `repository rebuild` finishes it for the refs and the search index,
  because the replay derives every row; the observed index it queues for the
  pass again. Until the pass reaches a row, a reverse read through a
  moved reference site and a search over a moved `fts` flag answer for the
  previous declaration of that row.

  A `POST …/vocabulary/apply` and a provider upgrade (the catalog's add
  over an older version) use the same pass for the search index and the
  observed index. Their transaction holds the registry lock every write
  waits on, so it re-derives only the refs rows of the rows carrying a
  moved reference property, logging `substrate: vocabulary apply` with the
  `step` `re-deriving the refs index`, and records the `fts` half and the
  observed points of a kind that starts or stops binding the `observed`
  trait for the pass, logging `substrate: vocabulary apply` with the `step`
  `requesting the search and observed index re-derivation behind the
  commit` and `kinds`. The pass
  starts once the batch has committed and logs the lines above. A reverse
  read is current when the batch answers; until the pass reaches a row, a
  search over a moved `fts` flag and the observed index of a rebound kind
  answer for the declaration the batch replaced. A batch whose commit answer was
  lost leaves the repository refusing writes until restart, and may leave
  this process serving the declarations it replaced, so the pass stops
  there without touching a request, logging `substrate: the index
  re-derivation stopped before it finished; the next open resumes it` with
  the `error`, and the next open, which loads the committed declarations,
  re-derives the rows.
- **The search index is re-derived in the background** when the binary
  indexes text differently from the one that indexed the repository's rows.
  The repository serves reads and writes meanwhile, and a row keeps its old
  index, still searchable, until the reindex reaches it. The reindex works
  kind by kind, in transactions of 2000 rows, and logs
  `substrate: re-derived the search index of one kind` with `kind`, `done`
  and `total` after each kind, then `substrate: re-derived the search index`
  once it finishes. A transaction that fails logs
  `substrate: a background pass step failed; trying it again` with the
  `pass` `search index re-derivation`, and is tried again after a pause of
  up to a minute. A reindex a shutdown
  interrupts starts again at the next open; `repository rebuild` finishes
  it, because the replay indexes every row.
- **The observed index is re-derived in the background** when the binary
  derives it by newer rules than the ones recorded in `observed_index`, or
  when none are recorded (a repository written before migration 0013). The
  open hands every row of every kind that binds the `observed` trait to the
  pass that re-derives a reshaped kind's indexes, and logs `substrate: the
  observed index was derived under older rules; re-deriving it behind the
  open` with `from`, `to` and `kinds`. A rebuild and a boot import clear the
  index and queue the same work instead of deriving it inside the replay,
  so their transaction holds the repository's write lock for the records
  alone; the pass starts once they commit. A metric record whose `rollups`
  change queues the rows of its streams the same way. While any of this is
  owed for a kind, an aggregate read over it answers `503` with
  `Retry-After`.
- Persisted function bodies re-warm in the background. One that no longer
  prepares logs an error naming the function, and its deliveries park rather
  than the repository failing.

Five loops then run in-process: the trigger dispatcher every 5 seconds, garbage
collection every 5 minutes, OAuth refresh and finalizer processing every
minute, the resolution sweep (the recovery path for a resume that a restart or
a lost lease dropped, and for a `running` thread whose run died with its lease
expired) every 2 minutes, and the embed-queue drain every minute,
whether or not any repository holds an embedding provider yet. A drain tick
takes batches of 64 queued values from each repository in turn, starting at the
next repository each tick, until a repository's batch applies nothing or fails
(a 429 included: the next tick resumes) or 50 seconds have passed, and packs a
batch's text into as few embeddings requests as 256 inputs and 250,000 bytes
each allow, so a backlog drains at the endpoint's speed. The GC sweep
also drops `idempotency_keys` rows past their 24 hour retention
([idempotency and retries](api.md#idempotency-and-retries)), and collects
orphaned mapping targets where the deployment asked for it
([below](#collecting-orphaned-mapping-targets)). Each enumerates
repositories and opens each one through the same row-level-security-bound pool
a request uses.

**A purge erases the record's sealed material.** When the GC sweep purges a
tombstone, or `DELETE ?purge=true` purges a record, the record's rows in
`sealed` go in the same transaction, and the archive's next pass deletes
their copies. A tombstone keeps them, so a restore reads its secrets again.
Every open of a repository as its writer, the server's and an operator
command's alike, and each sweep after it, also erase every sealed row that
no record, live or tombstoned, holds the ref of: the rows purges in earlier
releases left behind, and any an imported copy carries. So the archive never
copies one, and `repository verify`, which opens the repository read-only,
names each such row as an orphan until the next open erases it. The erasure
covers the live table and the archive only, so every earlier copy that held
the record's secrets keeps their ciphertext
([a rotated or deleted secret stays in older backups](#a-rotated-or-deleted-secret-stays-in-older-backups)).

The trigger dispatcher runs each repository's pass in a goroutine of its own,
at most 8 at once. The cap bounds the function runner processes and the
transactions the dispatcher has in flight; a pass takes a connection for each
statement or transaction and none while a function body runs. Every tick
queues one pass for each repository that has none running or queued, and a
pass that ends starts the next queued one at once, so while passes are short
every repository gets a pass per tick. The queue takes repositories in order
of how long ago their last pass started, a repository that has never run
first, so busy older repositories cannot keep a new one at the back
([#639](https://github.com/geoah/substrate/issues/639)). Eight long passes do
fill every slot, and then a new repository waits for one of them to end.
What bounds that wait is the per-trigger budget: inside a pass each record
trigger gets 30 seconds before the pass moves on, plus the one delivery in
hand ([how a pass walks triggers](functions.md#triggers)). There is no bound
on the pass as a whole, so a repository with many backlogged or slow record
triggers holds its slot for roughly 30 seconds per trigger. Its schedule
triggers do not wait for that walk: they fire in a lane beside it, up to
`SUBSTRATE_TRIGGER_LANE_WORKERS` (4) triggers at once, one fire per trigger
at a time
([decision 0150](decisions/0150-a-schedule-lane-fires-its-due-triggers-in-parallel-one-fire-per-trigger.md)).
Every delivery of every pass, each record trigger's turn and each schedule
fire, holds one of 16 process-wide delivery slots, so the dispatcher runs at
most sixteen runner processes and sixteen transactions however many passes
and lanes are busy; a delivery that finds every slot held waits for one. A
wake, a retry by hand and a webhook fire take no slot. A wake never walks a
record trigger beside the pass's turn on it: whichever starts second runs
nothing
([decision 0182](decisions/0182-a-record-trigger-is-walked-by-one-caller-at-a-time.md)).
Schedule triggers that
name the same Python function still fire one after another, because the
runner keeps one process per function. A fire that starts more than a
minute after its occurrence logs `schedule fire dispatched late` with the
trigger and the delay. The other four loops still walk repositories one
after another.

Writes to one repository run one at a time: each holds the repository's
changelog lock from its start to its commit. An owner's put, patch or delete of
a data record through the API or the console goes ahead of queued background
writes such as function effects, agent writes and syncs; every other write,
an owner's blob upload or vocabulary apply included, queues with the
background writes. A queued background write becomes eligible to go ahead of
pending owner writes once 8 owner writes have started since it queued or it
has waited 2 seconds, and even then it waits for the background write admitted
before it to take the lock. Priority never shortens the transaction already
holding the lock.

### Collecting orphaned mapping targets

A record minted from a mapping's source outlives that source: delete the last
mirror that projected onto it, or replace a provider (which purges its rows and
mints them under new ids), and the target's mapped properties recompute away
while the row stays. The engine always **marks** those rows — `filter.orphaned`
lists them, `substratectl get <kind> --orphaned` prints them
([the orphan mark](projection.md#when-the-last-source-goes-the-orphan-mark)) —
and by default it collects none of them.

`SUBSTRATE_ORPHAN_GRACE` turns the collection on. With a positive duration, the
GC sweep tombstones every marked record whose mark is older than the window AND
that no live record points at, and the same sweep collects the tombstone like
any other. Three things keep it honest, and they are all worth knowing before
setting it:

- **The window is the guard.** "The last source went" is also what a connector
  outage, a revoked token and a re-seed in flight look like from inside the
  engine. Set the window longer than the longest sync gap you would consider
  normal; a week (`168h`) is a reasonable start on a personal substrate.
- **A referenced record is never collected.** If anything live still points at
  it — a task's assignee, a message's author — it is spared however old the
  mark is, and the mark stays for you to look at.
- **A rebuild restarts every window.** The mark is derived and its stamp is not
  in the changelog, so `repository rebuild` re-derives the fact and stamps it
  now. Nothing is collected sooner because of a rebuild; some things are
  collected later.

A deployment with the knob set logs a warning at boot naming the window, because
this is the one loop that deletes records nobody asked it to delete
([decision 0092](decisions/0092-an-orphaned-mapping-target-is-marked-and-collected-on-request.md)).

Keep it to **one replica**. The watch signal and the trigger dispatcher are
in-process, and two dispatchers would serialize on compare-and-swap rather than
scale. The archive passes lean on the same shape: one writer process per
repository.

## A repository set aside at start-up

A repository the start cannot bring to serve is **set aside**, and the
server starts and serves the others
([0281](decisions/0281-a-repository-the-start-finds-damaged-is-set-aside-in-memory-and-the-rest-serve.md)).
Nothing about it is stored, and from the moment it is set aside the server
writes nothing to it: it never opens it, holds no writer lease on it and
runs no loop over it, and the next start decides again. What the start
does to one repository goes through one gate: the opening of its data key
wrap, its writer lease, its keying, its recorded deletion or take, the read
of its directory, its 0.x import or cleanup, its restore and the check of
its rows. The gate passes over a repository already set aside, so no later
pass reads its directory or changes anything of it (its adopted 0.x history
is not cleaned up, and a deletion or a take recorded for it waits, its mark
kept, for a start that finds it whole), and a failure that is the
repository's own sets it aside with the pass as its stage. What is not one
repository's is done outside it, and its failure stops the start: whether
the wraps that name another key open, the one read of which repositories
hold a recorded deletion or take, and the listing of the data root and the
archive. The archive's store not answering is no repository's damage: a
copy the start had not begun to write (an archive with no row, the copy a
take removed the rows for, staged again) is left for a later start, as an
archive the store does not answer for always was, and one it had begun is
set aside at `archive store` (below). What the start
committed for it before the failure stays, and is resumable (below). What sets one aside, by the `stage` its log line
names:

| Stage | What failed for it | The way back |
| --- | --- | --- |
| `credential key` | its row's data key wrap names this host's key and does not open | restore its row in `repositories` from a database dump taken before the damage: the row is what the start reads, so an archive restored beside it changes nothing |
| `writer lease` | its writer lease could not be taken, for a reason that is neither another process holding it nor a database that stopped answering | a restart; the error names what failed |
| `id` | a repository or a 0.x directory from before ids could not take its id: a data key that does not open, a name another repository took since the copy was made | as the error says: restore it, list it in `SUBSTRATE_IMPORT_WITHOUT_SECRETS`, or move the directory out of the data root. A directory is tied to a row by one of two things only: its data key opening to the row's (a manifest's id is held to the row of that id the same way), or the keying's own record, `repositories/.keying/<name>` under the data root, which the keying writes before it gives the row of that name its id and every start reads before any directory; tied, the row is set aside with it (its blobs and spool are in it), whatever the directory holds. Where neither ties it, the directory claims its name alone, and the repository that holds the name serves, whatever the manifest says in the clear |
| `take` | a take its operator recorded (`archive resolve --take-archive`) failed once it began to remove the repository's rows and files | repair what the error names, then restart: the choice is still recorded, and the next start carries the take on from the copy staged for it. A take whose staged copy the restore would refuse sets nothing aside: the repository stays whole and served, the choice recorded, and the log names what the copy failed |
| `boot check` | its directory, which the start could not read, or its rows: a 0.x directory that disagrees with them at a seq, an import a start began whose archive cannot be read now or holds a line whose `sum` does not verify, a history conversion step that refuses a line | put the archive (or the 0.x directory) back, or restore it from a copy taken before the damage. A row the comparison or the conversion refuses is named by its seq, and the repository's 0.x directory, where it had one, is kept whole until its history converts: its `changelog/` segment files hold that seq's line, one JSON line with its `sum`. Write the row's columns in the `changelog` table back from that line, or restore the database from the copy taken before the upgrade, check the table with `substratectl --dsn … repository verify --recanonicalize`, and restart: the next start converts the history and only then removes the directory's |
| `import` | a 0.x directory with no row: a data key this host's key does not open and the operator did not list, a manifest that does not read, a newer dialect, a sealed file the key does not open, a name another repository holds | list it in `SUBSTRATE_IMPORT_WITHOUT_SECRETS` ([restore without the credential key](#restore-without-the-credential-key)), restore it, or move it out of the data root. Like the keying's, it only claims its name |
| `restore` | an archive with no row, or the copy staged for a take whose rows the take removed: a data key this host's key does not open and the operator did not list, a sealed value the key does not open, a newer dialect, a piece or a line that does not verify, a name another repository holds | list it in `SUBSTRATE_IMPORT_WITHOUT_SECRETS`, restore `repositories/<id>/` in the archive from a copy, or move it out of the archive. It only claims its name, under the name its newest manifest gives |
| `archive store` | nothing of the repository: the archive's store did not answer (refused the connection, timed out, answered a service error) while the start read the repository's copy, after it had written part of it (a restore, or an import a start began) | nothing to repair in the repository: once the store answers, a restart finishes it from the same copy |
| `people` | its people, after the boot check: the index of who may sign in to it, derived from its person actors, could not be written, or its sign-in record from before identities could not become an identity (a sign-in record that does not open is not this: that repository is served and refuses to open until `user reset` names it) | the error names what failed; a restart runs the pass again |

These still stop the server, because no repository's absence cures them:
the database not answering (a failure after which the database does not
answer a ping is the database's), the start interrupted, another process
holding a repository's writer lease ([below](#one-writer-per-repository-and-a-second-is-refused))
or the server losing its own, a credential key that opens no wrap or one
that names another key (the wrong key, not a damaged row), an entry under
`repositories/` named by neither an id nor an authority, a SQL migration,
and **a start that set aside every repository it found**: a server with
nothing to serve stops with each one's error, as it always has, so a
deployment of one repository sees no change. A repository migration and
the shipped vocabulary upgrade run at a repository's first open, after the
start: one that fails answers that request with the error and runs again at
the next open, and the other repositories are not touched by it.

**How it shows.** Each one logs at warn, once, at the start:

```text
level=WARN msg="substrate: repository set aside at start-up; the server serves the others and never opens it until a restart finds it whole" repository=ada.example.com key=rnd-… stage="boot check" error="…"
```

and the start ends with `substrate: started with repositories set aside`,
with `setAside`, `serving` and the ids. The metric
`substrate_repositories_set_aside` holds the count, and `substratectl
repository list` shows each one's `STATE` as `set aside`: a running server
holds the writer lease of every repository it serves and none of one it set
aside, which the list reads from `pg_locks`. While the start is still
checking, a repository it has not reached yet reads `set aside` too. A 0.x
directory or an archive the start set aside before it had a row has none;
with `SUBSTRATE_DATA_ROOT` set, the list reads the data root and the archive
(with the server's `SUBSTRATE_ARCHIVE` settings) and shows it too, by its id
and with no creation time. `substratectl repository verify <name>` holds a
set-aside repository's table and archive to each other beside the running
server and names what does not verify.

**What it answers.** Every request for it answers `503 unavailable` with
the reason `set_aside` and a five-minute `Retry-After`: a token of its own
(an app's check of its set up included), a webhook addressed to it by its
id, and a sign-in, a deletion or a second factor's enrollment once the
password and code are right. A sign-in into a
repository whose data key wrap is damaged cannot read the password, and
fails as a wrong one does; a webhook addressed by the name answers the
door's one refusal, as an unknown name does. The console's sign-in page and
`substratectl` say the repository is set aside and that its operator brings
it back. Nothing is lost that was not lost already: an app's queued writes
stay queued, and a webhook's provider retries.

**What the start may already have written.** A start does its work on a
repository in steps that each commit, and it sets the repository aside at
the step that fails, not before the first. So a set-aside repository may
hold what the steps before the failure committed:

- **The keying** (stage `id`): the row may already carry its new id, with
  every table's rows moved to it, and the directory's manifest may already
  name the id while the directory is still under the name. The next start
  carries the keying on from where it stopped under the same id.
- **A history conversion** (stage `boot check`): the table's lines may
  already be converted to this binary's changelog dialect, and its stamp
  says so. An older server refuses that repository, as it would after a
  whole start.
- **An import or a restore** (stages `import` and `restore`): a 0.x
  directory or an archive with no row may have a row and part of its fold,
  with an `import_progress` row marking the import unfinished. The next
  start resumes it, reading the same copy, and sets it aside at the boot
  check if that copy is gone.

None of these is the damage: each is what any start does, and the next one
finishes it. They matter when you restore from a copy instead of repairing
in place: restore the copy and the database from the same point before the
start, both of them, since a directory from before the keying beside a row
that took its id is the mismatch the keying is careful about.

**The way back** is the repair the error names, then a restart. The server
holds no file of a set-aside repository open and no lease on it, so its
archive or its 0.x directory can be put back while the others serve; the
restart is what checks it again. A repaired repository serves at that
start; one still damaged is set aside again with the same error.
`substratectl repository rebuild` is not a repair for any of these: it
opens the repository, and the start refuses to.

**Operator commands beside it.** The server's list of what it set aside is
its own memory, and an operator command runs in a process of its own, so
what tells the command is the writer leases: a running server holds the
lease of every repository it serves and none of one it set aside. A running
server holds at least one: a start that serves no repository stops.

| Command | What tells it | What it does |
| --- | --- | --- |
| `repository rebuild`, `repository reindex`, `repository rotate-generation`, the `archive` commands, and a `user reset` that writes a person's actor | it opens as the writer, and before its start keys, checks or imports any repository it takes the lease of every repository with a row: the first one the server holds refuses it | beside a running server it refuses having written to no repository, whatever repository it names and whichever comes first, a set-aside one included, and the refusal names the lease's holder; with the server stopped, its own start decides again and refuses a repository it finds damaged, as the server does |
| an import or a restore | it happens at a start, the server's or a writer command's | the same as above |
| `repository reembed` | it opens read-only beside the server and runs no start, so it reads the leases before it writes its queue rows | refuses a repository the running server holds no lease of while it holds others' (`set aside`), and writes no queue row for it |
| `repository verify`, `repository list` | they read and write nothing | they run, and name what does not verify or read `set aside` |


## One writer per repository, and a second is refused

**Two processes on one database are refused at open**, whether or not they
share a data root
([0083](decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)).
The exclusion is **the database's writer lease**: the server takes a Postgres
session-level advisory lock per repository, keyed by its id, on one
connection pinned for the life of the process, when it opens the repository
(at the boot check, before any import and before any write). It holds
whatever the second process's data root or archive is.

A server that cannot take the lease **does not boot**. The error names the
repository and the rule:

```text
substrate/engine: boot check: repository ada.example.com (id rnd-0vrkvsgbs6bnz2sjy4196kccck):
substrate/engine: another process is this repository's writer: repository
rnd-0vrkvsgbs6bnz2sjy4196kccck. One server per database: this one refuses to open the
repository rather than write it from a directory of its own. If no server is running,
the holder is a connection that has not gone away yet; the lease is the advisory lock
hashtext(current_schema() || '|writer|' || 'rnd-0vrkvsgbs6bnz2sjy4196kccck')::bigint,
visible in pg_locks. Held by pid 4242, application_name "substrate-writer-…",
client_addr 192.0.2.10, backend_start 2026-01-01T00:00:00Z
```

**To see who holds one**, compute the key and ask `pg_locks`. The key is
`hashtext(current_schema() || '|writer|' || <repository id>)::bigint`, composed
the way every other advisory lock the engine takes is:

```sql
WITH k AS (SELECT hashtext(current_schema() || '|writer|' || 'rnd-0vrkvsgbs6bnz2sjy4196kccck')::bigint AS key)
SELECT l.pid, a.application_name, a.client_addr, a.backend_start, a.state
  FROM pg_locks l JOIN pg_stat_activity a USING (pid), k
 WHERE l.locktype = 'advisory' AND l.objsubid = 1
   -- pg_locks splits a 64-bit advisory key across two oids; the mask is what
   -- keeps a negative key's halves unsigned.
   AND l.classid = ((k.key >> 32) & 4294967295)::oid
   AND l.objid = (k.key & 4294967295)::oid;
```

Run it in the schema the server runs in: `current_schema()` is part of the key,
so a psql session on a different `search_path` computes a different one.

A lease is released by its session ending, so a writer that crashed holds
nothing once its backend is gone and the next boot simply takes it. A lease
still held with no server running is a connection that has not gone away yet —
a pooler in front of Postgres will do that — and `pg_terminate_backend(pid)`
on the pid above ends it.

**The lease's session is named and reaped fast.** The pinned connection sets
its own `application_name`, `substrate-writer-` and a token minted once per
process, and its own server-side keepalives (`tcp_keepalives_idle` 10 s,
`tcp_keepalives_interval` 5 s, `tcp_keepalives_count` 3, `tcp_user_timeout`
30 s), so Postgres ends a lease session whose client vanished in about thirty
seconds rather than the two hours the OS defaults allow. A platform that
refuses one of those settings gets a `WARN` naming it, and the lease works
without it. The name is what the holder query above shows in
`application_name`.

**A dropped connection fails closed.** A heartbeat proves the pinned
connection alive every five seconds, and every round trip the lease makes —
the heartbeat's and an acquisition's alike — is bounded at three: a host that
vanished leaves the socket open, so a session that never answers is a lost
lease and not something to wait on. The moment the session
cannot be proven alive the process logs at `ERROR` and refuses every write with
`503 unavailable` until it has taken back every lease it held — or until it
holds none, because a lease guarding nothing has nothing to be lost about.
A write is held to that refusal at its door, again once it holds the changelog
lock, and again immediately before it commits, so a write that queued behind
another one cannot land under a lease that went away while it waited. Every
later beat tries again, so a Postgres that restarted is recovered from without
restarting the server, and the recovery is logged at `WARN`.

A connection the server gave up on is not always one Postgres has noticed is
gone: after a network blip the old backend can live on, still holding every
lease. When a retake or an acquisition is refused, the server reads the
holder; if it carries THIS process's `application_name`, it is the process's
own orphaned session, so the server ends it with `pg_terminate_backend`, takes
the lease again inside the same beat and logs at `WARN` that its previous
session was ended. A holder under any other name is never touched. The server
keeps refusing while another process holds one of its leases, which is the
honest answer, and logs at `ERROR` that another process took the lease,
naming the holder's `application_name`, `client_addr` and `backend_start`:
stop that one, and run one server.

**A repository is leased before it is published.** The control-plane row is
what another process sees, so the lease is taken before it, on both paths that
write one: a registration takes it before the seed, and the boot's restore of
an archive or a 0.x directory takes it before the row it builds from the
manifest. Either one that fails hands it back. Without that order a second
server could see the new row and claim the repository — erasing it under a
failing registration, or, for a restore, serving a repository whose history
the import had not brought in yet.

**The read-only hat takes no lease.** `repository verify` and `repository
reembed` open with no lease on purpose, which is what leaves them safe to run
beside a live server; every other operator command opens as a writer and
meets the lease.

## Upgrading the binary

**Have an agent run the upgrade.**
[`skills/substrate-runbook-upgrade`](../skills/substrate-runbook-upgrade/SKILL.md)
is the runbook for the whole upgrade, from reading the notes to the
provider and sample upgrades afterwards, and the
[README](../README.md#upgrading) says how to install it. What follows is
the mechanics it relies on.

**Read the changelog first.** [`CHANGELOG.md`](../CHANGELOG.md) has one
section per release, newest first: the breaking changes, each with the steps
to follow in order, then what was added and what was fixed. Read the section
of every release between the version you run and the one you deploy. A break
in an env var, a default or the boot is listed there; the
[release page](https://github.com/geoah/substrate/releases) carries the same
section as its body. [Releasing](releasing.md) says how a section is written
and how a release is cut.

**Take a backup before you deploy** ([backups](#backups)). Stop the server,
whose last pass leaves each repository's archive at the head it stopped at,
then copy the database (a dump), the data root and the archive, if it is
kept elsewhere. An upgrade that applies a schema migration closes the
rollback for the whole database, and the copy you take beforehand is the
only way back.

**Each repository carries two dialect stamps, and each refuses a binary that
is behind it.** The
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)
says what shape the stored declaration rows are in, and the
[changelog dialect](changelog.md#the-dialect-a-changelog-is-written-in) says
what a binary must understand to replay the entries. A binary whose maximum is
below a stored stamp refuses to open that repository, by name, and the API
surfaces the refusal as `503 unavailable` with a `Retry-After`, never as an
invalid token, so a store the binary cannot serve is diagnosable rather than
mysterious. There is no promotion step: a fresh
repository is stamped at the binary's maximum at its first open, and the
changelog's claim is written by the first transaction a binary appends, so a
new binary that opened a repository and wrote nothing leaves that stamp alone.
History is rewritten in one case only: a binary that carries a
[conversion step](stored-format.md#conversions) above a repository's changelog
dialect converts that repository at its boot check, once, before it serves
it, and logs `substrate: converting the changelog` with the dialects and the
steps.

**Rolling the image back is only safe while both stamps and the schema still
fit.** An older binary refuses a repository stamped above its maxima and a
database holding a migration it does not carry (below), so the way back from
either is to restore the database, the data root and the archive together,
all from the copies taken before the upgrade, and boot the older binary under
the same credential key. A database restored alone is not a rollback: the
archive still holds what the newer binary wrote, and the older binary's boot
meets it, refusing the repository or importing its newer lines into the
restored copy.
Rolling *forward* to a binary whose maxima cover the stamps always works.
Going back to an older server any other way is not supported, and a release
that closes the way back says so in its notes
([stability](stability.md#going-back)).

**From a release before the archive.** A release before the archive kept
each repository's history in its directory under the data root, and wrote
it there before every commit. The first boot of a release with the archive
holds each such directory to the table, imports any line it holds past the
table's head, removes its manifest, segments and sealed files, and keeps its
blobs and spool ([what happens at boot](#what-happens-at-boot)); the archive's
first passes then copy every repository whole, paced as
[the archive](#the-archive) says. That boot applies migration 0022, which
adds the archive's mark to the `repositories` row, so the way back to the
older release is the database and the data root copied before the upgrade,
restored together: the older binary refuses the migration and reads no
archive. `SUBSTRATE_CHANGELOG_SEGMENT_BYTES` and
`SUBSTRATE_DIGEST_BYTES_PER_SECOND` are no longer read; a host that still
sets either boots and ignores it, and `SUBSTRATE_ARCHIVE_BYTES_PER_SECOND` is
the pacing that took the digest's place.

[Quarantine](vocabulary.md#quarantine) is what a tightened contract does: a
binary that narrows what a declaration may say quarantines each installed
bundle whose stored closure no longer admits, rather than bricking the
repository. Re-installing the bundle, or a later open under a binary that
relaxed the contract, clears the marker.

**A webhook trigger declares the headers its callable reads, since decision
0097.** A trigger written before that as `source.webhook: {}` or `{key: …}`
keeps working, but its fires carry only the five headers that describe the
body (`content-type`, `content-length`, `content-encoding`, `user-agent`,
`date`); a header the callable read from the old built-in list
(`x-github-event`, `stripe-signature`, the Pebble app's `x-index-*`) reads
as absent until the record lists it under `source.webhook.headers`. Before
deploying that binary, run
`substratectl get substrate.reamde.dev/core/trigger -o yaml`, find every
webhook arm, and add the names each callable reads; adding a shipped
sample again does the same for its own trigger but discards a hand-set `key`
([functions](functions.md#triggers)).

**A migration this binary does not carry stops the boot.** The runner records
each migration's version, name and sha256 in `schema_migrations` as it applies
it, and every boot reads that table back before applying anything. A recorded
version the binary does not embed means a newer binary migrated the database,
and the binary refuses to open it: every step after the runner (the orphan
sweep, the declared indexes, the boot check) writes to the schema, and
an older binary does not know the shape it would be writing to. The refusal
names each such row by its recorded name, which is the migration file's name
(`0002_something`); the tree's history says which release added that file. The
repair is to run that release or a later one, or to restore the database and
the data root together from the copies taken before the upgrade, under the same
credential key, as for any rollback (above). This is the database's own downgrade
refusal, beside the two per-repository ones above, and it closes the rollback
even when no repository was written: a new binary that carries a migration
applies it at its first boot. The operator commands that open the engine
refuse the same database: `repository rebuild`, `repository reindex` and
`repository rotate-generation` run the same runner, and `repository verify`,
`repository reembed` and `user reset` read the same table (below).
`repository list` and `inspect` read the tables directly and do not.

**A read-only operator command applies no migration.** `repository verify`
and `repository reembed` run beside a live server, and a newer `substratectl`
that migrated that server's database would close its rollback. So they read
`schema_migrations` and apply nothing: a database missing a migration they
carry is refused, naming each one, and a database holding one they do not
carry is refused as the boot refuses it. The refusal reads:

```text
substrate/engine: the database has not applied migrations this binary carries:
1 migration(s) pending, 10 (0010_records_matching), and this process opened the
database read-only; open it once with a process that writes (the server's boot
applies them), or run the substratectl of the release the server runs
```

Run the `substratectl` of the release the server runs. The writing commands
need the server stopped and apply pending migrations as its boot would, so
run them with the server's release too.

**A migration this binary does not recognize stops the boot too.** The same
read compares the recorded hashes against the files the binary carries. A
difference means the database applied a migration whose text has changed
since, so the binary refuses before applying anything pending: a new migration
must not land on a schema its predecessors did not build. The refusal names
every migration that diverges, with both hashes, rather than the first one it
meets. A pending migration numbered below one the database already recorded
refuses for the same reason: the runner applies in order, and that migration
would land on a schema its successors already changed.

A released binary never triggers this, because a landed migration is never
edited. What does trigger it is a database migrated by a build from a branch
that was still revising its migration. Throw such a database away:
`mise run dev:wipe` for a development one, a restore from a backup a matching
binary wrote for anything else. There is no repair, because two branch
revisions of one migration can differ in any way at all.

**A repository migration is code the boot runs once per repository.** Where a
schema migration changes the tables, a repository migration rewrites what a
repository holds, through the changelog as ordinary record writes, so a
rebuild reproduces the result
([0099](decisions/0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md)).
Each one runs at the repository's first open under a binary that carries it,
before the stored vocabulary loads, in one transaction with the row it leaves
in `repository_migrations` (`version`, `name`, `applied_at`, one row per
repository), and never runs again on that database. The ledger is checked the
way `schema_migrations` is: a recorded version this binary does not carry, a
recorded name that differs, or a pending version below one already recorded
refuses that repository's open by name, and the API answers `503`. A row
a migration rewrites whose kind the binary's registry does not yet hold keeps
its refs rows and observed points as they stand, and its kind is queued in
`index_reprojections`, so the background pass after the open re-derives the
refs rows, search bands and observed points of every row of it under the
loaded vocabulary. A
migration is written to find nothing to do the second time, because an
archive restored into a fresh database carries its changelog and not the
database's ledger, so every migration runs once more there. That is also how
an archive from an older installation, or from one that never opened it
under the newer binary, is brought up to date: the
[boot check](#what-happens-at-boot) folds it from its lines, the migrations
run at its first open, the rewrite lands in this installation's changelog for
the repository, and the ledger row in this database. A process
that opened the data root read-only cannot run one, and refuses a repository
with a pending migration by name. The first
migration, `0001_qualify_bare_declaration_names`, writes the full identity
into every stored `kind:` pin, `trait:` pin, `traits:` binding and function
allowlist entry a binary before
[decision 0098](decisions/0098-a-declaration-names-a-kind-or-trait-in-full.md)
stored bare, resolving each word the way that binary did. Migration 9,
`link_actors`, makes every hand that writes an
[actor record](api.md#actors)
([0270](decisions/0270-every-hand-that-writes-is-an-actor-record.md)): it
tombstones every actor declaration the repository held (core shipped four),
clears each package's `actors` list, links the stored values that still name
a hand by its spelling (a package's `declaredBy`, a blob's `createdBy`, a
call run's `caller`, whose `principal` moves to `token`), and writes an actor
record for the person, the system, each package, function and agent it
holds, and each app a live token was approved for, and a disabled one for
each hand its history names whose code or approval is gone.

**A schema migration never changes what the fold holds.** The fold is every
table `repository rebuild` clears and fills again from the changelog
(`internal/engine/rebuild.go`): `records` with its derived columns `fts`,
`orphaned_at` and `ambiguous_at`, `refs`, `former_ids`, `annotations`,
`property_marks`, `property_offers`, the three tables of the
[observed index](observations.md#the-observed-index), and the delivery ledger
(`trigger_cursors`, `trigger_schedule`, `trigger_failures`,
`paged_cursors`; a rebuild brings a cursor back at its last acknowledged
delivery, not at the scan position past it). A SQL migration changes their
schema alone (an index, a table, a column whose default is the value the
fold writes) and never writes a row of one: `internal/engine/fold.go` is the
one path from the changelog into them. A change to what a row holds goes in a repository
migration, which writes it through the changelog as ordinary record writes,
so a rebuild replays it. A repository migration cannot repair a SQL
migration that already edited the rows: the rows hold the new value, a
write that changes nothing appends nothing, and the next rebuild puts the
old value back. `TestEveryMigrationLeavesAFoldTheRebuildReproduces`
(`internal/engine/migrationrefold_db_test.go`) holds every embedded
migration to this: it upgrades a store one migration behind each one,
rebuilds it, and compares every fold table, and a fixture migration that
runs `UPDATE records` alone must fail it. The test sees only the rows its
seeded repository holds, so a migration aimed at a shape this binary no
longer writes, or at `trigger_failures` or `paged_cursors` (empty in the
seed), adds such rows to the seed first. Three landed migrations predate the
rule: `0003_orphaned_at`, `0004_property_offers_source` and
`0006_ambiguous_at` each add a derived column empty, a row written before
them keeps it empty until a recompute or the source's next write sets it,
and a rebuild derives it for every row. The test exempts those three columns
for a store older than the migration that added each, and nothing else.

## Backups

**The archive is the backup, and the credential key is kept apart from it.**
Each repository's archive holds its history, every blob its records hold,
its sealed values and its spool, and its manifest carries the wrapped DEK
([the archive](#the-archive)). Nothing in the database is needed to bring a
repository back: a server whose database does not hold a repository restores
it from the archive at boot. An archive and the key that opens its sealed
values are a complete backup as of its mark: a write is in it once a pass
after its commit has finished, about ten seconds later with the store
answering.

**Keep the archive off the server's disk.** The default,
`<SUBSTRATE_DATA_ROOT>/archive`, is on the server's own disk and shares its
blobs' files. Point `SUBSTRATE_ARCHIVE` at another disk or a bucket, or copy
the directory elsewhere. A copy taken while passes run can catch a pass
between its steps and hold a manifest whose pieces or blobs it read before
they were written; a copy of a stopped server's archive, whose close ran one
last pass, is whole. So a copy is a backup once
[`repository verify`](#operator-recovery) passes on it: restore it into a
scratch server with an empty database, then verify with
`SUBSTRATE_CREDENTIAL_KEY` set, so every sealed value is opened. A copy that
fails is retaken. A cron running this is enough for the default directory:

```
rsync -a --delete "$SUBSTRATE_DATA_ROOT"/archive/ backup-host:/srv/substrate-backup/archive/
```

**The webhook spool rides with the archive.** `spool/` holds the webhook
requests the door answered `202` and the drain has not recorded in the
changelog yet, each sealed under the repository's DEK
([decision 0167](decisions/0167-the-webhook-door-answers-after-a-sealed-spool-write.md)).
A pass copies every request answered before its snapshot, and the restored
server's first trigger dispatcher pass drains them. A file whose request the
restored changelog already holds is deleted and delivers nothing twice. A
request answered after the last pass is in the data root alone until the
next one.

**An owner downloads the archive from a running server.**
`GET /api/v1/export` streams the repository's archive as one tar, as of one
committed point
([decision 0278](decisions/0278-the-export-is-the-archive-as-one-file-and-the-snapshot-is-gone.md));
[`substratectl export`](substratectl.md#exporting) is the client, and the
bearer token is the whole credential. The tar holds one archive of its own,
under an id minted at the export and so newer than the one the server
writes: `repositories/<id>/<archive>/` with `blobs/`, `sealed/`, `spool/`
and the pieces under `changelog/<dialect>/`, and the manifest last, under
`repositories/<id>/manifests/<archive>/`, whose `head` is the point.
Extracted into a store, it is the newest archive there, so a boot with no
rows for the repository restores from it; one beside a database that holds
the repository stops that repository's passes until the operator resolves
it ([the archive's contract](#the-archives-contract)). An export cut short
has no manifest, and nothing restores it. The
server pins the point with one repeatable-read snapshot of the database and
streams the history afterwards, so writes go on during the download and the
tar still holds one committed state. A blob's bytes are held against
collection from before the snapshot until the stream ends, so every blob the
point holds is there to stream, and the orphan sweep deletes what it held
back on a later pass. The tar carries no host key:
the manifest keeps the DEK wrapped under this server's
`SUBSTRATE_CREDENTIAL_KEY`, so a same-key restore boots with nothing else,
and a restore under any other key brings every record back without the
secrets
([restore without the credential key](#restore-without-the-credential-key)).
One export streams per repository at a time and a second request answers
`409 conflict`; a response the server could not finish is aborted rather
than closed as a tar that looks complete.

```
tar -x -C "$SUBSTRATE_DATA_ROOT"/archive -f ada.example.com-1234.tar   # on a stopped server with the same key, then boot
```

On the compose deployment the data root is the `substrate-data` volume
mounted at `/var/lib/substrate`, and the archive is under it unless the
environment moves it, so copy it out of the container, or point
`SUBSTRATE_ARCHIVE` at a host directory the backup already covers:

```
docker compose cp substrate:/var/lib/substrate/archive ./substrate-backup
```

**Keep `SUBSTRATE_CREDENTIAL_KEY` somewhere the copy is not.** Every
manifest carries the repository's data-encryption key wrapped under it, and
every sealed value is ciphertext under that DEK. The archive without the key
is every record and every attachment in the clear and no secret; the key
beside the archive is every secret too. Of the people, the archive holds
the name, the sealed password and the sealed second factor of everyone in
each repository, which are useless without the key. On compose the key is the
`substrate-keys` volume (`/keys/credential.key`) unless the environment sets
one. The key is the operator's to keep, and it is the one thing that keeps
the secrets: nothing else wraps the DEK, so a copy restored without it comes
back without them
([restore without the credential key](#restore-without-the-credential-key)).

**A database dump is optional beside the archive.** The tables hold nothing
the archive lacks but the runtime state named below and the writes since the
last pass. A dump with the archive is the fastest way back to a known state,
and a boot over a dump older than the archive takes the archive's lines past
the dump's head; a fresh database and the archive are enough too. A dump
reads the whole database, so skip it when the upgrade's downtime matters more
than the restore's; restoring a long history from the archive takes longer
than restoring a dump. With `SUBSTRATE_ARCHIVE=off` there is no archive, and
the backup is Postgres's own, a dump or a continuous copy of its WAL, taken with
the data root's `repositories/`, where the blob bytes are and nowhere else.

**Restore.** Stop the server. Put each repository's archive,
`repositories/<id>/` whole, into the archive the server reads (an export
extracts straight into a directory archive), set the same
`SUBSTRATE_CREDENTIAL_KEY`, and boot over a database that does not hold
those repositories: the boot restores every repository the archive holds and
the database does not, and copies its blobs and spool into the data root
([what happens at boot](#what-happens-at-boot)). A repository whose name
another repository in the database holds is refused. The people in a
repository come back with it, each with the password and second factor the
archive's last pass found, so the tokens it carried authenticate again
([decision 0274](decisions/0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)).
A person the database already held keeps the sign-in it has; a person in
several of the repositories one start restores takes the copy changed last;
a person whose sign-in name another person here holds is left out, and the
boot logs it. An archive written before the archive carried sign-ins still
holds the sign-in record of a release before identities, and the boot makes
the person from it, as for a repository upgraded in place. A repository can
come back with somebody missing only where its copy does not carry them: an
export (it carries only the person who took it), a restore without its
secrets, or a person left out. For
each person missing, `user reset <repository> --identity <name>` gives them back
under the identity the repository's actor for them names, with a password
you choose and a new second factor; `repository list` shows who signs in to
each. Then verify each repository with the key in the environment, so every
sealed value is opened:

```
rsync -a ./substrate-backup/archive/ "$SUBSTRATE_DATA_ROOT"/archive/
SUBSTRATE_DATA_ROOT=… SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substrate   # restores at boot
DATABASE_URL=… substratectl repository list
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify ada.example.com     # once per repository
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl user reset ada.example.com          # a repository nobody signs in to yet
```

**Restoring a dump takes one more step.** A change cursor is a seq under the
repository's history generation
([the changelog](changelog.md#watching)), and the restore has to change that
generation whenever the history clients saved cursors against is not the one
that comes back. A restore from the archive does it by itself: creating the
row from the manifest mints a new generation. A database dump does not: the
row comes back with the generation the dump held, whether or not the
archive's later lines are imported into it. So after any restore that starts
from a dump, boot once so the boot check lands (an archive ahead of the dump
is imported into the table), stop the server, and rotate each repository:

```
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository rotate-generation ada.example.com
```

Every client then re-lists once at its next resume instead of continuing past
writes the restored history never had. A restart and `repository rebuild`
change nothing here.

**An import that dies is resumed, not served.** The next boot check finishes
it and logs `substrate: finishing an interrupted import or conversion`, and
so does `repository rebuild`, which opens the engine the same way. Until one
of them runs, an open of the repository refuses with `the import of the
repository's history has not completed`, and `repository verify` reports the
unfinished import as a finding.

A restore of a long history takes minutes. The boot check logs `substrate:
boot check started`, each import logs `substrate: importing a repository's
history` before its lines, and the server logs `still booting` every 10 s
until its listener is up. A SIGTERM or SIGINT during the boot ends it with
`action=interrupted`, the repository it stopped on and the signal, so the
next boot's resumed import has its cause in the log above it.

A boot that sets a repository aside names why: a bad `sum`, a piece that
is not the run its name says, an archive that cannot be read, or a DEK this
host's `SUBSTRATE_CREDENTIAL_KEY` does not open (below). Move
`repositories/<id>/` out of the archive, or restore it from an older copy,
and restart
([a repository set aside at start-up](#a-repository-set-aside-at-start-up)).

### Restore without the credential key

Moving a repository to another host means bringing `SUBSTRATE_CREDENTIAL_KEY`
with it. The key wraps the repository's DEK and nothing else does
([decision 0264](decisions/0264-a-repositorys-secrets-travel-with-its-host-key-or-are-left-behind-by-the-operators-choice.md)),
so it is what keeps the secrets, the sealed store: every account's OAuth
token, every secret-typed property such as an LLM provider's API key. It
wraps each person's own key too, so it keeps the password hash and TOTP seed
the manifest carries for each person. With the same key set, the copied
archive (an export extracted into the archive is one) restores with all of
them, as above.

An archive brought to a host whose key is another one is set aside at
boot, and so is a 0.x directory; a take of the archive
(`archive resolve --take-archive`) is refused. The log line or the refusal
names the repository's name and id, the id of the key the copy was written
under and the id of the key this host holds. Set the key it names and
restart, which keeps the secrets. Where that key is lost, accept the loss
instead: list the repository, by its name or its id, in
`SUBSTRATE_IMPORT_WITHOUT_SECRETS` (comma-separated for several) and restart.

```
SUBSTRATE_IMPORT_WITHOUT_SECRETS=ada.example.com SUBSTRATE_DATA_ROOT=… SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substrate   # restores at boot, without the secrets
```

The restore mints the repository a new DEK under this host's key and brings
every record and every line of history, the sealed values that do not open
under the new key included; the open that follows deletes the spooled
webhook requests nothing here opens. It leaves out each person whose sign-in
does not open under this host's key, and `substratectl user reset` gives
each one back. A directory from before repository ids
takes its id at the same boot, under the same listing.

**The rule.** After the restore, no read, listing or use of a secret-typed
property returns or acts on a reference whose sealed value the store does
not hold, wherever in the record the reference sits: it reads as unset, whatever state its package is in then or later
(admitted, parked, uninstalled with its records left, installed again) and
however its type is spelled (`secret`, a property type based on it, the
OAuth facility's `tokenRef`), and the owner is shown what to put back. It
holds by order, without asking any declaration: a sealed value that does not
open names its ref and the record holding it in the clear, so it is itself
the record of its loss, and nothing removes it but the write that clears its
ref. The restore keeps each one and marks the repository's row as it creates
it; the repository's next open takes each one's ref out of the record that
holds it, in whatever property holds it and at any depth (in an object or a
list, leaving the rest of what they hold), raises an alert naming the record
and the property (the console's list of what to put back), removes the
value in the same transaction, and lifts the mark once every one is cleared.
A value whose ref its record does not hold anywhere stays, and so does the
mark, until the GC's sweep takes the value no record holds. The archive's next pass
then leaves the value out of its manifest, because the table no longer
holds it. Putting a value back resolves its alert. A boot or an open cut
short anywhere is finished by the next one, since what is left is found
again. A value that opens under the key is never touched, so a retry under
the key the copy was written under keeps every secret, and the same archive
restored again into another database, under the same listing, goes on where
the first left off. Once the mark is lifted, a sealed value that does not
open is damage, not a loss to accept. The owner puts each one back:

- **The sign-in.** Each person whose sign-in does not open under this
  host's key is left out, so they do not sign in until the operator runs
  `substratectl user reset <repository>` (with `--identity <name>` to pick
  one of several) ([operator recovery](#operator-recovery)), which needs
  this host's key and the database, runs beside the server, and sets a new
  password and TOTP enrollment to hand to the person. A token minted before
  the move, a console session or an app's, is a record and travels, and it
  opens the repository again once the reset gives its person back.
- **Each connected account.** An account whose OAuth token stayed behind
  reads `tokenStatus: erroring`, its `tokenError` saying the sign-in did not
  move with the repository, and the console shows it with its reconnect
  button. The owner reconnects each one.
- **Each API key.** An LLM provider's `apiKey`, and every other secret-typed
  property, reads unset. The owner enters each one again.

In order, as the operator types it:

```
SUBSTRATE_IMPORT_WITHOUT_SECRETS=ada.example.com SUBSTRATE_DATA_ROOT=… SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substrate   # 1. start: it restores
SUBSTRATE_DATA_ROOT=… SUBSTRATE_CREDENTIAL_KEY=… substratectl --dsn "$DATABASE_URL" user reset ada.example.com   # 2. beside it: a new password and second factor for the owner
# 3. the listing may go at the next start
```

The owner then signs in with what step 3 printed, and puts back what the
console's alerts name: each account reconnected, each key entered again.

The host must hold a credential key of its own. A host with none refuses a
listed repository before it restores anything, naming
`SUBSTRATE_CREDENTIAL_KEY`: the new DEK would be stored unsealed.
`substrated` does not boot without a key, so this is the engine's own guard
for whatever else opens it.

A listed repository this host's key does open restores with its secrets, as
if it were not listed. The variable reaches only a copy being restored: a
repository the database already holds, whose DEK this host's key does not
open, still refuses the boot ([configuration](#configuration)). So moving
without the key means bringing the archive without the database: restore it
into an empty database, never a dump of the old one.

**What comes back, and what does not.** Trigger state is in the history:
every cursor advance, schedule fire, parked failure and paged-drain page is a
`delivery` changelog entry
([decision 0064](decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)),
folded back into the trigger tables on restore with the rest of the changelog,
and so is every webhook request the door answered `202` to whose fire had not
settled: the server's first trigger dispatcher pass over the restored
repository runs each one under its original fire id, with the body read back
from the blob store
([decision 0068](decisions/0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md)).
A request answered and not yet recorded is a file under `spool/`, which the
same pass drains into the changelog first
([decision 0167](decisions/0167-the-webhook-door-answers-after-a-sealed-spool-write.md)).
A record trigger comes back at the last delivery it acknowledged, or at its
last edit if that is later, and the next pass re-reads the rows after it,
which matched nothing under the source that scanned them, so nothing is
delivered twice and nothing an older source skipped is delivered late; a schedule trigger comes back at the occurrence it last fired
and fires the newest one it missed, or, where its `missed` is `all`, each
one it missed, oldest first, at most ten per pass; a parked
failure keeps the id `…/parked` listed, so a saved retry still names it, and
a parked drain resumes from its last committed page, unless the archive or
the directory was written by a release before record ids sorted in byte
order: its history
carries no mark of the order its drains' continuations are in, so every drain
holding one starts over from its first page at the repository's first open
(repository migration 7). A paged drain that stopped
between pages without parking (a crash, or a shutdown mid-drain) starts over
from its first page: a middle page's entry names the cursor by its SHA-256 and
leaves the bytes in the database, and only a park carries them into the
history
([decision 0141](decisions/0141-a-paged-drain-names-its-cursor-by-hash-and-a-park-carries-it-whole.md)).
On an import of a newer
archive over an older database dump the entries fold over the dump's rows,
so the triggers land where the archive says, not where the dump did.

Runtime state is not in the archive: embedding vectors (queued again,
below), OAuth flows in flight, a record trigger's scan position past rows
that matched nothing, and the `Idempotency-Key` rows, so a retry carrying a
key from before the restore runs its operation again. A consent flow in flight is started again: it is a nonce
and a PKCE verifier with an expiry, and the callback fails once, so the user
starts the flow over. A user's tokens are records, so they come back.
Change cursors that clients saved (the console's tail, `substratectl watch
--from`, a provider's bookmark) are refused once after an import: the row
comes back with a new history generation, and a resume under the old one
answers `410 compacted` naming the head to re-list from ([the
changelog](changelog.md#frames-and-the-horizon)). A dump keeps the row's
generation, which is what `repository rotate-generation` above is for. A
restart and a rebuild keep it too, and neither costs a client its cursor. The
alternatives beside a property (`marks.<property>.alternatives`) are not in
the archive either, and need not be: the import derives them again from the
records it folded, values, source records and stamps alike
([reading provenance](projection.md#where-a-value-came-from-the-marks)).

**The import queues the embeddable properties and the drain buys the vectors
again.** In the transaction that completes it, the import compares the vectors
the database already holds with the records it folded: a vector for a record
or a value that is gone is deleted, a vector
bought by the current provider and model for unchanged text is kept,
and every property without a current vector is queued (the boot logs `import
queued the repository's embeddable properties` with the count). Into an empty
database that is every property; a newer archive restored over an older
database dump queues only what changed, so it does not re-buy the repository.
The drain loop then buys the vectors a batch at a time once the repository's
`llm/provider` row resolves; with no such row the queue rows wait for one. A
`semantic` search says which of the two it is answering from, refused or
partial ([search](api.md#search)). The new vectors come from new provider
calls, so a ranking may differ from before the copy. `reembed` is not part of
a restore; it is for a row re-pointed at another model.

**Encrypt the copy.** The changelog and the blobs are plaintext in the
archive, on the backup host and in the dump alike. The substrate does not
encrypt the storage under it; do that yourself, or use a bucket that does.

### A rotated or deleted secret stays in older backups

Rotating or deleting a secret does not remove it from a backup or an export
taken while it was current, so a credential that leaked must be
revoked at the provider that issued it as well as rotated here. A new value
written to a secret-typed property, a cleared one, a password or TOTP change,
a [purge](#what-happens-at-boot) and the OAuth teardown of a deleted
connected account each delete the old row from the `sealed` table, the
archive's next pass deletes its copy, and no other copy changes. The ciphertext can stay in
Postgres's dead tuples and WAL, on the primary and on any replica, until
Postgres reclaims them (`VACUUM` or page pruning for a tuple, recycling for
a WAL segment), which the substrate does not control. It stays in any copy
of Postgres's WAL, and in every database dump and copy of the archive taken
while it was current (an `rsync` mirror keeps it until its next run). Each of those
still opens under the repository's DEK, which a rotation does not change.
Restoring one of the archive's copies brings the old value back as the
property's current value: an `llm/provider` row restored from a week-old copy
holds the leaked `apiKey` again, and every agent that names the row uses it.
Only the OAuth teardown reaches the provider: it asks the provider to revoke
the account's tokens, best effort, where the provider's bundle declares a
revocation endpoint.

A deleted repository is the same. Deleting it deletes the archive the
server writes, manifest first and before the rows, and with
`SUBSTRATE_ARCHIVE=off` the copy under `<data root>/archive` if one is
there. Every other copy keeps it: a bucket the server no longer writes, a
copy of the archive, a database dump. A server booted with such a copy as its
archive restores the repository, because a repository the archive holds
and the database does not is a restore.

## Operator recovery

These are the [operator hat](substratectl.md#two-hats): no token, `--dsn` (or
`DATABASE_URL`), and a refusal before touching anything without it. The
commands that open the engine also read `SUBSTRATE_DATA_ROOT`, and the
`SUBSTRATE_ARCHIVE` settings the server runs with, so they read the archive
the server writes; none of them runs an archive pass.

**Six of them run beside a live server; four need it stopped.** `repository list` and `repository inspect` read the tables
directly; `repository verify`, `repository history`, `repository reembed`
and `user reset` open the engine read-only, so they apply no migration, run
no boot check and append nothing: `verify` reports what it finds instead of
repairing it, `reembed` writes queue rows, which are not changelog entries,
and `user reset` writes a person's sign-in, which no repository holds.
`repository rebuild`, `repository compact`, `repository reindex` and
`repository rotate-generation` open the
repository as its writer, and a running server holds its
[writer lease](#one-writer-per-repository-and-a-second-is-refused): the
command refuses, naming the lease, until the server is stopped. Opening as
the writer runs the boot check, so they also finish an interrupted import
and restore what the archive holds and the database does not. A reset that
must write the repository a person's actor, because its history names none,
refuses beside a running server too.

```
DATABASE_URL=… substratectl repository list
DATABASE_URL=… substratectl repository inspect ada.example.com
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify ada.example.com
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository rebuild ada.example.com
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository compact ada.example.com
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository history ada.example.com
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository reindex ada.example.com --observed
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository rotate-generation ada.example.com
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl user reset ada.example.com
```

Every command that takes a repository takes its name, as above, or its id;
`repository list` prints both.

**On the compose deployment, run them inside the container.** The image
carries `substratectl` beside the server, because `compose.yaml`
publishes no Postgres port and the DSN resolves nowhere else. The container
holds `DATABASE_URL` and `SUBSTRATE_DATA_ROOT` in its environment, so
neither is repeated on the command line. It holds `SUBSTRATE_CREDENTIAL_KEY`
only when the environment compose reads sets it: a key the entrypoint minted
into `/keys/credential.key` reaches the server process alone, and a bare
`repository verify` then passes without opening a sealed value. A command
that should open them reads the file when the variable is empty:

```
docker compose exec substrate substratectl repository list
docker compose exec substrate sh -c 'SUBSTRATE_CREDENTIAL_KEY="${SUBSTRATE_CREDENTIAL_KEY:-$(cat /keys/credential.key)}" exec substratectl repository verify ada.example.com'
docker compose exec substrate substratectl user reset ada.example.com
```

Publishing the Postgres port to reach the same commands from the host is a
worse trade: it exposes the database to everything that can reach the host, and
the exec path needs nothing open at all.

- **`repository list`** reads the control-plane tables: one row per
  repository, with its name, its id, the names of the people who sign in to
  it (`SIGN-INS`, empty where nobody does yet), when it was created and its
  archive's mark (`ARCHIVED`: the seq the archive holds every line up to and
  how long ago the pass that wrote it ran, `never` before the first pass or
  with the archive off). `-o json` carries the people as `identities` and the
  mark as `archivedSeq`, `archivedAt` and `archiveStore`.
- **`repository inspect <repository>`** reports the authority, the
  repository's id, when it was created, the id of the key its DEK is wrapped
  under, the changelog's head in the table, the archive's mark as last
  published, from the database's record and not read from the store (how far
  it is behind the head, how many blobs it lacks because the server lost
  their bytes, how long ago it was written and to which store), live and
  tombstoned record counts, and the declaration versions per package. It is
  the first thing to run when something looks wrong.
- **`repository verify <repository>`** walks the `changelog` table from seq 1
  to the head in one read-only snapshot: every row must name a transaction
  that fits the rows around it and carry its stamped checksum, and every seq
  must hold a line, but where a compaction may have taken it. A compaction
  removes a line only below a [state entry](stored-format.md#state-entries)
  that stands for it, so a seq with no line at or below the highest line a
  state stands at is counted (`absent`, beside `compactedUpTo`) and is no
  finding, and one above it is a gap, as in a history never compacted. A
  state does not name the lines it replaced, so verify cannot tell a line a
  compaction took from one that went missing below it. With an archive it then holds the archive
  to the table: its manifest's head may not be past the table's, and every
  line of the chain of pieces to it must carry the checksum the table stamped
  for its seq. An archive behind the table is not a finding, because it is
  copied after the commits. What it could not hold to the table it prints
  as `NOT VERIFIED` (`archiveUnverified` in `-o json`), and that is a
  finding for a store that cannot be read, a manifest gone from a store a
  pass published to, and history in an earlier changelog dialect than the
  table's; an archive no pass has published to yet is said and is no
  finding. It then
  holds the side stores to the fold: every blob whose manifest says `stored`
  is read out of the blob store and hashed against its digest, every secret
  reference a live record holds (by the repository's own declarations, as a
  replay loads them, so the records of a package the loader parked are not
  walked) must have its sealed row, every sealed row must have its ref held
  by the record that owns it, live or tombstoned (one that is not is an
  orphan, counted and named by ref, and the next writable open or GC sweep
  erases it), and with `SUBSTRATE_CREDENTIAL_KEY` in the environment every
  sealed row is opened under the repository's key; without the key the
  report says nothing was opened. It reports the head `(seq, checksum)` or
  every finding by seq, piece, digest or ref, never repairs the repository it
  judges (and applies no migration: see
  [upgrading the binary](#upgrading-the-binary)), and exits nonzero on any
  finding. `--recanonicalize` also recomputes every row's checksum from its
  stored columns, which reads and canonicalizes every payload the table holds
  and takes hours on a long history; only it finds a row edited in place with
  its checksum left alone. A verify runs no conversion, so it may meet a
  history the boot has not converted yet (an upgrade stopped before the
  conversion committed, or a repository the start set aside at it): that is
  a finding of its own, and each row is held to the frame its line was
  written in, so a whole row from before the conversion is no mismatch. On a long
  history it prints a progress line to stderr at most every thirty seconds.
  `-o json` prints the report: `repository`, `entries`, `head`, `headHash`,
  `archive`, `archiveHead`, `archivePieces`, `sealedRows`, `sealedOpened`,
  `sealedOrphans`, `secretRefs`, `blobs`, `blobBytes`, `recanonicalized`,
  `findings`, `truncated`, `ok` and `took`. It is safe beside a running
  server; a finding about a blob can be an upload the sweep just collected,
  so run it twice before believing one. Run it on every restored copy and
  before and after a Postgres major upgrade. It proves the history is
  undamaged, the archive agrees with the table and the side stores hold what
  the fold needs; it does not prove who wrote them
  ([the checksum](changelog.md#the-checksum-and-the-archive)).
- **`repository rebuild <repository>`** replays the `changelog` table into a
  fresh fold, in one transaction, under that repository's own lock. It
  reproduces the fold bit for bit and appends nothing, so it is safe to run on
  a healthy repository, and it is the proof that the history alone reproduces
  the records. The search index (`fts`)
  is reproduced too, with one limit: the replay indexes every row under the
  kind declarations in force at the end, and a kind edit that changes what
  its records index re-indexes the kind's rows in the same apply, so the two
  agree from that apply on. Rows indexed before this re-indexing existed,
  under a declaration that has since changed, keep the old bands until a
  rebuild or the next such edit of their kind; a search over them can return
  a hit the rebuilt repository does not, or miss one it does. The records of
  a [quarantined](vocabulary.md#quarantine) package index under its stored
  declaration in the replay as they did live, except where that declaration
  no longer parses. It does not
  touch blobs or sealed values, which were never in the changelog. It replays the delivery ledger with the rest of the
  fold: each trigger's cursor lands at the last delivery it acknowledged, its
  parked failures and a parked drain's resume row come back, and the next pass
  re-reads the rows after the cursor, which deliver nothing. A drain that
  stopped between pages without parking keeps the cursor the database holds,
  because its last page entry names that cursor's SHA-256. OAuth flows in
  flight are left alone. On a long history it prints a progress line to
  stderr every thirty seconds, as `verify` does. Stop the server
  first: it opens the repository as its writer and refuses while the server
  holds the lease. Run it with the binary the server runs: it opens the
  repository the way the server does, so a newer `substratectl` stamps it
  with its own dialects and the older server then refuses the repository.
- **`repository compact <repository> [--up-to <seq>]`** replaces each
  record's entries up to the seq (the head when it is left out) with one
  [state entry](stored-format.md#state-entries), the record's whole state as
  of the newest entry it replaces, and a trigger's delivery entries with one
  ledger entry the same way. A rebuild of the compacted history gives the
  records, versions and times a rebuild gave before, and the history is
  smaller: an entry left with no effect is removed and its seq is not used
  again. The values a record held before its state are not kept: the
  activity view reads them as unknown, and no rebuild brings a state from
  before the compaction back; an archive or a backup taken before it still
  holds them. The archive's next pass writes its pieces again from the lowest
  entry the compaction changed, and until it has, `verify` says the lines
  from there were not held to the table, which is no finding. It stops below the first
  row that does not verify: it reads nothing of that row but its seq,
  compacts only what lies below it, does not look above it, and prints the
  seq under `stopped`. Write the row back as for [a row that stops the
  archive pass](#the-archive), and the next compaction goes past it. Nothing runs it by itself yet. Stop the server first, as
  for a rebuild.
- **`repository reindex <repository> --observed`** queues every record of
  every kind that binds the `observed` trait for the background pass that
  derives the [observed index](observations.md#the-observed-index) again:
  segments, rollup rows in every tier the metrics name, and latest rows,
  their point counts included. It changes no record and appends nothing to
  the changelog, and it is the repair for an index a fault left behind its
  records, short of a rebuild. It prints the kinds and the records it
  queued. The server re-derives them at its next start and logs the
  progress lines of a reshaped kind's indexes; until the pass ends, an
  aggregate read over those kinds answers `503` with `Retry-After`. Stop the
  server first, as for `rebuild`.
- **`user reset <name>`** is the answer to a person who has lost both
  factors. `<name>` is the name they sign in with. It seals a fresh password
  hash and seed on their identity and prints a fresh TOTP enrollment. The data
  and the tokens are untouched; the person gets new keys. There is no
  self-serve recovery, deliberately. It writes no repository, so the server
  keeps running and takes the new factors at once. Named by a repository
  (its id where its name is a person's too) whose person actor names a
  person this server does not hold, one a restore left out or an export did
  not carry, it gives that person back under the identity the actor names,
  so their tokens open it again, with the name the actor carries or
  `--identity`, which also picks one of several. A repository whose
  restored history took its person out (their actor disabled or deleted)
  gets a new person with an actor of their own, which is a write into the
  repository: the reset asks for the server to be stopped, and is run again
  then.

`user reset` refuses on a deployment whose `SUBSTRATE_CREDENTIAL_KEY` the
container was not created with, because it writes sealed material. Set the key
in the environment compose reads, then `docker compose up -d`, which recreates
the container around the new value. Not `docker compose restart`: that restarts
the process the container already has, with the environment it was created
with, so the command goes on refusing and nothing says why.

**A lost second factor is an operator's job, and only an operator's.** Every
credential-change endpoint requires the current TOTP code, and no route resets
a lost credential. So `user reset`, run on the box or through
`docker compose exec`, is the whole of the escape from a lockout in v1. A
deployment nobody can exec into is a deployment where a lost authenticator is
permanent.

Two rules keep operator commands honest, and they explain the output: the CLI
opens the engine with an empty registry, so an operator command can never
overwrite a repository's stored vocabulary with the declarations compiled into
the CLI's own build; and its reads assume the `substrate_app` role, because
row level security does
not bind a superuser and an operator DSN usually is one — without that,
`inspect` would count every repository's rows and report them as one user's.

## What it does not do

This list describes the server as it is today. [Stability](stability.md)
says what a release may change, and the first four below hold for every 1.x
release.

- **No evidence that history was not altered.** The per-entry checksum
  catches corruption. Nothing is signed and there is no hash chain, so
  nothing stored is evidence against the host operator, who holds the
  database, the archive and the credential key alike
  ([the checksum](changelog.md#the-checksum-and-the-archive)).
- **One server process per database.** A second is refused at open
  ([one writer per repository](#one-writer-per-repository-and-a-second-is-refused)),
  so there is no standby, and an upgrade stops the old process before the
  new one serves.
- **No going back to an older server** once a newer one stamped a repository
  or migrated the database. The way back is the copy taken before the upgrade
  ([going back](stability.md#going-back)).
- **No server for strangers.** A server is for one household, or for people
  who trust each other and its operator
  ([who may share a server](#who-may-share-a-server)).

And today:

- **One sign-in per repository.** There is no second person in a
  repository, no sharing between two, and no query across two. What else
  reads a repository does so on its owner's token or approval: an app, an
  assistant, the owner's own functions and agents.
- **No erasure beyond the repository.** A permanent delete erases the
  record's history and what it owns, the archive's copy once its next pass
  has published, but not an export, a database backup or an archive
  version a bucket keeps, a request already in the webhook spool, or text
  another record quotes
  ([decision 0305](decisions/0305-a-permanent-delete-reaches-what-the-record-owns-and-what-asks-to-change-it.md)).
- **No bound on the archive's delay.** The server reports how far each
  archive is behind and goes on serving however far that is
  ([the archive](#the-archive)).

## Publisher refresh

A minute after boot and every 24 hours, the server checks the authority
documents of installed packages carrying `publishedFrom`, processing one
repository at a time. Each outcome is logged with the repository and
package. Only additive kinds-only releases apply automatically, as the
bundle. Code changes and other edits wait for owner review. Live app builds
whose recorded floors are below a narrowing release hold it back.

A failed lookup or a removed document leaves the installed package intact.
The catalog's upgrade offer reports the reason. Local authority documents
use closure paths relative to the document directory; absolute paths,
parent-directory escapes and symlinks escaping that directory are refused.

Next: [the live tests](testing.md), the one suite that talks to real LLM
providers.
