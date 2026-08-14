/** Merge request detail (`/merge-requests/:id`): the evidence, then the
 * field-by-field side-by-side of the two records that SAYS what the merge
 * will do per row — machine-held differences are settled by the post-merge
 * recompute from the union of live sources; owner-held values yield to
 * nobody (§7.1), so a differing one is the owner's explicit choice. The two
 * verdicts are single atomic submits behind a Dialog that states the
 * recompute/reversibility posture (`recordsplit` exists); an optional note
 * rides the same write as `owner/note`. A resolved request keeps its record:
 * state, decider, note, and the door to the survivor. */

import { useMemo, useState } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  CheckIcon,
  ChevronRightIcon,
  FileQuestionIcon,
  GitMergeIcon,
  XIcon,
} from "lucide-react"
import { useForm } from "react-hook-form"
import { z } from "zod"

import { ActorChip } from "@/components/actor-chip"
import { RecordPeek } from "@/components/record-peek"
import { StateBadge } from "@/components/state-badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
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
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/components/ui/toast"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { recordQueryOptions } from "@/lib/api/records"
import {
  mergeRequestQueryOptions,
  submitVerdict,
} from "@/lib/api/mergerequests"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { EdgeTarget, SubstrateRecord, KindInfo } from "@/lib/api/types"
import { cellValue, relativeTime } from "@/lib/format"
import {
  DECISION_INITIAL,
  decisionOf,
  deriveDiff,
  verdictNote,
  type DiffPosture,
  type DiffRow,
  type MergeVerdict,
} from "@/lib/mergerequests"
import { splitKind, kindByIdentity } from "@/lib/definition"
import { cn } from "@/lib/utils"
import { EvidenceChips } from "@/components/merge-request"
import { mergeRequestDetailRoute } from "@/router"

function refTitle(ref?: EdgeTarget): string {
  return ref?.title || ref?.id || "unknown"
}

// ── posture voice ───────────────────────────────────────────────────────────

const POSTURE_TEXT: Record<
  Exclude<DiffPosture, "equal">,
  { label: string; explain: string }
> = {
  choice: {
    label: "your choice",
    explain:
      "Held by you on at least one side. Recompute yields to you (§7.1), so the surviving value stands as-is after the merge — if the other value is the right one, edit the survivor afterwards.",
  },
  recompute: {
    label: "recompute settles",
    explain:
      "Machine-held. Values never migrate in a merge — after it, the survivor re-derives this property from the union of both records' live sources.",
  },
  moves: {
    label: "moves to survivor",
    explain:
      "The merged-away record's edges re-point at the survivor; a duplicate edge collapses into one.",
  },
}

function PostureCell({ posture }: { posture: DiffPosture }) {
  if (posture === "equal") {
    return <span className="text-xs text-muted-foreground">already agree</span>
  }
  const text = POSTURE_TEXT[posture]
  // Only the row that needs a person wears a chip; the machine's own work
  // stays plain text (codex finding, 2026-08-06 — bordered muted read as a
  // disabled button).
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className={cn(
              "inline-flex w-fit items-center text-xs whitespace-nowrap",
              posture === "choice"
                ? "rounded-sm border border-warning/60 px-1.5 py-0.5 text-warning"
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

// ── side-by-side ────────────────────────────────────────────────────────────

function ValueCell({
  row,
  side,
  types,
}: {
  row: DiffRow
  side: "loser" | "winner"
  types: KindInfo[]
}) {
  const value = side === "loser" ? row.loser : row.winner
  const manager = side === "loser" ? row.loserManager : row.winnerManager

  if (row.kind === "edge") {
    const targets = (value as EdgeTarget[]) ?? []
    if (!targets.length)
      return <span className="data text-muted-foreground">—</span>
    return (
      <span className="flex min-w-0 flex-col gap-0.5">
        {targets.map((t) => (
          <span key={t.id} className="min-w-0 truncate data">
            <RecordPeek target={t} types={types} />
          </span>
        ))}
      </span>
    )
  }

  const text = value === undefined || value === null ? "" : cellValue(value)
  // A long repeated value truncates; the count says what the ellipsis hides
  // (codex finding, 2026-08-06). The full join rides the title.
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
        <span className="data text-muted-foreground">—</span>
      )}
      {manager && row.posture !== "equal" && <ActorChip actor={manager} />}
    </span>
  )
}

/** The diff rides the table system's look (owner ruling, 2026-08-06): real
 * table anatomy — fixed columns, bordered rows, muted lowercase headers —
 * though it stays a comparison, not a list, so no column dropdown or pages. */
