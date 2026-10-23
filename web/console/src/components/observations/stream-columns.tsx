/* eslint-disable react-refresh/only-export-components -- a columns module is
 * a factory of cell renderers, not a page module; nothing here hot-reloads on
 * its own. */

/** The streams table's columns (`pages/observations.tsx`): a selection box
 * on each row and in the header, then the metric (the row's link to the
 * timeline), source, about, channel, reading, latest value, when, points
 * and covered time. A row folding several sources (`foldSources`) opens
 * from a toggle beside its metric, names its sources, and says whose its
 * newest value is; its members draw indented under it. */

import type { MouseEvent } from "react"
import { Link } from "@tanstack/react-router"
import { ChevronRightIcon } from "lucide-react"

import { timeText } from "@/components/data-table/columns"
import { GridColumnHeader } from "@/components/data-table/data-grid-header"
import type { DataTableColumn } from "@/components/data-table/data-table"
import { EmptyValue } from "@/components/identity/empty-value"
import {
  AboutMark,
  CodeMark,
  GroupRecord,
  Reading,
  ShapeWord,
} from "@/components/observations/marks"
import type { Observations } from "@/hooks/use-observations"
import {
  DEFAULT_PRECISION,
  keptStatisticsWords,
  statisticLabel,
  windowed,
} from "@/lib/observations"
import { coveredWords, spanWords } from "@/lib/observations-time"
import {
  rowTimelineSearch,
  sourceWords,
  sourcesWords,
  type StreamRow,
} from "@/lib/stream-table"
import { cn } from "@/lib/utils"

/** A row's or the header's selection box. The header's is mixed while
 * some rows are selected and others not. */
function SelectBox({
  checked,
  mixed = false,
  label,
  onClick,
}: {
  checked: boolean
  mixed?: boolean
  label: string
  onClick: (event: MouseEvent<HTMLInputElement>) => void
}) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      className="size-3.5 cursor-pointer align-middle accent-primary"
      checked={checked}
      ref={(el) => {
        if (el) el.indeterminate = mixed
      }}
      onChange={() => {}}
      onClick={onClick}
    />
  )
}

export function selectColumn({
  state,
  selected,
  mixed,
  onAll,
  onPick,
}: {
  state: "all" | "some" | "none"
  selected: ReadonlySet<string>
  /** Folded rows some of whose members are selected and some not. */
  mixed: ReadonlySet<string>
  onAll: () => void
  onPick: (row: StreamRow, checked: boolean, shift: boolean) => void
}): DataTableColumn<StreamRow> {
  return {
    id: "select",
    enableSorting: false,
    enableHiding: false,
    header: () => (
      <SelectBox
        checked={state === "all"}
        mixed={state === "some"}
        label={
          state === "all"
            ? "Clear every stream shown"
            : "Select every stream shown"
        }
        onClick={onAll}
      />
    ),
    cell: ({ row }) => (
      <SelectBox
        checked={selected.has(row.original.key)}
        mixed={mixed.has(row.original.key)}
        label={`Select ${row.original.metricName}${row.original.members ? ` from ${sourcesWords(row.original.members)}` : row.original.sourceTitle ? ` from ${row.original.sourceTitle}` : ""}${row.original.channel ? ` (${row.original.channel})` : ""}`}
        onClick={(e) =>
          onPick(row.original, e.currentTarget.checked, e.shiftKey)
        }
      />
    ),
    meta: { label: "Select", width: 36, fixed: true },
  }
}

/** The folded rows open, and how one opens or closes. */
export interface Folds {
  open: ReadonlySet<string>
  onToggle: (key: string) => void
}

