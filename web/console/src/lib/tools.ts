/** Tools: the everyday word for functions. Everything here is pure: it reads
 * function, agent, trigger, trigger-run and tool-message records and answers
 * what the Tools pages say about them — a readable name, where a tool comes
 * from, who uses it, when it runs, what it may touch, and how its last run
 * went. The function reference stays the identity; every name here is a
 * label. */

import { CORE_AUTHORITY, CORE_PACKAGE, splitKind } from "@/lib/api/http"
import type { KindInfo, SubstrateRecord, TriggerStatus } from "@/lib/api/types"
import {
  PROVIDERS_AUTHORITY,
  actorIdentity,
  providerInfo,
  type ProviderInfo,
} from "@/lib/actor-identity"
import { firstClause } from "@/lib/kind-copy"
import {
  displayName,
  displayPlural,
  lowerFirst,
  packageDisplayName,
  splitWords,
} from "@/lib/kind-names"

export const FUNCTION_KIND = `${CORE_PACKAGE}/function`
export const TRIGGER_KIND = `${CORE_PACKAGE}/trigger`
export const TRIGGER_RUN_KIND = `${CORE_PACKAGE}/triggerrun`
export const AGENT_KIND = `${CORE_PACKAGE}/agent`
export const KIND_KIND = `${CORE_PACKAGE}/kind`

/** The four host functions core ships. */
export const HOST_QUERY = `${CORE_PACKAGE}/query`
export const HOST_WRITE = `${CORE_PACKAGE}/write`
export const HOST_PROPOSE = `${CORE_PACKAGE}/propose`
export const HOST_ASK = `${CORE_PACKAGE}/ask`

// ── references ──────────────────────────────────────────────────────────────

/** The id a stored reference points at: `{ref: "<kind>/<id>"}` (or the bare
 * path) minus the kind's three segments. A kind's id is itself a reference
 * with slashes, so only the first three segments are the kind. */
export function refId(value: unknown): string | undefined {
  const path =
    typeof value === "string"
      ? value
      : value && typeof value === "object" && "ref" in value
        ? (value as { ref?: unknown }).ref
        : undefined
  if (typeof path !== "string" || !path) return undefined
  const parts = path.split("/")
  if (parts.length < 4) return undefined
  return parts.slice(3).join("/")
}

function refIds(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map(refId).filter((v): v is string => Boolean(v))
}

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === "string" && v !== "")
    : []
}

function capitalise(text: string): string {
  return text ? text[0].toUpperCase() + text.slice(1) : text
}

// ── the model ───────────────────────────────────────────────────────────────

export type ToolOrigin =
  | { kind: "core" }
  | { kind: "provider"; provider: ProviderInfo; bundle: string }
  | { kind: "yours" }
  | { kind: "other"; authority: string }

export type TriggerSourceArm =
  | { arm: "schedule"; recurrence: string; timezone?: string }
  | { arm: "record"; kinds: string[]; ops: string[]; when: string }
  | { arm: "webhook" }
  | { arm: "unknown" }

/** One trigger that invokes a tool. */
export interface ToolTrigger {
  id: string
  enabled: boolean
  source: TriggerSourceArm
}

/** One agent that lists the tool, and the name its model calls it by. */
export interface ToolUse {
  agent: string
  name: string
}

export interface ToolArgument {
  name: string
  type: string
  repeated: boolean
  required: boolean
  description: string
  values: string[]
}

export interface Tool {
  /** The function reference, `<authority>/<package>/<name>`. */
  ref: string
  authority: string
  pkg: string
  name: string
  record: SubstrateRecord
  runtime: string
  description: string
  origin: ToolOrigin
  uses: ToolUse[]
  triggers: ToolTrigger[]
  arguments: ToolArgument[]
  returns: ToolArgument[]
}

export function originOf(
  authority: string,
  pkg: string,
  repository?: string | null
): ToolOrigin {
  if (authority === CORE_AUTHORITY) return { kind: "core" }
  if (authority === PROVIDERS_AUTHORITY) {
    return {
      kind: "provider",
      provider: providerInfo(pkg),
      bundle: `${authority}/${pkg}`,
    }
  }
  if (repository && authority === repository) return { kind: "yours" }
  return { kind: "other", authority }
}

function argumentsOf(value: unknown): ToolArgument[] {
  if (!Array.isArray(value)) return []
  const out: ToolArgument[] = []
  for (const raw of value) {
    if (!raw || typeof raw !== "object") continue
    const a = raw as Record<string, unknown>
    if (typeof a.name !== "string") continue
    out.push({
      name: a.name,
      type: typeof a.type === "string" ? a.type : "json",
      repeated: a.repeated === true,
      required: a.required === true,
      description: typeof a.description === "string" ? a.description : "",
      values: strings(a.values),
    })
  }
  return out
}

/** A trigger record's source arm. */
export function triggerSource(record: SubstrateRecord): TriggerSourceArm {
  const source = record.properties.source as Record<string, unknown> | undefined
  if (!source || typeof source !== "object") return { arm: "unknown" }
  const schedule = source.schedule as Record<string, unknown> | undefined
  if (schedule && typeof schedule === "object") {
    return {
      arm: "schedule",
      recurrence:
        typeof schedule.recurrence === "string" ? schedule.recurrence : "",
      timezone:
        typeof schedule.timezone === "string" ? schedule.timezone : undefined,
    }
  }
  const rec = source.record as Record<string, unknown> | undefined
  if (rec && typeof rec === "object") {
    return {
      arm: "record",
      kinds: strings(rec.kinds),
      ops: strings(rec.ops),
      when: typeof rec.when === "string" ? rec.when : "",
    }
  }
  if (source.webhook && typeof source.webhook === "object")
    return { arm: "webhook" }
  return { arm: "unknown" }
}

