/** The models an AI model row offers, and the one an agent runs on: the
 * console's copy of the engine's two rules (internal/engine/llmmodels.go,
 * decision record 0242), pure, so llm-models.test.ts holds each case the
 * server decides.
 *
 * RULE A, what a row offers. A row the operator serves offers exactly the
 * models the operator's slot lists, at prices that never reach the console.
 * Every other row offers each model its own deprecated `pricing` names and
 * each model a live llm/model record names for it. The row's own entry
 * prices its model exactly as before records existed (an absent rate is 0,
 * a later entry for the same model wins); a record prices a model no entry
 * names, and only with both base rates. The window resolves on its own: the
 * entry's where it states one, else the record's.
 *
 * RULE B, what a new thread of an agent on `runsOn` runs on: the first entry
 * whose record is live, whose row is live, and whose row holds a key of its
 * own or is served by the operator with the model on its list. Readiness
 * never moves it: a key the vendor refused, an open alert or Needs attention
 * stop a run on the server and never pass an entry over. A continued thread
 * stays on the row and model it is stamped with.
 *
 * Model record ids are free: nothing here reads a row or a model out of one. */

import { tokenWords } from "@/lib/agent-runs"
import { providerHasKey } from "@/lib/api/agents"
import { readReference, type SubstrateRecord } from "@/lib/api/types"
import { LLM_MODEL_KIND, LLM_PACKAGE } from "@/lib/core-kinds"
import {
  modelName,
  type ModelKey,
  type OperatorWords,
} from "@/lib/operator-defaults"
import { andList } from "@/lib/providers"

const PROVIDER_KIND = `${LLM_PACKAGE}/provider`

/** The most models an agent's `runsOn` names (the agent loader's bound). */
export const RUNS_ON_MAX = 8

// ── reading the records ─────────────────────────────────────────────────────

/** The id a reference names under its pinned kind: the stored path less the
 * kind, or a bare id as authored (`vocabulary.ReferentID`). */
function referentId(value: unknown, pin: string): string {
  const path = readReference(value)?.path ?? ""
  return path.startsWith(`${pin}/`) ? path.slice(pin.length + 1) : path
}

/** The llm/provider row an agent names in `provider`, by id. An agent on
 * `runsOn` names none: agentRuns says what it runs on. */
export function agentProviderId(agent: SubstrateRecord): string | undefined {
  const held = readReference(agent.properties.provider)?.path
  if (!held) return undefined
  return held.slice(held.lastIndexOf("/") + 1)
}

/** The llm/model record ids an agent on `runsOn` names, best first;
 * undefined for an agent that says what it runs on another way. */
export function agentRunsOn(agent: SubstrateRecord): string[] | undefined {
  const held = agent.properties.runsOn
  if (!Array.isArray(held)) return undefined
  return held.map((value) => referentId(value, LLM_MODEL_KIND))
}

/** The value a write of `runsOn` stores for these record ids. */
export function runsOnValue(ids: readonly string[]): { ref: string }[] {
  return ids.map((id) => ({ ref: `${LLM_MODEL_KIND}/${id}` }))
}

/** One rate, as a price table stores it. */
export interface Rate {
  /** The digits as written: "0.20". */
  text: string
  value: number
}

/** What a model costs per 1M tokens: the two base rates always, a cache
 * rate only where the table states one. */
export interface Price {
  input: Rate
  output: Rate
  /** Absent: a tenth of the input rate. */
  cacheRead?: Rate
  /** Absent: 1.25 times the input rate, or twice it for a call that asked
   * for a one-hour cache. */
  cacheWrite?: Rate
}

const DECIMAL = /^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/

/** A stored price: exact digits as a string, or a bare number a repository
 * held at an older declaration kept (`priceFloat`). */
function rate(value: unknown): Rate | undefined {
  if (typeof value === "string")
    return DECIMAL.test(value)
      ? { text: value, value: Number(value) }
      : undefined
  if (typeof value === "number" && Number.isFinite(value))
    return { text: String(value), value }
  return undefined
}

