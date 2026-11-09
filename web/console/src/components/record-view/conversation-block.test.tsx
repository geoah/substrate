// @vitest-environment jsdom
/** A record view's conversation block on a chat's page: the chat's
 * messages oldest first with their sender, time, reply, attachments,
 * reactions and edited, deleted and send marks, and where the owner read
 * up to; the composer writing a request record in the view's names for a
 * send, a reply, a reaction and a read mark, asking first for someone
 * outside the contacts; the requests written here with their state, a
 * queued one cancelled and a failed one sent again; what a gate holds for
 * this chat, with the agent that asked; and a view naming a kind the
 * repository does not declare said as a notice. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render as renderUI,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

import type { KindInfo, RecordFilter, SubstrateRecord } from "@/lib/api/types"
import { RECORD_VIEW_KIND, viewsOf } from "@/lib/record-view"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

import { RecordViews } from "./record-views"
import { ShippedUnits } from "@/components/units-provider.test-fixtures"

const render = (ui: Parameters<typeof renderUI>[0]) =>
  renderUI(ui, { wrapper: ShippedUnits })

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const AUTHORITY = "ada.example.com"
const DECLARATION = "substrate.reamde.dev/core/kind"
const CR_KIND = "substrate.reamde.dev/core/recordpatchrequest"

function kindOf(name: string, properties: Record<string, unknown>): KindInfo {
  return {
    identity: `${AUTHORITY}/chat/${name}`,
    name,
    authority: AUTHORITY,
    package: "chat",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      authority: AUTHORITY,
      package: "chat",
      names: { singular: name },
      properties,
    },
  }
}

const USER = `${AUTHORITY}/chat/user`
const MESSAGE = `${AUTHORITY}/chat/message`
const ref = (kind: string) => ({ type: "reference", kind })

const USER_KIND = kindOf("user", {
  fullName: { type: "string" },
  isContact: { type: "bool" },
})
const CHAT_KIND = kindOf("chat", {
  chatName: { type: "string" },
  otherUser: ref(USER),
  readUpTo: ref(MESSAGE),
})
const MESSAGE_KIND = kindOf("message", {
  chat: ref(`${AUTHORITY}/chat/chat`),
  sentAt: { type: "datetime" },
  sender: ref(USER),
  fromOwner: { type: "bool" },
  text: { type: "text" },
  replyTo: ref(MESSAGE),
  editedAt: { type: "datetime" },
  deleted: {
    type: "enum",
    values: [
      { value: "everyone", label: "For everyone" },
      { value: "owner", label: "For you" },
    ],
  },
  sendStatus: {
    type: "enum",
    values: [
      { value: "sent", label: "Sent" },
      { value: "delivered", label: "Delivered" },
    ],
  },
  reactions: {
    type: "object",
    repeated: true,
    fields: { key: { type: "string" }, user: ref(USER) },
  },
})
const ATTACHMENT_KIND = kindOf("attachment", {
  message: ref(MESSAGE),
  fileName: { type: "string" },
  file: { type: "blobref" },
})
const ACTION_KIND = kindOf("action", {
  action: {
    type: "enum",
    values: [
      { value: "send", label: "Send" },
      { value: "react", label: "React" },
      { value: "markread", label: "Mark read" },
    ],
  },
  chat: ref(`${AUTHORITY}/chat/chat`),
  target: ref(MESSAGE),
  text: { type: "text" },
  replyTo: ref(MESSAGE),
  reactionKey: { type: "string" },
  allowNewChat: { type: "bool", writer: "owner" },
  cancelRequestedAt: { type: "datetime", writer: "owner" },
  state: {
    type: "state",
    states: [
      "queued",
      "sending",
      "done",
      "failed",
      "uncertain",
      "expired",
      "cancelled",
    ],
    initial: "queued",
    writer: "connector",
  },
  errorMessage: {
    type: "string",
    writer: "connector",
    displayName: "Why it failed",
  },
})
const KINDS = [USER_KIND, CHAT_KIND, MESSAGE_KIND, ATTACHMENT_KIND, ACTION_KIND]

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 3,
    createdAt: "2026-11-09T09:00:00Z",
    updatedAt: "2026-11-09T09:00:00Z",
  }
}

const pathOf = (r: SubstrateRecord) => `${r.kind}/${r.id}`
const CHAT = record(CHAT_KIND.identity, "c1", {
  chatName: "Ada",
  title: "Ada",
  otherUser: { ref: `${USER}/ada` },
  readUpTo: { ref: `${MESSAGE}/m2` },
})
const ADA = (isContact: boolean) =>
  record(USER, "ada", {
    fullName: "Ada Lovelace",
    title: "Ada Lovelace",
    isContact,
  })

const message = (id: string, props: Record<string, unknown>) =>
  record(MESSAGE, id, { chat: { ref: pathOf(CHAT) }, ...props })
/** Withdrawn by its sender for everyone, and kept: the archive holds what
 * it said. */
