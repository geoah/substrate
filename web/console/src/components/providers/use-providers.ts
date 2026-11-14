/** Every read the Providers list and one provider's page share, folded once:
 * the bundles this repository holds with their shipped catalog entries (and
 * the requirement chain under each), the providers among them with their
 * accounts, and the one state each provider's card wears. Both pages call
 * the same hook, so a card and its page cannot disagree. */

import { useCallback, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { useLiveRecords } from "@/hooks/use-live-records"
import { useNow } from "@/hooks/use-now"
import {
  accountFormGroups,
  ungrantedToggles,
  writeToggles,
} from "@/lib/account-form"
import { providerInfo, type ProviderInfo } from "@/lib/actor-identity"
import {
  ACCOUNT_CONFIG_TRAIT,
  bundleStatusesQueryOptions,
  traitRecordsQueryOptions,
} from "@/lib/api/bundles"
import {
  catalogQueryOptions,
  shippedUpgradesQueryOptions,
} from "@/lib/api/catalog"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { triggerStatusesQueryOptions } from "@/lib/api/sync"
import type { KindInfo, TriggerStatus } from "@/lib/api/types"
import { TRIGGER_RUN_KIND } from "@/lib/core-kinds"
import {
  accountKindOf,
  addonOf,
  heldVersions,
  isInputSetupCode,
  isProvider,
  mergeBundles,
  pendingShippedUpgrades,
  presentPackages,
  publisherUnreadable,
  requirementTree,
  upgradeAvailable,
  upgradeBlocked,
  upgradeHolders,
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
  accountViewsOf,
  providerViews,
  type AccountView,
  type ProviderView,
} from "@/lib/sync"
import { useUnits } from "@/hooks/use-units"
import type { UnitReader } from "@/lib/unit-reader"

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
  /** The switches that let the owner's agents change things at the
   * provider (its bundle marks them `access: write`), with what each says. */
  permissions: { name: string; label: string; description?: string }[]
  updateAvailable: boolean
}

/** The account kind's owner-written bool properties that bring something
 * in, by name and label: every bool but the ones `write` names. */
export function accountToggles(
  units: UnitReader,
  kind: KindInfo | undefined,
  write?: ReadonlySet<string>
): { name: string; label: string }[] {
  if (!kind) return []
  return accountFormGroups(units, kind, write).toggles.map((f) => ({
    name: f.name,
    label: f.label,
  }))
}

/** The account kind's switches that let the owner's agents change things
 * at the provider, by name, label and description. */
export function accountPermissions(
  units: UnitReader,
  kind: KindInfo | undefined,
  write: ReadonlySet<string>
): { name: string; label: string; description?: string }[] {
  if (!kind || write.size === 0) return []
  return accountFormGroups(units, kind, write).permissions.map((f) => ({
    name: f.name,
    label: f.label,
    description: f.description,
  }))
}

/** The triggers that run one bundle's own functions: a function's reference
 * is `<bundle id>/<name>`. */
export function bundleTriggers(
  statuses: readonly TriggerStatus[],
  bundleId: string
): TriggerStatus[] {
  return statuses.filter((t) => t.callable.startsWith(`${bundleId}/`))
}

/** The four steps one provider stands at, and its account kind's switches.
 * The Providers pages and the collections the sidebar and Home list both
 * read this, so a provider those list is one its card calls set up. */
export function providerSteps(
  units: UnitReader,
  bundleId: string,
  installed: boolean,
  view: ProviderView | undefined,
  kinds: KindInfo[]
): { steps: SetupStep[]; toggles: { name: string; label: string }[] } {
  const accountKind = view?.accountKind ?? accountKindOf(kinds, bundleId)
  const toggles = accountToggles(
    units,
    accountKind,
    writeToggles(view?.status?.features)
  )
  const steps = setupSteps(
    stepFactsOf(
      installed,
      view,
      toggles.map((t) => t.name)
    )
  )
  return { steps, toggles }
}

