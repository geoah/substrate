// @vitest-environment jsdom
/** The agent page's provider and model: read side by side under the agent
 * kind's own labels, and changed together in one PATCH under the version the
 * page read, naming only what moved. An agent on `runsOn`: each model with
 * its provider, its price and why a new chat would pass it over, and the
 * list reordered, trimmed and added to in one PATCH. Switching a provider
 * and model agent to a list is one PATCH that also removes the pair. */

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
  /** The llm/provider rows, and every other list. */
  listed: [] as SubstrateRecord[],
  /** The llm/model records. */
  models: [] as SubstrateRecord[],
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
      const filter = new URL(path, "http://x").searchParams.get("filter") ?? ""
      return Promise.resolve({
        records: filter.includes("/llm/model") ? wire.models : wire.listed,
        head: 0,
        generation: "g",
      })
    }),
  }
})

import { RunsOn } from "./runs-on"

const AGENT = "substrate.reamde.dev/core/agent"
const PROVIDER = "substrate.reamde.dev/llm/provider"
const MODEL = "substrate.reamde.dev/llm/model"

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
      runsOn: {
        type: "reference",
        kind: MODEL,
        repeated: true,
        description: "one to eight models this agent runs on, best first",
      },
    },
  }),
  kind(PROVIDER, { properties: { label: { type: "string" } } }),
  kind(MODEL, {
    properties: {
      provider: { type: "reference", kind: PROVIDER, required: true },
      model: { type: "string", required: true },
      label: { type: "string" },
      pricing: {
        type: "object",
        fields: {
          inputPer1M: { type: "decimal", required: true },
          outputPer1M: { type: "decimal", required: true },
        },
      },
    },
  }),
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

function renderRunsOn(subject: SubstrateRecord = agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(["registry", "kinds"], KINDS)
  return render(
    <QueryClientProvider client={client}>
      <RunsOn agent={subject} />
    </QueryClientProvider>
  )
}

function model(
  id: string,
  row: string,
  modelId: string,
  label: string,
  pricing?: Record<string, string>
): SubstrateRecord {
  return {
    id,
    kind: MODEL,
    properties: {
      provider: { ref: `${PROVIDER}/${row}` },
      model: modelId,
      label,
      title: label,
      ...(pricing ? { pricing } : {}),
    },
    labels: {},
    version: 1,
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
  }
}

