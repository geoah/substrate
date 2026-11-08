// @vitest-environment jsdom
/** The agent's panel says what it runs on, "Runs on <model card>, <model>",
 * and both that line and the "Add your key" prompt link to the model's card
 * on Providers (`/providers?model=<row id>`), where the key is written. An
 * agent on `runsOn` reads as the entry a new chat takes, and, where none can
 * run, points at the first entry's provider. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { OperatorDefaults, SubstrateRecord } from "@/lib/api/types"
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

const PROVIDER = "substrate.reamde.dev/llm/provider"
const MODEL = "substrate.reamde.dev/llm/model"

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    version: 1,
    labels: {},
    createdAt: "",
    updatedAt: "",
    properties,
  }
}

const agent = record(
  "substrate.reamde.dev/core/agent",
  "samples.substrate.reamde.dev/llm/helper",
  {
    model: "gpt-5",
    provider: { ref: `${PROVIDER}/openrouter` },
  }
)

const agentOnModels = record(
  "substrate.reamde.dev/core/agent",
  "samples.substrate.reamde.dev/llm/helper",
  {
    runsOn: [
      { ref: `${MODEL}/anthropic:claude-opus-5-5` },
      { ref: `${MODEL}/openai:gpt-5` },
    ],
  }
)

const MODELS = [
  record(MODEL, "anthropic:claude-opus-5-5", {
    provider: { ref: `${PROVIDER}/anthropic` },
    model: "claude-opus-5-5",
    label: "Claude Opus 5.5",
  }),
  record(MODEL, "openai:gpt-5", {
    provider: { ref: `${PROVIDER}/openai` },
    model: "gpt-5",
    label: "GPT-5",
  }),
]

let rows: SubstrateRecord[] = []
/** The operator's read; null answers as a server older than it. */
let operator: OperatorDefaults | null = null

function kindsOf(url: string): string[] {
  const u = new URL(url, "http://x")
  if (u.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(u.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
}

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
  rows = []
  operator = null
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = String(input)
      if (url === "/api/v1/operator/defaults")
        return operator
          ? Response.json(operator)
          : Response.json(
              { error: { code: "not_found", message: "no route" } },
              { status: 404 }
            )
      const kinds = kindsOf(url)
      if (kinds.includes(PROVIDER)) return Response.json({ records: rows })
      if (kinds.includes(MODEL)) return Response.json({ records: MODELS })
      return Response.json({ records: [], kinds: [] })
    })
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
  it("says what the agent runs on, linking to the model's card", async () => {
    rows = [
      record(PROVIDER, "openrouter", { label: "OpenRouter", apiKey: "••••" }),
    ]
    wrap(<AgentPanel agent={agent} policies={[]} />)
    const link = await screen.findByText("OpenRouter", { selector: "a" })
    expect(screen.getByText(/^Runs on/).textContent).toBe(
      "Runs on OpenRouter, gpt-5"
    )
    expect(target(link)).toEqual({
      to: "/providers",
      search: { model: "openrouter" },
    })
    expect(screen.queryByText(/Add your/)).toBeNull()
  })

  it("links the Add your key prompt to the same card where no key resolves", async () => {
    rows = [record(PROVIDER, "openrouter", { label: "OpenRouter" })]
    wrap(<AgentPanel agent={agent} policies={[]} />)
    expect(
      await screen.findByText("No API key for OpenRouter yet.")
    ).toBeTruthy()
    expect(
      target(screen.getByText("Add your OpenRouter key", { selector: "a" }))
    ).toEqual({ to: "/providers", search: { model: "openrouter" } })
  })

  it("reads an agent on runsOn as the first entry that can run", async () => {
    rows = [
      record(PROVIDER, "anthropic", { label: "Anthropic" }),
      record(PROVIDER, "openai", { label: "OpenAI", apiKey: "••••" }),
    ]
    wrap(<AgentPanel agent={agentOnModels} policies={[]} />)
    expect(await screen.findByText("OpenAI", { selector: "a" })).toBeTruthy()
    expect(screen.getByText(/^Runs on/).textContent).toBe(
      "Runs on OpenAI, GPT-5"
    )
    expect(screen.queryByText(/can run yet/)).toBeNull()
  })

  it("takes the operator's key for a model on its list", async () => {
    rows = [
      record(PROVIDER, "anthropic", { label: "Anthropic" }),
      record(PROVIDER, "openai", { label: "OpenAI", apiKey: "••••" }),
    ]
    operator = {
      items: [
        {
          id: "llm/anthropic",
          slot: "llm",
          label: "Anthropic",
          source: "operator",
          operatorOffers: true,
          // Readiness never moves the choice.
          readiness: "failing",
          message: "Anthropic refused the key (401)",
          models: ["claude-opus-5-5"],
        },
      ],
    }
    wrap(<AgentPanel agent={agentOnModels} policies={[]} />)
    expect(await screen.findByText("Anthropic", { selector: "a" })).toBeTruthy()
    expect(screen.getByText(/^Runs on/).textContent).toBe(
      "Runs on Anthropic, Claude Opus 5.5"
    )
  })

  it("points at the first entry's provider where none can run", async () => {
    rows = [
      record(PROVIDER, "anthropic", { label: "Anthropic" }),
      record(PROVIDER, "openai", { label: "OpenAI" }),
    ]
    wrap(<AgentPanel agent={agentOnModels} policies={[]} />)
    expect(
      await screen.findByText("None of the models it runs on can run yet.")
    ).toBeTruthy()
    expect(
      target(screen.getByText("Add your Anthropic key", { selector: "a" }))
    ).toEqual({ to: "/providers", search: { model: "anthropic" } })
  })
})
