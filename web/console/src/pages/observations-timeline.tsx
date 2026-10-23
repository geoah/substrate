/** The timeline (`/observations/timeline`): the chosen metrics over one
 * range on a shared time axis, cut into buckets and reduced the way each
 * metric's form allows. One row of controls scopes every chart: the
 * metrics, the range, the bucket, what to group by, where to read from, and
 * the sources and things to keep. Each metric picks its own reducer and
 * output unit.
 *
 * The metrics overlay in one chart while they fit two units (the first on
 * the left axis, the second on the right) and otherwise split into as few
 * charts as hold them two units each (`lib/overlay.ts` splitCharts). The
 * metrics whose reads share everything but each metric's own choices are
 * one request, across charts, and each chart carries its metrics' numbers
 * under it, with the requests themselves in technical mode. */

import { useEffect, useMemo, useState } from "react"
import { useQueries } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ListIcon } from "lucide-react"
import {
  parseAsBoolean,
  parseAsString,
  parseAsStringLiteral,
  useQueryState,
} from "nuqs"

import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import {
  BucketControl,
  DayControl,
  GroupControl,
} from "@/components/observations/chart-controls"
import {
  DiscoveryError,
  FacetListErrors,
  MetricListError,
  RegistryError,
  StreamReadErrors,
} from "@/components/observations/discovery-error"
import {
  FacetFilters,
  type Facet,
} from "@/components/observations/facet-filters"
import {
  MeasurementSources,
  NoObservations,
} from "@/components/observations/no-observations"
import { OverlayChart } from "@/components/observations/overlay-chart"
import { Labelled, Picker } from "@/components/observations/picker"
import { RangePicker } from "@/components/observations/range-picker"
import { Button } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import { Skeleton } from "@/components/ui/skeleton"
import { useConnectGaps } from "@/hooks/use-console-preferences"
import { useFacetValues, useObservations } from "@/hooks/use-observations"
import { useTimelineControls } from "@/hooks/use-timeline-controls"
import { aggregateQueryOptions } from "@/lib/api/observations"
import type { AggregateFrom } from "@/lib/api/types"
import { chosenValue, distinct, NONE, sameRef } from "@/lib/observations"
import {
  earliestOf,
  earliestRead,
  FROM_CHOICES,
} from "@/lib/observations-reads"
import {
  endPlacedBucket,
  presetReaching,
  RANGE_PRESETS,
} from "@/lib/observations-time"
import { groupFixed, planPlot, splitCharts } from "@/lib/overlay"
import { shortRef } from "@/lib/stream-table"
import {
  ARRAY,
  FROM_VALUES,
  GROUP_VALUES,
  STREAM_KEYS,
} from "@/lib/timeline-address"

/** Where to read from, Automatic first: each metric reads what its streams
 * hold (`lib/overlay.ts` fromOf) until the reader picks. */
const FROM_PICKS: readonly { value: AggregateFrom | "auto"; label: string }[] =
  [{ value: "auto", label: "Automatic" }, ...FROM_CHOICES]

