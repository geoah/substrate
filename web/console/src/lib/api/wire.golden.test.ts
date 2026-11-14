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

import type {
  AgentEvent,
  AgentMention,
  AgentResult,
  ChatRequest,
} from "./agents"
import type { LoginRequest, RegisterInput, RegisterResult } from "./auth"
import type { RecordPatch } from "./records"
import golden from "./wire.golden.json"
import type {
  A2UICreateSurface,
  A2UIDeleteSurface,
  A2UIMessage,
  A2UIUpdateComponents,
  A2UIUpdateDataModel,
  AffectedRecord,
  Aggregate,
  AggregateBucket,
  AggregateBudget,
  AggregateGroup,
  AggregateHead,
  AggregateMetric,
  AggregateMetricRead,
  AggregateProperty,
  AggregateRange,
  AggregateRecord,
  AggregateRecordOffsets,
  AggregateRecordSources,
  AggregateRecords,
  AggregateResult,
  AggregateRolling,
  AggregateRow,
  AggregateSources,
  AggregateValue,
  AuthorizationAccount,
  AuthorizationAnswer,
  AuthorizationApp,
  AuthorizationApproval,
  AuthorizationBlock,
  AuthorizationCheck,
  AuthorizationCheckResult,
  AuthorizationCheckScopes,
  AuthorizationDetail,
  AuthorizationFailure,
  AuthorizationFunction,
  AuthorizationReach,
  AuthorizationReachBy,
  AuthorizationKind,
  AuthorizationProviderInput,
  AuthorizationProviderAgent,
  AuthorizationProviderOption,
  AuthorizationProviderChoice,
  AuthorizationPackage,
  AuthorizationSetting,
  AuthorizationSettingValue,
  AddonInstallResult,
  AuthorizationPackageStep,
  AuthorizationPrevious,
  AuthorizationPublisher,
  AuthorizationPackagePublisher,
  AppAddon,
  AppletActInput,
  AppletAction,
  AppletFrame,
  AppletHeader,
  AppletRenderInput,
  AppletShown,
  AppletState,
  AuthorizationRedirectTo,
  AuthorizationRequest,
  AuthorizationScope,
  AuthorizationCallFunction,
  AuthorizationSetup,
  AuthorizationTrigger,
  AuthorizationWebhookPlan,
  BlobInfo,
  DomainCheck,
  DomainProof,
  SiteInfo,
  OperatorDefaults,
  OperatorInfo,
  OperatorAllowance,
  OperatorSpend,
  OperatorDefault,
  BundleClosure,
  BundlePurged,
  BundleFeature,
  BundleStatus,
  BundleUninstalled,
  BundleUpgrade,
  BundleUpgradeHolder,
  BundleUpgradeChange,
  BundleCodeChange,
  CatalogBundle,
  CatalogInput,
  CatalogItem,
  Change,
  ChangePage,
  ChangeRow,
  ChangeRun,
  ChangeRunPage,
  ChangeTrigger,
  Cond,
  ConversionConfirm,
  ConversionPlan,
  ConversionStep,
  Downsample,
  ErrorEnvelope,
  ErrorPayload,
  FunctionCalled,
  InputStatus,
  KindInfo,
  KindLabel,
  KindRetention,
  RetentionChange,
  LinkedRecord,
  Erasure,
  ErasureWait,
  ErasureOwned,
  MintedToken,
  OAuthStarted,
  OccurrenceProblem,
  OperationalList,
  Page,
  ProblemDetail,
  MarkInput,
  PropertyAlternative,
  PropertyChange,
  PropertyMark,
  PutInput,
  RankedPage,
  RecordFilter,
  RecordLink,
  ReferenceSite,
  Referencing,
  SessionCredential,
  IdentityInfo,
  RepositoryChoice,
  SetupItem,
  ShippedRecord,
  Scores,
  ShippedUpgrade,
  SubstrateRecord,
  SuggestedMapping,
  TOTPEnrollment,
  TokenInfo,
  SyncProgress,
  SyncStatus,
  SyncStream,
  TriggerFailure,
  TriggerRan,
  TriggerReplayed,
  TriggerStatus,
  RepositoryConversion,
  RepositoryUnit,
  UnseededRecord,
  UnitConversion,
  UnitTable,
  VocabularyPlan,
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
  reason: false,
  message: true,
  problems: false,
  problemDetails: false,
  head: false,
  generation: false,
  repositories: false,
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
  marks: false,
  linkedFrom: false,
  erasure: false,
  computed: false,
}

/** One inbound mapping-owned link, on a single-record read. */
/** Where erasing a record's history stands (decision 0304). */
const erasure: Shape<Erasure> = {
  kind: true,
  id: true,
  live: true,
  pending: true,
  since: false,
  waits: false,
  held: false,
  damaged: false,
  purged: false,
  owned: false,
}

