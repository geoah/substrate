/** The editor's FORM lens over an `llmprovider`-shaped kind: the issue's own
 * worked example, where setting an `apiKey` was only ever possible through raw
 * YAML with no help in it.
 *
 * What the lens owes the declaration: a control per property with its label and
 * one-liner, an enum as a select rather than a free-text guess, a write-only
 * secret, a state that says a put may not move it, and every edit landing in
 * the SAME document the YAML lens shows, one key at a time. */

import { useState } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { propertiesOf, templateYAML } from "@/lib/record-yaml"
import { PropertyForm } from "./property-form"

const llmprovider: KindInfo = {
  identity: "core.substrate.reamde.dev/llmprovider",
  name: "llmprovider",
  authority: "core.substrate.reamde.dev",
  version: "",
  plural: "llmproviders",
  source: "builtin",
  definition: {
    properties: {
      name: { type: "string", required: true, description: "a human label" },
      wire: {
        type: "enum",
        values: ["openai", "anthropic", "azure"],
        description: "the wire protocol the adapter speaks",
      },
      baseURL: { type: "url", description: "the endpoint" },
      apiKey: { type: "secret", description: "bearer for the endpoint" },
      extras: { type: "json" },
      pricing: {
        type: "object",
        repeated: true,
        description: "USD per 1M tokens, one row per model",
        fields: {
          model: { type: "string" },
          inputPer1M: { type: "float", min: 0 },
        },
      },
      maxRetries: { type: "int" },
      status: {
        type: "state",
        states: ["draft", "live"],
        initial: "draft",
      },
      tokenRef: { type: "secret", writer: "oauth" },
    },
  },
}

const record: SubstrateRecord = {
  id: "default",
  kind: "core.substrate.reamde.dev/llmprovider",
  properties: {
    name: "the gateway",
    wire: "openai",
    apiKey: "<redacted>",
    status: "live",
  },
  labels: {},
  version: 3,
  createdAt: "x",
  updatedAt: "x",
}

/** The page owns the document and hands it back, so the harness does too: an
 * uncontrolled wrapper would drop every draft the moment a field wrote. */
function renderKindForm(
  kind: KindInfo,
  seed: string,
  over: { record?: SubstrateRecord; kinds?: KindInfo[] } = {}
) {
  const onChange = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  function Harness() {
    const [text, setText] = useState(seed)
    return (
      <PropertyForm
        text={text}
        kind={kind}
        kinds={over.kinds ?? [kind]}
        record={over.record}
        onChange={(next) => {
          onChange(next)
          setText(next)
        }}
      />
    )
  }

  const view = render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>
  )
  return { onChange, view }
}

function renderForm(seed: string, over: { record?: SubstrateRecord } = {}) {
  return renderKindForm(llmprovider, seed, over)
}

/** The last document the form emitted. */
function emitted(onChange: { mock: { calls: unknown[][] } }): string {
  return onChange.mock.calls.at(-1)![0] as string
}

// ── the record dropdown, as a caller drives it ──────────────────────────────

function searchBox(): HTMLInputElement {
  return screen.getByPlaceholderText(
    "Search records, or type an id"
  ) as HTMLInputElement
}

/** The record ids the open dropdown is offering, in order. */
function offered(): string[] {
  return [...document.querySelectorAll("[cmdk-item]")]
    .filter((el) => !el.hasAttribute("hidden"))
    .map((el) => (el.getAttribute("data-value") ?? "").split(" ")[0])
    .filter((v) => !v.startsWith("use-typed-"))
}

/** Open the dropdown a control owns and choose the record named, once the
 * collection behind it has answered. */
async function pick(control: string | RegExp, id: string) {
  fireEvent.click(screen.getByLabelText(control))
  await waitFor(() => expect(offered()).toContain(id))
  fireEvent.click(screen.getByText(id))
}

/** Open it and type a record the collection does not hold. */
function pickTyped(control: string | RegExp, typed: string) {
  fireEvent.click(screen.getByLabelText(control))
  fireEvent.change(searchBox(), { target: { value: typed } })
  fireEvent.click(screen.getByText(/^Use/))
}

