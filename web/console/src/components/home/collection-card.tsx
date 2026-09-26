/** A collection as a card: its glyph and display plural, how many records
 * it holds, and where they come from. The whole card opens the collection. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath } from "@/components/identity/kind-ref"
import { OriginMark } from "@/components/identity/origin-mark"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { splitKind } from "@/lib/api/http"
import { formatCount, recordCountQueryOptions } from "@/lib/api/records"
import type { KindInfo } from "@/lib/api/types"
import { displayPlural } from "@/lib/kind-names"
import { originOfKind } from "@/lib/origin"

export function CollectionCard({ kind }: { kind: KindInfo }) {
  const [technical] = useTechnicalDetails()
  const { authority, pkg, name } = splitKind(kind.identity)
  const count = useQuery(recordCountQueryOptions(authority, pkg, name))
  return (
    <Link
      to="/data/$authority/$pkg/$name"
      params={{ authority, pkg, name }}
      className="flex min-w-0 flex-col gap-2.5 rounded-[10px] border border-border bg-background p-3.5 text-foreground no-underline transition-colors hover:border-border-strong hover:bg-panel"
    >
      <span className="flex min-w-0 items-center gap-2 font-semibold">
        <KindGlyph kind={kind} size="md" />
        <span className="truncate">{displayPlural(kind)}</span>
      </span>
      {technical && (
        <KindPath reference={kind.identity} className="text-[11.5px]" />
      )}
      <span className="flex min-w-0 items-center justify-between gap-2">
        {count.data ? (
          <span className="text-[22px] font-semibold tracking-[-0.02em] tabular-nums">
            {formatCount(count.data)}
          </span>
        ) : count.isError ? (
          <span className="text-[22px] font-semibold text-faint">—</span>
        ) : (
          <Skeleton className="h-7 w-10" />
        )}
        <OriginMark
          origin={originOfKind(kind.identity)}
          className="text-[12.5px] text-faint"
        />
      </span>
    </Link>
  )
}
