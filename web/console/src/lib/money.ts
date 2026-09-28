/** The `money` datatype, client-side: the stored shape, the substrate's own
 * checks on it (`internal/engine/validate.go` coerceMoney), the currencies it
 * knows, and the text an editor holds.
 *
 * A money value is `{amount: 1999, currency: "EUR"}`: an integer count of the
 * currency's minor units and its ISO 4217 code. The currency's minor unit says
 * where the decimal point sits, so that value is 19.99 EUR, and
 * `{amount: 1999, currency: "JPY"}` is 1999 yen.
 *
 * - `moneyText` is the EXACT text an editor holds, `19.99 EUR`, and
 *   `parseMoneyText` reads it back to the same value.
 * - `formatMoney` is the display, in the reader's locale (`€19.99`). */

export interface Money {
  amount: number
  currency: string
}

/** ISO 4217's minor unit per active code, the engine's own table
 * (`internal/vocabulary/money.go`). `TestConsoleCurrenciesMatchTheEngine`
 * reads this block back and fails when the two differ, so keep one
 * `CODE: digits,` entry per line. */
export const CURRENCY_DECIMALS: Record<string, number> = {
  AED: 2,
  AFN: 2,
  ALL: 2,
  AMD: 2,
  ANG: 2,
  AOA: 2,
  ARS: 2,
  AUD: 2,
  AWG: 2,
  AZN: 2,
  BAM: 2,
  BBD: 2,
  BDT: 2,
  BGN: 2,
  BHD: 3,
  BIF: 0,
  BMD: 2,
  BND: 2,
  BOB: 2,
  BOV: 2,
  BRL: 2,
  BSD: 2,
  BTN: 2,
  BWP: 2,
  BYN: 2,
  BZD: 2,
  CAD: 2,
  CDF: 2,
  CHE: 2,
  CHF: 2,
  CHW: 2,
  CLF: 4,
  CLP: 0,
  CNY: 2,
  COP: 2,
  COU: 2,
  CRC: 2,
  CUP: 2,
  CVE: 2,
  CZK: 2,
  DJF: 0,
  DKK: 2,
  DOP: 2,
  DZD: 2,
  EGP: 2,
  ERN: 2,
  ETB: 2,
  EUR: 2,
  FJD: 2,
  FKP: 2,
  GBP: 2,
  GEL: 2,
  GHS: 2,
  GIP: 2,
  GMD: 2,
  GNF: 0,
  GTQ: 2,
  GYD: 2,
  HKD: 2,
  HNL: 2,
  HTG: 2,
  HUF: 2,
  IDR: 2,
  ILS: 2,
  INR: 2,
  IQD: 3,
  IRR: 2,
  ISK: 0,
  JMD: 2,
  JOD: 3,
  JPY: 0,
  KES: 2,
  KGS: 2,
  KHR: 2,
  KMF: 0,
  KPW: 2,
  KRW: 0,
  KWD: 3,
  KYD: 2,
  KZT: 2,
  LAK: 2,
  LBP: 2,
  LKR: 2,
  LRD: 2,
  LSL: 2,
  LYD: 3,
  MAD: 2,
  MDL: 2,
  MGA: 2,
  MKD: 2,
  MMK: 2,
  MNT: 2,
  MOP: 2,
  MRU: 2,
  MUR: 2,
  MVR: 2,
  MWK: 2,
  MXN: 2,
  MXV: 2,
  MYR: 2,
  MZN: 2,
  NAD: 2,
  NGN: 2,
  NIO: 2,
  NOK: 2,
  NPR: 2,
  NZD: 2,
  OMR: 3,
  PAB: 2,
  PEN: 2,
  PGK: 2,
  PHP: 2,
  PKR: 2,
  PLN: 2,
  PYG: 0,
  QAR: 2,
  RON: 2,
  RSD: 2,
  RUB: 2,
  RWF: 0,
  SAR: 2,
  SBD: 2,
  SCR: 2,
  SDG: 2,
  SEK: 2,
  SGD: 2,
  SHP: 2,
  SLE: 2,
  SOS: 2,
  SRD: 2,
  SSP: 2,
  STN: 2,
  SVC: 2,
  SYP: 2,
  SZL: 2,
  THB: 2,
  TJS: 2,
  TMT: 2,
  TND: 3,
  TOP: 2,
  TRY: 2,
  TTD: 2,
  TWD: 2,
  TZS: 2,
  UAH: 2,
  UGX: 0,
  USD: 2,
  USN: 2,
  UYI: 0,
  UYU: 2,
  UYW: 4,
  UZS: 2,
  VED: 2,
  VES: 2,
  VND: 0,
  VUV: 0,
  WST: 2,
  XAF: 0,
  XCD: 2,
  XCG: 2,
  XOF: 0,
  XPF: 0,
  YER: 2,
  ZAR: 2,
  ZMW: 2,
  ZWG: 2,
}