const erasureOwned: Shape<ErasureOwned> = {
  remaining: true,
  purged: true,
  batch: true,
}

const erasureWait: Shape<ErasureWait> = {
  trigger: true,
  cursor: true,
  since: true,
}

const linkedRecord: Shape<LinkedRecord> = {
  ref: true,
  kind: true,
  title: false,
  property: true,
  mapping: true,
}

/** The reverse read's target, and one site a record points from. */
const referencing: Shape<Referencing> = {
  ref: false,
  refs: false,
  property: false,
}

const referenceSite: Shape<ReferenceSite> = {
  property: true,
  path: false,
}

const propertyMark: Shape<PropertyMark> = {
  pinned: false,
  setBy: false,
  onBehalfOf: false,
  derivedFrom: false,
  setAt: false,
  alternatives: false,
}

const propertyAlternative: Shape<PropertyAlternative> = {
  derivedFrom: false,
  value: true,
  updatedAt: true,
}

const recordLink: Shape<RecordLink> = {
  ref: true,
}

/** The pin and release body. */
const markInput: Shape<MarkInput> = {
  properties: true,
  ifVersion: false,
}

/** PutInput carries `kind`: `POST /records` reads it to place the record, and
 * a PUT at a record path lets the URL say it instead. */
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
  match: false,
}

const recordFilter: Shape<RecordFilter> = {
  search: false,
  kinds: false,
  implements: false,
  purposes: false,
  ids: false,
  properties: false,
  labels: false,
  deleted: false,
  referencing: false,
  orphaned: false,
  ambiguous: false,
}

const kindInfo: Shape<KindInfo> = {
  identity: true,
  name: true,
  authority: true,
  package: true,
  version: true,
  source: true,
  description: true,
  label: false,
  definition: true,
}

const kindLabel: Shape<KindLabel> = {
  singular: true,
  plural: true,
}

const kindRetention: Shape<KindRetention> = {
  kind: true,
  declared: false,
  override: false,
  overrideId: false,
  effective: true,
}

const retentionChange: Shape<RetentionChange> = {
  kind: true,
  from: true,
  to: true,
}

const blobInfo: Shape<BlobInfo> = {
  digest: true,
  size: true,
  name: false,
  mediaType: false,
  status: true,
}

const domainProof: Shape<DomainProof> = {
  name: true,
  record: true,
  proof: true,
  verifiedAt: false,
}

const domainCheck: Shape<DomainCheck> = {
  verified: true,
  verifiedAt: false,
  reason: false,
}

const siteInfo: Shape<SiteInfo> = {
  name: true,
  tagline: false,
  logo: false,
  icon: false,
  landing: false,
}

const operatorDefaults: Shape<OperatorDefaults> = {
  operator: false,
  items: true,
}

const operatorInfo: Shape<OperatorInfo> = {
  name: false,
  allowance: false,
}

const operatorAllowance: Shape<OperatorAllowance> = {
  centsPerDay: true,
  usedCents: true,
  resetsAt: true,
  bySlot: false,
}

const operatorSpend: Shape<OperatorSpend> = {
  slot: true,
  cents: true,
}

const operatorDefault: Shape<OperatorDefault> = {
  id: true,
  slot: true,
  label: true,
  target: false,
  source: true,
  operatorOffers: true,
  readiness: true,
  message: false,
  notice: false,
  models: false,
}

const change: Shape<Change> = {
  seq: true,
  ts: true,
  actor: true,
  onBehalfOf: false,
  token: false,
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
  state: false,
  properties: false,
}

/** One property's before and after on an affected record (decision 0135). */
const propertyChange: Shape<PropertyChange> = {
  name: true,
  renamedFrom: false,
  before: false,
  after: false,
  beforeUnknown: false,
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
  horizon: false,
}

const changeRun: Shape<ChangeRun> = {
  actor: true,
  kind: true,
  verb: true,
  count: true,
  records: true,
  recordId: false,
  newestSeq: true,
  oldestSeq: true,
  newestTs: true,
  oldestTs: true,
  token: false,
  tokenName: false,
}

const changeRunPage: Shape<ChangeRunPage> = {
  runs: true,
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
  included: false,
  matches: false,
  problems: false,
  count: false,
}

/** The ranked read's envelope, and one hit's per-arm scores. */
const rankedPage: Shape<RankedPage> = {
  records: true,
  scores: true,
  pending: true,
  capped: false,
  note: false,
}

const scores: Shape<Scores> = {
  lexical: false,
  semantic: false,
}

/** The operational-list envelope is generic; its keys do not depend on the
 * element, so `unknown` pins them. */
