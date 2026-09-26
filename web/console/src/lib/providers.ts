/** The Providers area's pure half: where a provider stands on the way to a
 * syncing account (the four set-up steps and the one state its card wears),
 * the everyday words for an account's sync and connection, how often a tool
 * runs, and what a provider's kinds fill in of yours. No fetching here; the
 * pages fold these off the reads `lib/api/*` returns. */

import type {
  KindInfo,
  SetupItem,
  SubstrateRecord,
  TriggerStatus,
} from "@/lib/api/types"
import { FAILED_PREVIEW_BLOCKER } from "@/lib/bundles"
import { mappedKind } from "@/lib/provenance"
import { refId, toolName, writeKinds } from "@/lib/tools"
import type { AccountView, ProviderView, SyncFields } from "@/lib/sync"

// ── the four steps ───────────────────────────────────────────────────────────

export type StepKey = "add" | "credentials" | "account" | "choose"
export type StepState = "done" | "now" | "todo"

export interface SetupStep {
  key: StepKey
  /** 1-based, the number the page prints. */
  n: number
  state: StepState
}

/** What the steps are decided from, folded once off the bundle status, the
 * provider view and the account records. */
export interface StepFacts {
  installed: boolean
  /** The provider declares a credentials record (an OAuth client or a token
   * config); without one step 2 has nothing to ask. */
  needsCredentials: boolean
  configured: boolean
  /** The provider declares an account kind; without one there is nothing to
   * connect and steps 3 and 4 stand done. */
  hasAccountKind: boolean
  accounts: {
    /** Approved with the provider (an OAuth grant), or a token account, which
     * needs no approval. */
    connected: boolean
    /** How many of the account kind's switches are on; undefined when the
     * kind declares none, which leaves nothing to choose. */
    chosen?: number
  }[]
}

const ORDER: StepKey[] = ["add", "credentials", "account", "choose"]

function stepDone(key: StepKey, f: StepFacts): boolean {
  switch (key) {
    case "add":
      return f.installed
    case "credentials":
      return f.installed && (!f.needsCredentials || f.configured)
    case "account":
      return (
        f.installed &&
        (!f.hasAccountKind || f.accounts.some((a) => a.connected))
      )
    case "choose":
      return (
        f.installed &&
        (!f.hasAccountKind ||
          f.accounts.some((a) => a.chosen === undefined || a.chosen > 0))
      )
  }
}

/** The four steps in order: every done one, then the first open one as the
 * current step, then the rest to do. A later step that happens to be done
 * while an earlier one is open still reads as done. */
export function setupSteps(f: StepFacts): SetupStep[] {
  let current = false
  return ORDER.map((key, i) => {
    const done = stepDone(key, f)
    let state: StepState = done ? "done" : "todo"
    if (!done && !current) {
      state = "now"
      current = true
    }
    return { key, n: i + 1, state }
  })
}

export function currentStep(steps: SetupStep[]): SetupStep | undefined {
  return steps.find((s) => s.state === "now")
}

/** The switches on one account: the bool properties its kind declares the
 * owner writes, counted where they are on. Undefined when there are none. */
export function chosenCount(
  record: SubstrateRecord,
  toggles: readonly string[]
): number | undefined {
  if (!toggles.length) return undefined
  return toggles.filter((t) => record.properties?.[t] === true).length
}

export function stepFactsOf(
  installed: boolean,
  view: ProviderView | undefined,
  toggles: readonly string[]
): StepFacts {
  return {
    installed,
    needsCredentials: Boolean(view?.configKind),
    configured: view?.configured ?? false,
    hasAccountKind: Boolean(view?.accountKind),
    accounts: (view?.accounts ?? []).map((a) => ({
      connected: !view?.oauth || a.tokenStatus === "connected",
      chosen: chosenCount(a.record, toggles),
    })),
  }
}

// ── the card's one state, and why ────────────────────────────────────────────

export type ProviderTone = "on" | "setup" | "attention" | "paused" | "add"

/** What a person can do about a problem, in the order the callout offers it.
 * `sync-now` asks the account for a run; `reconnect` sends it through the
 * provider's consent again; `add-again` reinstalls the provider; `set-up`
 * goes to the set-up step (sign-in details) or the settings that are
 * empty; `retry-parked` retries every parked run of the provider's. */
export type ProblemFix =
  "sync-now" | "reconnect" | "add-again" | "set-up" | "retry-parked"

