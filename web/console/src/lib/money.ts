/** The `money` datatype, client-side: the stored shape, the substrate's own
 * checks on it (`internal/engine/validate.go` coerceMoney), and the two text
 * forms a person meets.
 *
 * A money value is `{amount: 1999, currency: "EUR", decimals: 2}`: an integer
 * count of minor units, an ISO 4217 code, and how many of the amount's digits
 * follow the decimal point. That value is 19.99 EUR.
 *
 * - `moneyText` is the EXACT text an editor holds, `19.99 EUR`. The digits
 *   after the point ARE the decimal places, so `19.90 EUR` and `19.9 EUR` are
 *   two different values, and `parseMoneyText` reads the text back to exactly
 *   the value it came from.
 * - `formatMoney` is the display, in the reader's locale (`€19.99`), with the
 *   stored scale kept. */

export interface Money {
  amount: number
  currency: string
  decimals: number
}

/** `internal/vocabulary/money.go` MaxMoneyDecimals. */
export const MAX_MONEY_DECIMALS = 18

const CURRENCY = /^[A-Z]{3}$/
const KEYS = new Set(["amount", "currency", "decimals"])

/** The worked value, in the YAML a document carries. */
export const MONEY_EXAMPLE = "{amount: 1999, currency: EUR, decimals: 2}"

/** The worked value, as the text an editor takes. */
export const MONEY_TEXT_EXAMPLE = "19.99 EUR"

/** What a money input takes, said beside it. */
export const MONEY_HINT =
  "An amount and its ISO 4217 code. The digits after the point are the decimal places: 19.90 EUR keeps two."

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

/** Whether a value holds the stored shape: the three members, well formed.
 * A bound is not part of the shape; `checkMoney` holds that. */
export function isMoney(value: unknown): value is Money {
  return isRecord(value) && checkMoney(value) === undefined
}

/** The substrate's refusal of a money value, word for word where it can be,
 * or undefined when the value is admissible. `min`/`max` bound the number the
 * value denotes; the comparison may round through a float here, and the
 * server holds the exact line. */
export function checkMoney(
  value: unknown,
  min?: number,
  max?: number
): string | undefined {
  if (!isRecord(value)) {
    return 'a money value is an object {amount: 1999, currency: "EUR", decimals: 2}'
  }
  for (const key of Object.keys(value)) {
    if (!KEYS.has(key)) {
      return `a money value holds amount, currency and decimals, and "${key}" is none of them`
    }
  }
  const { amount, currency, decimals } = value
  if (amount === undefined) {
    return "a money value needs amount, the integer count of minor units (1999 for 19.99)"
  }
  if (typeof amount === "string") {
    return "amount is an integer count of minor units (1999 for 19.99), not a string"
  }
  if (typeof amount !== "number" || !Number.isInteger(amount)) {
    return "amount: expected an integer"
  }
  if (!Number.isSafeInteger(amount)) {
    return `amount: an int is a safe integer (|value| <= ${Number.MAX_SAFE_INTEGER})`
  }
  if (typeof currency !== "string" || !CURRENCY.test(currency)) {
    return "currency is an ISO 4217 code, three capital letters (EUR)"
  }
  if (decimals === undefined) {
    return "a money value needs decimals, how many of the amount's digits follow the decimal point (2 for cents)"
  }
  if (
    typeof decimals !== "number" ||
    !Number.isInteger(decimals) ||
    decimals < 0 ||
    decimals > MAX_MONEY_DECIMALS
  ) {
    return `decimals is an integer from 0 to ${MAX_MONEY_DECIMALS}`
  }
  const n = Number(moneyDecimal(amount, decimals))
  if (min !== undefined && n < min) return `must be >= ${min}`
  if (max !== undefined && n > max) return `must be <= ${max}`
  return undefined
}

/** An amount of minor units at a scale as its exact digits: 1999 at 2 is
 * "19.99", -5 at 2 is "-0.05", 7 at 0 is "7" (`vocabulary.MoneyDecimal`). */
export function moneyDecimal(amount: number, decimals: number): string {
  const neg = amount < 0
  let digits = String(Math.abs(amount))
  if (decimals > 0) {
    digits = digits.padStart(decimals + 1, "0")
    digits = `${digits.slice(0, -decimals)}.${digits.slice(-decimals)}`
  }
  return neg ? `-${digits}` : digits
}

/** A stored value as the exact text an editor holds: `19.99 EUR`. A value
 * that is not money renders as "". */
export function moneyText(value: unknown): string {
  if (!isMoney(value)) return ""
  return `${moneyDecimal(value.amount, value.decimals)} ${value.currency}`
}

/** A stored value as a reader sees it, in their locale, with the stored scale
 * kept: `€19.99`, `¥500`, `-$2.50`. The digits go to Intl as a string, which
 * it formats exactly, so an amount past a float's precision is not rounded.
 * A value that is not money renders as "". */
export function formatMoney(value: unknown, locale?: string): string {
  if (!isMoney(value)) return ""
  const digits = moneyDecimal(value.amount, value.decimals)
  try {
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency: value.currency,
      minimumFractionDigits: value.decimals,
      maximumFractionDigits: value.decimals,
      // A string is formatted as the exact decimal it spells; the type
      // declares only number and bigint.
    }).format(digits as unknown as number)
  } catch {
    // A scale Intl will not render (past 100 digits there is none, but an
    // engine may refuse sooner) still reads exactly.
    return `${digits} ${value.currency}`
  }
}

const AMOUNT_FIRST = /^([+-]?)(\d+)(?:\.(\d+))?\s*([A-Za-z]{3})$/
const CODE_FIRST = /^([A-Za-z]{3})\s*([+-]?)(\d+)(?:\.(\d+))?$/

/** Read the text an editor holds back into the value: an amount and a code,
 * either order (`19.99 EUR`, `EUR 19.99`, `-2.50 usd`). The digits typed
 * after the point are the decimal places, so `19.90 EUR` stores 1990 at 2. */
export function parseMoneyText(text: string): {
  value?: Money
  error?: string
} {
  const s = text.trim()
  const a = AMOUNT_FIRST.exec(s)
  const c = a ? undefined : CODE_FIRST.exec(s)
  const parts = a
    ? { sign: a[1], whole: a[2], frac: a[3] ?? "", code: a[4] }
    : c
      ? { sign: c[2], whole: c[3], frac: c[4] ?? "", code: c[1] }
      : undefined
  if (!parts) {
    return {
      error: `expected an amount and an ISO 4217 code (${MONEY_TEXT_EXAMPLE})`,
    }
  }
  if (parts.frac.length > MAX_MONEY_DECIMALS) {
    return {
      error: `at most ${MAX_MONEY_DECIMALS} digits follow the decimal point`,
    }
  }
  const minor = `${parts.whole}${parts.frac}`.replace(/^0+(?=\d)/, "")
  const amount = Number(`${parts.sign === "-" ? "-" : ""}${minor}`)
  const value: Money = {
    amount: Object.is(amount, -0) ? 0 : amount,
    currency: parts.code.toUpperCase(),
    decimals: parts.frac.length,
  }
  if (!Number.isSafeInteger(value.amount)) {
    return {
      error: `the amount in minor units must stay within ${Number.MAX_SAFE_INTEGER}`,
    }
  }
  return { value }
}
