/** The filter toolbar (rule 3: full-size controls, never chips). Each active
 * filter is an h-8 outline control with the `field | value | ×` anatomy;
 * clicking it reopens its value editor. The dashed "Add filter" opens a
 * Popover+Command faceted picker built ONLY from the declared/filterable
 * properties the server will actually filter.
 *
 * A reference is filtered by PICKING its referents: the bar is handed the
 * registry so a pin resolves to the collection to offer, and the control
 * reads the chosen records' titles. A bar handed none (the changelog's) keeps
 * the text box.
 *
 * Everyday, a property is its icon and its label; its datatype and a
 * reference's target kind are technical details, shown under the label only
 * with the switch on. */

import { useState } from "react"
import { ListFilterIcon, XIcon } from "lucide-react"

import { EnumTag } from "@/components/identity/enum-tag"
import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import { ChoiceList, type ChoiceOption } from "@/components/ui/choice-list"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Input } from "@/components/ui/input"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import type { KindInfo } from "@/lib/api/types"
import {
  canMatch,
  canPrefix,
  choiceWord,
  displayValue,
  filterValueText,
  isChoiceField,
  opFor,
  picksMany,
  parseValueInput,
  splitReferenceIds,
  type ActiveFilter,
} from "@/lib/filters"
import {
  kindByIdentity,
  propertyTypeLabel,
  type DeclaredProperty,
} from "@/lib/definition"
import { cn } from "@/lib/utils"
import { propertyIcon } from "./property-icon"
import { ReferenceFilterLabel, ReferencePicker } from "./reference-picker"

interface DataTableFiltersProps {
  fields: DeclaredProperty[]
  filters: ActiveFilter[]
  onChange: (filters: ActiveFilter[]) => void
  /** The registry, which resolves a reference field's pin to the collection
   * its picker offers. Absent, a reference field takes text. */
  kinds?: KindInfo[]
  /** How a field is named on screen; its key by default. */
  labelOf?: (name: string) => string
  /** The fields are a kind's own properties: states read as their words
   * ("Suggested"), not their stored values. Off for a bar whose facets only
   * borrow the state shape (History's kinds and actors). */
  words?: boolean
  className?: string
}

/** The kind a reference field's picker offers: the one its pin names. A pin
 * is a full identity (record 0098), so the lookup is the whole resolution.
 * Undefined for a field that is no reference, an unpinned one (`kind: any`),
 * a pin the registry does not hold, and a bar handed no registry, all of
 * which keep the text box. */
function referenceTarget(
  field: DeclaredProperty | undefined,
  kinds?: KindInfo[]
): KindInfo | undefined {
  if (field?.kind !== "reference" || !field.to || !kinds) return undefined
  return kindByIdentity(kinds, field.to)
}

/** A declared set as the value step lists it: the words the grid shows
 * (a state's badge, an enum's tag), in declaration order. A deprecated enum
 * value is still listed, since records may hold it. */
function choiceOptions(
  field: DeclaredProperty,
  words: boolean
): ChoiceOption[] {
  if (field.kind === "bool")
    return ["true", "false"].map((v) => ({
      value: v,
      label: choiceWord(v, field, words),
    }))
  if (field.kind === "enum")
    return (field.values ?? []).map((v) => ({
      value: v.value,
      label: choiceWord(v.value, field, words),
      display: <EnumTag prop={field} value={v.value} />,
      hint: v.deprecated ? "no longer offered" : undefined,
    }))
  return (field.states ?? []).map((state) => ({
    value: state,
    label: choiceWord(state, field, words),
    display: words ? (
      <StateBadge value={state} initial={field.initial} />
    ) : undefined,
  }))
}

/** The value step, shaped by the declared kind: a declared set (states,
 * enum values, yes or no) is a ChoiceList — several at once apply live, one
 * closes — a pinned reference offers its referents the same way; everything
 * else takes text on Enter. */
