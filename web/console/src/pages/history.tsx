/** History (`/history`): every change to the repository's data, newest
 * first, as sentences grouped by day, with the live tail on top. Four views
 * narrow it by who made the change: everything, you, your agents, your
 * providers; each is an actor filter the change feed applies server-side.
 * Technical mode adds sequence numbers and actor record ids, and a Table view
 * that is the full changelog table with its facet filters. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { parseAsBoolean, parseAsStringLiteral, useQueryState } from "nuqs"

import { ChangelogPanel } from "@/components/changelog/changelog-panel"
import {
  HistoryFeed,
  HistorySkeleton,
  LiveStatus,
  SystemChangesNote,
} from "@/components/changelog/history-feed"
import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { Button } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useEverydayChanges, useHistoryFeed } from "@/hooks/use-history-feed"
import { actorsQueryOptions } from "@/lib/api/actors"
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

/** The actor set each view filters on, from the repository's actor
 * records. Every view but everything waits for the whole set: while it loads
 * or after its read failed there is no set to filter on, and filtering on
 * none would read as nobody having changed anything. */
function useViewActors(view: HistoryView) {
  const records = useQuery(actorsQueryOptions)
  const byActor = view !== "everything"
  const actors = useMemo(
    () => (records.data ? viewActors(view, records.data) : undefined),
    [view, records.data]
  )
  return {
    actors,
    ready: !byActor || records.isSuccess,
    loading: byActor && records.isPending,
    failed: byActor && records.isError ? records : undefined,
  }
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
  const { actors, ready, loading, failed } = useViewActors(view)
  const nobody = ready && actors !== undefined && actors.length === 0
  const [showSystem, setShowSystem] = useQueryState(
    "system",
    parseAsBoolean.withDefault(false)
  )
  const everyday = useEverydayChanges(showSystem)
  const filter = useMemo(
    () => ({ ...everyday, ...(actors ? { actors } : {}) }),
    [everyday, actors]
  )
  const feed = useHistoryFeed(filter, {
    enabled: ready && everyday !== undefined && !nobody && !table,
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
              {!table && ready && !nobody && (
                <LiveStatus status={feed.status} />
              )}
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
                  shown={showSystem}
                  onToggle={() => void setShowSystem(showSystem ? null : true)}
                />
              )}
            </div>
            {loading ? (
              <HistorySkeleton />
            ) : failed ? (
              <div className="flex flex-col items-start gap-2 py-6 text-muted-foreground">
                <p>Who made each change didn’t load: {failed.error.message}</p>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void failed.refetch()}
                >
                  Try again
                </Button>
              </div>
            ) : nobody ? (
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
