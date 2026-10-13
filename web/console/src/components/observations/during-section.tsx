/** The record page's "During this" section for a workout or a sleep (any
 * record of the health package spanning `[at, endsAt)` that is no measured
 * value itself, `lib/during.ts`): the heart rate over the span as one line
 * with its band, which a click or a drag zooms into; a workout's time in
 * each heart rate zone; a sleep's stages as a hypnogram where its source
 * recorded them in order, and its minutes in each stage; and a link that
 * opens the timeline over the same window. */

import { useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChartLineIcon } from "lucide-react"

import { SectionHead } from "@/components/identity/section-head"
import { CodeMark } from "@/components/observations/marks"
import { TimelineChart } from "@/components/observations/timeline-chart"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useWidth } from "@/hooks/use-width"
import { aggregateQueryOptions } from "@/lib/api/observations"
import { recordsQueryOptions } from "@/lib/api/records"
import {
  ApiError,
  type AggregateResult,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import {
  duringOf,
  duringTimelineSearch,
  heartCurveRead,
  hypnogramOf,
  stageListParams,
  stageRows,
  stageSegments,
  stageTotalsRead,
  zonesRead,
  type During,
  type StageSegment,
} from "@/lib/during"
import { linearScale } from "@/lib/chart"
import { displayName, lowerFirst } from "@/lib/kind-names"
import {
  codeWords,
  HEART_RATE_ZONES,
  readingText,
  reducerChoiceWords,
  zoneKey,
} from "@/lib/observations"
import {
  autoBucket,
  clockWords,
  durationWords,
  readerTimezone,
  windowWords,
  type TimeRange,
} from "@/lib/observations-time"
import {
  bucketsOf,
  numberOf,
  rowValue,
  seriesOn,
} from "@/lib/observations-answer"
import { unitSymbol } from "@/lib/units"

export function DuringSection({
  record,
  kind,
  kinds,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: readonly KindInfo[]
}) {
  const during = useMemo(
    () => duringOf(record, kind, kinds),
    [record, kind, kinds]
  )
  if (!during) return null
  return (
    <DuringParts
      during={during}
      noun={lowerFirst(displayName(kind ?? record.kind))}
    />
  )
}

/** Whether a read found nothing to say: an answer with no rows, or a
 * repository with no such metric record, which the server refuses by its
 * `metric.ref`. */
function noMetric(error: Error | null): boolean {
  return (
    error instanceof ApiError &&
    error.status === 422 &&
    error.problems.some((p) => p.startsWith("aggregate.metric"))
  )
}

function DuringParts({ during, noun }: { during: During; noun: string }) {
  const timezone = useMemo(() => readerTimezone(), [])
  // A zoom reads part of the span; the zones and the stages stay whole.
  const [view, setView] = useState<TimeRange>()
  const window = view ?? during.window
  const curve = useQuery(
    aggregateQueryOptions(
      heartCurveRead(during, window, timezone),
      during.observed
    )
  )
  const zones = useQuery({
    ...aggregateQueryOptions(zonesRead(during), during.observed),
    enabled: during.zones,
  })
  const totals = useQuery({
    ...aggregateQueryOptions(stageTotalsRead(during), during.observed),
    enabled: Boolean(during.stage),
  })
  const stages = useQuery({
    ...recordsQueryOptions(stageListParams(during)),
    enabled: Boolean(during.stage),
  })
  const open = (
    <Button
      variant="ghost"
      size="sm"
      render={
        <Link
          to="/observations/timeline"
          search={duringTimelineSearch(during) as never}
        />
      }
      nativeButton={false}
    >
      <ChartLineIcon />
      Open in the timeline
    </Button>
  )
  return (
    <section aria-label="During this">
      <SectionHead
        title="During this"
        hint={`what was measured during this ${noun}`}
        actions={open}
      />
      <Part title="Heart rate">
        <HeartCurve
          answer={curve.data}
          pending={curve.isPending}
          error={noMetric(curve.error) ? null : curve.error}
          window={window}
          noun={noun}
          zoomed={Boolean(view)}
          onZoom={setView}
          onReset={() => setView(undefined)}
        />
      </Part>
      {during.zones && (
        <Part title="Time in heart rate zones">
          {zones.isPending ? (
            <Skeleton className="h-24 w-full max-w-[520px]" />
          ) : zones.error && !noMetric(zones.error) ? (
            <Failed error={zones.error} />
          ) : (
            <ZoneBars answer={zones.data} />
          )}
        </Part>
      )}
      {during.stage && (
        <Part title="Sleep stages">
          {stages.isPending || totals.isPending ? (
            <Skeleton className="h-24 w-full max-w-[520px]" />
          ) : (
            <>
              {stages.data && (
                <Hypnogram
                  window={during.window}
                  segments={stageSegments(stages.data.records)}
                />
              )}
              {totals.error && !noMetric(totals.error) ? (
                <Failed error={totals.error} />
              ) : (
                <StageTotals answer={totals.data} />
              )}
              {stages.error && <Failed error={stages.error} />}
            </>
          )}
        </Part>
      )}
    </section>
  )
}

function Part({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="mt-3 flex flex-col gap-1.5" role="group" aria-label={title}>
      <h3 className="text-[13px] font-medium">{title}</h3>
      {children}
    </div>
  )
}

function Failed({ error }: { error: Error }) {
  return (
    <p className="text-[12.5px] text-destructive">
      This read failed: {error.message}
    </p>
  )
}

/** The heart rate over the window, one line with its lowest-to-highest
 * band; a click or a drag zooms into part of it. */
function HeartCurve({
  answer,
  pending,
  error,
  window,
  noun,
  zoomed,
  onZoom,
  onReset,
}: {
  answer?: AggregateResult
  pending: boolean
  error: Error | null
  window: TimeRange
  noun: string
  zoomed: boolean
  onZoom: (range: TimeRange) => void
  onReset: () => void
}) {
  if (pending) return <Skeleton className="h-[250px] w-full" />
  if (error) return <Failed error={error} />
  const rows = answer?.rows ?? []
  const buckets = bucketsOf([rows])
  const [series] = seriesOn(buckets, rows, "avg")
  const measured = series?.points.some((p) => p.value !== null) ?? false
  const unit = answer?.unit ?? "/min"
  return (
    <>
      {zoomed && (
        <div className="flex items-center gap-2 text-[12.5px] text-muted-foreground">
          <span>{windowWords(window.start, window.end)}</span>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-[12.5px] font-normal"
            onClick={onReset}
          >
            Show the whole {noun}
          </Button>
        </div>
      )}
      {measured ? (
        <TimelineChart
          // A new window is a new chart: no hover or drag carries over.
          key={`${window.start}-${window.end}`}
          buckets={buckets}
          series={[
            {
              key: "heart",
              color: "var(--series-1)",
              points: series.points,
              mark: "line",
              band: true,
            },
          ]}
          domain={window}
          size={autoBucket(window)}
          label={`Heart rate during this ${noun}`}
          units={{ left: unitSymbol(unit) || unit }}
          onZoom={(next) => onZoom(next)}
          tooltip={(b) => {
            const row = series.points[b]?.row
            const value = (key: string) => {
              const v = rowValue(row, key)
              return readingText(v?.value, v?.unit, 0)
            }
            return (
              <>
                <p className="font-medium">
                  {windowWords(buckets[b].start, buckets[b].end)}
                </p>
                {row ? (
                  <p className="mt-1">
                    Average {value("avg")}, lowest {value("min")}, highest{" "}
                    {value("max")}
                  </p>
                ) : (
                  <p className="mt-1 text-muted-foreground">
                    Nothing measured.
                  </p>
                )}
              </>
            )
          }}
        />
      ) : (
        <p className="text-[12.5px] text-muted-foreground">
          No heart rate was measured during this {noun}.
        </p>
      )}
    </>
  )
}

/** A bar per item, its length its share of the longest. */
function Bars({
  label,
  items,
}: {
  label: string
  items: { key: string; name: ReactNode; minutes: number | null }[]
}) {
  const most = Math.max(0, ...items.map((i) => i.minutes ?? 0))
  return (
    <dl
      aria-label={label}
      className="grid max-w-[520px] grid-cols-[auto_1fr_auto] items-center gap-x-3 gap-y-1 text-[13px]"
    >
      {items.map((item) => (
        <div key={item.key} className="contents">
          <dt className="text-muted-foreground">{item.name}</dt>
          <div aria-hidden className="h-2 rounded-full bg-hover">
            <div
              className="h-2 rounded-full bg-[var(--series-1)]"
              style={{
                width: most ? `${((item.minutes ?? 0) / most) * 100}%` : 0,
              }}
            />
          </div>
          <dd className="text-right tabular-nums">
            {item.minutes === null
              ? "none"
              : durationWords(item.minutes * 60_000)}
          </dd>
        </div>
      ))}
    </dl>
  )
}

/** The time in each heart rate zone, zone 1 to zone 5. */
function ZoneBars({ answer }: { answer?: AggregateResult }) {
  const row = answer?.rows[0]
  if (!row) {
    return (
      <p className="text-[12.5px] text-muted-foreground">
        No heart rate points to place in zones.
      </p>
    )
  }
  return (
    <Bars
      label="Time in each heart rate zone"
      items={HEART_RATE_ZONES.map((zone) => {
        const key = zoneKey(zone)
        return {
          key,
          name: reducerChoiceWords(key).replace(/^Time in z/, "Z"),
          minutes: numberOf(rowValue(row, key)?.value),
        }
      })}
    />
  )
}

/** The minutes in each stage, by the code's words. */
function StageTotals({ answer }: { answer?: AggregateResult }) {
  const rows = (answer?.rows ?? []).filter((r) => r.group?.code)
  if (!rows.length) {
    return (
      <p className="text-[12.5px] text-muted-foreground">
        No sleep stages were recorded for this sleep.
      </p>
    )
  }
  return (
    <Bars
      label="Time in each sleep stage"
      items={rows.map((r) => ({
        key: r.group!.code!,
        name: <CodeMark code={r.group!.code} />,
        minutes: numberOf(rowValue(r, "sum")?.value),
      }))}
    />
  )
}

const HYPNO_ROW = 18
const HYPNO_LEFT = 64
const HYPNO_AXIS = 18

/** A night's stages in order: a row per stage, a block per segment over
 * the sleep's span, so the night reads left to right. Drawn only where the
 * segments never overlap (`hypnogramOf`). */
function Hypnogram({
  window,
  segments,
}: {
  window: TimeRange
  segments: StageSegment[]
}) {
  const { ref, width } = useWidth(520)
  const night = hypnogramOf(segments)
  if (!night) return null
  const rows = stageRows(night)
  const x = linearScale(
    [window.start, window.end],
    [HYPNO_LEFT, Math.max(HYPNO_LEFT + 40, width - 8)]
  )
  const height = rows.length * HYPNO_ROW + HYPNO_AXIS
  const words = rows.map(codeWords).join(", ")
  return (
    <div ref={ref} className="w-full max-w-[720px]">
      <svg
        width={width}
        height={height}
        role="img"
        aria-label={`Sleep stages from ${clockWords(window.start)} to ${clockWords(window.end)}: ${night.length} segments of ${words}`}
        className="block"
        data-slot="hypnogram"
      >
        {rows.map((code, i) => (
          <text
            key={code}
            x={HYPNO_LEFT - 8}
            y={i * HYPNO_ROW + HYPNO_ROW / 2}
            textAnchor="end"
            dominantBaseline="middle"
            className="fill-faint text-[11.5px]"
          >
            {codeWords(code)}
          </text>
        ))}
        {night.map((s) => {
          const row = rows.indexOf(s.code)
          const x0 = x(Math.max(s.start, window.start))
          const x1 = x(Math.min(s.end, window.end))
          return (
            <rect
              key={`${s.start}-${s.code}`}
              data-stage={s.code}
              x={x0}
              y={row * HYPNO_ROW + 2}
              width={Math.max(1, x1 - x0)}
              height={HYPNO_ROW - 4}
              rx={2}
              fill={`var(--series-${(row % 8) + 1})`}
            >
              <title>
                {`${codeWords(s.code)}, ${clockWords(s.start)} to ${clockWords(s.end)}`}
              </title>
            </rect>
          )
        })}
        <text
          x={HYPNO_LEFT}
          y={height - 4}
          className="fill-faint text-[11.5px] tabular-nums"
        >
          {clockWords(window.start)}
        </text>
        <text
          x={x(window.end)}
          y={height - 4}
          textAnchor="end"
          className="fill-faint text-[11.5px] tabular-nums"
        >
          {clockWords(window.end)}
        </text>
      </svg>
    </div>
  )
}
