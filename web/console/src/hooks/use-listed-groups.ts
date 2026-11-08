/** The collection groups the sidebar and Home list: every group but a
 * provider's whose set-up was started and left (`collectionsListed`), its
 * steps read the way the Providers pages read them. All data, the command
 * menu and the provider's own page list its collections either way, so they
 * stay reachable while they are not listed here. */

import { useMemo } from "react"
import { useQueries, useQuery } from "@tanstack/react-query"

import { providerSteps } from "@/components/providers/use-providers"
import { useUnits } from "@/hooks/use-units"
import {
  ACCOUNT_CONFIG_TRAIT,
  bundleStatusesQueryOptions,
  traitRecordsQueryOptions,
} from "@/lib/api/bundles"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { glanceCountQueryOptions } from "@/lib/api/records"
import type { CollectionGroup } from "@/lib/collections"
import { kindPackage } from "@/lib/definition"
import { collectionsListed, currentStep, type SetupStep } from "@/lib/providers"
import { accountViewOf, providerViews } from "@/lib/sync"

/** A provider group holds one package's kinds: the bundle's id. */
function bundleOf(g: CollectionGroup): string {
  return g.kinds[0] ? kindPackage(g.kinds[0]) : ""
}

/** Undefined while `groups` is. A provider's group waits for its reads
 * rather than flashing in and out; where a read failed it is listed, since
 * hiding data on a failed read is worse than an empty entry. */
export function useListedGroups(
  groups: CollectionGroup[] | undefined
): CollectionGroup[] | undefined {
  const units = useUnits()
  const registry = useQuery(kindsQueryOptions)
  const statuses = useQuery(bundleStatusesQueryOptions)
  const accountsRead = useQuery(traitRecordsQueryOptions(ACCOUNT_CONFIG_TRAIT))

  const providers = useMemo(
    () => (groups ?? []).filter((g) => g.type === "provider"),
    [groups]
  )
  const steps = useMemo(() => {
    if (!statuses.data || !accountsRead.data) return undefined
    const kinds = registry.data ?? []
    const ids = new Set(providers.map(bundleOf))
    const accounts = accountsRead.data.records.map((r) =>
      accountViewOf(r, kinds)
    )
    const views = providerViews(statuses.data, ids, kinds, accounts)
    const out = new Map<string, SetupStep[]>()
    for (const id of ids) {
      const installed = Boolean(
        statuses.data.find((b) => b.id === id)?.installed
      )
      const view = views.find((v) => v.id === id)
      out.set(id, providerSteps(units, id, installed, view, kinds).steps)
    }
    return out
  }, [units, registry.data, statuses.data, accountsRead.data, providers])

  const liveRecords = (g: CollectionGroup) =>
    statuses.data?.find((b) => b.id === bundleOf(g))?.liveRecords
  // Only a provider that is not set up is counted, and not where its package
  // holds no live row at all: then none of its collections holds one.
  const counted = providers
    .filter((g) => {
      const s = steps?.get(bundleOf(g))
      const live = liveRecords(g)
      return s && currentStep(s) && (live === undefined || live > 0)
    })
    .flatMap((g) => g.primary)
  const counts = useQueries({
    queries: counted.map((k) =>
      glanceCountQueryOptions(k.authority, k.package, k.name)
    ),
  })

  if (!groups) return undefined
  if (statuses.isError || accountsRead.isError) return groups
  const countOf = new Map(counted.map((k, i) => [k.identity, counts[i]]))
  const held = (g: CollectionGroup): boolean | undefined => {
    if (liveRecords(g) === 0) return false
    const answers = g.primary.map((k) => countOf.get(k.identity))
    if (answers.some((a) => a?.isError || (a?.data?.value ?? 0) > 0))
      return true
    return answers.every((a) => a?.data) ? false : undefined
  }
  return groups.filter(
    (g) =>
      g.type !== "provider" ||
      (collectionsListed(steps?.get(bundleOf(g)), held(g)) ?? false)
  )
}
