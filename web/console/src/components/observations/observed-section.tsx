/** The record page's section for a kind binding core's `observed` trait,
 * switched on the way the Sync section is. A trace (the `packed` variant)
 * shows its values over `[at, endsAt)` as a sparkline, its managed `stats`
 * as labelled values and its points as a paged table; a single value shows
 * what was measured, by what, about what and how. Both link to the timeline
 * narrowed to the record's stream. */

import { useMemo, useState, type KeyboardEvent } from "react"
import { Link } from "@tanstack/react-router"
import { ChartLineIcon } from "lucide-react"

import { DataTablePagination } from "@/components/data-table/data-table-pagination"
import { EmptyValue } from "@/components/identity/empty-value"
import { ReferenceValue } from "@/components/identity/reference-value"
import { SectionHead } from "@/components/identity/section-head"
import { CodeMark, Reading, ShapeWord } from "@/components/observations/marks"
import { Button } from "@/components/ui/button"
import { useWidth } from "@/hooks/use-width"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { barPath, linePath, linearScale, niceDomain } from "@/lib/chart"
import { tableDateTime } from "@/lib/format"
import {
  isForm,
  observedVariant,
  readingText,
  recordTimelineSearch,
  type ReadingShape,
} from "@/lib/observations"
import { coverageWords, durationMs, secondWords } from "@/lib/observations-time"
import { numberOf } from "@/lib/observations-answer"
import {
  breakAtGaps,
  plainNumber,
  stddevOf,
  tracePoints,
  traceStats,
  type TracePoint,
  type TraceStats,
} from "@/lib/observations-trace"

const PAGE = 50

function str(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined
}

/** The record's form as a reader reads it: coded where it holds a code
 * rather than a number. */
function shapeOfRecord(p: Record<string, unknown>): ReadingShape | undefined {
  if (str(p.code) || Array.isArray(p.codes)) return "coded"
  return isForm(p.form) ? p.form : undefined
}

function stamp(t: number): string {
  return tableDateTime(new Date(t).toISOString())
}

export function ObservedSection({
  record,
  kind,
}: {
  record: SubstrateRecord
  kind?: KindInfo
}) {
  const variant = observedVariant(kind)
  if (!variant) return null
  const search = recordTimelineSearch(record.properties ?? {})
  const open = search && (
    <Button
      variant="ghost"
      size="sm"
      render={<Link to="/observations/timeline" search={search as never} />}
      nativeButton={false}
    >
      <ChartLineIcon />
      Open in the timeline
    </Button>
  )
  return variant === "packed" ? (
    <TraceSection record={record} open={open} />
  ) : (
    <ValueSection record={record} open={open} />
  )
}