afterEach(cleanup)

describe("the form lens", () => {
  it("composes a control per declared property, with its one-liner", () => {
    renderForm(templateYAML(llmprovider))
    expect(screen.getByLabelText(/^Name/)).toBeTruthy()
    expect(screen.getByText("the endpoint")).toBeTruthy()
    // The enum is a select over what the kind admits, not a text box.
    const wire = screen.getByLabelText("Wire") as HTMLSelectElement
    expect(wire.tagName).toBe("SELECT")
    expect([...wire.options].map((o) => o.value)).toEqual([
      "",
      "openai",
      "anthropic",
      "azure",
    ])
  })

  it("never offers a host-managed property (writer: oauth)", () => {
    renderForm(templateYAML(llmprovider))
    expect(screen.queryByLabelText(/Token ref/)).toBeNull()
  })

  it("writes an edit into the document, one key at a time", () => {
    const text = templateYAML(llmprovider)
    const { onChange } = renderForm(text)
    fireEvent.change(screen.getByLabelText(/^Name/), {
      target: { value: "the gateway" },
    })
    const next = onChange.mock.calls[0][0] as string
    expect(propertiesOf(next)?.name).toBe("the gateway")
    // Everything else in the document is exactly as it was.
    expect(next.split("\n").filter((l) => !l.includes("name:"))).toEqual(
      text.split("\n").filter((l) => !l.includes("name:"))
    )
  })

  it("writes a number as a number, and JSON as parsed JSON", () => {
    const { onChange } = renderForm(templateYAML(llmprovider))
    fireEvent.change(screen.getByLabelText(/Max retries/), {
      target: { value: "3" },
    })
    expect(propertiesOf(onChange.mock.calls[0][0] as string)?.maxRetries).toBe(
      3
    )

    fireEvent.change(screen.getByLabelText(/Extras/), {
      target: { value: '{"opus": 5}' },
    })
    const last = onChange.mock.calls.at(-1)![0] as string
    expect(propertiesOf(last)?.extras).toEqual({ opus: 5 })
  })

  it("keeps a draft that cannot be a value out of the document, and says why", () => {
    const { onChange } = renderForm(templateYAML(llmprovider))
    fireEvent.change(screen.getByLabelText(/Extras/), {
      target: { value: "{oops" },
    })
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByText(/not valid JSON/)).toBeTruthy()
  })

  it("keeps a secret write-only: it never seeds, and blank never touches the document", () => {
    const seeded = `kind: core.substrate.reamde.dev/llmprovider
metadata:
  id: default
data:
  properties:
    name: the gateway
    apiKey: <redacted>
`
    const { onChange } = renderForm(seeded, { record })
    const apiKey = screen.getByLabelText(/API key|Api key/) as HTMLInputElement
    expect(apiKey.type).toBe("password")
    expect(apiKey.value).toBe("")
    // Typing one seals a new value; clearing it again leaves the sealed one.
    fireEvent.change(apiKey, { target: { value: "sk-live" } })
    expect(propertiesOf(onChange.mock.calls[0][0] as string)?.apiKey).toBe(
      "sk-live"
    )
    onChange.mockClear()
    fireEvent.change(apiKey, { target: { value: "" } })
    expect(onChange).not.toHaveBeenCalled()
  })

  it("freezes a state on an edit, and offers the machine's states on a create", () => {
    renderForm(templateYAML(llmprovider))
    const create = screen.getByLabelText("Status") as HTMLSelectElement
    expect(create.disabled).toBe(false)
    expect([...create.options].map((o) => o.value)).toContain("live")
    cleanup()

    renderForm(templateYAML(llmprovider), { record })
    const edit = screen.getByLabelText("Status") as HTMLSelectElement
    expect(edit.disabled).toBe(true)
    expect(screen.getByText(/moves by transition/)).toBeTruthy()
  })

  it("edits a repeated object as rows of its declared fields, not as JSON", () => {
    const text = templateYAML(llmprovider)
    const { onChange } = renderForm(text)
    // A repeated object starts empty and grows a row at a time.
    fireEvent.click(screen.getByRole("button", { name: "Add Pricing row" }))
    fireEvent.change(screen.getByLabelText("Model"), {
      target: { value: "claude-opus-5" },
    })
    fireEvent.change(screen.getByLabelText(/^Input/i), {
      target: { value: "5" },
    })
    const last = onChange.mock.calls.at(-1)![0] as string
    expect(propertiesOf(last)?.pricing).toEqual([
      { model: "claude-opus-5", inputPer1M: 5 },
    ])
  })

  it("says so rather than guessing when the document does not parse", () => {
    renderForm("data:\n  properties:\n    a: [1, 2")
    expect(screen.getByText(/does not parse/)).toBeTruthy()
    expect(screen.queryByLabelText(/^Name/)).toBeNull()
  })
})

