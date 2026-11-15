/** The chats column. Agents come first, one line each, so a long history
 * never buries them: "All agents" and the primary agents you talk to. "Show
 * more agents" opens the rest: the supporting and internal agents you can
 * still talk to, tagged with their purpose, and the ones that only work for
 * other agents under a "Runs on its own" caption (`agentListing`).
 * Picking an agent narrows the chats below to that agent's. The chats follow:
 * a search over the loaded chats' titles, Today / Yesterday / Earlier, the
 * most recent few with "Show more". A chat the person has not read through
 * carries a dot and a heavier title; a chat with a question, a suggested
 * change or a call waiting says it waits on you. Each row has a menu of what
 * the person does with a chat (`useThreadMarks`): mark it unread or read,
 * pin it, archive it. Pinned chats sit in a section of their own above the
 * rest, however old. An archived chat leaves the list, unless it waits on a
 * person, and is found again under Archived, where it is unarchived.
 * Technical mode adds each thread's stored status and tally. */

import { useEffect, useMemo, useRef, useState } from "react"
import { Link } from "@tanstack/react-router"
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ArrowLeftIcon,
  EllipsisIcon,
  MailIcon,
  MailOpenIcon,
  MessagesSquareIcon,
  PinIcon,
  PinOffIcon,
  PlusIcon,
  SearchIcon,
} from "lucide-react"

import { AgentRef } from "@/components/agent/agent-ref"
import { Pill, type PillTone } from "@/components/identity/pill"
import { PurposeTag } from "@/components/identity/purpose-tag"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  agentListing,
  agentName,
  agentPurpose,
  groupByDay,
  sinceWords,
  tallyWords,
  threadActiveAt,
  threadTally,
  type ChatRow,
} from "@/lib/agent-chat"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import type { SubstrateRecord } from "@/lib/api/types"
import { unfinishedTone, type UnfinishedTurn } from "@/lib/agent-runs"
import { chatSections, threadWaiting } from "@/lib/thread-marks"
import { cn } from "@/lib/utils"
import { CHAT_PAGE, visibleChats } from "./visible-chats"

const AGENT_ROW =
  "flex h-[30px] w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-left text-[13px] text-muted-foreground no-underline hover:bg-hover hover:text-foreground aria-[current=true]:bg-selection aria-[current=true]:font-medium aria-[current=true]:text-foreground"
const CHAT_ROW =
  "flex w-full cursor-pointer flex-col gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-hover aria-[current=true]:bg-selection"
// Shown while the row is pointed at or holds the focus, while its menu is
// open, and always where nothing hovers (a touch screen).
const ROW_ACTION =
  "absolute top-1 right-1 inline-flex size-6 cursor-pointer items-center justify-center rounded-md border bg-background text-muted-foreground opacity-0 group-focus-within/chat:opacity-100 group-hover/chat:opacity-100 hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none aria-expanded:opacity-100 [@media(hover:none)]:opacity-100"
const DAY = "px-1.5 pt-2.5 pb-1 text-[11.5px] font-medium text-faint"

/** Which part of the chats the column shows. */
export type ChatsView = "chats" | "archived"
const HEADING = "px-2 pt-3 pb-1 text-[11.5px] font-medium text-faint"

const UNFINISHED_PILLS = {
  bad: { tone: "bad", words: "Failed" },
  warn: { tone: "warn", words: "Didn’t finish" },
  quiet: { tone: "neutral", words: "Stopped" },
} as const satisfies Record<
  UnfinishedTurn["tone"],
  { tone: PillTone; words: string }
>

/** The mark of a chat whose last turn didn't finish, so it is found from
 * the list; the chat itself says why. */
function UnfinishedPill({ thread }: { thread: SubstrateRecord }) {
  const tone = unfinishedTone(thread)
  if (!tone) return null
  const pill = UNFINISHED_PILLS[tone]
  return (
    <Pill tone={pill.tone} className="shrink-0">
      {pill.words}
    </Pill>
  )
}

