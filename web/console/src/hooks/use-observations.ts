/** What every observations surface reads first: the kinds binding the
 * `observed` trait, the kinds their references are pinned to, the metric
 * records (names, default forms, units, codes), the streams (one row per
 * metric, source and thing, from the latest read) and the titles of the
 * sources and the things observed.
 *
 * Discovery is partitioned, because one latest read makes at most 200
 * groups and a repository can hold more streams than that: a first read
 * lists the metrics with a point (one group each), then the streams are
 * read a few metrics at a time (`discoveryChunks`). A page's chosen
 * metrics, sources and things narrow every read on the server, except the
 * Source and About menus' own reads (`useFacetValues`), which leave out
 * the menu's own choice. */

import { useMemo } from "react"
import { useQueries, useQuery } from "@tanstack/react-query"

import { useReferenceTitles } from "@/hooks/use-reference-titles"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { aggregateQueryOptions } from "@/lib/api/observations"
import { recordsQueryOptions } from "@/lib/api/records"
import type { AggregateRow, KindInfo } from "@/lib/api/types"
import {
  discoveryChunks,
  facetRead,
  formOf,
  latestRead,
  metricInfoOf,
  metricWords,
  metricsRead,
  observedKinds,
  observedPins,
  refIdOf,
  refPathOf,
  sameRef,
  shapeOf,
  streamsOf,
  type Form,
  type MetricInfo,
  type ObservedPins,
  type ReadingShape,
  type Stream,
  type StreamNarrowing,
} from "@/lib/observations"
import { splitRecordPath } from "@/lib/record-path"

const NO_KINDS: KindInfo[] = []
const NO_ROWS: AggregateRow[] = []

/** How many metric records one read lists: a catalogue is a few hundred. */
const METRIC_PAGE = 500

/** Where discovery stands, across the reads the streams come from: pending
 * until every one has answered, and failed with the first one's error. The
 * list of every metric is one of them only where no metric is chosen. */
export interface Discovery {
  isPending: boolean
  isFetching: boolean
  isError: boolean
  error: Error | null
  refetch: () => void
}

export interface Observations {
  /** The registry is still loading. */
  pending: boolean
  /** The streams and the metric records have answered (or failed), so a
   * metric's form, codes and sources are known: a chart asks nothing
   * before, or it would ask in the wrong shape and ask again. */
  ready: boolean
  kinds: KindInfo[]
  observed: KindInfo[]
  /** The observed kinds' references, for query keys. */
  observedIds: string[]
  pins: ObservedPins
  /** Every metric with a point under the chosen sources and things, as the
   * group values carry them: what a metric picker offers, whichever
   * metrics are chosen. */
  metrics: string[]
  /** Why the list of every metric failed, where metrics are chosen: their
   * streams are read without it, so its failure costs a metric picker the
   * metrics not chosen, never the chosen streams, and the picker says so.
   * With none chosen the list is what discovery reads from, and its failure
   * is `discovery`'s; null where it loaded. */
  metricListError: Error | null
  streams: Stream[]
  discovery: Discovery
  /** The metric a group value names, where a record says. */
  metricOf: (value: string) => MetricInfo | undefined
  /** A metric's name: its record's, else its id read as words. */
  metricName: (value: string) => string
  /** The metric's record path, for a request. */
  metricPath: (value: string) => string | undefined
  /** A source's or a thing's title where it has one. */
  titleOf: (path: string | undefined) => string | undefined
  /** A record a group value names, in words: its title, else its id. */
  nameOf: (value: string, pin?: string) => string
  /** How a metric is read and the form its values are stored in: from its
   * newest stream (the value's own form, or a code), else its record. */
  readingOf: (value: string) => { shape: ReadingShape; form?: Form }
}

/** The address's values (ids or record paths) as record paths. */
function paths(values: readonly string[] | undefined, pin?: string): string[] {
  return (values ?? []).map((v) => refPathOf(v, pin) ?? v)
}

