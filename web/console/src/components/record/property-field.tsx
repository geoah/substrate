/** ONE control per declared property, and the only place the console decides
 * what a datatype looks like on a form. Both typed surfaces render through it:
 * the integrations dialog (`RecordConfigForm`, one bundle config or account)
 * and the record editor's form lens (any kind at all).
 *
 * What the declaration buys, control by control:
 * - an enum (or any property that narrows its values) is a SELECT, never a
 *   free-text guess;
 * - a `state` offers its machine's states, and says so when a put may not move
 *   it (the transition is a patch, driven from the record page);
 * - a `reference` offers the records of the kind it is pinned to, so an id is
 *   picked rather than remembered, and asks for the kind when `to: any`;
 * - a `secret` is write-only: it never shows a stored value, because the read
 *   never serves one;
 * - a `json`/`object` gets a monospaced editor validated as JSON, a repeated
 *   property a one-per-line list, a number a number input, and every datatype
 *   with a worked example carries it as the placeholder. */

import { useQuery } from "@tanstack/react-query"

import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import { recordsQueryOptions } from "@/lib/api/records"
import type { KindInfo } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import {
  asBag,
  asRef,
  asRows,
  humanizeName,
  objectFields,
  type FieldBag,
  type FormField,
  type FormMode,
  type FormValue,
} from "@/lib/record-form"
import { TO_ANY } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

/** A native select styled to match the Input primitive (no shadcn Select
 * primitive is vendored yet; an enum field is a small, closed set). */
const SELECT_CLASS =
  "h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 disabled:opacity-50 md:text-sm dark:bg-input/30"

/** How many records a reference picker offers before it asks for a typed id. */
const PICKER_PAGE = 50

export interface PropertyFieldProps {
  field: FormField
  value: FormValue
  onChange: (value: FormValue) => void
  /** The write being prepared: a secret is required on a create and optional
   * on a patch, and a state may only be chosen on a create. */
  mode: FormMode
  error?: string
  /** The registry, for a reference's kind and its record picker. */
  kinds?: KindInfo[]
  /** The surface's own affordance beside the label (the dialog's Clear/Undo). */
  labelAction?: React.ReactNode
  /** Distinguishes ids when two forms are mounted at once. */
  idPrefix?: string
}

