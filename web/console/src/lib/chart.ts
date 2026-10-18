/** The arithmetic behind the console's hand-drawn SVG charts: a linear scale
 * and its round ticks, compact tick labels, and the path strings for a line,
 * a band and a bar. No charting dependency: what a chart draws is these
 * strings, so each is tested as text. */

/** A linear map from a domain onto pixels, and back. */
export interface Scale {
  (value: number): number
  invert(px: number): number
  domain: [number, number]
  range: [number, number]
}

export function linearScale(
  domain: [number, number],
  range: [number, number]
): Scale {
  const [d0, d1] = domain
  const [r0, r1] = range
  const span = d1 - d0 || 1
  const scale = ((value: number) =>
    r0 + ((value - d0) / span) * (r1 - r0)) as Scale
  scale.invert = (px: number) => d0 + ((px - r0) / (r1 - r0 || 1)) * span
  scale.domain = [d0, d1]
  scale.range = [r0, r1]
  return scale
}

/** The step between round ticks, 1, 2 or 5 times a power of ten, as that
 * factor and the power's exponent. */
function tickStep(
  min: number,
  max: number,
  count: number
): { factor: number; exponent: number } {
  const raw = (max - min) / Math.max(1, count)
  if (!(raw > 0) || !Number.isFinite(raw)) return { factor: 1, exponent: 0 }
  const exponent = Math.floor(Math.log10(raw))
  const error = raw / 10 ** exponent
  const factor = error >= 7.5 ? 10 : error >= 3.5 ? 5 : error >= 1.5 ? 2 : 1
  return { factor, exponent }
}

/** The most ticks one axis enumerates. A step the domain holds more of is
 * no round step for it, and the domain's own ends stand in. */
const MAX_TICKS = 64

/** Round tick values covering `[min, max]`, about `count` of them.
 *
 * Each tick is an integer index times the step, and below a step of one the
 * index divided by the step's whole inverse, so no tick is a running sum and
 * none carries float noise (`0.30000000000000004`). Where the step is finer
 * than the float spacing at the domain's magnitude (`1e20` and `1e20 +
 * 16384`), or the index or its inverse leaves the finite numbers, two ticks
 * would be equal; the answer is then the domain's own two ends. */
export function niceTicks(min: number, max: number, count = 4): number[] {
  if (!Number.isFinite(min) || !Number.isFinite(max)) return []
  if (min > max) return niceTicks(max, min, count)
  if (min === max) {
    const pad = Math.abs(min) * 0.1 || 1
    return niceTicks(min - pad, max + pad, count)
  }
  const ends = [min, max]
  const { factor, exponent } = tickStep(min, max, count)
  let lo: number
  let hi: number
  let at: (index: number) => number
  if (exponent >= 0) {
    const step = factor * 10 ** exponent
    lo = Math.floor(min / step)
    hi = Math.ceil(max / step)
    at = (index) => index * step
  } else {
    const inverse = 10 ** -exponent / factor
    lo = Math.floor(min * inverse)
    hi = Math.ceil(max * inverse)
    at = (index) => index / inverse
  }
  const n = hi - lo
  if (!Number.isFinite(n) || n < 1 || n > MAX_TICKS) return ends
  const out: number[] = []
  for (let i = 0; i <= n; i++) {
    const tick = at(lo + i)
    if (!Number.isFinite(tick)) return ends
    if (out.length && !(tick > out[out.length - 1])) return ends
    out.push(tick)
  }
  return out
}

/** How close to the outer tick, as a share of a step, an extreme takes the
 * axis one step further: a tenth of a step is a few pixels on a chart. */
const EDGE = 0.1

/** A value domain widened to round ticks, and one step further where an
 * extreme lies within a tenth of a step of the outer tick, so no point
 * sits on or against the frame (159.97 lb under a 160 top). `zero`
 * anchors it at zero, as a bar's baseline must be, and a zero baseline is
 * never padded. The domain is always two finite numbers. */