export function useObservations(narrow: StreamNarrowing = {}): Observations {
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? NO_KINDS
  const observed = useMemo(() => observedKinds(kinds), [kinds])
  const observedIds = useMemo(() => observed.map((k) => k.identity), [observed])
  const pins = useMemo(() => observedPins(observed), [observed])
  const enabled = observedIds.length > 0

  const chosenMetrics = paths(narrow.metrics, pins.metric)
  const sources = paths(narrow.sources, pins.source)
  const abouts = paths(narrow.abouts, pins.about)
  const catalogue = useQuery({
    ...aggregateQueryOptions(metricsRead({ sources, abouts }), observedIds),
    enabled,
  })
  const withPoints = useMemo(
    () =>
      (catalogue.data?.rows ?? NO_ROWS).flatMap((r) =>
        r.group?.metric ? [r.group.metric] : []
      ),
    [catalogue.data]
  )
  // The chosen metrics are read at once; with none chosen, every metric the
  // first read found, a few at a time.
  const toRead = chosenMetrics.length ? chosenMetrics : withPoints
  const reads = useQueries({
    queries: discoveryChunks(toRead).map((chunk) => ({
      ...aggregateQueryOptions(
        latestRead({ metrics: chunk, sources, abouts }),
        observedIds
      ),
      enabled,
    })),
    combine: combineReads,
  })
  const streams = useMemo(() => streamsOf({ rows: reads.rows }), [reads.rows])

  const waitsForCatalogue = enabled && !chosenMetrics.length
  const {
    isPending: cataloguePending,
    isFetching: catalogueFetching,
    error: catalogueError,
    refetch: refetchCatalogue,
  } = catalogue
  const metricListError = waitsForCatalogue ? null : catalogueError
  const discovery = useMemo<Discovery>(() => {
    const error = (waitsForCatalogue ? catalogueError : null) ?? reads.error
    return {
      isPending:
        enabled && ((waitsForCatalogue && cataloguePending) || reads.isPending),
      isFetching: catalogueFetching || reads.isFetching,
      isError: Boolean(error),
      error,
      refetch: () => {
        void refetchCatalogue()
        reads.refetch()
      },
    }
  }, [
    enabled,
    waitsForCatalogue,
    cataloguePending,
    catalogueFetching,
    catalogueError,
    refetchCatalogue,
    reads,
  ])

  const metricRecords = useQuery({
    ...recordsQueryOptions({
      kinds: pins.metric ? [pins.metric] : [],
      first: METRIC_PAGE,
    }),
    enabled: Boolean(pins.metric),
  })
  const metricsSettled = !pins.metric || !metricRecords.isPending

  const metrics = useMemo(() => {
    const out = new Map<string, MetricInfo>()
    for (const record of metricRecords.data?.records ?? []) {
      const info = metricInfoOf(record)
      out.set(info.id, info)
      out.set(info.path, info)
    }
    return out
  }, [metricRecords.data])

  const titlePaths = useMemo(() => {
    const out = new Set<string>()
    for (const s of streams) {
      const source = refPathOf(s.source, pins.source)
      const about = refPathOf(s.about, pins.about)
      if (source) out.add(source)
      if (about) out.add(about)
    }
    return [...out].sort()
  }, [streams, pins.source, pins.about])
  const titles = useReferenceTitles(titlePaths, kinds)

  const metricList = useMemo(() => {
    const out = [...withPoints]
    for (const s of streams) {
      if (!out.some((m) => sameRef(m, s.metric))) out.push(s.metric)
    }
    return out
  }, [withPoints, streams])

  return useMemo(() => {
    const metricOf = (value: string) =>
      metrics.get(value) ?? metrics.get(splitRecordPath(value)?.id ?? "")
    const titleOf = (path: string | undefined) =>
      path ? titles.get(path) || undefined : undefined
    return {
      pending: registry.isPending,
      ready: metricsSettled && !discovery.isPending,
      kinds,
      observed,
      observedIds,
      pins,
      metrics: metricList,
      metricListError,
      streams,
      discovery,
      metricOf,
      metricName: (value: string) =>
        metricOf(value)?.name ??
        metricWords(splitRecordPath(value)?.id ?? value),
      metricPath: (value: string) =>
        metricOf(value)?.path ?? refPathOf(value, pins.metric),
      titleOf,
      nameOf: (value: string, pin?: string) =>
        titleOf(refPathOf(value, pin)) ?? refIdOf(value),
      readingOf: (value: string) => {
        const metric = metricOf(value)
        // Streams are newest first, so this is the newest point's form.
        const stream = streams.find((s) => sameRef(s.metric, value))
        return {
          shape: shapeOf(metric, stream),
          form: formOf(metric, stream),
        }
      },
    }
  }, [
    registry.isPending,
    metricsSettled,
    kinds,
    observed,
    observedIds,
    pins,
    metricList,
    metricListError,
    streams,
    discovery,
    metrics,
    titles,
  ])
}

