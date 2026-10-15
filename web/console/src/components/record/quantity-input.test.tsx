// @vitest-environment jsdom
/** The quantity input: a value box, a unit select narrowed by the
 * declaration and, where it declares one, a comparator select, that together
 * write the editor text `72.5 kg`, controlled so a list's rows keep their
 * own. */

import { useState } from "react"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { QuantityInput } from "./quantity-input"
import type { QuantityDeclaration } from "@/lib/units"

afterEach(cleanup)
beforeEach(() => localStorage.clear())

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

const value = () => screen.getByLabelText("Weight, value") as HTMLInputElement
const unit = () => screen.getByLabelText("Weight, unit") as HTMLSelectElement
const comparator = () =>
  screen.queryByLabelText("Weight, comparator") as HTMLSelectElement | null
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
