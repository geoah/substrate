/** A new record, laid out like the record it will become: the title as a
 * large input at the top, then the property rows (an icon and a label on the
 * left, the control on the right), the optional ones that start empty folded
 * into "N more", and the prose under a divider. It writes into the same
 * document the YAML lens edits (`useDocumentForm`), so the two stay one. */

import { useState, type ReactNode } from "react"
import { AlertTriangleIcon, ChevronDownIcon } from "lucide-react"

import { propertyIcon } from "@/components/property-sheet/sheet-model"
import { PropertyField } from "@/components/record/property-field"
import { useDocumentForm } from "@/components/record/use-document-form"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { KindInfo } from "@/lib/api/types"
import {
  AUTHORITY_PROPERTY,
  PACKAGE_PROPERTY,
  declarationIdShape,
} from "@/lib/declarations"
import { displayPlural, untitled } from "@/lib/kind-names"
import { fieldOf, type FormField, type FormValue } from "@/lib/record-form"
import { bodyProperty, systemSpecs, titleProperty } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

/** Whether a control holds anything yet. */
function holds(value: FormValue | undefined): boolean {
  if (value === undefined || value === null || value === "") return false
  if (value === false) return false
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === "object") {
    if ("id" in value && "kind" in value) return Boolean(value.id)
    return Object.keys(value).length > 0
  }
  return true
}

function RowLabel({ field, htmlFor }: { field: FormField; htmlFor: string }) {
  const [technical] = useTechnicalDetails()
  const { icon: Icon } = propertyIcon(field.spec)
  return (
    <label
      htmlFor={htmlFor}
      title={field.description}
      className="flex min-h-9 min-w-0 items-center gap-[7px] pr-1.5 pl-0.5 text-[13.5px] text-muted-foreground"
    >
      <Icon aria-hidden className="size-3.5 shrink-0 text-faint" />
      <span className="truncate">
        {field.label}
        {field.required && (
          <span aria-hidden className="text-destructive">
            {" "}
            *
          </span>
        )}
      </span>
      {technical && field.label !== field.name && (
        <span className="truncate font-mono text-[11.5px] text-faint">
          {field.name}
        </span>
      )}
    </label>
  )
}

