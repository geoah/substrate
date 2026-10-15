# Codebase map: providers, OAuth, webhooks, provider tests, catalog, secrets

Snapshot of 2026-10-11, taken by a read-only agent for the observations
build. Paths are relative to the repository root.

Four facts that shape the provider work:

- No shipped provider receives webhooks today. WHOOP has no webhook trigger;
  its `triggers.yaml` holds only on-connect, on-request and hourly schedule
  triggers. The one working example of an inbound webhook is the Pebble
  sample (`samples/pebble`).
- The health plan (`docs/plans/health-data.md:107-108`) records that Google
  Fit ends in 2026 and that Garmin's partner program is closed. Both
  providers are built here against documented shapes and mock upstreams, and
  their READMEs must say so.
- A function body can never read an OAuth client secret. A webhook verifier
  that needs the app secret keeps its own copy as a core `secret` record.
- The webhook endpoint caps a non-multipart body at 1 MiB
  (`internal/api/webhooks.go:21`). A larger body gets a 413.

## 1. Layout of one provider (whoop, google)

Each provider is a directory `kinds/providers.substrate.reamde.dev/<pkg>/`
with three files: `bundle.yaml` (the install unit), `triggers.yaml`
(ordinary trigger records) and `README.md`. The whole tree is embedded via
`kinds/kinds.go:43`. The authority record is shared:
`kinds/providers.substrate.reamde.dev/authority.yaml`.

`whoop/bundle.yaml`, in order: package (lines 91-121, `version: 18`); bundle
(127-209) with `inputs.client` (kind `whoop/config`, `inject: functions`,
142-146), the trusted `oauth2:` block (164-200: authorization and token
endpoints, `emailEndpoint` and `emailProperty`, `featureScopes` mapping each
toggle to its scopes), `installs:` (201-209); config kind (222-264, trait
`core/oauth2`, with `clientId`, `clientSecret: secret` and `apiBase`, the
test seam); account kind (277-581); mirror kinds from 610; the function from
1367.

Google has the same shape with four functions (`synccontacts` at
`google/bundle.yaml:4475`, then `syncgmail`, `synccalendar`, `syncdrive`).
Each declares `stream:` (e.g. line 4486), and `triggers.yaml` has
on-connect, scheduled and on-request triggers for each stream.

Function declaration (`whoop/bundle.yaml:1367-1397`):

```yaml
kind: substrate.reamde.dev/core/function
metadata: {id: providers.substrate.reamde.dev/whoop/whoopsync}
data:
  runtime: python
  timeout: PT60S
  permissions:
    writes: [.../whoop/user, .../cycle, ..., .../whoop/account]
    network: ["api.prod.whoop.com", "127.0.0.1", "localhost"]
  source: |
    # /// script
    # dependencies = ["requests==2.32.4"]
    # ///
```

- The entry point is `def main(input, host)` at line 2338.
- It reads `input["config"]`: `accounts[]` (each with `token` or
  `tokenError`) and `inputs.client.properties.apiBase`.
- A paged drain uses `host.page.resume()` and returns `{"more":
  host.page.more(cur)}` (lines 2350 and 2247).
- The body pins the origin itself (`_check_origin`, line 1535).

How functions run: `internal/runner/pyhost.go` runs one Python process per
installation. A PEP 723 block is resolved by `uv sync --script` when the
function is installed (`pep723.go`). The host SDK is
`internal/runner/host.py`. The body runs under Landlock, seccomp and rlimits
(`internal/sandbox/sandbox.go`). Timeout defaults to 5 s and is capped at
60 s (`docs/functions.md:59`). Paged-drain caps: 512 pages, 2 minutes,
200k effects (`internal/engine/functions.go:115-127`).

How records are written: writes are staged effects, applied atomically after
the body returns: `host.effects.put/patch/delete` (`host.py:414-470`).
WHOOP's pattern is a put with `if_absent=True` carrying the non-null
fields, then a patch with everything (`whoop/bundle.yaml:1881-1883`). Every
row a function writes must be listed in `permissions.writes`.

