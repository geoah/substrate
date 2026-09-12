/** The timeline layout: one windowed page per implementor of the view's
 * trait (or the one kind of a kind view), each read on its own bound point,
 * merged by instant and sectioned by local day with empty days left out and
 * today marked. A row is the kind's word, the time (a range where the kind
 * binds `endsAt`), the title and the state badge when the kind has one. An
 * implementor that could not be read is named in the footer instead of
 * failing the view, and a page the size cut says so per kind. Reads go
 * through `implementorsOptions` and `recordsQueryOptions`; a row tap is
 * reported through `onOpenRecord`.
 *
 * On a page the person may choose the days instead of the view's `window`:
 * the choice lives in the URL as `from` and `to` (`timeline-range.ts`),
 * never in the view record, and re-keys every implementor's read. A card or
 * an inline mount always shows the window, because the address is the
 * page's and not the card's. */

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
} from "react"
import { useQueries, useQuery } from "@tanstack/react-query"
import { CalendarIcon, TriangleAlertIcon } from "lucide-react"
import { createParser, useQueryStates } from "nuqs"

import { GroupHeader } from "@/components/apps/group-header"
import { StateBadge } from "@/components/state-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"
import { recordsQueryOptions, type ListParams } from "@/lib/api/records"
import type { KindInfo } from "@/lib/api/types"
import { stateSpecOf } from "@/lib/apps/machine"
import { implementorsOptions } from "@/lib/apps/queries"
import { titleOf } from "@/lib/apps/referents"
import type { LayoutProps } from "@/lib/apps/spec"
import { kindByIdentity } from "@/lib/definition"
import { shortDate, shortTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  inRange,
  parseDayKey,
  rangeBounds,
  rangeLabel,
  resolveRange,
  windowRange,
  type DayKey,
  type TimelineRange,
} from "./timeline-range"
import { TimelineRangeBar } from "./timeline-range-bar"
import {
  dayLabel,
  dayKey,
  groupByDay,
  isTimelineKind,
  mergeRows,
  timelineRead,
  type TimelineRow,
} from "./timeline-window"

/** A stable, disabled stand-in for a read whose token did not resolve. */
const NO_LIST: ListParams = { authority: "", package: "", name: "" }

const NO_KINDS: KindInfo[] = []

/** `from` / `to` as the URL carries them: a real local calendar day or
 * nothing, so a mangled link falls back to the window. */
const dayKeyParser = createParser<DayKey>({
  parse: (value) => parseDayKey(value) ?? null,
  serialize: (value) => value,
})

const RANGE_PARAMS = { from: dayKeyParser, to: dayKeyParser }

/** `09:00–09:30` for a range, `09:00` for a point; a range that ends on
 * another day names that day. */
function timeOf(row: TimelineRow): string {
  const start = shortTime(new Date(row.at).toISOString())
  if (row.endsAt === undefined) return start
  const end = new Date(row.endsAt).toISOString()
  const sameDay = dayKey(row.at) === dayKey(row.endsAt)
  return `${start}–${sameDay ? shortTime(end) : shortDate(end)}`
}

function TimelineItem({
  row,
  onOpen,
}: {
  row: TimelineRow
  onOpen: () => void
}) {
  const state = stateSpecOf(row.kind)
  const value = state ? row.record.properties[state.name] : undefined
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault()
      onOpen()
    }
  }
  return (
    <li className="border-b last:border-b-0">
      <div
        role="button"
        tabIndex={0}
        onClick={onOpen}
        onKeyDown={onKey}
        className="flex min-h-14 items-center gap-3 px-4 py-2 text-left outline-none select-none [-webkit-touch-callout:none] focus-visible:bg-muted/60 active:bg-muted/60"
      >
        <span className="w-[4.75rem] shrink-0 data text-xs text-muted-foreground tabular-nums">
          {timeOf(row)}
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="truncate text-sm">{titleOf(row.record)}</span>
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <Badge
              variant="secondary"
              className="h-4 px-1.5 text-[0.65rem] font-normal"
            >
              {row.kind.name}
            </Badge>
            {typeof value === "string" && value !== "" && (
              <StateBadge value={value} initial={state?.initial} />
            )}
          </div>
        </div>
      </div>
    </li>
  )
}

