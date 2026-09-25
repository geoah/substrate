/** A change's values in words: "Priority  High → Urgent", "Emails  +
 * grace@example.com", "Notes  cleared". Each value renders as the property
 * sheet renders it (a reference its RecordRef, a state its StateBadge, an
 * enum its label, a date the day a person would say), long text cut to one
 * line with the whole of it in the hover. Technical mode shows the property
 * keys and the raw values instead. */

import type { ReactNode } from "react"

import { DeclaredValue } from "@/components/property-sheet/property-value"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { shortText, type ValueMove } from "@/lib/change-values"
import { elementSpec, humanizeName, type PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

/** Datatypes whose value is a sentence of its own, cut to one line here. */
const TEXT_KINDS = new Set(["string", "text", "markdown", "url", "email"])

function Raw({ value }: { value: unknown }) {
  const { text, full } = shortText(value)
  return (
    <span title={full} className="font-mono text-[11.5px] break-all">
      {text}
    </span>
  )
}

/** One value, one line. */
export function ChangeValue({
  value,
  spec,
}: {
  value: unknown
  spec?: PropSpec
}) {
  const [technical] = useTechnicalDetails()
  if (technical) return <Raw value={value} />
  if (Array.isArray(value)) {
    const item = spec && elementSpec(spec)
    return (
      <span className="inline-flex flex-wrap items-center gap-x-1">
        {value.map((v, i) => (
          <span key={i} className="inline-flex items-center">
            <ChangeValue value={v} spec={item} />
            {i < value.length - 1 && <span className="text-faint">,</span>}
          </span>
        ))}
      </span>
    )
  }
  const scalar =
    spec && (spec.repeated || spec.keyed ? elementSpec(spec) : spec)
  const prose =
    !scalar ||
    (typeof value === "object" && scalar.kind !== "reference") ||
    (TEXT_KINDS.has(scalar.kind) && !scalar.values?.length) ||
    spec?.keyed
  if (prose && scalar?.kind !== "secret") {
    const { text, full } = shortText(value)
    return (
      <span title={full === text ? undefined : full} className="break-words">
        {text}
      </span>
    )
  }
  return <DeclaredValue spec={scalar} value={value} />
}

function Old({ children }: { children: ReactNode }) {
  return (
    <span className="text-faint line-through decoration-faint">{children}</span>
  )
}

function Item({
  sign,
  value,
  spec,
}: {
  sign: "+" | "−"
  value: unknown
  spec?: PropSpec
}) {
  const item = spec && elementSpec(spec)
  return (
    <span className="inline-flex items-center gap-1">
      <span
        aria-label={sign === "+" ? "added" : "removed"}
        className={sign === "+" ? "text-ok" : "text-faint"}
      >
        {sign}
      </span>
      {sign === "+" ? (
        <ChangeValue value={value} spec={item} />
      ) : (
        <Old>
          <ChangeValue value={value} spec={item} />
        </Old>
      )}
    </span>
  )
}

function Move({ move, spec }: { move: ValueMove; spec?: PropSpec }) {
  const [technical] = useTechnicalDetails()
  const label = technical ? move.name : (spec?.label ?? humanizeName(move.name))
  const has = (v: unknown) => v !== undefined
  let body: ReactNode
  if (move.added || move.removed) {
    body = (
      <>
        {(move.added ?? []).map((v, i) => (
          <Item key={`a${i}`} sign="+" value={v} spec={spec} />
        ))}
        {(move.removed ?? []).map((v, i) => (
          <Item key={`r${i}`} sign="−" value={v} spec={spec} />
        ))}
      </>
    )
  } else if (!has(move.after)) {
    body = move.beforeUnknown ? (
      <span className="text-faint">cleared</span>
    ) : (
      <Item sign="−" value={move.before} spec={spec} />
    )
  } else if (move.beforeUnknown || !has(move.before)) {
    body = move.beforeUnknown ? (
      <>
        <span aria-hidden className="text-faint">
          →
        </span>
        <ChangeValue value={move.after} spec={spec} />
      </>
    ) : (
      <Item sign="+" value={move.after} spec={spec} />
    )
  } else {
    body = (
      <>
        <Old>
          <ChangeValue value={move.before} spec={spec} />
        </Old>
        <span aria-label="to" className="text-faint">
          →
        </span>
        <ChangeValue value={move.after} spec={spec} />
      </>
    )
  }
  return (
    <span
      data-slot="value-move"
      className="inline-flex max-w-full min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5"
    >
      <span
        className={cn(
          "font-medium text-muted-foreground",
          technical && "font-mono text-[11.5px]"
        )}
      >
        {label}
      </span>
      {body}
    </span>
  )
}

/** Every move, wrapped as one line of the sentence's detail. */
export function ValueMoves({
  moves,
  specs,
  className,
}: {
  moves: readonly ValueMove[]
  specs: ReadonlyMap<string, PropSpec>
  className?: string
}) {
  if (!moves.length) return null
  return (
    <div
      data-slot="value-moves"
      className={cn(
        "flex flex-wrap items-center gap-x-3.5 gap-y-1 text-[12.5px] text-foreground",
        className
      )}
    >
      {moves.map((m) => (
        <Move key={m.name} move={m} spec={specs.get(m.name)} />
      ))}
    </div>
  )
}
