// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { tokensQueryOptions } from "@/lib/api/auth"
import { discoveryQueryOptions } from "@/lib/api/discovery"

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

import { AssistantsRow, ServerRows } from "./console-settings"

function renderRow(tokens: unknown[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(tokensQueryOptions.queryKey, tokens as never)
  return render(
    <QueryClientProvider client={client}>
      <AssistantsRow />
    </QueryClientProvider>
  )
}

describe("AssistantsRow", () => {
  afterEach(cleanup)

  // Settings says how an assistant connects without a click: the address,
  // the approval, and where each client's own steps are.
  it("says in three lines how to connect an assistant", () => {
    renderRow([])
    const steps = within(
      screen.getByRole("list", { name: "How to connect an assistant" })
    ).getAllByRole("listitem")
    expect(steps).toHaveLength(3)
    expect(steps[0].textContent).toContain(`${window.location.origin}/mcp`)
    expect(steps[1].textContent).toMatch(/approve it/)
    expect(steps[2].textContent).toMatch(
      /Claude Code, Codex, Claude and Cursor/
    )
  })

  it("counts the connected assistants", () => {
    renderRow([
      {
        id: "a",
        label: "claude.ai",
        createdAt: "2026-11-05T00:00:00Z",
        scopes: ["mcp"],
      },
      { id: "b", label: "console", createdAt: "2026-11-05T00:00:00Z" },
    ])
    expect(screen.getByText(/1 connected/)).toBeTruthy()
  })
})

function renderServer(doc: unknown) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(discoveryQueryOptions.queryKey, doc as never)
  return render(
    <QueryClientProvider client={client}>
      <ServerRows />
    </QueryClientProvider>
  )
}

/** The cells of the feature's row, keyed by the header of the column each
 * sits under: a fact is read by its label, never by its position. */
function featureFacts(name: string): Record<string, string> {
  const table = screen.getByRole("table", { name: "Features" })
  const headers = within(table)
    .getAllByRole("columnheader")
    .map((h) => h.textContent)
  const row = within(table)
    .getAllByRole("row")
    .find((r) => within(r).queryAllByRole("cell")[0]?.textContent === name)
  if (!row) throw new Error(`no row for the feature ${name}`)
  const cells = within(row).getAllByRole("cell")
  return Object.fromEntries(
    headers.map((h, i) => [h, cells[i]?.textContent ?? ""])
  )
}

describe("ServerRows", () => {
  afterEach(cleanup)

  const DOC = {
    server: { version: "1.2.3" },
    surfaces: { rest: { endpoint: "/api/v1" } },
    features: [
      { name: "records", stability: "stable", revision: 3, surfaces: ["rest"] },
      {
        name: "auth",
        stability: "beta",
        revision: 1,
        surfaces: ["rest"],
        notice:
          "Before 1.0, sign-in names a person, who then picks a repository.",
      },
      {
        name: "mcp",
        stability: "experimental",
        revision: 2,
        surfaces: ["mcp"],
      },
    ],
  }

  it("says the version and where the API is", () => {
    renderServer(DOC)
    expect(screen.getByText("Version 1.2.3 · API at /api/v1")).toBeTruthy()
  })

  // Name, stability and revision each under its own column header, and a
  // stable feature states its label like any other.
  it("shows each feature's name, stability and revision as labelled facts", () => {
    renderServer(DOC)
    expect(featureFacts("records")).toEqual({
      Feature: "records",
      Stability: "stable",
      Revision: "3",
      Notice: "",
    })
    expect(featureFacts("mcp")).toEqual({
      Feature: "mcp",
      Stability: "experimental",
      Revision: "2",
      Notice: "",
    })
  })

  it("shows a feature's notice in its row, under its own header", () => {
    renderServer(DOC)
    expect(featureFacts("auth")).toEqual({
      Feature: "auth",
      Stability: "beta",
      Revision: "1",
      Notice:
        "Before 1.0, sign-in names a person, who then picks a repository.",
    })
  })

  it("draws no notice column where no feature carries one", () => {
    renderServer({
      ...DOC,
      features: DOC.features.filter((f) => !f.notice),
    })
    expect(Object.keys(featureFacts("records"))).toEqual([
      "Feature",
      "Stability",
      "Revision",
    ])
  })

  // An older server lists features without a revision: none is stated, so
  // none is shown, where a 0 would read as a count the server gave.
  it("shows a dash for a feature an older server lists without a revision", () => {
    renderServer({
      features: [{ name: "triggers", stability: "stable" }],
    })
    expect(featureFacts("triggers")).toEqual({
      Feature: "triggers",
      Stability: "stable",
      Revision: "—",
    })
  })

  it("draws no table for a document that lists no features", () => {
    renderServer({ server: { version: "1.2.3" } })
    expect(screen.queryByRole("table")).toBeNull()
  })
})
