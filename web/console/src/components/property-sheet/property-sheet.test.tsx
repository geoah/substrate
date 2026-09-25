// @vitest-environment jsdom
/** The property sheet's promises: the title and the body are not rows, empty
 * properties fold into one line, a click edits a value in place and a save is
 * ONE property's PATCH carrying the version the page read, a state offers only
 * the moves its machine declares, Esc writes nothing, a refusal is said under
 * the row, a host-kept property is read-only, and the chip at a row's end says
 * who holds the value: "You", a provider by name, and an amber pill when a
 * source differs, whose detail adopts or releases through the same PATCH. */

import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

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
  refuse: undefined as
    undefined | { code: "conflict" | "validation"; message: string },
}))

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  const { ApiError } = await import("@/lib/api/types")
  return {
    ...actual,
    request: vi.fn((method: string, path: string, body?: unknown) => {
      if (method === "PATCH") {
        wire.writes.push({ method, path, body })
        if (wire.refuse) {
          return Promise.reject(
            new ApiError(wire.refuse.code, wire.refuse.message, 409)
          )
        }
        return Promise.resolve({})
      }
      return Promise.resolve({ records: [], head: 0, generation: "g" })
    }),
  }
})

import { PropertySheet } from "./property-sheet"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const TASK = "ada.example.com/tasks/task"
const PERSON = "ada.example.com/people/person"
const CONTACT = "providers.substrate.reamde.dev/google/contact"
const GOOGLE_SYNC =
  "function:providers.substrate.reamde.dev:google:synccontacts"

function kind(identity: string, definition: Record<string, unknown>): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition,
  }
}

const task = kind(TASK, {
  displayTemplate: "{name|title}",
  properties: {
    name: { type: "string" },
    description: { type: "markdown" },
    location: { type: "string", description: "where it happens" },
    priority: {
      type: "enum",
      values: ["none", "low", "high"],
    },
    relationship: {
      type: "enum",
      values: [
        "friend",
        { value: "publicfigure", label: "Public figure" },
        { value: "colleague", label: "Colleague", deprecated: true },
      ],
    },
    status: {
      type: "state",
      states: ["proposed", "open", "done", "abandoned"],
      initial: "open",
      transitions: [
        { from: "proposed", to: "open" },
        { from: "open", to: "done", stamps: { completedAt: "now" } },
        { from: "done", to: "open" },
      ],
    },
    assignee: { type: "reference", kind: PERSON },
    token: { type: "string", writer: "oauth" },
    notes: { type: "string" },
    url: { type: "url" },
    due: { type: "datetime" },
  },
})

const record = (over: Partial<SubstrateRecord> = {}): SubstrateRecord => ({
  id: "t1",
  kind: TASK,
  properties: {
    name: "Draft the brief",
    title: "Draft the brief",
    description: "Keep it short.",
    location: "Lisbon",
    priority: "high",
    status: "open",
    token: "abc",
  },
  labels: {},
  version: 7,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-10T00:00:00Z",
  propertyMeta: {
    location: {
      manager: "console",
      tier: "owner",
      updatedAt: "2026-09-10T00:00:00Z",
    },
  },
  ...over,
})

function renderSheet(r: SubstrateRecord, k: KindInfo = task, readOnly = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(["registry", "kinds"], [task, kind(PERSON, {})])
  return render(
    <QueryClientProvider client={client}>
      <PropertySheet record={r} kind={k} kinds={[task]} readOnly={readOnly} />
    </QueryClientProvider>
  )
}

const row = (name: string) =>
  document.querySelector(`[data-property="${name}"]`) as HTMLElement
const valueOf = (name: string) =>
  row(name).querySelector("[role=button]") as HTMLElement | null

beforeEach(() => {
  wire.writes = []
  wire.refuse = undefined
})
afterEach(cleanup)

describe("PropertySheet rows", () => {
  it("keeps the title and the body off the sheet and folds the empties", () => {
    renderSheet(record())
    expect(row("name")).toBeNull()
    expect(row("title")).toBeNull()
    expect(row("description")).toBeNull()
    expect(row("location")).not.toBeNull()
    expect(row("notes")).toBeNull()
    const fold = screen.getByRole("button", { name: /empty:/ })
    expect(fold.textContent).toContain("Notes")
    fireEvent.click(fold)
    expect(row("notes")).not.toBeNull()
    expect(screen.getByRole("button", { name: /Hide empty/ })).toBeTruthy()
  })

  it("reads a host-kept property but never offers it for editing", () => {
    renderSheet(record())
    expect(row("token")).not.toBeNull()
    expect(valueOf("token")).toBeNull()
    expect(valueOf("location")).not.toBeNull()
  })

  it("offers nothing to edit on a provider's copy", () => {
    renderSheet(record({ kind: CONTACT }), task, true)
    expect(
      document.querySelectorAll("[role=button][aria-label^=Edit]")
    ).toHaveLength(0)
  })
})

