/** The integrations flow's generic record-edit surface (GUIDE §8): a single
 * dialog that CREATES or PATCHES a trait-typed config/account record through
 * the normal record API. It renders one control per editable schema property
 * (text / write-only secret / boolean toggle / single-choice select /
 * repeated-string list), validates required fields and a create's secret before
 * it submits, lets an ordinary field be explicitly cleared, confirms the write
 * is real, and never shows a stored secret. Scoped to the bundle config +
 * account records the flow owns — not a general record editor. */

import { useMemo, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { cn } from "@/lib/utils"
import { createRecord, patchRecord } from "@/lib/api/records"
import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  buildFormFields,
  humanizeName,
  initialValues,
  toProperties,
  validate,
  type FieldError as FieldErr,
  type FormMode,
  type FormValues,
} from "@/lib/record-form"
import { splitKind } from "@/lib/definition"

/** A native select styled to match the Input primitive (no shadcn Select
 * primitive is vendored yet; an enum field is a small, closed set). */
const SELECT_CLASS =
  "h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30"

interface RecordConfigFormProps {
  /** The trait-typed config/account kind being written. */
  type: KindInfo
  /** The record being edited; absent creates a new one. */
  record?: SubstrateRecord
  /** Dialog copy — what this write is, in the surface's own words. */
  title: string
  description: React.ReactNode
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Run after a successful write (refresh the owning surface). */
  onSaved?: (record: SubstrateRecord) => void
}

