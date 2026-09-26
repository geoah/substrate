/** Home's recent chats: the last few conversations with your agents, each
 * titled by what opened it, with the agent and when. A row opens the chat. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronRightIcon } from "lucide-react"

import { ActorMark } from "@/components/identity/actor-ref"
import { Skeleton } from "@/components/ui/skeleton"
import { actorIdentity } from "@/lib/actor-identity"
import {
  agentActor,
  chatRows,
  openingMessages,
  threadStartedAt,
} from "@/lib/agent-chat"
import {
  conversationsQueryOptions,
  openingMessagesQueryOptions,
} from "@/lib/api/agents"
import { relativeTime } from "@/lib/format"

const SHOWN_CHATS = 3

export function RecentChats() {
  const conversations = useQuery(conversationsQueryOptions(SHOWN_CHATS))
  const threads = useMemo(
    () => conversations.data?.records ?? [],
    [conversations.data]
  )
  const openings = useQuery(
    openingMessagesQueryOptions(threads.map((t) => t.id))
  )
  const rows = useMemo(
    () => chatRows(threads, openingMessages(openings.data?.records ?? [])),
    [threads, openings.data]
  )

  if (conversations.isPending) {
    return (
      <div className="flex flex-col gap-2">
        {Array.from({ length: SHOWN_CHATS }, (_, i) => (
          <Skeleton key={i} className="h-[52px] w-full rounded-[10px]" />
        ))}
      </div>
    )
  }
  if (conversations.isError) {
    return (
      <p className="text-muted-foreground">
        Your chats didn’t load: {conversations.error.message}{" "}
        <button
          type="button"
          className="cursor-pointer underline"
          onClick={() => void conversations.refetch()}
        >
          Try again
        </button>
      </p>
    )
  }
  if (!rows.length) {
    return (
      <p className="text-muted-foreground">
        No chats yet. <Link to="/agents">Talk to an agent</Link> about your
        data.
      </p>
    )
  }
  return (
    <ul className="overflow-hidden rounded-[10px] border border-border">
      {rows.map((row) => {
        const agent = row.agentId
          ? actorIdentity(agentActor(row.agentId))
          : undefined
        const at = threadStartedAt(row.thread)
        return (
          <li
            key={row.thread.id}
            className="border-b border-border last:border-b-0"
          >
            <Link
              to="/agents"
              search={{ thread: row.thread.id } as never}
              className="flex items-center gap-3 px-3.5 py-2.5 text-foreground no-underline transition-colors hover:bg-panel"
            >
              {agent && <ActorMark identity={agent} size="md" />}
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{row.title}</span>
                <span className="block truncate text-[12.5px] text-faint">
                  {agent ? `with ${agent.name} · ` : ""}
                  <span title={at}>{relativeTime(at)}</span>
                </span>
              </span>
              <ChevronRightIcon
                aria-hidden
                className="size-4 shrink-0 text-faint"
              />
            </Link>
          </li>
        )
      })}
    </ul>
  )
}
