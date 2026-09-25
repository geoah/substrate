/** The chats column: a New chat button, a search over the loaded chats'
 * titles, every agent's conversations under Today / Yesterday / Earlier, and
 * the agents themselves — the ones you can talk to start a chat, the ones
 * that only work for other agents say so. */

import { useMemo, useState } from "react"
import { Link } from "@tanstack/react-router"
import { PlusIcon, SearchIcon } from "lucide-react"

import { AgentMark } from "@/components/agent/agent-mark"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  agentName,
  chatCapable,
  groupByDay,
  threadStartedAt,
  type ChatRow,
} from "@/lib/agent-chat"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import type { SubstrateRecord } from "@/lib/api/types"
import { relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"

const ROW =
  "flex w-full cursor-pointer flex-col gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-hover aria-[current=true]:bg-selection"

export function ThreadList({
  rows,
  loading,
  error,
  agents,
  selected,
  onSelect,
  onNewChat,
}: {
  rows: ChatRow[]
  loading: boolean
  error?: string
  agents: SubstrateRecord[]
  /** The thread being read; empty for a new chat. */
  selected: string
  onSelect: (thread: string) => void
  /** Starts a new chat, with one agent or with the current one. */
  onNewChat: (agent?: string) => void
}) {
  const [query, setQuery] = useState("")
  const needle = query.trim().toLowerCase()
  const shown = useMemo(
    () =>
      needle
        ? rows.filter((r) => r.title.toLowerCase().includes(needle))
        : rows,
    [rows, needle]
  )
  const groups = groupByDay(shown, (r) => threadStartedAt(r.thread))
  const counts = new Map<string, number>()
  for (const r of rows) {
    if (r.agentId) counts.set(r.agentId, (counts.get(r.agentId) ?? 0) + 1)
  }
  const talkable = agents.filter(chatCapable)
  const background = agents.filter((a) => !chatCapable(a))

  return (
    <div className="flex h-full min-h-0 flex-col bg-panel">
      <div className="flex shrink-0 flex-col gap-2 border-b p-3">
        <Button
          size="sm"
          className="w-full justify-start"
          onClick={() => onNewChat()}
        >
          <PlusIcon />
          New chat
        </Button>
        <label className="flex h-8 items-center gap-2 rounded-md border bg-background px-2.5 text-[13px] text-muted-foreground focus-within:ring-2 focus-within:ring-ring/40">
          <SearchIcon className="size-3.5 shrink-0 text-faint" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search chats"
            aria-label="Search chats"
            className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-faint"
          />
        </label>
      </div>
      <nav
        aria-label="Chats"
        className="min-h-0 flex-1 overflow-y-auto px-2 pt-1 pb-3"
      >
        {loading ? (
          <div className="flex flex-col gap-2 p-2">
            {Array.from({ length: 5 }, (_, i) => (
              <Skeleton key={i} className="h-9" />
            ))}
          </div>
        ) : error ? (
          <p className="px-2 py-3 text-[13px] text-muted-foreground">
            Your chats didn’t load: {error}
          </p>
        ) : rows.length === 0 ? (
          <p className="px-2 py-3 text-[13px] text-muted-foreground">
            No chats yet. Start one with an agent below.
          </p>
        ) : shown.length === 0 ? (
          <p className="px-2 py-3 text-[13px] text-muted-foreground">
            No chat matches “{query.trim()}”.
          </p>
        ) : (
          groups.map((group) => (
            <div key={group.day}>
              <div className="px-1.5 pt-2.5 pb-1 text-[11.5px] font-medium text-faint">
                {group.day}
              </div>
              {group.items.map((row) => (
                <button
                  key={row.thread.id}
                  type="button"
                  aria-current={row.thread.id === selected}
                  onClick={() => onSelect(row.thread.id)}
                  className={ROW}
                >
                  <span className="truncate text-[13px] font-medium">
                    {row.title}
                  </span>
                  <span className="truncate text-xs text-faint">
                    {row.agentId ? agentName(row.agentId) : "An agent"} ·{" "}
                    {relativeTime(threadStartedAt(row.thread))}
                  </span>
                </button>
              ))}
            </div>
          ))
        )}

        {(talkable.length > 0 || background.length > 0) && (
          <div className="px-1.5 pt-4 pb-1 text-[11.5px] font-medium text-faint">
            Agents
          </div>
        )}
        {talkable.map((agent) => {
          const chats = counts.get(agent.id) ?? 0
          return (
            <button
              key={agent.id}
              type="button"
              onClick={() => onNewChat(agent.id)}
              title={`Start a chat with ${agentName(agent.id)}`}
              className={ROW}
            >
              <span className="flex min-w-0 items-center gap-1.5 text-[13px] font-medium">
                <AgentMark id={agent.id} size="xs" />
                <span className="truncate">{agentName(agent.id)}</span>
              </span>
              <span className="truncate pl-[22px] text-xs text-faint">
                {chats === 0
                  ? "No chats yet"
                  : chats === 1
                    ? "1 chat"
                    : `${chats} chats`}
              </span>
            </button>
          )
        })}
        {background.map((agent) => (
          <Link
            key={agent.id}
            to="/data/$authority/$pkg/$name/$id"
            params={{
              authority: CORE_AUTHORITY,
              pkg: CORE_PACKAGE_NAME,
              name: "agent",
              id: agent.id,
            }}
            className={cn(ROW, "no-underline")}
          >
            <span className="flex min-w-0 items-center gap-1.5 text-[13px] font-medium">
              <AgentMark id={agent.id} size="xs" />
              <span className="truncate">{agentName(agent.id)}</span>
            </span>
            <span className="truncate pl-[22px] text-xs text-faint">
              Runs on its own
            </span>
          </Link>
        ))}
      </nav>
    </div>
  )
}
