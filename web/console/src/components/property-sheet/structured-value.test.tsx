// @vitest-environment jsdom
/** A structured property's promises on the record page: a list of flat
 * objects reads as a table and a list of items holding lists as stacked
 * items; a click on any value, however deep, edits it where it sits, with no
 * editor of the whole property opening anywhere; every write is the whole
 * property with only the edited thing changed, asserting the version the
 * page read; an emptied value is taken out of its item, never written as
 * null; a new item is written once it holds what an item must; and a
 * provider's copy offers nothing to edit. */

import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render as renderUI,
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
      return Promise.resolve({ records: [], head: 0, generation: "g" })
    }),
  }
})

import { DeclaredValue } from "./property-value"
import { PropertySheet } from "./property-sheet"
import { ShippedUnits } from "@/components/units-provider.test-fixtures"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { propSpecs } from "@/lib/record-schema"

const render = (ui: Parameters<typeof renderUI>[0]) =>
  renderUI(ui, { wrapper: ShippedUnits })

const ROUTINE = "ada.example.com/gym/routine"
const EXERCISE = "ada.example.com/gym/exercise"

const routine: KindInfo = {
  identity: ROUTINE,
  name: "routine",
  authority: "ada.example.com",
  package: "gym",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    displayTemplate: "{name}",
    properties: {
      name: { type: "string" },
      profile: {
        type: "object",
        fields: {
          height: { type: "int", required: true },
          unit: { type: "string" },
        },
      },
      meta: {
        type: "object",
        fields: { coach: { type: "string" }, level: { type: "int" } },
      },
      blocks: {
        type: "object",
        repeated: true,
        fields: {
          title: { type: "string", required: true },
          steps: {
            type: "object",
            repeated: true,
            required: true,
            fields: { text: { type: "string", required: true } },
          },
        },
      },
      groups: {
        type: "object",
        repeated: true,
        fields: {
          key: { type: "string", required: true },
          pairing: { type: "enum", values: ["antagonist", "unrelated"] },
          rounds: { type: "int", min: 1 },
        },
      },
      slots: {
        type: "object",
        repeated: true,
        fields: {
          key: { type: "string", required: true },
          exercise: { type: "reference", kind: EXERCISE, required: true },
          notes: { type: "text" },
          cues: { type: "string", keyed: true },
          flags: { type: "bool", repeated: true },
          sets: {
            type: "object",
            repeated: true,
            fields: {
              key: { type: "string", required: true },
              role: {
                type: "enum",
                default: "working",
                values: ["warmup", "working"],
              },
              repsMin: { type: "int", min: 0 },
              repsMax: { type: "int", min: 0 },
              tempo: { type: "string" },
            },
          },
        },
      },
    },
  },
}

const stored = () => ({
  name: "Floor first",
  title: "Floor first",
  groups: [
    { key: "a", pairing: "unrelated", rounds: 2 },
    { key: "b", pairing: "antagonist", rounds: 3 },
  ],
  slots: [
    {
      key: "bridge",
      exercise: { ref: `${EXERCISE}/glute-bridge` },
      notes: "Squeeze at the top.",
      sets: [
        { key: "s1", role: "working", repsMin: 12, repsMax: 15, odd: "kept" },
        { key: "s2", role: "working", repsMin: 12, repsMax: 15 },
      ],
    },
    {
      key: "walk",
      exercise: { ref: `${EXERCISE}/walking` },
      cues: { pace: "easy" },
      flags: [true, false],
      sets: [],
    },
  ],
  meta: { coach: "Ada" },
})

const record = (over: Partial<SubstrateRecord> = {}): SubstrateRecord => ({
  id: "r1",
  kind: ROUTINE,
  properties: stored(),
  labels: {},
  version: 7,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-10T00:00:00Z",
  ...over,
})

function renderSheet(r: SubstrateRecord = record(), readOnly = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(["registry", "kinds"], [routine])
  return render(
    <QueryClientProvider client={client}>
      <PropertySheet
        record={r}
        kind={routine}
        kinds={[routine]}
        readOnly={readOnly}
      />
    </QueryClientProvider>
  )
}