/** Every known code, sorted. */
export const CURRENCIES = Object.keys(CURRENCY_DECIMALS).sort()

const KEYS = new Set(["amount", "currency"])

/** The worked value, in the YAML a document carries. */
export const MONEY_EXAMPLE = "{amount: 1999, currency: EUR}"

/** The worked value, as the text an editor takes. */
export const MONEY_TEXT_EXAMPLE = "19.99 EUR"

/** A known currency's minor unit: 2 for EUR, 0 for JPY, 3 for KWD. */
export function currencyDecimals(code: string): number | undefined {
  return Object.hasOwn(CURRENCY_DECIMALS, code)
    ? CURRENCY_DECIMALS[code]
    : undefined
}

/** A currency's name in the reader's language ("Euro"), or undefined where
 * the browser has none. */
export function currencyName(
  code: string,
  locale?: string
): string | undefined {
  try {
    const name = new Intl.DisplayNames(locale, { type: "currency" }).of(code)
    return name && name !== code ? name : undefined
  } catch {
    return undefined
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

/** Whether a value holds the stored shape. A bound is not part of the shape;
 * `checkMoney` holds that. */
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
    return 'a money value is an object {amount: 1999, currency: "EUR"}'
  }
  for (const key of Object.keys(value)) {
    if (!KEYS.has(key)) {
      return `a money value holds amount and currency, and "${key}" is neither: the currency says where the decimal point sits`
    }
  }
  const { amount, currency } = value
  if (amount === undefined) {
    return "a money value needs amount, the integer count of minor units (1999 for 19.99 EUR)"
  }
  if (typeof amount === "string") {
    return "amount is an integer count of minor units (1999 for 19.99 EUR), not a string"
  }
  if (typeof amount !== "number" || !Number.isInteger(amount)) {
    return "amount: expected an integer"
  }
  if (!Number.isSafeInteger(amount)) {
    return `amount: an int is a safe integer (|value| <= ${Number.MAX_SAFE_INTEGER})`
  }
  const decimals =
    typeof currency === "string" ? currencyDecimals(currency) : undefined
  if (decimals === undefined) {
    return "currency is an active ISO 4217 code, three capital letters (EUR)"
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

/** A stored value's amount as the digits a person types: 1999 EUR is
 * "19.99". Not money: "". */
export function moneyAmountText(value: unknown): string {
  if (!isMoney(value)) return ""
  return moneyDecimal(value.amount, currencyDecimals(value.currency) ?? 0)
}

/** A stored value as the exact text an editor holds: `19.99 EUR`. Not
 * money: "". */
export function moneyText(value: unknown): string {
  if (!isMoney(value)) return ""
  return `${moneyAmountText(value)} ${value.currency}`
}

/** A stored value as a reader sees it, in their locale, at the currency's own
 * minor unit: `€19.99`, `¥500`, `-$2.50`. The digits go to Intl as a string,
 * which it formats exactly, so an amount past a float's precision is not
 * rounded. The minor unit is passed, not left to Intl, whose locale data
 * disagrees with ISO 4217 for a few codes. Not money: "". */
export function formatMoney(value: unknown, locale?: string): string {
  if (!isMoney(value)) return ""
  const decimals = currencyDecimals(value.currency) ?? 0
  const digits = moneyDecimal(value.amount, decimals)
  try {
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency: value.currency,
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
      // A string is formatted as the exact decimal it spells; the type
      // declares only number and bigint.
    }).format(digits as unknown as number)
  } catch {
    return `${digits} ${value.currency}`
  }
}

