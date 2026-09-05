/** The agent's declaration, on the chat page: the system prompt the loop sends
 * and the tools the model may call, read off the agent record itself
 * (`substrate.reamde.dev/core/agent`; the row IS the prompt store). Collapsed
 * by default — the conversation is the page's subject — and editing stays on
 * the record page, one link away.
 *
 * A tool entry names a function (`{function, name?, description?}`): the
 * model-facing name is the alias where one is declared, else the function's
 * own local name — the last segment of the reference it carries. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon, ChevronRightIcon, BotIcon } from "lucide-react"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Spinner } from "@/components/ui/spinner"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"

interface ToolEntry {
  /** The function reference, verbatim off the row. */
  function: string
  /** The model-facing tool name: the alias, else the function's local name. */
  name: string
  description?: string
}

function lastSegment(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1)
}

function toolsOf(record: SubstrateRecord): ToolEntry[] {
  const raw = record.properties.tools
  if (!Array.isArray(raw)) return []
  const out: ToolEntry[] = []
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue
    const entry = item as Record<string, unknown>
    const fn = typeof entry.function === "string" ? entry.function : ""
    if (!fn) continue
    const alias = typeof entry.name === "string" ? entry.name : ""
    out.push({
      function: fn,
      name: alias || lastSegment(fn),
      description:
        typeof entry.description === "string" ? entry.description : undefined,
    })
  }
  return out
}

/** A permissions grant names kind records
 * (`substrate.reamde.dev/core/kind/<kind-ref>`); the kind reference after
 * `/kind/` is the readable part. */
function grantKinds(raw: unknown): string[] {
  if (!Array.isArray(raw)) return []
  return raw
    .filter((v): v is string => typeof v === "string" && v !== "")
    .map((path) => {
      const marker = "/kind/"
      const at = path.indexOf(marker)
      return at >= 0 ? path.slice(at + marker.length) : path
    })
}

export function AgentManifest({ id }: { id: string }) {
  const agent = useQuery(
    recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, "agent", id)
  )

  if (agent.isPending) {
    return (
      <div className="flex items-center gap-1.5 rounded-md border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
        <Spinner className="size-3" />
        loading the agent
      </div>
    )
  }
  // The declaration may be unreadable (a permissions gap, a gone row); the
  // chat may still work, so the panel says so and stays out of the way.
  if (agent.isError || !agent.data) {
    return (
      <p className="text-xs text-muted-foreground">
        The agent record didn't load
        {agent.error ? `: ${agent.error.message}` : ""}.
      </p>
    )
  }

  const record = agent.data
  const prompt =
    typeof record.properties.prompt === "string" ? record.properties.prompt : ""
  const tools = toolsOf(record)
  const permissions =
    typeof record.properties.permissions === "object" &&
    record.properties.permissions !== null
      ? (record.properties.permissions as Record<string, unknown>)
      : {}
  const reads =
    typeof permissions.reads === "object" && permissions.reads !== null
      ? grantKinds((permissions.reads as Record<string, unknown>).kinds)
      : []
  const writes = grantKinds(permissions.writes)

  return (
    <Collapsible className="group/manifest rounded-md border bg-muted/40">
      <CollapsibleTrigger className="flex w-full cursor-pointer items-center gap-1.5 px-3 py-2 text-left text-xs hover:bg-muted/60">
        <BotIcon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="font-medium">Prompt and tools</span>
        <span className="data text-muted-foreground">
          {tools.length} {tools.length === 1 ? "tool" : "tools"}
        </span>
        <ChevronRightIcon className="ml-auto size-3 shrink-0 text-muted-foreground transition-transform group-data-open/manifest:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="flex flex-col gap-3 border-t px-3 py-2.5">
          <div className="flex flex-col gap-1">
            <span className="text-[0.65rem] tracking-wide text-muted-foreground uppercase">
              system prompt
            </span>
            {prompt ? (
              <p className="text-xs [overflow-wrap:anywhere] whitespace-pre-wrap">
                {prompt}
              </p>
            ) : (
              <span className="text-xs text-muted-foreground">no prompt</span>
            )}
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[0.65rem] tracking-wide text-muted-foreground uppercase">
              tools
            </span>
            {tools.length ? (
              <ul className="flex flex-col gap-0.5">
                {tools.map((tool) => (
                  <li
                    key={tool.function}
                    className="text-xs [overflow-wrap:anywhere]"
                  >
                    <span className="data" title={tool.function}>
                      {tool.name}
                    </span>
                    {tool.description && (
                      <span className="text-muted-foreground">
                        : {tool.description}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            ) : (
              <span className="text-xs text-muted-foreground">no tools</span>
            )}
          </div>
          {(reads.length > 0 || writes.length > 0) && (
            <div className="flex flex-col gap-1">
              <span className="text-[0.65rem] tracking-wide text-muted-foreground uppercase">
                permissions
              </span>
              {reads.length > 0 && (
                <p className="data text-xs [overflow-wrap:anywhere] text-muted-foreground">
                  reads {reads.join(", ")}
                </p>
              )}
              {writes.length > 0 && (
                <p className="data text-xs [overflow-wrap:anywhere] text-muted-foreground">
                  writes {writes.join(", ")}
                </p>
              )}
            </div>
          )}
          <Link
            to="/data/$authority/$pkg/$name/$id"
            params={{
              authority: CORE_AUTHORITY,
              pkg: CORE_PACKAGE_NAME,
              name: "agent",
              id,
            }}
            className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
          >
            The full record
            <ArrowUpRightIcon className="size-3" />
          </Link>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
