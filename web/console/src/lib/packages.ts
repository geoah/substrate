/** The repository's packages as the console reads them: the core `package`
 * rows, one per `<authority>/<package>`, and the facts on them the console
 * shows: who declared the package (decision 0109), the stability it
 * states for its kinds, and the function interface its code is written
 * against (decision 0265). A package an agent declared is an app that agent
 * made: its collections read "Made by <agent>" and its package page gathers
 * what it ships. */

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
  publishedFrom?: string
  /** `<authority>/<package>`, the row's id. */
  id: string
  /** The actor whose transaction created the row; absent on a package
   * created before the engine stamped it. */
  declaredBy?: string
  /** The label the package states for its kinds (`stable`, `beta`,
   * `experimental`); absent where it states none. */
  stability?: string
  /** The function interface the package's code is written against; absent
   * where it names none. */
  functionInterface?: number
}

/** The stability to show for a package: the label it states, and
 * `experimental` where it states none, since a package that promises nothing
 * is read as promising the least. */
export function packageStability(row: PackageRow): string {
  return row.stability ?? "experimental"
}

/** The function interface to show for a package: the one it names, and 1
 * where it names none, the interface every package was written against
 * before the key existed. */
export function packageInterface(row: PackageRow): number {
  return row.functionInterface ?? 1
}

/** A page of `package` records → the rows, by id. */
export function packageRows(
  page: Pick<Page, "records">
): Map<string, PackageRow> {
  const out = new Map<string, PackageRow>()
  for (const record of page.records ?? []) {
    const declaredBy = record.properties?.declaredBy
    const stability = record.properties?.stability
    const functionInterface = record.properties?.interface
    out.set(record.id, {
      id: record.id,
      ...(typeof record.properties?.publishedFrom === "string"
        ? { publishedFrom: record.properties.publishedFrom }
        : {}),
      ...(typeof declaredBy === "string" && declaredBy ? { declaredBy } : {}),
      ...(typeof stability === "string" && stability ? { stability } : {}),
      ...(typeof functionInterface === "number" ? { functionInterface } : {}),
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
 * (`console`, `substratectl`), an app, a catalog's bundle or the substrate
 * declaring it is not an app someone's agent made. */
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
  const maker = packageAgent(rows, `${authority}/${pkg}`)
  if (maker) return { kind: "actor", identity: actorIdentity(maker) }
  return originOfKind(kind)
}