/** A label above its value, in the property sheet's voice. */
function Fact({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[12.5px] text-faint">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  )
}

function ValueSection({
  record,
  open,
}: {
  record: SubstrateRecord
  open: React.ReactNode
}) {
  const p = record.properties ?? {}
  const value = p.value as { value?: unknown; unit?: unknown } | undefined
  const shape = shapeOfRecord(p)
  const at = str(p.at)
  const endsAt = str(p.endsAt)
  return (
    <>
      <SectionHead
        title="Measurement"
        hint="what was measured, by what, and when"
        actions={open}
      />
      <dl
        data-slot="observed-value"
        className="grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-x-6 gap-y-3.5"
      >
        <Fact label="Value">
          {str(p.code) ? (
            <CodeMark code={str(p.code)} />
          ) : value && typeof value.value === "string" ? (
            <Reading
              value={value.value}
              unit={str(value.unit)}
              className="text-[15px] font-medium"
            />
          ) : (
            <EmptyValue />
          )}
        </Fact>
        <Fact label="Metric">
          {p.metric ? <ReferenceValue value={p.metric} /> : <EmptyValue />}
        </Fact>
        <Fact label="Source">
          {p.source ? <ReferenceValue value={p.source} /> : <EmptyValue />}
        </Fact>
        <Fact label="About">
          {p.about ? (
            <ReferenceValue value={p.about} />
          ) : (
            <span className="text-muted-foreground">You</span>
          )}
        </Fact>
        <Fact label="Form">
          {shape ? <ShapeWord shape={shape} /> : <EmptyValue />}
        </Fact>
        {(str(p.statistic) || str(p.period)) && (
          <Fact label="Statistic">
            {[str(p.statistic), str(p.period) && `per ${str(p.period)}`]
              .filter(Boolean)
              .join(", ")}
          </Fact>
        )}
        <Fact label={endsAt ? "From" : "When"}>
          {at ? (
            <span title={at}>{stamp(Date.parse(at))}</span>
          ) : (
            <EmptyValue />
          )}
        </Fact>
        {endsAt && (
          <Fact label="Until">
            <span title={endsAt}>{stamp(Date.parse(endsAt))}</span>
          </Fact>
        )}
        <Fact label="Revision">
          {typeof p.revision === "number" ? (
            <span className="tabular-nums">{p.revision}</span>
          ) : (
            <EmptyValue />
          )}
        </Fact>
        <Fact label="Delivered by">
          {p.origin ? <ReferenceValue value={p.origin} /> : <EmptyValue />}
        </Fact>
      </dl>
    </>
  )
}

function TraceSection({
  record,
  open,
}: {
  record: SubstrateRecord
  open: React.ReactNode
}) {
  const p = record.properties ?? {}
  const points = useMemo(
    () => tracePoints(record.properties ?? {}),
    [record.properties]
  )
  const stats = traceStats(p.stats)
  const unit = str(p.unit)
  const shape = shapeOfRecord(p)
  const coded = shape === "coded"
  const [page, setPage] = useState(1)
  const shown = points.slice((page - 1) * PAGE, page * PAGE)
  const at = str(p.at)
  const endsAt = str(p.endsAt)
  const window = {
    start: at ? Date.parse(at) : (points[0]?.at ?? 0),
    end: endsAt ? Date.parse(endsAt) : (points.at(-1)?.at ?? 0),
  }
  return (
    <>
      <SectionHead
        title="Readings"
        hint={
          points.length === 1
            ? "1 value"
            : `${points.length.toLocaleString("en")} values`
        }
        actions={open}
      />
      {points.length > 0 && !coded && (
        <Sparkline
          points={points}
          window={window}
          unit={unit}
          holdMs={durationMs(str(p.hold))}
          bars={shape === "amount"}
        />
      )}
      {stats && <StatsFacts stats={stats} unit={unit} />}
      {points.length > 0 ? (
        <div className="mt-4">
          <table
            aria-label="Values"
            className="w-full max-w-[520px] text-[13px]"
          >
            <thead>
              <tr className="[&>th]:h-[34px] [&>th]:border-b [&>th]:border-border [&>th]:px-2.5 [&>th]:text-left [&>th]:text-[12.5px] [&>th]:font-medium [&>th]:text-faint">
                <th scope="col">Time</th>
                <th scope="col" className={coded ? undefined : "text-right!"}>
                  {coded ? "Category" : "Value"}
                </th>
              </tr>
            </thead>
            <tbody>
              {shown.map((pt, i) => (
                <tr
                  key={`${pt.at}-${i}`}
                  className="[&>td]:h-[30px] [&>td]:border-b [&>td]:border-border [&>td]:px-2.5"
                >
                  <td
                    className="tabular-nums"
                    title={new Date(pt.at).toISOString()}
                  >
                    {secondWords(pt.at)}
                  </td>
                  <td className={coded ? undefined : "text-right"}>
                    {coded ? (
                      <CodeMark code={pt.code ?? undefined} />
                    ) : (
                      <Reading value={pt.value} unit={unit} />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {points.length > PAGE && (
            <DataTablePagination
              className="mt-2 max-w-[520px]"
              page={page}
              pageSize={PAGE}
              rows={shown.length}
              total={points.length}
              hasNext={page * PAGE < points.length}
              onPage={setPage}
            />
          )}
        </div>
      ) : (
        <p className="text-muted-foreground">
          This trace holds no values the console can place in time.
        </p>
      )}
    </>
  )
}

function StatsFacts({ stats, unit }: { stats: TraceStats; unit?: string }) {
  const spread = stddevOf(stats)
  return (
    <dl
      data-slot="trace-stats"
      className="mt-4 grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-x-6 gap-y-3"
    >
      {stats.count !== undefined && (
        <Fact label="Values">
          <span className="tabular-nums">
            {stats.count.toLocaleString("en")}
          </span>
        </Fact>
      )}
      {stats.min !== undefined && (
        <Fact label="Lowest">
          <Reading value={stats.min} unit={unit} />
        </Fact>
      )}
      {stats.max !== undefined && (
        <Fact label="Highest">
          <Reading value={stats.max} unit={unit} />
        </Fact>
      )}
      {stats.mean !== undefined && (
        <Fact label="Average">
          <Reading value={plainNumber(stats.mean)} unit={unit} />
        </Fact>
      )}
      {spread !== undefined && (
        <Fact label="Standard deviation">
          <Reading value={plainNumber(spread)} unit={unit} />
        </Fact>
      )}
      {stats.first && (
        <Fact label="First">
          <Reading value={stats.first.value} unit={unit} />
          <span
            className="block text-[12.5px] text-faint"
            title={stats.first.at}
          >
            {stamp(Date.parse(stats.first.at))}
          </span>
        </Fact>
      )}
      {stats.last && (
        <Fact label="Last">
          <Reading value={stats.last.value} unit={unit} />
          <span
            className="block text-[12.5px] text-faint"
            title={stats.last.at}
          >
            {stamp(Date.parse(stats.last.at))}
          </span>
        </Fact>
      )}
      {stats.coverage && (
        <Fact label="Covered">
          <span title={stats.coverage}>{coverageWords(stats.coverage)}</span>
        </Fact>
      )}
    </dl>
  )
}

const SPARK_H = 72
const SPARK_PAD = 6

/** A trace's values over its period, one line (bars for amounts) on one
 * axis, broken wherever two values sit further apart than the trace's hold.
 * The pointer or the arrow keys pick a value, read out above the line; the
 * table under it holds every value. */
function Sparkline({
  points,
  window,
  unit,
  holdMs,
  bars,
}: {
  points: TracePoint[]
  window: { start: number; end: number }
  unit?: string
  holdMs?: number
  bars: boolean
}) {
  const { ref, width } = useWidth(640)
  const [active, setActive] = useState<number | null>(null)
  const numbers = points.map((p) => numberOf(p.value))
  const { domain } = niceDomain(
    numbers.filter((n): n is number => n !== null),
    { zero: bars, count: 2 }
  )
  const x = linearScale(
    [window.start, Math.max(window.end, window.start + 1)],
    [SPARK_PAD, width - SPARK_PAD]
  )
  const y = linearScale(domain, [SPARK_H - SPARK_PAD, SPARK_PAD])
  const line = breakAtGaps(points, holdMs).map((p) => {
    const n = p ? numberOf(p.value) : null
    return p && n !== null ? { x: x(p.at), y: y(n) } : null
  })
  const base = y(Math.max(domain[0], 0))

  function nearest(px: number): number {
    let best = 0
    let gap = Infinity
    points.forEach((p, i) => {
      const d = Math.abs(x(p.at) - px)
      if (d < gap) {
        gap = d
        best = i
      }
    })
    return best
  }

  function onKey(event: KeyboardEvent<HTMLDivElement>) {
    const last = points.length - 1
    if (event.key === "ArrowRight")
      setActive((a) => (a === null ? 0 : Math.min(last, a + 1)))
    else if (event.key === "ArrowLeft")
      setActive((a) => (a === null ? last : Math.max(0, a - 1)))
    else if (event.key === "Escape") setActive(null)
    else return
    event.preventDefault()
  }

  const picked = active !== null ? points[active] : undefined
  return (
    <div className="mt-1">
      <p
        role="status"
        className="h-5 text-[12.5px] text-muted-foreground tabular-nums"
      >
        {picked
          ? `${secondWords(picked.at)}: ${readingText(picked.value, unit)}`
          : `${stamp(window.start)} to ${stamp(window.end)}`}
      </p>
      <div
        ref={ref}
        tabIndex={0}
        role="group"
        aria-label="Values over the trace's period"
        aria-roledescription="chart"
        onKeyDown={onKey}
        onBlur={() => setActive(null)}
        className="rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <svg
          width={width}
          height={SPARK_H}
          aria-hidden
          className="block"
          onPointerMove={(e) => {
            const box = e.currentTarget.getBoundingClientRect()
            setActive(nearest(e.clientX - box.left))
          }}
          onPointerLeave={() => setActive(null)}
        >
          <line
            x1={SPARK_PAD}
            x2={width - SPARK_PAD}
            y1={base}
            y2={base}
            stroke="var(--border-strong)"
            shapeRendering="crispEdges"
          />
          {bars ? (
            numbers.map((n, i) =>
              n === null ? null : (
                <path
                  key={i}
                  d={barPath(x(points[i].at) - 1.5, 3, base, y(n), 1.5)}
                  fill="var(--series-1)"
                />
              )
            )
          ) : (
            <path
              d={linePath(line)}
              fill="none"
              stroke="var(--series-1)"
              strokeWidth={2}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          )}
          {picked && numberOf(picked.value) !== null && (
            <>
              <line
                x1={x(picked.at)}
                x2={x(picked.at)}
                y1={SPARK_PAD}
                y2={SPARK_H - SPARK_PAD}
                stroke="var(--border-strong)"
              />
              <circle
                cx={x(picked.at)}
                cy={y(numberOf(picked.value)!)}
                r={4}
                fill="var(--series-1)"
                stroke="var(--background)"
                strokeWidth={2}
              />
            </>
          )}
        </svg>
      </div>
      <span className="sr-only">
        {points.length} values from {stamp(window.start)} to {stamp(window.end)}
      </span>
    </div>
  )
}
