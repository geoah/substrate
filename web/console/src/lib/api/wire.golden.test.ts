/** The console's half of the wire-drift guard.
 *
 * The interfaces under `src/lib/api/` are written BY HAND to mirror the Go
 * structs in `internal/substrate`. Nothing generates them, so the two can
 * drift silently: a renamed Go field is invisible here until something breaks
 * in a browser.
 *
 * `wire.golden.json` is what stops that. A Go test
 * (`internal/substrate/wire_test.go`) reflects over the structs and writes,
 * per shape, the field names it serializes and whether each is REQUIRED
 * (`true`: always written, `null` included) or optional (`false`: dropped at
 * its zero value, so a reader meets it absent). This test asserts the
 * TypeScript interfaces carry exactly those keys with exactly that
 * optionality: a `head?` here against a `head: true` there is the drift the
 * golden exists to catch, and so is a `manager: string` against a
 * `manager: false`.
 *
 * The trick that makes it real rather than decorative is the `Shape<T>` maps
 * below. TypeScript types are erased at runtime, so a test cannot enumerate an
 * interface's keys. A `Shape<T>` literal, though, will not COMPILE unless it
 * lists every key of `T` and no others, `true` for a required key and `false`
 * for an optional one. So each map is checked twice: by `tsc`, against the
 * interface, and here, against the golden. A field that moves in Go fails the
 * Go test; once the golden is regenerated, it fails here until the interface
 * and its map agree.
 *
 * The last test reads the source of every module in this directory: an
 * exported interface is either in the golden or in `notOnTheWire` with the
 * reason it mirrors no Go struct. A wire shape added in `auth.ts` or
 * `agents.ts` is held the same way as one in `types.ts`.
 *
 * When this fails, fix the interface: the golden is the server's word, not a
 * suggestion. */

import { describe, expect, it } from "vitest"

import type { AgentEvent, AgentResult } from "./agents"
import type { RegisterInput, RegisterResult } from "./auth"
import type { RecordPatch } from "./records"
import golden from "./wire.golden.json"
import type {
  AffectedRecord,
  BundleClosure,
  BundlePurged,
  BundleStatus,
  BundleUninstalled,
  BundleUpgrade,
  BundleUpgradeChange,
  CatalogBundle,
  CatalogInput,
  CatalogItem,
  Change,
  ChangePage,
  ChangeRow,
  ChangeTrigger,
  Cond,
  ErrorEnvelope,
  ErrorPayload,
  FunctionCalled,
  IncomingPage,
  IncomingReference,
  IncomingSource,
  InputStatus,
  KindInfo,
  MintedToken,
  OAuthStarted,
  Occurrence,
  OccurrenceList,
  OccurrenceLog,
  OccurrenceProblem,
  OperationalList,
  Page,
  ProblemDetail,
  PropertyAlternative,
  PropertyMeta,
  PutInput,
  RecordFilter,
  SessionUser,
  SetupItem,
  ShippedRecord,
  ShippedUpgrade,
  SubstrateRecord,
  SuggestedMapping,
  TOTPEnrollment,
  TokenInfo,
  TriggerRan,
  TriggerReplayed,
  WebhookAccepted,
} from "./types"

/** The keys of T a literal may leave out. */
type OptionalKeys<T> = {
  [K in keyof T]-?: object extends Pick<T, K> ? K : never
}[keyof T]

/** Every key of T, exactly once: `true` where the interface requires it,
 * `false` where it is optional. `tsc` rejects a missing key, an extra one, and
 * the wrong boolean. */
type Shape<T> = { [K in Exclude<keyof T, OptionalKeys<T>>]: true } & {
  [K in OptionalKeys<T>]: false
}

const errorEnvelope: Shape<ErrorEnvelope> = { error: true }

const errorPayload: Shape<ErrorPayload> = {
  code: true,
  message: true,
  problems: false,
  problemDetails: false,
  head: false,
  generation: false,
}

const problemDetail: Shape<ProblemDetail> = {
  path: true,
  message: true,
}

const substrateRecord: Shape<SubstrateRecord> = {
  id: true,
  kind: true,
  canonicalId: false,
  formerIds: false,
  properties: true,
  labels: true,
  annotations: false,
  version: true,
  kindVersion: false,
  createdAt: true,
  updatedAt: true,
  deletedAt: false,
  finalizers: false,
  propertyMeta: false,
}

const incomingReference: Shape<IncomingReference> = {
  property: true,
  path: false,
  from: true,
}

const incomingSource: Shape<IncomingSource> = {
  id: true,
  kind: true,
  title: false,
}

const incomingPage: Shape<IncomingPage> = {
  incoming: true,
  cursor: false,
  total: true,
}

