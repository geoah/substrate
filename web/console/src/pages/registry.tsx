/** Registry (`/registry`): every bundle this substrate knows: the ones this
 * repository holds, with their runtime state (from the computed status
 * endpoint), and the ones in the shipped catalog it does not have yet.
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
 * EVERY ROW SAYS WHAT IT ADDS (owner ask): a fresh repository holds
 * `substrate.reamde.dev/core` and nothing else, so the reader meets this page
 * before they have any vocabulary at all, and the question is what an import
 * will give them. The chevron opens that in place, as READABLE SECTIONS, each
 * a heading over a list of names with the prose each declaration carries:
 * kinds (linked once they are here), traits, functions, agents, triggers,
 * the settings and secrets it ships, and what it requires. A counted line
 * stands in for the rest, because the set of records a bundle may ship is
 * open.
 *
 * REQUIREMENTS NEST AND ARRIVE TOGETHER. The wire's `requires` is direct
 * only, so the chain is walked here and shown as a tree; the button takes the
 * whole of it, leaves first, one call each, and stops at the first refusal.
 * Nothing is disabled: a reader who presses the one button gets the thing
 * they asked for.
 *
 * The two doors are two endpoints: `…/catalog/{id}/install` for a provider,
 * `…/catalog/{id}/import` for a sample. enable/disable/uninstall are a
 * DIFFERENT lifecycle and keep their own words. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { DataTableColumn } from "@/components/data-table/data-table"
import {
  BoxesIcon,
  CheckIcon,
  CircleArrowUpIcon,
  DownloadIcon,
  SearchXIcon,
  TriangleAlertIcon,
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
  fetchCatalogItem,
  importBundle,
  installBundle,
  shippedUpgradesQueryOptions,
} from "@/lib/api/catalog"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { CORE_PACKAGE } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  ApiError,
  type BundleStatus,
  type CatalogItem,
  type KindInfo,
  type ShippedUpgrade,
} from "@/lib/api/types"
import {
  bundleRecordRows,
  bundleSections,
  chainHint,
  closureRows,
  confirmationOf,
  heldVersions,
  importFailureText,
  installedKindRows,
  lossyStepLines,
  mappingLinksSentence,
  mergeBundles,
  importPlan,
  missingChain,
  needsConfirmation,
  presentPackages,
  previewFailed,
  requirementTree,
  stepLines,
  triggerRows,
  upgradeAvailable,
  upgradeBlocked,
  upgradeMotion,
  pendingShippedUpgrades,
  type BundleRow,
  type ClosureRow,
  type RequirementNode,
} from "@/lib/bundles"

/** A row's counts: the live status when the bundle is here, else what the
 * shipped catalog declares. KINDS and FUNCTIONS only: the account count is
 * the number of connected accounts, which is zero for everything nobody has
 * connected yet, and a live row count answers a question nobody asked of a
 * registry (owner ruling). The live status counts are optional on the v1
 * wire, so each is guarded. */