/** The WIDENED dialect on the form lens: a keyed map is rows the author names,
 * an object's fields nest as deep as the declaration goes, a `refersTo:` string
 * is a picker over the records it names, a managed property is read-only, and
 * an agent naming a host tool it has not paid for is told so. Every one of them
 * still writes through the ONE document. */

const mappingKind: KindInfo = {
  identity: "crew.test.dev/mapping",
  name: "mapping",
  authority: "crew.test.dev",
  version: "",
  plural: "mappings",
  source: "installed",
  definition: {
    properties: {
      keys: {
        type: "string",
        keyed: true,
        keyPattern: "camel",
        description: "the properties a binding must declare, name → datatype",
      },
      map: {
        type: "object",
        keyed: true,
        keyPattern: "camel",
        fields: { path: { type: "string", required: true } },
      },
    },
  },
}

const AGENT = "core.substrate.reamde.dev/agent"
const FUNCTION = "core.substrate.reamde.dev/function"
const KIND = "core.substrate.reamde.dev/kind"
const LLMPROVIDER = "core.substrate.reamde.dev/llmprovider"

const agentKind: KindInfo = {
  identity: "core.substrate.reamde.dev/agent",
  name: "agent",
  authority: "core.substrate.reamde.dev",
  version: "",
  plural: "agents",
  source: "builtin",
  definition: {
    properties: {
      version: {
        type: "string",
        managed: true,
        description: "this declaration's version",
      },
      authority: {
        type: "string",
        required: true,
        description: "the authority that declares it",
      },
      // EVERY POINTER IS A REFERENCE, pinned with `kind:`, and its value is
      // the flat path `<kind-identity>/<record-id>`.
      provider: { type: "reference", kind: LLMPROVIDER, required: true },
      agents: { type: "reference", kind: AGENT, repeated: true },
      tools: {
        type: "object",
        repeated: true,
        fields: {
          // `function`, not `callable`: within `tools:` an entry names a
          // function and nothing else.
          function: { type: "reference", kind: FUNCTION, required: true },
          name: { type: "string" },
        },
      },
      // THE GRANTS: one `permissions` object, with the pins now sitting on
      // fields two and three levels down. The pickers have to reach them there.
      permissions: {
        type: "object",
        fields: {
          writes: { type: "reference", kind: KIND, repeated: true },
          reads: {
            type: "object",
            fields: {
              kinds: { type: "reference", kind: KIND, repeated: true },
              budgets: { type: "object", fields: { calls: { type: "int" } } },
            },
          },
        },
      },
    },
  },
}

function agentRecord(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "crew.test.dev/scout",
    kind: agentKind.identity,
    properties,
    labels: {},
    version: 2,
    createdAt: "x",
    updatedAt: "x",
  }
}

/** The REGISTRY a pointer's pin resolves through: a `KindInfo` per pinned
 * kind, which is where the picker learns the collection to read. */
function registryKind(identity: string, plural: string): KindInfo {
  return {
    identity,
    name: identity.split("/")[1],
    authority: identity.split("/")[0],
    version: "",
    plural,
    source: "builtin",
    description: "",
    definition: {},
  }
}

/** The kind records the registry collection serves, which is what a pointer
 * pinned at `core.substrate.reamde.dev/kind` offers. */
const KIND_RECORDS = [FUNCTION, KIND, LLMPROVIDER]

