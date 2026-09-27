/** One agent (`/agents/$id`): what it is and runs on, what it is allowed to
 * see and change (edited here, by collection), the rules its changes go
 * through (the ones "Always allow this" wrote, each revocable), and its
 * recent runs with what they spent and which failed. Chatting with it is the
 * chat app's; this page links there. Technical mode adds the references, the
 * budgets and the declaration. */

import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { BotIcon, MessageSquarePlusIcon, PencilIcon } from "lucide-react"

import { AgentManifest } from "@/components/agent/agent-manifest"
import { AgentMark } from "@/components/agent/agent-mark"
import { AgentRef } from "@/components/agent/agent-ref"
import { AllowRules } from "@/components/agent/always-allow"
import { GrantsEditor } from "@/components/agent/grants-editor"
import { AddKeyButton } from "@/components/agent/model-key-dialog"
import { EmptyValue } from "@/components/identity/empty-value"
import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { DocPage } from "@/components/identity/page-layout"
import { Pill, type PillTone } from "@/components/identity/pill"
import { SectionHead } from "@/components/identity/section-head"
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
  costWords,
  isConversation,
  policySentence,
  runCost,
  runFailed,
  runStartWords,
  runStats,
  runStatus,
  runTokens,
  tokenWords,
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
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { agoWords, toolName } from "@/lib/tools"
import { cn } from "@/lib/utils"
import { agentRoute } from "@/router"

const STATUS_TONE: Record<RunStatus, PillTone> = {
  running: "accent",
  ok: "ok",
  overbudget: "warn",
  error: "bad",
}

export function AgentPage() {
  const { id } = agentRoute.useParams()
  const [technical] = useTechnicalDetails()
  const agent = useQuery(
    recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, "agent", id)
  )
  const runs = useQuery(agentRunsQueryOptions(id))
  const policies = useQuery(writePoliciesQueryOptions())
  const providers = useQuery(llmProvidersQueryOptions())

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
  const tools = agentTools(record)
  const subagents = agentSubagents(record)
  const hidden = record.properties.hiddenFromChat === true

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

      <SectionHead
        title="Recent runs"
        hint={
          threads.length >= AGENT_RUNS_WINDOW
            ? `the last ${AGENT_RUNS_WINDOW}`
            : undefined
        }
      />
      {runs.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : runs.isError ? (
        <p className="text-[13px] text-destructive">
          Its runs didn’t load: {runs.error.message}
        </p>
      ) : threads.length === 0 ? (
        <p className="text-[13px] text-faint">It hasn’t run yet.</p>
      ) : (
        <>
          <Stats threads={threads} />
          <RunsTable threads={threads} />
        </>
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

function Stats({ threads }: { threads: SubstrateRecord[] }) {
  const stats = runStats(threads)
  const tiles: Array<[string, string, boolean?]> = [
    ["Runs", String(stats.runs)],
    ["Didn’t finish", String(stats.failed), stats.failed > 0],
    ["Tokens used", tokenWords(stats.tokens)],
    ["Cost", stats.priced ? costWords(stats.cost) : "Not priced"],
  ]
  return (
    <dl className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
      {tiles.map(([label, value, bad]) => (
        <div key={label} className="rounded-[10px] border px-3 py-2.5">
          <dt className="text-[12.5px] text-faint">{label}</dt>
          <dd
            className={cn(
              "mt-0.5 text-[22px] font-semibold tracking-[-0.02em]",
              bad && "text-destructive"
            )}
          >
            {value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function RunsTable({ threads }: { threads: SubstrateRecord[] }) {
  const grid =
    "grid grid-cols-[100px_minmax(0,1.2fr)_minmax(0,1.6fr)_70px_70px] items-center gap-2.5 px-3 max-md:grid-cols-[84px_minmax(0,1fr)_minmax(0,1fr)]"
  return (
    <div
      className="overflow-hidden rounded-[10px] border"
      role="table"
      aria-label="Recent runs"
    >
      <div role="row" className={cn(grid, "h-8 bg-panel text-xs text-faint")}>
        <span role="columnheader">When</span>
        <span role="columnheader">What started it</span>
        <span role="columnheader">What happened</span>
        <span role="columnheader" className="text-right max-md:hidden">
          Tokens
        </span>
        <span role="columnheader" className="text-right max-md:hidden">
          Cost
        </span>
      </div>
      {threads.map((thread) => {
        const status = runStatus(thread)
        const at =
          typeof thread.properties.startedAt === "string"
            ? thread.properties.startedAt
            : thread.createdAt
        const reason =
          typeof thread.properties.reason === "string"
            ? thread.properties.reason
            : undefined
        const tokens = runTokens(thread)
        const cost = runCost(thread)
        const start = runStartWords(thread)
        return (
          <div
            key={thread.id}
            role="row"
            className={cn(grid, "min-h-10 border-t py-2 text-[13px]")}
          >
            <span role="cell" className="text-muted-foreground" title={at}>
              {agoWords(at)}
            </span>
            <span role="cell" className="min-w-0 break-words">
              {isConversation(thread) ? (
                <Link
                  to="/agents"
                  search={{ thread: thread.id } as never}
                  className="underline-offset-2 hover:underline"
                >
                  {start}
                </Link>
              ) : (
                start
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
              {tokens !== undefined ? tokenWords(tokens) : <EmptyValue />}
            </span>
            <span
              role="cell"
              className="text-right text-muted-foreground tabular-nums max-md:hidden"
            >
              {cost !== undefined ? costWords(cost) : <EmptyValue />}
            </span>
          </div>
        )
      })}
    </div>
  )
}
