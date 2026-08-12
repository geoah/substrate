/* eslint-disable react-refresh/only-export-components -- a columns module is
 * a factory of cell renderers, not a page; nothing here hot-reloads alone. */

/** Shared column factories (owner ruling, 2026-08-06): the recurring cells —
 * a timestamp with the wire ISO on hover, an actor chip, a record link, an
 * op verb, the change-feed vocabulary — built ONCE, so every table differs
 * by configuration, not code. Feed surfaces (changelog, actor log, activity)
 * compose the ChangeRow factories; anything with a stamp or an actor uses
 * the generic ones. */

import type { ColumnDef } from "@tanstack/react-table"
import { Link } from "@tanstack/react-router"

import { ActorChip } from "@/components/actor-chip"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
import { relativeTime, shortDate, shortTime, tableDateTime } from "@/lib/format"
import { splitKind, kindByIdentity } from "@/lib/definition"
import { changeSummary, verbOf } from "@/lib/changelog"
import { cn } from "@/lib/utils"

// ── time ────────────────────────────────────────────────────────────────────

/** How a stamp reads in a cell; hover ALWAYS carries the wire ISO verbatim.
 * - `relative`: `3m ago` — list temporality.
 * - `datetime`: `Aug 6, 01:10` — local absolute, redline format.
 * - `clock`: `01:10:32`, dated when not today's — the changelog's voice, where
 *   seconds order rows inside a burst. */
export type TimeVoice = "relative" | "datetime" | "clock"

export function timeText(
  iso: string,
  voice: TimeVoice,
  now = Date.now()
): string {
  if (voice === "relative") return relativeTime(iso, now)
  if (voice === "datetime") return tableDateTime(iso, now)
  const day = shortDate(iso)
  const stamp = shortTime(iso, true)
  return day === shortDate(new Date(now).toISOString())
    ? stamp
    : `${day} ${stamp}`
}

export function timeColumn<T>(opts: {
  id: string
  title?: string
  iso: (row: T) => string | undefined
  voice?: TimeVoice
  width?: number
  align?: "left" | "right"
  sortable?: boolean
  description?: string
}): ColumnDef<T, unknown> {
  const voice = opts.voice ?? "relative"
  const title = opts.title ?? opts.id
  return {
    id: opts.id,
    accessorFn: (row) => opts.iso(row),
    enableSorting: opts.sortable ?? false,
    header: ({ column }) => (
      <DataTableColumnHeader
        column={column}
        title={title}
        align={opts.align}
        description={opts.description}
      />
    ),
    cell: ({ row }) => {
      const iso = opts.iso(row.original)
      if (!iso) return <span className="text-muted-foreground">—</span>
      return (
        // hover shows the wire ISO verbatim — one convention everywhere
        <span
          className={cn(
            "block truncate data text-muted-foreground",
            opts.align === "right" && "text-right"
          )}
          title={iso}
        >
          {timeText(iso, voice)}
        </span>
      )
    },
    meta: {
      label: title,
      width: opts.width ?? 130,
      ...(opts.align === "right"
        ? { headerClassName: "text-right", cellClassName: "text-right" }
        : {}),
    },
  }
}

// ── actor ───────────────────────────────────────────────────────────────────

export function actorColumn<T>(opts: {
  id?: string
  title?: string
  actor: (row: T) => string | undefined
  width?: number
}): ColumnDef<T, unknown> {
  const id = opts.id ?? "actor"
  const title = opts.title ?? "actor"
  return {
    id,
    accessorFn: (row) => opts.actor(row),
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={title} />
    ),
    cell: ({ row }) => {
      const actor = opts.actor(row.original)
      if (!actor) return <span className="text-muted-foreground">—</span>
      return (
        <span className="flex min-w-0 items-center">
          <ActorChip actor={actor} />
        </span>
      )
    },
    meta: {
      label: title,
      // chips are identity-length (`github.connectors.substrate.reamde.dev`): a modest
      // share with a cap, or the caller's fixed px (the rails pass one).
      ...(opts.width
        ? { width: opts.width }
        : { size: { min: 140, max: 240, weight: 0.75 } }),
    },
  }
}

// ── the change-feed vocabulary ──────────────────────────────────────────────

/** `issue/issue-123`, linking into the data vertical when the registry
 * knows the kind; an uninstalled kind renders inert, never a dead link.
 *
 * The local name alone does NOT identify a kind — every bundle names its
 * settings record `config` and its connection `account`, and github and linear
 * both ship an `issue` — so the cell shows the short form to stay inside its
 * column and puts the FULL kind reference in the title, which is the only thing
 * that tells two of these rows apart. */
