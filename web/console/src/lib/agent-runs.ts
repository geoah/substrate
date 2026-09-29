/** An agent's runs, read off its `substrate.reamde.dev/llm/thread` rows: a
 * thread is one run (its `mode` says what started it, its `status` how it
 * ended, its tally what it spent), so the agent page's numbers are sums over
 * the rows the page read, never a second source. The limits one run runs
 * under, as the loader reads them. And the write policies that speak for the
 * agent, in words. */

import { threadStartedAt } from "@/lib/agent-chat"
import { displayPlural, lowerFirst } from "@/lib/kind-names"
import { readReference, type SubstrateRecord } from "@/lib/api/types"
import { kindPatternWords } from "@/lib/tools"
import { allowVerbs, policyIdOf, type PolicyOp } from "@/lib/agent-rules"

export type RunStatus = "running" | "ok" | "overbudget" | "error"

export function runStatus(thread: SubstrateRecord): RunStatus {
  const status = thread.properties.status
  return status === "running" || status === "overbudget" || status === "error"
    ? status
    : "ok"
}

/** Whether a run ended without doing what it was asked. */
export function runFailed(thread: SubstrateRecord): boolean {
  const status = runStatus(thread)
  return status === "error" || status === "overbudget"
}

const MODE_WORDS: Record<string, string> = {
  chat: "A chat",
  manual: "Started by hand",
  call: "Called directly",
  record: "A change to your data",
  schedule: "On a schedule",
  webhook: "A webhook",
  subagent: "Asked by another agent",
  judge: "Reviewing a suggestion",
}

/** What started a run, in words. */
export function runStartWords(thread: SubstrateRecord): string {
  const mode = thread.properties.mode
  return (typeof mode === "string" && MODE_WORDS[mode]) || "A run"
}

/** Whether a run reads as a conversation the chat app opens. */
export function isConversation(thread: SubstrateRecord): boolean {
  const mode = thread.properties.mode
  return mode !== "subagent" && mode !== "judge"
}

export const STATUS_WORDS: Record<RunStatus, string> = {
  running: "Running",
  ok: "Done",
  overbudget: "Ran out of budget",
  error: "Failed",
}

function num(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined
}

export function runTokens(thread: SubstrateRecord): number | undefined {
  return num(thread.properties.totalTokens)
}

export function runCost(thread: SubstrateRecord): number | undefined {
  return num(thread.properties.costUSD)
}

/** Whether a run spent tokens and recorded no cost for them. The loop prices
 * a run by its provider row as the row stood then and writes `costUSD: 0`
 * for a model the row had no price for, so "$0" beside tokens would read as
 * free. The thread row alone decides: today's pricing and today's sub-agents
 * cannot say what an old run was priced at. Every cost on the agent page is
 * the cost a run RECORDED, a root run's included, which records each
 * sub-agent's spend as that sub-agent's row priced it. */
export function runUnpriced(thread: SubstrateRecord): boolean {
  return (runTokens(thread) ?? 0) > 0 && !((runCost(thread) ?? 0) > 0)
}

export interface RunStats {
  runs: number
  failed: number
  tokens: number
  cost: number
  /** How many runs spent tokens and recorded no cost (`runUnpriced`): the
   * cost is then a floor, not the whole. */
  unpriced: number
}

export function runStats(threads: SubstrateRecord[]): RunStats {
  const stats = {
    runs: threads.length,
    failed: 0,
    tokens: 0,
    cost: 0,
    unpriced: 0,
  }
  for (const thread of threads) {
    if (runFailed(thread)) stats.failed++
    stats.tokens += runTokens(thread) ?? 0
    stats.cost += runCost(thread) ?? 0
    if (runUnpriced(thread)) stats.unpriced++
  }
  return stats
}

