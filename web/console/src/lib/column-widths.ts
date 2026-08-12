/** Kind-aware proportional column widths for THE table system (owner-approved
 * fix, 2026-08-06): a column is either FIXED (an exact px — action verbs,
 * datetimes, the expander, any user drag-resize override) or FLEXIBLE
 * (`{min, max?, weight?}` — it shares the container's leftover width in
 * proportion to its weight, clamped to its bounds). This replaces the old
 * "every width-less column splits the remainder equally" rule that ballooned
 * one lucky column on wide screens while data columns truncated.
 *
 * Distribution contract:
 * - Fixed columns take their px off the top.
 * - Flexible columns split what remains proportionally to `weight`
 *   (default 1), clamped to `[min, max]`; clamping frees or demands space,
 *   which redistributes among the still-unclamped columns until stable.
 * - If the mins alone overflow the container, every flexible column sits at
 *   its min and the table scrolls sideways (the caller sets the table width
 *   to the returned sum).
 * - If EVERY flexible column hits its max and space remains, the caps go
 *   soft: the leftover redistributes by weight so the table always fills its
 *   container instead of leaving truncation next to blank space.
 * - Results are integers that sum exactly to the container width whenever at
 *   least one flexible column exists and the mins fit. */

export interface FlexSize {
  /** Floor px — the column never renders narrower (the table scrolls). */
  min: number
  /** Ceiling px — hard while any sibling is still hungry, soft once every
   * flexible column is capped (the table must fill its container). */
  max?: number
  /** Appetite for leftover width relative to siblings (default 1). */
  weight?: number
}

export type ColumnWidthSpec =
  { id: string; px: number } | { id: string; flex: FlexSize }

function weightOf(f: FlexSize): number {
  return f.weight !== undefined && f.weight > 0 ? f.weight : 1
}

/** Largest-remainder rounding: integers that keep each column's fractional
 * intent and sum EXACTLY to the float total (no ±2px scrollbar surprises). */
function roundPreservingSum(
  ids: string[],
  floats: Record<string, number>
): Record<string, number> {
  const out: Record<string, number> = {}
  let target = 0
  for (const id of ids) target += floats[id]
  target = Math.round(target)
  let floorSum = 0
  const fractions: { id: string; frac: number }[] = []
  for (const id of ids) {
    const f = Math.floor(floats[id])
    out[id] = f
    floorSum += f
    fractions.push({ id, frac: floats[id] - f })
  }
  fractions.sort((a, b) => b.frac - a.frac)
  for (let i = 0; i < target - floorSum && i < fractions.length; i++) {
    out[fractions[i].id] += 1
  }
  return out
}

/** Container width + visible column specs → px per column id. Pure. */
export function distributeWidths(
  containerWidth: number,
  specs: ColumnWidthSpec[]
): Record<string, number> {
  const out: Record<string, number> = {}
  const flex: { id: string; flex: FlexSize }[] = []
  let fixedTotal = 0
  for (const s of specs) {
    if ("px" in s) {
      out[s.id] = Math.round(s.px)
      fixedTotal += out[s.id]
    } else {
      flex.push(s)
    }
  }
  if (!flex.length) return out

  const minTotal = flex.reduce((sum, s) => sum + s.flex.min, 0)
  const avail = containerWidth - fixedTotal

  // Overflow: mins don't fit — everyone sits at min, the table scrolls.
  if (avail <= minTotal) {
    for (const s of flex) out[s.id] = Math.round(s.flex.min)
    return out
  }

  // Water-fill: share by weight, clamp violators, redistribute, repeat.
  const clamped = new Map<string, number>()
  let pool = [...flex]
  const floats: Record<string, number> = {}
  while (pool.length) {
    let clampedSum = 0
    for (const px of clamped.values()) clampedSum += px
    const remaining = avail - clampedSum
    const totalWeight = pool.reduce((sum, c) => sum + weightOf(c.flex), 0)
    const next: typeof pool = []
    let clampedAny = false
    for (const c of pool) {
      const share = (remaining * weightOf(c.flex)) / totalWeight
      if (share < c.flex.min) {
        clamped.set(c.id, c.flex.min)
        clampedAny = true
      } else if (c.flex.max !== undefined && share > c.flex.max) {
        clamped.set(c.id, c.flex.max)
        clampedAny = true
      } else {
        next.push(c)
      }
    }
    if (!clampedAny) {
      // Stable: the pool's shares all sit inside their bounds.
      for (const c of pool) {
        floats[c.id] = (remaining * weightOf(c.flex)) / totalWeight
      }
      break
    }
    pool = next
  }

  for (const [id, px] of clamped) floats[id] = px

  // Every column clamped and space remains: grow whoever still has headroom
  // below its max (by weight, iteratively); only when EVERY column is capped
  // do the caps go soft — the table must fill its container either way.
  if (!pool.length) {
    let used = 0
    for (const px of clamped.values()) used += px
    let leftover = avail - used
    let growable = flex.filter(
      (c) => c.flex.max === undefined || floats[c.id] < c.flex.max
    )
    while (leftover > 0.5 && growable.length) {
      const totalWeight = growable.reduce((sum, c) => sum + weightOf(c.flex), 0)
      const next: typeof growable = []
      let consumed = 0
      for (const c of growable) {
        const add = (leftover * weightOf(c.flex)) / totalWeight
        const headroom =
          c.flex.max === undefined ? Infinity : c.flex.max - floats[c.id]
        if (add >= headroom) {
          floats[c.id] += headroom
          consumed += headroom
        } else {
          floats[c.id] += add
          consumed += add
          next.push(c)
        }
      }
      leftover -= consumed
      growable = next
      if (consumed <= 0) break
    }
    if (leftover > 0.5) {
      const totalWeight = flex.reduce((sum, c) => sum + weightOf(c.flex), 0)
      for (const c of flex) {
        floats[c.id] += (leftover * weightOf(c.flex)) / totalWeight
      }
    }
  }

  const rounded = roundPreservingSum(
    flex.map((c) => c.id),
    floats
  )
  for (const c of flex) out[c.id] = rounded[c.id]
  return out
}
