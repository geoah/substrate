/** Agents (`/agents`): a chat app over your agents — every conversation on
 * the left, the one being read in the middle, what its agent is and may do on
 * the right. Suggested changes are cards inside the thread; there is no
 * separate place to go and approve them.
 *
 * THE ADDRESS. `?thread=<id>` opens one conversation (a deep link);
 * `?agent=<agent id>` opens a new chat with that agent; `?prompt=<text>`
 * prefills the composer of a new chat (other pages link here with a question),
 * and `?send=1` beside it sends that question as soon as the agent can take
 * it, once (Add a collection's Ask an agent hands its request over this way).
 * A bare `/agents` opens the most recent conversation, or a new chat when
 * there is none. The chats column can be narrowed to one agent; `?agent=`
 * narrows it too, and New chat starts with the agent it is narrowed to. `/agents/<agent id>` is the old per-agent address and
 * redirects to `?agent=` (its `?thread=` carried along).
 *
 * THE CHATS. The column is read in parts: every chat that waits on a person,
 * and three lists of threads the server narrows by the person's own marks
 * (decision 0322), the chats they have not archived, the ones they pinned
 * however old, and, while the Archived view is open, a page of the archived
 * ones. A server from before pin and archive serves the one list it always
 * did, and the page offers neither action.
 *
 * The llm/provider rows are not agents and are not listed; an agent's
 * provider shows on its panel, and a provider without a key says so where the
 * conversation would otherwise fail. */

import { useMemo, useState, useSyncExternalStore } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  ArchiveRestoreIcon,
  MessagesSquareIcon,
  PanelRightIcon,
  PlusIcon,
} from "lucide-react"
import { parseAsString, useQueryStates } from "nuqs"

import { AgentPanel } from "@/components/agent/agent-panel"
import { Conversation } from "@/components/agent/conversation"
import { ThreadList, type ChatsView } from "@/components/agent/thread-list"
import { AddSampleDialog } from "@/components/samples/add-sample-dialog"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { toast } from "@/components/ui/toast"
import { useAgentRuns } from "@/hooks/use-agent-runs"
import { useLiveRecords } from "@/hooks/use-live-records"
import { earliestRead, useThreadMarks } from "@/hooks/use-thread-marks"
import {
  agentPurpose,
  chatCapable,
  chatRows,
  openingMessages,
  threadAgentId,
  threadTitle,
} from "@/lib/agent-chat"
import {
  agentsQueryOptions,
  openingMessagesQueryOptions,
  policiesForAgent,
  THREAD_LISTS_KEY,
  writePoliciesQueryOptions,
} from "@/lib/api/agents"
import { discoveryQueryOptions, hasFeature } from "@/lib/api/discovery"
import { CORE_AUTHORITY, LLM_PACKAGE, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import {
  chatListQueryOptions,
  PIN_ARCHIVE_REVISION,
  THREAD_MARKS_REVISION,
} from "@/lib/api/thread-marks"
import type { SubstrateRecord } from "@/lib/api/types"

/** A media query as state, for the two column breakpoints. */
function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (notify) => {
      const mql = window.matchMedia(query)
      mql.addEventListener("change", notify)
      return () => mql.removeEventListener("change", notify)
    },
    () => window.matchMedia(query).matches,
    () => true
  )
}

/** How long an archive offers its Undo. */
export const UNDO_MS = 6000

/** The chats of every part read, each once, most recently active first. */
function mergeChats(...parts: (SubstrateRecord[] | undefined)[]) {
  const seen = new Map<string, SubstrateRecord>()
  for (const part of parts)
    for (const thread of part ?? [])
      if (!seen.has(thread.id)) seen.set(thread.id, thread)
  return [...seen.values()].sort((a, b) =>
    a.updatedAt < b.updatedAt ? 1 : a.updatedAt > b.updatedAt ? -1 : 0
  )
}

const SEARCH = {
  thread: parseAsString.withDefault(""),
  agent: parseAsString.withDefault(""),
  prompt: parseAsString.withDefault(""),
  send: parseAsString.withDefault(""),
  about: parseAsString.withDefault(""),
}

