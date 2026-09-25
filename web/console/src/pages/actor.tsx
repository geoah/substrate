/** One actor (`/actors/:id`): who it is, in plain words, and everything it
 * changed, as History's sentences narrowed to it.
 *
 * Actors are records: the mirror row in `substrate.reamde.dev/core/actor`,
 * or, for a single-writer bundle whose actor IS its authority (record 60),
 * the `authority` mirror. A name in neither still has a real history; it
 * renders without a record, never as a dead end. Technical mode shows the
 * raw actor id, the record behind it and the full changelog table. */

import { useQuery } from "@tanstack/react-query"
import { parseAsStringLiteral, useQueryState } from "nuqs"

import { ChangelogPanel } from "@/components/changelog/changelog-panel"
import { HistoryFeed, LiveStatus } from "@/components/changelog/history-feed"
import { ActorMark } from "@/components/identity/actor-ref"
import { CopyButton } from "@/components/identity/copy-button"
import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { RecordRef } from "@/components/identity/record-ref"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useHistoryFeed } from "@/hooks/use-history-feed"
import { actorIdentity } from "@/lib/actor-identity"
import { actorMirrorsQueryOptions, resolveActor } from "@/lib/api/actors"
import { CORE_PACKAGE } from "@/lib/api/http"
import { actorRoute } from "@/router"
import { cn } from "@/lib/utils"

function ActorMeta({ actorId }: { actorId: string }) {
  const resolved = useQuery({
    ...actorMirrorsQueryOptions,
    select: (mirrors) => resolveActor(mirrors, actorId),
  })
  const identity = actorIdentity(actorId)
  const record = identity.record
  const mirror = resolved.data
  return (
    <>
      <span className="inline-flex items-center gap-1">
        <span className="font-mono text-[12px] [overflow-wrap:anywhere]">
          {actorId}
        </span>
        <CopyButton value={actorId} label="Copy the actor id" />
      </span>
      {record && <RecordRef kind={record.kind} id={record.id} />}
      {mirror && (
        <RecordRef
          kind={`${CORE_PACKAGE}/${mirror.collection}`}
          id={mirror.record.id}
        />
      )}
    </>
  )
}

export function ActorPage() {
  const { actorId } = actorRoute.useParams()
  const [technical] = useTechnicalDetails()
  const [layout, setLayout] = useQueryState(
    "layout",
    parseAsStringLiteral(["sentences", "table"] as const).withDefault(
      "sentences"
    )
  )
  const table = technical && layout === "table"
  const identity = actorIdentity(actorId)
  const feed = useHistoryFeed(
    { actors: [actorId] },
    { enabled: !table, values: true }
  )
  const title =
    identity.cls === "agent"
      ? `${identity.name} agent`
      : identity.via
        ? `${identity.name}, via ${identity.via}`
        : identity.name

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <DocPage className={cn(table && "pb-2 md:pb-3")}>
        <PageHeader
          glyph={
            <span className="inline-grid origin-top-left scale-[1.6] p-0.5">
              <ActorMark identity={identity} />
            </span>
          }
          title={title}
          description={identity.description}
          meta={technical ? <ActorMeta actorId={actorId} /> : undefined}
          actions={
            <div className="flex items-center gap-3">
              {!table && <LiveStatus status={feed.status} />}
              {technical && (
                <div
                  role="group"
                  aria-label="Layout"
                  className="inline-flex gap-0.5 rounded-[7px] border border-border-strong p-0.5"
                >
                  {(["sentences", "table"] as const).map((value) => (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={layout === value}
                      onClick={() =>
                        void setLayout(value === "sentences" ? null : value)
                      }
                      className={cn(
                        "cursor-pointer rounded-[5px] px-2.5 py-1 text-[12.5px] text-muted-foreground",
                        layout === value && "bg-foreground text-background"
                      )}
                    >
                      {value === "table" ? "Table view" : "Sentences"}
                    </button>
                  ))}
                </div>
              )}
            </div>
          }
        />
        {!table && (
          <>
            <h2 className="mt-8 mb-1 text-[15px] font-semibold tracking-[-0.01em]">
              What {identity.cls === "you" ? "you" : "it"} changed
            </h2>
            <HistoryFeed
              feed={feed}
              empty={`${identity.cls === "you" ? "You haven’t" : "It hasn’t"} changed anything yet.`}
            />
          </>
        )}
      </DocPage>
      {/* keyed: switching actors resets the tail and the facets cleanly */}
      {table && (
        <ChangelogPanel key={actorId} fixedActors={[actorId]} surface="actor" />
      )}
    </div>
  )
}