/** A token count as a person reads it: "840", "12.3k", "1.2M". */
export function tokenWords(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${trim(n / 1000)}k`
  return `${trim(n / 1_000_000)}M`
}

function trim(n: number): string {
  return n >= 100 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, "")
}

/** Dollars, to the cent, and to the tenth of a cent under one. */
export function costWords(usd: number): string {
  if (usd === 0) return "$0"
  if (usd < 0.001) return "under $0.001"
  if (usd < 1) return `$${usd.toFixed(3)}`
  return `$${usd.toFixed(2)}`
}

// ── when, how long, and who asked ───────────────────────────────────────────

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** When a run started: its `startedAt`, else when its row was made. */
export function runStartedAt(thread: SubstrateRecord): string {
  return threadStartedAt(thread)
}

/** When a run last settled: `finishedAt`, else its start. A failure's time. */
export function runSettledAt(thread: SubstrateRecord): string {
  const end = thread.properties.finishedAt
  return typeof end === "string" && end ? end : runStartedAt(thread)
}

/** How long a run took, from its start to its last settle; undefined while
 * it runs or when a stamp is missing. The thread IS the run, and one that is
 * continued (a chat you came back to, a run resumed by a decision) settles
 * again each time, so its figure spans the wait between too. */
export function runDurationMs(thread: SubstrateRecord): number | undefined {
  if (runStatus(thread) === "running") return undefined
  const end = thread.properties.finishedAt
  if (typeof end !== "string") return undefined
  const ms = Date.parse(end) - Date.parse(runStartedAt(thread))
  return Number.isFinite(ms) && ms >= 0 ? ms : undefined
}

/** Past this a span is not one invocation's working time: an invocation's
 * wall clock is capped at 600 seconds (`vocabulary.MaxAgentDeadlineSec`), and
 * the minute on top covers the settle after it. */
const ONE_INVOCATION_MS = 11 * MINUTE

/** Whether a run's span is longer than any one invocation may work: its
 * thread was continued later, or a restart settled it long after it began.
 * Either way the span is not how long it worked. */
export function runOutlastsInvocation(thread: SubstrateRecord): boolean {
  return (runDurationMs(thread) ?? 0) > ONE_INVOCATION_MS
}

/** A run's length: "0.4s", "12s", "3 min", "2.5 h", "3 days". */
export function durationWords(ms: number): string {
  if (ms < 100) return "under 0.1s"
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`
  if (ms < MINUTE) return `${Math.round(ms / 1000)}s`
  if (ms < HOUR) return `${Math.round(ms / MINUTE)} min`
  if (ms < DAY) return `${trim(ms / HOUR)} h`
  const days = Math.round(ms / DAY)
  return days === 1 ? "1 day" : `${days} days`
}

/** Whether another agent asked for this run. Its tally is its own spend
 * alone; a run nobody asked for (the root of a chain) carries the whole
 * chain's, sub-agents included (`agentLoop.settle`). */
export function askedByAgent(thread: SubstrateRecord): boolean {
  return (
    thread.properties.mode === "subagent" ||
    readReference(thread.properties.parent) !== undefined
  )
}

// ── what it spent, by period ────────────────────────────────────────────────

export interface RunPeriod {
  key: "day" | "week" | "month"
  label: string
  ms: number
}

/** The periods the agent page totals, each ending now. */
export const RUN_PERIODS: RunPeriod[] = [
  { key: "day", label: "Last 24 hours", ms: DAY },
  { key: "week", label: "Last 7 days", ms: 7 * DAY },
  { key: "month", label: "Last 30 days", ms: 30 * DAY },
]

export interface GroupTotals extends RunStats {
  /** Whether every run active in the period was read: false when the read
   * stopped at its bound before reaching the period's start. The counts are
   * then floors. */
  complete: boolean
  /** Whether the tokens and cost are the period's whole spend. False when
   * the counts are floors, or when a run that started before the period
   * went on inside it: part of its spend fell in the period, and a thread's
   * totals cannot say how much. */
  spendComplete: boolean
}

