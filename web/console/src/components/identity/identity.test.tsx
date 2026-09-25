// @vitest-environment jsdom
/** The identity marks' promises: a record is never a bare id, "You" is a
 * person on a disc and never a letter, and technical mode is what puts a full
 * kind reference beside a label. */

import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ActorRef } from "./actor-ref"
import { KindRef } from "./kind-ref"
import { RecordRef } from "./record-ref"
import { StateBadge } from "./state-badge"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function preferences(
  technicalDetails: boolean
): ConsolePreferencesContextValue {
  return {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
      technicalDetails,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
}

/** Renders `ui` under a real router (Link builds its href from the route
 * tree), optionally a QueryClient, and a technical-mode switch. */
function renderWith(
  ui: ReactNode,
  {
    technical = false,
    client,
  }: { technical?: boolean; client?: QueryClient } = {}
) {
  const tree = (
    <ConsolePreferencesContext.Provider value={preferences(technical)}>
      {ui}
    </ConsolePreferencesContext.Provider>
  )
  const rootRoute = createRootRoute({
    component: () =>
      client ? (
        <QueryClientProvider client={client}>{tree}</QueryClientProvider>
      ) : (
        tree
      ),
  })
  const routeTree = rootRoute.addChildren(
    [
      "/data/$authority/$pkg/$name",
      "/data/$authority/$pkg/$name/$id",
      "/actors/$actorId",
    ].map((path) =>
      createRoute({
        getParentRoute: () => rootRoute,
        path,
        component: () => null,
      })
    )
  )
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  return render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}

const PERSON = "samples.substrate.reamde.dev/people/person"

describe("RecordRef", () => {
  it("shows the title it is given and links the record", async () => {
    renderWith(<RecordRef kind={PERSON} id="grace" title="Grace Hopper" />)
    const link = await screen.findByRole("link", { name: /Grace Hopper/ })
    expect(link.getAttribute("href")).toBe(
      "/data/samples.substrate.reamde.dev/people/person/grace"
    )
  })

  it("never shows a bare id: an untitled record is named by its kind", async () => {
    renderWith(<RecordRef kind={PERSON} id="kq3v9x2m41pf" />)
    const link = await screen.findByRole("link")
    expect(link.textContent).toBe("Untitled person")
    expect(link.textContent).not.toContain("kq3v9x2m41pf")
  })

  it("reads the title when only the reference is known", async () => {
    const fetch = vi.fn(async (url: string) => {
      const u = decodeURIComponent(String(url))
      if (u.includes("substrate.reamde.dev/core/kind")) {
        return Response.json({
          records: [
            {
              id: PERSON,
              kind: "substrate.reamde.dev/core/kind",
              properties: { names: { singular: "person" } },
            },
          ],
        })
      }
      return Response.json({
        records: [
          { id: "grace", kind: PERSON, properties: { title: "Grace Hopper" } },
        ],
      })
    })
    vi.stubGlobal("fetch", fetch)
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    renderWith(<RecordRef kind={PERSON} id="grace" />, { client })
    await waitFor(() =>
      expect(screen.getByRole("link").textContent).toBe("Grace Hopper")
    )
  })
})

describe("ActorRef", () => {
  it("names a request-asserted actor You, on a person disc, not a letter", async () => {
    renderWith(<ActorRef actor="console" />)
    const link = await screen.findByRole("link")
    expect(link.textContent).toBe("You")
    expect(link.getAttribute("href")).toBe("/actors/console")
    const mark = link.querySelector("[data-slot=actor-mark]")
    expect(mark?.getAttribute("data-actor")).toBe("you")
    expect(mark?.querySelector("svg")).not.toBeNull()
  })

  it("shows the raw actor inline only in technical mode", async () => {
    renderWith(<ActorRef actor="agent:ada.localhost:llm:substrate" />)
    expect((await screen.findByRole("link")).textContent).toBe("substrateagent")
    cleanup()
    renderWith(<ActorRef actor="agent:ada.localhost:llm:substrate" />, {
      technical: true,
    })
    expect((await screen.findByRole("link")).textContent).toContain(
      "agent:ada.localhost:llm:substrate"
    )
  })

  it("marks a provider's function with the provider's badge", async () => {
    renderWith(
      <ActorRef actor="function:providers.substrate.reamde.dev:google:synccontacts" />
    )
    const link = await screen.findByRole("link")
    expect(link.textContent).toBe("GGoogle Contacts sync")
    expect(link.querySelector("[data-slot=provider-badge]")).not.toBeNull()
  })
})

describe("KindRef", () => {
  it("labels a kind by its display plural, and links its collection", async () => {
    renderWith(<KindRef kind={PERSON} />)
    const link = await screen.findByRole("link")
    expect(link.textContent).toBe("People")
    expect(link.getAttribute("href")).toBe(
      "/data/samples.substrate.reamde.dev/people/person"
    )
  })

  it("adds the full reference in technical mode", async () => {
    renderWith(<KindRef kind={PERSON} />, { technical: true })
    expect((await screen.findByRole("link")).textContent).toBe(
      `People${PERSON}`
    )
  })

  it("shows the full reference in reference mode", async () => {
    renderWith(<KindRef kind={PERSON} mode="reference" />)
    expect((await screen.findByRole("link")).textContent).toBe(PERSON)
  })
})

describe("StateBadge", () => {
  it("says a state in plain words, and the stored value in technical mode", async () => {
    renderWith(<StateBadge value="proposed" />)
    expect(
      (await screen.findByText("Suggested"))
        .closest("[data-tone]")
        ?.getAttribute("data-tone")
    ).toBe("pending")
    expect(screen.queryByText("proposed")).toBeNull()
    cleanup()
    renderWith(<StateBadge value="proposed" />, { technical: true })
    expect(await screen.findByText("proposed")).not.toBeNull()
  })
})
