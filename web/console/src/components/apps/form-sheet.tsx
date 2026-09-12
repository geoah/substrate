/** The create and patch sheet: a bottom sheet on a phone, a right one on a
 * desktop, the declaration's own controls (`PropertyField`) for exactly the
 * action's `prompt` list, every seeded value (`via`, `set`, the filter's
 * `eq`) shown read-only above them, and the submit INSIDE the sheet's scroll
 * so a keyboard never hides it. Validation is the form core's (`validate`),
 * the payload is `toProperties`, typed as the declaration says. */

import { useState } from "react"

import { PropertyField } from "@/components/record/property-field"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Spinner } from "@/components/ui/spinner"
import { useIsMobile } from "@/hooks/use-mobile"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { seedField } from "@/lib/apps/form"
import {
  initialValues,
  toProperties,
  validate,
  type FormField,
  type FormMode,
  type FormValue,
  type FormValues,
} from "@/lib/record-form"
import { editableValue, formatValue } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

export interface FormSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  kind?: KindInfo
  kinds: KindInfo[]
  /** The prompted fields, in the action's order. */
  fields: FormField[]
  /** Written without asking; shown read-only and never editable here. */
  seed: Record<string, unknown>
  /** A heading for a seeded value where one is known (a parent's title). */
  seedLabels?: Record<string, string>
  mode: FormMode
  /** The record a patch edits; its values seed the prompted fields. */
  record?: SubstrateRecord
  submitLabel: string
  /** Resolves to whether the write landed; the sheet closes on true. */
  onSubmit: (properties: Record<string, unknown>) => Promise<boolean>
}

function SeedRow({
  field,
  value,
  label,
}: {
  field: FormField
  value: unknown
  label?: string
}) {
  const text =
    label ?? formatValue(field.spec, editableValue(field.spec, value))
  return (
    <Field>
      <div className="flex items-center gap-2">
        <FieldLabel className="font-normal">{field.label}</FieldLabel>
        <Badge variant="secondary" className="text-[0.65rem]">
          from the view
        </Badge>
      </div>
      <p className="data text-sm break-words">{text}</p>
    </Field>
  )
}

export function FormSheet({
  open,
  onOpenChange,
  title,
  description,
  kind,
  kinds,
  fields,
  seed,
  seedLabels,
  mode,
  record,
  submitLabel,
  onSubmit,
}: FormSheetProps) {
  const isMobile = useIsMobile()
  // A prompted name the seed already answers is shown, not asked.
  const asked = fields.filter((f) => !(f.name in seed))
  const seeded = Object.keys(seed)
    .map((name) => ({ name, field: seedField(kind, name, kinds) }))
    .filter((s): s is { name: string; field: FormField } => Boolean(s.field))

  const seedKey = `${open}:${record?.id ?? ""}:${record?.version ?? ""}`
  const [seededKey, setSeededKey] = useState(seedKey)
  const [values, setValues] = useState<FormValues>(() =>
    initialValues(asked, record)
  )
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [pending, setPending] = useState(false)
  // Reseed on every open (and on a newer record), adjusting during render.
  if (seededKey !== seedKey) {
    setSeededKey(seedKey)
    setValues(initialValues(asked, record))
    setErrors({})
  }

  function setValue(name: string, value: FormValue) {
    setValues((prev) => ({ ...prev, [name]: value }))
    setErrors((prev) => {
      if (!prev[name]) return prev
      const next = { ...prev }
      delete next[name]
      return next
    })
  }

  async function submit() {
    const failures = validate(asked, values, mode)
    if (failures.length) {
      setErrors(Object.fromEntries(failures.map((f) => [f.name, f.message])))
      return
    }
    setPending(true)
    try {
      const ok = await onSubmit(toProperties(asked, values, mode))
      if (ok) onOpenChange(false)
    } finally {
      setPending(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={(next) => !pending && onOpenChange(next)}>
      <SheetContent
        side={isMobile ? "bottom" : "right"}
        className={cn(
          "gap-0 p-0",
          isMobile ? "max-h-[92dvh] rounded-t-2xl" : "sm:max-w-md"
        )}
      >
        <SheetHeader className="shrink-0 pr-12">
          <SheetTitle>{title}</SheetTitle>
          {description && <SheetDescription>{description}</SheetDescription>}
        </SheetHeader>
        <form
          className="flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain px-4 pb-[max(1rem,env(safe-area-inset-bottom))]"
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <FieldGroup className="gap-5">
            {seeded.map(({ name, field }) => (
              <SeedRow
                key={name}
                field={field}
                value={seed[name]}
                label={seedLabels?.[name]}
              />
            ))}
            {asked.map((field) => (
              <PropertyField
                key={field.name}
                field={field}
                value={values[field.name]}
                onChange={(next) => setValue(field.name, next)}
                mode={mode}
                error={errors[field.name]}
                kinds={kinds}
                idPrefix="view-form"
              />
            ))}
          </FieldGroup>
          <Button
            type="submit"
            className="mt-6 h-12 w-full shrink-0 text-base sm:h-9 sm:text-sm"
            disabled={pending}
          >
            {pending && <Spinner className="size-4" />}
            {submitLabel}
          </Button>
        </form>
      </SheetContent>
    </Sheet>
  )
}
