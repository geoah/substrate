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
 *   picked rather than remembered, and asks for the kind when `to: any`; a
 *   REPEATED one is a list of those pickers, because the write carries a list;
 * - a string marked `refersTo:` offers the records it names: the functions,
 *   the agents, the kind registry, the authorities, the provider rows;
 * - a `secret` is write-only: it never shows a stored value, because the read
 *   never serves one;
 * - a `managed:` property is the ENGINE's stamp and never an input: it renders
 *   as the value it holds, said to be stamped;
 * - an `object` is its declared fields, nested as deep as the declaration goes;
 *   and a `keyed:` map is an add-remove list of key/value rows whose keys are
 *   held to the declared `keyPattern`;
 * - a `json` gets a monospaced editor validated as JSON, a repeated scalar a
 *   one-per-line list, a number a number input, and every datatype with a
 *   worked example carries it as the placeholder. */

import { PlusIcon } from "lucide-react"

import { ReferenceListPicker } from "@/components/record/identity-picker"
import { RecordCombobox } from "@/components/record/record-combobox"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import type { KindInfo } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { useRecordOptions } from "@/lib/identities"
import {
  asBag,
  asKeyedRows,
  asRows,
  asRef,
  asRefs,
  elementField,
  humanizeName,
  objectFields,
  type FieldBag,
  type FormField,
  type FormMode,
  type FormValue,
  type KeyedRow,
} from "@/lib/record-form"
import { TO_ANY, formatValue } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

/** A native select styled to match the Input primitive (no shadcn Select
 * primitive is vendored yet; an enum field is a small, closed set). */
