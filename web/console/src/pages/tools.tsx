/** Tools: everything that can act on your data besides you, as card grids —
 * the tools your agents use, the syncs your providers run, anything nobody
 * uses yet, and (in everyday mode) the ones built into substrate. */

import { Link } from "@tanstack/react-router"

import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import { OriginMark } from "@/components/identity/origin-mark"
import { Pill } from "@/components/identity/pill"
import { SectionHead } from "@/components/identity/section-head"
import { ToolTile } from "@/components/tools/tool-marks"
import {
  agentName,
  useTools,
  type ToolsModel,
} from "@/components/tools/use-tools"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { splitKind } from "@/lib/api/http"
import {
  cadenceSummary,
  groupTools,
  listWords,
  toolDescription,
  toolName,
  toolStatus,
  type Tool,
} from "@/lib/tools"

export function ToolsPage() {
  const model = useTools()
  const [technical] = useTechnicalDetails()
  const groups = groupTools(model.tools, technical)
  return (
    <TablePage className="pb-16">
      <PageHeader
        title="Tools"
        description="Everything that can act on your data besides you. Each one says exactly what it can see, what it can change and when it runs."
      />
      {model.isPending ? (
        <div className="mt-8 grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-36 rounded-[10px]" />
          ))}
        </div>
      ) : model.error ? (
        <p className="mt-8 text-sm text-destructive">
          Couldn’t load your tools. Check your connection and reload the page.
        </p>
      ) : !groups.length ? (
        <p className="mt-8 text-sm text-faint">
          Nothing can act on your data yet. Tools arrive with the providers and
          samples you add.
        </p>
      ) : (
        groups.map((g) => (
          <section key={g.key} aria-labelledby={`tools-${g.key}`}>
            <SectionHead id={`tools-${g.key}`} title={g.title} hint={g.hint} />
            <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3">
              {g.tools.map((t) => (
                <ToolCard
                  key={t.ref}
                  tool={t}
                  model={model}
                  technical={technical}
                />
              ))}
            </div>
          </section>
        ))
      )}
    </TablePage>
  )
}

function ToolCard({
  tool,
  model,
  technical,
}: {
  tool: Tool
  model: ToolsModel
  technical: boolean
}) {
  const { authority, pkg, name } = splitKind(tool.ref)
  const status = toolStatus(tool, model.runsOf(tool)[0], {
    waitingFor: model.waitingFor(tool),
    usage: model.usageOf(tool),
  })
  const users = [...new Set(tool.uses.map((u) => agentName(u.agent)))]
  return (
    <Link
      to="/tools/$authority/$pkg/$name"
      params={{ authority, pkg, name }}
      data-slot="tool-card"
      className="flex flex-col gap-2.5 rounded-[10px] border bg-background p-3.5 text-left text-foreground no-underline transition-colors outline-none hover:border-border-strong hover:bg-panel focus-visible:ring-2 focus-visible:ring-ring/50"
    >
      <div className="flex items-center gap-2.5">
        <ToolTile tool={tool.ref} />
        <span className="min-w-0 flex-1 font-semibold break-words">
          {toolName(tool)}
        </span>
        {status && <Pill tone={status.tone}>{status.label}</Pill>}
      </div>
      <p className="line-clamp-3 text-[13px] text-muted-foreground">
        {toolDescription(tool)}
      </p>
      {technical && <IdText value={tool.ref} />}
      <div className="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-faint">
        <OriginMark origin={tool.origin} className="text-muted-foreground" />
        <span aria-hidden>·</span>
        <span>
          {users.length
            ? `Used by ${listWords(users)}`
            : cadenceSummary(tool, model.label)}
        </span>
      </div>
    </Link>
  )
}