export type ProblemCode =
  | "failed-to-load"
  | "sign-in"
  | "sync"
  | "credentials-missing"
  | "setup-missing"
  | "trigger-broken"
  | "parked"
  | "update-blocked"

/** One reason a provider needs attention: what is wrong in a sentence, the
 * server's own words when it gave some, and what fixes it. */
export interface ProviderProblem {
  code: ProblemCode
  /** What is wrong, in one sentence: the card's line and the callout's. */
  summary: string
  /** The server's words for it (an error, a reason, guard lines), when it
   * gave some. */
  detail?: string[]
  /** The account the problem is on, by record id. */
  account?: string
  fixes: ProblemFix[]
}

export interface ProviderStanding {
  tone: ProviderTone
  /** The pill's word; empty for a provider not added, which shows a button. */
  pill: string
  /** The line under the description: what is true right now. */
  line: string
  /** The current step's number while one is open. */
  step?: number
  /** Why it needs attention: never empty when the tone is `attention`, empty
   * otherwise. */
  problems: ProviderProblem[]
}

export interface StandingInput {
  /** The bundle is here and admitted. */
  installed: boolean
  enabled: boolean
  quarantined: boolean
  quarantineReason?: string
  /** The server's guard lines on an update it will not offer; empty when
   * none blocks it. */
  upgradeBlockers: string[]
  /** Setup items the four steps do not cover (a required setting). */
  otherSetup: Pick<SetupItem, "code" | "message">[]
  steps: SetupStep[]
  /** Accounts connect through the provider's consent (an OAuth grant). */
  oauth?: boolean
  accounts: (Pick<
    AccountView,
    "sync" | "label" | "tokenStatus" | "legacySyncStatus"
  > & { record: Pick<SubstrateRecord, "id"> })[]
  /** The delivery bookkeeping of the triggers that run the provider's own
   * functions. */
  triggers?: Pick<TriggerStatus, "id" | "parked" | "error">[]
}

const ATTENTION = "Needs attention"

/** One state per provider, in the order a reader needs to hear it: broken
 * before paused before unfinished before on. A broken one carries every
 * reason it is broken. */
export function providerStanding(s: StandingInput): ProviderStanding {
  if (s.quarantined) {
    return attention([
      {
        code: "failed-to-load",
        summary: "It failed to load",
        detail: s.quarantineReason ? [s.quarantineReason] : undefined,
        fixes: ["add-again"],
      },
    ])
  }
  if (!s.installed) {
    return { tone: "add", pill: "", line: "Not added", problems: [] }
  }
  if (!s.enabled) {
    return {
      tone: "paused",
      pill: "Paused",
      line: "Paused · nothing syncs",
      problems: [],
    }
  }
  const problems = standingProblems(s)
  if (problems.length) return attention(problems)
  const now = currentStep(s.steps)
  if (now) {
    return {
      tone: "setup",
      pill: "Set up",
      line: `Almost there · step ${now.n} of ${s.steps.length}`,
      step: now.n,
      problems: [],
    }
  }
  if (s.otherSetup.length > 0) {
    return {
      tone: "setup",
      pill: "Set up",
      line: "Almost there · finish its settings",
      problems: [],
    }
  }
  return {
    tone: "on",
    pill: "On",
    line: connectedLine(s.accounts),
    problems: [],
  }
}

function attention(problems: ProviderProblem[]): ProviderStanding {
  const more = problems.length - 1
  return {
    tone: "attention",
    pill: ATTENTION,
    line:
      more > 0
        ? `${problems[0].summary} · and ${more} more`
        : problems[0].summary,
    problems,
  }
}

/** Every reason an added, running provider needs attention, most pressing
 * first: an account that cannot sign in or sync, a gap in its setup once it
 * has accounts, a trigger that cannot run, runs that failed and wait to be
 * retried, an update your records block. */
