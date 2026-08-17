/** Change-request detail (`/change-requests/:id`): the proposal's own case
 * (rationale, proposer, op), then what accepting it WOULD DO, per op. A patch
 * reads field by field against the target's live values, so a reviewer sees
 * whose value each row overwrites and what a `null` removes; a create previews
 * the record the accept would mint, because nothing exists to compare against
 * yet; a delete says plainly that it tombstones the record it names. The two
 * verdicts are one atomic submit each, CAS'd on the version of the request as
 * loaded (the write path refuses a decision without it, which is what keeps the
 * reviewed envelope the decided one), and a refused apply comes back as the
 * request's `substrate/conflict` annotation rather than as a half-applied
 * change. A decided request renders read-only: the decision, the decider, and
 * when. */

import { useMemo, useState, type ReactNode } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  AlertTriangleIcon,
  CheckIcon,
  FileQuestionIcon,
  FilePenLineIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"
import { useForm } from "react-hook-form"
import { z } from "zod"

import { ActorChip } from "@/components/actor-chip"
import { ChangeTarget, OpBadge } from "@/components/change-request"
import { StateBadge } from "@/components/state-badge"
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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/components/ui/toast"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  CR_COLLECTION,
  changeRequestQueryOptions,
  submitDecision,
} from "@/lib/api/changerequests"
import { CORE_AUTHORITY } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import { ApiError, type KindInfo, type SubstrateRecord } from "@/lib/api/types"
import {
  DECISION_INITIAL,
  appliesNothing,
  applyConflict,
  changeOp,
  changeTarget,
  decidedAtOf,
  deciderOf,
  decisionNote,
  decisionOf,
  deriveChangeRows,
  describeProposedEdge,
  diffCannotApply,
  diffNamesNothing,
  proposedDiff,
  proposerOf,
  rationaleOf,
  targetDrift,
  type ChangeEffect,
  type ChangeOp,
  type ChangeRow,
  type ChangeTargetRef,
  type ProposedDiff,
  type ProposedEdge,
  type UnreadableField,
  type Verdict,
} from "@/lib/changerequests"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { cellValue, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { changeRequestDetailRoute } from "@/router"

function targetLabel(target?: ChangeTargetRef): string {
  return target?.title || target?.id || "unknown"
}

// ── effect voice ────────────────────────────────────────────────────────────

const EFFECT_TEXT: Record<ChangeEffect, { label: string; explain: string }> = {
  clear: {
    label: "removes",
    explain:
      "The diff names null for this property, and a patch merges key-wise: accepting DELETES the key from the target.",
  },
  set: {
    label: "overwrites",
    explain:
      "The target already carries a value here; accepting replaces it with the proposed one.",
  },
  add: {
    label: "adds",
    explain:
      "The target carries no value for this property; accepting writes one.",
  },
  unchanged: {
    label: "no change",
    explain:
      "The target already carries the proposed value, so this row applies nothing.",
  },
}

function EffectCell({ effect }: { effect: ChangeEffect }) {
  const text = EFFECT_TEXT[effect]
  // Only the row that takes something away wears a chip; an ordinary write and
  // a no-op stay plain text, so the destructive row is the one the eye finds.
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className={cn(
              "inline-flex w-fit items-center text-xs whitespace-nowrap",
              effect === "clear"
                ? "rounded-sm border border-destructive/60 px-1.5 py-0.5 text-destructive"
                : "pt-0.5 text-muted-foreground"
            )}
          />
        }
      >
        {text.label}
      </TooltipTrigger>
      <TooltipContent className="max-w-72">{text.explain}</TooltipContent>
    </Tooltip>
  )
}

// ── values ──────────────────────────────────────────────────────────────────

