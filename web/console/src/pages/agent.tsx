/** One agent (`/agents/$id`): what it is and runs on (the provider and the
 * model, changed together here), what it is allowed to see and change
 * (edited here, by collection), the limits one run runs under, the rules its
 * changes go through (the ones "Always allow this" wrote, each revocable),
 * and its runs: what they spent over the last day, week and month, the ones
 * that didn't finish grouped by why, and the newest of them, each opening
 * its thread. Every figure is summed on the client from ONE read of its
 * newest runs (`AGENT_RUNS_WINDOW`, the wire's page cap), and a period that
 * reaches past that read says "at least". Chatting with it is the chat
 * app's; this page links there. Technical mode adds the references and the
 * declaration. */

import { useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { BotIcon, MessageSquarePlusIcon, PencilIcon } from "lucide-react"

import { AgentManifest } from "@/components/agent/agent-manifest"
import { AgentMark } from "@/components/agent/agent-mark"
import { AgentRef } from "@/components/agent/agent-ref"
import { AllowRules } from "@/components/agent/always-allow"
import { GrantsEditor } from "@/components/agent/grants-editor"
import { AddKeyButton } from "@/components/agent/model-key-dialog"
import { RunsOn } from "@/components/agent/runs-on"
import { EmptyValue } from "@/components/identity/empty-value"
import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { DocPage } from "@/components/identity/page-layout"
import { Pill, type PillTone } from "@/components/identity/pill"
import { SectionHead } from "@/components/identity/section-head"
import { AlertsPanel } from "@/components/alerts/alerts-panel"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  agentName,
  agentProviderId,
  agentSubagents,
  agentTools,
  providerName,
  toolRoute,
} from "@/lib/agent-chat"
import { standingAllows } from "@/lib/agent-rules"
import {
  STATUS_WORDS,
  agentLimits,
  costWords,
  durationWords,
  failureGroups,
  isConversation,
  periodTotals,
  policySentence,
  runOutlastsInvocation,
  runCost,
  runDurationMs,
  runFailed,
  runSettledAt,
  runStartWords,
  runStartedAt,
  runStatus,
  runTokens,
  runUnpriced,
  tokenWords,
  type AgentLimit,
  type FailureGroup,
  type GroupTotals,
  type PeriodTotals,
  type RunStatus,
} from "@/lib/agent-runs"
import {
  AGENT_RUNS_WINDOW,
  agentRunsQueryOptions,
  llmProvidersQueryOptions,
  policiesForAgent,
  providerHasKey,
  writePoliciesQueryOptions,
} from "@/lib/api/agents"
import {
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  LLM_PACKAGE_NAME,
} from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { AGENT_KIND } from "@/lib/declarations"
import { propertyLabel } from "@/lib/grid-values"
import { propSpecsByName, type PropSpec } from "@/lib/record-schema"
import { agoWords, toolName } from "@/lib/tools"
import { cn } from "@/lib/utils"
import { agentRoute } from "@/router"

const STATUS_TONE: Record<RunStatus, PillTone> = {
  running: "accent",
  ok: "ok",
  overbudget: "warn",
  error: "bad",
}

/** How many runs the table shows at first, and adds per "Show more". */
const RUNS_PAGE = 50

