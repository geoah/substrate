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
 * document only ever holds values the declaration admits. */

import { useMemo, useState } from "react"

import { PropertyField } from "@/components/record/property-field"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  buildFormFields,
  requiredFirst,
  seedField,
  toFieldValue,
  type FormField,
  type FormValue,
  type FormValues,
} from "@/lib/record-form"
import { systemSpecs } from "@/lib/record-schema"
import { deleteIn, parseApplyDoc, propertiesOf, setIn } from "@/lib/record-yaml"

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
    const problem = toFieldValue(field, values[field.name]).error
    if (problem) errors[field.name] = problem
  }

  function emit(next: string) {
    setEmitted(next)
    onChange(next)
  }

  function commit(field: FormField, next: FormValue) {
    setValues((prev) => ({ ...prev, [field.name]: next }))
    const submitted = toFieldValue(field, next)
    // A draft that does not parse stays a draft: the document keeps the last
    // value the declaration admitted.
    if (submitted.error) return
    const path = ["data", "properties", field.name]
    if (submitted.value === undefined) {
      // A blank secret means "leave the sealed value alone", so it never
      // touches the document.
      if (field.control === "secret") return
      emit(
        BLANK_STAYS.has(field.spec.kind) && !field.spec.repeated
          ? setIn(text, path, "")
          : deleteIn(text, path)
      )
      return
    }
    emit(setIn(text, path, submitted.value))
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

  return (
    // Left-aligned, like every other surface in the console: a centered column
    // here would be the one page that floats.
    <div className="w-full max-w-2xl p-6">
      <FieldGroup className="gap-5">
        <Field>
          <FieldLabel htmlFor="record-id" className="font-normal">
            Record id
          </FieldLabel>
          <Input
            id="record-id"
            className="data"
            disabled={Boolean(record)}
            value={idOf(text)}
            placeholder="the substrate mints one when this is blank"
            onChange={(e) =>
              emit(setIn(text, ["metadata", "id"], e.target.value))
            }
          />
          <FieldDescription>
            {record
              ? "A record's id is fixed: this write lands on the record you opened."
              : "Optional. Leave it blank and the substrate mints one."}
          </FieldDescription>
        </Field>

        {fields.map((field) => (
          <PropertyField
            key={field.name}
            field={field}
            value={values[field.name]}
            onChange={(next) => commit(field, next)}
            mode={record ? "patch" : "create"}
            error={errors[field.name]}
            kinds={kinds}
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
