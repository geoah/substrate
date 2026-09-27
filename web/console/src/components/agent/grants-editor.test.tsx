// @vitest-environment jsdom
/** The grants editor and the model-key dialog write what they say: a grant
 * edit is one PATCH of the agent's whole `permissions`, the rest carried
 * through, and an edit that would leave a held tool without its grant is
 * held back with the reason; the key is one PATCH of the provider row's
 * `apiKey` under its version. */

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
    children,
    ...rest
  }: {
    to: string
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} {...rest}>
      {children}
    </a>
  ),
}))

import { GrantsEditor } from "./grants-editor"
import { ModelKeyDialog } from "./model-key-dialog"

const KIND = "substrate.reamde.dev/core/kind/"
const TASK = "samples.substrate.reamde.dev/tasks/task"
const PERSON = "samples.substrate.reamde.dev/people/person"
const AGENT_PATH =
  "/api/v1/substrate.reamde.dev/core/agent/samples.substrate.reamde.dev%2Fllm%2Fhelper"
const PROVIDER_PATH = "/api/v1/substrate.reamde.dev/llm/provider/openai"

function kind(identity: string, name: string): KindInfo {
  const [authority, pkg] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "imported",
    description: "",
    definition: { properties: {} },
  }
}

function record(over: Partial<SubstrateRecord>): SubstrateRecord {
  return {
    id: "",
    kind: "",
    properties: {},
    labels: {},
    version: 5,
    createdAt: "",
    updatedAt: "",
    ...over,
  }
}

const agent = record({
  id: "samples.substrate.reamde.dev/llm/helper",
  kind: "substrate.reamde.dev/core/agent",
  properties: {
    tools: [
      {
        function: {
          ref: "substrate.reamde.dev/core/function/substrate.reamde.dev/core/query",
        },
      },
    ],
    permissions: {
      reads: { kinds: [{ ref: `${KIND}${TASK}` }] },
      writes: [{ ref: `${KIND}${TASK}` }, { ref: `${KIND}${PERSON}` }],
    },
  },
})

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

function wrap(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

describe("GrantsEditor and ModelKeyDialog", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    fetchMock.mockImplementation(async (url, init) => {
      const path = String(url)
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      if (path.startsWith("/api/v1/records")) {
        return jsonResponse(200, {
          kinds: [kind(TASK, "task"), kind(PERSON, "person")],
          records: [],
        })
      }
      if (path === PROVIDER_PATH && method === "GET") {
        return jsonResponse(
          200,
          record({
            id: "openai",
            kind: "substrate.reamde.dev/llm/provider",
            properties: { wire: "openai", baseURL: "https://example.com" },
          })
        )
      }
      return jsonResponse(200, record({}))
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  const patches = () =>
    fetchMock.mock.calls.filter(
      ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
    )

  it("removes a collection it may change, carrying the rest through", async () => {
    wrap(<GrantsEditor agent={agent} />)
    const removes = await screen.findAllByRole("button", {
      name: "Remove People",
    })
    fireEvent.click(removes[0])
    await waitFor(() => expect(patches()).toHaveLength(1))
    const [url, init] = patches()[0]
    expect(String(url)).toBe(AGENT_PATH)
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      properties: {
        permissions: {
          reads: { kinds: [{ ref: `${KIND}${TASK}` }] },
          writes: [`${KIND}${TASK}`],
        },
      },
      ifVersion: 5,
    })
  })

  it("holds back an edit that would leave its lookup tool with nothing to see", async () => {
    wrap(<GrantsEditor agent={agent} />)
    const removes = await screen.findAllByRole("button", {
      name: "Remove Tasks",
    })
    // The first Tasks chip is the read grant's.
    fireEvent.click(removes[0])
    expect(
      await screen.findByText(
        "It looks things up, so it needs to see at least one collection."
      )
    ).toBeTruthy()
    expect(patches()).toHaveLength(0)
  })

  it("writes the key to the provider row, sealed, under its version", async () => {
    wrap(<ModelKeyDialog providerId="openai" open onOpenChange={() => {}} />)
    expect(await screen.findByText("Add your OpenAI key")).toBeTruthy()
    const input = await screen.findByLabelText("OpenAI API key")
    fireEvent.change(input, { target: { value: " sk-test " } })
    fireEvent.click(screen.getByRole("button", { name: "Save the key" }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    const [url, init] = patches()[0]
    expect(String(url)).toBe(PROVIDER_PATH)
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      properties: { apiKey: "sk-test" },
      ifVersion: 5,
    })
  })
})