export function niceDomain(
  values: readonly number[],
  { zero = false, count = 4 }: { zero?: boolean; count?: number } = {}
): { domain: [number, number]; ticks: number[] } {
  const finite = values.filter((v) => Number.isFinite(v))
  let min = finite.length ? Math.min(...finite) : 0
  let max = finite.length ? Math.max(...finite) : 1
  if (zero) {
    min = Math.min(0, min)
    max = Math.max(0, max)
    if (min === 0 && max === 0) max = 1
  }
  let ticks = niceTicks(min, max, count)
  if (ticks.length < 2) {
    // Padding a single value overflowed: the value itself is the axis.
    return { domain: [min, max], ticks: [min] }
  }
  const step = ticks[1] - ticks[0]
  const near = (value: number, tick: number) =>
    Math.abs(value - tick) <= Math.abs(step) * EDGE
  // A tick beyond, cut to the digits a step carries so no float noise
  // shows in its label.
  const beyond = (tick: number, by: number) =>
    Number((tick + by).toPrecision(12))
  if (
    finite.length &&
    near(max, ticks[ticks.length - 1]) &&
    !(zero && max === 0)
  ) {
    const next = beyond(ticks[ticks.length - 1], step)
    if (Number.isFinite(next) && next > ticks[ticks.length - 1]) {
      ticks = [...ticks, next]
    }
  }
  if (finite.length && near(min, ticks[0]) && !(zero && min === 0)) {
    const next = beyond(ticks[0], -step)
    if (Number.isFinite(next) && next < ticks[0]) ticks = [next, ...ticks]
  }
  return { domain: [ticks[0], ticks[ticks.length - 1]], ticks }
}

/** The most decimals a tick label prints before it switches to an
 * exponent. */
const MAX_LABEL_DECIMALS = 20

/** The decimals that tell values `step` apart. */
function decimalsFor(step: number): number {
  return step > 0 ? Math.max(0, -Math.floor(Math.log10(step))) : 0
}

/** The compact suffixes from a thousand up, largest first. */
const COMPACT: readonly [number, string][] = [
  [1e9, "B"],
  [1e6, "M"],
  [1e3, "K"],
]

/** One tick from a thousand up, in its suffix's unit to as many decimals as
 * the step needs in that unit (`10.5K` beside `10K`), trailing zeros
 * dropped. */
function compactTick(t: number, step: number): string {
  const [unit, suffix] = COMPACT.find(([u]) => Math.abs(t) >= u)!
  const decimals = Math.min(100, decimalsFor(step / unit))
  return `${Number((t / unit).toFixed(decimals))}${suffix}`
}

/** Exponent labels, each with as many digits as the ticks need to differ
 * (`1.2e-101`, `1.7400000000000004e+13`). Seventeen significant digits
 * tell any two finite floats apart. */
function exponentLabels(ticks: readonly number[], step: number): string[] {
  const largest = Math.max(...ticks.map((t) => Math.abs(t)))
  const from =
    largest > 0 && step > 0
      ? Math.floor(Math.log10(largest)) - Math.floor(Math.log10(step))
      : 0
  let labels: string[] = []
  for (let digits = Math.min(16, Math.max(0, from)); digits <= 16; digits++) {
    labels = ticks.map((t) => t.toExponential(digits))
    if (new Set(labels).size === labels.length) break
  }
  return labels
}

/** One axis's tick labels, each as precise as the step between them, so
 * ticks 0.2 apart never print the same number twice (`72.2`, `72.4`). From
 * a thousand up they are compact, to the decimals the step needs in the
 * suffix's unit (`10K`, `10.5K`, `11K`). A step finer than twenty decimals,
 * and labels that would still print one number twice (two adjacent floats
 * that divide into one number of billions), print exponents instead. */
