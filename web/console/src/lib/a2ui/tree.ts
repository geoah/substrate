/** A component's children as the surface draws them: a ChildList is a
 * static list of ids, or a template (`{path, componentId}`) drawn once per
 * item of the list at `path`, each in its own scope so its relative
 * bindings read that item. Templates nest, and so do their scopes. */

import { isRecord } from "./coerce"
import { escapeToken, getAt, resolvePath } from "./pointer"
import type { EvalContext } from "./values"

export interface ChildRef {
  id: string
  ctx: EvalContext
  /** Stable across re-renders: the item's own `id` where it has one, so an
   * input in a list row keeps its focus when the list re-sorts. */
  key: string
}

function itemKey(item: unknown, fallback: string): string {
  if (
    isRecord(item) &&
    (typeof item.id === "string" || typeof item.id === "number")
  )
    return `id:${String(item.id)}`
  return fallback
}

export function childRefs(list: unknown, ctx: EvalContext): ChildRef[] {
  if (Array.isArray(list))
    return list
      .filter((id): id is string => typeof id === "string")
      .map((id, i) => ({ id, ctx, key: `${id}#${i}` }))
  if (
    !isRecord(list) ||
    typeof list.componentId !== "string" ||
    typeof list.path !== "string"
  )
    return []
  const componentId = list.componentId
  const resolved = resolvePath(list.path, ctx.scope)
  const base = resolved === "/" ? "" : resolved
  const items = getAt(ctx.model, resolved)
  const entries: [string, unknown][] = Array.isArray(items)
    ? items.map((item, i) => [String(i), item])
    : isRecord(items)
      ? Object.entries(items).map(([k, item]) => [escapeToken(k), item])
      : []
  const seen = new Set<string>()
  return entries.map(([token, item], index) => {
    let key = itemKey(item, `#${token}`)
    // Two items with one id would share a key; the later one keeps its slot.
    if (seen.has(key)) key = `${key}#${token}`
    seen.add(key)
    return {
      id: componentId,
      ctx: { ...ctx, scope: `${base}/${token}`, index },
      key,
    }
  })
}

/** A single `child` prop: one id, in the parent's scope. */
export function childRef(id: unknown, ctx: EvalContext): ChildRef | undefined {
  return typeof id === "string" ? { id, ctx, key: id } : undefined
}
