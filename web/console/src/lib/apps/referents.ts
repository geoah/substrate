/** The titles behind a page's reference values: ONE `filter.ids` read per
 * referenced kind per page, folded into a path → title map, so a row shows
 * "Taxes 2026" rather than `taxes` and a group header reads the same way. A
 * kind the registry lacks is skipped and its ids stand in. */

import { useQueries } from "@tanstack/react-query"

import { recordsQueryOptions } from "@/lib/api/records"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { recordPath, splitRecordPath } from "@/lib/record-path"

/** The referent paths a set of records carries under the named properties,
 * grouped by the referent's kind. */
export function referencedIds(
  records: SubstrateRecord[],
  names: string[]
): Map<string, Set<string>> {
  const byKind = new Map<string, Set<string>>()
  const add = (value: unknown) => {
    const held = readReference(value)
    if (!held) return
    const parts = splitRecordPath(held.path)
    if (!parts) return
    const ids = byKind.get(parts.kind) ?? new Set<string>()
    ids.add(parts.id)
    byKind.set(parts.kind, ids)
  }
  for (const record of records) {
    for (const name of names) {
      const value = record.properties[name]
      if (Array.isArray(value)) value.forEach(add)
      else add(value)
    }
  }
  return byKind
}

/** A record's heading as a referent: the server title, else its `name`, else
 * its id. */
export function titleOf(record: SubstrateRecord): string {
  const title = recordTitle(record.properties)
  if (title) return title
  const name = record.properties.name
  return typeof name === "string" && name ? name : record.id
}

/** A path → title map over the referents the records name. */
export function useReferentTitles(
  records: SubstrateRecord[],
  names: string[],
  kinds: KindInfo[]
): Map<string, string> {
  const wanted = [...referencedIds(records, names)]
    .map(([kind, ids]) => ({
      kind: kindByIdentity(kinds, kind),
      ids: [...ids].sort(),
    }))
    .filter((w): w is { kind: KindInfo; ids: string[] } => Boolean(w.kind))
  return useQueries({
    queries: wanted.map(({ kind, ids }) => {
      const { authority, pkg, name } = splitKind(kind.identity)
      return {
        ...recordsQueryOptions({
          authority,
          package: pkg,
          name,
          first: Math.min(Math.max(ids.length, 1), 500),
          filter: { ids },
        }),
        staleTime: 60_000,
      }
    }),
    combine: (results) => {
      const titles = new Map<string, string>()
      for (const result of results) {
        for (const record of result.data?.records ?? []) {
          titles.set(recordPath(record.kind, record.id), titleOf(record))
        }
      }
      return titles
    },
  })
}