const section = (name: string) =>
  document.querySelector<HTMLElement>(
    `[data-property="${name}"] > [role="group"]`
  )!
/** The property's own item `n`, not an item of a list one of its items
 * holds. */
const item = (name: string, n: number) =>
  [...section(name).querySelectorAll<HTMLElement>(`[data-item="${n}"]`)].find(
    (el) => !el.parentElement?.closest("[data-item]")
  )!
/** Opens the sheet's fold of the properties that hold nothing. */
const showEmpty = () =>
  fireEvent.click(screen.getByRole("button", { name: /empty/ }))
/** The value a click edits, by its label. */
const value = (scope: HTMLElement, label: string) =>
  within(scope).getByRole("button", { name: new RegExp(`^${label}\\b`) })
/** The one write's value of `property`. */
const written = (property: string) =>
  (wire.writes[0].body as { properties: Record<string, unknown> }).properties[
    property
  ]

beforeEach(() => {
  wire.writes = []
})
afterEach(cleanup)

describe("structured values, read", () => {
  it("reads a list of flat objects as a table, its fields as columns", () => {
    renderSheet()
    const table = within(section("groups")).getByRole("table")
    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((h) => h.textContent)
    ).toEqual(["Key", "Pairing", "Rounds", "Actions"])
    expect(item("groups", 2).textContent).toContain("Antagonist")
  })

  it("stacks items that hold a list, each named, its list a table under it", () => {
    renderSheet()
    const bridge = item("slots", 1)
    expect(bridge.tagName).toBe("LI")
    expect(bridge.textContent).toContain("Exercise")
    expect(bridge.textContent).toContain("Squeeze at the top.")
    const sets = within(bridge).getByRole("table")
    // A range's two ends read as one value.
    expect(within(sets).getAllByRole("row")[1].textContent).toContain("12–15")
  })

  it("opens no editor of the whole property: there is no panel to open", () => {
    renderSheet()
    fireEvent.click(section("slots"))
    expect(document.querySelector('[data-slot="list-editor"]')).toBeNull()
    expect(screen.queryByRole("button", { name: /Add another/ })).toBeNull()
  })

  it("offers nothing to edit on a provider's copy", () => {
    renderSheet(record(), true)
    expect(
      within(section("slots")).queryAllByRole("button", { name: /edit$/ })
    ).toEqual([])
    expect(screen.queryByRole("button", { name: /^Add an item/ })).toBeNull()
    expect(screen.queryByRole("button", { name: /^Actions for/ })).toBeNull()
  })

  it("reads a keyed map as each key beside its value", () => {
    const [spec] = propSpecs({
      ...routine,
      definition: { properties: { tags: { type: "string", keyed: true } } },
    })
    render(<DeclaredValue spec={spec} value={{ b: "two", a: "one" }} />)
    expect(
      [...document.querySelectorAll("dt")].map((dt) => dt.textContent)
    ).toEqual(["a", "b"])
  })
})

