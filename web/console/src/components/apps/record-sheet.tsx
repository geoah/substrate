/** What a row tap opens when the view names no `opens`: the record read-only
 * (`PropertiesRail`), the view's row actions the record admits as full-width
 * buttons, and the door to the generic record page. Bottom on a phone, right
 * on a desktop; the route owns whether it is open, so back closes it. The
 * same frame says "no such record" when the segment names one the read
 * cannot answer, so a stale link is told rather than ignored. */

import type { ReactNode } from "react"
import { Link } from "@tanstack/react-router"
import { ExternalLinkIcon, TriangleAlertIcon } from "lucide-react"

import { ActionButton } from "@/components/apps/action-button"
import { PropertiesRail } from "@/components/record/properties"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { useIsMobile } from "@/hooks/use-mobile"
import { splitKind } from "@/lib/api/http"
import type { SubstrateRecord } from "@/lib/api/types"
import { rowActionsFor } from "@/lib/apps/actions"
import { titleOf } from "@/lib/apps/referents"
import type { ActionHost } from "@/lib/apps/spec"
import { kindByIdentity } from "@/lib/definition"
import { cn } from "@/lib/utils"

function Frame({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: ReactNode
  children: ReactNode
  footer: ReactNode
}) {
  const isMobile = useIsMobile()
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side={isMobile ? "bottom" : "right"}
        className={cn(
          "gap-0 p-0",
          isMobile ? "max-h-[92dvh] rounded-t-2xl" : "sm:max-w-md"
        )}
      >
        <SheetHeader className="shrink-0 pr-12">
          <SheetTitle className="truncate">{title}</SheetTitle>
          <SheetDescription className="truncate data text-xs">
            {description}
          </SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain border-t [&>div]:px-4">
          {children}
        </div>
        <div className="flex shrink-0 flex-col gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          {footer}
        </div>
      </SheetContent>
    </Sheet>
  )
}

export function RecordSheet({
  host,
  record,
  open,
  onOpenChange,
}: {
  host: ActionHost
  record: SubstrateRecord
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  // A trait view's row may be of another kind than the view's.
  const kind =
    host.kind?.identity === record.kind
      ? host.kind
      : kindByIdentity(host.kinds, record.kind)
  const rowHost: ActionHost = { ...host, kind }
  const actions = rowActionsFor(rowHost, record)
  const { authority, pkg, name } = splitKind(record.kind)
  return (
    <Frame
      open={open}
      onOpenChange={onOpenChange}
      title={titleOf(record)}
      description={
        <>
          {kind?.name ?? record.kind} · {record.id}
        </>
      }
      footer={
        <>
          {actions.map((action) => (
            <ActionButton
              key={action.name}
              host={rowHost}
              action={action}
              record={record}
              className="w-full justify-start"
            />
          ))}
          <Button
            variant="ghost"
            className="h-11 w-full justify-start gap-1.5 px-3 text-muted-foreground"
            render={
              <Link
                to="/data/$authority/$pkg/$name/$id"
                params={{ authority, pkg, name, id: record.id }}
              />
            }
          >
            <ExternalLinkIcon className="size-4" />
            Open in data
          </Button>
        </>
      }
    >
      <PropertiesRail record={record} kind={kind} kinds={host.kinds} />
    </Frame>
  )
}

/** The sheet for a segment the read could not answer. It closes the way a
 * record's sheet does, so the address clears instead of lingering. */
export function MissingRecordSheet({
  segment,
  message,
  open,
  onOpenChange,
}: {
  segment: string
  message: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Frame
      open={open}
      onOpenChange={onOpenChange}
      title="No such record"
      description={segment}
      footer={
        <Button
          variant="outline"
          className="h-11 w-full"
          onClick={() => onOpenChange(false)}
        >
          Close
        </Button>
      }
    >
      <div className="flex items-start gap-3 py-4 text-sm">
        <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
        <p className="min-w-0 break-words">{message}</p>
      </div>
    </Frame>
  )
}