const M0 = message("m0", {
  sentAt: "2026-11-08T18:00:00Z",
  sender: { ref: `${USER}/ada` },
  text: "running late, sorry",
  deleted: "everyone",
  reactions: [{ key: "😢", user: { ref: `${USER}/me` } }],
})
const M1 = message("m1", {
  sentAt: "2026-11-09T10:00:00Z",
  sender: { ref: `${USER}/ada` },
  text: "dinner?",
  replyTo: { ref: `${MESSAGE}/m0` },
})
const M2 = message("m2", {
  sentAt: "2026-11-09T10:02:00Z",
  sender: { ref: `${USER}/ada` },
  text: "see you at 7",
  replyTo: { ref: `${MESSAGE}/m1` },
  editedAt: "2026-11-09T10:03:00Z",
  reactions: [
    { key: "👍", user: { ref: `${USER}/ada` } },
    { key: "👍", user: { ref: `${USER}/me` } },
  ],
})
const M3 = message("m3", {
  sentAt: "2026-11-09T10:05:00Z",
  fromOwner: true,
  text: "On my way",
  sendStatus: "delivered",
})
const MENU = record(ATTACHMENT_KIND.identity, "menu", {
  message: { ref: pathOf(M2) },
  fileName: "menu.pdf",
  title: "menu.pdf",
})

const action = (id: string, props: Record<string, unknown>) =>
  record(ACTION_KIND.identity, id, { chat: { ref: pathOf(CHAT) }, ...props })
const QUEUED = action("q1", {
  action: "send",
  text: "running late",
  state: "queued",
})
const FAILED = action("f1", {
  action: "send",
  text: "see you",
  state: "failed",
  errorMessage: "The network refused it",
  allowNewChat: true,
})
const UNCERTAIN = action("u1", {
  action: "send",
  text: "maybe sent",
  state: "uncertain",
})
const REACTED = action("r1", {
  action: "react",
  target: { ref: pathOf(M2) },
  reactionKey: "👍",
  state: "done",
})

const held = (id: string, chat: string, text: string) =>
  record(CR_KIND, id, {
    op: "create",
    targetKind: ACTION_KIND.identity,
    targetId: id,
    decision: "proposed",
    thread: { ref: "substrate.reamde.dev/llm/thread/t1" },
    rationale: "Ada asked whether you are still coming.",
    diff: { properties: { action: "send", chat, text } },
  })
const HELD = held("h1", pathOf(CHAT), "Sounds good!")
const ELSEWHERE = held("h2", `${CHAT_KIND.identity}/c2`, "Not this chat")
const THREAD = record("substrate.reamde.dev/llm/thread", "t1", {
  agent: {
    ref: "substrate.reamde.dev/core/agent/substrate.reamde.dev/assistant/moss",
  },
})

/** Who wrote each request: the owner through this console, and the
 * reaction an agent added unasked. */
const REQUESTERS: Record<string, string> = {
  r1: "agent:substrate.reamde.dev:assistant:moss",
}

const declaration = (k: KindInfo) => ({ ref: `${DECLARATION}/${k.identity}` })

const CONVERSATION = {
  label: "Messages",
  kind: declaration(MESSAGE_KIND),
  via: "chat",
  time: "sentAt",
  sender: "sender",
  mine: "fromOwner",
  text: "text",
  replyTo: "replyTo",
  edited: "editedAt",
  deleted: "deleted",
  status: "sendStatus",
  reactions: "reactions",
  attachments: declaration(ATTACHMENT_KIND),
  attachmentVia: "message",
  attachmentFile: "file",
  readUpTo: "readUpTo",
  compose: {
    kind: declaration(ACTION_KIND),
    via: "chat",
    verb: "action",
    send: "send",
    react: "react",
    markRead: "markread",
    text: "text",
    replyTo: "replyTo",
    target: "target",
    reaction: "reactionKey",
    state: "state",
    cancel: "cancelRequestedAt",
    cancelIn: ["queued"],
    retryIn: ["failed", "uncertain"],
    error: "errorMessage",
    confirm: "allowNewChat",
    peer: "otherUser",
    peerKnown: "isContact",
  },
}

interface Write {
  method: string
  path: string
  body: Record<string, unknown>
}

