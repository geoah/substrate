// @vitest-environment jsdom
/** The quantity input: a value box, a unit select narrowed by the
 * declaration and, where it declares one, a comparator select, that together
 * write the editor text `72.5 kg`, controlled so a list's rows keep their
 * own. */

import { useState } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { UnitsProvider } from "@/components/units-provider"
import { forgetUnitGraphs } from "@/lib/api/units"
import { editedTable } from "@/lib/unit-graph.test-fixtures"

import { QuantityInput } from "./quantity-input"
import type { QuantityDeclaration } from "@/lib/units"

vi.mock("@/lib/api/changes", () => ({
  watchChanges: () => ({ stop: () => undefined }),
}))

afterEach(() => {
  cleanup()
  forgetUnitGraphs()
  vi.unstubAllGlobals()
})
beforeEach(() => localStorage.clear())

/** A server with the graph switch on, whose units route answers `units`. */
function graphServer(units: () => Promise<Response>) {
  const fetch = vi.fn((input: RequestInfo | URL) =>
    String(input).includes("server.json")
      ? Promise.resolve(
          new Response(JSON.stringify({ units: { graph: true } }), {
            status: 200,
          })
        )
      : units()
  )
  vi.stubGlobal("fetch", fetch)
  return () => fetch.mock.calls.some((c) => String(c[0]).endsWith("/units"))
}

const editedGraph = () =>
  Promise.resolve(
    new Response(
      JSON.stringify({
        ...editedTable,
        measures: [],
        aliases: {},
        unseeded: [],
      }),
      { status: 200 }
    )
  )

/** The input under the signed-in console's unit reader. */
function renderUnderProvider(spec: QuantityDeclaration, initial = "") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <UnitsProvider>
        <Harness initial={initial} spec={spec} seen={[]} />
      </UnitsProvider>
    </QueryClientProvider>
  )
}

function Harness({
  initial = "",
  spec = {},
  seen,
}: {
  initial?: string
  spec?: QuantityDeclaration
  seen: string[]
}) {
  const [text, setText] = useState(initial)
  return (
    <QuantityInput
      label="Weight"
      value={text}
      spec={spec}
      onChange={(next) => {
        seen.push(next)
        setText(next)
      }}
    />
  )
}

const value = () => screen.getByLabelText<HTMLInputElement>("Weight, value")
const unit = () => screen.getByLabelText<HTMLSelectElement>("Weight, unit")
const comparator = () =>
  screen.queryByLabelText<HTMLSelectElement>("Weight, comparator")
const offered = () => [...unit().options].map((o) => o.value)

describe("QuantityInput", () => {
  it("splits a stored text into its value and its unit", () => {
    render(
      <Harness initial="37 Cel" spec={{ measures: "temperature" }} seen={[]} />
    )
    expect(value().value).toBe("37")
    expect(value().inputMode).toBe("decimal")
    expect(value().type).toBe("text")
    expect(unit().value).toBe("Cel")
    expect(unit().selectedOptions[0].textContent).toBe("°C (Cel)")
    expect(offered()).toEqual(["K", "Cel", "[degF]"])
    expect(comparator()).toBeNull()
  })

  it("writes the value and the chosen unit as one text", () => {
    const seen: string[] = []
    render(<Harness spec={{ measures: "mass" }} seen={seen} />)
    expect(unit().value).toBe("kg")
    fireEvent.change(value(), { target: { value: "72.5" } })
    expect(seen.at(-1)).toBe("72.5 kg")
    fireEvent.change(unit(), { target: { value: "[lb_av]" } })
    expect(seen.at(-1)).toBe("72.5 [lb_av]")
  })

  it("keeps a chosen unit while the value is blank, writing no value", () => {
    const seen: string[] = []
    render(<Harness spec={{ measures: "mass" }} seen={seen} />)
    fireEvent.change(unit(), { target: { value: "g" } })
    expect(seen.at(-1)).toBe("")
    expect(unit().value).toBe("g")
    fireEvent.change(value(), { target: { value: "1" } })
    expect(seen.at(-1)).toBe("1 g")
    expect(value().value).toBe("1")
  })

  it("narrows to the declared units and keeps a stored unit no list offers", () => {
    render(
      <Harness
        initial="5 GJ"
        spec={{ measures: "energy", units: ["kJ", "kcal"] }}
        seen={[]}
      />
    )
    expect(unit().value).toBe("GJ")
    expect(offered()).toEqual(["GJ", "kJ", "kcal"])
  })

  it("groups every unit by measure when the declaration names none", () => {
    render(<Harness seen={[]} />)
    expect(unit().value).toBe("")
    const groups = [...unit().querySelectorAll("optgroup")].map((g) => g.label)
    expect(groups).toContain("Mass")
    expect(groups).toContain("Temperature")
    expect(groups).not.toContain("Temperature difference")
  })

  it("offers a comparator only where the property declares one", () => {
    const seen: string[] = []
    render(
      <Harness
        initial="0.5 ng/mL"
        spec={{ measures: "mass-concentration", comparator: true }}
        seen={seen}
      />
    )
    expect(comparator()?.value).toBe("")
    fireEvent.change(comparator()!, { target: { value: "<" } })
    expect(seen.at(-1)).toBe("<0.5 ng/mL")
    cleanup()
    render(<Harness initial="<0.5 ng/mL" spec={{}} seen={[]} />)
    // A stored comparator stays visible, for the server to refuse by name.
    expect(comparator()?.value).toBe("<")
  })

  it("opens a blank value on the default's unit, the display unit, then the one picked last", () => {
    render(
      <Harness
        spec={{
          measures: "mass",
          default: { value: "150", unit: "[lb_av]" },
        }}
        seen={[]}
      />
    )
    expect(unit().value).toBe("[lb_av]")
    cleanup()
    render(<Harness spec={{ measures: "length" }} seen={[]} />)
    expect(unit().value).toBe("m")
    cleanup()
    render(<Harness seen={[]} />)
    fireEvent.change(unit(), { target: { value: "mmol/L" } })
    cleanup()
    render(<Harness seen={[]} />)
    expect(unit().value).toBe("mmol/L")
  })
})

describe("QuantityInput on a server whose units are the repository's graph", () => {
  it("offers a unit only the repository holds, under its symbol", async () => {
    graphServer(editedGraph)
    renderUnderProvider({ unit: "kg" })
    await waitFor(() => expect(offered()).toContain("sack25"))
    expect(offered()).toEqual(["g", "kg", "lb", "sack25", "ug"])
    expect(unit().value).toBe("kg")
    const sack = [...unit().options].find((o) => o.value === "sack25")
    expect(sack?.textContent).toBe("sack (sack25)")
    fireEvent.change(value(), { target: { value: "2" } })
    fireEvent.change(unit(), { target: { value: "sack25" } })
    expect(unit().value).toBe("sack25")
  })

  it("takes no value and offers no unit while the graph loads", async () => {
    const asked = graphServer(() => new Promise<Response>(() => undefined))
    renderUnderProvider({ unit: "kg" }, "72.5 kg")
    // Past discovery: the switch is on and the graph is on its way.
    await waitFor(() => expect(asked()).toBe(true))
    expect((await screen.findByRole("status")).textContent).toBe(
      "The repository's units are loading"
    )
    expect(value().disabled).toBe(true)
    expect(unit().disabled).toBe(true)
    // The stored unit stays chosen as stored; the table's list is not offered.
    expect(offered()).toEqual(["kg"])
  })
})
