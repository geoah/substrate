import type { ValueFormat } from "./report"

/** The number and its small grey unit, printed apart. */
export function formatValue(
  v: number,
  f: ValueFormat
): { num: string; unit: string } {
  switch (f) {
    case "percent":
      return { num: `${Math.round(v)}%`, unit: "" }
    case "percent1":
      return { num: `${round1(v)}%`, unit: "" }
    case "hours": {
      const total = Math.round(v * 60)
      const h = Math.floor(total / 60)
      const m = total % 60
      return { num: `${h}:${String(m).padStart(2, "0")}`, unit: "h" }
    }
    case "steps":
      return { num: Math.round(v).toLocaleString("en-US"), unit: "Steps" }
    case "vo2":
      return { num: String(Math.round(v)), unit: "ml/kg/min" }
    case "bpm":
      return { num: String(Math.round(v)), unit: "bpm" }
  }
}

export function formatValueText(v: number | null, f: ValueFormat): string {
  if (v === null) return "No data"
  const { num, unit } = formatValue(v, f)
  return unit ? `${num} ${unit}` : num
}

/** `68%` but `80.3%`: one decimal only where it carries signal. */
function round1(v: number): string {
  const r = Math.round(v * 10) / 10
  return Number.isInteger(r) ? String(r) : r.toFixed(1)
}

/** Round to tenths, halves away from zero (0.45 is 0.5). */
export function tenths(v: number): number {
  const r = (Math.sign(v) * Math.round(Math.abs(v) * 10 + 1e-9)) / 10
  return r === 0 ? 0 : r
}

/** `+0.3`, `-1.6`, `0.0`. A value that rounds to zero prints unsigned. */
export function formatImpact(years: number): string {
  const r = tenths(years)
  if (r === 0) return "0.0"
  return `${r > 0 ? "+" : "-"}${Math.abs(r).toFixed(1)}`
}

export type Tone = "good" | "bad" | "neutral"

export function impactTone(years: number | null): Tone {
  if (years === null) return "neutral"
  const r = tenths(years)
  return r < 0 ? "good" : r > 0 ? "bad" : "neutral"
}

export function formatPace(p: number): string {
  return `${p.toFixed(1)}x`
}

const MONTHS = [
  "JAN",
  "FEB",
  "MAR",
  "APR",
  "MAY",
  "JUN",
  "JUL",
  "AUG",
  "SEP",
  "OCT",
  "NOV",
  "DEC",
]

/** `2026-10-19` → `OCT 19`. */
export function shortDate(iso: string): string {
  const [, m, d] = iso.split("-").map(Number)
  return `${MONTHS[m - 1]} ${d}`
}

export function weekLabel(start: string, end: string): string {
  return `${shortDate(start)} - ${shortDate(end)}`
}

/** `1.2 years younger`, `0.3 years older`, `On track`. */
export function deltaText(delta: number): string {
  const r = Math.round(Math.abs(delta) * 10) / 10
  if (r === 0) return "Right on track"
  return `${r.toFixed(1)} ${r === 1 ? "year" : "years"} ${delta < 0 ? "younger" : "older"}`
}
