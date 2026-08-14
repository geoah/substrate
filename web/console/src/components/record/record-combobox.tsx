/** THE record picker: a dropdown you open and choose from.
 *
 * A datalist was the old idiom here and it was wrong: it hangs off a text box,
 * shows nothing but the value, and reads as autocomplete rather than as a
 * choice. This is the tree's own combobox anatomy instead (the `Popover`
 * primitive around the vendored `Command`, cmdk), so it OPENS ON CLICK, puts a
 * search on top, and gives every record a row with its title, its id and its
 * one-liner. The four host functions therefore read as cards, which is the
 * whole point of offering them at all.
 *
 * FREE TEXT IS NOT AN AFTERTHOUGHT. A record can be minted between one page and
 * the next, and a model can be told to name one this repository does not hold
 * yet, so whatever is typed is offered at the bottom as its own row. Selecting
 * it inserts the text verbatim.
 *
 * The list is CLIENT-side filtered, which is right for the registry-shaped
 * collections a marker names (tens of rows, fetched whole). A pinned reference
 * can point at an ordinary data collection, and there the fetch is capped and
 * the cap is said out loud rather than pretended away. */

import { useState } from "react"
import { CheckIcon, ChevronsUpDownIcon, PlusIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Spinner } from "@/components/ui/spinner"
import type { RecordOption, RecordOptions } from "@/lib/identities"
import { cn } from "@/lib/utils"

export interface RecordComboboxProps extends RecordOptions {
  /** Ids the label points at and the popover is described by. */
  id?: string
  /** The record chosen, by its own id: the pin supplies the kind the write
   * joins onto it. */
  value?: string
  onSelect: (value: string) => void
  /** What the trigger reads while nothing is chosen. */
  placeholder?: string
  /** Named when no `<label>` points at the trigger (a row of a list). */
  ariaLabel?: string
  invalid?: boolean
  /** The trigger is an ADD rather than a value: a repeated picker stacks what
   * it has chosen above and opens this to choose one more. */
  adding?: boolean
  /** What the add reads, and what a screen reader is told it adds. */
  addLabel?: string
}

/** One offered record, as a row: the title a reader recognises, the id a write
 * carries, and the one-liner that says what the thing is for. */
function OptionRow({
  option,
  chosen,
}: {
  option: RecordOption
  chosen: boolean
}) {
  // The RECORD is what a reader recognises: a title where there is one, else
  // the id it is named by.
  const heading = option.title || option.value
  return (
    <>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate", !option.title && "data")}>
          {heading}
        </span>
        {option.title && (
          <span className="truncate data text-xs text-muted-foreground">
            {option.value}
          </span>
        )}
        {option.description && (
          <span className="line-clamp-2 text-xs text-muted-foreground">
            {option.description}
          </span>
        )}
      </div>
      {chosen && <CheckIcon className="ml-auto size-3.5 shrink-0" />}
    </>
  )
}

export function RecordCombobox({
  id,
  value = "",
  onSelect,
  options,
  loading,
  error,
  capped,
  placeholder = "select a record",
  ariaLabel,
  invalid,
  adding,
  addLabel = "Add",
}: RecordComboboxProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")

  function choose(next: string) {
    onSelect(next)
    setQuery("")
    setOpen(false)
  }

  const typed = query.trim()
  // The escape hatch, offered only when it would say something the list does
  // not already: an exact match is the row above, not a second way to pick it.
  const freeText = typed && !options.some((o) => o.value === typed)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            id={id}
            type="button"
            variant="outline"
            size={adding ? "xs" : "default"}
            aria-label={ariaLabel}
            aria-invalid={invalid}
            className={cn(
              "justify-between font-normal",
              adding ? "self-start" : "w-full",
              !adding && !value && "text-muted-foreground"
            )}
          />
        }
      >
        {adding ? (
          <>
            <PlusIcon />
            {addLabel}
          </>
        ) : (
          <>
            <span className={cn("truncate", value && "data")}>
              {value || placeholder}
            </span>
            <ChevronsUpDownIcon className="ml-2 size-3.5 shrink-0 opacity-50" />
          </>
        )}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 max-w-[90vw] p-0">
        <Command>
          <CommandInput
            placeholder="Search records, or type an id"
            value={query}
            onValueChange={setQuery}
          />
          <CommandList>
            {loading ? (
              <div className="flex items-center gap-2 px-3 py-6 text-sm text-muted-foreground">
                <Spinner className="size-3.5" />
                Reading the collection
              </div>
            ) : error ? (
              <div className="px-3 py-6 text-sm text-destructive">
                {error}
                <p className="mt-1 text-xs text-muted-foreground">
                  Type an id to name a record anyway.
                </p>
              </div>
            ) : (
              <CommandEmpty className="text-muted-foreground">
                {options.length
                  ? "Nothing here matches."
                  : "This collection has no records yet."}
              </CommandEmpty>
            )}
            {options.length > 0 && (
              <CommandGroup>
                {options.map((option) => (
                  <CommandItem
                    key={option.value}
                    // Searching covers what a reader can SEE, so a description
                    // is as findable as an id.
                    value={`${option.value} ${option.title} ${option.description}`}
                    onSelect={() => choose(option.value)}
                  >
                    <OptionRow
                      option={option}
                      chosen={option.value === value}
                    />
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {freeText && (
              <CommandGroup>
                <CommandItem
                  value={`use-typed-${typed}`}
                  onSelect={() => choose(typed)}
                >
                  <span className="truncate">
                    Use <span className="data">{typed}</span>
                  </span>
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
          {capped && (
            <p className="border-t px-3 py-2 text-xs text-muted-foreground">
              Showing the first {options.length}. Type to narrow, or type an id
              that is not listed.
            </p>
          )}
        </Command>
      </PopoverContent>
    </Popover>
  )
}
