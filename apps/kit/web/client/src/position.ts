/** Positions are `decimal` properties written as strings: moving a row writes
 * that one row with a number between its neighbours, exactly, so halving
 * never runs out of precision (the kit's `Position`). */

interface Scaled {
  n: bigint
  scale: number
}

function parse(text: string): Scaled | undefined {
  const m = /^(-)?(\d+)(?:\.(\d+))?$/.exec(text.trim())
  if (!m) return undefined
  const frac = m[3] ?? ""
  const n = BigInt(m[2] + frac) * (m[1] ? -1n : 1n)
  return { n, scale: frac.length }
}

function align(a: Scaled, scale: number): bigint {
  return a.n * 10n ** BigInt(scale - a.scale)
}

function format({ n, scale }: Scaled): string {
  const neg = n < 0n
  let digits = (neg ? -n : n).toString().padStart(scale + 1, "0")
  let out = scale
    ? `${digits.slice(0, -scale)}.${digits.slice(-scale)}`
    : digits
  if (scale) out = out.replace(/0+$/, "").replace(/\.$/, "")
  digits = out
  return (neg ? "-" : "") + digits
}

/** Compares two positions as numbers; an absent one sorts last. */
export function comparePositions(a?: string, b?: string): number {
  const x = a !== undefined ? parse(a) : undefined
  const y = b !== undefined ? parse(b) : undefined
  if (!x && !y) return 0
  if (!x) return 1
  if (!y) return -1
  const scale = Math.max(x.scale, y.scale)
  const d = align(x, scale) - align(y, scale)
  return d < 0n ? -1 : d > 0n ? 1 : 0
}

/** A position between two neighbours: either may be absent (the start or
 * the end of the list). */
export function positionBetween(before?: string, after?: string): string {
  const a = before !== undefined ? parse(before) : undefined
  const b = after !== undefined ? parse(after) : undefined
  if (!a && !b) return "1"
  if (a && !b)
    return format({
      n: align(a, a.scale) / 10n ** BigInt(a.scale) + 1n,
      scale: 0,
    })
  if (!a && b)
    return format({
      n: align(b, b.scale) / 10n ** BigInt(b.scale) - 1n,
      scale: 0,
    })
  const scale = Math.max(a!.scale, b!.scale)
  const sum = align(a!, scale) + align(b!, scale)
  if (sum % 2n === 0n) return format({ n: sum / 2n, scale })
  return format({ n: sum * 5n, scale: scale + 1 })
}

/** The position after the highest of `positions`. */
export function positionAfter(positions: (string | undefined)[]): string {
  const last = positions
    .filter((p): p is string => p !== undefined)
    .sort(comparePositions)
    .at(-1)
  return positionBetween(last, undefined)
}
