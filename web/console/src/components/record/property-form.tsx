/** The record editor's FORM lens: one typed control per declared property of
 * any kind, over the very same document the YAML lens edits.
 *
 * The YAML text is the single source of truth. A control reads its value out of
 * the parsed document and writes back through `setIn`, which touches exactly
 * one key and leaves every other line (comments included) as authored — so
 * switching lenses is lossless in both directions, and filling a template in on
 * the form keeps the template's own annotations.
 *
 * A draft that cannot be a value yet (a half-typed number, malformed JSON) sits
 * in the control with its complaint and is NOT written to the document: the
 * document only ever holds values the declaration admits.
 *
 * Above the controls sit the GRANT hints (`lib/agent-grants.ts`): an agent
 * naming a host tool it has not paid for is told which property pays for it,
 * beside the control that would. The loader still refuses the write; this is
 * the same question asked while the answer is one field away. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { AlertTriangleIcon } from "lucide-react"

import { PropertyField } from "@/components/record/property-field"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { AGENT_KIND, grantHints } from "@/lib/agent-grants"
import {
  buildFormFields,
  narrowEdit,
  requiredFirst,
  seedField,
  toFieldValue,
  type EditPath,
  type FormField,
  type FormValue,
  type FormValues,
} from "@/lib/record-form"
import {
  AUTHORITY_PROPERTY,
  authorityIsDerived,
  declarationIdShape,
  derivedAuthority,
  isDeclarationKind,
} from "@/lib/declarations"
import { checkValue, emptyContainer, systemSpecs } from "@/lib/record-schema"
import {
  MODEL_PROPERTY,
  PROVIDER_PROPERTY,
  pricedModels,
} from "@/lib/model-suggestions"
import { recordQueryOptions } from "@/lib/api/records"
import { kindByIdentity } from "@/lib/definition"
import { splitKind } from "@/lib/api/http"
import { splitRecordPath } from "@/lib/record-path"
import {
  canSetIn,
  deleteIn,
  hasIn,
  parseApplyDoc,
  propertiesOf,
  setIn,
} from "@/lib/record-yaml"

/** Datatypes whose blank is an authored empty string rather than "unset": the
 * key stays in the document (with its template comment) instead of vanishing
 * the moment a field is cleared. */
const BLANK_STAYS = new Set(["string", "text", "markdown"])

