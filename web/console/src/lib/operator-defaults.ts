/** The Providers page's reading of `GET /api/v1/operator/defaults`
 * (lib/api/operator.ts, docs/plans/host-defaults.md): where one AI model
 * row's key comes from and whether it works, the "whose key" line a card
 * shows, the search line, and the words for the operator's daily allowance.
 * Pure, so every rule is tested without a page.
 *
 * The operator is shown by the site's name, or as "this server" where the
 * deployment set none: never a deployment's or a person's name written
 * here. */

import { PROVIDERS_AUTHORITY, providerInfo } from "@/lib/actor-identity"
import { providerLabelName, providerName } from "@/lib/agent-chat"
import { capWords } from "@/lib/agent-runs"
import { providerHasKey } from "@/lib/api/agents"
import type {
  OperatorAllowance,
  OperatorDefault,
  OperatorDefaultSource,
  OperatorInfo,
  SubstrateRecord,
} from "@/lib/api/types"
import { LLM_PACKAGE } from "@/lib/core-kinds"
import { shortTime } from "@/lib/format"
import { andList } from "@/lib/providers"

/** The kind every AI model card's row is a record of. */
export const LLM_PROVIDER_KIND = `${LLM_PACKAGE}/provider`

/** How the page names the operator: by its `site.yaml` name, or as "this
 * server" where the deployment has none. */
export interface OperatorWords {
  /** The deployment set a name. */
  named: boolean
  /** "Crane", "this server". */
  name: string
  /** At the start of a sentence: "Crane", "This server". */
  Name: string
  /** "Crane’s", "this server’s". */
  possessive: string
}

export function operatorWords(info: OperatorInfo | undefined): OperatorWords {
  const named = info?.name?.trim()
  if (named)
    return { named: true, name: named, Name: named, possessive: `${named}’s` }
  return {
    named: false,
    name: "this server",
    Name: "This server",
    possessive: "this server’s",
  }
}

// ── items ───────────────────────────────────────────────────────────────────

/** The item for one llm/provider row: `llm/<row id>`. */
export function modelItem(
  items: readonly OperatorDefault[] | undefined,
  rowId: string
): OperatorDefault | undefined {
  return items?.find((i) => i.id === `llm/${rowId}`)
}

/** The package an OAuth client or a tool key belongs to: the item's target,
 * else the rest of `oauth/<package>` or `function/<package>`. */
export function itemPackage(
  item: Pick<OperatorDefault, "id" | "slot" | "target">
): string | undefined {
  if (item.slot !== "oauth" && item.slot !== "function") return undefined
  if (item.target) return item.target
  const at = item.id.indexOf("/")
  return at > 0 ? item.id.slice(at + 1) : undefined
}

/** The llm/provider row a record path names, when it names one. */
export function providerRowOf(target: string | undefined): string | undefined {
  const prefix = `${LLM_PROVIDER_KIND}/`
  return target?.startsWith(prefix) ? target.slice(prefix.length) : undefined
}

// ── AI model cards ──────────────────────────────────────────────────────────

/** An AI model row's name: the vendor's spelling where its label or, with no
 * label, its id is a well-known one ("openai" is OpenAI), else its label as
 * written ("My gateway"), else its id capitalised. */
export function modelName(
  row: Pick<SubstrateRecord, "id" | "properties">
): string {
  const label = row.properties.label
  if (typeof label === "string" && label.trim())
    return providerLabelName(label.trim())
  return providerName(row.id)
}

/** The pill an AI model card wears. Never Paused: a row has no off switch.
 * `checking` while the operator's read has not answered, `unchecked` where
 * it failed: a row without a key of its own may be the operator's, so
 * neither says Set up. */
export type ModelState = "on" | "setup" | "attention" | "checking" | "unchecked"

export interface ModelKey {
  state: ModelState
  /** Where the key comes from now; undefined while that cannot be told. */
  source?: OperatorDefaultSource
  /** The operator serves this row, so deleting your own key falls back to
   * theirs. */
  operatorOffers: boolean
  /** What a card with no key lacks, in plain words: "Needs a key". */
  status?: string
  /** Why its calls fail, or why a key of its own does not resolve, in the
   * server's words. Never set beside `status`: the server's sentence for a
   * row with no key names the row's `apiKey` property, which the card never
   * shows. */
  message?: string
  /** The models the operator's key allows, where it serves the row. */
  models?: string[]
}

/** The status a card that is not set up and holds no key of its own says. */
const NEEDS_A_KEY = "Needs a key"

/** Where one AI model row's key comes from and whether it works: the
 * operator read's item where there is one, else the row's own `apiKey` (a
 * server older than the read). A key that resolves is On, none is Set up
 * and says it needs a key, and a key whose calls fail (`failing`) Needs
 * attention. */
