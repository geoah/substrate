/** The contacts layout: the filtered collection walked to its end and
 * sectioned A–Z on the title's initial ("#" for the rest), an initials avatar
 * per row, then the `show` properties drawn by datatype (an email is a
 * `mailto:` link, a reference its referent's title with the link's string
 * properties beside it, a repeated value its first plus "+n"), a local filter
 * over the loaded rows, an index rail on touch, and the view's first
 * row-placed action as the row's trailing button. Every page is loaded
 * because an index over a partial page would jump to sections that are not
 * there yet. The facet chips sit above the filter box wherever the mount
 * threads a selection (`ctx.facets`), which `useViewRecords` already ANDs
 * into the read, and stay while the rows come back empty so a narrowing can
 * be undone. Reads go through `useViewRecords`, writes through `runAction`,
 * and a row tap is reported through `onOpenRecord`; the screen owns the
 * chrome and the primary create. */

import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react"
import { ContactIcon, TriangleAlertIcon } from "lucide-react"

import { FacetBar } from "@/components/apps/facet-bar"
import { GroupHeader } from "@/components/apps/group-header"
import { cellNode } from "@/components/apps/list-view"
import { RowActions } from "@/components/apps/row-actions"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { useIsMobile } from "@/hooks/use-mobile"
import { readReference, type SubstrateRecord } from "@/lib/api/types"
import { specOf } from "@/lib/apps/cond"
import { useViewRecords } from "@/lib/apps/queries"
import { useReferentTitles } from "@/lib/apps/referents"
import type { ActionHost, LayoutProps } from "@/lib/apps/spec"
import { splitRecordPath } from "@/lib/record-path"
import type { PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import {
  SECTIONS,
  displayTitle,
  firstAndMore,
  groupContacts,
  initialsOf,
  matchesNeedle,
} from "./contacts-sections"

function stop(e: { stopPropagation: () => void }) {
  e.stopPropagation()
}

/** One `show` value by its declared datatype: an email is a `mailto:` link
 * and a reference its referent's title with the link's string properties,
 * both as the first value plus "+n"; every other datatype draws as the list
 * layout draws it. A link stops the tap so a mailto never also opens the
 * record. */
function ShowCell({
  spec,
  value,
  titles,
}: {
  spec: PropSpec
  value: unknown
  titles: Map<string, string>
}) {
  if (spec.kind !== "email" && spec.kind !== "reference") {
    return cellNode(spec, value, titles)
  }
  const { first, more } = firstAndMore(value)
  if (first === undefined || first === null || first === "") return null
  let cell: ReactNode
  if (spec.kind === "email") {
    cell = (
      <a
        href={`mailto:${String(first)}`}
        onClick={stop}
        className="truncate text-primary underline-offset-2 hover:underline"
      >
        {String(first)}
      </a>
    )
  } else {
    const held = readReference(first)
    if (!held) return null
    const title =
      titles.get(held.path) ?? splitRecordPath(held.path)?.id ?? held.path
    const link = (spec.linkFields ?? [])
      .filter((f) => f.kind === "string")
      .map((f) => held.properties[f.name])
      .filter((v): v is string => typeof v === "string" && v !== "")
    cell = (
      <span className="min-w-0 break-words">
        {[title, ...link].join(" · ")}
      </span>
    )
  }
  return (
    <span className="flex max-w-full min-w-0 items-center">
      {cell}
      {more > 0 && <span className="ml-1 text-muted-foreground">+{more}</span>}
    </span>
  )
}

/** The A–Z rail: a touch-width strip whose letters are targets and whose
 * whole height scrubs, so a finger dragged down it walks the sections. A
 * letter with no section is shown dimmed and does nothing. */
function IndexRail({
  present,
  onJump,
}: {
  present: Set<string>
  onJump: (key: string) => void
}) {
  const ref = useRef<HTMLElement>(null)
  const jumpAt = (clientY: number) => {
    const items = ref.current?.querySelectorAll<HTMLElement>("[data-section]")
    if (!items) return
    for (const item of items) {
      const r = item.getBoundingClientRect()
      if (clientY >= r.top && clientY < r.bottom) {
        const key = item.dataset.section
        if (key && present.has(key)) onJump(key)
        return
      }
    }
  }
  return (
    <nav
      ref={ref}
      aria-label="Index"
      className="absolute top-2 right-0 z-[2] flex w-7 touch-none flex-col items-center rounded-l-lg bg-background/70 py-1 backdrop-blur select-none"
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        jumpAt(e.clientY)
      }}
      onPointerMove={(e) => {
        if (e.buttons) jumpAt(e.clientY)
      }}
    >
      {SECTIONS.map((key) => (
        <button
          key={key}
          type="button"
          data-section={key}
          tabIndex={-1}
          aria-label={`Jump to ${key}`}
          disabled={!present.has(key)}
          onClick={() => onJump(key)}
          className={cn(
            "flex h-4 w-full items-center justify-center text-[0.65rem] leading-none font-medium",
            present.has(key) ? "text-primary" : "text-muted-foreground/40"
          )}
        >
          {key}
        </button>
      ))}
    </nav>
  )
}

