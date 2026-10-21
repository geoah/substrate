/** A record view's table block: the records of one kind as rows with the
 * view's columns, those pointing here, those sharing a property's value
 * with this record, or those starting inside the view's window. A kind
 * binding core `observed` as single values gets its time, value, flag,
 * reference range (where it binds core `result`) and source by default, and
 * may draw its values over time as points, each against its own reference
 * range: a lab's results of one analyte over the years. A row whose `nest`
 * reference names another listed row sits under it and its point draws
 * hollow; what the reference means is the listed kind's, said by the
 * property's own label in the point's tooltip. With `show: tiles` each
 * result is a tile instead of a row (`Tiles`). */

import { useEffect, useMemo, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"

import {
  Failed,
  Loading,
  Notice,
  Part,
  Quiet,
  TileGrid,
  type BlockContext,
} from "./parts"
import { PointsChart, type ChartPoint } from "./points-chart"
import { StatTile } from "./stat-tile"
import { RecordRef } from "@/components/identity/record-ref"
import { DeclaredValue } from "@/components/property-sheet/property-value"
import { recordLink } from "@/components/record/record-model"
import {
  recordsQueryOptions,
  referenceTitlesQueryOptions,
  type ListParams,
} from "@/lib/api/records"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { CORE_PACKAGE } from "@/lib/api/http"
import { OBSERVED_TRAIT } from "@/lib/core-kinds"
import { kindByIdentity } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { displayName } from "@/lib/kind-names"
import {
  codeWords,
  DEFAULT_PRECISION,
  firstRange,
  flagWords,
  metricInfoOf,
  rangeWords,
  readingText,
  resultBar,
  resultReading,
  type MetricInfo,
} from "@/lib/observations"
import { clockWords, dayWords, type TimeRange } from "@/lib/observations-time"
import { splitRecordPath } from "@/lib/record-path"
import {
  humanizeName,
  propSpecsByName,
  type PropSpec,
} from "@/lib/record-schema"
import {
  cellFraction,
  changeSinceWords,
  tableParams,
  tileHistoryParams,
  type TableBlock,
} from "@/lib/record-view"
import { kindHasTrait } from "@/lib/traits"

/** Core's `result` trait: a lab's flags and reference ranges. */
const RESULT_TRAIT = `${CORE_PACKAGE}/result`

type Props = Record<string, unknown>

function isBag(v: unknown): v is Props {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

function text(v: unknown): string | undefined {
  return typeof v === "string" && v ? v : undefined
}

function instant(v: unknown): number | undefined {
  if (typeof v !== "string") return undefined
  const t = Date.parse(v)
  return Number.isNaN(t) ? undefined : t
}

function pathOf(r: SubstrateRecord): string {
  return `${r.kind}/${r.id}`
}

/** Whether a kind binds core `observed` as one value per record. */
function observedSingle(kind: KindInfo | undefined): boolean {
  const traits: unknown = kind?.definition?.traits
  return Array.isArray(traits) && traits.includes(`${OBSERVED_TRAIT}(single)`)
}

/** The record path a reference property holds: the reference, or the
 * first of a repeated one. */
function referenceAt(p: Props, name: string): string | undefined {
  const v = p[name]
  return readReference(Array.isArray(v) ? v[0] : v)?.path
}

function Ref({ path }: { path?: string }) {
  if (!path) return null
  const parts = splitRecordPath(path)
  return parts ? (
    <RecordRef kind={parts.kind} id={parts.id} />
  ) : (
    <span className="font-mono">{path}</span>
  )
}

/** A reading's instant with its day and clock. */
function whenWords(v: unknown): string {
  const t = instant(v)
  return t === undefined ? "" : `${dayWords(t, true)}, ${clockWords(t)}`
}

/** A single observed value in words: its code, its quantity, its text, or
 * why it has none. */
function valueText(p: Props): string {
  const code = text(p.code)
  if (code) return codeWords(code)
  const value = isBag(p.value) ? p.value : undefined
  if (
    value &&
    (typeof value.value === "string" || typeof value.value === "number")
  ) {
    return readingText(
      String(value.value),
      text(value.unit),
      DEFAULT_PRECISION,
      text(value.comparator)
    )
  }
  const said = text(p.text)
  if (said) return said
  const absent = text(p.absentReason)
  return absent ? codeWords(absent) : ""
}

/** The first reference range in words, and how many more there are. */
function rangesText(v: unknown): string {
  const range = firstRange(v)
  const first = range ? rangeWords(range) : undefined
  if (!first) return ""
  const more = Array.isArray(v) ? v.length - 1 : 0
  return more > 0 ? `${first} +${more} more` : first
}

/** One column: its header and how a row's cell reads. */
interface Column {
  key: string
  label: string
  cell: (r: SubstrateRecord) => ReactNode
}

/** The cells of the columns a view may name that the console reads by the
 * core `observed` and `result` traits' meaning, keyed by property, each with
 * whether the listed kind binds the trait that gives the property that
 * meaning. A kind that does not draws the property as it declares it. */
const NAMED: Record<
  string,
  {
    binds: (kind: KindInfo | undefined) => boolean
    cell: (p: Props) => ReactNode
  }
> = {
  metric: {
    binds: (k) => kindHasTrait(k, OBSERVED_TRAIT),
    cell: (p) => <Ref path={referenceAt(p, "metric")} />,
  },
  value: { binds: observedSingle, cell: (p) => valueText(p) },
  interpretation: {
    binds: (k) => kindHasTrait(k, RESULT_TRAIT),
    cell: (p) => flagWords(p.interpretation) ?? "",
  },
  referenceRanges: {
    binds: (k) => kindHasTrait(k, RESULT_TRAIT),
    cell: (p) => rangesText(p.referenceRanges),
  },
  at: {
    binds: (k) => kindHasTrait(k, OBSERVED_TRAIT),
    cell: (p) => whenWords(p.at),
  },
  source: {
    binds: (k) => kindHasTrait(k, OBSERVED_TRAIT),
    cell: (p) => <Ref path={referenceAt(p, "source")} />,
  },
}

function columnsOf(block: TableBlock, kind: KindInfo | undefined): Column[] {
  const specs = kind ? propSpecsByName(kind) : []
  const spec = (name: string): PropSpec | undefined =>
    specs.find((s) => s.name === name)
  const labelOf = (name: string) => spec(name)?.label ?? humanizeName(name)
  if (!block.columns.length) {
    if (observedSingle(kind)) {
      const result = kindHasTrait(kind, RESULT_TRAIT)
      return [
        {
          key: "when",
          label: "When",
          cell: (r) => whenWords(r.properties?.at),
        },
        {
          key: "value",
          label: "Value",
          cell: (r) => valueText(r.properties ?? {}),
        },
        ...(result
          ? [
              {
                key: "flag",
                label: "Flag",
                cell: (r: SubstrateRecord) =>
                  flagWords(r.properties?.interpretation) ?? "",
              },
              {
                key: "range",
                label: "Range",
                cell: (r: SubstrateRecord) =>
                  rangesText(r.properties?.referenceRanges),
              },
            ]
          : []),
        {
          key: "source",
          label: "Source",
          cell: (r) => <Ref path={referenceAt(r.properties ?? {}, "source")} />,
        },
      ]
    }
    return [
      {
        key: "title",
        label: "Title",
        cell: (r) => (
          <RecordRef
            kind={r.kind}
            id={r.id}
            title={recordTitle(r.properties ?? {}) || undefined}
          />
        ),
      },
      { key: "when", label: "When", cell: (r) => whenWords(r.properties?.at) },
    ]
  }
  return block.columns.map((name) => {
    const s = spec(name)
    const named =
      Object.hasOwn(NAMED, name) && NAMED[name].binds(kind)
        ? NAMED[name].cell
        : undefined
    return {
      key: name,
      label: labelOf(name),
      cell: (r: SubstrateRecord) => {
        const p = r.properties ?? {}
        if (named) return named(p)
        if (s?.kind === "reference") return <Ref path={referenceAt(p, name)} />
        if (s && !s.repeated && !s.keyed) {
          const short = cellFraction(s.kind, p[name])
          if (short !== undefined) {
            return (
              <span className="tabular-nums" title={String(p[name])}>
                {short}
              </span>
            )
          }
        }
        if (s) return <DeclaredValue spec={s} value={p[name]} />
        const v = p[name]
        return v === undefined || v === null
          ? ""
          : typeof v === "string"
            ? v
            : JSON.stringify(v)
      },
    }
  })
}

/** The rows in drawing order: a row whose `nest` reference names another
 * listed row is placed right under it, in the list's order, and indented;
 * one whose referent is not listed stays where it is. Both are marked as
 * holding the reference. */
function nestedOrder(
  records: readonly SubstrateRecord[],
  nest: string | undefined
): { record: SubstrateRecord; nested: boolean; refers: boolean }[] {
  if (!nest) {
    return records.map((record) => ({
      record,
      nested: false,
      refers: false,
    }))
  }
  const listed = new Set(records.map(pathOf))
  const parentOf = new Map<string, string>()
  for (const r of records) {
    const to = referenceAt(r.properties ?? {}, nest)
    if (to && listed.has(to) && to !== pathOf(r)) parentOf.set(pathOf(r), to)
  }
  const out: {
    record: SubstrateRecord
    nested: boolean
    refers: boolean
  }[] = []
  const placed = new Set<string>()
  const emit = (r: SubstrateRecord, nested: boolean) => {
    const path = pathOf(r)
    if (placed.has(path)) return
    placed.add(path)
    const refers = Boolean(referenceAt(r.properties ?? {}, nest))
    out.push({ record: r, nested, refers })
    for (const child of records) {
      if (parentOf.get(pathOf(child)) === path) emit(child, true)
    }
  }
  for (const r of records) {
    if (!parentOf.has(pathOf(r))) emit(r, false)
  }
  // A cycle of twins naming each other leaves rows no root reached.
  for (const r of records) emit(r, false)
  return out
}

export function TableBlockView({
  block,
  ctx,
  onEmpty,
}: {
  block: TableBlock
  ctx: BlockContext
  /** Said under `empty: hide`: whether the block drew nothing. */
  onEmpty?: (hidden: boolean) => void
}) {
  const listedKind = kindByIdentity([...ctx.kinds], block.kind)
  const params = tableParams(ctx.bound, block, ctx.record, listedKind)
  const problem = "problem" in params ? params.problem : undefined
  const page = useQuery({
    ...recordsQueryOptions(
      "problem" in params ? { kinds: [block.kind] } : params
    ),
    enabled: Boolean(listedKind) && !problem,
  })
  const records = useMemo(() => page.data?.records ?? [], [page.data])
  const hide = block.empty === "hide"
  const settled = !page.isPending || !listedKind || Boolean(problem)
  const hidden =
    hide &&
    settled &&
    !page.error &&
    !problem &&
    Boolean(listedKind) &&
    !records.length
  useEffect(() => {
    if (!hide || !settled) return
    onEmpty?.(hidden)
  }, [hide, settled, hidden, onEmpty])
  if (!listedKind) {
    return (
      <Notice>
        This table names a kind this repository does not declare: {block.kind}.
      </Notice>
    )
  }
  if (problem) return <Notice>{problem}</Notice>
  if (hidden) return null
  const name = block.label ?? displayName(listedKind)
  return (
    <Part title={block.label}>
      {block.chart && !observedSingle(listedKind) ? (
        <Notice>
          This table draws a chart of an observed single-value kind, and{" "}
          {block.kind} is not one.
        </Notice>
      ) : null}
      {block.show === "tiles" && !observedSingle(listedKind) ? (
        <Notice>
          This table draws an observed single-value kind's results as tiles, and{" "}
          {block.kind} is not one, so it lists them as rows.
        </Notice>
      ) : null}
      {page.isPending ? (
        <Loading />
      ) : page.error ? (
        <Failed error={page.error} onRetry={() => void page.refetch()} />
      ) : !records.length ? (
        <Quiet>None.</Quiet>
      ) : (
        <>
          {block.chart && observedSingle(listedKind) && (
            <Points
              records={records}
              block={block}
              kind={listedKind}
              ctx={ctx}
              label={`${name} over time`}
            />
          )}
          {block.show === "tiles" &&
          observedSingle(listedKind) &&
          !("problem" in params) ? (
            <Tiles
              records={records}
              block={block}
              table={params}
              label={name}
            />
          ) : (
            <Rows
              records={records}
              block={block}
              kind={listedKind}
              ctx={ctx}
              label={name}
            />
          )}
          {page.data.cursor && (
            <Quiet>
              {page.data.count !== undefined && page.data.count > records.length
                ? `and ${(page.data.count - records.length).toLocaleString("en")} more`
                : "and more"}
            </Quiet>
          )}
        </>
      )}
    </Part>
  )
}

function Rows({
  records,
  block,
  kind,
  ctx,
  label,
}: {
  records: readonly SubstrateRecord[]
  block: TableBlock
  kind: KindInfo
  ctx: BlockContext
  label: string
}) {
  const columns = columnsOf(block, kind)
  const own = pathOf(ctx.record)
  const rows = nestedOrder(records, block.nest)
  return (
    <div className="max-w-full overflow-x-auto">
      <table aria-label={label} className="w-full text-[13px]">
        <thead>
          <tr className="text-left text-muted-foreground">
            {columns.map((c) => (
              <th key={c.key} scope="col" className="px-2 py-1 font-normal">
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map(({ record, nested, refers }) => {
            const mine = pathOf(record) === own
            return (
              <tr
                key={pathOf(record)}
                data-record={pathOf(record)}
                data-nest-ref={refers ? "true" : undefined}
                aria-current={mine ? "true" : undefined}
                className={mine ? "bg-selection" : undefined}
              >
                {columns.map((c, i) => (
                  <td
                    key={c.key}
                    className={`px-2 py-1 align-top ${i === 0 && nested ? "pl-6" : ""}`}
                  >
                    {c.cell(record)}
                  </td>
                ))}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/** The rows' values over time, each against its own first reference range
 * (`resultReading`); the page's own record ringed, a row holding the
 * `nest` reference hollow, its tooltip naming what it refers to by the
 * property's label. The chart draws in one unit, the first result's: a
 * result in another unit of the same measure (g beside kg) is converted,
 * and one of another measure is listed and not drawn. A result written as
 * a bound (`<0.5`) draws as a marker toward its open side. Results of
 * several channels (a DXA scan's trunk and arms) draw one line each. */
function Points({
  records,
  block,
  kind,
  ctx,
  label,
}: {
  records: readonly SubstrateRecord[]
  block: TableBlock
  kind: KindInfo
  ctx: BlockContext
  label: string
}) {
  const own = pathOf(ctx.record)
  const nest = block.nest
  const nestLabel = nest
    ? (propSpecsByName(kind).find((s) => s.name === nest)?.label ??
      humanizeName(nest))
    : undefined
  const byPath = new Map(records.map((r) => [pathOf(r), r]))
  /** What a row's `nest` reference names, in words: the listed record's
   * title or time, else the record's id. */
  const nestNote = (p: Props): string | undefined => {
    const to = nest ? referenceAt(p, nest) : undefined
    if (!to || !nestLabel) return undefined
    const referent = byPath.get(to)
    const named = referent
      ? recordTitle(referent.properties ?? {}) ||
        whenWords(referent.properties?.at) ||
        referent.id
      : (splitRecordPath(to)?.id ?? to)
    return `${nestLabel}: ${named}`
  }
  const timed = records.flatMap((r) => {
    const p = r.properties ?? {}
    const at = instant(p.at)
    return at === undefined || !resultReading(p) ? [] : [{ r, p, at }]
  })
  const unit = timed.length ? resultReading(timed[0].p)?.unit : undefined
  // Each result in the newest's unit, converted within one measure; under
  // a unitless newest result only the unitless ones draw, never every
  // result in its own unit on one axis.
  const drawn = timed.flatMap((x) => {
    const reading = resultReading(x.p, unit)
    return reading && reading.unit === unit ? [{ ...x, reading }] : []
  })
  const other = timed.length - drawn.length
  const channels = new Set(drawn.map((x) => text(x.p.channel) ?? ""))
  const points: ChartPoint[] = drawn.map(({ r, p, at, reading }) => {
    const value = isBag(p.value) ? p.value : {}
    const channel = text(p.channel)
    const range = reading.range
    return {
      at,
      value: reading.value,
      ...(reading.comparator ? { comparator: reading.comparator } : {}),
      ...(range?.low !== undefined ? { low: range.low } : {}),
      ...(range?.high !== undefined ? { high: range.high } : {}),
      ...(range ? { rangeTone: range.tone } : {}),
      tone: reading.tone,
      // The lab's own digits and unit, whatever unit the chart draws in.
      title: `${readingText(String(value.value), text(value.unit), DEFAULT_PRECISION, text(value.comparator))} on ${dayWords(at, true)}`,
      hollow: Boolean(block.nest && referenceAt(p, block.nest)),
      notes: [
        ...(channels.size > 1
          ? [`Channel: ${channel ? codeWords(channel) : "none"}`]
          : []),
        ...(nestNote(p) ? [nestNote(p)!] : []),
      ],
      ...(channels.size > 1 ? { series: channel ?? "" } : {}),
      kind: r.kind,
      id: r.id,
    }
  })
  if (!points.length) return null
  const highlight = drawn.findIndex((x) => pathOf(x.r) === own)
  const domain: TimeRange =
    block.inWindow && ctx.bound?.window
      ? ctx.bound.window
      : (() => {
          const first = Math.min(...points.map((p) => p.at))
          const last = Math.max(...points.map((p) => p.at))
          const pad = (last - first) * 0.02 || 86_400_000
          return { start: first - pad, end: last + pad }
        })()
  return (
    <>
      <PointsChart
        points={points}
        domain={domain}
        unit={unit}
        highlight={highlight >= 0 ? highlight : undefined}
        label={label}
      />
      {other > 0 && (
        <Quiet>
          {other === 1
            ? "1 result in a unit this chart cannot convert to is listed and not drawn."
            : `${other} results in a unit this chart cannot convert to are listed and not drawn.`}
        </Quiet>
      )}
    </>
  )
}

/** The listed results as tiles, laid out as a stats block's: each headed by
 * its metric's name (and its channel, a DXA scan's region), linked to the
 * result's own page; its value with its flag in the flag's tone; a bar
 * against its first reference range (`resultBar`); and its change since the
 * previous result of the same metric and channel, all found in one list
 * read (`tileHistoryParams`). A row whose `nest` reference names a listed
 * result (a converted twin) is said on that result's tile, not drawn as one.
 * A flagged result comes first, then the rest by name. */
function Tiles({
  records,
  block,
  table,
  label,
}: {
  records: readonly SubstrateRecord[]
  block: TableBlock
  table: ListParams
  label: string
}) {
  const nest = block.nest
  const { tops, twins } = useMemo(() => {
    const listed = new Set(records.map(pathOf))
    const twins = new Map<string, SubstrateRecord[]>()
    const tops: SubstrateRecord[] = []
    for (const r of records) {
      const to = nest ? referenceAt(r.properties ?? {}, nest) : undefined
      if (to && listed.has(to) && to !== pathOf(r)) {
        twins.set(to, [...(twins.get(to) ?? []), r])
      } else {
        tops.push(r)
      }
    }
    return { tops, twins }
  }, [records, nest])
  const metrics = useMemo(() => {
    const paths = new Set<string>()
    for (const r of tops) {
      const m = referenceAt(r.properties ?? {}, "metric")
      if (m) paths.add(m)
    }
    return [...paths].sort()
  }, [tops])
  const targets = metrics.flatMap((m) => {
    const parts = splitRecordPath(m)
    return parts ? [parts] : []
  })
  const names = useQuery(
    referenceTitlesQueryOptions({
      kinds: [...new Set(targets.map((t) => t.kind))].sort(),
      ids: [...new Set(targets.map((t) => t.id))].sort(),
    })
  )
  const historyParams = tileHistoryParams(table, tops)
  const history = useQuery({
    ...recordsQueryOptions(historyParams ?? { kinds: table.kinds }),
    enabled: Boolean(historyParams),
  })
  const infos = useMemo(() => {
    const out = new Map<string, MetricInfo>()
    for (const r of names.data?.records ?? []) {
      const info = metricInfoOf(r)
      out.set(info.path, info)
    }
    return out
  }, [names.data])
  const previous = useMemo(() => {
    const out = new Map<string, SubstrateRecord>()
    for (const r of history.data?.records ?? []) {
      const p = r.properties ?? {}
      // A converted twin repeats a result in another unit; the result
      // itself is the one compared against.
      if (nest && referenceAt(p, nest)) continue
      const key = tileKey(p)
      if (key && !out.has(key)) out.set(key, r)
    }
    return out
  }, [history.data, nest])
  if (names.isLoading) return <Loading />
  const tiles = tops.map((r) => {
    const p = r.properties ?? {}
    const metric = referenceAt(p, "metric")
    const info = metric ? infos.get(metric) : undefined
    const named =
      info?.name ??
      (metric ? splitRecordPath(metric)?.id : undefined) ??
      (recordTitle(p) || r.id)
    const channel = text(p.channel)
    const places = info?.displayPrecision ?? DEFAULT_PRECISION
    const v = isBag(p.value) ? p.value : undefined
    const numeric =
      v && (typeof v.value === "string" || typeof v.value === "number")
    const reading = resultReading(p)
    const bar = resultBar(p)
    const flag = flagWords(p.interpretation)
    const before = previous.get(tileKey(p) ?? "")
    const then = before ? instant(before.properties?.at) : undefined
    const earlier =
      reading && !reading.comparator && before
        ? resultReading(before.properties ?? {}, reading.unit)
        : undefined
    const others = twins.get(pathOf(r)) ?? []
    return {
      key: pathOf(r),
      label: channel ? `${named}, ${codeWords(channel)}` : named,
      href: recordLink(r, ""),
      flagged: reading?.tone === "poor",
      value: numeric
        ? readingText(String(v.value), undefined, places, text(v.comparator))
        : valueText(p) || undefined,
      unit: numeric ? text(v.unit) : undefined,
      band: flag
        ? {
            label: flag.charAt(0).toUpperCase() + flag.slice(1),
            tone: reading?.tone ?? ("none" as const),
          }
        : undefined,
      bar,
      places,
      delta:
        earlier && !earlier.comparator && then !== undefined
          ? changeSinceWords(
              reading!.value,
              earlier.value,
              then,
              reading!.unit,
              places
            )
          : undefined,
      note: others.length
        ? `Also ${others.map((t) => valueText(t.properties ?? {})).join(", ")}`
        : undefined,
    }
  })
  tiles.sort(
    (a, b) =>
      Number(b.flagged) - Number(a.flagged) || a.label.localeCompare(b.label)
  )
  return (
    <>
      <TileGrid label={label}>
        {tiles.map((t) => (
          <div
            key={t.key}
            role="listitem"
            className="contents"
            data-record={t.key}
          >
            <StatTile
              label={t.label}
              href={t.href}
              value={t.value}
              unit={t.unit}
              band={t.band}
              show={t.bar ? "bar" : "number"}
              numeric={t.bar?.value}
              bands={t.bar ? [t.bar.band] : []}
              min={t.bar?.min}
              marked={Boolean(t.bar)}
              precision={t.places}
              delta={t.delta ? { text: t.delta, tone: "none" } : undefined}
              note={t.note}
            />
          </div>
        ))}
      </TileGrid>
      {history.error && (
        <Failed error={history.error} onRetry={() => void history.refetch()} />
      )}
    </>
  )
}

/** The analyte a result is compared within: its metric and its channel (a
 * DXA scan's trunk is never compared with its arms). */
function tileKey(p: Props): string | undefined {
  const metric = referenceAt(p, "metric")
  return metric ? `${metric}|${text(p.channel) ?? ""}` : undefined
}
