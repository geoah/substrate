/** Which samples each "Add …" entry offers, and what each one shows. "Add a
 * collection" offers a sample that declares at least one primary kind; "Add
 * tools" one that ships functions; Agents one that ships agents. A sample can
 * be all three (notes brings a collection, its tools and the agents that use
 * them).
 *
 * A kind's purpose is the held declaration's where it declares one, else the
 * shipped closure's (`kindPurposes`), else primary: a sample not yet taken
 * has no declaration here, and a copy taken before its kinds declared a
 * purpose still reads the shipped answer. */

import type { KindInfo } from "@/lib/api/types"
import type { BundleRow } from "@/lib/bundles"
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

/** The samples that add a collection, by package display name. `rows` are
 * the merged catalog rows, so a sample's closure already names its kinds
 * under this repository's authority. */
export function collectionSamples(
  rows: readonly BundleRow[],
  registry: readonly KindInfo[]
): SamplePick[] {
  const byIdentity = new Map(registry.map((k) => [k.identity, k]))
  const out: SamplePick[] = []
  for (const row of rows) {
    if (row.tier !== "sample") continue
    const closure = row.catalog?.closure
    if (!closure) continue
    const shipped = closure.kindPurposes ?? {}
    const named = packageDisplayName(row.package).toLowerCase()
    const members = listed(closure.kinds)
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
    if (members.length) out.push({ row, members })
  }
  return byName(out)
}

/** The samples that ship tools or agents: `plane` names which list of the
 * closure counts. */
export function callableSamples(
  rows: readonly BundleRow[],
  plane: "functions" | "agents"
): SamplePick[] {
  const out: SamplePick[] = []
  for (const row of rows) {
    if (row.tier !== "sample") continue
    const members = listed(row.catalog?.closure[plane])
    if (members.length) out.push({ row, members })
  }
  return byName(out)
}
