/** About this agent: who it is, what it runs on, what it can see and change
 * (edited here, by collection, as on its page), which tools it reaches for,
 * and the way to edit it. Everything is read off the agent record, the llm
 * provider rows and model records it runs on (lib/llm-models.ts) and the
 * write policies that name it; technical mode adds the references and the
 * declaration itself. */

import { Link } from "@tanstack/react-router"
import { BotIcon, GaugeIcon, PencilIcon, WrenchIcon } from "lucide-react"

import { AgentManifest } from "@/components/agent/agent-manifest"
import { AllowRules } from "@/components/agent/always-allow"
import { GrantsEditor } from "@/components/agent/grants-editor"
import { AddKeyButton, RunsOnLine } from "@/components/agent/model-key-link"
import { AgentMark } from "@/components/agent/agent-mark"
import { IdText } from "@/components/identity/id-text"
import { Button } from "@/components/ui/button"
import { useAgentRuns } from "@/hooks/use-agent-runs"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  agentName,
  agentSubagents,
  agentTools,
  toolRoute,
} from "@/lib/agent-chat"
import { standingAllows } from "@/lib/agent-rules"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME, LLM_PACKAGE } from "@/lib/api/http"
import type { SubstrateRecord } from "@/lib/api/types"
import { LLM_MODEL_KIND } from "@/lib/core-kinds"
import { rowName } from "@/lib/llm-models"
import { toolName } from "@/lib/tools"

function Section({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <section className="flex flex-col gap-1.5 text-[13px]">
      <h3 className="text-[11.5px] font-medium text-faint">{label}</h3>
      {children}
    </section>
  )
}

/** One policy as words: what it does to which writes. */
function policyWords(policy: SubstrateRecord): string {
  const action =
    typeof policy.properties.action === "string"
      ? policy.properties.action
      : "allow"
  const selector = policy.properties.selector as
    Record<string, unknown> | undefined
  const kinds = Array.isArray(selector?.kinds)
    ? (selector.kinds as unknown[]).join(", ")
    : ""
  const ops = Array.isArray(selector?.ops)
    ? (selector.ops as unknown[]).join("/")
    : ""
  return `${action}${ops ? ` ${ops}` : ""} ${kinds || "*"}`
}

export function AgentPanel({
  agent,
  policies,
}: {
  agent: SubstrateRecord
  /** The write policies that speak for this agent. */
  policies: SubstrateRecord[]
}) {
  const [technical] = useTechnicalDetails()
  const read = useAgentRuns([agent])
  const runs = read.of(agent)
  const nameOf = (rowId: string) => rowName(read.keys.rows, rowId)
  const description =
    typeof agent.properties.description === "string"
      ? agent.properties.description
      : ""
  const tools = agentTools(agent)
  const subagents = agentSubagents(agent)
  const allowed = standingAllows(policies, agent.id)

  return (
    <div className="flex flex-col gap-5 p-4">
      <section className="flex flex-col gap-2">
        <div className="flex items-center gap-2.5">
          <AgentMark id={agent.id} size="lg" />
          <div className="min-w-0">
            <div className="font-semibold">{agentName(agent.id)}</div>
            <div className="text-[12.5px] text-muted-foreground">
              <RunsOnLine
                providerId={runs.row}
                name={runs.row ? nameOf(runs.row) : undefined}
                model={runs.label ?? runs.model}
              />
            </div>
          </div>
        </div>
        {runs.canRun === false && (runs.keyRow || runs.runsOn) && (
          <div className="flex flex-col items-start gap-1.5">
            <p className="text-[12.5px] text-warning">
              {runs.runsOn
                ? "None of the models it runs on can run yet."
                : `No API key for ${nameOf(runs.keyRow ?? "")} yet.`}
            </p>
            {runs.keyRow && (
              <AddKeyButton
                providerId={runs.keyRow}
                name={nameOf(runs.keyRow)}
                size="xs"
              />
            )}
          </div>
        )}
        {description && (
          <p className="text-[13px] text-muted-foreground">{description}</p>
        )}
      </section>

      <GrantsEditor agent={agent} layout="stacked" />
      {allowed.length > 0 && (
        <Section label="Always allowed">
          <AllowRules agent={agent.id} rules={allowed} />
        </Section>
      )}
      {subagents.length > 0 && (
        <Section label="Can ask">
          <ul className="flex flex-col gap-1.5">
            {subagents.map((id) => (
              <li key={id} className="flex items-center gap-2">
                <AgentMark id={id} size="xs" />
                <span>{agentName(id)}</span>
              </li>
            ))}
          </ul>
        </Section>
      )}
      <Section label="Tools it can use">
        {tools.length === 0 ? (
          <p className="text-muted-foreground">
            None. It answers from what you tell it.
          </p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {tools.map((tool) => {
              const route = toolRoute(tool.function)
              return (
                <li key={tool.function} className="flex items-start gap-2">
                  <WrenchIcon className="mt-[3px] size-3.5 shrink-0 text-faint" />
                  <span className="min-w-0">
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
                    {technical && (
                      <span className="block font-mono text-[11.5px] [overflow-wrap:anywhere] text-faint">
                        {tool.function}
                      </span>
                    )}
                  </span>
                </li>
              )
            })}
          </ul>
        )}
      </Section>

      {technical && (
        <section className="flex flex-col gap-2.5 rounded-lg border bg-background p-3">
          <h3 className="flex items-center gap-1.5 text-[11.5px] font-medium text-faint">
            <BotIcon className="size-3.5" />
            Technical details
          </h3>
          <div className="flex flex-col gap-1 text-[12px]">
            <span className="text-faint">Agent</span>
            <IdText
              value={`${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/agent/${agent.id}`}
            />
            {runs.runsOn && (
              <>
                <span className="text-faint">Runs on</span>
                {runs.runsOn.entries.map((entry) => (
                  <IdText
                    key={entry.id}
                    value={`${LLM_MODEL_KIND}/${entry.id}`}
                  />
                ))}
              </>
            )}
            {runs.model && (
              <>
                <span className="text-faint">Model</span>
                <IdText value={runs.model} />
              </>
            )}
            {runs.row && (
              <>
                <span className="text-faint">Provider</span>
                <IdText value={`${LLM_PACKAGE}/provider/${runs.row}`} />
              </>
            )}
            <span className="text-faint">Write policies</span>
            {policies.length === 0 ? (
              <span className="text-muted-foreground">
                None: its writes land as it makes them.
              </span>
            ) : (
              policies.map((policy) => (
                <span
                  key={policy.id}
                  className="font-mono text-[11.5px] [overflow-wrap:anywhere] text-muted-foreground"
                >
                  {policy.id}: {policyWords(policy)}
                </span>
              ))
            )}
          </div>
          <AgentManifest agent={agent} />
        </section>
      )}

      <div className="flex flex-wrap gap-1.5">
        <Button
          size="sm"
          variant="outline"
          render={
            <Link to="/agents/$id" params={{ id: agent.id }}>
              <GaugeIcon />
              Runs and permissions
            </Link>
          }
        />
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
                id: agent.id,
              }}
            >
              <PencilIcon />
              Edit agent
            </Link>
          }
        />
      </div>
    </div>
  )
}