export function standingProblems(s: StandingInput): ProviderProblem[] {
  const out: ProviderProblem[] = []
  for (const a of s.accounts) {
    const why = firstLine(
      a.sync.error ?? a.sync.message ?? a.legacySyncStatus ?? ""
    )
    if (a.tokenStatus === "erroring") {
      out.push({
        code: "sign-in",
        summary: `The sign-in for ${a.label} stopped working`,
        detail: why ? [why] : undefined,
        account: a.record.id,
        fixes: ["reconnect"],
      })
    } else if (
      a.sync.state === "erroring" ||
      Boolean(a.legacySyncStatus?.startsWith("erroring"))
    ) {
      out.push({
        code: "sync",
        summary: `Syncing ${a.label} is failing`,
        detail: why ? [why] : undefined,
        account: a.record.id,
        fixes: s.oauth ? ["sync-now", "reconnect"] : ["sync-now"],
      })
    }
  }
  // A gap in setup is a problem once accounts depend on it; before then it
  // is the set-up the steps walk through.
  if (s.accounts.length > 0) {
    if (currentStep(s.steps)?.key === "credentials") {
      out.push({
        code: "credentials-missing",
        summary: "Its sign-in details are missing",
        fixes: ["set-up"],
      })
    }
    for (const item of s.otherSetup) {
      out.push({
        code: "setup-missing",
        summary:
          item.code === "setting"
            ? "A setting it needs is empty"
            : "Something it needs is missing",
        detail: item.message ? [item.message] : undefined,
        fixes: ["set-up"],
      })
    }
  }
  const triggers = s.triggers ?? []
  const broken = triggers.filter((t) => t.error)
  if (broken.length) {
    out.push({
      code: "trigger-broken",
      summary:
        broken.length === 1
          ? "One of its syncs can’t run"
          : `${broken.length} of its syncs can’t run`,
      detail: broken.map((t) => `${t.id}: ${t.error}`),
      fixes: ["add-again"],
    })
  }
  const parked = triggers.reduce((n, t) => n + t.parked, 0)
  if (parked > 0) {
    out.push({
      code: "parked",
      summary:
        parked === 1
          ? "1 run failed and is waiting to be tried again"
          : `${parked} runs failed and are waiting to be tried again`,
      fixes: ["retry-parked"],
    })
  }
  if (s.upgradeBlockers.length) {
    const unchecked = s.upgradeBlockers.includes(FAILED_PREVIEW_BLOCKER)
    out.push({
      code: "update-blocked",
      summary: unchecked
        ? "An update couldn’t be checked"
        : "An update is waiting on your records",
      detail: unchecked
        ? ["The server couldn’t preview it; its log says why."]
        : s.upgradeBlockers,
      fixes: [],
    })
  }
  return out
}

function connectedLine(accounts: StandingInput["accounts"]): string {
  if (!accounts.length) return "Ready"
  if (accounts.some((a) => a.sync.state === "running")) {
    return "Connected · syncing"
  }
  if (accounts.every((a) => a.sync.paused)) return "Connected · paused"
  if (accounts.every((a) => a.sync.state === "never")) {
    return "Connected · first sync on its way"
  }
  return "Connected · up to date"
}

// ── an account in everyday words ─────────────────────────────────────────────

export type WordTone = "ok" | "active" | "warn" | "bad" | "muted"

export interface Words {
  text: string
  tone: WordTone
}

/** The core `sync` trait's five states as a person reads them. */
export function syncWords(
  f: Pick<SyncFields, "state" | "paused" | "message" | "error">,
  providerName: string
): Words {
  if (f.paused) return { text: "Paused", tone: "warn" }
  switch (f.state) {
    case "running":
      return { text: "Syncing…", tone: "active" }
    case "ok":
      return { text: "Up to date", tone: "ok" }
    case "erroring": {
      const why = firstLine(f.error ?? f.message ?? "")
      return {
        text: why ? `Having trouble: ${why}` : "Having trouble",
        tone: "bad",
      }
    }
    case "throttled":
      return { text: `Slowed down by ${providerName}`, tone: "warn" }
    default:
      return { text: "Not synced yet", tone: "muted" }
  }
}

/** The OAuth facility's `tokenStatus` as a person reads it. */
export function connectionWords(
  tokenStatus: string | undefined,
  providerName: string
): Words {
  switch (tokenStatus) {
    case "connected":
      return { text: "Connected", tone: "ok" }
    case "pending":
      return {
        text: `Waiting for you to approve it at ${providerName}`,
        tone: "warn",
      }
    case "erroring":
      return { text: "Its sign-in stopped working · reconnect it", tone: "bad" }
    case undefined:
    case "":
      return { text: "Not connected yet", tone: "warn" }
    default:
      return { text: capitalise(tokenStatus), tone: "muted" }
  }
}

/** The label an enum property declares for one value (`hourly` → "Every
 * hour"), else the value itself. */
