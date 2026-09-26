/** The integrations flow's generic record-edit surface (GUIDE §8): a single
 * dialog that CREATES or PATCHES a trait-typed config/account record through
 * the normal record API. The controls are `PropertyField`'s, the same ones the
 * record editor's form lens renders, so a datatype looks and validates the same
 * wherever it is edited; this dialog adds what is ITS own: the clear/undo
 * affordance, and a scope of one bundle config or account record rather than
 * any kind at all. */

import { useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { ChevronRightIcon } from "lucide-react"

import { PropertyField } from "@/components/record/property-field"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { FieldGroup } from "@/components/ui/field"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { createRecord, patchRecord } from "@/lib/api/records"
import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  buildFormFields,
  initialValues,
  toProperties,
  validate,
  type FieldError as FieldErr,
  type FormField,
  type FormMode,
  type FormValue,
  type FormValues,
} from "@/lib/record-form"
import { splitKind } from "@/lib/definition"

/** The named fields first, in the order named; the rest keep theirs. */
function firstThenRest(
  fields: FormField[],
  first: readonly string[]
): FormField[] {
  if (!first.length) return fields
  const ahead = first
    .map((name) => fields.find((f) => f.name === name))
    .filter((f): f is FormField => Boolean(f))
  return [...ahead, ...fields.filter((f) => !first.includes(f.name))]
}

interface RecordConfigFormProps {
  /** The trait-typed config/account kind being written. */
  type: KindInfo
  /** The record being edited; absent creates a new one. */
  record?: SubstrateRecord
  /** Dialog copy — what this write is, in the surface's own words. */
  title: string
  description: React.ReactNode
  /** Content between the header and the fields: what a person needs beside
   * them (the callback URL a client must register) and cannot sit inside the
   * description's paragraph. */
  lead?: React.ReactNode
  /** Property names the surface puts FIRST, in this order: a trait's
   * contracted properties (the `oauth2` client id and secret). The
   * declaration's other properties then fold under "more settings", closed
   * unless one of them holds a value or an error, because a bundle's extras
   * (a test seam, a budget) are not what a person setting up credentials
   * came for. */
  first?: readonly string[]
  /** Help under a property's control, by property name, in the surface's
   * own words: it replaces the declaration's description, which is written
   * for whoever wrote the bundle. */
  help?: Readonly<Record<string, string>>
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
  lead,
  first,
  help,
  open,
  onOpenChange,
  onSaved,
}: RecordConfigFormProps) {
  const queryClient = useQueryClient()
  const registry = useQuery(kindsQueryOptions)
  const fields = useMemo(
    () =>
      firstThenRest(buildFormFields(type), first ?? []).map((f) =>
        help?.[f.name] ? { ...f, description: help[f.name] } : f
      ),
    [type, first, help]
  )
  // The fold: with a `first` set, everything not in it is an extra.
  const extras = useMemo(
    () => (first?.length ? fields.filter((f) => !first.includes(f.name)) : []),
    [fields, first]
  )
  const [extrasOpen, setExtrasOpen] = useState(false)
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

  const { authority, pkg, name } = splitKind(type.identity)

  const mutation = useMutation({
    mutationFn: () => {
      const properties = toProperties(fields, values, mode)
      return record
        ? patchRecord(authority, pkg, type.name, record.id, { properties })
        : createRecord(authority, pkg, type.name, { properties })
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
        title: record
          ? `Saving the ${name} failed`
          : `Creating the ${name} failed`,
        description: error.message,
      })
    },
  })

  function setValue(field: string, value: FormValue) {
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

  function renderField(field: FormField) {
    const cleared = values[field.name] === null
    // An ordinary field with a stored value can be explicitly cleared on
    // edit — blank alone only preserves it.
    const clearable =
      mode === "patch" && field.control !== "secret" && field.control !== "bool"
    const stored = record?.properties?.[field.name]
    const hasStored = stored !== undefined && stored !== null && stored !== ""
    return (
      <PropertyField
        key={field.name}
        field={field}
        value={values[field.name]}
        onChange={(next) => setValue(field.name, next)}
        mode={mode}
        error={errors[field.name]}
        kinds={registry.data ?? []}
        labelAction={
          clearable && hasStored ? (
            <button
              type="button"
              className="text-xs text-muted-foreground underline-offset-4 hover:underline"
              onClick={() =>
                setValue(field.name, cleared ? seed[field.name] : null)
              }
            >
              {cleared ? "Undo" : "Clear"}
            </button>
          ) : undefined
        }
      />
    )
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
        {lead}
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
              if (extras.includes(field)) return null
              return renderField(field)
            })}
            {extras.length > 0 && (
              <Collapsible
                open={extrasOpen || extras.some((f) => Boolean(errors[f.name]))}
                onOpenChange={setExtrasOpen}
                className="flex flex-col gap-4"
              >
                <CollapsibleTrigger className="group flex w-fit items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline">
                  <ChevronRightIcon className="size-3 transition-transform group-data-[panel-open]:rotate-90" />
                  {extras.length === 1
                    ? "1 more setting, rarely needed"
                    : `${extras.length} more settings, rarely needed`}
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <FieldGroup className="gap-5">
                    {extras.map((field) => renderField(field))}
                  </FieldGroup>
                </CollapsibleContent>
              </Collapsible>
            )}
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
