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
    tags: { type: "string", repeated: true },
    emails: { type: "email", repeated: true },
    quotes: { type: "string", repeated: true },
    steps: { type: "markdown", repeated: true },
    moods: { type: "enum", values: ["calm", "busy"], repeated: true },
    members: {
      type: "reference",
      kind: PERSON,
      repeated: true,
      properties: { role: { type: "string", displayName: "role" } },
    },
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

function renderSheet(
  r: SubstrateRecord,
  k: KindInfo = task,
  readOnly = false,
  holders = false
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(["registry", "kinds"], [task, kind(PERSON, {})])
  const sheet = (at: SubstrateRecord) => (
    <QueryClientProvider client={client}>
      <PropertySheet
        record={at}
        kind={k}
        kinds={[task]}
        readOnly={readOnly}
        holders={holders}
      />
    </QueryClientProvider>
  )
  const view = render(sheet(r))
  /** A live re-read: the page hands the sheet the record as it is now. */
  const refresh = (at: SubstrateRecord) => view.rerender(sheet(at))
  return { ...view, refresh }
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
    // Under 480px the names hide and the count stands alone.
    const names = within(fold).getByText(/^: /)
    expect(names.className).toContain("hidden")
    expect(names.className).toContain("min-[480px]:inline")
    expect(names.textContent).toContain("Notes")
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
    expect(names()).toContain("Colleagueno longer offered(chosen)")
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

describe("PropertySheet from the keyboard", () => {
  it("names a value cell by its label and value, and says it edits", () => {
    renderSheet(record())
    expect(
      screen.getByRole("button", { name: /^Location\s+Lisbon\s*, edit$/ })
    ).toBe(valueOf("location"))
  })

  it("gives focus back to the cell after Enter saves nothing and after Esc", () => {
    renderSheet(record())
    const cell = valueOf("location")!
    cell.focus()
    fireEvent.keyDown(cell, { key: "Enter" })
    const box = screen.getByRole("textbox", { name: "Location" })
    expect(document.activeElement).toBe(box)
    fireEvent.keyDown(box, { key: "Enter" })
    expect(document.activeElement).toBe(valueOf("location"))
    fireEvent.keyDown(valueOf("location")!, { key: "Enter" })
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Location" }), {
      key: "Escape",
    })
    expect(document.activeElement).toBe(valueOf("location"))
  })

  it("gives focus back to the cell after a save", async () => {
    renderSheet(record())
    fireEvent.keyDown(valueOf("location")!, { key: "Enter" })
    const box = screen.getByRole("textbox", { name: "Location" })
    fireEvent.change(box, { target: { value: "Porto" } })
    fireEvent.keyDown(box, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    await waitFor(() =>
      expect(document.activeElement).toBe(valueOf("location"))
    )
  })
})

describe("PropertySheet dates", () => {
  const dated = kind(TASK, {
    displayTemplate: "{name|title}",
    properties: {
      name: { type: "string" },
      due: { type: "datetime" },
      birthday: { type: "date" },
    },
  })
  const local = (y: number, m: number, d: number, h = 0, min = 0) =>
    new Date(y, m, d, h, min).toISOString().replace(".000Z", "Z")
  const withDates = () =>
    record({
      properties: {
        name: "Plan",
        due: local(2026, 9, 8, 11, 0),
        birthday: "1990-03-14",
      },
    })

  it("picks a date from the calendar in one click", async () => {
    renderSheet(withDates(), dated)
    fireEvent.click(valueOf("birthday")!)
    const grid = await screen.findByRole("grid", { name: "Birthday" })
    expect(
      within(grid).getByRole("button", { name: /14 March 1990/ })
    ).toBeTruthy()
    fireEvent.click(within(grid).getByRole("button", { name: /20 March 1990/ }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { birthday: "1990-03-20" },
      ifVersion: 7,
    })
  })

  it("walks the grid with the arrows and moves month on Page Down", async () => {
    renderSheet(withDates(), dated)
    fireEvent.click(valueOf("birthday")!)
    const grid = await screen.findByRole("grid", { name: "Birthday" })
    const day = within(grid).getByRole("button", { name: /14 March 1990/ })
    fireEvent.keyDown(day, { key: "ArrowRight" })
    expect(
      within(grid)
        .getByRole("button", { name: /15 March 1990/ })
        .getAttribute("tabindex")
    ).toBe("0")
    fireEvent.keyDown(grid, { key: "PageDown" })
    expect(screen.getByText("April 1990")).toBeTruthy()
  })

  it("sets a day and a typed time, and writes the instant on Save", async () => {
    renderSheet(withDates(), dated)
    fireEvent.click(valueOf("due")!)
    const grid = await screen.findByRole("grid", { name: "Due" })
    fireEvent.click(
      within(grid).getByRole("button", { name: /\b9 October 2026/ })
    )
    fireEvent.change(screen.getByRole("textbox", { name: "Time" }), {
      target: { value: "9pm" },
    })
    expect(wire.writes).toHaveLength(0)
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { due: local(2026, 9, 9, 21, 0) },
      ifVersion: 7,
    })
  })

  it("refuses a time it cannot read, and writes nothing", async () => {
    renderSheet(withDates(), dated)
    fireEvent.click(valueOf("due")!)
    const time = await screen.findByRole("textbox", { name: "Time" })
    fireEvent.change(time, { target: { value: "noon" } })
    fireEvent.keyDown(time, { key: "Enter" })
    expect(screen.getByText("Type a time like 09:30")).toBeTruthy()
    expect(wire.writes).toHaveLength(0)
  })

  it("clears an optional date", async () => {
    renderSheet(withDates(), dated)
    fireEvent.click(valueOf("birthday")!)
    fireEvent.click(await screen.findByRole("button", { name: "Clear" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { birthday: null },
      ifVersion: 7,
    })
  })
})