export function RecordConfigForm({
  type,
  record,
  title,
  description,
  open,
  onOpenChange,
  onSaved,
}: RecordConfigFormProps) {
  const queryClient = useQueryClient()
  const fields = useMemo(() => buildFormFields(type), [type])
  const mode: FormMode = record ? "patch" : "create"
  // The seed the form starts from (and the value a Clear can be undone back to).
  const seed = useMemo(() => initialValues(fields, record), [fields, record])
  // Reseed whenever the dialog opens or the record changes — a secret never
  // seeds, so an edit always presents its secret inputs blank.
  const [values, setValues] = useState<FormValues>(seed)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const seedKey = `${record?.id ?? "new"}:${record?.version ?? ""}:${open}`
  const [seeded, setSeeded] = useState(seedKey)
  if (seeded !== seedKey) {
    setSeeded(seedKey)
    setValues(seed)
    setErrors({})
  }

  const { authority, name } = splitKind(type.identity)

  const mutation = useMutation({
    mutationFn: () => {
      const properties = toProperties(fields, values, mode)
      return record
        ? patchRecord(authority, type.plural, record.id, { properties })
        : createRecord(authority, type.plural, { properties })
    },
    onSuccess: (saved) => {
      toast.add({
        type: "success",
        title: record ? `${name} updated.` : `${name} created.`,
      })
      // A config/account write moves what the bundle status, the trait query
      // and the record reads all show — refresh broadly.
      void queryClient.invalidateQueries()
      onOpenChange(false)
      onSaved?.(saved)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: record ? `Could not update the ${name}` : `Could not create the ${name}`,
        description: error.message,
      })
    },
  })

  function setValue(field: string, value: string | boolean | null) {
    setValues((prev) => ({ ...prev, [field]: value }))
    setErrors((prev) => {
      if (!prev[field]) return prev
      const next = { ...prev }
      delete next[field]
      return next
    })
  }

  function submit() {
    const failures = validate(fields, values, mode)
    if (failures.length) {
      setErrors(
        Object.fromEntries(failures.map((f: FieldErr) => [f.name, f.message]))
      )
      return
    }
    mutation.mutate()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !mutation.isPending && onOpenChange(next)}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <form
          id="record-config-form"
          className="max-h-[65vh] overflow-y-auto px-px"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <FieldGroup className="gap-5">
            {fields.map((field) => {
              const id = `f-${field.name}`
              const error = errors[field.name]
              const cleared = values[field.name] === null
              // An ordinary text/list/select field with a stored value can be
              // explicitly cleared on edit — blank alone only preserves it.
              const clearable =
                mode === "patch" &&
                field.control !== "secret" &&
                field.control !== "bool"
              const hasStored =
                record?.properties?.[field.name] !== undefined &&
                record?.properties?.[field.name] !== null &&
                record?.properties?.[field.name] !== ""

              if (field.control === "bool") {
                const checked = values[field.name] === true
                // Full-width block (GUIDE rule 13): the checkbox + label ride
                // one line, the description flows full-width beneath — never a
                // narrow label column with the help wrapping in a second one.
                return (
                  <Field key={field.name}>
                    <div className="flex items-center gap-2">
                      <input
                        id={id}
                        type="checkbox"
                        className="size-4 accent-primary"
                        checked={checked}
                        onChange={(e) => setValue(field.name, e.target.checked)}
                      />
                      <FieldLabel htmlFor={id} className="font-normal">
                        {field.label}
                      </FieldLabel>
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
                        {" "}*
                      </span>
                    )}
                  </FieldLabel>
                  {clearable && hasStored && !cleared && (
                    <button
                      type="button"
                      className="text-xs text-muted-foreground underline-offset-4 hover:underline"
                      onClick={() => setValue(field.name, null)}
                    >
                      Clear
                    </button>
                  )}
                  {clearable && cleared && (
                    <button
                      type="button"
                      className="text-xs text-muted-foreground underline-offset-4 hover:underline"
                      onClick={() => setValue(field.name, seed[field.name])}
                    >
                      Undo
                    </button>
                  )}
                </div>
              )

              if (field.control === "select") {
                const selectValue = cleared
                  ? ""
                  : String(values[field.name] ?? "")
                // The empty choice is offered for OPTIONAL enums (its "— none —"
                // is a real value: unset). A REQUIRED enum offers it ONLY while
                // the field is empty — a required select seeded with a default
                // (or an existing value) must not present an empty option, which
                // is the "two none options" the owner saw: an empty placeholder
                // beside the enum's own `none` value. Suppressing it when a value
                // is present leaves exactly the declared options.
                const showEmpty = !field.required || selectValue === ""
                return (
                  <Field key={field.name}>
                    {label}
                    <select
                      id={id}
                      className={cn(SELECT_CLASS, "data")}
                      aria-invalid={Boolean(error)}
                      value={selectValue}
                      onChange={(e) => setValue(field.name, e.target.value)}
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
                    {field.description && (
                      <FieldDescription>{field.description}</FieldDescription>
                    )}
                    {error && <FieldError>{error}</FieldError>}
                  </Field>
                )
              }

              if (field.control === "list") {
                return (
                  <Field key={field.name}>
                    {label}
                    <Textarea
                      id={id}
                      rows={3}
                      className="data text-xs"
                      aria-invalid={Boolean(error)}
                      value={cleared ? "" : String(values[field.name] ?? "")}
                      onChange={(e) => setValue(field.name, e.target.value)}
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

              const isSecret = field.control === "secret"
              return (
                <Field key={field.name}>
                  {label}
                  <Input
                    id={id}
                    type={isSecret ? "password" : field.inputType}
                    autoComplete={isSecret ? "off" : undefined}
                    className="data"
                    aria-invalid={Boolean(error)}
                    placeholder={
                      isSecret && record ? "•••••••• (unchanged)" : undefined
                    }
                    value={cleared ? "" : String(values[field.name] ?? "")}
                    onChange={(e) => setValue(field.name, e.target.value)}
                  />
                  {field.description && (
                    <FieldDescription>{field.description}</FieldDescription>
                  )}
                  {error && <FieldError>{error}</FieldError>}
                </Field>
              )
            })}
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button
            variant="outline"
            disabled={mutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            form="record-config-form"
            disabled={mutation.isPending}
          >
            {mutation.isPending && <Spinner className="size-3.5" />}
            {record ? "Save changes" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
