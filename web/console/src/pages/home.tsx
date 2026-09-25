/** Home (`/`): what the substrate holds and what just happened. The four
 * things the console is for (data, providers, agents, tools) as cards, the
 * main collections with their sizes, and the latest changes in History's
 * sentences. No inbox: nothing here asks the reader to act. */

import { useMemo, type ReactNode } from "react"
import { useQueries, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import {
  HistorySentences,
  HistorySkeleton,
  SystemChangesNote,
} from "@/components/changelog/history-feed"
import { CollectionCard } from "@/components/home/collection-card"
import { OverviewCards } from "@/components/home/overview-cards"
import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useEverydayChanges, useHistoryFeed } from "@/hooks/use-history-feed"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordCountQueryOptions } from "@/lib/api/records"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import { collectionGroups } from "@/lib/collections"

/** The collections Home shows before "All data" takes over: yours first,
 * then what providers bring in. */
const SHOWN_COLLECTIONS = 9
const SHOWN_YOURS = 6
/** The newest changes Home shows, as folded sentences, and the rows read to
 * fold them from. */
const SHOWN_CHANGES = 6
const RECENT_ROWS = 60

function Section({
  title,
  action,
  children,
}: {
  title: string
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="mt-8">
      <div className="mb-2.5 flex items-center gap-2">
        <h2 className="text-[15px] font-semibold tracking-[-0.01em]">
          {title}
        </h2>
        {action && <div className="ml-auto">{action}</div>}
      </div>
      {children}
    </section>
  )
}

export function HomePage() {
  const registry = useQuery(kindsQueryOptions)
  const repository = useQuery(repositoryQueryOptions)
  const groups = useMemo(
    () =>
      registry.data
        ? collectionGroups(
            registry.data,
            repository.data?.authority ?? getRepository() ?? ""
          )
        : undefined,
    [registry.data, repository.data]
  )
  const yourKinds = (groups ?? [])
    .filter((g) => g.type === "yours")
    .flatMap((g) => g.primary)
  // The collections that hold something lead; the counts are the ones the
  // cards read, so this costs no extra request.
  const counts = useQueries({
    queries: yourKinds.map((k) =>
      recordCountQueryOptions(k.authority, k.package, k.name)
    ),
  })
  const held = (i: number) => ((counts[i]?.data?.value ?? 0) > 0 ? 0 : 1)
  const yours = yourKinds
    .map((k, i) => ({ k, i }))
    .sort((a, b) => held(a.i) - held(b.i) || a.i - b.i)
    .map(({ k }) => k)
  const provided = (groups ?? [])
    .filter((g) => g.type === "provider")
    .flatMap((g) => g.primary)
  const shownYours = yours.slice(
    0,
    Math.max(SHOWN_YOURS, SHOWN_COLLECTIONS - provided.length)
  )
  const collections = [...shownYours, ...provided].slice(0, SHOWN_COLLECTIONS)
  const keep = useEverydayChanges()
  const recent = useHistoryFeed(
    {},
    { first: RECENT_ROWS, keep, fill: SHOWN_CHANGES * 3 }
  )

  return (
    <DocPage>
      <PageHeader
        title="Your substrate"
        description="Everything you keep here, the services that fill it, the agents that work on it, and the tools they use."
      />
      <div className="mt-[22px]">
        <OverviewCards groups={groups} />
      </div>

      <Section
        title="Collections"
        action={
          <Button
            variant="ghost"
            size="sm"
            render={<Link to="/data" />}
            nativeButton={false}
          >
            All data
          </Button>
        }
      >
        {registry.isPending ? (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2.5">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-[98px] rounded-[10px]" />
            ))}
          </div>
        ) : registry.isError ? (
          <p className="text-muted-foreground">
            Your collections didn’t load: {registry.error.message}{" "}
            <button
              type="button"
              className="cursor-pointer underline"
              onClick={() => void registry.refetch()}
            >
              Try again
            </button>
          </p>
        ) : collections.length === 0 ? (
          <p className="text-muted-foreground">
            Nothing here yet. Add a collection from{" "}
            <Link to="/data">All data</Link>, or a provider from{" "}
            <Link to="/providers">Providers</Link>.
          </p>
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2.5">
            {collections.slice(0, SHOWN_COLLECTIONS).map((k) => (
              <CollectionCard key={k.identity} kind={k} />
            ))}
          </div>
        )}
      </Section>

      <Section
        title="Recent changes"
        action={
          <Button
            variant="ghost"
            size="sm"
            render={<Link to="/history" />}
            nativeButton={false}
          >
            See all
          </Button>
        }
      >
        {recent.isPending ? (
          <HistorySkeleton rows={4} />
        ) : recent.error ? (
          <p className="text-muted-foreground">
            Recent changes didn’t load: {recent.error.message}
          </p>
        ) : recent.rows.length === 0 && !recent.hidden ? (
          <p className="text-muted-foreground">
            Nothing has changed yet. Changes show up here as they happen.
          </p>
        ) : (
          <>
            {recent.rows.length > 0 ? (
              <HistorySentences
                rows={recent.rows}
                limit={SHOWN_CHANGES}
                more={recent.hasOlder}
              />
            ) : (
              <p className="text-muted-foreground">
                Only system changes lately.
              </p>
            )}
            <SystemChangesNote
              className="pt-2.5"
              hidden={recent.hidden}
              action={
                <Link
                  to="/history"
                  search={{ system: true }}
                  className="text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
                >
                  Show in History
                </Link>
              }
            />
          </>
        )}
      </Section>
    </DocPage>
  )
}
