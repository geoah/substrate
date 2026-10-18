// @vitest-environment jsdom
/** A quantity read: its digits with the unit's print symbol, and on hover
 * the same words, or with Technical details on the text a write takes. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

import { QuantityValue } from "./quantity-value"

afterEach(cleanup)

function show(value: unknown, technical: boolean) {
  render(
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