export function enumLabel(
  kind: KindInfo | undefined,
  property: string,
  value: string | undefined
): string | undefined {
  if (!value) return undefined
  const props = kind?.definition?.properties as
    Record<string, { values?: unknown }> | undefined
  const values = props?.[property]?.values
  if (Array.isArray(values)) {
    for (const v of values) {
      if (
        typeof v === "object" &&
        v !== null &&
        (v as { value?: unknown }).value === value &&
        typeof (v as { label?: unknown }).label === "string"
      ) {
        return (v as { label: string }).label
      }
    }
  }
  return value
}

/** The switches an account turned on and off, as one sentence: "Calendar
 * and Contacts. Gmail and Drive are off." */
export function choiceSentence(
  record: SubstrateRecord,
  toggles: readonly { name: string; label: string }[]
): string {
  const on = toggles.filter((t) => record.properties?.[t.name] === true)
  const off = toggles.filter((t) => record.properties?.[t.name] !== true)
  if (!toggles.length) return "Everything it can bring in."
  if (!on.length) return "Nothing is turned on yet."
  const head = `${andList(on.map((t) => t.label))}.`
  if (!off.length) return head
  return `${head} ${andList(off.map((t) => t.label))} ${off.length === 1 ? "is" : "are"} off.`
}

// ── tools ────────────────────────────────────────────────────────────────────

/** An iCalendar recurrence rule in words: FREQ=HOURLY → "Every hour",
 * FREQ=MINUTELY;INTERVAL=15 → "Every 15 minutes". */
export function recurrenceWords(rule: string): string {
  const parts = new Map(
    rule
      .replace(/^RRULE:/i, "")
      .split(";")
      .map((p) => p.split("=") as [string, string])
      .map(([k, v]) => [k.toUpperCase(), v ?? ""])
  )
  const unit: Record<string, string> = {
    SECONDLY: "second",
    MINUTELY: "minute",
    HOURLY: "hour",
    DAILY: "day",
    WEEKLY: "week",
    MONTHLY: "month",
    YEARLY: "year",
  }
  const word = unit[(parts.get("FREQ") ?? "").toUpperCase()]
  if (!word) return "On a schedule"
  const n = Number(parts.get("INTERVAL") ?? "1")
  return n > 1 ? `Every ${n} ${word}s` : `Every ${word}`
}

/** When one trigger runs its callable, in words. */
export function triggerCadence(trigger: SubstrateRecord): string {
  const p = trigger.properties ?? {}
  const source = (p.source ?? {}) as Record<string, unknown>
  const schedule = source.schedule as Record<string, unknown> | undefined
  if (schedule) {
    return typeof schedule.recurrence === "string"
      ? recurrenceWords(schedule.recurrence)
      : "Once, on a schedule"
  }
  if (source.webhook) return "When the service calls in"
  const record = source.record as Record<string, unknown> | undefined
  if (record) {
    const when = typeof record.when === "string" ? record.when : ""
    if (
      /-on-(request|demand)$/.test(trigger.id) ||
      when.includes("syncRequestedAt")
    )
      return "When you press Sync now"
    if (/-on-connect$/.test(trigger.id)) return "When an account connects"
    return "When records change"
  }
  return "When it is called"
}

/** The function a trigger invokes, as its reference (`<authority>/<package>/
 * <name>`), off the stored `callable` reference value. */
export function triggerCallable(trigger: SubstrateRecord): string | undefined {
  const callable = trigger.properties?.callable
  const ref =
    typeof callable === "string"
      ? callable
      : typeof callable === "object" && callable !== null
        ? (callable as { ref?: unknown }).ref
        : undefined
  if (typeof ref !== "string") return undefined
  const prefix = "substrate.reamde.dev/core/function/"
  return ref.startsWith(prefix) ? ref.slice(prefix.length) : ref
}

export interface ToolRow {
  /** The function's reference, `<authority>/<package>/<name>`. */
  reference: string
  name: string
  description?: string
  /** One phrase per trigger that runs it, deduplicated. */
  cadences: string[]
  triggerIds: string[]
}

