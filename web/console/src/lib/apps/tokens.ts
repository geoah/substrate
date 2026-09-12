/** The two binding tokens. A string inside `filter` or an action's `set` that
 * begins with `$input.` resolves against the app's inputs at request time:
 * `$input.<name>` is the bound record's `<kind>/<id>` path, and
 * `$input.<name>.<property>` is one property of it, the same single hop a
 * displayTemplate allows. `$record` is the clicked row's path and
 * `$record.<property>` one property of it; it is valid only where there IS a
 * row (a row-placed action, a detail's actions), never in a filter. A string
 * that begins with `$$` is the escape: it reads as the literal `$...` with one
 * dollar dropped. A token that cannot resolve is a Problem the screen renders
 * as its empty state, never a throw and never a write, because a view does
 * not know which apps mount it. */

import {
  readReference,
  type Cond,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import { splitKind } from "@/lib/api/http"
import { recordPath } from "@/lib/record-path"
import { parseValue, type PropSpec } from "@/lib/record-schema"
import { specOf } from "./cond"
import type { Problem, ViewContext, ViewSpec } from "./spec"

export const INPUT_PREFIX = "$input."
export const RECORD_TOKEN = "$record"
export const ESCAPE = "$$"

export type Token =
  | { scope: "input"; input: string; property?: string }
  | { scope: "record"; property?: string }

export function isEscaped(value: unknown): value is string {
  return typeof value === "string" && value.startsWith(ESCAPE)
}

/** The literal a `$$...` string stands for: one dollar dropped. */
export function unescape(value: string): string {
  return value.slice(1)
}

export function isToken(value: unknown): value is string {
  if (typeof value !== "string") return false
  return (
    value.startsWith(INPUT_PREFIX) ||
    value === RECORD_TOKEN ||
    value.startsWith(`${RECORD_TOKEN}.`)
  )
}

export function parseToken(token: string): Token | undefined {
  if (!isToken(token)) return undefined
  if (token === RECORD_TOKEN) return { scope: "record" }
  if (token.startsWith(`${RECORD_TOKEN}.`)) {
    const [property, ...rest] = token.slice(RECORD_TOKEN.length + 1).split(".")
    if (!property || rest.length) return undefined
    return { scope: "record", property }
  }
  const [input, property, ...rest] = token.slice(INPUT_PREFIX.length).split(".")
  if (!input || rest.length) return undefined
  return property
    ? { scope: "input", input, property }
    : { scope: "input", input }
}

function visitStrings(value: unknown, fn: (s: string) => void) {
  if (typeof value === "string") fn(value)
  else if (Array.isArray(value)) value.forEach((v) => visitStrings(v, fn))
  else if (value && typeof value === "object") {
    Object.values(value as Record<string, unknown>).forEach((v) =>
      visitStrings(v, fn)
    )
  }
}

/** Every input name a view's filter and actions refer to, so the composition
 * check can ask whether the app declares each one. */
export function tokenInputs(spec: ViewSpec): string[] {
  const names = new Set<string>()
  const collect = (s: string) => {
    const parsed = parseToken(s)
    if (parsed?.scope === "input") names.add(parsed.input)
  }
  visitStrings(spec.filter, collect)
  for (const action of spec.actions) visitStrings(action.set, collect)
  return [...names]
}

/** Whether a value carries a `$record` token anywhere inside it. */
export function usesRecord(value: unknown): boolean {
  let hit = false
  visitStrings(value, (s) => {
    if (parseToken(s)?.scope === "record") hit = true
  })
  return hit
}

export interface Resolved {
  value?: unknown
  problem?: string
}

/** A held value as a token yields it: a reference (`{ref}`, or a list of
 * them) as its path, because a filter compares a reference by its
 * `<kind>/<id>` path and the engine refuses the object; anything else as
 * itself. */
function tokenValue(held: unknown): unknown {
  if (Array.isArray(held)) return held.map(tokenValue)
  if (held && typeof held === "object") return readReference(held)?.path ?? held
  return held
}

/** One token against the context. The record's own path for a bare token,
 * one property for a hopped one. `record` is the clicked row, absent where
 * there is none. `use` is what the caller cannot do without the value
 * ("this screen cannot filter on it"), so the problem reads as a sentence
 * about the screen. */
export function resolveToken(
  token: string,
  ctx: ViewContext,
  record?: SubstrateRecord,
  use?: string
): Resolved {
  const parsed = parseToken(token)
  if (!parsed) return { problem: `${token} is not a token` }
  const so = use ? `, so ${use}` : ""
  let subject: SubstrateRecord | undefined
  let who: string
  if (parsed.scope === "record") {
    if (!record) {
      return { problem: `\`${token}\` needs a row; none is open here` }
    }
    subject = record
    who = "the row"
  } else {
    if (!(parsed.input in ctx.inputs)) {
      return { problem: `\`${token}\` names an input no app declares here` }
    }
    subject = ctx.inputs[parsed.input]
    who = `\`${parsed.input}\``
    if (!subject) {
      return { problem: `${who} has no record yet${so}` }
    }
  }
  if (!parsed.property) return { value: recordPath(subject.kind, subject.id) }
  const held = subject.properties[parsed.property]
  if (held === undefined || held === null || held === "") {
    const noun = splitKind(subject.kind).name || "record"
    return {
      problem: `${who} has no \`${parsed.property}\` yet${so} (the ${noun} has not synced)`,
    }
  }
  return { value: tokenValue(held) }
}

/** A declared string read as a value: the escape unwrapped, a token
 * resolved, anything else as itself. */
function literal(
  value: unknown,
  ctx: ViewContext,
  record: SubstrateRecord | undefined,
  at: string,
  problems: Problem[],
  use: string
): unknown {
  if (isEscaped(value)) return unescape(value)
  if (!isToken(value)) return value
  const r = resolveToken(value, ctx, record, use)
  if (r.problem) {
    problems.push({ path: at, message: r.problem, severity: "error" })
  }
  return r.value
}

/** The filter with every token replaced. A token that cannot resolve leaves
 * its problem and the filter is not to be sent. A filter never has a row, so
 * `$record` inside one is a problem by construction. */
export function substituteFilter(
  filter: RecordFilter,
  ctx: ViewContext,
  path = "filter"
): { filter: RecordFilter; problems: Problem[] } {
  const problems: Problem[] = []
  const one = (value: unknown, at: string) =>
    literal(
      value,
      ctx,
      undefined,
      at,
      problems,
      "this screen cannot filter on it"
    )
  const properties: Record<string, Cond> = {}
  for (const [name, cond] of Object.entries(filter.properties ?? {})) {
    const at = `${path}.properties.${name}`
    const out: Cond = { ...cond }
    if (cond.eq !== undefined) out.eq = one(cond.eq, `${at}.eq`)
    if (cond.in) out.in = cond.in.map((v, i) => one(v, `${at}.in[${i}]`))
    if (cond.contains !== undefined) {
      out.contains = one(cond.contains, `${at}.contains`)
    }
    if (cond.prefix !== undefined) {
      out.prefix = String(one(cond.prefix, `${at}.prefix`) ?? "")
    }
    for (const op of ["gt", "gte", "lt", "lte"] as const) {
      if (cond[op] !== undefined) out[op] = one(cond[op], `${at}.${op}`)
    }
    properties[name] = out
  }
  return {
    filter: { ...filter, properties },
    problems,
  }
}

/** The typed properties a `set` block writes: a token resolved, then coerced
 * to the property's datatype the way a form value is. A reference-typed
 * property takes `{ref}` around the path. */
export function substituteSet(
  set: Record<string, string>,
  ctx: ViewContext,
  kind: KindInfo | undefined,
  path = "set",
  record?: SubstrateRecord
): { properties: Record<string, unknown>; problems: Problem[] } {
  const properties: Record<string, unknown> = {}
  const problems: Problem[] = []
  for (const [name, raw] of Object.entries(set)) {
    const at = `${path}.${name}`
    const before = problems.length
    const value = literal(
      raw,
      ctx,
      record,
      at,
      problems,
      "this action cannot write it"
    )
    if (problems.length > before) continue
    properties[name] = typedValue(specOf(kind, name), value)
  }
  return { properties, problems }
}

function typedValue(spec: PropSpec | undefined, value: unknown): unknown {
  if (!spec) return value
  if (spec.kind === "reference") {
    return typeof value === "string" ? { ref: value } : value
  }
  if (typeof value !== "string") return value
  const parsed = parseValue(spec, value)
  return parsed.error || parsed.value === undefined ? value : parsed.value
}
