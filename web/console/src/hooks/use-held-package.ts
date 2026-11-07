/** The held bundle row of one package: its status, its catalog entry and the
 * upgrade the catalog previews for it, folded the way Providers folds them. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { catalogQueryOptions } from "@/lib/api/catalog"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import { mergeBundles, type BundleRow } from "@/lib/bundles"

/** Undefined while the reads load, and where the package is not held. */
export function useHeldPackage(id: string): BundleRow | undefined {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  const repository = useQuery(repositoryQueryOptions)
  const home = repository.data?.authority ?? getRepository() ?? ""
  return useMemo(
    () =>
      mergeBundles(statuses.data ?? [], catalog.data ?? [], home).find(
        (r) => r.id === id && r.status
      ),
    [statuses.data, catalog.data, home, id]
  )
}