export function CreateSheet({
  text,
  kind,
  kinds,
  onChange,
  meta,
}: {
  text: string
  kind: KindInfo
  kinds: KindInfo[]
  onChange: (text: string) => void
  /** One quiet line under the title. */
  meta?: ReactNode
}) {
  const [technical] = useTechnicalDetails()
  const form = useDocumentForm({ text, kind, onChange })
  const titleName = titleProperty(kind)
  const body = bodyProperty(kind)
  const titleField = form.fields.find((f) => f.name === titleName)
  const bodyField = body
    ? form.fields.find((f) => f.name === body.name)
    : undefined
  // A temporal trait binds a hot column the kind may not declare; a new
  // record may set it like any other property.
  const [temporal] = useState(() =>
    systemSpecs(kind)
      .filter(
        (s) => s.name !== "title" && !form.fields.some((f) => f.name === s.name)
      )
      .map((s) => fieldOf({ ...s, description: undefined }))
  )
  const rows = [...form.fields, ...temporal].filter(
    (f) => f !== titleField && f !== bodyField
  )
  // Which rows start open is decided once: a row the person empties again
  // must not vanish under their cursor.
  const [initiallyShown] = useState(
    () =>
      new Set(
        rows
          .filter(
            (f) =>
              f.required || f.control === "state" || holds(form.values[f.name])
          )
          .map((f) => f.name)
      )
  )
  const [more, setMore] = useState(false)
  const shown = more ? rows : rows.filter((f) => initiallyShown.has(f.name))
  const folded = rows.filter((f) => !initiallyShown.has(f.name))

  if (form.properties === undefined) {
    return (
      <p className="py-4 text-sm text-muted-foreground">
        This document does not parse yet, so the form cannot read it. Fix the
        YAML and the fields come back.
      </p>
    )
  }

  const titleValue = titleField
    ? String(form.values[titleField.name] ?? "")
    : typeof form.properties.title === "string"
      ? form.properties.title
      : ""
  const showId = form.declared || technical

  return (
    <div data-slot="create-sheet">
      <input
        aria-label={titleField?.label ?? "Title"}
        placeholder={untitled(kind)}
        value={titleValue}
        autoFocus
        onChange={(e) =>
          titleField
            ? form.commit(titleField, e.target.value)
            : form.setProperty("title", e.target.value)
        }
        className="mt-2.5 mb-1 w-full border-0 bg-transparent text-[32px] leading-[1.15] font-bold tracking-[-0.025em] outline-none placeholder:text-faint"
      />
      {titleField && form.errors[titleField.name] && titleValue && (
        <p role="alert" className="text-[12.5px] text-destructive">
          {form.errors[titleField.name]}
        </p>
      )}

      {meta && (
        <div className="flex flex-wrap items-center gap-x-3.5 text-[12.5px] text-faint">
          {meta}
        </div>
      )}

      {form.hints.length > 0 && (
        <ul className="mt-3 flex flex-col gap-1.5 rounded-md border border-warning/40 bg-warn-soft p-3">
          {form.hints.map((hint) => (
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

      <div className="my-[18px] grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-2 gap-y-1.5 sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]">
        {showId && (
          <>
            <label
              htmlFor="new-record-id"
              className="flex min-h-9 items-center pl-0.5 text-[13.5px] text-muted-foreground"
            >
              ID
              {form.declared && (
                <span aria-hidden className="text-destructive">
                  {" "}
                  *
                </span>
              )}
            </label>
            <div className="flex min-h-9 flex-col justify-center">
              <input
                id="new-record-id"
                value={form.id}
                placeholder={
                  form.declared
                    ? declarationIdShape(kind.identity)
                    : "Leave empty and one is made for you"
                }
                onChange={(e) => form.setRecordId(e.target.value)}
                className="h-8 w-full rounded-md border border-input bg-transparent px-2.5 font-mono text-[13px] outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
              />
            </div>
          </>
        )}
        {shown.map((field) => {
          const id = `new-${field.name}`
          return (
            <div
              key={field.name}
              className="contents"
              data-property={field.name}
            >
              <RowLabel field={field} htmlFor={id} />
              <div className="flex min-h-9 min-w-0 flex-col justify-center gap-1 py-0.5">
                <PropertyField
                  field={field}
                  value={form.values[field.name]}
                  onChange={(next) => form.commit(field, next)}
                  mode="create"
                  error={form.errors[field.name]}
                  kinds={kinds}
                  idPrefix="new"
                  bare
                  derivedNote={
                    form.derivesAuthority && field.name === AUTHORITY_PROPERTY
                      ? "Taken from the ID, its first segment."
                      : form.derivesPackage && field.name === PACKAGE_PROPERTY
                        ? "Taken from the ID, its second segment."
                        : undefined
                  }
                />
                {field.control === "state" && (
                  <span className="text-xs text-faint">
                    New {displayPlural(kind).toLowerCase()} start here.
                  </span>
                )}
              </div>
            </div>
          )
        })}
        {folded.length > 0 && !more && (
          <button
            type="button"
            onClick={() => setMore(true)}
            className="col-span-full flex items-center gap-1.5 px-0.5 py-1.5 text-left text-[13px] text-faint hover:text-muted-foreground"
          >
            <ChevronDownIcon aria-hidden className="size-3.5" />
            <span className="truncate">
              {folded.length} more: {folded.map((f) => f.label).join(", ")}
            </span>
          </button>
        )}
      </div>

      {bodyField && (
        <>
          <div className="my-[18px] h-px bg-border" />
          <textarea
            aria-label={bodyField.label}
            placeholder={`Add ${bodyField.label.toLowerCase()}…`}
            value={String(form.values[bodyField.name] ?? "")}
            rows={5}
            onChange={(e) => form.commit(bodyField, e.target.value)}
            className={cn(
              "w-full max-w-[68ch] resize-y border-0 bg-transparent p-0 leading-[1.65] outline-none placeholder:text-faint"
            )}
          />
        </>
      )}

      {technical && form.elsewhere.length > 0 && (
        <p className="mt-3 text-xs text-muted-foreground">
          <span className="font-mono">{form.elsewhere.join(", ")}</span>{" "}
          {form.elsewhere.length === 1 ? "is" : "are"} in the document but not
          offered here. The YAML edits them, and they are written as they stand.
        </p>
      )}
    </div>
  )
}
