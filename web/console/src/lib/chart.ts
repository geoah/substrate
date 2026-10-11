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

/** The step between round ticks: 1, 2 or 5 times a power of ten. */
function tickStep(min: number, max: number, count: number): number {
  const raw = (max - min) / Math.max(1, count)
  if (!(raw > 0) || !Number.isFinite(raw)) return 1
  const power = 10 ** Math.floor(Math.log10(raw))
  const error = raw / power
  const factor = error >= 7.5 ? 10 : error >= 3.5 ? 5 : error >= 1.5 ? 2 : 1
  return factor * power
}

/** Round tick values covering `[min, max]`, about `count` of them. */
export function niceTicks(min: number, max: number, count = 4): number[] {
  if (!Number.isFinite(min) || !Number.isFinite(max)) return []
  if (min === max) {
    const pad = Math.abs(min) * 0.1 || 1
    return niceTicks(min - pad, max + pad, count)
  }
  const step = tickStep(min, max, count)
  const first = Math.floor(min / step) * step
  const last = Math.ceil(max / step) * step
  const out: number[] = []
  // Rounding to the step's own decimals keeps 0.30000000000000004 out.
  const decimals = Math.max(0, -Math.floor(Math.log10(step)) + 1)
  for (let v = first; v <= last + step / 2; v += step) {
    out.push(Number(v.toFixed(decimals)))
  }
  return out
}

/** A value domain widened to round ticks. `zero` anchors it at zero, as a
 * bar's baseline must be. */
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
  const ticks = niceTicks(min, max, count)
  return { domain: [ticks[0], ticks[ticks.length - 1]], ticks }
}

/** A tick label: compact from a thousand up (`1.2K`, `12K`, `3.4M`), up to
 * two decimals below. */
export function compactNumber(n: number): string {
  const abs = Math.abs(n)
  const trim = (x: number, digits: number) => Number(x.toFixed(digits))
  if (abs >= 1e9) return `${trim(n / 1e9, 1)}B`
  if (abs >= 1e6) return `${trim(n / 1e6, 1)}M`
  if (abs >= 1e4) return `${trim(n / 1e3, 0)}K`
  if (abs >= 1e3) return `${trim(n / 1e3, 1)}K`
  return String(trim(n, abs >= 10 ? 0 : 2))
}

/** One axis's tick labels, each as precise as the step between them, so
 * ticks 0.2 apart never print the same number twice (`72.2`, `72.4`); from
 * a thousand up they are compact. */
export function tickLabels(ticks: readonly number[]): string[] {
  const step = ticks.length > 1 ? Math.abs(ticks[1] - ticks[0]) : 1
  const decimals = step > 0 ? Math.max(0, -Math.floor(Math.log10(step))) : 0
  return ticks.map((t) =>
    Math.abs(t) >= 1e3 ? compactNumber(t) : t.toFixed(decimals)
  )
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

/** Which of `count` items get a label so that no more than `max` do: every
 * k-th, from the first. */
export function labelEvery(count: number, max: number): number {
  return Math.max(1, Math.ceil(count / Math.max(1, max)))
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