function ValueCell({
  value,
  absent,
  manager,
}: {
  value: unknown
  /** True when the record simply has no key here: the empty-cell glyph, never a
   * rendering of `undefined`. */
  absent: boolean
  manager?: string
}) {
  if (absent) return <span className="data text-muted-foreground">—</span>
  if (value === null) {
    return <span className="data text-destructive">removed</span>
  }
  const text = cellValue(value)
  // A long repeated value truncates; the count says what the ellipsis hides,
  // and the full join rides the title.
  const count = Array.isArray(value) && value.length > 1 ? value.length : 0
  return (
    <span className="flex min-w-0 flex-col items-start gap-1">
      {text ? (
        <span className="flex w-full min-w-0 items-baseline gap-1.5">
          <span className="min-w-0 truncate data" title={text}>
            {text}
          </span>
          {count > 0 && (
            <span className="shrink-0 data text-xs text-muted-foreground">
              ×{count}
            </span>
          )}
        </span>
      ) : (
        <span className="data text-muted-foreground">
          {value === undefined ? "—" : '""'}
        </span>
      )}
      {manager && <ActorChip actor={manager} />}
    </span>
  )
}

function FieldCell({ row }: { row: ChangeRow }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={<span className="block truncate data text-muted-foreground" />}
      >
        {row.key}
      </TooltipTrigger>
      <TooltipContent className="max-w-72">
        {row.description ??
          (row.declared
            ? "declared property"
            : "not declared by the target's kind")}
      </TooltipContent>
    </Tooltip>
  )
}

/** The patch's before/after, in the table system's anatomy: fixed columns,
 * bordered rows, muted lowercase headers. It stays a comparison, not a list,
 * so there is no column dropdown and no pages. */