const propertyMeta: Shape<PropertyMeta> = {
  manager: false,
  tier: false,
  updatedAt: false,
  alternatives: false,
}

const propertyAlternative: Shape<PropertyAlternative> = {
  actor: true,
  value: true,
  updatedAt: true,
}

/** PutInput carries `kind` for the editor's benefit even though the REST body
 * omits it (the collection path already said the kind), which is why it is
 * here rather than being treated as an extra key. */
const putInput: Shape<PutInput> = {
  kind: false,
  id: false,
  properties: false,
  labels: false,
  annotations: false,
  ifVersion: false,
}

/** The patch body (`substrate.PatchInput`). */
const recordPatch: Shape<RecordPatch> = {
  properties: false,
  labels: false,
  annotations: false,
  addFinalizers: false,
  removeFinalizers: false,
  ifVersion: false,
}

const cond: Shape<Cond> = {
  eq: false,
  in: false,
  prefix: false,
  gt: false,
  gte: false,
  lt: false,
  lte: false,
  contains: false,
  exists: false,
}

const recordFilter: Shape<RecordFilter> = {
  kinds: false,
  implements: false,
  ids: false,
  properties: false,
  labels: false,
  deleted: false,
}

const kindInfo: Shape<KindInfo> = {
  identity: true,
  name: true,
  authority: true,
  package: true,
  version: true,
  plural: true,
  source: true,
  description: true,
  definition: true,
}

const change: Shape<Change> = {
  seq: true,
  ts: true,
  actor: true,
  op: true,
  recordId: true,
  kind: true,
  payload: false,
  affected: false,
  hash: false,
}

/** The public change event nested in a Change (decision 0061). */
const affectedRecord: Shape<AffectedRecord> = {
  kind: true,
  id: true,
  version: false,
  deleted: false,
}

const changeTrigger: Shape<ChangeTrigger> = {
  trigger: true,
  callable: true,
  state: true,
  error: false,
}

/** The feed row is the entry plus `triggers`; Go embeds Change and the
 * golden flattens it, as `extends` does here. */
const changeRow: Shape<ChangeRow> = {
  ...change,
  triggers: false,
}

const changePage: Shape<ChangePage> = {
  changes: true,
  cursor: false,
  head: true,
  generation: true,
}

/** The list envelope; the element type does not change its keys. */
const page: Shape<Page<unknown>> = {
  records: true,
  cursor: false,
  head: true,
  generation: true,
}

/** The operational-list envelope is generic; its keys do not depend on the
 * element, so `unknown` pins them. */
const operationalList: Shape<OperationalList<unknown>> = {
  items: true,
  cursor: false,
}

const occurrence: Shape<Occurrence> = {
  kind: true,
  id: true,
  title: false,
  at: true,
  log: false,
}

const occurrenceLog: Shape<OccurrenceLog> = {
  kind: true,
  id: true,
  status: false,
}

const occurrenceProblem: Shape<OccurrenceProblem> = {
  kind: true,
  id: true,
  message: true,
}

const occurrenceList: Shape<OccurrenceList> = {
  occurrences: true,
  truncated: true,
  problems: false,
}

const tokenInfo: Shape<TokenInfo> = {
  id: true,
  label: true,
  createdAt: true,
  expiresAt: false,
}

const mintedToken: Shape<MintedToken> = {
  token: true,
  secret: true,
}

const totpEnrollment: Shape<TOTPEnrollment> = {
  totpSecret: true,
  otpauthUri: true,
}

/** Registration (auth.ts): the request the door decodes and its answer, which
 * is the mint plus the authority and the recovery material. */
const registerInput: Shape<RegisterInput> = {
  inviteCode: true,
  username: true,
  password: true,
  totpSecret: true,
  totpCode: true,
  label: false,
  authority: false,
  recoveryPublicKey: false,
}

const sessionUser: Shape<SessionUser> = { username: true }

const registerResult: Shape<RegisterResult> = {
  ...mintedToken,
  authority: true,
  recoveryKey: false,
  recoveryPublicKey: false,
}

/** The agent chat stream (agents.ts). */
const agentResult: Shape<AgentResult> = {
  reply: true,
  thread: true,
  status: true,
  reason: false,
  effects: true,
  effectsByAction: false,
  turns: true,
  toolCalls: true,
  promptTokens: true,
  completionTokens: true,
  totalTokens: true,
  costUSD: true,
}

const agentEvent: Shape<AgentEvent> = {
  kind: true,
  thread: false,
  text: false,
  id: false,
  tool: false,
  args: false,
  ok: false,
  output: false,
  result: false,
  error: false,
}

