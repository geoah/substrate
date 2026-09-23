/** The Connections page's pure half: what the core `sync` trait says about a
 * record, how a provider's accounts fold into one row, which of a kind's
 * triggers are the on-request ones, and the health a row's dot shows. No
 * fetching here; the components read these off the records and statuses
 * `lib/api/sync.ts` returns. */

import type {
  BundleStatus,
  KindInfo,
  SubstrateRecord,
  SyncProgress,
  SyncState,
  SyncStatus,
  SyncStream,
  TriggerStatus,
} from "@/lib/api/types"
import { splitKind } from "@/lib/api/http"
import { kindPackage } from "@/lib/definition"

/** The trait's five words, in the order a legend lists them. */
export const SYNC_STATES: readonly SyncState[] = [
  "never",
  "running",
  "ok",
  "erroring",
  "throttled",
]

/** Narrow the wire's string to the trait's set; anything else (a body that
 * wrote its own word) renders as the neutral `never`-styled chip with the
 * raw word shown. */
export function syncStateOf(raw: unknown): SyncState {
  return typeof raw === "string" &&
    (SYNC_STATES as readonly string[]).includes(raw)
    ? (raw as SyncState)
    : "never"
}

/** The trait's properties as one record carries them, read leniently: a row
 * written before its kind bound the trait, or by a body that mis-shaped
 * `syncProgress`, still reads with what it has. */
export interface SyncFields {
  state: SyncState
  /** The wire's raw word, shown when it is not one of the five. */
  rawState?: string
  message?: string
  paused: boolean
  lastSyncedAt?: string
  lastSyncStartedAt?: string
  lastSyncDurationMs?: number
  requestedAt?: string
  requestedAck?: string
  progress?: SyncProgress
  error?: string
  errorAt?: string
  streams: Record<string, SyncStream>
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v ? v : undefined
}

function num(v: unknown): number | undefined {
  if (typeof v === "number" && Number.isFinite(v)) return v
  if (typeof v === "string" && v.trim() && !Number.isNaN(Number(v)))
    return Number(v)
  return undefined
}

function progressOf(v: unknown): SyncProgress | undefined {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return undefined
  const o = v as Record<string, unknown>
  return {
    phase: str(o.phase),
    done: num(o.done) ?? 0,
    total: num(o.total) ?? 0,
    pending: num(o.pending) ?? 0,
  }
}

function streamsOf(v: unknown): Record<string, SyncStream> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return {}
  const out: Record<string, SyncStream> = {}
  for (const [name, raw] of Object.entries(v as Record<string, unknown>)) {
    const o =
      typeof raw === "object" && raw !== null
        ? (raw as Record<string, unknown>)
        : {}
    out[name] = {
      cursor: o.cursor,
      lastAt: str(o.lastAt),
      pending: num(o.pending) ?? 0,
      state: str(o.state),
      message: str(o.message),
      requestedAck: str(o.requestedAck),
    }
  }
  return out
}

/** The trait off a RECORD's properties. */
export function syncFieldsOf(properties: Record<string, unknown>): SyncFields {
  const raw = str(properties.syncState)
  const state = syncStateOf(raw)
  return {
    state,
    rawState: raw && raw !== state ? raw : undefined,
    message: str(properties.syncMessage),
    paused: properties.syncPaused === true,
    lastSyncedAt: str(properties.lastSyncedAt),
    lastSyncStartedAt: str(properties.lastSyncStartedAt),
    lastSyncDurationMs: num(properties.lastSyncDurationMs),
    requestedAt: str(properties.syncRequestedAt),
    requestedAck: str(properties.syncRequestedAck),
    progress: progressOf(properties.syncProgress),
    error: str(properties.syncError),
    errorAt: str(properties.syncErrorAt),
    streams: streamsOf(properties.syncStreams),
  }
}

/** The same fields off a `sync/status` ROW, so one renderer serves both. */
export function syncFieldsOfStatus(s: SyncStatus): SyncFields {
  const state = syncStateOf(s.state)
  return {
    state,
    rawState: s.state !== state ? s.state : undefined,
    message: s.message,
    paused: s.paused,
    lastSyncedAt: s.lastSyncedAt,
    lastSyncStartedAt: s.lastSyncStartedAt,
    lastSyncDurationMs: s.lastSyncDurationMs,
    requestedAt: s.requestedAt,
    requestedAck: s.requestedAck,
    progress: s.progress,
    error: s.error,
    errorAt: s.errorAt,
    streams: s.streams ?? {},
  }
}

