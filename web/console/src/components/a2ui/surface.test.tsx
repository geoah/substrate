// @vitest-environment jsdom
/** The A2UI renderer over a small surface: a Column holding a TextField and
 * a List whose template draws one Button per item. Typing writes the bound
 * path in the local data model, with no network; a tap resolves the
 * button's context and its userMessage in its item's scope and hands the
 * host the event; a check holds a button back until it passes; Enter in a
 * field presses the primary button after it; a single choice walks with
 * the arrow keys; an image holds its box while it is fetched; a button
 * holds phrasing content alone; and what the catalog does not know is
 * said, not thrown. */

import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SurfaceStore } from "@/lib/a2ui/store"
import type { A2UIMessage } from "@/lib/api/types"

import { A2UISurface } from "./surface"

const CATALOG =
  "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"

function surface(
  components: Record<string, unknown>[],
  dataModel: Record<string, unknown>
): A2UIMessage {
  return {
    version: "1.0",
    createSurface: {
      surfaceId: "applet",
      catalogId: CATALOG,
      components,
      dataModel,
    },
  }
}

const COMPONENTS: Record<string, unknown>[] = [
  { id: "root", component: "Column", children: ["name", "list", "save"] },
  {
    id: "name",
    component: "TextField",
    label: "Name",
    value: { "@path": "/ui/name" },
    checks: [
      {
        condition: {
          "@call": "required",
          args: { value: { "@path": "/ui/name" } },
        },
        message: "A name is required.",
      },
    ],
  },
  {
    id: "list",
    component: "List",
    children: { path: "/items", componentId: "item" },
  },
  {
    id: "item",
    component: "Button",
    variant: "borderless",
    child: "itemText",
    action: {
      event: {
        name: "pick",
        context: {
          id: { "@path": "id" },
          position: { "@call": "@index", args: { offset: 1 } },
          name: { "@path": "/ui/name" },
        },
      },
    },
  },
  {
    id: "itemText",
    component: "Text",
    text: {
      "@call": "formatString",
      args: { value: "**${label}** for ${/ui/name}" },
    },
  },
  {
    id: "save",
    component: "Button",
    variant: "primary",
    child: "saveText",
    checks: [
      {
        condition: {
          "@call": "required",
          args: { value: { "@path": "/ui/name" } },
        },
        message: "A name is required.",
      },
    ],
    action: { event: { name: "save" } },
  },
  { id: "saveText", component: "Text", text: "Save" },
]

function mount(
  components = COMPONENTS,
  dataModel: Record<string, unknown> = {
    ui: { name: "" },
    items: [
      { id: "a", label: "Alpha" },
      { id: "b", label: "Beta" },
    ],
  },
  { destructive }: { destructive?: string[] } = {}
) {
  const store = new SurfaceStore()
  store.process([surface(components, dataModel)])
  const onAction = vi.fn()
  const onProblem = vi.fn()
  render(
    <A2UISurface
      store={store}
      surfaceId="applet"
      onAction={onAction}
      onProblem={onProblem}
      destructive={destructive}
    />
  )
  return { store, onAction, onProblem }
}

afterEach(cleanup)