const ZERO: Rate = { text: "0", value: 0 }

/** A price table's rates, as the engine reads a row's entry (`pricingRow`):
 * an absent or unreadable base rate is 0. */
function tablePrice(table: Record<string, unknown>): Price {
  const price: Price = {
    input: rate(table.inputPer1M) ?? ZERO,
    output: rate(table.outputPer1M) ?? ZERO,
  }
  const read = rate(table.cacheReadPer1M)
  const write = rate(table.cacheWritePer1M)
  if (read) price.cacheRead = read
  if (write) price.cacheWrite = write
  return price
}

/** A window in tokens: a positive number, whole (`anyFloat`, then int). */
function windowOf(value: unknown): number | undefined {
  if (typeof value !== "number" || !Number.isFinite(value)) return undefined
  const whole = Math.trunc(value)
  return whole > 0 ? whole : undefined
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function objectRows(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.filter(isObject) : []
}

/** One llm/model record, read. */
export interface ModelFacts {
  id: string
  /** The llm/provider row it names, by id. */
  row: string
  /** The model id that row's API takes. */
  model: string
  /** Its name as people read it. */
  label?: string
  /** Where its `pricing` states both base rates: a record without them
   * describes a model without pricing it. */
  price?: Price
  window?: number
}

export function modelFacts(record: SubstrateRecord): ModelFacts {
  const p = record.properties
  const facts: ModelFacts = {
    id: record.id,
    row: referentId(p.provider, PROVIDER_KIND),
    model: typeof p.model === "string" ? p.model : "",
  }
  if (typeof p.label === "string" && p.label.trim()) facts.label = p.label
  if (
    isObject(p.pricing) &&
    rate(p.pricing.inputPer1M) &&
    rate(p.pricing.outputPer1M)
  )
    facts.price = tablePrice(p.pricing)
  const window = windowOf(p.contextWindow)
  if (window) facts.window = window
  return facts
}

/** A model's name: its record's label, else its id. */
export function modelTitle(facts: Pick<ModelFacts, "label" | "model">) {
  return facts.label ?? facts.model
}

/** Two prices charge the same for every call: the base rates, and the cache
 * rates as each charges them, a stated one or its default ratio, for both
 * cache lifetimes. */
function samePrice(a: Price, b: Price): boolean {
  const charged = (p: Price) => [
    p.input.value,
    p.output.value,
    p.cacheRead?.value ?? p.input.value * 0.1,
    p.cacheWrite?.value ?? p.input.value * 1.25,
    p.cacheWrite?.value ?? p.input.value * 2,
  ]
  const x = charged(a)
  const y = charged(b)
  return x.every(
    (v, i) =>
      Math.abs(v - y[i]) <= 1e-9 * Math.max(1, Math.abs(v), Math.abs(y[i]))
  )
}

// ── rule A: what a row offers ───────────────────────────────────────────────

/** Where a model's price comes from. */
export type PriceSource = "operator" | "row" | "record"

export interface ModelOffer {
  /** The model id the row's API takes. */
  model: string
  /** The live llm/model record for this row and model, where there is one.
   * On a row the operator serves it names the model and prices nothing. */
  record?: ModelFacts
  /** Absent where nothing prices the model: its runs count tokens and cost
   * nothing. */
  source?: PriceSource
  /** The row's own entry's or the record's; never the operator's, which
   * stays on the server. */
  price?: Price
  window?: number
  /** The row's own entry prices the model differently from the record it
   * wins over. */
  shadowsRecord?: boolean
}

/** The live records of one row, first by id per model: the write keeps one
 * record per row and model, and one that predates it loses to the lower
 * id. */
function rowRecords(
  rowId: string,
  models: readonly SubstrateRecord[]
): Map<string, ModelFacts> {
  const out = new Map<string, ModelFacts>()
  const live = models
    .filter((r) => !r.deletedAt)
    .map(modelFacts)
    .filter((f) => f.row === rowId && f.model)
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
  for (const facts of live)
    if (!out.has(facts.model)) out.set(facts.model, facts)
  return out
}

/** The models one row offers, by model id. `served` is the operator's list
 * where the operator serves the row (its slot's source is the operator),
 * else undefined. */
export function rowOffers(
  row: SubstrateRecord,
  models: readonly SubstrateRecord[],
  served: readonly string[] | undefined
): ModelOffer[] {
  const records = rowRecords(row.id, models)
  if (served) {
    return served.map((model) => {
      const record = records.get(model)
      return record
        ? { model, record, source: "operator" }
        : { model, source: "operator" }
    })
  }
  const entries = new Map<string, { price: Price; window?: number }>()
  for (const entry of objectRows(row.properties.pricing)) {
    const model = typeof entry.model === "string" ? entry.model : ""
    if (!model) continue
    entries.set(model, {
      price: tablePrice(entry),
      window: windowOf(entry.contextWindow),
    })
  }
  const ids = [...new Set([...entries.keys(), ...records.keys()])].sort()
  return ids.map((model) => {
    const entry = entries.get(model)
    const record = records.get(model)
    const offer: ModelOffer = { model }
    if (record) offer.record = record
    if (entry) {
      offer.source = "row"
      offer.price = entry.price
      if (record?.price && !samePrice(entry.price, record.price))
        offer.shadowsRecord = true
    } else if (record?.price) {
      offer.source = "record"
      offer.price = record.price
    }
    const window = entry?.window ?? record?.window
    if (window) offer.window = window
    return offer
  })
}

/** The operator's list for a row it serves, else undefined: what rowOffers
 * takes as `served`. */
export function servedModels(
  key: Pick<ModelKey, "source" | "models">
): readonly string[] | undefined {
  return key.source === "operator" ? (key.models ?? []) : undefined
}

/** What one row offers of one model, where the row and the records are
 * read; undefined where the row does not offer it. */
export function offerFor(
  rowId: string,
  model: string,
  ctx: Pick<RunsOnContext, "rows" | "models" | "keyOf">
): ModelOffer | undefined {
  const row = ctx.rows?.find((r) => r.id === rowId)
  if (!row || !ctx.models) return undefined
  return rowOffers(row, ctx.models, servedModels(ctx.keyOf(row))).find(
    (offer) => offer.model === model
  )
}

/** The row's own `pricing` without its entries for one model: what "Use the
 * model record's price" writes, so the record prices the model from then
 * on. */
export function pricingWithout(row: SubstrateRecord, model: string): unknown[] {
  const held = row.properties.pricing
  return Array.isArray(held)
    ? held.filter((e) => !(isObject(e) && e.model === model))
    : []
}

/** "$4 input · $20 output", and a cache rate where the table states one. */
export function priceWords(price: Price): string {
  return [
    `$${price.input.text} input`,
    `$${price.output.text} output`,
    price.cacheRead && `$${price.cacheRead.text} cache reads`,
    price.cacheWrite && `$${price.cacheWrite.text} cache writes`,
  ]
    .filter(Boolean)
    .join(" · ")
}

/** "200k tokens". */
export function windowWords(window: number): string {
  return `${tokenWords(window)} tokens`
}

/** What a price source is called where a model lists it. */
export const PRICE_SOURCE_WORDS: Record<"row" | "record", string> = {
  row: "This row’s own price list",
  record: "Model record",
}

// ── rule B: what an agent runs on ───────────────────────────────────────────

/** Why an entry cannot take a new thread. */
export type Refusal =
  | { code: "no-record" }
  | { code: "no-row"; row: string }
  | { code: "no-key"; row: string }
  | { code: "not-served"; row: string; served: readonly string[] }

/** What rule B reads: each undefined until read. */
export interface RunsOnContext {
  /** The live llm/model records. */
  models: readonly SubstrateRecord[] | undefined
  /** The live llm/provider rows. */
  rows: readonly SubstrateRecord[] | undefined
  /** Where a row's key comes from now (useModelKeys' keyOf): `operator`
   * with the models it serves, `own`, `none`, or undefined while the
   * operator's read has not answered. */
  keyOf: (row: SubstrateRecord) => Pick<ModelKey, "source" | "models">
}

/** Whether a call for one model on one row can be sent: a live row with a
 * key of its own, or one the operator serves with the model on its list
 * (`modelAvailable`). Undefined while that cannot be told. */
export function modelAvailable(
  rowId: string,
  model: string,
  ctx: Pick<RunsOnContext, "rows" | "keyOf">
): { available?: boolean; refusal?: Refusal } {
  if (!ctx.rows) return {}
  const row = ctx.rows.find((r) => r.id === rowId && !r.deletedAt)
  if (!row) return { available: false, refusal: { code: "no-row", row: rowId } }
  if (providerHasKey(row)) return { available: true }
  const key = ctx.keyOf(row)
  if (key.source === undefined) return {}
  if (key.source !== "operator")
    return { available: false, refusal: { code: "no-key", row: rowId } }
  const served = key.models ?? []
  if (served.includes(model)) return { available: true }
  return {
    available: false,
    refusal: { code: "not-served", row: rowId, served },
  }
}

/** One `runsOn` entry, as a new thread would take it. */
export interface RunsOnEntry {
  /** The llm/model record id the agent names. */
  id: string
  /** The record, where it is live. */
  facts?: ModelFacts
  /** Whether a new thread can run on it now; undefined while that cannot be
   * told. */
  available?: boolean
  refusal?: Refusal
}

export interface RunsOnChoice {
  entries: RunsOnEntry[]
  /** The entry a new thread runs on: the first that can, or the first that
   * cannot be told yet ahead of it. */
  chosen?: RunsOnEntry
  /** Whether a new thread can start now: true when an entry can, false when
   * none can, undefined while one cannot be told and none can. */
  canStart?: boolean
  /** Where none can: the row a key of its own would open, the first
   * entry's whose record and row are live. */
  keyRow?: string
}

function entryOf(id: string, ctx: RunsOnContext): RunsOnEntry {
  if (!ctx.models) return { id }
  const record = ctx.models.find((r) => r.id === id && !r.deletedAt)
  if (!record) return { id, available: false, refusal: { code: "no-record" } }
  const facts = modelFacts(record)
  return { id, facts, ...modelAvailable(facts.row, facts.model, ctx) }
}

/** What a new thread of an agent on `runsOn` runs on (`chooseRunsOn`). */
export function chooseRunsOn(
  ids: readonly string[],
  ctx: RunsOnContext
): RunsOnChoice {
  const entries = ids.map((id) => entryOf(id, ctx))
  const chosen = entries.find((e) => e.available !== false)
  const choice: RunsOnChoice = { entries }
  if (chosen) choice.chosen = chosen
  if (entries.some((e) => e.available === true)) choice.canStart = true
  else if (entries.every((e) => e.available === false)) {
    choice.canStart = false
    const keyable = entries.find(
      (e) => e.refusal?.code === "no-key" || e.refusal?.code === "not-served"
    )
    if (keyable?.facts) choice.keyRow = keyable.facts.row
  }
  return choice
}

/** What a continued thread runs on: the entry naming the row and model it is
 * stamped with, and whether that can take a call now (`continuedRunsOn`). An
 * entry the agent no longer names is refused, whatever the others offer. */
export function continuedRunsOn(
  ids: readonly string[],
  stamp: { row: string; model: string },
  ctx: RunsOnContext
): { entry?: RunsOnEntry; available?: boolean; dropped?: boolean } {
  if (!ctx.models) return {}
  for (const id of ids) {
    const entry = entryOf(id, ctx)
    if (entry.facts?.row !== stamp.row || entry.facts.model !== stamp.model)
      continue
    return { entry, available: entry.available }
  }
  return { available: false, dropped: true }
}

/** What an agent runs on, as every surface that names it reads it. */
export interface AgentRuns {
  /** The llm/provider row it runs on, by id. */
  row?: string
  /** The model id it sends. */
  model?: string
  /** The model's name, where an llm/model record gives one. */
  label?: string
  /** Whether it can run now: undefined while that cannot be told. */
  canRun?: boolean
  /** The row a key of its own would let it run on, where it cannot. */
  keyRow?: string
  /** An agent on `runsOn`: each entry, and the one a new thread takes. */
  runsOn?: RunsOnChoice
}

/** What rule B reads, and whether a row's key resolves for an agent that
 * names its provider (useModelKeys' resolves): that agent is read as it was
 * before `runsOn`. */
export interface AgentRunsContext extends RunsOnContext {
  resolves: (rowId: string | undefined) => boolean | undefined
}

/** What a new thread of an agent runs on: its `provider` and `model`, or
 * the entry rule B chooses, or, where none can run, the entry a key would
 * open. An agent on `providerInput` names neither here. */
export function agentRuns(
  agent: SubstrateRecord,
  ctx: AgentRunsContext
): AgentRuns {
  const ids = agentRunsOn(agent)
  if (!ids) {
    const row = agentProviderId(agent)
    const model = agent.properties.model
    const runs: AgentRuns = {}
    if (row) runs.row = row
    if (typeof model === "string" && model) runs.model = model
    const canRun = row ? ctx.resolves(row) : undefined
    if (canRun !== undefined) runs.canRun = canRun
    if (canRun === false) runs.keyRow = row
    return runs
  }
  const choice = chooseRunsOn(ids, ctx)
  const runs: AgentRuns = { runsOn: choice }
  const shown =
    choice.chosen ??
    choice.entries.find((e) => e.facts && e.facts.row === choice.keyRow) ??
    choice.entries.find((e) => e.facts)
  if (shown?.facts) {
    runs.row = shown.facts.row
    runs.model = shown.facts.model
    if (shown.facts.label) runs.label = shown.facts.label
  }
  if (choice.canStart !== undefined) runs.canRun = choice.canStart
  if (choice.keyRow) runs.keyRow = choice.keyRow
  return runs
}

/** What the next turn of a conversation runs on: a new chat's is the
 * agent's; a continued thread of an agent on `runsOn` stays on the row and
 * model it is stamped with, and its key is that row's. */
export function threadRuns(
  agent: SubstrateRecord,
  thread: SubstrateRecord | undefined,
  threadId: string,
  ctx: AgentRunsContext
): Pick<AgentRuns, "row" | "canRun" | "keyRow"> {
  const ids = agentRunsOn(agent)
  if (!threadId || !ids) return agentRuns(agent, ctx)
  if (!thread) return {}
  const row = referentId(thread.properties.provider, PROVIDER_KIND)
  const model = thread.properties.model
  if (!row || typeof model !== "string") return {}
  const next = continuedRunsOn(ids, { row, model }, ctx)
  const runs: Pick<AgentRuns, "row" | "canRun" | "keyRow"> = { row }
  if (next.available !== undefined) runs.canRun = next.available
  const refusal = next.entry?.refusal
  if (refusal?.code === "no-key" || refusal?.code === "not-served")
    runs.keyRow = row
  return runs
}

// ── words ───────────────────────────────────────────────────────────────────

/** A row's name: its card's, where the row is read, else its id's. */
export function rowName(
  rows: readonly SubstrateRecord[] | undefined,
  rowId: string
): string {
  const row = rows?.find((r) => r.id === rowId)
  return modelName(row ?? { id: rowId, properties: {} })
}

/** Why an entry cannot take a new thread, in words. */
export function refusalWords(
  refusal: Refusal,
  rows: readonly SubstrateRecord[] | undefined,
  words: OperatorWords
): string {
  switch (refusal.code) {
    case "no-record":
      return "This model record was deleted, or never existed."
    case "no-row":
      return `Its provider, ${rowName(rows, refusal.row)}, doesn’t exist.`
    case "no-key":
      return `${rowName(rows, refusal.row)} has no API key.`
    case "not-served":
      return refusal.served.length
        ? `${words.Name} serves only ${andList([...refusal.served])} on ${rowName(rows, refusal.row)}.`
        : `${words.Name} serves no model on ${rowName(rows, refusal.row)}.`
  }
}
