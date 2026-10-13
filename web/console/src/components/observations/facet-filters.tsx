/** Filters over a fixed set of facets whose values are known up front (the
 * metrics, sources and things the streams name), in the collection toolbar's
 * anatomy: each active filter one `facet | values | ×` control that reopens
 * its choices, a dashed "Add filter" that offers the facets, and "Clear all".
 * Several values of one facet are an either-or; an `ordered` facet's values
 * also rank, in the order they were picked, and its filter moves them up or
 * down. */

import { useState } from "react"
import { ArrowDownIcon, ArrowUpIcon, ListFilterIcon, XIcon } from "lucide-react"

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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { cn } from "@/lib/utils"

export interface Facet {
  /** The URL key and the filter's name. */
  name: string
  /** The facet in words ("Source"). */
  label: string
  options: ChoiceOption[]
  /** Its values rank in the order they were picked, a new one last, and
   * its filter lists them in that order to move up or down. */
  ordered?: boolean
  /** What the rank means, over that list ("Where sources overlap, the
   * first is used first."). */
  orderHint?: string
}

/** An ordered facet's chosen values in rank order, each with a move up and
 * a move down, so a reader reorders them without unpicking any. */
function RankList({
  facet,
  values,
  onChange,
}: {
  facet: Facet
  values: string[]
  onChange: (values: string[]) => void
}) {
  const labelOf = (v: string) =>
    facet.options.find((o) => o.value === v)?.label ?? v
  function move(at: number, by: -1 | 1) {
    const next = [...values]
    const [value] = next.splice(at, 1)
    next.splice(at + by, 0, value)
    onChange(next)
  }
  return (
    <div className="border-b border-border p-1">
      {facet.orderHint && (
        <p className="px-2 pt-1 pb-1.5 text-xs text-faint">{facet.orderHint}</p>
      )}
      <ol aria-label={`${facet.label} order`}>
        {values.map((value, i) => {
          const label = labelOf(value)
          return (
            <li
              key={value}
              className="flex min-h-[30px] items-center gap-2 px-2 text-[13px]"
            >
              <span className="w-3 shrink-0 text-faint tabular-nums">
                {i + 1}
              </span>
              <span className="min-w-0 flex-1 truncate">{label}</span>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={`Move ${label} up`}
                disabled={i === 0}
                onClick={() => move(i, -1)}
              >
                <ArrowUpIcon />
              </Button>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={`Move ${label} down`}
                disabled={i === values.length - 1}
                onClick={() => move(i, 1)}
              >
                <ArrowDownIcon />
              </Button>
            </li>
          )
        })}
      </ol>
    </div>
  )
}

function summary(facet: Facet, values: readonly string[]): string {
  const labels = values.map(
    (v) => facet.options.find((o) => o.value === v)?.label ?? v
  )
  if (labels.length <= 1) return labels[0] ?? ""
  return `${labels[0]} and ${labels.length - 1} more`
}

function ActiveFacet({
  facet,
  values,
  onChange,
}: {
  facet: Facet
  values: string[]
  onChange: (values: string[]) => void
}) {
  const [open, setOpen] = useState(false)
  const text = summary(facet, values)
  return (
    <Popover open={open} onOpenChange={setOpen}>
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
          <span className="text-muted-foreground">{facet.label}</span>
          <span aria-hidden className="h-4 w-px shrink-0 bg-border" />
          <span className="max-w-72 truncate" title={text}>
            {text}
          </span>
        </PopoverTrigger>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Remove ${facet.label} filter`}
          className="h-full w-7 rounded-none border-l border-border px-0 text-faint hover:text-foreground"
          onClick={() => onChange([])}
        >
          <XIcon className="size-3.5" />
        </Button>
      </div>
      <PopoverContent align="start" className="w-72 p-0">
        {facet.ordered && values.length > 1 && (
          <RankList facet={facet} values={values} onChange={onChange} />
        )}
        <ChoiceList
          multiple
          ordered={facet.ordered}
          label={facet.label}
          options={facet.options}
          selected={values}
          onChange={onChange}
        />
      </PopoverContent>
    </Popover>
  )
}

/** "Add filter": the facets with something to offer, then the one picked's
 * values. Mounted only while a facet is offered, so the menu's state goes
 * with it: a filter removed later never reopens a menu nobody opened. */
function AddFilter({
  facets,
  offered,
  selected,
  onChange,
}: {
  /** Every facet, so a picked one's list stays open once it is chosen. */
  facets: Facet[]
  /** The facets the menu lists: none chosen yet, each with values. */
  offered: Facet[]
  selected: Record<string, string[]>
  onChange: (name: string, values: string[]) => void
}) {
  const [open, setOpen] = useState(false)
  // By name, so the list reads the facet's current values.
  const [pendingName, setPending] = useState<string | null>(null)
  const pending = facets.find((f) => f.name === pendingName) ?? null
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setPending(null)
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
        className={cn(pending ? "w-72 p-0" : "w-56 p-1")}
      >
        {pending ? (
          <ChoiceList
            multiple
            ordered={pending.ordered}
            label={pending.label}
            options={pending.options}
            selected={selected[pending.name] ?? []}
            onChange={(values) => onChange(pending.name, values)}
          />
        ) : (
          <Command>
            <CommandInput placeholder="Filter by…" />
            <CommandList>
              <CommandEmpty>Nothing to filter by.</CommandEmpty>
              <CommandGroup>
                {offered.map((facet) => (
                  <CommandItem
                    key={facet.name}
                    value={facet.label}
                    onSelect={() => setPending(facet.name)}
                    className="[&>svg:last-child]:hidden"
                  >
                    {facet.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        )}
      </PopoverContent>
    </Popover>
  )
}

export function FacetFilters({
  facets,
  selected,
  onChange,
  className,
}: {
  facets: Facet[]
  /** The chosen values, by facet name. */
  selected: Record<string, string[]>
  onChange: (name: string, values: string[]) => void
  className?: string
}) {
  const active = facets.filter((f) => selected[f.name]?.length)
  const offered = facets.filter(
    (f) => !selected[f.name]?.length && f.options.length > 0
  )

  return (
    <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
      {active.map((facet) => (
        <ActiveFacet
          key={facet.name}
          facet={facet}
          values={selected[facet.name] ?? []}
          onChange={(values) => onChange(facet.name, values)}
        />
      ))}
      {offered.length > 0 && (
        <AddFilter
          facets={facets}
          offered={offered}
          selected={selected}
          onChange={onChange}
        />
      )}
      {active.length > 0 && (
        <Button
          variant="ghost"
          size="sm"
          className="h-7 text-[12.5px] font-normal text-muted-foreground"
          onClick={() => {
            for (const facet of active) onChange(facet.name, [])
          }}
        >
          Clear all
        </Button>
      )}
    </div>
  )
}
