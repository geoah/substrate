/** THE ONE GATE every mount of an app passes, whether it fills `/apps/$id`,
 * sits as a card on the overview or a record page, or is the body of a
 * browse tab: the same reads and the same checks in the same order, so an
 * app refused on its own page cannot run where it mounts on its own. The
 * record (the single-record read, the one carrier of `propertyMeta`), the
 * registry and the repository's package versions are all waited for before
 * anything is decoded: a `requiresAtLeast` floor can only be checked against
 * a version that has landed, and a mount before that check is one the floor
 * never guarded. Then, off the decoded spec: the problems that block, the
 * packages the grant names and the repository lacks, owner provenance, the
 * expanded grant, and the inputs resolved inside it. What a mount does with
 * a refusal is its own (the screen offers the Registry, a card draws the
 * strip); what is refused is decided here. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { useLiveRecords } from "@/hooks/use-live-records"
import { appQueryOptions } from "@/lib/api/apps"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordsQueryOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { appSpec, missingPackages } from "@/lib/apps/app-spec"
import { ownerProvenance } from "@/lib/apps/bridge/host"
import { expandGrant, type ExpandedGrant } from "@/lib/apps/grant"
import { useAppInputs, type ResolvedInputs } from "@/lib/apps/inputs"
import { blockingProblems, type AppSpec, type Problem } from "@/lib/apps/spec"

/** One page comfortably above any repository's package count. */
const PACKAGES_PAGE = 200

export type AppGate =
  | { phase: "pending" }
  /** The app cannot be read: no such record, or the read or the registry
   * failed. */
  | { phase: "absent"; message: string }
  | {
      phase: "ready"
      record: SubstrateRecord
      spec: AppSpec
      kinds: KindInfo[]
      expanded: ExpandedGrant
      /** Owner provenance on `source`, `modules` and `permissions`. */
      granted: boolean
      /** The packages the grant names and the repository lacks. */
      missing: string[]
      /** What refuses the mount: the spec's errors, a floor the repository
       * is below, or a floor that could not be checked. */
      blocking: Problem[]
      inputs: ResolvedInputs
    }

/** Package identity → the version the repository holds, off the
 * `core/package` collection, for the `requiresAtLeast` floor. */
export function packageVersions(
  records: SubstrateRecord[]
): Record<string, number> {
  const out: Record<string, number> = {}
  for (const r of records) {
    const v = r.properties.version
    if (typeof v === "number") out[r.id] = v
  }
  return out
}

export function useAppGate(id: string): AppGate {
  const registry = useQuery(kindsQueryOptions)
  const app = useQuery(appQueryOptions(id))
  const packages = useQuery(
    recordsQueryOptions({
      authority: CORE_AUTHORITY,
      package: CORE_PACKAGE_NAME,
      name: "package",
      first: PACKAGES_PAGE,
    })
  )
  const kinds = useMemo(() => registry.data ?? [], [registry.data])
  const versions = useMemo(
    () =>
      packages.data
        ? packageVersions(packages.data.records ?? [])
        : packages.isError
          ? {}
          : undefined,
    [packages.data, packages.isError]
  )
  // Decoded only once the versions are known (or known to be unreadable),
  // so a floor is never checked against nothing.
  const spec = useMemo(
    () =>
      app.data && registry.data && versions
        ? appSpec(app.data, registry.data, versions)
        : undefined,
    [app.data, registry.data, versions]
  )
  const granted = ownerProvenance(app.data)
  const expanded = useMemo(
    () => (spec ? expandGrant(spec, kinds) : undefined),
    [spec, kinds]
  )
  const inputs = useAppInputs(spec, kinds, {
    granted,
    reads: expanded?.reads ?? [],
  })
  useLiveRecords(inputs.kinds)

  return useMemo<AppGate>(() => {
    if (app.isError) return { phase: "absent", message: app.error.message }
    if (registry.isError) {
      return {
        phase: "absent",
        message: `the registry could not be read: ${registry.error.message}`,
      }
    }
    if (!app.data || !spec || !expanded) return { phase: "pending" }
    const blocking = blockingProblems(spec.problems)
    if (packages.isError && Object.keys(spec.requiresAtLeast).length) {
      blocking.push({
        path: "requiresAtLeast",
        message: `the repository's package versions could not be read, so the floor cannot be checked: ${packages.error.message}`,
        severity: "error",
      })
    }
    return {
      phase: "ready",
      record: app.data,
      spec,
      kinds,
      expanded,
      granted,
      missing: missingPackages(spec),
      blocking,
      inputs,
    }
  }, [
    app.isError,
    app.error,
    app.data,
    registry.isError,
    registry.error,
    packages.isError,
    packages.error,
    spec,
    expanded,
    kinds,
    granted,
    inputs,
  ])
}
