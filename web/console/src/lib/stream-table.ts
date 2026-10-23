/** The streams table (`/observations`) in the records lists' terms: its
 * rows, the fields its filter bar offers (each a set of values to pick:
 * metrics, sources, things, readings, channels), the sort and grouping it
 * applies in the page (discovery loads every stream at once), the
 * selection it keeps in the address, and the shape a saved view of it
 * names. Pure functions of the streams and the address. */

import type { DeclaredProperty } from "@/lib/definition"
import { decodeFilters, type ActiveFilter } from "@/lib/filters"
import {
  addressValue,
  distinct,
  NONE,
  refIdOf,
  rowSearch,
  samePinned,
  sameRef,
  SHAPE_WORDS,
  type ReadingShape,
  type Stream,
} from "@/lib/observations"
import { durationMs } from "@/lib/observations-time"
import { numberOf } from "@/lib/observations-answer"
import { splitRecordPath } from "@/lib/record-path"
import type { ViewState } from "@/lib/saved-views"

/** The collection key the streams table's saved views are kept under on
 * the console preference record. */
export const STREAM_VIEWS = "observations/streams"

/** Newest first, as discovery lists the streams. */
export const DEFAULT_STREAM_SORT = "when:desc"

/** The About value for a stream about the owner, which names no record. An
 * id never starts with `~`, so this can never be one. */
export const OWNER = "~you"

/** One table row: the stream and the words its marks read as. A row that
 * folds several sources' streams of one metric, about and channel
 * (`foldSources`) holds them in `members` and names no source of its own;
 * a member drawn under its open folded row names that row in `parent`. */
export interface StreamRow extends Stream {
  metricName: string
  sourceTitle?: string
  aboutTitle?: string
  shape: ReadingShape
  members?: StreamRow[]
  parent?: StreamRow
}

/** The stream columns a reader filters and groups by. `shape` is how a
 * stream reads: its form, or a category for a coded one. */
export type StreamField = "metric" | "source" | "about" | "shape" | "channel"

export const STREAM_FIELDS: readonly StreamField[] = [
  "metric",
  "source",
  "about",
  "shape",
  "channel",
]

export const FIELD_LABELS: Record<StreamField, string> = {
  metric: "Metric",
  source: "Source",
  about: "About",
  shape: "Reading",
  channel: "Channel",
}

/** One value a field offers, in words. */
export interface FieldChoice {
  value: string
  label: string
}

/** A value with the separators of the filter bar (a comma between values)
 * and of a selection token (`|` between parts) escaped, since a channel may
 * hold any text. */
function escapePart(part: string): string {
  return part.replace(/%/g, "%25").replace(/\|/g, "%7C").replace(/,/g, "%2C")
}

function unescapePart(part: string): string {
  return part.replace(/%2C/g, ",").replace(/%7C/g, "|").replace(/%25/g, "%")
}

/** The filter bar's fields: each one a set of values to pick from, several
 * meaning any of them, in the bar's anatomy. A field with nothing to offer
 * is left out. */
export function streamFields(
  choices: Partial<Record<StreamField, readonly FieldChoice[]>>
): DeclaredProperty[] {
  return STREAM_FIELDS.flatMap((name) => {
    const values = choices[name]
    if (!values?.length) return []
    return [
      {
        name,
        kind: "enum",
        repeated: false,
        values: values.map((c) => ({
          value: escapePart(c.value),
          label: c.label,
        })),
      },
    ]
  })
}

/** The values chosen per field, from the address's filter tokens. */
export type StreamChoices = Record<StreamField, string[]>

export function streamChoices(tokens: readonly string[] | null): StreamChoices {
  const out: StreamChoices = {
    metric: [],
    source: [],
    about: [],
    shape: [],
    channel: [],
  }
  for (const f of decodeFilters(tokens ? [...tokens] : null)) {
    if (!(STREAM_FIELDS as readonly string[]).includes(f.field)) continue
    const values = f.value
      .split(",")
      .map((v) => unescapePart(v.trim()))
      .filter(Boolean)
    out[f.field as StreamField].push(...values)
  }
  return out
}

/** The filter one field's chosen values are, the way the bar writes it. */
export function choiceFilter(
  field: StreamField,
  values: readonly string[]
): ActiveFilter {
  return { field, op: "eq", value: values.map(escapePart).join(",") }
}

/** Whether a row holds every chosen field's value (one of them, where
 * several are chosen) and every word of the search. A source or a thing is
 * matched by path or by id alike; a metric by its path, a bare id read
 * under `metricPin` alone. */