export interface PeriodTotals {
  period: RunPeriod
  /** The runs nobody asked for: each carries its whole chain's spend. */
  started: GroupTotals
  /** The runs another agent asked for: each carries its own spend alone,
   * which the asking agent's run counts too. */
  asked: GroupTotals
}

/** When a run was last active: its row's `updatedAt`, which every settle
 * moves, else when it settled. The key the page's read is ordered by. */
export function runActiveAt(thread: SubstrateRecord): number {
  const updated = Date.parse(thread.updatedAt)
  return Number.isFinite(updated) ? updated : Date.parse(runSettledAt(thread))
}

/** Totals for each period, from the runs one read returned, most recently
 * active first (`agentRunsQueryOptions`). A run belongs to the period it
 * STARTED in, and all it spent since is inside that period, because every
 * period ends now. `truncated` says the read hit its bound, so less recently
 * active runs exist that it did not return; a read that reaches back past a
 * period's start holds every run active in it. The two kinds of run are
 * totalled apart and never summed: a run it started already counts any run
 * it asked for, and this agent may sit twice in one chain. */
export function periodTotals(
  threads: SubstrateRecord[],
  { now = Date.now(), truncated = false } = {}
): PeriodTotals[] {
  let reached = Infinity
  for (const thread of threads) {
    const active = runActiveAt(thread)
    if (Number.isFinite(active)) reached = Math.min(reached, active)
  }
  return RUN_PERIODS.map((period) => {
    const from = now - period.ms
    const complete = !truncated || reached < from
    const group = (runs: SubstrateRecord[]): GroupTotals => {
      const inside: SubstrateRecord[] = []
      let straddled = false
      for (const thread of runs) {
        if (Date.parse(runStartedAt(thread)) >= from) inside.push(thread)
        // A settled run last spent when it settled; any later write to its
        // row (an edit, a migration) moves `updatedAt` and spends nothing.
        else if (
          runStatus(thread) === "running"
            ? runActiveAt(thread) >= from
            : Date.parse(runSettledAt(thread)) >= from
        ) {
          straddled = true
        }
      }
      return {
        ...runStats(inside),
        complete,
        spendComplete: complete && !straddled,
      }
    }
    return {
      period,
      started: group(threads.filter((t) => !askedByAgent(t))),
      asked: group(threads.filter(askedByAgent)),
    }
  })
}

// ── what went wrong ─────────────────────────────────────────────────────────

export interface FailureGroup {
  status: "error" | "overbudget"
  /** The reason's first line, as the loop recorded it; undefined when it
   * recorded none. */
  reason?: string
  count: number
  /** The run that failed this way most recently, by when it settled. */
  latest: SubstrateRecord
}

function reasonLine(thread: SubstrateRecord): string | undefined {
  const reason = thread.properties.reason
  if (typeof reason !== "string") return undefined
  const line = reason.split("\n")[0].trim()
  return line || undefined
}

/** The runs that didn't finish, one group per status and reason, the most
 * frequent first. */
export function failureGroups(threads: SubstrateRecord[]): FailureGroup[] {
  const groups = new Map<string, FailureGroup>()
  for (const thread of threads) {
    if (!runFailed(thread)) continue
    const status = runStatus(thread) as FailureGroup["status"]
    const reason = reasonLine(thread)
    const key = `${status}\n${reason ?? ""}`
    const held = groups.get(key)
    if (!held) {
      groups.set(key, { status, reason, count: 1, latest: thread })
      continue
    }
    held.count++
    if (
      Date.parse(runSettledAt(thread)) > Date.parse(runSettledAt(held.latest))
    ) {
      held.latest = thread
    }
  }
  return [...groups.values()].sort(
    (a, b) =>
      b.count - a.count ||
      Date.parse(runSettledAt(b.latest)) - Date.parse(runSettledAt(a.latest))
  )
}

// ── the limits one run runs under ───────────────────────────────────────────

