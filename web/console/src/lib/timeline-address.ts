/** The timeline's choices as the address keeps them: the URL keys' parsers
 * and values, a custom range, and the per-metric choices (a reducer, a
 * unit, a statistic), shared by the timeline page and the streams page's
 * panel. */

import { createParser, parseAsArrayOf, parseAsString } from "nuqs"

import { addressValue, sameRef, valueOfAddress } from "@/lib/observations"
import {
  BUCKET_CHOICES,
  DAY_BOUNDARIES,
  RANGE_PRESETS,
  type TimeRange,
} from "@/lib/observations-time"
import { FROM_CHOICES, GROUP_CHOICES } from "@/lib/observations-reads"

/** A list key, empty where the address has none. */
export const ARRAY = parseAsArrayOf(parseAsString).withDefault([])
/** A stream key's list (`source`, `about`, `channel`): `~none` reads as
 * `NONE`, and a value of the key's own is unescaped (`addressValue`). */
export const STREAM_KEYS = parseAsArrayOf(
  createParser({ parse: valueOfAddress, serialize: addressValue })
).withDefault([])
export const RANGE_VALUES = [
  ...RANGE_PRESETS.map((p) => p.value),
  "all",
  "custom",
] as const
export const BUCKET_VALUES = BUCKET_CHOICES.map((b) => b.value)
export const DAY_VALUES = DAY_BOUNDARIES
export const GROUP_VALUES = GROUP_CHOICES.map((g) => g.value)
export const FROM_VALUES = FROM_CHOICES.map((f) => f.value)

/** A custom range from the address's `start` and `end` instants;
 * undefined where either does not parse or the end is not after the
 * start. */
export function customRange(
  start: string | null,
  end: string | null
): TimeRange | undefined {
  const a = start ? Date.parse(start) : NaN
  const b = end ? Date.parse(end) : NaN
  return Number.isNaN(a) || Number.isNaN(b) || b <= a
    ? undefined
    : { start: a, end: b }
}

// ── per-metric choices in the address ──────────────────────────────────────

/** Per-metric choices (a reducer, a unit, a statistic) as URL tokens
 * `<metric>~<value>`, the metric URI-encoded since an id may hold a `~`. */
export function readChoices(
  tokens: readonly string[] | null
): Map<string, string> {
  const out = new Map<string, string>()
  for (const token of tokens ?? []) {
    const at = token.lastIndexOf("~")
    if (at <= 0) continue
    try {
      out.set(decodeURIComponent(token.slice(0, at)), token.slice(at + 1))
    } catch {
      // A hand-edited token that does not decode names no metric.
    }
  }
  return out
}

/** One metric's choice, whichever way the address spells the metric (an
 * id or a record path). */
export function choiceOf(
  choices: ReadonlyMap<string, string>,
  metric: string
): string | undefined {
  const exact = choices.get(metric)
  if (exact !== undefined) return exact
  for (const [m, v] of choices) if (sameRef(m, metric)) return v
  return undefined
}

/** The tokens with one metric's choice set, or cleared with "", in place of
 * any other spelling of the metric. */
export function writeChoice(
  tokens: readonly string[] | null,
  metric: string,
  value: string
): string[] {
  const choices = readChoices(tokens)
  for (const m of [...choices.keys()]) {
    if (m === metric || sameRef(m, metric)) choices.delete(m)
  }
  if (value) choices.set(metric, value)
  return [...choices].map(([m, v]) => `${encodeURIComponent(m)}~${v}`)
}