/** Every tool, joined with the agents that list it and the triggers that
 * invoke it, ordered by the name a person reads. */
export function buildTools(
  functions: SubstrateRecord[],
  agents: SubstrateRecord[],
  triggers: SubstrateRecord[],
  repository?: string | null
): Tool[] {
  const uses = new Map<string, ToolUse[]>()
  for (const agent of agents) {
    const tools = agent.properties.tools
    if (!Array.isArray(tools)) continue
    for (const entry of tools) {
      if (!entry || typeof entry !== "object") continue
      const e = entry as Record<string, unknown>
      const ref = refId(e.function)
      if (!ref) continue
      const alias = typeof e.name === "string" && e.name ? e.name : undefined
      const list = uses.get(ref) ?? []
      list.push({ agent: agent.id, name: alias ?? splitKind(ref).name })
      uses.set(ref, list)
    }
  }
  const bound = new Map<string, ToolTrigger[]>()
  for (const t of triggers) {
    const callable = t.properties.callable as { ref?: string } | undefined
    const path = typeof callable === "string" ? callable : callable?.ref
    if (typeof path !== "string" || !path.startsWith(`${FUNCTION_KIND}/`))
      continue
    const ref = path.slice(FUNCTION_KIND.length + 1)
    const list = bound.get(ref) ?? []
    list.push({
      id: t.id,
      enabled: t.properties.enabled !== false,
      source: triggerSource(t),
    })
    bound.set(ref, list)
  }
  const tools = functions.map((record): Tool => {
    const { authority, pkg, name } = splitKind(record.id)
    const p = record.properties
    return {
      ref: record.id,
      authority,
      pkg,
      name,
      record,
      runtime: typeof p.runtime === "string" ? p.runtime : "",
      description: typeof p.description === "string" ? p.description : "",
      origin: originOf(authority, pkg, repository),
      uses: uses.get(record.id) ?? [],
      triggers: (bound.get(record.id) ?? []).sort((a, b) =>
        a.id.localeCompare(b.id)
      ),
      arguments: argumentsOf(p.arguments),
      returns: argumentsOf(p.returns),
    }
  })
  return tools.sort(
    (a, b) =>
      toolName(a).localeCompare(toolName(b)) || a.ref.localeCompare(b.ref)
  )
}

// ── names and icons ─────────────────────────────────────────────────────────

/** What core's host functions are called, in words. `ask` asks the person,
 * not another agent: it lands a batch of questions and the answer arrives in
 * a later turn. */
const HOST_NAMES: Record<string, string> = {
  [HOST_QUERY]: "Look things up",
  [HOST_PROPOSE]: "Suggest a change",
  [HOST_WRITE]: "Make a change",
  [HOST_ASK]: "Ask you a question",
}

/** Verbs a function name commonly opens with; the kind-name word list has
 * nouns only. */
const VERBS = [
  "archive",
  "classify",
  "count",
  "create",
  "delete",
  "export",
  "fetch",
  "find",
  "get",
  "import",
  "list",
  "make",
  "mark",
  "merge",
  "save",
  "search",
  "send",
  "set",
  "sync",
  "summarize",
  "tag",
  "update",
]

/** A function's local name in words: `savenote` → ["save", "note"],
 * `noteTitle` → ["note", "title"]. A name that does not split reads whole. */
export function nameWords(name: string): string[] {
  const cased = name
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .toLowerCase()
    .split(/[\s_-]+/)
    .filter(Boolean)
  if (cased.length > 1) return cased
  const word = cased[0] ?? name
  const verb = VERBS.filter(
    (v) => word.startsWith(v) && word.length > v.length
  ).sort((a, b) => b.length - a.length)[0]
  if (verb) {
    const rest = word.slice(verb.length)
    return [verb, ...(splitWords(rest) ?? [rest])]
  }
  return splitWords(word) ?? [word]
}

/** The name a person reads: core's four in words, a provider's sync named as
 * the History page names its actor ("Google Calendar sync"), anything else
 * its local name split into words ("Save note"). */
export function toolName(tool: Pick<Tool, "ref"> | string): string {
  const ref = typeof tool === "string" ? tool : tool.ref
  const host = HOST_NAMES[ref]
  if (host) return host
  const { authority, pkg, name } = splitKind(ref)
  if (authority === PROVIDERS_AUTHORITY) {
    return actorIdentity(`function:${authority}:${pkg}:${name}`).name
  }
  return capitalise(nameWords(name).join(" "))
}

export type ToolIconName =
  | "search"
  | "pencil"
  | "message-circle-question"
  | "calendar"
  | "at-sign"
  | "mail"
  | "hard-drive"
  | "file-text"
  | "hash"
  | "refresh"
  | "wrench"

