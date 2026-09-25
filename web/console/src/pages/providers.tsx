/** Providers (`/providers`): every service that can bring data in, one card
 * each — the shipped catalog's providers joined with what this repository
 * holds of them and their accounts. A card says in one pill where the
 * provider stands (On, Set up, Needs attention, Paused) or offers to add it,
 * and one line under the description says what is true right now. The card
 * opens the provider's page, where the set-up continues.
 *
 * Technical mode adds OTHER PACKAGES: every bundle this repository holds that
 * is not a provider (imported samples, the seeded llm package, anything
 * applied directly), with its version, its state, its update and its
 * removal, so nothing the old Registry listed is out of reach. */

import { useCallback, useState } from "react"
import { Link } from "@tanstack/react-router"
import { SearchXIcon, TriangleAlertIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import {
  LossyUpgradeDialog,
  PendingUpgradeNotice,
  RemoveBundleButton,
  TakeButton,
  UpgradeButton,
} from "@/components/providers/bundle-actions"
import {
  Pill,
  ProviderLogo,
  SectionHead,
  StandingPill,
  type PillTone,
} from "@/components/providers/provider-marks"
import {
  useProviders,
  type ProviderEntry,
} from "@/components/providers/use-providers"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { bundleState, setupCount } from "@/lib/api/bundles"
import {
  upgradeAvailable,
  upgradeBlocked,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import { bundleParams, firstSentence } from "@/lib/providers"
import { cn } from "@/lib/utils"
import { packageDisplayName } from "@/lib/kind-names"

export function ProvidersPage() {
  const [technical] = useTechnicalDetails()
  const data = useProviders()

  return (
    <TablePage>
      <PageHeader
        title="Providers"
        description="Services that bring your data in and keep it up to date. What they bring in is a copy: you can read it, link to it and let agents use it."
      />
      {data.error ? (
        <Empty className="mt-6 rounded-[10px] border py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The providers didn’t load</EmptyTitle>
            <EmptyDescription>{data.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" size="sm" onClick={data.refetch}>
              Try again
            </Button>
          </EmptyContent>
        </Empty>
      ) : data.pending ? (
        <div className="mt-6 grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-[118px] rounded-[10px]" />
          ))}
        </div>
      ) : data.providers.length === 0 ? (
        <p className="mt-6 text-muted-foreground">
          This substrate ships no providers.
        </p>
      ) : (
        <div className="mt-6 grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5">
          {data.providers.map((p) => (
            <ProviderCard
              key={p.row.id}
              entry={p}
              chain={data.chains(p.row)}
              technical={technical}
            />
          ))}
        </div>
      )}
      {technical && !data.pending && !data.error && (
        <OtherPackages
          rows={data.others}
          chains={data.chains}
          pendingUpgrades={data.pendingUpgrades}
        />
      )}
    </TablePage>
  )
}

const LINE_TONE: Record<ProviderEntry["standing"]["tone"], string> = {
  on: "text-muted-foreground",
  setup: "text-warning",
  attention: "text-destructive",
  paused: "text-muted-foreground",
  add: "text-faint",
}

function ProviderCard({
  entry,
  chain,
  technical,
}: {
  entry: ProviderEntry
  chain: RequirementNode[]
  technical: boolean
}) {
  const { row, info, standing } = entry
  return (
    <div
      data-slot="provider-card"
      className="relative flex flex-col gap-2 rounded-[10px] border bg-background p-3.5 transition-colors hover:border-border-strong"
    >
      <div className="flex items-center gap-2.5">
        <ProviderLogo info={info} />
        <Link
          to="/providers/$authority/$pkg"
          params={bundleParams(row.id)}
          className="min-w-0 flex-1 truncate font-semibold outline-none after:absolute after:inset-0 after:rounded-[10px] focus-visible:after:ring-2 focus-visible:after:ring-ring/50"
        >
          {info.name}
        </Link>
        {standing.tone === "add" ? (
          <TakeButton
            row={row}
            chain={chain}
            name={info.name}
            label="Add"
            className="relative z-10 h-6"
          />
        ) : (
          <StandingPill standing={standing} />
        )}
      </div>
      {row.catalog?.description && (
        <p className="line-clamp-2 text-[12.5px] text-muted-foreground">
          {firstSentence(row.catalog.description)}
        </p>
      )}
      <div className="mt-auto flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className={cn("text-[12.5px]", LINE_TONE[standing.tone])}>
          {standing.line}
        </span>
        {entry.updateAvailable && (
          <Pill tone="accent" dot={false}>
            Update available
          </Pill>
        )}
      </div>
      {technical && <IdText value={row.id} />}
    </div>
  )
}

// ── other packages (technical) ──────────────────────────────────────────────

const STATE_WORDS: Record<
  ReturnType<typeof bundleState>,
  { word: string; tone: PillTone }
> = {
  enabled: { word: "On", tone: "ok" },
  disabled: { word: "Paused", tone: "neutral" },
  uninstalled: { word: "Removed", tone: "neutral" },
  quarantined: { word: "Failed to load", tone: "bad" },
}

