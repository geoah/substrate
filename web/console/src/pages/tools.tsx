/** Tools: everything that can act on your data besides you, as card grids —
 * the tools your agents use, the syncs your providers run, anything nobody
 * uses yet, and (in everyday mode) the ones built into substrate. */

import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { PlusIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import { AddSampleDialog } from "@/components/samples/add-sample-dialog"
import { OriginTag, StatusPill, ToolTile } from "@/components/tools/tool-marks"
import {
  agentName,
  useTools,
  type ToolsModel,
} from "@/components/tools/use-tools"
import { Button } from "@/components/ui/button"
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
  const [adding, setAdding] = useState(false)
  return (
    <TablePage className="pb-16">
      <PageHeader
        title="Tools"
        description="Everything that can act on your data besides you. Each one says exactly what it can see, what it can change and when it runs."
        actions={
          <Button variant="outline" onClick={() => setAdding(true)}>
            <PlusIcon />
            Add tools
          </Button>
        }
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
            <div className="mt-8 mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
              <h2
                id={`tools-${g.key}`}
                className="text-[15px] font-semibold tracking-[-0.01em]"
              >
                {g.title}
              </h2>
              <span className="text-[12.5px] text-faint">{g.hint}</span>
            </div>
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
      <AddSampleDialog
        plane="functions"
        open={adding}
        onOpenChange={setAdding}
      />
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
        {status && <StatusPill status={status} />}
      </div>
      <p className="line-clamp-3 text-[13px] text-muted-foreground">
        {toolDescription(tool)}
      </p>
      {technical && <IdText value={tool.ref} />}
      <div className="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-faint">
        <OriginTag origin={tool.origin} className="text-xs" />
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