describe("structured values, edited in place", () => {
  it("writes the whole property with one nested value changed, asserting the version read", async () => {
    renderSheet()
    const sets = within(item("slots", 1)).getByRole("table")
    fireEvent.click(value(within(sets).getAllByRole("row")[2], "Key"))
    const box = screen.getByRole("textbox", { name: "Key" })
    fireEvent.change(box, { target: { value: "s2b" } })
    fireEvent.keyDown(box, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    const expected = stored().slots
    expected[0].sets[1].key = "s2b"
    expect(wire.writes[0].body).toEqual({
      properties: { slots: expected },
      ifVersion: 7,
    })
  })

  it("writes a range's two ends in one write", async () => {
    renderSheet()
    const sets = within(item("slots", 1)).getByRole("table")
    fireEvent.click(value(within(sets).getAllByRole("row")[1], "Reps"))
    const lo = screen.getByRole("textbox", { name: "Reps min" })
    const hi = screen.getByRole("textbox", { name: "Reps max" })
    fireEvent.change(lo, { target: { value: "8" } })
    fireEvent.change(hi, { target: { value: "10" } })
    fireEvent.keyDown(hi, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    const expected = stored().slots
    expected[0].sets[0] = { ...expected[0].sets[0], repsMin: 8, repsMax: 10 }
    expect(written("slots")).toEqual(expected)
  })

  it("takes an emptied value out of its item, never writing null", async () => {
    renderSheet()
    fireEvent.click(value(item("groups", 1), "Rounds"))
    const box = screen.getByRole("spinbutton", { name: "Rounds" })
    fireEvent.change(box, { target: { value: "" } })
    fireEvent.keyDown(box, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(written("groups")).toEqual([
      { key: "a", pairing: "unrelated" },
      { key: "b", pairing: "antagonist", rounds: 3 },
    ])
  })

  it("refuses to empty a value every item holds, and writes nothing", async () => {
    renderSheet()
    fireEvent.click(value(item("groups", 1), "Key"))
    const box = screen.getByRole("textbox", { name: "Key" })
    fireEvent.change(box, { target: { value: "" } })
    fireEvent.keyDown(box, { key: "Enter" })
    expect(
      (await within(section("groups")).findByRole("alert")).textContent
    ).toContain("Key")
    expect(wire.writes).toEqual([])
  })

  it("picks an enum from its labels", async () => {
    renderSheet()
    fireEvent.click(value(item("groups", 1), "Pairing"))
    fireEvent.click(await screen.findByRole("option", { name: "Antagonist" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(written("groups")).toEqual([
      { key: "a", pairing: "antagonist", rounds: 2 },
      { key: "b", pairing: "antagonist", rounds: 3 },
    ])
  })
})

describe("structured values, items added, moved and removed", () => {
  it("asks for what a new item must hold, and writes it once it does", async () => {
    renderSheet()
    const bridge = item("slots", 1)
    fireEvent.click(
      within(bridge).getByRole("button", { name: "Add an item to Sets" })
    )
    const draft = document.querySelector<HTMLElement>(
      '[data-slot="structured-draft"]'
    )!
    fireEvent.click(within(draft).getByRole("button", { name: "Add" }))
    expect((await within(draft).findByRole("alert")).textContent).toContain(
      "Key"
    )
    expect(wire.writes).toEqual([])
    fireEvent.change(draft.querySelector('input[id$="-key"]')!, {
      target: { value: "s3" },
    })
    fireEvent.click(within(draft).getByRole("button", { name: "Add" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    const expected = stored().slots
    // The declared default rides in, as the server would store it anyway.
    expected[0].sets.push({ key: "s3", role: "working" } as never)
    expect(written("slots")).toEqual(expected)
  })

  it("moves an item with one write of the reordered list", async () => {
    renderSheet()
    fireEvent.click(
      within(item("groups", 1)).getByRole("button", { name: /^Actions for/ })
    )
    fireEvent.click(await screen.findByRole("menuitem", { name: "Move down" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    const [a, b] = stored().groups
    expect(written("groups")).toEqual([b, a])
  })

  it("asks before removing, and an emptied list is written empty", async () => {
    const one = record({
      properties: { ...stored(), groups: [{ key: "a", rounds: 2 }] },
    })
    renderSheet(one)
    fireEvent.click(
      within(item("groups", 1)).getByRole("button", { name: /^Actions for/ })
    )
    fireEvent.click(await screen.findByRole("menuitem", { name: "Remove" }))
    expect(wire.writes).toEqual([])
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Remove item 1 (a) from Groups?")
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(written("groups")).toEqual([])
  })

  it("names what a removed item takes with it", async () => {
    renderSheet()
    fireEvent.click(
      within(item("slots", 1)).getAllByRole("button", {
        name: /^Actions for/,
      })[0]
    )
    fireEvent.click(await screen.findByRole("menuitem", { name: "Remove" }))
    expect((await screen.findByRole("dialog")).textContent).toContain(
      "2 in Sets"
    )
  })
})

describe("structured values, as the review found them", () => {
  it("shows a key no field declares, read as stored", () => {
    renderSheet()
    const sets = within(item("slots", 1)).getByRole("table")
    expect(
      within(sets)
        .getAllByRole("columnheader")
        .map((h) => h.textContent)
    ).toContain("odd")
    expect(within(sets).getAllByRole("row")[1].textContent).toContain("kept")
  })

  it("names a value's button by its label and its value", () => {
    renderSheet()
    const sets = within(item("slots", 1)).getByRole("table")
    const row = within(sets).getAllByRole("row")[1]
    expect(
      within(row).getByRole("button", { name: "Reps: 12–15, edit" })
    ).toBeTruthy()
    expect(
      within(row).getByRole("button", { name: "Role: Working, edit" })
    ).toBeTruthy()
  })

  it("reads a list of flags as a list, never one switch", () => {
    renderSheet()
    expect(within(item("slots", 2)).queryByRole("switch")).toBeNull()
    expect(value(item("slots", 2), "Flags").textContent).toMatch(/Yes.*No/)
  })

  it("edits a keyed map an item holds from its heading", () => {
    renderSheet()
    fireEvent.click(
      within(item("slots", 2)).getByRole("button", { name: "Edit Cues" })
    )
    expect(
      within(item("slots", 2)).getByRole("button", { name: "Save" })
    ).toBeTruthy()
  })

  it("sets a field a single object does not hold, from its heading", async () => {
    renderSheet()
    fireEvent.click(
      within(section("meta")).getByRole("button", {
        name: "Set a field of Meta",
      })
    )
    fireEvent.click(await screen.findByRole("menuitem", { name: "Level" }))
    const box = await screen.findByRole("spinbutton", { name: "Level" })
    fireEvent.change(box, { target: { value: "3" } })
    fireEvent.keyDown(box, { key: "Enter" })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(written("meta")).toEqual({ coach: "Ada", level: 3 })
  })

  it("fills in an object that must hold a field as one write", async () => {
    renderSheet()
    showEmpty()
    fireEvent.click(
      within(section("profile")).getByRole("button", {
        name: "Fill in Profile",
      })
    )
    const draft = document.querySelector<HTMLElement>(
      '[data-slot="structured-draft"]'
    )!
    fireEvent.change(draft.querySelector('input[id$="-height"]')!, {
      target: { value: "180" },
    })
    fireEvent.click(within(draft).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(written("profile")).toEqual({ height: 180 })
  })

  it("asks a new item for a list it must hold", () => {
    renderSheet()
    showEmpty()
    fireEvent.click(
      screen.getByRole("button", { name: "Add an item to Blocks" })
    )
    const draft = document.querySelector<HTMLElement>(
      '[data-slot="structured-draft"]'
    )!
    expect(
      [...draft.querySelectorAll("label")].map((l) => l.textContent)
    ).toEqual(["Title *", "Steps *"])
  })

  it("writes a new item against the version its form opened on", async () => {
    const view = renderSheet()
    const bridge = item("slots", 1)
    fireEvent.click(
      within(bridge).getByRole("button", { name: "Add an item to Sets" })
    )
    // Another writer reorders the slots while the form is open.
    const moved = stored()
    moved.slots.reverse()
    const client = new QueryClient()
    client.setQueryData(["registry", "kinds"], [routine])
    view.rerender(
      <QueryClientProvider client={client}>
        <PropertySheet
          record={record({ properties: moved, version: 8 })}
          kind={routine}
          kinds={[routine]}
        />
      </QueryClientProvider>
    )
    const draft = document.querySelector<HTMLElement>(
      '[data-slot="structured-draft"]'
    )!
    fireEvent.change(draft.querySelector('input[id$="-key"]')!, {
      target: { value: "s3" },
    })
    fireEvent.click(within(draft).getByRole("button", { name: "Add" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    const body = wire.writes[0].body as {
      properties: { slots: Array<{ key: string; sets: unknown[] }> }
      ifVersion: number
    }
    // The write asserts the version the form opened on, so the server
    // refuses it rather than adding the set to the slot that moved in.
    expect(body.ifVersion).toBe(7)
    expect(body.properties.slots[0].key).toBe("bridge")
  })

  it("gives focus back to the control that opened a form it closes", async () => {
    renderSheet()
    const add = within(item("slots", 1)).getByRole("button", {
      name: "Add an item to Sets",
    })
    fireEvent.click(add)
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(document.activeElement).toBe(add))
  })
})
