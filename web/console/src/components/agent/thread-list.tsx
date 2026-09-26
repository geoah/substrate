/** The chats column. Agents come first, one line each, so a long history
 * never buries them: "All agents", every agent you can talk to, and the ones
 * that only work for other agents listed under a "Runs on its own" caption.
 * Picking an agent narrows the chats below to that agent's. The chats follow:
 * a search over the loaded chats' titles, Today / Yesterday / Earlier, the
 * most recent few with "Show more". Technical mode adds each thread's stored
 * status and tally. */

import { useEffect, useMemo, useRef, useState } from "react"
import { Link } from "@tanstack/react-router"
import { MessagesSquareIcon, PlusIcon, SearchIcon } from "lucide-react"

import { AgentRef } from "@/components/agent/agent-ref"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  agentName,
  chatCapable,
  groupByDay,
  sinceWords,
  tallyWords,
  threadStartedAt,
  threadTally,
  type ChatRow,
} from "@/lib/agent-chat"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import type { SubstrateRecord } from "@/lib/api/types"
import { CHAT_PAGE, visibleChats } from "./visible-chats"

const AGENT_ROW =
  "flex h-[30px] w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-left text-[13px] text-muted-foreground no-underline hover:bg-hover hover:text-foreground aria-[current=true]:bg-selection aria-[current=true]:font-medium aria-[current=true]:text-foreground"
const CHAT_ROW =
  "flex w-full cursor-pointer flex-col gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-hover aria-[current=true]:bg-selection"
const HEADING = "px-2 pt-3 pb-1 text-[11.5px] font-medium text-faint"

function Count({ n }: { n: number }) {
  if (!n) return null
  return (
    <span className="ml-auto shrink-0 text-[11.5px] font-normal text-faint tabular-nums">
      {n}
    </span>
  )
}