export function PropertyField({
  field,
  value,
  onChange,
  mode,
  error,
  kinds = [],
  labelAction,
  idPrefix = "f",
}: PropertyFieldProps) {
  const id = `${idPrefix}-${field.name}`
  // A cleared field (`null`) and an untouched blank one both render empty; what
  // separates them is what the write does, which is the form core's business.
  const text = typeof value === "string" ? value : ""

  // A bool is its own block: the checkbox and label ride one line and the
  // description flows full-width beneath, never trapped in a label column.
  if (field.control === "bool") {
    return (
      <Field>
        <div className="flex items-center gap-2">
          <input
            id={id}
            type="checkbox"
            className="size-4 accent-primary"
            checked={value === true}
            onChange={(e) => onChange(e.target.checked)}
          />
          <FieldLabel htmlFor={id} className="font-normal">
            {field.label}
          </FieldLabel>
          {labelAction}
        </div>
        {field.description && (
          <FieldDescription>{field.description}</FieldDescription>
        )}
      </Field>
    )
  }

  const label = (
    <div className="flex items-center justify-between gap-2">
      <FieldLabel htmlFor={id} className="font-normal">
        {field.label}
        {field.required && (
          <span className="text-destructive" aria-hidden>
            {" "}
            *
          </span>
        )}
      </FieldLabel>
      {labelAction}
    </div>
  )

  // The declaration's one-liner and the control's own hint are two sentences,
  // never one run-on line.
  const help = (hint?: string) => (
    <>
      {field.description && (
        <FieldDescription>{field.description}</FieldDescription>
      )}
      {hint && <FieldDescription>{hint}</FieldDescription>}
    </>
  )

  if (field.control === "select") {
    // The empty choice is offered for OPTIONAL enums (its "— none —" is a real
    // value: unset). A REQUIRED enum offers it ONLY while the field is empty,
    // so a required select seeded with a default does not present an empty
    // option beside the enum's own `none` value.
    const showEmpty = !field.required || text === ""
    return (
      <Field>
        {label}
        <select
          id={id}
          className={cn(SELECT_CLASS, "data")}
          aria-invalid={Boolean(error)}
          value={text}
          onChange={(e) => onChange(e.target.value)}
        >
          {showEmpty && (
            <option value="">
              {field.required ? "— select —" : "— none —"}
            </option>
          )}
          {field.options?.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label || humanizeName(opt.value)}
            </option>
          ))}
        </select>
        {help()}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  if (field.control === "state") {
    // A put may not move a state (engine/write.go): a create may be born in
    // any declared state, an edit may not change it.
    const frozen = mode === "patch"
    return (
      <Field>
        {label}
        <select
          id={id}
          className={cn(SELECT_CLASS, "data")}
          disabled={frozen}
          aria-invalid={Boolean(error)}
          value={text}
          onChange={(e) => onChange(e.target.value)}
        >
          {!text && <option value="">— select —</option>}
          {(field.spec.states ?? []).map((state) => (
            <option key={state} value={state}>
              {state}
            </option>
          ))}
        </select>
        {help(
          frozen
            ? "A state moves by transition, not by editing."
            : "The state this record is born into."
        )}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  if (field.control === "reference") {
    return (
      <ReferenceField
        id={id}
        field={field}
        value={value}
        onChange={onChange}
        error={error}
        kinds={kinds}
        label={label}
        help={help}
      />
    )
  }

  if (field.control === "object") {
    return (
      <Field>
        {label}
        <ObjectRow
          field={field}
          bag={asBag(value)}
          onChange={(bag) => onChange(bag)}
          mode={mode}
          kinds={kinds}
          idPrefix={id}
        />
        {help()}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  if (field.control === "objectList") {
    const rows = asRows(value)
    return (
      <Field>
        {label}
        <div className="flex flex-col gap-2">
          {rows.map((bag, i) => (
            <div key={i} className="relative">
              <ObjectRow
                field={field}
                bag={bag}
                onChange={(next) =>
                  onChange(rows.map((row, at) => (at === i ? next : row)))
                }
                mode={mode}
                kinds={kinds}
                idPrefix={`${id}-${i}`}
              />
              <button
                type="button"
                aria-label={`Remove ${field.label} row ${i + 1}`}
                className="absolute top-1.5 right-1.5 text-xs text-muted-foreground underline-offset-4 hover:underline"
                onClick={() => onChange(rows.filter((_, at) => at !== i))}
              >
                Remove
              </button>
            </div>
          ))}
          <button
            type="button"
            className="self-start text-xs text-muted-foreground underline-offset-4 hover:underline"
            onClick={() => onChange([...rows, {} as FieldBag])}
          >
            Add row
          </button>
        </div>
        {help()}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  if (field.control === "list") {
    return (
      <Field>
        {label}
        <Textarea
          id={id}
          rows={3}
          className="data text-xs"
          aria-invalid={Boolean(error)}
          placeholder={field.example}
          value={text}
          onChange={(e) => onChange(e.target.value)}
        />
        <FieldDescription>
          {field.description
            ? `${field.description} — one per line.`
            : "One value per line."}
        </FieldDescription>
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  if (field.control === "json" || field.control === "prose") {
    const isJSON = field.control === "json"
    return (
      <Field>
        {label}
        <Textarea
          id={id}
          rows={isJSON ? 4 : 5}
          className={cn(isJSON && "data text-xs")}
          aria-invalid={Boolean(error)}
          placeholder={isJSON ? field.example : undefined}
          value={text}
          onChange={(e) => onChange(e.target.value)}
        />
        {help(isJSON ? "JSON." : undefined)}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  const isSecret = field.control === "secret"
  const inputType = isSecret
    ? "password"
    : field.control === "number"
      ? "number"
      : field.spec.kind === "date"
        ? "date"
        : field.inputType
  return (
    <Field>
      {label}
      <Input
        id={id}
        type={inputType}
        autoComplete={isSecret ? "off" : undefined}
        className="data"
        aria-invalid={Boolean(error)}
        placeholder={
          isSecret
            ? mode === "patch"
              ? "•••••••• (unchanged)"
              : undefined
            : field.example
        }
        value={text}
        onChange={(e) => onChange(e.target.value)}
      />
      {help(
        isSecret && mode === "patch"
          ? "Sealed. Leave blank to keep the stored value."
          : undefined
      )}
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

/** A typed pointer: the kind it points at (fixed when the declaration pins one,
 * chosen from the registry when it is `to: any`) and the record itself, offered
 * from that collection so an id is picked, not remembered. */
function ReferenceField({
  id,
  field,
  value,
  onChange,
  error,
  kinds,
  label,
  help,
}: {
  id: string
  field: FormField
  value: FormValue
  onChange: (value: FormValue) => void
  error?: string
  kinds: KindInfo[]
  label: React.ReactNode
  help: (hint?: string) => React.ReactNode
}) {
  const ref = asRef(value)
  const pinned = field.spec.to && field.spec.to !== TO_ANY ? field.spec.to : ""
  const target = kindByIdentity(kinds, ref.kind || pinned)

  const options = useQuery({
    ...recordsQueryOptions({
      authority: target?.authority ?? "",
      plural: target?.plural ?? "",
      first: PICKER_PAGE,
    }),
    enabled: Boolean(target),
  })

  return (
    <Field>
      {label}
      <div className="flex flex-col gap-1.5">
        {!pinned && (
          <select
            aria-label={`${field.label} kind`}
            className={cn(SELECT_CLASS, "data")}
            value={ref.kind}
            onChange={(e) => onChange({ kind: e.target.value, id: ref.id })}
          >
            <option value="">select a kind</option>
            {kinds.map((k) => (
              <option key={k.identity} value={k.identity}>
                {k.identity}
              </option>
            ))}
          </select>
        )}
        <Input
          id={id}
          className="data"
          list={`${id}-options`}
          aria-invalid={Boolean(error)}
          placeholder={target ? `an ${target.name} id` : "the record id"}
          value={ref.id}
          onChange={(e) =>
            onChange({ kind: ref.kind || pinned, id: e.target.value })
          }
        />
        <datalist id={`${id}-options`}>
          {(options.data?.records ?? []).map((r) => (
            <option key={r.id} value={r.id}>
              {recordTitle(r.properties) || r.id}
            </option>
          ))}
        </datalist>
      </div>
      {help(pinned ? `Points at a ${pinned}.` : "Points at any kind: name it.")}
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

/** One object's declared fields, edited inline. An object is one level deep by
 * declaration, so this never recurses: every field is a scalar control. */
function ObjectRow({
  field,
  bag,
  onChange,
  mode,
  kinds,
  idPrefix,
}: {
  field: FormField
  bag: FieldBag
  onChange: (bag: FieldBag) => void
  mode: FormMode
  kinds: KindInfo[]
  idPrefix: string
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-input p-3">
      {objectFields(field).map((sub) => (
        <PropertyField
          key={sub.name}
          field={sub}
          value={bag[sub.name] ?? ""}
          onChange={(next) =>
            onChange({ ...bag, [sub.name]: next as string | boolean | null })
          }
          mode={mode}
          kinds={kinds}
          idPrefix={idPrefix}
        />
      ))}
    </div>
  )
}
