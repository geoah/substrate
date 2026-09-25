/** All data (`/data`): every collection in the substrate, grouped the way the
 * sidebar groups them. "Your data" is yours to change; each "From
 * <Provider>" group holds copies that provider keeps up to date. Everyday
 * mode lists the primary collections and says how many supporting ones sit
 * behind them; technical mode lists every kind with its reference, the
 * substrate's own included. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { PlusIcon } from "lucide-react"
import { parseAsStringLiteral, useQueryState } from "nuqs"

import {
  AddCollectionDialog,
  ADD_WAYS,
} from "@/components/all-data/add-collection-dialog"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath } from "@/components/identity/kind-ref"
import { TablePage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  useDensity,
  useTechnicalDetails,
} from "@/hooks/use-console-preferences"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { formatCount, recordCountQueryOptions } from "@/lib/api/records"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import type { KindInfo } from "@/lib/api/types"
import {
  collectionGroups,
  hiddenExamples,
  type CollectionGroup,
} from "@/lib/collections"
import { kindPurpose } from "@/lib/definition"
import { displayPlural } from "@/lib/kind-names"
import { cn } from "@/lib/utils"
import { kindDescription } from "@/lib/kind-copy"

const HINT: Record<CollectionGroup["type"], (g: CollectionGroup) => string> = {
  yours: () => "yours to change",
  provider: (g) =>
    `copies kept up to date by ${g.provider?.name ?? "a provider"}`,
  system: () => "the substrate’s own machinery",
}

function RecordCount({ kind }: { kind: KindInfo }) {
  const { authority, pkg, name } = splitKind(kind.identity)
  const count = useQuery(recordCountQueryOptions(authority, pkg, name))
  if (count.data) return <>{formatCount(count.data)}</>
  if (count.isError) return <span className="text-faint">—</span>
  return <Skeleton className="ml-auto h-3.5 w-8" />
}

function CollectionRow({
  kind,
  technical,
  compact,
}: {
  kind: KindInfo
  technical: boolean
  compact: boolean
}) {
  const navigate = useNavigate()
  const { authority, pkg, name } = splitKind(kind.identity)
  const purpose = kindPurpose(kind)
  const cell = cn(
    "border-b border-border px-2.5 align-middle",
    compact ? "h-[30px]" : "h-[38px]"
  )
  return (
    <tr
      className="cursor-pointer hover:bg-hover"
      onClick={() =>
        void navigate({
          to: "/data/$authority/$pkg/$name",
          params: { authority, pkg, name },
        })
      }
    >
      <td className={cell}>
        <Link
          to="/data/$authority/$pkg/$name"
          params={{ authority, pkg, name }}
          onClick={(e) => e.stopPropagation()}
          className="flex min-w-0 items-center gap-2 font-medium text-foreground no-underline hover:underline"
        >
          <KindGlyph kind={kind} size="sm" />
          <span className="truncate">{displayPlural(kind)}</span>
          {technical && purpose !== "primary" && (
            <span className="shrink-0 rounded-[3px] border border-border-strong px-1 text-[10px] leading-4 font-normal text-faint">
              {purpose}
            </span>
          )}
        </Link>
      </td>
      {technical && (
        <td className={cn(cell, "border-l")}>
          <KindPath reference={kind.identity} className="text-[12px]" />
        </td>
      )}
      <td className={cn(cell, "border-l text-muted-foreground")}>
        <span className="line-clamp-1">
          {kindDescription(kind, technical) || "—"}
        </span>
      </td>
      <td className={cn(cell, "border-l text-right tabular-nums")}>
        <RecordCount kind={kind} />
      </td>
    </tr>
  )
}

function GroupTable({
  group,
  technical,
  compact,
}: {
  group: CollectionGroup
  technical: boolean
  compact: boolean
}) {
  const shown = technical ? [...group.primary, ...group.hidden] : group.primary
  const hidden = technical ? 0 : group.hidden.length
  return (
    <section className="mt-8">
      <div className="mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <h2 className="flex items-center gap-2 text-[15px] font-semibold tracking-[-0.01em]">
          {group.provider && (
            <ProviderBadge provider={group.provider} size="sm" />
          )}
          {group.label}
        </h2>
        <span className="text-[12.5px] text-faint">
          {HINT[group.type](group)}
        </span>
      </div>
      <div className="overflow-x-auto rounded-[10px] border border-border">
        <table className="w-full min-w-[640px] table-fixed border-separate border-spacing-0 text-sm">
          <colgroup>
            <col className="w-[240px]" />
            {technical && <col className="w-[400px]" />}
            <col />
            <col className="w-[90px]" />
          </colgroup>
          <thead>
            <tr className="text-left text-[12.5px] text-faint">
              <th className="h-[34px] border-b border-border px-2.5 font-medium">
                Collection
              </th>
              {technical && (
                <th className="border-b border-l border-border px-2.5 font-medium">
                  Reference
                </th>
              )}
              <th className="border-b border-l border-border px-2.5 font-medium">
                What it holds
              </th>
              <th className="border-b border-l border-border px-2.5 text-right font-medium">
                Records
              </th>
            </tr>
          </thead>
          <tbody className="[&>tr:last-child>td]:border-b-0">
            {shown.map((k) => (
              <CollectionRow
                key={k.identity}
                kind={k}
                technical={technical}
                compact={compact}
              />
            ))}
          </tbody>
        </table>
      </div>
      {hidden > 0 && (
        <p className="mx-0.5 mt-2 text-[12.5px] text-faint">
          {hidden} more {hidden === 1 ? "holds" : "hold"} supporting details
          (like {hiddenExamples(group)}). You see them from the records they
          belong to, or here with Technical details on.
        </p>
      )}
    </section>
  )
}

export function AllDataPage() {
  const [technical] = useTechnicalDetails()
  const [density] = useDensity()
  // In the URL, so "Add a collection" is a link another page can hand over.
  const [adding, setAdding] = useQueryState(
    "add",
    parseAsStringLiteral(ADD_WAYS)
  )
  const registry = useQuery(kindsQueryOptions)
  const repository = useQuery(repositoryQueryOptions)
  const groups = useMemo(
    () =>
      collectionGroups(
        registry.data ?? [],
        repository.data?.authority ?? getRepository() ?? ""
      ).filter((g) =>
        technical ? true : g.type !== "system" && g.primary.length > 0
      ),
    [registry.data, repository.data, technical]
  )

  return (
    <TablePage className="pb-16">
      <PageHeader
        title="All data"
        description="Every collection in your substrate. Some are yours to change; some are kept up to date by a provider."
        actions={
          <Button onClick={() => void setAdding("agent")}>
            <PlusIcon />
            Add a collection
          </Button>
        }
      />
      {registry.isPending ? (
        <div className="mt-8 flex flex-col gap-2">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : registry.isError ? (
        <div className="mt-8 flex flex-col items-start gap-2 text-muted-foreground">
          <p>Your collections didn’t load: {registry.error.message}</p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void registry.refetch()}
          >
            Try again
          </Button>
        </div>
      ) : groups.length === 0 ? (
        <p className="mt-8 text-muted-foreground">
          Nothing here yet. Add a collection to start keeping something.
        </p>
      ) : (
        groups.map((g) => (
          <GroupTable
            key={g.id}
            group={g}
            technical={technical}
            compact={density === "compact"}
          />
        ))
      )}
      <AddCollectionDialog
        way={adding}
        onWayChange={(way) => void setAdding(way)}
      />
    </TablePage>
  )
}
