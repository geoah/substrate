/* eslint-disable react-refresh/only-export-components */
/** The list layout, whole: the proof that a view record reaches pixels and
 * that a tap writes back. Rows are the server `title` and then the `show`
 * properties drawn by datatype (a state as its badge, a reference as its
 * referent's title from one `filter.ids` read per kind per page, a datetime
 * relative, an enum by label and hidden at its default, a url as a link);
 * the kind's sole state is the badge, drawn only when the filter admits more
 * than one state; `groupBy` sections the rows (the temporal point as Overdue
 * to Undated, a state or enum in declared order, a reference by title); the
 * next page loads as the sentinel scrolls into view; a tap reports the row
 * and the screen decides between `opens` and the sheet; the facet chips sit
 * above the first group wherever the mount threads a selection
 * (`ctx.facets`), and stay while the rows load or come back empty, so a
 * narrowing can always be undone. Rows are at least 44 px, carry no
 * text-selection callout, and no swipe means anything. */

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { InboxIcon, TriangleAlertIcon } from "lucide-react"

import { FacetBar } from "@/components/apps/facet-bar"
import { GroupHeader } from "@/components/apps/group-header"
import { IncompleteNote } from "@/components/apps/incomplete-note"
import { RowActions } from "@/components/apps/row-actions"
import { StateBadge } from "@/components/state-badge"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { readReference, type SubstrateRecord } from "@/lib/api/types"
import { allSpecs } from "@/lib/apps/cond"
import { groupRecords } from "@/lib/apps/group"
import { admittedStates, stateSpecOf } from "@/lib/apps/machine"
import { temporalPoint, useViewRecords } from "@/lib/apps/queries"
import { titleOf, useReferentTitles } from "@/lib/apps/referents"
import type { ActionHost, LayoutProps } from "@/lib/apps/spec"
import { bucketOf, relativeDay } from "@/lib/apps/time"
import { defaultShow } from "@/lib/apps/view-spec"
import { formatValue, type PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

/** How many `show` cells a row draws when the view names none. */
const DEFAULT_CELLS = 3

/** One `show` value as the row draws it, or nothing when it is absent or is
 * an enum at its declared default. `point` is the kind's temporal point: the
 * one datetime whose past reads as overdue, since a sync time has no
 * "Overdue". */
export function cellNode(
  spec: PropSpec,
  value: unknown,
  titles: Map<string, string>,
  point?: string
): ReactNode {
  if (value === undefined || value === null || value === "") return null
  if (Array.isArray(value)) {
    if (!value.length) return null
    const first = cellNode(
      { ...spec, repeated: false },
      value[0],
      titles,
      point
    )
    if (first === null) return null
    return (
      <>
        {first}
        {value.length > 1 && (
          <span className="text-muted-foreground/70"> +{value.length - 1}</span>
        )}
      </>
    )
  }
  switch (spec.kind) {
    case "state":
      return <StateBadge value={String(value)} initial={spec.initial} />
    case "reference": {
      const held = readReference(value)
      if (!held) return null
      return (
        <span className="truncate">
          {titles.get(held.path) ?? held.path.split("/").pop()}
        </span>
      )
    }
    case "datetime":
    case "date": {
      const iso = String(value)
      const overdue = spec.name === point && bucketOf(iso) === "overdue"
      return (
        <time
          dateTime={iso}
          title={iso}
          className={cn(overdue && "text-destructive")}
        >
          {relativeDay(iso)}
        </time>
      )
    }
    case "enum": {
      const raw = String(value)
      if (spec.default !== undefined && raw === String(spec.default)) {
        return null
      }
      return (
        <span>{spec.values?.find((v) => v.value === raw)?.label || raw}</span>
      )
    }
    case "url": {
      const href = String(value)
      let label = href
      try {
        label = new URL(href).hostname
      } catch {
        // not a URL: the raw text stands
      }
      return (
        <a
          href={href}
          target="_blank"
          rel="noreferrer"
          className="underline-offset-4 hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          {label}
        </a>
      )
    }
    case "bool":
      return value === true ? <span>{spec.label}</span> : null
    case "int":
      // The one name-keyed rendering: an int called `number` is an ordinal
      // in every tracker's mirror, and reads as one.
      return <span>{spec.name === "number" ? `#${value}` : String(value)}</span>
    default:
      if (spec.values?.length) {
        const raw = String(value)
        if (spec.default !== undefined && raw === String(spec.default)) {
          return null
        }
        return (
          <span>{spec.values.find((v) => v.value === raw)?.label || raw}</span>
        )
      }
      return <span className="truncate">{formatValue(spec, value)}</span>
  }
}

function Row({
  record,
  host,
  cells,
  badge,
  point,
  titles,
  onOpen,
}: {
  record: SubstrateRecord
  host: ActionHost
  cells: PropSpec[]
  badge?: PropSpec
  point?: string
  titles: Map<string, string>
  onOpen: (record: SubstrateRecord) => void
}) {
  const nodes = cells
    .map((spec) => ({
      name: spec.name,
      node: cellNode(spec, record.properties[spec.name], titles, point),
    }))
    .filter((c) => c.node !== null)
  const state = badge ? record.properties[badge.name] : undefined
  const meta = nodes.length > 0 || (badge && typeof state === "string")
  return (
    <li className="flex items-stretch border-b last:border-b-0">
      <div
        role="button"
        tabIndex={0}
        onClick={() => onOpen(record)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault()
            onOpen(record)
          }
        }}
        className="flex min-h-14 min-w-0 flex-1 cursor-pointer flex-col justify-center gap-0.5 px-4 py-2 text-left outline-none select-none [-webkit-touch-callout:none] hover:bg-muted/40 focus-visible:bg-muted/40 active:bg-muted/60"
      >
        <span className="truncate text-[0.95rem] leading-snug font-medium">
          {titleOf(record)}
        </span>
        {meta && (
          <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs text-muted-foreground">
            {badge && typeof state === "string" && (
              <StateBadge value={state} initial={badge.initial} />
            )}
            {nodes.map((c, i) => (
              <span key={c.name} className="flex min-w-0 items-center gap-1.5">
                {(i > 0 || (badge && typeof state === "string")) && (
                  <span aria-hidden>·</span>
                )}
                {c.node}
              </span>
            ))}
          </div>
        )}
      </div>
      <RowActions host={host} record={record} className="pr-2" />
    </li>
  )
}

