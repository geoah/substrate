/** Home (`/`): what the substrate holds and what just happened. The four
 * things the console is for (data, providers, agents, tools) as cards, the
 * main collections with their sizes, the latest chats with your agents, and
 * the latest changes to your data in History's sentences. No inbox: nothing
 * here asks the reader to act. */

import { useEffect, useMemo, useRef, type ReactNode } from "react"
import { useQueries, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import {
  HistorySentences,
  HistorySkeleton,
} from "@/components/changelog/history-feed"
import { CollectionCard } from "@/components/home/collection-card"
import { OverviewCards } from "@/components/home/overview-cards"
import { RecentChats } from "@/components/home/recent-chats"
import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { SectionHead } from "@/components/identity/section-head"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useEverydayChanges, useHistoryFeed } from "@/hooks/use-history-feed"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordCountQueryOptions } from "@/lib/api/records"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import { collectionGroups } from "@/lib/collections"
import { historyEntries } from "@/lib/history"
import { homeSections } from "@/lib/home-summary"

/** The collections Home shows before "All data" takes over: yours first,
 * then what providers bring in, each group under its own heading. */
const SHOWN_COLLECTIONS = 9
const SHOWN_YOURS = 6
/** The newest changes Home shows, as folded sentences, and the rows read to
 * fold them from. */
const SHOWN_CHANGES = 6
const RECENT_ROWS = 60
/** The further pages Home reads to close the oldest run it shows. */
const CLOSE_PAGES = 4

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
    <section>
      <SectionHead title={title} actions={action} />
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
  const heldBy = new Map(
    yourKinds.map((k, i) => [k.identity, (counts[i]?.data?.value ?? 0) > 0])
  )
  const sections = homeSections(
    groups ?? [],
    (k) => heldBy.get(k.identity) ?? false,
    SHOWN_COLLECTIONS,
    SHOWN_YOURS
  )
  const keep = useEverydayChanges()
  const recent = useHistoryFeed(
    {},
    { first: RECENT_ROWS, keep, fill: SHOWN_CHANGES * 3 }
  )
  // The oldest sentence loaded may go on in older rows. While it is among
  // the ones shown, read on so its count is whole; past the budget it is
  // said without one.
  const [technical] = useTechnicalDetails()
  const shownEntries = useMemo(
    () => historyEntries(recent.rows, technical).length,
    [recent.rows, technical]
  )
  const closing = useRef(CLOSE_PAGES)
  const { hasOlder, loadingOlder, isPending, fetchOlder } = recent
  const open = hasOlder && shownEntries <= SHOWN_CHANGES
  useEffect(() => {
    if (!open || loadingOlder || isPending || closing.current <= 0) return
    closing.current -= 1
    fetchOlder()
  }, [open, loadingOlder, isPending, fetchOlder])

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
        ) : sections.length === 0 ? (
          <p className="text-muted-foreground">
            Nothing here yet. Add a collection from{" "}
            <Link to="/data">All data</Link>, or a provider from{" "}
            <Link to="/providers">Providers</Link>.
          </p>
        ) : (
          <div className="flex flex-col gap-4">
            {sections.map((section) => (
              <section key={section.id} aria-label={section.label}>
                {sections.length > 1 && (
                  <h3 className="mb-2 flex items-center gap-1.5 text-[12.5px] font-semibold text-muted-foreground">
                    {section.provider && (
                      <ProviderBadge provider={section.provider} size="xs" />
                    )}
                    {section.label}
                  </h3>
                )}
                <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2.5">
                  {section.kinds.map((k) => (
                    <CollectionCard key={k.identity} kind={k} />
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </Section>

      <Section
        title="Recent chats"
        action={
          <Button
            variant="ghost"
            size="sm"
            render={<Link to="/agents" />}
            nativeButton={false}
          >
            Open Agents
          </Button>
        }
      >
        <RecentChats />
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
        ) : recent.rows.length === 0 ? (
          <p className="text-muted-foreground">
            {recent.hidden
              ? "None of your data has changed lately."
              : "Nothing has changed yet. Changes show up here as they happen."}
          </p>
        ) : (
          <HistorySentences
            rows={recent.rows}
            limit={SHOWN_CHANGES}
            more={recent.hasOlder}
          />
        )}
      </Section>
    </DocPage>
  )
}
