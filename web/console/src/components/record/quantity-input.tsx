/** A quantity value's editor: the value as typed digits beside the unit it is
 * in, and where the property declares `comparator: true`, the comparator in
 * front. HTML has no quantity input. The value is a text box that asks for a
 * decimal keypad, because a number input rides a float and changes its value
 * on a scroll; the unit is a native select of the units the declaration
 * admits, grouped by measure when it names none. The value in and out is the
 * editor text `72.5 kg` (lib/units.ts), so a quantity property rides every
 * form path a text property does. */

import { useState, type KeyboardEvent, type Ref } from "react"

import { useUnits } from "@/hooks/use-units"
import { unitsUnavailable } from "@/lib/unit-reader"
import { measureLabel, type QuantityDeclaration } from "@/lib/units"
import { cn } from "@/lib/utils"

interface Parts {
  comparator: string
  value: string
  unit: string
}

const COMPARATOR_OPTIONS = [
  { value: "", text: "=" },
  { value: "<", text: "<" },
  { value: "<=", text: "≤" },
  { value: ">=", text: "≥" },
  { value: ">", text: ">" },
]

/** The editor text of the parts: `<0.5 ng/mL`, or the digits alone while no
 * unit is chosen (which the parse refuses by asking for one). No digits is
 * no value, whatever unit or comparator is chosen: the unit stays in this
 * control's own state, so the text never has to say a unit alone. */
function joined(parts: Parts): string {
  const value = parts.value.trim()
  if (!value) return ""
  return `${parts.comparator}${value}${parts.unit ? ` ${parts.unit}` : ""}`
}

/** The parts of an editor text this control did not write: a stored value's
 * text (`72.5 kg`), a default's, or a row's after the rows above it moved. A
 * lone token is the digits when it reads as the start of a number, else the
 * unit. */
function partsOf(text: string): Parts {
  const m = /^(<=|>=|<|>)?\s*(.*)$/.exec(text.trim())
  const comparator = m?.[1] ?? ""
  const rest = m?.[2] ?? ""
  const space = rest.search(/\s/)
  if (space < 0) {
    return /^[+-]?[\d.]/.test(rest) || !rest
      ? { comparator, value: rest, unit: "" }
      : { comparator, value: "", unit: rest }
  }
  return {
    comparator,
    value: rest.slice(0, space),
    unit: rest.slice(space).trim(),
  }
}

export function QuantityInput({
  id,
  label,
  value,
  onChange,
  spec,
  disabled,
  invalid,
  autoFocus,
  onKeyDown,
  valueRef,
  boxClassName,
  className,
}: {
  /** The value box's id, for the label that names the field. */
  id?: string
  /** The property's name, which each part's accessible name carries. */
  label: string
  /** The editor text, `72.5 kg`, or "" for none. */
  value: string
  onChange: (text: string) => void
  /** The declaration: its measure, its units, its comparator, its default. */
  spec: QuantityDeclaration
  disabled?: boolean
  invalid?: boolean
  autoFocus?: boolean
  /** Keys on the value box: the sheet saves on Enter, a list adds a row. */
  onKeyDown?: (e: KeyboardEvent<HTMLInputElement>) => void
  valueRef?: Ref<HTMLInputElement>
  /** The frame every part wears, the surface's own input look. */
  boxClassName?: string
  className?: string
}) {
  const units = useUnits()
  // A graph-enabled server's units are the repository's: until they load the
  // control offers none and takes no value, rather than the table's.
  const unavailable = unitsUnavailable(units)
  const off = disabled || unavailable !== undefined
  // Controlled by the text, with the parts this control wrote kept beside it:
  // a unit chosen before any digits is not in the text, and a text from
  // outside (a list's row after the one above it goes) is read afresh.
  const [held, setHeld] = useState(() => ({
    text: value,
    parts: partsOf(value),
  }))
  // The unit a blank value opens on is read once the reader answers, and
  // kept from then on.
  const [opened, setOpened] = useState<string | undefined>(() =>
    unavailable ? undefined : units.opening(spec)
  )
  const opening = opened ?? (unavailable ? "" : units.opening(spec))
  if (opened === undefined && !unavailable) setOpened(opening)
  const own = held.text === value ? held.parts : partsOf(value)
  const parts = { ...own, unit: own.unit || opening }
  const groups = unavailable ? [] : units.unitsFor(spec)
  const offered = groups.some((g) => g.units.includes(parts.unit))
  const showComparator = Boolean(spec.comparator || parts.comparator)

  function change(next: Parts) {
    const text = joined(next)
    setHeld({ text, parts: next })
    onChange(text)
  }

  const option = (code: string) => (
    <option key={code} value={code}>
      {units.label(code)}
    </option>
  )

  return (
    <div
      role="group"
      aria-label={label}
      className={cn("flex min-w-0 items-center gap-1.5", className)}
    >
      {showComparator && (
        <select
          aria-label={`${label}, comparator`}
          title="Exactly, or a bound the value lies beyond"
          disabled={off}
          value={parts.comparator}
          onChange={(e) => change({ ...parts, comparator: e.target.value })}
          className={cn(boxClassName, "w-14 shrink-0")}
        >
          {COMPARATOR_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.text}
            </option>
          ))}
        </select>
      )}
      <input
        id={id}
        ref={valueRef}
        type="text"
        inputMode="decimal"
        autoComplete="off"
        aria-label={`${label}, value`}
        aria-invalid={invalid || undefined}
        autoFocus={autoFocus}
        disabled={off}
        placeholder="0"
        value={parts.value}
        onChange={(e) => change({ ...parts, value: e.target.value })}
        onKeyDown={onKeyDown}
        className={cn(boxClassName, "w-28 shrink-0 text-right tabular-nums")}
      />
      <select
        aria-label={`${label}, unit`}
        aria-invalid={(invalid && !parts.unit) || undefined}
        disabled={off}
        value={parts.unit}
        onChange={(e) => {
          units.remember(e.target.value)
          change({ ...parts, unit: e.target.value })
        }}
        className={cn(boxClassName, "w-auto max-w-56 min-w-0")}
      >
        {!parts.unit && (
          <option value="">{unavailable ? "Units loading…" : "Unit…"}</option>
        )}
        {/* A unit no list offers (a stored compound, `GJ`) stays chosen, so
            the value it holds is written back as it was. */}
        {parts.unit && !offered && option(parts.unit)}
        {groups.length === 1
          ? groups[0].units.map(option)
          : groups.map((g) => (
              <optgroup
                key={g.measure ?? ""}
                label={g.measure ? measureLabel(g.measure) : "Units"}
              >
                {g.units.map(option)}
              </optgroup>
            ))}
      </select>
      {unavailable && (
        <span role="status" className="text-xs text-muted-foreground">
          {unavailable}
        </span>
      )}
    </div>
  )
}
