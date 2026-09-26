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

import { ActorRef } from "@/components/identity/actor-ref"
import { IdText } from "@/components/identity/id-text"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { DocPage } from "@/components/identity/page-layout"
import { RecordRef } from "@/components/identity/record-ref"
import { SectionHead } from "@/components/identity/section-head"
import { StateBadge } from "@/components/identity/state-badge"
import type { PeekTarget } from "@/components/record-peek"
import { ReferenceValue } from "@/components/record/reference-value"
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
import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import { cellValue, referenceObjects, relativeTime } from "@/lib/format"
import {
  DECISION_INITIAL,
  decisionOf,
  deriveDiff,
  mergePair,
  verdictNote,
  type DiffPosture,
  type DiffRow,
  type MergeVerdict,
} from "@/lib/mergerequests"
import { kindByIdentity } from "@/lib/definition"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { CORE_PACKAGE } from "@/lib/api/http"
import { cn } from "@/lib/utils"
import { EvidenceChips } from "@/components/merge-request"
import { mergeRequestDetailRoute } from "@/router"

function refTitle(ref?: PeekTarget): string {
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
      "You hold this value on at least one side, so the surviving value stands as it is. If the other one is right, edit the survivor after the merge.",
  },
  recompute: {
    label: "recompute settles",
    explain:
      "A machine holds this value. After the merge the survivor works it out again from both records' sources.",
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
  kinds,
}: {
  row: DiffRow
  side: "loser" | "winner"
  /** The registry, so a reference value renders as its referent's pill. */
  kinds: KindInfo[]
}) {
  const value = side === "loser" ? row.loser : row.winner
  const manager = side === "loser" ? row.loserManager : row.winnerManager

  // The cell is datatype-blind, so a served reference is recognized by its own
  // shape (issue #332): both sides of the pair carry real record values, and a
  // pointer printed as `{"ref":"…"}` would hide a record the reviewer can open.
  const references = referenceObjects(value)
  if (references) {
    return (
      <span className="flex min-w-0 flex-col items-start gap-1">
        {references.map((one, at) => (
          <ReferenceValue key={at} value={one} kinds={kinds} />
        ))}
        {manager && row.posture !== "equal" && <ActorRef actor={manager} />}
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
          <span className="min-w-0 truncate" title={text}>
            {text}
          </span>
          {count > 0 && (
            <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
              ×{count}
            </span>
          )}
        </span>
      ) : (
        <span className="text-faint">—</span>
      )}
      {manager && row.posture !== "equal" && <ActorRef actor={manager} />}
    </span>
  )
}

/** The diff rides the table system's look (owner ruling, 2026-08-06): real
 * table anatomy — fixed columns, bordered rows, muted lowercase headers —
 * though it stays a comparison, not a list, so no column dropdown or pages. */