const operationalList: Shape<OperationalList<unknown>> = {
  items: true,
  cursor: false,
}

const occurrenceProblem: Shape<OccurrenceProblem> = {
  kind: true,
  id: true,
  message: true,
}

const aggregate: Shape<Aggregate> = {
  metric: false,
  metrics: false,
  property: false,
  latest: false,
  from: false,
  range: false,
  bucket: false,
  groupBy: false,
  reducers: false,
  sources: false,
  unit: false,
  durationUnit: false,
  maxGap: false,
  fill: false,
  rolling: false,
  budget: false,
}

const aggregateMetric: Shape<AggregateMetric> = { ref: true }

const aggregateMetricRead: Shape<AggregateMetricRead> = {
  ref: true,
  unit: false,
  durationUnit: false,
  reducers: false,
  from: false,
  statistic: false,
}

const aggregateProperty: Shape<AggregateProperty> = {
  name: true,
  anchor: false,
  utcOffset: false,
  groupBy: false,
  sources: false,
}

const aggregateRecordSources: Shape<AggregateRecordSources> = {
  policy: false,
  order: false,
  property: false,
  match: false,
}

const aggregateRange: Shape<AggregateRange> = {
  gte: true,
  lt: true,
  recordedLocal: false,
}

const aggregateBucket: Shape<AggregateBucket> = {
  every: false,
  origin: false,
  calendar: false,
  timezone: false,
  fallbackTimezone: false,
  weekStart: false,
  place: false,
  records: false,
}

const aggregateRecords: Shape<AggregateRecords> = {
  filter: true,
  sources: false,
  utcOffsets: false,
}

const aggregateRecordOffsets: Shape<AggregateRecordOffsets> = {
  start: true,
  end: false,
}

const aggregateSources: Shape<AggregateSources> = {
  policy: false,
  order: false,
}

const aggregateRolling: Shape<AggregateRolling> = {
  buckets: true,
  reducers: false,
  through: false,
}

const aggregateBudget: Shape<AggregateBudget> = {
  maxBuckets: false,
  maxSamples: false,
  maxGroups: false,
}

const downsample: Shape<Downsample> = { points: true }

const aggregateResult: Shape<AggregateResult> = {
  metric: false,
  property: false,
  unit: false,
  units: false,
  bucket: false,
  from: false,
  exact: true,
  rows: true,
  sources: false,
  head: true,
}

const aggregateHead: Shape<AggregateHead> = {
  changelog: true,
  generation: true,
}

const aggregateRow: Shape<AggregateRow> = {
  start: true,
  end: true,
  form: false,
  group: false,
  record: false,
  from: false,
  sources: false,
  coverage: false,
  values: true,
  count: true,
  first: false,
  offsetless: false,
  prorated: false,
  bounded: false,
  synthetic: false,
  nonAdditive: false,
}

const aggregateGroup: Shape<AggregateGroup> = {
  source: false,
  about: false,
  code: false,
  metric: false,
  channel: false,
  statistic: false,
  currency: false,
  properties: false,
}

const aggregateRecord: Shape<AggregateRecord> = { ref: true, version: true }

const aggregateValue: Shape<AggregateValue> = {
  reducer: true,
  value: true,
  unit: false,
  code: false,
  comparator: false,
  at: false,
  rolling: false,
  of: false,
  n: false,
}

const tokenInfo: Shape<TokenInfo> = {
  retired: false,
  id: true,
  label: true,
  createdAt: true,
  expiresAt: false,
  scopes: false,
  clientId: false,
  clientName: false,
  appAuthority: false,
  appAuthorityFromFile: false,
  appLocal: false,
  appPackages: false,
  appSoftwareVersion: false,
  appCheckedAt: false,
  appAddons: false,
  identity: false,
}

const appAddon: Shape<AppAddon> = {
  package: true,
  version: true,
  addonFor: true,
}

/** An app's authorization request as the approval page reads it, and the
 * approval and answer it trades (decision 0194), with the setup it runs
 * (docs/plans/app-setup.md). */
const authorizationRequest: Shape<AuthorizationRequest> = {
  id: true,
  app: true,
  redirect: true,
  repository: true,
  scopes: true,
  lifetimeDays: true,
  defaultLifetimeDays: true,
  expiresAt: true,
  details: true,
  setup: true,
  planHash: false,
  previous: false,
  state: true,
  failure: false,
  next: false,
}

const authorizationApp: Shape<AuthorizationApp> = {
  clientId: true,
  host: true,
  local: true,
  name: false,
  uri: false,
  uriHostDiffers: false,
  appearsAs: true,
  verified: true,
  publisher: true,
}

const authorizationPublisher: Shape<AuthorizationPublisher> = {
  authority: false,
  name: false,
  appName: false,
  unconfirmed: false,
  record: false,
  documentUrl: false,
  listed: true,
  fromFile: false,
}

