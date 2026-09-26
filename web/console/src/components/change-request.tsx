/** A suggested change's voice, shared by the chat card and the review page so
 * the two can never say the same decision two ways: its values in the
 * sheet's labels and renderers, the decision's words, and the one set of
 * decision buttons (Apply, Dismiss; a delete confirms by a second press). */

import { useEffect, useState, type ReactNode } from "react"
import { useQueryClient } from "@tanstack/react-query"

import { propertyIcon } from "@/components/property-sheet/sheet-model"
import {
  DeclaredValue,
  Empty,
  LooseValue,
} from "@/components/property-sheet/property-value"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { applyWord, changeLabel } from "@/lib/agent-chat"
import { submitDecision } from "@/lib/api/changerequests"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"
import type { ChangeOp, Verdict } from "@/lib/changerequests"
import type { PropSpec } from "@/lib/record-schema"

/** A property's label as the sheet draws it: the datatype's icon, then the
 * label. */
export function ChangeLabel({ name, spec }: { name: string; spec?: PropSpec }) {
  const Icon = spec ? propertyIcon(spec).icon : undefined
  return (
    <span className="flex min-w-0 items-center gap-[7px] text-muted-foreground">
      {Icon && <Icon aria-hidden className="size-3.5 shrink-0 text-faint" />}
      <span className="min-w-0 break-words">{changeLabel(name, spec)}</span>
    </span>
  )
}

/** One value, read the way the record page reads it. `null` is what a change
 * writes to empty a property, so it reads "Cleared". */
export function ChangeValue({
  value,
  spec,
}: {
  value: unknown
  spec?: PropSpec
}) {
  if (value === null) return <Empty>Cleared</Empty>
  return spec ? (
    <DeclaredValue spec={spec} value={value} />
  ) : (
    <LooseValue value={value} />
  )
}

/** How long a pressed "Delete it" waits for the second press. */
const ARMED_MS = 6000

/** Apply and Dismiss, one atomic decision each, CAS'd on the request's
 * version as loaded (the write path refuses a decision without it, which is
 * what keeps the reviewed envelope the decided one). A delete asks for a
 * second press, with the consequence beside it. Deciding writes a system turn
 * into the thread and resumes the agent, whose reply lands a few seconds
 * later, so one more sweep catches it without polling forever. */
export function DecisionButtons({
  request,
  op,
  review,
}: {
  request: SubstrateRecord
  op: ChangeOp
  /** Anything between Apply and Dismiss: the card's link to the review. */
  review?: ReactNode
}) {
  const client = useQueryClient()
  const [submitting, setSubmitting] = useState<Verdict | null>(null)
  const [armed, setArmed] = useState(false)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    if (!armed) return undefined
    const timer = setTimeout(() => setArmed(false), ARMED_MS)
    return () => clearTimeout(timer)
  }, [armed])

  async function decide(next: Verdict) {
    setSubmitting(next)
    setArmed(false)
    setError(null)
    try {
      await submitDecision(request.id, next, request.version)
      await client.invalidateQueries()
      setTimeout(() => void client.invalidateQueries(), 4000)
    } catch (err) {
      setError(
        err instanceof ApiError && err.code === "conflict"
          ? "Nothing was applied: the suggestion or the record changed since it was made. Ask the agent again."
          : `That didn’t go through: ${err instanceof Error ? err.message : String(err)}`
      )
      void client.invalidateQueries()
    } finally {
      setSubmitting(null)
    }
  }

  const deleting = op === "delete"
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-1.5">
        <Button
          size="sm"
          variant={deleting ? "destructive" : "default"}
          disabled={submitting !== null}
          onClick={() =>
            deleting && !armed ? setArmed(true) : void decide("accepted")
          }
        >
          {submitting === "accepted" && <Spinner className="size-3" />}
          {deleting && armed ? "Yes, delete it" : applyWord(op)}
        </Button>
        {review}
        <Button
          size="sm"
          variant="ghost"
          disabled={submitting !== null}
          onClick={() => void decide("rejected")}
        >
          {submitting === "rejected" && <Spinner className="size-3" />}
          Dismiss
        </Button>
        {armed && (
          <span role="status" className="text-[12.5px] text-destructive">
            Press again to delete it.
          </span>
        )}
      </div>
      {error && (
        <p role="alert" className="text-[12.5px] text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