function entryOf(
  units: UnitReader,
  row: BundleRow,
  view: ProviderView | undefined,
  kinds: KindInfo[],
  triggers: readonly TriggerStatus[],
  now: number
): ProviderEntry {
  const installed = Boolean(row.status?.installed)
  const { steps, toggles } = providerSteps(
    units,
    row.id,
    installed,
    view,
    kinds
  )
  const features = view?.status?.features
  const permissions = accountPermissions(
    units,
    view?.accountKind,
    writeToggles(features)
  )
  const labelOf = new Map(
    [...toggles, ...permissions].map((t) => [t.name, t.label])
  )
  // The four steps cover the credentials input; anything else the status
  // still lists (a required setting, another input) is set up on the page.
  const otherSetup = (row.status?.setup ?? []).filter(
    (item) =>
      item.code !== "oauth-client" &&
      !(isInputSetupCode(item.code) && item.input === configInputName(view))
  )
  return {
    row,
    info: providerInfo(row.package),
    view,
    steps,
    toggles,
    permissions,
    updateAvailable: upgradeAvailable(row) && !upgradeBlocked(row),
    standing: providerStanding(
      {
        installed,
        enabled: row.status?.enabled ?? false,
        quarantined: Boolean(row.status?.quarantined),
        quarantineReason: row.status?.quarantineReason,
        upgradeBlockers:
          installed && upgradeBlocked(row) ? (row.upgrade?.blockers ?? []) : [],
        upgradeHolders:
          installed && upgradeBlocked(row) ? upgradeHolders(row.upgrade) : [],
        upgradePublisherUnread:
          installed && upgradeBlocked(row) && publisherUnreadable(row.upgrade),
        otherSetup,
        steps,
        oauth: view?.oauth,
        accounts: (view?.accounts ?? []).map((a) => ({
          ...a,
          ungranted: ungrantedToggles(
            a.record.properties,
            a.grantedScopes,
            features
          ).map((t) => labelOf.get(t) ?? t),
        })),
        triggers: installed ? bundleTriggers(triggers, row.id) : [],
      },
      now
    ),
  }
}

function configInputName(view: ProviderView | undefined): string | undefined {
  if (!view?.configKind) return undefined
  return view.status?.inputs?.find((i) => i.kind === view.configKind?.identity)
    ?.name
}

export function useProviders() {
  const units = useUnits()
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  // The boot upgrade's preview, for the one package no catalog entry carries:
  // core. Not waited on and not fatal.
  const shipped = useQuery(shippedUpgradesQueryOptions)
  const registry = useQuery(kindsQueryOptions)
  const accountsRead = useQuery(traitRecordsQueryOptions(ACCOUNT_CONFIG_TRAIT))
  // Not waited on: a provider's parked and broken runs join its state once
  // they are read.
  const triggerStatuses = useQuery(triggerStatusesQueryOptions)
  // A waited-on sync request reads as failing again after a quarter hour
  // (providerStanding); the minute tick moves the card without a refetch.
  const now = useNow(60_000)

  const kinds = useMemo(() => registry.data ?? [], [registry.data])

  const rows = useMemo(
    () => mergeBundles(statuses.data ?? [], catalog.data ?? []),
    [statuses.data, catalog.data]
  )
  // Presence is computed over EVERY row: a sample held here still satisfies
  // a provider's requirement.
  const chains = useMemo(() => {
    const present = presentPackages(rows, kinds)
    const versions = heldVersions(rows)
    const trees = new Map<string, RequirementNode[]>()
    for (const row of rows) {
      trees.set(row.key, requirementTree(row, rows, present, versions))
    }
    return (row: BundleRow) => trees.get(row.key) ?? []
  }, [rows, kinds])

  const accounts = useMemo<AccountView[]>(
    () => accountViewsOf(accountsRead.data?.records ?? [], kinds),
    [accountsRead.data, kinds]
  )
  const views = useMemo(() => {
    const listed = new Set(rows.filter(isProvider).map((r) => r.id))
    return providerViews(statuses.data ?? [], listed, kinds, accounts)
  }, [rows, statuses.data, kinds, accounts])

  // A provider is one the catalog says connects to an outside service
  // (decision record 0238), or, held with no catalog entry or one whose
  // publisher cannot be read, one whose stored declarations bring an account,
  // as its status says. An add-on gets a provider's page too, and is listed
  // on the page of the provider it extends.
  const providers = useMemo(() => {
    const viewOf = new Map(views.map((v) => [v.id, v]))
    return rows
      .filter((r) => isProvider(r) || Boolean(addonOf(r)))
      .map((r) =>
        entryOf(
          units,
          r,
          viewOf.get(r.id),
          kinds,
          triggerStatuses.data ?? [],
          now
        )
      )
      .sort(
        (a, b) =>
          Number(!a.row.status) - Number(!b.row.status) ||
          a.info.name.localeCompare(b.info.name)
      )
  }, [units, rows, views, kinds, triggerStatuses.data, now])

  // The providers Providers lists, and each one's add-ons, which its page
  // lists instead.
  const listed = useMemo(
    () => providers.filter((p) => !addonOf(p.row)),
    [providers]
  )
  const addonsOf = useCallback(
    (id: string) => providers.filter((p) => addonOf(p.row) === id),
    [providers]
  )

  // Everything else this repository holds: standard collections, imported
  // samples, the seeded llm package, anything applied directly.
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
    const out = new Set<string>([TRIGGER_RUN_KIND])
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
    accountsRead,
    kinds,
    rows,
    chains,
    accounts,
    providers,
    listed,
    addonsOf,
    others,
    pendingUpgrades,
    // The kind registry is waited on too: without it a requirement reads as
    // missing for a frame.
    pending: statuses.isPending || catalog.isPending || registry.isPending,
    error: statuses.error ?? catalog.error ?? undefined,
    refetch: () => {
      void statuses.refetch()
      void catalog.refetch()
    },
  }
}

export type ProvidersData = ReturnType<typeof useProviders>
