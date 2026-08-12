/** The filter toolbar (rule 3: full-size controls, never chips). Each active
 * filter is an h-8 outline control with the `field | value | ×` anatomy;
 * clicking it reopens its value editor. The dashed "Add filter" opens a
 * Popover+Command faceted picker built ONLY from the declared/filterable
 * properties the server will actually filter. */

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
import {
  canPrefix,
  displayValue,
  opFor,
  parseValueInput,
  type ActiveFilter,
} from "@/lib/filters"
import type { DeclaredProperty } from "@/lib/definition"
import { cn } from "@/lib/utils"

interface DataTableFiltersProps {
  fields: DeclaredProperty[]
  filters: ActiveFilter[]
  onChange: (filters: ActiveFilter[]) => void
}

/** The value step, shaped by the declared kind: states and booleans facet
 * (toggle membership, applied live); everything else takes text on Enter. */
function ValueEditor({
  field,
  value,
  onApply,
  onCommit,
}: {
  field: DeclaredProperty
  value: string
  /** Live update (facets) — keeps the popover open. */
  onApply: (value: string) => void
  /** Final value (text entry) — closes the popover. */
  onCommit: (value: string) => void
}) {
  const [draft, setDraft] = useState(value)

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
                  <span className="data">{state}</span>
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
                <span className="data">{v}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </Command>
    )
  }

  return (
    <div className="flex flex-col gap-1.5 p-1">
      <Input
        autoFocus
        placeholder={
          field.repeated ? `${field.name} contains…` : `${field.name} is…`
        }
        className="h-8 data"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && draft.trim()) onCommit(draft.trim())
        }}
      />
      <span className="px-1 text-xs text-muted-foreground">
        Enter applies, comma = any of
        {canPrefix(field) && (
          <>
            , <span className="data">geo*</span> = starts with
          </>
        )}
      </span>
    </div>
  )
}

function ActiveFilterControl({
  filter,
  field,
  onChange,
  onRemove,
}: {
  filter: ActiveFilter
  field: DeclaredProperty | undefined
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
      <div className="flex h-8 items-stretch overflow-hidden rounded-lg border border-border bg-background bg-clip-padding dark:border-input dark:bg-input/30">
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="h-full gap-1.5 rounded-none pr-1.5 font-normal"
            />
          }
        >
          <span className="text-muted-foreground">{filter.field}</span>
          {/* an explicit rule, not Separator: the field | value seam must be
              visible inside the control (codex finding, 2026-08-05) */}
          <span aria-hidden className="h-4 w-px shrink-0 bg-border" />
          {/* max-w-72 fits a full group identity (the longest common value,
              e.g. github.connectors.substrate.reamde.dev) before truncating; the title
              carries the whole value regardless (sweep finding, 2026-08-06) */}
          <span
            className="max-w-72 truncate data"
            title={displayValue(filter).replaceAll(",", ", ")}
          >
            {displayValue(filter).replaceAll(",", ", ")}
          </span>
        </PopoverTrigger>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Remove ${filter.field} filter`}
          className="h-full w-6 rounded-none px-0 text-muted-foreground hover:text-foreground"
          onClick={onRemove}
        >
          <XIcon className="size-3.5" />
        </Button>
      </div>
      <PopoverContent align="start" className="w-56 p-1">
        {field ? (
          <ValueEditor
            field={field}
            value={displayValue(filter)}
            onApply={(value) => {
              if (!value) onRemove()
              else onChange({ ...filter, value })
            }}
            onCommit={(value) => {
              onChange({ field: filter.field, ...parseValueInput(value, field) })
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
}: DataTableFiltersProps) {
  const [addOpen, setAddOpen] = useState(false)
  const [pending, setPending] = useState<DeclaredProperty | null>(null)

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
    <div className="flex shrink-0 flex-wrap items-center gap-2 px-6 py-2.5">
      {filters.map((filter, i) => (
        <ActiveFilterControl
          key={`${filter.field}-${i}`}
          filter={filter}
          field={fields.find((f) => f.name === filter.field)}
          onChange={(next) => upsert(next, i)}
          onRemove={() => onChange(filters.filter((_, j) => j !== i))}
        />
      ))}
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
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 border-dashed font-normal text-muted-foreground"
            />
          }
        >
          <ListFilterIcon className="size-3.5" /> Add filter
        </PopoverTrigger>
        <PopoverContent align="start" className="w-64 p-1">
          {pending ? (
            <ValueEditor
              field={pending}
              value={filters.find((f) => f.field === pending.name)?.value ?? ""}
              onApply={(value) => {
                if (!value) {
                  onChange(filters.filter((f) => f.field !== pending.name))
                } else {
                  upsert({ field: pending.name, op: opFor(pending), value })
                }
              }}
              onCommit={(value) => {
                upsert({ field: pending.name, ...parseValueInput(value, pending) })
                closeAdd()
              }}
            />
          ) : (
            <Command>
              <CommandInput placeholder="Filter by…" />
              <CommandList>
                <CommandEmpty>No filterable property.</CommandEmpty>
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
                      <span>{field.name}</span>
                      <span className="ml-auto text-right text-xs text-muted-foreground">
                        {field.kind}
                        {field.repeated ? "[]" : ""}
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
          className="h-8 font-normal text-muted-foreground"
          onClick={() => onChange([])}
        >
          Clear all
        </Button>
      )}
    </div>
  )
}
