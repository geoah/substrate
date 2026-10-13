/** The panel the streams page's Timeline toggle opens: the selected
 * streams over a range in one chart of up to two units, with the range,
 * the bucket and the grouping, and each metric's reducer and unit, in the
 * address (`useTimelineControls`). */

import { useMemo } from "react"
import { XIcon } from "lucide-react"
import { parseAsStringLiteral, useQueryState } from "nuqs"

import { OverlayChart } from "@/components/observations/overlay-chart"
import { Labelled } from "@/components/observations/picker"
import { RangePicker } from "@/components/observations/range-picker"
import { Button } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import { Skeleton } from "@/components/ui/skeleton"
import { useObservations } from "@/hooks/use-observations"
import { useTimelineControls } from "@/hooks/use-timeline-controls"
import { GROUP_CHOICES, type GroupChoice } from "@/lib/observations-reads"
import {
  BUCKET_CHOICES,
  BUCKET_SIZE_WORDS,
  type BucketChoice,
  type RangePreset,
} from "@/lib/observations-time"
import { planPlot } from "@/lib/overlay"
import {
  selectedStreams,
  selectionByMetric,
  shortRef,
  tokenMetric,
  tokenNames,
} from "@/lib/stream-table"
import { GROUP_VALUES } from "@/lib/timeline-address"
import { cn } from "@/lib/utils"

/** The panel opens on a month: long enough to compare, daily buckets. */
const PANEL_RANGE: RangePreset = "30d"

export function TimelinePanel({
  selection,
  onClear,
  className,
}: {
  selection: string[]
  onClear: () => void
  className?: string
}) {
  const controls = useTimelineControls(PANEL_RANGE)
  const { range, size, bucket } = controls
  // `?group=` is the grid's grouping on this page; the chart's is `?by=`.
  const [by, setBy] = useQueryState(
    "by",
    parseAsStringLiteral(GROUP_VALUES).withDefault("metric")
  )

  // The selection's own streams, read apart from the table's filters, so
  // narrowing the table never drops a selected stream from the chart.
  const metrics = useMemo(
    () => [...new Set(selection.map(tokenMetric).filter(Boolean))],
    [selection]
  )
  const own = useObservations({ metrics })
  const picked = selectedStreams(selection, own.streams)
  const plots = selectionByMetric(picked).map((entry) =>
    // By id where the pin allows, so a metric's reducer and unit read
    // short in the address.
    planPlot(own, shortRef(entry.metric, own.pins.metric), {
      range,
      size,
      timezone: controls.timezone,
      group: by,
      sources: entry.sources,
      abouts: entry.abouts,
      channels: entry.channels,
      streams: entry.streams,
      reducer: controls.reducers.get(shortRef(entry.metric, own.pins.metric)),
      unit: controls.units.get(shortRef(entry.metric, own.pins.metric)),
    })
  )
  // A token whose metric's streams are read and name no stream of it.
  const missing = selection.filter(
    (t) =>
      own.knows(tokenMetric(t)) && !own.streams.some((s) => tokenNames(t, s))
  ).length
  // A metric just selected waits for its streams; the others stay drawn.
  const waiting = metrics.some((m) => !own.knows(m))

  return (
    <section
      aria-label="Timeline of the selected streams"
      data-slot="stream-timeline"
      className={cn("shrink-0", className)}
    >
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        <h2 className="text-[13px] font-semibold">
          {picked.length === 0
            ? "Timeline"
            : picked.length === 1
              ? "1 stream over time"
              : `${picked.length} streams over time`}
        </h2>
        <RangePicker
          preset={controls.preset}
          range={range}
          onPreset={controls.onPreset}
          onCustom={controls.onCustom}
        />
        <Labelled label="Bucket">
          <Segmented
            label="Bucket"
            value={bucket}
            options={BUCKET_CHOICES.map((b) => ({
              value: b.value,
              label:
                b.value === "auto" && bucket === "auto"
                  ? `Automatic: ${BUCKET_SIZE_WORDS[size]}`
                  : b.label,
            }))}
            onChange={(next: BucketChoice) => controls.onBucket(next)}
          />
        </Labelled>
        <Labelled label="Group by">
          <Segmented
            label="Group by"
            value={by}
            options={GROUP_CHOICES}
            onChange={(next: GroupChoice) =>
              void setBy(next === "metric" ? null : next)
            }
          />
        </Labelled>
        <span className="flex-1" />
        {selection.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 text-[12.5px] font-normal text-muted-foreground"
            onClick={onClear}
          >
            <XIcon className="size-3.5" />
            Clear the selection
          </Button>
        )}
      </div>
      {missing > 0 && (
        <p className="mt-2 text-[12.5px] text-faint">
          {missing === 1
            ? "1 selected stream has no points any more, so the chart leaves it out."
            : `${missing} selected streams have no points any more, so the chart leaves them out.`}
        </p>
      )}
      {picked.length === 0 && waiting ? (
        <Skeleton className="mt-3 h-[250px] w-full" />
      ) : picked.length === 0 ? (
        <p className="mt-3 text-muted-foreground">
          Select streams in the table to draw them here, in one chart. Streams
          of one unit share an axis; a second unit reads on the right.
        </p>
      ) : (
        <OverlayChart
          className="mt-3"
          obs={own}
          plots={plots}
          range={range}
          size={size}
          onReducer={controls.onReducer}
          onUnit={controls.onUnit}
          onZoom={controls.zoom}
        />
      )}
    </section>
  )
}
