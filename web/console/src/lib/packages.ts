/** The repository's packages as the console reads them: the core `package`
 * rows, one per `<authority>/<package>`, and the one fact on them the console
 * shows a person, who declared the package (decision 0109). A package an
 * agent declared is an app that agent made: its collections read "Made by
 * <agent>" and its package page gathers what it ships. A package an approved
 * app declared under its `declare:` scope is that app's (decision 0205): its
 * collections read "Managed by <app>", and its declarations are read only
 * until the owner takes it over. */

import { queryOptions } from "@tanstack/react-query"

import { actorIdentity } from "@/lib/actor-identity"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME, splitKind } from "@/lib/api/http"
import { fetchRecordsPage } from "@/lib/api/records"
import type { Page, SubstrateRecord } from "@/lib/api/types"
import { KIND_KIND } from "@/lib/core-kinds"
import { BUNDLE_KIND, PACKAGE_KIND } from "@/lib/declarations"
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
  /** The client id of the app that declared it, beside its `app:<host>`
   * actor: what ownership is compared on (decision 0205). */
  declaredByClient?: string
}

/** A page of `package` records → the rows, by id. */
export function packageRows(
  page: Pick<Page, "records">
): Map<string, PackageRow> {
  const out = new Map<string, PackageRow>()
  for (const record of page.records ?? []) {
    const declaredBy = record.properties?.declaredBy
    const declaredByClient = record.properties?.declaredByClient
    out.set(record.id, {
      id: record.id,
      ...(typeof declaredBy === "string" && declaredBy ? { declaredBy } : {}),
      ...(typeof declaredByClient === "string" && declaredByClient
        ? { declaredByClient }
        : {}),
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

/** The app actor that manages a package (decision 0205): the one that
 * declared it under its `declare:` scope. Its package, bundle and kind
 * documents change only through that app until the owner takes it over. */
export function packageApp(
  rows: ReadonlyMap<string, PackageRow> | undefined,
  packageId: string
): string | undefined {
  return appActor(rows?.get(packageId)?.declaredBy)
}

function appActor(actor: unknown): string | undefined {
  return typeof actor === "string" && actorIdentity(actor).cls === "app"
    ? actor
    : undefined
}

/** The app managing the package a declaration record belongs to: a package
 * row (whose own `declaredBy` says it), its bundle (named for the package)
 * or one of its kinds (whose id leads with the package). `undefined` for any
 * other record, and for a package no app manages. */
export function declarationApp(
  record: Pick<SubstrateRecord, "kind" | "id" | "properties">,
  rows: ReadonlyMap<string, PackageRow> | undefined
): string | undefined {
  switch (record.kind) {
    case PACKAGE_KIND:
      return appActor(record.properties?.declaredBy)
    case BUNDLE_KIND:
      return packageApp(rows, record.id)
    case KIND_KIND: {
      const { authority, pkg } = splitKind(record.id)
      return packageApp(rows, `${authority}/${pkg}`)
    }
    default:
      return undefined
  }
}

/** Whether a record's edit doors wait for the package rows: a bundle or a
 * kind, whose package's row says whether an app manages it (decision 0205),
 * while those rows load. A package row says it on itself and never waits.
 * Without the wait, an app's kind offers the edit sheet for a moment and the
 * server's refusal arrives as a toast instead of the read-only page. */
export function awaitsPackageRows(
  record: Pick<SubstrateRecord, "kind">,
  pending: boolean
): boolean {
  return pending && (record.kind === BUNDLE_KIND || record.kind === KIND_KIND)
}

/** Where a kind's records come from, with its package's declarer in view: an
 * agent's package is "Made by <agent>", an app's "Managed by <app>",
 * everything else as `originOfKind` says. */
export function kindOrigin(
  kind: string,
  rows: ReadonlyMap<string, PackageRow> | undefined
): Origin {
  const { authority, pkg } = splitKind(kind)
  const maker =
    packageAgent(rows, `${authority}/${pkg}`) ??
    packageApp(rows, `${authority}/${pkg}`)
  if (maker) return { kind: "actor", identity: actorIdentity(maker) }
  return originOfKind(kind)
}

/** The words every surface uses for an app-managed package: who keeps its
 * definitions, and that the records stay the reader's. */
export function appManagedLine(app: string): string {
  return `${actorIdentity(app).name} manages these collections’ definitions. The records in them are yours.`
}
