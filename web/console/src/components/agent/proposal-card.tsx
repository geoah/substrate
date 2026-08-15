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
import {
  changeOp,
  changeTarget,
  decisionOf,
  rationaleOf,
  type Verdict,
} from "@/lib/changerequests"
import { cn } from "@/lib/utils"

export function ProposalCard({ id }: { id: string }) {
  const client = useQueryClient()
  const request = useQuery(changeRequestQueryOptions(id))
  const [submitting, setSubmitting] = useState<Verdict | null>(null)
  const [error, setError] = useState<string | null>(null)

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
  const targetLabel = target
    ? `${target.kind.split("/").pop()}/${target.id}`
    : undefined

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
        <ReviewLink id={id} />
      </div>
    </div>
  )
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
