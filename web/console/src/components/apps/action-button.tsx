/* eslint-disable react-refresh/only-export-components */
/** One action as a button, and the runner behind it. `useActionRunner` owns
 * what a verb needs before `runAction` can go: the prompt sheet for a create
 * or a patch with a `prompt`, the confirm for a delete or a `confirm: true`,
 * an idempotency key minted once per opened form, and the navigate an `open`
 * pushes with. An action with both a prompt and a confirm asks in that
 * order: the form collects the values, the confirm asks about them, and only
 * then does the write run, under the key the form was opened with, because
 * a `confirm: true` the form could bypass would be no confirm at all.
 * `<ActionButton>` is the runner with a button; `<ActionRunner>` is the
 * runner started on mount, for a menu item whose menu closes before the
 * sheet opens. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  ArrowRightIcon,
  CheckIcon,
  ExternalLinkIcon,
  MessageSquareIcon,
  PencilIcon,
  PlayIcon,
  PlusIcon,
  Trash2Icon,
  type LucideProps,
} from "lucide-react"
import type { ComponentType } from "react"

import { ConfirmDialog } from "@/components/apps/confirm-dialog"
import { FormSheet } from "@/components/apps/form-sheet"
import { AppIcon } from "@/components/apps/icon"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import type { SubstrateRecord } from "@/lib/api/types"
import { createSeed, runAction, type ActionOutcome } from "@/lib/apps/actions"
import { newIdempotencyKey, promptFields } from "@/lib/apps/form"
import { stateSpecOf } from "@/lib/apps/machine"
import { titleOf } from "@/lib/apps/referents"
import { recordSegment } from "@/lib/apps/route"
import type { ActionHost, ActionSpec, Verb } from "@/lib/apps/spec"
import { substituteSet } from "@/lib/apps/tokens"
import { cn } from "@/lib/utils"

const VERB_ICONS: Record<Verb, ComponentType<LucideProps>> = {
  create: PlusIcon,
  transition: CheckIcon,
  patch: PencilIcon,
  delete: Trash2Icon,
  call: PlayIcon,
  chat: MessageSquareIcon,
  open: ArrowRightIcon,
  link: ExternalLinkIcon,
}

export function ActionIcon({
  action,
  className,
}: {
  action: ActionSpec
  className?: string
}) {
  return (
    <AppIcon
      name={action.icon}
      fallback={VERB_ICONS[action.verb]}
      className={className}
    />
  )
}

export interface ActionRunner {
  run: () => void
  pending: boolean
  /** The sheet and the confirm, mounted beside the trigger. */
  dialogs: React.ReactNode
}

