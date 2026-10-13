/** What every observations surface reads first: the kinds binding the
 * `observed` trait, the kinds their references are pinned to, the metric
 * records (names, default forms, units, codes), the streams (one row per
 * metric, source, thing and channel, from the latest read) and the titles of
 * the sources and the things observed.
 *
 * With no metric chosen, one latest read lists every stream under the
 * chosen sources and things, at the group cap (2000). Where the server
 * refuses it for making more groups, a first read lists the metrics with a
 * point (one group each) and the streams are read a few metrics at a time
 * (`discoveryChunks`). With metrics chosen, each one's streams are one read,
 * by metric alone: adding a metric keeps the others' answers, and choosing a
 * source or a thing re-reads no stream, since a page narrows its streams by
 * those itself. The list of every metric is then read beside them, under the
 * chosen sources and things, for a metric picker. The Source and About
 * menus' own reads (`useFacetValues`) leave out the menu's own choice. */

import { useMemo } from "react"
import {
  useQueries,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query"

import { useReferenceTitles } from "@/hooks/use-reference-titles"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { aggregateQueryOptions } from "@/lib/api/observations"
import { recordsQueryOptions } from "@/lib/api/records"
import type { AggregateResult, AggregateRow, KindInfo } from "@/lib/api/types"
import {
  metricInfoOf,
  metricWords,
  observedKinds,
  observedPins,
  refIdOf,
  refPathOf,
  sameRef,
  streamsOf,
  type MetricInfo,
  type ObservedPins,
  type Stream,
} from "@/lib/observations"
import {
  discoveryChunks,
  facetRead,
  latestRead,
  MAX_STREAM_READS,
  metricsRead,
  overGroupCap,
  type StreamFacet,
  type StreamNarrowing,
} from "@/lib/observations-reads"
import { splitRecordPath } from "@/lib/record-path"

const NO_KINDS: KindInfo[] = []
const NO_ROWS: AggregateRow[] = []

/** How many metric records one read lists: a catalogue is a few hundred. */
const METRIC_PAGE = 500

/** A chosen metric whose own stream read failed while another chosen
 * metric's answered: its streams are missing, and a page says so where its
 * chart would be (`StreamReadErrors`) and draws the others. */
export interface StreamReadFailure {
  metric: string
  error: Error
  retry: () => void
}

/** Where discovery stands, across the reads the streams come from. */
export interface Discovery {
  /** Nothing has answered yet, so there is nothing to show. */
  isPending: boolean
  /** A read is on its way: the first, or a newer one while an earlier
   * answer is shown. */
  isFetching: boolean
  /** The streams shown are an earlier read's, under other choices, while
   * the read of the current ones loads. */
  isHeld: boolean
  isError: boolean
  /** The first failed read's error. With metrics chosen, only where every
   * metric's read failed: one metric's failure is `failedMetrics`'. */
  error: Error | null
  refetch: () => void
}

export interface Observations {
  /** The registry is still loading. */
  pending: boolean
  /** Why the registry did not load, null where it did: with no registry no
   * page can tell whether anything records measurements. */
  registryError: Error | null
  retryRegistry: () => void
  /** Every stream read has answered (or failed) and so have the metric
   * records, so every metric's form, codes and sources are known. */
  ready: boolean
  /** Whether one metric's streams have been read (or their read failed) and
   * the metric records have answered: a chart plans a metric only then, or
   * it would ask in the wrong shape and ask again. */
  knows: (metric: string) => boolean
  /** The chosen metrics whose stream read failed while another's answered,
   * in the order chosen. Empty where none failed or every one did. */
  failedMetrics: StreamReadFailure[]
  /** Whether a metric is among `failedMetrics`: a page plans it not. */
  failed: (metric: string) => boolean
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
  /** The streams, newest first: with metrics chosen, every stream of each
   * (whatever source or thing is chosen); with none, every stream under the
   * chosen sources and things. */
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
}

/** What a surface asks discovery for besides its narrowing. */
export interface DiscoveryOptions {
  /** Read no stream and no list of metrics: a panel with nothing selected
   * has nothing to read. On by default. */
  enabled?: boolean
  /** With metrics chosen, read the list of every metric beside their
   * streams, for a metric picker. On by default; a panel that draws a
   * selection has no picker. */
  catalogue?: boolean
}

/** The address's values (ids or record paths) as record paths. */
function paths(values: readonly string[] | undefined, pin?: string): string[] {
  return (values ?? []).map((v) => refPathOf(v, pin) ?? v)
}

export function useObservations(
  narrow: StreamNarrowing = {},
  options: DiscoveryOptions = {}
): Observations {
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? NO_KINDS
  const observed = useMemo(() => observedKinds(kinds), [kinds])
  const observedIds = useMemo(() => observed.map((k) => k.identity), [observed])
  const pins = useMemo(() => observedPins(observed), [observed])
  const enabled = options.enabled !== false && observedIds.length > 0

  const chosen = paths(narrow.metrics, pins.metric)
  const sources = paths(narrow.sources, pins.source)
  const abouts = paths(narrow.abouts, pins.about)

  // No metric chosen: every stream in one read, unless it makes too many
  // groups, and then the metrics first and their streams in batches. A
  // refused read is not asked again under the same choices: a refetch
  // clears its error while it runs, which would drop the batches and their
  // rows until the server refused again.
  const whole = useQuery({
    ...aggregateQueryOptions(latestRead({ sources, abouts }), observedIds),
    enabled: (query) =>
      enabled && !chosen.length && !overGroupCap(query.state.error),
  })
  const split = !chosen.length && overGroupCap(whole.error)
  const catalogue = useQuery({
    ...aggregateQueryOptions(metricsRead({ sources, abouts }), observedIds),
    enabled:
      enabled && (split || (chosen.length > 0 && options.catalogue !== false)),
  })
  const withPoints = useMemo(
    () =>
      (catalogue.data?.rows ?? NO_ROWS).flatMap((r) =>
        r.group?.metric ? [r.group.metric] : []
      ),
    [catalogue.data]
  )
  // One read per chosen metric, so each keeps its answer while the others
  // change; past `MAX_STREAM_READS`, a few at a time.
  const batches = chosen.length
    ? chosen.length <= MAX_STREAM_READS
      ? chosen.map((m) => [m])
      : discoveryChunks(chosen)
    : split
      ? discoveryChunks(withPoints)
      : []
  const client = useQueryClient()
  const reads = useQueries({
    queries: batches.map((batch) => ({
      ...aggregateQueryOptions(
        latestRead(
          chosen.length
            ? { metrics: batch }
            : { metrics: batch, sources, abouts }
        ),
        observedIds
      ),
      // A metric chosen from every stream already read starts from them.
      placeholderData: (prev?: AggregateResult) =>
        prev ??
        (chosen.length ? streamsRead(client, batch, observedIds) : undefined),
      enabled,
    })),
    combine: combineReads,
  })
  const rows = chosen.length || split ? reads.rows : whole.data?.rows
  const streams = useMemo(() => streamsOf({ rows: rows ?? NO_ROWS }), [rows])

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

  const metricListError = chosen.length ? catalogue.error : null
  const chosenKey = chosen.join("\n")
  const batchKey = batches.map((b) => b.join("\n")).join("\t")
  const {
    isPending: wholePending,
    isFetching: wholeFetching,
    isPlaceholderData: wholeHeld,
    error: wholeError,
    refetch: refetchWhole,
  } = whole
  const {
    isPending: cataloguePending,
    isFetching: catalogueFetching,
    error: catalogueError,
    refetch: refetchCatalogue,
  } = catalogue
  const discovery = useMemo<Discovery>(() => {
    // One chosen metric's failure leaves the others' streams to show.
    const error = chosenKey
      ? reads.results.every((r) => r.error)
        ? reads.error
        : null
      : split
        ? (catalogueError ?? reads.error)
        : wholeError
    return {
      isPending:
        enabled &&
        (chosenKey
          ? reads.results.every((r) => r.isPending)
          : split
            ? cataloguePending || reads.isPending
            : wholePending),
      isFetching: wholeFetching || catalogueFetching || reads.isFetching,
      isHeld: !chosenKey && !split && wholeHeld,
      isError: Boolean(error),
      error,
      refetch: () => {
        if (!chosenKey && !split) void refetchWhole()
        void refetchCatalogue()
        reads.refetch()
      },
    }
  }, [
    enabled,
    chosenKey,
    split,
    reads,
    wholePending,
    wholeFetching,
    wholeHeld,
    wholeError,
    refetchWhole,
    cataloguePending,
    catalogueFetching,
    catalogueError,
    refetchCatalogue,
  ])

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

  const { error: registryError, refetch: refetchRegistry } = registry
  return useMemo(() => {
    const metricOf = (value: string) =>
      metrics.get(value) ?? metrics.get(splitRecordPath(value)?.id ?? "")
    const titleOf = (path: string | undefined) =>
      path ? titles.get(path) || undefined : undefined
    const chosenList = chosenKey ? chosenKey.split("\n") : []
    const batchList = batchKey
      ? batchKey.split("\t").map((b) => b.split("\n"))
      : []
    const batchOf = (metric: string) =>
      reads.results[
        batchList.findIndex((b) => b.some((m) => sameRef(m, metric)))
      ]
    const knows = (metric: string) => {
      if (!enabled || !metricsSettled) return false
      if (!chosenList.length) return !discovery.isPending
      const read = batchOf(metric)
      return read ? !read.isPending : false
    }
    const failedMetrics: StreamReadFailure[] = discovery.isError
      ? []
      : chosenList.flatMap((metric) => {
          const read = batchOf(metric)
          return read?.error
            ? [{ metric, error: read.error, retry: read.refetch }]
            : []
        })
    return {
      pending: registry.isPending,
      registryError,
      retryRegistry: () => void refetchRegistry(),
      ready:
        enabled &&
        metricsSettled &&
        !discovery.isPending &&
        !reads.isPending &&
        (Boolean(chosenList.length) || !wholePending),
      knows,
      failedMetrics,
      failed: (metric: string) =>
        failedMetrics.some((f) => sameRef(f.metric, metric)),
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
    }
  }, [
    registry.isPending,
    registryError,
    refetchRegistry,
    enabled,
    metricsSettled,
    chosenKey,
    batchKey,
    reads,
    wholePending,
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
  /** Why a menu's own read failed, by facet, null where it answered or is
   * on its way. A failed menu offers only the values of the streams the
   * page shows, so a page says so beside it (`FacetListErrors`). */
  errors: Record<StreamFacet, Error | null>
  /** Asks a facet's own read again. */
  retry: (facet: StreamFacet) => void
  /** A source or a thing in words: its title, else its id. */
  nameOf: (value: string, pin?: string) => string
}

/** The values each facet menu offers, one read per facet (`facetRead`):
 * under the page's metrics and the other facet's choice, never its own, so
 * a reopened menu still offers what a reader can add beside the chosen
 * values. The streams' own values join them, so a menu loses none while its
 * read is on its way or failed, and a failed read's error is kept per facet
 * so the page can say the menu is short. `narrow.metrics` are the metrics
 * the page shows. */
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

  const key = [metrics, sources, abouts].map((l) => l.join("\n")).join("\t")
  const values = useMemo(() => {
    const [m, so, ab] = key.split("\t").map((l) => (l ? l.split("\n") : []))
    const among = (list: string[], value: string | undefined) =>
      !list.length || list.some((v) => sameRef(value, v))
    // A menu's own values come from the streams under the page's other
    // choices, as its read does.
    const own = (facet: StreamFacet) =>
      obs.streams
        .filter(
          (s) =>
            among(m, s.metric) &&
            (facet === "source" || among(so, s.source)) &&
            (facet === "about" || among(ab, s.about))
        )
        .map((s) => s[facet])
    const of = (
      rows: AggregateRow[] | undefined,
      facet: StreamFacet,
      streams: (string | undefined)[]
    ) => [
      ...new Set(
        [...(rows ?? NO_ROWS).map((r) => r.group?.[facet]), ...streams].filter(
          (v): v is string => Boolean(v)
        )
      ),
    ]
    return {
      sources: of(bySource.data?.rows, "source", own("source")),
      abouts: of(byAbout.data?.rows, "about", own("about")),
    }
  }, [bySource.data, byAbout.data, obs.streams, key])

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

  const { error: sourceError, refetch: refetchSources } = bySource
  const { error: aboutError, refetch: refetchAbouts } = byAbout
  return useMemo(
    () => ({
      ...values,
      errors: { source: sourceError, about: aboutError },
      retry: (facet: StreamFacet) =>
        void (facet === "source" ? refetchSources() : refetchAbouts()),
      nameOf: (value: string, pin?: string) => {
        const path = refPathOf(value, pin)
        return (
          (path ? titles.get(path) || obs.titleOf(path) : undefined) ??
          refIdOf(value)
        )
      },
    }),
    [
      values,
      titles,
      obs,
      sourceError,
      aboutError,
      refetchSources,
      refetchAbouts,
    ]
  )
}

/** The streams of some metrics, out of a cached read of every stream under
 * no other choice: the same rows their own read answers. */
function streamsRead(
  client: QueryClient,
  metrics: readonly string[],
  kinds: readonly string[]
): AggregateResult | undefined {
  const every = client.getQueryData<AggregateResult>(
    aggregateQueryOptions(latestRead(), kinds).queryKey
  )
  if (!every) return undefined
  return {
    ...every,
    rows: every.rows.filter((r) =>
      metrics.some((m) => sameRef(r.group?.metric, m))
    ),
  }
}

/** The stream reads as one answer: their rows in order, each read's own
 * state (a metric's chart waits for its read alone, and a failed read is
 * said and retried on its own), pending while any is, and the first error.
 * Module-level, so the combined result keeps its identity while no read
 * changes. */
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
    results: results.map((r) => ({
      isPending: r.isPending,
      error: r.error,
      refetch: () => void r.refetch(),
    })),
    isPending: results.some((r) => r.isPending),
    isFetching: results.some((r) => r.isFetching),
    error: results.find((r) => r.error)?.error ?? null,
    refetch: () => {
      for (const r of results) void r.refetch()
    },
  }
}
