/** The chat surface's words, derived from records: what an agent is called,
 * what a thread is called, when it happened, what a tool call did and what an
 * agent may see and change — all in everyday copy, and all pure, so the
 * components render answers and this file is where the answers are tested.
 *
 * Nothing here is sent to the API: display names are the console's own. */

import { deliveryNoticeOf, type ToolCallView } from "@/lib/api/transcript"
import { readReference, type SubstrateRecord } from "@/lib/api/types"
import {
  HOST_FUNCTION_PROPOSE,
  HOST_FUNCTION_QUERY,
  HOST_FUNCTION_WRITE,
  RECORD_PATCH_REQUEST_KIND,
  TOOL_FUNCTION_FIELD,
  valueIdentity,
} from "@/lib/agent-grants"
import { displayName, displayPlural } from "@/lib/kind-names"
import { splitRecordPath } from "@/lib/record-path"

/** The `ask` host function: it writes nothing but a question, so it carries no
 * grant and agent-grants does not name it. */
export const HOST_FUNCTION_ASK = "substrate.reamde.dev/core/ask"

const AGENT_KIND = "substrate.reamde.dev/core/agent"
const FUNCTION_PREFIX = "substrate.reamde.dev/core/function/"

function capitalise(text: string): string {
  return text ? text[0].toUpperCase() + text.slice(1) : text
}

/** An agent's name as a person reads it: the local name, camelCase split into
 * words, the first capitalised (`substrateEditor` → "Substrate editor"). The
 * id stays the identity; this is a label. */
export function agentName(id: string): string {
  const local = id.slice(id.lastIndexOf("/") + 1)
  const spaced = local
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[-_]+/g, " ")
    .toLowerCase()
  return capitalise(spaced.trim() || id)
}

/** The actor string an agent writes under (decision 0025), from its record id
 * `<authority>/<package>/<name>`. */
export function agentActor(id: string): string {
  return `agent:${id.split("/").join(":")}`
}

/** The agent a thread ran, as the agent record's id. */
export function threadAgentId(thread: SubstrateRecord): string | undefined {
  const held = readReference(thread.properties.agent)?.path
  if (!held) return undefined
  const split = splitRecordPath(held)
  if (split && split.kind === AGENT_KIND) return split.id
  // A bare id: the declaration pins the kind, so the server admits it.
  return split ? undefined : held
}

/** Whether an agent belongs on the chat surface. A `hiddenFromChat` agent is
 * callable only by other agents (the chat API refuses it too), so it is listed
 * as working on its own rather than offered a conversation. */
export function chatCapable(agent: SubstrateRecord): boolean {
  return agent.properties.hiddenFromChat !== true
}

/** When a thread happened: the loop's own stamp, else the row's creation. */
export function threadStartedAt(thread: SubstrateRecord): string {
  const declared = thread.properties.startedAt
  return typeof declared === "string" && declared ? declared : thread.createdAt
}

export type DayGroup = "Today" | "Yesterday" | "Earlier"

/** Which heading a moment sits under, by the reader's LOCAL calendar day. */
export function dayGroup(iso: string, now = new Date()): DayGroup {
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return "Earlier"
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(start.getTime())
  yesterday.setDate(start.getDate() - 1)
  if (t >= start) return "Today"
  if (t >= yesterday) return "Yesterday"
  return "Earlier"
}

/** Items under their day headings, headings in order, empty ones dropped. The
 * items keep the order they arrived in (newest first). */
export function groupByDay<T>(
  items: T[],
  at: (item: T) => string,
  now = new Date()
): { day: DayGroup; items: T[] }[] {
  const order: DayGroup[] = ["Today", "Yesterday", "Earlier"]
  const groups = new Map<DayGroup, T[]>(order.map((d) => [d, []]))
  for (const item of items) groups.get(dayGroup(at(item), now))!.push(item)
  return order
    .map((day) => ({ day, items: groups.get(day)! }))
    .filter((g) => g.items.length > 0)
}

const TITLE_LENGTH = 64

/** What a thread is called: its opening message, first line, cut short. A
 * triggered thread opens with the delivery envelope, which reads as what
 * fired rather than as JSON. */
export function threadTitle(opening: string | undefined): string {
  const text = (opening ?? "").trim()
  if (!text) return "New chat"
  const delivery = deliveryNoticeOf({
    key: "",
    role: "user",
    content: text,
    tools: [],
  })
  if (delivery) {
    const what = delivery.record.title || displayName(delivery.record.kind)
    return `When ${what} changed`
  }
  const line = text.split("\n")[0].trim()
  return line.length > TITLE_LENGTH
    ? line.slice(0, TITLE_LENGTH - 1).trimEnd() + "…"
    : line
}

/** Each thread's opening user message, from a page of user messages in turn
 * order: the first one seen per thread wins. */
