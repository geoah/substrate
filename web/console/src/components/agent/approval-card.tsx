/** A call an agent wants to make that waits for the owner (decision record
 * 0254), in the conversation it was asked on: which agent asks, what would
 * run and with exactly what, and Approve or Decline while it waits. After
 * the decision it shows where the call ended: running, done, didn't work, or
 * declined. Approving is one CAS'd patch; the engine runs exactly the stored
 * call, never anything this card sends.
 *
 * The arguments are MODEL-AUTHORED and render as plain untrusted text. */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { ShieldQuestionIcon } from "lucide-react"

import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { useLiveRecords } from "@/hooks/use-live-records"
import {
  agentWord,
  APPROVAL_NAME,
  approvalViewOf,
  decideApproval,
  type ApprovalState,
} from "@/lib/api/approvals"
import {
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  LLM_PACKAGE_NAME,
} from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import { APPROVAL_KIND } from "@/lib/core-kinds"

const CARD =
  "flex flex-col gap-2.5 rounded-[10px] border border-border-strong bg-background px-3.5 py-3 text-sm"

const HEADINGS: Record<ApprovalState, string> = {
  pending: "Waiting for your approval",
  approved: "Approved, starting",
  running: "Approved, running",
  done: "Approved and done",
  failed: "Approved, but it didn’t work",
  denied: "Declined",
}

/** The first sentence of a function's description: what the call does. */
function firstSentence(text: string): string {
  const cut = text.search(/\.(\s|$)/)
  return (cut > 0 ? text.slice(0, cut + 1) : text).trim()
}

export function ApprovalCard({ id }: { id: string }) {
  const client = useQueryClient()
  const record = useQuery(
    recordQueryOptions(CORE_AUTHORITY, LLM_PACKAGE_NAME, APPROVAL_NAME, id)
  )
  const view = record.data ? approvalViewOf(record.data) : undefined
  const fn = useQuery({
    ...recordQueryOptions(
      CORE_AUTHORITY,
      CORE_PACKAGE_NAME,
      "function",
      view?.function ?? ""
    ),
    enabled: Boolean(view?.function),
  })
  const [deciding, setDeciding] = useState<"approve" | "decline" | null>(null)
  const [error, setError] = useState<string | null>(null)
  // While an approved call runs, the record moves without the owner: the
  // change feed re-reads it until it ends.
  const moving = view?.state === "approved" || view?.state === "running"
  useLiveRecords(moving ? [APPROVAL_KIND] : [], [["record"], ["records"]])

  if (record.isPending) {
    return (
      <div className={CARD}>
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Spinner className="size-3" />
          Loading what it wants to do…
        </span>
      </div>
    )
  }
  if (record.isError || !view) return null

  const pending = view.state === "pending"
  const description =
    typeof fn.data?.properties.description === "string"
      ? firstSentence(fn.data.properties.description)
      : ""

  async function decide(approve: boolean) {
    if (!view) return
    setDeciding(approve ? "approve" : "decline")
    setError(null)
    try {
      await decideApproval(view, approve)
      await client.invalidateQueries()
      // The call runs and the agent answers after this returns: two more
      // sweeps catch them without polling forever.
      setTimeout(() => void client.invalidateQueries(), 3000)
      setTimeout(() => void client.invalidateQueries(), 8000)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      void client.invalidateQueries()
    } finally {
      setDeciding(null)
    }
  }

  return (
    <div className={CARD} data-slot="approval-card" data-state={view.state}>
      <div className="flex flex-wrap items-center gap-2 font-medium">
        <ShieldQuestionIcon className="size-4 shrink-0 text-faint" />
        <span>{HEADINGS[view.state]}</span>
        {!pending && (
          <StateBadge value={view.state} className="ml-auto text-[12.5px]" />
        )}
      </div>
      <div className="flex flex-col gap-0.5 text-[13px]">
        <span>
          {agentWord(view.agent)} {pending ? "wants" : "asked"} to run{" "}
          <span className="font-mono text-[12.5px]">{view.tool}</span>
        </span>
        {description && (
          <span className="[overflow-wrap:anywhere] text-muted-foreground">
            {description}
          </span>
        )}
      </div>
      {view.arguments.length > 0 && (
        <dl className="grid grid-cols-[minmax(5rem,max-content)_1fr] gap-x-3 gap-y-1.5 rounded-lg bg-panel px-3 py-2 text-[13px]">
          {view.arguments.map((a) => (
            <div key={a.name} className="contents">
              <dt className="text-muted-foreground">{a.label}</dt>
              <dd className="max-h-48 min-w-0 overflow-y-auto [overflow-wrap:anywhere] whitespace-pre-wrap">
                {a.value}
              </dd>
            </div>
          ))}
        </dl>
      )}
      {view.state === "failed" && view.error && (
        <p className="text-[12.5px] [overflow-wrap:anywhere] text-destructive">
          {view.error}
        </p>
      )}
      {error && (
        <p className="text-[12.5px] text-destructive">
          That didn’t go through: {error}
        </p>
      )}
      {pending && (
        <div className="flex items-center gap-1.5">
          <Button
            size="sm"
            disabled={deciding !== null}
            onClick={() => void decide(true)}
          >
            {deciding === "approve" && <Spinner className="size-3" />}
            Approve
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={deciding !== null}
            onClick={() => void decide(false)}
          >
            {deciding === "decline" && <Spinner className="size-3" />}
            Decline
          </Button>
        </div>
      )}
    </div>
  )
}

/** The approvals a sub-agent asked on this conversation: their own tool rows
 * are in the sub-agent's thread, which the conversation does not inline, so
 * they wait here, after the transcript, until decided. */
export function PendingApprovals({ ids }: { ids: string[] }) {
  if (ids.length === 0) return null
  return (
    <section
      aria-label="Waiting for your approval"
      className="flex flex-col gap-2"
    >
      {ids.map((id) => (
        <ApprovalCard key={id} id={id} />
      ))}
    </section>
  )
}
