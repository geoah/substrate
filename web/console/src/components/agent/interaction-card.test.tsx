// @vitest-environment jsdom
/** The question card: a waiting batch offers its options and, inside a
 * conversation, to be answered in a message; a resolved one says how it
 * ended and whose hand ended it, where the record links one. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"

import { InteractionCard } from "./interaction-card"
import { QuestionReplyContext, type QuestionReply } from "./question-reply"
import type { SubstrateRecord } from "@/lib/api/types"

const ACTOR = "substrate.reamde.dev/core/actor/6nk6ekkczdgh399nt2pqpccjz0"

const fetchMock = vi.fn<typeof fetch>()
beforeEach(() => vi.stubGlobal("fetch", fetchMock))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

function interaction(
  state: string,
  extra: Record<string, unknown> = {}
): SubstrateRecord {
  return {
    id: "i1",
    kind: "substrate.reamde.dev/llm/interaction",
    properties: {
      state,
      thread: { ref: "substrate.reamde.dev/llm/thread/t1" },
      questions: [
        {
          id: "day",
          prompt: "Which day suits you?",
          options: [{ value: "mon", label: "Monday" }, { value: "tue" }],
        },
      ],
      ...extra,
    },
    labels: {},
    version: 3,
    createdAt: "2026-11-15T10:00:00Z",
    updatedAt: "2026-11-15T10:00:00Z",
  }
}

/** A server at `agents` revision `revision` that holds `record`. `mine` says
 * whether the signed-in person resolved it; `actorName` is the name an admin
 * reads off the actor record, absent for a person who may not read it. */
function serve(
  record: SubstrateRecord,
  opts: { revision?: number; mine?: boolean; actorName?: string } = {}
) {
  const asked: string[] = []
  fetchMock.mockImplementation(async (input) => {
    const url = decodeURIComponent(String(input))
    asked.push(url)
    if (url.includes("server.json")) {
      return Response.json({
        features: [{ name: "agents", revision: opts.revision ?? 7 }],
      })
    }
    if (url.includes("/core/actor/")) {
      if (!opts.actorName)
        return Response.json(
          { error: { code: "not_found", message: "no" } },
          { status: 404 }
        )
      return Response.json({
        id: "6nk6ekkczdgh399nt2pqpccjz0",
        kind: "substrate.reamde.dev/core/actor",
        properties: { type: "person", name: opts.actorName },
        labels: {},
        version: 1,
        createdAt: "2026-11-15T10:00:00Z",
        updatedAt: "2026-11-15T10:00:00Z",
      })
    }
    if (url.includes("/api/v1/records")) {
      return Response.json({
        records: opts.mine ? [record] : [],
        head: 0,
        generation: "g",
      })
    }
    return Response.json(record)
  })
  return asked
}

function renderCard(reply: QuestionReply | null = null) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <QuestionReplyContext.Provider value={reply}>
        <InteractionCard id="i1" />
      </QuestionReplyContext.Provider>
    </QueryClientProvider>
  )
}

it("offers to be answered in a message inside a conversation that takes one", async () => {
  serve(interaction("pending"))
  const begin = vi.fn()
  renderCard({ begin, cancel: vi.fn() })
  fireEvent.click(
    await screen.findByRole("button", { name: "Answer in your own words" })
  )
  expect(begin).toHaveBeenCalledWith("i1", "Which day suits you?")
})

it("offers only its options where no message can answer it", async () => {
  serve(interaction("pending"))
  renderCard(null)
  await screen.findByRole("button", { name: "Answer" })
  expect(
    screen.queryByRole("button", { name: "Answer in your own words" })
  ).toBeNull()
  expect(screen.getByRole("button", { name: "Dismiss" })).toBeTruthy()
})

it("sets its options aside while the next message answers it", async () => {
  serve(interaction("pending"))
  const cancel = vi.fn()
  renderCard({ replyingTo: "i1", begin: vi.fn(), cancel })
  await screen.findByText(
    "Write your answer in the message box below and send it."
  )
  expect(screen.queryByRole("button", { name: "Answer" })).toBeNull()
  expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull()
  for (const radio of screen.getAllByRole<HTMLInputElement>("radio"))
    expect(radio.disabled).toBe(true)
  fireEvent.click(screen.getByRole("button", { name: "Back to the options" }))
  expect(cancel).toHaveBeenCalled()
})

it("says a question answered in a message was, and that you answered it", async () => {
  const asked = serve(
    interaction("replied", {
      resolvedBy: { ref: ACTOR },
      repliedIn: { ref: "substrate.reamde.dev/llm/message/m9" },
    }),
    { mine: true }
  )
  renderCard()
  expect(await screen.findByText("Answered in a message")).toBeTruthy()
  const who = await screen.findByText("You")
  expect(who.closest("[data-slot=resolved-by]")?.textContent).toBe(
    "Answered by You"
  )
  expect(screen.queryByRole("button", { name: "Answer" })).toBeNull()
  // Which cards are mine is asked of the server, by thread and `me`: the
  // page never holds its person's actor id.
  const mine = asked.find((u) => u.includes("/api/v1/records"))
  expect(mine).toContain('"thread":{"eq":"t1"}')
  expect(mine).toContain('"resolvedBy":{"eq":"me"}')
  // The actor is read only for somebody else.
  expect(asked.some((u) => u.includes("/core/actor/"))).toBe(false)
})

it("names another person where the reader may read actors, and not otherwise", async () => {
  serve(interaction("dismissed", { resolvedBy: { ref: ACTOR } }), {
    actorName: "ada.example.com",
  })
  renderCard()
  const who = await screen.findByText("ada.example.com")
  expect(who.closest("[data-slot=resolved-by]")?.textContent).toBe(
    "Dismissed by ada.example.com"
  )
  cleanup()

  serve(interaction("answered", { resolvedBy: { ref: ACTOR } }))
  renderCard()
  const other = await screen.findByText("Someone else")
  expect(other.closest("[data-slot=resolved-by]")?.textContent).toBe(
    "Answered by Someone else"
  )
})

it("says nothing about whose hand it was for a card resolved before the link", async () => {
  const asked = serve(interaction("answered"))
  const { container } = renderCard()
  await screen.findByText("Answered")
  await waitFor(() =>
    expect(asked.some((u) => u.includes("/llm/interaction/"))).toBe(true)
  )
  expect(container.querySelector("[data-slot=resolved-by]")).toBeNull()
  expect(asked.some((u) => u.includes("/api/v1/records"))).toBe(false)
})

it("asks an older server nothing it would refuse", async () => {
  // A link on a record of a server below revision 7 cannot be: the property
  // is not declared there. A newer record read through an older server is
  // drawn as resolved, with no who.
  const asked = serve(
    interaction("dismissed", { resolvedBy: { ref: ACTOR } }),
    { revision: 6 }
  )
  const { container } = renderCard()
  await screen.findByText("Dismissed")
  await waitFor(() =>
    expect(asked.some((u) => u.includes("server.json"))).toBe(true)
  )
  expect(container.querySelector("[data-slot=resolved-by]")).toBeNull()
  expect(asked.some((u) => u.includes("/api/v1/records"))).toBe(false)
})