export function openingMessages(
  messages: SubstrateRecord[]
): Map<string, string> {
  const out = new Map<string, string>()
  for (const message of messages) {
    const held = readReference(message.properties.thread)?.path
    if (!held) continue
    const id = held.slice(held.lastIndexOf("/") + 1)
    if (out.has(id)) continue
    const content = message.properties.content
    out.set(id, typeof content === "string" ? content : "")
  }
  return out
}

/** One row of the chats list. */
export interface ChatRow {
  thread: SubstrateRecord
  title: string
  agentId?: string
}

/** The loaded conversations as rows, titled off their opening messages. */
export function chatRows(
  threads: SubstrateRecord[],
  openings: Map<string, string>
): ChatRow[] {
  return threads.map((thread) => ({
    thread,
    title: threadTitle(openings.get(thread.id)),
    agentId: threadAgentId(thread),
  }))
}

// ── tools ──────────────────────────────────────────────────────────────────

/** One entry of an agent's `tools:` list, resolved. */
export interface AgentTool {
  /** The function's identity, `<authority>/<package>/<name>`. */
  function: string
  /** The model-facing name the transcript records: the alias, else the
   * function's local name. */
  name: string
  description?: string
}

/** An agent's tools, in declaration order. */
export function agentTools(agent: SubstrateRecord | undefined): AgentTool[] {
  const raw = agent?.properties.tools
  if (!Array.isArray(raw)) return []
  const out: AgentTool[] = []
  for (const item of raw) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue
    const entry = item as Record<string, unknown>
    const held = readReference(entry[TOOL_FUNCTION_FIELD])?.path
    if (!held) continue
    const fn = held.startsWith(FUNCTION_PREFIX)
      ? held.slice(FUNCTION_PREFIX.length)
      : held
    const alias = typeof entry.name === "string" ? entry.name : ""
    out.push({
      function: fn,
      name: alias || fn.slice(fn.lastIndexOf("/") + 1),
      description:
        typeof entry.description === "string" ? entry.description : undefined,
    })
  }
  return out
}

/** The agents an agent may hand work to, by record id. */
export function agentSubagents(agent: SubstrateRecord | undefined): string[] {
  const raw = agent?.properties.subagents
  if (!Array.isArray(raw)) return []
  const out: string[] = []
  for (const item of raw) {
    const held = readReference(item)?.path
    if (!held) continue
    const split = splitRecordPath(held)
    out.push(split && split.kind === AGENT_KIND ? split.id : held)
  }
  return out
}

/** A tool's name in everyday words. The host functions have fixed ones; any
 * other function reads as its local name, split into words. */
export function toolLabel(fn: string): string {
  switch (fn) {
    case HOST_FUNCTION_QUERY:
      return "Look things up"
    case HOST_FUNCTION_WRITE:
      return "Change your data"
    case HOST_FUNCTION_PROPOSE:
      return "Suggest changes"
    case HOST_FUNCTION_ASK:
      return "Ask you questions"
  }
  return agentName(fn)
}

/** The route params of a function's tool page. */
export function toolRoute(
  fn: string
): { authority: string; pkg: string; name: string } | undefined {
  const [authority, pkg, name, ...rest] = fn.split("/")
  if (!authority || !pkg || !name || rest.length) return undefined
  return { authority, pkg, name }
}

function parseJSON(raw: string | undefined): Record<string, unknown> {
  if (!raw) return {}
  try {
    const parsed: unknown = JSON.parse(raw)
    return typeof parsed === "object" && parsed !== null
      ? (parsed as Record<string, unknown>)
      : {}
  } catch {
    return {}
  }
}

/** A list of labels as a sentence: "Tasks", "Tasks and Notes",
 * "Tasks, Notes and People". */
export function joinWords(words: string[]): string {
  if (words.length <= 1) return words[0] ?? ""
  return `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`
}

function kindsOf(args: Record<string, unknown>): string[] {
  const filter = args.filter
  if (!filter || typeof filter !== "object") return []
  const kinds = (filter as Record<string, unknown>).kinds
  return Array.isArray(kinds)
    ? kinds.filter((k): k is string => typeof k === "string" && k.length > 0)
    : []
}

/** What a tool call did, in words, from the function behind its name.
 *
 * The transcript records the model-facing NAME only, so the caller resolves
 * it against the agent's own `tools:` (and `subagents:`) first; a name the
 * agent does not declare reads as itself. */