/** Whether the owner's last request has been served: no request is served
 * (nothing is owed), and an ack at or after the request is too. */
export function requestServed(
  f: Pick<SyncFields, "requestedAt" | "requestedAck">
): boolean {
  if (!f.requestedAt) return true
  if (!f.requestedAck) return false
  return Date.parse(f.requestedAck) >= Date.parse(f.requestedAt)
}

// ── health ───────────────────────────────────────────────────────────────────

export type Health = "healthy" | "attention" | "broken" | "idle"

/** The one dot a Connection row wears, from the token and the sync together:
 * a dead grant or an erroring sync is broken; a pending grant, a paused or
 * throttled sync is attention; a connected account whose sync is fine is
 * healthy; an account that never synced is idle. The legacy free-text
 * `syncStatus` that begins with `erroring` counts as broken too, so a bundle
 * that has not bound the trait yet still reads honestly. */
export function healthOf(
  tokenStatus: string | undefined,
  sync: SyncFields,
  legacySyncStatus?: string
): Health {
  if (tokenStatus === "erroring" || sync.state === "erroring") return "broken"
  if (legacySyncStatus?.startsWith("erroring")) return "broken"
  if (tokenStatus && tokenStatus !== "connected") return "attention"
  if (sync.paused || sync.state === "throttled") return "attention"
  if (sync.state === "never" && !legacySyncStatus) {
    return tokenStatus === "connected" ? "idle" : "attention"
  }
  return "healthy"
}

// ── the account rows ─────────────────────────────────────────────────────────

/** The read surface of one account record: what the table and the detail
 * render, folded once off the record and the kind registry. */
export interface AccountView {
  record: SubstrateRecord
  kind: KindInfo | undefined
  /** The bundle id, `<authority>/<package>`: the provider the account is of. */
  provider: string
  /** `email`, else `displayName`, else the rendered title, else the id. */
  label: string
  tokenStatus?: string
  grantedScopes: string[]
  syncFrequency?: string
  backfillDepth?: string
  /** The legacy free-text rollup, shown in full while a bundle still writes
   * it. */
  legacySyncStatus?: string
  /** The kind binds the core `sync` trait, so the trait's actions apply. */
  syncable: boolean
  sync: SyncFields
  health: Health
}

/** A kind carries a trait when its reconciled declaration lists it — by bare
 * name (the shipped spelling) or by full identity. */
export function kindHasTrait(
  kind: KindInfo | undefined,
  trait: string
): boolean {
  const traits = (kind?.definition as { traits?: unknown } | undefined)?.traits
  if (!Array.isArray(traits)) return false
  const bare = trait.slice(trait.lastIndexOf("/") + 1)
  return traits.some((t) => t === trait || t === bare)
}

export function accountViewOf(
  record: SubstrateRecord,
  kinds: KindInfo[]
): AccountView {
  const kind = kinds.find((k) => k.identity === record.kind)
  const p = record.properties ?? {}
  const { authority, pkg } = splitKind(record.kind)
  const sync = syncFieldsOf(p)
  const tokenStatus = str(p.tokenStatus)
  const legacy = str(p.syncStatus)
  const scopes = Array.isArray(p.grantedScopes)
    ? (p.grantedScopes as unknown[]).filter(
        (s): s is string => typeof s === "string"
      )
    : []
  return {
    record,
    kind,
    provider: `${authority}/${pkg}`,
    label:
      str(p.email) ??
      str(p.displayName) ??
      str(p.title) ??
      str(p.login) ??
      str(p.userId) ??
      record.id,
    tokenStatus,
    grantedScopes: scopes,
    syncFrequency: str(p.syncFrequency),
    backfillDepth: str(p.backfillDepth),
    legacySyncStatus: legacy,
    syncable: kindHasTrait(kind, SYNC_TRAIT_IDENTITY),
    sync,
    health: healthOf(tokenStatus, sync, legacy),
  }
}

/** The trait identity, spelled here so this module stays free of the API
 * layer's constants. Must equal `lib/api/sync.ts` `SYNC_TRAIT`. */
export const SYNC_TRAIT_IDENTITY = "substrate.reamde.dev/core/sync"