/** The catalog entry and the shapes nested in it. */
const catalogBundle: Shape<CatalogBundle> = {
  id: true,
  name: true,
  authority: true,
  package: true,
  description: true,
  version: true,
  tier: true,
  inputs: false,
  requires: false,
  suggestedMappings: false,
  origin: false,
  originVersion: false,
  modified: false,
  closure: true,
}

/** The API's entry is the bundle plus `installed` and `upgrade`; Go embeds
 * CatalogBundle and the golden flattens it, as `extends` does here. */
const catalogItem: Shape<CatalogItem> = {
  ...catalogBundle,
  installed: true,
  upgrade: false,
}

const catalogInput: Shape<CatalogInput> = {
  kind: true,
  inject: false,
  description: false,
}

const suggestedMapping: Shape<SuggestedMapping> = {
  id: true,
  from: true,
  to: true,
  package: true,
  state: true,
  problems: false,
}

const bundleClosure: Shape<BundleClosure> = {
  kinds: true,
  kindDescriptions: false,
  functions: true,
  agents: true,
  mappings: true,
  records: true,
}

const shippedRecord: Shape<ShippedRecord> = {
  kind: true,
  id: true,
}

const bundleStatus: Shape<BundleStatus> = {
  id: true,
  name: true,
  authority: true,
  package: true,
  installed: true,
  enabled: true,
  inputs: false,
  setup: false,
  accounts: true,
  functions: true,
  kinds: true,
  liveRecords: true,
  quarantined: false,
  quarantineReason: false,
  origin: false,
  originVersion: false,
  modified: false,
}

const inputStatus: Shape<InputStatus> = {
  name: true,
  kind: true,
  description: false,
  record: false,
  via: false,
}

const setupItem: Shape<SetupItem> = {
  code: true,
  input: false,
  kind: false,
  record: false,
  message: true,
}

/** The upgrade preview, on a catalog entry and on the shipped-upgrade read. */
const bundleUpgrade: Shape<BundleUpgrade> = {
  available: true,
  from: false,
  to: false,
  changes: false,
  blockers: false,
}

const bundleUpgradeChange: Shape<BundleUpgradeChange> = {
  kind: true,
  id: true,
  from: false,
  to: false,
}

const shippedUpgrade: Shape<ShippedUpgrade> = {
  package: true,
  upgrade: true,
}

const bundleUninstalled: Shape<BundleUninstalled> = { uninstalled: true }
const bundlePurged: Shape<BundlePurged> = { purged: true }
const oauthStarted: Shape<OAuthStarted> = { url: true }
const triggerReplayed: Shape<TriggerReplayed> = { from: true }
const triggerRan: Shape<TriggerRan> = { ran: true }
const functionCalled: Shape<FunctionCalled> = { output: true, effects: true }
const webhookAccepted: Shape<WebhookAccepted> = { fire: true }

const mirrors: Record<string, Record<string, boolean>> = {
  ErrorEnvelope: errorEnvelope,
  ErrorPayload: errorPayload,
  ProblemDetail: problemDetail,
  SubstrateRecord: substrateRecord,
  IncomingReference: incomingReference,
  IncomingSource: incomingSource,
  IncomingPage: incomingPage,
  PropertyMeta: propertyMeta,
  PropertyAlternative: propertyAlternative,
  PutInput: putInput,
  RecordPatch: recordPatch,
  Cond: cond,
  RecordFilter: recordFilter,
  KindInfo: kindInfo,
  Change: change,
  AffectedRecord: affectedRecord,
  ChangeTrigger: changeTrigger,
  ChangeRow: changeRow,
  ChangePage: changePage,
  Page: page,
  OperationalList: operationalList,
  Occurrence: occurrence,
  OccurrenceLog: occurrenceLog,
  OccurrenceProblem: occurrenceProblem,
  OccurrenceList: occurrenceList,
  TokenInfo: tokenInfo,
  MintedToken: mintedToken,
  TOTPEnrollment: totpEnrollment,
  RegisterInput: registerInput,
  RegisterResult: registerResult,
  SessionUser: sessionUser,
  AgentResult: agentResult,
  AgentEvent: agentEvent,
  CatalogBundle: catalogBundle,
  CatalogItem: catalogItem,
  CatalogInput: catalogInput,
  BundleClosure: bundleClosure,
  ShippedRecord: shippedRecord,
  SuggestedMapping: suggestedMapping,
  BundleStatus: bundleStatus,
  InputStatus: inputStatus,
  SetupItem: setupItem,
  BundleUpgrade: bundleUpgrade,
  BundleUpgradeChange: bundleUpgradeChange,
  ShippedUpgrade: shippedUpgrade,
  BundleUninstalled: bundleUninstalled,
  BundlePurged: bundlePurged,
  OAuthStarted: oauthStarted,
  TriggerReplayed: triggerReplayed,
  TriggerRan: triggerRan,
  FunctionCalled: functionCalled,
  WebhookAccepted: webhookAccepted,
}