export function PropertyForm({
  text,
  kind,
  kinds,
  record,
  onChange,
}: {
  /** The document, which is the truth for both lenses. */
  text: string
  kind: KindInfo
  kinds: KindInfo[]
  /** The record being edited; absent on a create. */
  record?: SubstrateRecord
  onChange: (text: string) => void
}) {
  const fields = useMemo(() => requiredFirst(buildFormFields(kind)), [kind])
  const properties = propertiesOf(text)
  // A DECLARATION is named by its id, not given one, and the label in front of
  // that id's slash IS its authority.
  const declared = isDeclarationKind(kind.identity)
  const derivesAuthority = authorityIsDerived(kind.identity)

  // Drafts hold what the person is typing; the document holds what parses. A
  // change that came from ELSEWHERE (the YAML lens) is what reseeds them, which
  // is exactly "the text is not the text we last emitted".
  const [emitted, setEmitted] = useState(text)
  const [values, setValues] = useState<FormValues>(() =>
    seedValues(fields, properties, record)
  )
  if (emitted !== text) {
    setEmitted(text)
    setValues(seedValues(fields, properties, record))
  }

  const errors: Record<string, string> = {}
  for (const field of fields) {
    // What the CONTROL holds may not be a value yet; what the DOCUMENT holds
    // may not satisfy the declaration (an object missing a required field).
    // Both belong on the control, and the first one wins.
    const problem =
      toFieldValue(field, values[field.name]).error ??
      checkValue(field.spec, properties?.[field.name])
    if (problem) errors[field.name] = problem
  }

  // The models the CHOSEN provider prices, offered on the model field (#54.1).
  // The provider is a sibling property on the same document, so the list
  // follows the picker: change the provider and the suggestions change with
  // it. Free text stays legal throughout — a datalist suggests, it does not
  // constrain — so a model the provider does not price is still typeable, and
  // an existing unlisted value is left exactly as it is.
  const providerPath =
    fields.some((f) => f.name === MODEL_PROPERTY) &&
    typeof values[PROVIDER_PROPERTY] === "string"
      ? splitRecordPath(values[PROVIDER_PROPERTY])
      : undefined
  const providerKind = providerPath
    ? kindByIdentity(kinds, providerPath.kind)
    : undefined
  const providerRecord = useQuery({
    ...recordQueryOptions(
      providerKind ? splitKind(providerKind.identity).authority : "",
      providerKind?.plural ?? "",
      providerPath?.id ?? ""
    ),
    enabled: Boolean(providerKind && providerPath?.id),
  })
  const modelSuggestions = pricedModels(providerRecord.data)

  function emit(next: string) {
    setEmitted(next)
    onChange(next)
  }

  /** The id, and with it the authority a DECLARATION's id puts it under. The
   * write still carries `authority` (the loader requires it), but nobody is
   * asked to type a label they have already typed in front of the slash. */
  function setRecordId(next: string) {
    let doc = setIn(text, ["metadata", "id"], next)
    const authority = derivedAuthority(kind.identity, next)
    if (derivesAuthority && authority !== undefined) {
      doc = setIn(doc, ["data", "properties", AUTHORITY_PROPERTY], authority)
      // The draft moves with the document: an emit is deliberately NOT a
      // reseed (a draft outranks what parses), so a value written from here
      // has to be put in the bag as well as in the text.
      setValues((prev) => ({ ...prev, [AUTHORITY_PROPERTY]: authority }))
    }
    emit(doc)
  }

  /** Whether a blank leaves an authored empty string behind rather than
   * removing the key. A CONTAINER never does: its datatype describes what the
   * container holds, not the container. */
  function blankStays(spec: FormField["spec"]): boolean {
    return BLANK_STAYS.has(spec.kind) && !spec.repeated && !spec.keyed
  }

  /** Write ONE value at a path, or take it out when the control says nothing. */
  function write(target: FormField, path: EditPath, value: FormValue) {
    const submitted = toFieldValue(target, value)
    // A draft that does not parse stays a draft: the document keeps the last
    // value the declaration admitted.
    if (submitted.error) return
    if (submitted.value !== undefined) {
      emit(setIn(text, path, submitted.value))
      return
    }
    // A blank secret means "leave the sealed value alone", so it never touches
    // the document.
    if (target.control === "secret") return
    // A CONTAINER the person EMPTIED is a statement, and its key stays holding
    // nothing; one the document never had has nothing to empty, so the key
    // goes. Absent and empty are two different things to say.
    const empty = emptyContainer(target.spec)
    if (empty !== undefined) {
      emit(hasIn(text, path) ? setIn(text, path, empty) : deleteIn(text, path))
      return
    }
    emit(blankStays(target.spec) ? setIn(text, path, "") : deleteIn(text, path))
  }

  function commit(field: FormField, next: FormValue) {
    const before = values[field.name]
    setValues((prev) => ({ ...prev, [field.name]: next }))
    const base = ["data", "properties", field.name]

    // The NARROWEST write the change allows. A nested edit lands on its own
    // key, so every neighbouring line (a sibling's comment, an empty list
    // nobody touched, a hand-authored blank) is left exactly as it was.
    const edit = narrowEdit(field, before, next)
    if (edit) {
      const path = [...base, ...edit.path]
      if (canSetIn(text, path)) {
        write(edit.field, path, edit.value)
        return
      }
      // The document and the form have drifted apart (rows the document never
      // took). Falling through writes the property whole, which is always safe.
    }
    write(field, base, next)
  }

  if (properties === undefined) {
    return (
      <div className="p-6 text-sm text-muted-foreground">
        This document does not parse yet, so the form cannot read it. Fix the
        YAML and the fields come back.
      </div>
    )
  }

  // What the form does not offer: host-managed properties, undeclared keys, and
  // the system columns (title/body and the hot times). They stay in the
  // document and are written as they stand.
  const system = new Set(systemSpecs(kind).map((s) => s.name))
  const elsewhere = Object.keys(properties).filter(
    (name) => !fields.some((f) => f.name === name) && !system.has(name)
  )

  // The grants a host tool needs, asked here rather than waiting for the
  // loader's refusal. The loader is still the enforcement; this only says
  // which control answers it, while the control is on the screen.
  const hints = kind.identity === AGENT_KIND ? grantHints(properties) : []

  return (
    // Left-aligned, like every other surface in the console: a centered column
    // here would be the one page that floats.
    <div className="w-full max-w-2xl p-6">
      <FieldGroup className="gap-5">
        <Field>
          <FieldLabel htmlFor="record-id" className="font-normal">
            Record id
            {declared && !record && (
              <span className="text-destructive" aria-hidden>
                {" "}
                *
              </span>
            )}
          </FieldLabel>
          <Input
            id="record-id"
            className="data"
            disabled={Boolean(record)}
            aria-invalid={declared && !record && !idOf(text).trim()}
            value={idOf(text)}
            placeholder={
              declared
                ? declarationIdShape(kind.identity)
                : "the substrate mints one when this is blank"
            }
            onChange={(e) => setRecordId(e.target.value)}
          />
          <FieldDescription>
            {record
              ? "A record's id is fixed: this write lands on the record you opened."
              : declared
                ? `Required. A ${kind.name} is addressed by the identity it declares, and the substrate never mints one.`
                : "Optional. Leave it blank and the substrate mints one."}
          </FieldDescription>
        </Field>

        {hints.length > 0 && (
          <ul className="flex flex-col gap-1.5 rounded-md border border-warning/40 bg-warning/5 p-3">
            {hints.map((hint) => (
              <li
                key={hint.function}
                className="flex items-start gap-2 text-xs text-muted-foreground"
              >
                <AlertTriangleIcon className="mt-0.5 size-3.5 shrink-0 text-warning" />
                <span>{hint.message}</span>
              </li>
            ))}
          </ul>
        )}

        {fields.map((field) => (
          <PropertyField
            key={field.name}
            field={field}
            value={values[field.name]}
            onChange={(next) => commit(field, next)}
            mode={record ? "patch" : "create"}
            error={errors[field.name]}
            kinds={kinds}
            self={record?.id}
            suggestions={
              field.name === MODEL_PROPERTY ? modelSuggestions : undefined
            }
            derivedNote={
              derivesAuthority && field.name === AUTHORITY_PROPERTY
                ? "Derived from the record id: the label in front of its slash."
                : undefined
            }
          />
        ))}

        {fields.length === 0 && (
          <p className="text-sm text-muted-foreground">
            {kind.name} declares no editable properties. Everything it carries
            is host-managed, so the YAML lens is the honest surface.
          </p>
        )}

        {elsewhere.length > 0 && (
          <p className="text-xs text-muted-foreground">
            <span className="data">{elsewhere.join(", ")}</span>{" "}
            {elsewhere.length === 1 ? "is" : "are"} in the document but not
            offered here (undeclared, or host-managed). The YAML lens edits
            them, and they are written as they stand.
          </p>
        )}
      </FieldGroup>
    </div>
  )
}

function seedValues(
  fields: FormField[],
  properties: Record<string, unknown> | undefined,
  record?: SubstrateRecord
): FormValues {
  const values: FormValues = {}
  for (const field of fields) {
    values[field.name] = seedField(field, properties?.[field.name], !record)
  }
  return values
}

function idOf(text: string): string {
  const id = parseApplyDoc(text).value?.metadata?.id
  return typeof id === "string" ? id : ""
}
