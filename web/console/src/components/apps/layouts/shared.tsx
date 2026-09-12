/* eslint-disable react-refresh/only-export-components -- the cell helpers and
 * the create form are one module by design: the three layouts share them and
 * nothing here hot-reloads alone. */
/** What the board, the detail and the form layouts share and the foundation
 * does not already ship: the `show` cells of one row (over `cellNode`, the
 * list's own per-datatype renderer), a detail's richer value (a reference as
 * its pill, prose as prose), a board's columns, and the create form the form
 * layout mounts standalone, the same fields and seed the foundation's
 * `FormSheet` shows inside a sheet. */

import { useMemo, useState, type FormEvent, type ReactNode } from "react"
import { useQueryClient } from "@tanstack/react-query"

import { cellNode } from "@/components/apps/list-view"
import { ProblemStrip } from "@/components/apps/problems"
import { PropertyField } from "@/components/record/property-field"
import { ReferenceValue } from "@/components/record/reference-value"
import { StateBadge } from "@/components/state-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Spinner } from "@/components/ui/spinner"
import {
  readReference,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import { createSeed, runAction } from "@/lib/apps/actions"
import { specOf } from "@/lib/apps/cond"
import {
  headingAlternatives,
  newIdempotencyKey,
  promptFields,
  seedField,
} from "@/lib/apps/form"
import { admittedStates } from "@/lib/apps/machine"
import { titleOf } from "@/lib/apps/referents"
import type { ActionHost, ActionSpec, ViewSpec } from "@/lib/apps/spec"
import { relativeDay } from "@/lib/apps/time"
import { columnProperties } from "@/lib/definition"
import { shortDateTime } from "@/lib/format"
import {
  initialValues,
  toProperties,
  validate,
  type FormField,
  type FormMode,
  type FormValue,
  type FormValues,
} from "@/lib/record-form"
import { recordPath } from "@/lib/record-path"
import {
  editableValue,
  formatValue,
  humanizeName,
  type PropSpec,
} from "@/lib/record-schema"
import { cn } from "@/lib/utils"

// ── what a row shows ────────────────────────────────────────────────────────

/** How many cells a row draws when the view names none, as the list does. */
const DEFAULT_CELLS = 3

/** The names a row shows after the title: the declared `show`, else the
 * kind's column properties minus the one the title already reads. */
export function showNames(
  spec: ViewSpec,
  kind: KindInfo | undefined
): string[] {
  if (spec.show.length) return spec.show
  if (!kind) return []
  const heading = new Set(headingAlternatives(kind))
  return columnProperties(kind)
    .map((p) => p.name)
    .filter((name) => !heading.has(name))
    .slice(0, DEFAULT_CELLS)
}

/** The reference-typed names among what a row shows, for one referent read. */
export function referenceNames(
  names: string[],
  kind: KindInfo | undefined
): string[] {
  return names.filter((n) => specOf(kind, n)?.kind === "reference")
}

export interface Column {
  key: string
  label: string
}

/** A board's columns: the values its filter admits for the grouped property
 * (`in`, else the one `eq`), else every declared one, in declaration order. A
 * state keeps its data casing; an enum shows its authored label. */
export function columnsOf(spec: PropSpec, filter: RecordFilter): Column[] {
  let keys: string[]
  if (spec.kind === "state") {
    keys = admittedStates(filter, spec)
  } else {
    const cond = filter.properties?.[spec.name]
    if (cond?.in?.length) keys = cond.in.map(String)
    else if (cond?.eq !== undefined) keys = [String(cond.eq)]
    else keys = spec.values?.map((v) => v.value) ?? []
  }
  return keys.map((key) => ({
    key,
    label: spec.values?.find((v) => v.value === key)?.label || key,
  }))
}

/** The `show` cells of one row, in order, each drawn by the list's own
 * renderer, which already skips an absent value and an enum at its default. */
export function ShowCells({
  record,
  kind,
  names,
  titles,
  className,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  names: string[]
  titles: Map<string, string>
  className?: string
}) {
  const cells = names.flatMap((name) => {
    const spec = specOf(kind, name)
    if (!spec) return []
    const node = cellNode(spec, record.properties[name], titles)
    return node === null ? [] : [{ name, node }]
  })
  if (!cells.length) return null
  return (
    <span
      className={cn(
        "flex max-w-full min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs text-muted-foreground",
        className
      )}
    >
      {cells.map((cell, i) => (
        <span key={cell.name} className="flex min-w-0 items-center gap-1.5">
          {i > 0 && <span aria-hidden>·</span>}
          {cell.node}
        </span>
      ))}
    </span>
  )
}

/** Prose (`text`, `markdown`) as paragraphs. There is no markdown renderer in
 * the console and the prototype adds no dependency, so the author's line
 * breaks are kept and nothing else is interpreted. */
export function Prose({ text }: { text: string }) {
  const paragraphs = text.split(/\n{2,}/).filter((p) => p.trim())
  return (
    <div className="flex max-w-prose flex-col gap-3 text-sm leading-relaxed break-words whitespace-pre-wrap">
      {paragraphs.map((p, i) => (
        <p key={i}>{p}</p>
      ))}
    </div>
  )
}

const PROSE = new Set(["text", "markdown"])

/** One value with the room a detail has: prose whole, a reference as its
 * pill, a state as its badge, a datetime as the stamp with its relative
 * phrase, a url as a link, an enum by its label, and "not set" said. */
export function DetailValue({
  spec,
  value,
  kinds,
}: {
  spec: PropSpec
  value: unknown
  kinds: KindInfo[]
}) {
  if (
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && !value.length)
  ) {
    return <span className="text-muted-foreground/70">not set</span>
  }
  if (PROSE.has(spec.kind) && typeof value === "string") {
    return <Prose text={value} />
  }
  if (spec.kind === "reference") {
    const items = Array.isArray(value) ? value : [value]
    return (
      <span className="flex flex-wrap gap-1.5">
        {items.map((item, i) => (
          <ReferenceValue key={i} value={item} kinds={kinds} />
        ))}
      </span>
    )
  }
  if (spec.kind === "state" && typeof value === "string") {
    return <StateBadge value={value} initial={spec.initial} />
  }
  if (
    (spec.kind === "datetime" || spec.kind === "date") &&
    typeof value === "string"
  ) {
    return (
      <span title={value}>
        <span className="data">{shortDateTime(value)}</span>
        <span className="text-muted-foreground"> · {relativeDay(value)}</span>
      </span>
    )
  }
  if (spec.kind === "url" && typeof value === "string") {
    return (
      <a
        href={value}
        target="_blank"
        rel="noreferrer"
        className="data break-all underline-offset-4 hover:underline"
      >
        {value}
      </a>
    )
  }
  if (spec.values?.length && typeof value === "string") {
    const authored = spec.values.find((v) => v.value === value)?.label
    return <span title={value}>{authored || humanizeName(value)}</span>
  }
  if (typeof value === "boolean") return <span>{value ? "yes" : "no"}</span>
  if (typeof value === "object" && !Array.isArray(value)) {
    return (
      <pre className="overflow-x-auto rounded-lg border bg-muted/30 px-3 py-2 data text-xs break-words whitespace-pre-wrap">
        {JSON.stringify(value, null, 2)}
      </pre>
    )
  }
  return <span className="data break-words">{formatValue(spec, value)}</span>
}