export function AgentsPage() {
  const [search, setSearch] = useQueryStates(SEARCH, { history: "push" })
  const wide = useMedia("(min-width: 1100px)")
  const roomy = useMedia("(min-width: 900px)")
  const [panelOpen, setPanelOpen] = useState(true)
  const [panelSheet, setPanelSheet] = useState(false)
  const [chatsSheet, setChatsSheet] = useState(false)
  const [adding, setAdding] = useState(false)
  // The draft outlives switching the new chat's agent, which remounts the
  // conversation; `?prompt=` seeds it once.
  const [draft, setDraft] = useState(search.prompt)
  // A new chat whose run minted a thread keeps its conversation mounted.
  const [adopted, setAdopted] = useState<{ from: string; to: string }>()
  // Which new chat this is. A conversation that adopted a thread holds it as
  // its own, so the next new chat, even with the same agent, is another one.
  const [chat, setChat] = useState(0)
  // The agent the chats column is narrowed to. It follows `?agent=` whenever
  // the address names one, and stays while a chat from the narrowed list is
  // read (which drops `?agent=`).
  const [narrowed, setNarrowed] = useState(search.agent)
  const [addressed, setAddressed] = useState(search.agent)
  if (addressed !== search.agent) {
    setAddressed(search.agent)
    if (search.agent) setNarrowed(search.agent)
  }

  // Which part of the chats the column shows, and which page of the
  // archived ones: facts of this window.
  const [view, setView] = useState<ChatsView>("chats")
  const [archivedPage, setArchivedPage] = useState(0)

  const agents = useQuery(agentsQueryOptions())
  // Whether the server keeps pins and archives, and narrows a list by them.
  // The list waits for the answer: read before it, an older server would
  // refuse the filter, and a newer one would show the archived chats.
  const discovery = useQuery(discoveryQueryOptions)
  const flags = hasFeature(discovery.data, "records", PIN_ARCHIVE_REVISION)
  const counts = hasFeature(discovery.data, "records", THREAD_MARKS_REVISION)
  const asked = !discovery.isPending
  const conversations = useQuery({
    ...chatListQueryOptions(flags ? "unarchived" : "all"),
    enabled: asked,
  })
  const pinnedChats = useQuery({
    ...chatListQueryOptions("pinned"),
    enabled: flags,
  })
  // What waits on a person is read on its own, so neither an archive nor
  // the list's window can leave it out.
  const waitingChats = useQuery({
    ...chatListQueryOptions("waiting"),
    enabled: counts,
  })
  const archivedChats = useQuery({
    ...chatListQueryOptions("archived", archivedPage),
    enabled: flags && view === "archived",
  })
  const policies = useQuery(writePoliciesQueryOptions())

  const allAgents = useMemo(() => agents.data?.records ?? [], [agents.data])
  const runs = useAgentRuns(allAgents)
  // Primary agents first: a new chat defaults to the first of them, and the
  // picker offers them before the helpers.
  const talkable = useMemo(
    () =>
      allAgents
        .filter(chatCapable)
        .sort(
          (a, b) =>
            Number(agentPurpose(a) !== "primary") -
            Number(agentPurpose(b) !== "primary")
        ),
    [allAgents]
  )
  // The chats to open and to start from: the ones in the list, never one
  // the person put away.
  const recent = useMemo(
    () => conversations.data?.records ?? [],
    [conversations.data]
  )
  const threads = useMemo(
    () =>
      mergeChats(
        recent,
        pinnedChats.data?.records,
        waitingChats.data?.records,
        view === "archived" ? archivedChats.data?.records : undefined
      ),
    [recent, pinnedChats.data, waitingChats.data, archivedChats.data, view]
  )
  // A listed chat stays marked Working until its thread row is read again,
  // and the open conversation re-reads only at its own run's settle: without
  // this watch, a chat still running behind another one settles unseen. It
  // resumes from the list's own head, so a chat that settles after the list
  // was read and before the stream opens is heard too.
  //
  // Where a chat can be archived the watch stays open: a run the person put
  // away is in none of the lists read, and when it comes to wait on them it
  // has to come back.
  const anyRunning = threads.some((t) => t.properties.status === "running")
  useLiveRecords(
    anyRunning || flags ? [`${LLM_PACKAGE}/thread`] : [],
    [[...THREAD_LISTS_KEY]],
    undefined,
    conversations.data
  )
  const threadIds = useMemo(() => threads.map((t) => t.id), [threads])
  const openings = useQuery(openingMessagesQueryOptions(threadIds))
  const openingByThread = useMemo(
    () => openingMessages(openings.data?.records ?? []),
    [openings.data]
  )
  const rows = useMemo(
    () => chatRows(threads, openingByThread),
    [threads, openingByThread]
  )

  // Which conversation: the addressed thread; else a new chat when one was
  // asked for; else the most recent.
  const newChat = Boolean(search.agent || search.prompt)
  const threadId = search.thread || (newChat ? "" : (recent[0]?.id ?? ""))
  const listed = threads.find((t) => t.id === threadId)
  // A deep link past the loaded window reads its thread on its own.
  const single = useQuery({
    ...recordQueryOptions(CORE_AUTHORITY, LLM_PACKAGE_NAME, "thread", threadId),
    enabled: Boolean(threadId) && !listed && !conversations.isPending,
  })
  const threadRecord = listed ?? single.data
  // How far the person has read each chat: the dots in the list, and the
  // write that follows reading the one that is open.
  const marks = useThreadMarks(
    threads,
    threadRecord,
    earliestRead(
      conversations.data,
      pinnedChats.data,
      waitingChats.data,
      view === "archived" ? archivedChats.data : undefined
    )
  )
  const threadAgent = threadRecord ? threadAgentId(threadRecord) : undefined
  // A new chat's agent: the one asked for, else whoever you talked to last
  // (when you still can), else the first you can talk to.
  const lastAgent = recent[0] ? threadAgentId(recent[0]) : undefined
  const defaultAgent = talkable.some((a) => a.id === lastAgent)
    ? lastAgent
    : talkable[0]?.id
  const agentId = threadId ? threadAgent : search.agent || defaultAgent
  const agent = allAgents.find((a) => a.id === agentId)

  // An agent on `runsOn` continues a thread on the row and model the thread
  // is stamped with, and starts a new one on the first entry that can run.
  const nextTurn = agent ? runs.next(agent, threadRecord, threadId) : undefined
  const agentPolicies =
    agent && policies.data
      ? policiesForAgent(agent.id, policies.data.records)
      : []

  const title = threadId
    ? (rows.find((r) => r.thread.id === threadId)?.title ??
      threadTitle(threadRecord, undefined))
    : "New chat"

  const newKey = `new:${chat}:${agentId ?? ""}`
  // Back at a new chat whose conversation already adopted a thread (New
  // chat, or the browser's back button): that one is spent.
  if (!threadId && adopted?.from === newKey) {
    setChat(chat + 1)
    setAdopted(undefined)
  }
  const conversationKey = threadId
    ? adopted?.to === threadId
      ? adopted.from
      : threadId
    : newKey

  function openThread(id: string) {
    setChatsSheet(false)
    setDraft("")
    void setSearch({ thread: id, agent: null, prompt: null, send: null })
  }

  function startChat(agent?: string) {
    setChatsSheet(false)
    setDraft("")
    void setSearch({
      thread: null,
      agent: agent ?? (narrowed || agentId) ?? talkable[0]?.id ?? null,
      prompt: null,
    })
  }

  /** Narrows the chats to one agent. Unless the conversation open is already
   * with it, a new chat with it opens, so the middle matches the list. */
  function pickAgent(next: string) {
    setNarrowed(next)
    if (next && next !== agentId) startChat(next)
  }

  function togglePanel() {
    if (wide) setPanelOpen((v) => !v)
    else setPanelSheet(true)
  }

  const titleOf = (thread: string) =>
    rows.find((r) => r.thread.id === thread)?.title ?? "This chat"

  /** Pins or unpins a chat. A write that failed is said: the person asked
   * for it, and the row did not move. */
  async function pin(thread: string, on: boolean) {
    if (await marks.setPinned(thread, on)) return
    toast.add({
      type: "error",
      title: on
        ? "The chat couldn’t be pinned"
        : "The chat couldn’t be unpinned",
      description: titleOf(thread),
    })
  }

  /** Archives a chat at once, and offers to take it back for a few seconds:
   * no question is asked first, since nothing is lost and one click undoes
   * it. Unarchiving asks nothing either. */
  async function archive(thread: string, on: boolean) {
    const name = titleOf(thread)
    if (!(await marks.setArchived(thread, on))) {
      toast.add({
        type: "error",
        title: on
          ? "The chat couldn’t be archived"
          : "The chat couldn’t be unarchived",
        description: name,
      })
      return
    }
    if (!on) return
    const id = toast.add({
      title: "Chat archived",
      description: name,
      timeout: UNDO_MS,
      actionProps: {
        children: "Undo",
        onClick: () => {
          toast.close(id)
          void archive(thread, false)
        },
      },
    })
  }

  const list = (
    <ThreadList
      rows={rows}
      loading={conversations.isPending || agents.isPending}
      error={conversations.error?.message ?? agents.error?.message}
      agents={allAgents}
      selected={threadId}
      agent={narrowed}
      onAgent={pickAgent}
      onSelect={openThread}
      onNewChat={startChat}
      onAddAgents={() => {
        setChatsSheet(false)
        setAdding(true)
      }}
      unread={marks.unread}
      onMarkUnread={marks.supported ? marks.markUnread : undefined}
      onMarkRead={marks.supported ? marks.markRead : undefined}
      pinned={marks.pinned}
      archived={marks.archived}
      // By the server's revision, not by a read of marks: with every chat
      // archived the list is empty, no mark is read, and the Archived view
      // is the one way back to them.
      onPin={flags ? pin : undefined}
      onArchive={flags ? archive : undefined}
      view={view}
      onView={(next) => {
        setView(next)
        setArchivedPage(0)
      }}
      archivedLoading={archivedChats.isPending}
      onArchivedNewer={
        archivedPage > 0 ? () => setArchivedPage((n) => n - 1) : undefined
      }
      onArchivedOlder={
        archivedChats.data?.cursor && !archivedChats.isPlaceholderData
          ? () => setArchivedPage((n) => n + 1)
          : undefined
      }
    />
  )
  const panel = agent ? (
    <AgentPanel agent={agent} policies={agentPolicies} />
  ) : (
    <p className="p-4 text-[13px] text-muted-foreground">
      {agents.isPending ? "Loading the agent…" : "No agent is selected."}
    </p>
  )
  const showPanel = wide && panelOpen

  return (
    <div
      className="grid min-h-0 flex-1 grid-rows-[minmax(0,1fr)]"
      style={{
        gridTemplateColumns: roomy
          ? `${wide ? 250 : 220}px minmax(0,1fr)${showPanel ? " 290px" : ""}`
          : "minmax(0,1fr)",
      }}
    >
      {roomy && <aside className="min-h-0 border-r">{list}</aside>}
      <div className="min-h-0 min-w-0">
        <Conversation
          key={conversationKey}
          agentId={agentId}
          agent={agent}
          thread={threadId}
          threadRecord={threadRecord}
          title={title}
          onThread={(minted) => {
            setAdopted({ from: conversationKey, to: minted })
            void setSearch(
              {
                thread: minted,
                agent: null,
                prompt: null,
                send: null,
                about: null,
              },
              { history: "replace" }
            )
          }}
          draft={draft}
          onDraft={setDraft}
          pickable={talkable.map((a) => a.id)}
          onPickAgent={(next) => void setSearch({ agent: next, prompt: null })}
          keylessProvider={
            nextTurn?.canRun === false ? nextTurn.keyRow : undefined
          }
          // Sent once the agent and the provider rows are read, so a keyless
          // provider holds the question instead of sending it into a
          // refusal. Waiting for a known key would hold it forever where the
          // agent names a provider row that is not there, or the provider
          // read failed; sent, a refusal is shown and the question stays in
          // the composer.
          autoSend={
            search.send === "1" &&
            !threadId &&
            Boolean(search.prompt) &&
            Boolean(agent) &&
            !runs.pending
          }
          onAutoSent={() =>
            void setSearch({ send: null }, { history: "replace" })
          }
          context={search.about ? [search.about] : undefined}
          onContextDone={() =>
            void setSearch({ about: null }, { history: "replace" })
          }
          // Saying something in a chat takes it out of the archive: the
          // person is following it again, and its reply should find them.
          // The message is not held for it; an unarchive that fails is
          // said, and the header still offers it.
          onSpoke={() => {
            if (threadId && flags && marks.archived.has(threadId))
              void archive(threadId, false)
          }}
          actions={
            <>
              {threadId && flags && marks.archived.has(threadId) && (
                <Button
                  size="sm"
                  variant="outline"
                  title="This chat is archived: it is out of your list"
                  onClick={() => void archive(threadId, false)}
                >
                  <ArchiveRestoreIcon />
                  Unarchive
                </Button>
              )}
              {!roomy && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setChatsSheet(true)}
                >
                  <MessagesSquareIcon />
                  Chats
                </Button>
              )}
              <Button size="sm" variant="outline" onClick={() => startChat()}>
                <PlusIcon />
                New chat
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={
                  showPanel ? "Hide about this agent" : "About this agent"
                }
                aria-pressed={showPanel}
                onClick={togglePanel}
              >
                <PanelRightIcon />
              </Button>
            </>
          }
        />
      </div>
      {showPanel && (
        <aside
          aria-label="About this agent"
          className="min-h-0 overflow-y-auto border-l bg-panel"
        >
          {panel}
        </aside>
      )}

      <AddSampleDialog plane="agents" open={adding} onOpenChange={setAdding} />
      <Sheet open={chatsSheet && !roomy} onOpenChange={setChatsSheet}>
        <SheetContent side="left" className="w-[280px] gap-0 p-0">
          <SheetTitle className="sr-only">Chats</SheetTitle>
          {list}
        </SheetContent>
      </Sheet>
      <Sheet open={panelSheet && !wide} onOpenChange={setPanelSheet}>
        <SheetContent
          side="right"
          className="w-[300px] gap-0 overflow-y-auto bg-panel p-0"
        >
          <SheetTitle className="sr-only">About this agent</SheetTitle>
          {panel}
        </SheetContent>
      </Sheet>
    </div>
  )
}