export function tickLabels(ticks: readonly number[]): string[] {
  let step = 0
  for (let i = 1; i < ticks.length; i++) {
    const gap = Math.abs(ticks[i] - ticks[i - 1])
    if (gap > 0 && (!step || gap < step)) step = gap
  }
  if (!step) step = 1
  const decimals = decimalsFor(step)
  if (decimals > MAX_LABEL_DECIMALS) return exponentLabels(ticks, step)
  const labels = ticks.map((t) =>
    Math.abs(t) >= 1e3 ? compactTick(t, step) : t.toFixed(decimals)
  )
  return new Set(labels).size === labels.length
    ? labels
    : exponentLabels(ticks, step)
}

const r = (n: number) => Number(n.toFixed(2))

export interface XY {
  x: number
  y: number
}

/** A polyline through the points, broken where a point is null: a gap in
 * the data is a gap in the line, never a slope across it. */
export function linePath(points: readonly (XY | null)[]): string {
  let out = ""
  let pen = false
  for (const p of points) {
    if (!p) {
      pen = false
      continue
    }
    out += `${pen ? "L" : "M"}${r(p.x)} ${r(p.y)}`
    pen = true
  }
  return out
}

export interface Span {
  x: number
  y0: number
  y1: number
}

/** A closed band between a lower and an upper line, one shape per unbroken
 * run: along the top, back along the bottom. A run of one point is a
 * hairline. */
export function bandPath(points: readonly (Span | null)[]): string {
  const runs: Span[][] = []
  let run: Span[] = []
  for (const p of points) {
    if (p) run.push(p)
    else if (run.length) {
      runs.push(run)
      run = []
    }
  }
  if (run.length) runs.push(run)
  return runs
    .map((seg) => {
      const top = seg.map((p, i) => `${i ? "L" : "M"}${r(p.x)} ${r(p.y1)}`)
      const back = [...seg].reverse().map((p) => `L${r(p.x)} ${r(p.y0)}`)
      return `${top.join("")}${back.join("")}Z`
    })
    .join("")
}

/** A bar from its baseline to its value, the data end rounded (4px by
 * default) and the baseline end square. Works both ways: a value below the
 * baseline rounds its lower end. A bar too short for its corners rounds
 * less. */
export function barPath(
  x: number,
  width: number,
  base: number,
  value: number,
  radius = 4
): string {
  const height = Math.abs(base - value)
  if (height === 0 || width <= 0) return ""
  const rad = Math.max(0, Math.min(radius, width / 2, height))
  const left = r(x)
  const right = r(x + width)
  if (value <= base) {
    // Up from the baseline: the top corners round.
    return (
      `M${left} ${r(base)}V${r(value + rad)}` +
      `Q${left} ${r(value)} ${r(x + rad)} ${r(value)}` +
      `H${r(x + width - rad)}` +
      `Q${right} ${r(value)} ${right} ${r(value + rad)}` +
      `V${r(base)}Z`
    )
  }
  return (
    `M${left} ${r(base)}V${r(value - rad)}` +
    `Q${left} ${r(value)} ${r(x + rad)} ${r(value)}` +
    `H${r(x + width - rad)}` +
    `Q${right} ${r(value)} ${right} ${r(value - rad)}` +
    `V${r(base)}Z`
  )
}

/** A plain rectangle, for a stacked bar's inner segments: only the top of
 * the whole stack is rounded. */
export function rectPath(
  x: number,
  width: number,
  y0: number,
  y1: number
): string {
  const top = Math.min(y0, y1)
  const bottom = Math.max(y0, y1)
  if (bottom - top <= 0 || width <= 0) return ""
  return `M${r(x)} ${r(bottom)}V${r(top)}H${r(x + width)}V${r(bottom)}Z`
}

/** The index of the bucket under a pixel, or -1 outside them all. */
export function bucketAt(
  px: number,
  windows: readonly { x0: number; x1: number }[]
): number {
  for (let i = 0; i < windows.length; i++) {
    if (px >= windows[i].x0 && px < windows[i].x1) return i
  }
  return -1
}