const AMOUNT = /^([+-]?)(\d+)(?:\.(\d+))?$/

/** Read typed digits into minor units of a currency: "19.9" EUR is 1990, and a
 * fraction longer than the currency's minor unit is refused rather than
 * rounded. */
export function parseAmount(
  text: string,
  currency: string
): { amount?: number; error?: string } {
  const decimals = currencyDecimals(currency)
  if (decimals === undefined) {
    return { error: "choose a currency" }
  }
  const m = AMOUNT.exec(text.trim())
  if (!m) return { error: "expected an amount like 19.99" }
  const frac = m[3] ?? ""
  if (frac.length > decimals) {
    return {
      error:
        decimals === 0
          ? `${currency} has no decimal places`
          : `${currency} has ${decimals} decimal place${decimals === 1 ? "" : "s"}`,
    }
  }
  const minor = `${m[2]}${frac.padEnd(decimals, "0")}`.replace(/^0+(?=\d)/, "")
  const amount = Number(`${m[1] === "-" ? "-" : ""}${minor}`)
  if (!Number.isSafeInteger(amount)) {
    return {
      error: `the amount in minor units must stay within ${Number.MAX_SAFE_INTEGER}`,
    }
  }
  return { amount: Object.is(amount, -0) ? 0 : amount }
}

/** The two halves of an editor's text, as typed: `19.99 EUR`, `EUR 19.99`,
 * `-2.50 usd`. The code is upper-cased; either half may be missing. */
export function splitMoneyText(text: string): {
  amount: string
  currency: string
} {
  const parts = text.trim().split(/\s+/).filter(Boolean)
  let amount = ""
  let currency = ""
  for (const part of parts) {
    if (/^[A-Za-z]{3}$/.test(part)) currency = part.toUpperCase()
    else amount = amount ? `${amount} ${part}` : part
  }
  return { amount, currency }
}

/** Read the text an editor holds back into the value. */
export function parseMoneyText(text: string): {
  value?: Money
  error?: string
} {
  const { amount, currency } = splitMoneyText(text)
  if (!currency) {
    return amount
      ? { error: "choose a currency" }
      : { error: `expected an amount and a currency (${MONEY_TEXT_EXAMPLE})` }
  }
  if (currencyDecimals(currency) === undefined) {
    return { error: `${currency} is not an active ISO 4217 currency` }
  }
  const parsed = parseAmount(amount, currency)
  if (parsed.error) return { error: parsed.error }
  return { value: { amount: parsed.amount!, currency } }
}

/** The currency a stored value counts, or undefined. */
export function moneyCurrency(value: unknown): string | undefined {
  return isMoney(value) ? value.currency : undefined
}

const LAST_CURRENCY = "substrate.console.lastCurrency"

/** The currency this reader picked last, which a blank money input opens on. */
export function lastCurrency(): string | undefined {
  try {
    const code = localStorage.getItem(LAST_CURRENCY) ?? ""
    return currencyDecimals(code) === undefined ? undefined : code
  } catch {
    return undefined
  }
}

export function rememberCurrency(code: string): void {
  try {
    if (currencyDecimals(code) !== undefined) {
      localStorage.setItem(LAST_CURRENCY, code)
    }
  } catch {
    // Storage denied: the next blank input opens on no currency.
  }
}
