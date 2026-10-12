/** The observations pages' compact controls: a quiet label beside a
 * control, and a labelled button opening one list of choices (the metrics,
 * a metric's reducer, its unit). */

import { useState, type ReactNode } from "react"
import { ChevronDownIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { ChoiceList, type ChoiceOption } from "@/components/ui/choice-list"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"

export function Labelled({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-[12.5px] text-faint">{label}</span>
      {children}
    </div>
  )
}

/** A labelled button opening one list of choices. */
export function Picker({
  label,
  options,
  selected,
  multiple = false,
  onChange,
}: {
  label: string
  options: ChoiceOption[]
  selected: string[]
  multiple?: boolean
  onChange: (next: string[]) => void
}) {
  const [open, setOpen] = useState(false)
  const words = selected.map(
    (v) => options.find((o) => o.value === v)?.label ?? v
  )
  const shown =
    words.length > 2
      ? `${words.slice(0, 2).join(", ")} and ${words.length - 2} more`
      : words.join(", ")
  return (
    <Labelled label={label}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              aria-label={`${label}: ${shown || "none"}`}
              className="h-7 max-w-80 gap-1.5 text-[12.5px] font-normal"
            />
          }
        >
          <span className="truncate">{shown || "Choose"}</span>
          <ChevronDownIcon className="size-3.5 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-0">
          <ChoiceList
            label={label}
            multiple={multiple}
            options={options}
            selected={selected}
            onChange={(next) => {
              onChange(next)
              if (!multiple) setOpen(false)
            }}
          />
        </PopoverContent>
      </Popover>
    </Labelled>
  )
}
