/** Dashboard zone 3: what needs a verdict — the pending count and the top of
 * the queue in the shared merge-request voice (MergePair + EvidenceChips).
 * Each card opens its request; the header opens the kind's collection, which
 * is the queue now that the console has no bespoke one. The count rides the
 * page read where the queue is short (a cursorless page IS the count) and
 * probes only when it isn't. */

import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { GitMergeIcon } from "lucide-react"

import { ZoneError } from "@/components/home/zone"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { CORE_AUTHORITY } from "@/lib/api/http"
import {
  MR_NAME,
  mergeRequestsQueryOptions,
  pendingMergeCountQueryOptions,
} from "@/lib/api/mergerequests"
import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import { relativeTime } from "@/lib/format"
import { EvidenceChips, MergePair } from "@/components/merge-request"
import { mergePair } from "@/lib/mergerequests"

/** Evidence cards the zone shows; the queue holds the rest. */
const TOP_N = 3

function PendingCard({
  mr,
  kinds,
}: {
  mr: SubstrateRecord
  kinds: KindInfo[]
}) {
  const navigate = useNavigate()
  const pair = mergePair(mr)
  return (
    <div
      role="link"
      tabIndex={0}
      className="flex cursor-pointer flex-col gap-1.5 rounded-md border px-3 py-2.5 text-xs transition-colors hover:bg-muted/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      onClick={() =>
        void navigate({ to: "/merge-requests/$id", params: { id: mr.id } })
      }
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          void navigate({ to: "/merge-requests/$id", params: { id: mr.id } })
        }
      }}
    >
      <span className="flex w-full items-center gap-3">
        <MergePair loser={pair.loser} winner={pair.winner} types={kinds} />
        <span
          className="ml-auto shrink-0 data text-muted-foreground"
          title={mr.createdAt}
        >
          {relativeTime(mr.createdAt)}
        </span>
      </span>
      <EvidenceChips mr={mr} />
    </div>
  )
}

export function MergeRequestsCard({ kinds }: { kinds: KindInfo[] }) {
  const requests = useQuery(
    mergeRequestsQueryOptions({ decision: "proposed", first: TOP_N })
  )
  const rows = requests.data?.records ?? []
  // A cursorless first page counts itself; only a longer queue pays the walk.
  const derivedTotal =
    requests.data && !requests.data.cursor ? rows.length : undefined
  const count = useQuery({
    ...pendingMergeCountQueryOptions(),
    enabled: Boolean(requests.data?.cursor),
  })
  const pending = derivedTotal ?? count.data?.value
  const pendingCapped =
    derivedTotal === undefined && Boolean(count.data?.capped)

  return (
    <Card size="sm" className="h-full gap-2">
      <CardHeader>
        <CardTitle className="flex items-baseline gap-2">
          <Link
            to="/data/$authority/$name"
            params={{ authority: CORE_AUTHORITY, name: MR_NAME }}
            className="underline-offset-4 hover:underline"
          >
            Merge requests
          </Link>
          {pending !== undefined && pending > 0 && (
            <span className="data text-xs font-normal text-muted-foreground">
              {pending.toLocaleString()}
              {pendingCapped ? "+" : ""} pending
            </span>
          )}
        </CardTitle>
        <CardAction>
          <Link
            to="/data/$authority/$name"
            params={{ authority: CORE_AUTHORITY, name: MR_NAME }}
            className="text-xs text-muted-foreground underline-offset-4 hover:underline"
          >
            View queue
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent>
        {requests.isPending ? (
          <div className="flex flex-col gap-2">
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-md" />
            ))}
          </div>
        ) : requests.isError ? (
          <ZoneError
            message={requests.error.message}
            onRetry={() => void requests.refetch()}
          />
        ) : rows.length === 0 ? (
          <div className="flex items-center gap-2 py-1 text-xs text-muted-foreground">
            <GitMergeIcon className="size-3.5" />
            Nothing waits on a verdict.
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {rows.map((mr) => (
              <PendingCard key={mr.id} mr={mr} kinds={kinds} />
            ))}
            {pending !== undefined && pending > rows.length && (
              <Link
                to="/data/$authority/$name"
                params={{ authority: CORE_AUTHORITY, name: MR_NAME }}
                className="text-xs text-muted-foreground underline-offset-4 hover:underline"
              >
                +{(pending - rows.length).toLocaleString()}
                {pendingCapped ? "+" : ""} more in the queue
              </Link>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
