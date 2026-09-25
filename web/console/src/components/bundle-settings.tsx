/** One bundle's settings as a form: every `setting` and `secret` record under
 * the bundle's id prefix, one control each (decision record 0076), on the
 * bundle's own page under Providers.
 *
 * The controls are `PropertyField`'s, the same ones every other typed surface
 * renders, so a datatype looks and validates the same wherever it is edited.
 * A save PATCHES each changed record's `value` alone: the bundle owns the rest
 * of the record, and a blank secret is never sent, because that would seal an
 * empty string over the stored one. */

import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import { PropertyField } from "@/components/record/property-field"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { saveSettingValue } from "@/lib/api/settings"
import type { FormValue } from "@/lib/record-form"
import { unset, type SettingField } from "@/lib/settings"

/** The value a control holds, as the record stores it: `value` is a string on
 * both kinds, so a checkbox's boolean is spelled out. */
function stored(value: FormValue): string {
  if (typeof value === "boolean") return value ? "true" : "false"
  return typeof value === "string" ? value : ""
}

function seedOf(fields: SettingField[]): Record<string, FormValue> {
  return Object.fromEntries(
    fields.map((f) => [
      f.key,
      f.field.control === "bool" ? f.value === "true" : f.value,
    ])
  )
}

/** Whether this field carries a write. A secret has no stored value to compare
 * against, so anything typed into one is a write and a blank one is not. */
function changed(field: SettingField, value: FormValue): boolean {
  if (field.secret) return stored(value) !== ""
  return stored(value) !== field.value
}

export function BundleSettingsForm({ fields }: { fields: SettingField[] }) {
  const queryClient = useQueryClient()
  const [values, setValues] = useState<Record<string, FormValue>>(() =>
    seedOf(fields)
  )
  // What the person actually touched. A control has a value whether or not
  // anybody typed into it — an empty bool renders as `false` — so a save that
  // went by the value alone would fill in every unset checkbox on the card and
  // clear its setup item behind the person's back.
  const [edited, setEdited] = useState<string[]>([])
  // A required setting's error waits for the person: it says so once they
  // have touched the control or tried to save, not on a form nobody has used.
  const [attempted, setAttempted] = useState(false)
  // Reseed when the records move under the form (a save, a fresh read): the
  // stored value is the truth, and a secret input always returns to blank.
  const seedKey = fields
    .map((f) => `${f.key}:${f.record.version}`)
    .join("\u0000")
  const [seeded, setSeeded] = useState(seedKey)
  if (seeded !== seedKey) {
    setSeeded(seedKey)
    setValues(seedOf(fields))
    setEdited([])
    setAttempted(false)
  }

  const pending = fields.filter(
    (f) => edited.includes(f.key) && changed(f, values[f.key])
  )

  const save = useMutation({
    mutationFn: async () => {
      for (const field of pending) {
        await saveSettingValue(field.record, stored(values[field.key]))
      }
      return pending.length
    },
    onSuccess: (count) => {
      toast.add({
        type: "success",
        title: count === 1 ? "1 setting saved." : `${count} settings saved.`,
      })
      // A setting clears a setup item and feeds the bundle's functions, so the
      // status surfaces and the record reads both move.
      void queryClient.invalidateQueries()
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: "Saving the settings failed",
        description: error.message,
      })
    },
  })

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        setAttempted(true)
        if (pending.length) save.mutate()
      }}
    >
      <FieldGroup className="gap-5">
        {fields.map((field) => (
          <PropertyField
            key={field.key}
            field={field.field}
            idPrefix="setting"
            value={values[field.key] ?? ""}
            onChange={(next) => {
              setValues((prev) => ({ ...prev, [field.key]: next }))
              setEdited((prev) =>
                prev.includes(field.key) ? prev : [...prev, field.key]
              )
            }}
            mode="patch"
            error={
              unset(field) &&
              (attempted || edited.includes(field.key)) &&
              !pending.includes(field)
                ? "Required. This has no value yet."
                : undefined
            }
            labelAction={
              field.secret ? (
                <span
                  className={
                    field.set ? "text-xs text-ok" : "text-xs text-warning"
                  }
                >
                  {field.set ? "Saved" : "Not saved yet"}
                </span>
              ) : undefined
            }
          />
        ))}
      </FieldGroup>
      <div className="flex justify-end">
        <Button
          type="submit"
          size="sm"
          disabled={save.isPending || pending.length === 0}
        >
          {save.isPending && <Spinner className="size-3.5" />}
          Save
        </Button>
      </div>
    </form>
  )
}
