/** A money value's editor: the amount as typed digits beside the currency it
 * counts. HTML has no money input. The amount is a text box that asks for a
 * decimal keypad, because a number input rides a float and changes its value
 * on a scroll; the currency is a native select, which types ahead by code.
 * The value in and out is the editor text `19.99 EUR` (lib/money.ts), so a
 * money property rides every form path a text property does. */

import { useState, type KeyboardEvent, type Ref } from "react"

import {
  CURRENCIES,
  currencyDecimals,
  currencyName,
  lastCurrency,
  rememberCurrency,
} from "@/lib/money"
import { cn } from "@/lib/utils"

let options: Array<{ code: string; text: string }> | undefined

/** The currencies as the select lists them, the code first so typing it
 * finds it: "EUR · Euro". Built once, in the reader's language. */
function currencyOptions() {
  options ??= CURRENCIES.map((code) => {
    const name = currencyName(code)
    return { code, text: name ? `${code} · ${name}` : code }
  })
  return options
}

/** The editor text of the two halves: the amount then the code (`19.99 EUR`),
 * the code alone while no amount is typed (which reads as no value), or the
 * amount alone while no currency is chosen (which the parse refuses by asking
 * for one). */
function joined(amount: string, currency: string): string {
  return [amount.trim(), currency].filter(Boolean).join(" ")
}

/** The halves of the editor text this control writes. The code is the
 * trailing capitals, so a letter typed into the amount stays in the amount
 * for the parse to refuse. */
function halves(text: string): { amount: string; currency: string } {
  const m = /^(.*?)\s*([A-Z]{3})?$/.exec(text.trim())
  return { amount: m?.[1] ?? "", currency: m?.[2] ?? "" }
}

export function MoneyInput({
  id,
  label,
  value,
  onChange,
  fallbackCurrency,
  disabled,
  invalid,
  autoFocus,
  onKeyDown,
  amountRef,
  boxClassName,
  className,
}: {
  /** The amount box's id, for the label that names the field. */
  id?: string
  /** The property's name, which each half's accessible name carries. */
  label: string
  /** The editor text, `19.99 EUR`, or "" for none. */
  value: string
  onChange: (text: string) => void
  /** The currency a blank value opens on, before the one picked last. */
  fallbackCurrency?: string
  disabled?: boolean
  invalid?: boolean
  autoFocus?: boolean
  /** Keys on the amount box: the sheet saves on Enter, a list adds a row. */
  onKeyDown?: (e: KeyboardEvent<HTMLInputElement>) => void
  amountRef?: Ref<HTMLInputElement>
  /** The frame both halves wear, the surface's own input look. */
  boxClassName?: string
  className?: string
}) {
  // Controlled: the text is the whole state, so a list that keys its rows by
  // position shows each row's own value after a row above it goes.
  const held = halves(value)
  const [opening] = useState(() => fallbackCurrency || lastCurrency() || "")
  const parts = {
    amount: held.amount,
    currency: held.currency || opening,
  }
  const decimals = currencyDecimals(parts.currency)

  function change(next: { amount: string; currency: string }) {
    onChange(joined(next.amount, next.currency))
  }

  return (
    <div
      role="group"
      aria-label={label}
      className={cn("flex min-w-0 items-center gap-1.5", className)}
    >
      <input
        id={id}
        ref={amountRef}
        type="text"
        inputMode="decimal"
        autoComplete="off"
        aria-label={`${label}, amount`}
        aria-invalid={invalid || undefined}
        autoFocus={autoFocus}
        disabled={disabled}
        placeholder={
          decimals ? `0.${"0".repeat(decimals)}` : decimals === 0 ? "0" : "0.00"
        }
        value={parts.amount}
        onChange={(e) => change({ ...parts, amount: e.target.value })}
        onKeyDown={onKeyDown}
        className={cn(boxClassName, "w-32 shrink-0 text-right tabular-nums")}
      />
      <select
        aria-label={`${label}, currency`}
        aria-invalid={(invalid && !parts.currency) || undefined}
        disabled={disabled}
        value={parts.currency}
        onChange={(e) => {
          rememberCurrency(e.target.value)
          change({ ...parts, currency: e.target.value })
        }}
        className={cn(boxClassName, "w-auto max-w-56 min-w-0")}
      >
        {!parts.currency && <option value="">Currency…</option>}
        {currencyOptions().map((o) => (
          <option key={o.code} value={o.code}>
            {o.text}
          </option>
        ))}
      </select>
    </div>
  )
}