export function AgentPage() {
  const { id } = agentRoute.useParams()
  const [technical] = useTechnicalDetails()
  const agent = useQuery(
    recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, "agent", id)
  )
  const runs = useQuery(agentRunsQueryOptions(id))
  const policies = useQuery(writePoliciesQueryOptions())
  const providers = useQuery(llmProvidersQueryOptions())
  const registry = useQuery(kindsQueryOptions)

  if (agent.isPending) {
    return (
      <DocPage>
        <Skeleton className="h-10 w-72" />
        <Skeleton className="mt-6 h-32 w-full" />
      </DocPage>
    )
  }
  if (agent.isError || !agent.data) {
    return (
      <DocPage>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BotIcon />
            </EmptyMedia>
            <EmptyTitle>This agent didn’t load</EmptyTitle>
            <EmptyDescription>
              {agent.error?.message ?? "It may have been removed."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </DocPage>
    )
  }

  const record = agent.data
  const name = agentName(id)
  const model =
    typeof record.properties.model === "string" ? record.properties.model : ""
  const provider = agentProviderId(record)
  const providerRow = providers.data?.records.find((p) => p.id === provider)
  const keyless = providerRow ? !providerHasKey(providerRow) : false
  const description =
    typeof record.properties.description === "string"
      ? record.properties.description
      : undefined
  const mine = policiesForAgent(id, policies.data?.records ?? [])
  const allowed = standingAllows(mine, id)
  // The allows "Always allow this" wrote are listed on their own, revocable.
  const written = new Set(allowed.flatMap((r) => r.records.map((p) => p.id)))
  const rules = mine.filter((p) => !written.has(p.id))
  const threads = runs.data?.records ?? []
  // The page carries a cursor only when rows past it exist.
  const truncated = Boolean(runs.data?.cursor)
  const tools = agentTools(record)
  const subagents = agentSubagents(record)
  const hidden = record.properties.hiddenFromChat === true
  const agentKind = registry.data?.find((k) => k.identity === AGENT_KIND)

  return (
    <DocPage>
      <PageHeader
        title={name}
        glyph={<AgentMark id={id} size="lg" />}
        meta={
          <span className="text-muted-foreground">
            {[model, provider && providerName(provider)]
              .filter(Boolean)
              .join(" · ") || "Agent"}
            {hidden && " · Works for other agents"}
          </span>
        }
        description={description}
        actions={
          <>
            {!hidden && (
              <Button
                size="sm"
                render={
                  <Link to="/agents" search={{ agent: id } as never}>
                    <MessageSquarePlusIcon />
                    Start a chat
                  </Link>
                }
              />
            )}
            <Button
              size="sm"
              variant="outline"
              render={
                <Link
                  to="/data/$authority/$pkg/$name/$id"
                  params={{
                    authority: CORE_AUTHORITY,
                    pkg: CORE_PACKAGE_NAME,
                    name: "agent",
                    id,
                  }}
                >
                  <PencilIcon />
                  Edit agent
                </Link>
              }
            />
          </>
        }
      >
        {technical && (
          <div className="mt-2">
            <IdText
              value={`${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/agent/${id}`}
              copy
            />
          </div>
        )}
      </PageHeader>

      {keyless && provider && (
        <div
          role="note"
          className="mt-5 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-warning/30 bg-warn-soft px-3.5 py-2.5 text-[13px]"
        >
          <span className="min-w-0 flex-1">
            <span className="font-medium">{name} can’t run yet.</span>{" "}
            <span className="text-muted-foreground">
              It runs on {providerName(provider)}, which has no API key.
            </span>
          </span>
          <AddKeyButton providerId={provider} />
        </div>
      )}

      <AlertsPanel refs={[`${AGENT_KIND}/${id}`]} />

      <SectionHead
        title="What it runs on"
        hint="a model id belongs to one provider"
      />
      <RunsOn agent={record} />

      <SectionHead title="What it’s allowed to do" />
      <GrantsEditor agent={record} />
      <Facts
        rows={[
          [
            "Tools it can use",
            tools.length === 0 ? (
              <span className="text-muted-foreground">
                None. It answers from what you tell it.
              </span>
            ) : (
              <ul className="flex flex-wrap gap-x-3 gap-y-1">
                {tools.map((tool) => {
                  const route = toolRoute(tool.function)
                  return (
                    <li key={tool.function}>
                      {route ? (
                        <Link
                          to="/tools/$authority/$pkg/$name"
                          params={route}
                          className="underline-offset-2 hover:underline"
                        >
                          {toolName(tool.function)}
                        </Link>
                      ) : (
                        toolName(tool.function)
                      )}
                    </li>
                  )
                })}
              </ul>
            ),
          ],
          ...(subagents.length
            ? ([
                [
                  "Can ask",
                  <ul className="flex flex-wrap gap-x-3 gap-y-1">
                    {subagents.map((sub) => (
                      <li key={sub}>
                        <AgentRef id={sub} />
                      </li>
                    ))}
                  </ul>,
                ],
              ] as Array<[string, ReactNode]>)
            : []),
        ]}
      />

      <SectionHead title="Limits" hint="what bounds one run" />
      <Limits
        limits={agentLimits(record)}
        specs={agentKind ? propSpecsByName(agentKind) : []}
      />

      <SectionHead
        title="Always allowed"
        hint="Changes it makes without asking you first"
      />
      <AllowRules agent={id} rules={allowed} />

      {rules.length > 0 && (
        <>
          <SectionHead
            title="Rules for its changes"
            hint="What happens when it tries to change something"
          />
          <ul className="flex flex-col gap-1.5 text-[13.5px]">
            {rules.map((policy) => {
              const sentence = policySentence(policy)
              return (
                <li
                  key={policy.id}
                  className="flex flex-wrap items-center gap-2"
                >
                  <Pill tone={sentence.tone}>
                    {sentence.tone === "bad"
                      ? "Refused"
                      : sentence.tone === "warn"
                        ? "Asks first"
                        : "Allowed"}
                  </Pill>
                  <span>{sentence.text}</span>
                  {sentence.everyone && (
                    <span className="text-[12.5px] text-faint">
                      For every agent
                    </span>
                  )}
                  {technical && <IdText value={policy.id} />}
                </li>
              )
            })}
          </ul>
        </>
      )}

      {runs.isPending ? (
        <>
          <SectionHead title="Recent runs" />
          <Skeleton className="h-24 w-full" />
        </>
      ) : runs.isError ? (
        <>
          <SectionHead title="Recent runs" />
          <p className="text-[13px] text-destructive">
            Its runs didn’t load: {runs.error.message}
          </p>
        </>
      ) : threads.length === 0 ? (
        <>
          <SectionHead title="Recent runs" />
          <p className="text-[13px] text-faint">It hasn’t run yet.</p>
        </>
      ) : (
        <Runs threads={threads} truncated={truncated} />
      )}

      {technical && (
        <section className="mt-9 rounded-lg border bg-panel p-4">
          <SectionHead title="Technical details" className="mt-0" />
          <AgentManifest agent={record} />
        </section>
      )}
    </DocPage>
  )
}