export function toolSummary(
  call: ToolCallView,
  opts: {
    /** The function the name resolves to on this agent, if any. */
    function?: string
    /** The sub-agent the name resolves to, if any. */
    subagent?: string
    /** The function's own description, for a function without fixed words. */
    description?: string
  } = {}
): string {
  const args = parseJSON(call.arguments)
  if (opts.subagent) return `Asked ${agentName(opts.subagent)}`
  switch (opts.function) {
    case HOST_FUNCTION_QUERY: {
      const q = typeof args.q === "string" ? args.q.trim() : ""
      const kinds = kindsOf(args)
      let words = "Looked things up"
      if (q) words = `Searched for “${q}”`
      else if (typeof args.kind === "string" && typeof args.id === "string")
        words = `Looked up one ${displayName(args.kind).toLowerCase()}`
      else if (kinds.length)
        words = `Looked through your ${joinWords(kinds.map((k) => displayPlural(k)))}`
      const found = parseJSON(call.output).records
      if (Array.isArray(found) && call.ok !== false) {
        words += found.length === 1 ? ", found 1" : `, found ${found.length}`
      }
      return words
    }
    case HOST_FUNCTION_PROPOSE:
      return "Suggested a change"
    case HOST_FUNCTION_WRITE: {
      const kind = typeof args.kind === "string" ? args.kind : ""
      const what = kind ? displayName(kind).toLowerCase() : "record"
      if (args.op === "put" || args.op === "create") return `Saved a ${what}`
      if (args.op === "delete") return `Deleted a ${what}`
      return kind ? `Changed a ${what}` : "Made a change"
    }
    case HOST_FUNCTION_ASK:
      return "Asked you some questions"
  }
  if (opts.description) return opts.description
  return `Used ${toolLabel(opts.function ?? call.name).toLowerCase()}`
}

// ── grants, in words ───────────────────────────────────────────────────────

function permissionsOf(agent: SubstrateRecord): Record<string, unknown> {
  const held = agent.properties.permissions
  return held && typeof held === "object" && !Array.isArray(held)
    ? (held as Record<string, unknown>)
    : {}
}

function grantKinds(raw: unknown): string[] {
  if (!Array.isArray(raw)) return []
  const out: string[] = []
  for (const item of raw) {
    const named = valueIdentity(item)
    if (named) out.push(named)
  }
  return out
}

/** A grant's kinds as words: a bare `*` is everything, a glob is a package or
 * an authority, an exact kind is its display plural. */
function grantWords(kinds: string[]): string {
  if (kinds.some((k) => k === "*")) return "All your data"
  const words = kinds.map((k) => {
    if (!k.endsWith("/*")) return displayPlural(k)
    const parts = k.slice(0, -2).split("/")
    return parts.length >= 2
      ? `everything in ${parts[1]}`
      : `everything from ${parts[0]}`
  })
  return capitalise(joinWords(words))
}

/** The kinds an agent may read, as words, or undefined when it may read
 * nothing (the query tool is withheld without the grant). */
export function canSee(agent: SubstrateRecord): string | undefined {
  const reads = permissionsOf(agent).reads
  if (!reads || typeof reads !== "object" || Array.isArray(reads))
    return undefined
  const kinds = grantKinds((reads as Record<string, unknown>).kinds)
  return kinds.length ? grantWords(kinds) : undefined
}

/** What an agent may change, as one sentence. Holding the write tool with a
 * write grant is changing things itself; holding only `propose` is asking
 * first, every time. */
export function canChange(agent: SubstrateRecord): string {
  const tools = agentTools(agent).map((t) => t.function)
  const writes = grantKinds(permissionsOf(agent).writes).filter(
    (k) => k !== RECORD_PATCH_REQUEST_KIND
  )
  const writesItself = tools.includes(HOST_FUNCTION_WRITE) && writes.length > 0
  const proposes = tools.includes(HOST_FUNCTION_PROPOSE)
  if (writesItself && proposes) {
    return `${grantWords(writes)}. It suggests anything you should decide yourself.`
  }
  if (writesItself) return grantWords(writes)
  if (proposes) {
    return "Nothing on its own; it suggests changes and you approve each one"
  }
  // A function tool acts within its OWN grants, which this record does not
  // carry; the tool's page says what each may do.
  const host = [
    HOST_FUNCTION_QUERY,
    HOST_FUNCTION_WRITE,
    HOST_FUNCTION_PROPOSE,
    HOST_FUNCTION_ASK,
  ]
  if (tools.some((fn) => !host.includes(fn))) {
    return "Nothing directly. Its tools can, each within what it’s allowed to do"
  }
  return "Nothing"
}

/** A few openers for an empty chat, by what the agent can do. Generic on
 * purpose: they are hints, not a script. */
export function examplePrompts(agent: SubstrateRecord | undefined): string[] {
  if (!agent) return []
  const tools = agentTools(agent).map((t) => t.function)
  const out: string[] = []
  if (tools.includes(HOST_FUNCTION_QUERY)) {
    out.push("What’s due this week?", "What changed in my data today?")
  }
  if (tools.includes(HOST_FUNCTION_WRITE)) {
    out.push("Tidy up the titles of my recent notes")
  } else if (tools.includes(HOST_FUNCTION_PROPOSE)) {
    out.push("Is anything overdue that should be rescheduled?")
  }
  if (out.length === 0) out.push("What can you help me with?")
  return out.slice(0, 3)
}