/** The trait's two OWNER hands, by name: `syncRequestedAt` is Sync now and
 * `syncPaused` is Pause, and the sync panel's buttons are their controls. An
 * account form on a kind binding the trait leaves them out for that reason.
 * This is the trait's contract spelled beside its identity (the panel patches
 * the same two names), not a list of a provider's property names. */
export const SYNC_OWNER_HANDS: readonly string[] = [
  "syncRequestedAt",
  "syncPaused",
]

/** The `oauth2` trait's two contracted properties, in the order a person
 * copies them off the provider's client page: what a credentials form puts
 * first, ahead of whatever else the bundle's client kind declares. */
export const OAUTH2_CLIENT_PROPERTIES: readonly string[] = [
  "clientId",
  "clientSecret",
]

// ── the providers list ───────────────────────────────────────────────────────

export interface ProviderView {
  /** The bundle id, `<authority>/<package>`. */
  id: string
  name: string
  status: BundleStatus | undefined
  /** The account kind, when the closure declares one. */
  accountKind: KindInfo | undefined
  /** The client kind wearing `oauth2`, or the token-config kind the bundle's
   * input names, when there is one. */
  configKind: KindInfo | undefined
  /** The one config record the input resolved to, by id, when it did. */
  configRecord?: string
  /** Credentials present: no `oauth-client` or input setup item stands. */
  configured: boolean
  /** The client input wears `oauth2`: connecting is the host's consent flow,
   * and an account is not connected until the owner approves it there. A
   * token provider (a pasted key on its config) has no such step. */
  oauth: boolean
  setupSteps: number
  accounts: AccountView[]
  /** Accounts by `tokenStatus`; an account carrying none counts under
   * `unset`. */
  byTokenStatus: Record<string, number>
}

/** Fold the installed bundles and the account records into one row per
 * provider: every bundle whose closure declares an `accountconfig` kind, plus
 * every bundle the catalog calls a provider, whether or not it has accounts
 * yet. Bundles that are neither (a vocabulary sample) are not connections. */
export function providerViews(
  statuses: BundleStatus[],
  providerTierIds: Set<string>,
  kinds: KindInfo[],
  accounts: AccountView[]
): ProviderView[] {
  const accountKinds = kinds.filter((k) => kindHasTrait(k, "accountconfig"))
  const ids = new Set<string>()
  for (const k of accountKinds) ids.add(kindPackage(k))
  for (const id of providerTierIds) ids.add(id)
  for (const a of accounts) ids.add(a.provider)
  const out: ProviderView[] = []
  for (const id of ids) {
    const status = statuses.find((b) => b.id === id)
    if (!status) continue
    const accountKind = accountKinds.find((k) => kindPackage(k) === id)
    const clientInput = status.inputs?.find((i) => {
      const k = kinds.find((x) => x.identity === i.kind)
      return Boolean(k && kindHasTrait(k, "oauth2"))
    })
    const configInput = clientInput ?? status.inputs?.[0]
    const configKind = configInput
      ? kinds.find((k) => k.identity === configInput.kind)
      : undefined
    const setup = status.setup ?? []
    const configured =
      !setup.some((s) => s.code === "oauth-client") &&
      !setup.some(
        (s) =>
          configInput &&
          s.input === configInput.name &&
          ["missing", "ambiguous", "dangling"].includes(s.code)
      )
    const mine = accounts.filter((a) => a.provider === id)
    const byTokenStatus: Record<string, number> = {}
    for (const a of mine) {
      const key = a.tokenStatus ?? "not connected"
      byTokenStatus[key] = (byTokenStatus[key] ?? 0) + 1
    }
    out.push({
      id,
      name: status.name || id.slice(id.indexOf("/") + 1),
      status,
      accountKind,
      configKind,
      configRecord: configInput?.record || undefined,
      configured,
      oauth: Boolean(clientInput),
      setupSteps: setup.length,
      accounts: mine,
      byTokenStatus,
    })
  }
  return out.sort((a, b) => a.name.localeCompare(b.name))
}

// ── the next step ────────────────────────────────────────────────────────────

/** Where a provider stands on the way to a syncing account, so the card can
 * say what to do next in one sentence: enable the bundle, set the
 * credentials, add an account, connect the one that is not, or nothing. */
export type ProviderStep =
  "install" | "credentials" | "account" | "connect" | "ready"

export interface ProviderNextStep {
  step: ProviderStep
  /** The account the `connect` step is about. */
  account?: AccountView
}

