// @vitest-environment jsdom
/** The picker walks its kind's collection on its own, page by page behind
 * Load more, so a collection larger than one page is listed whole and no
 * candidate list is ever what the resolver or the guest held; a pick writes
 * the binding onto the app record under its version. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { InputBinder } from "./input-binder"

const USER = "providers.example.com/github/user"
const userKind: KindInfo = {
  identity: USER,
  name: "user",
  authority: "providers.example.com",
  package: "github",
  version: 1,
  source: "published",
  description: "",
  definition: { properties: {} },
}

const account = (id: string): SubstrateRecord => ({
  id,
  kind: USER,
  properties: { name: `@${id}` },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

const app: SubstrateRecord = {
  id: "pulls",
  kind: "substrate.reamde.dev/core/app",
  properties: { name: "Pulls", bindings: { other: { ref: `${USER}/x` } } },
  labels: {},
  version: 4,
  createdAt: "",
  updatedAt: "",
}

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200 })

describe("InputBinder", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it("pages the collection behind Load more and writes the pick as a binding", async () => {
    const patches: { path: string; body: unknown }[] = []
    fetchMock.mockImplementation(async (url, init) => {
      const u = new URL(String(url), "http://console")
      if (init?.method === "PATCH") {
        patches.push({ path: u.pathname, body: JSON.parse(String(init.body)) })
        return json({ ...app, version: 5 })
      }
      if (u.pathname !== `/api/v1/${USER}`) return json({ records: [] })
      expect(u.searchParams.get("first")).toBe("50")
      return u.searchParams.get("after") === "c1"
        ? json({ records: [account("c")], head: 1, generation: "g" })
        : json({
            records: [account("a"), account("b")],
            cursor: "c1",
            head: 1,
            generation: "g",
          })
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <Toaster>
          <InputBinder
            app={app}
            name="me"
            input={{ kind: USER }}
            kind={userKind}
            current={account("b")}
          />
        </Toaster>
      </QueryClientProvider>
    )
    expect(await screen.findByText("@a")).toBeTruthy()
    expect(screen.getByText("current")).toBeTruthy()
    expect(screen.queryByText("@c")).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "Load more" }))
    expect(await screen.findByText("@c")).toBeTruthy()
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Load more" })).toBeNull()
    )

    fireEvent.click(screen.getByRole("button", { name: /@c/ }))
    await waitFor(() => expect(patches).toHaveLength(1))
    expect(patches[0]).toEqual({
      path: "/api/v1/substrate.reamde.dev/core/app/pulls",
      body: {
        properties: {
          bindings: { other: { ref: `${USER}/x` }, me: { ref: `${USER}/c` } },
        },
        ifVersion: 4,
      },
    })
  })
})
