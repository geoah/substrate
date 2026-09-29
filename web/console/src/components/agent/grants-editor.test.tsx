// @vitest-environment jsdom
/** The grants editor and the model-key dialog write what they say: a grant
 * edit is one PATCH of the agent's whole `permissions`, the rest carried
 * through; an edit that would leave a held tool without its grant is held
 * back with the reason; an edit that takes a collection away asks first;
 * the key is one PATCH of the provider row's `apiKey` under its version.
 * The chat panel carries the same editor under the same labels. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactNode } from "react"
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest"

import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

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

import { AgentPanel } from "./agent-panel"
import { GrantsEditor } from "./grants-editor"
import { ModelKeyDialog } from "./model-key-dialog"

const KIND = "substrate.reamde.dev/core/kind/"
const TASK = "samples.substrate.reamde.dev/tasks/task"
const PERSON = "samples.substrate.reamde.dev/people/person"
const TOKEN = "substrate.reamde.dev/core/token"
const QUERY = "substrate.reamde.dev/core/query"
const AGENT_PATH =
  "/api/v1/substrate.reamde.dev/core/agent/samples.substrate.reamde.dev%2Fllm%2Fhelper"
const PROVIDER_PATH = "/api/v1/substrate.reamde.dev/llm/provider/openai"

beforeAll(() => {
  // cmdk scrolls the highlighted row into view; jsdom has no layout.
  Element.prototype.scrollIntoView ??= () => {}
})

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

const queryTool = {
  function: { ref: `substrate.reamde.dev/core/function/${QUERY}` },
}

function helper(
  permissions: Record<string, unknown>,
  tools: unknown[] = [queryTool]
): SubstrateRecord {
  return record({
    id: "samples.substrate.reamde.dev/llm/helper",
    kind: "substrate.reamde.dev/core/agent",
    properties: { tools, permissions },
  })
}

const agent = helper({
  reads: { kinds: [{ ref: `${KIND}${TASK}` }] },
  writes: [{ ref: `${KIND}${TASK}` }, { ref: `${KIND}${PERSON}` }],
})

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

function preferences(technical: boolean): ConsolePreferencesContextValue {
  return {
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
  }
}

function wrap(ui: ReactNode, technical = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const tree = (node: ReactNode) => (
    <ConsolePreferencesContext.Provider value={preferences(technical)}>
      <QueryClientProvider client={client}>{node}</QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
  const mounted = render(tree(ui))
  return {
    ...mounted,
    rerender: (node: ReactNode) => mounted.rerender(tree(node)),
  }
}

/** The chips of one grant, by its label. */
const grant = (label: "Can see" | "Can change") =>
  screen.getByRole("list", { name: label })

/** Opens one grant's picker and returns its choices once they load. */
async function openPicker(label: "Can see" | "Can change") {
  fireEvent.click(within(grant(label)).getByRole("button", { name: "Add" }))
  await screen.findByRole("listbox", {
    name: label === "Can see" ? "Let it see" : "Let it change",
  })
}

const option = (name: string | RegExp) => screen.findByRole("option", { name })

/** The confirmation, told apart from the picker's popover (also a dialog). */
const confirmation = () =>
  document.querySelector<HTMLElement>("[data-slot=confirm-dialog]")

