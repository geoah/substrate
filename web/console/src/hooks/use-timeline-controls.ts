/** The controls every timeline shares, in the address: the range (a preset
 * or a custom window), the bucket, where a day starts, and each metric's
 * reducer, output unit and summary statistic. The timeline page and the streams page's
 * Timeline panel read them the same way and differ only in the preset they
 * open on; what each groups by, and the panel's selection, stay with the
 * page. */

import { useMemo, useState } from "react"
import { parseAsString, parseAsStringLiteral, useQueryState } from "nuqs"

import {
  bucketSizeOf,
  presetRange,
  readerTimezone,
  type BucketChoice,
  type BucketSize,
  type DayBoundary,
  type RangePreset,
  type TimeRange,
} from "@/lib/observations-time"
import {
  ARRAY,
  BUCKET_VALUES,
  DAY_VALUES,
  RANGE_VALUES,
  choiceOf,
  customRange,
  readChoices,
  writeChoice,
} from "@/lib/timeline-address"

export interface TimelineControls {
  /** The window read, and how the range picker names it. */
  range: TimeRange
  preset: RangePreset | "custom"
  /** The bucket picked, and the size it reads at. */
  bucket: BucketChoice
  size: BucketSize
  /** The zone calendar buckets are cut in. */
  timezone: string
  /** Where a day, week or month bucket starts: where each value was
   * recorded (the default), or the reader's zone. */
  days: DayBoundary
  /** A metric's picked reducer, output unit and summary statistic,
   * whichever way the address spells the metric. */
  reducerOf: (metric: string) => string | undefined
  unitOf: (metric: string) => string | undefined
  statisticOf: (metric: string) => string | undefined
  onPreset: (preset: RangePreset) => void
  onCustom: (range: TimeRange) => void
  onBucket: (bucket: BucketChoice) => void
  onDays: (days: DayBoundary) => void
  onReducer: (metric: string, value: string) => void
  onUnit: (metric: string, value: string) => void
  onStatistic: (metric: string, value: string) => void
  /** Reads a window: a custom range, the bucket back to automatic. */
  zoom: (range: TimeRange) => void
}

export function useTimelineControls(
  defaultRange: RangePreset
): TimelineControls {
  const [rangeParam, setRangeParam] = useQueryState(
    "range",
    parseAsStringLiteral(RANGE_VALUES).withDefault(defaultRange)
  )
  const [startParam, setStart] = useQueryState("start", parseAsString)
  const [endParam, setEnd] = useQueryState("end", parseAsString)
  const [bucket, setBucket] = useQueryState(
    "bucket",
    parseAsStringLiteral(BUCKET_VALUES).withDefault("auto")
  )
  const [days, setDays] = useQueryState(
    "days",
    parseAsStringLiteral(DAY_VALUES).withDefault("local")
  )
  const [reducerTokens, setReducerTokens] = useQueryState("reducer", ARRAY)
  const [unitTokens, setUnitTokens] = useQueryState("unit", ARRAY)
  const [statisticTokens, setStatisticTokens] = useQueryState(
    "statistic",
    ARRAY
  )
  // Presets count back from when the page opened, so a re-render never
  // moves the window (and never re-asks).
  const [now] = useState(() => Date.now())
  const timezone = useMemo(() => readerTimezone(), [])

  const reducers = readChoices(reducerTokens)
  const units = readChoices(unitTokens)
  const statistics = readChoices(statisticTokens)
  const custom =
    rangeParam === "custom" ? customRange(startParam, endParam) : undefined
  const preset: RangePreset | "custom" = custom
    ? "custom"
    : rangeParam === "custom"
      ? defaultRange
      : rangeParam
  const range = custom ?? presetRange(preset as RangePreset, now)

  function onCustom(next: TimeRange) {
    void setRangeParam("custom")
    void setStart(new Date(next.start).toISOString())
    void setEnd(new Date(next.end).toISOString())
  }

  return {
    range,
    preset,
    bucket,
    size: bucketSizeOf(bucket, range),
    timezone,
    days,
    reducerOf: (metric) => choiceOf(reducers, metric),
    unitOf: (metric) => choiceOf(units, metric),
    statisticOf: (metric) => choiceOf(statistics, metric),
    onPreset: (p) => {
      void setRangeParam(p === defaultRange ? null : p)
      void setStart(null)
      void setEnd(null)
    },
    onCustom,
    onBucket: (next) => void setBucket(next === "auto" ? null : next),
    onDays: (next) => void setDays(next === "local" ? null : next),
    onReducer: (metric, value) => {
      const next = writeChoice(reducerTokens, metric, value)
      void setReducerTokens(next.length ? next : null)
    },
    onUnit: (metric, value) => {
      const next = writeChoice(unitTokens, metric, value)
      void setUnitTokens(next.length ? next : null)
    },
    onStatistic: (metric, value) => {
      const next = writeChoice(statisticTokens, metric, value)
      void setStatisticTokens(next.length ? next : null)
    },
    zoom: (next) => {
      onCustom(next)
      void setBucket(null)
    },
  }
}