/** The interfaces the modules in this directory export that mirror no Go
 * struct in `internal/substrate`, each with the reason. Everything else they
 * export must be in the golden. */
const notOnTheWire: Record<string, string> = {
  // types.ts
  LinkedReference:
    "an open map: a reference value is `ref` plus whatever link properties the declaration names, not a struct",
  EnumValue:
    "one element of a property declaration's `values`, declared in internal/vocabulary and out of the golden's reach",
  // discovery.ts
  AuthPolicy:
    "one field of server.json's `registration` object, read as a Partial; the discovery document is #361's to pin",
  // http.ts
  RequestOpts: "the fetch wrapper's own options, never serialized",
  // agents.ts
  ChatHandle: "a client handle over the stream, never serialized",
  // kinds.ts
  PackageNav: "the sidebar's grouping of KindInfo rows",
  AuthorityNav: "the sidebar's grouping of KindInfo rows",
  KindNav: "the sidebar's grouping of KindInfo rows",
  // transcript.ts
  ChangeStamp: "a view the console folds from llmmessage records",
  ToolCallView: "a view the console folds from llmmessage records",
  TurnView: "a view the console folds from llmmessage records",
  DecisionNotice: "a view the console folds from llmmessage records",
  InteractionNotice: "a view the console folds from llmmessage records",
  DeliveryNotice: "a view the console folds from llmmessage records",
  // actors.ts
  ActorMirrors: "client-side actor resolution state",
  ResolvedActor: "client-side actor resolution state",
  // bundles.ts
  TraitRecords: "a bounded read's result the console derives from a Page",
  // changes.ts
  ChangeFeedFilter:
    "the feed's query parameters, URL-encoded, never a JSON body",
  ChangesFeedOpts: "query options for the infinite feed, client-side",
  HistoryPosition: "a seek position the console holds, client-side",
  WatchError:
    "a loose reading of ErrorPayload off a stream frame (`code` as any string); the shape is pinned as ErrorPayload",
  WatchLine:
    "the union of the three ndjson frame shapes (a ChangeRow, `{bookmark, generation}`, `{error}`), keyed by which field is present",
  WatchHandle: "a client handle over the stream, never serialized",
  // overview.ts
  KindCount: "a dashboard tally the console computes",
  // records.ts
  ListParams: "the list read's query parameters, never a JSON body",
  RecordCount: "a bounded walk's tally the console computes",
  IncomingGroup: "a client-side fold of IncomingReference rows",
  // repository.ts
  RepositoryInfo:
    "two properties read off the repository record; not the operator's substrate.RepositoryInfo",
}

/** Every module in this directory, source included, tests excluded: the shapes
 * a module exports are read off its text, since types are erased at runtime. */
const modules = import.meta.glob<string>("./*.ts", {
  query: "?raw",
  import: "default",
  eager: true,
})

const exportedShape = /^export (?:interface (\w+)|type (\w+) = \{)/gm

function exportedShapes(): Map<string, string> {
  const out = new Map<string, string>()
  for (const [path, source] of Object.entries(modules)) {
    if (path.endsWith(".test.ts")) continue
    for (const m of source.matchAll(exportedShape)) {
      out.set(m[1] ?? m[2], path)
    }
  }
  return out
}

describe("wire types mirror the Go structs", () => {
  it("covers every shape the golden names", () => {
    // A shape added on the Go side must be mirrored here, not quietly skipped.
    expect(Object.keys(mirrors).sort()).toEqual(Object.keys(golden).sort())
  })

  it("pins every shape a module under lib/api exports", () => {
    const exported = exportedShapes()
    // The glob has to have read something, or the check below is vacuous.
    expect(exported.get("SubstrateRecord")).toBe("./types.ts")
    const unpinned = [...exported.entries()]
      .filter(([name]) => !(name in golden) && !(name in notOnTheWire))
      .map(([name, path]) => `${name} (${path})`)
    expect(
      unpinned,
      "an exported shape is neither in the golden nor listed in notOnTheWire with a reason"
    ).toEqual([])
    const stale = Object.keys(notOnTheWire).filter(
      (name) => !exported.has(name) || name in golden
    )
    expect(stale, "notOnTheWire names a shape that is gone or pinned").toEqual(
      []
    )
  })

  for (const [name, fields] of Object.entries(
    golden as Record<string, Record<string, boolean>>
  )) {
    it(`${name} has exactly the server's fields, required where it always writes them`, () => {
      const mirror = mirrors[name]
      expect(mirror, `${name} is in the golden but not mirrored`).toBeDefined()
      expect(mirror).toEqual(fields)
    })
  }
})