Composed ids: `host.ids.external(provider, accountId, "<prefix>/<key>")`,
defined at `host.py:252` and mirrored in Go at `internal/runner/ids.go:32`,
is the provider slug plus a SHA-256 hash. WHOOP uses `_rid` at
`bundle.yaml:1737`. The observations plan composes readable ids instead
(`<provider>/<account record id>/<upstream id>[/<role>]`, up to 128
characters, `/`, `:` and `@` admitted) and falls back to the hash only for a
segment outside the id alphabet.

Triggers (`whoop/triggers.yaml`): `whoop-on-connect` (lines 18-44), a record
trigger with CEL `when: tokenStatus=="connected" && !lastSyncedAt && any
toggle`; `whoop-on-request` (55-71), fires while `syncRequestedAt !=
syncRequestedAck`; `whoop-scheduled` (76-90), `FREQ=HOURLY`.

The `sync` trait: declared in `kinds/substrate.reamde.dev/core/core.yaml:263-284`.
Its 12 properties must be redeclared on the account with `writer:` roles
(`whoop/bundle.yaml:507-581`). The dispatcher stamps start, finish and park
only around a record-sourced delivery (`internal/engine/sync.go:68-90`; it
returns nil when `envelope["record"]` is nil). On the schedule path, and on
a webhook, the body must stamp the fields itself. Status reads: `GET
/api/v1/sync/status` (`internal/api/api.go:321`), `SyncStatuses`
(`sync.go:342`), and `substratectl sync status`
(`cmd/substratectl/commands/sync.go`).

OAuth facility: `internal/oauthflow/oauthflow.go` handles signed state,
PKCE, exchange and refresh. It always sends client id and secret in the
request body, `access_type=offline` and `prompt=consent`, and always uses
PKCE (lines 116-150). There is no per-provider override.
`internal/engine/oauth.go`: `StartOAuth` (309) derives scopes from the
enabled toggles (`oauthScopesForAccount`, 274); `oauthClientOf` (246) reads
`clientId` and `clientSecret` from the resolved client input;
`accountAccessToken` (603) refreshes on demand; the background loop is
`RefreshOAuthTokens` (640). Routes: `POST /api/v1/oauth/start` and `GET
/api/v1/oauth/callback` (`api.go:240,252`). Server config:
`SUBSTRATE_OAUTH_STATE_KEY` and `SUBSTRATE_OAUTH_CALLBACK_URL`
(`internal/config/config.go:127-128`). `revocationEndpoint` must be an RFC
7009 form POST. WHOOP omits it (`bundle.yaml:158-163`).

OAuth account kind (`whoop/bundle.yaml:277-321`):

```yaml
kind: substrate.reamde.dev/core/kind
metadata: {id: providers.substrate.reamde.dev/whoop/account}
data:
  purpose: internal
  traits: [substrate.reamde.dev/core/accountconfig, substrate.reamde.dev/core/sync]
  displayTemplate: "{email}"
  properties:
    tokenRef:      {type: secret, writer: oauth}
    tokenStatus:   {type: string, writer: oauth}
    grantedScopes: {type: string, repeated: true, writer: oauth}
    tokenError / tokenErrorAt / email      # writer: oauth
    enabledProfile ... enabledWorkouts     # writer: owner, keys of featureScopes
    syncFrequency / backfillDepth          # writer: owner enums
    syncCursors {keyed}, retryNotBefore, syncStatus, ...  # writer: connector
```

## 2. Inbound webhooks

Route (`internal/api/api.go:183-184`):

```go
r.Post("/webhooks/{authority}/{trigger}", h.postWebhook)
r.Post("/webhooks/{authority}/{trigger}/{key}", h.postWebhook)
```

Handler (`internal/api/webhooks.go:48-71`): a semaphore of 8 deliveries in
flight (then 503 "webhook door is busy, retry"), `readWebhookRequest`, then
`h.svc.ReceiveWebhook(ctx, authority, trigger, webhookKey(r), req)`, then
`202 {"fire": id}`.

