/** The stacked control a REPEATED pointer earns: what has been chosen shows as
 * one row per record (its glyph and title, and a remove), and the add opens the
 * same dropdown one pointer opens. A row's title is read by `RecordRef`, batched
 * with every other mark on the page.
 *
 * A column of empty pickers would ask somebody to look at a control per row; a
 * list should read as the list it is. What to offer and how is
 * `RecordCombobox`'s; this owns only the stacking. Each row holds two halves
 * (the pin's kind, the record's id), and the form core joins them into the one
 * path the write carries. */

import { XIcon } from "lucide-react"

import { RecordRef } from "@/components/identity/record-ref"
import { RecordCombobox } from "@/components/record/record-combobox"
import { Button } from "@/components/ui/button"
import type { KindInfo } from "@/lib/api/types"
import type { RefValue } from "@/lib/record-form"

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
  // A list of pointers is a SET: what is already in it is not offered again.
  const held = new Set(value.filter((r) => r.kind === pin).map((r) => r.id))
  const pathOf = (ref: RefValue) => `${ref.kind}/${ref.id}`

  return (
    <div className="flex flex-col gap-1.5">
      {value.length > 0 && (
        <ul className="flex flex-col overflow-hidden rounded-lg border border-input">
          {value.map((ref, i) => (
            <li
              key={`${pathOf(ref)}-${i}`}
              className="flex min-h-9 items-center gap-2 border-b px-2.5 py-1 last:border-b-0"
            >
              <span
                className="min-w-0 flex-1 text-sm"
                aria-label={`${label} ${i + 1}`}
              >
                {ref.kind ? (
                  <RecordRef kind={ref.kind} id={ref.id} link={false} />
                ) : (
                  <span className="data">{ref.id}</span>
                )}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Remove ${label} ${i + 1}`}
                title="Remove"
                onClick={() => onChange(value.filter((_, at) => at !== i))}
              >
                <XIcon />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <RecordCombobox
        pin={pin}
        kinds={kinds}
        self={self}
        exclude={held}
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
