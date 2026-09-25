/** A change the agent suggested, inline in the thread that suggested it. The
 * card RESOLVES the recordpatchrequest and renders its live state — never a
 * snapshot the row carried — so the thread and the review page can never
 * disagree about what a suggestion says or whether it is decided.
 *
 * Apply and Dismiss are the same atomic decision patch the review page
 * submits (`submitDecision`, CAS'd on the request version as loaded); Edit
 * first is the review page itself. Deciding also writes a `system` message
 * into this very thread and resumes the agent, so a decision made here shows
 * up as new turns — the invalidation below is what lets them arrive. */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useState } from "react"
import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { AgentMark } from "@/components/agent/agent-mark"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  agentName,
  propertyLabel,
  proposedHeading,
  threadAgentId,
  valueWords,
} from "@/lib/agent-chat"
import {
  changeRequestQueryOptions,
  submitDecision,
} from "@/lib/api/changerequests"
import {
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  LLM_PACKAGE_NAME,
} from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { putRecord, recordQueryOptions } from "@/lib/api/records"
import { readReference, type SubstrateRecord } from "@/lib/api/types"
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
import { kindByIdentity } from "@/lib/definition"
import { displayName, displayPlural } from "@/lib/kind-names"
import { splitRecordPath } from "@/lib/record-path"

const CARD =
  "flex flex-col gap-2 rounded-[10px] border border-border-strong bg-background px-3.5 py-3 text-sm"