const authorizationRedirectTo: Shape<AuthorizationRedirectTo> = {
  uri: true,
  host: true,
  loopback: false,
}

const authorizationScope: Shape<AuthorizationScope> = {
  scope: true,
  requested: true,
  verb: true,
  write: true,
  pattern: true,
  glob: true,
  kinds: true,
  agents: true,
  functions: true,
  allowed: false,
}

const authorizationCallFunction: Shape<AuthorizationCallFunction> = {
  identity: true,
  description: false,
  reads: true,
  writes: true,
  covered: true,
  uncovered: false,
}

const authorizationKind: Shape<AuthorizationKind> = {
  identity: true,
  label: false,
  purpose: false,
  added: false,
}

const authorizationDetail: Shape<AuthorizationDetail> = {
  type: true,
  package: false,
  version: false,
  kind: false,
  id: false,
  properties: false,
  trigger: false,
  url: false,
}

const authorizationSetup: Shape<AuthorizationSetup> = {
  type: true,
  action: true,
  blocked: false,
  progress: false,
  package: false,
  account: false,
  webhook: false,
}

const authorizationBlock: Shape<AuthorizationBlock> = {
  reason: true,
  problem: true,
  fix: false,
}

const authorizationPackage: Shape<AuthorizationPackage> = {
  oauthHosts: false,
  providerInputs: false,
  addonFor: false,
  settings: false,
  id: true,
  description: false,
  keptBy: false,
  required: true,
  held: false,
  shipped: false,
  requires: false,
  kinds: false,
  functions: false,
  triggers: false,
  reach: false,
  publisher: false,
}

const authorizationPackagePublisher: Shape<AuthorizationPackagePublisher> = {
  authority: true,
  name: false,
  documentUrl: true,
  fromFile: false,
  digest: false,
}

const authorizationPackageStep: Shape<AuthorizationPackageStep> = {
  oauthHosts: false,
  id: true,
  description: false,
  action: true,
  blocked: false,
  held: false,
  shipped: false,
  kinds: false,
  functions: false,
  triggers: false,
  publisher: false,
  reach: false,
}

const authorizationFunction: Shape<AuthorizationFunction> = {
  id: true,
  reads: false,
  writes: false,
  network: false,
}

const authorizationReach: Shape<AuthorizationReach> = {
  pattern: true,
  write: false,
  by: true,
  covered: false,
  scope: true,
}

const authorizationReachBy: Shape<AuthorizationReachBy> = {
  type: true,
  id: true,
}

const authorizationTrigger: Shape<AuthorizationTrigger> = {
  id: true,
  callable: false,
  webhook: false,
  enabled: true,
}

const authorizationAccount: Shape<AuthorizationAccount> = {
  kind: true,
  label: false,
  id: true,
  properties: false,
}

const authorizationWebhookPlan: Shape<AuthorizationWebhookPlan> = {
  trigger: true,
  package: false,
  enabled: false,
  keyed: false,
  allowed: false,
}

const authorizationPrevious: Shape<AuthorizationPrevious> = {
  approvedAt: true,
  expiresAt: false,
  scopes: true,
}

const authorizationFailure: Shape<AuthorizationFailure> = {
  row: false,
  message: true,
}

const authorizationCheck: Shape<AuthorizationCheck> = {
  scope: true,
  details: true,
  softwareVersion: false,
}

const authorizationCheckResult: Shape<AuthorizationCheckResult> = {
  scopes: true,
  setup: true,
  expiresAt: false,
}

const authorizationCheckScopes: Shape<AuthorizationCheckScopes> = {
  missing: true,
}

const authorizationApproval: Shape<AuthorizationApproval> = {
  providerChoices: false,
  addons: false,
  settings: false,
  scopes: true,
  lifetimeDays: true,
  planHash: false,
}

const authorizationAnswer: Shape<AuthorizationAnswer> = {
  redirect: false,
  state: false,
  setup: false,
}

const mintedToken: Shape<MintedToken> = {
  token: true,
  secret: true,
  repository: false,
  identity: false,
}

/** A person (`GET /identity`) and a repository they may open. */
const identityInfo: Shape<IdentityInfo> = {
  id: true,
  name: true,
  secondFactor: true,
  repositories: true,
}

const repositoryChoice: Shape<RepositoryChoice> = {
  id: true,
  name: true,
}

const totpEnrollment: Shape<TOTPEnrollment> = {
  totpSecret: true,
  otpauthUri: true,
}

/** Registration (auth.ts): the request the door decodes and its answer, which
 * is the mint with the repository it created. */
