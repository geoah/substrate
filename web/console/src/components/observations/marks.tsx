/** The observations pages' small marks, each one way everywhere: a source or
 * a thing observed as a `RecordRef` (or "You" where nothing is named, the
 * owner), a metric's form in words, a category's code in words, and one
 * reading as its digits and unit. */

import { EmptyValue } from "@/components/identity/empty-value"
import { IdText } from "@/components/identity/id-text"
import { QuantityValue } from "@/components/identity/quantity-value"
import { RecordRef } from "@/components/identity/record-ref"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  codeWords,
  readingText,
  refPathOf,
  roundDigits,
  SHAPE_WORDS,
  type ReadingShape,
} from "@/lib/observations"
import { splitRecordPath } from "@/lib/record-path"
import { formatQuantity } from "@/lib/units"
import { cn } from "@/lib/utils"

/** A record a group value names: its `RecordRef` where the value or the
 * reference's pin says its kind, else the id as it came (the answer named a
 * record by id alone, and no pin says which kind). */
export function GroupRecord({
  value,
  pin,
  title,
  className,
}: {
  value: string
  pin?: string
  title?: string
  className?: string
}) {
  const path = refPathOf(value, pin)
  const parts = path ? splitRecordPath(path) : undefined
  if (!parts) return <IdText value={value} className={className} />
  return (
    <RecordRef
      kind={parts.kind}
      id={parts.id}
      title={title}
      className={className}
    />
  )
}

/** What a value is about: the record it names, or the owner where it names
 * none (decision 0155). */
export function AboutMark({
  value,
  pin,
  title,
}: {
  value?: string
  pin?: string
  title?: string
}) {
  if (!value) return <span className="text-muted-foreground">You</span>
  return <GroupRecord value={value} pin={pin} title={title} />
}

/** A metric's form in words, with the stored word in technical mode. */
export function ShapeWord({
  shape,
  className,
}: {
  shape: ReadingShape
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  return (
    <span className={cn("inline-flex items-baseline gap-1.5", className)}>
      {SHAPE_WORDS[shape]}
      {technical && (
        <span className="font-mono text-[11.5px] text-faint">{shape}</span>
      )}
    </span>
  )
}

/** A category's code in words, the stored code beside it in technical
 * mode. */
export function CodeMark({ code }: { code?: string }) {
  const [technical] = useTechnicalDetails()
  if (!code) return <EmptyValue />
  return (
    <span className="inline-flex items-baseline gap-1.5">
      {codeWords(code)}
      {technical && (
        <span className="font-mono text-[11.5px] text-faint">{code}</span>
      )}
    </span>
  )
}

/** One reading: its digits with the unit's print symbol, the digits alone
 * where no unit came, and the empty mark where there is no answer. A
 * `precision` rounds a computed value's digits for display; the exact digits
 * are on hover. */
export function Reading({
  value,
  unit,
  precision,
  comparator,
  bound,
  className,
}: {
  value: string | null | undefined
  unit?: string
  precision?: number
  /** The value is a bound (`<`, `<=`, `>=`, `>`): the true one lies
   * beyond it, and the comparator is shown in front of the digits. */
  comparator?: string
  /** Why there is no value although the row holds some (`boundWords`). */
  bound?: string
  className?: string
}) {
  if (value === null || value === undefined) {
    return bound ? (
      <span
        data-slot="bounded"
        className={cn("text-muted-foreground", className)}
        title={bound}
      >
        no number
      </span>
    ) : (
      <EmptyValue />
    )
  }
  const shown = precision === undefined ? value : roundDigits(value, precision)
  if (shown !== value) {
    return (
      <span
        className={cn("tabular-nums", className)}
        title={`${comparator ?? ""}${value}`}
      >
        {readingText(shown, unit, undefined, comparator)}
      </span>
    )
  }
  if (unit && formatQuantity({ value, unit })) {
    return (
      <QuantityValue
        value={{ value, unit, ...(comparator ? { comparator } : {}) }}
        places={precision}
        className={className}
      />
    )
  }
  return (
    <span className={cn("tabular-nums", className)}>
      {comparator}
      {value}
    </span>
  )
}
