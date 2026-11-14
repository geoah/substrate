/** Which samples each "Add …" entry offers, and what each one shows. "Add a
 * collection" offers a sample that declares at least one primary kind, and
 * the standard collections beside them (decision record 0238); "Add tools" one
 * that ships functions; Agents one that ships agents. A sample can be all
 * three (notes brings a collection, its tools and the agents that use them).
 *
 * A kind's purpose is the held declaration's where it declares one, else the
 * shipped closure's (`kindPurposes`), else primary: a sample not yet taken
 * has no declaration here, and a copy taken before its kinds declared a
 * purpose still reads the shipped answer. */

import type { KindInfo } from "@/lib/api/types"
import { isStandard, type BundleRow } from "@/lib/bundles"
import { kindPurpose } from "@/lib/definition"
import { displayPlural, packageDisplayName } from "@/lib/kind-names"

export interface SamplePick {
  row: BundleRow
  /** What the entry shows the sample adds, as it lands here: primary kinds
   * (the one the package is named for first), functions, or agents. */
  members: string[]
}

function listed(list: readonly string[] | null | undefined): string[] {
  return list ? [...list] : []
}

function byName(picks: SamplePick[]): SamplePick[] {
  return picks.sort((a, b) =>
    packageDisplayName(a.row.package).localeCompare(
      packageDisplayName(b.row.package)
    )
  )
}

/** A closure's primary kinds, the one the package is named for first. */
function primaryKinds(
  row: BundleRow,
  byIdentity: ReadonlyMap<string, KindInfo>
): string[] {
  const closure = row.catalog?.closure
  if (!closure) return []
  const shipped = closure.kindPurposes ?? {}
  const named = packageDisplayName(row.package).toLowerCase()
  return listed(closure.kinds)
    .filter(
      (k) => kindPurpose(byIdentity.get(k) ?? k, shipped[k]) === "primary"
    )
    .map((k, i) => ({
      k,
      lead: displayPlural(byIdentity.get(k) ?? k).toLowerCase() === named,
      i,
    }))
    .sort((a, b) => Number(b.lead) - Number(a.lead) || a.i - b.i)
    .map(({ k }) => k)
}

/** The samples that add a collection, by package display name. `rows` are
 * the merged catalog rows, so a sample's closure already names its kinds
 * under `~`, where they land. */
export function collectionSamples(
  rows: readonly BundleRow[],
  registry: readonly KindInfo[]
): SamplePick[] {
  const byIdentity = new Map(registry.map((k) => [k.identity, k]))
  const out: SamplePick[] = []
  for (const row of rows) {
    if (row.keptBy !== "repository") continue
    const members = primaryKinds(row, byIdentity)
    if (members.length) out.push({ row, members })
  }
  return byName(out)
}

/** The standard collections (`health`, `labs`, `clinical`, an app's own
 * package): published, no provider and no add-on, adding collections or
 * metrics (`isStandard`). One that adds metrics alone (`environment`) has no
 * members, and its row says what it is instead. */
export function standardCollections(
  rows: readonly BundleRow[],
  registry: readonly KindInfo[]
): SamplePick[] {
  const byIdentity = new Map(registry.map((k) => [k.identity, k]))
  return byName(
    rows
      .filter(isStandard)
      .map((row) => ({ row, members: primaryKinds(row, byIdentity) }))
  )
}

/** The samples that ship tools or agents: `plane` names which list of the
 * closure counts. */
export function callableSamples(
  rows: readonly BundleRow[],
  plane: "functions" | "agents"
): SamplePick[] {
  const out: SamplePick[] = []
  for (const row of rows) {
    if (row.keptBy !== "repository") continue
    const members = listed(row.catalog?.closure[plane])
    if (members.length) out.push({ row, members })
  }
  return byName(out)
}