function mount({
  contact = true,
  conversation = CONVERSATION,
  requests = [QUEUED, FAILED, UNCERTAIN, REACTED],
  heldRequests = [HELD, ELSEWHERE],
  messagesFail = false,
}: {
  contact?: boolean
  conversation?: Record<string, unknown>
  requests?: SubstrateRecord[]
  heldRequests?: SubstrateRecord[]
  messagesFail?: boolean
} = {}) {
  const writes: Write[] = []
  const ok = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200 })
  const page = (
    records: SubstrateRecord[],
    more: Record<string, unknown> = {}
  ) => ok({ records, head: 1, generation: "g", ...more })
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), "http://test")
      const method = init?.method ?? "GET"
      if (method !== "GET") {
        const body = JSON.parse(String(init?.body ?? "{}")) as Record<
          string,
          unknown
        >
        writes.push({ method, path: url.pathname, body })
        return ok(
          record(ACTION_KIND.identity, "new", {
            ...((body.properties as Record<string, unknown>) ?? {}),
          })
        )
      }
      if (url.pathname === `/api/v1/${USER}/ada`) return ok(ADA(contact))
      if (url.pathname.startsWith(`/api/v1/${ACTION_KIND.identity}/`)) {
        const id = url.pathname.split("/").pop()!
        const found = requests.find((r) => r.id === id)
        return ok({
          ...found,
          propertyMeta: {
            action: { manager: REQUESTERS[id] ?? "console" },
          },
        })
      }
      if (url.pathname === "/api/v1/substrate.reamde.dev/llm/thread/t1")
        return ok(THREAD)
      if (url.pathname.startsWith(`/api/v1/${CR_KIND}/`)) {
        const id = url.pathname.split("/").pop()
        return ok(heldRequests.find((r) => r.id === id) ?? {})
      }
      if (url.pathname !== "/api/v1/records") {
        return new Response(
          JSON.stringify({ error: { code: "not_found", message: "" } }),
          { status: 404 }
        )
      }
      const filter = JSON.parse(
        url.searchParams.get("filter") ?? "{}"
      ) as RecordFilter
      const kinds = filter.kinds ?? []
      if (kinds.includes(MESSAGE)) {
        if (messagesFail) {
          return new Response(
            JSON.stringify({ error: { code: "internal", message: "down" } }),
            { status: 500 }
          )
        }
        return page([M3, M2, M1, M0], {
          included: {
            [`${USER}/ada`]: ADA(contact),
            [pathOf(M1)]: M1,
          },
        })
      }
      if (kinds.includes(ATTACHMENT_KIND.identity)) return page([MENU])
      if (kinds.includes(ACTION_KIND.identity)) return page(requests)
      if (kinds.includes(CR_KIND)) return page(heldRequests)
      if (kinds.includes(USER)) return page([ADA(contact)])
      return page([])
    })
  )
  const view = record(RECORD_VIEW_KIND, "chat/view", {
    appliesTo: declaration(CHAT_KIND),
    name: "This chat",
    sections: [{ conversation }],
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RecordViews
        views={viewsOf([view])}
        record={CHAT}
        kind={CHAT_KIND}
        kinds={KINDS}
      />
    </QueryClientProvider>
  )
  return { writes }
}

const messages = async () =>
  within(await screen.findByRole("list", { name: "Messages" }))

