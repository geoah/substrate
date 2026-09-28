// @vitest-environment jsdom
/** The money input: an amount box and a currency select that together write
 * the editor text `19.99 EUR`, controlled so a list's rows keep their own. */

import { useState } from "react"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { MoneyInput } from "./money-input"

afterEach(cleanup)
beforeEach(() => localStorage.clear())

function Harness({
  initial = "",
  fallback,
  seen,
}: {
  initial?: string
  fallback?: string
  seen: string[]
}) {
  const [text, setText] = useState(initial)
  return (
    <MoneyInput
      label="Price"
      value={text}
      fallbackCurrency={fallback}
      onChange={(next) => {
        seen.push(next)
        setText(next)
      }}
    />
  )
}

const amount = () => screen.getByLabelText("Price, amount") as HTMLInputElement
const currency = () =>
  screen.getByLabelText("Price, currency") as HTMLSelectElement

describe("MoneyInput", () => {
  it("splits a stored text into its amount and its currency", () => {
    render(<Harness initial="19.99 EUR" seen={[]} />)
    expect(amount().value).toBe("19.99")
    expect(amount().inputMode).toBe("decimal")
    expect(amount().type).toBe("text")
    expect(currency().value).toBe("EUR")
    expect(currency().selectedOptions[0].textContent).toMatch(/^EUR · Euro$/)
  })

  it("writes the amount and the chosen currency as one text", () => {
    const seen: string[] = []
    render(<Harness seen={seen} />)
    expect(currency().value).toBe("")
    fireEvent.change(amount(), { target: { value: "4.2" } })
    expect(seen.at(-1)).toBe("4.2")
    fireEvent.change(currency(), { target: { value: "JPY" } })
    expect(seen.at(-1)).toBe("4.2 JPY")
    expect(amount().placeholder).toBe("0")
  })

  it("keeps a chosen currency while the amount is blank", () => {
    const seen: string[] = []
    render(<Harness seen={seen} />)
    fireEvent.change(currency(), { target: { value: "KWD" } })
    expect(seen.at(-1)).toBe("KWD")
    expect(amount().placeholder).toBe("0.000")
    fireEvent.change(amount(), { target: { value: "1.5" } })
    expect(seen.at(-1)).toBe("1.5 KWD")
  })

  it("opens a blank value on the declared currency, then the one picked last", () => {
    const seen: string[] = []
    render(<Harness fallback="USD" seen={seen} />)
    expect(currency().value).toBe("USD")
    cleanup()
    render(<Harness seen={[]} />)
    fireEvent.change(currency(), { target: { value: "GBP" } })
    cleanup()
    render(<Harness seen={[]} />)
    expect(currency().value).toBe("GBP")
  })
})
