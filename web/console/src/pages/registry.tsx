/** Registry (`/registry`): every bundle this substrate knows: the ones this
 * repository holds, with their runtime state (from the computed status
 * endpoint), and the closures shipped in the catalog it has not taken yet.
 *
 * TWO SECTIONS, the two catalog tiers (decision record 0048). PROVIDERS are
 * packages a publisher owns: they install under the authority that publishes
 * them, and their upgrades are offered here. SAMPLES are vocabulary to copy:
 * importing one rewrites it onto THIS repository's authority, so the row
 * previews the identity it will land under before the button is pressed, and
 * nothing upstream changes it afterwards: when the binary ships the sample
 * at a newer version the copy's origin stamp earns it an upgrade OFFER here,
 * taken through the import door and confirmed first where the copy was
 * edited (decision record 0070). A bundle applied outside the shipped catalog
 * has no tier and is listed on its own rather than guessed into one.
 *
 * EVERY ROW DISCLOSES ITS CLOSURE (owner ask): a fresh repository holds
 * `substrate.reamde.dev/core` and nothing else, so the reader meets this page before they
 * have any vocabulary at all and must be able to see what an import will DO
 * before pressing it. The chevron opens the closure in place — the kinds it
 * adds (linked once they are imported), its functions, agents, triggers and
 * mappings, its version and owned authority, and the authorities it REQUIRES,
 * each marked present or missing.
 *
 * REQUIREMENTS ARE A GATE, not a surprise: `schema.resolveBundle` refuses an
 * install whose `requires:` packages are absent, so the console refuses it
 * first, so the button is disabled with a tooltip naming what to take first. A
 * sample's requirements are shown REHOMED, under this repository's authority,
 * because that is what the server will look for. If the server still refuses
 * (a race), its own problems ride the toast verbatim.
 *
 * The two doors are two endpoints: `…/catalog/{id}/install` for a provider,
 * `…/catalog/{id}/import` for a sample. enable/disable/uninstall are a
 * DIFFERENT lifecycle and keep their own words. */

import { useCallback, useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { DataTableColumn } from "@/components/data-table/data-table"
import {
  BotIcon,
  BoxesIcon,
  BoxIcon,
  CheckIcon,
  CircleArrowUpIcon,
  DownloadIcon,
  FunctionSquareIcon,
  SearchXIcon,
  TriangleAlertIcon,
  ZapIcon,
} from "lucide-react"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { RowDetail } from "@/components/data-table/row-detail"
import { BundleStateBadge, SetupBadge } from "@/components/bundle-state-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import {
  bundleState,
  bundleStatusesQueryOptions,
  refetchBundleStateSoon,
  seedBundleStatus,
  setupCount,
} from "@/lib/api/bundles"
import {
  catalogQueryOptions,
  importBundle,
  installBundle,
  shippedUpgradesQueryOptions,
  takeBundle,
} from "@/lib/api/catalog"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { CORE_PACKAGE } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  ApiError,
  type BundleStatus,
  type KindInfo,
  type ShippedUpgrade,
} from "@/lib/api/types"
import { splitKind } from "@/lib/definition"
import {
  bundleRecordRows,
  bundleSections,
  confirmationOf,
  heldVersions,
  importFailureText,
  installedKindRows,
  lossyStepLines,
  mergeBundles,
  missingRequirements,
  needsConfirmation,
  presentPackages,
  previewFailed,
  requirementsOf,
  stepLines,
  readySuggestedMappings,
  REIMPORT_WARNING,
  requiresHint,
  samplesMappingOnto,
  suggestedMappingHint,
  suggestedMappingsOf,
  upgradeAvailable,
  upgradeBlocked,
  upgradeMotion,
  pendingShippedUpgrades,
  type BundleRow,
  type Requirement,
  type SuggestedMappingRow,
} from "@/lib/bundles"

/** A row's counts: the live status when imported, else the catalog closure's
 * declared closure counts (nothing is live yet, so accounts/rows read 0). The
 * live status counts are optional on the v1 wire — guard each with `?? 0`. */
function counts(row: BundleRow): {
  accounts: number
  functions: number
  kinds: number
  liveRecords: number
} {
  if (row.status) {
    const s = row.status
    return {
      accounts: s.accounts ?? 0,
      functions: s.functions ?? 0,
      kinds: s.kinds ?? 0,
      liveRecords: s.liveRecords ?? 0,
    }
  }
  const r = row.catalog?.closure
  return {
    accounts: 0,
    functions: r?.functions?.length ?? 0,
    kinds: r?.kinds?.length ?? 0,
    liveRecords: 0,
  }
}

function numColumn(
  id: string,
  title: string,
  value: (r: BundleRow) => number
): DataTableColumn<BundleRow> {
  return {
    id,
    accessorFn: value,
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={title} align="right" />
    ),
    cell: ({ row }) => (
      <span className="block text-right data text-muted-foreground">
        {value(row.original).toLocaleString()}
      </span>
    ),
    meta: {
      label: title,
      width: 90,
      headerClassName: "text-right",
      cellClassName: "text-right",
    },
  }
}

/** The row's door, named for its tier: a PROVIDER installs under the authority
 * that publishes it, a SAMPLE imports as yours. Gated by the closure's own
 * `requires:`. A missing requirement is a refusal the server WILL make
 * (schema.resolveBundle), so the button is disabled and its tooltip names what
 * to take first; the trigger is a span, since a disabled button dispatches no
 * pointer events. A refusal that still arrives (the requirement was torn down
 * between the read and the click) surfaces the server's own problems
 * verbatim. */
