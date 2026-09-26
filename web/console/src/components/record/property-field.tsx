/** ONE control per declared property, for the surfaces that edit a whole
 * record at once: a provider's dialogs (`RecordConfigForm`, one bundle config
 * or account), the record editor's form lens, and the property sheet's panel
 * for the shapes that need room. Its controls are the sheet's own: a choice
 * is the same popover list, a date the same picker, a list the same stack of
 * items, so a value type is edited one way everywhere.
 *
 * What the declaration buys, control by control:
 * - an enum (or any property that narrows its values) is chosen from a list
 *   of its display words, never a free-text guess;
 * - a `state` offers its machine's states on a create, and on an edit says
 *   the record page moves it (a put may not);
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
 *   stack of items, a number a number input, and every datatype with a
 *   worked example carries it as the placeholder. */

import { useEffect, useRef, type KeyboardEvent } from "react"
import { PlusIcon, XIcon } from "lucide-react"

import { fromLocalInput, toLocalInput } from "@/components/property-sheet/dates"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { EnumTag } from "@/components/identity/enum-tag"
import { StateBadge } from "@/components/identity/state-badge"
import { ReferenceListPicker } from "@/components/record/identity-picker"
import { PropertyChoice } from "@/components/record/property-choice"
import { RecordCombobox } from "@/components/record/record-combobox"
import { Button } from "@/components/ui/button"
import type { ChoiceOption } from "@/components/ui/choice-list"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import type { KindInfo } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { type EnumProperty } from "@/lib/enum-hue"
import { enumLabel } from "@/lib/grid-values"
import { displayName, displayPlural, lowerFirst } from "@/lib/kind-names"
import {
  asBag,
  asKeyedRows,
  asRows,
  asRef,
  asRefs,
  elementField,
  objectFields,
  type FieldBag,
  type FormField,
  type FormMode,
  type FormValue,
  type KeyedRow,
} from "@/lib/record-form"
import { TO_ANY, elementSpec, formatValue } from "@/lib/record-schema"
import { stateWord } from "@/lib/state-words"
import { cn } from "@/lib/utils"

/** An enum's values as choices, each on its tag. A deprecated value is never
 * offered; the one held keeps its row, so the list says what is there. */
function enumChoices(prop: EnumProperty, held: string): ChoiceOption[] {
  return (prop.values ?? [])
    .filter((o) => !o.deprecated || o.value === held)
    .map((o) => ({
      value: o.value,
      label: enumLabel(prop, o.value),
      display: <EnumTag prop={prop} value={o.value} />,
      hint: o.deprecated ? "no longer offered" : undefined,
    }))
}

/** "a person", "an organization": a record of a kind, in a sentence. */
function aRecordOf(kind: KindInfo | string): string {
  const noun = lowerFirst(displayName(kind))
  return `${/^[aeiou]/.test(noun) ? "an" : "a"} ${noun}`
}

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
  /** The control alone: the surface around it says the label and the
   * one-liner (the property sheet's rows). Errors still show. */
  bare?: boolean
}

/** A worked example as a placeholder. In the data voice an example reads like
 * a stored value, so it says it is one. */
