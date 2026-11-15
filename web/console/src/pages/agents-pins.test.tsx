// @vitest-environment jsdom
/** Pinned and archived chats on the chats page (decision 0322): the list is
 * read in parts the server narrows by the person's own marks; pinned chats
 * sit above the rest; a chat is archived at once and taken back with Undo;
 * the Archived view lists what was put away and unarchives it; an archived
 * chat that waits on a person stays in the list; a mark another device wrote
 * moves the row without a reload; saying something in an archived chat
 * unarchives it; and a server from before all this is read as one list that
 * offers neither action. The page, the list and the marks hook are real; the
 * server and the feed are fakes. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const watch = vi.hoisted(() => ({
  calls: [] as {
    filter?: { kinds?: string[] }
    onRow: (row: unknown) => void
  }[],
}))

vi.mock("@/lib/api/changes", () => ({
  watchChanges: (opts: {
    filter?: { kinds?: string[] }
    onRow: (row: unknown) => void
  }) => {
    watch.calls.push(opts)
    return { stop: () => {} }
  },
}))
vi.mock("@/components/agent/conversation", () => ({
  Conversation: (props: {
    thread: string
    onSpoke?: () => void
    actions?: ReactNode
  }) => (
    <div>
      <span data-testid="thread">{props.thread}</span>
      <button type="button" onClick={() => props.onSpoke?.()}>
        Say something
      </button>
      {props.actions}
    </div>
  ),
}))
vi.mock("@/components/agent/agent-panel", () => ({ AgentPanel: () => null }))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a>{children}</a>,
}))

import { Toaster, toast } from "@/components/ui/toast"

import { AgentsPage, UNDO_MS } from "./agents"

const THREAD = "substrate.reamde.dev/llm/thread"
const MARK = "substrate.reamde.dev/llm/threadmark"
const AGENT = "substrate.reamde.dev/core/agent"
const HELPER = "ada.example.com/llm/helper"

interface Flags {
  pinned?: boolean
  archived?: boolean
}

/** The fake server: the chats, newest first, the caller's own flag on each,
 * every write it took and every list of threads it was asked for. */
let server: {
  revision: number
  threads: { id: string; title: string; waiting?: number }[]
  marks: Map<string, Flags>
  writes: ({ thread: string } & Flags)[]
  /** The `referencedBy` each thread list carried, undefined for none. */
  threadReads: (
    { flag: string; exists: boolean; waiting: boolean } | undefined
  )[]
  /** Refuse the next write of a mark. */
  failWrite?: boolean
}

function threadRecord(t: { id: string; title: string; waiting?: number }) {
  const at = new Date(
    Date.UTC(2026, 10, 14, 12) - server.threads.indexOf(t) * 3600_000
  ).toISOString()
  return {
    id: t.id,
    kind: THREAD,
    properties: {
      subject: t.title,
      mode: "chat",
      status: "ok",
      agent: { ref: `${AGENT}/${HELPER}` },
      startedAt: at,
      ...(t.waiting ? { waitingOnPerson: t.waiting } : {}),
    },
    labels: {},
    version: 1,
    createdAt: at,
    updatedAt: at,
  }
}

function markRecord(thread: string) {
  return {
    id: `${thread}.me`,
    kind: MARK,
    properties: {
      thread: { ref: `${THREAD}/${thread}` },
      ...server.marks.get(thread),
    },
    labels: {},
    version: 1,
    createdAt: "2026-11-14T10:00:00Z",
    updatedAt: "2026-11-14T10:00:00Z",
  }
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status })
const page = (records: unknown[]) => json({ records, head: 1, generation: "g" })