function DiffRows({ rows, types }: { rows: DiffRow[]; types: KindInfo[] }) {
  return (
    <>
      {rows.map((row) => (
        <TableRow key={`${row.kind}-${row.key}`} className="hover:bg-muted/30">
          <TableCell className="pl-4 align-top">
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="block truncate data text-muted-foreground" />
                }
              >
                {row.key}
              </TooltipTrigger>
              {row.description ? (
                <TooltipContent className="max-w-72">
                  {row.description}
                </TooltipContent>
              ) : (
                <TooltipContent>
                  {row.declared
                    ? row.kind === "edge"
                      ? "declared edge"
                      : "declared property"
                    : "not declared by the schema"}
                </TooltipContent>
              )}
            </Tooltip>
          </TableCell>
          <TableCell className="align-top">
            <ValueCell row={row} side="loser" types={types} />
          </TableCell>
          <TableCell className="align-top">
            <ValueCell row={row} side="winner" types={types} />
          </TableCell>
          <TableCell className="pr-4 align-top">
            <PostureCell posture={row.posture} />
          </TableCell>
        </TableRow>
      ))}
    </>
  )
}

function SideBySide({
  loser,
  winner,
  type,
  types,
}: {
  loser: SubstrateRecord
  winner: SubstrateRecord
  type?: KindInfo
  types: KindInfo[]
}) {
  const rows = useMemo(
    () => deriveDiff(winner, loser, type),
    [winner, loser, type]
  )
  const open = rows.filter((r) => r.posture !== "equal")
  const equal = rows.filter((r) => r.posture === "equal")
  const [showEqual, setShowEqual] = useState(false)

  return (
    <div className="mx-6 mb-4 overflow-x-auto rounded-md border">
      <Table className="table-fixed [&_td]:py-2.5" style={{ minWidth: 640 }}>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-4" style={{ width: 140 }}>
              field
            </TableHead>
            {/* the qualifiers carry the direction — twins share a name, so
                they must not whisper (codex finding, 2026-08-06) */}
            <TableHead className="py-2">
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate font-medium text-foreground">
                  {refTitle({
                    id: loser.id,
                    kind: loser.kind,
                    title: String(loser.properties.title ?? ""),
                  })}{" "}
                  <span className="data font-normal text-muted-foreground">
                    {loser.id}
                  </span>
                </span>
                <span className="text-[0.65rem] font-medium tracking-wide text-muted-foreground uppercase">
                  merges away
                </span>
              </span>
            </TableHead>
            <TableHead className="py-2">
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate font-medium text-foreground">
                  {refTitle({
                    id: winner.id,
                    kind: winner.kind,
                    title: String(winner.properties.title ?? ""),
                  })}{" "}
                  <span className="data font-normal text-muted-foreground">
                    {winner.id}
                  </span>
                </span>
                <span className="text-[0.65rem] font-medium tracking-wide text-primary uppercase">
                  survives
                </span>
              </span>
            </TableHead>
            <TableHead className="pr-4" style={{ width: 150 }}>
              after the merge
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {open.length > 0 ? (
            <DiffRows rows={open} types={types} />
          ) : (
            <TableRow className="hover:bg-transparent">
              <TableCell
                colSpan={4}
                className="px-4 text-xs text-muted-foreground"
              >
                No differences — every field the pair carries already agrees.
              </TableCell>
            </TableRow>
          )}
          {equal.length > 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={4} className="p-0">
                <button
                  type="button"
                  aria-expanded={showEqual}
                  className="flex w-full cursor-pointer items-center gap-1 px-4 py-2 text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => setShowEqual((v) => !v)}
                >
                  <ChevronRightIcon
                    className={cn(
                      "size-3 transition-transform",
                      showEqual && "rotate-90"
                    )}
                  />
                  {equal.length === 1
                    ? "1 identical field"
                    : `${equal.length} identical fields`}
                </button>
              </TableCell>
            </TableRow>
          )}
          {showEqual && <DiffRows rows={equal} types={types} />}
        </TableBody>
      </Table>
    </div>
  )
}

// ── the verdict dialog ──────────────────────────────────────────────────────

const noteSchema = z.object({
  note: z.string().max(2000, "Keep the note under 2000 characters.").optional(),
})
type NoteValues = z.infer<typeof noteSchema>

