/** A change the agent suggested, inline in the thread that suggested it. The
 * card RESOLVES the recordpatchrequest and renders its live state — never a
 * snapshot the row carried — so the thread and the review page can never
 * disagree about what a suggestion says or whether it is decided.
 *
 * Apply and Dismiss are the review page's own buttons (`DecisionButtons`);
 * Review opens that page. Deciding also writes a `system` message into this
 * very thread and resumes the agent, so a decision made here shows up as new
 * turns. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { ChangeValue, DecisionButtons } from "@/components/change-request"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  DECISION_WORDS,
  changeLabel,
  changeSpecs,
  judgeVerdictOf,
  proposedHeading,
  verdictWords,
} from "@/lib/agent-chat"
import { changeRequestQueryOptions } from "@/lib/api/changerequests"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import {
  changeOp,
  changeTarget,
  decisionOf,
  deriveChangeRows,
  proposedDiff,
  rationaleOf,
  type ChangeRow,
} from "@/lib/changerequests"
import { kindByIdentity } from "@/lib/definition"
import { displayName, lowerFirst } from "@/lib/kind-names"
import type { PropSpec } from "@/lib/record-schema"

const CARD =
  "flex flex-col gap-2 rounded-[10px] border border-border-strong bg-background px-3.5 py-3 text-sm"

export function ProposalCard({ id }: { id: string }) {
  const [technical] = useTechnicalDetails()
  const request = useQuery(changeRequestQueryOptions(id))
  // The card shows WHAT would change in the record page's own labels and
  // values: the target's kind is read for every op, the live target for a
  // patch so every row renders before AND after.
  const pendingOp = request.data ? (changeOp(request.data) ?? "patch") : "patch"
  const pendingTarget = request.data ? changeTarget(request.data) : undefined
  const registry = useQuery({
    ...kindsQueryOptions,
    enabled: Boolean(pendingTarget),
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
  const specs = changeSpecs(targetKindInfo)

  const heading = op === "create" ? proposedHeading(diff.properties) : undefined
  const Icon =
    op === "create" ? PlusIcon : op === "delete" ? Trash2Icon : PencilIcon

  return (
    <div className={CARD} data-slot="proposal-card">
      <div className="flex flex-wrap items-center gap-2 font-medium">
        <Icon className="size-4 shrink-0 text-faint" />
        {op === "create" ? (
          <>
            <span>
              New {target ? lowerFirst(displayName(target.kind)) : "record"}
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
          <StateBadge
            value={decision}
            label={DECISION_WORDS[decision]}
            className="ml-auto text-[12.5px]"
          />
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
          specs={specs}
          skip={heading?.key}
        />
      )}
      {verdict && (
        <p className="text-[12.5px] [overflow-wrap:anywhere] text-muted-foreground">
          {verdictWords(verdict)}
        </p>
      )}
      {pending ? (
        <DecisionButtons
          request={record}
          op={op}
          review={
            <Button
              size="sm"
              variant="outline"
              render={
                <Link to="/change-requests/$id" params={{ id }}>
                  Review
                </Link>
              }
            />
          }
        />
      ) : (
        <div>
          <ReviewLink id={id}>See the change</ReviewLink>
        </div>
      )}
    </div>
  )
}

/** WHAT applying would do, in words: per property before → after for a
 * patch, the values a create would set, the consequence of a delete. Shown
 * while the suggestion is PENDING — the moment of decision. */
function ChangeWords({
  record,
  op,
  target,
  specs,
  skip,
}: {
  record: SubstrateRecord
  op: string
  target?: SubstrateRecord
  specs: Map<string, PropSpec>
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
          <RowWords row={row} spec={specs.get(row.key)} />
        </span>
      ))}
    </div>
  )
}

function RowWords({ row, spec }: { row: ChangeRow; spec?: PropSpec }) {
  const label = changeLabel(row.key, spec)
  switch (row.effect) {
    case "clear":
      return (
        <>
          <span>{label}</span>
          <s className="text-faint">
            <ChangeValue value={row.before} spec={spec} />
          </s>
          <span>cleared</span>
        </>
      )
    case "unchanged":
      return (
        <>
          <span>{label}</span>
          <span className="text-foreground">
            <ChangeValue value={row.after} spec={spec} />
          </span>
          <span className="text-faint">(already set)</span>
        </>
      )
    case "set":
      return (
        <>
          <span>{label}</span>
          <s className="text-faint">
            <ChangeValue value={row.before} spec={spec} />
          </s>
          <span aria-hidden>→</span>
          <span className="text-foreground">
            <ChangeValue value={row.after} spec={spec} />
          </span>
        </>
      )
    default:
      return (
        <>
          <span>{label}</span>
          <span className="text-foreground">
            <ChangeValue value={row.after} spec={spec} />
          </span>
        </>
      )
  }
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