/** The one line a layout says when a column or a section has nothing. */
export function EmptyLine({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <p
      className={cn(
        "px-1 py-3 text-center text-sm text-muted-foreground",
        className
      )}
    >
      {children}
    </p>
  )
}

// ── the standalone create form ──────────────────────────────────────────────

/** A seeded value as the read-only row shows it: the parent's title where
 * the value is the parent, else the value as a form would format it. */
function seedText(field: FormField, value: unknown, parent?: SubstrateRecord) {
  const held = readReference(value)
  if (held && parent && held.path === recordPath(parent.kind, parent.id)) {
    return titleOf(parent)
  }
  return formatValue(field.spec, editableValue(field.spec, value))
}

/** The form over an action's `prompt`, mounted on a page rather than in a
 * sheet. A create is seeded from `set`, `via` and the filter's `eq`s, shown
 * read-only above the prompted fields; the write carries an idempotency key
 * minted once and replaced only after the record lands, so a retry of a
 * failed submit is the same create. A successful submit clears the fields
 * for the next one. */
export function CreateForm({
  host,
  action,
  record,
  onDone,
  className,
  idPrefix = "vf",
}: {
  host: ActionHost
  action: ActionSpec
  /** The row a patch edits; a create has none. */
  record?: SubstrateRecord
  onDone?: (record?: SubstrateRecord) => void
  className?: string
  idPrefix?: string
}) {
  const { spec, kind, kinds, ctx } = host
  const queryClient = useQueryClient()
  const mode: FormMode = action.verb === "patch" ? "patch" : "create"
  const seed = useMemo(
    () =>
      mode === "create"
        ? createSeed(action, spec, kind, ctx, record)
        : { properties: {}, problems: [] },
    [mode, action, spec, kind, ctx, record]
  )
  // A prompted name the seed already answers is shown, not asked.
  const fields = useMemo(
    () =>
      promptFields(kind, action.prompt).filter(
        (f) => !(f.name in seed.properties)
      ),
    [kind, action.prompt, seed.properties]
  )
  const seeded = useMemo(
    () =>
      Object.keys(seed.properties)
        .map((name) => seedField(kind, name))
        .filter((f): f is FormField => Boolean(f)),
    [seed.properties, kind]
  )
  const [values, setValues] = useState<FormValues>(() =>
    initialValues(fields, mode === "patch" ? record : undefined)
  )
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [idempotencyKey, setIdempotencyKey] = useState(newIdempotencyKey)
  const [pending, setPending] = useState(false)

  function setValue(name: string, value: FormValue) {
    setValues((prev) => ({ ...prev, [name]: value }))
    setErrors((prev) => {
      if (!prev[name]) return prev
      const next = { ...prev }
      delete next[name]
      return next
    })
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    const failures = validate(fields, values, mode)
    if (failures.length) {
      setErrors(Object.fromEntries(failures.map((f) => [f.name, f.message])))
      return
    }
    setPending(true)
    try {
      const out = await runAction({
        spec,
        kind,
        kinds,
        ctx,
        action,
        record,
        queryClient,
        values: toProperties(fields, values, mode),
        idempotencyKey: mode === "create" ? idempotencyKey : undefined,
      })
      if (out.ok) {
        setValues(initialValues(fields))
        setErrors({})
        setIdempotencyKey(newIdempotencyKey())
        onDone?.(out.record)
      }
    } finally {
      setPending(false)
    }
  }

  const parent = ctx.parent?.record
  return (
    <form
      onSubmit={(e) => void submit(e)}
      className={cn("flex flex-col gap-5", className)}
    >
      {seed.problems.length > 0 && (
        <ProblemStrip problems={seed.problems} className="rounded-lg border" />
      )}
      <FieldGroup className="gap-5">
        {seeded.map((field) => (
          <Field key={field.name}>
            <div className="flex items-center gap-2">
              <FieldLabel className="font-normal">{field.label}</FieldLabel>
              <Badge variant="secondary" className="text-[0.65rem]">
                from the view
              </Badge>
            </div>
            <p className="data text-sm break-words">
              {seedText(field, seed.properties[field.name], parent)}
            </p>
          </Field>
        ))}
        {fields.map((field) => (
          <PropertyField
            key={field.name}
            field={field}
            value={values[field.name]}
            onChange={(next) => setValue(field.name, next)}
            mode={mode}
            error={errors[field.name]}
            kinds={kinds}
            idPrefix={idPrefix}
          />
        ))}
      </FieldGroup>
      {!fields.length && !seeded.length && (
        <p className="text-sm text-muted-foreground">
          This {action.verb} asks for nothing.
        </p>
      )}
      <Button
        type="submit"
        className="h-12 w-full text-base sm:h-9 sm:w-auto sm:self-end sm:text-sm"
        disabled={pending}
      >
        {pending && <Spinner className="size-4" />}
        {action.label}
      </Button>
    </form>
  )
}