const REGISTRY: KindInfo[] = [
  agentKind,
  registryKind(FUNCTION, "functions"),
  registryKind(KIND, "kinds"),
  registryKind(LLMPROVIDER, "llmproviders"),
]

/** The collections the pickers read, served from one stub so a dropdown can be
 * asserted on what the substrate would have offered. */
function stubCollections() {
  const page = (records: SubstrateRecord[]) =>
    new Response(JSON.stringify({ records }), { status: 200 })
  const record = (id: string, properties: Record<string, unknown>) =>
    ({
      id,
      kind: "x",
      properties,
      labels: {},
      version: 1,
      createdAt: "x",
      updatedAt: "x",
    }) as SubstrateRecord

  vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes("/functions?")) {
      return Promise.resolve(
        page([
          record("core.substrate.reamde.dev/propose", {
            description: "Propose a reviewed change to the graph",
          }),
          record("crew.test.dev/summarize", { description: "shorten a note" }),
        ])
      )
    }
    if (url.includes("/agents?")) {
      return Promise.resolve(
        page([
          record("crew.test.dev/scout", {
            description: "the one being edited",
          }),
          record("crew.test.dev/librarian", { description: "finds things" }),
        ])
      )
    }
    if (url.includes("/kinds?")) {
      return Promise.resolve(
        page(KIND_RECORDS.map((id) => record(id, { description: "a kind" })))
      )
    }
    if (url.includes("/llmproviders?")) {
      return Promise.resolve(page([record("claude", { name: "the gateway" })]))
    }
    return Promise.resolve(page([]))
  })
}

