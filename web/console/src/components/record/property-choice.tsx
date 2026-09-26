/** A value picked from a short list, in a form: a trigger that reads the
 * value's display word and a popover list the keyboard drives (arrows, Home,
 * End, Enter; typing filters a long one). The same look as the property
 * sheet's in-place choosers, so an enum, a state or a collection is chosen
 * the same way on every surface. The stored value rides beside the word in
 * technical mode. */

import { useRef, useState, type ReactNode } from "react"
import { CheckIcon, ChevronsUpDownIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { cn } from "@/lib/utils"

export interface Choice {
  /** What is stored. */
  value: string
  /** The display word; also what the filter matches. */
  label: string
  /** How the choice draws, where a word alone is not it (a state's dot, a
   * collection's glyph). */
  node?: ReactNode
  /** A short note at the row's end. */
  hint?: string
}

/** How many choices a list shows before it offers a filter box. */
const FILTER_FROM = 8

export function PropertyChoice({
  id,
  label,
  choices,
  value,
  onChange,
  placeholder = "Choose…",
  clearLabel,
  invalid,
  disabled,
}: {
  /** The trigger's id, for the label that names it. */
  id?: string
  /** What the list is choosing, for a screen reader. */
  label: string
  choices: Choice[]
  value: string
  onChange: (value: string) => void
  placeholder?: string
  /** Offered last, when the value may be emptied; picking it writes "". */
  clearLabel?: string
  invalid?: boolean
  disabled?: boolean
}) {
  const [technical] = useTechnicalDetails()
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const held = choices.find((c) => c.value === value)

  function pick(next: string) {
    onChange(next)
    setOpen(false)
  }

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
          {held ? (held.node ?? held.label) : value || placeholder}
        </span>
        <ChevronsUpDownIcon className="ml-2 size-3.5 shrink-0 opacity-50" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        initialFocus={root}
        className="w-64 max-w-[calc(100vw-2rem)] gap-0 p-0"
      >
        <Command
          ref={root}
          tabIndex={-1}
          loop
          defaultValue={value || choices[0]?.value}
          className="rounded-lg! p-0 outline-none"
        >
          {choices.length >= FILTER_FROM && (
            <CommandInput placeholder="Filter…" autoFocus />
          )}
          <CommandList label={label} className="p-1">
            <CommandEmpty className="py-3 text-center text-[13px] text-faint">
              Nothing matches.
            </CommandEmpty>
            {choices.map((choice) => (
              <CommandItem
                key={choice.value}
                value={choice.value}
                keywords={[choice.label]}
                onSelect={() => pick(choice.value)}
                className="min-h-[30px] rounded-[5px] px-2 text-[13px] data-selected:bg-hover [&>svg:last-child]:hidden"
              >
                <span className="flex min-w-0 items-center gap-1.5">
                  {choice.node ?? choice.label}
                </span>
                <span className="ml-auto flex items-center gap-1.5 text-xs text-faint">
                  {choice.hint}
                  {technical && choice.value !== choice.label && (
                    <span className="font-mono text-[11.5px]">
                      {choice.value}
                    </span>
                  )}
                  {choice.value === value && <CheckIcon className="size-3.5" />}
                </span>
              </CommandItem>
            ))}
            {clearLabel && value && (
              <CommandItem
                value="__clear"
                keywords={["clear"]}
                onSelect={() => pick("")}
                className="min-h-[30px] rounded-[5px] px-2 text-[13px] text-muted-foreground data-selected:bg-hover [&>svg:last-child]:hidden"
              >
                {clearLabel}
              </CommandItem>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