export function streamMatches(
  row: StreamRow,
  choices: StreamChoices,
  search: string,
  metricPin?: string
): boolean {
  if (
    choices.metric.length &&
    !choices.metric.some((m) => samePinned(row.metric, m, metricPin))
  ) {
    return false
  }
  if (
    choices.source.length &&
    !choices.source.some((s) => sameRef(row.source, s))
  ) {
    return false
  }
  if (
    choices.about.length &&
    !choices.about.some((a) =>
      row.about ? sameRef(row.about, a) : a === OWNER
    )
  ) {
    return false
  }
  if (choices.shape.length && !choices.shape.includes(row.shape)) return false
  if (choices.channel.length && !choices.channel.includes(row.channel ?? "")) {
    return false
  }
  const words = search.toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return true
  const text = [
    row.metricName,
    row.metric,
    row.sourceTitle,
    row.source,
    row.aboutTitle,
    row.about,
    row.channel,
    SHAPE_WORDS[row.shape],
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase()
  return words.every((w) => text.includes(w))
}

// ── sort ───────────────────────────────────────────────────────────────────

/** The columns a reader sorts by. */
export const SORTABLE = [
  "metric",
  "source",
  "about",
  "channel",
  "shape",
  "latest",
  "when",
  "count",
  "coverage",
] as const

export type StreamSort = (typeof SORTABLE)[number]

/** A sort as the address spells it: `column:asc` or `column:desc`; an
 * unknown column is the default. */
export function parseStreamSort(sort: string): {
  id: StreamSort
  desc: boolean
} {
  const [id, dir] = sort.split(":")
  if (!(SORTABLE as readonly string[]).includes(id)) {
    return { id: "when", desc: true }
  }
  return { id: id as StreamSort, desc: dir !== "asc" }
}

/** What a column sorts a row by: words compare as words, numbers and times
 * as numbers; a row with nothing there sorts last either way. */
function sortValue(
  row: StreamRow,
  id: StreamSort
): string | number | undefined {
  switch (id) {
    case "metric":
      return row.metricName.toLowerCase()
    case "source":
      if (row.members) return sourcesWords(row.members).toLowerCase()
      return (
        row.sourceTitle ??
        (row.source && refIdOf(row.source))
      )?.toLowerCase()
    case "about":
      // The owner first: "You" names nobody.
      return row.about
        ? (row.aboutTitle ?? refIdOf(row.about)).toLowerCase()
        : ""
    case "channel":
      return row.channel?.toLowerCase()
    case "shape":
      return SHAPE_WORDS[row.shape].toLowerCase()
    case "latest":
      return numberOf(row.value) ?? row.code ?? undefined
    case "when": {
      const t = Date.parse(row.at)
      return Number.isNaN(t) ? undefined : t
    }
    case "count":
      return row.count
    case "coverage":
      return durationMs(row.coverage)
  }
}

/** The rows in a column's order, ties kept in the order they came. */
export function sortStreams(
  rows: readonly StreamRow[],
  sort: string
): StreamRow[] {
  const { id, desc } = parseStreamSort(sort)
  const keyed = rows.map((row, i) => ({ row, i, v: sortValue(row, id) }))
  keyed.sort((a, b) => {
    if (a.v === undefined || b.v === undefined) {
      if (a.v === b.v) return a.i - b.i
      return a.v === undefined ? 1 : -1
    }
    // A number and a word (a code beside readings) never compare: the
    // numbers lead either way.
    if (typeof a.v !== typeof b.v) return typeof a.v === "number" ? -1 : 1
    const c =
      typeof a.v === "number" && typeof b.v === "number"
        ? a.v - b.v
        : String(a.v).localeCompare(String(b.v))
    return (desc ? -c : c) || a.i - b.i
  })
  return keyed.map((k) => k.row)
}

// ── fold ───────────────────────────────────────────────────────────────────

/** A stream's source in words: its title, else its id, else "No source". */
export function sourceWords(row: Pick<StreamRow, "source" | "sourceTitle">) {
  return row.sourceTitle ?? (row.source ? refIdOf(row.source) : "No source")
}

/** The sources of a folded row's members in words, newest first. */
export function sourcesWords(members: readonly StreamRow[]): string {
  return distinct(members.map(sourceWords)).join(", ")
}

/** The rows with every metric, about and channel that several sources
 * measured folded into one row (decision record 0188): its newest value
 * and that value's time, form and statistic are the newest member's, its
 * points are every member's, and its members are those streams, newest
 * first. A channel stays a row of its own: a WHOOP workout's heart rate is
 * another context than its cycle's. A stream no other source shares stays
 * as it is. The folded row stands where its first member stood. */
export function foldSources(rows: readonly StreamRow[]): StreamRow[] {
  const keyOf = (r: StreamRow) =>
    [r.metric, r.about ?? "", r.channel ?? ""].join("\n")
  const runs = new Map<string, StreamRow[]>()
  for (const r of rows) {
    const key = keyOf(r)
    runs.set(key, [...(runs.get(key) ?? []), r])
  }
  const out: StreamRow[] = []
  for (const r of rows) {
    const run = runs.get(keyOf(r))!
    if (run.length < 2) {
      out.push(r)
      continue
    }
    if (run[0] !== r) continue
    const members = [...run].sort(
      (a, b) => (Date.parse(b.at) || 0) - (Date.parse(a.at) || 0)
    )
    const newest = members[0]
    const counts = members.flatMap((m) =>
      m.count === undefined ? [] : [m.count]
    )
    const statistics = distinct(members.flatMap((m) => m.statistics ?? []))
    out.push({
      ...newest,
      key: `sources\n${keyOf(r)}`,
      source: undefined,
      sourceTitle: undefined,
      count: counts.length ? counts.reduce((a, b) => a + b, 0) : undefined,
      // How long each source covered does not add up across sources that
      // overlap; each member says its own.
      coverage: undefined,
      statistics: statistics.length ? statistics : undefined,
      members,
    })
  }
  return out
}

/** The timeline's address for a row: a stream's own (`rowSearch`), and
 * for a folded row every member's source, one line each. */
export function rowTimelineSearch(row: StreamRow): Record<string, string> {
  const out = rowSearch(row)
  if (!row.members) return out
  return {
    ...out,
    source: distinct(
      row.members.map((m) => addressValue(m.source || NONE))
    ).join(","),
    group: "source",
  }
}

/** The rows with the members of each open folded row right under it. */
export function openFolds(
  rows: readonly StreamRow[],
  open: ReadonlySet<string>
): StreamRow[] {
  return rows.flatMap((r) =>
    r.members && open.has(r.key)
      ? [r, ...r.members.map((m) => ({ ...m, parent: r }))]
      : [r]
  )
}

/** The streams the rows stand for, each once: a folded row's members, a
 * member under an open row, every other row itself. */
export function rowStreams(rows: readonly StreamRow[]): StreamRow[] {
  const seen = new Set<string>()
  const out: StreamRow[] = []
  for (const r of rows) {
    for (const s of r.members ?? [r]) {
      if (seen.has(s.key)) continue
      seen.add(s.key)
      out.push(s)
    }
  }
  return out
}

// ── group ──────────────────────────────────────────────────────────────────

/** The fields a reader groups the rows by. */
export const STREAM_GROUPS: readonly StreamField[] = [
  "metric",
  "source",
  "about",
  "shape",
  "channel",
]

export function isStreamGroup(value: unknown): value is StreamField {
  return (STREAM_GROUPS as readonly unknown[]).includes(value)
}

/** The group a row is in: its value of the field, empty for none. A member
 * under an open folded row is in its row's group. */
export function streamGroupKey(row: StreamRow, group: StreamField): string {
  if (row.parent) return streamGroupKey(row.parent, group)
  switch (group) {
    case "metric":
      return row.metric
    case "source":
      return row.source ?? ""
    case "about":
      return row.about ?? ""
    case "shape":
      return row.shape
    case "channel":
      return row.channel ?? ""
  }
}

/** The rows in runs of one group each, the groups in the order of their
 * words (the empty group last), each run in the sort's order. */
export function groupStreams(
  rows: readonly StreamRow[],
  group: StreamField,
  wordsOf: (key: string) => string
): StreamRow[] {
  const keys: string[] = []
  const runs = new Map<string, StreamRow[]>()
  for (const row of rows) {
    const key = streamGroupKey(row, group)
    if (!runs.has(key)) {
      keys.push(key)
      runs.set(key, [])
    }
    runs.get(key)!.push(row)
  }
  keys.sort((a, b) => {
    // About's empty group is the owner, "You", and leads; every other
    // field's empty group holds the rows with none, and trails.
    if (!a || !b) {
      if (a === b) return 0
      const first = group === "about" ? -1 : 1
      return !a ? first : -first
    }
    return wordsOf(a).localeCompare(wordsOf(b))
  })
  return keys.flatMap((k) => runs.get(k)!)
}

// ── selection ──────────────────────────────────────────────────────────────

/** A record path as its id where the pin says its kind and the id cannot
 * be mistaken for a path, so an address stays short. */
export function shortRef(value: string | undefined, pin?: string): string {
  if (!value) return ""
  if (pin && value.startsWith(`${pin}/`)) {
    const id = value.slice(pin.length + 1)
    if (!splitRecordPath(id)) return id
  }
  return value
}

/** One selected stream as the address keeps it:
 * `metric|source|about|channel`, the metric by its record path (two metric
 * kinds may hold one id) and the source and the thing by id where their
 * pins allow. */
export function selectionToken(
  stream: Pick<Stream, "metric" | "source" | "about" | "channel">,
  pins: { metric?: string; source?: string; about?: string } = {}
): string {
  return [
    stream.metric,
    shortRef(stream.source, pins.source),
    shortRef(stream.about, pins.about),
    stream.channel ?? "",
  ]
    .map(escapePart)
    .join("|")
}

/** The metric a selection token names, its record path. */
export function tokenMetric(token: string): string {
  return unescapePart(token.split("|")[0] ?? "")
}

/** Whether a stream is the one a token names. */
export function tokenNames(
  token: string,
  stream: Pick<Stream, "metric" | "source" | "about" | "channel">
): boolean {
  const [metric = "", source = "", about = "", channel = ""] = token
    .split("|")
    .map(unescapePart)
  const same = (a: string, b?: string) => (a ? sameRef(b, a) : !b)
  return (
    stream.metric === metric &&
    same(source, stream.source) &&
    same(about, stream.about) &&
    channel === (stream.channel ?? "")
  )
}

/** The streams a selection names, in the order they were selected. A token
 * that names no stream (one that has gone, or a hand-edited one) is
 * skipped. */
export function selectedStreams<T extends Stream>(
  tokens: readonly string[],
  streams: readonly T[]
): T[] {
  const out: T[] = []
  for (const token of tokens) {
    const hit = streams.find((s) => tokenNames(token, s))
    if (hit && !out.includes(hit)) out.push(hit)
  }
  return out
}

/** The selection with rows `from` to `to` of the shown rows (inclusive,
 * either way round) checked or cleared: a click is one row, a shift-click
 * the run from the row clicked before. A newly checked stream joins the
 * end, so the order is the order of selecting. */
export function selectRange(
  tokens: readonly string[],
  rows: readonly Stream[],
  from: number,
  to: number,
  checked: boolean,
  pins: { metric?: string; source?: string; about?: string } = {}
): string[] {
  const lo = Math.max(0, Math.min(from, to))
  const hi = Math.min(rows.length - 1, Math.max(from, to))
  const run = rows.slice(lo, hi + 1)
  if (!checked) {
    return tokens.filter((t) => !run.some((r) => tokenNames(t, r)))
  }
  const out = [...tokens]
  for (const row of run) {
    if (!out.some((t) => tokenNames(t, row))) {
      out.push(selectionToken(row, pins))
    }
  }
  return out
}

/** The header box: every shown row checked, some, or none. */
export function selectionState(
  tokens: readonly string[],
  rows: readonly Stream[]
): "all" | "some" | "none" {
  const n = rows.filter((r) => tokens.some((t) => tokenNames(t, r))).length
  if (!n) return "none"
  return n === rows.length ? "all" : "some"
}

/** The selected streams of each metric, in the order the metrics were
 * first selected: one read per metric, narrowed to its selected streams'
 * sources, things and channels, `NONE` for a stream naming none (no source,
 * the owner, no channel), which the read asks for as a null inside `in`.
 * The filter narrows each key apart, so the read may still take a stream
 * the selection leaves out (one source's channel beside another's), which
 * the plot drops or names (`lib/overlay.ts` planPlot). */
export interface MetricSelection {
  metric: string
  streams: Stream[]
  sources: string[]
  abouts: string[]
  channels: string[]
}

export function selectionByMetric(
  selected: readonly Stream[]
): MetricSelection[] {
  const out: MetricSelection[] = []
  for (const stream of selected) {
    let entry = out.find((e) => sameRef(e.metric, stream.metric))
    if (!entry) {
      entry = {
        metric: stream.metric,
        streams: [],
        sources: [],
        abouts: [],
        channels: [],
      }
      out.push(entry)
    }
    entry.streams.push(stream)
  }
  for (const entry of out) {
    entry.sources = distinct(entry.streams.map((s) => s.source || NONE))
    entry.abouts = distinct(entry.streams.map((s) => s.about || NONE))
    entry.channels = distinct(entry.streams.map((s) => s.channel || NONE))
    // Newest first, as the plot reads its streams' forms.
    entry.streams.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
  }
  return out
}

// ── saved views ────────────────────────────────────────────────────────────

/** What the table shows, in the terms a saved view compares: the filters,
 * the sort, the grouping and the columns. Never the search, the selection
 * or the timeline panel: a view is a shape, not a question or a pick. */
export function streamViewState(page: {
  filter: readonly string[]
  sort: string
  group?: string | null
  columns: readonly string[]
  hidden: readonly string[]
}): ViewState {
  return {
    filter: [...page.filter],
    sort: page.sort,
    nest: true,
    group: page.group ?? undefined,
    columns: [...page.columns],
    hidden: [...page.hidden],
  }
}