it("draws the messages oldest first, each with its sender, reply, attachments, reactions and marks", async () => {
  mount()
  const list = await messages()
  const rows = list
    .getAllByRole("listitem")
    .filter((li) => li.matches("[data-slot=conversation-message]"))
  expect(rows.map((r) => r.id)).toEqual([
    "message-m0",
    "message-m1",
    "message-m2",
    "message-m3",
  ])
  // A withdrawn message keeps what it said, under a mark in the
  // declaration's own words, and its reactions are no longer buttons.
  const withdrawn = rows[0].querySelector("[data-deleted=true]")!
  expect(
    withdrawn.querySelector("[data-slot=conversation-deleted]")?.textContent
  ).toBe("Deleted for everyone")
  expect(withdrawn.textContent).toContain("running late, sorry")
  expect(within(rows[0]).getByText("😢 1").tagName).toBe("SPAN")
  expect(rows[1].querySelector("[data-deleted]")).toBeNull()
  // An answer to it quotes it marked.
  expect(
    rows[1].querySelector("[data-slot=conversation-reply]")?.textContent
  ).toContain("Deleted for everyone: running late, sorry")
  // The answered message's text, the edit and the attachment's name.
  expect(
    rows[2].querySelector("[data-slot=conversation-reply]")?.textContent
  ).toContain("dinner?")
  expect(within(rows[2]).getByText("Edited")).toBeTruthy()
  expect(await within(rows[2]).findByText("menu.pdf")).toBeTruthy()
  const reaction = within(rows[2]).getByRole("button", {
    name: /React with 👍 too/,
  })
  expect(reaction.textContent).toBe("👍 2")
  // The owner's own message carries its send status, labelled.
  expect(rows[3].getAttribute("data-mine")).toBe("true")
  expect(
    rows[3].querySelector("[data-slot=conversation-status]")?.textContent
  ).toBe("Delivered")
  // Where the owner read up to sits right after the message the chat names.
  const items = [...screen.getByRole("list", { name: "Messages" }).children]
  const mark = items.findIndex((li) =>
    li.matches("[data-slot=conversation-read-mark]")
  )
  expect(mark).toBe(items.findIndex((li) => li.id === "message-m2") + 1)
  expect(items.map((li) => li.getAttribute("data-slot"))).toEqual([
    "conversation-day",
    "conversation-message",
    "conversation-day",
    "conversation-message",
    "conversation-message",
    "conversation-read-mark",
    "conversation-message",
  ])
})

it("offers no reply or reaction on a withdrawn message, and still marks read up to it", async () => {
  const { writes } = mount()
  const list = await messages()
  const withdrawn = list
    .getAllByRole("listitem")
    .find((li) => li.id === "message-m0")!
  fireEvent.click(
    within(withdrawn).getByRole("button", {
      name: "Act on “running late, sorry”",
    })
  )
  const read = await screen.findByRole("menuitem", {
    name: "Mark read up to here",
  })
  expect(
    screen.getAllByRole("menuitem").map((item) => item.textContent?.trim())
  ).toEqual(["Mark read up to here"])
  fireEvent.click(read)
  await waitFor(() => expect(writes).toHaveLength(1))
  expect(writes[0].body.properties).toEqual({
    action: "markread",
    chat: pathOf(CHAT),
    target: pathOf(M0),
  })
})

it("sends as the owner in the view's names, asking first for someone outside the contacts", async () => {
  const { writes } = mount({ contact: false })
  await messages()
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "hello" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Send" }))
  const dialog = await screen.findByRole("dialog")
  expect(
    within(dialog).getByText("Send to someone outside your contacts?")
  ).toBeTruthy()
  expect(dialog.textContent).toContain("Ada Lovelace isn’t among your contacts")
  fireEvent.click(within(dialog).getByRole("button", { name: "Send" }))
  await waitFor(() => expect(writes).toHaveLength(1))
  expect(writes[0]).toEqual({
    method: "POST",
    path: "/api/v1/records",
    body: {
      kind: ACTION_KIND.identity,
      properties: {
        action: "send",
        chat: pathOf(CHAT),
        text: "hello",
        allowNewChat: true,
      },
    },
  })
})

it("replies, reacts and marks read, each a request of its own", async () => {
  const { writes } = mount()
  const list = await messages()
  const second = list
    .getAllByRole("listitem")
    .find((li) => li.id === "message-m2")!
  fireEvent.click(
    within(second).getByRole("button", { name: "Act on “see you at 7”" })
  )
  fireEvent.click(await screen.findByRole("menuitem", { name: "Reply" }))
  expect((await screen.findByText(/Replying to/)).textContent).toContain(
    "see you at 7"
  )
  const box = screen.getByRole("textbox", { name: "Message" })
  fireEvent.change(box, { target: { value: "yes!" } })
  fireEvent.keyDown(box, { key: "Enter" })
  await waitFor(() => expect(writes).toHaveLength(1))
  expect(writes[0].body.properties).toEqual({
    action: "send",
    chat: pathOf(CHAT),
    text: "yes!",
    replyTo: pathOf(M2),
  })
  await waitFor(() => expect(screen.queryByText(/Replying to/)).toBeNull())

  fireEvent.click(
    within(second).getByRole("button", { name: /React with 👍 too/ })
  )
  await waitFor(() => expect(writes).toHaveLength(2))
  expect(writes[1].body.properties).toEqual({
    action: "react",
    chat: pathOf(CHAT),
    target: pathOf(M2),
    reactionKey: "👍",
  })

  fireEvent.click(screen.getByRole("button", { name: "Mark as read" }))
  await waitFor(() => expect(writes).toHaveLength(3))
  expect(writes[2].body.properties).toEqual({
    action: "markread",
    chat: pathOf(CHAT),
    target: pathOf(M3),
  })
})