function exampleHint(example: string | undefined): string | undefined {
  return example ? `e.g. ${example}` : undefined
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
  bare = false,
}: PropertyFieldProps) {
  const [technical] = useTechnicalDetails()
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
    if (bare) {
      return (
        <p className="text-sm text-muted-foreground">
          {formatValue(field.spec, value) ||
            (derivedNote ?? (stamped ? "Set automatically" : "Not set yet"))}
        </p>
      )
    }
    return (
      <Field>
        <FieldLabel className="font-normal">{field.label}</FieldLabel>
        <p className="text-sm">
          {formatValue(field.spec, value) || (
            <span className="text-muted-foreground">
              {stamped ? "Set automatically" : "Not set yet"}
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
    if (bare) {
      return (
        <input
          id={id}
          type="checkbox"
          className="size-4 accent-primary"
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
        />
      )
    }
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

  const label = bare ? null : (
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
  const help = (hint?: string) =>
    bare ? null : (
      <>
        {field.description && (
          <FieldDescription>{field.description}</FieldDescription>
        )}
        {hint && <FieldDescription>{hint}</FieldDescription>}
      </>
    )

  if (field.control === "select") {
    // An OPTIONAL enum may be emptied (unset is a real answer); a required one
    // never offers the empty beside its own values.
    return (
      <Field>
        {label}
        <PropertyChoice
          id={id}
          label={`Choose ${field.label}`}
          choices={enumChoices(
            { name: field.name, values: field.options },
            text
          )}
          value={text}
          onChange={onChange}
          clearLabel={field.required ? undefined : "Clear"}
          invalid={Boolean(error)}
        />
        {help()}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  if (field.control === "state") {
    // A put may not move a state (engine/write.go): a create may be born in
    // any declared state, an edit may not change it.
    const frozen = mode === "patch"
    const initial = field.spec.initial
    return (
      <Field>
        {label}
        {frozen ? (
          <output id={id} className="flex min-h-8 items-center text-sm">
            {text ? (
              <StateBadge value={text} initial={initial} />
            ) : (
              <span className="text-muted-foreground">Not set</span>
            )}
          </output>
        ) : (
          <PropertyChoice
            id={id}
            label={`Choose ${field.label}`}
            choices={(field.spec.states ?? []).map((state) => ({
              value: state,
              label: stateWord(state),
              display: <StateBadge value={state} initial={initial} />,
            }))}
            // A state's badge says its stored value itself in technical mode.
            showValues={false}
            value={text}
            onChange={onChange}
            invalid={Boolean(error)}
          />
        )}
        {help(
          frozen
            ? `${field.label} changes by moving it on the record’s page.`
            : "Where it starts."
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
          linkFields={field.spec.linkFields}
          label={field.label}
          pin={pinned}
          kinds={kinds}
          self={self}
          value={asRefs(value)}
          onChange={onChange}
          invalid={Boolean(error)}
        />
        {help(
          technical
            ? pinned
              ? `Each one points at ${pinned}.`
              : "Each one points at any kind: give the whole path."
            : undefined
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
            ? `Each key must match ${field.spec.keyPattern}.`
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
                />
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
          />
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
        <ItemList
          id={id}
          field={field}
          text={text}
          onChange={onChange}
          invalid={Boolean(error)}
        />
        {help()}
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
          className={cn(
            "field-sizing-content min-h-16",
            isJSON && "font-mono text-xs"
          )}
          aria-invalid={Boolean(error)}
          placeholder={isJSON ? exampleHint(field.example) : undefined}
          value={text}
          onChange={(e) => onChange(e.target.value)}
        />
        {help(isJSON ? "JSON." : undefined)}
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  const isSecret = field.control === "secret"
  // A time is picked on the same local-time control the sheet opens; what is
  // stored is the instant it names.
  const isInstant = field.control === "datetime" && field.spec.kind !== "date"
  const inputType = isSecret
    ? "password"
    : field.control === "number"
      ? "number"
      : field.spec.kind === "date"
        ? "date"
        : isInstant
          ? "datetime-local"
          : field.inputType
  return (
    <Field>
      {label}
      <Input
        id={id}
        type={inputType}
        autoComplete={isSecret ? "off" : undefined}
        aria-invalid={Boolean(error)}
        placeholder={
          isSecret
            ? mode === "patch"
              ? "•••••••• (unchanged)"
              : undefined
            : exampleHint(field.example)
        }
        value={isInstant ? toLocalInput(text) || text : text}
        onChange={(e) =>
          onChange(isInstant ? fromLocalInput(e.target.value) : e.target.value)
        }
      />
      {help(
        isSecret && mode === "patch"
          ? "It’s never shown again. Leave it blank to keep the saved one."
          : undefined
      )}
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

/** The affordances a container's rows carry, the property sheet's own: a
 * quiet cross to take a row out and a quiet "Add another" to grow the list.
 * Each is NAMED for the property it acts on: a declaration nests, so one form
 * can hold three lists at three depths. */
function RemoveButton({
  label,
  onClick,
}: {
  label: string
  onClick: () => void
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      aria-label={label}
      title="Remove"
      className="shrink-0 text-muted-foreground"
      onClick={onClick}
    >
      <XIcon />
    </Button>
  )
}

function AddButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      aria-label={label}
      className="self-start text-muted-foreground"
      onClick={onClick}
    >
      <PlusIcon />
      Add another
    </Button>
  )
}

/** A repeated scalar as the stack of items it is, one box per item, held as
 * the lines of one text (the form core's shape for a list). Enter adds an
 * item after the one being typed in and Backspace in an empty one removes it;
 * a choice item is chosen from its list. */
function ItemList({
  id,
  field,
  text,
  onChange,
  invalid,
}: {
  id: string
  field: FormField
  text: string
  onChange: (value: FormValue) => void
  invalid: boolean
}) {
  const item = elementSpec(field.spec)
  const items = text === "" ? [""] : text.split("\n")
  const boxes = useRef<(HTMLInputElement | null)[]>([])
  // The item to focus once the list re-renders with it.
  const focusNext = useRef<number | null>(null)
  useEffect(() => {
    if (focusNext.current === null) return
    boxes.current[focusNext.current]?.focus()
    focusNext.current = null
  })

  const set = (next: string[]) => onChange(next.join("\n"))
  const change = (at: number, value: string) =>
    set(items.map((v, i) => (i === at ? value : v)))
  const remove = (at: number) => {
    const rest = items.filter((_, i) => i !== at)
    set(rest.length ? rest : [""])
    focusNext.current = Math.max(0, at - 1)
  }
  const insertAfter = (at: number) => {
    set([...items.slice(0, at + 1), "", ...items.slice(at + 1)])
    focusNext.current = at + 1
  }
  function onKeyDown(e: KeyboardEvent<HTMLInputElement>, at: number) {
    if (e.key === "Enter") {
      e.preventDefault()
      insertAfter(at)
    } else if (e.key === "Backspace" && items[at] === "" && items.length > 1) {
      e.preventDefault()
      remove(at)
    }
  }
  const inputType =
    field.inputType === "email" || field.inputType === "url"
      ? field.inputType
      : item.kind === "phone"
        ? "tel"
        : ["int", "integer", "number", "float", "decimal"].includes(item.kind)
          ? "number"
          : "text"

  return (
    <div className="flex flex-col gap-1.5">
      <ol aria-label={field.label} className="flex flex-col gap-1.5">
        {items.map((value, i) => (
          <li key={i} className="flex items-center gap-1">
            {item.values?.length ? (
              <PropertyChoice
                id={i === 0 ? id : undefined}
                label={`${field.label} ${i + 1}`}
                choices={enumChoices(item, value)}
                value={value}
                onChange={(next) => change(i, next)}
                invalid={invalid}
              />
            ) : (
              <Input
                id={i === 0 ? id : undefined}
                ref={(el) => {
                  boxes.current[i] = el
                }}
                aria-label={`${field.label} ${i + 1}`}
                type={inputType}
                aria-invalid={invalid}
                placeholder={exampleHint(field.example)}
                value={value}
                onChange={(e) => change(i, e.target.value)}
                onKeyDown={(e) => onKeyDown(e, i)}
              />
            )}
            <RemoveButton
              label={`Remove ${field.label} ${i + 1}`}
              onClick={() => remove(i)}
            />
          </li>
        ))}
      </ol>
      <AddButton
        label={`Add ${field.label}`}
        onClick={() => insertAfter(items.length - 1)}
      />
    </div>
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
  const [technical] = useTechnicalDetails()
  const ref = asRef(value)
  const pinned = pinnedKind(field)
  const chosen = ref.kind || pinned
  const target = kindByIdentity(kinds, chosen)
  // The chosen record reads by its title: one batched read over its path.
  const unlisted = chosen && ref.id ? [`${chosen}/${ref.id}`] : []
  const titles = useReferenceTitles(unlisted, kinds)

  return (
    <Field>
      {label}
      <div className="flex flex-col gap-1.5">
        {!pinned && (
          <PropertyChoice
            label={`${field.label} collection`}
            placeholder="Pick a collection"
            choices={kinds.map((k) => ({
              value: k.identity,
              label: displayPlural(k),
              display: (
                <>
                  <KindGlyph kind={k} size="xs" />
                  {displayPlural(k)}
                </>
              ),
            }))}
            value={ref.kind}
            onChange={(next) => onChange({ kind: next, id: "" })}
          />
        )}
        <RecordCombobox
          pin={chosen}
          kinds={kinds}
          self={self}
          id={id}
          value={ref.id}
          valueTitle={titles.get(`${chosen}/${ref.id}`)}
          invalid={Boolean(error)}
          placeholder={
            chosen
              ? `Pick ${aRecordOf(target ?? chosen)}`
              : "Pick a collection first"
          }
          // Choosing the record already held keeps what its link carries;
          // another record starts with none.
          onSelect={(next) =>
            onChange(
              next === ref.id && ref.kind === chosen
                ? ref
                : { kind: chosen, id: next }
            )
          }
        />
      </div>
      {help(
        technical
          ? pinned
            ? `Points at ${pinned}.`
            : "Points at any kind: pick its collection, or give the whole path."
          : undefined
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
                className="font-mono"
                aria-label={`${field.label} key ${i + 1}`}
                placeholder={field.spec.keyPattern ?? "the key"}
                value={row.key}
                onChange={(e) => patch(i, { key: e.target.value })}
              />
            </Field>
            <RemoveButton
              label={`Remove ${field.label} entry ${i + 1}`}
              onClick={() => onChange(rows.filter((_, at) => at !== i))}
            />
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
      />
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
