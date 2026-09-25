/** The form lens's plumbing, shared by the editor's form and the new-record
 * sheet: the document is the truth, a control's draft that is not a value
 * yet stays a draft, and a change writes the narrowest key it can.
 *
 * The YAML text is the single source of truth. A control reads its value out
 * of the parsed document and writes back through `setIn`, which touches
 * exactly one key and leaves every other line (comments included) as
 * authored, so switching lenses is lossless in both directions. */

import { useMemo, useState } from "react"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { AGENT_KIND, grantHints } from "@/lib/agent-grants"
import {
  AUTHORITY_PROPERTY,
  PACKAGE_PROPERTY,
  authorityIsDerived,
  derivedAuthority,
  derivedPackage,
  isDeclarationKind,
  packageIsDerived,
} from "@/lib/declarations"
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
import { checkValue, emptyContainer, systemSpecs } from "@/lib/record-schema"
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

export function useDocumentForm({
  text,
  kind,
  record,
  onChange,
}: {
  text: string
  kind: KindInfo
  record?: SubstrateRecord
  onChange: (text: string) => void
}) {
  const fields = useMemo(() => requiredFirst(buildFormFields(kind)), [kind])
  const properties = propertiesOf(text)
  // A DECLARATION is named by its id, not given one: the id's first segment IS
  // its authority and the second IS its package.
  const declared = isDeclarationKind(kind.identity)
  const derivesAuthority = authorityIsDerived(kind.identity)
  const derivesPackage = packageIsDerived(kind.identity)

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

  function emit(next: string) {
    setEmitted(next)
    onChange(next)
  }

  /** The id, and with it the authority and the package a DECLARATION's id puts
   * it under. The write still carries `authority` and `package` (the loader
   * requires both), but nobody is asked to type segments they have already
   * typed in the id. */
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
    const pkg = derivedPackage(kind.identity, next)
    if (derivesPackage && pkg !== undefined) {
      doc = setIn(doc, ["data", "properties", PACKAGE_PROPERTY], pkg)
      setValues((prev) => ({ ...prev, [PACKAGE_PROPERTY]: pkg }))
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

  const system = new Set(systemSpecs(kind).map((s) => s.name))
  const elsewhere = Object.keys(properties ?? {}).filter(
    (name) => !fields.some((f) => f.name === name) && !system.has(name)
  )
  const hints =
    kind.identity === AGENT_KIND && properties ? grantHints(properties) : []

  /** Write one value straight into the document at a property: the title
   * and the body, which the sheet renders outside the rows. */
  function setProperty(name: string, value: string) {
    const path = ["data", "properties", name]
    emit(value ? setIn(text, path, value) : deleteIn(text, path))
  }

  return {
    fields,
    values,
    errors,
    properties,
    declared,
    derivesAuthority,
    derivesPackage,
    elsewhere,
    hints,
    id: idOf(text),
    commit,
    setRecordId,
    setProperty,
  }
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
