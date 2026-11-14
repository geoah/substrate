/** The held bundle row of one package: its status, its catalog entry and the
 * upgrade the catalog previews for it, folded the way Providers folds them. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { catalogQueryOptions } from "@/lib/api/catalog"
import { mergeBundles, type BundleRow } from "@/lib/bundles"

/** Undefined while the reads load, and where the package is not held. */
export function useHeldPackage(id: string): BundleRow | undefined {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  return useMemo(
    () =>
      mergeBundles(statuses.data ?? [], catalog.data ?? []).find(
        (r) => r.id === id && r.status
      ),
    [statuses.data, catalog.data, id]
  )
}