const registerInput: Shape<RegisterInput> = {
  inviteCode: true,
  identity: true,
  repository: false,
  password: true,
  totpSecret: true,
  totpCode: true,
  skipTotp: false,
  label: false,
}

/** Login (auth.ts): the person, the repository they pick, the two factors
 * and the token label. */
const loginRequest: Shape<LoginRequest> = {
  identity: true,
  repository: false,
  password: true,
  totpCode: true,
  label: false,
}

const sessionCredential: Shape<SessionCredential> = {
  identity: true,
  signedOut: false,
}

const registerResult: Shape<RegisterResult> = mintedToken

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
  mentions: false,
}

const agentMention: Shape<AgentMention> = {
  kind: true,
  id: true,
  text: true,
  missing: false,
  unread: false,
}

const chatRequest: Shape<ChatRequest> = {
  thread: false,
  message: true,
  timezone: false,
  context: false,
}

const agentEvent: Shape<AgentEvent> = {
  kind: true,
  thread: false,
  text: false,
  id: false,
  tool: false,
  callable: false,
  args: false,
  ok: false,
  output: false,
  tokensBefore: false,
  covered: false,
  result: false,
  error: false,
  code: false,
  turn: false,
  status: false,
  job: false,
}

/** The catalog entry and the shapes nested in it. */
const catalogBundle: Shape<CatalogBundle> = {
  publisher: false,
  oauthHosts: false,
  id: true,
  name: true,
  authority: true,
  package: true,
  description: true,
  version: true,
  keptBy: true,
  provider: false,
  brings: false,
  addonFor: false,
  inputs: false,
  requires: false,
  requiresAtLeast: false,
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
  kindPurposes: false,
  traits: true,
  traitDescriptions: false,
  functions: true,
  functionDescriptions: false,
  agents: true,
  agentDescriptions: false,
  mappings: true,
  triggers: true,
  triggerCallables: false,
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
  features: false,
  accounts: true,
  functions: true,
  kinds: true,
  liveRecords: true,
  quarantined: false,
  quarantineReason: false,
  version: false,
  origin: false,
  originVersion: false,
  modified: false,
  shippedVersion: false,
  provider: false,
  addonFor: false,
}