export function modelKey(
  row: SubstrateRecord,
  item: OperatorDefault | undefined
): ModelKey {
  if (!item) {
    const own = providerHasKey(row)
    return {
      state: own ? "on" : "setup",
      source: own ? "own" : "none",
      operatorOffers: false,
      status: own ? undefined : NEEDS_A_KEY,
    }
  }
  const state: ModelState =
    item.readiness === "failing"
      ? "attention"
      : item.source === "none" || item.readiness === "not-set-up"
        ? "setup"
        : "on"
  // A row whose own key is stored but does not resolve lacks something
  // other than a key (its wire, its endpoint), and only the server's words
  // say which.
  const keyless = state === "setup" && item.source !== "own"
  return {
    state,
    source: item.source,
    operatorOffers: item.operatorOffers,
    status: keyless ? NEEDS_A_KEY : undefined,
    message: item.readiness === "ready" || keyless ? undefined : item.message,
    models: item.source === "operator" ? item.models : undefined,
  }
}

/** One row's key while the operator's read has not answered or failed. A
 * key of its own is the row's, because the operator's is used only where the
 * row holds none; without one, nobody can say yet whose it would be. */
export function uncheckedKey(
  row: SubstrateRecord,
  state: "checking" | "unchecked"
): ModelKey {
  return {
    state,
    source: providerHasKey(row) ? "own" : undefined,
    operatorOffers: false,
  }
}

/** Whether a key in this state resolves: true for On and Needs attention,
 * false for Set up, undefined while it cannot be told. */
export function stateResolves(state: ModelState): boolean | undefined {
  if (state === "on" || state === "attention") return true
  if (state === "setup") return false
  return undefined
}

/** "Using Crane’s key, for gpt-5 and gpt-5-mini.", "Using your key";
 * nothing where no key resolves or nobody can say whose it is. */
export function modelWhoseKey(
  key: Pick<ModelKey, "source" | "state" | "models">,
  words: OperatorWords
): string | undefined {
  if (key.state === "setup") return undefined
  if (key.source === "operator")
    return key.models?.length
      ? `Using ${words.possessive} key, for ${andList(key.models)}.`
      : `Using ${words.possessive} key`
  if (key.source === "own") return "Using your key"
  return undefined
}

/** The second line on the card the embeddings item names (its `target`):
 * whose key search by meaning runs on. Nothing on any other card, and
 * nothing where search by meaning has no key. */
export function searchLine(
  items: readonly OperatorDefault[] | undefined,
  rowId: string,
  words: OperatorWords
): string | undefined {
  const item = items?.find((i) => i.slot === "embeddings")
  if (!item || providerRowOf(item.target) !== rowId) return undefined
  if (item.source === "operator")
    return `Search by meaning: ${words.possessive} key`
  if (item.source === "own") return "Search by meaning: your key"
  return undefined
}

// ── provider cards ──────────────────────────────────────────────────────────

/** The "whose key" line on a provider's card, from its OAuth client or its
 * tool key: only where the operator is in the picture (it offers the slot,
 * or serves it now), because elsewhere nobody but the person can supply
 * one. `notice` is the operator's own words about the slot. */
export function packageWhoseKey(
  items: readonly OperatorDefault[] | undefined,
  pkg: string,
  /** The provider's name: "Google". */
  name: string,
  words: OperatorWords
): { line: string; notice?: string } | undefined {
  const mine = (items ?? []).filter(
    (i) =>
      itemPackage(i) === pkg && (i.operatorOffers || i.source === "operator")
  )
  const oauth = mine.find((i) => i.slot === "oauth")
  if (oauth?.source === "operator")
    return {
      line: `Through ${words.possessive} ${name} sign-in`,
      notice: oauth.notice,
    }
  if (oauth?.source === "own") return { line: "Using your sign-in details" }
  const tool = mine.find((i) => i.slot === "function")
  if (tool?.source === "operator")
    return { line: `Using ${words.possessive} key`, notice: tool.notice }
  if (tool?.source === "own") return { line: "Using your key" }
  return undefined
}

/** The operator's failed attempt to add a package for this repository, in
 * the server's words. */
export function installFailure(
  items: readonly OperatorDefault[] | undefined,
  pkg: string
): string | undefined {
  const item = (items ?? []).find(
    (i) => itemPackage(i) === pkg && i.readiness === "install-failed"
  )
  return item ? (item.message ?? "") : undefined
}

/** Whether the operator pays for any slot right now: what puts the
 * allowance on the AI models header. */
export function operatorServesAny(
  items: readonly OperatorDefault[] | undefined
): boolean {
  return (items ?? []).some((i) => i.source === "operator")
}