it("lists the requests written here with their state, cancels a queued one and sends a failed one again", async () => {
  const { writes } = mount()
  const group = await screen.findByRole("group", { name: "Actions" })
  const rows = within(group).getAllByRole("listitem")
  expect(rows).toHaveLength(4)
  expect(rows[0].textContent).toContain("Send")
  expect(rows[0].textContent).toContain("running late")
  expect(rows[0].textContent).toContain("Queued")
  expect(rows[1].textContent).toContain("Why it failed: The network refused it")
  expect(rows[3].textContent).toContain("👍")
  expect(rows[3].textContent).toContain("see you at 7")
  // Each says who asked: the owner, or an agent no rule held.
  expect(group.textContent).toContain("What you and your agents asked for here")
  await waitFor(() =>
    expect(
      rows[3].querySelector("[data-slot=conversation-requester]")?.textContent
    ).toContain("Moss")
  )
  expect(
    rows[0].querySelector("[data-slot=conversation-requester]")?.textContent
  ).toContain("You")
  // Only the queued one can be cancelled; only the failed and the
  // uncertain one sent again.
  expect(
    within(rows[0]).queryByRole("button", { name: "Send again" })
  ).toBeNull()
  expect(within(rows[3]).queryByRole("button")).toBeNull()

  fireEvent.click(within(rows[0]).getByRole("button", { name: "Cancel" }))
  await waitFor(() => expect(writes).toHaveLength(1))
  expect(writes[0].method).toBe("PATCH")
  expect(writes[0].path).toBe(`/api/v1/${ACTION_KIND.identity}/q1`)
  expect(writes[0].body.ifVersion).toBe(3)
  expect(
    Object.keys(writes[0].body.properties as Record<string, unknown>)
  ).toEqual(["cancelRequestedAt"])

  fireEvent.click(within(rows[2]).getByRole("button", { name: "Send again" }))
  const uncertain = await screen.findByRole("dialog")
  expect(uncertain.textContent).toContain("may already have been sent")
  fireEvent.click(within(uncertain).getByRole("button", { name: "Cancel" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

  fireEvent.click(within(rows[1]).getByRole("button", { name: "Send again" }))
  const again = await screen.findByRole("dialog")
  expect(within(again).getByText("Send this again?")).toBeTruthy()
  fireEvent.click(within(again).getByRole("button", { name: "Send" }))
  await waitFor(() => expect(writes).toHaveLength(2))
  // The same values, never the old confirmation or outcome.
  expect(writes[1].body.properties).toEqual({
    action: "send",
    chat: pathOf(CHAT),
    text: "see you",
  })
})

it("still asks before sending a request again where the messages did not load", async () => {
  mount({ messagesFail: true })
  const group = await screen.findByRole("group", { name: "Actions" })
  const failed = within(group)
    .getAllByRole("listitem")
    .find((li) => li.textContent?.includes("The network refused it"))!
  fireEvent.click(within(failed).getByRole("button", { name: "Send again" }))
  expect(await screen.findByText("Send this again?")).toBeTruthy()
})

it("shows what a gate holds for this chat alone, with the agent that asked, to apply or dismiss", async () => {
  mount()
  const group = await screen.findByRole("group", { name: "Waiting for you" })
  // What a rule holds, never a claim that every agent request waits.
  expect(group.textContent).toContain(
    "Asked by an agent and held by a rule until you decide."
  )
  const rows = within(group).getAllByRole("listitem")
  expect(rows).toHaveLength(1)
  expect(rows[0].textContent).toContain("asks to")
  expect(rows[0].textContent).toContain("send")
  expect(rows[0].textContent).toContain("Sounds good!")
  expect(rows[0].textContent).toContain(
    "Ada asked whether you are still coming."
  )
  expect(
    await within(rows[0]).findByText(
      "agent:substrate.reamde.dev:assistant:moss"
    )
  ).toBeTruthy()
  expect(within(rows[0]).getByRole("button", { name: "Add it" })).toBeTruthy()
  expect(within(rows[0]).getByRole("button", { name: "Dismiss" })).toBeTruthy()
  expect(group.textContent).not.toContain("Not this chat")
})

it("says so where the view names a kind the repository does not declare", async () => {
  mount({
    conversation: {
      ...CONVERSATION,
      kind: { ref: `${DECLARATION}/${AUTHORITY}/chat/gone` },
    },
  })
  expect(
    await screen.findByText(
      /This conversation names a kind this repository does not declare: ada.example.com\/chat\/gone/
    )
  ).toBeTruthy()
})