describe("PropertySheet inline edit", () => {
  it("saves one property as a PATCH with ifVersion on Enter", async () => {
    renderSheet(record())
    fireEvent.click(valueOf("location")!)
    const input = screen.getByRole("textbox", { name: "Location" })
    fireEvent.change(input, { target: { value: "Porto" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].path).toContain("/ada.example.com/tasks/task/t1")
    expect(wire.writes[0].body).toEqual({
      properties: { location: "Porto" },
      ifVersion: 7,
    })
  })

  it("writes null when a value is emptied", async () => {
    renderSheet(record())
    fireEvent.click(valueOf("location")!)
    const input = screen.getByRole("textbox", { name: "Location" })
    fireEvent.change(input, { target: { value: "" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { location: null },
      ifVersion: 7,
    })
  })

  it("writes nothing when an editor is opened and left unchanged", async () => {
    // The date editor shows minutes; the stored instant carries seconds, so
    // leaving it as shown must not round the stored value down.
    renderSheet(
      record({
        properties: { ...record().properties, due: "2026-09-26T10:15:42.123Z" },
      })
    )
    fireEvent.click(valueOf("due")!)
    const input = screen.getByLabelText("Due")
    fireEvent.blur(input)
    fireEvent.click(valueOf("location")!)
    fireEvent.blur(screen.getByRole("textbox", { name: "Location" }))
    await new Promise((r) => setTimeout(r, 0))
    expect(wire.writes).toHaveLength(0)
  })

  it("writes nothing on Esc", async () => {
    renderSheet(record())
    fireEvent.click(valueOf("location")!)
    const input = screen.getByRole("textbox", { name: "Location" })
    fireEvent.change(input, { target: { value: "Porto" } })
    fireEvent.keyDown(input, { key: "Escape" })
    expect(screen.queryByRole("textbox", { name: "Location" })).toBeNull()
    expect(wire.writes).toHaveLength(0)
  })

  it("offers only the declared moves for a state, and a move is the patch", async () => {
    renderSheet(record())
    fireEvent.click(valueOf("status")!)
    const pop = screen.getByRole("listbox", { name: "Move Status" })
    const options = within(pop).getAllByRole("option")
    expect(options).toHaveLength(1)
    expect(options[0].textContent).toContain("Done")
    expect(options[0].textContent).toContain("fills in Completed at")
    fireEvent.click(options[0])
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { status: "done" },
      ifVersion: 7,
    })
  })

  it("picks an enum value in one click", async () => {
    renderSheet(record())
    fireEvent.click(valueOf("priority")!)
    fireEvent.click(screen.getByRole("option", { name: "Low" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { priority: "low" },
      ifVersion: 7,
    })
  })

  it("chooses an enum value from the keyboard", async () => {
    renderSheet(record())
    fireEvent.click(valueOf("priority")!)
    const list = screen.getByRole("listbox", { name: "Choose Priority" })
    // The list opens on the value held, so one step up is the one before it.
    fireEvent.keyDown(list, { key: "ArrowUp" })
    fireEvent.keyDown(list, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { priority: "low" },
      ifVersion: 7,
    })
  })

  it("writes an authored value's own spelling, never its label", async () => {
    renderSheet(record())
    fireEvent.click(screen.getByRole("button", { name: /empty:/ }))
    fireEvent.click(valueOf("relationship")!)
    fireEvent.click(screen.getByRole("option", { name: "Public figure" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { relationship: "publicfigure" },
      ifVersion: 7,
    })
  })

  it("never offers a deprecated value, but still reads the one held", () => {
    const names = () => screen.getAllByRole("option").map((o) => o.textContent)
    renderSheet(
      record({ properties: { ...record().properties, relationship: "friend" } })
    )
    fireEvent.click(valueOf("relationship")!)
    expect(names().some((n) => n?.startsWith("Colleague"))).toBe(false)
    expect(names().some((n) => n?.startsWith("Public figure"))).toBe(true)
    cleanup()

    // A record still holding it reads it, and its row says it is on its way
    // out rather than vanishing from the list it was chosen from.
    renderSheet(
      record({
        properties: { ...record().properties, relationship: "colleague" },
      })
    )
    expect(row("relationship").textContent).toContain("Colleague")
    fireEvent.click(valueOf("relationship")!)
    expect(names()).toContain("Colleagueno longer offered")
  })

  it("says the server's refusal under the row", async () => {
    wire.refuse = { code: "validation", message: "location is too long" }
    renderSheet(record())
    fireEvent.click(valueOf("location")!)
    const input = screen.getByRole("textbox", { name: "Location" })
    fireEvent.change(input, { target: { value: "x" } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect((await screen.findByRole("alert")).textContent).toBe(
      "location is too long"
    )
  })

  it("turns a version conflict into what to do next", async () => {
    wire.refuse = { code: "conflict", message: "version mismatch" }
    renderSheet(record())
    fireEvent.click(valueOf("location")!)
    const input = screen.getByRole("textbox", { name: "Location" })
    fireEvent.change(input, { target: { value: "x" } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect((await screen.findByRole("alert")).textContent).toMatch(
      /changed since you opened it/
    )
  })
})

describe("OwnershipChip", () => {
  const held = (meta: SubstrateRecord["propertyMeta"]) =>
    record({ propertyMeta: meta })

  it("says You for the owner's own value", () => {
    renderSheet(held({ location: { manager: "console", tier: "owner" } }))
    const chip = row("location").querySelector("[data-slot=owner-chip]")!
    expect(chip.getAttribute("data-holder")).toBe("you")
    expect(chip.textContent).toBe("You")
  })

  it("names the provider for a synced value, with its badge", () => {
    renderSheet(
      held({
        location: {
          manager: GOOGLE_SYNC,
          tier: "machine",
          source: `${CONTACT}/c1`,
        },
      })
    )
    const chip = row("location").querySelector("[data-slot=owner-chip]")!
    expect(chip.getAttribute("data-holder")).toBe("provider")
    expect(chip.textContent).toContain("Google")
    expect(chip.querySelector("[data-slot=provider-badge]")).not.toBeNull()
  })

  it("marks a value a source disagrees with, and adopts the source's", async () => {
    renderSheet(
      held({
        location: {
          manager: "console",
          tier: "owner",
          alternatives: [
            {
              actor: GOOGLE_SYNC,
              value: "Lisboa",
              updatedAt: "2026-09-09T00:00:00Z",
              source: `${CONTACT}/c1`,
            },
          ],
        },
      })
    )
    const pill = row("location").querySelector("[data-slot=differs]")!
    expect(pill.textContent).toBe("Google differs")
    fireEvent.click(
      screen.getByRole("button", { name: "Where Location comes from" })
    )
    const detail = document.querySelector(
      "[data-slot=ownership-detail]"
    ) as HTMLElement
    expect(detail.textContent).toContain("Yours")
    expect(detail.textContent).toContain("“Lisboa”")
    fireEvent.click(
      within(detail).getByRole("button", { name: "Use Google’s" })
    )
    const dialog = await screen.findByRole("dialog")
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Use Google’s" })
    )
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { location: "Lisboa" },
      ifVersion: 7,
    })
  })

  it("stops overriding by patching the property to null", async () => {
    renderSheet(
      held({
        location: {
          manager: "console",
          tier: "owner",
          alternatives: [
            { actor: GOOGLE_SYNC, value: "Lisboa", updatedAt: "" },
          ],
        },
      })
    )
    fireEvent.click(
      screen.getByRole("button", { name: "Where Location comes from" })
    )
    fireEvent.click(
      screen.getByRole("button", { name: /Stop overriding · follow Google/ })
    )
    const dialog = await screen.findByRole("dialog")
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Follow Google" })
    )
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { location: null },
      ifVersion: 7,
    })
  })

  it("offers the owner's own value over a bundle's pin", () => {
    renderSheet(
      held({
        location: {
          manager: "bundle:providers.substrate.reamde.dev:google",
          tier: "bundle",
        },
      })
    )
    fireEvent.click(
      screen.getByRole("button", { name: "Where Location comes from" })
    )
    const detail = document.querySelector(
      "[data-slot=ownership-detail]"
    ) as HTMLElement
    expect(detail.textContent).toContain("Set by provider")
    fireEvent.click(
      within(detail).getByRole("button", { name: "Use my own value" })
    )
    expect(screen.getByRole("textbox", { name: "Location" })).toBeTruthy()
  })
})
