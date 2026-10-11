/** What every observations surface reads first: the kinds binding the
 * `observed` trait, the kinds their references are pinned to, the metric
 * records (names, default forms, units, codes), the latest read (one row per
 * stream) and the titles of the sources and the things observed. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { useReferenceTitles } from "@/hooks/use-reference-titles"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { aggregateQueryOptions } from "@/lib/api/observations"
import { recordsQueryOptions } from "@/lib/api/records"
import type { KindInfo } from "@/lib/api/types"
import {
  formOf,
  latestRead,
  metricInfoOf,
  metricWords,
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
} from "@/lib/observations"
import { splitRecordPath } from "@/lib/record-path"

const NO_KINDS: KindInfo[] = []

/** How many metric records one read lists: a catalogue is a few hundred. */
const METRIC_PAGE = 500

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
  streams: Stream[]
  latest: ReturnType<typeof useLatest>
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

function useLatest(observedIds: string[]) {
  return useQuery({
    ...aggregateQueryOptions(latestRead(), observedIds),
    enabled: observedIds.length > 0,
  })
}

export function useObservations(): Observations {
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? NO_KINDS
  const observed = useMemo(() => observedKinds(kinds), [kinds])
  const observedIds = useMemo(() => observed.map((k) => k.identity), [observed])
  const pins = useMemo(() => observedPins(observed), [observed])
  const latest = useLatest(observedIds)
  const metricRecords = useQuery({
    ...recordsQueryOptions({
      kinds: pins.metric ? [pins.metric] : [],
      first: METRIC_PAGE,
    }),
    enabled: Boolean(pins.metric),
  })
  const streams = useMemo(() => streamsOf(latest.data), [latest.data])
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

  const paths = useMemo(() => {
    const out = new Set<string>()
    for (const s of streams) {
      const source = refPathOf(s.source, pins.source)
      const about = refPathOf(s.about, pins.about)
      if (source) out.add(source)
      if (about) out.add(about)
    }
    return [...out].sort()
  }, [streams, pins.source, pins.about])
  const titles = useReferenceTitles(paths, kinds)

  return useMemo(() => {
    const metricOf = (value: string) =>
      metrics.get(value) ?? metrics.get(splitRecordPath(value)?.id ?? "")
    const titleOf = (path: string | undefined) =>
      path ? titles.get(path) || undefined : undefined
    return {
      pending: registry.isPending,
      ready: metricsSettled && !latest.isPending,
      kinds,
      observed,
      observedIds,
      pins,
      streams,
      latest,
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
    streams,
    latest,
    metrics,
    titles,
  ])
}
