/** History (`/history`): every change to the repository's data, newest
 * first, as sentences grouped by day, with the live tail on top. Four views
 * narrow it by who made the change: everything, you, your agents, your
 * providers; each is an actor filter the change feed applies server-side.
 * Technical mode adds sequence numbers and raw actor ids, and a Table view
 * that is the full changelog table with its facet filters. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { parseAsBoolean, parseAsStringLiteral, useQueryState } from "nuqs"

import { ChangelogPanel } from "@/components/changelog/changelog-panel"
import {
  HistoryFeed,
  LiveStatus,
  SystemChangesNote,
} from "@/components/changelog/history-feed"
import { functionsQueryOptions } from "@/components/home/overview-cards"
import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { Segmented } from "@/components/ui/segmented"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useEverydayChanges, useHistoryFeed } from "@/hooks/use-history-feed"
import { actorMirrorsQueryOptions, actorNames } from "@/lib/api/actors"
import { agentsQueryOptions } from "@/lib/api/agents"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import {
  HISTORY_LAYOUTS,
  HISTORY_VIEWS,
  viewActors,
  type HistoryView,
} from "@/lib/history"
import { cn } from "@/lib/utils"

const VIEW_VALUES = HISTORY_VIEWS.map((v) => v.value)

const EMPTY: Record<HistoryView, string> = {
  everything: "Nothing has changed yet. Changes show up here as they happen.",
  you: "You haven’t changed anything yet.",
  agents: "None of your agents has changed anything yet.",
  providers: "None of your providers has changed anything yet.",
}

/** The actor set each view filters on, from the reads that name them. */
function useViewActors(view: HistoryView): {
  actors: string[] | undefined
  ready: boolean
} {
  const mirrors = useQuery({ ...actorMirrorsQueryOptions, select: actorNames })
  const agents = useQuery({
    ...agentsQueryOptions(),
    enabled: view === "agents",
  })
  const functions = useQuery({
    ...functionsQueryOptions,
    enabled: view === "providers",
  })
  const bundles = useQuery({
    ...bundleStatusesQueryOptions,
    enabled: view === "providers",
  })
  const ready =
    view === "everything" ||
    (view === "you" && !mirrors.isPending) ||
    (view === "agents" && !agents.isPending) ||
    (view === "providers" && !functions.isPending && !bundles.isPending)
  const actors = useMemo(
    () =>
      viewActors(view, {
        actors: mirrors.data ?? [],
        agents: (agents.data?.records ?? []).map((r) => r.id),
        functions: (functions.data?.records ?? []).map((r) => r.id),
        bundles: (bundles.data ?? []).map((b) => b.id),
      }),
    [view, mirrors.data, agents.data, functions.data, bundles.data]
  )
  return { actors, ready }
}

export function HistoryPage() {
  const [technical] = useTechnicalDetails()
  const [view, setView] = useQueryState(
    "view",
    parseAsStringLiteral(VIEW_VALUES).withDefault("everything")
  )
  const [layout, setLayout] = useQueryState(
    "layout",
    parseAsStringLiteral(["sentences", "table"] as const).withDefault(
      "sentences"
    )
  )
  const table = technical && layout === "table"
  const { actors, ready } = useViewActors(view)
  const filter = useMemo(() => (actors ? { actors } : {}), [actors])
  const nobody = actors !== undefined && actors.length === 0
  const [showSystem, setShowSystem] = useQueryState(
    "system",
    parseAsBoolean.withDefault(false)
  )
  const keep = useEverydayChanges(showSystem)
  const feed = useHistoryFeed(filter, {
    enabled: ready && !nobody && !table,
    keep,
    fill: 30,
    values: true,
  })

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <DocPage className={cn(table && "pb-2 md:pb-3")}>
        <PageHeader
          title="History"
          description="Every change to your data, newest first: by you, your agents and your providers. Nothing is ever lost from here."
          actions={
            <div className="flex items-center gap-3">
              {!table && !nobody && <LiveStatus status={feed.status} />}
              {technical && (
                <Segmented
                  label="Layout"
                  value={layout}
                  options={HISTORY_LAYOUTS}
                  onChange={(value) =>
                    void setLayout(value === "sentences" ? null : value)
                  }
                />
              )}
            </div>
          }
        />
        {!table && (
          <>
            <div className="mt-[18px] flex flex-wrap gap-0.5 border-b border-border pb-2.5">
              <Segmented
                look="plain"
                label="Whose changes"
                value={view}
                options={HISTORY_VIEWS}
                onChange={(value) =>
                  void setView(value === "everything" ? null : value)
                }
              />
              {!technical && (
                <SystemChangesNote
                  className="ml-auto self-center"
                  hidden={feed.hidden}
                  shown={showSystem}
                  onToggle={() => void setShowSystem(showSystem ? null : true)}
                />
              )}
            </div>
            {nobody ? (
              <p className="py-8 text-muted-foreground">{EMPTY[view]}</p>
            ) : (
              <HistoryFeed feed={feed} empty={EMPTY[view]} />
            )}
          </>
        )}
      </DocPage>
      {table && <ChangelogPanel />}
    </div>
  )
}
