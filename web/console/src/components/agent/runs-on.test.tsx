// @vitest-environment jsdom
/** The agent page's provider and model: read side by side under the agent
 * kind's own labels, and changed together in one PATCH under the version the
 * page read, naming only what moved. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children?: ReactNode
  }) => (
    <a
      href={Object.entries(params ?? {}).reduce(
        (path, [key, value]) => path.replace(`$${key}`, value),
        to
      )}
      {...rest}
    >
      {children}
    </a>
  ),
}))

const wire = vi.hoisted(() => ({
  writes: [] as { method: string; path: string; body: unknown }[],
  listed: [] as SubstrateRecord[],
}))

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((method: string, path: string, body?: unknown) => {
      if (method === "PATCH") {
        wire.writes.push({ method, path, body })
        return Promise.resolve({})
      }
      return Promise.resolve({
        records: wire.listed,
        head: 0,
        generation: "g",
      })
    }),
  }
})

import { RunsOn } from "./runs-on"

const AGENT = "substrate.reamde.dev/core/agent"
const PROVIDER = "substrate.reamde.dev/llm/provider"

function kind(identity: string, definition: Record<string, unknown>): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "seeded",
    description: "",
    definition,
  }
}

const KINDS = [
  kind(AGENT, {
    properties: {
      provider: {
        type: "reference",
        kind: PROVIDER,
        required: true,
        description: "the provider row this agent's loop completes against",
      },
      model: {
        type: "string",
        required: true,
        description: "the model id sent on every completion",
      },
    },
  }),
  kind(PROVIDER, { properties: { label: { type: "string" } } }),
]

const provider = (id: string, label: string): SubstrateRecord => ({
  id,
  kind: PROVIDER,
  properties: { label, title: label },
  labels: {},
  version: 1,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:00:00Z",
})

const agent: SubstrateRecord = {
  id: "ada.example.com/llm/helper",
  kind: AGENT,
  properties: {
    provider: { ref: `${PROVIDER}/openai` },
    model: "gpt-5-mini",
  },
  labels: {},
  version: 6,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-10T00:00:00Z",
}

function renderRunsOn() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(["registry", "kinds"], KINDS)
  return render(
    <QueryClientProvider client={client}>
      <RunsOn agent={agent} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  wire.writes = []
  wire.listed = [
    provider("openai", "OpenAI"),
    provider("anthropic", "Anthropic"),
  ]
})
afterEach(cleanup)

describe("RunsOn", () => {
  it("reads the provider and the model side by side", () => {
    renderRunsOn()
    const terms = [...document.querySelectorAll("dt")].map((d) => d.textContent)
    expect(terms).toEqual(["Provider", "Model"])
    expect(screen.getByText("gpt-5-mini").tagName).toBe("DD")
    expect(document.querySelector("a")?.getAttribute("href")).toBe(
      "/data/substrate.reamde.dev/llm/provider/openai"
    )
  })

  it("switches the provider and the model in one PATCH", async () => {
    renderRunsOn()
    fireEvent.click(
      screen.getByRole("button", { name: "Change Provider and Model" })
    )
    expect(
      screen.getByText("the provider row this agent's loop completes against")
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Provider" }))
    fireEvent.click(await screen.findByRole("option", { name: /Anthropic/ }))
    fireEvent.change(screen.getByRole("textbox", { name: "Model" }), {
      target: { value: "claude-haiku-4-5" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].path).toContain(
      "/substrate.reamde.dev/core/agent/ada.example.com%2Fllm%2Fhelper"
    )
    expect(wire.writes[0].body).toEqual({
      properties: {
        provider: `${PROVIDER}/anthropic`,
        model: "claude-haiku-4-5",
      },
      ifVersion: 6,
    })
  })

  it("writes only what moved, and nothing when nothing did", async () => {
    renderRunsOn()
    fireEvent.click(
      screen.getByRole("button", { name: "Change Provider and Model" })
    )
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await new Promise((r) => setTimeout(r, 0))
    expect(wire.writes).toHaveLength(0)
    fireEvent.click(
      screen.getByRole("button", { name: "Change Provider and Model" })
    )
    fireEvent.change(screen.getByRole("textbox", { name: "Model" }), {
      target: { value: "gpt-5" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { model: "gpt-5" },
      ifVersion: 6,
    })
  })

  it("refuses an empty model before writing", async () => {
    renderRunsOn()
    fireEvent.click(
      screen.getByRole("button", { name: "Change Provider and Model" })
    )
    fireEvent.change(screen.getByRole("textbox", { name: "Model" }), {
      target: { value: "  " },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByRole("alert").textContent).toBe("Model is required.")
    expect(wire.writes).toHaveLength(0)
  })
})