- Key: `webhookKey` (line 77) takes it from the trailing path segment,
  `?key=`, or `Authorization: Bearer`. Bearer suits Health Auto Export,
  which lets the user set custom headers. `Authorization`, `Cookie` and
  their proxy and set variants are stripped before the function sees the
  request (line 37).
- Limits: 1 MiB inline body (line 21); 32 MiB multipart; 16 KiB of
  headers; 8 deliveries in flight.
- Responses: it answers only POST, always with `202 {"fire":id}`. There is
  no GET challenge or echo step.
- Engine side: `ReceiveWebhook` and `admitWebhook`
  (`internal/engine/webhooks.go:75,130`) resolve the repository by
  authority, check the trigger is `Webhook && Enabled`, and compare the key
  in constant time. The request is recorded in the delivery ledger before
  the 202, then fired with `runner.ModeWebhook` (line 212).
- What the function receives: `input.envelope.request` = `{method,
  contentType, headers, query, body:{text|base64}}` or `parts`. It is built
  at lines 627-660, and the body is byte-exact so a signature over it can be
  checked. Only the header names in the trigger's `source.webhook.headers`,
  plus the body headers, arrive (lines 424-442; decision 0097).
- Signature checks happen in the function, not the host (decision 0045).
  Webhook-fired functions still receive the full `config` (accounts, inputs,
  settings) via `invocationconfig.go:37`.
- Webhook URL: `webhookPath(authority, triggerID)` builds
  `/webhooks/<repo authority>/<trigger id>` (`webhooks.go:37`). It is
  surfaced at `functions.go:3085`, which feeds the WEBHOOK column of
  `substratectl trigger status`.
- Trigger shape is in `kinds/substrate.reamde.dev/core/trigger.yaml:9-22,129-145`:
  `webhook: {key?, headers?}`; `arguments` is refused.
- Example: `samples/pebble/triggers.yaml:23-40` (trigger `pebble-webhook`)
  and `samples/pebble/bundle.yaml:253-310` (`main` reads
  `envelope.request`). It does not verify a signature.

## 3. Tests

`internal/providertest` drives one function through the engine against a
hand-written loopback fake; it needs a database and uv. The harness is
`harness_test.go`:

| Helper | Line |
| --- | --- |
| `newCoreDataset`, `newDataset`, `newOAuthDataset` | 178, 199, 227 |
| `install(t, ds, dir, rewire)` (applies `bundle.yaml` via `ApplyVocabularyDocuments`) | 305 |
| `installRewired`, `rewriteSource` (swap the API base or constants in the source; fails if nothing matched) | 322, 374 |
| `rewriteOAuthEndpoints` | 400 |
| `installTriggers` | 335 |
| `newStepper` (`engine.NewStepper`, `internal/engine/steptest.go:51`), with `step`, `apply`, `drain`, `drainApplying`, `setEnvelope` | 715 |
| `stepConfig` (injected `{accounts:[{id,type,properties,token}]}`) | 817 |
| `fakeAPI` (httptest server, request log, `bearer()` check, `fakeToken = "at-1"`) | 885-979 |

`main_test.go:19-28` opens loopback egress (`SUBSTRATE_SANDBOX_EGRESS_ALLOW`,
`SUBSTRATE_EGRESS_ALLOW`).

Example test entry (`github_test.go:228-239`):

```go
requireUV(t); t.Parallel()
_, ds := newCoreDataset(t)
install(t, ds, githubDir, nil)
fake := newFakeGitHub(t, 3)
mustPut(t, ds, substrate.PutInput{Kind: githubAccountType, ID: githubAccount,
	Properties: syncProps(nil, "enabledPullRequests")})
s := newStepper(t, ds, githubSyncFn, githubConfig(t, ds, fake)) // config.inputs.client.properties.apiBase = fake.ts.URL
for _, ef := range s.drainApplying(nil) { ... }
```

No real credentials are involved: the config is injected with a fake token,
and the API base is pointed at loopback. For a webhook function, use
`setEnvelope({"request": ...})`. The inline seam `ReceiveWebhookSync` sits
in `internal/engine/export_test.go:573`, which providertest cannot see.

