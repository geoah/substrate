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
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
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
function renderForm(seed: string, over: { record?: SubstrateRecord } = {}) {
  const onChange = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  function Harness() {
    const [text, setText] = useState(seed)
    return (
      <PropertyForm
        text={text}
        kind={llmprovider}
        kinds={[llmprovider]}
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
    expect(propertiesOf(onChange.mock.calls[0][0] as string)?.maxRetries).toBe(3)

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
    expect(
      propertiesOf(onChange.mock.calls[0][0] as string)?.apiKey
    ).toBe("sk-live")
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
    fireEvent.click(screen.getByRole("button", { name: "Add row" }))
    fireEvent.change(screen.getByLabelText("Model"), {
      target: { value: "claude-opus-5" },
    })
    fireEvent.change(screen.getByLabelText(/^Input/i), { target: { value: "5" } })
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