const bundleFeature: Shape<BundleFeature> = {
  toggle: true,
  scopes: true,
  access: true,
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

/** The conversion plan (decision 0067): flattened into the upgrade preview
 * and the apply preview, so both carry its keys beside their own. */
const conversionPlan: Shape<ConversionPlan> = {
  steps: false,
  work: true,
  lossy: true,
  planHash: false,
  changelogSeq: false,
}

const conversionStep: Shape<ConversionStep> = {
  step: true,
  kind: true,
  property: true,
  from: false,
  to: false,
  records: true,
  lossy: false,
}

const conversionConfirm: Shape<ConversionConfirm> = {
  planHash: true,
  changelogSeq: true,
}

/** The upgrade preview, on a catalog entry and on the shipped-upgrade read. */

const bundleUpgradeHolder: Shape<BundleUpgradeHolder> = {
  clientName: true,
  clientId: true,
  softwareVersion: false,
  checkedAt: false,
  floor: true,
}

const bundleUpgrade: Shape<BundleUpgrade> = {
  holders: false,
  reviewRequired: false,
  codeChanges: false,
  retentionShortens: false,
  candidateVersion: false,
  candidateDigest: false,
  ...conversionPlan,
  available: true,
  from: false,
  to: false,
  changes: false,
  blockers: false,
  discardsEdits: false,
}

const vocabularyPlan: Shape<VocabularyPlan> = {
  ...conversionPlan,
  blockers: false,
  discardsEdits: false,
}

const bundleCodeChange: Shape<BundleCodeChange> = {
  kind: true,
  id: true,
  diff: true,
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
const triggerStatus: Shape<TriggerStatus> = {
  id: true,
  kind: true,
  callable: true,
  enabled: true,
  cursor: false,
  head: true,
  lag: false,
  lastFire: false,
  webhookPath: false,
  parked: true,
  pending: true,
  inFlight: true,
  lastPassAt: false,
  lastDeliveredAt: false,
  lastParkedError: false,
  lastParkedAt: false,
  held: false,
  error: false,
  health: true,
  failingSince: false,
  lastOkAt: false,
}
const triggerFailure: Shape<TriggerFailure> = {
  id: true,
  trigger: true,
  seq: false,
  fireId: false,
  recordId: false,
  attempts: true,
  lastError: true,
  parkedAt: true,
  stream: false,
  running: false,
  pending: false,
}
const syncStatus: Shape<SyncStatus> = {
  kind: true,
  id: true,
  title: false,
  state: true,
  message: false,
  paused: true,
  tokenStatus: false,
  tokenError: false,
  tokenErrorAt: false,
  lastSyncedAt: false,
  lastSyncStartedAt: false,
  lastSyncDurationMs: false,
  requestedAt: false,
  requestedAck: false,
  progress: false,
  error: false,
  errorAt: false,
  streams: false,
  parked: true,
  lastParkedError: false,
  lastParkedAt: false,
  health: true,
  failingSince: false,
  lastOkAt: false,
  triggers: true,
}
const syncProgress: Shape<SyncProgress> = {
  phase: false,
  done: true,
  total: true,
  pending: true,
}
const syncStream: Shape<SyncStream> = {
  cursor: false,
  lastAt: false,
  pending: true,
  state: false,
  message: false,
  requestedAck: false,
}
const triggerReplayed: Shape<TriggerReplayed> = { from: true }
const triggerRan: Shape<TriggerRan> = { ran: true }
const functionCalled: Shape<FunctionCalled> = { output: true, effects: true }
const webhookAccepted: Shape<WebhookAccepted> = { fire: true }

const unitTable: Shape<UnitTable> = {
  units: true,
  conversions: true,
  revision: true,
  unseeded: true,
}

const repositoryUnit: Shape<RepositoryUnit> = {
  id: true,
  name: true,
  symbol: true,
  code: false,
  aliases: true,
  scale: false,
  turn: false,
  root: true,
  factor: true,
  offset: false,
}

const repositoryConversion: Shape<RepositoryConversion> = {
  id: true,
  from: true,
  to: true,
  factor: false,
  offset: false,
  expr: false,
  inverse: false,
}

const unseededRecord: Shape<UnseededRecord> = {
  kind: true,
  id: true,
  reason: true,
}

/** An applet's runtime (docs/plans/applets.md): what the renderer sends to
 * render a screen and run an action, the frame it gets back, and the A2UI
 * v1.0 messages the frame carries. */
const appletState: Shape<AppletState> = {
  screen: true,
  params: false,
  ui: false,
}

const appletShown: Shape<AppletShown> = {
  screen: true,
  version: true,
  params: false,
}

const appletRenderInput: Shape<AppletRenderInput> = {
  state: false,
  shown: false,
}

const appletAction: Shape<AppletAction> = {
  name: true,
  context: false,
  sourceComponentId: false,
  userMessage: false,
}

const appletActInput: Shape<AppletActInput> = {
  state: true,
  shown: false,
  action: true,
  confirmed: false,
}

const appletFrame: Shape<AppletFrame> = {
  applet: true,
  state: true,
  shown: true,
  messages: true,
  notice: false,
  confirm: false,
  confirmLabel: false,
  confirmDanger: false,
}

const appletHeader: Shape<AppletHeader> = {
  id: true,
  name: true,
  runtime: true,
  description: false,
  icon: false,
  version: true,
  title: true,
  start: true,
  watch: false,
  refreshOn: false,
  destructive: false,
}

const a2uiMessage: Shape<A2UIMessage> = {
  version: true,
  createSurface: false,
  updateComponents: false,
  updateDataModel: false,
  deleteSurface: false,
}

const a2uiCreateSurface: Shape<A2UICreateSurface> = {
  surfaceId: true,
  catalogId: true,
  components: true,
  dataModel: true,
}

const a2uiUpdateComponents: Shape<A2UIUpdateComponents> = {
  surfaceId: true,
  components: true,
}

const a2uiUpdateDataModel: Shape<A2UIUpdateDataModel> = {
  surfaceId: true,
  path: true,
  value: true,
}

const a2uiDeleteSurface: Shape<A2UIDeleteSurface> = {
  surfaceId: true,
}

const unitConversion: Shape<UnitConversion> = {
  value: true,
  unit: true,
  exact: true,
}

const mirrors: Record<string, Record<string, boolean>> = {
  ErrorEnvelope: errorEnvelope,
  ErrorPayload: errorPayload,
  ProblemDetail: problemDetail,
  SubstrateRecord: substrateRecord,
  LinkedRecord: linkedRecord,
  Erasure: erasure,
  ErasureWait: erasureWait,
  ErasureOwned: erasureOwned,
  Referencing: referencing,
  ReferenceSite: referenceSite,
  PropertyMark: propertyMark,
  PropertyAlternative: propertyAlternative,
  RecordLink: recordLink,
  MarkInput: markInput,
  PutInput: putInput,
  RecordPatch: recordPatch,
  Cond: cond,
  RecordFilter: recordFilter,
  KindInfo: kindInfo,
  KindLabel: kindLabel,
  KindRetention: kindRetention,
  RetentionChange: retentionChange,
  BlobInfo: blobInfo,
  DomainProof: domainProof,
  DomainCheck: domainCheck,
  SiteInfo: siteInfo,
  OperatorDefaults: operatorDefaults,
  OperatorInfo: operatorInfo,
  OperatorAllowance: operatorAllowance,
  OperatorSpend: operatorSpend,
  OperatorDefault: operatorDefault,
  Change: change,
  AffectedRecord: affectedRecord,
  PropertyChange: propertyChange,
  ChangeTrigger: changeTrigger,
  ChangeRow: changeRow,
  ChangePage: changePage,
  ChangeRun: changeRun,
  ChangeRunPage: changeRunPage,
  Page: page,
  Aggregate: aggregate,
  AggregateMetric: aggregateMetric,
  AggregateMetricRead: aggregateMetricRead,
  AggregateProperty: aggregateProperty,
  AggregateRange: aggregateRange,
  AggregateBucket: aggregateBucket,
  AggregateRecords: aggregateRecords,
  AggregateSources: aggregateSources,
  AggregateRecordSources: aggregateRecordSources,
  AggregateRecordOffsets: aggregateRecordOffsets,
  AggregateRolling: aggregateRolling,
  AggregateBudget: aggregateBudget,
  Downsample: downsample,
  AggregateResult: aggregateResult,
  AggregateHead: aggregateHead,
  AggregateRow: aggregateRow,
  AggregateGroup: aggregateGroup,
  AggregateRecord: aggregateRecord,
  AggregateValue: aggregateValue,
  RankedPage: rankedPage,
  Scores: scores,
  OperationalList: operationalList,
  OccurrenceProblem: occurrenceProblem,
  TokenInfo: tokenInfo,
  AuthorizationRequest: authorizationRequest,
  AuthorizationApp: authorizationApp,
  AuthorizationPublisher: authorizationPublisher,
  AuthorizationPackagePublisher: authorizationPackagePublisher,
  AppAddon: appAddon,
  AuthorizationSetting: {
    id: true,
    description: false,
    secret: true,
    required: true,
  } satisfies Shape<AuthorizationSetting>,
  AuthorizationSettingValue: {
    id: true,
    secret: true,
    value: true,
  } satisfies Shape<AuthorizationSettingValue>,
  AddonInstallResult: {
    package: true,
    tokenId: true,
    state: true,
    problem: false,
  } satisfies Shape<AddonInstallResult>,
  AuthorizationRedirectTo: authorizationRedirectTo,
  AuthorizationScope: authorizationScope,
  AuthorizationCallFunction: authorizationCallFunction,
  AuthorizationKind: authorizationKind,
  AuthorizationDetail: authorizationDetail,
  AuthorizationSetup: authorizationSetup,
  AuthorizationBlock: authorizationBlock,
  AuthorizationProviderInput: {
    package: true,
    input: true,
    agents: true,
    options: true,
    selected: false,
  } satisfies Shape<AuthorizationProviderInput>,
  AuthorizationProviderAgent: {
    agent: true,
    models: true,
    spendCentsPerDay: true,
  } satisfies Shape<AuthorizationProviderAgent>,
  AuthorizationProviderOption: {
    id: true,
    label: true,
    models: true,
  } satisfies Shape<AuthorizationProviderOption>,
  AuthorizationProviderChoice: {
    package: true,
    input: true,
    provider: true,
  } satisfies Shape<AuthorizationProviderChoice>,
  AuthorizationPackage: authorizationPackage,
  AuthorizationPackageStep: authorizationPackageStep,
  AuthorizationFunction: authorizationFunction,
  AuthorizationReach: authorizationReach,
  AuthorizationReachBy: authorizationReachBy,
  AuthorizationTrigger: authorizationTrigger,
  AuthorizationAccount: authorizationAccount,
  AuthorizationWebhookPlan: authorizationWebhookPlan,
  AuthorizationPrevious: authorizationPrevious,
  AuthorizationFailure: authorizationFailure,
  AuthorizationCheck: authorizationCheck,
  AuthorizationCheckResult: authorizationCheckResult,
  AuthorizationCheckScopes: authorizationCheckScopes,
  AuthorizationApproval: authorizationApproval,
  AuthorizationAnswer: authorizationAnswer,
  MintedToken: mintedToken,
  TOTPEnrollment: totpEnrollment,
  RegisterInput: registerInput,
  RegisterResult: registerResult,
  LoginRequest: loginRequest,
  SessionCredential: sessionCredential,
  IdentityInfo: identityInfo,
  RepositoryChoice: repositoryChoice,
  AgentResult: agentResult,
  AgentMention: agentMention,
  AgentEvent: agentEvent,
  ChatRequest: chatRequest,
  CatalogBundle: catalogBundle,
  CatalogItem: catalogItem,
  CatalogInput: catalogInput,
  BundleClosure: bundleClosure,
  ShippedRecord: shippedRecord,
  SuggestedMapping: suggestedMapping,
  BundleStatus: bundleStatus,
  BundleFeature: bundleFeature,
  InputStatus: inputStatus,
  SetupItem: setupItem,
  BundleUpgrade: bundleUpgrade,
  BundleUpgradeHolder: bundleUpgradeHolder,
  BundleUpgradeChange: bundleUpgradeChange,
  BundleCodeChange: bundleCodeChange,
  ConversionPlan: conversionPlan,
  ConversionStep: conversionStep,
  ConversionConfirm: conversionConfirm,
  VocabularyPlan: vocabularyPlan,
  ShippedUpgrade: shippedUpgrade,
  BundleUninstalled: bundleUninstalled,
  BundlePurged: bundlePurged,
  OAuthStarted: oauthStarted,
  SyncStatus: syncStatus,
  SyncProgress: syncProgress,
  SyncStream: syncStream,
  TriggerStatus: triggerStatus,
  TriggerFailure: triggerFailure,
  TriggerReplayed: triggerReplayed,
  TriggerRan: triggerRan,
  FunctionCalled: functionCalled,
  WebhookAccepted: webhookAccepted,
  UnitTable: unitTable,
  UnitConversion: unitConversion,
  RepositoryUnit: repositoryUnit,
  RepositoryConversion: repositoryConversion,
  UnseededRecord: unseededRecord,
  AppletState: appletState,
  AppletShown: appletShown,
  AppletRenderInput: appletRenderInput,
  AppletAction: appletAction,
  AppletActInput: appletActInput,
  AppletFrame: appletFrame,
  AppletHeader: appletHeader,
  A2UIMessage: a2uiMessage,
  A2UICreateSurface: a2uiCreateSurface,
  A2UIUpdateComponents: a2uiUpdateComponents,
  A2UIUpdateDataModel: a2uiUpdateDataModel,
  A2UIDeleteSurface: a2uiDeleteSurface,
}

/** The interfaces the modules in this directory export that mirror no Go
 * struct in `internal/substrate`, each with the reason. Everything else they
 * export must be in the golden. */
const notOnTheWire: Record<string, string> = {
  // actors.ts
  ActorInfo:
    "the console's reading of one actor record, built from a record's properties; the record itself is the SubstrateRecord the golden pins",
  // types.ts
  BundleInstallRequest:
    "the body of the catalog's add route, declared in internal/api (catalogAddRequest) and out of the golden's reach",
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
  ChangeStamp: "a view the console folds from llm/message records",
  ContextRecord:
    "one entry of an llm/message row's contextRecords, folded from the record",
  SubagentWrites: "a view the console folds from llm/message records",
  ToolCallView: "a view the console folds from llm/message records",
  TurnView: "a view the console folds from llm/message records",
  CompactionView: "a view the console folds from llm/message records",
  DecisionNotice: "a view the console folds from llm/message records",
  InteractionNotice: "a view the console folds from llm/message records",
  DeliveryNotice: "a view the console folds from llm/message records",
  JobNotice: "a view the console folds from llm/message records",
  ApprovalNotice: "a view the console folds from llm/message records",
  ApprovalView: "a view the console folds from an llm/approval record",
  ApprovalArgument: "one argument of an llm/approval, folded for the card",
  LiveOverlay: "the chat stream's events folded client-side, never serialized",
  // actors.ts
  // bundles.ts
  TraitRecords: "a bounded read's result the console derives from a Page",
  // changes.ts
  ChangeFeedFilter:
    "the feed's query parameters, URL-encoded, never a JSON body",
  ChangesFeedOpts: "query options for the infinite feed, client-side",
  HistoryPosition: "a seek position the console holds, client-side",
  RunSource:
    "a run summary plus the page filter its rows are read under, client-side",
  WatchError:
    "a loose reading of ErrorPayload off a stream frame (`code` as any string); the shape is pinned as ErrorPayload",
  WatchLine:
    "the union of the three ndjson frame shapes (a ChangeRow, `{bookmark, generation}`, `{error}`), keyed by which field is present",
  WatchHandle: "a client handle over the stream, never serialized",
  // records.ts
  ListParams: "the list read's query parameters, never a JSON body",
  RecordCount: "a bounded walk's tally the console computes",
  ReferencingRow:
    "one (record, site) pair the console folds from a referencing page and its matches",
  ReferencingGroup: "a client-side fold of ReferencingRow rows",
  // observations.ts
  AggregateRead:
    "the `filter` and `aggregate` query parameters of one read, each JSON on its own, never one body",
  // owner-contact.ts
  OwnerContact:
    "two records the console reads one after the other and pairs, client-side",
  // repository.ts
  RepositoryInfo:
    "one property read off the repository record; not the operator's substrate.RepositoryInfo",
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
