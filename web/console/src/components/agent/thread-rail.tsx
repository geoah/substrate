/** The agent's sessions: every `llmthread` this agent ran, newest first, with
 * the one being read selected. A thread IS a run (there is no agent-run
 * record), so a row says what a run says — when it started, how it settled, and
 * what it burned. Picking one loads its transcript; **New thread** opens an
 * empty conversation without touching the old ones. */

import { useQuery } from "@tanstack/react-query"
import { MessageSquarePlusIcon } from "lucide-react"

import { StateBadge } from "@/components/state-badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { agentThreadsQueryOptions } from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"
import { cellValue, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"

/** When the run happened: `startedAt` is the loop's own stamp, and the record's
 * creation is the honest fallback for a row written before it settled. */
function startedAt(thread: SubstrateRecord): string {
  const declared = thread.properties.startedAt
  return typeof declared === "string" && declared ? declared : thread.createdAt
}

function ThreadRow({
  thread,
  selected,
  onSelect,
}: {
  thread: SubstrateRecord
  selected: boolean
  onSelect: () => void
}) {
  const status = cellValue(thread.properties.status)
  const turns = Number(thread.properties.turns ?? 0)
  const tokens = Number(thread.properties.totalTokens ?? 0)
  const at = startedAt(thread)
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full cursor-pointer flex-col gap-1 border-b px-3 py-2 text-left hover:bg-muted/50",
        selected && "bg-muted"
      )}
    >
      <div className="flex items-center gap-1.5">
        <span className="truncate text-xs" title={at}>
          {relativeTime(at)}
        </span>
        {status && <StateBadge value={status} />}
      </div>
      <span className="truncate data text-[0.7rem] text-muted-foreground">
        {turns} {turns === 1 ? "turn" : "turns"}
        {tokens > 0 && `, ${tokens.toLocaleString()} tokens`}
      </span>
    </button>
  )
}

export function ThreadRail({
  agent,
  selected,
  onSelect,
  onNew,
}: {
  agent: string
  /** The thread being read; empty means an unopened new conversation. */
  selected: string
  onSelect: (id: string) => void
  onNew: () => void
}) {
  const threads = useQuery(agentThreadsQueryOptions(agent))
  const rows = threads.data?.records ?? []

  return (
    <div className="flex w-60 shrink-0 flex-col border-r">
      <div className="flex shrink-0 items-center justify-between gap-2 border-b px-3 py-2">
        <span className="text-xs font-medium">Threads</span>
        <Button
          variant="outline"
          size="sm"
          className="h-6 gap-1 px-2 text-xs"
          onClick={onNew}
          disabled={!selected}
        >
          <MessageSquarePlusIcon className="size-3" />
          New
        </Button>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {threads.isPending ? (
          <div className="flex flex-col gap-2 p-3">
            {Array.from({ length: 5 }, (_, i) => (
              <Skeleton key={i} className="h-8" />
            ))}
          </div>
        ) : threads.isError ? (
          <p className="px-3 py-3 text-xs text-muted-foreground">
            The threads didn't load — {threads.error.message}
          </p>
        ) : rows.length === 0 ? (
          <p className="px-3 py-3 text-xs text-muted-foreground">
            No threads yet. Send a message to open one.
          </p>
        ) : (
          rows.map((thread) => (
            <ThreadRow
              key={thread.id}
              thread={thread}
              selected={thread.id === selected}
              onSelect={() => onSelect(thread.id)}
            />
          ))
        )}
      </ScrollArea>
    </div>
  )
}