export function ListView({
  spec,
  kind,
  kinds,
  ctx,
  onOpenRecord,
}: LayoutProps) {
  const data = useViewRecords(spec, ctx, kinds)
  const host = useMemo<ActionHost>(
    () => ({ spec, kind, kinds, ctx }),
    [spec, kind, kinds, ctx]
  )
  const specs = useMemo(() => (kind ? allSpecs(kind) : []), [kind])
  const cells = useMemo(() => {
    const names = spec.show.length
      ? spec.show
      : kind
        ? defaultShow(kind).slice(0, DEFAULT_CELLS)
        : []
    return names
      .map((name) => specs.find((s) => s.name === name))
      .filter((s): s is PropSpec => Boolean(s))
  }, [spec.show, kind, specs])
  const groupSpec = spec.groupBy
    ? specs.find((s) => s.name === spec.groupBy)
    : undefined
  const refNames = useMemo(
    () =>
      [...cells, groupSpec]
        .filter((s): s is PropSpec => s?.kind === "reference")
        .map((s) => s.name),
    [cells, groupSpec]
  )
  const titles = useReferentTitles(data.records, refNames, kinds)
  const point = kind ? temporalPoint(kind) : undefined
  const state = stateSpecOf(kind)
  const badge =
    state && admittedStates(spec.filter, state).length !== 1 ? state : undefined
  const groups = useMemo(
    () => groupRecords(data.records, groupSpec, titles),
    [data.records, groupSpec, titles]
  )
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())

  // The next page loads as the sentinel scrolls into view.
  const sentinel = useRef<HTMLDivElement>(null)
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = data
  useEffect(() => {
    const el = sentinel.current
    if (!el || !hasNextPage) return
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting) && !isFetchingNextPage) {
          fetchNextPage()
        }
      },
      { rootMargin: "200px" }
    )
    io.observe(el)
    return () => io.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const bar = ctx.facets && spec.facets.length > 0 && (
    <FacetBar spec={spec} kind={kind} kinds={kinds} records={data.records} />
  )

  let body: ReactNode
  if (data.problems.length) {
    body = (
      <Empty className="py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <InboxIcon />
          </EmptyMedia>
          <EmptyTitle>{spec.empty ?? "Nothing here yet"}</EmptyTitle>
          <EmptyDescription>{data.problems[0].message}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else if (data.error) {
    body = (
      <Empty className="py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>Could not read this view</EmptyTitle>
          <EmptyDescription className="break-words">
            {data.error.message}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button variant="outline" onClick={data.refetch}>
            Retry
          </Button>
        </EmptyContent>
      </Empty>
    )
  } else if (data.isPending) {
    body = (
      <ul className="flex flex-col">
        {Array.from({ length: 6 }, (_, i) => (
          <li key={i} className="flex flex-col gap-2 border-b px-4 py-3">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3 w-1/3" />
          </li>
        ))}
      </ul>
    )
  } else if (!data.records.length) {
    body = (
      <Empty className="py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <InboxIcon />
          </EmptyMedia>
          <EmptyTitle>{spec.empty ?? "Nothing here."}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  } else {
    body = (
      <>
        {groups.map((group) => {
          const folded = collapsed.has(group.key)
          return (
            <section key={group.key || "all"}>
              {groupSpec && (
                <GroupHeader
                  label={group.label}
                  count={group.records.length}
                  collapsed={folded}
                  onToggle={() =>
                    setCollapsed((prev) => {
                      const next = new Set(prev)
                      if (next.has(group.key)) next.delete(group.key)
                      else next.add(group.key)
                      return next
                    })
                  }
                />
              )}
              {!folded && (
                <ul className="flex flex-col">
                  {group.records.map((record) => (
                    <Row
                      key={`${record.kind}/${record.id}`}
                      record={record}
                      host={host}
                      cells={cells}
                      badge={badge}
                      point={point}
                      titles={titles}
                      onOpen={onOpenRecord}
                    />
                  ))}
                </ul>
              )}
            </section>
          )
        })}
        <div ref={sentinel} className="flex h-12 items-center justify-center">
          {isFetchingNextPage && <Spinner className="size-4" />}
        </div>
        <IncompleteNote
          shown={data.incomplete ? data.records.length : 0}
          first={spec.first}
          unread={data.unread}
        />
      </>
    )
  }

  return (
    <div className="flex flex-col">
      {bar}
      {body}
    </div>
  )
}
