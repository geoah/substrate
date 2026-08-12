/** The changelog surface shared by /changelog, the actor view and the connector
 * timeline: facet toolbar (the shared filter-control anatomy over the
 * changelog's vocabulary — kind, authority, actor, op, since/until, q), the
 * Follow toggle with its connection state, and the FLAT change table (owner
 * ruling 2026-08-06 — no intent grouping). Facets live in the URL (nuqs) — a
 * filtered changelog is a shareable view. The actor view fixes one actor, the
 * connector detail fixes the connector's whole actor set; either way the
 * actor facet leaves the toolbar and the actor column hides by default. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { RadioIcon } from "lucide-react"
import { parseAsArrayOf, parseAsString, useQueryState } from "nuqs"

import { DataTableFilters } from "@/components/data-table/data-table-filters"
import { ChangelogTable } from "@/components/changelog/changelog-table"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { actorMirrorsQueryOptions, actorNames } from "@/lib/api/actors"
import type { WatchStatus } from "@/lib/api/changes"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { decodeFilters, encodeFilter } from "@/lib/filters"
import { changelogFacetFields, toChangelogQuery } from "@/lib/changelog"
import { cn } from "@/lib/utils"

function FollowToggle({
  follow,
  status,
  disabled,
  onToggle,
}: {
  follow: boolean
  status: WatchStatus
  disabled: boolean
  onToggle: () => void
}) {
  const label: Record<WatchStatus, string> = {
    live: "live",
    retrying: "reconnecting…",
    connecting: "connecting…",
    // The tail fell below retention; the table is re-listing from a fresh head.
    compacted: "recovering…",
    // A terminal error frame ended the tail — it will not silently reconnect.
    stopped: "stopped",
    off: "connecting…",
  }
  return (
    <div className="flex items-center gap-2">
      {follow && (
        <span className="data text-xs text-muted-foreground">
          {label[status]}
        </span>
      )}
      <Button
        variant={follow ? "default" : "outline"}
        size="sm"
        className="h-8 gap-1.5"
        disabled={disabled}
        title={
          disabled
            ? "Follow reads from the head — clear the until filter first"
            : "Tail the changelog live"
        }
        onClick={onToggle}
      >
        <span
          className={cn(
            "size-1.5 rounded-full",
            !follow && "bg-muted-foreground/50",
            follow && status === "live" && "bg-primary-foreground",
            follow &&
              status !== "live" &&
              "animate-pulse bg-primary-foreground/60"
          )}
        />
        <RadioIcon className="size-3.5" />
        Follow
      </Button>
    </div>
  )
}

export function ChangelogPanel({
  fixedActors,
  surface = "changelog",
}: {
  fixedActors?: string[]
  /** The column-prefs surface key — /changelog, the actor view and the
   * connector timeline each remember their own columns. */
  surface?: string
}) {
  const fixed = fixedActors?.length ? fixedActors : undefined
  const [filterTokens, setFilterTokens] = useQueryState(
    "filter",
    parseAsArrayOf(parseAsString).withDefault([])
  )
  const [follow, setFollow] = useState(false)
  const [status, setStatus] = useState<WatchStatus>("off")

  const registry = useQuery(kindsQueryOptions)
  const mirrors = useQuery({
    ...actorMirrorsQueryOptions,
    select: actorNames,
  })

  const filters = useMemo(() => decodeFilters(filterTokens), [filterTokens])
  const fields = useMemo(
    () =>
      changelogFacetFields({
        kinds: registry.data ?? [],
        actors: mirrors.data ?? [],
        fixedActor: Boolean(fixed),
      }),
    [registry.data, mirrors.data, fixed]
  )
  const query = useMemo(
    () => toChangelogQuery(filters, registry.data ?? []),
    [filters, registry.data]
  )
  const filter = useMemo(
    () => (fixed ? { ...query.filter, actors: fixed } : query.filter),
    [query.filter, fixed]
  )

  // Following a bounded past is a contradiction; the toggle waits.
  const bounded = query.untilMs !== undefined

  // A fixed-actor view repeats the actor in every row — the column stays a
  // dropdown toggle away, not a default.
  const defaultHidden = useMemo(
    () => (fixed ? ["authority", "actor"] : ["authority"]),
    [fixed]
  )

  // The authority facet expands through the registry — filtering before it
  // loads would briefly ask the wire for the wrong feed.
  if (registry.isPending) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="flex shrink-0 items-center gap-2 px-6 py-2.5">
          <Skeleton className="h-8 w-24" />
          <Skeleton className="ml-auto h-8 w-24" />
        </div>
        <div className="flex-1 px-6">
          <Skeleton className="mt-2 h-4 w-2/3" />
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ChangelogTable
        filter={filter}
        sinceMs={query.sinceMs}
        untilMs={query.untilMs}
        follow={follow && !bounded}
        kinds={registry.data ?? []}
        onStatus={setStatus}
        surface={surface}
        defaultHidden={defaultHidden}
        toolbarLeft={
          <DataTableFilters
            fields={fields}
            filters={filters}
            onChange={(next) =>
              void setFilterTokens(next.length ? next.map(encodeFilter) : null)
            }
          />
        }
        toolbarRight={
          <FollowToggle
            follow={follow && !bounded}
            status={status}
            disabled={bounded}
            onToggle={() => setFollow((v) => !v)}
          />
        }
      />
    </div>
  )
}
