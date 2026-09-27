// @vitest-environment jsdom
/** Ask an agent hands the request to an agent that can declare a kind and
 * asks it to send, or says plainly that none can. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const navigate = vi.fn()

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

import { AddCollectionDialog } from "./add-collection-dialog"

const tool = (fn: string) => ({
  function: { ref: `substrate.reamde.dev/core/function/${fn}` },
})

function agent(id: string, properties: Record<string, unknown>) {
  return {
    id,
    kind: "substrate.reamde.dev/core/agent",
    properties,
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

const titler = agent("x.example.com/notes/titler", {
  tools: [tool("substrate.reamde.dev/core/query")],
  permissions: { reads: { kinds: ["*"] } },
})
const builder = agent("x.example.com/llm/builder", {
  tools: [tool("substrate.reamde.dev/core/write")],
  permissions: { writes: [{ ref: "substrate.reamde.dev/core/kind/*" }] },
})

function serve(records: unknown[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () => new Response(JSON.stringify({ records }), { status: 200 })
    )
  )
}

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const onWayChange = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <AddCollectionDialog way="agent" onWayChange={onWayChange} />
    </QueryClientProvider>
  )
  return onWayChange
}

describe("Add a collection: Ask an agent", () => {
  beforeEach(() => navigate.mockReset())
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it("says so when none of the agents can set one up", async () => {
    serve([titler])
    const onWayChange = mount()
    expect(
      await screen.findByText(
        "None of your agents can set up a collection yet. Start from a sample, or give an agent that permission."
      )
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Start from a sample" }))
    expect(onWayChange).toHaveBeenCalledWith("sample")
  })

  it("asks the agent that can, and has the question sent", async () => {
    serve([titler, builder])
    mount()
    const box = await screen.findByLabelText(
      "What do you want to keep track of?"
    )
    expect(screen.getByText("Builder")).toBeTruthy()
    fireEvent.change(box, { target: { value: "My recipes" } })
    fireEvent.click(screen.getByRole("button", { name: "Ask" }))
    expect(navigate).toHaveBeenCalledWith({
      href: "/agents?agent=x.example.com%2Fllm%2Fbuilder&prompt=My+recipes&send=1",
    })
  })
})