export function ThreadList({
  rows,
  loading,
  error,
  agents,
  selected,
  agent,
  onAgent,
  onSelect,
  onNewChat,
  onAddAgents,
}: {
  rows: ChatRow[]
  loading: boolean
  error?: string
  agents: SubstrateRecord[]
  /** The thread being read; empty for a new chat. */
  selected: string
  /** The agent the chats are narrowed to; empty for every agent's. */
  agent: string
  /** Narrows the chats to one agent, or to every agent's with "". */
  onAgent: (agent: string) => void
  onSelect: (thread: string) => void
  /** Starts a new chat, with one agent or with the current one. */
  onNewChat: (agent?: string) => void
  /** Offers the shipped samples that bring agents. */
  onAddAgents?: () => void
}) {
  const [query, setQuery] = useState("")
  const [limit, setLimit] = useState(CHAT_PAGE)
  const [technical] = useTechnicalDetails()
  // The agents list scrolls on its own; the picked agent is kept in view.
  const pickedRow = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    pickedRow.current?.scrollIntoView?.({ block: "nearest" })
  }, [agent])
  // A new narrowing or search starts from the most recent page again.
  const [scope, setScope] = useState({ agent, query })
  if (scope.agent !== agent || scope.query !== query) {
    setScope({ agent, query })
    setLimit(CHAT_PAGE)
  }

  const { chats, matched, more } = useMemo(
    () => visibleChats(rows, { agent, query, limit }),
    [rows, agent, query, limit]
  )
  const groups = groupByDay(chats, (r) => threadStartedAt(r.thread))
  const counts = new Map<string, number>()
  for (const r of rows) {
    if (r.agentId) counts.set(r.agentId, (counts.get(r.agentId) ?? 0) + 1)
  }
  const talkable = agents.filter(chatCapable)
  const background = agents.filter((a) => !chatCapable(a))
  const picked = agent ? agentName(agent) : ""

  return (
    <div className="flex h-full min-h-0 flex-col bg-panel">
      <nav
        aria-label="Agents"
        className="max-h-[45%] shrink-0 overflow-y-auto border-b px-2 pt-1 pb-2"
      >
        <div className="flex items-center justify-between gap-2">
          <div className={HEADING}>Agents</div>
          {onAddAgents && (
            <button
              type="button"
              onClick={onAddAgents}
              className="mt-2 inline-flex h-6 cursor-pointer items-center gap-1 rounded-md px-1.5 text-[12px] text-muted-foreground hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <PlusIcon className="size-3.5" />
              Add agents
            </button>
          )}
        </div>
        {loading && agents.length === 0 ? (
          <div className="flex flex-col gap-1.5 px-2 py-1">
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-5" />
            ))}
          </div>
        ) : (
          <>
            <button
              type="button"
              aria-current={!agent}
              onClick={() => onAgent("")}
              className={AGENT_ROW}
            >
              <MessagesSquareIcon
                className="size-4 shrink-0 text-faint"
                strokeWidth={1.8}
              />
              <span className="min-w-0 flex-1 truncate">All agents</span>
              <Count n={rows.length} />
            </button>
            {talkable.map((a) => (
              <button
                key={a.id}
                ref={a.id === agent ? pickedRow : undefined}
                type="button"
                aria-current={a.id === agent}
                onClick={() => onAgent(a.id)}
                className={AGENT_ROW}
              >
                <span className="flex min-w-0 flex-1">
                  <AgentRef id={a.id} agent={a} className="text-inherit" />
                </span>
                <Count n={counts.get(a.id) ?? 0} />
              </button>
            ))}
            {background.length > 0 && (
              <>
                <div className={HEADING}>Runs on its own</div>
                {background.map((a) => (
                  <Link
                    key={a.id}
                    to="/data/$authority/$pkg/$name/$id"
                    params={{
                      authority: CORE_AUTHORITY,
                      pkg: CORE_PACKAGE_NAME,
                      name: "agent",
                      id: a.id,
                    }}
                    aria-label={`${agentName(a.id)}, works for other agents: open its record`}
                    className={AGENT_ROW}
                  >
                    <span className="flex min-w-0 flex-1">
                      <AgentRef id={a.id} agent={a} className="text-inherit" />
                    </span>
                  </Link>
                ))}
              </>
            )}
          </>
        )}
      </nav>

      <div className="flex shrink-0 flex-col gap-2 px-3 pt-3 pb-2">
        <div className="flex items-center gap-2">
          <h2 className="min-w-0 flex-1 truncate text-[13px] font-semibold">
            {picked ? `Chats with ${picked}` : "Chats"}
          </h2>
          <Button
            size="icon-xs"
            variant="outline"
            onClick={() => onNewChat(agent || undefined)}
            aria-label="New chat"
            title={picked ? `Start a chat with ${picked}` : "Start a new chat"}
          >
            <PlusIcon />
          </Button>
        </div>
        <label className="flex h-8 items-center gap-2 rounded-md border bg-background px-2.5 text-[13px] text-muted-foreground focus-within:ring-2 focus-within:ring-ring/40">
          <SearchIcon className="size-3.5 shrink-0 text-faint" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={
              picked ? `Search chats with ${picked}` : "Search chats"
            }
            aria-label="Search chats"
            className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-faint"
          />
        </label>
        {agent && (
          <button
            type="button"
            onClick={() => onAgent("")}
            className="cursor-pointer self-start text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          >
            Show every agent’s chats
          </button>
        )}
      </div>
      <nav
        aria-label="Chats"
        className="min-h-0 flex-1 overflow-y-auto px-2 pb-3"
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
        ) : matched === 0 ? (
          <p className="px-2 py-3 text-[13px] text-muted-foreground">
            {query.trim()
              ? `No chat matches “${query.trim()}”.`
              : picked
                ? `No chats with ${picked} yet.`
                : "No chats yet. Pick an agent above to start one."}
          </p>
        ) : (
          <>
            {groups.map((group) => (
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
                    className={CHAT_ROW}
                  >
                    <span className="truncate text-[13px] font-medium">
                      {row.title}
                    </span>
                    <span className="truncate text-xs text-faint">
                      {agent
                        ? sinceWords(threadStartedAt(row.thread))
                        : `${row.agentId ? agentName(row.agentId) : "An agent"} · ${sinceWords(threadStartedAt(row.thread))}`}
                    </span>
                    {technical && (
                      <span className="truncate text-[11.5px] text-faint">
                        {tallyWords(threadTally(row.thread)).join(" · ") ||
                          "No tally recorded"}
                      </span>
                    )}
                  </button>
                ))}
              </div>
            ))}
            {more > 0 && (
              <Button
                size="xs"
                variant="ghost"
                className="mt-2 w-full text-muted-foreground"
                onClick={() => setLimit((n) => n + CHAT_PAGE)}
              >
                Show more ({more} older)
              </Button>
            )}
          </>
        )}
      </nav>
    </div>
  )
}
