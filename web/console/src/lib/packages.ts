/** The repository's packages as the console reads them: the core `package`
 * rows, one per `<authority>/<package>`, and the one fact on them the console
 * shows a person, who declared the package (decision 0111). A package an
 * agent declared is an app that agent made: its collections read "Made by
 * <agent>" and its package page gathers what it ships. */

import { queryOptions } from "@tanstack/react-query"

import { actorIdentity } from "@/lib/actor-identity"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME, splitKind } from "@/lib/api/http"
import { fetchRecordsPage } from "@/lib/api/records"
import type { Page } from "@/lib/api/types"
import { originOfKind, type Origin } from "@/lib/origin"

/** The `package` rows are registry-shaped: tens, changing when a package is
 * declared, never while a page is open. One page holds them. */
const PACKAGE_PAGE = 500

export interface PackageRow {
  /** `<authority>/<package>`, the row's id. */
  id: string
  /** The actor whose transaction created the row; absent on a package
   * created before the engine stamped it. */
  declaredBy?: string
}

/** A page of `package` records → the rows, by id. */
export function packageRows(
  page: Pick<Page, "records">
): Map<string, PackageRow> {
  const out = new Map<string, PackageRow>()
  for (const record of page.records ?? []) {
    const declaredBy = record.properties?.declaredBy
    out.set(record.id, {
      id: record.id,
      ...(typeof declaredBy === "string" && declaredBy ? { declaredBy } : {}),
    })
  }
  return out
}

export const packagesQueryOptions = queryOptions({
  queryKey: ["registry", "packages"],
  queryFn: async ({ signal }) => {
    const rows = new Map<string, PackageRow>()
    let after: string | undefined
    do {
      const page = await fetchRecordsPage(
        {
          authority: CORE_AUTHORITY,
          package: CORE_PACKAGE_NAME,
          name: "package",
          first: PACKAGE_PAGE,
          after,
        },
        signal
      )
      for (const [id, row] of packageRows(page)) rows.set(id, row)
      after = page.cursor
    } while (after)
    return rows
  },
  staleTime: 5 * 60_000,
})

/** The agent actor that declared a package, when an agent did. A door
 * (`console`, `substratectl`), a catalog's bundle or the substrate declaring
 * it is not an app someone's agent made. */
export function packageAgent(
  rows: ReadonlyMap<string, PackageRow> | undefined,
  packageId: string
): string | undefined {
  const declaredBy = rows?.get(packageId)?.declaredBy
  return declaredBy && actorIdentity(declaredBy).cls === "agent"
    ? declaredBy
    : undefined
}

/** Where a kind's records come from, with its package's declarer in view: an
 * agent's package is "Made by <agent>", everything else as `originOfKind`
 * says. */
export function kindOrigin(
  kind: string,
  rows: ReadonlyMap<string, PackageRow> | undefined
): Origin {
  const { authority, pkg } = splitKind(kind)
  const agent = packageAgent(rows, `${authority}/${pkg}`)
  if (agent) return { kind: "actor", identity: actorIdentity(agent) }
  return originOfKind(kind)
}
