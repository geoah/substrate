// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { tokensQueryOptions } from "@/lib/api/auth"

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

import { AssistantsRow } from "./console-settings"

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
