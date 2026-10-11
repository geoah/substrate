/** The timeline (`/observations/timeline`): the chosen metrics over one
 * range, each in its own chart on a shared time axis, cut into buckets and
 * reduced the way each metric's form allows. One row of controls scopes
 * every chart: the metrics, the range, the bucket, what to group by, where
 * to read from, and the sources and things to keep. Each chart picks its own
 * reducer and output unit, and carries its numbers in a table under it, with
 * the request itself in technical mode.
 *
 * One aggregate read per chart (`lib/observations.ts` laneRead), so a metric
 * is always one request and two metrics never share an axis. */

import { useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronDownIcon, ListIcon } from "lucide-react"
import {
  parseAsArrayOf,
  parseAsString,
  parseAsStringLiteral,
  useQueryState,
} from "nuqs"

import { CodeBlock } from "@/components/code-block"
import { CopyButton } from "@/components/identity/copy-button"
import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import { SectionHead } from "@/components/identity/section-head"
import {
  DiscoveryError,
  MetricListError,
} from "@/components/observations/discovery-error"
import {
  FacetFilters,
  type Facet,
} from "@/components/observations/facet-filters"
import {
  AboutMark,
  CodeMark,
  GroupRecord,
  ShapeWord,
} from "@/components/observations/marks"
import { NoObservations } from "@/components/observations/no-observations"
import { RangePicker } from "@/components/observations/range-picker"
import { RollupTable } from "@/components/observations/rollup-table"
import {
  TimelineChart,
  type ChartMark,
  type ChartSeries,
} from "@/components/observations/timeline-chart"
import { Button } from "@/components/ui/button"
import { ChoiceList, type ChoiceOption } from "@/components/ui/choice-list"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Segmented } from "@/components/ui/segmented"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useObservations, type Observations } from "@/hooks/use-observations"
import { aggregateCurl, aggregateQueryOptions } from "@/lib/api/observations"
import { ApiError } from "@/lib/api/types"
import {
  BUCKET_CHOICES,
  FROM_CHOICES,
  GROUP_CHOICES,
  DEFAULT_PRECISION,
  MAX_BUCKETS,
  RANGE_PRESETS,
  answerReducers,
  bucketEstimate,
  bucketOf,
  bucketSizeOf,
  codeWords,
  coverageWords,
  laneGroupKey,
  laneRead,
  laneReducers,
  latestRead,
  presetRange,
  readChoices,
  readerTimezone,
  readingText,
  reducerKey,
  reducerLabel,
  reducerName,
  reducerOptions,
  refPathOf,
  sameRef,
  seriesOf,
  unitChoices,
  windowWords,
  writeChoice,
  type AggregateFrom,
  type AggregateGroupKey,
  type BucketChoice,
  type BucketSize,
  type GroupChoice,
  type LaneSpec,
  type RangePreset,
  type ReadingShape,
  type TimeRange,
} from "@/lib/observations"
import { unitLabel } from "@/lib/units"
import { cn } from "@/lib/utils"

const ARRAY = parseAsArrayOf(parseAsString).withDefault([])
const RANGE_VALUES = [...RANGE_PRESETS.map((p) => p.value), "custom"] as const
const BUCKET_VALUES = BUCKET_CHOICES.map((b) => b.value)
const GROUP_VALUES = GROUP_CHOICES.map((g) => g.value)
const FROM_VALUES = FROM_CHOICES.map((f) => f.value)

const SIZE_WORDS: Record<BucketSize, string> = {
  "15m": "15 min",
  hour: "hour",
  day: "day",
  week: "week",
  month: "month",
}

function customRange(
  start: string | null,
  end: string | null
): TimeRange | undefined {
  const a = start ? Date.parse(start) : NaN
  const b = end ? Date.parse(end) : NaN
  return Number.isNaN(a) || Number.isNaN(b) || b <= a
    ? undefined
    : { start: a, end: b }
}

function distinct(values: (string | undefined)[]): string[] {
  return [...new Set(values.filter((v): v is string => Boolean(v)))]
}

/** How a chart draws a reducer's answer: categories stack, totals and
 * counts are bars, levels and rates are a line. */
function markOf(shape: ReadingShape, reducer: string): ChartMark {
  if (shape === "coded") return "stack"
  if (shape === "amount") return "bars"
  if (["sum", "count", "increase"].includes(reducer)) return "bars"
  return "line"
}

function Labelled({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-[12.5px] text-faint">{label}</span>
      {children}
    </div>
  )
}

