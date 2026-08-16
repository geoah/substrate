/** The proposal, inline in the thread that proposed it. The card RESOLVES the
 * recordpatchrequest and renders its live state — never a snapshot the row
 * carried — so the transcript and the review inbox can never disagree about
 * what a proposal says or whether it is decided. Accept/reject here is the
 * same atomic decision patch the review page submits (`submitDecision`,
 * CAS'd on the request version as loaded); the full diff review stays on the
 * request page, one link away.
 *
 * Deciding also writes a `system` message into this very thread and resumes
 * the agent, so a decision made here shows up as new turns — the invalidation
 * below is what lets them arrive. */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useState } from "react"
import { ArrowUpRightIcon, FilePenLineIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import {
  changeRequestQueryOptions,
  submitDecision,
} from "@/lib/api/changerequests"
import { CORE_AUTHORITY } from "@/lib/api/http"
import { putRecord, recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import {
  changeOp,
  changeTarget,
  decisionOf,
  deriveChangeRows,
  proposedDiff,
  rationaleOf,
  type ChangeRow,
  type Verdict,
} from "@/lib/changerequests"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { cn } from "@/lib/utils"

export function ProposalCard({ id }: { id: string }) {
  const client = useQueryClient()
  const request = useQuery(changeRequestQueryOptions(id))
  const [submitting, setSubmitting] = useState<Verdict | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [remedyOpen, setRemedyOpen] = useState(false)
  // The remedy needs the proposer's identity, which lives on the thread the
  // request points at; fetched lazily, only for gated pending requests.
  const threadPath =
    typeof request.data?.properties.thread === "string"
      ? request.data.properties.thread
      : ""
  const threadId = threadPath.slice(threadPath.lastIndexOf("/") + 1)
  const gated = typeof request.data?.properties.policy === "string"
  const thread = useQuery({
    ...recordQueryOptions(CORE_AUTHORITY, "llmthreads", threadId),
    enabled: gated && Boolean(threadId),
  })
  // The card shows WHAT would change, not a link to find out: for a patch,
  // the live target is read so every row renders before AND after.
  const pendingOp = request.data ? (changeOp(request.data) ?? "patch") : "patch"
  const pendingTarget = request.data ? changeTarget(request.data) : undefined
  const registry = useQuery({
    ...kindsQueryOptions,
    enabled:
      Boolean(request.data) && pendingOp === "patch" && Boolean(pendingTarget),
  })
  const targetKindInfo =
    pendingTarget && registry.data
      ? kindByIdentity(registry.data, pendingTarget.kind)
      : undefined
  const targetRecord = useQuery({
    ...recordQueryOptions(
      pendingTarget ? splitKind(pendingTarget.kind).authority : "",
      targetKindInfo?.plural ?? "",
      pendingTarget?.id ?? ""
    ),
    enabled: pendingOp === "patch" && Boolean(pendingTarget && targetKindInfo),
  })

  if (request.isPending) {
    return (
      <div className="flex items-center gap-1.5 border-t px-2 py-1.5 text-xs text-muted-foreground">
        <Spinner className="size-3" />
        loading the proposal
      </div>
    )
  }
  // The row may be gone (a purge) or unreadable: the link is still the
  // honest rendering — the id came off the transcript, not a guess.
  if (request.isError || !request.data) {
    return (
      <div className="border-t px-2 py-1.5">
        <ReviewLink id={id} />
      </div>
    )
  }

  const record = request.data
  const decision = decisionOf(record)
  const op = changeOp(record) ?? "patch"
  const target = changeTarget(record)
  const rationale = rationaleOf(record)
  const verdict = judgeVerdictOf(record)
  const targetLabel = target
    ? `${target.kind.split("/").pop()}/${target.id}`
    : undefined

  // The remedy, server-shaped and deliberately narrow: exactly this agent,
  // this kind, this op — never a wildcard, provenance on the minted rule —
  // shown verbatim behind its own confirmation before anything lands.
  //
  // THE REQUEST'S OP IS NOT THE SELECTOR'S. A selector matches the verb the
  // agent called (put/patch/delete); the request records what accepting would
  // do, and convertToRequest maps BOTH put and patch onto create (target not
  // live) or patch (target live), keeping no record of which. So a request
  // that reads create or patch is remedied by naming both verbs; only delete
  // maps one to one. See docs/changelog.md#change-verbs.
  const proposerPath =
    typeof thread.data?.properties.agent === "string"
      ? thread.data.properties.agent
      : ""
  const proposer = proposerPath.slice(proposerPath.lastIndexOf("/") + 1)
  const remedyRule =
    gated && proposer && target
      ? {
          selector: {
            kinds: [target.kind],
            ops: op === "delete" ? ["delete"] : ["put", "patch"],
            agents: [proposer],
          },
          action: "allow",
        }
      : undefined

  async function acceptAndAllow() {
    if (!remedyRule) return
    setSubmitting("accepted")
    setError(null)
    try {
      await putRecord(CORE_AUTHORITY, "recordpatchpolicies", `allow-${id}`, {
        properties: remedyRule,
        annotations: {
          "owner/provenance": `minted from the gate card of recordpatchrequest ${id}`,
        },
      })
      await submitDecision(id, "accepted", record.version)
      await client.invalidateQueries()
      setTimeout(() => void client.invalidateQueries(), 4000)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      void client.invalidateQueries()
    } finally {
      setSubmitting(null)
      setRemedyOpen(false)
    }
  }

  async function decide(verdict: Verdict) {
    setSubmitting(verdict)
    setError(null)
    try {
      await submitDecision(id, verdict, record.version)
      // The decision changed the request, (on accept) the target, and wrote a
      // system turn into THIS thread; the resumed agent's reply lands a few
      // seconds later, so one more sweep catches it without polling forever.
      await client.invalidateQueries()
      setTimeout(() => void client.invalidateQueries(), 4000)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      void client.invalidateQueries()
    } finally {
      setSubmitting(null)
    }
  }

  return (
    <div className="flex flex-col gap-1.5 border-t px-2 py-1.5">
      <div className="flex items-center gap-1.5 text-xs">
        <FilePenLineIcon className="size-3 shrink-0 text-muted-foreground" />
        <span className="truncate">
          Proposes <span className="data">{op}</span>
          {targetLabel && (
            <>
              {" "}
              on <span className="data">{targetLabel}</span>
            </>
          )}
        </span>
        <span
          className={cn(
            "ml-auto shrink-0 rounded-sm px-1.5 py-0.5 text-[0.65rem] tracking-wide uppercase",
            decision === "proposed" && "bg-amber-500/15 text-amber-600",
            decision === "accepted" && "bg-emerald-500/15 text-emerald-600",
            decision === "rejected" && "bg-muted text-muted-foreground"
          )}
        >
          {decision}
        </span>
      </div>
      {rationale && (
        <p className="text-xs [overflow-wrap:anywhere] text-muted-foreground">
          {rationale}
        </p>
      )}
      {decision === "proposed" && (
        <ChangePreview
          record={record}
          op={op}
          targetLabel={targetLabel}
          target={targetRecord.data}
        />
      )}
      {verdict && (
        <p className="text-xs [overflow-wrap:anywhere] text-muted-foreground">
          Judge: <span className="data">{verdict.verdict}</span>
          {typeof verdict.confidence === "number" && (
            <>
              {" "}
              at <span className="data">{verdict.confidence.toFixed(2)}</span>
            </>
          )}
          {verdict.outcome && (
            <>
              {" "}
              (<span className="data">{verdict.outcome}</span>)
            </>
          )}
          {verdict.rationale && <> — {verdict.rationale}</>}
        </p>
      )}
      {error && <p className="text-xs text-destructive">{error}</p>}
      <div className="flex items-center gap-2">
        {decision === "proposed" && (
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={submitting !== null}
              onClick={() => void decide("accepted")}
            >
              {submitting === "accepted" && <Spinner className="size-3" />}
              Accept
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={submitting !== null}
              onClick={() => void decide("rejected")}
            >
              {submitting === "rejected" && <Spinner className="size-3" />}
              Reject
            </Button>
          </>
        )}
        {decision === "proposed" && remedyRule && (
          <Button
            size="sm"
            variant="ghost"
            disabled={submitting !== null}
            onClick={() => setRemedyOpen((v) => !v)}
          >
            Accept + always allow
          </Button>
        )}
        <ReviewLink id={id} />
      </div>
      {remedyOpen && remedyRule && (
        <div className="flex flex-col gap-1.5 rounded-sm border bg-background/60 p-2">
          <p className="text-xs text-muted-foreground">
            Accepting this way also mints ONE standing rule, exactly this narrow
            — every future diff of this class lands without review:
          </p>
          <pre className="overflow-x-auto rounded-sm bg-muted/40 p-1.5 data text-[0.7rem]">
            {JSON.stringify(remedyRule, null, 2)}
          </pre>
          <div>
            <Button
              size="sm"
              variant="outline"
              disabled={submitting !== null}
              onClick={() => void acceptAndAllow()}
            >
              {submitting === "accepted" && <Spinner className="size-3" />}
              Mint the rule and accept
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

/** One value, rendered short: the card previews a change, it does not dump
 * JSON. Long values truncate; the full diff stays one click away. */
function short(value: unknown): string {
  if (value === undefined) return ""
  if (value === null) return "null"
  const text = typeof value === "string" ? value : JSON.stringify(value)
  return text.length > 80 ? text.slice(0, 77) + "…" : text
}

/** WHAT the accept would do, inline: per-property before → after for a
 * patch, the values a create would mint, the one line a delete means. Shown
 * while the request is PENDING — that is the moment of decision; a decided
 * card keeps the outcome line, and the live rows would drift anyway. */
function ChangePreview({
  record,
  op,
  targetLabel,
  target,
}: {
  record: SubstrateRecord
  op: string
  targetLabel?: string
  target?: SubstrateRecord
}) {
  if (op === "delete") {
    return (
      <p className="text-xs text-destructive/80">
        Deletes <span className="data">{targetLabel}</span> — the record is
        tombstoned, not erased.
      </p>
    )
  }
  const diff = proposedDiff(record)
  const rows = deriveChangeRows(
    diff.properties,
    op === "patch" ? target : undefined
  )
  if (rows.length === 0) return null
  return (
    <div className="flex flex-col gap-0.5 rounded-sm bg-background/60 p-1.5">
      {rows.map((row) => (
        <ChangeRowLine key={row.key} row={row} create={op === "create"} />
      ))}
    </div>
  )
}

function ChangeRowLine({ row, create }: { row: ChangeRow; create: boolean }) {
  return (
    <div className="flex items-baseline gap-1.5 text-xs [overflow-wrap:anywhere]">
      <span className="shrink-0 data text-muted-foreground">{row.key}</span>
      {row.effect === "clear" ? (
        <span className="text-destructive/80">
          <s className="data">{short(row.before)}</s> cleared
        </span>
      ) : row.effect === "unchanged" ? (
        <span className="text-muted-foreground">
          <span className="data">{short(row.after)}</span> (already matches)
        </span>
      ) : row.effect === "set" ? (
        <span>
          <s className="data text-muted-foreground">{short(row.before)}</s>{" "}
          <span className="data text-emerald-700 dark:text-emerald-400">
            {short(row.after)}
          </span>
        </span>
      ) : (
        <span className="data text-emerald-700 dark:text-emerald-400">
          {create ? short(row.after) : `+ ${short(row.after)}`}
        </span>
      )}
    </div>
  )
}

/** The engine-owned policy/verdict audit, read tolerantly for the card. */
function judgeVerdictOf(record: SubstrateRecord):
  | {
      verdict?: string
      confidence?: number
      outcome?: string
      rationale?: string
    }
  | undefined {
  const raw = record.annotations?.["policy/verdict"]
  if (typeof raw !== "object" || raw === null) return undefined
  const a = raw as Record<string, unknown>
  const out = {
    verdict: typeof a.verdict === "string" ? a.verdict : undefined,
    confidence: typeof a.confidence === "number" ? a.confidence : undefined,
    outcome: typeof a.outcome === "string" ? a.outcome : undefined,
    rationale: typeof a.rationale === "string" ? a.rationale : undefined,
  }
  if (!out.verdict && !out.outcome) return undefined
  return out
}

function ReviewLink({ id }: { id: string }) {
  return (
    <Link
      to="/change-requests/$id"
      params={{ id }}
      className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
    >
      Review the full change
      <ArrowUpRightIcon className="size-3" />
    </Link>
  )
}
