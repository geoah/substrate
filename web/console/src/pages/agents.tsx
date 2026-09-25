/** Agents (`/agents`): a chat app over your agents — every conversation on
 * the left, the one being read in the middle, what its agent is and may do on
 * the right. Suggested changes are cards inside the thread; there is no
 * separate place to go and approve them.
 *
 * THE ADDRESS. `?thread=<id>` opens one conversation (a deep link);
 * `?agent=<agent id>` opens a new chat with that agent; `?prompt=<text>`
 * prefills the composer of a new chat (other pages link here with a question).
 * A bare `/agents` opens the most recent conversation, or a new chat when
 * there is none. `/agents/<agent id>` is the old per-agent address and
 * redirects to `?agent=` (its `?thread=` carried along).
 *
 * The llm/provider rows are not agents and are not listed; an agent's
 * provider shows on its panel, and a provider without a key says so where the
 * conversation would otherwise fail. */

import { useMemo, useState, useSyncExternalStore } from "react"
import { useQuery } from "@tanstack/react-query"
import { MessagesSquareIcon, PanelRightIcon, PlusIcon } from "lucide-react"
import { parseAsString, useQueryStates } from "nuqs"

import { AgentPanel } from "@/components/agent/agent-panel"
import { Conversation } from "@/components/agent/conversation"
import { ThreadList } from "@/components/agent/thread-list"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import {
  agentProviderId,
  chatCapable,
  chatRows,
  openingMessages,
  threadAgentId,
  threadTitle,
} from "@/lib/agent-chat"
import {
  agentsQueryOptions,
  conversationsQueryOptions,
  llmProvidersQueryOptions,
  openingMessagesQueryOptions,
  policiesForAgent,
  providerHasKey,
  writePoliciesQueryOptions,
} from "@/lib/api/agents"
import { CORE_AUTHORITY, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"

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

const SEARCH = {
  thread: parseAsString.withDefault(""),
  agent: parseAsString.withDefault(""),
  prompt: parseAsString.withDefault(""),
}

export function AgentsPage() {
  const [search, setSearch] = useQueryStates(SEARCH, { history: "push" })
  const wide = useMedia("(min-width: 1100px)")
  const roomy = useMedia("(min-width: 900px)")
  const [panelOpen, setPanelOpen] = useState(true)
  const [panelSheet, setPanelSheet] = useState(false)
  const [chatsSheet, setChatsSheet] = useState(false)
  // The draft outlives switching the new chat's agent, which remounts the
  // conversation; `?prompt=` seeds it once.
  const [draft, setDraft] = useState(search.prompt)
  // A new chat whose run minted a thread keeps its conversation mounted.
  const [adopted, setAdopted] = useState<{ from: string; to: string }>()

  const agents = useQuery(agentsQueryOptions())
  const conversations = useQuery(conversationsQueryOptions())
  const providers = useQuery(llmProvidersQueryOptions())
  const policies = useQuery(writePoliciesQueryOptions())

  const allAgents = useMemo(() => agents.data?.records ?? [], [agents.data])
  const talkable = useMemo(() => allAgents.filter(chatCapable), [allAgents])
  const threads = useMemo(
    () => conversations.data?.records ?? [],
    [conversations.data]
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
  const threadId = search.thread || (newChat ? "" : (threads[0]?.id ?? ""))
  const listed = threads.find((t) => t.id === threadId)
  // A deep link past the loaded window reads its thread on its own.
  const single = useQuery({
    ...recordQueryOptions(CORE_AUTHORITY, LLM_PACKAGE_NAME, "thread", threadId),
    enabled: Boolean(threadId) && !listed && !conversations.isPending,
  })
  const threadRecord = listed ?? single.data
  const threadAgent = threadRecord ? threadAgentId(threadRecord) : undefined
  // A new chat's agent: the one asked for, else whoever you talked to last
  // (when you still can), else the first you can talk to.
  const lastAgent = threads[0] ? threadAgentId(threads[0]) : undefined
  const defaultAgent = talkable.some((a) => a.id === lastAgent)
    ? lastAgent
    : talkable[0]?.id
  const agentId = threadId ? threadAgent : search.agent || defaultAgent
  const agent = allAgents.find((a) => a.id === agentId)

  const providerId = agent ? agentProviderId(agent) : undefined
  const providerRow = providers.data?.records.find((p) => p.id === providerId)
  const hasKey = providerRow ? providerHasKey(providerRow) : undefined
  const agentPolicies =
    agent && policies.data
      ? policiesForAgent(agent.id, policies.data.records)
      : []

  const title = threadId
    ? (rows.find((r) => r.thread.id === threadId)?.title ??
      threadTitle(undefined))
    : "New chat"

  const conversationKey = threadId
    ? adopted?.to === threadId
      ? adopted.from
      : threadId
    : `new:${agentId ?? ""}`

  function openThread(id: string) {
    setChatsSheet(false)
    setDraft("")
    void setSearch({ thread: id, agent: null, prompt: null })
  }

  function startChat(agent?: string) {
    setChatsSheet(false)
    setDraft("")
    void setSearch({
      thread: null,
      agent: agent ?? agentId ?? talkable[0]?.id ?? null,
      prompt: null,
    })
  }

  function togglePanel() {
    if (wide) setPanelOpen((v) => !v)
    else setPanelSheet(true)
  }

  const list = (
    <ThreadList
      rows={rows}
      loading={conversations.isPending || agents.isPending}
      error={conversations.error?.message ?? agents.error?.message}
      agents={allAgents}
      selected={threadId}
      onSelect={openThread}
      onNewChat={startChat}
    />
  )
  const panel = agent ? (
    <AgentPanel agent={agent} hasKey={hasKey} policies={agentPolicies} />
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
      <main className="min-h-0 min-w-0">
        <Conversation
          key={conversationKey}
          agentId={agentId}
          agent={agent}
          thread={threadId}
          title={title}
          onThread={(minted) => {
            setAdopted({ from: conversationKey, to: minted })
            void setSearch(
              { thread: minted, agent: null, prompt: null },
              { history: "replace" }
            )
          }}
          draft={draft}
          onDraft={setDraft}
          pickable={talkable.map((a) => a.id)}
          onPickAgent={(next) => void setSearch({ agent: next, prompt: null })}
          keylessProvider={hasKey === false ? providerId : undefined}
          actions={
            <>
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
      </main>
      {showPanel && (
        <aside
          aria-label="About this agent"
          className="min-h-0 overflow-y-auto border-l bg-panel"
        >
          {panel}
        </aside>
      )}

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