/** The tile icon, from what the name says the tool does. */
export function toolIconName(ref: string): ToolIconName {
  switch (ref) {
    case HOST_QUERY:
      return "search"
    case HOST_WRITE:
    case HOST_PROPOSE:
      return "pencil"
    case HOST_ASK:
      return "message-circle-question"
  }
  const name = splitKind(ref).name.toLowerCase()
  const rules: [RegExp, ToolIconName][] = [
    [/calendar|event/, "calendar"],
    [/contact|people|person/, "at-sign"],
    [/gmail|mail|email|message/, "mail"],
    [/drive|file/, "hard-drive"],
    [/note|page|doc/, "file-text"],
    [/stat|count/, "hash"],
    [/query|search|find|lookup/, "search"],
    [/write|propose|edit|update|set/, "pencil"],
    [/ask/, "message-circle-question"],
    [/sync|import|fetch/, "refresh"],
  ]
  for (const [pattern, icon] of rules) if (pattern.test(name)) return icon
  return "wrench"
}

/** What core's host functions do, in everyday words; their declarations
 * are written for the model. */
const HOST_DESCRIPTIONS: Record<string, string> = {
  [HOST_QUERY]:
    "Finds and reads records: a search, a filtered list, or one record by name.",
  [HOST_PROPOSE]:
    "Lets an agent suggest a new record, a change or a deletion. Nothing happens until you approve it.",
  [HOST_WRITE]:
    "Lets an agent change records directly, within what that agent may change.",
  [HOST_ASK]:
    "Lets an agent ask you a few questions and carry on once you answer.",
}

/** The line a card and a page lede show: the description's first clause, as
 * a kind's everyday line is (the full text is one section down the page). */
export function toolDescription(
  tool: Pick<Tool, "ref" | "description">
): string {
  return HOST_DESCRIPTIONS[tool.ref] ?? firstClause(tool.description)
}

// ── groups ──────────────────────────────────────────────────────────────────

export type ToolGroupKey = "agents" | "syncs" | "other" | "builtin"

export interface ToolGroup {
  key: ToolGroupKey
  title: string
  hint: string
  tools: Tool[]
}

const GROUPS: Record<ToolGroupKey, { title: string; hint: string }> = {
  agents: {
    title: "Tools your agents use",
    hint: "What an agent can reach for while it works with you.",
  },
  syncs: {
    title: "Syncs",
    hint: "Bring data in from your providers, on a schedule or when something changes.",
  },
  other: {
    title: "Other tools",
    hint: "Nothing uses these yet.",
  },
  builtin: {
    title: "Built in",
    hint: "Come with substrate. Your agents use them to read and suggest changes.",
  },
}

export function toolGroupOf(tool: Tool, technical: boolean): ToolGroupKey {
  if (!technical && tool.origin.kind === "core") return "builtin"
  if (tool.uses.length) return "agents"
  if (tool.triggers.length) return "syncs"
  return "other"
}

/** The page's groups, in reading order, empty ones left out. Everyday mode
 * gathers core's host functions at the end; technical mode files them with
 * everything else. */
export function groupTools(tools: Tool[], technical: boolean): ToolGroup[] {
  const order: ToolGroupKey[] = ["agents", "syncs", "other", "builtin"]
  return order
    .map((key) => ({
      key,
      ...GROUPS[key],
      tools: tools.filter((t) => toolGroupOf(t, technical) === key),
    }))
    .filter((g) => g.tools.length)
}

// ── when it runs ────────────────────────────────────────────────────────────

const WEEKDAYS: Record<string, string> = {
  MO: "Monday",
  TU: "Tuesday",
  WE: "Wednesday",
  TH: "Thursday",
  FR: "Friday",
  SA: "Saturday",
  SU: "Sunday",
}

const UNITS: Record<string, [string, string]> = {
  SECONDLY: ["second", "seconds"],
  MINUTELY: ["minute", "minutes"],
  HOURLY: ["hour", "hours"],
  DAILY: ["day", "days"],
  WEEKLY: ["week", "weeks"],
  MONTHLY: ["month", "months"],
  YEARLY: ["year", "years"],
}

/** An RRULE in words: `FREQ=HOURLY` → "Every hour", `FREQ=MINUTELY;
 * INTERVAL=15` → "Every 15 minutes", `FREQ=WEEKLY;BYDAY=MO` → "Every
 * Monday". A rule the words cannot say reads "On a schedule". */
export function cadenceWords(recurrence: string): string {
  const parts = new Map<string, string>()
  for (const piece of recurrence.replace(/^RRULE:/i, "").split(";")) {
    const [k, v] = piece.split("=")
    if (k && v !== undefined) parts.set(k.trim().toUpperCase(), v.trim())
  }
  const freq = parts.get("FREQ")?.toUpperCase()
  const unit = freq ? UNITS[freq] : undefined
  if (!unit) return "On a schedule"
  const interval = Number(parts.get("INTERVAL") ?? "1") || 1
  let words =
    interval === 1 ? `Every ${unit[0]}` : `Every ${interval} ${unit[1]}`
  const byday = parts.get("BYDAY")
  if (freq === "WEEKLY" && byday && interval === 1) {
    const days = byday
      .split(",")
      .map((d) => WEEKDAYS[d.slice(-2).toUpperCase()])
      .filter(Boolean)
    if (days.length) words = `Every ${listWords(days)}`
  }
  const hour = parts.get("BYHOUR")
  if (hour && /^\d+$/.test(hour) && (freq === "DAILY" || freq === "WEEKLY")) {
    const minute = parts.get("BYMINUTE")
    const mm = minute && /^\d+$/.test(minute) ? minute.padStart(2, "0") : "00"
    words += ` at ${hour.padStart(2, "0")}:${mm}`
  }
  return words
}

