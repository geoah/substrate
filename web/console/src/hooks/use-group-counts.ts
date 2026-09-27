/** Each group's size, for a grouped collection's heads: one bounded count per
 * group the page shows, over the view's filter narrowed to that group, so a
 * head counts the whole group wherever the page boundaries fall. */

import { useQueries } from "@tanstack/react-query"

import { recordCountQueryOptions, type RecordCount } from "@/lib/api/records"
import type { RecordFilter } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"
import { groupFilter } from "@/lib/grouping"

export function useGroupCounts(
  collection: { authority: string; pkg: string; name: string },
  filter: RecordFilter | undefined,
  property: DeclaredProperty | undefined,
  keys: readonly string[]
): ReadonlyMap<string, RecordCount> {
  const wanted = property ? [...new Set(keys)] : []
  const results = useQueries({
    queries: wanted.map((key) =>
      recordCountQueryOptions(
        collection.authority,
        collection.pkg,
        collection.name,
        groupFilter(filter, property!, key)
      )
    ),
  })
  const out = new Map<string, RecordCount>()
  wanted.forEach((key, i) => {
    const data = results[i]?.data
    if (data) out.set(key, data)
  })
  return out
}