/** What the Source and About menus offer. */
export interface FacetValues {
  /** Every source with a point under the page's other choices, as the
   * group values carry them. */
  sources: string[]
  /** Every thing observed with a point under the page's other choices; the
   * owner names none and is not among them. */
  abouts: string[]
  /** A source or a thing in words: its title, else its id. */
  nameOf: (value: string, pin?: string) => string
}

/** The values each facet menu offers, one read per facet (`facetRead`):
 * under the page's metrics and the other facet's choice, never its own, so
 * a reopened menu still offers what a reader can add beside the chosen
 * values. The streams' own values join them, so a menu loses none while its
 * read is on its way or failed. `narrow.metrics` are the metrics the page
 * shows. */
export function useFacetValues(
  obs: Observations,
  narrow: StreamNarrowing
): FacetValues {
  const metrics = paths(narrow.metrics, obs.pins.metric)
  const sources = paths(narrow.sources, obs.pins.source)
  const abouts = paths(narrow.abouts, obs.pins.about)
  const enabled = obs.observedIds.length > 0
  const bySource = useQuery({
    ...aggregateQueryOptions(
      facetRead("source", { metrics, abouts }),
      obs.observedIds
    ),
    enabled,
  })
  const byAbout = useQuery({
    ...aggregateQueryOptions(
      facetRead("about", { metrics, sources }),
      obs.observedIds
    ),
    enabled,
  })

  const metricKey = metrics.join("\n")
  const values = useMemo(() => {
    const shown = obs.streams.filter(
      (s) =>
        !metricKey || metricKey.split("\n").some((m) => sameRef(s.metric, m))
    )
    const of = (
      rows: AggregateRow[] | undefined,
      key: "source" | "about",
      own: (string | undefined)[]
    ) => [
      ...new Set(
        [...(rows ?? NO_ROWS).map((r) => r.group?.[key]), ...own].filter(
          (v): v is string => Boolean(v)
        )
      ),
    ]
    return {
      sources: of(
        bySource.data?.rows,
        "source",
        shown.map((s) => s.source)
      ),
      abouts: of(
        byAbout.data?.rows,
        "about",
        shown.map((s) => s.about)
      ),
    }
  }, [bySource.data, byAbout.data, obs.streams, metricKey])

  const titlePaths = useMemo(() => {
    const out = new Set<string>()
    for (const v of values.sources) {
      const path = refPathOf(v, obs.pins.source)
      if (path) out.add(path)
    }
    for (const v of values.abouts) {
      const path = refPathOf(v, obs.pins.about)
      if (path) out.add(path)
    }
    return [...out].sort()
  }, [values, obs.pins.source, obs.pins.about])
  const titles = useReferenceTitles(titlePaths, obs.kinds)

  return useMemo(
    () => ({
      ...values,
      nameOf: (value: string, pin?: string) => {
        const path = refPathOf(value, pin)
        return (
          (path ? titles.get(path) || obs.titleOf(path) : undefined) ??
          refIdOf(value)
        )
      },
    }),
    [values, titles, obs]
  )
}

/** The stream reads as one answer: their rows in order, pending while any
 * is, and the first error. Module-level, so the combined result keeps its
 * identity while no read changes. */
function combineReads(
  results: {
    data?: { rows: AggregateRow[] }
    isPending: boolean
    isFetching: boolean
    error: Error | null
    refetch: () => unknown
  }[]
) {
  return {
    rows: results.flatMap((r) => r.data?.rows ?? NO_ROWS),
    isPending: results.some((r) => r.isPending),
    isFetching: results.some((r) => r.isFetching),
    error: results.find((r) => r.error)?.error ?? null,
    refetch: () => {
      for (const r of results) void r.refetch()
    },
  }
}