/** "A, B and C". */
export function listWords(items: string[]): string {
  if (items.length <= 1) return items[0] ?? ""
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`
}

/** A list capped at `max`, the rest counted: "A, B, C and 3 more". */
export function cappedListWords(items: string[], max = 4): string {
  if (items.length <= max) return listWords(items)
  return `${items.slice(0, max).join(", ")} and ${items.length - max} more`
}

export type KindLabeler = (ref: string) => string

/** The default kind label: its display plural, off the registry entry when
 * one is known. */
export function kindLabeler(kinds?: KindInfo[]): KindLabeler {
  const byRef = new Map((kinds ?? []).map((k) => [k.identity, k]))
  return (ref) => {
    const k = byRef.get(ref)
    return displayPlural(k ?? ref)
  }
}

/** A kind entry of a grant or a selector in words: a kind by its plural, a
 * glob by what it covers. */
export function kindPatternWords(pattern: string, label: KindLabeler): string {
  if (pattern === "*") return "Everything"
  if (pattern.endsWith("/*")) {
    const [authority, pkg] = pattern.slice(0, -2).split("/")
    if (authority === PROVIDERS_AUTHORITY && pkg)
      return `Everything from ${providerInfo(pkg).name}`
    if (authority === CORE_AUTHORITY)
      return pkg ? `Everything built into ${pkg}` : "Everything built in"
    return pkg
      ? `Everything in ${packageDisplayName(pkg)}`
      : `Everything from ${authority}`
  }
  return label(pattern)
}

export type StartKind = "agent" | "schedule" | "change" | "webhook" | "you"

export interface ToolStart {
  kind: StartKind
  /** "By an agent", "On a schedule". */
  label: string
  /** "Every hour", "When Google accounts change". */
  detail: string
  /** The trigger behind a schedule, change or webhook start. */
  trigger?: string
  enabled: boolean
}

const CHANGE_OPS: Record<string, string> = {
  create: "are added",
  update: "change",
  delete: "are removed",
}

function changeWords(
  kinds: string[],
  ops: string[],
  label: KindLabeler
): string {
  const what = kinds.length
    ? listWords(kinds.map((k) => kindPatternWords(k, label)))
    : "Records"
  const verbs = ops.length && ops.length < 3 ? ops : ["update"]
  const how =
    verbs.length === 2 && verbs.includes("create") && verbs.includes("update")
      ? "are added or change"
      : listWords(verbs.map((o) => CHANGE_OPS[o] ?? o))
  return `When ${lowerFirst(what)} ${how}`
}

/** A record trigger the owner fires by asking for a sync: its guard reads
 * the sync trait's request stamp, or its id says so the way the shipped
 * providers name them. */
function onRequest(t: ToolTrigger): boolean {
  return (
    t.source.arm === "record" &&
    (t.source.when.includes("syncRequestedAt") ||
      /-on-(request|demand)$/.test(t.id))
  )
}

/** A record trigger that runs once when an account is first connected. */
function onConnect(t: ToolTrigger): boolean {
  return t.source.arm === "record" && /-on-connect$/.test(t.id)
}

function article(word: string): string {
  return /^[aeiou]/i.test(word) ? "an" : "a"
}

/** The rows of "When it runs": by an agent, on each trigger, and by you
 * where a direct run is offered. Two triggers that read the same are one
 * row. */
export function toolStarts(tool: Tool, label: KindLabeler): ToolStart[] {
  const out: ToolStart[] = []
  const push = (s: ToolStart) => {
    const same = out.find((o) => o.kind === s.kind && o.detail === s.detail)
    if (same) same.enabled = same.enabled || s.enabled
    else out.push(s)
  }
  if (tool.uses.length) {
    push({
      kind: "agent",
      label: "By an agent",
      detail: "When an agent needs it",
      enabled: true,
    })
  }
  for (const t of tool.triggers) {
    const base = { trigger: t.id, enabled: t.enabled }
    switch (t.source.arm) {
      case "schedule":
        push({
          ...base,
          kind: "schedule",
          label: "On a schedule",
          detail: cadenceWords(t.source.recurrence),
        })
        break
      case "record": {
        if (onRequest(t)) {
          push({
            ...base,
            kind: "you",
            label: "By you",
            detail: "When you press Sync now",
          })
          break
        }
        const only = t.source.kinds.length === 1 ? t.source.kinds[0] : ""
        if (onConnect(t) && only && !only.endsWith("*")) {
          const noun = lowerFirst(displayName(only))
          push({
            ...base,
            kind: "change",
            label: "On a change",
            detail: `When ${article(noun)} ${noun} is first connected`,
          })
          break
        }
        push({
          ...base,
          kind: "change",
          label: "On a change",
          detail: changeWords(t.source.kinds, t.source.ops, label),
        })
        break
      }
      case "webhook":
        push({
          ...base,
          kind: "webhook",
          label: "From the internet",
          detail: "When another service calls its web address",
        })
        break
      default:
        break
    }
  }
  if (canTryIt(tool)) {
    push({
      kind: "you",
      label: "By you",
      detail: "When you run it here",
      enabled: true,
    })
  }
  return out
}

/** The one-line cadence a card shows for a tool no agent uses. */
export function cadenceSummary(tool: Tool, label: KindLabeler): string {
  const starts = toolStarts(tool, label).filter((s) => s.kind !== "you")
  const schedule = starts.find((s) => s.kind === "schedule")
  if (schedule) return schedule.detail
  if (starts[0]) return starts[0].detail
  return "Nothing runs it yet"
}

/** A sync: a tool some trigger invokes and no agent uses. */
export function isSync(tool: Tool): boolean {
  return tool.triggers.length > 0 && tool.uses.length === 0
}

/** Whether the call API runs this tool for you. Of the host functions only
 * `query` answers a direct call (the writes are bounded by a calling agent's
 * grant, which a direct call has none of); a sync is started with Sync now
 * instead, because its body reads the delivery a trigger hands it. */
export function canTryIt(tool: Tool): boolean {
  if (tool.runtime === "host") return tool.ref === HOST_QUERY
  return tool.runtime === "python" && !isSync(tool)
}

/** Every trigger of the tool is switched off. */
export function isPaused(tool: Tool): boolean {
  return tool.triggers.length > 0 && tool.triggers.every((t) => !t.enabled)
}

/** The kinds a tool's change triggers watch: the records whose presence
 * says the provider is set up. */
export function watchedKinds(tool: Tool): string[] {
  const out = new Set<string>()
  for (const t of tool.triggers)
    if (t.source.arm === "record") for (const k of t.source.kinds) out.add(k)
  return [...out]
}

// ── what it may do ──────────────────────────────────────────────────────────

export interface PermissionWords {
  /** Null reads as the card's "none" sentence. */
  sees: string | null
  changes: string | null
  internet: string | null
  asks: string | null
}

export const PERMISSION_NONE: Record<keyof PermissionWords, string> = {
  sees: "Only what it’s given",
  changes: "Nothing",
  internet: "No internet access",
  asks: "No other agents or tools",
}

const HOST_PERMISSIONS: Record<string, PermissionWords> = {
  [HOST_QUERY]: {
    sees: "Whatever the agent using it may see",
    changes: null,
    internet: null,
    asks: null,
  },
  [HOST_WRITE]: {
    sees: null,
    changes: "Whatever the agent using it may change",
    internet: null,
    asks: null,
  },
  [HOST_PROPOSE]: {
    sees: null,
    changes:
      "Nothing on its own. It suggests changes and you approve each one.",
    internet: null,
    asks: null,
  },
  [HOST_ASK]: {
    sees: null,
    changes: "Nothing. It writes down the questions for you.",
    internet: null,
    asks: "You. The answer comes back once you reply.",
  },
}

const MUTATION_WORDS: Record<string, string> = {
  merge: "can merge records",
  split: "can split records",
}

/** The four "What it’s allowed to do" sentences, off the declaration's
 * `permissions` (or, for core's host functions, what the docs say they are
 * held to: the calling agent's own grants). */
export function permissionWords(
  tool: Tool,
  label: KindLabeler,
  nameOf: (ref: string) => string = toolName
): PermissionWords {
  const host = HOST_PERMISSIONS[tool.ref]
  if (host) return host
  const perms = (tool.record.properties.permissions ?? {}) as Record<
    string,
    unknown
  >
  const reads = (perms.reads ?? {}) as Record<string, unknown>
  const readKinds = refIds(reads.kinds)
  const writeKinds = refIds(perms.writes)
  const mutations = strings(perms.mutations)
  const network = strings(perms.network)
  const calls = refIds(perms.call)
  const kindWords = (refs: string[]) =>
    cappedListWords(
      refs.map((k, i) => {
        const words = kindPatternWords(k, label)
        return i ? lowerFirst(words) : words
      })
    )
  let changes: string | null = writeKinds.length ? kindWords(writeKinds) : null
  if (mutations.length) {
    const m = listWords(mutations.map((x) => MUTATION_WORDS[x] ?? x))
    changes = changes ? `${changes}; ${m}` : capitalise(m)
  }
  return {
    sees: readKinds.length ? kindWords(readKinds) : null,
    changes,
    internet: network.length ? `Only ${cappedListWords(network, 3)}` : null,
    asks: calls.length ? cappedListWords(calls.map(nameOf)) : null,
  }
}

/** The declaration's `permissions` block as the YAML an author wrote,
 * references back to their plain spelling. */
export function permissionsYaml(tool: Tool): string {
  const perms = tool.record.properties.permissions as
    Record<string, unknown> | undefined
  if (!perms || !Object.keys(perms).length) {
    return tool.runtime === "host"
      ? "# none of its own: it is held to the calling agent's grants"
      : "{}  # it reads and writes nothing"
  }
  const lines: string[] = ["permissions:"]
  const list = (key: string, items: string[], indent = "  ") => {
    if (!items.length) return
    lines.push(`${indent}${key}:`)
    for (const i of items) lines.push(`${indent}  - ${i}`)
  }
  const reads = perms.reads as Record<string, unknown> | undefined
  if (reads) {
    lines.push("  reads:")
    list("kinds", refIds(reads.kinds), "    ")
    const budgets = reads.budgets as Record<string, unknown> | undefined
    if (budgets && Object.keys(budgets).length) {
      lines.push("    budgets:")
      for (const [k, v] of Object.entries(budgets))
        lines.push(`      ${k}: ${String(v)}`)
    }
  }
  list("writes", refIds(perms.writes))
  list("call", refIds(perms.call))
  list("network", strings(perms.network))
  list("mutations", strings(perms.mutations))
  return lines.join("\n")
}

// ── arguments ───────────────────────────────────────────────────────────────

const ARGUMENT_LABELS: Record<string, string> = {
  id: "ID",
  q: "Search words",
  first: "How many",
  after: "Continue after",
  orderBy: "Order",
  ifVersion: "Only if still at version",
  kind: "Kind",
}

/** A model-facing description as a person reads it: first letter up. */
export function sentence(text: string): string {
  const t = text.trim()
  return t ? t[0].toUpperCase() + t.slice(1) : t
}

/** An argument's label: `noteTitle` → "Note title", `id` → "ID". */
export function argumentLabel(name: string): string {
  const own = ARGUMENT_LABELS[name]
  if (own) return own
  const words = name
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .toLowerCase()
    .trim()
  return capitalise(words.replace(/\bid\b/g, "ID"))
}

/** An argument's type in words, for the technical column. */
export function argumentTypeWords(a: ToolArgument): string {
  const base =
    a.type === "enum" && a.values.length
      ? `enum(${a.values.join("|")})`
      : a.type
  return a.repeated ? `[${base}]` : base
}

export type FieldControl =
  "text" | "textarea" | "number" | "checkbox" | "select" | "json" | "list"

const LONG_TEXT =
  /^(text|body|content|prompt|markdown|message|question|source)$/i

/** The form control an argument is asked for with. */
export function fieldControl(a: ToolArgument): FieldControl {
  if (a.type === "json") return "json"
  if (a.repeated) return "list"
  switch (a.type) {
    case "int":
    case "float":
      return "number"
    case "bool":
      return "checkbox"
    case "enum":
      return "select"
    default:
      return LONG_TEXT.test(a.name) ? "textarea" : "text"
  }
}

export type FieldValue = string | boolean

/** The call's input from the form: empty fields are left out, numbers and
 * JSON parsed, a list one value per line. Errors are keyed by argument. */
export function buildCallInput(
  args: ToolArgument[],
  values: Record<string, FieldValue | undefined>
): { input: Record<string, unknown>; errors: Record<string, string> } {
  const input: Record<string, unknown> = {}
  const errors: Record<string, string> = {}
  for (const a of args) {
    const raw = values[a.name]
    const control = fieldControl(a)
    if (control === "checkbox") {
      if (raw === true) input[a.name] = true
      else if (a.required) input[a.name] = false
      continue
    }
    const text = typeof raw === "string" ? raw.trim() : ""
    if (!text) {
      if (a.required) errors[a.name] = "Needed"
      continue
    }
    switch (control) {
      case "number": {
        const n = Number(text)
        if (!Number.isFinite(n) || (a.type === "int" && !Number.isInteger(n)))
          errors[a.name] = a.type === "int" ? "A whole number" : "A number"
        else input[a.name] = n
        break
      }
      case "json":
        try {
          input[a.name] = JSON.parse(text)
        } catch {
          errors[a.name] = "Not valid JSON"
        }
        break
      case "list": {
        const items = text
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean)
        if (a.type === "int" || a.type === "float") {
          const nums = items.map(Number)
          if (nums.some((n) => !Number.isFinite(n)))
            errors[a.name] = "One number per line"
          else input[a.name] = nums
        } else input[a.name] = items
        break
      }
      default:
        input[a.name] = text
    }
  }
  return { input, errors }
}

/** A returned value in a few words, for the everyday result. */
export function valueWords(value: unknown): string {
  if (value === null || value === undefined) return "Nothing"
  if (typeof value === "boolean") return value ? "Yes" : "No"
  if (typeof value === "number") return value.toLocaleString()
  if (typeof value === "string")
    return value.length > 140 ? `${value.slice(0, 140)}…` : value
  if (Array.isArray(value))
    return value.length === 1
      ? "1 item"
      : `${value.length.toLocaleString()} items`
  const n = Object.keys(value as object).length
  return n === 1 ? "1 field" : `${n} fields`
}

/** The returned fields a person reads: each key labelled, each value in a
 * few words; a bare value is one row. */
export function outputRows(
  output: unknown
): { label: string; value: string }[] {
  if (output && typeof output === "object" && !Array.isArray(output)) {
    return Object.entries(output as Record<string, unknown>).map(([k, v]) => ({
      label: argumentLabel(k),
      value: valueWords(v),
    }))
  }
  if (output === null || output === undefined) return []
  return [{ label: "Gave back", value: valueWords(output) }]
}

// ── runs ────────────────────────────────────────────────────────────────────

export type RunStatus = "ok" | "skipped" | "trouble"

export interface ToolRun {
  key: string
  /** When it finished. */
  at: string
  by: "schedule" | "change" | "webhook" | "you" | "agent"
  /** The agent record id, on an agent's call. */
  agent?: string
  /** The trigger, on a trigger run. */
  trigger?: string
  happened: string
  tookMs?: number
  status: RunStatus
  /** The full reason on a run that had trouble. */
  reason?: string
  /** What the run was handed, as stored: a trigger run's delivery (the
   * change or fire it answered); absent on an agent's call, whose arguments
   * live on the assistant turn `call` names. */
  input?: Record<string, unknown>
  /** What it gave back, as stored: a trigger run's outcome, or the tool
   * message's result and the changes it wrote. */
  output?: Record<string, unknown>
  /** An agent's call: the thread and turn whose assistant row carries the
   * call's arguments, and the call's id there. */
  call?: { thread: string; turn?: number; id: string }
}

/** The stored keys of `from` that are set, in the order named. */
function present(
  from: Record<string, unknown>,
  keys: readonly string[]
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const k of keys) {
    const v = from[k]
    if (v !== undefined && v !== null && v !== "") out[k] = v
  }
  return out
}

/** A tool result as it was returned: the model's JSON parsed back where it
 * is JSON, else the text. */
function resultOf(content: unknown): unknown {
  if (typeof content !== "string") return content
  try {
    return JSON.parse(content) as unknown
  } catch {
    return content
  }
}

/** One call's arguments off the assistant turn that dispatched it: the
 * model's JSON parsed back, else the text; undefined when the turn does not
 * carry the call. */
export function callArguments(
  assistant: SubstrateRecord[],
  callId: string
): { found: boolean; value?: unknown } {
  for (const m of assistant) {
    const calls = m.properties.toolCalls
    if (!Array.isArray(calls)) continue
    for (const c of calls) {
      if (!c || typeof c !== "object") continue
      const call = c as Record<string, unknown>
      if (call.id !== callId) continue
      return { found: true, value: resultOf(call.arguments) }
    }
  }
  return { found: false }
}

const EFFECT_WORDS: Record<string, string> = {
  put: "saved",
  patch: "changed",
  delete: "removed",
  merge: "merged",
  split: "split",
}

/** An effects summary in words: `{put: 3, patch: 1}` → "3 saved, 1 changed". */
export function effectsWords(
  effects: Record<string, number> | undefined
): string {
  if (!effects) return ""
  return Object.entries(effects)
    .filter(([, n]) => typeof n === "number" && n > 0)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([k, n]) => `${n.toLocaleString()} ${EFFECT_WORDS[k] ?? k}`)
    .join(", ")
}

function durationBetween(start: unknown, end: unknown): number | undefined {
  if (typeof start !== "string" || typeof end !== "string") return undefined
  const ms = Date.parse(end) - Date.parse(start)
  return Number.isFinite(ms) && ms >= 0 ? ms : undefined
}

const MODE_BY: Record<string, ToolRun["by"]> = {
  schedule: "schedule",
  record: "change",
  webhook: "webhook",
  manual: "you",
}

/** One `triggerrun` record as a row of Recent runs. */
export function runFromTriggerRun(record: SubstrateRecord): ToolRun {
  const p = record.properties
  const status =
    p.status === "parked"
      ? "trouble"
      : p.status === "skipped"
        ? "skipped"
        : "ok"
  const reason = typeof p.reason === "string" && p.reason ? p.reason : undefined
  const effects = effectsWords(p.effects as Record<string, number> | undefined)
  const pages = typeof p.pages === "number" ? `, in ${p.pages} pages` : ""
  let happened: string
  if (status === "trouble") {
    happened = reason
      ? `Stopped: ${reason.split("\n")[0]}`
      : "Stopped after retrying"
  } else if (status === "skipped") {
    happened = "Nothing to do"
  } else {
    happened = effects ? `${capitalise(effects)}${pages}` : "Nothing changed"
  }
  const at =
    (typeof p.finishedAt === "string" && p.finishedAt) || record.updatedAt
  return {
    key: `run:${record.id}`,
    at,
    by: MODE_BY[String(p.mode)] ?? "change",
    trigger:
      refId(p.trigger) ??
      (typeof p.trigger === "string" ? p.trigger : undefined),
    happened,
    tookMs: durationBetween(p.startedAt, p.finishedAt),
    status,
    reason,
    input: present(p, ["mode", "seq", "record", "fireId", "attempt"]),
    output: present(p, ["status", "effects", "pages", "reason"]),
  }
}

/** One agent's tool-result message as a row of Recent runs. */
export function runFromToolMessage(
  message: SubstrateRecord,
  agent: string
): ToolRun {
  const p = message.properties
  const ok = p.ok !== false
  const changes = Array.isArray(p.changes) ? p.changes.length : 0
  const content = typeof p.content === "string" ? p.content : ""
  const thread = refId(p.thread)
  return {
    key: `msg:${message.id}`,
    at: message.createdAt,
    by: "agent",
    agent,
    happened: !ok
      ? `Failed${content ? `: ${firstLine(content)}` : ""}`
      : changes
        ? `Changed ${changes === 1 ? "1 record" : `${changes} records`}`
        : "Answered",
    status: ok ? "ok" : "trouble",
    reason: ok ? undefined : content || undefined,
    output: {
      ok,
      ...(content && { result: resultOf(content) }),
      ...(changes > 0 && { changes: p.changes }),
    },
    call:
      thread && typeof p.toolCallId === "string" && p.toolCallId
        ? {
            thread,
            turn: typeof p.turn === "number" ? p.turn : undefined,
            id: p.toolCallId,
          }
        : undefined,
  }
}

function firstLine(text: string): string {
  const line = text.split("\n")[0]
  return line.length > 120 ? `${line.slice(0, 120)}…` : line
}

/** The tool-result messages that answer for this tool: the message's `name`
 * is the name the agent's model calls the tool by, and the thread's agent is
 * one that lists it under that name. */
export function toolMessageRuns(
  tool: Tool,
  messages: SubstrateRecord[],
  threadAgent: (message: SubstrateRecord) => string | undefined
): ToolRun[] {
  const out: ToolRun[] = []
  for (const m of messages) {
    if (m.properties.role !== "tool") continue
    const agent = threadAgent(m)
    if (!agent) continue
    const name = m.properties.name
    if (tool.uses.some((u) => u.agent === agent && u.name === name))
      out.push(runFromToolMessage(m, agent))
  }
  return out
}

/** Newest first. */
export function sortRuns(runs: ToolRun[]): ToolRun[] {
  return [...runs].sort((a, b) => b.at.localeCompare(a.at))
}

/** A run's length: "0.4s", "12s", "2 min". */
export function tookWords(ms: number | undefined): string {
  if (ms === undefined) return "—"
  if (ms < 100) return "under 0.1s"
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`
  if (ms < 60_000) return `${Math.round(ms / 1000)}s`
  return `${Math.round(ms / 60_000)} min`
}

