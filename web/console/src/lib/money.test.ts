/** The money datatype, client-side: the substrate's own checks on the stored
 * shape, the currency's minor unit placing the point, the exact text an
 * editor holds, and the reader's display. */

import { describe, expect, it } from "vitest"

import {
  CURRENCIES,
  checkMoney,
  currencyDecimals,
  formatMoney,
  isMoney,
  moneyDecimal,
  moneyText,
  parseAmount,
  parseMoneyText,
  splitMoneyText,
} from "./money"

const money = (amount: unknown, currency: unknown) => ({ amount, currency })

describe("the currencies", () => {
  it("knows ISO 4217's minor unit per code", () => {
    expect(currencyDecimals("EUR")).toBe(2)
    expect(currencyDecimals("JPY")).toBe(0)
    expect(currencyDecimals("KWD")).toBe(3)
    expect(currencyDecimals("CLF")).toBe(4)
    expect(currencyDecimals("XAU")).toBeUndefined()
    expect(currencyDecimals("toString")).toBeUndefined()
    expect(CURRENCIES).toContain("USD")
    expect([...CURRENCIES].sort()).toEqual(CURRENCIES)
  })
})

describe("checkMoney", () => {
  it("admits what the substrate admits", () => {
    expect(checkMoney(money(1999, "EUR"))).toBeUndefined()
    expect(checkMoney(money(-250, "USD"))).toBeUndefined()
    expect(checkMoney(money(500, "JPY"))).toBeUndefined()
  })

  it("names the reason a value is wrong, in the substrate's words", () => {
    expect(checkMoney("19.99")).toMatch(/is an object/)
    expect(checkMoney(money(19.99, "EUR"))).toMatch(/expected an integer/)
    expect(checkMoney(money("1999", "EUR"))).toMatch(/not a string/)
    expect(checkMoney(money(2 ** 53, "EUR"))).toMatch(/safe integer/)
    expect(checkMoney(money(1999, "eur"))).toMatch(/ISO 4217/)
    expect(checkMoney(money(1999, "XYZ"))).toMatch(/ISO 4217/)
    expect(checkMoney({ ...money(1999, "EUR"), decimals: 2 })).toMatch(
      /"decimals" is neither/
    )
  })

  it("bounds the number the currency's minor unit makes of the amount", () => {
    expect(checkMoney(money(-1, "EUR"), 0)).toMatch(/>= 0/)
    expect(checkMoney(money(1000, "EUR"), undefined, 10)).toBeUndefined()
    expect(checkMoney(money(1001, "EUR"), undefined, 10)).toMatch(/<= 10/)
    expect(checkMoney(money(11, "JPY"), undefined, 10)).toMatch(/<= 10/)
  })
})

describe("the exact text", () => {
  it("places the point by the currency", () => {
    expect(moneyDecimal(1999, 2)).toBe("19.99")
    expect(moneyDecimal(-5, 2)).toBe("-0.05")
    expect(moneyText(money(1990, "EUR"))).toBe("19.90 EUR")
    expect(moneyText(money(500, "JPY"))).toBe("500 JPY")
    expect(moneyText(money(1500, "KWD"))).toBe("1.500 KWD")
    expect(moneyText("19.90 EUR")).toBe("")
  })

  it("reads back to exactly the value it came from", () => {
    for (const value of [
      money(1999, "EUR"),
      money(-5, "USD"),
      money(500, "JPY"),
      money(1500, "KWD"),
    ]) {
      expect(parseMoneyText(moneyText(value))).toEqual({ value })
    }
  })

  it("pads a short fraction and refuses a long one", () => {
    expect(parseAmount("19.9", "EUR")).toEqual({ amount: 1990 })
    expect(parseAmount("19", "EUR")).toEqual({ amount: 1900 })
    expect(parseAmount("-0", "EUR")).toEqual({ amount: 0 })
    expect(parseAmount("19.999", "EUR").error).toBe("EUR has 2 decimal places")
    expect(parseAmount("5.5", "JPY").error).toBe("JPY has no decimal places")
    expect(parseAmount("1,999.00", "EUR").error).toMatch(/like 19.99/)
    expect(parseAmount("90071992547409.92", "EUR").error).toMatch(/minor units/)
  })

  it("takes the code on either side, in either case", () => {
    const want = { value: money(1999, "EUR") }
    expect(parseMoneyText("EUR 19.99")).toEqual(want)
    expect(parseMoneyText("19.99 eur")).toEqual(want)
    expect(splitMoneyText("  +19.99 EUR ")).toEqual({
      amount: "+19.99",
      currency: "EUR",
    })
  })

  it("asks for what is missing", () => {
    expect(parseMoneyText("19.99").error).toBe("choose a currency")
    expect(parseMoneyText("19.99 XYZ").error).toMatch(
      /not an ISO 4217 currency/
    )
    expect(parseMoneyText("").error).toMatch(/an amount and a currency/)
  })
})

describe("formatMoney", () => {
  it("shows the currency at its own minor unit", () => {
    expect(formatMoney(money(1999, "EUR"), "en-US")).toBe("€19.99")
    expect(formatMoney(money(500, "JPY"), "en-US")).toBe("¥500")
    expect(formatMoney(money(-250, "USD"), "en-US")).toBe("-$2.50")
    expect(formatMoney(money(1500, "KWD"), "en-US")).toMatch(/^KWD\s1\.500$/)
  })

  it("places the point by ISO 4217 where the locale data disagrees", () => {
    // CLDR gives the Iraqi dinar no decimals; ISO 4217 gives it three.
    expect(formatMoney(money(1500, "IQD"), "en-US")).toMatch(/^IQD\s1\.500$/)
  })

  it("formats digits past a float's precision exactly", () => {
    expect(formatMoney(money(2 ** 53 - 1, "USD"), "en-US")).toBe(
      "$90,071,992,547,409.91"
    )
  })

  it("renders nothing for a value that is not money", () => {
    expect(formatMoney({ amount: 1 }, "en-US")).toBe("")
    expect(isMoney(money(1, "EUR"))).toBe(true)
    expect(isMoney(money(1.5, "EUR"))).toBe(false)
  })
})