/** No chat is pinned or archived where the page says nothing of either. */
const NONE: ReadonlySet<string> = new Set()

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
  unread,
  onMarkUnread,
  onMarkRead,
  pinned = NONE,
  archived = NONE,
  onPin,
  onArchive,
  view = "chats",
  onView,
  archivedLoading = false,
  onArchivedNewer,
  onArchivedOlder,
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
  /** The chats the person has not read through. */
  unread?: ReadonlySet<string>
  /** Marks a chat unread, or read; absent where the server keeps no marks,
   * and the rows then offer neither. */
  onMarkUnread?: (thread: string) => void
  onMarkRead?: (thread: string) => void
  /** The chats the person pinned, and the ones they archived. */
  pinned?: ReadonlySet<string>
  archived?: ReadonlySet<string>
  /** Pins or unpins a chat, archives or unarchives one; absent where the
   * server keeps neither, and the rows and the Archived view are not
   * offered. */
  onPin?: (thread: string, on: boolean) => void
  onArchive?: (thread: string, on: boolean) => void
  /** The chats, or the archived ones. */
  view?: ChatsView
  onView?: (view: ChatsView) => void
  /** The archived chats are still being read. */
  archivedLoading?: boolean
  /** The Archived view shows one page of the archived chats, most recently
   * active first: these turn it, and each is absent where there is no such
   * page. */
  onArchivedNewer?: () => void
  onArchivedOlder?: () => void
}) {
  const [query, setQuery] = useState("")
  const [limit, setLimit] = useState(CHAT_PAGE)
  const [technical] = useTechnicalDetails()
  const [showAll, setShowAll] = useState(false)
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

  // The Archived view is there only where a chat can be archived.
  const showArchived = view === "archived" && Boolean(onArchive)
  const sections = useMemo(
    () => chatSections(rows, { pinned, archived }),
    [rows, pinned, archived]
  )
  // What this view lists: the archived chats, or the pinned ones and the
  // rest. The agents' counts and the search are over the same rows.
  const shown = useMemo(
    () =>
      showArchived
        ? sections.archived
        : [...sections.pinned, ...sections.listed],
    [sections, showArchived]
  )
  // Every pinned chat that matches shows, however old; the page of recent
  // chats is cut from the rest.
  const pins = useMemo(
    () =>
      showArchived
        ? []
        : visibleChats(sections.pinned, {
            agent,
            query,
            limit: sections.pinned.length,
          }).chats,
    [sections, showArchived, agent, query]
  )
  const { chats, matched, more } = useMemo(
    () =>
      visibleChats(showArchived ? sections.archived : sections.listed, {
        agent,
        query,
        limit,
      }),
    [sections, showArchived, agent, query, limit]
  )
  const groups = groupByDay(chats, (r) => threadActiveAt(r.thread))
  const counts = new Map<string, number>()
  for (const r of shown) {
    if (r.agentId) counts.set(r.agentId, (counts.get(r.agentId) ?? 0) + 1)
  }
  const { listed, more: extra, background } = agentListing(agents, agent)
  const hidden = extra.length + background.length
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
              <Count n={shown.length} />
            </button>
            {[...listed, ...(showAll ? extra : [])].map((a) => (
              <button
                key={a.id}
                ref={a.id === agent ? pickedRow : undefined}
                type="button"
                aria-current={a.id === agent}
                onClick={() => onAgent(a.id)}
                className={AGENT_ROW}
              >
                <span className="flex min-w-0 flex-1 items-center gap-1.5">
                  <AgentRef id={a.id} agent={a} className="text-inherit" />
                  <PurposeTag purpose={agentPurpose(a)} />
                </span>
                <Count n={counts.get(a.id) ?? 0} />
              </button>
            ))}
            {showAll && background.length > 0 && (
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
            {hidden > 0 && (
              <button
                type="button"
                aria-expanded={showAll}
                onClick={() => setShowAll((v) => !v)}
                className="mt-0.5 w-full cursor-pointer rounded-md px-2 py-1 text-left text-xs text-faint hover:text-muted-foreground"
              >
                {showAll
                  ? "Show fewer agents"
                  : `Show ${hidden} more ${hidden === 1 ? "agent" : "agents"}`}
              </button>
            )}
          </>
        )}
      </nav>

      <div className="flex shrink-0 flex-col gap-2 px-3 pt-3 pb-2">
        <div className="flex items-center gap-2">
          {showArchived && (
            <Button
              size="icon-xs"
              variant="ghost"
              onClick={() => onView?.("chats")}
              aria-label="Back to chats"
              title="Back to chats"
            >
              <ArrowLeftIcon />
            </Button>
          )}
          <h2 className="min-w-0 flex-1 truncate text-[13px] font-semibold">
            {showArchived
              ? picked
                ? `Archived chats with ${picked}`
                : "Archived chats"
              : picked
                ? `Chats with ${picked}`
                : "Chats"}
          </h2>
          {!showArchived && onArchive && onView && (
            <Button
              size="xs"
              variant="ghost"
              className="text-muted-foreground"
              onClick={() => onView("archived")}
              title="The chats you archived"
            >
              <ArchiveIcon />
              Archived
            </Button>
          )}
          {!showArchived && (
            <Button
              size="icon-xs"
              variant="outline"
              onClick={() => onNewChat(agent || undefined)}
              aria-label="New chat"
              title={
                picked ? `Start a chat with ${picked}` : "Start a new chat"
              }
            >
              <PlusIcon />
            </Button>
          )}
        </div>
        {showArchived && (
          <p className="text-xs text-muted-foreground">
            Archived chats stay out of your list. One that waits on you shows in
            Chats until it is answered.
          </p>
        )}
        <label className="flex h-8 items-center gap-2 rounded-md border bg-background px-2.5 text-[13px] text-muted-foreground focus-within:ring-2 focus-within:ring-ring/40">
          <SearchIcon className="size-3.5 shrink-0 text-faint" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={
              showArchived
                ? "Search archived chats"
                : picked
                  ? `Search chats with ${picked}`
                  : "Search chats"
            }
            aria-label={showArchived ? "Search archived chats" : "Search chats"}
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
        aria-label={showArchived ? "Archived chats" : "Chats"}
        className="min-h-0 flex-1 overflow-y-auto px-2 pb-3"
      >
        {loading || (showArchived && archivedLoading) ? (
          <div className="flex flex-col gap-2 p-2">
            {Array.from({ length: 5 }, (_, i) => (
              <Skeleton key={i} className="h-9" />
            ))}
          </div>
        ) : error ? (
          <p className="px-2 py-3 text-[13px] text-muted-foreground">
            Your chats didn’t load: {error}
          </p>
        ) : matched === 0 && pins.length === 0 ? (
          <p className="px-2 py-3 text-[13px] text-muted-foreground">
            {query.trim()
              ? showArchived
                ? `No archived chat matches “${query.trim()}”.`
                : `No chat matches “${query.trim()}”.`
              : showArchived
                ? picked
                  ? `No archived chats with ${picked}.`
                  : "No archived chats."
                : picked
                  ? `No chats with ${picked} yet.`
                  : "No chats yet. Pick an agent above to start one."}
          </p>
        ) : (
          <>
            {pins.length > 0 && (
              <section aria-label="Pinned">
                <div className={DAY}>Pinned</div>
                {pins.map(chatRow)}
              </section>
            )}
            {groups.map((group) => (
              <div key={group.day}>
                <div className={DAY}>{group.day}</div>
                {group.items.map(chatRow)}
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
        {showArchived && (onArchivedNewer || onArchivedOlder) && (
          <div className="mt-2 flex gap-2">
            <Button
              size="xs"
              variant="ghost"
              className="flex-1 text-muted-foreground"
              disabled={!onArchivedNewer}
              onClick={onArchivedNewer}
            >
              Newer chats
            </Button>
            <Button
              size="xs"
              variant="ghost"
              className="flex-1 text-muted-foreground"
              disabled={!onArchivedOlder}
              onClick={onArchivedOlder}
            >
              Older chats
            </Button>
          </div>
        )}
      </nav>
    </div>
  )

  /** One chat's row, with the menu of what the person does with it. */
  function chatRow(row: ChatRow) {
    const id = row.thread.id
    const isUnread = unread?.has(id) ?? false
    const isPinned = pinned.has(id)
    const isArchived = archived.has(id)
    const mark = isUnread ? onMarkRead : onMarkUnread
    const since = sinceWords(threadActiveAt(row.thread))
    const meta = [
      !agent && (row.agentId ? agentName(row.agentId) : "An agent"),
      since,
      // In the list only because it waits on a person.
      isArchived && !showArchived && "Archived",
    ].filter(Boolean)
    return (
      <div key={id} className="group/chat relative">
        <button
          type="button"
          aria-current={id === selected}
          data-unread={isUnread || undefined}
          onClick={() => onSelect(id)}
          className={CHAT_ROW}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            {isUnread && (
              <>
                <span
                  aria-hidden
                  className="size-2 shrink-0 rounded-full bg-primary"
                />
                <span className="sr-only">{"Unread: "}</span>
              </>
            )}
            {isPinned && showArchived && (
              <>
                <PinIcon aria-hidden className="size-3 shrink-0 text-faint" />
                <span className="sr-only">{"Pinned: "}</span>
              </>
            )}
            <span
              className={cn(
                "truncate text-[13px]",
                isUnread ? "font-semibold" : "font-medium"
              )}
            >
              {row.title}
            </span>
            {row.thread.properties.status === "running" && (
              <Pill tone="accent" live className="shrink-0">
                Working
              </Pill>
            )}
            <UnfinishedPill thread={row.thread} />
            {threadWaiting(row.thread) > 0 && (
              <Pill tone="warn" className="shrink-0">
                Waiting on you
              </Pill>
            )}
          </span>
          {row.preview && (
            <span className="truncate text-xs text-muted-foreground">
              {row.preview}
            </span>
          )}
          <span className="truncate text-xs text-faint">
            {meta.join(" · ")}
          </span>
          {technical && (
            <span className="truncate text-[11.5px] text-faint">
              {tallyWords(threadTally(row.thread)).join(" · ") ||
                "No tally recorded"}
            </span>
          )}
        </button>
        {(mark || onPin || onArchive) && (
          <DropdownMenu>
            <DropdownMenuTrigger
              aria-label={`Actions: ${row.title}`}
              title="Actions"
              className={ROW_ACTION}
            >
              <EllipsisIcon className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              {onPin && (
                <DropdownMenuItem onClick={() => onPin(id, !isPinned)}>
                  {isPinned ? <PinOffIcon /> : <PinIcon />}
                  {isPinned ? "Unpin" : "Pin"}
                </DropdownMenuItem>
              )}
              {mark && (
                <DropdownMenuItem onClick={() => mark(id)}>
                  {isUnread ? <MailOpenIcon /> : <MailIcon />}
                  {isUnread ? "Mark as read" : "Mark as unread"}
                </DropdownMenuItem>
              )}
              {onArchive && (
                <DropdownMenuItem onClick={() => onArchive(id, !isArchived)}>
                  {isArchived ? <ArchiveRestoreIcon /> : <ArchiveIcon />}
                  {isArchived ? "Unarchive" : "Archive"}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    )
  }
}