function ValueEditor({
  field,
  label,
  value,
  words,
  target,
  kinds,
  onApply,
  onCommit,
}: {
  field: DeclaredProperty
  /** How the field is named on screen. */
  label: string
  value: string
  words: boolean
  /** A reference field's resolved referent kind; the picker wants it. */
  target?: KindInfo
  kinds?: KindInfo[]
  /** Live update (facets) — keeps the popover open. */
  onApply: (value: string) => void
  /** Final value (text entry) — closes the popover. */
  onCommit: (value: string) => void
}) {
  const [draft, setDraft] = useState(value)

  if (target && kinds) {
    return (
      <ReferencePicker
        target={target}
        kinds={kinds}
        selected={splitReferenceIds(value)}
        onChange={(ids) => onApply(ids.join(","))}
      />
    )
  }

  if (isChoiceField(field)) {
    const many = picksMany(field)
    return (
      <ChoiceList
        label={label}
        options={choiceOptions(field, words)}
        selected={splitReferenceIds(value)}
        multiple={many}
        // A state's badge says its stored value itself in technical mode.
        showValues={field.kind === "state" && words ? false : undefined}
        onChange={(next) => {
          if (many) onApply(next.join(","))
          else if (next[0]) onCommit(next[0])
        }}
      />
    )
  }

  // Free text is a full-text MATCH on the property's own words (the wire's
  // `match`, in the search grammar); `=` asks for the exact value. Everything
  // else is the exact value it always was, with a comma for membership. A
  // reference whose pin did not resolve (unpinned, or ambiguous) is the one
  // pointer still typed, and the server admits only the whole record path.
  const matches = canMatch(field)
  const pointer = field.kind === "reference"
  return (
    <div className="flex flex-col gap-1.5 p-1">
      <Input
        autoFocus
        placeholder={
          pointer
            ? `${label} points at…`
            : matches
              ? `${label} mentions…`
              : field.repeated
                ? `${label} contains…`
                : `${label} is…`
        }
        className="h-8"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && draft.trim()) onCommit(draft.trim())
        }}
      />
      <span className="px-1 text-xs text-muted-foreground">
        {matches ? (
          <>
            Press Enter to apply. Every word must appear.{" "}
            <span className="data">lay*</span> is a word prefix,{" "}
            <span className="data">"a phrase"</span> keeps words together,{" "}
            <span className="data">-word</span> excludes,{" "}
            <span className="data">a OR b</span> takes either.{" "}
            <span className="data">=value</span> means exactly that value
          </>
        ) : pointer ? (
          <>
            Press Enter to apply. The record's whole path,{" "}
            <span className="data">{"<kind>/<id>"}</span>. A comma means any of
          </>
        ) : (
          <>
            Press Enter to apply. A comma means any of
            {canPrefix(field) && (
              <>
                . <span className="data">geo*</span> means starts with
              </>
            )}
          </>
        )}
      </span>
    </div>
  )
}

