/** Every read the Providers list and one provider's page share, folded once:
 * the bundles this repository holds with their shipped catalog entries (and
 * the requirement chain under each), the providers among them with their
 * accounts, and the one state each provider's card wears. Both pages call
 * the same hook, so a card and its page cannot disagree. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { useLiveRecords } from "@/hooks/use-live-records"
import { accountFormGroups } from "@/lib/account-form"
import { providerInfo, type ProviderInfo } from "@/lib/actor-identity"
import {
  ACCOUNT_CONFIG_TRAIT,
  bundleStatusesQueryOptions,
  setupCount,
  traitRecordsQueryOptions,
} from "@/lib/api/bundles"
import {
  catalogQueryOptions,
  shippedUpgradesQueryOptions,
} from "@/lib/api/catalog"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { repositoryQueryOptions } from "@/lib/api/repository"
import type { KindInfo } from "@/lib/api/types"
import {
  accountKindOf,
  heldVersions,
  isInputSetupCode,
  mergeBundles,
  pendingShippedUpgrades,
  presentPackages,
  requirementTree,
  upgradeAvailable,
  upgradeBlocked,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import {
  providerStanding,
  setupSteps,
  stepFactsOf,
  type ProviderStanding,
  type SetupStep,
} from "@/lib/providers"
import {
  accountViewOf,
  providerViews,
  type AccountView,
  type ProviderView,
} from "@/lib/sync"

/** One provider as both pages show it. */
export interface ProviderEntry {
  row: BundleRow
  info: ProviderInfo
  /** Present once the bundle is here. */
  view?: ProviderView
  steps: SetupStep[]
  standing: ProviderStanding
  /** The account kind's switches: what step 4 chooses between. */
  toggles: { name: string; label: string }[]
  updateAvailable: boolean
}

/** The account kind's owner-written bool properties, by name and label. */
export function accountToggles(
  kind: KindInfo | undefined
): { name: string; label: string }[] {
  if (!kind) return []
  return accountFormGroups(kind).toggles.map((f) => ({
    name: f.name,
    label: f.label,
  }))
}

function entryOf(
  row: BundleRow,
  view: ProviderView | undefined,
  kinds: KindInfo[]
): ProviderEntry {
  const accountKind = view?.accountKind ?? accountKindOf(kinds, row.id)
  const toggles = accountToggles(accountKind)
  const installed = Boolean(row.status?.installed)
  const steps = setupSteps(
    stepFactsOf(
      installed,
      view,
      toggles.map((t) => t.name)
    )
  )
  // The four steps cover the credentials input; anything else the status
  // still lists (a required setting, another input) is set up on the page.
  const otherSetup = (row.status?.setup ?? []).filter(
    (item) =>
      item.code !== "oauth-client" &&
      !(isInputSetupCode(item.code) && item.input === configInputName(view))
  ).length
  return {
    row,
    info: providerInfo(row.package),
    view,
    steps,
    toggles,
    updateAvailable: upgradeAvailable(row) && !upgradeBlocked(row),
    standing: providerStanding({
      installed,
      enabled: row.status?.enabled ?? false,
      quarantined: Boolean(row.status?.quarantined),
      upgradeBlocked: installed && upgradeBlocked(row),
      otherSetup,
      steps,
      accounts: view?.accounts ?? [],
    }),
  }
}

function configInputName(view: ProviderView | undefined): string | undefined {
  if (!view?.configKind) return undefined
  return view.status?.inputs?.find((i) => i.kind === view.configKind?.identity)
    ?.name
}