describe("PropertySheet lists", () => {
  const listed = () =>
    record({
      properties: {
        ...record().properties,
        tags: ["alpha", "beta"],
        emails: ["ada@example.com", "a.lovelace@example.com"],
        quotes: [
          "That brain of mine is something more than merely mortal, as time will show.",
          "short",
        ],
      },
    })
  const box = (name: string) =>
    screen.getByRole("textbox", { name }) as HTMLInputElement

  it("reads short tokens as chips and longer text one per line", () => {
    renderSheet(listed())
    const emails = row("emails").querySelector("[data-layout=chips]")!
    expect(emails.querySelectorAll("li")).toHaveLength(2)
    const quotes = row("quotes").querySelector("[data-layout=lines]")!
    expect(quotes.querySelectorAll("li")).toHaveLength(2)
  })

  it("edits one box per item and saves the whole list in one PATCH", async () => {
    renderSheet(listed())
    fireEvent.click(valueOf("tags")!)
    expect(box("Tags 1").value).toBe("alpha")
    // Enter adds an item after the one being typed in.
    fireEvent.keyDown(box("Tags 2"), { key: "Enter" })
    fireEvent.change(box("Tags 3"), { target: { value: "gamma" } })
    // Moving is an edit: gamma goes first.
    fireEvent.click(screen.getByRole("button", { name: "Move Tags 3 up" }))
    fireEvent.click(screen.getByRole("button", { name: "Move Tags 2 up" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { tags: ["gamma", "alpha", "beta"] },
      ifVersion: 7,
    })
  })

  it("removes an empty item on Backspace and splits a pasted list", async () => {
    renderSheet(listed())
    fireEvent.click(valueOf("tags")!)
    fireEvent.keyDown(box("Tags 2"), { key: "Enter" })
    fireEvent.keyDown(box("Tags 3"), { key: "Backspace" })
    expect(screen.queryByRole("textbox", { name: "Tags 3" })).toBeNull()
    fireEvent.paste(box("Tags 2"), {
      clipboardData: { getData: () => "delta\nepsilon, zeta\n" },
    })
    expect(box("Tags 2").value).toBe("betadelta")
    expect(box("Tags 3").value).toBe("epsilon, zeta")
    fireEvent.keyDown(box("Tags 3"), { key: "Enter", metaKey: true })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { tags: ["alpha", "betadelta", "epsilon, zeta"] },
      ifVersion: 7,
    })
  })

  it("writes an emptied list as [], not as a deletion", async () => {
    renderSheet(listed())
    fireEvent.click(valueOf("tags")!)
    fireEvent.click(screen.getByRole("button", { name: "Remove Tags 2" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove Tags 1" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { tags: [] },
      ifVersion: 7,
    })
  })

  it("edits a prose item in a box that keeps its lines", async () => {
    renderSheet(
      record({
        properties: {
          ...record().properties,
          steps: ["Draft\n\n- outline", "Review"],
        },
      })
    )
    fireEvent.click(valueOf("steps")!)
    const first = box("Steps 1")
    expect(first.tagName).toBe("TEXTAREA")
    expect(first.value).toBe("Draft\n\n- outline")
    // Enter is a new line inside the item, never a new item.
    fireEvent.keyDown(first, { key: "Enter" })
    expect(screen.queryByRole("textbox", { name: "Steps 3" })).toBeNull()
    fireEvent.change(box("Steps 2"), { target: { value: "Review\nand send" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { steps: ["Draft\n\n- outline", "Review\nand send"] },
      ifVersion: 7,
    })
  })

  it("leaves a choice item's keys to the choice itself", () => {
    renderSheet(
      record({
        properties: { ...record().properties, moods: ["calm", "busy"] },
      })
    )
    fireEvent.click(valueOf("moods")!)
    const first = screen.getByRole("combobox", { name: "Moods 1" })
    for (const key of ["ArrowDown", "ArrowUp", "Enter"]) {
      // Not prevented: the select opens, or steps its own value.
      expect(fireEvent.keyDown(first, { key })).toBe(true)
    }
    expect(screen.queryByRole("combobox", { name: "Moods 3" })).toBeNull()
  })

  it("says which item its datatype refuses, and writes nothing", async () => {
    renderSheet(listed())
    fireEvent.click(valueOf("emails")!)
    fireEvent.change(screen.getByLabelText("Emails 2"), {
      target: { value: "not an address" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect((await screen.findByRole("alert")).textContent).toMatch(/^Item 2/)
    expect(wire.writes).toHaveLength(0)
  })
})

describe("PropertySheet references with link data", () => {
  const linked = () =>
    record({
      properties: {
        ...record().properties,
        members: [
          { ref: `${PERSON}/ada`, role: "lead" },
          { ref: `${PERSON}/grace` },
          { ref: `${PERSON}/alan`, role: "reviewer" },
        ],
      },
    })

  it("reads each item's link data beside it", () => {
    renderSheet(linked())
    expect(row("members").textContent).toMatch(/Role: lead/)
  })

  it("keeps the link data of every item it did not touch", async () => {
    renderSheet(linked())
    fireEvent.click(valueOf("members")!)
    // The editor says what each membership carries, under its record.
    const links = [
      ...document.querySelectorAll("[data-slot=reference-link]"),
    ].map((el) => el.textContent)
    expect(links).toEqual(["Role: lead", "Role: reviewer"])
    fireEvent.click(screen.getByRole("button", { name: "Remove Members 2" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: {
        members: [
          { ref: `${PERSON}/ada`, role: "lead" },
          { ref: `${PERSON}/alan`, role: "reviewer" },
        ],
      },
      ifVersion: 7,
    })
  })

  it("writes nothing when the list is saved as it was", async () => {
    renderSheet(linked())
    fireEvent.click(valueOf("members")!)
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await new Promise((r) => setTimeout(r, 0))
    expect(wire.writes).toHaveLength(0)
  })
})

describe("OwnershipChip", () => {
  const held = (meta: SubstrateRecord["propertyMeta"]) =>
    record({ propertyMeta: meta })

  it("stays quiet on the owner's own value, the page's default", () => {
    renderSheet(held({ location: { manager: "console", tier: "owner" } }))
    expect(row("location").querySelector("[data-slot=owner-chip]")).toBeNull()
  })

  it("says You on every row when asked who holds each value", () => {
    renderSheet(
      held({ location: { manager: "console", tier: "owner" } }),
      task,
      false,
      true
    )
    const chip = row("location").querySelector("[data-slot=owner-chip]")!
    expect(chip.getAttribute("data-holder")).toBe("you")
    expect(chip.textContent).toBe("You")
  })

  it("sits in its own column, never inside the value it describes", () => {
    renderSheet(record(), task, false, true)
    const cell = row("location").querySelector("[data-slot=provenance]")!
    expect(cell.querySelector("[data-slot=owner-chip]")).not.toBeNull()
    expect(
      valueOf("location")!.querySelector("[data-slot=owner-chip]")
    ).toBeNull()
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

  it("names the mapping a synced value came through", () => {
    renderSheet(
      record({
        linkedFrom: [
          {
            ref: `${CONTACT}/c1`,
            kind: CONTACT,
            property: "person",
            mapping: "ada.example.com/people/googlecontactperson",
          },
        ],
        propertyMeta: {
          location: {
            manager: GOOGLE_SYNC,
            tier: "machine",
            source: `${CONTACT}/c1`,
          },
        },
      })
    )
    fireEvent.click(
      screen.getByRole("button", { name: "Where Location comes from" })
    )
    const detail = document.querySelector(
      "[data-slot=ownership-detail]"
    ) as HTMLElement
    expect(detail.textContent).toContain("Linked throughContact → Task")
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
    const pill = row("location").querySelector(
      "[data-slot=pill][data-tone=warn]"
    )!
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

describe("PropertySheet under a live refresh", () => {
  // Another tab or an agent moved the record while an editor was open: the
  // page re-reads it (version 8), and the edit must still assert the version
  // it began from (7), so the server refuses it instead of it silently
  // overwriting the other write.
  const moved = (over: Partial<SubstrateRecord> = {}) =>
    record({
      version: 8,
      properties: { ...record().properties, location: "Porto" },
      ...over,
    })
  const pinned = async () => {
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect((wire.writes[0].body as { ifVersion: number }).ifVersion).toBe(7)
  }

  it("keeps a text edit's version, and says the conflict", async () => {
    const { refresh } = renderSheet(record())
    fireEvent.click(valueOf("location")!)
    const box = screen.getByRole("textbox", { name: "Location" })
    fireEvent.change(box, { target: { value: "Madrid" } })
    wire.refuse = { code: "conflict", message: "version moved" }
    refresh(moved())
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Location" }), {
      key: "Enter",
    })
    await pinned()
    expect((await screen.findByRole("alert")).textContent).toMatch(
      /changed since you opened it/
    )
  })

  it("keeps an enum pick's version", async () => {
    const { refresh } = renderSheet(record())
    fireEvent.click(valueOf("priority")!)
    refresh(moved())
    fireEvent.click(screen.getByRole("option", { name: "Low" }))
    await pinned()
  })

  it("keeps a list edit's version", async () => {
    const tagged = (over: Partial<SubstrateRecord> = {}) =>
      record({
        properties: { ...record().properties, tags: ["alpha"] },
        ...over,
      })
    const { refresh } = renderSheet(tagged())
    fireEvent.click(valueOf("tags")!)
    fireEvent.change(screen.getByRole("textbox", { name: "Tags 1" }), {
      target: { value: "beta" },
    })
    refresh(tagged({ version: 8 }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await pinned()
  })

  it("keeps a date edit's version", async () => {
    const dated = (over: Partial<SubstrateRecord> = {}) =>
      record({
        properties: { ...record().properties, due: "2026-10-08T10:00:00Z" },
        ...over,
      })
    const { refresh } = renderSheet(dated())
    fireEvent.click(valueOf("due")!)
    const grid = await screen.findByRole("grid", { name: "Due" })
    fireEvent.click(
      within(grid).getByRole("button", { name: /\b9 October 2026/ })
    )
    refresh(dated({ version: 8 }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await pinned()
  })

  it("writes a later edit against the version then on the page", async () => {
    const { refresh } = renderSheet(record())
    refresh(moved())
    fireEvent.click(valueOf("location")!)
    fireEvent.change(screen.getByRole("textbox", { name: "Location" }), {
      target: { value: "Madrid" },
    })
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Location" }), {
      key: "Enter",
    })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect((wire.writes[0].body as { ifVersion: number }).ifVersion).toBe(8)
  })

  it("keeps the version a source's value was offered at", async () => {
    const offered = (over: Partial<SubstrateRecord> = {}) =>
      record({
        propertyMeta: {
          location: {
            manager: "console",
            tier: "owner",
            alternatives: [
              { actor: GOOGLE_SYNC, value: "Lisboa", updatedAt: "" },
            ],
          },
        },
        ...over,
      })
    const { refresh } = renderSheet(offered())
    fireEvent.click(
      screen.getByRole("button", { name: "Where Location comes from" })
    )
    fireEvent.click(screen.getByRole("button", { name: "Use Google’s" }))
    const dialog = await screen.findByRole("dialog")
    refresh(offered({ version: 8 }))
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Use Google’s" })
    )
    await pinned()
  })
})