export function ObservationsTimelinePage() {
  const [metricParam, setMetricParam] = useQueryState("metric", ARRAY)
  const [sourceParam, setSources] = useQueryState("source", STREAM_KEYS)
  const [aboutParam, setAbouts] = useQueryState("about", STREAM_KEYS)
  // A stream's channel, where a row of the streams table named one.
  const [channels, setChannels] = useQueryState("channel", STREAM_KEYS)
  // Chosen metrics narrow discovery on the server by metric alone; the
  // chosen sources and things narrow it only while no metric is chosen, and
  // otherwise narrow each chart's read.
  const obs = useObservations({
    metrics: metricParam,
    sources: sourceParam,
    abouts: aboutParam,
  })
  // One spelling of each source and thing, by id where its pin allows, so a
  // picked option and a linked value match. A metric is always its record
  // path: two metric kinds may hold one id, and a bare id in the address is
  // read under the metric pin alone.
  const { pins } = obs
  const sources = distinct(sourceParam.map((v) => shortRef(v, pins.source)))
  const abouts = distinct(aboutParam.map((v) => shortRef(v, pins.about)))
  const [group, setGroup] = useQueryState(
    "group",
    parseAsStringLiteral(GROUP_VALUES).withDefault("metric")
  )
  // Whether lines run on across empty buckets: the address's where it says,
  // else the reader's preference. A toggle writes both.
  const [connectParam, setConnect] = useQueryState("connect", parseAsBoolean)
  const [connectPref, setConnectPref] = useConnectGaps()
  const connect = connectParam ?? connectPref
  const [fromParam, setFrom] = useQueryState(
    "from",
    parseAsStringLiteral(FROM_VALUES)
  )

  const metricOptions = useMemo(() => {
    // Every metric with a point, whichever are chosen.
    const names = new Map(obs.metrics.map((m) => [m, obs.metricName(m)]))
    return [...names]
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }, [obs])
  // With none in the address, the newest stream's metric, written there once
  // it is known: a later read whose newest point is another metric's never
  // swaps the chart under the reader. Never from streams held from other
  // sources and things: once written, the read of the chosen ones could not
  // correct it.
  const fallback =
    obs.streams[0] && !obs.discovery.isHeld ? obs.streams[0].metric : undefined
  const metrics = metricParam.length
    ? distinct(metricParam.map((m) => obs.metricPath(m) ?? m))
    : fallback
      ? [fallback]
      : []
  // `All` reads every chosen metric's earliest point first: the range runs
  // from the earliest of them to today.
  const [rangeKey] = useQueryState("range", parseAsString)
  const [opened] = useState(() => Date.now())
  const earliestReads = useQueries({
    // A metric is probed once its record is known: an address may name
    // it by a bare id the metric pin resolves.
    queries: (rangeKey === "all"
      ? metrics.filter((m) => obs.knows(m))
      : []
    ).map((m) => ({
      ...aggregateQueryOptions(earliestRead(m, opened), obs.observedIds),
      retry: false,
    })),
  })
  const earliest = earliestReads.some((r) => r.isPending)
    ? undefined
    : earliestReads.reduce<number | null>((min, r) => {
        const t = r.data ? earliestOf(r.data) : null
        return t !== null && (min === null || t < min) ? t : min
      }, null)
  const controls = useTimelineControls("7d", { earliest })
  const { range } = controls
  useEffect(() => {
    if (metricParam.length || !fallback) return
    void setMetricParam([fallback], { history: "replace" })
  }, [metricParam.length, fallback, setMetricParam])
  // The shown metrics' sources and things, each facet's under the other's
  // choice only, so a chosen source's menu still offers the ones to add.
  const values = useFacetValues(obs, { metrics, sources, abouts })
  // The shown metrics' streams, for the values a stream may name none of:
  // each facet offers NONE where one of them does, or the address names it.
  const shown = obs.streams.filter((s) =>
    metrics.some((m) => sameRef(s.metric, m))
  )
  // The newest value the chosen streams hold. A range that starts after it
  // (a provider that stopped syncing, a lab tested months ago) reads
  // nothing, and the page says when the newest value is and offers the
  // shortest range that reaches it.
  const newest = shown
    .filter(
      (s) =>
        !sources.length ||
        sources.some((src) =>
          src === NONE ? !s.source : Boolean(s.source && sameRef(s.source, src))
        )
    )
    .reduce((max, s) => Math.max(max, Date.parse(s.end ?? s.at) || 0), 0)
  const before = newest > 0 && newest < range.start ? newest : undefined
  const reaching = before === undefined ? undefined : presetReaching(before)
  const none = (
    key: "source" | "about" | "channel",
    chosen: readonly string[],
    label: string
  ) =>
    chosen.includes(NONE) || shown.some((s) => !s[key])
      ? [{ value: NONE, label }]
      : []
  const channelOptions = [
    ...new Set([
      ...shown
        .filter(
          (s) =>
            !sources.length || sources.some((v) => chosenValue(s.source, v))
        )
        .flatMap((s) => (s.channel ? [s.channel] : [])),
      ...channels.filter((c) => c !== NONE),
    ]),
  ].map((value) => ({ value, label: value }))
  const facets: Facet[] = [
    {
      // The picked order is the priority order a read resolves sources in.
      name: "source",
      label: "Source",
      ordered: true,
      orderHint: "Where sources overlap, the first is used first.",
      options: [
        ...values.sources.map((value) => ({
          value: shortRef(value, pins.source),
          label: values.nameOf(value, pins.source),
        })),
        ...(values.sources.length || sources.includes(NONE)
          ? none("source", sources, "No source")
          : []),
      ],
    },
    {
      name: "about",
      label: "About",
      options:
        values.abouts.length || abouts.includes(NONE)
          ? [
              ...none("about", abouts, "You"),
              ...values.abouts.map((value) => ({
                value: shortRef(value, pins.about),
                label: values.nameOf(value, pins.about),
              })),
            ]
          : [],
    },
    {
      // A source's own channels of the shown metrics (a WHOOP cycle's heart
      // rate beside its workouts'), with any the address names.
      name: "channel",
      label: "Channel",
      options:
        channelOptions.length || channels.includes(NONE)
          ? [...channelOptions, ...none("channel", channels, "No channel")]
          : [],
    },
  ]
  // A metric placed by its end reads a week in days, never in hours its
  // spans overfill.
  const size = metrics.some((m) => obs.metricOf(m)?.bucketPlace === "end")
    ? endPlacedBucket(controls.bucket, controls.size)
    : controls.size
  const sized = { ...controls, size }
  // A metric is planned once its streams are read, so one being added waits
  // while the others stay drawn; one whose read failed is said apart.
  const known = metrics.filter((m) => obs.knows(m) && !obs.failed(m))
  const waiting = metrics.filter((m) => !obs.knows(m)).length
  const plots = known.map((metric) =>
    planPlot(obs, metric, {
      range,
      size,
      timezone: controls.timezone,
      recordedLocal: controls.days === "local",
      group,
      from: fromParam ?? undefined,
      sources,
      // The Source filter's order is the priority the reader set.
      sourcesRanked: true,
      abouts,
      channels,
      reducer: controls.reducerOf(metric),
      unit: controls.unitOf(metric),
      statistic: controls.statisticOf(metric),
    })
  )
  const charts = splitCharts(plots)

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
        {obs.registryError ? (
          <RegistryError
            error={obs.registryError}
            onRetry={obs.retryRegistry}
          />
        ) : (
          <NoObservations />
        )}
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
            preset={controls.preset}
            range={range}
            onPreset={controls.onPreset}
            onCustom={controls.onCustom}
            all
          />
        </div>
        {obs.metricListError && (
          <MetricListError
            error={obs.metricListError}
            onRetry={() => obs.discovery.refetch()}
          />
        )}
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          <BucketControl controls={sized} />
          <DayControl controls={sized} />
          <GroupControl
            value={group}
            onChange={(next) => void setGroup(next === "metric" ? null : next)}
            fixed={groupFixed(plots)}
          />
          <button
            type="button"
            aria-pressed={connect}
            title={
              connect
                ? "Break each line where a bucket holds no value"
                : "Draw each line on across buckets with no value, the gap dashed"
            }
            className="inline-flex h-7 cursor-pointer items-center rounded-md border border-border-strong bg-background px-2.5 text-[12.5px] text-muted-foreground outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 aria-pressed:border-transparent aria-pressed:bg-primary-soft aria-pressed:text-primary-text"
            onClick={() => {
              void setConnect(!connect)
              setConnectPref(!connect)
            }}
          >
            Connect gaps
          </button>
          <Labelled label="Read from">
            <Segmented
              label="Read from"
              value={fromParam ?? "auto"}
              options={FROM_PICKS}
              onChange={(next) => void setFrom(next === "auto" ? null : next)}
            />
          </Labelled>
        </div>
        <FacetFilters
          facets={facets}
          selected={{ source: sources, about: abouts, channel: channels }}
          onChange={(name, values) =>
            void (
              name === "source"
                ? setSources
                : name === "about"
                  ? setAbouts
                  : setChannels
            )(values.length ? values : null)
          }
        />
        {!obs.discovery.isError && <FacetListErrors values={values} />}
      </div>
      {obs.discovery.isError ? (
        <DiscoveryError discovery={obs.discovery} />
      ) : metrics.length === 0 && (!obs.ready || obs.discovery.isHeld) ? (
        <Skeleton className="mt-8 h-[236px] w-full" />
      ) : metrics.length === 0 ? (
        sources.length || abouts.length ? (
          <p className="mt-6 text-muted-foreground">
            No stream matches the chosen sources and things. Remove a filter to
            see the others.
          </p>
        ) : (
          <div className="mt-6">
            <p className="text-muted-foreground">
              Nothing has been measured yet. A metric shows up here once a
              provider or a sample writes its first value.
            </p>
            <MeasurementSources className="mt-3" />
          </div>
        )
      ) : (
        <>
          <StreamReadErrors obs={obs} className="mt-4" />
          {controls.allPending && (
            <Skeleton
              role="status"
              aria-label="Reading the earliest point"
              className="mt-8 h-[236px] w-full"
            />
          )}
          {!controls.allPending && before !== undefined && (
            <p
              data-slot="timeline-older"
              className="mt-4 text-[12.5px] text-muted-foreground"
            >
              Nothing in this range: the newest value is from{" "}
              {new Date(before).toLocaleDateString(undefined, {
                day: "numeric",
                month: "short",
                year: "numeric",
              })}
              .{" "}
              {reaching ? (
                <Button
                  variant="link"
                  className="h-auto p-0 text-[12.5px]"
                  onClick={() => controls.onPreset(reaching)}
                >
                  Show the{" "}
                  {(
                    RANGE_PRESETS.find((p) => p.value === reaching)?.label ??
                    reaching
                  ).toLowerCase()}
                </Button>
              ) : (
                <Button
                  variant="link"
                  className="h-auto p-0 text-[12.5px]"
                  onClick={() =>
                    controls.onCustom({
                      start: before - 30 * 86_400_000,
                      end: range.end,
                    })
                  }
                >
                  Show from a month before it
                </Button>
              )}
            </p>
          )}
          {!controls.allPending &&
            charts.map((chart) => (
              <OverlayChart
                key={chart.map((p) => p.metric).join("\n")}
                obs={obs}
                plots={chart}
                range={range}
                size={size}
                title
                onReducer={controls.onReducer}
                onUnit={controls.onUnit}
                onStatistic={controls.onStatistic}
                onZoom={controls.zoom}
                connect={connect}
              />
            ))}
          {waiting > 0 && (
            <Skeleton
              role="status"
              aria-label="Reading the streams"
              className="mt-8 h-[236px] w-full"
            />
          )}
        </>
      )}
    </TablePage>
  )
}
