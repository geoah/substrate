// @vitest-environment jsdom
/** The agent's panel says what it runs on, "Runs on <model card>, <model>",
 * and both that line and the "Add your key" prompt link to the model's card
 * on Providers (`/providers?model=<row id>`), where the key is written. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    search,
    children,
    ...rest
  }: {
    to: string
    search?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-search={JSON.stringify(search ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

import { AgentPanel } from "./agent-panel"

const agent = {
  id: "samples.substrate.reamde.dev/llm/helper",
  kind: "substrate.reamde.dev/core/agent",
  version: 1,
  labels: {},
  createdAt: "",
  updatedAt: "",
  properties: {
    model: "gpt-5",
    provider: { ref: "substrate.reamde.dev/llm/provider/openrouter" },
  },
} as unknown as SubstrateRecord

function wrap(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>{ui}</QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ records: [], kinds: [] }))
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/** The link's target and its search, as the router would build them. */
function target(link: HTMLElement) {
  return {
    to: link.getAttribute("data-to"),
    search: JSON.parse(link.getAttribute("data-search") ?? "{}"),
  }
}

describe("AgentPanel", () => {
  it("says what the agent runs on, linking to the model's card", () => {
    wrap(
      <AgentPanel
        agent={agent}
        hasKey
        providerLabel="OpenRouter"
        policies={[]}
      />
    )
    const line = screen.getByText(/^Runs on/)
    expect(line.textContent).toBe("Runs on OpenRouter, gpt-5")
    expect(target(screen.getByText("OpenRouter", { selector: "a" }))).toEqual({
      to: "/providers",
      search: { model: "openrouter" },
    })
    expect(screen.queryByText(/Add your/)).toBeNull()
  })

  it("links the Add your key prompt to the same card where no key resolves", () => {
    wrap(
      <AgentPanel
        agent={agent}
        hasKey={false}
        providerLabel="OpenRouter"
        policies={[]}
      />
    )
    expect(screen.getByText("No API key for OpenRouter yet.")).toBeTruthy()
    expect(
      target(screen.getByText("Add your OpenRouter key", { selector: "a" }))
    ).toEqual({ to: "/providers", search: { model: "openrouter" } })
  })
})
