/** An agent's runs, read off its `substrate.reamde.dev/llm/thread` rows: a
 * thread is one run (its `mode` says what started it, its `status` how it
 * ended, its tally what it spent), so the agent page's numbers are sums over
 * the rows the page read, never a second source. And the write policies that
 * speak for the agent, in words. */

import { displayPlural, lowerFirst } from "@/lib/kind-names"
import type { SubstrateRecord } from "@/lib/api/types"
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

export interface RunStats {
  runs: number
  failed: number
  tokens: number
  cost: number
  /** Whether any run carried a cost; a provider row without pricing records
   * none, and "$0" would be a lie. */
  priced: boolean
}

export function runStats(threads: SubstrateRecord[]): RunStats {
  let failed = 0
  let tokens = 0
  let cost = 0
  let priced = false
  for (const thread of threads) {
    if (runFailed(thread)) failed++
    tokens += runTokens(thread) ?? 0
    const spent = runCost(thread)
    if (spent !== undefined) {
      cost += spent
      priced = true
    }
  }
  return { runs: threads.length, failed, tokens, cost, priced }
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