/** A past moment in words: "just now", "5 min ago", "3 hours ago",
 * "2 days ago", else the date. */
export function agoWords(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 60) return "just now"
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.floor(m / 60)
  if (h < 24) return h === 1 ? "1 hour ago" : `${h} hours ago`
  const d = Math.floor(h / 24)
  if (d < 14) return d === 1 ? "yesterday" : `${d} days ago`
  return new Date(t).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year:
      new Date(t).getFullYear() === new Date(now).getFullYear()
        ? undefined
        : "numeric",
  })
}

/** An ISO 8601 duration in words: `PT5S` → "5 seconds", `PT1M` → "1 minute". */
export function isoDurationWords(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  const m = /^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?$/.exec(value)
  if (!m) return value
  const part = (n: string | undefined, one: string, many: string) =>
    n && Number(n) ? `${n} ${Number(n) === 1 ? one : many}` : ""
  return (
    [
      part(m[1], "hour", "hours"),
      part(m[2], "minute", "minutes"),
      part(m[3], "second", "seconds"),
    ]
      .filter(Boolean)
      .join(" ") || value
  )
}

// ── trigger progress ────────────────────────────────────────────────────────

/** Where a trigger stands in the changelog, for technical mode: a record
 * source's cursor against the head and how far behind it is, the last fire
 * of a schedule or webhook, and what is parked or pending. */
