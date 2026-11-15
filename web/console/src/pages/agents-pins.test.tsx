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
    from?: number
    filter?: { kinds?: string[] }
    onRow: (row: unknown) => void
  }[],
}))

vi.mock("@/lib/api/changes", () => ({
  watchChanges: (opts: {
    from?: number
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
  /** What each thread list asked for: the `referencedBy` it carried,
   * "waiting" for the chats that wait, undefined for every chat. */
  threadReads: ({ flag: string; exists: boolean } | "waiting" | undefined)[]
  /** Refuse the next write of a mark. */
  failWrite?: boolean
  /** How many chats a page of a list holds, where a test cuts it short. */
  window?: number
  /** Fail every read of marks. */
  failMarks?: boolean
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
/** A page of a list. The thread lists are read before the marks, so their
 * head is the older one. */
const page = (records: unknown[], head = 5, cursor?: string) =>
  json({ records, head, generation: "g", ...(cursor && { cursor }) })

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
      if (kind === MARK && server.failMarks)
        return json({ error: { code: "internal", message: "no" } }, 500)
      if (kind === MARK)
        return page(
          (filter.properties?.thread?.in ?? [])
            .filter((id) => server.marks.has(id))
            .map(markRecord),
          9
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
      // What waits is read on its own, whatever the marks say.
      if (waiting) expect(by).toBeUndefined()
      server.threadReads.push(
        by
          ? { flag: flag!, exists: by.exists !== false }
          : waiting
            ? "waiting"
            : undefined
      )
      const found = server.threads
        .filter(
          (t) =>
            !flag ||
            (server.marks.get(t.id)?.[flag] === true) === (by!.exists !== false)
        )
        .filter((t) => !waiting || (t.waiting ?? 0) > 0)
      // The list's window, and a page of it by offset.
      const size = waiting ? found.length : (server.window ?? found.length)
      const offset = Number(u.searchParams.get("offset") ?? 0)
      const start = server.window ? (offset / 100) * server.window : 0
      return page(
        found.slice(start, start + size).map(threadRecord),
        5,
        start + size < found.length ? "more" : undefined
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
    .flatMap((b) => {
      const chat = server.threads.find((t) => b.textContent?.includes(t.title))
      return chat ? [chat.title] : []
    })
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
        { flag: "archived", exists: false },
        { flag: "pinned", exists: true },
        "waiting",
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

  it("lists a chat that waits on you though it is older than the list's window", async () => {
    server.threads[2].waiting = 1
    server.window = 2
    renderPage()
    const list = await chats()
    await waitFor(() =>
      expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
    )
    expect(
      within(list).getByRole("button", { name: /^Oldest/ }).textContent
    ).toContain("Waiting on you")
  })

  it("brings back an archived run when it comes to wait on you", async () => {
    server.marks.set("t-old", { archived: true })
    renderPage()
    const list = await chats()
    await waitFor(() => expect(titles(list)).toEqual(["Newest", "Middle"]))
    // Nothing listed is running, and the thread rows are watched all the
    // same: the run the person put away is in none of the lists.
    const feed = () =>
      watch.calls.find((c) => c.filter?.kinds?.includes(THREAD))
    await waitFor(() => expect(feed()).toBeTruthy())
    server.threads[2].waiting = 1
    act(() => feed()!.onRow({ seq: 6, kind: THREAD, recordId: "t-old" }))
    await waitFor(
      () => expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"]),
      { timeout: 4000 }
    )
    // Its mark is read once it is listed, and says why Unarchive is offered.
    await waitFor(() =>
      expect(
        within(list).getByRole("button", { name: /^Oldest/ }).textContent
      ).toContain("Archived")
    )
  })

  it("follows the marks from the oldest of the lists it read", async () => {
    renderPage()
    await chats()
    const feed = () => watch.calls.find((c) => c.filter?.kinds?.includes(MARK))
    await waitFor(() => expect(feed()).toBeTruthy())
    // The lists answered at head 5 and the marks, read after them, at 9: a
    // mark written between the two is heard only from 5.
    expect(feed()!.from).toBe(5)
  })

  it("turns the pages of the archived chats", async () => {
    for (const t of server.threads) server.marks.set(t.id, { archived: true })
    server.window = 2
    // Every chat is archived: the list is empty, and the view is still
    // offered.
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Archived" }))
    const archived = await screen.findByRole("navigation", {
      name: "Archived chats",
    })
    await waitFor(() => expect(titles(archived)).toEqual(["Newest", "Middle"]))
    expect(screen.getByRole("button", { name: "Newer chats" })).toHaveProperty(
      "disabled",
      true
    )
    fireEvent.click(screen.getByRole("button", { name: "Older chats" }))
    await waitFor(() => expect(titles(archived)).toEqual(["Oldest"]))
    expect(screen.getByRole("button", { name: "Older chats" })).toHaveProperty(
      "disabled",
      true
    )
    fireEvent.click(screen.getByRole("button", { name: "Newer chats" }))
    await waitFor(() => expect(titles(archived)).toEqual(["Newest", "Middle"]))
  })

  it("says so when the unarchive of a chat its person spoke in fails", async () => {
    server.marks.set("t-mid", { archived: true })
    renderPage("?thread=t-mid")
    await chats()
    await screen.findByRole("button", { name: "Unarchive" })
    server.failWrite = true
    await act(async () => fireEvent.click(screen.getByText("Say something")))
    expect(
      await screen.findByText("The chat couldn’t be unarchived")
    ).toBeTruthy()
    // The way back is still offered.
    expect(screen.getByRole("button", { name: "Unarchive" })).toBeTruthy()
  })

  it("follows the thread rows from the oldest list read, once every list answered", async () => {
    renderPage()
    await chats()
    const feed = () =>
      watch.calls.find((c) => c.filter?.kinds?.includes(THREAD))
    await waitFor(() => expect(feed()).toBeTruthy())
    // Never from the server's head when the stream happens to open.
    expect(feed()!.from).toBe(5)
    expect(
      watch.calls.filter((c) => c.filter?.kinds?.includes(THREAD))
    ).toHaveLength(1)
  })

  it("brings back a chat unarchived on another device when every chat was archived", async () => {
    for (const t of server.threads) server.marks.set(t.id, { archived: true })
    renderPage("?agent=" + encodeURIComponent(HELPER))
    const list = await screen.findByRole("navigation", { name: "Chats" })
    await waitFor(() => expect(list.textContent).toContain("No chats"))
    // No chat is listed and no mark was read, and the marks are followed
    // all the same.
    const feed = () => watch.calls.find((c) => c.filter?.kinds?.includes(MARK))
    await waitFor(() => expect(feed()).toBeTruthy())
    server.marks.set("t-mid", { archived: false })
    act(() => feed()!.onRow({ seq: 6, kind: MARK, recordId: "t-mid.me" }))
    await waitFor(() => expect(titles(list)).toEqual(["Middle"]), {
      timeout: 4000,
    })
  })

  it("goes by the list a chat came from when its marks cannot be read", async () => {
    server.marks.set("t-old", { pinned: true })
    server.marks.set("t-mid", { archived: true })
    server.failMarks = true
    renderPage()
    const list = await chats()
    // The server narrowed the lists by the marks the page could not read.
    await waitFor(() =>
      expect(
        titles(within(list).getByRole("region", { name: "Pinned" }))
      ).toEqual(["Oldest"])
    )
    expect(titles(list)).toEqual(["Oldest", "Newest"])
    fireEvent.click(screen.getByRole("button", { name: "Archived" }))
    const archived = await screen.findByRole("navigation", {
      name: "Archived chats",
    })
    await waitFor(() => expect(titles(archived)).toEqual(["Middle"]))
    fireEvent.click(
      within(archived).getByRole("button", { name: "Actions: Middle" })
    )
    expect(
      await screen.findByRole("menuitem", { name: "Unarchive" })
    ).toBeTruthy()
  })

  it("reads one list and offers neither action on a server from before them", async () => {
    server.revision = 8
    renderPage()
    const list = await chats()
    expect(titles(list)).toEqual(["Newest", "Middle", "Oldest"])
    // Every chat, and the ones that wait: no read is narrowed by a mark.
    expect(server.threadReads).toEqual(
      expect.arrayContaining([undefined, "waiting"])
    )
    expect(server.threadReads).toHaveLength(2)
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
