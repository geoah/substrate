import type { ChatRow } from "@/lib/agent-chat"

/** How many chats show before "Show more", and how many each press adds. */
export const CHAT_PAGE = 20

/** The chats the column shows: the agent's when one is picked, matching the
 * search, newest first, cut at `limit`. `more` is how many the cut hid. */
export function visibleChats(
  rows: ChatRow[],
  { agent, query, limit }: { agent: string; query: string; limit: number }
): { chats: ChatRow[]; matched: number; more: number } {
  const needle = query.trim().toLowerCase()
  const matched = rows.filter(
    (r) =>
      (!agent || r.agentId === agent) &&
      (!needle || r.title.toLowerCase().includes(needle))
  )
  return {
    chats: matched.slice(0, limit),
    matched: matched.length,
    more: Math.max(0, matched.length - limit),
  }
}