export function providerNextStep(p: ProviderView): ProviderNextStep {
  if (!p.status?.installed || !p.status.enabled) return { step: "install" }
  if (p.configKind && !p.configured) return { step: "credentials" }
  if (p.accounts.length === 0) return { step: "account" }
  if (p.oauth) {
    const pending = p.accounts.find((a) => a.tokenStatus !== "connected")
    if (pending) return { step: "connect", account: pending }
  }
  return { step: "ready" }
}

// ── triggers on a kind ───────────────────────────────────────────────────────

/** The record-source glob grammar, mirrored from the server: `*`,
 * `<authority>/*`, `<authority>/<package>/*`, or the exact reference. */
export function kindGlobMatches(pattern: string, kind: string): boolean {
  if (pattern === "*") return true
  if (pattern.endsWith("/*")) return kind.startsWith(pattern.slice(0, -1))
  return pattern === kind
}

/** One trigger record's source, as the Connections page reads it. */
export interface TriggerSource {
  id: string
  kinds: string[]
  ops: string[]
  when: string
  enabled: boolean
}

export function triggerSourceOf(
  record: SubstrateRecord
): TriggerSource | undefined {
  const p = record.properties ?? {}
  const source = p.source as Record<string, unknown> | undefined
  const rec = source?.record as Record<string, unknown> | undefined
  if (!rec) return undefined
  const kinds = Array.isArray(rec.kinds)
    ? (rec.kinds as unknown[]).filter((k): k is string => typeof k === "string")
    : []
  const ops = Array.isArray(rec.ops)
    ? (rec.ops as unknown[]).filter((o): o is string => typeof o === "string")
    : []
  return {
    id: record.id,
    kinds,
    ops,
    when: typeof rec.when === "string" ? rec.when : "",
    enabled: p.enabled !== false,
  }
}

/** The record triggers whose source names this kind. */
export function triggersOnKind(
  triggers: SubstrateRecord[],
  kind: string
): TriggerSource[] {
  const out: TriggerSource[] = []
  for (const t of triggers) {
    const src = triggerSourceOf(t)
    if (src && src.kinds.some((pat) => kindGlobMatches(pat, kind)))
      out.push(src)
  }
  return out
}

/** The on-request triggers: the record triggers on the kind whose guard
 * reads the trait's request property, or whose id says so the way the
 * shipped bundles name them. These are what Sync now wakes after the stamp. */
export function requestTriggers(sources: TriggerSource[]): TriggerSource[] {
  return sources.filter(
    (s) =>
      s.enabled &&
      (s.when.includes("syncRequestedAt") || /-on-(request|demand)$/.test(s.id))
  )
}

/** The trigger statuses for a kind, off the whole list and the trigger
 * records: the status carries no source, so the records say which apply. */
export function statusesOnKind(
  statuses: TriggerStatus[],
  sources: TriggerSource[]
): TriggerStatus[] {
  const ids = new Set(sources.map((s) => s.id))
  return statuses.filter((s) => ids.has(s.id))
}

/** Sum a kind's triggers into the two numbers a row shows. */
export function triggerTotals(statuses: TriggerStatus[]): {
  parked: number
  lag: number
} {
  return statuses.reduce(
    (acc, s) => ({
      parked: acc.parked + s.parked,
      lag: acc.lag + (s.lag ?? 0),
    }),
    { parked: 0, lag: 0 }
  )
}

/** A count of `{key: n}` as `n key` fragments, largest first. */
export function countPhrase(counts: Record<string, number>): string {
  return Object.entries(counts)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([k, n]) => `${n} ${k}`)
    .join(", ")
}

/** A duration in milliseconds as a short human figure. */
export function durationText(ms: number | undefined): string {
  if (ms === undefined) return ""
  if (ms < 1000) return `${ms} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`
  return `${Math.round(ms / 60_000)} min`
}

/** A cursor rendered as what it is — nothing, a count, or "set" — never its
 * bytes: a cursor is connector-private. */
export function cursorText(cursor: unknown): string {
  if (cursor === undefined || cursor === null || cursor === "") return "—"
  if (Array.isArray(cursor)) return `${cursor.length.toLocaleString()} entries`
  if (typeof cursor === "object")
    return `${Object.keys(cursor as object).length.toLocaleString()} keys`
  return "set"
}
