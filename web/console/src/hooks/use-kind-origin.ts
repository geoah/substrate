/** Where a kind's records come from, as every collection surface says it:
 * yours, from a provider, made by one of your agents, or built into
 * substrate. Reads the package rows (shared, cached) for the agent case. */

import { useQuery } from "@tanstack/react-query"

import type { Origin } from "@/lib/origin"
import { kindOrigin, packagesQueryOptions } from "@/lib/packages"

export function useKindOrigin(kind: string): Origin {
  const packages = useQuery(packagesQueryOptions)
  return kindOrigin(kind, packages.data)
}
