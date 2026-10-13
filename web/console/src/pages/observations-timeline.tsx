/** The timeline (`/observations/timeline`): the chosen metrics over one
 * range on a shared time axis, cut into buckets and reduced the way each
 * metric's form allows. One row of controls scopes every chart: the
 * metrics, the range, the bucket, what to group by, where to read from, and
 * the sources and things to keep. Each metric picks its own reducer and
 * output unit.
 *
 * The metrics overlay in one chart while they fit two units (the first on
 * the left axis, the second on the right) and otherwise split into as few
 * charts as hold them two units each (`lib/overlay.ts` splitCharts). Each
 * metric is still one aggregate read; a chart composes its metrics' reads
 * and carries each one's numbers under it, with the request itself in
 * technical mode. */

import { useEffect, useMemo } from "react"
import { Link } from "@tanstack/react-router"
import { ListIcon } from "lucide-react"
import { parseAsStringLiteral, useQueryState } from "nuqs"

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
import { useFacetValues, useObservations } from "@/hooks/use-observations"
import { useTimelineControls } from "@/hooks/use-timeline-controls"
import type { AggregateFrom } from "@/lib/api/types"
import { distinct, sameRef } from "@/lib/observations"
import { FROM_CHOICES } from "@/lib/observations-reads"
import { planPlot, splitCharts } from "@/lib/overlay"
import { shortRef } from "@/lib/stream-table"
import { ARRAY, FROM_VALUES, GROUP_VALUES } from "@/lib/timeline-address"

/** Where to read from, Automatic first: each metric reads what its streams
 * hold (`lib/overlay.ts` fromOf) until the reader picks. */
const FROM_PICKS: readonly { value: AggregateFrom | "auto"; label: string }[] =
  [{ value: "auto", label: "Automatic" }, ...FROM_CHOICES]

export function ObservationsTimelinePage() {
  const [metricParam, setMetricParam] = useQueryState("metric", ARRAY)
  const [sourceParam, setSources] = useQueryState("source", ARRAY)
  const [aboutParam, setAbouts] = useQueryState("about", ARRAY)
  // A stream's channel, where a row of the streams table named one.
  const [channels, setChannels] = useQueryState("channel", ARRAY)
  // Chosen metrics narrow discovery on the server by metric alone; the
  // chosen sources and things narrow it only while no metric is chosen, and
  // otherwise narrow each chart's read.
  const obs = useObservations({
    metrics: metricParam,
    sources: sourceParam,
    abouts: aboutParam,
  })
  // One spelling of each record, by id where its pin allows, so a metric's
  // choices, a picked option and a linked value all match.
  const { pins } = obs
  const sources = distinct(sourceParam.map((v) => shortRef(v, pins.source)))
  const abouts = distinct(aboutParam.map((v) => shortRef(v, pins.about)))
  const controls = useTimelineControls("7d")
  const { range, size } = controls
  const [group, setGroup] = useQueryState(
    "group",
    parseAsStringLiteral(GROUP_VALUES).withDefault("metric")
  )
  const [fromParam, setFrom] = useQueryState(
    "from",
    parseAsStringLiteral(FROM_VALUES)
  )

  const metricOptions = useMemo(() => {
    // Every metric with a point, whichever are chosen.
    const names = new Map(
      obs.metrics.map((m) => [shortRef(m, obs.pins.metric), obs.metricName(m)])
    )
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
    obs.streams[0] && !obs.discovery.isHeld
      ? shortRef(obs.streams[0].metric, obs.pins.metric)
      : undefined
  const metrics = metricParam.length
    ? distinct(metricParam.map((m) => shortRef(m, pins.metric)))
    : fallback
      ? [fallback]
      : []
  useEffect(() => {
    if (metricParam.length || !fallback) return
    void setMetricParam([fallback], { history: "replace" })
  }, [metricParam.length, fallback, setMetricParam])
  // The shown metrics' sources and things, each facet's under the other's
  // choice only, so a chosen source's menu still offers the ones to add.
  const values = useFacetValues(obs, { metrics, sources, abouts })
  const facets: Facet[] = [
    {
      // The picked order is the priority order a read resolves sources in.
      name: "source",
      label: "Source",
      ordered: true,
      orderHint: "Where sources overlap, the first is used first.",
      options: values.sources.map((value) => ({
        value: shortRef(value, pins.source),
        label: values.nameOf(value, pins.source),
      })),
    },
    {
      name: "about",
      label: "About",
      options: values.abouts.map((value) => ({
        value: shortRef(value, pins.about),
        label: values.nameOf(value, pins.about),
      })),
    },
    {
      // A source's own channels of the shown metrics (a WHOOP cycle's heart
      // rate beside its workouts'), with any the address names.
      name: "channel",
      label: "Channel",
      options: [
        ...new Set([
          ...obs.streams
            .filter(
              (s) =>
                metrics.some((m) => sameRef(s.metric, m)) &&
                (!sources.length || sources.some((v) => sameRef(s.source, v)))
            )
            .flatMap((s) => (s.channel ? [s.channel] : [])),
          ...channels,
        ]),
      ].map((value) => ({ value, label: value })),
    },
  ]
  // A metric is planned once its streams are read, so one being added waits
  // while the others stay drawn.
  const known = metrics.filter((m) => obs.knows(m))
  const plots = known.map((metric) =>
    planPlot(obs, metric, {
      range,
      size,
      timezone: controls.timezone,
      recordedLocal: controls.days === "local",
      group,
      from: fromParam ?? undefined,
      sources,
      abouts,
      channels,
      reducer: controls.reducerOf(metric),
      unit: controls.unitOf(metric),
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
          />
        </div>
        {obs.metricListError && (
          <MetricListError
            error={obs.metricListError}
            onRetry={() => obs.discovery.refetch()}
          />
        )}
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          <BucketControl controls={controls} />
          <DayControl controls={controls} />
          <GroupControl
            value={group}
            onChange={(next) => void setGroup(next === "metric" ? null : next)}
          />
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
          {charts.map((chart) => (
            <OverlayChart
              key={chart.map((p) => p.metric).join("\n")}
              obs={obs}
              plots={chart}
              range={range}
              size={size}
              title
              onReducer={controls.onReducer}
              onUnit={controls.onUnit}
              onZoom={controls.zoom}
            />
          ))}
          {known.length < metrics.length && (
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