describe("the form lens over the widened dialect", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("edits a keyed SCALAR map as key/value rows, into the document", () => {
    const { onChange } = renderKindForm(mappingKind, templateYAML(mappingKind))
    fireEvent.click(screen.getByRole("button", { name: "Add Keys entry" }))
    fireEvent.change(screen.getByLabelText("Keys key 1"), {
      target: { value: "displayName" },
    })
    fireEvent.change(screen.getByLabelText("Value"), {
      target: { value: "string" },
    })
    expect(propertiesOf(emitted(onChange))?.keys).toEqual({
      displayName: "string",
    })
  })

  it("holds a keyed map's keys to the declared contract, off the document", () => {
    const { onChange } = renderKindForm(mappingKind, templateYAML(mappingKind))
    fireEvent.click(screen.getByRole("button", { name: "Add Keys entry" }))
    fireEvent.change(screen.getByLabelText("Keys key 1"), {
      target: { value: "Display Name" },
    })
    fireEvent.change(screen.getByLabelText("Value"), {
      target: { value: "string" },
    })
    expect(screen.getByText(/must be camelCase/)).toBeTruthy()
    // The draft stays a draft: the map is still the empty one the template
    // seeded, and the refused key never landed in it.
    expect(propertiesOf(emitted(onChange))?.keys).toEqual({})
  })

  it("edits a keyed OBJECT map as a key beside the object's own fields", () => {
    const { onChange } = renderKindForm(mappingKind, templateYAML(mappingKind))
    fireEvent.click(screen.getByRole("button", { name: "Add Map entry" }))
    fireEvent.change(screen.getByLabelText("Map key 1"), {
      target: { value: "summary" },
    })
    fireEvent.change(screen.getByLabelText(/^Path/), {
      target: { value: "$.title" },
    })
    expect(propertiesOf(emitted(onChange))?.map).toEqual({
      summary: { path: "$.title" },
    })
  })

  it("seeds a keyed map's rows from the document and removes one", () => {
    const seeded = `kind: crew.test.dev/mapping
metadata:
  id: m1
data:
  properties:
    keys:
      displayName: string
      dueAt: datetime
`
    const { onChange } = renderKindForm(mappingKind, seeded)
    expect(
      (screen.getByLabelText("Keys key 1") as HTMLInputElement).value
    ).toBe("displayName")
    fireEvent.click(screen.getByRole("button", { name: "Remove Keys entry 2" }))
    expect(propertiesOf(emitted(onChange))?.keys).toEqual({
      displayName: "string",
    })
  })

  it("nests an object's fields three levels down, and writes each back", async () => {
    stubCollections()
    const { onChange } = renderKindForm(agentKind, templateYAML(agentKind), {
      kinds: REGISTRY,
    })
    // permissions.reads.budgets.calls: the deepest the dialect goes.
    fireEvent.change(screen.getByLabelText("Calls"), {
      target: { value: "20" },
    })
    expect(propertiesOf(emitted(onChange))?.permissions).toEqual({
      reads: { budgets: { calls: 20 } },
    })
    // The nested repeated pointer grows a row of its own, beside the nested
    // object: three levels below the property, in one document.
    await pick("Add Kinds", FUNCTION)
    expect(propertiesOf(emitted(onChange))?.permissions).toEqual({
      reads: {
        kinds: [`${KIND}/${FUNCTION}`],
        budgets: { calls: 20 },
      },
    })
  })

  it("offers the KIND dropdown on a pointer nested inside permissions", async () => {
    stubCollections()
    renderKindForm(agentKind, templateYAML(agentKind), { kinds: REGISTRY })
    // `permissions.writes` is pinned two levels down, and
    // `permissions.reads.kinds` three. The pin has to survive the projection
    // to that depth, or the control is a text box asking somebody to remember
    // a kind reference.
    fireEvent.click(screen.getByLabelText("Add Writes"))
    await waitFor(() => expect(offered()).toEqual(KIND_RECORDS))
    fireEvent.keyDown(searchBox(), { key: "Escape" })

    fireEvent.click(screen.getByLabelText("Add Kinds"))
    await waitFor(() => expect(offered()).toEqual(KIND_RECORDS))
  })

  it("renders a managed property read-only, stamped, never an input", () => {
    stubCollections()
    const stamped = `kind: core.substrate.reamde.dev/agent
metadata:
  id: crew.test.dev/scout
data:
  properties:
    version: v1alpha7
    provider: default
`
    renderKindForm(agentKind, stamped, {
      record: agentRecord({ version: "v1alpha7", provider: "default" }),
    })
    expect(screen.getByText("engine-stamped")).toBeTruthy()
    expect(screen.getByText("v1alpha7")).toBeTruthy()
    expect(screen.queryByLabelText(/^Version/)).toBeNull()
  })

  it("offers the records a pointer is pinned to, and inserts the path", async () => {
    stubCollections()
    const { onChange } = renderKindForm(agentKind, templateYAML(agentKind), {
      kinds: REGISTRY,
    })
    fireEvent.click(screen.getByLabelText(/^Provider/))
    await waitFor(() => expect(offered()).toEqual(["claude"]))
    fireEvent.click(screen.getByText("the gateway"))
    // The PATH is the value; the row read as the record.
    expect(propertiesOf(emitted(onChange))?.provider).toBe(
      `${LLMPROVIDER}/claude`
    )
  })

  it("keeps free text open: a record can be minted at any time", () => {
    stubCollections()
    const { onChange } = renderKindForm(agentKind, templateYAML(agentKind), {
      kinds: REGISTRY,
    })
    pickTyped(/^Provider/, "not-listed-yet")
    // The PIN completes what was typed: a bare id is the authored short form,
    // and the write carries the path it names.
    expect(propertiesOf(emitted(onChange))?.provider).toBe(
      `${LLMPROVIDER}/not-listed-yet`
    )
  })

  it("picks a function inside a tool row, host functions listed like any other", async () => {
    stubCollections()
    const { onChange } = renderKindForm(agentKind, templateYAML(agentKind), {
      kinds: REGISTRY,
    })
    fireEvent.click(screen.getByRole("button", { name: "Add Tools row" }))
    fireEvent.click(screen.getByLabelText(/^Function/))
    await waitFor(() =>
      expect(offered()).toContain("core.substrate.reamde.dev/propose")
    )
    // The card is what makes a host function choosable: its one-liner.
    expect(screen.getByText(/Propose a reviewed change/)).toBeTruthy()
    fireEvent.click(screen.getByText("core.substrate.reamde.dev/propose"))
    expect(propertiesOf(emitted(onChange))?.tools).toEqual([
      { function: `${FUNCTION}/core.substrate.reamde.dev/propose` },
    ])
  })

  it("never offers the agent being edited as its own sub-agent", async () => {
    stubCollections()
    renderKindForm(agentKind, templateYAML(agentKind), {
      kinds: REGISTRY,
      record: agentRecord({ provider: `${LLMPROVIDER}/claude` }),
    })
    fireEvent.click(screen.getByLabelText("Add Agents"))
    await waitFor(() => expect(offered()).toEqual(["crew.test.dev/librarian"]))
  })

  it("stacks a repeated pointer as rows, and removes one", async () => {
    stubCollections()
    const { onChange } = renderKindForm(agentKind, templateYAML(agentKind), {
      kinds: REGISTRY,
    })
    fireEvent.click(screen.getByLabelText("Add Agents"))
    await waitFor(() => expect(offered()).toContain("crew.test.dev/librarian"))
    fireEvent.click(screen.getByText("crew.test.dev/librarian"))
    expect(propertiesOf(emitted(onChange))?.agents).toEqual([
      `${AGENT}/crew.test.dev/librarian`,
    ])
    // The chosen entry is a ROW, and the row is what removes it.
    expect(screen.getByLabelText("Agents 1")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Remove Agents 1" }))
    // Emptied on purpose, so the key stays holding nothing.
    expect(propertiesOf(emitted(onChange))?.agents).toEqual([])
  })

  it("leaves a commented document byte-for-byte, but for the one scalar edited", () => {
    stubCollections()
    // A comment at every nesting level. A deep edit that re-serialized the
    // property would take all of them with it, which is what the form lens
    // promises never to do.
    const commented = `kind: core.substrate.reamde.dev/agent
metadata:
  id: crew.test.dev/scout # the declared identity
data:
  properties:
    provider: default # which endpoint
    # what this agent is allowed to do while it runs
    permissions:
      # what it may read
      reads:
        # the allowlist every read is held to
        kinds:
          - crew.test.dev/widget
        budgets:
          calls: 3 # how many reads one run may make
          rows: 50
`
    const { onChange } = renderKindForm(agentKind, commented, {
      kinds: REGISTRY,
    })
    fireEvent.change(screen.getByLabelText("Calls"), {
      target: { value: "20" },
    })
    const after = emitted(onChange)
    expect(after).toBe(commented.replace("calls: 3 #", "calls: 20 #"))
  })

  it("keeps an empty container a sibling edit did not touch", () => {
    stubCollections()
    // Absent and empty are different claims: editing `budgets.calls` must not
    // quietly delete an `kinds: []` nobody went near.
    const seeded = `kind: core.substrate.reamde.dev/agent
metadata:
  id: crew.test.dev/scout
data:
  properties:
    provider: default
    permissions:
      writes: []
      reads:
        kinds: []
        budgets:
          calls: 3
`
    const { onChange } = renderKindForm(agentKind, seeded, { kinds: REGISTRY })
    fireEvent.change(screen.getByLabelText("Calls"), {
      target: { value: "20" },
    })
    expect(propertiesOf(emitted(onChange))?.permissions).toEqual({
      writes: [],
      reads: { kinds: [], budgets: { calls: 20 } },
    })
  })

  it("keeps a keyed map the person EMPTIED, and it stays empty", () => {
    const seeded = `kind: crew.test.dev/mapping
metadata:
  id: m1
data:
  properties:
    keys:
      displayName: string
`
    const { onChange } = renderKindForm(mappingKind, seeded)
    fireEvent.click(screen.getByRole("button", { name: "Remove Keys entry 1" }))
    const props = propertiesOf(emitted(onChange))
    // The key stays, holding nothing: emptying a map is a statement, and
    // dropping it would be the form saying the author never had one.
    expect(props).toHaveProperty("keys")
    expect(props?.keys).toEqual({})
    // A map that was never in the document is not invented by touching
    // anything else.
    expect(props).not.toHaveProperty("map")
  })

  it("asks for a DECLARATION's id, and shows the shape it is spelled in", () => {
    stubCollections()
    renderKindForm(agentKind, templateYAML(agentKind), { kinds: REGISTRY })
    const id = screen.getByLabelText(/^Record id/) as HTMLInputElement
    expect(id.placeholder).toBe("<authority>/<name>")
    expect(id.getAttribute("aria-invalid")).toBe("true")
    expect(screen.getByText(/never mints one/)).toBeTruthy()
  })

  it("derives a declaration's authority from its id, and never asks for it", () => {
    stubCollections()
    const { onChange } = renderKindForm(agentKind, templateYAML(agentKind), {
      kinds: REGISTRY,
    })
    // No input for it: the label in front of the slash has already been typed.
    expect(screen.queryByLabelText(/^Authority/)).toBeNull()
    fireEvent.change(screen.getByLabelText(/^Record id/), {
      target: { value: "crew.test.dev/scout" },
    })
    // The write still CARRIES it: the loader requires the property, only the
    // asking stops.
    expect(propertiesOf(emitted(onChange))?.authority).toBe("crew.test.dev")
    expect(screen.getByText("derived")).toBeTruthy()
    expect(screen.getByText("crew.test.dev")).toBeTruthy()
  })

  it("still asks an ORDINARY kind for everything, id included", () => {
    const { onChange } = renderKindForm(mappingKind, templateYAML(mappingKind))
    const id = screen.getByLabelText(/^Record id/) as HTMLInputElement
    expect(id.placeholder).toMatch(/mints one/)
    expect(id.getAttribute("aria-invalid")).not.toBe("true")
    fireEvent.change(id, { target: { value: "m1" } })
    // Nothing derived: an ordinary kind's id says nothing about an authority.
    expect(propertiesOf(emitted(onChange))).not.toHaveProperty("authority")
  })

  it("renders every row action as a button, and names what it adds", () => {
    stubCollections()
    renderKindForm(agentKind, templateYAML(agentKind), { kinds: REGISTRY })
    // A declaration nests, so one form holds several lists: each add says
    // which one it grows rather than all of them reading "Add". A pointer's
    // add is the dropdown's own trigger, which is a button all the same.
    for (const name of ["Add Tools row", "Add Agents", "Add Writes"]) {
      expect(screen.getByRole("button", { name }).tagName).toBe("BUTTON")
    }
    fireEvent.click(screen.getByRole("button", { name: "Add Tools row" }))
    const remove = screen.getByRole("button", { name: "Remove Tools row 1" })
    expect(remove.getAttribute("data-slot")).toBe("button")
  })

  it("keeps a key exactly as typed, spaces and all", () => {
    // Go's CheckKey validates the ORIGINAL and admits any non-empty string
    // where no keyPattern narrows it. Trimming here would store a key nobody
    // named.
    const spaced: KindInfo = {
      ...mappingKind,
      definition: {
        properties: { keys: { type: "string", keyed: true } },
      },
    }
    const { onChange } = renderKindForm(spaced, templateYAML(spaced))
    fireEvent.click(screen.getByRole("button", { name: "Add Keys entry" }))
    fireEvent.change(screen.getByLabelText("Keys key 1"), {
      target: { value: " helper " },
    })
    fireEvent.change(screen.getByLabelText("Value"), {
      target: { value: "string" },
    })
    expect(propertiesOf(emitted(onChange))?.keys).toEqual({
      " helper ": "string",
    })
  })

  it("warns that a host tool's grant is unpaid, and clears when it is paid", () => {
    stubCollections()
    const unpaid = `kind: core.substrate.reamde.dev/agent
metadata:
  id: crew.test.dev/scout
data:
  properties:
    provider: default
    tools:
      - function: core.substrate.reamde.dev/propose
`
    const { view } = renderKindForm(agentKind, unpaid, { kinds: REGISTRY })
    // The hint names the path the loader's own refusal names.
    expect(
      screen.getByText(
        /needs core\.substrate\.reamde\.dev\/recordpatchrequest in data\.permissions\.writes/
      )
    ).toBeTruthy()
    view.unmount()

    renderKindForm(
      agentKind,
      `${unpaid}    permissions:
      writes:
        - core.substrate.reamde.dev/recordpatchrequest
`
    )
    expect(screen.queryByText(/propose lands a change request/)).toBeNull()
  })
})
