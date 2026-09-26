/** One keyboard-driven list of choices, for every place a reader picks from a
 * declared set: a filter's states and enum values, a property's value. Each
 * row reads in display words (a state's word, an enum's label) and, with
 * Technical details on, the stored value beside them; the stored value is
 * what the caller hears. Arrow keys move, Enter picks, and a list long
 * enough to search offers a filter box.
 *
 * `multiple` toggles membership and keeps the list open, a box at each
 * row's end as the reference picker draws it; single marks the chosen row
 * with a check. */

import { useState, type ReactNode } from "react"
import { CheckIcon } from "lucide-react"

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { cn } from "@/lib/utils"

export interface ChoiceOption {
  /** The stored value: what the caller hears and what a write sends. */
  value: string
  /** The display words; what the filter box searches and a screen reader
   * reads. */
  label: string
  /** How the row draws the choice when it is more than words (a state's
   * badge, an enum's tag). The label by default. */
  display?: ReactNode
  /** A quiet note at the row's end ("no longer offered"). */
  hint?: ReactNode
  disabled?: boolean
}

/** How many choices a list shows before it offers a filter box. */
export const CHOICE_FILTER_FROM = 8

export function ChoiceList({
  options,
  selected,
  onChange,
  multiple = false,
  label,
  showValues,
  filter,
  className,
}: {
  options: ChoiceOption[]
  /** The stored values chosen now. */
  selected: readonly string[]
  /** Multiple: the whole next set. Single: the one value picked. */
  onChange: (next: string[]) => void
  multiple?: boolean
  /** The list's accessible name ("Status"). */
  label: string
  /** Show the stored value beside the words. Follows Technical details by
   * default; a `display` that already carries it (a StateBadge) passes
   * false. */
  showValues?: boolean
  /** Offer the filter box; by default once the list is long. */
  filter?: boolean
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const values = showValues ?? technical
  const chosen = new Set(selected)
  const searchable = filter ?? options.length > CHOICE_FILTER_FROM
  // The highlight opens on what is chosen, so Enter keeps it and one arrow
  // reaches its neighbour.
  const [highlight, setHighlight] = useState(
    () => selected[0] ?? options[0]?.value ?? ""
  )

  function pick(value: string) {
    if (!multiple) {
      onChange([value])
      return
    }
    const next = new Set(chosen)
    if (next.has(value)) next.delete(value)
    else next.add(value)
    // Declaration order, whatever order the reader clicked in.
    onChange(options.map((o) => o.value).filter((v) => next.has(v)))
  }

  return (
    <Command
      loop
      value={highlight}
      onValueChange={setHighlight}
      data-slot="choice-list"
      className={cn("rounded-lg! bg-transparent p-0", className)}
    >
      {searchable && <CommandInput placeholder="Filter…" autoFocus />}
      <CommandList
        label={label}
        aria-multiselectable={multiple || undefined}
        className="p-1"
      >
        <CommandEmpty className="py-3 text-[13px] text-faint">
          Nothing matches.
        </CommandEmpty>
        <CommandGroup className="p-0">
          {options.map((option) => {
            const on = chosen.has(option.value)
            return (
              <CommandItem
                key={option.value}
                value={option.value}
                keywords={[option.label]}
                disabled={option.disabled}
                data-chosen={on || undefined}
                onSelect={() => pick(option.value)}
                className="h-[30px] gap-2 rounded-[5px] px-2 text-[13px] data-selected:bg-hover [&>svg:last-child]:hidden"
              >
                <span className="min-w-0 truncate">
                  {option.display ?? option.label}
                </span>
                {values && option.value !== option.label && (
                  <span className="shrink-0 font-mono text-[11.5px] text-faint">
                    {option.value}
                  </span>
                )}
                <span className="ml-auto flex shrink-0 items-center gap-1.5 text-xs text-faint">
                  {option.hint}
                  {multiple ? (
                    <span
                      aria-hidden
                      data-slot="choice-check"
                      className={cn(
                        "flex size-4 items-center justify-center rounded-[4px] border",
                        on
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-border-strong bg-background"
                      )}
                    >
                      {on && <CheckIcon className="size-3" strokeWidth={3} />}
                    </span>
                  ) : (
                    on && (
                      <CheckIcon
                        aria-hidden
                        data-slot="choice-check"
                        className="size-3.5 text-foreground"
                      />
                    )
                  )}
                </span>
                {on && <span className="sr-only">(chosen)</span>}
              </CommandItem>
            )
          })}
        </CommandGroup>
      </CommandList>
    </Command>
  )
}