// ── providers ──────────────────────────────────────────────────────────────

const PROVIDER_NAMES: Record<string, string> = {
  openai: "OpenAI",
  anthropic: "Anthropic",
  gemini: "Gemini",
  azure: "Azure",
}

/** The llm provider row an agent completes against, by id. */
export function agentProviderId(agent: SubstrateRecord): string | undefined {
  const held = readReference(agent.properties.provider)?.path
  if (!held) return undefined
  return held.slice(held.lastIndexOf("/") + 1)
}

/** A provider row's name as a person reads it. */
export function providerName(id: string): string {
  return PROVIDER_NAMES[id.toLowerCase()] ?? capitalise(id)
}

// ── one call, resolved ─────────────────────────────────────────────────────

/** What a call's model-facing name stands for on the agent that made it. */
export interface ResolvedTool {
  function?: string
  subagent?: string
  description?: string
}

export function resolveTool(
  agent: SubstrateRecord | undefined,
  name: string
): ResolvedTool {
  const tool = agentTools(agent).find((t) => t.name === name)
  if (tool) return { function: tool.function, description: tool.description }
  const subagent = agentSubagents(agent).find(
    (id) => id.slice(id.lastIndexOf("/") + 1) === name
  )
  if (subagent) return { subagent }
  // An agent the console could not read still names the host functions by
  // their own local names, which is what an unaliased entry records.
  const host = [
    HOST_FUNCTION_QUERY,
    HOST_FUNCTION_WRITE,
    HOST_FUNCTION_PROPOSE,
    HOST_FUNCTION_ASK,
  ].find((fn) => fn.slice(fn.lastIndexOf("/") + 1) === name)
  return host && !agent ? { function: host } : {}
}

/** The records a read answered with, as far as its payload names them. */
export function foundRecords(
  output: string | undefined,
  limit = 8
): { kind: string; id: string }[] {
  const records = parseJSON(output).records
  if (!Array.isArray(records)) return []
  const out: { kind: string; id: string }[] = []
  for (const item of records) {
    if (!item || typeof item !== "object") continue
    const r = item as Record<string, unknown>
    if (typeof r.kind === "string" && typeof r.id === "string") {
      out.push({ kind: r.kind, id: r.id })
    }
    if (out.length >= limit) break
  }
  return out
}

/** A failed call's reason, from the `{"error": …}` envelope a refusal
 * answers with. */
export function toolFailure(output: string | undefined): string | undefined {
  const parsed = parseJSON(output)
  return typeof parsed.error === "string" ? parsed.error : undefined
}

// ── a suggested change, in words ───────────────────────────────────────────

/** A property key as a label: `dueAt` → "Due", `assignee` → "Assignee",
 * `startedAt` → "Started". */
export function propertyLabel(key: string): string {
  const spaced = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[-_]+/g, " ")
    .toLowerCase()
    .replace(/ at$/, "")
  return capitalise(spaced.trim() || key)
}

const ISO_DATE = /^\d{4}-\d{2}-\d{2}(T[\d:.]+(Z|[+-]\d{2}:?\d{2})?)?$/

/** A proposed value as words, for anything that is not a reference (the
 * card renders those as records). Dates read as dates, empty as "Empty". */
export function valueWords(value: unknown): string {
  if (value === null || value === undefined || value === "") return "Empty"
  if (typeof value === "boolean") return value ? "Yes" : "No"
  if (typeof value === "string") {
    if (ISO_DATE.test(value)) {
      const t = new Date(value)
      if (!Number.isNaN(t.getTime())) {
        return t.toLocaleDateString(undefined, {
          day: "numeric",
          month: "short",
          year:
            t.getFullYear() === new Date().getFullYear()
              ? undefined
              : "numeric",
        })
      }
    }
    return value.length > 80 ? value.slice(0, 79) + "…" : value
  }
  if (Array.isArray(value)) return value.map(valueWords).join(", ")
  const text = JSON.stringify(value)
  return text.length > 80 ? text.slice(0, 79) + "…" : text
}

/** The properties a new record's heading is read from, in the order kinds
 * tend to declare them. */
const HEADING_KEYS = ["name", "title", "summary", "subject"]

/** What a new record would be called, from the values a create proposes. */
export function proposedHeading(
  properties: Record<string, unknown>
): { key: string; text: string } | undefined {
  for (const key of HEADING_KEYS) {
    const value = properties[key]
    if (typeof value === "string" && value.trim()) {
      return { key, text: value.trim() }
    }
  }
  return undefined
}