export function providerTools(
  functions: readonly string[],
  descriptions: Record<string, string> | undefined,
  triggers: readonly SubstrateRecord[]
): ToolRow[] {
  return functions
    .map((reference) => {
      const mine = triggers.filter((t) => triggerCallable(t) === reference)
      return {
        reference,
        name: toolName(reference),
        description: descriptions?.[reference],
        cadences: [...new Set(mine.map(triggerCadence))],
        triggerIds: mine.map((t) => t.id),
      }
    })
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** The last time any of a tool's triggers fired, and how many deliveries are
 * parked across them. */
export function toolActivity(
  triggerIds: readonly string[],
  statuses: readonly TriggerStatus[]
): { lastFire?: string; parked: number } {
  let lastFire: string | undefined
  let parked = 0
  for (const s of statuses) {
    if (!triggerIds.includes(s.id)) continue
    parked += s.parked
    if (s.lastFire && (!lastFire || s.lastFire > lastFire))
      lastFire = s.lastFire
  }
  return { lastFire, parked }
}

// ── what a provider did ──────────────────────────────────────────────────────

function colons(id: string): string {
  return id.split("/").join(":")
}

/** The actors a provider writes as: its bundle, and each of its functions
 * (decision 0025 derives both from the declaration ids). */
export function providerActors(
  bundleId: string,
  functionIds: readonly string[]
): string[] {
  return [
    `bundle:${colons(bundleId)}`,
    ...functionIds
      .filter((id) => id.startsWith(`${bundleId}/`))
      .map((id) => `function:${colons(id)}`),
  ].sort()
}

/** When a provider last synced one of its kinds: the newest finished run of
 * any trigger whose function may write the kind. A skipped run (its guard
 * said no) and a parked one brought nothing in, so neither counts. */
export function lastSyncOf(
  kind: string,
  bundleId: string,
  functions: readonly SubstrateRecord[],
  triggers: readonly SubstrateRecord[],
  runs: readonly SubstrateRecord[]
): string | undefined {
  const writers = new Set(
    functions
      .filter(
        (f) => f.id.startsWith(`${bundleId}/`) && writeKinds(f).includes(kind)
      )
      .map((f) => f.id)
  )
  if (!writers.size) return undefined
  const fired = new Set(
    triggers
      .filter((t) => writers.has(triggerCallable(t) ?? ""))
      .map((t) => t.id)
  )
  let last: string | undefined
  for (const r of runs) {
    const p = r.properties
    if (p.status !== "ok" || !fired.has(refId(p.trigger) ?? "")) continue
    const at =
      typeof p.finishedAt === "string" && p.finishedAt
        ? p.finishedAt
        : r.updatedAt
    if (!last || at > last) last = at
  }
  return last
}

// ── what a provider's kinds fill in ──────────────────────────────────────────

/** The kinds of yours a provider kind's records are mapped onto, off the
 * recordmapping declarations: each one reads `from` and writes `to`. */
export function fillsIn(
  mappings: readonly SubstrateRecord[],
  kind: string
): string[] {
  const out = new Set<string>()
  for (const m of mappings) {
    if (mappedKind(m.properties?.from) !== kind) continue
    const to = mappedKind(m.properties?.to)
    if (to) out.add(to)
  }
  return [...out].sort()
}

// ── removal ──────────────────────────────────────────────────────────────────

export type LadderStep = "disable" | "purge" | "uninstall"

/** The rungs the server makes a removal climb, from where the bundle
 * stands: purge is refused while it runs, uninstall while its data lives. A
 * quarantined bundle has no registration to remove, only data to delete. */
export function removalLadder(b: {
  installed: boolean
  enabled: boolean
  liveRecords?: number
}): LadderStep[] {
  const steps: LadderStep[] = []
  if (b.installed && b.enabled) steps.push("disable")
  if ((b.liveRecords ?? 0) > 0) steps.push("purge")
  if (b.installed) steps.push("uninstall")
  return steps
}

// ── ids and words ────────────────────────────────────────────────────────────

/** A bundle id, `<authority>/<package>`, as the two route segments. */
export function bundleParams(id: string): { authority: string; pkg: string } {
  const at = id.lastIndexOf("/")
  return { authority: id.slice(0, at), pkg: id.slice(at + 1) }
}

/** A catalog description's first sentence: what a card has room for. */
export function firstSentence(text: string | undefined): string {
  if (!text) return ""
  const trimmed = text.trim()
  const end = trimmed.search(/[.!?](\s|$)/)
  return end === -1 ? trimmed : trimmed.slice(0, end + 1)
}

function firstLine(text: string): string {
  return text.split("\n")[0].trim()
}

function capitalise(text: string): string {
  return text ? text[0].toUpperCase() + text.slice(1) : text
}

/** "a", "a and b", "a, b and c". */
export function andList(items: string[]): string {
  if (items.length <= 1) return items.join("")
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`
}
