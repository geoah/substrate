/** The agent's declaration as the substrate reads it, for technical mode: the
 * system prompt the loop sends, each tool's function reference and alias, the
 * grants verbatim and the budgets. The agent row IS the prompt store
 * (`substrate.reamde.dev/core/agent`), so editing any of it is a record
 * write, one link away on the record page. */

import { ChevronRightIcon } from "lucide-react"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { agentSubagents, agentTools } from "@/lib/agent-chat"
import type { SubstrateRecord } from "@/lib/api/types"

function Row({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[11.5px] font-medium text-faint">{label}</span>
      <div className="font-mono text-[11px] [overflow-wrap:anywhere] text-muted-foreground">
        {children}
      </div>
    </div>
  )
}

export function AgentManifest({ agent }: { agent: SubstrateRecord }) {
  const prompt =
    typeof agent.properties.prompt === "string" ? agent.properties.prompt : ""
  const tools = agentTools(agent)
  const subagents = agentSubagents(agent)
  const permissions = agent.properties.permissions
  const budgets = agent.properties.budgets
  const params = agent.properties.params

  return (
    <div className="flex flex-col gap-2.5">
      <Collapsible className="group/prompt flex flex-col gap-1">
        <CollapsibleTrigger className="flex w-fit cursor-pointer items-center gap-1 text-[11.5px] font-medium text-faint hover:text-foreground">
          <ChevronRightIcon className="size-3 transition-transform group-data-open/prompt:rotate-90" />
          System prompt
        </CollapsibleTrigger>
        <CollapsibleContent>
          <p className="max-h-64 overflow-y-auto rounded-md border bg-background p-2 font-mono text-[11px] [overflow-wrap:anywhere] whitespace-pre-wrap text-muted-foreground">
            {prompt || "No prompt"}
          </p>
        </CollapsibleContent>
      </Collapsible>
      {tools.length > 0 && (
        <Row label="Tools">
          {tools.map((tool) => (
            <div key={tool.function}>
              {tool.name}: {tool.function}
            </div>
          ))}
        </Row>
      )}
      {subagents.length > 0 && (
        <Row label="Sub-agents">
          {subagents.map((id) => (
            <div key={id}>{id}</div>
          ))}
        </Row>
      )}
      {permissions !== undefined && (
        <Row label="Permissions">
          <pre className="whitespace-pre-wrap">
            {JSON.stringify(permissions, null, 2)}
          </pre>
        </Row>
      )}
      {budgets !== undefined && (
        <Row label="Budgets">
          <pre className="whitespace-pre-wrap">
            {JSON.stringify(budgets, null, 2)}
          </pre>
        </Row>
      )}
      {params !== undefined && (
        <Row label="Request knobs">
          <pre className="whitespace-pre-wrap">
            {JSON.stringify(params, null, 2)}
          </pre>
        </Row>
      )}
    </div>
  )
}