/** A labelled button opening one list of choices. */
function Picker({
  label,
  options,
  selected,
  multiple = false,
  onChange,
}: {
  label: string
  options: ChoiceOption[]
  selected: string[]
  multiple?: boolean
  onChange: (next: string[]) => void
}) {
  const [open, setOpen] = useState(false)
  const words = selected.map(
    (v) => options.find((o) => o.value === v)?.label ?? v
  )
  const shown =
    words.length > 2
      ? `${words.slice(0, 2).join(", ")} and ${words.length - 2} more`
      : words.join(", ")
  return (
    <Labelled label={label}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              aria-label={`${label}: ${shown || "none"}`}
              className="h-7 max-w-80 gap-1.5 text-[12.5px] font-normal"
            />
          }
        >
          <span className="truncate">{shown || "Choose"}</span>
          <ChevronDownIcon className="size-3.5 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-0">
          <ChoiceList
            label={label}
            multiple={multiple}
            options={options}
            selected={selected}
            onChange={(next) => {
              onChange(next)
              if (!multiple) setOpen(false)
            }}
          />
        </PopoverContent>
      </Popover>
    </Labelled>
  )
}

export function ObservationsTimelinePage() {
  const [metricParam, setMetricParam] = useQueryState("metric", ARRAY)
  const [sources, setSources] = useQueryState("source", ARRAY)
  const [abouts, setAbouts] = useQueryState("about", ARRAY)
  // The chosen metrics, sources and things narrow discovery on the server.
  const obs = useObservations({ metrics: metricParam, sources, abouts })
  const [rangeParam, setRangeParam] = useQueryState(
    "range",
    parseAsStringLiteral(RANGE_VALUES).withDefault("7d")
  )
  const [startParam, setStart] = useQueryState("start", parseAsString)
  const [endParam, setEnd] = useQueryState("end", parseAsString)
  const [bucket, setBucket] = useQueryState(
    "bucket",
    parseAsStringLiteral(BUCKET_VALUES).withDefault("auto")
  )
  const [group, setGroup] = useQueryState(
    "group",
    parseAsStringLiteral(GROUP_VALUES).withDefault("metric")
  )
  const [fromParam, setFrom] = useQueryState(
    "from",
    parseAsStringLiteral(FROM_VALUES)
  )
  const [reducerTokens, setReducerTokens] = useQueryState("reducer", ARRAY)
  const [unitTokens, setUnitTokens] = useQueryState("unit", ARRAY)
  // Presets count back from when the page opened, so a re-render never
  // moves the window (and never re-asks).
  const [now] = useState(() => Date.now())

  const custom =
    rangeParam === "custom" ? customRange(startParam, endParam) : undefined
  const preset: RangePreset | "custom" = custom
    ? "custom"
    : rangeParam === "custom"
      ? "7d"
      : rangeParam
  const range = custom ?? presetRange(preset as RangePreset, now)
  const size = bucketSizeOf(bucket, range)
  const timezone = useMemo(() => readerTimezone(), [])

  const metricOptions = useMemo(() => {
    // Every metric with a point, whichever are chosen.
    const names = new Map(obs.metrics.map((m) => [m, obs.metricName(m)]))
    return [...names]
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }, [obs])
  const metrics = metricParam.length
    ? metricParam
    : obs.streams[0]
      ? [obs.streams[0].metric]
      : []
  const chosenStreams = obs.streams.filter((s) =>
    metrics.some((m) => sameRef(s.metric, m))
  )
  const facets: Facet[] = [
    {
      name: "source",
      label: "Source",
      options: distinct(chosenStreams.map((s) => s.source)).map((value) => ({
        value,
        label: obs.nameOf(value, obs.pins.source),
      })),
    },
    {
      name: "about",
      label: "About",
      options: distinct(chosenStreams.map((s) => s.about)).map((value) => ({
        value,
        label: obs.nameOf(value, obs.pins.about),
      })),
    },
  ]
  const from: AggregateFrom =
    fromParam ??
    (metrics.length > 0 &&
    metrics.every((m) => obs.readingOf(m).shape === "summary")
      ? "summaries"
      : "raw")
  const reducers = readChoices(reducerTokens)
  const units = readChoices(unitTokens)

  function zoom(next: TimeRange) {
    void setRangeParam("custom")
    void setStart(new Date(next.start).toISOString())
    void setEnd(new Date(next.end).toISOString())
    void setBucket(null)
  }

  const header = (
    <PageHeader
      title="Timeline"
      description="Your measurements over time, cut into buckets. Pick the metrics and the range; click a bucket or drag across several to zoom in."
      actions={
        <Button
          variant="outline"
          size="sm"
          render={<Link to="/observations" />}
          nativeButton={false}
        >
          <ListIcon />
          All streams
        </Button>
      }
    />
  )

  if (obs.pending) {
    return (
      <TablePage>
        <Skeleton className="h-7 w-48" />
        <Skeleton className="mt-3 h-4 w-96" />
      </TablePage>
    )
  }
  if (obs.observed.length === 0) {
    return (
      <TablePage>
        {header}
        <NoObservations />
      </TablePage>
    )
  }

  return (
    <TablePage>
      {header}
      <div
        data-slot="timeline-controls"
        className="mt-4 flex flex-col gap-2.5 border-b border-border pb-3"
      >
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          <Picker
            label="Metrics"
            multiple
            options={metricOptions}
            selected={metrics}
            onChange={(next) => void setMetricParam(next.length ? next : null)}
          />
          <RangePicker
            preset={preset}
            range={range}
            onPreset={(p) => {
              void setRangeParam(p === "7d" ? null : p)
              void setStart(null)
              void setEnd(null)
            }}
            onCustom={(next) => {
              void setRangeParam("custom")
              void setStart(new Date(next.start).toISOString())
              void setEnd(new Date(next.end).toISOString())
            }}
          />
        </div>
        {obs.metricListError && (
          <MetricListError
            error={obs.metricListError}
            onRetry={() => obs.discovery.refetch()}
          />
        )}
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          <Labelled label="Bucket">
            <Segmented
              label="Bucket"
              value={bucket}
              options={BUCKET_CHOICES.map((b) => ({
                value: b.value,
                label:
                  b.value === "auto" && bucket === "auto"
                    ? `Automatic: ${SIZE_WORDS[size]}`
                    : b.label,
              }))}
              onChange={(next: BucketChoice) =>
                void setBucket(next === "auto" ? null : next)
              }
            />
          </Labelled>
          <Labelled label="Group by">
            <Segmented
              label="Group by"
              value={group}
              options={GROUP_CHOICES}
              onChange={(next: GroupChoice) =>
                void setGroup(next === "metric" ? null : next)
              }
            />
          </Labelled>
          <Labelled label="Read from">
            <Segmented
              label="Read from"
              value={from}
              options={FROM_CHOICES}
              onChange={(next) => void setFrom(next)}
            />
          </Labelled>
        </div>
        <FacetFilters
          facets={facets}
          selected={{ source: sources, about: abouts }}
          onChange={(name, values) =>
            void (name === "source" ? setSources : setAbouts)(
              values.length ? values : null
            )
          }
        />
      </div>
      {obs.discovery.isError ? (
        <DiscoveryError discovery={obs.discovery} />
      ) : !obs.ready ? (
        <Skeleton className="mt-8 h-[236px] w-full" />
      ) : metrics.length === 0 ? (
        <p className="mt-6 text-muted-foreground">
          Nothing has been measured yet. A metric shows up here once a provider
          or a sample writes its first value.
        </p>
      ) : (
        metrics.map((metric) => (
          <TimelineLane
            key={metric}
            obs={obs}
            metric={metric}
            range={range}
            size={size}
            timezone={timezone}
            group={group}
            from={fromParam ?? undefined}
            sources={sources}
            abouts={abouts}
            reducer={reducers.get(metric)}
            unit={units.get(metric)}
            onReducer={(value) =>
              void setReducerTokens(
                writeChoice(reducerTokens, metric, value).length
                  ? writeChoice(reducerTokens, metric, value)
                  : null
              )
            }
            onUnit={(value) =>
              void setUnitTokens(
                writeChoice(unitTokens, metric, value).length
                  ? writeChoice(unitTokens, metric, value)
                  : null
              )
            }
            onZoom={zoom}
          />
        ))
      )}
    </TablePage>
  )
}

