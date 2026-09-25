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

import { AlertTriangleIcon } from "lucide-react"

import { useDocumentForm } from "@/components/record/use-document-form"
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
  AUTHORITY_PROPERTY,
  PACKAGE_PROPERTY,
  declarationIdShape,
} from "@/lib/declarations"

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
  const {
    fields,
    values,
    errors,
    properties,
    declared,
    derivesAuthority,
    derivesPackage,
    elsewhere,
    hints,
    id,
    commit,
    setRecordId,
  } = useDocumentForm({ text, kind, record, onChange })

  if (properties === undefined) {
    return (
      <div className="p-6 text-sm text-muted-foreground">
        This document does not parse yet, so the form cannot read it. Fix the
        YAML and the fields come back.
      </div>
    )
  }

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
            aria-invalid={declared && !record && !id.trim()}
            value={id}
            placeholder={
              declared
                ? declarationIdShape(kind.identity)
                : "the substrate mints one when this is blank"
            }
            onChange={(e) => setRecordId(e.target.value)}
          />
          <FieldDescription>
            {record
              ? "A record's id never changes. This saves onto the record you opened."
              : declared
                ? `Required. A ${kind.name} carries its own id, so write one here.`
                : "Optional. Leave it blank and the substrate makes one."}
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
            derivedNote={
              derivesAuthority && field.name === AUTHORITY_PROPERTY
                ? "Taken from the record id, its first segment."
                : derivesPackage && field.name === PACKAGE_PROPERTY
                  ? "Taken from the record id, its second segment."
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