describe("an A2UI surface", () => {
  it("binds an input two ways, in the local data model alone", () => {
    const fetch = vi.fn()
    vi.stubGlobal("fetch", fetch)
    const { store } = mount()
    const input = screen.getByLabelText<HTMLInputElement>("Name")
    fireEvent.change(input, { target: { value: "Ada" } })
    expect(store.surface("applet")!.dataModel.ui).toEqual({ name: "Ada" })
    expect(input.value).toBe("Ada")
    // The template's text reads the new value through its absolute binding.
    expect(screen.getByRole("button", { name: "Alpha for Ada" })).toBeTruthy()
    // A model change from elsewhere shows in the input.
    act(
      () =>
        void store.process([
          {
            version: "1.0",
            updateDataModel: {
              surfaceId: "applet",
              path: "/ui/name",
              value: "Grace",
            },
          },
        ])
    )
    expect(input.value).toBe("Grace")
    expect(fetch).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it("draws a template once per item and resolves a tap's context in its scope", () => {
    const { onAction } = mount()
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Ada" },
    })
    const rows = screen.getAllByRole("listitem")
    expect(rows).toHaveLength(2)
    fireEvent.click(screen.getByRole("button", { name: "Beta for Ada" }))
    expect(onAction).toHaveBeenCalledWith({
      name: "pick",
      context: { id: "b", position: 2, name: "Ada" },
      sourceComponentId: "item",
    })
  })

  it("holds a button back while a check fails, and says it once touched", () => {
    const { onAction } = mount()
    const save = screen.getByRole<HTMLButtonElement>("button", { name: "Save" })
    expect(save.disabled).toBe(true)
    // Untouched, the input does not scold.
    expect(screen.queryByText("A name is required.")).toBeNull()
    const input = screen.getByLabelText("Name")
    fireEvent.change(input, { target: { value: "x" } })
    fireEvent.change(input, { target: { value: "" } })
    expect(screen.getByText("A name is required.")).toBeTruthy()
    fireEvent.change(input, { target: { value: "Ada" } })
    expect(save.disabled).toBe(false)
    fireEvent.click(save)
    expect(onAction).toHaveBeenCalledWith({
      name: "save",
      context: {},
      sourceComponentId: "save",
    })
  })

  it("draws nothing for what it does not know, and says so", () => {
    const { onProblem } = mount(
      [
        { id: "root", component: "Column", children: ["ok", "odd", "ghost"] },
        { id: "ok", component: "Text", text: "Still here" },
        { id: "odd", component: "Marquee", text: "no" },
      ],
      {}
    )
    expect(screen.getByText("Still here")).toBeTruthy()
    expect(onProblem).toHaveBeenCalledWith(
      "odd: “Marquee” is not a component of the basic catalog"
    )
    expect(onProblem).toHaveBeenCalledWith("no component with the id “ghost”")
  })

  it("writes a choice, a checkbox and a number as their own types", () => {
    const { store } = mount(
      [
        {
          id: "root",
          component: "Column",
          children: ["status", "done", "qty"],
        },
        {
          id: "status",
          component: "ChoicePicker",
          label: "Status",
          variant: "multipleSelection",
          displayStyle: "chips",
          value: { "@path": "/ui/status" },
          options: [
            { label: "Reading", value: "reading" },
            { label: "Read", value: "read" },
          ],
        },
        {
          id: "done",
          component: "CheckBox",
          label: "Done",
          value: { "@path": "/ui/done" },
        },
        {
          id: "qty",
          component: "TextField",
          label: "Quantity",
          variant: "number",
          value: { "@path": "/ui/qty" },
        },
      ],
      { ui: { status: [] } }
    )
    fireEvent.click(screen.getByRole("button", { name: "Read" }))
    fireEvent.click(screen.getByRole("button", { name: "Reading" }))
    expect(store.surface("applet")!.dataModel.ui).toMatchObject({
      status: ["reading", "read"],
    })
    fireEvent.click(screen.getByLabelText("Done"))
    fireEvent.change(screen.getByLabelText("Quantity"), {
      target: { value: "12" },
    })
    expect(store.surface("applet")!.dataModel.ui).toMatchObject({
      done: true,
      qty: 12,
    })
    fireEvent.change(screen.getByLabelText("Quantity"), {
      target: { value: "1." },
    })
    expect(store.surface("applet")!.dataModel.ui).toMatchObject({ qty: "1." })
  })

  it("opens only a web address from a button", () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null)
    const { onProblem } = mount(
      [
        { id: "root", component: "Row", children: ["web", "script"] },
        {
          id: "web",
          component: "Button",
          child: "webText",
          action: {
            functionCall: {
              "@call": "openUrl",
              args: { url: "https://example.com/a" },
            },
          },
        },
        { id: "webText", component: "Text", text: "Web" },
        {
          id: "script",
          component: "Button",
          child: "scriptText",
          action: {
            functionCall: {
              "@call": "openUrl",
              args: { url: "javascript:alert(1)" },
            },
          },
        },
        { id: "scriptText", component: "Text", text: "Script" },
      ],
      {}
    )
    fireEvent.click(screen.getByRole("button", { name: "Web" }))
    expect(open).toHaveBeenCalledWith(
      "https://example.com/a",
      "_blank",
      "noopener,noreferrer"
    )
    fireEvent.click(screen.getByRole("button", { name: "Script" }))
    expect(open).toHaveBeenCalledTimes(1)
    expect(onProblem).toHaveBeenCalledWith(
      "script: only a web address can be opened"
    )
    open.mockRestore()
  })

  it("resolves an event's userMessage at the tap, like its context", () => {
    const { onAction } = mount(
      [
        {
          id: "root",
          component: "List",
          children: { path: "/items", componentId: "item" },
        },
        {
          id: "item",
          component: "Button",
          child: "itemText",
          action: {
            event: {
              name: "pick",
              userMessage: {
                "@call": "formatString",
                args: { value: "Picked ${label}" },
              },
            },
          },
        },
        { id: "itemText", component: "Text", text: { "@path": "label" } },
      ],
      { items: [{ id: "a", label: "Alpha" }] }
    )
    fireEvent.click(screen.getByRole("button", { name: "Alpha" }))
    expect(onAction).toHaveBeenCalledWith({
      name: "pick",
      context: {},
      sourceComponentId: "item",
      userMessage: "Picked Alpha",
    })
  })

  it("presses the primary button after a field on Enter, and no other", () => {
    const { onAction } = mount(
      [
        {
          id: "root",
          component: "Column",
          children: ["before", "q", "go", "after", "notes"],
        },
        {
          id: "before",
          component: "Button",
          variant: "primary",
          child: "beforeText",
          action: { event: { name: "before" } },
        },
        { id: "beforeText", component: "Text", text: "Before" },
        {
          id: "q",
          component: "TextField",
          label: "Query",
          value: { "@path": "/ui/q" },
        },
        {
          id: "go",
          component: "Button",
          variant: "primary",
          child: "goText",
          action: {
            event: { name: "go", context: { q: { "@path": "/ui/q" } } },
          },
        },
        { id: "goText", component: "Text", text: "Go" },
        {
          id: "after",
          component: "Button",
          variant: "primary",
          child: "afterText",
          action: { event: { name: "after" } },
        },
        { id: "afterText", component: "Text", text: "After" },
        {
          id: "notes",
          component: "TextField",
          label: "Notes",
          value: { "@path": "/ui/notes" },
        },
      ],
      { ui: { q: "" } }
    )
    const q = screen.getByLabelText("Query")
    fireEvent.change(q, { target: { value: "dune" } })
    fireEvent.keyDown(q, { key: "Enter" })
    expect(onAction).toHaveBeenCalledTimes(1)
    expect(onAction).toHaveBeenCalledWith({
      name: "go",
      context: { q: "dune" },
      sourceComponentId: "go",
    })
    // No primary button after it: Enter does nothing.
    fireEvent.keyDown(screen.getByLabelText("Notes"), { key: "Enter" })
    expect(onAction).toHaveBeenCalledTimes(1)
  })

  it("holds Enter while the primary button after a field is held back", () => {
    const { onAction } = mount()
    const name = screen.getByLabelText("Name")
    fireEvent.keyDown(name, { key: "Enter" })
    expect(onAction).not.toHaveBeenCalled()
    fireEvent.change(name, { target: { value: "Ada" } })
    fireEvent.keyDown(name, { key: "Enter" })
    expect(onAction).toHaveBeenCalledWith({
      name: "save",
      context: {},
      sourceComponentId: "save",
    })
  })

  it("walks a single choice's chips with the arrow keys", () => {
    const { store } = mount(
      [
        {
          id: "root",
          component: "ChoicePicker",
          label: "Shelf",
          displayStyle: "chips",
          value: { "@path": "/ui/shelf" },
          options: [
            { label: "Reading", value: "reading" },
            { label: "Want", value: "want" },
            { label: "Read", value: "read" },
          ],
        },
      ],
      { ui: { shelf: ["want"] } }
    )
    const radio = (name: string) => screen.getByRole("radio", { name })
    // One Tab stop: the chosen chip.
    expect(screen.getAllByRole("radio").map((r) => r.tabIndex)).toEqual([
      -1, 0, -1,
    ])
    radio("Want").focus()
    fireEvent.keyDown(radio("Want"), { key: "ArrowRight" })
    expect(store.surface("applet")!.dataModel.ui).toEqual({ shelf: ["read"] })
    expect(document.activeElement).toBe(radio("Read"))
    expect(radio("Read").getAttribute("aria-checked")).toBe("true")
    expect(radio("Read").tabIndex).toBe(0)
    // The ends wrap.
    fireEvent.keyDown(radio("Read"), { key: "ArrowDown" })
    expect(document.activeElement).toBe(radio("Reading"))
    expect(store.surface("applet")!.dataModel.ui).toEqual({
      shelf: ["reading"],
    })
    fireEvent.keyDown(radio("Reading"), { key: "ArrowLeft" })
    expect(document.activeElement).toBe(radio("Read"))
    fireEvent.keyDown(radio("Read"), { key: "Home" })
    expect(document.activeElement).toBe(radio("Reading"))
    // Any other key is the button's own.
    fireEvent.keyDown(radio("Reading"), { key: "a" })
    expect(store.surface("applet")!.dataModel.ui).toEqual({
      shelf: ["reading"],
    })
  })

  it("holds an image's box while it is fetched, and draws the image in it", async () => {
    let respond: (res: Response) => void = () => {}
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise<Response>((resolve) => {
            respond = resolve
          })
      )
    )
    const createObjectURL = vi.fn(() => "blob:cover")
    Object.assign(URL, { createObjectURL })
    mount(
      [
        { id: "root", component: "Column", children: ["cover", "svg"] },
        {
          id: "cover",
          component: "Image",
          url: "https://covers.example.com/dune.jpg",
          variant: "smallFeature",
          description: "Dune’s cover",
        },
        // Never SVG, as the proxy serves none.
        { id: "svg", component: "Image", url: "data:image/svg+xml,<svg/>" },
      ],
      {}
    )
    const box = document.querySelector("[data-slot=a2ui-image-pending]")
    expect(box?.className).toContain("bg-muted")
    expect(box?.className).toContain("size-20")
    respond(new Response(new Blob(["png"]), { status: 200 }))
    const img = await screen.findByRole("img", { name: "Dune’s cover" })
    expect(img.getAttribute("src")).toBe("blob:cover")
    expect(img.className).toContain("size-20")
    expect(document.querySelector("[data-slot=a2ui-image-pending]")).toBeNull()
    expect(document.querySelectorAll("img")).toHaveLength(1)
    vi.unstubAllGlobals()
  })

  it("drops an image's box only when the proxy cannot serve it", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("", { status: 415 }))
    )
    mount(
      [
        {
          id: "root",
          component: "Image",
          url: "https://covers.example.com/missing.jpg",
          variant: "header",
        },
      ],
      {}
    )
    expect(
      document.querySelector("[data-slot=a2ui-image-pending]")
    ).toBeTruthy()
    await waitFor(() =>
      expect(
        document.querySelector("[data-slot=a2ui-image-pending]")
      ).toBeNull()
    )
    expect(document.querySelector("img")).toBeNull()
    vi.unstubAllGlobals()
  })

  it("draws a button's text as phrasing content", () => {
    mount(
      [
        {
          id: "root",
          component: "Button",
          variant: "borderless",
          child: "row",
          action: { event: { name: "open" } },
        },
        { id: "row", component: "Column", children: ["title", "body"] },
        { id: "title", component: "Text", text: "## Dune" },
        { id: "body", component: "Text", text: "A desert planet.\n\n- spice" },
      ],
      {}
    )
    const button = screen.getByRole("button")
    expect(
      button.querySelector("p, h1, h2, h3, h4, h5, h6, div, ul, ol, li, pre")
    ).toBeNull()
    expect(button.textContent).toContain("Dune")
    expect(button.textContent).toContain("• spice")
  })

  it("draws a button whose event deletes as destructive, borderless or not", () => {
    mount(
      [
        { id: "root", component: "Row", children: ["remove", "keep", "row"] },
        {
          id: "remove",
          component: "Button",
          child: "removeText",
          action: { event: { name: "remove" } },
        },
        { id: "removeText", component: "Text", text: "Remove" },
        {
          id: "keep",
          component: "Button",
          variant: "primary",
          child: "keepText",
          action: { event: { name: "keep" } },
        },
        { id: "keepText", component: "Text", text: "Keep" },
        {
          id: "row",
          component: "Button",
          variant: "borderless",
          child: "rowText",
          action: { event: { name: "remove" } },
        },
        { id: "rowText", component: "Text", text: "Remove this one" },
      ],
      {},
      { destructive: ["remove"] }
    )
    expect(screen.getByRole("button", { name: "Remove" }).className).toContain(
      "bg-destructive/10"
    )
    expect(
      screen.getByRole("button", { name: "Keep" }).className
    ).not.toContain("bg-destructive/10")
    const row = screen.getByRole("button", { name: "Remove this one" })
    expect(row.className).toContain("text-destructive")
    expect(row.className).not.toContain("text-foreground")
  })
})