/** The stable colour of a series: its place among every value the lane
 * could show, so a filter never repaints the ones that stay. Past the
 * eighth, the quiet grey. */
function colorOf(key: string, universe: readonly string[]): string {
  const at = universe.indexOf(key)
  // A row with no group value, where the lane has no place for one, is the
  // lane's one series: the first slot.
  if (at < 0 && !key) return "var(--series-1)"
  return at >= 0 && at < 8 ? `var(--series-${at + 1})` : "var(--faint)"
}

function TimelineLane({
  obs,
  metric,
  range,
  size,
  timezone,
  group,
  from: pickedFrom,
  sources,
  abouts,
  reducer: picked,
  unit: pickedUnit,
  onReducer,
  onUnit,
  onZoom,
}: {
  obs: Observations
  metric: string
  range: TimeRange
  size: BucketSize
  timezone?: string
  group: GroupChoice
  /** The reader's pick; absent, a vendor summary reads summaries (it has
   * nothing else) and every other form raw points. */
  from?: AggregateFrom
  sources: string[]
  abouts: string[]
  reducer?: string
  unit?: string
  onReducer: (value: string) => void
  onUnit: (value: string) => void
  onZoom: (range: TimeRange) => void
}) {
  const [technical] = useTechnicalDetails()
  const info = obs.metricOf(metric)
  const name = obs.metricName(metric)
  const { shape, form } = obs.readingOf(metric)
  const from: AggregateFrom =
    pickedFrom ?? (shape === "summary" ? "summaries" : "raw")
  const precision = info?.displayPrecision ?? DEFAULT_PRECISION
  const codes = info?.codeSet
  const options = reducerOptions(shape, form)
  const reducer = picked && options.includes(picked) ? picked : options[0]
  const streams = obs.streams.filter((s) => sameRef(s.metric, metric))
  const stored = info?.unit ?? streams.find((s) => s.unit)?.unit
  const unitOptions = unitChoices(stored, info?.measures)
  const unit =
    pickedUnit && pickedUnit !== stored && unitOptions.includes(pickedUnit)
      ? pickedUnit
      : undefined
  // The address may spell a record by id or by path; the lane uses the
  // spelling its streams carry.
  const laneSources = distinct(streams.map((s) => s.source))
  const chosenSources = laneSources.filter((l) =>
    sources.some((s) => sameRef(l, s))
  )
  const noSource = sources.length > 0 && chosenSources.length === 0
  const laneAbouts = distinct(streams.map((s) => s.about))
  const chosenAbouts = laneAbouts.filter((l) =>
    abouts.some((a) => sameRef(l, a))
  )
  const noAbout = abouts.length > 0 && chosenAbouts.length === 0
  const metricPath = obs.metricPath(metric)
  // The budget counts groups times buckets: a guess at the groups before
  // the answer says (every chosen source or thing, a category's codes).
  const groups =
    shape === "coded"
      ? codes?.length || 4
      : group === "source"
        ? chosenSources.length || laneSources.length || 1
        : group === "about"
          ? (chosenAbouts.length || laneAbouts.length) + 1
          : 1
  const estimate = bucketEstimate(range, size, groups)
  const tooMany = estimate > MAX_BUCKETS

  const spec: LaneSpec | undefined = metricPath
    ? {
        metricPath,
        shape,
        reducer,
        unit,
        from,
        range,
        bucket: bucketOf(size, timezone),
        group,
        sources: chosenSources.map((s) => refPathOf(s, obs.pins.source) ?? s),
        sourceOrder: chosenSources.length ? chosenSources : laneSources,
        about: chosenAbouts.map((a) => refPathOf(a, obs.pins.about) ?? a),
      }
    : undefined
  const read = spec ? laneRead(spec) : undefined
  const runnable =
    obs.ready && Boolean(read) && !tooMany && !noSource && !noAbout
  const query = useQuery({
    ...aggregateQueryOptions(read ?? latestRead(), obs.observedIds),
    enabled: runnable,
  })

  const groupKey: AggregateGroupKey | undefined = spec
    ? laneGroupKey(spec)
    : undefined
  const pickedKey = spec ? reducerKey(laneReducers(spec)[0]) : reducer
  const rows = runnable ? (query.data?.rows ?? []) : []
  const answer = seriesOf(rows, pickedKey, groupKey)
  const buckets = answer.buckets
  const columns = answerReducers(rows)

  const universe =
    groupKey === "source"
      ? [...laneSources].sort()
      : groupKey === "about"
        ? ["", ...[...laneAbouts].sort()]
        : groupKey === "code"
          ? [...(codes ?? []), ...answer.series.map((s) => s.key).sort()]
          : [""]
  // Categories stack and list in the metric's own order, the colours with
  // them; other series keep the order the answer gave.
  const series =
    groupKey === "code"
      ? [...answer.series].sort(
          (a, b) => universe.indexOf(a.key) - universe.indexOf(b.key)
        )
      : answer.series
  const sourceText = (value: string) => obs.nameOf(value, obs.pins.source)
  const seriesText = (value: string | undefined): string => {
    if (groupKey === "source" && value) return sourceText(value)
    if (groupKey === "about")
      return value ? obs.nameOf(value, obs.pins.about) : "You"
    if (groupKey === "code" && value) return codeWords(value)
    return name
  }
  const groupCell = (value: string | undefined): ReactNode => {
    if (groupKey === "source" && value)
      return <GroupRecord value={value} pin={obs.pins.source} />
    if (groupKey === "about")
      return <AboutMark value={value} pin={obs.pins.about} />
    if (groupKey === "code") return <CodeMark code={value} />
    return value ?? ""
  }
  const chartSeries: ChartSeries[] = series.map((s) => ({
    key: s.key,
    value: s.value,
    label: seriesText(s.value),
    color: colorOf(s.key, universe),
    points: s.points,
  }))
  const mark = markOf(shape, reducer)

  function tooltip(b: number): ReactNode {
    const bucket = buckets[b]
    const shown = chartSeries.filter((s) => s.points[b]?.row)
    return (
      <>
        <p className="font-medium">{windowWords(bucket.start, bucket.end)}</p>
        {shown.length === 0 && (
          <p className="mt-1 text-muted-foreground">Nothing measured.</p>
        )}
        {shown.map((s) => {
          const row = s.points[b].row!
          return (
            <div key={s.key} className="mt-2 border-t border-border pt-2">
              {chartSeries.length > 1 && (
                <p className="mb-1 flex items-center gap-1.5 text-muted-foreground">
                  <span
                    aria-hidden
                    className="h-0.5 w-3 shrink-0 rounded-full"
                    style={{ background: s.color }}
                  />
                  {s.label}
                </p>
              )}
              <dl className="grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
                {row.values.map((v, i) => (
                  <TipLine
                    key={`${reducerKey(v.reducer)}-${v.code ?? ""}-${i}`}
                    label={
                      v.rolling !== undefined
                        ? `${reducerLabel(v.reducer)} of ${v.rolling}`
                        : reducerLabel(v.reducer)
                    }
                    value={
                      row.unavailable
                        ? "not available"
                        : v.value === null && v.code
                          ? codeWords(v.code)
                          : readingText(
                              v.value,
                              v.unit,
                              reducerName(v.reducer) === "count"
                                ? undefined
                                : precision
                            )
                    }
                    strong
                  />
                ))}
                <TipLine
                  label="Points"
                  value={row.count.toLocaleString("en")}
                />
                {row.coverage && (
                  <TipLine
                    label="Covered"
                    value={coverageWords(row.coverage)}
                  />
                )}
                {row.sources?.length ? (
                  <TipLine
                    label={row.sources.length === 1 ? "Source" : "Sources"}
                    value={row.sources.map(sourceText).join(", ")}
                  />
                ) : null}
                {row.from && row.from !== "raw" && (
                  <TipLine
                    label="From"
                    value={
                      row.from === "summaries" ? "Vendor summary" : row.from
                    }
                  />
                )}
              </dl>
              {row.synthetic && (
                <p className="mt-1 text-muted-foreground">
                  Filled in, not measured.
                </p>
              )}
              {!row.synthetic && row.count === 0 && (
                <p className="mt-1 text-muted-foreground">
                  No reading here; an earlier one still holds.
                </p>
              )}
              {row.unavailable && (
                <p className="mt-1 text-warning">
                  Not available: the points this needs have expired.
                </p>
              )}
            </div>
          )
        })}
      </>
    )
  }

  const notes: string[] = []
  if (groupKey !== "source" && spec && spec.sourceOrder.length > 1) {
    const [first, ...rest] = spec.sourceOrder.map(sourceText)
    notes.push(
      `Where sources overlap, ${first} is used first, then ${rest.join(", then ")}.`
    )
  }
  if (shape === "coded") notes.push("Stacked by category.")
  if (shape === "summary" && !pickedFrom) {
    notes.push("Read from the vendor's summaries, the only values it has.")
  }
  // `exact` is false whenever a float reduction was involved, so an average
  // is never exact: a developer's fact, not an everyday warning.
  if (technical && query.data && query.data.exact === false) {
    notes.push("Not exact: a float reduction or a rounded conversion.")
  }

  return (
    <section data-slot="timeline-lane" aria-label={name}>
      <SectionHead
        title={name}
        hint={<ShapeWord shape={shape} />}
        actions={
          <div className="flex flex-wrap items-center gap-3">
            <Picker
              label="Show"
              options={options.map((value) => ({
                value,
                label: reducerLabel(value),
              }))}
              selected={[reducer]}
              onChange={([next]) =>
                onReducer(next && next !== options[0] ? next : "")
              }
            />
            {unitOptions.length > 1 && (
              <Picker
                label="Unit"
                options={unitOptions.map((value) => ({
                  value,
                  label: unitLabel(value),
                }))}
                selected={[unit ?? stored ?? ""]}
                onChange={([next]) =>
                  onUnit(next && next !== stored ? next : "")
                }
              />
            )}
          </div>
        }
      />
      {technical && metricPath && (
        <IdText value={metricPath} copy className="-mt-1.5 mb-2" />
      )}
      {notes.map((note) => (
        <p key={note} className="mb-1.5 text-[12.5px] text-faint">
          {note}
        </p>
      ))}
      {!metricPath ? (
        <p className="text-muted-foreground">
          No record names this metric, so it can’t be read here.
        </p>
      ) : tooMany ? (
        <p className="text-muted-foreground">
          This read would make about {estimate.toLocaleString("en")} buckets
          counting every group, more than the {MAX_BUCKETS.toLocaleString("en")}{" "}
          one read may hold. Pick a larger bucket or a shorter range.
        </p>
      ) : noSource ? (
        <p className="text-muted-foreground">
          None of the chosen sources wrote this metric.
        </p>
      ) : noAbout ? (
        <p className="text-muted-foreground">
          This metric is about none of the chosen things.
        </p>
      ) : query.isError ? (
        <div className="text-destructive">
          <p>
            {query.error instanceof ApiError && query.error.status === 422
              ? "The server refused this read: "
              : "This read failed: "}
            {query.error.message}
          </p>
          {query.error instanceof ApiError &&
            query.error.problems.map((p) => (
              <p key={p} className="text-[12.5px]">
                {p}
              </p>
            ))}
        </div>
      ) : query.isPending ? (
        <Skeleton className="h-[236px] w-full" />
      ) : (
        <>
          {chartSeries.length > 1 && (
            <ul
              aria-label="Legend"
              className="mb-1.5 flex flex-wrap gap-x-4 gap-y-1 text-[12.5px] text-muted-foreground"
            >
              {chartSeries.map((s) => (
                <li key={s.key} className="flex items-center gap-1.5">
                  <span
                    aria-hidden
                    className={cn(
                      "shrink-0",
                      mark === "line"
                        ? "h-0.5 w-3 rounded-full"
                        : "size-2.5 rounded-[3px]"
                    )}
                    style={{ background: s.color }}
                  />
                  {groupCell(s.value) || s.label}
                </li>
              ))}
            </ul>
          )}
          <TimelineChart
            // A new window is a new chart: no hover or drag carries over.
            key={`${range.start}-${range.end}`}
            mark={mark}
            band={mark === "line" && shape === "gauge"}
            buckets={buckets}
            series={chartSeries}
            domain={range}
            size={size}
            label={`${name}, ${reducerLabel(reducer)}`}
            tooltip={tooltip}
            onZoom={onZoom}
            stale={query.isFetching}
          />
          {rows.length > 0 && (
            <RollupTable
              rows={rows}
              reducers={columns}
              groupKey={groupKey}
              groupCell={groupCell}
              sourceText={sourceText}
              label={`${name} in numbers`}
              precision={precision}
            />
          )}
        </>
      )}
      {technical && read && <RequestDetails read={read} />}
    </section>
  )
}

function TipLine({
  label,
  value,
  strong = false,
}: {
  label: string
  value: string
  strong?: boolean
}) {
  return (
    <>
      <dt className="text-faint">{label}</dt>
      <dd
        className={cn(
          "text-right tabular-nums",
          strong && "font-medium text-foreground"
        )}
      >
        {value}
      </dd>
    </>
  )
}

/** The request behind a chart, to run again with `curl`: the `filter` and
 * `aggregate` parameters as JSON, and the whole call. */
function RequestDetails({ read }: { read: ReturnType<typeof laneRead> }) {
  const json = JSON.stringify(read, null, 2)
  const curl = aggregateCurl(read)
  return (
    <div data-slot="lane-request" className="mt-3 rounded-md bg-panel p-2.5">
      <div className="flex items-center gap-2 text-[12.5px] text-faint">
        <span>Request</span>
        <CopyButton value={json} label="Copy the request" />
        <span className="ml-3">As curl</span>
        <CopyButton value={curl} label="Copy the curl command" />
      </div>
      <CodeBlock source={json} lang="json" className="mt-1.5" />
    </div>
  )
}
