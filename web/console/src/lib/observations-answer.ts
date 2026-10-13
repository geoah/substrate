/** An aggregate answer as a chart and a table read it, without React: its
 * rows cut into one bucket axis and one series per group value, a row's
 * value for one reducer, its columns and its copy as text. */

import type {
  AggregateGroup,
  AggregateGroupKey,
  AggregateReducer,
  AggregateRow,
  AggregateValue,
} from "@/lib/api/types"
import { channelKey, reducerKey, reducerName } from "@/lib/observations"
import {
  localKey,
  localSlot,
  type BucketSize,
  type TimeRange,
} from "@/lib/observations-time"

// ── the answer, cut into buckets and series ────────────────────────────────

/** A decimal string as a number to plot; null for no answer. */
export function numberOf(value: string | null | undefined): number | null {
  if (value === null || value === undefined || value === "") return null
  const n = Number(value)
  return Number.isFinite(n) ? n : null
}

/** One bucket of the answer: its window and the rows that fell in it. A
 * recorded-local bucket's window is its slot on the reader's axis
 * (`localSlot`), keyed by its date (`local`), and `instants` is the time its
 * rows cover, which is the reader's own day only where the values were
 * recorded at the reader's offset. */
export interface Bucket {
  start: number
  end: number
  startIso: string
  endIso: string
  rows: AggregateRow[]
  local?: string
  instants?: TimeRange
}

/** The bucket a row falls in: its date in the recorded-local calendar
 * (`local` names the size), else its window's two instants. */
export function bucketKeyOf(row: AggregateRow, local?: BucketSize): string {
  const key = local ? localKey(row.start, local) : undefined
  return key ?? `${Date.parse(row.start)}\n${Date.parse(row.end)}`
}

function keyOfBucket(bucket: Bucket): string {
  return bucket.local ?? `${bucket.start}\n${bucket.end}`
}

/** The rows a recorded-local answer holds inside `range`: a read of whole
 * local days asks wider (`readRange`), and a date whose slot lies outside
 * the range is not one the reader asked for. */
export function rowsInRange(
  rows: readonly AggregateRow[],
  size: BucketSize,
  range: TimeRange
): AggregateRow[] {
  return rows.filter((row) => {
    const key = localKey(row.start, size)
    if (!key) return true
    const slot = localSlot(key, size)
    return slot.end > range.start && slot.start < range.end
  })
}

/** One series: the rows one group value answers, a point per bucket. */
export interface Series {
  key: string
  /** The group value, or undefined for the one series of an ungrouped lane. */
  value?: string
  /** The source's channel, where the answer is grouped by one. */
  channel?: string
  points: SeriesPoint[]
  /** Rows of this series that met a bucket already holding one, which the
   * series does not draw. A read the console plans answers none. */
  dropped: number
}

export interface SeriesPoint {
  bucket: number
  row?: AggregateRow
  /** The picked reducer's value. */
  value: number | null
  /** The band: the lowest and highest, on a level. */
  low: number | null
  high: number | null
}

/** A row's value for one reducer, by key. A rolling value (one carrying
 * `rolling` and `of`) is never the reducer's own: the timeline asks for
 * none, and one would otherwise shadow the column it rolls over.
 *
 * A bare `timeInState` is asked for once and answered once per state, each
 * value naming its state (`{"timeInState": ["rem"]}`, `code: "rem"`), so the
 * bare key resolves to the value for the row's own code. */
export function rowValue(
  row: AggregateRow | undefined,
  key: string
): AggregateValue | undefined {
  const own = (row?.values ?? []).filter((v) => v.rolling === undefined)
  const exact = own.find((v) => reducerKey(v.reducer) === key)
  if (exact || key !== "timeInState") return exact
  const code = row?.group?.code
  if (code === undefined) return undefined
  return own.find(
    (v) =>
      reducerName(v.reducer) === "timeInState" &&
      reducerKey(v.reducer) === `timeInState:${code}`
  )
}

/** The empty buckets between windows that do not meet: an answer without
 * `fill` omits the buckets nothing fell in, and a line drawn from one
 * returned window to the next would bridge the time between them. Each
 * stretch of missing time becomes one bucket holding no rows, so every
 * series breaks there. */
function withGaps(buckets: Bucket[]): Bucket[] {
  const out: Bucket[] = []
  for (const bucket of buckets) {
    const previous = out.at(-1)
    if (previous && bucket.start > previous.end) {
      out.push({
        start: previous.end,
        end: bucket.start,
        startIso: previous.endIso,
        endIso: bucket.startIso,
        rows: [],
      })
    }
    out.push(bucket)
  }
  return out
}

/** The windows of one or more answers as one bucket axis: every distinct
 * window, oldest first, with an empty bucket standing in for each stretch
 * no answer covered. Several reads over one range and one bucket share
 * their windows, so a chart that composes them draws one axis; each
 * bucket holds every row that fell in it, from any answer.
 *
 * In the recorded-local calendar (`local` names the size) a window is a
 * date, read off each row's own `start`: two rows of one date are one
 * bucket whatever their offsets, and two dates are two slots, though a day
 * lived west of the next one overlaps it in time. */