export function ProposalCard({ id }: { id: string }) {
  const client = useQueryClient()
  const [technical] = useTechnicalDetails()
  const request = useQuery(changeRequestQueryOptions(id))
  const [submitting, setSubmitting] = useState<Verdict | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [remedyOpen, setRemedyOpen] = useState(false)
  // The remedy needs the proposer's identity, which lives on the thread the
  // request points at; fetched lazily, only for gated pending requests.
  const threadPath = readReference(request.data?.properties.thread)?.path ?? ""
  const threadId = threadPath.slice(threadPath.lastIndexOf("/") + 1)
  const gated = readReference(request.data?.properties.policy) !== undefined
  const thread = useQuery({
    ...recordQueryOptions(CORE_AUTHORITY, LLM_PACKAGE_NAME, "thread", threadId),
    enabled: gated && Boolean(threadId),
  })
  // The card shows WHAT would change: for a patch, the live target is read so
  // every row renders before AND after.
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
      targetKindInfo?.authority ?? "",
      targetKindInfo?.package ?? "",
      targetKindInfo?.name ?? "",
      pendingTarget?.id ?? ""
    ),
    enabled: pendingOp === "patch" && Boolean(pendingTarget && targetKindInfo),
  })

  if (request.isPending) {
    return (
      <div className={CARD}>
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Spinner className="size-3" />
          Loading the suggested change…
        </span>
      </div>
    )
  }
  // The row may be gone (a purge) or unreadable: the link is still the
  // honest rendering — the id came off the transcript, not a guess.
  if (request.isError || !request.data) {
    return (
      <div className={CARD}>
        <span className="text-muted-foreground">
          This suggested change couldn’t be read.{" "}
          <ReviewLink id={id}>Open it</ReviewLink>
        </span>
      </div>
    )
  }

  const record = request.data
  const decision = decisionOf(record)
  const op = changeOp(record) ?? "patch"
  const target = changeTarget(record)
  const rationale = rationaleOf(record)
  const verdict = judgeVerdictOf(record)
  const diff = proposedDiff(record)
  const pending = decision === "proposed"

  // The remedy, deliberately narrow: exactly this agent, this kind, this op —
  // never a wildcard, provenance on the minted rule — shown behind its own
  // confirmation before anything lands. The selector names the agent by its
  // IDENTITY (`<authority>/<package>/<name>`), which is what policy matching
  // compares.
  //
  // THE REQUEST'S OP IS NOT THE SELECTOR'S. A selector matches the verb the
  // agent called (put/patch/delete); the request records what accepting would
  // do, and convertToRequest maps BOTH put and patch onto create (target not
  // live) or patch (target live), keeping no record of which. So a request
  // that reads create or patch is remedied by naming both verbs; only delete
  // maps one to one. See docs/changelog.md#change-verbs.
  const proposer = thread.data ? threadAgentId(thread.data) : undefined
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
      await putRecord(
        CORE_AUTHORITY,
        CORE_PACKAGE_NAME,
        "recordpatchpolicy",
        `allow-${id}`,
        {
          properties: remedyRule,
          annotations: {
            "owner/provenance": `minted from the gate card of recordpatchrequest ${id}`,
          },
        }
      )
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

  async function decide(next: Verdict) {
    setSubmitting(next)
    setError(null)
    try {
      await submitDecision(id, next, record.version)
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

  const heading = op === "create" ? proposedHeading(diff.properties) : undefined
  const Icon =
    op === "create" ? PlusIcon : op === "delete" ? Trash2Icon : PencilIcon
  const applyLabel =
    op === "create" ? "Add it" : op === "delete" ? "Delete it" : "Apply"

  return (
    <div className={CARD} data-slot="proposal-card">
      <div className="flex flex-wrap items-center gap-2 font-medium">
        <Icon className="size-4 shrink-0 text-faint" />
        {op === "create" ? (
          <>
            <span>
              New {target ? displayName(target.kind).toLowerCase() : "record"}
            </span>
            {heading && <span className="font-semibold">{heading.text}</span>}
          </>
        ) : (
          <>
            <span>{op === "delete" ? "Delete" : "Change"}</span>
            {target && <RecordRef kind={target.kind} id={target.id} />}
          </>
        )}
        {technical && target && (
          <span className="font-mono text-[11px] font-normal [overflow-wrap:anywhere] text-faint">
            {target.kind}/{target.id}
          </span>
        )}
        {!pending && (
          <StateBadge value={decision} className="ml-auto text-[12.5px]" />
        )}
      </div>
      {rationale && (
        <p className="text-[13px] [overflow-wrap:anywhere] text-muted-foreground">
          {rationale}
        </p>
      )}
      {pending && (
        <ChangeWords
          record={record}
          op={op}
          target={targetRecord.data}
          skip={heading?.key}
        />
      )}
      {verdict && (
        <p className="text-[12.5px] [overflow-wrap:anywhere] text-muted-foreground">
          A judge said {verdict.verdict ?? verdict.outcome}
          {typeof verdict.confidence === "number" &&
            ` (${Math.round(verdict.confidence * 100)}% sure)`}
          {verdict.rationale && `: ${verdict.rationale}`}
        </p>
      )}
      {error && (
        <p className="text-[12.5px] text-destructive">
          That didn’t go through: {error}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {pending ? (
          <>
            <Button
              size="sm"
              variant={op === "delete" ? "destructive" : "default"}
              disabled={submitting !== null}
              onClick={() => void decide("accepted")}
            >
              {submitting === "accepted" && <Spinner className="size-3" />}
              {applyLabel}
            </Button>
            <Button
              size="sm"
              variant="outline"
              render={
                <Link to="/change-requests/$id" params={{ id }}>
                  Edit first
                </Link>
              }
            />
            <Button
              size="sm"
              variant="ghost"
              disabled={submitting !== null}
              onClick={() => void decide("rejected")}
            >
              {submitting === "rejected" && <Spinner className="size-3" />}
              Dismiss
            </Button>
            {remedyRule && (
              <Button
                size="sm"
                variant="ghost"
                className="text-muted-foreground"
                disabled={submitting !== null}
                onClick={() => setRemedyOpen((v) => !v)}
              >
                Always allow this
              </Button>
            )}
          </>
        ) : (
          <ReviewLink id={id}>See the change</ReviewLink>
        )}
      </div>
      {remedyOpen && remedyRule && proposer && target && (
        <div className="flex flex-col gap-2 rounded-lg bg-panel p-2.5">
          <p className="flex flex-wrap items-center gap-1.5 text-[12.5px] text-muted-foreground">
            This also saves a rule:
            <AgentMark id={proposer} size="xs" />
            <span className="text-foreground">{agentName(proposer)}</span>
            may {op === "delete" ? "delete" : "change"}{" "}
            {displayPlural(target.kind).toLowerCase()} without asking you first.
          </p>
          {technical && (
            <pre className="overflow-x-auto rounded-md bg-background p-2 font-mono text-[11px]">
              {JSON.stringify(remedyRule, null, 2)}
            </pre>
          )}
          <div>
            <Button
              size="sm"
              variant="outline"
              disabled={submitting !== null}
              onClick={() => void acceptAndAllow()}
            >
              {submitting === "accepted" && <Spinner className="size-3" />}
              Save the rule and apply
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

/** A value as the card shows it: a reference as the record it names, anything
 * else in words. */
function Value({ value }: { value: unknown }) {
  const held = readReference(value)
  const split =
    held && typeof value === "object" ? splitRecordPath(held.path) : undefined
  if (split) return <RecordRef kind={split.kind} id={split.id} />
  return <span>{valueWords(value)}</span>
}

/** WHAT the accept would do, in words: per property before → after for a
 * patch, the values a create would set, the consequence of a delete. Shown
 * while the suggestion is PENDING — the moment of decision. */
function ChangeWords({
  record,
  op,
  target,
  skip,
}: {
  record: SubstrateRecord
  op: string
  target?: SubstrateRecord
  /** A create's heading property, already in the card's title. */
  skip?: string
}) {
  if (op === "delete") {
    return (
      <p className="text-[13px] text-destructive">
        Deleting it removes it from your data. History keeps what it was.
      </p>
    )
  }
  const diff = proposedDiff(record)
  const rows = deriveChangeRows(
    diff.properties,
    op === "patch" ? target : undefined
  ).filter((row) => row.key !== skip)
  if (rows.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted-foreground">
      {rows.map((row, i) => (
        <span
          key={row.key}
          className="inline-flex flex-wrap items-center gap-1.5"
        >
          {i > 0 && <span className="text-faint">·</span>}
          <RowWords row={row} />
        </span>
      ))}
    </div>
  )
}

function RowWords({ row }: { row: ChangeRow }) {
  const label = propertyLabel(row.key)
  switch (row.effect) {
    case "clear":
      return (
        <>
          <span>{label}</span>
          <s className="text-faint">
            <Value value={row.before} />
          </s>
          <span>cleared</span>
        </>
      )
    case "unchanged":
      return (
        <>
          <span>{label}</span>
          <span className="text-foreground">
            <Value value={row.after} />
          </span>
          <span className="text-faint">(already set)</span>
        </>
      )
    case "set":
      return (
        <>
          <span>{label}</span>
          <s className="text-faint">
            <Value value={row.before} />
          </s>
          <span aria-hidden>→</span>
          <span className="text-foreground">
            <Value value={row.after} />
          </span>
        </>
      )
    default:
      return (
        <>
          <span>{label}</span>
          <span className="text-foreground">
            <Value value={row.after} />
          </span>
        </>
      )
  }
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

function ReviewLink({ id, children }: { id: string; children: string }) {
  return (
    <Link
      to="/change-requests/$id"
      params={{ id }}
      className="text-[12.5px] text-muted-foreground underline underline-offset-4 hover:text-foreground"
    >
      {children}
    </Link>
  )
}