function BeforeAfter({
  rows,
  target,
  emptyText,
}: {
  rows: ChangeRow[]
  target?: ChangeTargetRef
  /** What an EMPTY table means here, which is never simply "nothing": a
   * refused decode, or a change that lives in the labels/finalizers instead. */
  emptyText: string
}) {
  return (
    <div className="mx-6 mb-4 overflow-x-auto rounded-md border">
      <Table className="table-fixed [&_td]:py-2.5" style={{ minWidth: 640 }}>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-4" style={{ width: 140 }}>
              field
            </TableHead>
            <TableHead className="py-2">
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate font-medium text-foreground">
                  {targetLabel(target)}{" "}
                  <span className="data font-normal text-muted-foreground">
                    {target?.id}
                  </span>
                </span>
                <span className="text-[0.65rem] font-medium tracking-wide text-muted-foreground uppercase">
                  now
                </span>
              </span>
            </TableHead>
            <TableHead className="py-2">
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate font-medium text-foreground">
                  proposed
                </span>
                <span className="text-[0.65rem] font-medium tracking-wide text-primary uppercase">
                  if accepted
                </span>
              </span>
            </TableHead>
            <TableHead className="pr-4" style={{ width: 130 }}>
              the accept
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell
                colSpan={4}
                className="px-4 text-xs text-muted-foreground"
              >
                {emptyText}
              </TableCell>
            </TableRow>
          )}
          {rows.map((row) => (
            <TableRow key={row.key} className="hover:bg-muted/30">
              <TableCell className="pl-4 align-top">
                <FieldCell row={row} />
              </TableCell>
              <TableCell className="align-top">
                <ValueCell
                  value={row.before}
                  absent={row.before === undefined}
                  manager={row.effect === "unchanged" ? undefined : row.manager}
                />
              </TableCell>
              <TableCell className="align-top">
                <ValueCell value={row.after} absent={false} />
              </TableCell>
              <TableCell className="pr-4 align-top">
                <EffectCell effect={row.effect} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

/** One edge a create would write, with the EDGE's own properties beside it:
 * they are reviewed content like any value, and the accept writes them. */
function EdgeRows({ edges, kind }: { edges: ProposedEdge[]; kind?: KindInfo }) {
  return (
    <>
      {edges.map((edge) => {
        const declaration = describeProposedEdge(edge, kind)
        const props = Object.entries(edge.properties ?? {})
        return (
          <TableRow key={`edge-${edge.rel}-${edge.id}`}>
            <TableCell className="pl-4 align-top">
              <Tooltip>
                <TooltipTrigger
                  render={
                    <span className="block truncate data text-muted-foreground" />
                  }
                >
                  {edge.rel}
                </TooltipTrigger>
                <TooltipContent className="max-w-72">
                  {declaration.description ??
                    (declaration.declared
                      ? "declared edge"
                      : "not declared by the target's kind")}
                </TooltipContent>
              </Tooltip>
            </TableCell>
            <TableCell className="pr-4 align-top">
              <span className="flex min-w-0 flex-col gap-1">
                <span className="flex min-w-0 items-baseline gap-1.5">
                  <span className="text-muted-foreground" aria-hidden>
                    →
                  </span>
                  <span className="min-w-0 truncate data" title={edge.id}>
                    {edge.id}
                  </span>
                  {edge.kind && (
                    <span className="shrink-0 data text-xs text-muted-foreground">
                      {splitKind(edge.kind).name}
                    </span>
                  )}
                </span>
                {props.length > 0 && (
                  <span className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 pl-4 text-xs">
                    {props.map(([key, value]) => (
                      <span key={key} className="contents">
                        <span className="max-w-40 truncate data text-muted-foreground">
                          {key}
                        </span>
                        <span
                          className="min-w-0 truncate data"
                          title={cellValue(value)}
                        >
                          {value === null ? "removed" : cellValue(value)}
                        </span>
                      </span>
                    ))}
                  </span>
                )}
              </span>
            </TableCell>
          </TableRow>
        )
      })}
    </>
  )
}

/** The proposed values alone: the record a create would mint, and the fallback
 * whenever the live target cannot be read (a value with nothing to compare it
 * to must not be dressed up as a comparison). */
function ProposedValues({
  rows,
  edges,
  kind,
  caption,
  emptyText,
}: {
  rows: ChangeRow[]
  edges: ProposedEdge[]
  kind?: KindInfo
  caption: string
  /** What an EMPTY table means here, which is never simply "nothing": a
   * refused decode, or a change that lives in the labels/finalizers instead. */
  emptyText: string
}) {
  return (
    <div className="mx-6 mb-4 overflow-x-auto rounded-md border">
      <Table className="table-fixed [&_td]:py-2.5" style={{ minWidth: 480 }}>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-4" style={{ width: 180 }}>
              field
            </TableHead>
            <TableHead className="pr-4">{caption}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 && edges.length === 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell
                colSpan={2}
                className="px-4 text-xs text-muted-foreground"
              >
                {emptyText}
              </TableCell>
            </TableRow>
          )}
          {rows.map((row) => (
            <TableRow key={row.key} className="hover:bg-muted/30">
              <TableCell className="pl-4 align-top">
                <FieldCell row={row} />
              </TableCell>
              <TableCell className="pr-4 align-top">
                <ValueCell value={row.after} absent={false} />
              </TableCell>
            </TableRow>
          ))}
          <EdgeRows edges={edges} kind={kind} />
        </TableBody>
      </Table>
    </div>
  )
}

// ── warnings ────────────────────────────────────────────────────────────────

function Warning({
  tone = "warning",
  children,
}: {
  tone?: "warning" | "destructive"
  children: ReactNode
}) {
  return (
    <div
      className={cn(
        "mx-6 mb-4 flex items-start gap-3 rounded-md border px-4 py-3 text-sm",
        tone === "destructive"
          ? "border-destructive/40 bg-destructive/5"
          : "border-warning/40 bg-warning/5"
      )}
    >
      <AlertTriangleIcon
        className={cn(
          "mt-0.5 size-4 shrink-0",
          tone === "destructive" ? "text-destructive" : "text-warning"
        )}
      />
      <div className="min-w-0 space-y-1">{children}</div>
    </div>
  )
}

// ── the decision dialog ─────────────────────────────────────────────────────

const noteSchema = z.object({
  note: z.string().max(2000, "Keep the note under 2000 characters.").optional(),
})
type NoteValues = z.infer<typeof noteSchema>

const ACCEPT_TEXT: Record<ChangeOp, string> = {
  patch:
    "The substrate applies the diff to the target in the same transaction as this decision, checked against the version the diff was computed for: if the target has moved, nothing partial happens, the decision fails whole and the request says why.",
  create:
    "The substrate mints the named record in the same transaction as this decision. It is create-if-absent: an id already holding the very record proposed is a verified no-op, and any other collision fails the decision whole.",
  delete:
    "The substrate tombstones the target in the same transaction as this decision. The record stops answering reads; the changelog keeps what it was.",
}

function DecisionDialog({
  verdict,
  op,
  target,
  version,
  busy,
  onConfirm,
  onClose,
}: {
  verdict: Verdict
  op?: ChangeOp
  target?: ChangeTargetRef
  version: number
  busy: boolean
  onConfirm: (note?: string) => void
  onClose: () => void
}) {
  const form = useForm<NoteValues>({
    resolver: zodResolver(noteSchema),
    defaultValues: { note: "" },
  })
  const accepting = verdict === "accepted"
  const destructive = accepting && op === "delete"

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          className="contents"
          onSubmit={form.handleSubmit((values) => onConfirm(values.note))}
        >
          <DialogHeader>
            <DialogTitle>
              {!accepting
                ? "Reject this change?"
                : op === "delete"
                  ? `Delete ${targetLabel(target)}?`
                  : op === "create"
                    ? `Create ${targetLabel(target)}?`
                    : `Apply this change to ${targetLabel(target)}?`}
            </DialogTitle>
            <DialogDescription className="space-y-2">
              <span className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 rounded-sm border bg-muted/40 px-2.5 py-1.5 text-xs">
                <span>op</span>
                <span className="min-w-0 truncate data text-foreground">
                  {op ?? "unknown"}
                </span>
                <span>target</span>
                <span className="min-w-0 truncate text-foreground">
                  {targetLabel(target)}{" "}
                  <span className="data text-muted-foreground">
                    {target?.id}
                  </span>
                </span>
              </span>
              {accepting ? (
                <span className="block">{op ? ACCEPT_TEXT[op] : ""}</span>
              ) : (
                <span className="block">
                  Nothing is applied: the target stays exactly as it is and only
                  the decision is written. The request is kept as the record of
                  the refusal.
                </span>
              )}
              <span className="block">
                Decided against version <span className="data">{version}</span>{" "}
                of this request, so a change to the proposal since it was read
                refuses the decision rather than deciding something else.
              </span>
            </DialogDescription>
          </DialogHeader>
          <Field data-invalid={!!form.formState.errors.note || undefined}>
            <FieldLabel htmlFor="decision-note">Note (optional)</FieldLabel>
            <Textarea
              id="decision-note"
              rows={2}
              placeholder={
                accepting ? "why this is right…" : "why this is wrong…"
              }
              aria-invalid={!!form.formState.errors.note}
              {...form.register("note")}
            />
            <FieldDescription>
              Saved with the decision, as{" "}
              <span className="data">owner/note</span> on this request.
            </FieldDescription>
            <FieldError errors={[form.formState.errors.note]} />
          </Field>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={onClose}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant={destructive ? "destructive" : "default"}
              disabled={busy}
            >
              {busy && <Spinner className="size-3.5" />}
              {!accepting
                ? "Reject"
                : op === "delete"
                  ? "Accept and delete"
                  : op === "create"
                    ? "Accept and create"
                    : "Accept and apply"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** What an empty value table means, which is never simply "nothing proposed":
 * the decode refuses part of the diff, or the change is real but lives in the
 * labels, annotations or finalizers listed under the table, or the diff truly
 * names nothing and the substrate will refuse the accept for it. */
function emptyTableText(diff: ProposedDiff, blocked: boolean): string {
  if (blocked) {
    return "Nothing readable here: what the diff carries is listed below, as it is stored."
  }
  if (!diffNamesNothing(diff)) {
    return "No property is named. What the accept would write is listed under this table."
  }
  return "The diff names nothing at all, so accepting would apply nothing: the substrate refuses that decision."
}

// ── the page ────────────────────────────────────────────────────────────────

/** The live target, read only where there is one to read: a create's target
 * does not exist yet, and a decided request's target has moved on, so neither
 * is compared against. */
function useTargetQuery(
  target: ChangeTargetRef | undefined,
  types: KindInfo[],
  enabled: boolean
) {
  const kind = target ? kindByIdentity(types, target.kind) : undefined
  const authority = target ? splitKind(target.kind).authority : ""
  return {
    kind,
    query: useQuery({
      ...recordQueryOptions(authority, kind?.name ?? "", target?.id ?? ""),
      enabled: enabled && Boolean(target && kind),
    }),
  }
}

export function ChangeRequestDetailPage() {
  const { id } = changeRequestDetailRoute.useParams()
  const queryClient = useQueryClient()

  const registry = useQuery(kindsQueryOptions)
  const cr = useQuery(changeRequestQueryOptions(id))
  const types = registry.data ?? []

  const op = cr.data ? changeOp(cr.data) : undefined
  const target = cr.data ? changeTarget(cr.data) : undefined
  const decision = cr.data ? decisionOf(cr.data) : undefined
  const proposed = decision === "proposed"
  const targetSide = useTargetQuery(target, types, proposed && op !== "create")

  const [confirming, setConfirming] = useState<Verdict | null>(null)

  const decide = useMutation({
    mutationFn: ({ v, note }: { v: Verdict; note?: string }) =>
      // The version is the request AS LOADED: the write path requires it, and
      // it is what makes the decided envelope the reviewed one.
      submitDecision(id, v, cr.data?.version ?? 0, note),
    onSuccess: (_data, { v }) => {
      setConfirming(null)
      toast.add({
        type: "success",
        title: v === "accepted" ? "Applied." : "Rejected, nothing changed.",
      })
      // An accept writes another record entirely, plus the changelog, the
      // counts and the queue. Drop everything and re-read.
      void queryClient.invalidateQueries()
    },
    onError: (error, { v }) => {
      setConfirming(null)
      const conflict = error instanceof ApiError && error.code === "conflict"
      toast.add({
        type: "error",
        title: conflict
          ? "The request moved, or the apply was refused"
          : `Could not ${v === "accepted" ? "accept" : "reject"} the request`,
        description: conflict
          ? `${error.message} Reloaded: read it again, and re-propose if the target has moved.`
          : error.message,
      })
      // Either way it moved under us: the re-read shows the real current state
      // and the server's own account of a refused apply.
      void queryClient.invalidateQueries()
    },
  })

  if (cr.isPending || registry.isPending) return <DetailSkeleton />

  if (cr.isError) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileQuestionIcon />
            </EmptyMedia>
            <EmptyTitle>The change request didn't load</EmptyTitle>
            <EmptyDescription>
              <span className="data">{id}</span>: {cr.error.message}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void cr.refetch()}
            >
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  const request = cr.data
  const diff = proposedDiff(request)
  const rationale = rationaleOf(request)
  const decidedAt = decidedAtOf(request)
  const proposer = proposerOf(request)
  const decider = deciderOf(request)
  const note = decisionNote(request)
  const conflict = applyConflict(request)

  const targetRecord = targetSide.query.data
  // The CAS the accept will actually use, which is the diff's own `ifVersion`
  // wherever it carries one: only a patch checks it.
  const drift =
    op === "patch" ? targetDrift(request, diff, targetRecord) : undefined
  const comparable = op === "patch" && Boolean(targetRecord)
  const blocked = diffCannotApply(diff)
  const emptyText = emptyTableText(diff, blocked)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="flex min-w-0 items-center gap-2 truncate text-lg font-semibold">
            <OpBadge op={op} />
            <ChangeTarget target={target} types={types} />
          </h1>
          <p className="data text-xs text-muted-foreground">
            {CORE_AUTHORITY}/{CR_COLLECTION}/{request.id}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2 pt-0.5">
          {decision && (
            <StateBadge value={decision} initial={DECISION_INITIAL} />
          )}
          {proposed && (
            <>
              <Button
                variant="outline"
                size="sm"
                disabled={decide.isPending}
                onClick={() => setConfirming("rejected")}
              >
                <XIcon className="size-3.5" />
                Reject
              </Button>
              <Button
                size="sm"
                variant={op === "delete" ? "destructive" : "default"}
                disabled={decide.isPending}
                onClick={() => setConfirming("accepted")}
              >
                <CheckIcon className="size-3.5" />
                Accept
              </Button>
            </>
          )}
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        {/* the proposal's own case */}
        <div className="mx-6 mb-4 flex flex-col gap-2 rounded-md border bg-muted/40 px-4 py-3 text-sm">
          {rationale ? (
            <p>{rationale}</p>
          ) : (
            <p className="text-muted-foreground">
              The proposal carries no rationale.
            </p>
          )}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-muted-foreground">
            {proposer && (
              <span className="flex items-center gap-1.5">
                proposed by <ActorChip actor={proposer} />
              </span>
            )}
            <span className="data" title={request.createdAt}>
              {relativeTime(request.createdAt)}
            </span>
            {decidedAt && (
              <span className="flex items-center gap-1.5">
                decided{" "}
                <span className="data" title={decidedAt}>
                  {relativeTime(decidedAt)}
                </span>
                {decider && (
                  <>
                    by <ActorChip actor={decider} />
                  </>
                )}
              </span>
            )}
          </div>
          {note && (
            <p className="text-xs">
              <span className="text-muted-foreground">note:</span> {note}
            </p>
          )}
        </div>

        {/* the server's account of a refused apply */}
        {conflict && (
          <Warning>
            <p className="font-medium">
              The substrate refused to apply this change.
            </p>
            <p className="data text-xs">{conflict.reason}</p>
            {conflict.at && (
              <p className="text-xs text-muted-foreground" title={conflict.at}>
                {relativeTime(conflict.at)}
              </p>
            )}
          </Warning>
        )}

        {!op && (
          <Warning>
            <p>
              This request names an op the console does not know (
              <span className="data">{String(request.properties.op)}</span>), so
              it cannot say what accepting would do. The substrate refuses the
              accept too: reject it, or re-propose the change.
            </p>
          </Warning>
        )}

        {diff.unreadable && (
          <Warning>
            <p>
              The stored <span className="data">diff</span> is not an object, so
              nothing can be read out of it. Accepting fails the decode.
            </p>
          </Warning>
        )}

        {diff.refused.length > 0 && (
          <Warning>
            <p>
              The diff names keys the substrate's decoder refuses (
              <span className="data">{diff.refused.join(", ")}</span>), so the
              accept fails whole. Property values belong under{" "}
              <span className="data">properties</span>
              {op === "create" ? (
                <>
                  {" "}
                  and edges under <span className="data">edges</span>
                </>
              ) : null}
              .
            </p>
          </Warning>
        )}

        {/* Independent of the refused-key warning: a diff can carry both, and
            each is its own reason no accept can succeed. */}
        {diff.malformed.length > 0 && (
          <Warning>
            <p>
              Part of this diff is stored in a shape the substrate's decoder
              refuses, so no accept can succeed until it is proposed again: the
              keys and their stored values are listed below.
            </p>
          </Warning>
        )}

        {drift && (
          <Warning>
            <p>
              The target has moved. The accept checks it against version{" "}
              <span className="data">{drift.version}</span>
              {drift.via === "diff.ifVersion"
                ? " (the diff's own ifVersion, which overrides the stamped targetVersion)"
                : " (the stamped targetVersion)"}
              , and the record is now at{" "}
              <span className="data">{drift.current}</span>. Accepting is
              REFUSED while they differ, rather than overwriting a write nobody
              reviewed, so the change has to be proposed again.
            </p>
          </Warning>
        )}

        {/* what accepting would do */}
        {proposed && op === "delete" && (
          <>
            <Warning tone="destructive">
              <p className="font-medium">
                Accepting DELETES{" "}
                <span className="data">{target?.id ?? "the target"}</span>.
              </p>
              <p>
                The record is tombstoned: it stops answering reads and every
                edge pointing at it dangles. The changelog keeps what it was, so
                the history survives, but the record does not come back on its
                own.
              </p>
            </Warning>
            {targetRecord && (
              <DeleteSummary record={targetRecord} kind={targetSide.kind} />
            )}
          </>
        )}

        {proposed && op !== "delete" && (
          <PatchBody
            request={request}
            diff={diff}
            op={op}
            target={target}
            targetRecord={targetRecord}
            targetKind={targetSide.kind}
            comparable={comparable}
            loading={targetSide.query.isLoading}
            error={targetSide.query.error?.message}
            kindMissing={Boolean(target && !targetSide.kind)}
            emptyText={emptyText}
          />
        )}

        {/* the decided record */}
        {!proposed && (
          <div className="mx-6 mb-4 flex items-start gap-3 rounded-md border px-4 py-3 text-sm">
            {decision === "accepted" ? (
              <>
                {op === "delete" ? (
                  <Trash2Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                ) : (
                  <FilePenLineIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                )}
                <p>
                  Applied
                  {decidedAt ? ` ${relativeTime(decidedAt)}` : ""}: the change
                  landed on <span className="data">{target?.id}</span> in the
                  same transaction as the decision. The record below is what was
                  reviewed, frozen since it was proposed; the target's own page
                  and the changelog are where it stands now.
                </p>
              </>
            ) : (
              <>
                <XIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <p>
                  Rejected
                  {decidedAt ? ` ${relativeTime(decidedAt)}` : ""}: nothing was
                  applied and <span className="data">{target?.id}</span> was
                  left exactly as it was. This request is the record of the
                  refusal.
                </p>
              </>
            )}
          </div>
        )}

        {/* a decided request still shows WHAT was proposed, read-only */}
        {!proposed && op !== "delete" && (
          <ProposedValues
            rows={deriveChangeRows(diff.properties, undefined, targetSide.kind)}
            edges={op === "create" ? diff.edges : []}
            kind={targetSide.kind}
            caption="proposed value"
            emptyText={emptyText}
          />
        )}

        <ExtraDiffKeys diff={diff} />
        <UnreadableFields fields={diff.malformed} />
      </ScrollArea>

      {confirming && (
        <DecisionDialog
          verdict={confirming}
          op={op}
          target={target}
          version={request.version}
          busy={decide.isPending}
          onConfirm={(note) => decide.mutate({ v: confirming, note })}
          onClose={() => setConfirming(null)}
        />
      )}
    </div>
  )
}

