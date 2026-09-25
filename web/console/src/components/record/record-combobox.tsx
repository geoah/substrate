/** THE record picker: a dropdown you open and choose from.
 *
 * It opens on click, puts a search on top and gives every record a row: its
 * kind's glyph, its title, and its one-liner. A record with no title (the
 * registry-shaped collections: functions, kinds) is named by its id, which is
 * the only name it has; a titled one keeps its id for technical mode.
 *
 * The rows are the most recent page of the pinned collection, and typing asks
 * the SERVER, so a collection of thousands is a few keystrokes away rather
 * than capped at the page the browser loaded (`usePickerRecords`). Every read
 * ends: rows, "No <plural> yet", or what went wrong beside a retry. A spinner
 * that never stops is the failure this is built against.
 *
 * FREE TEXT IS NOT AN AFTERTHOUGHT. A record can be minted between one page and
 * the next, and a model can be told to name one this repository does not hold
 * yet, so whatever is typed is offered at the bottom as its own row. Selecting
 * it inserts the text verbatim. */

import { useState } from "react"
import { CheckIcon, ChevronsUpDownIcon, PlusIcon } from "lucide-react"

import { KindGlyph } from "@/components/identity/kind-glyph"
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
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { KindInfo } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import {
  usePickerRecords,
  type PickerRecords,
  type RecordOption,
} from "@/lib/identities"
import { displayPlural, lowerFirst } from "@/lib/kind-names"
import { cn } from "@/lib/utils"

export interface RecordComboboxProps {
  /** Ids the label points at and the popover is described by. */
  id?: string
  /** The kind every offered record is: the reference's pin. */
  pin: string
  kinds: KindInfo[]
  /** A record never offered as its own referent. */
  self?: string
  /** Ids already held, so a list of pointers is not offered them again. */
  exclude?: ReadonlySet<string>
  /** The record chosen, by its own id: the pin supplies the kind the write
   * joins onto it. */
  value?: string
  onSelect: (value: string) => void
  /** The chosen record's title where the loaded options do not hold it;
   * absent, the trigger falls back to the id. */
  valueTitle?: string
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
  /** Open on mount: an in-place edit has already been asked for. */
  defaultOpen?: boolean
  /** Told whenever the list opens or closes; a close without a choice is a
   * cancel to an in-place edit. */
  onOpenChange?: (open: boolean) => void
  /** Offered as the list's last row when the value may be emptied. */
  onClear?: () => void
}

/** One offered record, as a row: the glyph and title a reader recognises, the
 * one-liner that says what the thing is for, and the id only in technical
 * mode (or as the name of a record that has no title). */