/** Whether the operator pays for a tool key right now: what puts the
 * allowance note on the Tools header. */
export function operatorServesTools(
  items: readonly OperatorDefault[] | undefined
): boolean {
  return (items ?? []).some(
    (i) => i.slot === "function" && i.source === "operator"
  )
}

// ── the allowance ───────────────────────────────────────────────────────────

/** "$0.37". */
export const centsWords = capWords

/** When the allowance resets, in the reader's own time zone: "Resets at
 * 02:00, in 9 hours". The operator's day is a UTC calendar day; the clock
 * shown is the browser's. */
export function resetWords(resetsAt: string, now: number): string {
  const at = Date.parse(resetsAt)
  if (Number.isNaN(at)) return ""
  const left = at - now
  const minutes = Math.floor(left / 60_000)
  const hours = Math.floor(minutes / 60)
  const when =
    left <= 0
      ? "now"
      : minutes < 1
        ? "in under a minute"
        : hours < 1
          ? `in ${minutes} minute${minutes === 1 ? "" : "s"}`
          : `in ${hours} hour${hours === 1 ? "" : "s"}`
  return `Resets at ${shortTime(resetsAt)}, ${when}`
}

/** What each slot is called in the allowance's split. */
export const SLOT_WORDS: Record<string, string> = {
  llm: "chat",
  embeddings: "search by meaning",
  function: "tools",
  oauth: "sign-in",
}

/** Today's spend by slot, the ones that spent something, in the order the
 * server lists them. */
export function spendSplit(
  allowance: OperatorAllowance
): { slot: string; word: string; cents: number }[] {
  return (allowance.bySlot ?? [])
    .filter((s) => s.cents > 0)
    .map((s) => ({ ...s, word: SLOT_WORDS[s.slot] ?? s.slot }))
}

/** A share of the day's limit, 0 to 100. */
export function percentOf(cents: number, centsPerDay: number): number {
  if (centsPerDay <= 0) return cents > 0 ? 100 : 0
  return Math.min(100, Math.max(0, (cents / centsPerDay) * 100))
}

// ── the welcome ─────────────────────────────────────────────────────────────

/** What a tool does, in the words its card and the welcome name it by:
 * never the vendor's name, which says nothing to someone who has not used
 * it. */
const TOOL_WORDS: Record<string, string> = {
  [`${PROVIDERS_AUTHORITY}/firecrawl`]: "web search",
}

/** The provider an OAuth client or a tool key belongs to, by its brand name
 * ("Google", "WHOOP"). The item's `label` is the bare package word
 * ("google"), so it is shown only where the item names no package. */
export function itemProviderName(
  item: Pick<OperatorDefault, "id" | "slot" | "target" | "label">
): string {
  const pkg = itemPackage(item)
  const word = pkg?.slice(pkg.lastIndexOf("/") + 1)
  return word ? providerInfo(word).name : item.label
}

/** "web search" for the Firecrawl provider; undefined for a package with no
 * words here. */
export function toolWords(pkg: string | undefined): string | undefined {
  return pkg ? TOOL_WORDS[pkg] : undefined
}

/** The order the welcome names what works in, whatever order the read
 * lists the slots in: chat, then search by meaning, then the tools. */
const WELCOME_ORDER: Record<string, number> = {
  llm: 0,
  embeddings: 1,
  function: 2,
}

/** What the welcome says works now: the operator-served slots that are
 * ready, in words ("chat", "search by meaning", what a tool does), and the
 * accounts the operator's sign-in waits on the reader to connect. */
export function welcomeFacts(items: readonly OperatorDefault[]): {
  works: string[]
  connect: string[]
} {
  const works: string[] = []
  const add = (word: string) => {
    if (!works.includes(word)) works.push(word)
  }
  const served = items
    .filter(
      (i) =>
        i.source === "operator" &&
        i.readiness === "ready" &&
        i.slot in WELCOME_ORDER
    )
    .sort((a, b) => WELCOME_ORDER[a.slot] - WELCOME_ORDER[b.slot])
  for (const item of served) {
    if (item.slot === "llm") add("chat")
    else if (item.slot === "embeddings") add("search by meaning")
    else add(toolWords(itemPackage(item)) ?? "tools for your agents")
  }
  const connect = items
    .filter(
      (i) =>
        i.slot === "oauth" &&
        i.source === "operator" &&
        (i.readiness === "needs-connect" || i.readiness === "reconnect-needed")
    )
    .map(itemProviderName)
  return { works, connect }
}

/** The first word capitalised: "Chat and search by meaning". */
export function sentenceCase(text: string): string {
  return text ? text[0].toUpperCase() + text.slice(1) : text
}