const SELECT_CLASS =
  "h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 disabled:opacity-50 md:text-sm dark:bg-input/30"

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
  /** The record being edited, by id. An agent picker drops it: a declaration
   * that names itself as its own sub-agent is a loop, not a choice. */
  self?: string
  /** This property is not the person's to type: the surface DERIVES it from
   * something already on the screen, and the note says from what. Rendered
   * read-only, like a managed property, and written by whatever derives it. */
  derivedNote?: string
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
  self,
  derivedNote,
  labelAction,
  idPrefix = "f",
}: PropertyFieldProps) {
  const id = `${idPrefix}-${field.name}`
  // A cleared field (`null`) and an untouched blank one both render empty; what
  // separates them is what the write does, which is the form core's business.
  const text = typeof value === "string" ? value : ""

  // Two properties nobody types into: the one the ENGINE stamps (it refuses a
  // write that disagrees) and the one the surface DERIVES from something
  // already on the screen. Both are worth reading, and neither is worth an
  // input that only ever gets overwritten.
  if (field.spec.managed || derivedNote) {
    const stamped = field.spec.managed
    return (
      <Field>
        <div className="flex items-center gap-2">
          <FieldLabel className="font-normal">{field.label}</FieldLabel>
          <Badge variant="secondary" className="text-[0.65rem]">
            {stamped ? "engine-stamped" : "derived"}
          </Badge>
        </div>
        <p className="data text-sm">
          {formatValue(field.spec, value) || (
            <span className="text-muted-foreground">
              {stamped ? "not stamped on this record yet" : "not set yet"}
            </span>
          )}
        </p>
        {(derivedNote ?? field.description) && (
          <FieldDescription>
            {derivedNote ?? field.description}
          </FieldDescription>
        )}
      </Field>
    )
  }

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
        self={self}
        label={label}
        help={help}
      />
    )
  }

  if (field.control === "referenceList") {
    const pinned = pinnedKind(field)
    return (
      <Field>
        {label}
        <ReferenceListPicker
          id={id}
          label={field.label}
          pin={pinned}
          kinds={kinds}
          self={self}
          value={asRefs(value)}
          onChange={onChange}
          invalid={Boolean(error)}
        />
        {help(
          pinned
            ? `Each one points at a ${pinned}.`
            : "Each one points at any kind: give the whole path."
        )}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  if (field.control === "keyedMap") {
    return (
      <Field>
        {label}
        <KeyedRows
          field={field}
          rows={asKeyedRows(value)}
          onChange={onChange}
          mode={mode}
          kinds={kinds}
          self={self}
          idPrefix={id}
        />
        {help(
          field.spec.keyPattern
            ? `Each key holds to the ${field.spec.keyPattern} contract.`
            : undefined
        )}
        {error && <FieldError>{error}</FieldError>}
      </Field>
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
          self={self}
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
            // The row is headed rather than overlaid: a button floating over
            // the first field's label is a button in the way of it.
            <div key={i} className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">
                  {field.label} {i + 1}
                </span>
                <RemoveButton
                  label={`Remove ${field.label} row ${i + 1}`}
                  onClick={() => onChange(rows.filter((_, at) => at !== i))}
                >
                  Remove
                </RemoveButton>
              </div>
              <ObjectRow
                field={field}
                bag={bag}
                onChange={(next) =>
                  onChange(rows.map((row, at) => (at === i ? next : row)))
                }
                mode={mode}
                kinds={kinds}
                self={self}
                idPrefix={`${id}-${i}`}
              />
            </div>
          ))}
          <AddButton
            label={`Add ${field.label} row`}
            onClick={() => onChange([...rows, {} as FieldBag])}
          >
            Add row
          </AddButton>
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

/** The affordances a container's rows carry. An action is a BUTTON, not an
 * underlined word: adding and removing a row are things this form DOES, and
 * they wear the console's own variants (secondary to add, destructive to
 * remove) wherever a list grows. */
function RemoveButton({
  label,
  onClick,
  className,
  children,
}: {
  label: string
  onClick: () => void
  className?: string
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      variant="destructive"
      size="xs"
      aria-label={label}
      className={cn("shrink-0", className)}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

/** The add for one container. It is LABELLED with the property it grows: a
 * declaration nests, so one form can hold three lists at three depths, and
 * three buttons all reading "Add" name none of them. */
function AddButton({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      variant="secondary"
      size="xs"
      aria-label={label}
      className="self-start"
      onClick={onClick}
    >
      <PlusIcon />
      {children}
    </Button>
  )
}

/** The kind a pointer is PINNED to, or "" where the declaration pins none. */
function pinnedKind(field: FormField): string {
  return field.spec.to && field.spec.to !== TO_ANY ? field.spec.to : ""
}

/** One typed pointer: the kind it points at (fixed when the declaration pins
 * one, chosen from the registry when it is `kind: any`) and the record itself,
 * offered from that collection as a dropdown so a record is PICKED, not
 * remembered. The two controls edit two halves; the write carries the one
 * record path they join into (`record-form.toFieldValue`). */
function ReferenceField({
  id,
  field,
  value,
  onChange,
  error,
  kinds,
  self,
  label,
  help,
}: {
  id: string
  field: FormField
  value: FormValue
  onChange: (value: FormValue) => void
  error?: string
  kinds: KindInfo[]
  self?: string
  label: React.ReactNode
  help: (hint?: string) => React.ReactNode
}) {
  const ref = asRef(value)
  const pinned = pinnedKind(field)
  const chosen = ref.kind || pinned
  const target = kindByIdentity(kinds, chosen)
  // The PIN names a kind, a KindInfo is an authority and a collection, so the
  // registry the editor already holds says which collection to offer.
  const offered = useRecordOptions(chosen, kinds, self)

  return (
    <Field>
      {label}
      <div className="flex flex-col gap-1.5">
        {!pinned && (
          <select
            aria-label={`${field.label} kind`}
            className={cn(SELECT_CLASS, "data")}
            value={ref.kind}
            onChange={(e) => onChange({ kind: e.target.value, id: "" })}
          >
            <option value="">select a kind</option>
            {kinds.map((k) => (
              <option key={k.identity} value={k.identity}>
                {k.identity}
              </option>
            ))}
          </select>
        )}
        <RecordCombobox
          {...offered}
          id={id}
          value={ref.id}
          invalid={Boolean(error)}
          placeholder={
            target ? `select a ${target.name}` : "select a kind first"
          }
          onSelect={(next) => onChange({ kind: chosen, id: next })}
        />
      </div>
      {help(
        pinned
          ? `Points at a ${pinned}.`
          : "Points at any kind: name it, or give the whole path."
      )}
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

/** A KEYED map: the author names the keys, so a row is a key beside the value
 * it maps to, and the value wears whatever control its datatype earns: a
 * scalar control, or the object's own fields. */
function KeyedRows({
  field,
  rows,
  onChange,
  mode,
  kinds,
  self,
  idPrefix,
}: {
  field: FormField
  rows: KeyedRow[]
  onChange: (rows: KeyedRow[]) => void
  mode: FormMode
  kinds: KindInfo[]
  self?: string
  idPrefix: string
}) {
  // A row's value has no name of its own (the KEY is its name), so it is
  // labelled "Value" rather than repeating the property's label once per row.
  // The property's own one-liner is said once, above the rows.
  const valueField: FormField = {
    ...elementField(field),
    label: "Value",
    description: undefined,
    required: false,
  }

  function patch(at: number, next: Partial<KeyedRow>) {
    onChange(rows.map((row, i) => (i === at ? { ...row, ...next } : row)))
  }

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, i) => (
        <div
          key={i}
          className="flex flex-col gap-3 rounded-lg border border-input p-3"
        >
          <div className="flex items-start gap-2">
            <Field className="min-w-0 flex-1">
              <FieldLabel
                htmlFor={`${idPrefix}-key-${i}`}
                className="font-normal"
              >
                Key
              </FieldLabel>
              <Input
                id={`${idPrefix}-key-${i}`}
                className="data"
                aria-label={`${field.label} key ${i + 1}`}
                placeholder={field.spec.keyPattern ?? "the key"}
                value={row.key}
                onChange={(e) => patch(i, { key: e.target.value })}
              />
            </Field>
            <RemoveButton
              label={`Remove ${field.label} entry ${i + 1}`}
              onClick={() => onChange(rows.filter((_, at) => at !== i))}
            >
              Remove
            </RemoveButton>
          </div>
          <PropertyField
            field={valueField}
            value={row.value}
            onChange={(next) => patch(i, { value: next })}
            mode={mode}
            kinds={kinds}
            self={self}
            idPrefix={`${idPrefix}-${i}`}
          />
        </div>
      ))}
      <AddButton
        label={`Add ${field.label} entry`}
        onClick={() => onChange([...rows, { key: "", value: "" }])}
      >
        Add entry
      </AddButton>
    </div>
  )
}

/** One object's declared fields, edited inline. A field is a property in its
 * own right, so this RECURSES: a field that is itself an object renders its own
 * row, a repeated field its own list, a keyed field its own key/value rows,
 * as deep as the declaration nests, which the dialect bounds at four. */
function ObjectRow({
  field,
  bag,
  onChange,
  mode,
  kinds,
  self,
  idPrefix,
}: {
  field: FormField
  bag: FieldBag
  onChange: (bag: FieldBag) => void
  mode: FormMode
  kinds: KindInfo[]
  self?: string
  idPrefix: string
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-input p-3">
      {objectFields(field).map((sub) => (
        <PropertyField
          key={sub.name}
          field={sub}
          value={bag[sub.name] ?? ""}
          onChange={(next) => onChange({ ...bag, [sub.name]: next })}
          mode={mode}
          kinds={kinds}
          self={self}
          idPrefix={idPrefix}
        />
      ))}
    </div>
  )
}
