/** Resolve what a record's pointers are CALLED, for the surface that cannot
 * ask the list to do it.
 *
 * A collection page expands its references on the read it already makes
 * (`expand=` → `included`). A single record cannot: a `GET` does not expand
 * (docs/api.md), so the Properties tab prints an id where a name belongs
 * unless something goes and asks. This is that something — ONE batched list
 * read over the paths the record holds, narrowed by `filter.ids` inside the
 * kinds they name.
 *
 * The cache is the query cache: the read is keyed by the scope it asks for
 * and held stale-free for a minute, so switching tabs on a record, coming
 * back to it, or re-rendering for any other reason is free, and a record
 * whose pointers have not changed does not re-read them. Titles are display
 * text — a stale one is cosmetic — which is why a minute is enough and
 * nothing invalidates it. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { referenceTitlesQueryOptions } from "@/lib/api/records"
import type { KindInfo } from "@/lib/api/types"
import {
  titleReadScope,
  titlesFromRecords,
  type ReferenceTitles,
} from "@/lib/reference-titles"

/** The titles for a set of record paths.
 *
 * `kinds` is the registry: a path whose kind nobody installed is never read —
 * naming it in `filter.kinds` would `404` the whole batch — and never
 * resolves, so that pill stays the inert text it already was. */
export function useReferenceTitles(
  paths: readonly string[],
  kinds: KindInfo[]
): ReferenceTitles {
  // An array literal from the caller is a new identity every render; the
  // joined paths are what actually move.
  const pathKey = paths.join("\n")
  const known = useMemo(() => new Set(kinds.map((k) => k.identity)), [kinds])
  const scope = useMemo(
    () => titleReadScope(paths, known),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- pathKey IS the paths
    [pathKey, known]
  )

  const batch = useQuery(referenceTitlesQueryOptions(scope))
  return useMemo(
    () => titlesFromRecords(batch.data?.records ?? []),
    [batch.data]
  )
}
