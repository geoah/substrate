// @vitest-environment jsdom
/** The record head: the meta line states the default holder once, the ⋯ menu
 * copies the link, duplicates, names every holder and (technical) opens the
 * YAML, with Delete apart under a separator, and the title is edited from
 * the keyboard with focus back on it after. */

import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const nav = vi.hoisted(() => ({ to: [] as unknown[] }))

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...rest }: { children?: ReactNode }) => (
    <a href="#" {...(rest as object)}>
      {children}
    </a>
  ),
  useNavigate: () => (to: unknown) => nav.to.push(to),
}))

const wire = vi.hoisted(() => ({
  writes: [] as { method: string; path: string; body: unknown }[],
}))

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((method: string, path: string, body?: unknown) => {
      if (method !== "GET") {
        wire.writes.push({ method, path, body })
        return Promise.resolve({ id: "copy1", kind: TASK, properties: {} })
      }
      return Promise.resolve({ records: [], head: 1, generation: "g" })
    }),
  }
})

import { RecordHeader } from "./record-header"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

const TASK = "ada.example.com/tasks/task"

const task: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    displayTemplate: "{name}",
    properties: {
      name: { type: "string" },
      location: { type: "string" },
    },
  },
}

const rec = (over: Partial<SubstrateRecord> = {}): SubstrateRecord => ({
  id: "t1",
  kind: TASK,
  properties: { name: "Plan", title: "Plan", location: "Lisbon" },
  labels: {},
  version: 3,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:00:00Z",
  propertyMeta: {
    name: { manager: "console", tier: "owner" },
    location: { manager: "console", tier: "owner" },
  },
  ...over,
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

function renderHeader(
  r: SubstrateRecord,
  opts: { technical?: boolean; onSource?: (on: boolean) => void } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const header = (at: SubstrateRecord) => (
    <ConsolePreferencesContext.Provider
      value={preferences(opts.technical ?? false)}
    >
      <QueryClientProvider client={client}>
        <RecordHeader
          record={at}
          kind={task}
          rows={[]}
          source={false}
          onSource={opts.onSource ?? (() => {})}
          holders={false}
          onHolders={() => {}}
        />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
  const view = render(header(r))
  return {
    ...view,
    refresh: (at: SubstrateRecord) => view.rerender(header(at)),
  }
}

beforeEach(() => {
  wire.writes = []
  nav.to = []
})
afterEach(cleanup)

describe("RecordHeader meta line", () => {
  it("says once that every value is yours", () => {
    renderHeader(rec())
    expect(screen.getByText("Every value is yours")).toBeTruthy()
  })

  it("says nothing of the kind when a provider holds a value", () => {
    renderHeader(
      rec({
        propertyMeta: {
          name: { manager: "console", tier: "owner" },
          location: {
            manager: "function:providers.substrate.reamde.dev:google:sync",
            tier: "machine",
          },
        },
      })
    )
    expect(screen.queryByText("Every value is yours")).toBeNull()
  })
})

describe("RecordHeader menu", () => {
  const open = () =>
    fireEvent.click(screen.getByRole("button", { name: "More" }))

  it("offers the link, a duplicate, the holders and a separate Delete", async () => {
    renderHeader(rec())
    open()
    await screen.findByRole("menuitem", { name: "Copy link" })
    const items = [
      ...document.querySelectorAll("[role^=menuitem], [role=separator]"),
    ].map((el) => el.textContent?.trim() || "—")
    expect(items).toEqual([
      "Copy link",
      "Duplicate",
      "Who holds each value",
      "—",
      "Delete",
    ])
  })

  it("opens the YAML from the menu in technical mode", async () => {
    const onSource = vi.fn()
    renderHeader(rec(), { technical: true, onSource })
    open()
    fireEvent.click(await screen.findByRole("menuitem", { name: /YAML/ }))
    expect(onSource).toHaveBeenCalledWith(true)
  })

  it("duplicates as one create and opens the copy", async () => {
    renderHeader(rec())
    open()
    fireEvent.click(await screen.findByRole("menuitem", { name: "Duplicate" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0]).toMatchObject({
      method: "POST",
      body: {
        kind: TASK,
        properties: { name: "Plan (copy)", location: "Lisbon" },
      },
    })
    await waitFor(() => expect(nav.to).toHaveLength(1))
    expect(nav.to[0]).toMatchObject({ params: { id: "copy1" } })
  })
})

describe("RecordHeader title", () => {
  it("is edited from the keyboard, and focus comes back to it", () => {
    renderHeader(rec())
    const title = screen.getByRole("button", { name: /^Plan\s*, edit$/ })
    expect(title.closest("h1")).not.toBeNull()
    fireEvent.click(title)
    const box = screen.getByRole("textbox", { name: "Name" })
    fireEvent.keyDown(box, { key: "Escape" })
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: /^Plan\s*, edit$/ })
    )
  })

  it("keeps the version it began from across a live refresh", async () => {
    const { refresh } = renderHeader(rec())
    fireEvent.click(screen.getByRole("button", { name: /^Plan\s*, edit$/ }))
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "Plan B" },
    })
    refresh(rec({ version: 4 }))
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Name" }), {
      key: "Enter",
    })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { name: "Plan B" },
      ifVersion: 3,
    })
  })
})
