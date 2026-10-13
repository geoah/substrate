/* eslint-disable react-refresh/only-export-components -- a columns module is
 * a factory of cell renderers, not a page module; nothing here hot-reloads on
 * its own. */

/** The streams table's columns (`pages/observations.tsx`): a selection box
 * on each row and in the header, then the metric (the row's link to the
 * timeline), source, about, channel, reading, latest value, when, points
 * and covered time. */

import type { MouseEvent } from "react"
import { Link } from "@tanstack/react-router"

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
import { DEFAULT_PRECISION, rowSearch, windowed } from "@/lib/observations"
import { coverageWords, spanWords } from "@/lib/observations-time"
import type { StreamRow } from "@/lib/stream-table"

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
  onAll,
  onPick,
}: {
  state: "all" | "some" | "none"
  selected: ReadonlySet<string>
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
        label={`Select ${row.original.metricName}${row.original.sourceTitle ? ` from ${row.original.sourceTitle}` : ""}${row.original.channel ? ` (${row.original.channel})` : ""}`}
        onClick={(e) =>
          onPick(row.original, e.currentTarget.checked, e.shiftKey)
        }
      />
    ),
    meta: { label: "Select", width: 36, fixed: true },
  }
}

export function streamColumns(
  obs: Observations,
  technical: boolean
): DataTableColumn<StreamRow>[] {
  return [
    {
      id: "metric",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Metric" />
      ),
      accessorFn: (r) => r.metricName,
      cell: ({ row }) => (
        <Link
          to="/observations/timeline"
          search={rowSearch(row.original) as never}
          className="block truncate font-medium underline-offset-[3px] hover:underline hover:decoration-border-strong"
          title={technical ? row.original.metric : undefined}
        >
          {row.original.metricName}
        </Link>
      ),
      meta: { label: "Metric", width: 200 },
    },
    {
      id: "source",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Source" />
      ),
      accessorFn: (r) => r.source,
      cell: ({ row }) =>
        row.original.source ? (
          <GroupRecord
            value={row.original.source}
            pin={obs.pins.source}
            title={row.original.sourceTitle}
          />
        ) : (
          <EmptyValue />
        ),
      meta: { label: "Source", width: 210 },
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
      cell: ({ row }) => <ShapeWord shape={row.original.shape} />,
      meta: { label: "Reading", width: 124 },
    },
    {
      id: "latest",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Latest" />
      ),
      accessorFn: (r) => r.value,
      cell: ({ row }) =>
        row.original.code ? (
          <CodeMark code={row.original.code} />
        ) : (
          <Reading
            value={row.original.value}
            unit={row.original.unit}
            // Shown to the metric's places, the stored digits on hover;
            // with Technical details on, the stored digits as they are.
            precision={
              technical
                ? undefined
                : (obs.metricOf(row.original.metric)?.displayPrecision ??
                  DEFAULT_PRECISION)
            }
          />
        ),
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
        return windowed(row.original) && end ? (
          <span
            className="block truncate data text-muted-foreground"
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
            {coverageWords(row.original.coverage)}
          </span>
        ) : (
          <EmptyValue />
        ),
      meta: { label: "Covered", width: 110 },
    },
  ]
}