export default function TimelineLayout({
  spec,
  kind,
  kinds,
  ctx,
  onOpenRecord,
}: LayoutProps) {
  // Minted once per mount and rounded to the minute, so every render keys
  // the same reads and two mounts inside a minute share the cache.
  const [now] = useState(() => Math.floor(Date.now() / 60_000) * 60_000)
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(
    () => new Set()
  )
  const dayRefs = useRef(new Map<string, HTMLElement>())
  const topRef = useRef<HTMLDivElement>(null)
  // The bar stays under the chrome while the days scroll, so the day
  // headers stick below it and a jump to today lands under it, not behind
  // it: both read its measured height through `--range-h`.
  const [barHeight, setBarHeight] = useState(0)
  const measureBar = useCallback((el: HTMLDivElement | null) => {
    if (!el) return
    const observer = new ResizeObserver(() => setBarHeight(el.offsetHeight))
    observer.observe(el)
    setBarHeight(el.offsetHeight)
    return () => {
      observer.disconnect()
      setBarHeight(0)
    }
  }, [])
  /** The range the page last opened on (`from/to`), so a new range opens
   * again and a re-render of the same one does not. */
  const scrolledFor = useRef<string>(undefined)

  // Pushed, so back and forward walk the ranges a person chose.
  const [params, setParams] = useQueryStates(RANGE_PARAMS, { history: "push" })
  const window = useMemo(() => windowRange(spec, now), [spec, now])
  const chosen = useMemo(
    () =>
      ctx.mode === "page"
        ? resolveRange({ from: params.from, to: params.to }, window)
        : undefined,
    [ctx.mode, params.from, params.to, window]
  )
  const range = chosen ?? window
  const bounds = useMemo(
    () => (chosen ? rangeBounds(chosen) : undefined),
    [chosen]
  )
  const setRange = (next: TimelineRange | null) =>
    void setParams(
      next ? { from: next.from, to: next.to } : { from: null, to: null }
    )

  const implementors = useQuery({
    ...implementorsOptions(spec.trait ?? ""),
    enabled: Boolean(spec.trait),
  })

  // The registry's copy of each implementor when it has one, so a kind is
  // one object everywhere the page compares it.
  const implementorData = implementors.data
  const readKinds = useMemo(() => {
    const raw = spec.trait
      ? (implementorData ?? NO_KINDS)
      : kind
        ? [kind]
        : NO_KINDS
    return raw
      .map((k) => kindByIdentity(kinds, k.identity) ?? k)
      .filter(isTimelineKind)
  }, [spec.trait, implementorData, kind, kinds])

  const reads = useMemo(
    () => readKinds.map((k) => timelineRead(spec, ctx, k, now, bounds)),
    [readKinds, spec, ctx, now, bounds]
  )

  const results = useQueries({
    queries: reads.map((read) => ({
      ...recordsQueryOptions(read.params ?? NO_LIST),
      enabled: Boolean(read.params),
    })),
  })

  const pages = results.map((result, i) => ({
    read: reads[i],
    page: result.data,
    error: result.error,
    isPending: Boolean(reads[i].params) && result.isPending,
  }))
  // At most `first` rows per kind, so the merge is cheaper than a memo key.
  const days = groupByDay(
    mergeRows(
      pages.map((p) => ({ read: p.read, records: p.page?.records ?? [] }))
    )
  )

  const today = dayKey(now)
  const anyLoaded = days.length > 0
  const anyPending = pages.some((p) => p.isPending)
  const failed = pages.filter((p) => p.error)
  const cut = pages.filter((p) => p.page?.cursor)
  const problems = reads.flatMap((r) => r.problems)

  // Open on today (else the first day after it) once per range, and only
  // after every implementor has settled: a page landing later inserts days
  // above and would leave an earlier jump pointing at the wrong offset. A
  // range without today starts at the top; the first mount is already there.
  const settled = pages.length > 0 && !anyPending
  const rangeKey = `${range.from}/${range.to}`
  useEffect(() => {
    // A card or inline mount shares the host's scroller: jumping to today
    // there would scroll the page it sits in, so only a page mount jumps.
    if (ctx.mode !== "page") return
    if (!settled || scrolledFor.current === rangeKey) return
    const first = scrolledFor.current === undefined
    if (first && !anyLoaded) return
    scrolledFor.current = rangeKey
    const target = inRange(today, range)
      ? (days.find((d) => d.key === today) ?? days.find((d) => d.key > today))
      : undefined
    if (target) {
      dayRefs.current.get(target.key)?.scrollIntoView({ block: "start" })
    } else if (!first) {
      topRef.current?.scrollIntoView({ block: "start" })
    }
  }, [ctx.mode, settled, anyLoaded, days, today, range, rangeKey])

  const toggle = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  if (!spec.trait && !kind) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>Nothing to show yet</EmptyTitle>
          <EmptyDescription className="data break-words">
            {spec.kind
              ? `${spec.kind} is not installed`
              : "this view names no kind or trait"}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  if (implementors.error) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>Could not read the trait's implementors</EmptyTitle>
          <EmptyDescription className="data break-words">
            {implementors.error.message}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button variant="outline" onClick={() => void implementors.refetch()}>
            Retry
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  if (problems.length && !anyLoaded) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>{spec.empty ?? "Nothing to show yet"}</EmptyTitle>
          <EmptyDescription>
            {[...new Set(problems.map((p) => p.message))].join("; ")}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const loading =
    (spec.trait && implementors.isPending) || (anyPending && !anyLoaded)

  return (
    // Never `min-h-0`: the root must grow with its days, or it shrinks to the
    // scroller's height and the sticky bar leaves with it after one screen.
    <div
      ref={topRef}
      className="flex flex-1 flex-col"
      style={{ "--range-h": `${barHeight}px` } as CSSProperties}
    >
      {ctx.mode === "page" && (
        <div ref={measureBar} className="sticky top-0 z-[2]">
          <TimelineRangeBar
            range={range}
            chosen={Boolean(chosen)}
            now={now}
            onChange={setRange}
          />
        </div>
      )}
      {loading ? (
        <div className="flex items-center justify-center py-10">
          <Spinner className="size-5" />
        </div>
      ) : !anyLoaded ? (
        <Empty className="py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              {failed.length ? <TriangleAlertIcon /> : <CalendarIcon />}
            </EmptyMedia>
            <EmptyTitle>
              {failed.length && failed.length === pages.length
                ? "Could not read the timeline"
                : (spec.empty ??
                  (chosen
                    ? "Nothing in this range"
                    : "Nothing in this window"))}
            </EmptyTitle>
            <EmptyDescription>
              {!readKinds.length && spec.trait
                ? "No installed kind binds a temporal point."
                : rangeLabel(range, now)}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        days.map((day) => {
          const isToday = day.key === today
          return (
            <section
              key={day.key}
              ref={(el) => {
                if (el) dayRefs.current.set(day.key, el)
                else dayRefs.current.delete(day.key)
              }}
              aria-label={dayLabel(day.key, now)}
              aria-current={isToday ? "date" : undefined}
              className="scroll-mt-(--range-h)"
            >
              <GroupHeader
                label={dayLabel(day.key, now)}
                count={day.rows.length}
                collapsed={collapsed.has(day.key)}
                onToggle={() => toggle(day.key)}
                className={cn(
                  "top-(--range-h)",
                  isToday && "border-primary/30 bg-primary/10 text-primary"
                )}
              />
              {!collapsed.has(day.key) && (
                <ul>
                  {day.rows.map((row) => (
                    <TimelineItem
                      key={`${row.kind.identity}/${row.record.id}`}
                      row={row}
                      onOpen={() => onOpenRecord(row.record)}
                    />
                  ))}
                </ul>
              )}
            </section>
          )
        })
      )}

      {(failed.length > 0 || cut.length > 0 || (anyPending && anyLoaded)) && (
        <footer className="flex flex-col gap-1 border-t px-4 py-3 text-xs text-muted-foreground">
          {failed.map((p) => (
            <p key={p.read.kind.identity} className="flex items-start gap-1.5">
              <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0 text-warning" />
              <span className="min-w-0 break-words">
                Could not read {p.read.kind.name}: {p.error?.message}
              </span>
            </p>
          ))}
          {cut.map((p) => (
            <p key={p.read.kind.identity}>
              showing the first {p.page?.records?.length ?? 0} of{" "}
              {p.read.kind.name}
            </p>
          ))}
          {anyPending && anyLoaded && (
            <p className="flex items-center gap-2">
              <Spinner className="size-3" />
              Loading more kinds
            </p>
          )}
          {failed.length > 0 && (
            <Button
              variant="outline"
              size="sm"
              className="mt-1 h-9 self-start"
              onClick={() => {
                for (const [i, p] of pages.entries()) {
                  if (p.error) void results[i].refetch()
                }
              }}
            >
              Retry
            </Button>
          )}
        </footer>
      )}
    </div>
  )
}