function ActiveFilterControl({
  filter,
  field,
  words,
  target,
  kinds,
  label,
  onChange,
  onRemove,
}: {
  filter: ActiveFilter
  field: DeclaredProperty | undefined
  words: boolean
  target?: KindInfo
  kinds?: KindInfo[]
  label: string
  onChange: (next: ActiveFilter) => void
  onRemove: () => void
}) {
  const [open, setOpen] = useState(false)
  return (
    <Popover open={open} onOpenChange={setOpen}>
      {/* Rule 3 anatomy `field | value | ×` as ONE outline-styled control,
          but the × is a REAL sibling button: nested inside the trigger it
          sat under the Button's [&_svg]:pointer-events-none and could never
          be clicked (owner redline, 2026-08-06). */}
      <div className="flex h-7 items-stretch overflow-hidden rounded-md border border-border-strong bg-background text-[12.5px]">
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="h-full gap-2 rounded-none px-2 text-[12.5px] font-normal"
            />
          }
        >
          <span className="text-muted-foreground">{label}</span>
          {/* an explicit rule, not Separator: the field | value seam must be
              visible inside the control (codex finding, 2026-08-05) */}
          <span aria-hidden className="h-4 w-px shrink-0 bg-border" />
          {/* max-w-72 fits a full group identity (the longest common value,
              e.g. providers.substrate.reamde.dev/github) before truncating; the title
              carries the whole value regardless (sweep finding, 2026-08-06) */}
          {target && kinds ? (
            <ReferenceFilterLabel
              target={target}
              kinds={kinds}
              ids={splitReferenceIds(filter.value)}
            />
          ) : (
            <span
              className="max-w-72 truncate"
              title={filterValueText(filter, field, words)}
            >
              {filterValueText(filter, field, words)}
            </span>
          )}
        </PopoverTrigger>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Remove ${label} filter`}
          className="h-full w-7 rounded-none border-l border-border px-0 text-faint hover:text-foreground"
          onClick={onRemove}
        >
          <XIcon className="size-3.5" />
        </Button>
      </div>
      <PopoverContent
        align="start"
        className={cn(
          isChoiceField(field) ? "p-0" : "p-1",
          target ? "w-80" : "w-56"
        )}
      >
        {field ? (
          <ValueEditor
            field={field}
            label={label}
            value={displayValue(filter, field)}
            words={words}
            target={target}
            kinds={kinds}
            onApply={(value) => {
              if (!value) onRemove()
              // A picked reference re-asserts its op: a filter stored before
              // pointers took `eq` may still say `contains`, which does not
              // split a comma.
              else
                onChange({
                  ...filter,
                  op: target ? opFor(field) : filter.op,
                  value,
                })
            }}
            onCommit={(value) => {
              onChange({
                field: filter.field,
                ...parseValueInput(value, field),
              })
              setOpen(false)
            }}
          />
        ) : null}
      </PopoverContent>
    </Popover>
  )
}

export function DataTableFilters({
  fields,
  filters,
  onChange,
  kinds,
  labelOf = (name) => name,
  words = false,
  className,
}: DataTableFiltersProps) {
  const [technical] = useTechnicalDetails()
  const [addOpen, setAddOpen] = useState(false)
  const [pending, setPending] = useState<DeclaredProperty | null>(null)
  const pendingTarget = referenceTarget(pending ?? undefined, kinds)

  function closeAdd() {
    setAddOpen(false)
    setPending(null)
  }

  function upsert(next: ActiveFilter, at?: number) {
    if (at !== undefined) {
      onChange(filters.map((f, i) => (i === at ? next : f)))
    } else {
      const existing = filters.findIndex((f) => f.field === next.field)
      if (existing >= 0) {
        onChange(filters.map((f, i) => (i === existing ? next : f)))
      } else {
        onChange([...filters, next])
      }
    }
  }

  return (
    <div
      className={cn(
        "flex shrink-0 flex-wrap items-center gap-2 px-6 py-2.5",
        className
      )}
    >
      {filters.map((filter, i) => {
        const field = fields.find((f) => f.name === filter.field)
        return (
          <ActiveFilterControl
            key={`${filter.field}-${i}`}
            filter={filter}
            field={field}
            words={words}
            target={referenceTarget(field, kinds)}
            kinds={kinds}
            label={labelOf(filter.field)}
            onChange={(next) => upsert(next, i)}
            onRemove={() => onChange(filters.filter((_, j) => j !== i))}
          />
        )
      })}
      <Popover
        open={addOpen}
        onOpenChange={(open) => {
          setAddOpen(open)
          if (!open) setPending(null)
        }}
      >
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-1.5 text-[12.5px] font-normal text-muted-foreground"
            />
          }
        >
          <ListFilterIcon className="size-3.5" />
          Add filter
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className={cn(
            pending && isChoiceField(pending) ? "p-0" : "p-1",
            pendingTarget || technical ? "w-80" : "w-64"
          )}
        >
          {pending ? (
            <ValueEditor
              field={pending}
              label={labelOf(pending.name)}
              value={filters.find((f) => f.field === pending.name)?.value ?? ""}
              words={words}
              target={pendingTarget}
              kinds={kinds}
              onApply={(value) => {
                if (!value) {
                  onChange(filters.filter((f) => f.field !== pending.name))
                } else {
                  upsert({ field: pending.name, op: opFor(pending), value })
                }
              }}
              onCommit={(value) => {
                upsert({
                  field: pending.name,
                  ...parseValueInput(value, pending),
                })
                closeAdd()
              }}
            />
          ) : (
            <Command>
              <CommandInput placeholder="Filter by…" />
              <CommandList>
                <CommandEmpty>No property can be filtered here.</CommandEmpty>
                <CommandGroup>
                  {fields.map((field) => {
                    const Icon = propertyIcon(field)
                    const label = labelOf(field.name)
                    return (
                      // [&>svg:last-child]:hidden drops CommandItem's built-in
                      // trailing check slot: its reserved width shoved the
                      // text off the right edge (owner redline, 2026-08-06).
                      // The label and the key are both searched.
                      <CommandItem
                        key={field.name}
                        value={`${label} ${field.name}`}
                        onSelect={() => setPending(field)}
                        className="items-start [&>svg:last-child]:hidden"
                      >
                        <Icon
                          aria-hidden
                          className="mt-0.5 size-3.5 shrink-0 text-muted-foreground"
                        />
                        <span className="flex min-w-0 flex-col">
                          <span className="truncate">{label}</span>
                          {technical && (
                            <span
                              className="truncate font-mono text-[11.5px] text-muted-foreground"
                              title={propertyTypeLabel(field)}
                            >
                              {propertyTypeLabel(field)}
                            </span>
                          )}
                        </span>
                      </CommandItem>
                    )
                  })}
                </CommandGroup>
              </CommandList>
            </Command>
          )}
        </PopoverContent>
      </Popover>
      {filters.length > 0 && (
        <Button
          variant="ghost"
          size="sm"
          className="h-7 text-[12.5px] font-normal text-muted-foreground"
          onClick={() => onChange([])}
        >
          Clear all
        </Button>
      )}
    </div>
  )
}