beforeEach(() => {
  wire.writes = []
  wire.listed = [
    provider("openai", "OpenAI"),
    provider("anthropic", "Anthropic"),
  ]
  wire.models = []
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

describe("RunsOn, switching to a list of models", () => {
  it("sets runsOn and removes the provider and the model in one PATCH", async () => {
    wire.models = [
      model("o:gpt-5", "openai", "gpt-5", "GPT-5"),
      model("o:mini", "openai", "gpt-5-mini", "GPT-5 mini"),
    ]
    renderRunsOn()
    fireEvent.click(
      screen.getByRole("button", { name: "Switch to a list of models" })
    )
    expect(
      screen.getByText("one to eight models this agent runs on, best first")
    ).toBeTruthy()
    // It starts from the record for the pair the agent runs on now.
    const first = screen.getByRole("button", { name: "Runs on" })
    await waitFor(() => expect(first.textContent).toContain("o:mini"))
    fireEvent.click(screen.getByRole("button", { name: "Switch" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: {
        runsOn: [{ ref: `${MODEL}/o:mini` }],
        provider: null,
        model: null,
      },
      ifVersion: 6,
    })
  })

  it("asks for a model where no record holds the pair", async () => {
    renderRunsOn()
    fireEvent.click(
      screen.getByRole("button", { name: "Switch to a list of models" })
    )
    fireEvent.click(screen.getByRole("button", { name: "Switch" }))
    expect(screen.getByRole("alert").textContent).toBe(
      "Choose the model it runs on first."
    )
    expect(wire.writes).toHaveLength(0)
  })
})

describe("RunsOn, a list of models", () => {
  const listed: SubstrateRecord = {
    ...agent,
    properties: {
      runsOn: [
        { ref: `${MODEL}/a:opus` },
        { ref: `${MODEL}/o:mini` },
        { ref: `${MODEL}/gone` },
      ],
    },
    version: 9,
  }

  beforeEach(() => {
    wire.listed = [
      provider("anthropic", "Anthropic"),
      {
        ...provider("openai", "OpenAI"),
        properties: { label: "OpenAI", apiKey: "••••" },
      },
    ]
    wire.models = [
      model("a:opus", "anthropic", "claude-opus-5-5", "Claude Opus 5.5", {
        inputPer1M: "4",
        outputPer1M: "20",
      }),
      model("o:mini", "openai", "gpt-5-mini", "GPT-5 mini", {
        inputPer1M: "0.25",
        outputPer1M: "2",
      }),
      model("o:gpt-5", "openai", "gpt-5", "GPT-5"),
    ]
  })

  function entries() {
    return [...document.querySelectorAll("ol > li")] as HTMLElement[]
  }

  it("shows each model, its provider, its price and why a new chat passes it over", async () => {
    renderRunsOn(listed)
    await screen.findByText("Claude Opus 5.5")
    const [opus, mini, gone] = entries()
    expect(opus.textContent).toContain("claude-opus-5-5")
    expect(opus.textContent).toContain("Anthropic")
    expect(opus.textContent).toContain("$4 input · $20 output per 1M tokens")
    expect(opus.textContent).toContain("Can’t run nowAnthropic has no API key.")
    expect(mini.textContent).toContain("New chats run on this")
    expect(mini.textContent).toContain("$0.25 input · $2 output per 1M tokens")
    expect(mini.textContent).not.toContain("Can’t run now")
    expect(gone.textContent).toContain("gone")
    expect(gone.textContent).toContain(
      "This model record was deleted, or never existed."
    )
    expect([...opus.querySelectorAll("dt")].map((d) => d.textContent)).toEqual([
      "Model",
      "Provider",
      "Pricing",
      "Can’t run now",
    ])
  })

  it("reorders, removes and adds in one PATCH of runsOn under the version read", async () => {
    renderRunsOn(listed)
    await screen.findByText("Claude Opus 5.5")
    fireEvent.click(screen.getByRole("button", { name: "Change Runs on" }))
    fireEvent.click(
      await screen.findByRole("button", { name: "Move GPT-5 mini up" })
    )
    fireEvent.click(screen.getByRole("button", { name: "Remove gone" }))
    fireEvent.click(screen.getByRole("button", { name: "Add a model" }))
    fireEvent.click(await screen.findByRole("option", { name: /^GPT-5$/ }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].path).toContain(
      "/substrate.reamde.dev/core/agent/ada.example.com%2Fllm%2Fhelper"
    )
    expect(wire.writes[0].body).toEqual({
      properties: {
        runsOn: [
          { ref: `${MODEL}/o:mini` },
          { ref: `${MODEL}/a:opus` },
          { ref: `${MODEL}/o:gpt-5` },
        ],
      },
      ifVersion: 9,
    })
  })

  it("writes nothing when nothing moved, and refuses an empty list", async () => {
    renderRunsOn(listed)
    await screen.findByText("Claude Opus 5.5")
    fireEvent.click(screen.getByRole("button", { name: "Change Runs on" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await new Promise((r) => setTimeout(r, 0))
    expect(wire.writes).toHaveLength(0)
    fireEvent.click(screen.getByRole("button", { name: "Change Runs on" }))
    for (const name of ["Claude Opus 5.5", "GPT-5 mini", "gone"])
      fireEvent.click(screen.getByRole("button", { name: `Remove ${name}` }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByRole("alert").textContent).toBe(
      "Runs on needs at least one model."
    )
    expect(wire.writes).toHaveLength(0)
  })
})
