/** The pending-changes queue on the overview: what a learner or an app has
 * proposed and nobody has decided, in the shared change-request voice (OpBadge
 * + ChangeTarget). Each card opens its request; the header opens the kind's
 * collection, which is the queue in full. The count rides the page read where
 * the queue is short (a cursorless page IS the count) and probes only when it
 * isn't, exactly as the merge queue does.
 *
 * NO PROPOSER on a row, deliberately: who proposed a change is the manager of
 * its `diff` property, and `propertyMeta` rides the SINGLE-record read only
 * (engine query.go), so a collection page cannot know it. Three extra reads to
 * label three rows is not a trade this glance is worth: the detail page says
 * who proposed, and the card does not pretend to. */

import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { FilePenLineIcon } from "lucide-react"

import { ChangeTarget, OpBadge } from "@/components/change-request"
import { ZoneError } from "@/components/home/zone"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  CR_NAME,
  changeRequestsQueryOptions,
  pendingChangeCountQueryOptions,
} from "@/lib/api/changerequests"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { changeOp, changeTarget, rationaleOf } from "@/lib/changerequests"
import { relativeTime } from "@/lib/format"

/** Cards the zone shows; the queue holds the rest. */
const TOP_N = 3

function PendingCard({
  request,
  kinds,
}: {
  request: SubstrateRecord
  kinds: KindInfo[]
}) {
  const navigate = useNavigate()
  const open = () =>
    void navigate({ to: "/change-requests/$id", params: { id: request.id } })
  const rationale = rationaleOf(request)
  return (
    <div
      role="link"
      tabIndex={0}
      className="flex cursor-pointer flex-col gap-1.5 rounded-md border px-3 py-2.5 text-xs transition-colors hover:bg-muted/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      onClick={open}
      onKeyDown={(e) => {
        if (e.key === "Enter") open()
      }}
    >
      <span className="flex w-full items-center gap-2">
        <OpBadge op={changeOp(request)} />
        <ChangeTarget target={changeTarget(request)} types={kinds} />
        <span
          className="ml-auto shrink-0 data text-muted-foreground"
          title={request.createdAt}
        >
          {relativeTime(request.createdAt)}
        </span>
      </span>
      {rationale && (
        <span className="line-clamp-2 text-muted-foreground">{rationale}</span>
      )}
    </div>
  )
}

export function ChangeRequestsCard({ kinds }: { kinds: KindInfo[] }) {
  const requests = useQuery(
    changeRequestsQueryOptions({ decision: "proposed", first: TOP_N })
  )
  const rows = requests.data?.records ?? []
  // A cursorless first page counts itself; only a longer queue pays the walk.
  const derivedTotal =
    requests.data && !requests.data.cursor ? rows.length : undefined
  const count = useQuery({
    ...pendingChangeCountQueryOptions(),
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
            to="/data/$authority/$pkg/$name"
            params={{
              authority: CORE_AUTHORITY,
              pkg: CORE_PACKAGE_NAME,
              name: CR_NAME,
            }}
            className="underline-offset-4 hover:underline"
          >
            Pending changes
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
            to="/data/$authority/$pkg/$name"
            params={{
              authority: CORE_AUTHORITY,
              pkg: CORE_PACKAGE_NAME,
              name: CR_NAME,
            }}
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
            <FilePenLineIcon className="size-3.5" />
            Nothing is waiting to be applied.
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {rows.map((request) => (
              <PendingCard key={request.id} request={request} kinds={kinds} />
            ))}
            {pending !== undefined && pending > rows.length && (
              <Link
                to="/data/$authority/$pkg/$name"
                params={{
                  authority: CORE_AUTHORITY,
                  pkg: CORE_PACKAGE_NAME,
                  name: CR_NAME,
                }}
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