export function useProviders() {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  // The boot upgrade's preview, for the one package no catalog entry carries:
  // core. Not waited on and not fatal.
  const shipped = useQuery(shippedUpgradesQueryOptions)
  // The authority this repository owns, which is where an imported sample
  // landed: without it a held sample would never meet its catalog entry.
  const repository = useQuery(repositoryQueryOptions)
  const registry = useQuery(kindsQueryOptions)
  const accountsRead = useQuery(traitRecordsQueryOptions(ACCOUNT_CONFIG_TRAIT))

  const kinds = useMemo(() => registry.data ?? [], [registry.data])
  const home = repository.data?.authority ?? ""

  const rows = useMemo(
    () => mergeBundles(statuses.data ?? [], catalog.data ?? [], home),
    [statuses.data, catalog.data, home]
  )
  // Presence is computed over EVERY row: a sample held here still satisfies
  // a provider's requirement.
  const chains = useMemo(() => {
    const present = presentPackages(rows, kinds)
    const versions = heldVersions(rows)
    const byId = new Map(rows.map((row) => [row.id, row]))
    const trees = new Map<string, RequirementNode[]>()
    for (const row of rows) {
      trees.set(row.id, requirementTree(row, byId, present, versions))
    }
    return (row: BundleRow) => trees.get(row.id) ?? []
  }, [rows, kinds])

  const accounts = useMemo<AccountView[]>(
    () => (accountsRead.data?.records ?? []).map((r) => accountViewOf(r, kinds)),
    [accountsRead.data, kinds]
  )
  const views = useMemo(() => {
    const tier = new Set(
      rows.filter((r) => r.tier === "provider").map((r) => r.id)
    )
    return providerViews(statuses.data ?? [], tier, kinds, accounts)
  }, [rows, statuses.data, kinds, accounts])

  // A provider is a catalog provider, or a bundle applied outside the
  // catalog that declares an account kind: it has accounts to connect, so it
  // is one whatever the catalog says.
  const providers = useMemo(() => {
    const viewOf = new Map(views.map((v) => [v.id, v]))
    return rows
      .filter(
        (r) =>
          r.tier === "provider" ||
          (!r.tier && r.status && viewOf.has(r.id))
      )
      .map((r) => entryOf(r, viewOf.get(r.id), kinds))
      .sort(
        (a, b) =>
          Number(!a.row.status) - Number(!b.row.status) ||
          a.info.name.localeCompare(b.info.name)
      )
  }, [rows, views, kinds])

  // Everything else this repository holds: imported samples, the seeded
  // llm package, anything applied directly.
  const others = useMemo(() => {
    const ids = new Set(providers.map((p) => p.row.id))
    return rows.filter((r) => r.status && !ids.has(r.id))
  }, [rows, providers])

  const pendingUpgrades = useMemo(
    () => pendingShippedUpgrades(shipped.data ?? []),
    [shipped.data]
  )

  // The kinds the change feed watches: every account kind, and the run
  // ledger, so a settled delivery re-reads the tools and details too.
  const liveKinds = useMemo(() => {
    const out = new Set<string>(["substrate.reamde.dev/core/triggerrun"])
    for (const v of views) if (v.accountKind) out.add(v.accountKind.identity)
    return [...out]
  }, [views])
  useLiveRecords(liveKinds, [
    ["trait", "records"],
    ["sync"],
    ["triggers"],
    ["record"],
    ["bundles", "status"],
  ])

  return {
    statuses,
    catalog,
    registry,
    repository,
    accountsRead,
    kinds,
    home,
    rows,
    chains,
    accounts,
    providers,
    others,
    pendingUpgrades,
    // The kind registry and the repository are waited on too: without them a
    // requirement reads as missing, and a held sample as not held, for a
    // frame.
    pending:
      statuses.isPending ||
      catalog.isPending ||
      registry.isPending ||
      repository.isPending,
    error: statuses.error ?? catalog.error ?? undefined,
    refetch: () => {
      void statuses.refetch()
      void catalog.refetch()
    },
  }
}

export type ProvidersData = ReturnType<typeof useProviders>

/** The setup items a bundle's status still lists, as the sidebar and the
 * card count them. */
export function setupLeft(row: BundleRow): number {
  return row.status ? setupCount(row.status) : 0
}
