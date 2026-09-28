/** The money datatype, client-side: the substrate's own checks on the stored
 * shape, the exact text an editor holds, and the reader's display. */

import { describe, expect, it } from "vitest"

import {
  checkMoney,
  formatMoney,
  isMoney,
  moneyDecimal,
  moneyText,
  parseMoneyText,
} from "./money"

const money = (amount: unknown, currency: unknown, decimals: unknown) => ({
  amount,
  currency,
  decimals,
})

describe("checkMoney", () => {
  it("admits what the substrate admits", () => {
    expect(checkMoney(money(1999, "EUR", 2))).toBeUndefined()
    expect(checkMoney(money(-250, "USD", 2))).toBeUndefined()
    expect(checkMoney(money(500, "JPY", 0))).toBeUndefined()
    expect(checkMoney(money(1, "ETH", 18))).toBeUndefined()
  })

  it("names the reason a value is wrong, in the substrate's words", () => {
    expect(checkMoney("19.99")).toMatch(/is an object/)
    expect(checkMoney(money(19.99, "EUR", 2))).toMatch(/expected an integer/)
    expect(checkMoney(money("1999", "EUR", 2))).toMatch(/not a string/)
    expect(checkMoney(money(2 ** 53, "EUR", 2))).toMatch(/safe integer/)
    expect(checkMoney(money(1999, "eur", 2))).toMatch(/ISO 4217/)
    expect(checkMoney({ amount: 1999, currency: "EUR" })).toMatch(
      /needs decimals/
    )
    expect(checkMoney(money(1999, "EUR", 19))).toMatch(/from 0 to 18/)
    expect(checkMoney({ ...money(1999, "EUR", 2), symbol: "€" })).toMatch(
      /"symbol" is none of them/
    )
  })

  it("bounds the number the value denotes, not its amount", () => {
    expect(checkMoney(money(-1, "EUR", 2), 0)).toMatch(/>= 0/)
    expect(checkMoney(money(1000, "EUR", 2), undefined, 10)).toBeUndefined()
    expect(checkMoney(money(1001, "EUR", 2), undefined, 10)).toMatch(/<= 10/)
  })
})

describe("the exact text", () => {
  it("renders the digits at the stored scale", () => {
    expect(moneyDecimal(1999, 2)).toBe("19.99")
    expect(moneyDecimal(1990, 2)).toBe("19.90")
    expect(moneyDecimal(-5, 2)).toBe("-0.05")
    expect(moneyDecimal(7, 0)).toBe("7")
    expect(moneyText(money(1990, "EUR", 2))).toBe("19.90 EUR")
    expect(moneyText("19.90 EUR")).toBe("")
  })

  it("reads back to exactly the value it came from", () => {
    for (const value of [
      money(1999, "EUR", 2),
      money(1990, "EUR", 2),
      money(-5, "USD", 2),
      money(500, "JPY", 0),
      money(1, "ETH", 18),
    ]) {
      expect(parseMoneyText(moneyText(value))).toEqual({ value })
    }
  })

  it("takes the code on either side, in either case", () => {
    const want = { value: money(1999, "EUR", 2) }
    expect(parseMoneyText("EUR 19.99")).toEqual(want)
    expect(parseMoneyText("19.99eur")).toEqual(want)
    expect(parseMoneyText("  +19.99 EUR ")).toEqual(want)
    expect(parseMoneyText("-0.00 EUR")).toEqual({ value: money(0, "EUR", 2) })
  })

  it("refuses text that is no amount and code", () => {
    expect(parseMoneyText("19.99").error).toMatch(/ISO 4217/)
    expect(parseMoneyText("€19.99").error).toMatch(/ISO 4217/)
    expect(parseMoneyText("19,99 EUR").error).toMatch(/ISO 4217/)
    expect(parseMoneyText("1e3 EUR").error).toMatch(/ISO 4217/)
    expect(parseMoneyText("90071992547409.92 EUR").error).toMatch(/minor units/)
    expect(parseMoneyText(`0.${"1".repeat(19)} EUR`).error).toMatch(
      /at most 18/
    )
  })
})

describe("formatMoney", () => {
  it("shows the currency and keeps the stored scale", () => {
    expect(formatMoney(money(1999, "EUR", 2), "en-US")).toBe("€19.99")
    expect(formatMoney(money(1990, "USD", 3), "en-US")).toBe("$1.990")
    expect(formatMoney(money(500, "JPY", 0), "en-US")).toBe("¥500")
    expect(formatMoney(money(-250, "USD", 2), "en-US")).toBe("-$2.50")
  })

  it("formats digits past a float's precision exactly", () => {
    expect(formatMoney(money(2 ** 53 - 1, "USD", 2), "en-US")).toBe(
      "$90,071,992,547,409.91"
    )
  })

  it("renders nothing for a value that is not money", () => {
    expect(formatMoney({ amount: 1 }, "en-US")).toBe("")
    expect(isMoney(money(1, "EUR", 2))).toBe(true)
    expect(isMoney(money(1, "EUR", 2.5))).toBe(false)
  })
})