export function triggerProgress(status: TriggerStatus, now?: number): string[] {
  const out: string[] = []
  if (status.kind === "record") {
    const at = status.cursor ?? status.head
    const lag = status.lag ?? 0
    out.push(
      `cursor #${at} of #${status.head}`,
      lag > 0 ? `${lag.toLocaleString()} behind` : "caught up"
    )
  } else if (status.lastFire) {
    out.push(`last fired ${agoWords(status.lastFire, now)}`)
  } else {
    out.push("not fired yet")
  }
  if (status.pending > 0) out.push(`${status.pending} pending`)
  if (status.parked > 0) out.push(`${status.parked} parked`)
  if (status.error) out.push(status.error)
  return out
}

// ── status ──────────────────────────────────────────────────────────────────

export type StatusTone = "ok" | "warn" | "neutral"

export interface ToolStatus {
  tone: StatusTone
  label: string
}

/** How often agents called a tool: the tool messages answering under its
 * names, counted by the server, and the newest of them. */
export interface ToolUsage {
  count: number
  latest?: ToolRun
}

/** The names a count of this tool's calls may filter the tool messages by:
 * every name an agent's model calls it by, provided no agent calls another
 * tool by any of them. A tool message carries the name alone, so a shared
 * name would count the other tool's calls as this one's. Undefined when no
 * agent lists the tool, or a name is shared. */