function TakeButton({
  row,
  missing,
}: {
  row: BundleRow
  missing: Requirement[]
}) {
  const queryClient = useQueryClient()
  const sample = row.tier === "sample"
  const verb = sample ? "Import" : "Install"
  const running = sample ? "Importing…" : "Installing…"
  const taking = useMutation({
    mutationFn: () =>
      takeBundle({
        id: row.catalog?.id ?? row.id,
        tier: row.tier ?? "provider",
      }),
    onSuccess: (status) => {
      toast.add({
        type: "success",
        title: sample
          ? `${row.name} imported as ${status.id}.`
          : `${row.name} installed.`,
      })
      // The door answers with the fresh status, so seed it and this row flips to
      // held immediately, without waiting on the next status probe.
      seedBundleStatus(queryClient, status)
      // It lands schema + wiring the whole console reads, so refresh all, and
      // re-read the bundle surfaces again shortly since the probe-backed reads
      // can lag it.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `${sample ? "Importing" : "Installing"} ${row.name} failed`,
        description: importFailureText(error),
      })
    },
  })

  const blocked = missing.length > 0
  const button = (
    <Button
      variant="outline"
      size="sm"
      disabled={blocked || taking.isPending}
      onClick={(e) => {
        e.stopPropagation()
        taking.mutate()
      }}
    >
      {taking.isPending ? <Spinner className="size-3.5" /> : <DownloadIcon />}
      {taking.isPending ? running : verb}
    </Button>
  )
  if (!blocked) return button
  const hint = requiresHint(missing)
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex cursor-help" />}>
        {button}
        <span className="sr-only">{hint}</span>
      </TooltipTrigger>
      <TooltipContent>{hint}</TooltipContent>
    </Tooltip>
  )
}

/** IMPORT AGAIN: the one action that lands a suggested mapping a first import
 * dropped (decision record 0049) while the shipped closure has not moved. A
 * reader who installs Linear after importing `tasks` would otherwise have
 * nothing to press: the mapping stays `ready` forever and the projection never
 * runs.
 *
 * It CONFIRMS first, because a re-import is not a merge: the batch replaces
 * the package wholesale (decision record 0048), so a kind or a property the
 * reader added since is dropped by it, or the narrowing guard refuses the
 * import while live records still hold the old shape. That cost is the
 * dialog's whole text. Where the server's preview says the copy WAS edited
 * (`discardsEdits`, decision record 0070) the click also sends that
 * preview's `planHash` and `changelogSeq`, which the door requires. */
function ImportAgainButton({
  row,
  ready,
}: {
  row: BundleRow
  ready: SuggestedMappingRow[]
}) {
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const importing = useMutation({
    mutationFn: () =>
      importBundle(row.catalog?.id ?? row.id, confirmationOf(row.upgrade)),
    onSuccess: (status) => {
      setConfirming(false)
      toast.add({
        type: "success",
        title:
          ready.length === 1
            ? `${row.name} re-imported: 1 mapping landed.`
            : `${row.name} re-imported: ${ready.length} mappings landed.`,
      })
      seedBundleStatus(queryClient, status)
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `Importing ${row.name} again failed`,
        description: importFailureText(error),
      })
    },
  })
  const what =
    ready.length === 1
      ? `the ${ready[0].label} mapping`
      : `${ready.length} mappings`
  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              disabled={importing.isPending}
              onClick={(e) => {
                e.stopPropagation()
                setConfirming(true)
              }}
            />
          }
        >
          {importing.isPending ? (
            <Spinner className="size-3.5" />
          ) : (
            <DownloadIcon />
          )}
          {importing.isPending ? "Importing…" : "Import again"}
        </TooltipTrigger>
        <TooltipContent>
          {`Import ${row.name} again to land ${what}. ${REIMPORT_WARNING}`}
        </TooltipContent>
      </Tooltip>
      {confirming && (
        <Dialog
          open
          onOpenChange={(open) =>
            !open && !importing.isPending && setConfirming(false)
          }
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Import {row.name} again?</DialogTitle>
              <DialogDescription>
                {`This lands ${what}, now that the provider each one reads is installed. ` +
                  `Importing again replaces ${row.id} instead of merging into it, so a kind or a property you added is dropped. ` +
                  `It is refused while live records still hold a shape the shipped package no longer declares. ` +
                  `Your records are untouched either way.`}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={importing.isPending}
                onClick={(e) => {
                  e.stopPropagation()
                  setConfirming(false)
                }}
              >
                Cancel
              </Button>
              <Button
                disabled={importing.isPending}
                onClick={(e) => {
                  e.stopPropagation()
                  importing.mutate()
                }}
              >
                {importing.isPending && <Spinner className="size-3.5" />}
                Import again
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

/** Upgrade: take the shipped closure again through the row's own door, which
 * is the upgrade verb: `…/catalog/{id}/install` for a provider, and for a
 * sample `…/catalog/{id}/import`, which lands the closure rehomed over the
 * copy this repository holds (decision record 0070). Offered only when the
 * server's preview says the closure moved AND nothing blocks it; a BLOCKED
 * upgrade renders as UpgradeBlockedChip instead, because the server would
 * refuse it, so the console never offers the click (owner decision: no
 * force).
 *
 * A preview that LOSES something asks first: a lossy plan (decision 0067), or
 * a re-import that replaces a sample copy the reader edited (`discardsEdits`,
 * decision record 0070). The click hands the row to the section's
 * LossyUpgradeDialog, which says what goes and sends the preview's `planHash`
 * and `changelogSeq` as the confirmation, so the consent covers exactly what
 * was shown and the server refuses it once anything moved. The dialog lives
 * in the section rather than in this cell because a catalog refetch rebuilds
 * the table's columns and remounts every cell, which would close a dialog
 * kept here. An upgrade that loses nothing lands on the click, as before. */
function UpgradeButton({
  row,
  onConfirmLoss,
}: {
  row: BundleRow
  onConfirmLoss: (row: BundleRow) => void
}) {
  const queryClient = useQueryClient()
  const upgrade = row.upgrade
  const asks = needsConfirmation(upgrade)
  const upgrading = useMutation({
    mutationFn: () => takeAgain(row),
    onSuccess: (status) => {
      toast.add({
        type: "success",
        title: upgrade?.to
          ? `${row.name} upgraded to ${upgrade.to}.`
          : `${row.name} upgraded.`,
      })
      seedBundleStatus(queryClient, status)
      // The upgrade lands schema the whole console reads, and the catalog's
      // preview must re-read as current: refresh everything.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `Upgrading ${row.name} failed`,
        description: importFailureText(error),
      })
    },
  })
  const motion = upgrade ? upgradeMotion(upgrade) : ""
  const steps = stepLines(upgrade)
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            disabled={upgrading.isPending}
            onClick={(e) => {
              e.stopPropagation()
              if (asks) onConfirmLoss(row)
              else upgrading.mutate()
            }}
          />
        }
      >
        {upgrading.isPending ? (
          <Spinner className="size-3.5" />
        ) : (
          <CircleArrowUpIcon />
        )}
        {upgrading.isPending ? "Upgrading…" : "Upgrade"}
      </TooltipTrigger>
      {(motion || steps.length > 0) && (
        <TooltipContent className="max-w-96">
          <div className="space-y-1">
            {motion && <p>{motion}</p>}
            {steps.map((s) => (
              <p key={s}>{s}</p>
            ))}
          </div>
        </TooltipContent>
      )}
    </Tooltip>
  )
}

