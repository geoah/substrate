/** A2UI's dynamic values: a literal, a binding (`{"@path": p}`) or a call
 * (`{"@call": name, args}`). A binding is read in the scope of the template
 * item being drawn; a call runs one of the basic catalog's functions. What
 * cannot be read is undefined, never a throw: an applet's screen is data an
 * agent wrote, and one bad binding must not take the page down. */

import { isRecord, toBool, toText } from "./coerce"
import { FUNCTIONS } from "./functions"
import { getAt, resolvePath } from "./pointer"

/** Where a value is read: the surface's data model, the template item a
 * binding is relative to, and that item's index for `@index`. */
export interface EvalContext {
  model: unknown
  /** The template item's absolute pointer (`/data/books/3`); the root
   * outside a template. */
  scope?: string
  /** The innermost template item's index. */
  index?: number
  /** The locale the format functions write in; the browser's by default. */
  locale?: string
}

export function isBinding(value: unknown): value is { "@path": string } {
  return isRecord(value) && typeof value["@path"] === "string"
}

export function isCall(
  value: unknown
): value is { "@call": string; args?: Record<string, unknown> } {
  return isRecord(value) && typeof value["@call"] === "string"
}

/** The absolute pointer a binding writes to, in this scope. */
export function bindingPath(
  value: unknown,
  ctx: EvalContext
): string | undefined {
  return isBinding(value) ? resolvePath(value["@path"], ctx.scope) : undefined
}

/** A call's result. `@index` is the template item's own; any other name
 * is the catalog's, called with its args resolved. */
export function callFunction(
  name: string,
  rawArgs: unknown,
  ctx: EvalContext
): unknown {
  const args = isRecord(rawArgs) ? rawArgs : {}
  if (name === "@index") {
    if (ctx.index === undefined) return undefined
    const offset = Number(resolve(args.offset, ctx) ?? 0)
    return ctx.index + (Number.isFinite(offset) ? offset : 0)
  }
  const fn = Object.hasOwn(FUNCTIONS, name) ? FUNCTIONS[name] : undefined
  if (!fn) return undefined
  const resolved: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(args))
    resolved[key] = resolveDeep(value, ctx)
  try {
    return fn(resolved, ctx, callFunction)
  } catch {
    return undefined
  }
}

/** One dynamic value: a binding's value, a call's result, or the literal. */
export function resolve(value: unknown, ctx: EvalContext): unknown {
  if (isBinding(value))
    return getAt(ctx.model, resolvePath(value["@path"], ctx.scope))
  if (isCall(value)) return callFunction(value["@call"], value.args, ctx)
  return value
}

/** A value and everything inside it: a function's args, an action's
 * context. A literal object's keys never start with `@`, so a binding or a
 * call is the only object that resolves to something else. */
export function resolveDeep(value: unknown, ctx: EvalContext): unknown {
  if (isBinding(value) || isCall(value)) return resolve(value, ctx)
  if (Array.isArray(value)) return value.map((v) => resolveDeep(v, ctx))
  if (isRecord(value)) {
    const out: Record<string, unknown> = {}
    for (const [key, v] of Object.entries(value)) out[key] = resolveDeep(v, ctx)
    return out
  }
  return value
}

export function resolveText(value: unknown, ctx: EvalContext): string {
  return toText(resolve(value, ctx))
}

export function resolveBool(value: unknown, ctx: EvalContext): boolean {
  return toBool(resolve(value, ctx))
}
