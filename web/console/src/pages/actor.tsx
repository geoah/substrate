/** Actor view (`/actors/:id`): "what has this actor done?" — the actor's
 * identity card over its changelog, which is the same changelog list
 * pre-filtered to one actor (one spine, two views).
 *
 * Actors are records: the mirror row in `core.substrate.reamde.dev/actors`, or — for a
 * single-writer connector whose actor IS its authority (record 60) — the
 * `authorities` mirror. A name in neither collection still has a real
 * changelog; it renders an unregistered stub, never a dead end. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon } from "lucide-react"

import { ChangelogPanel } from "@/components/changelog/changelog-panel"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { actorMirrorsQueryOptions, resolveActor } from "@/lib/api/actors"
import { actorRoute } from "@/router"

function IdentityCard({ actorId }: { actorId: string }) {
  const resolved = useQuery({
    ...actorMirrorsQueryOptions,
    select: (mirrors) => resolveActor(mirrors, actorId),
  })

  if (resolved.isPending) {
    return (
      <div className="flex flex-col gap-1.5">
        <Skeleton className="h-5 w-56" />
        <Skeleton className="h-3.5 w-40" />
      </div>
    )
  }

  const record = resolved.data?.record
  const collection = resolved.data?.collection
  const source =
    typeof record?.properties.source === "string"
      ? record.properties.source
      : undefined
  const authority =
    typeof record?.properties.authority === "string"
      ? record.properties.authority
      : undefined

  // No avatar/initials bubble — actors render name-only everywhere (owner
  // redline 2026-08-06 on ActorChip; the identity card follows the same voice).
  return (
    <div className="flex min-w-0 items-center gap-3">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <h1 className="truncate data text-lg font-semibold">{actorId}</h1>
          {source && (
            <Badge variant="outline" className="font-normal">
              {source}
            </Badge>
          )}
          {collection === "authority" && (
            <Badge variant="outline" className="font-normal">
              writes as its authority
            </Badge>
          )}
        </div>
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          {record ? (
            <>
              {authority && <span className="data">{authority}</span>}
              <Link
                to="/data/$authority/$collection/$id"
                params={{
                  authority: "core.substrate.reamde.dev",
                  collection: collection!,
                  id: actorId,
                }}
                className="inline-flex items-center gap-0.5 underline-offset-4 hover:underline"
              >
                View record
                <ArrowUpRightIcon className="size-3" />
              </Link>
            </>
          ) : resolved.isError ? (
            <span>{resolved.error.message}</span>
          ) : (
            <span>
              Not in the actor registry — the changelog below is still its full
              record.
            </span>
          )}
        </p>
      </div>
    </div>
  )
}

export function ActorPage() {
  const { actorId } = actorRoute.useParams()
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-2">
        <IdentityCard actorId={actorId} />
      </div>
      {/* keyed: switching actors resets follow state and facets cleanly */}
      <ChangelogPanel key={actorId} fixedActors={[actorId]} surface="actor" />
    </div>
  )
}