function Facts({ rows }: { rows: Array<[string, ReactNode]> }) {
  return (
    <dl className="grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-3 border-t sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]">
      {rows.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="py-2.5 text-[13.5px] text-muted-foreground">
            {label}
          </dt>
          <dd className="min-w-0 py-2.5 text-[13.5px]">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** The declared field a limit lives at, by its path through the agent kind's
 * properties: the record page reads its label and description there too. */
function specAt(specs: PropSpec[], path: string[]): PropSpec | undefined {
  let level = specs
  let spec: PropSpec | undefined
  for (const name of path) {
    spec = level.find((s) => s.name === name)
    if (!spec) return undefined
    level = spec.fields ?? []
  }
  return spec
}

function Limits({
  limits,
  specs,
}: {
  limits: AgentLimit[]
  specs: PropSpec[]
}) {
  const [technical] = useTechnicalDetails()
  return (
    <dl
      aria-label="Limits"
      className="grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-3 border-t sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]"
    >
      {limits.map((limit) => {
        const spec = specAt(specs, limit.path)
        const key = limit.path.join(".")
        return (
          <div key={key} className="contents">
            <dt className="flex flex-col py-2.5 text-[13.5px] text-muted-foreground">
              {spec?.label ?? propertyLabel(limit.path[limit.path.length - 1])}
              {spec?.description && (
                <span className="text-[12px] text-faint">
                  {spec.description}
                </span>
              )}
              {technical && (
                <span className="font-mono text-[11.5px] [overflow-wrap:anywhere] text-faint">
                  {key}
                </span>
              )}
            </dt>
            <dd className="min-w-0 py-2.5 text-[13.5px] tabular-nums">
              {limit.value.toLocaleString()}
              {!limit.declared && (
                <span className="ml-1.5 text-[12.5px] text-faint">
                  the default
                </span>
              )}
            </dd>
          </div>
        )
      })}
    </dl>
  )
}

/** Where one run opens: a conversation in the chat app, any other run (a
 * sub-agent's, a judgement) as its thread record. */
function RunLink({
  thread,
  children,
  className = "underline-offset-2 hover:underline",
}: {
  thread: SubstrateRecord
  children: ReactNode
  className?: string
}) {
  if (isConversation(thread)) {
    return (
      <Link
        to="/agents"
        search={{ thread: thread.id } as never}
        className={className}
      >
        {children}
      </Link>
    )
  }
  return (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{
        authority: CORE_AUTHORITY,
        pkg: LLM_PACKAGE_NAME,
        name: "thread",
        id: thread.id,
      }}
      className={className}
    >
      {children}
    </Link>
  )
}

function Runs({
  threads,
  truncated,
}: {
  /** Most recently active first, as the read returns them. */
  threads: SubstrateRecord[]
  truncated: boolean
}) {
  const [shown, setShown] = useState(RUNS_PAGE)
  const failures = failureGroups(threads)
  const newest = [...threads].sort(
    (a, b) => Date.parse(runStartedAt(b)) - Date.parse(runStartedAt(a))
  )
  return (
    <>
      <SectionHead
        title="What it spent"
        hint="each run counted in the period it started"
      />
      <Spend
        totals={periodTotals(threads, { truncated })}
        truncated={truncated}
      />

      {failures.length > 0 && (
        <>
          <SectionHead
            title="What went wrong"
            hint={
              truncated
                ? `the runs that didn’t finish, by reason, among the ${AGENT_RUNS_WINDOW.toLocaleString()} active most recently`
                : "the runs that didn’t finish, by reason"
            }
          />
          <Failures groups={failures} truncated={truncated} />
        </>
      )}

      <SectionHead
        title="Recent runs"
        hint={
          truncated
            ? `the ${AGENT_RUNS_WINDOW.toLocaleString()} active most recently, newest first`
            : "newest first"
        }
      />
      <RunsTable threads={newest.slice(0, shown)} />
      <p className="mt-2 text-[12.5px] text-faint">
        Took is a run’s start to its last reply, so a chat you came back to
        counts the wait between. A span longer than one run may work is faint:
        its thread went on later, or a restart settled it.
      </p>
      {shown < threads.length && (
        <Button
          size="sm"
          variant="ghost"
          className="mt-2 text-muted-foreground"
          onClick={() => setShown((n) => n + RUNS_PAGE)}
        >
          Show more
        </Button>
      )}
    </>
  )
}

/** One row of the spend table: its words, and whether the figure is a floor
 * rather than the whole. */
const SPEND_ROWS: Array<{
  label: string
  value: (s: GroupTotals) => string
  floor: (s: GroupTotals) => boolean
}> = [
  {
    label: "Runs",
    value: (s) => s.runs.toLocaleString(),
    floor: (s) => !s.complete,
  },
  {
    label: "Didn’t finish",
    value: (s) => s.failed.toLocaleString(),
    floor: (s) => !s.complete,
  },
  {
    label: "Tokens used",
    value: (s) => tokenWords(s.tokens),
    floor: (s) => !s.spendComplete,
  },
  {
    label: "Cost",
    // "$0" beside tokens a run recorded no cost for would read as free.
    value: (s) => (noCostRecorded(s) ? "No cost recorded" : costWords(s.cost)),
    floor: (s) => !noCostRecorded(s) && (!s.spendComplete || s.unpriced > 0),
  },
]

function noCostRecorded(s: GroupTotals): boolean {
  return s.cost === 0 && s.unpriced > 0
}

/** The periods' figures, one column each. The runs it started and the runs
 * another agent asked for are two groups, never summed: a run it started
 * already counts every run it asked for (`periodTotals`). */
function Spend({
  totals,
  truncated,
}: {
  totals: PeriodTotals[]
  truncated: boolean
}) {
  const started = totals.some((t) => t.started.runs > 0)
  const asked = totals.some((t) => t.asked.runs > 0)
  const both = totals.flatMap((t) => [t.started, t.asked])
  const straddled = both.some((g) => g.complete && !g.spendComplete)
  const unpriced = both.some((g) => g.unpriced > 0)
  const groups: Array<{
    heading?: string
    pick: (t: PeriodTotals) => GroupTotals
  }> = []
  if (started || !asked) {
    groups.push({
      heading: asked ? "Runs it started" : undefined,
      pick: (t) => t.started,
    })
  }
  if (asked) {
    groups.push({
      heading: started ? "When another agent asked it" : undefined,
      pick: (t) => t.asked,
    })
  }
  const cell = "px-3 py-2 text-right tabular-nums"
  return (
    <>
      <div className="overflow-x-auto rounded-[10px] border">
        <table aria-label="What it spent" className="w-full text-[13px]">
          <thead className="bg-panel text-xs text-faint">
            <tr>
              <th scope="col" className="px-3 py-2 text-left font-normal">
                <span className="sr-only">Figure</span>
              </th>
              {totals.map((t) => (
                <th
                  key={t.period.key}
                  scope="col"
                  className={cn(cell, "font-normal")}
                >
                  {t.period.label}
                </th>
              ))}
            </tr>
          </thead>
          {groups.map((group, i) => (
            <tbody key={i} className="border-t">
              {group.heading && (
                <tr>
                  <th
                    scope="colgroup"
                    colSpan={totals.length + 1}
                    className="px-3 pt-2.5 pb-1 text-left text-[12px] font-medium text-faint"
                  >
                    {group.heading}
                  </th>
                </tr>
              )}
              {SPEND_ROWS.map((row) => (
                <tr key={row.label}>
                  <th
                    scope="row"
                    className="px-3 py-2 text-left font-normal text-muted-foreground"
                  >
                    {row.label}
                  </th>
                  {totals.map((t) => (
                    <td key={t.period.key} className={cell}>
                      {row.floor(group.pick(t)) && (
                        <span className="text-faint">at least </span>
                      )}
                      {row.value(group.pick(t))}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          ))}
        </table>
      </div>
      <div className="mt-2 flex flex-col gap-0.5 text-[12.5px] text-faint">
        <p>
          Cost is what each run recorded. A run records no cost for tokens on a
          model its provider row has no price for.
        </p>
        {started && (
          <p>
            A run it started includes the tokens and the recorded cost of the
            agents it asked.
          </p>
        )}
        {asked && (
          <p>
            A run another agent asked it for also counts in that agent’s
            figures.
          </p>
        )}
        {straddled && (
          <p>
            A run that started before a period and went on inside it spent part
            of its total there, which its thread can’t split, so that period’s
            tokens and cost say “at least”.
          </p>
        )}
        {unpriced && (
          <p>
            A cost that counts a run with tokens and no recorded cost says “at
            least”.
          </p>
        )}
        {truncated && (
          <p>
            Counted from the {AGENT_RUNS_WINDOW.toLocaleString()} runs active
            most recently. A period that reaches past them says “at least”.
          </p>
        )}
      </div>
    </>
  )
}

function Failures({
  groups,
  truncated,
}: {
  groups: FailureGroup[]
  /** The read stopped at its bound: an older run may have failed the same
   * way, so each count is a floor. */
  truncated: boolean
}) {
  return (
    <ul aria-label="What went wrong" className="flex flex-col gap-2">
      {groups.map((group) => (
        <li
          key={`${group.status}:${group.reason ?? ""}`}
          className="rounded-[10px] border px-3 py-2.5 text-[13.5px]"
        >
          <div className="flex flex-wrap items-center gap-2">
            <Pill tone={STATUS_TONE[group.status]}>
              {STATUS_WORDS[group.status]}
            </Pill>
            <span className="font-medium">
              {truncated && "at least "}
              {group.count === 1
                ? "1 run"
                : `${group.count.toLocaleString()} runs`}
            </span>
          </div>
          <p className="mt-1 break-words">
            {group.reason ?? (
              <span className="text-muted-foreground">
                No reason was recorded.
              </span>
            )}
          </p>
          <p className="mt-1 text-[12.5px] text-muted-foreground">
            Last one {agoWords(runSettledAt(group.latest))} ·{" "}
            <RunLink
              thread={group.latest}
              className="underline underline-offset-2 hover:text-foreground"
            >
              Open that run
            </RunLink>
          </p>
        </li>
      ))}
    </ul>
  )
}

function RunsTable({ threads }: { threads: SubstrateRecord[] }) {
  const [technical] = useTechnicalDetails()
  const grid =
    "grid grid-cols-[100px_minmax(0,1.2fr)_minmax(0,1.6fr)_76px_64px_84px] items-center gap-2.5 px-3 max-md:grid-cols-[84px_minmax(0,1fr)_minmax(0,1fr)]"
  return (
    <div
      className="overflow-hidden rounded-[10px] border"
      role="table"
      aria-label="Recent runs"
    >
      <div role="row" className={cn(grid, "h-8 bg-panel text-xs text-faint")}>
        <span role="columnheader">Started</span>
        <span role="columnheader">What started it</span>
        <span role="columnheader">What happened</span>
        <span role="columnheader" className="text-right max-md:hidden">
          Took
        </span>
        <span role="columnheader" className="text-right max-md:hidden">
          Tokens
        </span>
        <span role="columnheader" className="text-right max-md:hidden">
          Cost
        </span>
      </div>
      {threads.map((thread) => {
        const status = runStatus(thread)
        const at = runStartedAt(thread)
        const reason =
          typeof thread.properties.reason === "string"
            ? thread.properties.reason
            : undefined
        const tokens = runTokens(thread)
        const cost = runCost(thread)
        return (
          <div
            key={thread.id}
            role="row"
            className={cn(grid, "min-h-10 border-t py-2 text-[13px]")}
          >
            <span role="cell" className="text-muted-foreground" title={at}>
              {agoWords(at)}
            </span>
            <span role="cell" className="flex min-w-0 flex-col break-words">
              <RunLink thread={thread}>{runStartWords(thread)}</RunLink>
              {technical && (
                <IdText
                  value={`${CORE_AUTHORITY}/${LLM_PACKAGE_NAME}/thread/${thread.id}`}
                />
              )}
            </span>
            <span
              role="cell"
              className="flex min-w-0 flex-wrap items-center gap-1.5"
            >
              <Pill tone={STATUS_TONE[status]} live={status === "running"}>
                {STATUS_WORDS[status]}
              </Pill>
              {runFailed(thread) && reason && (
                <span className="min-w-0 text-[12.5px] break-words text-muted-foreground">
                  {reason}
                </span>
              )}
            </span>
            <span
              role="cell"
              className="text-right text-muted-foreground tabular-nums max-md:hidden"
            >
              <TookCell thread={thread} />
            </span>
            <span
              role="cell"
              className="text-right text-muted-foreground tabular-nums max-md:hidden"
            >
              {tokens !== undefined ? tokenWords(tokens) : <EmptyValue />}
            </span>
            <span
              role="cell"
              className="text-right text-muted-foreground tabular-nums max-md:hidden"
            >
              {runUnpriced(thread) ? (
                <EmptyValue>None recorded</EmptyValue>
              ) : cost !== undefined ? (
                costWords(cost)
              ) : (
                <EmptyValue />
              )}
            </span>
          </div>
        )
      })}
    </div>
  )
}

/** A run's span in the Took column. One longer than any invocation may work
 * is not how long it worked, and reads faint (`runOutlastsInvocation`). */
function TookCell({ thread }: { thread: SubstrateRecord }) {
  const took = runDurationMs(thread)
  if (took === undefined) return <EmptyValue />
  if (runOutlastsInvocation(thread)) {
    return (
      <span
        data-slot="took-span"
        className="text-faint"
        title="Longer than one run may work: its thread went on later, or a restart settled it"
      >
        {durationWords(took)}
      </span>
    )
  }
  return <>{durationWords(took)}</>
}