function counts(row: BundleRow): { functions: number; kinds: number } {
  if (row.status) {
    return {
      functions: row.status.functions ?? 0,
      kinds: row.status.kinds ?? 0,
    }
  }
  const closure = row.catalog?.closure
  return {
    functions: closure?.functions?.length ?? 0,
    kinds: closure?.kinds?.length ?? 0,
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
 * that publishes it, a SAMPLE imports as this repository's own.
 *
 * IT TAKES THE WHOLE CHAIN. A bundle is refused while anything it declares
 * against is absent, and the missing thing usually declares against something
 * else in turn, so the button imports the missing packages leaves first, one
 * call each, and the bundle last. Sequential because each admission is
 * refused until the one before it has landed; it stops at the first refusal
 * and says which bundle refused, since "the import failed" with three calls
 * in flight names nothing.
 *
 * Nothing is ever disabled here: a disabled button with a sentence explaining
 * what to press instead is a reader doing the machine's work (owner ruling).
 *
 * A package this repository holds below the floor the closure puts under it
 * (`requiresAtLeast`, decision record 0070) is in the chain too, and taking it
 * again REPLACES the copy rather than merging into it (decision record 0048),
 * so a copy the reader has edited is confirmed first, in the same words the
 * upgrade uses. */
function TakeButton({
  row,
  chain,
}: {
  row: BundleRow
  chain: RequirementNode[]
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const sample = row.tier === "sample"
  const verb = sample ? "Import" : "Install"
  const plan = useMemo(() => importPlan(row, chain), [row, chain])
  // The bundles whose replacement the reader has agreed to, by id. A REF, not
  // state: the dialog's confirm click runs the mutation in the same handler
  // that records the consent, and a state write is not visible to it yet.
  const consented = useRef(new Set<string>())
  // The bundles the dialog is asking about right now, or null. Set before the
  // run from the previews the page holds, and again mid-run when a re-read
  // preview turns out to replace something nobody agreed to.
  const [asking, setAsking] = useState<BundleRow[] | null>(null)
  const taking = useMutation({
    mutationFn: async () => {
      let landed: BundleStatus | undefined
      for (const bundle of plan.bundles) {
        // RE-READ THE PREVIEW, one bundle at a time. A confirmation names one
        // plan at one changelog head, and the step before this one moved the
        // head, so the token the list was read with is refused (engine
        // convert.go). A fresh read is also the only way to see that this
        // step has BECOME lossy since the page loaded.
        let fresh: CatalogItem
        try {
          fresh = await fetchCatalogItem(bundle.catalog?.id ?? bundle.id)
        } catch (error) {
          throw new ChainFailure(bundle.name, error)
        }
        if (
          needsConfirmation(fresh.upgrade) &&
          !consented.current.has(bundle.id)
        ) {
          throw new NeedsConsent(bundle)
        }
        try {
          landed = await takeAgain(bundle, fresh.upgrade)
        } catch (error) {
          throw new ChainFailure(bundle.name, error)
        }
        // Seeded as each one lands, not at the end: a chain that refuses
        // halfway has still imported everything before the refusal, and rows
        // that read as available afterwards would be lying.
        seedBundleStatus(queryClient, landed)
      }
      // The last door answers with the row's own landed status, which is what
      // the toast names and where the setup handoff goes.
      return landed!
    },
    onSuccess: (landed) => {
      setAsking(null)
      toast.add({
        type: "success",
        title:
          plan.bundles.length === 1
            ? sample
              ? // A sample lands under THIS repository's authority, which is
                // not the id the reader clicked, so the toast says where it
                // went (decision record 0048).
                `${row.name} imported as ${landed.id}.`
              : `${row.name} installed.`
            : `${row.name} and ${plan.bundles.length - 1} ${
                plan.bundles.length === 2 ? "package" : "packages"
              } it needs are here.`,
      })
      // A bundle that landed with an empty required setting cannot run until
      // somebody fills it in, so the import hands the reader straight to the
      // form. The id is the LANDED one the door answered with: a sample's is
      // rehomed. Nothing to fill in leaves the reader on the list.
      if ((landed.setup ?? []).some((item) => item.code === "setting")) {
        void navigate({
          to: "/registry/$id",
          params: { id: landed.id },
          hash: "setup",
        })
      }
      // The doors land vocabulary and wiring the whole console reads, and the
      // probe-backed reads can lag them: refresh everything, then again.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      // Whatever ran before the stop has landed, so the rows are read again
      // either way: the ones that imported must stop offering an import.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
      if (error instanceof NeedsConsent) {
        setAsking([error.bundle])
        return
      }
      const failed = error instanceof ChainFailure ? error : undefined
      toast.add({
        type: "error",
        title: `${sample ? "Importing" : "Installing"} ${failed?.bundle ?? row.name} failed`,
        description: importFailureText(failed?.cause ?? error),
      })
    },
  })
  // "all" is about what the row NEEDS, not about what the plan managed to
  // order: a refused chain still has more missing than this one bundle, and a
  // button that says "Import" and then refuses for three other packages reads
  // as a bug in the button.
  const label = missingChain(chain).length > 0 ? `${verb} all` : verb
  const start = () => {
    // The refusal is stated on the press rather than by grey-ing the button:
    // a cycle and a package the catalog does not ship are both things the
    // reader can do nothing about here, and both are sentences, not states.
    if (plan.refusal) {
      toast.add({
        type: "error",
        title: `${row.name} cannot be ${sample ? "imported" : "installed"} from here`,
        description: plan.refusal,
      })
      return
    }
    const replaces = plan.bundles.filter(
      (b) => needsConfirmation(b.upgrade) && !consented.current.has(b.id)
    )
    if (replaces.length) setAsking(replaces)
    else taking.mutate()
  }
  return (
    <>
      <Button
        variant="outline"
        size="sm"
        disabled={taking.isPending}
        onClick={(e) => {
          e.stopPropagation()
          start()
        }}
      >
        {taking.isPending ? <Spinner className="size-3.5" /> : <DownloadIcon />}
        {taking.isPending ? `${verb}ing…` : label}
      </Button>
      {asking && (
        <Dialog
          open
          onOpenChange={(open) => !open && !taking.isPending && setAsking(null)}
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>
                Replace your edits to{" "}
                {asking.length === 1 ? asking[0].name : "these packages"}?
              </DialogTitle>
              <DialogDescription>
                {`${asking.map((b) => b.id).join(", ")} ` +
                  `${asking.length === 1 ? "is" : "are"} here at a version this bundle cannot use, and ` +
                  `${asking.length === 1 ? "it was" : "they were"} edited since ${asking.length === 1 ? "it" : "they"} arrived. ` +
                  `Importing ${asking.length === 1 ? "it" : "them"} again replaces the package instead of merging into it, so those edits go with it. ` +
                  `Your records are untouched.`}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={taking.isPending}
                onClick={(e) => {
                  e.stopPropagation()
                  setAsking(null)
                }}
              >
                Cancel
              </Button>
              <Button
                disabled={taking.isPending}
                onClick={(e) => {
                  e.stopPropagation()
                  for (const b of asking) consented.current.add(b.id)
                  setAsking(null)
                  taking.mutate()
                }}
              >
                {taking.isPending && <Spinner className="size-3.5" />}
                {label}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

/** A step whose freshly read preview replaces something the reader has not
 * agreed to lose. The run stops where it stands: what landed before it is
 * here, and the dialog asks about exactly this bundle. */
class NeedsConsent extends Error {
  bundle: BundleRow
  constructor(bundle: BundleRow) {
    super(bundle.id)
    this.bundle = bundle
  }
}

/** Which bundle in the chain refused, carried with the server's own error so
 * the toast can name it: one of five calls failing is not "the import
 * failed". */
class ChainFailure extends Error {
  bundle: string
  cause: unknown
  constructor(bundle: string, cause: unknown) {
    super(bundle)
    this.bundle = bundle
    this.cause = cause
  }
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
function takeAgain(
  row: BundleRow,
  upgrade = row.upgrade
): Promise<BundleStatus> {
  const door = row.tier === "sample" ? importBundle : installBundle
  return door(row.catalog?.id ?? row.id, confirmationOf(upgrade))
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
  chains: (row: BundleRow) => RequirementNode[],
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
      accessorFn: (r) => (r.status ? bundleState(r.status) : "not here"),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="state" />
      ),
      // A bundle this repository holds shows its own runtime lifecycle
      // (enabled / disabled / uninstalled) with the setup chip BESIDE it when
      // steps stand; one it does not hold shows the invitation, and what the
      // button will have to take first.
      cell: ({ row }) => {
        const missing = row.original.installed
          ? []
          : missingChain(chains(row.original))
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
                title={chainHint(
                  missing,
                  row.original.tier === "sample" ? "Import" : "Install",
                  row.original.name
                )}
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
    numColumn("kinds", "kinds", (r) => counts(r).kinds),
    numColumn("functions", "functions", (r) => counts(r).functions),
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
          ) : null
        ) : row.original.catalog ? (
          <div className="flex justify-end">
            <TakeButton row={row.original} chain={chains(row.original)} />
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

// ── the disclosure: what taking this bundle adds ────────────────────────────

/** One section of the disclosure: a heading over a list. Sections, not chips:
 * a reader deciding on an import is reading, and forty bordered words in a
 * row is not something anybody reads (owner ruling). */
function Group({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <section className="min-w-0">
      <h3 className="pb-0.5 font-medium text-muted-foreground">{title}</h3>
      {children}
    </section>
  )
}

/** One member on its own line: its word, then what it is. The description is
 * the declaration's own, which is the only thing there is to read before the
 * bundle lands. */
function Entry({
  name,
  description,
  link,
}: {
  name: string
  description?: string
  /** Where the name points once the kind is here; a name with nowhere to go
   * renders as text rather than as a link that lies. */
  link?: { authority: string; pkg: string; name: string }
}) {
  return (
    <li className="grid grid-cols-[minmax(6rem,11rem)_minmax(0,1fr)] items-baseline gap-x-3 py-0.5">
      {link ? (
        <Link
          to="/data/$authority/$pkg/$name"
          params={link}
          className="truncate data underline-offset-4 hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          {name}
        </Link>
      ) : (
        <span className="truncate data">{name}</span>
      )}
      <span className="min-w-0 text-muted-foreground">{description}</span>
    </li>
  )
}

function EntryList({ rows }: { rows: ClosureRow[] }) {
  return (
    <ul className="min-w-0">
      {rows.map((r) => (
        <Entry key={r.identity} name={r.name} description={r.description} />
      ))}
    </ul>
  )
}

/** The settings and the secrets a bundle ships as records (core `setting` and
 * `secret`): named, never valued, because the catalog carries the declaration
 * and not what the reader will have to put in it. */
const SETTING_KINDS = [`${CORE_PACKAGE}/setting`, `${CORE_PACKAGE}/secret`]

/** The requirement chain, nested: each package marked present or missing,
 * with what IT requires under it. The tree is what the one button takes, in
 * this shape, so the reader can see the whole of what pressing it does. */
function RequirementTree({ nodes }: { nodes: RequirementNode[] }) {
  return (
    <ul className="min-w-0">
      {nodes.map((node) => (
        <li key={node.package} className="py-0.5">
          <span
            className={cn(
              "inline-flex items-center gap-1",
              node.present ? "text-muted-foreground" : "text-warning"
            )}
          >
            {node.present ? (
              <CheckIcon className="size-3 shrink-0" />
            ) : (
              <TriangleAlertIcon className="size-3 shrink-0" />
            )}
            <span className="data">{node.package}</span>
            <span>
              {node.present
                ? "here"
                : node.held !== undefined
                  ? `here at version ${node.held}, and this needs version ${node.atLeast} or later`
                  : "not here yet"}
            </span>
          </span>
          {node.requires.length > 0 && (
            <div className="border-l pl-3">
              <RequirementTree nodes={node.requires} />
            </div>
          )}
        </li>
      ))}
    </ul>
  )
}

/** The row opened in place: what the bundle adds, what it links, and what it
 * needs first. */
function BundleDisclosure({
  row,
  chain,
  kinds,
}: {
  row: BundleRow
  chain: RequirementNode[]
  kinds: KindInfo[]
}) {
  const catalog = row.catalog
  const inputs = row.status?.inputs
  const closure = catalog?.closure
  const kindRows = useMemo(
    () => installedKindRows({ id: row.id, inputs }, kinds, catalog),
    [row.id, inputs, kinds, catalog]
  )
  const traits = useMemo(
    () => closureRows(closure?.traits, closure?.traitDescriptions),
    [closure]
  )
  const functions = useMemo(
    () => closureRows(closure?.functions, closure?.functionDescriptions),
    [closure]
  )
  const agents = useMemo(
    () => closureRows(closure?.agents, closure?.agentDescriptions),
    [closure]
  )
  const triggers = useMemo(() => triggerRows(catalog), [catalog])
  const records = useMemo(() => bundleRecordRows(catalog), [catalog])
  const settings = records.filter((r) => SETTING_KINDS.includes(r.kind))
  // Everything else the bundle ships: the set is open (a bundle may ship a
  // record of any kind), and a list of ids nobody can act on is noise, so it
  // is one counted line.
  const rest = records.filter(
    (r) =>
      !SETTING_KINDS.includes(r.kind) &&
      r.kind !== `${CORE_PACKAGE}/trigger` &&
      r.kind !== `${CORE_PACKAGE}/function` &&
      r.kind !== `${CORE_PACKAGE}/agent` &&
      r.kind !== `${CORE_PACKAGE}/recordmapping`
  )
  const links = mappingLinksSentence(row)
  const missing = missingChain(chain)
  // A chain that cannot be ordered, or one naming a package the catalog does
  // not ship, is a refusal rather than a list of steps: the button says the
  // same sentence when it is pressed.
  const { refusal } = importPlan(row, chain)

  return (
    <RowDetail>
      {catalog?.description && <p>{catalog.description}</p>}
      <div className="flex flex-col gap-2.5">
        {kindRows.length > 0 && (
          <Group
            title={
              kindRows.length === 1 ? "1 kind" : `${kindRows.length} kinds`
            }
          >
            <ul className="min-w-0">
              {kindRows.map((k) => (
                <Entry
                  key={k.identity}
                  name={k.name}
                  description={
                    k.role === "input"
                      ? [k.description, "its records satisfy a declared input"]
                          .filter(Boolean)
                          .join(" · ")
                      : k.role === "account"
                        ? [k.description, "the account record kind"]
                            .filter(Boolean)
                            .join(" · ")
                        : k.description
                  }
                  // A kind that is not here yet exists on paper only, so it
                  // names itself and links nowhere.
                  link={
                    k.authority && k.package
                      ? {
                          authority: k.authority,
                          pkg: k.package,
                          name: k.name,
                        }
                      : undefined
                  }
                />
              ))}
            </ul>
          </Group>
        )}
        {traits.length > 0 && (
          <Group
            title={traits.length === 1 ? "1 trait" : `${traits.length} traits`}
          >
            <EntryList rows={traits} />
          </Group>
        )}
        {functions.length > 0 && (
          <Group
            title={
              functions.length === 1
                ? "1 function"
                : `${functions.length} functions`
            }
          >
            <EntryList rows={functions} />
          </Group>
        )}
        {agents.length > 0 && (
          <Group
            title={agents.length === 1 ? "1 agent" : `${agents.length} agents`}
          >
            <EntryList rows={agents} />
          </Group>
        )}
        {triggers.length > 0 && (
          <Group
            title={
              triggers.length === 1
                ? "1 trigger"
                : `${triggers.length} triggers`
            }
          >
            <EntryList rows={triggers} />
          </Group>
        )}
        {settings.length > 0 && (
          <Group title="Settings and secrets">
            <ul className="min-w-0">
              {settings.map((r) => (
                <Entry
                  key={`${r.kind}:${r.id}`}
                  // A setting's id sits under the bundle's own (decision
                  // record 0076), so its last segment is its name.
                  name={r.id.split("/").pop() ?? r.id}
                  description={
                    r.kind === `${CORE_PACKAGE}/secret` ? "a secret" : undefined
                  }
                />
              ))}
            </ul>
            <p className="pt-0.5 text-muted-foreground">
              Their values are yours to fill in on the bundle's own page.
            </p>
          </Group>
        )}
        {rest.length > 0 && (
          <p className="text-muted-foreground">
            Also ships {rest.length} {rest.length === 1 ? "record" : "records"}.
          </p>
        )}
        {links && (
          <Group title="Links">
            <p className="text-muted-foreground">{links}</p>
          </Group>
        )}
        {chain.length > 0 && (
          <Group title="Requires">
            <RequirementTree nodes={chain} />
            {(refusal || missing.length > 0) && (
              <p className="pt-0.5 text-warning">
                {refusal ||
                  chainHint(
                    missing,
                    row.tier === "sample" ? "Import" : "Install",
                    row.name
                  )}
              </p>
            )}
          </Group>
        )}
        {row.upgrade?.available && (
          <Group title="Update">
            <p className="data">{upgradeMotion(row.upgrade)}</p>
            {stepLines(row.upgrade).map((line) => (
              <p key={line} className="text-muted-foreground">
                {line}
              </p>
            ))}
          </Group>
        )}
      </div>
      {(row.upgrade?.blockers?.length ?? 0) > 0 && (
        <div className="space-y-1 text-warning">
          <p>
            {previewFailed(row)
              ? "The update could not be previewed, so it is not offered yet."
              : "The update is blocked. Live records still hold a shape it would drop."}
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
  chains,
  kinds,
  onOpen,
}: {
  title: string
  description: string
  rows: BundleRow[]
  prefsKey: string
  emptyTitle: string
  emptyDescription: string
  chains: (row: BundleRow) => RequirementNode[]
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
    () => buildColumns(chains, confirmLoss),
    [chains, confirmLoss]
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
          <BundleDisclosure row={row} chain={chains(row)} kinds={kinds} />
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
  // bundle's kinds actually browse once they land.
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
  // The requirement chain under each row, walked across every row: a
  // requirement is supplied by another entry in either section, and what THAT
  // one requires is the rest of what one button has to take.
  const chains = useMemo(() => {
    const byId = new Map(allRows.map((row) => [row.id, row]))
    const trees = new Map<string, RequirementNode[]>()
    for (const row of allRows) {
      trees.set(row.id, requirementTree(row, byId, present, versions))
    }
    return (row: BundleRow) => trees.get(row.id) ?? []
  }, [allRows, present, versions])
  const sections = useMemo(() => bundleSections(allRows), [allRows])
  const heldCount = allRows.filter((r) => r.installed).length

  // The kind registry is a read the whole console shares (the sidebar holds it
  // warm); waiting for it here keeps a requirement from reading as missing for
  // one frame. The repository read is waited on for the same reason: without
  // the authority a sample row would preview the wrong identity for a frame.
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
  // status); a shipped one is taken from its row button and read from the
  // chevron.
  const open = (row: BundleRow) => {
    if (row.status)
      void navigate({ to: "/registry/$id", params: { id: row.id } })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <h1 className="text-lg font-semibold">Registry</h1>
        <p className="text-xs text-muted-foreground">
          {heldCount.toLocaleString()} of {allRows.length.toLocaleString()} in
          this repository
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
          chains={chains}
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
          chains={chains}
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
            chains={chains}
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
