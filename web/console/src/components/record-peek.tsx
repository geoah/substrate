/** The edge cell's HoverCard peek: the target's title where the cell shows
 * it, and on hover — lazily, never before — the target record's path and one
 * or two facts. The peek is a preview, not a page: clicking the trigger
 * navigates to the real record route. */

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card"
import { Skeleton } from "@/components/ui/skeleton"
import { recordQueryOptions } from "@/lib/api/records"
import type { EdgeTarget, KindInfo } from "@/lib/api/types"
import { cellValue, recordTitle, referenceCell } from "@/lib/format"
import {
  columnProperties,
  propertyTypeLabel,
  splitKind,
  kindByIdentity,
} from "@/lib/definition"
import { cn } from "@/lib/utils"

export function RecordPeek({
  target,
  types,
}: {
  target: EdgeTarget
  /** The registry, so the peek can resolve the target's kind to a route. */
  types: KindInfo[]
}) {
  const [open, setOpen] = useState(false)
  const targetKind = kindByIdentity(types, target.kind)
  const label = target.title || target.id
  // An untitled target shows its id — an identifier, so the data voice
  // (codex finding, 2026-08-06: bold-sans ids read wrong in the MR pair).
  const idVoice = !target.title
  const { authority } = splitKind(target.kind)

  // Unknown kind (not in the registry) — no route to peek at; plain text.
  if (!targetKind) {
    return <span className="data">{label}</span>
  }

  const path = `${authority}/${targetKind.name}/${target.id}`

  return (
    <HoverCard onOpenChange={setOpen}>
      <HoverCardTrigger
        render={
          <Link
            to="/data/$authority/$collection/$id"
            params={{
              authority: authority,
              collection: targetKind.name,
              id: target.id,
            }}
            onClick={(e) => e.stopPropagation()}
          />
        }
        // max-w-full: truncation happens at the column boundary, never at
        // an arbitrary clamp (owner ruling, 2026-08-06)
        className={cn(
          "inline-block max-w-full truncate align-bottom underline-offset-4 hover:underline",
          idVoice && "data font-normal"
        )}
      >
        {label}
      </HoverCardTrigger>
      {/* overflow-hidden + viewport-capped width: a long id or path must
          truncate inside the card, never paint past its edge or the
          viewport (owner redline, 2026-08-06) */}
      <HoverCardContent className="w-72 max-w-[calc(100vw-2rem)] overflow-hidden text-xs">
        <PeekBody
          open={open}
          authority={authority}
          collection={targetKind.name}
          id={target.id}
          targetKind={targetKind}
          fallbackTitle={label}
          path={path}
        />
      </HoverCardContent>
    </HoverCard>
  )
}

function PeekBody({
  open,
  authority,
  collection,
  id,
  targetKind,
  fallbackTitle,
  path,
}: {
  open: boolean
  authority: string
  collection: string
  id: string
  targetKind: KindInfo
  fallbackTitle: string
  path: string
}) {
  const record = useQuery({
    ...recordQueryOptions(authority, collection, id),
    enabled: open,
  })

  const facts: Array<{ name: string; value: string; doc: string }> = []
  if (record.data) {
    for (const prop of columnProperties(targetKind)) {
      const value = record.data.properties[prop.name]
      if (value === undefined || value === null) continue
      // A reference names its referent, not the referent's kind: the path's id
      // is the fact worth two lines of a peek.
      const text =
        prop.kind === "reference" ? referenceCell(value) : cellValue(value)
      if (!text) continue
      // The property name carries its declaration on hover (type, and the
      // record-56 one-liner where the kind wrote one) — a native title, not a
      // second floating layer over an already-floating card.
      facts.push({
        name: prop.name,
        value: text,
        doc: [propertyTypeLabel(prop), prop.description]
          .filter(Boolean)
          .join(" — "),
      })
      if (facts.length === 2) break
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="truncate font-semibold">
        {record.data
          ? recordTitle(record.data.properties) || id
          : fallbackTitle}
      </span>
      <span className="truncate data text-muted-foreground" title={path}>
        {path}
      </span>
      {record.isPending ? (
        <div className="mt-1 flex flex-col gap-1">
          <Skeleton className="h-3 w-4/5" />
          <Skeleton className="h-3 w-3/5" />
        </div>
      ) : facts.length ? (
        // minmax(0,1fr): a grid track's implicit min-width is `auto`, so a
        // long unbroken value would widen the track right past the card.
        <div className="mt-1 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5">
          {facts.map((fact) => (
            <span key={fact.name} className="contents">
              <span
                className="max-w-28 cursor-help truncate text-muted-foreground"
                title={fact.doc}
              >
                {fact.name}
              </span>
              <span className="truncate data">{fact.value}</span>
            </span>
          ))}
        </div>
      ) : null}
    </div>
  )
}
