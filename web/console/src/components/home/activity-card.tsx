/** Dashboard zone 2: the last stretch of the change feed, FLAT (the intent
 * fold died with the 2026-08-06 ruling) and compacted to the table system's
 * row voice — actor, action, record, time. No watch here: the query
 * refetches on a 60s cadence, and every row is a door — the row opens the
 * changelog, the record link its record, the actor chip its actor. */

import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { InboxIcon } from "lucide-react"

import { ActorChip } from "@/components/actor-chip"
import { ChangeRecordLink } from "@/components/data-table/columns"
import { ZoneError } from "@/components/home/zone"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { recentChangesQueryOptions } from "@/lib/api/overview"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
import { relativeTime } from "@/lib/format"
import { isVocabularyChange, verbOf } from "@/lib/changelog"
import { cn } from "@/lib/utils"

/** Rows the card shows — enough to read the morning, one screen. */
const SHOWN_ROWS = 8

function ActivityRow({ row, kinds }: { row: ChangeRow; kinds: KindInfo[] }) {
  const navigate = useNavigate()
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => void navigate({ to: "/changelog" })}
      onKeyDown={(e) => {
        if (e.key === "Enter") void navigate({ to: "/changelog" })
      }}
      className={cn(
        "grid cursor-pointer grid-cols-[auto_auto_minmax(0,1fr)_auto] items-center gap-2 border-b px-4 py-2 text-xs last:border-0 hover:bg-muted/50",
        // The changelog's own schema voice: a quiet primary tint.
        isVocabularyChange(row) && "bg-primary/5 dark:bg-primary/10"
      )}
    >
      <ActorChip actor={row.actor} />
      <span className="data shrink-0 text-muted-foreground">
        {verbOf(row)}
      </span>
      <ChangeRecordLink row={row} kinds={kinds} />
      <span
        className="data text-muted-foreground"
        // hover shows the wire ISO verbatim — one convention everywhere
        title={row.ts}
      >
        {relativeTime(row.ts)}
      </span>
    </div>
  )
}

export function ActivityCard({ kinds }: { kinds: KindInfo[] }) {
  const recent = useQuery(recentChangesQueryOptions())

  return (
    <Card size="sm" className="h-full gap-2">
      <CardHeader>
        <CardTitle>
          <Link to="/changelog" className="underline-offset-4 hover:underline">
            Recent activity
          </Link>
        </CardTitle>
        <CardAction>
          <Link
            to="/changelog"
            className="text-xs text-muted-foreground underline-offset-4 hover:underline"
          >
            View changelog
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent className="px-0">
        {recent.isPending ? (
          <div className="flex flex-col gap-2 px-4 py-1">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-6 w-full" />
            ))}
          </div>
        ) : recent.isError ? (
          <div className="px-4">
            <ZoneError
              message={recent.error.message}
              onRetry={() => void recent.refetch()}
            />
          </div>
        ) : recent.data.length === 0 ? (
          <div className="flex items-center gap-2 px-4 py-3 text-xs text-muted-foreground">
            <InboxIcon className="size-3.5" />
            The changelog is empty — nothing has written yet.
          </div>
        ) : (
          <div className="flex flex-col">
            {recent.data.slice(0, SHOWN_ROWS).map((row) => (
              <ActivityRow key={row.seq} row={row} kinds={kinds} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