async function findConfirmation(): Promise<HTMLElement> {
  await waitFor(() => expect(confirmation()).toBeTruthy())
  return confirmation()!
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
          kinds: [
            kind(TASK, "task"),
            kind(PERSON, "person"),
            kind(TOKEN, "token"),
          ],
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
  const patchBody = (i = 0) =>
    JSON.parse((patches()[i][1] as RequestInit).body as string)

  it("shows the current grants under the read view's labels, on the page and in the panel", async () => {
    wrap(<GrantsEditor agent={agent} />)
    await within(grant("Can see")).findByRole("button", {
      name: "Remove Tasks",
    })
    expect(
      within(grant("Can change"))
        .getAllByRole("button", { name: /^Remove / })
        .map((b) => b.getAttribute("aria-label"))
    ).toEqual(["Remove Tasks", "Remove People"])
    cleanup()

    wrap(<AgentPanel agent={agent} policies={[]} />)
    expect(screen.getByRole("heading", { name: "Can see" })).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Can change" })).toBeTruthy()
    expect(
      within(grant("Can see")).getByRole("button", { name: "Remove Tasks" })
    ).toBeTruthy()
    expect(
      within(grant("Can change")).getByRole("button", {
        name: "Remove People",
      })
    ).toBeTruthy()
  })

  it("says a grant that names nothing the way the read view does", () => {
    wrap(<GrantsEditor agent={helper({}, [])} />)
    expect(
      within(grant("Can see")).getByText(
        "Nothing. It can’t look things up in your data."
      )
    ).toBeTruthy()
    expect(within(grant("Can change")).getByText("Nothing")).toBeTruthy()
  })

  it("picking a collection adds it to the grant and carries the rest through", async () => {
    wrap(<GrantsEditor agent={agent} />)
    await openPicker("Can see")
    fireEvent.click(await option(/^People/))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(String(patches()[0][0])).toBe(AGENT_PATH)
    expect(patchBody()).toEqual({
      properties: {
        permissions: {
          reads: { kinds: [`${KIND}${TASK}`, `${KIND}${PERSON}`] },
          writes: [{ ref: `${KIND}${TASK}` }, { ref: `${KIND}${PERSON}` }],
        },
      },
      ifVersion: 5,
    })
    expect(confirmation()).toBeNull()
  })

  it("asks before it takes a collection away, then writes the grant without it", async () => {
    wrap(<GrantsEditor agent={agent} />)
    fireEvent.click(
      await within(grant("Can change")).findByRole("button", {
        name: "Remove People",
      })
    )
    const dialog = await findConfirmation()
    expect(
      within(dialog).getByText("Stop Helper changing People?")
    ).toBeTruthy()
    expect(patches()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patchBody()).toEqual({
      properties: {
        permissions: {
          reads: { kinds: [{ ref: `${KIND}${TASK}` }] },
          writes: [`${KIND}${TASK}`],
        },
      },
      ifVersion: 5,
    })
  })

  it("writes nothing when the confirmation is cancelled", async () => {
    wrap(<GrantsEditor agent={agent} />)
    fireEvent.click(
      await within(grant("Can change")).findByRole("button", {
        name: "Remove People",
      })
    )
    const dialog = await findConfirmation()
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(confirmation()).toBeNull())
    expect(patches()).toHaveLength(0)
  })

  it("drops a pending confirmation when the panel moves to another agent", async () => {
    const { rerender } = wrap(<GrantsEditor agent={agent} />)
    fireEvent.click(
      await within(grant("Can change")).findByRole("button", {
        name: "Remove People",
      })
    )
    await findConfirmation()
    const other = record({
      ...agent,
      id: "samples.substrate.reamde.dev/llm/other",
      version: 9,
    })
    rerender(<GrantsEditor agent={other} />)
    await waitFor(() => expect(confirmation()).toBeNull())
    expect(patches()).toHaveLength(0)
  })

  it("confirms against the version it asked about, so a record that moved meanwhile is refused", async () => {
    const { rerender } = wrap(<GrantsEditor agent={agent} />)
    fireEvent.click(
      await within(grant("Can change")).findByRole("button", {
        name: "Remove People",
      })
    )
    const dialog = await findConfirmation()
    // Someone else widened the grant while the dialog was open.
    rerender(
      <GrantsEditor
        agent={{
          ...agent,
          version: 6,
          properties: {
            ...agent.properties,
            permissions: { writes: [{ ref: `${KIND}*` }] },
          },
        }}
      />
    )
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patchBody().ifVersion).toBe(5)
    expect(patchBody().properties.permissions.writes).toEqual([
      `${KIND}${TASK}`,
    ])
  })

  it("holds back an edit that would leave its lookup tool with nothing to see", async () => {
    wrap(<GrantsEditor agent={agent} />)
    fireEvent.click(
      await within(grant("Can see")).findByRole("button", {
        name: "Remove Tasks",
      })
    )
    expect(
      await screen.findByText(
        "It looks things up, so it needs to see at least one collection."
      )
    ).toBeTruthy()
    expect(confirmation()).toBeNull()
    expect(patches()).toHaveLength(0)
  })

  it("picking All your data writes the one entry that covers every collection", async () => {
    wrap(<GrantsEditor agent={agent} />)
    await openPicker("Can change")
    fireEvent.click(await option(/^All your data/))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patchBody().properties.permissions.writes).toEqual([`${KIND}*`])
    expect(confirmation()).toBeNull()
  })

  it("reads All your data back, and narrows it to one collection only after asking", async () => {
    const seesAll = helper({
      reads: { kinds: [{ ref: `${KIND}*` }], budgets: { rows: 50 } },
    })
    wrap(<GrantsEditor agent={seesAll} />)
    expect(
      within(grant("Can see")).getByRole("button", {
        name: "Remove All your data",
      })
    ).toBeTruthy()
    await openPicker("Can see")
    fireEvent.click(await option(/^People/))
    const dialog = await findConfirmation()
    expect(within(dialog).getByText("Only let Helper see People?")).toBeTruthy()
    expect(patches()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Change" }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patchBody().properties.permissions).toEqual({
      reads: { kinds: [`${KIND}${PERSON}`], budgets: { rows: 50 } },
    })
  })

  it("shows each choice's full reference in technical mode, and never offers a kind the loader refuses", async () => {
    wrap(<GrantsEditor agent={agent} />, true)
    await openPicker("Can change")
    expect((await option(/^People/)).textContent).toContain(PERSON)
    expect(screen.queryByRole("option", { name: /^Tokens/ })).toBeNull()
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    })
    cleanup()

    wrap(<GrantsEditor agent={agent} />, true)
    await openPicker("Can see")
    expect((await option(/^Tokens/)).textContent).toContain(TOKEN)
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
