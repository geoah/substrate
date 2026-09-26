/** What a provider did to your data: History's sentences narrowed to the
 * provider's own actors (its bundle and each of its functions), the newest
 * ten, with the way to the rest. Everyday mode leaves the machinery out, as
 * History does. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import {
  HistorySentences,
  HistorySkeleton,
} from "@/components/changelog/history-feed"
import { SectionHead } from "@/components/identity/section-head"
import { Button } from "@/components/ui/button"
import { useEverydayChanges, useHistoryFeed } from "@/hooks/use-history-feed"
import { functionsQueryOptions } from "@/lib/api/functions"
import { providerActors } from "@/lib/providers"

/** Entries the section shows. */
const SHOWN = 10

/** The provider's actors, once the function records are read. */
function useProviderActors(bundleId: string): string[] | undefined {
  const functions = useQuery(functionsQueryOptions)
  return useMemo(
    () =>
      functions.data
        ? providerActors(
            bundleId,
            functions.data.map((f) => f.id)
          )
        : undefined,
    [functions.data, bundleId]
  )
}

export function RecentActivity({
  bundleId,
  name,
}: {
  bundleId: string
  /** The provider's everyday name ("Google"). */
  name: string
}) {
  const actors = useProviderActors(bundleId)
  const filter = useMemo(() => ({ actors: actors ?? [] }), [actors])
  const keep = useEverydayChanges()
  const feed = useHistoryFeed(filter, {
    enabled: Boolean(actors),
    first: 200,
    keep,
    fill: SHOWN,
  })
  return (
    <section aria-labelledby="activity">
      <SectionHead
        id="activity"
        title="Recent activity"
        hint={`What ${name} changed in your data`}
        actions={
          <Button
            variant="ghost"
            size="sm"
            nativeButton={false}
            render={<Link to="/history" search={{ view: "providers" }} />}
          >
            See all in History
          </Button>
        }
      />
      {!actors || feed.isPending ? (
        <HistorySkeleton rows={4} />
      ) : feed.error ? (
        <p className="text-[13px] text-muted-foreground">
          Its activity didn’t load: {feed.error.message}{" "}
          <button
            type="button"
            className="cursor-pointer underline"
            onClick={feed.retry}
          >
            Try again
          </button>
        </p>
      ) : feed.rows.length ? (
        <HistorySentences rows={feed.rows} limit={SHOWN} more={feed.hasOlder} />
      ) : (
        <p className="text-[13px] text-muted-foreground">
          {feed.hidden
            ? "Only system changes lately."
            : `${name} hasn’t changed anything yet.`}
        </p>
      )}
    </section>
  )
}
