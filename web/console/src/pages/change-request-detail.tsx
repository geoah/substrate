/** A suggested change (`/change-requests/:id`), read the way its chat card
 * reads it, at full size: "Change to <record>", why, who suggested it and
 * when, then what the record holds now beside what it would hold if applied,
 * in the record page's own labels and values, then Apply and Dismiss. A create
 * shows the record it would add; a delete says plainly what goes. The ids, the
 * op, the versions, the policy that held the write, the thread it came from,
 * a judge's verdict and the raw diff are behind Technical details.
 *
 * Nothing here decides anything the card does not: the buttons are the
 * card's, one CAS'd decision each, and a refused apply comes back as the
 * request's `substrate/conflict` annotation rather than as a half-applied
 * change. A decided request renders read-only. */

import { useMemo, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  AlertTriangleIcon,
  FilePenLineIcon,
  FileQuestionIcon,
} from "lucide-react"

import { AgentRef } from "@/components/agent/agent-ref"
import {
  ChangeLabel,
  ChangeValue,
  DecisionButtons,
} from "@/components/change-request"
import { ActorRef } from "@/components/identity/actor-ref"
import { IdText } from "@/components/identity/id-text"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { PageHeader } from "@/components/identity/page-header"
import { SectionHead } from "@/components/identity/section-head"
import { DocPage } from "@/components/identity/page-layout"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  DECISION_WORDS,
  agentActor,
  changeSpecs,
  judgeVerdictOf,
  proposedHeading,
  requestThreadId,
  sinceWords,
  threadAgentId,
  verdictWords,
} from "@/lib/agent-chat"
import { CR_NAME, changeRequestQueryOptions } from "@/lib/api/changerequests"
import {
  CORE_PACKAGE,
  LLM_PACKAGE,
  LLM_PACKAGE_NAME,
  CORE_AUTHORITY,
} from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
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
  diffCannotApply,
  diffNamesNothing,
  proposedDiff,
  proposerOf,
  rationaleOf,
  targetDrift,
  type ChangeRow,
  type ChangeTargetRef,
  type ProposedDiff,
  type UnreadableField,
} from "@/lib/changerequests"
import { kindByIdentity } from "@/lib/definition"
import { displayName, lowerFirst } from "@/lib/kind-names"
import type { PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import { changeRequestDetailRoute } from "@/router"

function Warning({
  tone = "warning",
  children,
}: {
  tone?: "warning" | "destructive"
  children: ReactNode
}) {
  return (
    <div
      role="note"
      className={cn(
        "mt-5 flex items-start gap-3 rounded-lg border px-4 py-3 text-[13px]",
        tone === "destructive"
          ? "border-destructive/30 bg-bad-soft"
          : "border-warning/30 bg-warn-soft"
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

/** What the change is, as the page's title. */
function Heading({
  op,
  target,
  diff,
}: {
  op?: string
  target?: ChangeTargetRef
  diff: ProposedDiff
}) {
  if (!op) return <>A change the console can’t read</>
  if (op === "create") {
    const what = target ? lowerFirst(displayName(target.kind)) : "record"
    const heading = proposedHeading(diff.properties)
    return <>{heading ? `New ${what}: ${heading.text}` : `New ${what}`}</>
  }
  const record = target ? (
    <RecordRef kind={target.kind} id={target.id} />
  ) : (
    "a record"
  )
  return (
    <span className="inline-flex flex-wrap items-baseline gap-x-2">
      <span>{op === "delete" ? "Delete" : "Change to"}</span>
      {record}
    </span>
  )
}

/** Who suggested it: the agent whose thread proposed it, else whoever wrote
 * the proposal. */
function Suggester({ agent, actor }: { agent?: string; actor?: string }) {
  if (agent) return <AgentRef id={agent} link />
  if (actor) return <ActorRef actor={actor} />
  return null
}

// ── the comparison ──────────────────────────────────────────────────────────

const GRID3 =
  "grid grid-cols-[minmax(96px,150px)_minmax(0,1fr)_minmax(0,1fr)] sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)_minmax(0,1fr)]"
const GRID2 =
  "grid grid-cols-[minmax(96px,150px)_minmax(0,1fr)] sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]"
const HEAD = "pr-4 pb-1.5 text-[12px] font-medium text-faint"
const CELL = "min-w-0 py-2 pr-4 text-[14px]"

/** Now beside If applied, one row per property the change names. */
function Comparison({
  rows,
  specs,
  emptyText,
}: {
  rows: ChangeRow[]
  specs: Map<string, PropSpec>
  emptyText: string
}) {
  const [technical] = useTechnicalDetails()
  return (
    <div data-slot="change-comparison" className={GRID3}>
      <span className={HEAD} />
      <span className={HEAD}>Now</span>
      <span className={cn(HEAD, "text-primary-text")}>If applied</span>
      {rows.length === 0 && (
        <p className="col-span-3 border-t py-3 text-[13px] text-muted-foreground">
          {emptyText}
        </p>
      )}
      {rows.map((row) => {
        const spec = specs.get(row.key)
        return (
          <div key={row.key} className="contents">
            <span className={cn(CELL, "border-t")}>
              <ChangeLabel name={row.key} spec={spec} />
              {technical && (
                <span className="block font-mono text-[11.5px] text-faint">
                  {row.key}
                </span>
              )}
            </span>
            <span className={cn(CELL, "border-t text-muted-foreground")}>
              <ChangeValue
                value={row.before === undefined ? "" : row.before}
                spec={spec}
              />
              {technical && row.manager && row.effect !== "unchanged" && (
                <span className="mt-1 flex items-center gap-1.5 text-[12px] text-faint">
                  set by <ActorRef actor={row.manager} />
                </span>
              )}
            </span>
            <span className={cn(CELL, "border-t")}>
              <ChangeValue value={row.after} spec={spec} />
              {row.effect === "unchanged" && (
                <span className="ml-1.5 text-[12.5px] text-faint">
                  Already set
                </span>
              )}
            </span>
          </div>
        )
      })}
    </div>
  )
}

/** The values alone: what a create would add, and what was suggested once
 * the request is decided (the record has moved on since). */
function Values({
  rows,
  specs,
  caption,
  emptyText,
}: {
  rows: Array<{ key: string; value: unknown }>
  specs: Map<string, PropSpec>
  caption: string
  emptyText: string
}) {
  return (
    <div data-slot="change-values" className={GRID2}>
      <span className={HEAD} />
      <span className={HEAD}>{caption}</span>
      {rows.length === 0 && (
        <p className="col-span-2 border-t py-3 text-[13px] text-muted-foreground">
          {emptyText}
        </p>
      )}
      {rows.map((row) => {
        const spec = specs.get(row.key)
        return (
          <div key={row.key} className="contents">
            <span className={cn(CELL, "border-t")}>
              <ChangeLabel name={row.key} spec={spec} />
            </span>
            <span className={cn(CELL, "border-t")}>
              <ChangeValue value={row.value} spec={spec} />
            </span>
          </div>
        )
      })}
    </div>
  )
}

/** What an empty comparison means, which is never simply "nothing". */
function emptyText(diff: ProposedDiff, blocked: boolean): string {
  if (blocked) return "Nothing here can be read. What it holds is listed below."
  if (!diffNamesNothing(diff)) {
    return "It changes no property. What it does change is listed below."
  }
  return "This suggestion changes nothing, so applying it would do nothing. Dismiss it instead."
}

// ── the page ────────────────────────────────────────────────────────────────

function useRecordOf(
  kinds: KindInfo[],
  kind: string | undefined,
  id: string | undefined,
  enabled: boolean
) {
  const info = kind ? kindByIdentity(kinds, kind) : undefined
  return {
    kind: info,
    query: useQuery({
      ...recordQueryOptions(
        info?.authority ?? "",
        info?.package ?? "",
        info?.name ?? "",
        id ?? ""
      ),
      enabled: enabled && Boolean(info && id),
    }),
  }
}

export function ChangeRequestDetailPage() {
  const { id } = changeRequestDetailRoute.useParams()
  const registry = useQuery(kindsQueryOptions)
  const cr = useQuery(changeRequestQueryOptions(id))
  const kinds = registry.data ?? []

  const op = cr.data ? changeOp(cr.data) : undefined
  const target = cr.data ? changeTarget(cr.data) : undefined
  const decision = cr.data ? decisionOf(cr.data) : undefined
  const pending = decision === "proposed"
  const targetSide = useRecordOf(
    kinds,
    target?.kind,
    target?.id,
    pending && op !== "create"
  )
  const threadId = cr.data ? requestThreadId(cr.data) : undefined
  const thread = useQuery({
    ...recordQueryOptions(
      CORE_AUTHORITY,
      LLM_PACKAGE_NAME,
      "thread",
      threadId ?? ""
    ),
    enabled: Boolean(threadId),
  })
  const [technical] = useTechnicalDetails()
  const specs = useMemo(() => changeSpecs(targetSide.kind), [targetSide.kind])

  if (cr.isPending || registry.isPending) return <DetailSkeleton />

  if (cr.isError) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileQuestionIcon />
            </EmptyMedia>
            <EmptyTitle>This suggestion didn’t load</EmptyTitle>
            <EmptyDescription>{cr.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void cr.refetch()}
            >
              Try again
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
  const decider = deciderOf(request)
  const note = decisionNote(request)
  const conflict = applyConflict(request)
  const agent = thread.data ? threadAgentId(thread.data) : undefined
  const proposer = proposerOf(request)

  const targetRecord = targetSide.query.data
  const drift =
    op === "patch" ? targetDrift(request, diff, targetRecord) : undefined
  const blocked = diffCannotApply(diff)
  const empty = emptyText(diff, blocked)

  return (
    <DocPage>
      <PageHeader
        size="record"
        glyph={
          target?.kind ? (
            <KindGlyph kind={target.kind} size="lg" />
          ) : (
            <span className="grid size-10 place-items-center rounded-[10px] bg-hover text-muted-foreground">
              <FilePenLineIcon className="size-5" />
            </span>
          )
        }
        title={<Heading op={op} target={target} diff={diff} />}
        meta={
          <>
            {decision && (
              <StateBadge
                value={decision}
                initial={DECISION_INITIAL}
                label={DECISION_WORDS[decision]}
              />
            )}
            {(agent || proposer) && (
              <span className="flex flex-wrap items-center gap-1.5 text-muted-foreground">
                Suggested by <Suggester agent={agent} actor={proposer} />
                <span title={request.createdAt}>
                  {sinceWords(request.createdAt)}
                </span>
              </span>
            )}
            {threadId && (
              <Link
                to="/agents"
                search={{ thread: threadId } as never}
                className="text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
              >
                Open the chat
              </Link>
            )}
          </>
        }
      />

      {rationale && (
        <p className="mt-5 max-w-[68ch] border-l-2 pl-3.5 text-[14px] [overflow-wrap:anywhere]">
          {rationale}
        </p>
      )}

      {conflict && (
        <Warning>
          <p className="font-medium">This change couldn’t be applied.</p>
          <p>
            The record changed after it was suggested, or the change no longer
            fits it. Nothing was written.
          </p>
          {technical && (
            <p className="text-xs [overflow-wrap:anywhere]">
              {conflict.reason}
            </p>
          )}
        </Warning>
      )}
      {!op && (
        <Warning>
          <p>
            The console can’t tell what applying this would do, so it can’t be
            applied. Dismiss it, or ask the agent again.
          </p>
          {technical && (
            <p className="text-xs">op: {String(request.properties.op)}</p>
          )}
        </Warning>
      )}
      {diff.unreadable && (
        <Warning>
          <p>
            This suggestion is stored in a shape that can’t be read, so applying
            it will fail. Dismiss it.
          </p>
        </Warning>
      )}
      {diff.refused.length > 0 && (
        <Warning>
          <p>
            This suggestion carries parts that can’t be applied, so applying it
            will fail:{" "}
            <span className="font-mono text-xs">{diff.refused.join(", ")}</span>
            .
          </p>
        </Warning>
      )}
      {diff.malformed.length > 0 && (
        <Warning>
          <p>
            Part of this suggestion is stored in a shape that can’t be applied,
            so applying it will fail. What it holds is listed below.
          </p>
        </Warning>
      )}
      {drift && (
        <Warning>
          <p>
            The record changed after this was suggested, so it can’t be applied
            as it is. Dismiss it and ask the agent again.
          </p>
          {technical && (
            <p className="text-xs text-muted-foreground">
              Written for version {drift.version} (
              {drift.via === "diff.ifVersion"
                ? "the diff’s own ifVersion"
                : "the request’s targetVersion"}
              ); the record is at version {drift.current}.
            </p>
          )}
        </Warning>
      )}

      <section className="mt-7">
        {pending && op === "delete" && (
          <DeleteBody
            target={target}
            record={targetRecord}
            kind={targetSide.kind}
            specs={specs}
          />
        )}
        {pending && op !== "delete" && (
          <PendingBody
            op={op}
            diff={diff}
            targetRecord={targetRecord}
            targetKind={targetSide.kind}
            specs={specs}
            loading={targetSide.query.isLoading}
            error={targetSide.query.error?.message}
            kindMissing={Boolean(target && !targetSide.kind)}
            emptyText={empty}
          />
        )}
        {!pending && (
          <>
            <p className="mb-5 text-[14px]">
              {decision === "accepted" ? "Applied" : "Dismissed"}
              {decidedAt && (
                <span title={decidedAt}> {sinceWords(decidedAt)}</span>
              )}
              {decider && (
                <span className="inline-flex items-center gap-1.5">
                  &nbsp;by <ActorRef actor={decider} />
                </span>
              )}
              .{" "}
              {decision === "accepted"
                ? "What is shown is what was suggested; open the record to see where it stands now."
                : "Nothing was changed."}
            </p>
            {note && (
              <p className="mb-5 text-[13px] text-muted-foreground">
                Note: {note}
              </p>
            )}
            {op !== "delete" && (
              <Values
                rows={Object.entries(diff.properties).map(([key, value]) => ({
                  key,
                  value,
                }))}
                specs={specs}
                caption="What was suggested"
                emptyText={empty}
              />
            )}
          </>
        )}
      </section>

      <AlsoChanges diff={diff} />
      <UnreadableFields fields={diff.malformed} />

      {pending && op && (
        <div className="mt-6">
          <DecisionButtons request={request} op={op} />
        </div>
      )}

      {technical && (
        <TechnicalDetails
          request={request}
          op={op}
          target={target}
          diff={diff}
          threadId={threadId}
          thread={thread.data}
        />
      )}
    </DocPage>
  )
}

/** A patch's comparison, or a create's values. */
function PendingBody({
  op,
  diff,
  targetRecord,
  targetKind,
  specs,
  loading,
  error,
  kindMissing,
  emptyText,
}: {
  op?: string
  diff: ProposedDiff
  targetRecord?: SubstrateRecord
  targetKind?: KindInfo
  specs: Map<string, PropSpec>
  loading: boolean
  error?: string
  kindMissing: boolean
  emptyText: string
}) {
  const comparable = op === "patch" && Boolean(targetRecord)
  const rows = useMemo(
    () =>
      deriveChangeRows(
        diff.properties,
        comparable ? targetRecord : undefined,
        targetKind
      ),
    [diff.properties, comparable, targetRecord, targetKind]
  )
  const values = rows.map((r) => ({ key: r.key, value: r.after }))

  if (op === "create") {
    // The property the title already reads from is not a row too.
    const heading = proposedHeading(diff.properties)?.key
    return (
      <>
        <SectionHead title="What it adds" className="mt-0" />
        <Values
          rows={values.filter((r) => r.key !== heading)}
          specs={specs}
          caption="If applied"
          emptyText={emptyText}
        />
      </>
    )
  }
  if (op === "patch" && !comparable) {
    if (loading) return <Skeleton className="h-24 w-full rounded-md" />
    return (
      <>
        <p className="mb-3 text-[13px] text-muted-foreground">
          {error
            ? `The record didn’t load, so what it holds now can’t be shown: ${error}`
            : kindMissing
              ? "This repository doesn’t have the record’s collection, so what it holds now can’t be shown."
              : "This suggestion names no record to change, so it can’t be applied."}
        </p>
        <Values
          rows={values}
          specs={specs}
          caption="If applied"
          emptyText={emptyText}
        />
      </>
    )
  }
  const noop = op === "patch" && appliesNothing(diff, rows)
  return (
    <>
      <SectionHead title="What changes" className="mt-0" />
      {noop && (
        <p className="mb-3 text-[13px] text-warning">
          The record already has every value suggested here, so applying would
          do nothing. Dismiss it instead.
        </p>
      )}
      {op === "patch" ? (
        <Comparison rows={rows} specs={specs} emptyText={emptyText} />
      ) : (
        <Values
          rows={values}
          specs={specs}
          caption="If applied"
          emptyText={emptyText}
        />
      )}
    </>
  )
}

/** What a delete takes away: the consequence, then enough of the record to
 * recognise it. */
function DeleteBody({
  target,
  record,
  kind,
  specs,
}: {
  target?: ChangeTargetRef
  record?: SubstrateRecord
  kind?: KindInfo
  specs: Map<string, PropSpec>
}) {
  const rows = record
    ? Object.entries(record.properties)
        .filter(
          ([key, value]) =>
            key !== "title" &&
            value !== null &&
            value !== undefined &&
            value !== "" &&
            (!kind || specs.has(key))
        )
        .slice(0, 8)
        .map(([key, value]) => ({ key, value }))
    : []
  return (
    <>
      <p className="mb-4 text-[14px] text-destructive">
        Applying deletes{" "}
        {target ? <RecordRef kind={target.kind} id={target.id} /> : "it"}. It
        leaves your data, and anything that points at it loses the link. History
        keeps what it was.
      </p>
      {rows.length > 0 && (
        <Values
          rows={rows}
          specs={specs}
          caption="What it holds now"
          emptyText=""
        />
      )}
    </>
  )
}

/** Labels, annotations and finalizers a change carries beside its properties:
 * rare, and applied with the rest, so never applied unseen. */
function AlsoChanges({ diff }: { diff: ProposedDiff }) {
  const groups: Array<[string, Array<[string, unknown]>]> = []
  if (diff.labels && Object.keys(diff.labels).length) {
    groups.push(["Labels it sets", Object.entries(diff.labels)])
  }
  if (diff.annotations && Object.keys(diff.annotations).length) {
    groups.push(["Annotations it sets", Object.entries(diff.annotations)])
  }
  if (diff.addFinalizers.length) {
    groups.push([
      "Finalizers it adds",
      diff.addFinalizers.map((f) => [f, "added"] as [string, unknown]),
    ])
  }
  if (diff.removeFinalizers.length) {
    groups.push([
      "Finalizers it removes",
      diff.removeFinalizers.map((f) => [f, "removed"] as [string, unknown]),
    ])
  }
  if (!groups.length) return null
  return (
    <section className="mt-7">
      <SectionHead title="It also changes" className="mt-0" />
      <div className="flex flex-col gap-3 text-[13px]">
        {groups.map(([name, values]) => (
          <div key={name}>
            <p className="text-[12px] font-medium text-faint">{name}</p>
            <div className="mt-1 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5">
              {values.map(([key, value]) => (
                <span key={key} className="contents">
                  <span className="max-w-60 font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
                    {key}
                  </span>
                  <span className="min-w-0 text-xs [overflow-wrap:anywhere]">
                    {value === null
                      ? "removed"
                      : typeof value === "string"
                        ? value
                        : JSON.stringify(value)}
                  </span>
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

/** What the diff carries where a shape is expected and another is found: the
 * key and the raw value, verbatim, because applying fails on every one. */
function UnreadableFields({ fields }: { fields: UnreadableField[] }) {
  if (!fields.length) return null
  return (
    <section className="mt-7">
      <SectionHead title="What couldn’t be read" className="mt-0" />
      <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
        {fields.map((field) => {
          const raw = JSON.stringify(field.raw) ?? String(field.raw)
          return (
            <span key={field.key} className="contents">
              <span className="max-w-40 truncate font-mono text-warning">
                {field.key}
              </span>
              <span className="min-w-0 font-mono [overflow-wrap:anywhere]">
                {raw}
              </span>
            </span>
          )
        })}
      </div>
    </section>
  )
}

/** Everything a developer reaches for, one hover or one switch away: the
 * request and target references, the op and versions, the policy whose gate
 * turned the write into this request, the thread it came from, a judge's
 * verdict, and the diff exactly as stored. */
function TechnicalDetails({
  request,
  op,
  target,
  diff,
  threadId,
  thread,
}: {
  request: SubstrateRecord
  op?: string
  target?: ChangeTargetRef
  diff: ProposedDiff
  threadId?: string
  thread?: SubstrateRecord
}) {
  const policy = readReference(request.properties.policy)?.path
  const revision = request.properties.policyRevision
  const verdict = judgeVerdictOf(request)
  const targetVersion = request.properties.targetVersion
  const agent = thread ? threadAgentId(thread) : undefined
  const proposer = proposerOf(request)
  const facts: Array<[string, ReactNode]> = [
    [
      "Request",
      <IdText value={`${CORE_PACKAGE}/${CR_NAME}/${request.id}`} copy />,
    ],
    ["Request version", String(request.version)],
    ["Op", <span>{op ?? String(request.properties.op)}</span>],
  ]
  if (target) {
    facts.push([
      "Target",
      <IdText value={`${target.kind}/${target.id}`} copy />,
    ])
  }
  if (typeof targetVersion === "number") {
    facts.push(["Written for", `version ${targetVersion}`])
  }
  if (diff.ifVersion !== undefined) {
    facts.push(["Diff ifVersion", String(diff.ifVersion)])
  }
  facts.push([
    "Policy",
    policy ? (
      <span className="flex flex-col gap-0.5">
        <IdText value={policy} copy />
        {typeof revision === "number" && (
          <span className="text-muted-foreground">at revision {revision}</span>
        )}
      </span>
    ) : (
      <span className="text-muted-foreground">
        None: this was suggested, not held by a gate.
      </span>
    ),
  ])
  if (verdict) facts.push(["Judge", verdictWords(verdict)])
  if (threadId) {
    facts.push([
      "Thread",
      <IdText value={`${LLM_PACKAGE}/thread/${threadId}`} copy />,
    ])
  }
  if (agent) facts.push(["Agent", <IdText value={agentActor(agent)} copy />])
  if (proposer) facts.push(["Written by", <IdText value={proposer} copy />])

  return (
    <section className="mt-9 rounded-lg border bg-panel p-4">
      <SectionHead title="Technical details" className="mt-0" />
      <dl className="grid grid-cols-[minmax(90px,130px)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-[13px]">
        {facts.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-faint">{label}</dt>
            <dd className="min-w-0 [overflow-wrap:anywhere]">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="mt-4 mb-1 text-[12px] font-medium text-faint">Diff</p>
      <pre className="overflow-x-auto rounded-md border bg-background px-3 py-2 font-mono text-xs whitespace-pre-wrap">
        {JSON.stringify(request.properties.diff ?? null, null, 2)}
      </pre>
    </section>
  )
}

function DetailSkeleton() {
  return (
    <DocPage>
      <div className="flex items-start gap-3.5">
        <Skeleton className="size-10 rounded-[10px]" />
        <div className="flex-1">
          <Skeleton className="h-7 w-72" />
          <Skeleton className="mt-2 h-3.5 w-80" />
        </div>
      </div>
      <Skeleton className="mt-5 h-10 w-full max-w-[68ch]" />
      <Skeleton className="mt-7 h-40 w-full rounded-lg" />
    </DocPage>
  )
}