function VerdictDialog({
  verdict,
  loser,
  winner,
  busy,
  onConfirm,
  onClose,
}: {
  verdict: MergeVerdict
  loser?: EdgeTarget
  winner?: EdgeTarget
  busy: boolean
  onConfirm: (note?: string) => void
  onClose: () => void
}) {
  const loserTitle = refTitle(loser)
  const winnerTitle = refTitle(winner)
  const form = useForm<NoteValues>({
    resolver: zodResolver(noteSchema),
    defaultValues: { note: "" },
  })
  const approving = verdict === "accepted"

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          className="contents"
          onSubmit={form.handleSubmit((values) => onConfirm(values.note))}
        >
          <DialogHeader>
            <DialogTitle>
              {approving
                ? `Merge ${loserTitle} into ${winnerTitle}?`
                : "Reject this suggestion?"}
            </DialogTitle>
            <DialogDescription className="space-y-2">
              {/* who is who, unambiguously — twins share a name, ids differ
                  (codex finding, 2026-08-06) */}
              <span className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 rounded-sm border bg-muted/40 px-2.5 py-1.5 text-xs">
                <span>merges away</span>
                <span className="min-w-0 truncate text-foreground">
                  {loserTitle}{" "}
                  <span className="data text-muted-foreground">
                    {loser?.id}
                  </span>
                </span>
                <span>survives</span>
                <span className="min-w-0 truncate text-foreground">
                  {winnerTitle}{" "}
                  <span className="data text-muted-foreground">
                    {winner?.id}
                  </span>
                </span>
              </span>
              {approving ? (
                <>
                  <span className="block">
                    The substrate applies the merge in the same transaction as
                    this decision: the merged-away record's edges and sources
                    move across, machine-held properties recompute from the
                    union of both sides' live sources, and values you hold stand
                    untouched.
                  </span>
                  <span className="block">
                    Reversible: the merged-away record is tombstoned, not erased
                    — a <span className="data">recordsplit</span> can take the
                    merge apart later. If the request went stale (already
                    merged, deleted), nothing partial happens: the whole
                    decision fails and the request says why.
                  </span>
                </>
              ) : (
                <span className="block">
                  Both records stay exactly as they are; only the decision is
                  written. The request is kept as the rejection memory — this
                  pair will not be suggested again.
                </span>
              )}
            </DialogDescription>
          </DialogHeader>
          <Field data-invalid={!!form.formState.errors.note || undefined}>
            <FieldLabel htmlFor="verdict-note">Note (optional)</FieldLabel>
            <Textarea
              id="verdict-note"
              rows={2}
              placeholder={
                approving
                  ? "why these are the same…"
                  : "why these are not the same…"
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
            <Button type="submit" disabled={busy}>
              {busy && <Spinner className="size-3.5" />}
              {approving ? "Accept and merge" : "Reject"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ── the page ────────────────────────────────────────────────────────────────

/** The server's own account of a refused apply, left on the request as a
 * `<actor>/conflict` annotation. */
function conflictAnnotation(mr: SubstrateRecord): string | undefined {
  for (const [key, value] of Object.entries(mr.annotations ?? {})) {
    if (!/(^|\/)conflict$/i.test(key)) continue
    return typeof value === "string" ? value : JSON.stringify(value)
  }
  return undefined
}

function useSideQuery(
  ref: EdgeTarget | undefined,
  types: KindInfo[],
  enabled: boolean
) {
  const type = ref ? kindByIdentity(types, ref.kind) : undefined
  const authority = ref ? splitKind(ref.kind).authority : ""
  return {
    type,
    query: useQuery({
      ...recordQueryOptions(authority, type?.plural ?? "", ref?.id ?? ""),
      enabled: enabled && Boolean(ref && type),
    }),
  }
}

export function MergeRequestDetailPage() {
  const { id } = mergeRequestDetailRoute.useParams()
  const queryClient = useQueryClient()

  const registry = useQuery(kindsQueryOptions)
  const mr = useQuery(mergeRequestQueryOptions(id))

  const decision = mr.data ? decisionOf(mr.data) : undefined
  const proposed = decision === "proposed"
  const types = registry.data ?? []

  const winnerRef = mr.data?.edges?.winner?.[0]
  const loserRef = mr.data?.edges?.loser?.[0]
  const winnerSide = useSideQuery(winnerRef, types, proposed)
  const loserSide = useSideQuery(loserRef, types, proposed)

  const [confirming, setConfirming] = useState<MergeVerdict | null>(null)

  const verdict = useMutation({
    mutationFn: ({ v, note }: { v: MergeVerdict; note?: string }) =>
      submitVerdict(id, v, note),
    onSuccess: (_data, { v }) => {
      setConfirming(null)
      toast.add({
        type: "success",
        title:
          v === "accepted"
            ? "Merged."
            : "Rejected — this pair won't be suggested again.",
      })
      // A merge touches far more than this request: the pair's records, the
      // changelog, counts, the queue. Drop everything and re-read.
      void queryClient.invalidateQueries()
    },
    onError: (error, { v }) => {
      setConfirming(null)
      toast.add({
        type: "error",
        title: `Could not ${v === "accepted" ? "accept" : "reject"} the request`,
        description: error.message,
      })
      // A conflict means it moved under us; the re-read shows the server's
      // own account (the conflict annotation) and the real current state.
      void queryClient.invalidateQueries()
    },
  })

  if (mr.isPending || registry.isPending) return <DetailSkeleton />

  if (mr.isError) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileQuestionIcon />
            </EmptyMedia>
            <EmptyTitle>The merge request didn't load</EmptyTitle>
            <EmptyDescription>
              <span className="data">{id}</span> — {mr.error.message}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void mr.refetch()}
            >
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  const request = mr.data
  const loserTitle = refTitle(loserRef)
  const winnerTitle = refTitle(winnerRef)
  const rationale =
    typeof request.properties.rationale === "string"
      ? request.properties.rationale
      : undefined
  const decidedAt =
    typeof request.properties.decidedAt === "string"
      ? request.properties.decidedAt
      : undefined
  const proposer = request.propertyMeta?.rationale?.manager
  const decider = request.propertyMeta?.decidedAt?.manager
  const note = verdictNote(request)
  const conflict = conflictAnnotation(request)

  const sidesReady = proposed && winnerSide.query.data && loserSide.query.data
  const sideError = proposed
    ? (winnerSide.query.error ?? loserSide.query.error)
    : undefined
  const sideTypeMissing =
    proposed &&
    (Boolean(winnerRef && !winnerSide.type) ||
      Boolean(loserRef && !loserSide.type))

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">
            {loserTitle} <span className="text-muted-foreground">→</span>{" "}
            {winnerTitle}
          </h1>
          <p className="data text-xs text-muted-foreground">
            core.substrate.reamde.dev/recordmergerequests/{request.id}
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
                disabled={verdict.isPending}
                onClick={() => setConfirming("rejected")}
              >
                <XIcon className="size-3.5" />
                Reject
              </Button>
              <Button
                size="sm"
                disabled={verdict.isPending}
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
        {/* the matcher's case */}
        <div className="mx-6 mb-4 flex flex-col gap-2 rounded-md border bg-muted/40 px-4 py-3 text-sm">
          {rationale && <p>{rationale}</p>}
          <EvidenceChips mr={request} />
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
          {conflict && (
            <p className="border-l-2 border-l-warning pl-2 text-xs">
              <span className="text-warning">conflict:</span>{" "}
              <span className="data">{conflict}</span>
            </p>
          )}
        </div>

        {/* the resolved record */}
        {!proposed && (
          <div className="mx-6 mb-4 flex items-start gap-3 rounded-md border px-4 py-3 text-sm">
            <GitMergeIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            {decision === "accepted" ? (
              <p>
                Merged — the survivor{" "}
                <span className="data">
                  {winnerRef ? (
                    <RecordPeek target={winnerRef} types={types} />
                  ) : (
                    winnerTitle
                  )}
                </span>{" "}
                carries both histories now, its machine-held properties
                recomputed from the union of sources.{" "}
                <span className="data">recordsplit</span> can take the merge
                apart if it was wrong.
              </p>
            ) : (
              <p>
                Rejected — the pair stays separate. This request is the
                rejection memory: the matcher will not suggest{" "}
                <span className="data">
                  {loserRef ? (
                    <RecordPeek target={loserRef} types={types} />
                  ) : (
                    loserTitle
                  )}
                </span>{" "}
                and{" "}
                <span className="data">
                  {winnerRef ? (
                    <RecordPeek target={winnerRef} types={types} />
                  ) : (
                    winnerTitle
                  )}
                </span>{" "}
                again.
              </p>
            )}
          </div>
        )}

        {/* the side-by-side */}
        {proposed &&
          (sidesReady ? (
            <SideBySide
              loser={loserSide.query.data!}
              winner={winnerSide.query.data!}
              type={winnerSide.type}
              types={types}
            />
          ) : sideError ? (
            <div className="mx-6 mb-4 rounded-md border px-4 py-3 text-sm text-muted-foreground">
              One side of the pair didn't load — {sideError.message}
            </div>
          ) : sideTypeMissing ? (
            <div className="mx-6 mb-4 rounded-md border px-4 py-3 text-sm text-muted-foreground">
              The pair's type isn't in the registry, so the side-by-side can't
              render. The verdict buttons still work.
            </div>
          ) : (
            <div className="mx-6 mb-4 flex flex-col gap-2">
              <Skeleton className="h-24 w-full rounded-md" />
            </div>
          ))}
      </ScrollArea>

      {confirming && (
        <VerdictDialog
          verdict={confirming}
          loser={loserRef}
          winner={winnerRef}
          busy={verdict.isPending}
          onConfirm={(note) => verdict.mutate({ v: confirming, note })}
          onClose={() => setConfirming(null)}
        />
      )}
    </div>
  )
}

/** Mirrors the final layout: header, evidence band, diff grid. */
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
