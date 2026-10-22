// @vitest-environment jsdom
/** A quantity read: its digits with the unit's print symbol, and on hover
 * the same words, or with Technical details on the text a write takes. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import { UnitsContext } from "@/hooks/use-units"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { UnitGraph } from "@/lib/unit-graph"
import { editedTable, SHIPPED_UNITS } from "@/lib/unit-graph.test-fixtures"
import { graphUnits, pendingUnits, type UnitReader } from "@/lib/unit-reader"

import { QuantityValue } from "./quantity-value"

afterEach(cleanup)

function show(value: unknown, technical: boolean, units = SHIPPED_UNITS) {
  render(
    <UnitsContext.Provider value={units}>
      {shown(value, technical)}
    </UnitsContext.Provider>
  )
}

function shown(value: unknown, technical: boolean) {
  return (
    <ConsolePreferencesContext.Provider
      value={{
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
      }}
    >
      <QuantityValue value={value} />
    </ConsolePreferencesContext.Provider>
  )
}

const edited: UnitReader = graphUnits(new UnitGraph(editedTable))

describe("a quantity value", () => {
  it("names an annotation unit without its braces, on hover too", () => {
    show({ value: "19", unit: "{score}" }, false)
    const shown = screen.getByText("19 score")
    expect(shown.getAttribute("title")).toBe("19 score")
  })

  it("gives the text a write takes on hover with Technical details on", () => {
    show({ value: "37", unit: "Cel" }, true)
    expect(screen.getByText("37 °C").getAttribute("title")).toBe("37 Cel")
  })
})

describe("a quantity value on a server whose units are the repository's graph", () => {
  it("prints a unit only the repository holds with its symbol", () => {
    show({ value: "2", unit: "sack25" }, false, edited)
    expect(screen.getByText("2 sack").getAttribute("title")).toBe("2 sack")
  })

  it("reads as stored, and says why, while the graph loads", () => {
    show({ value: "37", unit: "Cel" }, false, pendingUnits("loading"))
    const held = screen.getByText("37 Cel")
    expect(held.getAttribute("title")).toBe(
      "The repository's units are loading"
    )
    expect(screen.queryByText("37 °C")).toBeNull()
  })
})
