/** Which samples "Add a collection" offers, and what each one shows once it
 * lands. A sample is a collection when it declares at least one primary kind
 * and ships no tools or agents: a sample that brings functions or agents is
 * something an agent works with, and its kinds are what those tools write.
 *
 * The catalog does not carry a kind's `purpose`, so a kind reads its purpose
 * from the registry once the repository holds it and counts as primary until
 * then, which is what an absent `purpose` means. */

import type { KindInfo } from "@/lib/api/types"
import type { BundleRow } from "@/lib/bundles"
import { kindPurpose } from "@/lib/definition"
import { displayPlural, packageDisplayName } from "@/lib/kind-names"

export interface SampleCollection {
  row: BundleRow
  /** The primary kinds, as they land here: the one the package is named for
   * first, then in the closure's order. */
  kinds: string[]
}

function listed(list: readonly string[] | null | undefined): string[] {
  return list ? [...list] : []
}

/** The samples that add a collection, by package display name. `rows` are
 * the merged catalog rows, so a sample's closure already names its kinds
 * under this repository's authority. */
export function collectionSamples(
  rows: readonly BundleRow[],
  registry: readonly KindInfo[]
): SampleCollection[] {
  const byIdentity = new Map(registry.map((k) => [k.identity, k]))
  const out: SampleCollection[] = []
  for (const row of rows) {
    if (row.tier !== "sample") continue
    const closure = row.catalog?.closure
    if (!closure) continue
    if (listed(closure.functions).length || listed(closure.agents).length) {
      continue
    }
    const named = packageDisplayName(row.package).toLowerCase()
    const kinds = listed(closure.kinds)
      .filter((k) => kindPurpose(byIdentity.get(k) ?? k) === "primary")
      .map((k, i) => ({
        k,
        lead: displayPlural(byIdentity.get(k) ?? k).toLowerCase() === named,
        i,
      }))
      .sort((a, b) => Number(b.lead) - Number(a.lead) || a.i - b.i)
      .map(({ k }) => k)
    if (kinds.length) out.push({ row, kinds })
  }
  return out.sort((a, b) =>
    packageDisplayName(a.row.package).localeCompare(
      packageDisplayName(b.row.package)
    )
  )
}