function tierWord(row: BundleRow): string {
  if (row.tier === "sample") return "Sample"
  if (row.tier === "provider") return "Provider"
  return "Applied directly"
}

function OtherPackages({
  rows,
  chains,
  pendingUpgrades,
}: {
  rows: BundleRow[]
  chains: (row: BundleRow) => RequirementNode[]
  pendingUpgrades: ReturnType<typeof useProviders>["pendingUpgrades"]
}) {
  // The row whose lossy update is being confirmed, by id: the dialog reads
  // that row's current preview, so a refetch shows the fresh plan.
  const [lossyID, setLossyID] = useState<string | null>(null)
  const confirmLoss = useCallback((row: BundleRow) => setLossyID(row.id), [])
  const closeLoss = useCallback(() => setLossyID(null), [])
  return (
    <section aria-labelledby="other-packages">
      <SectionHead
        id="other-packages"
        title="Other packages"
        hint="Samples you imported and anything applied directly. They are not providers: nothing syncs them."
      />
      {pendingUpgrades.length > 0 && (
        <div className="mb-3 flex flex-col gap-2">
          {pendingUpgrades.map((item) => (
            <PendingUpgradeNotice key={item.package} item={item} />
          ))}
        </div>
      )}
      {rows.length === 0 ? (
        <p className="text-muted-foreground">
          This repository holds no other packages.
        </p>
      ) : (
        <div className="overflow-x-auto rounded-[10px] border">
          <table className="w-full min-w-[720px] text-[13px]">
            <thead>
              <tr className="border-b text-left text-xs text-faint">
                <th className="w-[30%] px-3 py-2 font-medium">Package</th>
                <th className="w-[26%] px-3 py-2 font-medium">From</th>
                <th className="px-3 py-2 text-right font-medium">Version</th>
                <th className="px-3 py-2 font-medium">State</th>
                <th className="px-3 py-2 font-medium">Update</th>
                <th className="px-3 py-2">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <OtherPackageRow
                  key={row.id}
                  row={row}
                  chain={chains(row)}
                  onConfirmLoss={confirmLoss}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      <LossyUpgradeDialog
        row={rows.find((r) => r.id === lossyID)}
        name={packageDisplayName(
          rows.find((r) => r.id === lossyID)?.package ?? ""
        )}
        onClose={closeLoss}
      />
    </section>
  )
}

function OtherPackageRow({
  row,
  chain,
  onConfirmLoss,
}: {
  row: BundleRow
  chain: RequirementNode[]
  onConfirmLoss: (row: BundleRow) => void
}) {
  const status = row.status!
  const state = STATE_WORDS[bundleState(status)]
  const setup = setupCount(status)
  const name = packageDisplayName(row.package || row.name)
  return (
    <tr
      data-slot="other-package"
      className="border-b align-top last:border-0 hover:bg-hover"
    >
      <td className="px-3 py-2.5">
        <Link
          to="/providers/$authority/$pkg"
          params={bundleParams(row.id)}
          className="font-medium underline-offset-2 hover:underline"
        >
          {name}
        </Link>
        <div>
          <IdText value={row.id} copy />
        </div>
        {status.quarantined && status.quarantineReason && (
          <p className="mt-1 max-w-[60ch] text-xs text-destructive">
            {status.quarantineReason}
          </p>
        )}
      </td>
      <td className="px-3 py-2.5 text-muted-foreground">
        {tierWord(row)}
        {status.origin && (
          <div>
            <IdText value={status.origin} />
            {status.modified && (
              <span className="ml-1 text-xs text-faint">· edited since</span>
            )}
          </div>
        )}
      </td>
      <td className="px-3 py-2.5 text-right text-muted-foreground tabular-nums">
        {status.version ?? "—"}
      </td>
      <td className="px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-1.5">
          <Pill tone={state.tone}>{state.word}</Pill>
          {setup > 0 && (
            <Pill tone="warn" dot={false}>
              {setup === 1 ? "1 thing to set up" : `${setup} things to set up`}
            </Pill>
          )}
        </div>
      </td>
      <td className="px-3 py-2.5">
        {upgradeBlocked(row) ? (
          <Tooltip>
            <TooltipTrigger
              render={<span className="inline-flex cursor-help" />}
            >
              <Pill tone="warn" dot={false}>
                <TriangleAlertIcon aria-hidden className="size-3" />
                Update blocked
              </Pill>
            </TooltipTrigger>
            <TooltipContent className="max-w-96">
              <div className="space-y-1">
                {(row.upgrade?.blockers ?? []).map((b) => (
                  <p key={b}>{b}</p>
                ))}
              </div>
            </TooltipContent>
          </Tooltip>
        ) : upgradeAvailable(row) ? (
          <UpgradeButton row={row} name={name} onConfirmLoss={onConfirmLoss} />
        ) : (
          <span className="text-faint">Current</span>
        )}
      </td>
      <td className="px-3 py-2.5 text-right">
        {status.quarantined && row.catalog ? (
          <TakeButton row={row} chain={chain} name={name} label="Take again" />
        ) : (
          <RemoveBundleButton bundle={status} name={name} variant="ghost" />
        )}
      </td>
    </tr>
  )
}