export function ChangeRecordLink({
  row,
  kinds,
}: {
  row: ChangeRow
  kinds: KindInfo[]
}) {
  const { name, authority } = splitKind(row.kind)
  const kindInfo = kindByIdentity(kinds, row.kind)
  const title = `${row.kind}/${row.recordId}`
  const label = (
    <>
      <span className="text-muted-foreground">{name}/</span>
      {row.recordId}
    </>
  )
  if (!kindInfo) {
    return (
      <span className="block truncate data" title={title}>
        {label}
      </span>
    )
  }
  return (
    <Link
      to="/data/$authority/$plural/$id"
      params={{ authority: authority, plural: kindInfo.plural, id: row.recordId }}
      className="block truncate data underline-offset-4 hover:underline"
      title={title}
      onClick={(e) => e.stopPropagation()}
    >
      {label}
    </Link>
  )
}

export function changeTimeColumn(opts?: {
  voice?: TimeVoice
  width?: number
}): ColumnDef<ChangeRow, unknown> {
  return timeColumn<ChangeRow>({
    id: "time",
    iso: (row) => row.ts,
    voice: opts?.voice ?? "clock",
    width: opts?.width ?? 140,
  })
}

export function changeActorColumn(opts?: {
  width?: number
}): ColumnDef<ChangeRow, unknown> {
  return actorColumn<ChangeRow>({
    actor: (row) => row.actor,
    width: opts?.width,
  })
}

/** The op said as its plain verb — created/updated/linked/merged/… */
export function changeOpColumn(opts?: {
  width?: number
}): ColumnDef<ChangeRow, unknown> {
  return {
    id: "action",
    accessorFn: (row) => verbOf(row),
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="action" />
    ),
    cell: ({ row }) => (
      <span className="block truncate data text-muted-foreground">
        {verbOf(row.original)}
      </span>
    ),
    meta: { label: "action", width: opts?.width ?? 90 },
  }
}

export function changeRecordColumn(
  kinds: KindInfo[],
  opts?: { width?: number }
): ColumnDef<ChangeRow, unknown> {
  return {
    id: "record",
    accessorFn: (row) => row.recordId,
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="record" />
    ),
    cell: ({ row }) => <ChangeRecordLink row={row.original} kinds={kinds} />,
    meta: {
      label: "record",
      // a record link (`issue/issue-123`) is the row's identity —
      // roomy share, capped so ids never balloon past usefulness.
      ...(opts?.width
        ? { width: opts.width }
        : { size: { min: 200, max: 460, weight: 1.5 } }),
    },
  }
}

export function changeKindColumn(opts?: {
  width?: number
}): ColumnDef<ChangeRow, unknown> {
  return {
    id: "kind",
    accessorFn: (row) => splitKind(row.kind).name,
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="kind" />
    ),
    cell: ({ row }) => {
      const { name } = splitKind(row.original.kind)
      return (
        <span
          className="block truncate data text-muted-foreground"
          title={row.original.kind}
        >
          {name}
        </span>
      )
    },
    meta: {
      label: "kind",
      // kind names hug their content: a small share inside tight bounds.
      ...(opts?.width
        ? { width: opts.width }
        : { size: { min: 110, max: 180, weight: 0.5 } }),
    },
  }
}

export function changeAuthorityColumn(opts?: {
  width?: number
}): ColumnDef<ChangeRow, unknown> {
  return {
    id: "authority",
    accessorFn: (row) => splitKind(row.kind).authority,
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="authority" />
    ),
    cell: ({ row }) => {
      const { authority } = splitKind(row.original.kind)
      return (
        <span
          className="block truncate data text-muted-foreground"
          title={authority}
        >
          {authority}
        </span>
      )
    },
    meta: {
      label: "authority",
      ...(opts?.width
        ? { width: opts.width }
        : { size: { min: 130, max: 230, weight: 0.5 } }),
    },
  }
}

/** What the row did, in the summary voice (`changeSummary`) — the feed's
 * designated absorber (weight 2, no cap): leftover width lands here, and the
 * text truncates only at the column boundary with the full value on hover. */
export function changeSummaryColumn(): ColumnDef<ChangeRow, unknown> {
  return {
    id: "summary",
    accessorFn: (row) => changeSummary(row),
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="summary" />
    ),
    cell: ({ row }) => {
      const text = changeSummary(row.original)
      if (!text) return <span className="text-muted-foreground">—</span>
      return (
        <span
          className="block truncate data text-muted-foreground"
          title={text}
        >
          {text}
        </span>
      )
    },
    meta: { label: "summary", size: { min: 200, weight: 2 } },
  }
}