function DiffRows({ rows, kinds }: { rows: DiffRow[]; kinds: KindInfo[] }) {
  return (
    <>
      {rows.map((row) => (
        <TableRow key={row.key} className="hover:bg-muted/30">
          <TableCell className="pl-4 align-top">
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="block truncate text-muted-foreground" />
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
                    ? "declared property"
                    : "not declared by the kind"}
                </TooltipContent>
              )}
            </Tooltip>
          </TableCell>
          <TableCell className="align-top">
            <ValueCell row={row} side="loser" kinds={kinds} />
          </TableCell>
          <TableCell className="align-top">
            <ValueCell row={row} side="winner" kinds={kinds} />
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
  kinds,
}: {
  loser: SubstrateRecord
  winner: SubstrateRecord
  type?: KindInfo
  /** The registry, so a reference value renders as its referent's pill. */
  kinds: KindInfo[]
}) {
  const rows = useMemo(
    () => deriveDiff(winner, loser, type),
    [winner, loser, type]
  )
  const open = rows.filter((r) => r.posture !== "equal")
  const equal = rows.filter((r) => r.posture === "equal")
  const [showEqual, setShowEqual] = useState(false)

  return (
    <div className="mb-4 overflow-x-auto rounded-md border">
      <Table className="table-fixed [&_td]:py-2.5" style={{ minWidth: 640 }}>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-4" style={{ width: 140 }}>
              property
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
                  <span className="font-mono text-xs font-normal text-faint">
                    {loser.id}
                  </span>
                </span>
                <span className="text-xs font-normal text-faint">
                  goes into the other
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
                  <span className="font-mono text-xs font-normal text-faint">
                    {winner.id}
                  </span>
                </span>
                <span className="text-xs font-normal text-primary-text">
                  stays
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
            <DiffRows rows={open} kinds={kinds} />
          ) : (
            <TableRow className="hover:bg-transparent">
              <TableCell
                colSpan={4}
                className="px-4 text-xs text-muted-foreground"
              >
                No differences. Every property the pair carries already agrees.
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
                    ? "1 identical property"
                    : `${equal.length} identical properties`}
                </button>
              </TableCell>
            </TableRow>
          )}
          {showEqual && <DiffRows rows={equal} kinds={kinds} />}
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
  loser?: PeekTarget
  winner?: PeekTarget
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
                ? `Combine ${loserTitle} into ${winnerTitle}?`
                : "Keep these two apart?"}
            </DialogTitle>
            <DialogDescription className="space-y-2">
              {/* who is who, unambiguously — twins share a name, ids differ
                  (codex finding, 2026-08-06) */}
              <span className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 rounded-sm border bg-muted/40 px-2.5 py-1.5 text-xs">
                <span>goes into</span>
                <span className="min-w-0 truncate text-foreground">
                  {loserTitle}{" "}
                  <span className="font-mono text-muted-foreground">
                    {loser?.id}
                  </span>
                </span>
                <span>stays</span>
                <span className="min-w-0 truncate text-foreground">
                  {winnerTitle}{" "}
                  <span className="font-mono text-muted-foreground">
                    {winner?.id}
                  </span>
                </span>
              </span>
              {approving ? (
                <>
                  <span className="block">
                    {loserTitle} stops being a record of its own: its history
                    and everything that points to it move to {winnerTitle}.
                    Values you set are kept.
                  </span>
                  <span className="block">
                    A split can take them apart again later.
                  </span>
                </>
              ) : (
                <span className="block">
                  Both are left as they are, and this pair won’t be suggested
                  again.
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
              Saved with your decision on this request.
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
              {approving ? "Combine" : "Keep apart"}
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
  ref: PeekTarget | undefined,
  types: KindInfo[],
  enabled: boolean
) {
  const type = ref ? kindByIdentity(types, ref.kind) : undefined
  return {
    type,
    query: useQuery({
      ...recordQueryOptions(
        type?.authority ?? "",
        type?.package ?? "",
        type?.name ?? "",
        ref?.id ?? ""
      ),
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

  // The pair rides `winner`/`loser` REFERENCE properties: the stored value is
  // the referent's record path, and the peek fetches the rest.
  const pair = mr.data ? mergePair(mr.data) : {}
  const winnerRef = pair.winner
  const loserRef = pair.loser
  const winnerSide = useSideQuery(winnerRef, types, proposed)
  const loserSide = useSideQuery(loserRef, types, proposed)

  const [confirming, setConfirming] = useState<MergeVerdict | null>(null)
  const [technicalMode] = useTechnicalDetails()

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
            : "Kept apart. This pair won't be suggested again.",
      })
      // A merge touches far more than this request: the pair's records, the
      // changelog, counts, the queue. Drop everything and re-read.
      void queryClient.invalidateQueries()
    },
    onError: (error, { v }) => {
      setConfirming(null)
      toast.add({
        type: "error",
        title: `${v === "accepted" ? "Combining them" : "Keeping them apart"} didn’t go through`,
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
              <span className="data">{id}</span>: {mr.error.message}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void mr.refetch()}
            >
              Try again
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
  const technical = technicalMode
  const conflict = conflictAnnotation(request)

  const sidesReady = proposed && winnerSide.query.data && loserSide.query.data
  const sideError = proposed
    ? (winnerSide.query.error ?? loserSide.query.error)
    : undefined
  const sideTypeMissing =
    proposed &&
    (Boolean(winnerRef && !winnerSide.type) ||
      Boolean(loserRef && !loserSide.type))

  const pairKind = winnerRef?.kind ?? loserRef?.kind
  return (
    <DocPage>
      <header className="flex items-start justify-between gap-3">
        {pairKind ? (
          <KindGlyph kind={pairKind} size="lg" />
        ) : (
          <span className="grid size-10 place-items-center rounded-[10px] bg-hover text-muted-foreground">
            <GitMergeIcon className="size-5" />
          </span>
        )}
        {proposed && (
          <div className="flex shrink-0 items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={verdict.isPending}
              onClick={() => setConfirming("rejected")}
            >
              <XIcon className="size-3.5" />
              Keep them apart
            </Button>
            <Button
              size="sm"
              disabled={verdict.isPending}
              onClick={() => setConfirming("accepted")}
            >
              <CheckIcon className="size-3.5" />
              Combine them
            </Button>
          </div>
        )}
      </header>
      <h1 className="mt-2.5 mb-1.5 text-[26px] leading-tight font-[650] tracking-[-0.02em] text-balance break-words">
        {proposed ? "Are these the same?" : "Suggested as the same"}
      </h1>
      <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-[12.5px] text-faint">
        {decision && <StateBadge value={decision} initial={DECISION_INITIAL} />}
        {proposer && (
          <span className="flex items-center gap-1.5">
            Suggested by <ActorRef actor={proposer} />
          </span>
        )}
        <span title={request.createdAt}>{relativeTime(request.createdAt)}</span>
        {decidedAt && (
          <span className="flex items-center gap-1.5">
            Decided <span title={decidedAt}>{relativeTime(decidedAt)}</span>
            {decider && (
              <>
                by <ActorRef actor={decider} />
              </>
            )}
          </span>
        )}
        {technical && (
          <IdText
            value={`${CORE_PACKAGE}/recordmergerequest/${request.id}`}
            copy
          />
        )}
      </div>

      {/* the pair */}
      <div className="mt-5 flex flex-wrap items-center gap-2 text-[15px]">
        {loserRef ? (
          <RecordRef kind={loserRef.kind} id={loserRef.id} />
        ) : (
          loserTitle
        )}
        <span className="text-faint">and</span>
        {winnerRef ? (
          <RecordRef kind={winnerRef.kind} id={winnerRef.id} />
        ) : (
          winnerTitle
        )}
      </div>

      {/* the matcher's case */}
      <div className="mt-4 flex flex-col gap-2 rounded-lg border bg-panel px-4 py-3 text-[13px]">
        {rationale && <p>{rationale}</p>}
        <EvidenceChips mr={request} />
        {note && (
          <p className="text-xs">
            <span className="text-faint">Note:</span> {note}
          </p>
        )}
        {conflict && (
          <p className="border-l-2 border-l-warning pl-2 text-xs">
            <span className="text-warning">Conflict:</span>{" "}
            <span className="font-mono">{conflict}</span>
          </p>
        )}
      </div>

      {/* the resolved record */}
      {!proposed && (
        <div className="mt-4 flex items-start gap-3 rounded-lg border px-4 py-3 text-[13px]">
          <GitMergeIcon className="mt-0.5 size-4 shrink-0 text-faint" />
          {decision === "accepted" ? (
            <p>
              Combined.{" "}
              {winnerRef ? (
                <RecordRef kind={winnerRef.kind} id={winnerRef.id} />
              ) : (
                winnerTitle
              )}{" "}
              carries both histories now.
            </p>
          ) : (
            <p>
              Kept apart. You won’t be asked about{" "}
              {loserRef ? (
                <RecordRef kind={loserRef.kind} id={loserRef.id} />
              ) : (
                loserTitle
              )}{" "}
              and{" "}
              {winnerRef ? (
                <RecordRef kind={winnerRef.kind} id={winnerRef.id} />
              ) : (
                winnerTitle
              )}{" "}
              again.
            </p>
          )}
        </div>
      )}

      {/* the side-by-side */}
      {proposed && (
        <>
          <SectionHead
            title="Side by side"
            hint="what each one holds, and what combining keeps"
          />
          {sidesReady ? (
            <SideBySide
              loser={loserSide.query.data!}
              winner={winnerSide.query.data!}
              type={winnerSide.type}
              kinds={types}
            />
          ) : sideError ? (
            <div className="rounded-lg border px-4 py-3 text-[13px] text-muted-foreground">
              One of the two didn’t load: {sideError.message}
            </div>
          ) : sideTypeMissing ? (
            <div className="rounded-lg border px-4 py-3 text-[13px] text-muted-foreground">
              This repository doesn’t have their kind, so the two can’t be shown
              side by side. You can still decide.
            </div>
          ) : (
            <Skeleton className="h-24 w-full rounded-lg" />
          )}
        </>
      )}

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
    </DocPage>
  )
}

/** Mirrors the final layout: header, evidence band, diff grid. */
function DetailSkeleton() {
  return (
    <DocPage>
      <Skeleton className="size-10 rounded-[10px]" />
      <Skeleton className="mt-3 h-7 w-72" />
      <Skeleton className="mt-2 h-3.5 w-80" />
      <Skeleton className="mt-5 h-20 w-full rounded-lg" />
      <Skeleton className="mt-4 h-48 w-full rounded-lg" />
    </DocPage>
  )
}