export function useActionRunner(
  host: ActionHost,
  action: ActionSpec,
  record?: SubstrateRecord,
  onSettled?: (outcome?: ActionOutcome) => void
): ActionRunner {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [formOpen, setFormOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  // What the form collected, held while the confirm asks about it.
  const [collected, setCollected] = useState<Record<string, unknown>>()
  const [pending, setPending] = useState(false)
  const [idempotencyKey, setIdempotencyKey] = useState<string>()

  const asksForm =
    (action.verb === "create" || action.verb === "patch") &&
    action.prompt.length > 0
  const asksConfirm = action.confirm

  const execute = useCallback(
    async (values?: Record<string, unknown>) => {
      setPending(true)
      try {
        const outcome = await runAction({
          action,
          spec: host.spec,
          kind: host.kind,
          kinds: host.kinds,
          ctx: host.ctx,
          queryClient,
          record,
          values,
          idempotencyKey,
          navigate: ({ view, record: row }) =>
            void navigate({
              to: "/views/$id/$record",
              params: { id: view, record: recordSegment(row) },
            }),
        })
        onSettled?.(outcome)
        return outcome.ok
      } finally {
        setPending(false)
      }
    },
    [action, host, queryClient, record, idempotencyKey, navigate, onSettled]
  )

  const run = useCallback(() => {
    if (asksForm) {
      setIdempotencyKey(newIdempotencyKey())
      setFormOpen(true)
      return
    }
    if (asksConfirm) {
      setConfirmOpen(true)
      return
    }
    void execute()
  }, [asksForm, asksConfirm, execute])

  // The form's submit. With a confirm still to ask, the values are held and
  // the question opens over the sheet, which stays (`false`: nothing landed)
  // so a cancelled confirm returns to the filled form.
  const submit = useCallback(
    async (values: Record<string, unknown>) => {
      if (!asksConfirm) return execute(values)
      setCollected(values)
      setConfirmOpen(true)
      return false
    },
    [asksConfirm, execute]
  )
  const confirm = useCallback(() => {
    void execute(collected).then((ok) => {
      setConfirmOpen(false)
      if (ok) setFormOpen(false)
    })
  }, [execute, collected])

  // The form's seed: what the write carries without asking.
  const seed = useMemo(() => {
    if (!asksForm) return {}
    if (action.verb === "create") {
      return createSeed(action, host.spec, host.kind, host.ctx, record)
        .properties
    }
    return substituteSet(
      action.set,
      host.ctx,
      host.kind,
      `actions.${action.name}.set`,
      record
    ).properties
  }, [asksForm, action, host, record])
  const fields = useMemo(
    () => (asksForm ? promptFields(host.kind, action.prompt, host.kinds) : []),
    [asksForm, host.kind, action.prompt, host.kinds]
  )
  const seedLabels = useMemo(() => {
    const parent = host.ctx.parent
    if (!parent || !host.spec.via) return undefined
    return { [host.spec.via]: titleOf(parent.record) }
  }, [host.ctx.parent, host.spec.via])

  const noun = host.kind?.name ?? "record"
  // The confirm's body is the declared description when there is one, else
  // what it always said; beneath it, the record and, for a transition, the
  // move spelled out so "Hide" is never a mystery.
  const consequence = record
    ? `${titleOf(record)}${action.verb === "delete" ? " will be deleted." : ""}`
    : undefined
  const state =
    action.verb === "transition"
      ? stateSpecOf(host.kind, action.property)
      : undefined
  const from = state && record ? record.properties[state.name] : undefined
  const detail = record && (action.description || state) && (
    <>
      {action.description && (
        <span className="font-medium text-foreground">{consequence}</span>
      )}
      {state && action.to && (
        <span className="data text-xs">
          {state.name}: {typeof from === "string" && from ? from : "—"} →{" "}
          {action.to}
        </span>
      )}
    </>
  )
  const dialogs = (
    <>
      {asksForm && (
        <FormSheet
          open={formOpen}
          onOpenChange={(open) => {
            setFormOpen(open)
            if (!open) onSettled?.()
          }}
          title={action.label}
          description={
            action.verb === "create"
              ? `A new ${noun}.`
              : record
                ? titleOf(record)
                : undefined
          }
          kind={host.kind}
          kinds={host.kinds}
          fields={fields}
          seed={seed}
          seedLabels={seedLabels}
          mode={action.verb === "create" ? "create" : "patch"}
          record={action.verb === "patch" ? record : undefined}
          submitLabel={action.label}
          onSubmit={submit}
        />
      )}
      {asksConfirm && (
        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={(open) => {
            setConfirmOpen(open)
            // Under an open form the sequence is not over: the form is.
            if (!open && !formOpen) onSettled?.()
          }}
          title={`${action.label}?`}
          description={action.description ?? consequence}
          detail={detail}
          confirmLabel={action.label}
          destructive={action.verb === "delete"}
          pending={pending}
          onConfirm={confirm}
        />
      )}
    </>
  )

  return { run, pending, dialogs }
}

export function ActionButton({
  host,
  action,
  record,
  className,
  variant = "outline",
  /** Icon alone when the label is long: a row's trailing 44 px button. */
  compact,
  onSettled,
}: {
  host: ActionHost
  action: ActionSpec
  record?: SubstrateRecord
  className?: string
  variant?: "default" | "outline" | "ghost" | "secondary" | "destructive"
  compact?: boolean
  onSettled?: (outcome?: ActionOutcome) => void
}) {
  const { run, pending, dialogs } = useActionRunner(
    host,
    action,
    record,
    onSettled
  )
  const iconOnly = compact && action.label.length > 6
  return (
    <>
      <Button
        type="button"
        variant={variant}
        className={cn(iconOnly ? "size-11" : "h-11 gap-1.5 px-3", className)}
        aria-label={iconOnly ? action.label : undefined}
        title={action.description ?? (iconOnly ? action.label : undefined)}
        disabled={pending}
        onClick={run}
      >
        {pending ? (
          <Spinner className="size-4" />
        ) : (
          <ActionIcon action={action} className="size-4" />
        )}
        {!iconOnly && action.label}
      </Button>
      {dialogs}
    </>
  )
}

/** Start the action as soon as it mounts. A row menu item renders one of
 * these outside the menu, because a sheet inside a menu item unmounts with
 * the menu. */
export function ActionRunner({
  host,
  action,
  record,
  onSettled,
}: {
  host: ActionHost
  action: ActionSpec
  record?: SubstrateRecord
  onSettled: (outcome?: ActionOutcome) => void
}) {
  const { run, dialogs } = useActionRunner(host, action, record, onSettled)
  const started = useRef(false)
  useEffect(() => {
    if (started.current) return
    started.current = true
    run()
  }, [run])
  return <>{dialogs}</>
}