`internal/providere2e` (`README.md`; `e2e_test.go:49` holds the provider
list) starts a real substrated: `runner/e2e.py` starts `runner/mockserver.py`
over `fixtures/<p>/*.json` (each file is named after the request it answers,
`GET_v2_cycle.json`, and the mock includes an OAuth stub); it rewires a
temporary copy of the bundle, writes the config and account records, and
completes OAuth or writes the token (`"auth":"token"`); it then runs
`providers/<p>/scenario.py`. Each provider pins a mock port in
`providers/<p>/e2e.json` (48111 to 48117 are taken; whoop is 48116). The
dispatcher runs at most 8 repositories at once.

`mise run test:db:providers` runs `go test -count=1 -p 1 -timeout 55m -skip
'^TestLive' ./internal/providere2e/...` (`.mise.toml:153-162`). Providertest
runs under `test:db` and `test:db:rest`.

## 4. Catalog

- Loading: `internal/catalog/catalog.go`. Any directory under
  `kinds/providers.substrate.reamde.dev/` with a bundle document becomes a
  provider entry automatically. Its parent directories' YAML (the
  `authority.yaml`) is included in the install (`loadBundle`, 211). Only
  `.yaml` files are read.
- Install: `POST /api/v1/catalog/{id}/install` (`api.go:285`) to
  `InstallConfirmed` (`catalog.go:423-469`). The package lands `Published`
  under its own authority, with `ShippedVersion` stamped, and is written as
  the bundle's own actor. Triggers are applied as data records.
- `kinds:check` (`.mise/kindscheck.sh`, `cmd/vocabularydiff`): a new
  package is an addition and needs no version bump; a changed declaration
  needs one; new kind names may not use a dead word from `docs/terms.md`
  and must follow their family's stem (`names.go`); package and kind names
  must be one lowercase word, so `googlefit` or `applehealth`, never
  `google-fit` (`host.py:201`).
- Tests a new provider must pass (`kinds/kinds_test.go`): every kind has a
  `displayTemplate` (268); purpose is valid (308); the function's short
  name is unique across every shipped bundle (425), so use names like
  `garminsync` rather than `sync`; the whole catalog installs with zero
  warnings (457).
- Docs and UI that list providers by name and need updating:
  `docs/bundles-catalog.md` (counts at 3-9, table at 57-70, OAuth and token
  lists at 91 and 113, plus a section per provider; WHOOP's is at 721);
  `docs/bundles.md:113-114, 305-312, 321`; `README.md:34, 357-358`;
  `docs/testing.md:13, 590` ("seven"); `.mise.toml:148-162` descriptions;
  console `web/console/src/lib/kind-names.ts:157-167` (brand casing) and
  `actor-identity.ts:21-29` (colours).

## 5. Secrets and config

- `internal/providersecret` is not for provider functions. It is a 38-line
  helper that removes an LLM or embed API key from error text (`Scrub`).
- Inputs (`internal/engine/inputs.go:1-25`): the bundle declares
  `inputs.<name>: {kind, inject: functions}`. One record is picked: the
  bound one, else the one with id `default`, else the only one. It is
  injected at `config.inputs.<name>.properties` with secret-typed values
  decrypted (`invocationconfig.go:59-81, 177-216`).
- The OAuth client secret and token reference are never injected
  (`isOAuthFacilitySecret`, `invocationconfig.go:222-233`).
- A user-supplied token without OAuth has two shipped patterns: a config
  record (Linear: a non-oauth2 config kind with `apiKey: {type: secret,
  writer: owner}`, `linear/bundle.yaml:118-180`; the function reads
  `config.inputs.connector.properties.apiKey`, line 5392; no `oauth2:`
  block); or core `secret` and `setting` records (decision 0076;
  `internal/engine/settings.go`), shipped empty in a `settings.yaml` with id
  `<authority>/<package>/<name>`, read as `config.settings.<name>`
  (`samples/firecrawl/settings.yaml:10-19`). This suits a webhook signing
  secret or a shared key the owner pastes in. Either way the secret is
  sealed at rest and removed from logs and errors by the invocation scrubber.
