/** The stacked control a REPEATED pointer earns: what has been chosen shows as
 * removable rows, and the add opens the same dropdown one pointer opens.
 *
 * A column of empty pickers would ask somebody to look at a control per row; a
 * list should read as the list it is. What to offer is `lib/identities.ts`'s
 * question and how to offer it is `RecordCombobox`'s; this owns only the
 * stacking. Each row holds two halves (the pin's kind, the record's id), and
 * the form core joins them into the one path the write carries. */

import { RecordCombobox } from "@/components/record/record-combobox"
import { Button } from "@/components/ui/button"
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import type { KindInfo } from "@/lib/api/types"
import { useRecordOptions } from "@/lib/identities"
import type { RefValue } from "@/lib/record-form"
import { cn } from "@/lib/utils"

export function ReferenceListPicker({
  id,
  label,
  pin,
  kinds,
  self,
  value,
  onChange,
  invalid,
}: {
  id: string
  /** The property's own label, so each row says which list it belongs to. */
  label: string
  /** The kind every entry points at, or "" for a pointer at any kind. */
  pin: string
  kinds: KindInfo[]
  self?: string
  value: RefValue[]
  onChange: (value: RefValue[]) => void
  invalid?: boolean
}) {
  const offered = useRecordOptions(pin, kinds, self)
  // A list of pointers is a SET: what is already in it is not offered again.
  const held = new Set(value.filter((r) => r.kind === pin).map((r) => r.id))
  const remaining = offered.options.filter((o) => !held.has(o.value))
  // A chosen row reads by its referent's title: the loaded options where they
  // hold it, else one batched read over the paths they do not.
  const pathOf = (ref: RefValue) => `${ref.kind}/${ref.id}`
  const listed = new Map<string, string>()
  for (const ref of value) {
    if (ref.kind !== pin) continue
    const title = offered.options.find((o) => o.value === ref.id)?.title
    if (title) listed.set(pathOf(ref), title)
  }
  const unlisted = offered.loading
    ? []
    : value.filter((r) => r.kind && r.id && !listed.has(pathOf(r))).map(pathOf)
  const titles = useReferenceTitles(unlisted, kinds)
  const titleOf = (ref: RefValue) =>
    listed.get(pathOf(ref)) || titles.get(pathOf(ref)) || ""

  return (
    <div className="flex flex-col gap-1.5">
      {value.map((ref, i) => {
        const title = titleOf(ref)
        return (
          <div
            key={`${ref.kind}/${ref.id}-${i}`}
            className="flex items-center gap-2 rounded-lg border border-input px-2.5 py-1.5"
          >
            <span
              className={cn(
                "min-w-0 flex-1 truncate text-sm",
                !title && "data"
              )}
              aria-label={`${label} ${i + 1}`}
              title={pathOf(ref)}
            >
              {title || ref.id}
            </span>
            <Button
              type="button"
              variant="destructive"
              size="xs"
              aria-label={`Remove ${label} ${i + 1}`}
              onClick={() => onChange(value.filter((_, at) => at !== i))}
            >
              Remove
            </Button>
          </div>
        )
      })}
      <RecordCombobox
        {...offered}
        options={remaining}
        id={id}
        adding
        addLabel="Add"
        ariaLabel={`Add ${label}`}
        invalid={invalid}
        onSelect={(next) => onChange([...value, { kind: pin, id: next }])}
      />
    </div>
  )
}