export function usageNames(tool: Tool, tools: Tool[]): string[] | undefined {
  if (!tool.uses.length) return undefined
  const names = [...new Set(tool.uses.map((u) => u.name))].sort()
  const shared = tools.some(
    (other) =>
      other.ref !== tool.ref && other.uses.some((u) => names.includes(u.name))
  )
  return shared ? undefined : names
}

/** The pill a tool carries: paused, waiting for its provider, the newest
 * run's outcome, how often agents used it, or never ran. Nothing where no
 * record would say: a tool no trigger calls and no agent lists runs only
 * when called directly, which leaves no run behind, and an agent's tool is
 * silent until its calls are counted. */
export function toolStatus(
  tool: Tool,
  latest: ToolRun | undefined,
  opts: { waitingFor?: ProviderInfo; usage?: ToolUsage; now?: number } = {}
): ToolStatus | undefined {
  if (isPaused(tool)) return { tone: "warn", label: "Paused" }
  if (latest?.status === "trouble")
    return { tone: "warn", label: "Had trouble" }
  const { usage } = opts
  if (usage && !tool.triggers.length) {
    const newest = [latest, usage.latest]
      .filter((r): r is ToolRun => Boolean(r))
      .sort((a, b) => b.at.localeCompare(a.at))[0]
    if (!usage.count && !newest)
      return { tone: "neutral", label: "Not used yet" }
    const times =
      usage.count === 1 ? "once" : `${usage.count.toLocaleString()} times`
    if (!usage.count || !newest)
      return {
        tone: "ok",
        label: newest
          ? `Ran ${agoWords(newest.at, opts.now)}`
          : `Used ${times}`,
      }
    return {
      tone: "ok",
      label: `Used ${times} · ${usage.count === 1 ? "" : "last "}${agoWords(newest.at, opts.now)}`,
    }
  }
  if (latest)
    return { tone: "ok", label: `Ran ${agoWords(latest.at, opts.now)}` }
  if (opts.waitingFor)
    return { tone: "warn", label: `Waiting for ${opts.waitingFor.name}` }
  if (!tool.triggers.length) return undefined
  return { tone: "neutral", label: "Never ran" }
}