/** The body a patch or a create shows: the before/after where the live target
 * can be read, the proposed values alone where it cannot. */
function PatchBody({
  request,
  diff,
  op,
  target,
  targetRecord,
  targetKind,
  comparable,
  loading,
  error,
  kindMissing,
  emptyText,
}: {
  request: SubstrateRecord
  diff: ProposedDiff
  op?: ChangeOp
  target?: ChangeTargetRef
  targetRecord?: SubstrateRecord
  targetKind?: KindInfo
  comparable: boolean
  loading: boolean
  error?: string
  kindMissing: boolean
  emptyText: string
}) {
  const rows = useMemo(
    () =>
      deriveChangeRows(
        diff.properties,
        comparable ? targetRecord : undefined,
        targetKind
      ),
    [diff.properties, comparable, targetRecord, targetKind]
  )
  // A patch that would apply nothing fails the accept: the write path refuses a
  // green decision that changed nothing. A label, an annotation or a finalizer
  // riding along means it DOES apply something, so the warning stays quiet.
  const noop = op === "patch" && comparable && appliesNothing(diff, rows)

  if (op === "patch" && !comparable) {
    if (loading) {
      return (
        <div className="mx-6 mb-4">
          <Skeleton className="h-24 w-full rounded-md" />
        </div>
      )
    }
    return (
      <>
        <Warning>
          <p>
            {error
              ? `The target didn't load, so the current values are unknown: ${error}`
              : kindMissing
                ? "The target's kind isn't in this repository's registry, so its current values can't be read. The decision buttons still work."
                : `This request names no target to compare against. A patch request carries one as its target edge; accepting is refused without it.`}
          </p>
        </Warning>
        <ProposedValues
          rows={rows}
          edges={[]}
          kind={targetKind}
          caption="proposed value"
          emptyText={emptyText}
        />
      </>
    )
  }

  return (
    <>
      {noop && (
        <Warning>
          <p>
            Every proposed value is already what the target carries, so this
            diff applies nothing. The substrate refuses that accept rather than
            recording a decision that did nothing: reject it.
          </p>
        </Warning>
      )}
      {op === "create" && (
        <p className="mx-6 mb-2 text-sm text-muted-foreground">
          Accepting mints{" "}
          <span className="data">{target?.id ?? request.id}</span> as{" "}
          <span className="data">{target?.kind}</span> with exactly the values
          below. It is create-if-absent: an id already holding this record is a
          no-op, any other collision fails the decision.
        </p>
      )}
      {/* Only a patch has a live side to compare against; a create, and an op
          the console cannot read, show the proposed values alone rather than
          dressing them up as a comparison with nothing. */}
      {op === "patch" ? (
        <BeforeAfter rows={rows} target={target} emptyText={emptyText} />
      ) : (
        <ProposedValues
          rows={rows}
          edges={op === "create" ? diff.edges : []}
          kind={targetKind}
          caption={op === "create" ? "the accept writes" : "proposed value"}
          emptyText={emptyText}
        />
      )}
    </>
  )
}