/** Take a row's shipped closure again through its own door, with the consent
 * its preview needs: the install verb for a provider, the import verb for a
 * sample, whose closure lands rehomed over the copy this repository holds
 * (decision record 0070). */
function takeAgain(row: BundleRow): Promise<BundleStatus> {
  const door = row.tier === "sample" ? importBundle : installBundle
  return door(row.catalog?.id ?? row.id, confirmationOf(row.upgrade))
}

/** The consent to an upgrade that loses something (decisions 0067 and 0070),
 * rendered by the section for the row the reader clicked, so it outlives the
 * table's re-render. It reads the row's CURRENT preview: after a `409`
 * (records changed since the preview was read, so the server no longer counts
 * that plan) the catalog is read again, the dialog stays open, says so, and
 * its next click confirms the fresh `planHash` and `changelogSeq`, never the
 * stale pair again. */
function LossyUpgradeDialog({
  row,
  onClose,
}: {
  row: BundleRow | undefined
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  // staleFor is the row whose last confirmation named a plan the server no
  // longer counts, so the notice below belongs to that row alone: the dialog
  // stays mounted with no row once closed, and keying on the id keeps a later
  // row from opening as "changed".
  const [staleFor, setStaleFor] = useState<string | null>(null)
  const upgrade = row?.upgrade
  const stale = row !== undefined && staleFor === row.id
  const upgrading = useMutation({
    mutationFn: () => {
      if (!row || !needsConfirmation(upgrade)) {
        throw new Error("no plan to confirm")
      }
      return takeAgain(row)
    },
    onSuccess: (status) => {
      setStaleFor(null)
      onClose()
      toast.add({
        type: "success",
        title: upgrade?.to
          ? `${row?.name} upgraded to ${upgrade.to}.`
          : `${row?.name} upgraded.`,
      })
      seedBundleStatus(queryClient, status)
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      // A 409 says records changed since the preview; a 403 `lossy` at the
      // same head says the plan itself reads differently now (the server
      // changed under the same records). Either way the consent named a
      // plan the server no longer counts: read the preview again.
      if (
        error instanceof ApiError &&
        (error.status === 409 || error.code === "lossy")
      ) {
        setStaleFor(row?.id ?? null)
        void queryClient.invalidateQueries({
          queryKey: catalogQueryOptions.queryKey,
        })
        return
      }
      toast.add({
        type: "error",
        title: `Upgrading ${row?.name} failed`,
        description: importFailureText(error),
      })
    },
  })
  // A re-read plan that removes nothing and replaces no edits has nothing to
  // consent to: the dialog closes and the row's Upgrade button takes it
  // unconfirmed, rather than sitting open with no steps and a dead button.
  const lossless = Boolean(row && !needsConfirmation(upgrade))
  useEffect(() => {
    if (!lossless) return
    onClose()
    toast.add({
      type: "success",
      title: `Upgrading ${row?.name} now loses nothing`,
      description: "Press Upgrade to take it.",
    })
  }, [lossless, onClose, row?.name])
  if (!row || lossless) return null
  const losses = lossyStepLines(upgrade)
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (open || upgrading.isPending) return
        setStaleFor(null)
        onClose()
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {upgrade?.discardsEdits
              ? `Upgrade ${row.name} and replace your edits?`
              : `Upgrade ${row.name} and remove values?`}
          </DialogTitle>
          <DialogDescription>
            {(upgrade?.discardsEdits
              ? `You edited ${row.id} since you imported it. Upgrading replaces the package with the shipped one, so your edits go with it. Your records are untouched. `
              : "") +
              (upgrade?.lossy
                ? `Upgrading rewrites ${upgrade?.work ?? 0} live ${
                    upgrade?.work === 1 ? "record" : "records"
                  } and removes some values from them. ` +
                  `The removed values stay in the changelog. `
                : "") +
              `This confirms exactly the plan below. If anything is written before it lands, the plan is read again.`}
          </DialogDescription>
        </DialogHeader>
        {stale && (
          <p role="status" className="text-sm text-warning">
            Records changed since this plan was read, so the upgrade was
            refused. Check the plan below and confirm it again.
          </p>
        )}
        <ul className="space-y-1 text-sm">
          {losses.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ul>
        <DialogFooter>
          <Button
            variant="outline"
            disabled={upgrading.isPending}
            onClick={(e) => {
              e.stopPropagation()
              setStaleFor(null)
              onClose()
            }}
          >
            Cancel
          </Button>
          <Button
            disabled={upgrading.isPending || !upgrade?.planHash}
            onClick={(e) => {
              e.stopPropagation()
              upgrading.mutate()
            }}
          >
            {upgrading.isPending && <Spinner className="size-3.5" />}
            {upgrade?.discardsEdits
              ? "Upgrade and replace my edits"
              : "Upgrade and remove values"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** A blocked upgrade, stated instead of offered: the shipped closure moved,
 * but re-importing it would strand live records, and the server refuses that
 * (refuse-breakage). The chip's tooltip carries the server's own guard lines,
 * which name the kind, the property and the count: the reader's migration
 * instructions. */
function UpgradeBlockedChip({ row }: { row: BundleRow }) {
  const blockers = row.upgrade?.blockers ?? []
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex cursor-help" />}>
        <Badge
          variant="outline"
          className="gap-1 border-warning/40 font-normal text-warning"
        >
          <TriangleAlertIcon className="size-3 shrink-0" />
          <span className="data">upgrade blocked</span>
        </Badge>
        <span className="sr-only">{blockers.join("; ")}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-96">
        <div className="space-y-1">
          {blockers.map((b) => (
            <p key={b}>{b}</p>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

function buildColumns(
  requirements: (row: BundleRow) => Requirement[],
  mappings: (row: BundleRow) => SuggestedMappingRow[],
  confirmLoss: (row: BundleRow) => void
): DataTableColumn<BundleRow>[] {
  return [
    {
      id: "bundle",
      accessorFn: (r) => r.name,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="bundle" />
      ),
      cell: ({ row }) => (
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate font-medium">{row.original.name}</span>
          </div>
          <div
            className="truncate data text-xs text-muted-foreground"
            title={row.original.catalog?.description || row.original.authority}
          >
            {row.original.catalog?.description || row.original.authority}
          </div>
        </div>
      ),
      meta: { label: "bundle", size: { min: 220, max: 460, weight: 1.5 } },
    },
    {
      id: "state",
      accessorFn: (r) => (r.status ? bundleState(r.status) : "not taken"),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="state" />
      ),
      // An imported bundle shows its own runtime lifecycle (enabled /
      // disabled / uninstalled) with the setup chip BESIDE it when steps
      // stand; one that has never been imported has no lifecycle to show, only
      // the invitation — and, when its closure declares against vocabulary
      // this repository lacks, what blocks it.
      cell: ({ row }) => {
        const missing = row.original.installed
          ? []
          : missingRequirements(requirements(row.original))
        return (
          <div className="min-w-0">
            {row.original.status ? (
              <span className="inline-flex flex-wrap items-center gap-1.5">
                <BundleStateBadge state={bundleState(row.original.status)} />
                <SetupBadge count={setupCount(row.original.status)} />
              </span>
            ) : (
              <Badge variant="outline" className="gap-1.5 font-normal">
                <span className="size-1.5 rounded-full bg-muted-foreground/40" />
                <span className="data">
                  {row.original.tier === "sample"
                    ? "not imported"
                    : "not installed"}
                </span>
              </Badge>
            )}
            {/* A sample is rewritten onto this repository's authority on the
                way in, so the row says the identity it will land under before
                the button is pressed, since the reader is about to own it. */}
            {!row.original.status && row.original.tier === "sample" && (
              <div
                className="truncate pt-0.5 data text-xs text-muted-foreground"
                title={`Importing lands ${row.original.id}, yours to edit`}
              >
                lands as {row.original.id}
              </div>
            )}
            {missing.length > 0 && (
              <div
                className="truncate pt-0.5 data text-xs text-warning"
                title={requiresHint(missing)}
              >
                needs {missing.map((r) => r.package).join(", ")}
              </div>
            )}
            {row.original.upgrade && upgradeAvailable(row.original) && (
              <div className="truncate pt-0.5 data text-xs text-muted-foreground">
                update {upgradeMotion(row.original.upgrade)}
              </div>
            )}
          </div>
        )
      },
      meta: { label: "state", width: 170 },
    },
    numColumn("accounts", "accounts", (r) => counts(r).accounts),
    numColumn("functions", "functions", (r) => counts(r).functions),
    numColumn("kinds", "kinds", (r) => counts(r).kinds),
    numColumn("liveRecords", "live rows", (r) => counts(r).liveRecords),
    {
      id: "action",
      enableSorting: false,
      enableHiding: false,
      header: () => <span className="sr-only">action</span>,
      cell: ({ row }) =>
        row.original.installed ? (
          upgradeAvailable(row.original) || upgradeBlocked(row.original) ? (
            <div className="flex justify-end">
              {upgradeBlocked(row.original) ? (
                <UpgradeBlockedChip row={row.original} />
              ) : (
                <UpgradeButton row={row.original} onConfirmLoss={confirmLoss} />
              )}
            </div>
          ) : row.original.tier === "sample" &&
            readySuggestedMappings(mappings(row.original)).length > 0 ? (
            // A held SAMPLE whose shipped closure has not moved is offered no
            // upgrade, so this is the one action that lands a mapping the
            // first import dropped: re-import the closure, now that the
            // provider it reads is here.
            //
            // THE TIER IS PART OF THE GATE. A provider row's mappings are the
            // INBOUND ones (which samples project onto it), so without this a
            // held provider with a ready sample mapping would offer to import
            // ITSELF, and the import door refuses a provider id.
            <div className="flex justify-end">
              <ImportAgainButton
                row={row.original}
                ready={readySuggestedMappings(mappings(row.original))}
              />
            </div>
          ) : null
        ) : row.original.catalog ? (
          <div className="flex justify-end">
            <TakeButton
              row={row.original}
              missing={missingRequirements(requirements(row.original))}
            />
          </div>
        ) : null,
      meta: {
        label: "action",
        width: 120,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
  ]
}

// ── the disclosure: what an import will actually do ─────────────────────────

/** One labelled line of the disclosure grid. */
function Line({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <>
      <dt className="pt-0.5 text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 flex-wrap items-center gap-1">{children}</dd>
    </>
  )
}

/** The shipped-record kinds this preview groups by, in reading order: the
 * declarations first, then the data rows an install writes. A record of any
 * other kind falls into the trailing `records` line, so a bundle shipping
 * something new is previewed rather than dropped. */
const RECORD_KINDS = [
  {
    kind: `${CORE_PACKAGE}/function`,
    label: "functions",
    icon: FunctionSquareIcon,
  },
  { kind: `${CORE_PACKAGE}/agent`, label: "agents", icon: BotIcon },
  {
    kind: `${CORE_PACKAGE}/recordmapping`,
    label: "mappings",
    icon: BoxesIcon,
  },
  { kind: `${CORE_PACKAGE}/trigger`, label: "triggers", icon: ZapIcon },
] as const

/** The row's closure, opened in place: what it adds, what it needs, and what it
 * IS (sample or provider) — everything the reader needs to decide before
 * the import, in the table's own voice. */
function BundleDisclosure({
  row,
  requirements,
  mappings,
  kinds,
}: {
  row: BundleRow
  requirements: Requirement[]
  /** The suggested mappings this row is about, from whichever side it sits on:
   * a sample's own, or the samples that carry one onto this provider
   * (decision record 0049). */
  mappings: SuggestedMappingRow[]
  kinds: KindInfo[]
}) {
  const catalog = row.catalog
  const inputs = row.status?.inputs
  const kindRows = useMemo(
    () => installedKindRows({ id: row.id, inputs }, kinds, catalog),
    [row.id, inputs, kinds, catalog]
  )
  const records = useMemo(() => bundleRecordRows(catalog), [catalog])
  const missing = missingRequirements(requirements)

  return (
    <RowDetail>
      {catalog?.description && <p>{catalog.description}</p>}
      <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1.5">
        <Line label="authority">
          <span className="data text-muted-foreground">{row.authority}</span>
          {catalog?.version ? (
            <span className="data text-muted-foreground">
              · {catalog.version}
            </span>
          ) : null}
        </Line>
        {row.upgrade?.available && (
          <Line label="upgrade">
            <span className="data">{upgradeMotion(row.upgrade)}</span>
            {(row.upgrade.changes ?? []).map((ch) => (
              <span
                key={`${ch.kind}:${ch.id}`}
                className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                title={
                  ch.to
                    ? ch.from
                      ? `${ch.id}: ${ch.from} → ${ch.to}`
                      : `${ch.id}: new at ${ch.to}`
                    : `${ch.id}: removed by this upgrade`
                }
              >
                {ch.kind} {splitKind(ch.id).name}
              </span>
            ))}
            {stepLines(row.upgrade).map((line) => (
              <span key={line} className="data text-muted-foreground">
                {line}
              </span>
            ))}
          </Line>
        )}
        <Line label="tier">
          <span>
            {row.tier === "provider"
              ? "Provider. It installs under the authority that publishes it, and each change its publisher ships arrives here as an upgrade."
              : row.tier === "sample"
                ? `Sample. Importing lands it as ${row.id}, yours to edit. Nothing upstream changes it afterwards.`
                : "Applied directly. This bundle is not in the catalog, so there is nothing to preview."}
          </span>
        </Line>
        {catalog?.inputs && Object.keys(catalog.inputs).length > 0 && (
          <Line label="inputs">
            {Object.entries(catalog.inputs).map(([name, input]) => (
              <span
                key={name}
                className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                title={
                  input.description
                    ? `${input.kind}\n\n${input.description}`
                    : input.kind
                }
              >
                {name}
              </span>
            ))}
          </Line>
        )}
        {requirements.length > 0 && (
          <Line label="requires">
            {requirements.map((req) => (
              <span
                key={req.package}
                className={cn(
                  "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 data",
                  req.present
                    ? "bg-background text-muted-foreground"
                    : "border-warning/40 text-warning"
                )}
                title={
                  req.present
                    ? `${req.package} is imported`
                    : `${req.package} is not imported yet. Import it first`
                }
              >
                {req.present ? (
                  <CheckIcon className="size-3 shrink-0" />
                ) : (
                  <TriangleAlertIcon className="size-3 shrink-0" />
                )}
                {req.package}
                <span className="sr-only">
                  {req.present ? " imported" : " missing"}
                </span>
              </span>
            ))}
          </Line>
        )}
        {mappings.length > 0 && (
          <Line label="mappings">
            {mappings.map((m) => (
              <span
                key={`${m.sample}:${m.mapping.id}`}
                className={cn(
                  "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 data",
                  m.landed
                    ? "bg-background text-muted-foreground"
                    : "border-warning/40 text-warning"
                )}
                title={m.title}
              >
                <BoxesIcon className="size-3 shrink-0" />
                {row.tier === "provider"
                  ? `${m.sampleWord}: ${m.label}`
                  : m.label}
                <span>{m.state}</span>
              </span>
            ))}
          </Line>
        )}
        <Line label="kinds">
          {kindRows.length ? (
            kindRows.map((k) => {
              // The host's own roles ride the hover, not a second chip: a kind
              // a declared input resolves records of, and the `account` kind
              // the connect flow writes tokens onto, are the two the reader
              // must be able to tell from ordinary vocabulary.
              const named =
                k.role === "input"
                  ? `${k.identity} (its records satisfy a declared input)`
                  : k.role === "account"
                    ? `${k.identity} (the account record kind)`
                    : k.identity
              // What the kind is, on the same hover: a reader deciding on an
              // import should not have to install it to find out.
              const title = k.description
                ? `${named}\n\n${k.description}`
                : named
              return k.authority && k.package && k.name ? (
                <Link
                  key={k.identity}
                  to="/data/$authority/$pkg/$name"
                  params={{
                    authority: k.authority,
                    pkg: k.package,
                    name: k.name,
                  }}
                  className="rounded border bg-background px-1.5 py-0.5 data underline-offset-4 hover:underline"
                  title={title}
                  onClick={(e) => e.stopPropagation()}
                >
                  {k.name}
                </Link>
              ) : (
                // Not imported yet (or unreconciled): the kind exists on paper
                // only, so it names itself and links nowhere.
                <span
                  key={k.identity}
                  className="rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                  title={title}
                >
                  {k.name}
                </span>
              )
            })
          ) : (
            <span className="text-muted-foreground">none</span>
          )}
        </Line>
        {RECORD_KINDS.map(({ kind, label, icon: Icon }) => {
          const members = records.filter((r) => r.kind === kind)
          if (!members.length) return null
          return (
            <Line key={kind} label={label}>
              {members.map((m) => (
                <span
                  key={m.id}
                  className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                  title={m.id}
                >
                  <Icon className="size-3 shrink-0" />
                  {m.name}
                </span>
              ))}
            </Line>
          )
        })}
        {(() => {
          // Everything the bundle ships that is not one of the four above —
          // the llm example's provider rows, say. Grouped under one line
          // because the set is open: a bundle may ship a record of any kind.
          const grouped = new Set<string>(RECORD_KINDS.map((r) => r.kind))
          const rest = records.filter((r) => !grouped.has(r.kind))
          if (!rest.length) return null
          return (
            <Line label="records">
              {rest.map((m) => (
                <span
                  key={`${m.kind}:${m.id}`}
                  className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                  title={`${m.kind}/${m.id}`}
                >
                  <BoxIcon className="size-3 shrink-0" />
                  {m.name}
                </span>
              ))}
            </Line>
          )
        })()}
      </dl>
      {missing.length > 0 && (
        <p className="text-warning">{requiresHint(missing)}</p>
      )}
      {suggestedMappingHint(mappings, row.installed) && (
        <p className="text-warning">
          {suggestedMappingHint(mappings, row.installed)}
        </p>
      )}
      {(row.upgrade?.blockers?.length ?? 0) > 0 && (
        <div className="space-y-1 text-warning">
          <p>
            {previewFailed(row)
              ? "The upgrade could not be previewed, so it is not offered yet."
              : "The upgrade is blocked. Live records still hold a shape it would drop."}
          </p>
          {row.upgrade?.blockers?.map((b) => (
            <p key={b} className="data text-xs">
              {b}
            </p>
          ))}
        </div>
      )}
      {!catalog && (
        <p className="text-muted-foreground">
          This bundle was applied directly. The catalog does not ship it, so
          only what this repository already knows is listed.
        </p>
      )}
      {row.installed && (
        <span className="text-muted-foreground">
          <Link
            to="/registry/$id"
            params={{ id: row.id }}
            className="underline-offset-4 hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            Open bundle
          </Link>
        </span>
      )}
    </RowDetail>
  )
}

/** One tier's table: its own heading, its own column preferences, its own
 * empty state. Two of these are the page (decision record 0048), because the
 * two tiers answer different questions: what connects to a service, and what
 * vocabulary to start from. */
function BundleSection({
  title,
  description,
  rows,
  prefsKey,
  emptyTitle,
  emptyDescription,
  requirements,
  mappings,
  kinds,
  onOpen,
}: {
  title: string
  description: string
  rows: BundleRow[]
  prefsKey: string
  emptyTitle: string
  emptyDescription: string
  requirements: (row: BundleRow) => Requirement[]
  mappings: (row: BundleRow) => SuggestedMappingRow[]
  kinds: KindInfo[]
  onOpen: (row: BundleRow) => void
}) {
  // The row whose lossy upgrade is being confirmed, by id: the dialog reads
  // the row's current preview from `rows`, so a refetch after a stale
  // confirmation shows the fresh plan (LossyUpgradeDialog).
  const [lossyID, setLossyID] = useState<string | null>(null)
  const confirmLoss = useCallback((row: BundleRow) => setLossyID(row.id), [])
  // Stable, because the dialog's lossless-re-read effect lists it as a
  // dependency.
  const closeLoss = useCallback(() => setLossyID(null), [])
  const columns = useMemo(
    () => buildColumns(requirements, mappings, confirmLoss),
    [requirements, mappings, confirmLoss]
  )
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.id,
    prefsKey,
  })
  const held = rows.filter((r) => r.installed).length
  return (
    <section className="pb-6">
      <div className="flex items-end justify-between gap-3 px-6 pt-4 pb-2">
        <div>
          <h2 className="text-sm font-semibold">{title}</h2>
          <p className="text-xs text-muted-foreground">{description}</p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">
            {held.toLocaleString()} of {rows.length.toLocaleString()}
          </span>
          <DataTableViewOptions table={table} />
        </div>
      </div>
      <DataTable
        table={table}
        onRowClick={(row) => onOpen(row)}
        renderExpanded={(row) => (
          <BundleDisclosure
            row={row}
            requirements={requirements(row)}
            mappings={mappings(row)}
            kinds={kinds}
          />
        )}
        empty={
          <Empty className="py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BoxesIcon />
              </EmptyMedia>
              <EmptyTitle>{emptyTitle}</EmptyTitle>
              <EmptyDescription>{emptyDescription}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />
      <LossyUpgradeDialog
        row={rows.find((r) => r.id === lossyID)}
        onClose={closeLoss}
      />
    </section>
  )
}

export function RegistryPage() {
  const navigate = useNavigate()
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  // The boot upgrade's preview, for the one package no catalog entry carries:
  // core. An available entry has not landed here: refused with blockers, or
  // admitted and waiting for the server to start again. Not waited on and not
  // fatal: a read that fails leaves the notice off, the sections stand.
  const shipped = useQuery(shippedUpgradesQueryOptions)
  const pending = useMemo(
    () => pendingShippedUpgrades(shipped.data ?? []),
    [shipped.data]
  )
  // The repository's own record answers ONE question: the authority this
  // repository owns, which is where an imported sample lands (decision records
  // 0046 and 0048) and so what a sample row previews.
  const repository = useQuery(repositoryQueryOptions)
  // The kind registry answers two more: which packages this repository already
  // holds (a requirement is met when its package is live), and where a
  // closure's kinds actually browse once they land.
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(() => registry.data ?? [], [registry.data])
  const home = repository.data?.authority ?? ""

  const allRows = useMemo(
    () => mergeBundles(statuses.data ?? [], catalog.data ?? [], home),
    [statuses.data, catalog.data, home]
  )
  // Presence is computed over EVERY row, never one section's: a package in the
  // other section still satisfies a requirement.
  const present = useMemo(
    () => presentPackages(allRows, kinds),
    [allRows, kinds]
  )
  // A requirement is also met only at or above the floor the closure puts
  // under it (`requiresAtLeast`, decision record 0070), read against the
  // version each held bundle's status reports.
  const versions = useMemo(() => heldVersions(allRows), [allRows])
  const requirements = useMemo(() => {
    const byId = new Map<string, Requirement[]>()
    for (const row of allRows) {
      byId.set(row.id, requirementsOf(row, present, versions))
    }
    return (row: BundleRow) => byId.get(row.id) ?? []
  }, [allRows, present, versions])
  // The suggested mappings, from whichever side a row sits on: a sample's own
  // (what an import would project, and what is waiting), a provider's inbound
  // (which samples are waiting for exactly this install). Computed over EVERY
  // row, because the two sides are in the two different sections.
  const mappings = useMemo(() => {
    const byId = new Map<string, SuggestedMappingRow[]>()
    for (const row of allRows) {
      byId.set(
        row.id,
        row.tier === "provider"
          ? samplesMappingOnto(row, allRows)
          : suggestedMappingsOf(row)
      )
    }
    return (row: BundleRow) => byId.get(row.id) ?? []
  }, [allRows])
  const sections = useMemo(() => bundleSections(allRows), [allRows])
  const heldCount = allRows.filter((r) => r.installed).length

  // The kind registry is a read the whole console shares (the sidebar holds it
  // warm); waiting for it here keeps a requirement from reading as missing for
  // one frame and disabling a button that is perfectly legal. The repository
  // read is waited on for the same reason: without the authority a sample row
  // would preview the wrong identity for a frame.
  if (
    statuses.isPending ||
    catalog.isPending ||
    registry.isPending ||
    repository.isPending
  )
    return <RegistrySkeleton />

  if (statuses.isError || catalog.isError) {
    const error = statuses.error ?? catalog.error
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The bundles didn't load</EmptyTitle>
            <EmptyDescription>{error?.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                void statuses.refetch()
                void catalog.refetch()
              }}
            >
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  // Only a bundle this repository holds has a detail page (it reads runtime
  // status); a catalog closure is taken from its row button and read from the
  // chevron's disclosure.
  const open = (row: BundleRow) => {
    if (row.status)
      void navigate({ to: "/registry/$id", params: { id: row.id } })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <h1 className="text-lg font-semibold">Registry</h1>
        <p className="text-xs text-muted-foreground">
          {heldCount.toLocaleString()} of {allRows.length.toLocaleString()}{" "}
          taken
        </p>
        <p className="pt-0.5 text-xs text-muted-foreground">
          A new repository holds{" "}
          <span className="data">substrate.reamde.dev/core</span> and nothing
          else. Every other kind comes from here. Expand a row to see what a
          bundle adds.
        </p>
        {pending.map((item) => (
          <PendingUpgradeNotice key={item.package} item={item} />
        ))}
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <BundleSection
          title="Providers"
          description="Packages a publisher owns. Installing one keeps the publisher's authority, and its upgrades arrive here."
          rows={sections.providers}
          prefsKey="registry.providers"
          emptyTitle="No providers"
          emptyDescription="This substrate ships no providers."
          requirements={requirements}
          mappings={mappings}
          kinds={kinds}
          onOpen={open}
        />
        <BundleSection
          title="Samples"
          description={
            home
              ? `Kinds to copy. Importing one lands them under ${home}, yours to edit.`
              : "Kinds to copy. Importing one lands them under this repository's own authority, yours to edit."
          }
          rows={sections.samples}
          prefsKey="registry.samples"
          emptyTitle="No samples"
          emptyDescription="This substrate ships no samples."
          requirements={requirements}
          mappings={mappings}
          kinds={kinds}
          onOpen={open}
        />
        {sections.applied.length > 0 && (
          <BundleSection
            title="Applied directly"
            description="Bundles applied outside the catalog. There is nothing to preview and no upgrade to offer."
            rows={sections.applied}
            prefsKey="registry.applied"
            emptyTitle="Nothing applied directly"
            emptyDescription="Every bundle came from the catalog."
            requirements={requirements}
            mappings={mappings}
            kinds={kinds}
            onOpen={open}
          />
        )}
      </div>
    </div>
  )
}

/** A shipped package whose upgrade has not landed here, stated where the
 * upgrades live. Two states, told apart by the blockers. REFUSED: the boot
 * upgrade ran and the refuse-breakage guards refused it, so the stored
 * declarations stand; the lines are the server's own, naming the kind, the
 * property and the count, which is what to migrate. ADMITTED: nothing blocks
 * any more (or nothing ever did), but the boot upgrade runs only at a
 * repository's first open under a binary, so the newer declarations land when
 * the server starts again. Without the second state the notice would vanish
 * the moment the last blocking record is migrated, with the store still old
 * and nobody told a restart is what finishes it. */
function PendingUpgradeNotice({ item }: { item: ShippedUpgrade }) {
  const motion = upgradeMotion(item.upgrade)
  const blockers = item.upgrade.blockers ?? []
  const refused = blockers.length > 0
  return (
    <div
      role="alert"
      className="mt-3 max-w-3xl rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-xs"
    >
      <p className="flex items-start gap-1.5 text-warning">
        <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
        <span>
          The upgrade of <span className="data">{item.package}</span>
          {motion ? (
            <>
              {" "}
              (<span className="data">{motion}</span>)
            </>
          ) : null}{" "}
          {refused
            ? "was refused when the server started. Fix what the lines below name, then start the server again."
            : "lands when the server starts again. Until then this repository runs on the kinds it already stores."}
        </span>
      </p>
      {refused && (
        <div className="mt-1 space-y-0.5 pl-5">
          {blockers.map((b) => (
            <p key={b} className="data text-muted-foreground">
              {b}
            </p>
          ))}
        </div>
      )}
      {stepLines(item.upgrade).length > 0 && (
        <div className="mt-1 space-y-0.5 pl-5">
          {stepLines(item.upgrade).map((line) => (
            <p key={line} className="data text-muted-foreground">
              {line}
            </p>
          ))}
        </div>
      )}
    </div>
  )
}

function RegistrySkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <Skeleton className="h-6 w-32" />
        <Skeleton className="mt-1.5 h-3.5 w-56" />
      </div>
      <div className="min-h-0 flex-1 overflow-hidden px-6 pt-4">
        {Array.from({ length: 5 }, (_, i) => (
          <div
            key={i}
            className="flex h-12 items-center gap-6 border-b last:border-0"
          >
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-4 w-24" />
            <Skeleton className="ml-auto h-4 w-16" />
          </div>
        ))}
      </div>
    </div>
  )
}
