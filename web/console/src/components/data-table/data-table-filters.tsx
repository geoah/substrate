/** The filter toolbar (rule 3: full-size controls, never chips). Each active
 * filter is an h-8 outline control with the `field | value | ×` anatomy;
 * clicking it reopens its value editor. The dashed "Add filter" opens a
 * Popover+Command faceted picker built ONLY from the declared/filterable
 * properties the server will actually filter.
 *
 * A reference is filtered by PICKING its referents: the bar is handed the
 * registry so a pin resolves to the collection to offer, and the control
 * reads the chosen records' titles. A bar handed none (the changelog's) keeps
 * the text box. */

import { useState } from "react"
import { CheckIcon, ListFilterIcon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Input } from "@/components/ui/input"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import type { KindInfo } from "@/lib/api/types"
import {
  canMatch,
  canPrefix,
  displayValue,
  opFor,
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

/** The value step, shaped by the declared kind: states and booleans facet
 * (toggle membership, applied live), a pinned reference offers its referents
 * the same way; everything else takes text on Enter. */
function ValueEditor({
  field,
  value,
  target,
  kinds,
  onApply,
  onCommit,
}: {
  field: DeclaredProperty
  value: string
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

  if (field.kind === "state" && field.states?.length) {
    const selected = new Set(
      value
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
    )
    return (
      <Command>
        {/* A short machine reads at a glance; a long facet (the changelog's
            type list) earns the search line. */}
        {field.states.length > 8 && (
          <CommandInput placeholder={`Filter ${field.name}…`} />
        )}
        <CommandList>
          <CommandEmpty>No match.</CommandEmpty>
          <CommandGroup>
            {field.states.map((state) => {
              const on = selected.has(state)
              return (
                <CommandItem
                  key={state}
                  value={state}
                  onSelect={() => {
                    const next = new Set(selected)
                    if (on) next.delete(state)
                    else next.add(state)
                    onApply([...next].join(","))
                  }}
                >
                  <span
                    className={cn(
                      "flex size-4 items-center justify-center rounded-sm border",
                      on
                        ? "border-primary bg-primary text-primary-foreground"
                        : "opacity-50"
                    )}
                  >
                    {on && <CheckIcon className="size-3" />}
                  </span>
                  <span>{state}</span>
                </CommandItem>
              )
            })}
          </CommandGroup>
        </CommandList>
      </Command>
    )
  }

  if (field.kind === "bool") {
    return (
      <Command>
        <CommandList>
          <CommandGroup>
            {["true", "false"].map((v) => (
              <CommandItem key={v} value={v} onSelect={() => onCommit(v)}>
                <span>{v}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </Command>
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
            ? `${field.name} points at…`
            : matches
              ? `${field.name} mentions…`
              : field.repeated
                ? `${field.name} contains…`
                : `${field.name} is…`
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
  target,
  kinds,
  label,
  onChange,
  onRemove,
}: {
  filter: ActiveFilter
  field: DeclaredProperty | undefined
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
              title={displayValue(filter, field).replaceAll(",", ", ")}
            >
              {displayValue(filter, field).replaceAll(",", ", ")}
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
        className={cn("p-1", target ? "w-80" : "w-56")}
      >
        {field ? (
          <ValueEditor
            field={field}
            value={displayValue(filter, field)}
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
  className,
}: DataTableFiltersProps) {
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
          className={cn("p-1", pendingTarget ? "w-80" : "w-64")}
        >
          {pending ? (
            <ValueEditor
              field={pending}
              value={filters.find((f) => f.field === pending.name)?.value ?? ""}
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
                  {fields.map((field) => (
                    // [&>svg:last-child]:hidden drops CommandItem's built-in
                    // trailing check slot: its reserved width shoved the kind
                    // text off the right edge (owner redline, 2026-08-06).
                    <CommandItem
                      key={field.name}
                      value={field.name}
                      onSelect={() => setPending(field)}
                      className="[&>svg:last-child]:hidden"
                    >
                      <span>{labelOf(field.name)}</span>
                      <span className="ml-auto text-right text-xs text-muted-foreground">
                        {propertyTypeLabel(field)}
                      </span>
                    </CommandItem>
                  ))}
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