/** What a delete would tombstone, in the record's own voice: enough of the
 * target to recognize it, and nothing that pretends to be the whole record. */
function DeleteSummary({
  record,
  kind,
}: {
  record: SubstrateRecord
  kind?: KindInfo
}) {
  const facts = Object.entries(record.properties)
    .filter(([, value]) => value !== null && value !== undefined)
    .slice(0, 6)
  return (
    <div className="mx-6 mb-4 rounded-md border px-4 py-3 text-sm">
      <p className="mb-2 text-xs text-muted-foreground">
        {kind?.name ?? record.kind} <span className="data">{record.id}</span>,
        version <span className="data">{record.version}</span>, last written{" "}
        <span className="data" title={record.updatedAt}>
          {relativeTime(record.updatedAt)}
        </span>
      </p>
      <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
        {facts.map(([key, value]) => (
          <span key={key} className="contents">
            <span className="max-w-40 truncate data text-muted-foreground">
              {key}
            </span>
            <span className="min-w-0 truncate data" title={cellValue(value)}>
              {cellValue(value)}
            </span>
          </span>
        ))}
      </div>
    </div>
  )
}

/** Everything else the accept would write or check, none of which fits the
 * property table: labels, annotations, the finalizers a patch adds or removes,
 * and the diff's own `ifVersion` (which OVERRIDES the stamped targetVersion, so
 * a reviewer must be able to see it). Applied unseen is not an option. */
