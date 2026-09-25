/** The Providers area's pure half: where a provider stands on the way to a
 * syncing account (the four set-up steps and the one state its card wears),
 * the everyday words for an account's sync and connection, how often a tool
 * runs, and what a provider's kinds fill in of yours. No fetching here; the
 * pages fold these off the reads `lib/api/*` returns. */

import type { KindInfo, SubstrateRecord, TriggerStatus } from "@/lib/api/types"
import { mappedKind } from "@/lib/provenance"
import { toolName } from "@/lib/tools"
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

// ── the card's one state ─────────────────────────────────────────────────────

export type ProviderTone = "on" | "setup" | "attention" | "paused" | "add"

export interface ProviderStanding {
  tone: ProviderTone
  /** The pill's word; empty for a provider not added, which shows a button. */
  pill: string
  /** The line under the description: what is true right now. */
  line: string
  /** The current step's number while one is open. */
  step?: number
}

export interface StandingInput {
  /** The bundle is here and admitted. */
  installed: boolean
  enabled: boolean
  quarantined: boolean
  upgradeBlocked: boolean
  /** Setup items the four steps do not cover (a required setting). */
  otherSetup: number
  steps: SetupStep[]
  accounts: Pick<AccountView, "health" | "sync" | "label">[]
}

/** One state per provider, in the order a reader needs to hear it: broken
 * before paused before unfinished before on. */
export function providerStanding(s: StandingInput): ProviderStanding {
  if (s.quarantined) {
    return {
      tone: "attention",
      pill: "Needs attention",
      line: "It failed to load · add it again to fix",
    }
  }
  if (!s.installed) return { tone: "add", pill: "", line: "Not added" }
  if (!s.enabled) {
    return { tone: "paused", pill: "Paused", line: "Paused · nothing syncs" }
  }
  const broken = s.accounts.find((a) => a.health === "broken")
  if (broken) {
    return {
      tone: "attention",
      pill: "Needs attention",
      line: `Having trouble with ${broken.label}`,
    }
  }
  if (s.upgradeBlocked) {
    return {
      tone: "attention",
      pill: "Needs attention",
      line: "An update is waiting on your records",
    }
  }
  const now = currentStep(s.steps)
  if (now) {
    return {
      tone: "setup",
      pill: "Set up",
      line: `Almost there · step ${now.n} of ${s.steps.length}`,
      step: now.n,
    }
  }
  if (s.otherSetup > 0) {
    return {
      tone: "setup",
      pill: "Set up",
      line: "Almost there · finish its settings",
    }
  }
  return { tone: "on", pill: "On", line: connectedLine(s.accounts) }
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