export function bucketsOf(
  answers: readonly (readonly AggregateRow[])[],
  local?: BucketSize
): Bucket[] {
  const byWindow = new Map<string, Bucket>()
  for (const rows of answers) {
    for (const row of rows) {
      const start = Date.parse(row.start)
      const end = Date.parse(row.end)
      const key = local ? localKey(row.start, local) : undefined
      const id = key ?? `${start}\n${end}`
      let bucket = byWindow.get(id)
      if (!bucket) {
        bucket = key
          ? {
              ...localSlot(key, local!),
              startIso: row.start,
              endIso: row.end,
              rows: [],
              local: key,
              instants: { start, end },
            }
          : { start, end, startIso: row.start, endIso: row.end, rows: [] }
        byWindow.set(id, bucket)
      } else if (bucket.instants) {
        bucket.instants = {
          start: Math.min(bucket.instants.start, start),
          end: Math.max(bucket.instants.end, end),
        }
      }
      bucket.rows.push(row)
    }
  }
  return withGaps([...byWindow.values()].sort((a, b) => a.start - b.start))
}

/** One answer's series on a bucket axis (`bucketsOf`, keyed the same way
 * by `local`): one per value of the group key and channel, in first-seen
 * order, each holding one point per bucket. A bucket the answer has no row
 * in is a point with no value, so a line breaks there. */
export function seriesOn(
  buckets: readonly Bucket[],
  rows: readonly AggregateRow[],
  pickedKey: string,
  groupKey?: AggregateGroupKey,
  local?: BucketSize
): Series[] {
  const at = new Map<string, number>()
  buckets.forEach((b, i) => at.set(keyOfBucket(b), i))
  const keys: string[] = []
  const channels = new Map<string, string | undefined>()
  const byKey = new Map<string, Map<number, AggregateRow>>()
  const dropped = new Map<string, number>()
  for (const row of rows) {
    const value = groupValue(row.group, groupKey) ?? ""
    // A row carries a channel only where its read grouped by one.
    const channel = row.group?.channel || undefined
    const key = channelKey(value, channel)
    if (!byKey.has(key)) {
      keys.push(key)
      channels.set(key, channel)
      byKey.set(key, new Map())
    }
    const i = at.get(bucketKeyOf(row, local))
    const held = byKey.get(key)!
    if (i === undefined) continue
    if (held.has(i)) dropped.set(key, (dropped.get(key) ?? 0) + 1)
    else held.set(i, row)
  }
  return keys.map((key) => {
    const held = byKey.get(key)!
    const channel = channels.get(key)
    const value = channel ? key.slice(0, key.indexOf("\u0001")) : key
    return {
      key,
      value: groupKey ? value || undefined : undefined,
      ...(channel ? { channel } : {}),
      dropped: dropped.get(key) ?? 0,
      points: buckets.map((_, i) => {
        const row = held.get(i)
        return {
          bucket: i,
          row,
          value: numberOf(rowValue(row, pickedKey)?.value),
          low: numberOf(rowValue(row, "min")?.value),
          high: numberOf(rowValue(row, "max")?.value),
        }
      }),
    }
  })
}

function groupValue(
  group: AggregateGroup | undefined,
  key: AggregateGroupKey | undefined
): string | undefined {
  return key ? group?.[key] : undefined
}

/** The sources that fed a row and have a name: the server answers `""`
 * for a stream that names no source. */
export function namedSources(row: AggregateRow): string[] {
  return (row.sources ?? []).filter(Boolean)
}

/** The reducer columns an answer carries, in first-seen order. */
export function answerReducers(
  rows: readonly AggregateRow[]
): AggregateReducer[] {
  const seen = new Map<string, AggregateReducer>()
  for (const row of rows) {
    for (const v of row.values ?? []) {
      if (v.rolling !== undefined) continue
      const key = reducerKey(v.reducer)
      if (!seen.has(key)) seen.set(key, v.reducer)
    }
  }
  return [...seen.values()]
}

/** An answer as tab-separated text, the rollup table's copy: a header
 * line, then one line per row with the wire's own values (instants,
 * digits, codes and durations as served). */
export function rollupText(
  rows: readonly AggregateRow[],
  reducers: readonly AggregateReducer[],
  groupKey?: AggregateGroupKey
): string {
  const channels = rows.some((r) => r.group?.channel)
  const offsetless = rows.some((r) => r.offsetless)
  const head = [
    "start",
    "end",
    ...(groupKey ? [groupKey] : []),
    ...(channels ? ["channel"] : []),
    ...reducers.map(reducerKey),
    "count",
    ...(offsetless ? ["offsetless"] : []),
    "coverage",
    "sources",
    "from",
    "synthetic",
    "unavailable",
  ]
  const lines = rows.map((row) => [
    row.start,
    row.end,
    ...(groupKey ? [row.group?.[groupKey] ?? ""] : []),
    ...(channels ? [row.group?.channel ?? ""] : []),
    ...reducers.map((r) => {
      const v = rowValue(row, reducerKey(r))
      if (v?.value == null) return v?.code ?? ""
      return `${v.value}${v.unit ? ` ${v.unit}` : ""}`
    }),
    row.count === undefined ? "" : String(row.count),
    ...(offsetless ? [row.offsetless ? String(row.offsetless) : ""] : []),
    row.coverage ?? "",
    (row.sources ?? []).join(","),
    row.from ?? "",
    row.synthetic ? "true" : "",
    row.unavailable ? "true" : "",
  ])
  return [head, ...lines].map((l) => l.join("\t")).join("\n")
}