beforeEach(() => {
  watch.calls.length = 0
  server = {
    revision: 9,
    threads: [
      { id: "t-new", title: "Newest" },
      { id: "t-mid", title: "Middle" },
      { id: "t-old", title: "Oldest" },
    ],
    marks: new Map(),
    writes: [],
    threadReads: [],
  }
  vi.stubGlobal(
    "matchMedia",
    (query: string) =>
      ({
        matches: true,
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      }) as unknown as MediaQueryList
  )
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const u = new URL(String(url), "http://x")
      if (u.pathname.endsWith("/server.json"))
        return json({
          features: [
            { name: "records", stability: "stable", revision: server.revision },
          ],
        })
      if (init?.method === "POST") {
        const body = JSON.parse(String(init.body)) as {
          properties: { thread: string } & Flags
        }
        if (server.failWrite) {
          server.failWrite = false
          return json({ error: { code: "internal", message: "no" } }, 500)
        }
        // A server from before pin and archive knows neither property.
        if (
          server.revision < 9 &&
          ("pinned" in body.properties || "archived" in body.properties)
        )
          return json({ error: { code: "validation", message: "no" } }, 422)
        const { thread, ...flags } = body.properties
        server.writes.push(body.properties)
        server.marks.set(thread, { ...server.marks.get(thread), ...flags })
        return json(markRecord(thread))
      }
      const single = server.threads.find((t) =>
        u.pathname.endsWith(`/llm/thread/${t.id}`)
      )
      if (single) return json(threadRecord(single))
      const filter = JSON.parse(u.searchParams.get("filter") ?? "{}") as {
        kinds?: string[]
        properties?: Record<string, { in?: string[]; gt?: number }>
        referencedBy?: {
          kind: string
          property: string
          properties: Record<string, unknown>
          exists?: boolean
        }
      }
      const kind = filter.kinds?.[0]
      if (kind === MARK)
        return page(
          (filter.properties?.thread?.in ?? [])
            .filter((id) => server.marks.has(id))
            .map(markRecord)
        )
      if (kind === AGENT)
        return page([
          {
            id: HELPER,
            kind: AGENT,
            properties: {},
            labels: {},
            version: 1,
            createdAt: "2026-11-14T10:00:00Z",
            updatedAt: "2026-11-14T10:00:00Z",
          },
        ])
      if (kind !== THREAD) return page([])
      const by = filter.referencedBy
      if (by && server.revision < 9)
        return json({ error: { code: "bad_request", message: "no" } }, 400)
      if (by) {
        expect(by.kind).toBe(MARK)
        expect(by.property).toBe("thread")
        expect(by.properties.readBy).toEqual({ eq: "me" })
      }
      const flag = by
        ? (["pinned", "archived"] as const).find((f) => f in by.properties)!
        : undefined
      const waiting = filter.properties?.waitingOnPerson?.gt === 0
      server.threadReads.push(
        by ? { flag: flag!, exists: by.exists !== false, waiting } : undefined
      )
      return page(
        server.threads
          .filter(
            (t) =>
              !flag ||
              (server.marks.get(t.id)?.[flag] === true) ===
                (by!.exists !== false)
          )
          .filter((t) => !waiting || (t.waiting ?? 0) > 0)
          .map(threadRecord)
      )
    })
  )
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function renderPage(searchParams = "") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <NuqsTestingAdapter searchParams={searchParams} hasMemory>
        <Toaster>
          <AgentsPage />
        </Toaster>
      </NuqsTestingAdapter>
    </QueryClientProvider>
  )
}

/** The titles of the chats a list shows, top to bottom. */
function titles(list: HTMLElement) {
  return within(list)
    .getAllByRole("button")
    .filter((b) => !b.getAttribute("aria-label"))
    .map((b) => b.textContent ?? "")
    .map(
      (text) =>
        server.threads.find((t) => text.includes(t.title))?.title ?? text
    )
}

async function chats() {
  const list = await screen.findByRole("navigation", { name: "Chats" })
  await within(list).findAllByRole("button")
  return list
}