function OptionRow({
  option,
  pin,
  chosen,
  technical,
}: {
  option: RecordOption
  pin: string
  chosen: boolean
  technical: boolean
}) {
  return (
    <>
      <KindGlyph kind={pin} size="xs" className="mt-px self-start" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className={cn("truncate", !option.title && "data text-[13px]")}>
          {option.title || option.value}
        </span>
        {option.title && technical && (
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

/** Where the read stands, in words, when there are no rows to show. */
function ReadState({
  read,
  pin,
  kind,
  typed,
}: {
  read: PickerRecords
  pin: string
  kind?: KindInfo
  typed: string
}) {
  const plural = lowerFirst(displayPlural(kind ?? pin))
  if (read.status === "unresolved") {
    return (
      <div className="px-3 py-5 text-sm text-muted-foreground">
        This repository doesn’t have <span className="data">{pin}</span>, so
        there is nothing to list. Type an id to name a record anyway.
      </div>
    )
  }
  if (read.status === "loading") {
    return (
      <div
        role="status"
        className="flex items-center gap-2 px-3 py-6 text-sm text-muted-foreground"
      >
        <Spinner className="size-3.5" />
        {read.searching ? "Searching" : `Reading ${plural}`}
      </div>
    )
  }
  if (read.status === "offline" || read.status === "error") {
    return (
      <div role="alert" className="flex flex-col gap-1.5 px-3 py-5 text-sm">
        <span
          className={cn(
            read.status === "error" ? "text-destructive" : "text-foreground"
          )}
        >
          {read.status === "offline"
            ? "You’re offline. The list reads as soon as you’re back."
            : `The list didn’t load: ${read.error}`}
        </span>
        <span className="text-xs text-muted-foreground">
          Try again, or type an id to name a record anyway.
        </span>
        <Button
          type="button"
          size="xs"
          variant="outline"
          className="self-start"
          onClick={read.retry}
        >
          Try again
        </Button>
      </div>
    )
  }
  return (
    <CommandEmpty className="px-3 text-muted-foreground">
      {typed
        ? `No ${plural} match “${typed}”.`
        : read.allHeld
          ? `No other ${plural} to choose.`
          : `No ${plural} yet.`}
    </CommandEmpty>
  )
}

/** Under the rows: what they are out of, and a search still on its way or
 * refused while the page's own matches show. */
function Footer({ read, plural }: { read: PickerRecords; plural: string }) {
  let words: React.ReactNode = null
  if (read.searching && read.busy) words = `Searching all ${plural}…`
  else if (read.searching && read.status === "error") {
    words = (
      <>
        The search didn’t finish: {read.error}{" "}
        <button type="button" className="underline" onClick={read.retry}>
          Try again
        </button>
      </>
    )
  } else if (read.searching && read.searchCapped) {
    words = "Showing the first matches. Keep typing to narrow."
  } else if (!read.searching && read.capped) {
    words = `Showing the most recent. Type to search all ${plural}.`
  }
  if (!words) return null
  return (
    <p className="flex items-center gap-2 border-t px-3 py-2 text-xs text-muted-foreground">
      {read.busy && <Spinner className="size-3" />}
      <span>{words}</span>
    </p>
  )
}

export function RecordCombobox({
  id,
  pin,
  kinds,
  self,
  exclude,
  value = "",
  valueTitle,
  onSelect,
  placeholder = "Choose…",
  ariaLabel,
  invalid,
  adding,
  addLabel = "Add",
  defaultOpen = false,
  onOpenChange,
  onClear,
}: RecordComboboxProps) {
  const [technical] = useTechnicalDetails()
  const [open, setOpenState] = useState(defaultOpen)
  function setOpen(next: boolean) {
    setOpenState(next)
    onOpenChange?.(next)
  }
  const [query, setQuery] = useState("")
  const kind = kindByIdentity(kinds, pin)
  const read = usePickerRecords(open ? pin : undefined, kinds, query, {
    self,
    exclude,
  })
  const options = read.options
  const plural = lowerFirst(displayPlural(kind ?? pin))

  function choose(next: string) {
    onSelect(next)
    setQuery("")
    setOpen(false)
  }

  const typed = query.trim()
  // The escape hatch, offered only when it would say something the list does
  // not already: an exact match is the row above, not a second way to pick it.
  // An id is one word; a phrase is a search, never a record's name.
  const freeText =
    typed && !/\s/.test(typed) && !options.some((o) => o.value === typed)
  const chosenTitle =
    valueTitle || options.find((o) => o.value === value)?.title || ""

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
            <span
              className={cn("truncate", value && !chosenTitle && "data")}
              title={value || undefined}
            >
              {chosenTitle || value || placeholder}
            </span>
            <ChevronsUpDownIcon className="ml-2 size-3.5 shrink-0 opacity-50" />
          </>
        )}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 max-w-[90vw] p-0">
        {/* The rows are already what matches: the server's search and the
            page's own filter decide, so cmdk only moves the highlight. */}
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={`Search ${plural}, or type an id`}
            value={query}
            onValueChange={setQuery}
          />
          <CommandList>
            {!options.length && (
              <ReadState read={read} pin={pin} kind={kind} typed={typed} />
            )}
            {options.length > 0 && (
              <CommandGroup>
                {options.map((option) => (
                  <CommandItem
                    key={option.value}
                    value={`${option.value} ${option.title}`}
                    onSelect={() => choose(option.value)}
                    className="items-start"
                  >
                    <OptionRow
                      option={option}
                      pin={pin}
                      chosen={option.value === value}
                      technical={technical}
                    />
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {onClear && value && (
              <CommandGroup>
                <CommandItem
                  value="remove-the-value"
                  onSelect={() => {
                    onClear()
                    setQuery("")
                    setOpen(false)
                  }}
                >
                  <span className="text-muted-foreground">Remove</span>
                </CommandItem>
              </CommandGroup>
            )}
            {freeText && (
              <CommandGroup>
                <CommandItem
                  value={`use-typed-${typed}`}
                  onSelect={() => choose(typed)}
                >
                  <span className="truncate">
                    Use <span className="data">{typed}</span> as the id
                  </span>
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
          {options.length > 0 && <Footer read={read} plural={plural} />}
        </Command>
      </PopoverContent>
    </Popover>
  )
}
