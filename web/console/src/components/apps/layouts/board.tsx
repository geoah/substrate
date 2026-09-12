/** The board layout: one column per value of `groupBy` (a state or an enum),
 * the values the filter admits else every declared one, in declaration order,
 * each holding its cards and its count. A card is the title and the `show`
 * cells. Under a pointer a card drags to another column only along an arm
 * the machine declares (an enum has no machine, so any column takes it) and
 * the drop is the same transition or patch a button would make, Undo toast
 * included. Under 768 px the columns are a segmented control over one list,
 * so nothing scrolls sideways. A board reads one page and says so when the
 * page was cut. A tap reports the card; the screen pushes `opens`. */

import { useMemo, useState, type DragEvent } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { TriangleAlertIcon } from "lucide-react"

import { ActionButton } from "@/components/apps/action-button"
import { IncompleteNote } from "@/components/apps/incomplete-note"
import { ProblemList } from "@/components/apps/problems"
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useIsMobile } from "@/hooks/use-mobile"
import type { SubstrateRecord } from "@/lib/api/types"
import { runAction } from "@/lib/apps/actions"
import { specOf } from "@/lib/apps/cond"
import { admits } from "@/lib/apps/machine"
import { useViewPage } from "@/lib/apps/queries"
import { titleOf, useReferentTitles } from "@/lib/apps/referents"
import type {
  ActionHost,
  ActionSpec,
  LayoutProps,
  Problem,
} from "@/lib/apps/spec"
import type { PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import {
  EmptyLine,
  ShowCells,
  columnsOf,
  referenceNames,
  showNames,
} from "./shared"

const CONTRACT: Problem = {
  path: "groupBy",
  message: "a board groups by a state or enum property",
  severity: "error",
}

/** The move a drop makes: a transition along the machine for a state, a
 * patch of the value for an enum. */
function moveAction(group: PropSpec, to: string): ActionSpec {
  const base = {
    name: `move-${to}`,
    label: to,
    placement: "row" as const,
    prompt: [],
    confirm: false,
  }
  return group.kind === "state"
    ? { ...base, verb: "transition", to, property: group.name, set: {} }
    : { ...base, verb: "patch", set: { [group.name]: to } }
}

function valueOf(record: SubstrateRecord, group: PropSpec): string {
  const value = record.properties[group.name]
  return value === undefined || value === null ? "" : String(value)
}

export default function BoardLayout({
  spec,
  kind,
  kinds,
  ctx,
  onOpenRecord,
}: LayoutProps) {
  const group = spec.groupBy ? specOf(kind, spec.groupBy) : undefined
  const grouped =
    group !== undefined && (group.kind === "state" || group.kind === "enum")
  const page = useViewPage(spec, ctx, kinds)
  const names = useMemo(
    () => showNames(spec, kind).filter((n) => n !== spec.groupBy),
    [spec, kind]
  )
  const titles = useReferentTitles(
    page.records,
    referenceNames(names, kind),
    kinds
  )
  const columns = useMemo(
    () => (group && grouped ? columnsOf(group, spec.filter) : []),
    [group, grouped, spec.filter]
  )
  const isMobile = useIsMobile()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<string>()
  const [dragging, setDragging] = useState<SubstrateRecord | null>(null)
  const [over, setOver] = useState<string | null>(null)
  const host: ActionHost = { spec, kind, kinds, ctx }

  if (!kind || !group || !grouped) {
    return <ProblemList problems={[CONTRACT]} />
  }
  if (page.problems.length) {
    return (
      <Empty className="py-16">
        <EmptyHeader>
          <EmptyTitle>{spec.empty ?? "Nothing here yet"}</EmptyTitle>
          <EmptyDescription>{page.problems[0].message}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  if (page.isPending) {
    return (
      <div className="flex gap-3 px-4 py-3">
        {columns.map((c) => (
          <Skeleton key={c.key} className="h-40 w-full md:w-72" />
        ))}
      </div>
    )
  }
  if (page.error) {
    return (
      <Empty className="py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>Could not read the board</EmptyTitle>
          <EmptyDescription className="break-words">
            {page.error.message}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button variant="outline" onClick={page.refetch}>
            Retry
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const buckets = new Map<string, SubstrateRecord[]>(
    columns.map((c) => [c.key, []])
  )
  for (const record of page.records) {
    buckets.get(valueOf(record, group))?.push(record)
  }
  const canMove = (record: SubstrateRecord, to: string): boolean => {
    const from = valueOf(record, group)
    if (from === to) return false
    return group.kind === "state" ? admits(kind, group.name, from, to) : true
  }
  const canDrag = (record: SubstrateRecord): boolean =>
    !isMobile && columns.some((c) => canMove(record, c.key))
  const drop = (to: string) => {
    const record = dragging
    setDragging(null)
    setOver(null)
    if (!record || !canMove(record, to)) return
    void runAction({
      ...host,
      action: moveAction(group, to),
      record,
      queryClient,
    })
  }
  // In a page mount the screen owns the one primary button; a card or an
  // inline mount has no chrome, so the create sits in the board's own header.
  const create =
    ctx.mode === "page"
      ? undefined
      : spec.actions.find((a) => a.verb === "create")
  const empty = spec.empty ?? "Nothing here"

  const card = (record: SubstrateRecord) => (
    <li key={record.id}>
      <button
        type="button"
        draggable={canDrag(record)}
        data-dragging={dragging?.id === record.id || undefined}
        onDragStart={(e: DragEvent<HTMLButtonElement>) => {
          e.dataTransfer.setData("text/plain", record.id)
          e.dataTransfer.effectAllowed = "move"
          setDragging(record)
        }}
        onDragEnd={() => {
          setDragging(null)
          setOver(null)
        }}
        onClick={() => onOpenRecord(record)}
        className={cn(
          "flex min-h-11 w-full flex-col items-start gap-1 rounded-lg border bg-card px-3 py-2.5 text-left text-sm shadow-xs transition-colors outline-none select-none [-webkit-touch-callout:none] hover:bg-muted/60 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 active:bg-muted/60 data-dragging:opacity-40",
          canDrag(record) && "cursor-grab active:cursor-grabbing"
        )}
      >
        <span className="w-full leading-snug font-medium break-words">
          {titleOf(record)}
        </span>
        <ShowCells record={record} kind={kind} names={names} titles={titles} />
      </button>
    </li>
  )

  const header = create && (
    <div className="flex items-center justify-end px-4 pt-3">
      <ActionButton host={host} action={create} />
    </div>
  )
  const footer = (
    <IncompleteNote
      shown={page.incomplete ? page.records.length : 0}
      first={spec.first}
      unread={page.unread}
    />
  )

  if (isMobile) {
    const active = columns.find((c) => c.key === selected) ?? columns[0]
    const rows = active ? (buckets.get(active.key) ?? []) : []
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {header}
        <Tabs
          value={active?.key}
          onValueChange={(value) => setSelected(String(value))}
          className="px-3 pt-2 pb-1"
        >
          <TabsList
            className="h-12 w-full group-data-horizontal/tabs:h-12"
            aria-label="Columns"
          >
            {columns.map((c) => (
              <TabsTrigger
                key={c.key}
                value={c.key}
                className="gap-1.5 text-sm"
              >
                <span className="truncate">{c.label}</span>
                <span className="data text-xs text-muted-foreground">
                  {buckets.get(c.key)?.length ?? 0}
                </span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        {rows.length ? (
          <ul className="flex flex-col gap-2 px-3 py-2">{rows.map(card)}</ul>
        ) : (
          <EmptyLine>{empty}</EmptyLine>
        )}
        {footer}
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header}
      <div className="flex min-h-0 flex-1 items-start gap-3 overflow-x-auto px-4 py-3">
        {columns.map((c) => {
          const rows = buckets.get(c.key) ?? []
          const admitted = dragging ? canMove(dragging, c.key) : false
          return (
            <section
              key={c.key}
              aria-label={c.label}
              data-drop={
                dragging ? (admitted ? "admitted" : "refused") : undefined
              }
              onDragOver={(e) => {
                if (!admitted) return
                e.preventDefault()
                e.dataTransfer.dropEffect = "move"
                if (over !== c.key) setOver(c.key)
              }}
              onDragLeave={() => {
                if (over === c.key) setOver(null)
              }}
              onDrop={(e) => {
                e.preventDefault()
                drop(c.key)
              }}
              className={cn(
                "flex w-72 shrink-0 flex-col rounded-xl bg-muted/40 ring-1 ring-foreground/5 transition-colors",
                over === c.key && admitted && "bg-primary/5 ring-primary/40",
                dragging && !admitted && "opacity-60"
              )}
            >
              <h3 className="flex min-h-10 items-center gap-2 px-3 text-xs font-medium tracking-wide text-muted-foreground uppercase">
                <span className="min-w-0 flex-1 truncate">{c.label}</span>
                <span className="data normal-case">{rows.length}</span>
              </h3>
              {rows.length ? (
                <ul className="flex flex-col gap-2 px-2 pb-2">
                  {rows.map(card)}
                </ul>
              ) : (
                <EmptyLine className="pb-4">{empty}</EmptyLine>
              )}
            </section>
          )
        })}
      </div>
      {footer}
    </div>
  )
}
