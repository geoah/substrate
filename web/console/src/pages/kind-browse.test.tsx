// @vitest-environment jsdom
/** A linked page of a collection stays that page: opening a nested collection
 * at `?page=2` before the registry has loaded must not fall back to page one
 * when the kind's metadata arrives and the view turns into a tree. Only a
 * reader's own change to the view renumbers it. The head names the
 * collection for a reader and keeps the kind reference for technical mode. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { NuqsTestingAdapter, type UrlUpdateEvent } from "nuqs/adapters/testing"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { KindInfo } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

vi.mock("@/lib/api/changes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/changes")>()),
  watchChanges: () => ({ stop: () => {} }),
}))

vi.mock("@/router", () => ({
  kindBrowseRoute: {
    useParams: () => ({
      authority: "acme.example.com",
      pkg: "people",
      name: "team",
    }),
  },
}))

const TEAM = "acme.example.com/people/team"
const team: KindInfo = {
  identity: TEAM,
  name: "team",
  authority: "acme.example.com",
  package: "people",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    authority: "acme.example.com",
    package: "people",
    properties: {
      name: { type: "string" },
      parent: { type: "reference", kind: TEAM },
    },
  },
}

let resolveRegistry: (kinds: KindInfo[]) => void = () => {}
const offsets: (number | undefined)[] = []

vi.mock("@/lib/api/kinds", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/kinds")>()
  return {
    ...actual,
    kindsQueryOptions: {
      queryKey: ["registry", "kinds"],
      queryFn: () =>
        new Promise<KindInfo[]>((resolve) => {
          resolveRegistry = resolve
        }),
    },
  }
})

vi.mock("@/lib/api/records", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/records")>()
  return {
    ...actual,
    recordsQueryOptions: (
      p: Parameters<typeof actual.recordsQueryOptions>[0]
    ) => ({
      ...actual.recordsQueryOptions(p),
      queryFn: () => {
        offsets.push(p.offset)
        return Promise.resolve({ records: [], cursor: "next" })
      },
    }),
    recordCountQueryOptions: (
      ...args: Parameters<typeof actual.recordCountQueryOptions>
    ) => ({
      ...actual.recordCountQueryOptions(...args),
      queryFn: () => Promise.resolve({ value: 400, capped: false }),
    }),
  }
})

import { KindBrowsePage } from "./kind-browse"

afterEach(() => {
  cleanup()
  offsets.length = 0
  localStorage.clear()
})

describe("KindBrowsePage", () => {
  it("keeps a linked ?page= when a cold registry turns the view into a tree", async () => {
    const updates: UrlUpdateEvent[] = []
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter
          searchParams="?page=2"
          onUrlUpdate={(e) => updates.push(e)}
        >
          <KindBrowsePage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    )

    await act(async () => resolveRegistry([team]))
    await waitFor(() => expect(offsets).toContain(50))

    expect(updates.map((u) => u.searchParams.get("page"))).not.toContain(null)
    expect(offsets).not.toContain(0)
  })
})

describe("the collection head", () => {
  async function renderHead(technical: boolean) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = render(
      <ConsolePreferencesContext.Provider
        value={{
          preferences: {
            collapsed: [],
            favorites: [],
            sidebarOpen: true,
            ...DEFAULT_SETTINGS,
            technicalDetails: technical,
          },
          busy: false,
          change: () => {},
          set: () => {},
        }}
      >
        <QueryClientProvider client={client}>
          <NuqsTestingAdapter>
            <KindBrowsePage />
          </NuqsTestingAdapter>
        </QueryClientProvider>
      </ConsolePreferencesContext.Provider>
    )
    await waitFor(async () => {
      await act(async () => resolveRegistry([team]))
      expect(view.container.querySelector("[data-slot=page-header]")).not.toBe(
        null
      )
    })
    return view.container.querySelector("[data-slot=page-header]")!
  }

  it("names the collection without its reference for everyday readers", async () => {
    const head = await renderHead(false)
    expect(head.textContent).toContain("Teams")
    expect(head.textContent).not.toContain("acme.example.com")
    expect(head.querySelector("[aria-label='Copy the kind reference']")).toBe(
      null
    )
  })

  it("shows the reference and its copy button in technical mode", async () => {
    const head = await renderHead(true)
    expect(head.textContent).toContain("acme.example.com/people/team")
    expect(
      head.querySelector("[aria-label='Copy the kind reference']")
    ).not.toBe(null)
  })
})