export function streamColumns(
  obs: Observations,
  technical: boolean,
  folds?: Folds
): DataTableColumn<StreamRow>[] {
  return [
    {
      id: "metric",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Metric" />
      ),
      accessorFn: (r) => r.metricName,
      cell: ({ row }) => {
        const r = row.original
        const link = (
          <Link
            to="/observations/timeline"
            search={rowTimelineSearch(r) as never}
            // Two lines before it cuts: a metric's name is what the row is.
            className="line-clamp-2 leading-tight font-medium break-words whitespace-normal underline-offset-[3px] hover:underline hover:decoration-border-strong"
            title={technical ? r.metric : r.metricName}
          >
            {r.metricName}
          </Link>
        )
        if (r.parent) {
          return (
            <span className="block pl-6 text-muted-foreground">{link}</span>
          )
        }
        if (!r.members || !folds) return link
        const open = folds.open.has(r.key)
        return (
          <span className="flex items-center gap-1">
            <button
              type="button"
              aria-expanded={open}
              aria-label={`${open ? "Hide" : "Show"} ${r.metricName} from each source`}
              className="hit-area grid size-5 shrink-0 cursor-pointer place-items-center rounded-[4px] text-faint outline-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
              onClick={() => folds.onToggle(r.key)}
            >
              <ChevronRightIcon
                aria-hidden
                className={cn(
                  "size-3.5 transition-transform motion-reduce:transition-none",
                  open && "rotate-90"
                )}
              />
            </button>
            {link}
          </span>
        )
      },
      meta: { label: "Metric", width: 200 },
    },
    {
      id: "source",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Source" />
      ),
      accessorFn: (r) => r.source,
      cell: ({ row }) =>
        row.original.members ? (
          <span
            className="block truncate"
            title={sourcesWords(row.original.members)}
          >
            {sourcesWords(row.original.members)}
          </span>
        ) : row.original.source ? (
          <GroupRecord
            value={row.original.source}
            pin={obs.pins.source}
            title={row.original.sourceTitle}
          />
        ) : (
          <EmptyValue />
        ),
      meta: { label: "Source", width: 240 },
    },
    {
      id: "about",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="About" />
      ),
      accessorFn: (r) => r.about,
      cell: ({ row }) => (
        <AboutMark
          value={row.original.about}
          pin={obs.pins.about}
          title={row.original.aboutTitle}
        />
      ),
      meta: { label: "About", width: 130 },
    },
    {
      id: "channel",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Channel" />
      ),
      accessorFn: (r) => r.channel,
      cell: ({ row }) => row.original.channel ?? <EmptyValue />,
      meta: { label: "Channel", width: 100 },
    },
    {
      id: "shape",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Reading" />
      ),
      accessorFn: (r) => r.shape,
      cell: ({ row }) => {
        // A stream whose vendor summaries keep several statistics (WHOOP's
        // average and highest heart rate per cycle) names them under its
        // form, which is all one value cannot say.
        const kept = keptStatisticsWords(row.original.statistics)
        return kept ? (
          <span className="flex flex-col leading-tight">
            <ShapeWord shape={row.original.shape} />
            <span
              className="truncate text-[12px] text-faint"
              title={`Its vendor summaries keep: ${kept}`}
            >
              Summaries: {kept}
            </span>
          </span>
        ) : (
          <ShapeWord shape={row.original.shape} />
        )
      },
      meta: { label: "Reading", width: 140 },
    },
    {
      id: "latest",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Latest" />
      ),
      accessorFn: (r) => r.value,
      cell: ({ row }) => {
        const reading = row.original.code ? (
          <CodeMark code={row.original.code} />
        ) : (
          <Reading
            value={row.original.value}
            unit={row.original.unit}
            comparator={row.original.comparator}
            // Shown to the metric's places, the stored digits on hover;
            // with Technical details on, the stored digits as they are.
            precision={
              technical
                ? undefined
                : (obs.metricOf(row.original.metric)?.displayPrecision ??
                  DEFAULT_PRECISION)
            }
          />
        )
        // A summary's value is one statistic of a period, and says which.
        const label = row.original.code
          ? undefined
          : statisticLabel(row.original.statistic)
        const value = label ? (
          <span className="inline-flex items-baseline gap-1.5">
            <span className="text-faint">{label}</span>
            {reading}
          </span>
        ) : (
          reading
        )
        // A folded row's newest value is one source's, and says whose.
        const newest = row.original.members?.[0]
        return newest ? (
          <span className="flex flex-col leading-tight">
            {value}
            <span className="truncate text-[12px] text-faint">
              from {sourceWords(newest)}
            </span>
          </span>
        ) : (
          value
        )
      },
      meta: { label: "Latest", width: 124 },
    },
    {
      id: "when",
      header: ({ column }) => <GridColumnHeader column={column} label="When" />,
      accessorFn: (r) => r.at,
      cell: ({ row }) => {
        const { at, end } = row.original
        // The wire's instants on hover, as every time cell has them. An
        // amount's or a summary's newest point is a window, which says so
        // rather than read as the day's.
        // A window's end is what the cell is for: two lines before it cuts.
        return windowed(row.original) && end ? (
          <span
            className="line-clamp-2 data leading-tight whitespace-normal text-muted-foreground"
            title={`${at} to ${end}`}
          >
            {spanWords(Date.parse(at), Date.parse(end))}
          </span>
        ) : (
          <span
            className="block truncate data text-muted-foreground"
            title={at}
          >
            {timeText(at, "datetime")}
          </span>
        )
      },
      meta: { label: "When", width: 160 },
    },
    {
      id: "count",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Points" align="right" />
      ),
      accessorFn: (r) => r.count,
      cell: ({ row }) =>
        row.original.count === undefined ? (
          <EmptyValue />
        ) : (
          <span className="tabular-nums">
            {row.original.count.toLocaleString("en")}
          </span>
        ),
      meta: {
        label: "Points",
        width: 90,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
    {
      id: "coverage",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Covered" />
      ),
      accessorFn: (r) => r.coverage,
      cell: ({ row }) =>
        row.original.coverage ? (
          <span title={row.original.coverage}>
            {coveredWords(row.original.coverage)}
          </span>
        ) : (
          <EmptyValue />
        ),
      meta: { label: "Covered", width: 110 },
    },
  ]
}
