/** Per-surface column preferences for THE table system (owner ruling,
 * 2026-08-06): which columns show and in what order, user-configured through
 * the Columns dropdown and persisted in localStorage — the same pattern the
 * browse filter/sort prefs use (`lib/filters.ts`), one key per surface.
 *
 * The stored shape is a delta, not a snapshot: `order` only when it differs
 * from the surface's natural order, `hidden` only when something is hidden.
 * A schema can grow columns between visits, so both readers reconcile against
 * the CURRENT column set — unknown ids drop, new columns slot into their
 * natural position instead of piling at an edge. */

export interface TablePrefs {
  /** Column ids in display order; absent = the natural order. */
  order?: string[]
  /** Hidden column ids; absent = the surface's own default visibility. */
  hidden?: string[]
  /** Drag-resize overrides: column id → fixed px. A resized column leaves
   * the proportional distribution and keeps its exact width; absent columns
   * stay computed. Reset clears this along with order and visibility. */
  sizing?: Record<string, number>
}

function sizingOf(value: unknown): Record<string, number> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined
  }
  const out: Record<string, number> = {}
  for (const [id, px] of Object.entries(value)) {
    if (typeof px === "number" && Number.isFinite(px) && px > 0) {
      out[id] = Math.round(px)
    }
  }
  return Object.keys(out).length ? out : undefined
}

function prefsKey(surface: string): string {
  return `substrate.table.${surface}`
}

export function loadTablePrefs(surface: string): TablePrefs | null {
  try {
    const raw = localStorage.getItem(prefsKey(surface))
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== "object" || parsed === null) return null
    const p = parsed as Record<string, unknown>
    const out: TablePrefs = {}
    if (
      Array.isArray(p.order) &&
      p.order.every((t): t is string => typeof t === "string")
    ) {
      out.order = p.order
    }
    if (
      Array.isArray(p.hidden) &&
      p.hidden.every((t): t is string => typeof t === "string")
    ) {
      out.hidden = p.hidden
    }
    const sizing = sizingOf(p.sizing)
    if (sizing) out.sizing = sizing
    return out.order || out.hidden || out.sizing ? out : null
  } catch {
    return null
  }
}

/** Persist the delta; an all-default view removes the entry entirely —
 * resetting columns resets the store too. An EMPTY `hidden` array is a real
 * value ("show everything") on a surface whose default hides something. */
export function saveTablePrefs(surface: string, prefs: TablePrefs): void {
  try {
    const out: TablePrefs = {}
    if (prefs.order?.length) out.order = prefs.order
    if (prefs.hidden) out.hidden = prefs.hidden
    if (prefs.sizing && Object.keys(prefs.sizing).length) {
      out.sizing = prefs.sizing
    }
    if (out.order || out.hidden || out.sizing) {
      localStorage.setItem(prefsKey(surface), JSON.stringify(out))
    } else {
      localStorage.removeItem(prefsKey(surface))
    }
  } catch {
    // Storage full or denied — the view still works, it just won't stick.
  }
}

/** The display order: the stored order filtered to columns that still exist,
 * with columns the store never saw inserted at their natural position (right
 * after their nearest surviving natural predecessor). */
export function orderedColumns(
  naturalIds: string[],
  order?: string[]
): string[] {
  if (!order?.length) return [...naturalIds]
  const existing = new Set(naturalIds)
  const out = order.filter((id) => existing.has(id))
  const placed = new Set(out)
  naturalIds.forEach((id, i) => {
    if (placed.has(id)) return
    let at = 0
    for (let j = i - 1; j >= 0; j--) {
      const k = out.indexOf(naturalIds[j])
      if (k >= 0) {
        at = k + 1
        break
      }
    }
    out.splice(at, 0, id)
    placed.add(id)
  })
  return out
}

/** The visibility map TanStack expects, from the stored hidden list (or the
 * surface's default when nothing is stored). Unknown ids drop. */
export function columnVisibilityOf(
  naturalIds: string[],
  hidden?: string[]
): Record<string, boolean> {
  const hide = new Set(hidden ?? [])
  return Object.fromEntries(naturalIds.map((id) => [id, !hide.has(id)]))
}

/** The delta worth storing: order only when it differs from natural, hidden
 * only when it differs from the surface's default, sizing only when a
 * still-existing column carries an override — so untouched surfaces store
 * nothing and a reset clears the entry. */
export function prefsDelta(
  naturalIds: string[],
  order: string[],
  visibility: Record<string, boolean>,
  defaultHidden: string[] = [],
  sizing: Record<string, number> = {}
): TablePrefs {
  const out: TablePrefs = {}
  const effective = orderedColumns(naturalIds, order)
  if (effective.some((id, i) => id !== naturalIds[i])) out.order = effective
  const hidden = naturalIds.filter((id) => visibility[id] === false)
  const def = new Set(defaultHidden.filter((id) => naturalIds.includes(id)))
  const same = hidden.length === def.size && hidden.every((id) => def.has(id))
  if (!same) out.hidden = hidden
  const kept = Object.fromEntries(
    Object.entries(sizing).filter(([id]) => naturalIds.includes(id))
  )
  if (Object.keys(kept).length) out.sizing = kept
  return out
}