function ExtraDiffKeys({ diff }: { diff: ProposedDiff }) {
  const groups: Array<[string, Array<[string, unknown]>]> = []
  if (diff.labels && Object.keys(diff.labels).length) {
    groups.push(["labels", Object.entries(diff.labels)])
  }
  if (diff.annotations && Object.keys(diff.annotations).length) {
    groups.push(["annotations", Object.entries(diff.annotations)])
  }
  if (diff.addFinalizers.length) {
    groups.push([
      "finalizers it adds",
      diff.addFinalizers.map((f) => [f, "added"] as [string, unknown]),
    ])
  }
  if (diff.removeFinalizers.length) {
    groups.push([
      "finalizers it removes",
      diff.removeFinalizers.map((f) => [f, "removed"] as [string, unknown]),
    ])
  }
  if (diff.ifVersion !== undefined) {
    groups.push([
      "the version it checks the target against",
      [["ifVersion", diff.ifVersion]],
    ])
  }
  if (!groups.length) return null
  return (
    <div className="mx-6 mb-4 flex flex-col gap-2 rounded-md border px-4 py-3 text-sm">
      {groups.map(([name, values]) => (
        <div key={name}>
          <p className="text-xs text-muted-foreground">
            {name === "labels" || name === "annotations"
              ? `the accept also writes ${name}`
              : name}
          </p>
          <div className="mt-1 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
            {values.map(([key, value]) => (
              <span key={key} className="contents">
                <span className="max-w-40 truncate data text-muted-foreground">
                  {key}
                </span>
                <span className="min-w-0 truncate data">
                  {value === null ? "removed" : cellValue(value)}
                </span>
              </span>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

/** What the diff carries where the decode expects a shape and finds another:
 * the key and the raw value, verbatim. The accept fails the strict decode on
 * every one of these, so the reviewer sees WHAT is wrong rather than a table
 * that looks merely empty. */
function UnreadableFields({ fields }: { fields: UnreadableField[] }) {
  if (!fields.length) return null
  return (
    <div className="mx-6 mb-4 rounded-md border border-warning/40 px-4 py-3 text-sm">
      <p className="text-xs text-muted-foreground">
        stored values the substrate's decoder cannot read
      </p>
      <div className="mt-1 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
        {fields.map((field) => {
          const raw = JSON.stringify(field.raw) ?? String(field.raw)
          return (
            <span key={field.key} className="contents">
              <span className="max-w-40 truncate data text-warning">
                {field.key}
              </span>
              <span className="min-w-0 truncate data" title={raw}>
                {raw}
              </span>
            </span>
          )
        })}
      </div>
    </div>
  )
}

/** Mirrors the final layout: header, the proposal's band, the value table. */
function DetailSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-3">
        <Skeleton className="h-6 w-72" />
        <Skeleton className="mt-1.5 h-3.5 w-80" />
      </div>
      <div className="flex flex-col gap-3 px-6">
        <Skeleton className="h-20 w-full rounded-md" />
        <Skeleton className="h-48 w-full rounded-md" />
      </div>
    </div>
  )
}
