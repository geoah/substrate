import { comparePositions, positionBetween } from "@substrate/client"

/** The kit's `Position.moves`, for a browser: the positions to write so a
 * dragged record sits where it was dropped. One write where its new
 * neighbours leave room; a record before it with no position (written
 * elsewhere without one) gets one first; one after it whose number no
 * longer sits above it (two devices that appended at once) gets a number
 * above it too; and a gap with no room left has the records around the
 * moved one spread out, so the order drawn is always the order kept. */

/** The most significant digits a position is written with, as the Swift
 * kit's: every system's decimal reads it back exactly. */
const digitLimit = 20

const above = (p: string, last: string | undefined) =>
  last === undefined || comparePositions(p, last) > 0

function digits(p: string): number {
  return Math.max(1, p.replace(/[^0-9]/g, "").replace(/^0+/, "").length)
}

/** A number strictly between the two within `digitLimit` digits, undefined
 * where there is none (equal neighbours, or a gap used up). */
function fit(before?: string, after?: string): string | undefined {
  if (
    before !== undefined &&
    after !== undefined &&
    comparePositions(before, after) >= 0
  )
    return undefined
  const p = positionBetween(before, after)
  return digits(p) > digitLimit ? undefined : p
}

/** `moved` dropped at `index` of `order` (the ids as drawn, `moved` among
 * them), given each one's position now: the positions that change. */
export function moves(
  moved: string,
  index: number,
  order: string[],
  positions: Record<string, string | undefined>
): Record<string, string> {
  const ids = order.filter((id) => id !== moved)
  const at = Math.max(0, Math.min(index, ids.length))
  ids.splice(at, 0, moved)
  const known: Record<string, string | undefined> = {
    ...positions,
    [moved]: undefined,
  }
  const writes: Record<string, string> = {}
  let last: string | undefined
  for (const id of ids.slice(0, at)) {
    const p = known[id]
    if (p !== undefined && above(p, last)) {
      last = p
      continue
    }
    const next = positionBetween(last ?? "0", undefined)
    known[id] = next
    writes[id] = next
    last = next
  }
  // The moved record, then each after it that does not sit above the one
  // before it, up to the first that does: the rest keep theirs.
  for (let offset = at; offset < ids.length; offset++) {
    const id = ids[offset]
    if (offset > at) {
      const p = known[id]
      if (p === undefined || above(p, last)) break
    }
    const ceiling = ids
      .slice(offset + 1)
      .map((i) => known[i])
      .find((p): p is string => p !== undefined && above(p, last))
    const next = fit(last, ceiling)
    if (next === undefined) return spread(ids, at, positions)
    known[id] = next
    writes[id] = next
    last = next
  }
  return writes
}

/** Numbers for the records around `index`, evenly between the nearest
 * neighbours that leave room for them, widening one record each side until
 * they do; at the widest, every record of the order, from 1. */
function spread(
  ids: string[],
  index: number,
  positions: Record<string, string | undefined>
): Record<string, string> {
  const known = { ...positions, [ids[index]]: undefined }
  let low = index
  let high = index + 1
  for (;;) {
    const floor = low > 0 ? known[ids[low - 1]] : undefined
    const ceiling = high < ids.length ? known[ids[high]] : undefined
    const values = evenly(high - low, floor, ceiling)
    if (values) {
      const writes: Record<string, string> = {}
      ids.slice(low, high).forEach((id, i) => {
        if (positions[id] !== values[i] || id === ids[index])
          writes[id] = values[i]
      })
      return writes
    }
    if (low === 0 && high === ids.length)
      return Object.fromEntries(ids.map((id, i) => [id, String(i + 1)]))
    low = Math.max(0, low - 1)
    high = Math.min(ids.length, high + 1)
  }
}

interface Scaled {
  n: bigint
  scale: number
}

function parse(text: string): Scaled | undefined {
  const m = /^(-)?(\d+)(?:\.(\d+))?$/.exec(text.trim())
  if (!m) return undefined
  const frac = m[3] ?? ""
  return { n: BigInt(m[2] + frac) * (m[1] ? -1n : 1n), scale: frac.length }
}

const at = (x: Scaled, scale: number) => x.n * 10n ** BigInt(scale - x.scale)

function format(n: bigint, scale: number): string {
  const neg = n < 0n
  const raw = (neg ? -n : n).toString().padStart(scale + 1, "0")
  let out = scale ? `${raw.slice(0, -scale)}.${raw.slice(-scale)}` : raw
  if (scale) out = out.replace(/0+$/, "").replace(/\.$/, "")
  return (neg ? "-" : "") + out
}

/** `count` numbers strictly increasing and strictly between the bounds, at
 * the fewest decimal places that fit them; undefined where none do. */
function evenly(
  count: number,
  floorText?: string,
  ceilingText?: string
): string[] | undefined {
  const floor = floorText !== undefined ? parse(floorText) : undefined
  const ceiling = ceilingText !== undefined ? parse(ceilingText) : undefined
  const steps = Array.from({ length: count }, (_, i) => BigInt(i + 1))
  let values: string[]
  if (!floor && !ceiling) values = steps.map(String)
  else if (floor && !ceiling)
    values = steps.map((k) =>
      format(
        at(floor, floor.scale) + k * 10n ** BigInt(floor.scale),
        floor.scale
      )
    )
  else if (!floor && ceiling)
    values = steps.map((k) =>
      format(
        at(ceiling, ceiling.scale) -
          (BigInt(count) + 1n - k) * 10n ** BigInt(ceiling.scale),
        ceiling.scale
      )
    )
  else {
    const base = Math.max(floor!.scale, ceiling!.scale)
    let found: string[] | undefined
    for (let scale = base; scale <= base + digitLimit && !found; scale++) {
      const f = at(floor!, scale)
      const gap = at(ceiling!, scale) - f
      if (gap < BigInt(count) + 1n) continue
      found = steps.map((k) =>
        format(f + (gap * k) / (BigInt(count) + 1n), scale)
      )
    }
    if (!found) return undefined
    values = found
  }
  return values.every((v) => digits(v) <= digitLimit) ? values : undefined
}
