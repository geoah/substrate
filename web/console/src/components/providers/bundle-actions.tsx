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
  fetchCatalogItem,
  importBundle,
  installBundle,
} from "@/lib/api/catalog"
import {
  ApiError,
  type BundleStatus,
  type CatalogItem,
  type ShippedUpgrade,
} from "@/lib/api/types"
import {
  confirmationOf,
  importPlan,
  lossyStepLines,
  missingChain,
  needsConfirmation,
  previewFailed,
  blockerWords,
  FAILED_PREVIEW_BLOCKER,
  readyMappings,
  REIMPORT_WARNING,
  stepLines,
  upgradeMotion,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import { bundleParams, removalLadder, type LadderStep } from "@/lib/providers"
import { cn } from "@/lib/utils"

/** The words a row's door is called by: a provider is ADDED (the everyday
 * word for its install), a sample IMPORTED. */

/** Take a row's shipped closure through its own door, with the consent its
 * preview needs: the install verb for a provider, the import verb for a
 * sample, whose closure lands rehomed over the copy this repository holds
 * (decision record 0070). */
function takeAgain(
  row: BundleRow,
  upgrade = row.upgrade
): Promise<BundleStatus> {
  const door = row.tier === "sample" ? importBundle : installBundle
  return door(row.catalog?.id ?? row.id, confirmationOf(upgrade))
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
  cause: unknown
  constructor(bundle: string, cause: unknown) {
    super(bundle)
    this.bundle = bundle
    this.cause = cause
  }
}

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
          !consented.current.has(bundle.id)
        ) {
          throw new NeedsConsent(bundle)
        }
        try {
          landed = await takeAgain(bundle, fresh.upgrade)
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
            ? row.tier === "sample"
              ? // A sample lands under THIS repository's authority, not the
                // id the reader clicked (decision record 0048).
                technical
                ? `${called} added as ${landed.id}.`
                : `${called} added.`
              : `${called} added.`
            : `${called} and ${plan.bundles.length - 1} ${
                plan.bundles.length === 2 ? "package" : "packages"
              } it needs are here.`,
      })
      // A provider's set-up continues on its own page; a sample that landed
      // with an empty required setting cannot run until it is filled in, so
      // it goes to its form. The id is the LANDED one: a sample's is rehomed.
      const needsSettings = (landed.setup ?? []).some(
        (item) => item.code === "setting"
      )
      if (row.tier !== "sample" || needsSettings) {
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
      (b) => needsConfirmation(b.upgrade) && !consented.current.has(b.id)
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
            for (const b of asking) consented.current.add(b.id)
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
 * something (a lossy plan, or a re-import over an edited sample copy) asks
 * first through `LossyUpgradeDialog`, which the caller renders so it outlives
 * a re-render of the row. */
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
  const asks = needsConfirmation(upgrade)
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
          <CircleArrowUpIcon aria-hidden />
        )}
        {upgrading.isPending ? "Updating…" : "Update"}
      </TooltipTrigger>
      {(motion || steps.length > 0) && (
        <TooltipContent className="max-w-96">
          <div className="space-y-1">
            {motion && <p>Version {motion}</p>}
            {steps.map((s) => (
              <p key={s}>{s}</p>
            ))}
          </div>
        </TooltipContent>
      )}
    </Tooltip>
  )
}

/** The consent to an update that loses something (decisions 0067 and 0070).
 * It reads the row's CURRENT preview: after a `409` the catalog is read
 * again, the dialog stays open and says so, and its next click confirms the
 * fresh `planHash` and `changelogSeq`, never the stale pair. */
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
  const [staleFor, setStaleFor] = useState<string | null>(null)
  const upgrade = row?.upgrade
  const called = name ?? row?.name
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
          ? `${called} updated to version ${upgrade.to}.`
          : `${called} updated.`,
      })
      seedBundleStatus(queryClient, status)
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      // A 409 says records changed since the preview; a 403 `lossy` at the
      // same head says the plan itself reads differently now. Either way the
      // consent named a plan the server no longer counts: read it again.
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
        title: `Updating ${called} failed`,
        description: <ImportRefusal error={error} />,
      })
    },
  })
  // A re-read plan that loses nothing has nothing to consent to.
  const lossless = Boolean(row && !needsConfirmation(upgrade))
  useEffect(() => {
    if (!lossless) return
    onClose()
    toast.add({
      type: "success",
      title: `Updating ${called} now loses nothing`,
      description: "Press Update to get it.",
    })
  }, [lossless, onClose, called])
  if (!row || lossless) return null
  const losses = lossyStepLines(upgrade)
  const close = () => {
    setStaleFor(null)
    onClose()
  }
  return (
    <ConfirmDialog
      title={
        upgrade?.discardsEdits
          ? `Update ${called} and replace your edits?`
          : `Update ${called} and remove values?`
      }
      consequence={
        (upgrade?.discardsEdits
          ? `You edited ${row.id} since you added it. Updating replaces the package with the shipped one, so your edits go with it. Your records are untouched. `
          : "") +
        (upgrade?.lossy
          ? `Updating rewrites ${upgrade?.work ?? 0} ${
              upgrade?.work === 1 ? "record" : "records"
            } and removes some values from them. ` +
            `The removed values stay in History. `
          : "") +
        `This confirms exactly the plan below. If a record or collection it changes is written before it lands, the plan is read again.`
      }
      confirm={
        upgrade?.discardsEdits
          ? "Update and replace my edits"
          : "Update and remove values"
      }
      pending={upgrading.isPending}
      disabled={!upgrade?.planHash}
      onConfirm={() => upgrading.mutate()}
      onClose={close}
    >
      {stale && (
        <p role="status" className="text-sm text-warning">
          A record or collection this plan changes was written since it was
          read, so the update was refused. Check the plan below and confirm it
          again.
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

/** A blocked update, stated instead of offered: the server would refuse it
 * (refuse-breakage), so there is no click. The server's own guard lines name
 * the kind, the property and the count to migrate. */
export function UpgradeBlockedNote({
  row,
  className,
}: {
  row: BundleRow
  className?: string
}) {
  // The heading already says a preview failed; its fixed line adds nothing.
  const blockers = (row.upgrade?.blockers ?? []).filter(
    (b) => b !== FAILED_PREVIEW_BLOCKER
  )
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
      <div className="min-w-0 space-y-1">
        <p className="font-medium">
          {previewFailed(row)
            ? "An update could not be checked, so it is not offered yet."
            : "An update is waiting. Some of your records still hold something it would drop."}
        </p>
        {blockers.map((b) => (
          <p key={b} className="text-xs break-words text-muted-foreground">
            {b}
          </p>
        ))}
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
}: {
  bundle: BundleStatus
  name: string
  variant?: "outline" | "ghost"
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
      void navigate({ to: "/providers" })
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
          (error as Error).message,
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
            (records > 0
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
  cause: unknown
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
  const importing = useMutation({
    mutationFn: () => importBundle(item.id, confirmationOf(item.upgrade)),
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
            `Your records are untouched either way.`
          }
          confirm="Add again"
          pending={importing.isPending}
          onConfirm={() => importing.mutate()}
          onClose={() => setConfirming(false)}
        />
      )}
    </div>
  )
}