/** The loader's defaults where a declaration is silent
 * (`vocabulary.DefaultAgentTurns` and the three beside it,
 * `vocabulary.DefaultReadCalls`, `vocabulary.DefaultReadRows`). */
const RUN_BUDGET_DEFAULTS: Record<string, number> = {
  maxTurns: 8,
  maxToolCalls: 32,
  deadlineSeconds: 120,
  depth: 3,
}
const READ_BUDGET_DEFAULTS: Record<string, number> = { calls: 16, rows: 500 }

export interface AgentLimit {
  /** The property path the value lives at on the agent record. */
  path: string[]
  value: number
  /** False where the declaration is silent and the loader's default holds. */
  declared: boolean
}

function bag(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

/** The limits one run of the agent runs under, as the loader reads them.
 * `depth` only bounds the chain below, so it is listed for an agent that asks
 * sub-agents; the read budgets only for one that may read. */
export function agentLimits(agent: SubstrateRecord): AgentLimit[] {
  const out: AgentLimit[] = []
  const budgets = bag(agent.properties.budgets)
  const asks =
    Array.isArray(agent.properties.subagents) &&
    agent.properties.subagents.length > 0
  for (const [key, fallback] of Object.entries(RUN_BUDGET_DEFAULTS)) {
    if (key === "depth" && !asks) continue
    const held = num(budgets[key])
    out.push({
      path: ["budgets", key],
      value: held ?? fallback,
      declared: held !== undefined,
    })
  }
  const reads = bag(bag(agent.properties.permissions).reads)
  if (Array.isArray(reads.kinds) && reads.kinds.length > 0) {
    const readBudgets = bag(reads.budgets)
    for (const [key, fallback] of Object.entries(READ_BUDGET_DEFAULTS)) {
      const held = num(readBudgets[key])
      out.push({
        path: ["permissions", "reads", "budgets", key],
        value: held ?? fallback,
        declared: held !== undefined,
      })
    }
  }
  return out
}

// ── the policies, in words ──────────────────────────────────────────────────

function selectorOf(policy: SubstrateRecord): Record<string, unknown> {
  const held = policy.properties.selector
  return held && typeof held === "object" && !Array.isArray(held)
    ? (held as Record<string, unknown>)
    : {}
}

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === "string")
    : []
}

function kindsWords(kinds: string[]): string {
  if (kinds.length === 0 || kinds.includes("*")) return "anything in your data"
  const words = kinds.map((k) =>
    k.endsWith("/*")
      ? lowerFirst(kindPatternWords(k, displayPlural))
      : lowerFirst(displayPlural(k))
  )
  if (words.length === 1) return words[0]
  return `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`
}

/** One policy as a sentence about the agent: "Asks you before it adds,
 * changes and deletes tasks", "Can't delete people", "Adds and changes
 * tasks without asking". `everyone` says the rule is not this agent's
 * alone. */
export function policySentence(policy: SubstrateRecord): {
  text: string
  tone: "ok" | "warn" | "bad"
  everyone: boolean
  overrides?: string
} {
  const selector = selectorOf(policy)
  const ops = strings(selector.ops).filter((op): op is PolicyOp =>
    ["put", "patch", "delete"].includes(op)
  )
  const verbs = allowVerbs(ops.length ? ops : ["put", "patch", "delete"])
  const what = kindsWords(strings(selector.kinds))
  const everyone = strings(selector.agents).length === 0
  const overrides = policyIdOf(policy.properties.overrides)
  switch (policy.properties.action) {
    case "refuse":
      return { text: `Can’t ${verbs} ${what}`, tone: "bad", everyone }
    case "gate":
      return {
        text: `Asks you before it can ${verbs} ${what}`,
        tone: "warn",
        everyone,
      }
    default:
      return {
        text: `Can ${verbs} ${what} without asking`,
        tone: "ok",
        everyone,
        overrides,
      }
  }
}