function ContactRow({
  record,
  host,
  showSpecs,
  titles,
  onOpen,
}: {
  record: SubstrateRecord
  host: ActionHost
  showSpecs: PropSpec[]
  titles: Map<string, string>
  onOpen: () => void
}) {
  const title = displayTitle(record)
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault()
      onOpen()
    }
  }
  return (
    <li className="flex items-stretch border-b last:border-b-0">
      <div
        role="button"
        tabIndex={0}
        onClick={onOpen}
        onKeyDown={onKey}
        className="flex min-h-14 min-w-0 flex-1 items-center gap-3 px-4 py-2 text-left outline-none select-none [-webkit-touch-callout:none] focus-visible:bg-muted/60 active:bg-muted/60"
      >
        <Avatar size="lg" className="shrink-0">
          <AvatarFallback className="text-xs font-medium">
            {initialsOf(title) || "?"}
          </AvatarFallback>
        </Avatar>
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-sm font-medium">{title}</span>
          {showSpecs.length > 0 && (
            <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
              {showSpecs.map((spec) => (
                <ShowCell
                  key={spec.name}
                  spec={spec}
                  value={record.properties[spec.name]}
                  titles={titles}
                />
              ))}
            </div>
          )}
        </div>
      </div>
      <RowActions host={host} record={record} className="pr-2" />
    </li>
  )
}

export default function ContactsLayout({
  spec,
  kind,
  kinds,
  ctx,
  onOpenRecord,
}: LayoutProps) {
  const isMobile = useIsMobile()
  const view = useViewRecords(spec, ctx, kinds)
  const host = useMemo<ActionHost>(
    () => ({ spec, kind, kinds, ctx }),
    [spec, kind, kinds, ctx]
  )
  const [needle, setNeedle] = useState("")
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(
    () => new Set()
  )
  const sectionRefs = useRef(new Map<string, HTMLElement>())

  // The index needs the whole collection, so the walk continues while a
  // cursor remains. Guarded on the in-flight flag: a second fetchNextPage
  // would cancel and restart the one running.
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = view
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage) fetchNextPage()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const showSpecs = useMemo(
    () =>
      spec.show
        .map((name) => specOf(kind, name))
        .filter((s): s is PropSpec => Boolean(s)),
    [spec.show, kind]
  )
  const referenceNames = useMemo(
    () => showSpecs.filter((s) => s.kind === "reference").map((s) => s.name),
    [showSpecs]
  )
  const emailNames = useMemo(
    () => showSpecs.filter((s) => s.kind === "email").map((s) => s.name),
    [showSpecs]
  )
  const titles = useReferentTitles(view.records, referenceNames, kinds)

  const filtered = useMemo(
    () => view.records.filter((r) => matchesNeedle(r, needle, emailNames)),
    [view.records, needle, emailNames]
  )
  const sections = useMemo(() => groupContacts(filtered), [filtered])
  const present = useMemo(() => new Set(sections.map((s) => s.key)), [sections])

  const toggle = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  const jump = (key: string) => {
    sectionRefs.current.get(key)?.scrollIntoView({ block: "start" })
  }

  if (!kind) {
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
              : "this view names no kind"}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  if (view.problems.length) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>{spec.empty ?? "Nothing to show yet"}</EmptyTitle>
          <EmptyDescription>
            {view.problems.map((p) => p.message).join("; ")}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const walking = view.isPending || hasNextPage || isFetchingNextPage

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {ctx.facets && (spec.facets?.length ?? 0) > 0 && (
        <FacetBar
          spec={spec}
          kind={kind}
          kinds={kinds}
          records={view.records}
        />
      )}
      <div className="px-4 py-2">
        <Input
          type="search"
          inputMode="search"
          enterKeyHint="search"
          autoComplete="off"
          aria-label="Filter contacts"
          placeholder="Filter by name or email"
          value={needle}
          onChange={(e) => setNeedle(e.target.value)}
          className="h-11 md:h-9"
        />
      </div>

      {view.error && !view.records.length ? (
        <Empty className="py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TriangleAlertIcon />
            </EmptyMedia>
            <EmptyTitle>Could not read the contacts</EmptyTitle>
            <EmptyDescription className="data break-words">
              {view.error.message}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" onClick={() => view.refetch()}>
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      ) : view.isPending ? (
        <div className="flex items-center justify-center py-10">
          <Spinner className="size-5" />
        </div>
      ) : !view.records.length ? (
        <Empty className="py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ContactIcon />
            </EmptyMedia>
            <EmptyTitle>{spec.empty ?? "No contacts yet"}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : !filtered.length ? (
        <p className="px-4 py-8 text-center text-sm text-muted-foreground">
          No one matches “{needle.trim()}”
        </p>
      ) : (
        <>
          {isMobile && (
            <div className="sticky top-0 z-[2] h-0">
              <IndexRail present={present} onJump={jump} />
            </div>
          )}
          <div className={cn(isMobile && "pr-7")}>
            {sections.map((section) => (
              <section
                key={section.key}
                ref={(el) => {
                  if (el) sectionRefs.current.set(section.key, el)
                  else sectionRefs.current.delete(section.key)
                }}
                aria-label={section.key}
              >
                <GroupHeader
                  label={section.key}
                  count={section.records.length}
                  collapsed={collapsed.has(section.key)}
                  onToggle={() => toggle(section.key)}
                />
                {!collapsed.has(section.key) && (
                  <ul>
                    {section.records.map((record) => (
                      <ContactRow
                        key={record.id}
                        record={record}
                        host={host}
                        showSpecs={showSpecs}
                        titles={titles}
                        onOpen={() => onOpenRecord(record)}
                      />
                    ))}
                  </ul>
                )}
              </section>
            ))}
          </div>
        </>
      )}

      {walking && view.records.length > 0 && (
        <div className="flex items-center justify-center gap-2 py-4 text-xs text-muted-foreground">
          <Spinner className="size-3.5" />
          Loading more
        </div>
      )}
      {!walking && view.records.length > 0 && (
        <p className="py-4 text-center text-xs text-muted-foreground">
          {filtered.length === view.records.length
            ? `${view.records.length} ${view.records.length === 1 ? "contact" : "contacts"}`
            : `${filtered.length} of ${view.records.length} contacts`}
        </p>
      )}
    </div>
  )
}
