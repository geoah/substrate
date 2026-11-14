/** The verbs a bundle takes, each with the confirmation its consequence
 * earns: ADD (a provider installs under the authority that publishes it, a
 * sample imports as this repository's own, decision record 0048), taking the
 * whole requirement chain leaves first; UPDATE (the row's own door again,
 * confirmed where it loses something, decisions 0067 and 0070); PAUSE and
 * RESUME (the bundle's `disabled` state); and REMOVE, which walks the
 * lifecycle ladder the server enforces (pause, delete what it brought in,
 * then remove its declarations) after one confirmation that names what goes.
 *
 * Nothing is disabled to explain itself: a refusal the reader can do nothing
 * about is said when the button is pressed (owner ruling). */

import { useEffect, useMemo, useRef, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  CircleArrowUpIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react"

import { ImportRefusal } from "@/components/import-refusal"
import { CodeChanges } from "@/components/packages/outside-code"
import { OUTSIDE_CODE_WORDS } from "@/lib/app-setup"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, PauseDialog } from "@/components/ui/confirm-dialog"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  purgeBundle,
  refetchBundleStateSoon,
  runBundleVerb,
  seedBundleStatus,
  uninstallBundle,
} from "@/lib/api/bundles"
import {
  catalogQueryOptions,
  addBundle,
  fetchCatalogItem,
  type BundleInstallResult,
} from "@/lib/api/catalog"
import {
  ApiError,
  type BundleStatus,
  type BundleUpgradeHolder,
  type CatalogItem,
  type ShippedUpgrade,
} from "@/lib/api/types"
import {
  addonOf,
  changeLines,
  confirmationOf,
  expectationOf,
  FAILED_PREVIEW_BLOCKER,
  holderFreeBlockers,
  importPlan,
  isProvider,
  lossyStepLines,
  missingChain,
  needsConfirmation,
  needsConsent,
  previewFailed,
  publisherReviewNeeded,
  publisherUnreadable,
  blockerWords,
  readyMappings,
  REIMPORT_WARNING,
  takeRunsOutsideCode,
  retentionShortLines,
  sameShortenings,
  stepLines,
  upgradeHolders,
  upgradeMotion,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import { shortDateTime } from "@/lib/format"
import { bundleParams, removalLadder, type LadderStep } from "@/lib/providers"
import { cn } from "@/lib/utils"

/** Add a row's shipped closure again, with the consent its preview needs.
 * The one add route takes every package (decision record 0258); a sample's
 * closure lands rehomed over the copy this repository holds (decision record
 * 0070). */
function takeAgain(
  row: BundleRow,
  upgrade = row.upgrade
): Promise<BundleInstallResult> {
  return addBundle(row.catalog?.id ?? row.id, confirmationOf(upgrade))
}

/** A step whose freshly read preview replaces something the reader has not
 * agreed to lose. The run stops where it stands. */
class NeedsConsent extends Error {
  bundle: BundleRow
  constructor(bundle: BundleRow) {
    super(bundle.id)
    this.bundle = bundle
  }
}

/** Which bundle in the chain refused, carried with the server's own error so
 * the toast can name it: one of five calls failing is not "the add failed". */
class ChainFailure extends Error {
  bundle: string
  override cause: unknown
  constructor(bundle: string, cause: unknown) {
    super(bundle)
    this.bundle = bundle
    this.cause = cause
  }
}

/** A fresh preview that keeps other history for less time than the one the
 * reader reviewed, or moved to another version: the review no longer covers
 * what would land. */
class ShorteningMoved extends Error {}

/** ADD (or import): the row's door, taking the whole missing chain leaves
 * first, one call each, and stopping at the first refusal with the name of
 * the bundle that refused. A package held below the closure's floor is in
 * the chain too, and taking it again REPLACES the copy, so a copy the reader
 * edited is confirmed first. */
export function TakeButton({
  row,
  chain,
  label,
  name,
  variant = "outline",
  className,
}: {
  row: BundleRow
  chain: RequirementNode[]
  /** Overrides the button's word ("Add Google", "Add again"). */
  label?: string
  /** What the toasts call the bundle; its package word by default. */
  name?: string
  variant?: "outline" | "default"
  className?: string
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [technical] = useTechnicalDetails()
  const called = name ?? row.name
  const plan = useMemo(() => importPlan(row, chain), [row, chain])
  // A REF, not state: the dialog's confirm runs the mutation in the same
  // handler that records the consent.
  const consented = useRef(new Set<string>())
  const [asking, setAsking] = useState<BundleRow[] | null>(null)
  const taking = useMutation({
    mutationFn: async () => {
      let landed: BundleStatus | undefined
      for (const bundle of plan.bundles) {
        // RE-READ THE PREVIEW, one bundle at a time: the step before moved
        // the changelog head, so a confirmation read with the page is
        // refused, and a fresh read is the only way to see a step that has
        // BECOME lossy since.
        let fresh: CatalogItem
        try {
          fresh = await fetchCatalogItem(bundle.catalog?.id ?? bundle.id)
        } catch (error) {
          throw new ChainFailure(bundle.name, error)
        }
        if (
          needsConfirmation(fresh.upgrade) &&
          !consented.current.has(bundle.key)
        ) {
          throw new NeedsConsent(bundle)
        }
        // A held step whose publisher's changes need review, or that keeps
        // history for less time, is its own page's to update: this run
        // shows neither for review.
        if (publisherReviewNeeded(fresh.upgrade)) {
          throw new ChainFailure(
            bundle.name,
            new Error(
              "Its publisher listed changes that need your review. Update it from its own page first."
            )
          )
        }
        if (fresh.upgrade?.retentionShortens?.length) {
          throw new ChainFailure(
            bundle.name,
            new Error(
              "Updating it keeps some history for less time. Update it from its own page first, where that is listed for your review."
            )
          )
        }
        try {
          const result = await takeAgain(bundle, fresh.upgrade)
          for (const addon of result.addons ?? []) {
            toast.add({
              type: addon.state === "installed" ? "success" : "error",
              title: addon.package
                ? `${addon.package}: ${addon.state === "installed" ? "installed" : "still waiting"}`
                : "Add-ons still waiting",
              description: addon.problem,
            })
          }
          landed = result
        } catch (error) {
          throw new ChainFailure(bundle.name, error)
        }
        // Seeded as each one lands: a chain that refuses halfway has still
        // taken everything before the refusal.
        seedBundleStatus(queryClient, landed)
      }
      return landed!
    },
    onSuccess: (landed) => {
      setAsking(null)
      toast.add({
        type: "success",
        title:
          plan.bundles.length === 1
            ? row.keptBy === "repository"
              ? // A sample lands in the local namespace `~`, not at the id
                // the reader clicked (decision records 0048 and 0259).
                technical
                ? `${called} added as ${landed.id}.`
                : `${called} added.`
              : `${called} added.`
            : `${called} and ${plan.bundles.length - 1} ${
                plan.bundles.length === 2 ? "package" : "packages"
              } it needs are here.`,
      })
      // A provider's (or an add-on's) set-up continues on its own page; a
      // package that landed with an empty required setting cannot run until
      // it is filled in, so it goes to its form. A standard collection and a
      // sample stay where they were added (decision record 0238). The id is
      // the LANDED one: a sample's is under `~`.
      const needsSettings = (landed.setup ?? []).some(
        (item) => item.code === "setting"
      )
      if (isProvider(row) || addonOf(row) || needsSettings) {
        void navigate({
          to: "/providers/$authority/$pkg",
          params: bundleParams(landed.id),
          hash: needsSettings ? "settings" : undefined,
        })
      }
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
      if (error instanceof NeedsConsent) {
        setAsking([error.bundle])
        return
      }
      const failed = error instanceof ChainFailure ? error : undefined
      toast.add({
        type: "error",
        title: `Adding ${failed?.bundle ?? called} failed`,
        description: <ImportRefusal error={failed?.cause ?? error} />,
      })
    },
  })
  // "all" is about what the row NEEDS, not what the plan managed to order.
  const word = label ?? (missingChain(chain).length > 0 ? "Add all" : "Add")
  const start = () => {
    if (plan.refusal) {
      toast.add({
        type: "error",
        title: `${called} cannot be added from here`,
        description: plan.refusal,
      })
      return
    }
    const replaces = plan.bundles.filter(
      (b) => needsConfirmation(b.upgrade) && !consented.current.has(b.key)
    )
    if (replaces.length) setAsking(replaces)
    else taking.mutate()
  }
  return (
    <>
      <Button
        variant={variant}
        size="sm"
        className={className}
        disabled={taking.isPending}
        onClick={(e) => {
          e.stopPropagation()
          e.preventDefault()
          start()
        }}
      >
        {taking.isPending ? (
          <Spinner className="size-3.5" />
        ) : (
          <PlusIcon aria-hidden />
        )}
        {taking.isPending ? "Adding…" : word}
      </Button>
      {asking && (
        <ConfirmDialog
          title={`Replace your edits to ${asking.length === 1 ? asking[0].name : "these packages"}?`}
          consequence={
            `${asking.map((b) => b.id).join(", ")} ` +
            `${asking.length === 1 ? "is" : "are"} here at a version this cannot use, and ` +
            `${asking.length === 1 ? "it was" : "they were"} edited since ${asking.length === 1 ? "it" : "they"} arrived. ` +
            `Adding ${asking.length === 1 ? "it" : "them"} again replaces the package instead of merging into it, so those edits go with it. ` +
            `Your records are untouched.`
          }
          confirm={word}
          pending={taking.isPending}
          onConfirm={() => {
            for (const b of asking) consented.current.add(b.key)
            setAsking(null)
            taking.mutate()
          }}
          onClose={() => setAsking(null)}
        />
      )}
    </>
  )
}

// ── update ───────────────────────────────────────────────────────────────────

/** UPDATE: the row's own door again, offered only when the server's preview
 * says the closure moved and nothing blocks it. An update that loses
 * something (a lossy plan, or a re-import over an edited sample copy), whose
 * publisher's changes need review (decision 0220), or that keeps some
 * history for less time at any tier (decision 0282), asks first through
 * `LossyUpgradeDialog`, which the caller renders so it outlives a re-render
 * of the row. */
export function UpgradeButton({
  row,
  name,
  onConfirmLoss,
}: {
  row: BundleRow
  name?: string
  onConfirmLoss: (row: BundleRow) => void
}) {
  const queryClient = useQueryClient()
  const called = name ?? row.name
  const upgrade = row.upgrade
  const asks = needsConsent(upgrade)
  const upgrading = useMutation({
    mutationFn: () => takeAgain(row),
    onSuccess: (status) => {
      toast.add({
        type: "success",
        title: upgrade?.to
          ? `${called} updated to version ${upgrade.to}.`
          : `${called} updated.`,
      })
      seedBundleStatus(queryClient, status)
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `Updating ${called} failed`,
        description: <ImportRefusal error={error} />,
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
            disabled={upgrading.isPending || Boolean(upgrade?.holders?.length)}
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
          <CircleArrowUpIcon aria-hidden />
        )}
        {upgrading.isPending ? "Updating…" : "Update"}
      </TooltipTrigger>
      {(motion || steps.length > 0) && (
        <TooltipContent className="max-w-96">
          <div className="space-y-1">
            {motion && <p>Version {motion}</p>}
            {upgrade?.holders && upgrade.holders.length > 0 && (
              <>
                <p>
                  Waiting for connected app builds that still ask for the
                  version you hold.
                </p>
                <HolderList holders={upgrade.holders} />
              </>
            )}

            {steps.map((s) => (
              <p key={s}>{s}</p>
            ))}
          </div>
        </TooltipContent>
      )}
    </Tooltip>
  )
}

/** The dialog an update that asks first goes through, for any of three
 * reasons: it loses something (decisions 0067 and 0070); the publisher's
 * changes need review before they land (decision 0220), code shown as
 * diffs, or what the server marked for review without one, a declaration
 * removed or a reach grown, listed by the versions that move; or it keeps
 * some kind's history for less time, at any tier (decision 0282). A review
 * is bound to what was shown: the install carries the preview's version and
 * digest, and a publisher that listed another version since answers 409. A
 * shortening with no publisher behind it reads the preview again at the
 * click and lands only if it shortens the same kinds at the same version.
 *
 * It reads the row's CURRENT preview: after a `409` the catalog is read
 * again, the dialog stays open and says so, and its next click confirms the
 * fresh preview, never the stale one. */
export function LossyUpgradeDialog({
  row,
  name,
  onClose,
}: {
  row: BundleRow | undefined
  name?: string
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  // Which row's confirmation was refused, and what moved: the reviewed
  // version (the publisher listed another), the plan (records moved), or
  // what the update keeps for less time.
  const [stale, setStale] = useState<{
    key: string
    moved: "publisher" | "plan" | "retention"
  } | null>(null)
  const upgrade = row?.upgrade
  const called = name ?? row?.name
  const publisher = publisherReviewNeeded(upgrade)
  const shorter = retentionShortLines(upgrade)
  const expectation = expectationOf(upgrade)
  const upgrading = useMutation({
    mutationFn: async () => {
      if (!row || !needsConsent(upgrade)) {
        throw new Error("no plan to confirm")
      }
      if (shorter.length > 0 && !publisher) {
        const fresh = await fetchCatalogItem(row.catalog?.id ?? row.id)
        if (!sameShortenings(upgrade, fresh.upgrade)) {
          throw new ShorteningMoved()
        }
      }
      return takeAgain(row)
    },
    onSuccess: (status) => {
      setStale(null)
      onClose()
      toast.add({
        type: "success",
        title: upgrade?.to
          ? `${called} updated to version ${upgrade.to}.`
          : `${called} updated.`,
      })
      seedBundleStatus(queryClient, status)
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      // A 409 says what was confirmed moved: the publisher listed another
      // version than the one reviewed, or records changed since the plan
      // was read; a 403 `lossy` at the same head says the plan itself
      // reads differently now; a fresh preview that shortens other history
      // says the binary moved. Either way the consent named something that
      // no longer holds: read it again.
      if (error instanceof ShorteningMoved) {
        setStale({ key: row?.key ?? "", moved: "retention" })
        void queryClient.invalidateQueries({
          queryKey: catalogQueryOptions.queryKey,
        })
        return
      }
      if (
        error instanceof ApiError &&
        (error.status === 409 || error.code === "lossy")
      ) {
        setStale({
          key: row?.key ?? "",
          moved:
            error.status === 409 && expectation !== undefined
              ? "publisher"
              : "plan",
        })
        void queryClient.invalidateQueries({
          queryKey: catalogQueryOptions.queryKey,
        })
        return
      }
      toast.add({
        type: "error",
        title: `Updating ${called} failed`,
        description: <ImportRefusal error={error} />,
      })
    },
  })
  // A re-read plan that loses nothing and needs no review has nothing to
  // consent to.
  const lossless = Boolean(row && !needsConsent(upgrade))
  useEffect(() => {
    if (!lossless) return
    onClose()
    toast.add({
      type: "success",
      title: `Updating ${called} now asks nothing`,
      description: "Press Update to get it.",
    })
  }, [lossless, onClose, called])
  if (!row || lossless) return null
  const losses = lossyStepLines(upgrade)
  const changes = publisher ? changeLines(upgrade) : []
  const staleHere = stale !== null && stale.key === row.key
  // A publisher's review binds the install to the version shown; a preview
  // that did not say which version it is of cannot be confirmed as reviewed.
  const unbound = publisher && !expectation
  const close = () => {
    setStale(null)
    onClose()
  }
  const title = upgrade?.discardsEdits
    ? `Update ${called} and replace your edits?`
    : publisher
      ? `Review the publisher’s changes to ${called}`
      : upgrade?.lossy || shorter.length === 0
        ? `Update ${called} and remove values?`
        : `Update ${called} and keep less history?`
  const consequence =
    (upgrade?.discardsEdits
      ? `You edited ${row.id} since you added it. Updating replaces the package with the shipped one, so your edits go with it. Your records are untouched. `
      : "") +
    (publisher
      ? `Its publisher listed ${
          upgrade?.candidateVersion
            ? `version ${upgrade.candidateVersion}`
            : "a version"
        } with changes that need your review before they land${
          upgrade?.codeChanges?.length
            ? ": the code it runs changed, shown below"
            : upgrade?.retentionShortens?.length
              ? ": it keeps some history for less time, shown below"
              : ": what it declares, or what its code reaches, is not the same as what you hold"
        }. `
      : "") +
    (upgrade?.lossy
      ? `Updating rewrites ${upgrade?.work ?? 0} ${
          upgrade?.work === 1 ? "record" : "records"
        } and removes some values from them. ` +
        `The removed values stay in History. `
      : "") +
    (shorter.length > 0 && !publisher
      ? `Updating keeps the history of the collections below for less time. Each record’s latest state is always kept, and nothing is removed until a release that compacts history. An override of a collection’s retention keeps its history whatever the update says. `
      : "") +
    (publisher
      ? `Confirming updates to exactly the version reviewed here. If the publisher lists another version before it lands, the update is refused and this asks again.`
      : upgrade?.lossy || upgrade?.discardsEdits
        ? `This confirms exactly the plan below. If a record or collection it changes is written before it lands, the plan is read again.`
        : `Confirming updates to the version shown here. If what it keeps for less time has changed when you confirm, nothing is updated and this asks again.`)
  const confirm = upgrade?.discardsEdits
    ? "Update and replace my edits"
    : publisher
      ? "Approve the changes and update"
      : upgrade?.lossy || shorter.length === 0
        ? "Update and remove values"
        : "Keep less history and update"
  return (
    <ConfirmDialog
      title={title}
      consequence={consequence}
      confirm={confirm}
      pending={upgrading.isPending}
      disabled={!confirmationOf(upgrade) || unbound}
      onConfirm={() => upgrading.mutate()}
      onClose={close}
    >
      {publisher && upgrade?.candidateVersion !== undefined && (
        <p className="text-xs text-muted-foreground">
          Reviewing version {upgrade.candidateVersion}
          {upgrade.candidateDigest ? (
            <>
              , digest{" "}
              <span className="font-mono break-all">
                {upgrade.candidateDigest}
              </span>
            </>
          ) : null}
          .
        </p>
      )}
      {takeRunsOutsideCode(row, []) && (
        <p className="text-xs">
          <span className="font-medium">Code: experimental.</span>{" "}
          {OUTSIDE_CODE_WORDS}
        </p>
      )}
      <CodeChanges changes={upgrade?.codeChanges} />
      {shorter.length > 0 && (
        <section className="space-y-1">
          <h3 className="text-sm font-medium">History kept for less time</h3>
          <ul className="space-y-1 text-xs">
            {shorter.map((line) => (
              <li key={line} className="break-all">
                {line}
              </li>
            ))}
          </ul>
        </section>
      )}
      {changes.length > 0 && (
        <section className="space-y-1">
          <h3 className="text-sm font-medium">Declarations that change</h3>
          <ul className="space-y-1 text-xs">
            {changes.map((line) => (
              <li key={line} className="break-all">
                {line}
              </li>
            ))}
          </ul>
        </section>
      )}
      {staleHere && (
        <p role="status" className="text-sm text-warning">
          {stale.moved === "publisher"
            ? "The publisher listed another version since you reviewed this one, so the update was refused. Review it again below, then confirm."
            : stale.moved === "retention"
              ? "What this update keeps for less time changed since you read it, so nothing was updated. Check it below and confirm again."
              : "A record or collection this plan changes was written since it was read, so the update was refused. Check the plan below and confirm it again."}
        </p>
      )}
      {unbound && (
        <p role="status" className="text-sm text-warning">
          The server did not say which version this preview is of, so the review
          cannot be bound to it. Reload the page and try again.
        </p>
      )}
      <ul className="space-y-1 text-sm">
        {losses.map((s) => (
          <li key={s}>{s}</li>
        ))}
      </ul>
    </ConfirmDialog>
  )
}

/** The builds holding an update back, each by its facts, labelled: the
 * app's name, its client id whole, the build and when it last checked in. */
function HolderList({ holders }: { holders: BundleUpgradeHolder[] }) {
  return (
    <ul
      aria-label="Connected app builds holding the update back"
      className="flex flex-col gap-2"
    >
      {holders.map((holder) => (
        <li key={holder.clientId}>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
            <dt className="text-muted-foreground">App</dt>
            <dd className="[overflow-wrap:anywhere]">
              {holder.clientName || "Unnamed app"}
            </dd>
            <dt className="text-muted-foreground">Client id</dt>
            <dd className="font-mono [overflow-wrap:anywhere]">
              {holder.clientId}
            </dd>
            <dt className="text-muted-foreground">Build</dt>
            <dd>{holder.softwareVersion || "Not reported"}</dd>
            <dt className="text-muted-foreground">Asks for</dt>
            <dd>version {holder.floor}</dd>
            <dt className="text-muted-foreground">Last checked in</dt>
            <dd>
              {holder.checkedAt
                ? shortDateTime(holder.checkedAt)
                : "No check-in recorded"}
            </dd>
          </dl>
        </li>
      ))}
    </ul>
  )
}

/** A blocked update, stated instead of offered: the server would refuse it,
 * so there is no click. Which of three things is in the way has its own
 * heading: connected app builds that still ask for the version held
 * (decision 0214), listed by their facts; a publisher's document that could
 * not be checked against what is held; or live records that hold something
 * the update would drop, named by the server's own guard lines. */
export function UpgradeBlockedNote({
  row,
  className,
}: {
  row: BundleRow
  className?: string
}) {
  const holders = upgradeHolders(row.upgrade)
  const unread = publisherUnreadable(row.upgrade)
  // The heading says a preview failed, or lists the holders; the lines
  // that restate either add nothing.
  const blockers = holderFreeBlockers(
    row.upgrade?.blockers ?? [],
    holders
  ).filter((b) => b !== FAILED_PREVIEW_BLOCKER)
  const heading = previewFailed(row)
    ? "An update could not be checked, so it is not offered yet."
    : holders.length > 0
      ? "An update is waiting. Connected app builds still ask for the version you hold."
      : unread
        ? "An update is waiting, but what you hold could not be checked against the publisher’s document."
        : "An update is waiting. Some of your records still hold something it would drop."
  return (
    <div
      role="note"
      className={cn(
        "flex items-start gap-2.5 rounded-lg bg-warn-soft px-3.5 py-3 text-[13px]",
        className
      )}
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-warning"
      />
      <div className="min-w-0 space-y-2">
        <p className="font-medium">{heading}</p>
        {holders.length > 0 && (
          <>
            <HolderList holders={holders} />
            <p className="text-xs text-muted-foreground">
              The update lands once each build checks in asking for the new
              version, or its access ends.
            </p>
          </>
        )}
        {blockers.length > 0 && (
          <div className="space-y-1">
            {holders.length > 0 && (
              <p className="text-xs font-medium">Also in the way</p>
            )}
            {blockers.map((b) => (
              <p key={b} className="text-xs break-words text-muted-foreground">
                {blockerWords(b)}
              </p>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

/** A shipped package whose upgrade has not landed here (core, which no
 * catalog entry carries). REFUSED: the boot upgrade ran and its guards
 * refused it, and the lines say what to migrate. ADMITTED: nothing blocks
 * it, but the boot upgrade runs at a repository's first open under a binary,
 * so it lands when the server starts again. */
export function PendingUpgradeNotice({ item }: { item: ShippedUpgrade }) {
  const motion = upgradeMotion(item.upgrade)
  const blockers = item.upgrade.blockers ?? []
  const refused = blockers.length > 0
  return (
    <div
      role="alert"
      className="flex items-start gap-2.5 rounded-lg bg-warn-soft px-3.5 py-3 text-[13px]"
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-warning"
      />
      <div className="min-w-0 space-y-1">
        <p>
          The update of{" "}
          <span className="font-mono text-xs">{item.package}</span>
          {motion ? ` (version ${motion})` : ""}{" "}
          {refused
            ? "was refused when the server started. Fix what the lines below name, then start the server again."
            : "lands when the server starts again. Until then everything works as it does now."}
        </p>
        {[...(refused ? blockers : []), ...stepLines(item.upgrade)].map(
          (line) => (
            <p key={line} className="text-xs break-words text-muted-foreground">
              {blockerWords(line)}
            </p>
          )
        )}
      </div>
    </div>
  )
}

// ── pause, resume, remove ────────────────────────────────────────────────────

function useLifecycleRefresh() {
  const queryClient = useQueryClient()
  return (status?: BundleStatus | null) => {
    if (status) seedBundleStatus(queryClient, status)
    void queryClient.invalidateQueries()
    refetchBundleStateSoon(queryClient)
  }
}

/** PAUSE (the bundle's `disabled`) asks first and says what stops; RESUME
 * picks up where it left off and needs no asking. */
export function PauseBundleButton({
  bundle,
  name,
}: {
  bundle: BundleStatus
  name: string
}) {
  const refresh = useLifecycleRefresh()
  const [confirming, setConfirming] = useState(false)
  const pausing = !bundle.enabled ? "enable" : "disable"
  const verb = useMutation({
    mutationFn: () => runBundleVerb(bundle.id, pausing),
    onSuccess: (status) => {
      setConfirming(false)
      toast.add({
        type: "success",
        title: pausing === "disable" ? `${name} paused.` : `${name} resumed.`,
      })
      refresh(status)
    },
    onError: (error) => {
      setConfirming(false)
      toast.add({
        type: "error",
        title: pausing === "disable" ? "Pausing failed" : "Resuming failed",
        description: error.message,
      })
      refresh()
    },
  })
  if (!bundle.installed) return null
  if (pausing === "enable") {
    return (
      <Button
        variant="outline"
        size="sm"
        disabled={verb.isPending}
        onClick={() => verb.mutate()}
      >
        {verb.isPending ? (
          <Spinner className="size-3.5" />
        ) : (
          <PlayIcon aria-hidden />
        )}
        Resume
      </Button>
    )
  }
  return (
    <>
      <Button
        variant="outline"
        size="sm"
        disabled={verb.isPending}
        onClick={() => setConfirming(true)}
      >
        <PauseIcon aria-hidden />
        Pause
      </Button>
      {confirming && (
        <PauseDialog
          name={name}
          pending={verb.isPending}
          onConfirm={() => verb.mutate()}
          onClose={() => setConfirming(false)}
        />
      )}
    </>
  )
}

const LADDER_WORDS: Record<LadderStep, string> = {
  disable: "Pausing",
  purge: "Deleting what it brought in",
  uninstall: "Removing its collections and tools",
}

/** REMOVE: one confirmation that names every consequence, then the ladder
 * run in order, stopping at the first refusal with the rung that refused. */
export function RemoveBundleButton({
  bundle,
  name,
  variant = "outline",
  owned = false,
  leaveTo = "/providers",
}: {
  bundle: BundleStatus
  name: string
  variant?: "outline" | "ghost"
  /** The records in its collections are the owner's own, not copies an
   * account brought in (a standard collection, a sample, an app's package):
   * the confirmation says they are deleted. */
  owned?: boolean
  /** Where the console goes once it is removed: the list it was reached from. */
  leaveTo?: "/providers" | "/data"
}) {
  const refresh = useLifecycleRefresh()
  const navigate = useNavigate()
  const [confirming, setConfirming] = useState(false)
  const ladder = removalLadder(bundle)
  const records = bundle.liveRecords ?? 0
  const remove = useMutation({
    mutationFn: async () => {
      let last: BundleStatus | null = null
      for (const step of ladder) {
        try {
          if (step === "disable")
            last = await runBundleVerb(bundle.id, "disable")
          else if (step === "purge") await purgeBundle(bundle.id)
          else await uninstallBundle(bundle.id)
        } catch (error) {
          throw new LadderFailure(step, error)
        }
      }
      return last
    },
    onSuccess: () => {
      setConfirming(false)
      toast.add({ type: "success", title: `${name} removed.` })
      refresh()
      void navigate({ to: leaveTo })
    },
    onError: (error) => {
      setConfirming(false)
      const failed = error instanceof LadderFailure ? error : undefined
      toast.add({
        type: "error",
        title: failed
          ? `Removing ${name} stopped at: ${LADDER_WORDS[failed.step].toLowerCase()}`
          : `Removing ${name} failed`,
        description:
          (failed?.cause instanceof Error ? failed.cause.message : undefined) ??
          error.message,
      })
      refresh()
    },
  })
  if (!ladder.length) return null
  return (
    <>
      <Button
        variant={variant}
        size="sm"
        className={
          variant === "ghost"
            ? "text-muted-foreground hover:text-destructive"
            : "text-destructive"
        }
        disabled={remove.isPending}
        onClick={() => setConfirming(true)}
      >
        <Trash2Icon aria-hidden />
        Remove…
      </Button>
      {confirming && (
        <ConfirmDialog
          title={`Remove ${name}?`}
          consequence={
            owned
              ? (records > 0
                  ? `This deletes the ${records.toLocaleString()} ${records === 1 ? "record" : "records"} in ${name}’s collections, yours included, `
                  : "This deletes any records in its collections, ") +
                `and removes its collections and tools. Anything that linked to them points nowhere afterwards. This cannot be undone. To stop it for a while instead, pause it.`
              : (records > 0
                  ? `This deletes the ${records.toLocaleString()} ${records === 1 ? "record" : "records"} ${name} brought in, its connected accounts among them, `
                  : "This deletes its connected accounts, if any, ") +
                `and removes its collections and tools. Your own records stay, but whatever they linked to from ${name} points nowhere afterwards. This cannot be undone. To stop it for a while instead, pause it.`
          }
          confirm={`Remove ${name}`}
          destructive
          pending={remove.isPending}
          onConfirm={() => remove.mutate()}
          onClose={() => setConfirming(false)}
        >
          <ol className="space-y-1 text-sm text-muted-foreground">
            {ladder.map((step, i) => (
              <li key={step}>
                {i + 1}. {LADDER_WORDS[step]}
              </li>
            ))}
          </ol>
        </ConfirmDialog>
      )}
    </>
  )
}

class LadderFailure extends Error {
  step: LadderStep
  override cause: unknown
  constructor(step: LadderStep, cause: unknown) {
    super(step)
    this.step = step
    this.cause = cause
  }
}

// ── a held sample's waiting links ────────────────────────────────────────────

/** IMPORT AGAIN: the one action that lands a mapping a first import dropped
 * (decision record 0049), because its provider was not here yet. It
 * confirms first: a re-import REPLACES the package rather than merging into
 * it (decision record 0048), and over an edited copy it sends the preview's
 * confirmation, which the door requires (decision record 0070). */
export function ImportAgainNote({ item }: { item: CatalogItem }) {
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const ready = readyMappings({ catalog: item })
  const shorter = retentionShortLines(item.upgrade)
  const importing = useMutation({
    mutationFn: async () => {
      // What it keeps for less time is bound as an update's is: the same
      // kinds at the same version, read again at the click.
      if (shorter.length > 0) {
        const fresh = await fetchCatalogItem(item.id)
        if (!sameShortenings(item.upgrade, fresh.upgrade)) {
          throw new Error(
            "What adding it again keeps for less time changed since you read it, so nothing was added. Read it again and confirm."
          )
        }
      }
      return addBundle(item.id, confirmationOf(item.upgrade))
    },
    onSuccess: (status) => {
      setConfirming(false)
      toast.add({
        type: "success",
        title:
          ready.length === 1
            ? `${item.name} added again: 1 link landed.`
            : `${item.name} added again: ${ready.length} links landed.`,
      })
      seedBundleStatus(queryClient, status)
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `Couldn’t add ${item.name} again`,
        description: <ImportRefusal error={error} />,
      })
    },
  })
  if (!ready.length) return null
  const providers = [
    ...new Set(ready.map((m) => m.package.split("/").pop() ?? m.package)),
  ].join(", ")
  const what = ready.length === 1 ? "1 link" : `${ready.length} links`
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-panel px-3.5 py-3">
      <div className="min-w-0 text-[13px]">
        <p className="font-medium">Links waiting</p>
        <p className="text-muted-foreground">
          {`Add it again to land ${what}, now that ${providers} ${
            ready.length === 1 ? "is" : "are"
          } here. ${REIMPORT_WARNING}`}
        </p>
      </div>
      <Button
        variant="outline"
        size="sm"
        disabled={importing.isPending}
        onClick={() => setConfirming(true)}
      >
        {importing.isPending && <Spinner className="size-3.5" />}
        Add again
      </Button>
      {confirming && (
        <ConfirmDialog
          title={`Add ${item.name} again?`}
          consequence={
            `This lands ${what}, now that the provider each one reads is here. ` +
            `Adding it again REPLACES ${item.id} rather than merging into it: a collection or a property you added is dropped by it, ` +
            `and it is refused outright while your records still hold something the shipped package no longer has. ` +
            `Your records are untouched either way.` +
            (shorter.length > 0
              ? ` It also keeps the history of the collections below for less time; nothing is removed until a release that compacts history.`
              : "")
          }
          confirm="Add again"
          pending={importing.isPending}
          onConfirm={() => importing.mutate()}
          onClose={() => setConfirming(false)}
        >
          {shorter.length > 0 && (
            <section className="space-y-1">
              <h3 className="text-sm font-medium">
                History kept for less time
              </h3>
              <ul className="space-y-1 text-xs">
                {shorter.map((line) => (
                  <li key={line} className="break-all">
                    {line}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </ConfirmDialog>
      )}
    </div>
  )
}
