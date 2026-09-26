/** A change's values in words: "Priority: High → Urgent", "Density: set to
 * Comfortable", "Emails: added grace@example.com", "Notes: cleared". Each
 * value renders as the property sheet renders it (a reference its RecordRef,
 * a state its StateBadge, an enum its label, a date the day a person would
 * say), long text cut to one line with the whole of it in the hover.
 * Everyday mode leaves out what the host writes (digests, cursors, sync
 * bookkeeping) and moves with nothing to say; technical mode shows every
 * move, by its property key and its raw values. */

import type { ReactNode } from "react"

import { DeclaredValue } from "@/components/property-sheet/property-value"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  isBlank,
  readerMoves,
  shortText,
  type ValueMove,
} from "@/lib/change-values"
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

function Word({ children }: { children: ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>
}

/** A list's items, comma-separated, each drawn as the sheet draws it. */
function Items({
  values,
  spec,
  old = false,
}: {
  values: readonly unknown[]
  spec?: PropSpec
  old?: boolean
}) {
  const item = spec && elementSpec(spec)
  return (
    <span className="inline-flex flex-wrap items-center gap-x-1">
      {values.map((v, i) => (
        <span key={i} className="inline-flex items-center">
          {old ? (
            <Old>
              <ChangeValue value={v} spec={item} />
            </Old>
          ) : (
            <ChangeValue value={v} spec={item} />
          )}
          {i < values.length - 1 && <span className="text-faint">,</span>}
        </span>
      ))}
    </span>
  )
}

function Move({ move, spec }: { move: ValueMove; spec?: PropSpec }) {
  const [technical] = useTechnicalDetails()
  const label = technical ? move.name : (spec?.label ?? humanizeName(move.name))
  let body: ReactNode
  if (move.replaced) {
    body = <Word>replaced</Word>
  } else if (move.changedBack) {
    body = <Word>changed and changed back</Word>
  } else if (move.added || move.removed) {
    const added = move.added ?? []
    const removed = move.removed ?? []
    body = (
      <>
        {added.length > 0 && (
          <>
            <Word>added</Word>
            <Items values={added} spec={spec} />
          </>
        )}
        {added.length > 0 && removed.length > 0 && (
          <span aria-hidden className="text-faint">
            ·
          </span>
        )}
        {removed.length > 0 && (
          <>
            <Word>removed</Word>
            <Items values={removed} spec={spec} old />
          </>
        )}
      </>
    )
  } else if (isBlank(move.after)) {
    body = (
      <>
        <Word>cleared</Word>
        {!move.beforeUnknown && !isBlank(move.before) && (
          <Old>
            <ChangeValue value={move.before} spec={spec} />
          </Old>
        )}
      </>
    )
  } else if (move.beforeUnknown || isBlank(move.before)) {
    body = (
      <>
        <Word>set to</Word>
        <ChangeValue value={move.after} spec={spec} />
      </>
    )
  } else {
    body = (
      <>
        <Old>
          <ChangeValue value={move.before} spec={spec} />
        </Old>
        <span aria-hidden className="text-faint">
          →
        </span>
        <span className="sr-only">to</span>
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
        {label}:
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
  const [technical] = useTechnicalDetails()
  const shown = technical ? moves : readerMoves(moves, specs)
  if (!shown.length) return null
  return (
    <div
      data-slot="value-moves"
      className={cn(
        "flex flex-wrap items-center gap-x-3.5 gap-y-1 text-[12.5px] text-foreground",
        className
      )}
    >
      {shown.map((m) => (
        <Move key={m.name} move={m} spec={specs.get(m.name)} />
      ))}
    </div>
  )
}
