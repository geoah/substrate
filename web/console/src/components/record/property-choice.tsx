/** A value picked from a short list, in a form: a trigger that reads the
 * value's display word and, dropping from it, the one ChoiceList the property
 * sheet and the filters use, so an enum, a state or a collection is chosen
 * the same way on every surface. */

import { useRef, useState } from "react"
import { ChevronsUpDownIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { ChoiceList, type ChoiceOption } from "@/components/ui/choice-list"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { cn } from "@/lib/utils"

export function PropertyChoice({
  id,
  label,
  choices,
  value,
  onChange,
  placeholder = "Choose…",
  clearLabel,
  showValues,
  invalid,
  disabled,
}: {
  /** The trigger's id, for the label that names it. */
  id?: string
  /** What the list is choosing, for a screen reader. */
  label: string
  choices: ChoiceOption[]
  value: string
  onChange: (value: string) => void
  placeholder?: string
  /** Offered last, when the value may be emptied; picking it writes "". */
  clearLabel?: string
  /** As ChoiceList's: false where each choice's display says it already. */
  showValues?: boolean
  invalid?: boolean
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const held = choices.find((c) => c.value === value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            id={id}
            type="button"
            variant="outline"
            disabled={disabled}
            aria-invalid={invalid || undefined}
            className={cn(
              "w-full justify-between font-normal",
              !value && "text-muted-foreground"
            )}
          />
        }
      >
        <span className="flex min-w-0 items-center gap-1.5 truncate">
          {held ? (held.display ?? held.label) : value || placeholder}
        </span>
        <ChevronsUpDownIcon className="ml-2 size-3.5 shrink-0 opacity-50" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        initialFocus={root}
        className="w-64 max-w-[calc(100vw-2rem)] gap-0 p-0"
      >
        <ChoiceList
          ref={root}
          label={label}
          options={choices}
          selected={value ? [value] : []}
          clearLabel={clearLabel}
          showValues={showValues}
          onChange={([next]) => {
            onChange(next ?? "")
            setOpen(false)
          }}
        />
      </PopoverContent>
    </Popover>
  )
}
