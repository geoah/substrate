/** The notice on a record an adapter wrote (`adapterOutput`): who wrote it
 * and from what, that its values are read only here, and how the owner
 * corrects one, with links to write their own record and to the sources
 * whose priorities rank it. Nothing here names a provider or a sample: the
 * writer comes from the record's own managers and `origin`. */

import { Link } from "@tanstack/react-router"

import { IdText } from "@/components/identity/id-text"
import { splitKind } from "@/lib/api/http"
import type { KindInfo } from "@/lib/api/types"
import { declaredProperties } from "@/lib/definition"
import { displayName, displayPlural, lowerFirst } from "@/lib/kind-names"
import type { AdapterOutput } from "@/lib/provenance"
import { splitRecordPath } from "@/lib/record-path"

export function AdapterNotice({
  adapter,
  recordKind,
  kind,
}: {
  adapter: AdapterOutput
  recordKind: string
  kind?: KindInfo
}) {
  const noun = lowerFirst(displayName(kind ?? recordKind))
  const own = splitKind(recordKind)
  // The kind a record's `source` points at, where the kind pins one: its
  // records hold the priorities a correction is ranked by.
  const sourceKind = kind
    ? declaredProperties(kind).find(
        (p) => p.name === "source" && p.kind === "reference" && p.to
      )?.to
    : undefined
  const sources =
    sourceKind && sourceKind !== "any" ? splitKind(sourceKind) : undefined
  const origin = splitRecordPath(adapter.origin)
  return (
    <div
      data-slot="adapter-output"
      className="mt-4 flex flex-col gap-1.5 rounded-lg border bg-panel px-3 py-2.5 text-[13px] text-muted-foreground"
    >
      <p>
        Written by {adapter.writer} from{" "}
        {origin ? (
          <Link
            to="/data/$authority/$pkg/$name/$id"
            params={{ ...splitKind(origin.kind), id: origin.id }}
            className="text-foreground underline-offset-2 hover:underline"
          >
            <IdText value={origin.id} copy={false} />
          </Link>
        ) : (
          adapter.origin
        )}
        . Its values are read only here, and the adapter rewrites it when the
        provider’s data changes.
      </p>
      <p>
        To correct it, write your own {noun} from your own source, and rank that
        source above this one for the metric in its priorities: a read takes the
        first ranked source where two overlap.
      </p>
      <p className="flex flex-wrap gap-x-3">
        <Link
          to="/data/$authority/$pkg/$name/new"
          params={own}
          className="text-foreground underline-offset-2 hover:underline"
        >
          New {noun}
        </Link>
        {sources && (
          <Link
            to="/data/$authority/$pkg/$name"
            params={sources}
            className="text-foreground underline-offset-2 hover:underline"
          >
            {displayPlural(sourceKind!)} and their priorities
          </Link>
        )}
      </p>
    </div>
  )
}