/** Opens a chat row's menu and picks one of its actions. */
async function pick(list: HTMLElement, title: string, action: string) {
  // The menu is there once the marks are read.
  fireEvent.click(
    await within(list).findByRole("button", { name: `Actions: ${title}` })
  )
  await act(async () =>
    fireEvent.click(await screen.findByRole("menuitem", { name: action }))
  )
}

describe("the chats page, pinned and archived", () => {
  it("reads the list in parts and shows pinned chats above the rest", async () => {
    server.marks.set("t-old", { pinned: true })
    server.marks.set("t-mid", { archived: true })
    renderPage()
    const list = await chats()
    await waitFor(() =>
      expect(within(list).getByRole("region", { name: "Pinned" })).toBeTruthy()
    )
    expect(
      titles(within(list).getByRole("region", { name: "Pinned" }))
    ).toEqual(["Oldest"])
    expect(titles(list)).toEqual(["Oldest", "Newest"])
    // Three reads, each narrowed by the reader's own mark; the archived
    // chats are not read until their view is opened.
    expect(server.threadReads).toEqual(
      expect.arrayContaining([
        { flag: "archived", exists: false, waiting: false },
        { flag: "pinned", exists: true, waiting: false },
        { flag: "archived", exists: true, waiting: true },
      ])
    )
    expect(server.threadReads).toHaveLength(3)
    // The chat opened by default is one in the list, never one put away.
    expect(screen.getByTestId("thread").textContent).toBe("t-new")
  })

  it("pins a chat from its menu and unpins it", async () => {
    renderPage()
    const list = await chats()
    await pick(list, "Middle", "Pin")
    expect(server.writes).toEqual([{ thread: "t-mid", pinned: true }])
    const pinned = await within(list).findByRole("region", { name: "Pinned" })
    expect(titles(pinned)).toEqual(["Middle"])
    expect(titles(list)).toEqual(["Middle", "Newest", "Oldest"])
    await pick(list, "Middle", "Unpin")
    expect(server.writes[1]).toEqual({ thread: "t-mid", pinned: false })
    await waitFor(() =>
      expect(within(list).queryByRole("region", { name: "Pinned" })).toBeNull()
    )
    expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
  })

  it("archives a chat at once, with no question, and takes it back with Undo", async () => {
    const added = vi.spyOn(toast, "add")
    renderPage()
    const list = await chats()
    await pick(list, "Middle", "Archive")
    // Written at once: the one click was the whole of it, and nothing
    // asked first.
    expect(server.writes).toEqual([{ thread: "t-mid", archived: true }])
    expect(screen.queryByRole("alertdialog")).toBeNull()
    await waitFor(() => expect(titles(list)).toEqual(["Newest", "Oldest"]))
    // The Undo is offered for a few seconds, and names the chat.
    expect(added).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Chat archived",
        description: "Middle",
        timeout: UNDO_MS,
      })
    )
    expect(UNDO_MS).toBeGreaterThanOrEqual(4000)
    expect(UNDO_MS).toBeLessThanOrEqual(10000)
    await act(async () =>
      fireEvent.click(await screen.findByRole("button", { name: "Undo" }))
    )
    expect(server.writes[1]).toEqual({ thread: "t-mid", archived: false })
    await waitFor(() =>
      expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
    )
    // Taking it back offers no Undo of its own.
    expect(added).toHaveBeenCalledTimes(1)
  })

  it("says so when an archive fails, and leaves the chat where it was", async () => {
    server.failWrite = true
    renderPage()
    const list = await chats()
    await pick(list, "Middle", "Archive")
    expect(
      await screen.findByText("The chat couldn’t be archived")
    ).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Undo" })).toBeNull()
    expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
  })

  it("lists what was archived in the Archived view, and unarchives from it", async () => {
    server.marks.set("t-mid", { archived: true })
    server.marks.set("t-old", { archived: true, pinned: true })
    renderPage()
    const list = await chats()
    await waitFor(() => expect(titles(list)).toEqual(["Newest"]))
    fireEvent.click(screen.getByRole("button", { name: "Archived" }))
    const archived = await screen.findByRole("navigation", {
      name: "Archived chats",
    })
    await waitFor(() => expect(titles(archived)).toEqual(["Middle", "Oldest"]))
    expect(server.threadReads).toContainEqual({
      flag: "archived",
      exists: true,
      waiting: false,
    })
    await pick(archived, "Oldest", "Unarchive")
    expect(server.writes).toEqual([{ thread: "t-old", archived: false }])
    await waitFor(() => expect(titles(archived)).toEqual(["Middle"]))
    // Back in the chats, it takes its pinned place again.
    fireEvent.click(screen.getByRole("button", { name: "Back to chats" }))
    const back = await chats()
    await waitFor(() =>
      expect(
        titles(within(back).getByRole("region", { name: "Pinned" }))
      ).toEqual(["Oldest"])
    )
    expect(titles(back)).toEqual(["Oldest", "Newest"])
  })

  it("keeps an archived chat that waits on you in the list", async () => {
    server.threads[2].waiting = 1
    server.marks.set("t-old", { archived: true })
    server.marks.set("t-mid", { archived: true })
    renderPage()
    const list = await chats()
    await waitFor(() => expect(titles(list)).toEqual(["Newest", "Oldest"]))
    const asking = within(list).getByRole("button", { name: /^Oldest/ })
    expect(asking.textContent).toContain("Waiting on you")
    expect(asking.textContent).toContain("Archived")
  })

  it("moves a chat another device archived or pinned, without a reload", async () => {
    renderPage()
    const list = await chats()
    expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
    const feed = () => watch.calls.find((c) => c.filter?.kinds?.includes(MARK))
    await waitFor(() => expect(feed()).toBeTruthy())
    // The phone archives one chat and pins another: its marks land on the
    // feed.
    server.marks.set("t-new", { archived: true })
    server.marks.set("t-old", { pinned: true })
    act(() => feed()!.onRow({ seq: 2, kind: MARK, recordId: "t-new.me" }))
    await waitFor(() => expect(titles(list)).toEqual(["Oldest", "Middle"]), {
      timeout: 4000,
    })
    expect(
      titles(within(list).getByRole("region", { name: "Pinned" }))
    ).toEqual(["Oldest"])
    expect(server.writes).toEqual([])
  })

  it("unarchives a chat its person says something in", async () => {
    server.marks.set("t-mid", { archived: true })
    renderPage("?thread=t-mid")
    const list = await chats()
    await waitFor(() => expect(titles(list)).toEqual(["Newest", "Oldest"]))
    // The open chat says it is archived, and offers the way back.
    expect(
      await screen.findByRole("button", { name: "Unarchive" })
    ).toBeTruthy()
    await act(async () => fireEvent.click(screen.getByText("Say something")))
    await waitFor(() =>
      expect(server.writes).toEqual([{ thread: "t-mid", archived: false }])
    )
    await waitFor(() =>
      expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
    )
    expect(screen.queryByRole("button", { name: "Unarchive" })).toBeNull()
    // Saying something in a chat that is not archived writes nothing.
    await act(async () => fireEvent.click(screen.getByText("Say something")))
    expect(server.writes).toHaveLength(1)
  })

  it("reads one list and offers neither action on a server from before them", async () => {
    server.revision = 8
    renderPage()
    const list = await chats()
    expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
    expect(server.threadReads).toEqual([undefined])
    expect(screen.queryByRole("button", { name: "Archived" })).toBeNull()
    fireEvent.click(
      await within(list).findByRole("button", { name: "Actions: Middle" })
    )
    await screen.findByRole("menuitem", { name: /^Mark as/ })
    expect(screen.queryByRole("menuitem", { name: "Pin" })).toBeNull()
    expect(screen.queryByRole("menuitem", { name: "Archive" })).toBeNull()
    expect(server.writes).toEqual([])
  })
})
