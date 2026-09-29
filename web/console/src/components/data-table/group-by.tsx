/** Grouping a collection's grid: the toolbar's Group by menu, and what a group
 * head says (the value, drawn the way the grid's cells draw it, and how many
 * records the group holds). `lib/grouping.ts` holds the rules. */

import { ChevronDownIcon, Rows3Icon } from "lucide-react"

import { EnumTag } from "@/components/identity/enum-tag"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { KindInfo } from "@/lib/api/types"
import { formatCount, type RecordCount } from "@/lib/api/records"
import { kindByIdentity, type DeclaredProperty } from "@/lib/definition"
import { lowerFirst } from "@/lib/kind-names"
import { splitRecordPath } from "@/lib/record-path"
import type { ReferenceTitles } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"

const NONE = "\u0000none"

export function GroupByMenu({
  options,
  value,
  labelOf,
  technical,
  onChange,
}: {
  /** The properties the collection may be grouped by. */
  options: DeclaredProperty[]
  /** The property grouped by now. */
  value?: string
  labelOf: (name: string) => string
  technical?: boolean
  onChange: (name: string | null) => void
}) {
  if (!options.length) return null
  const current = options.find((p) => p.name === value)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-pressed={Boolean(current)}
            className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-border-strong bg-background px-2.5 text-[12.5px] text-muted-foreground outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 aria-pressed:border-transparent aria-pressed:bg-primary-soft aria-pressed:text-primary-text"
          />
        }
      >
        <Rows3Icon aria-hidden className="size-3.5" />
        {current ? (
          <>
            Grouped by
            <span className={cn(technical && "font-mono text-[11.5px]")}>
              {technical ? current.name : lowerFirst(labelOf(current.name))}
            </span>
          </>
        ) : (
          "Group"
        )}
        <ChevronDownIcon aria-hidden className="size-3 opacity-70" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-96 min-w-52">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Group by</DropdownMenuLabel>
          <p className="max-w-56 px-1.5 pb-1.5 text-[11.5px] leading-snug text-faint">
            A page shows the groups of the records on it. Each count is the
            whole group.
          </p>
          <DropdownMenuRadioGroup
            value={current?.name ?? NONE}
            onValueChange={(next) =>
              onChange(next === NONE ? null : String(next))
            }
          >
            {options.map((p) => (
              <DropdownMenuRadioItem key={p.name} value={p.name}>
                {technical ? (
                  <span className="font-mono text-[12px]">{p.name}</span>
                ) : (
                  labelOf(p.name)
                )}
              </DropdownMenuRadioItem>
            ))}
            <DropdownMenuSeparator />
            <DropdownMenuRadioItem value={NONE}>
              No grouping
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** A group head's words: the value as the grid's cells draw it, then the
 * whole group's count once it is known. */
export function GroupHead({
  prop,
  groupKey,
  label,
  count,
  note,
  kinds,
  titles,
}: {
  prop: DeclaredProperty
  groupKey: string
  /** The property's label, for the empty group ("No project"). */
  label: string
  count?: RecordCount
  /** Where the group runs on from or on to ("continued from the previous
   * page"). */
  note?: string
  kinds: KindInfo[]
  titles?: ReferenceTitles
}) {
  return (
    <>
      <GroupValue
        prop={prop}
        groupKey={groupKey}
        label={label}
        kinds={kinds}
        titles={titles}
      />
      {count && (
        <span className="text-[12.5px] font-normal text-faint tabular-nums">
          {formatCount(count)}
        </span>
      )}
      {note && (
        <span className="text-[12px] font-normal text-faint">{note}</span>
      )}
    </>
  )
}

function GroupValue({
  prop,
  groupKey,
  label,
  kinds,
  titles,
}: {
  prop: DeclaredProperty
  groupKey: string
  label: string
  kinds: KindInfo[]
  titles?: ReferenceTitles
}) {
  if (!groupKey) {
    return (
      <span className="font-normal text-muted-foreground">
        No {lowerFirst(label)}
      </span>
    )
  }
  if (prop.kind === "enum") return <EnumTag prop={prop} value={groupKey} />
  if (prop.kind === "state") {
    return <StateBadge value={groupKey} initial={prop.initial} />
  }
  const target = splitRecordPath(groupKey)
  if (!target) return <span className="font-mono text-[12px]">{groupKey}</span>
  return (
    <RecordRef
      kind={target.kind}
      id={target.id}
      title={titles?.get(groupKey)}
      link={Boolean(kindByIdentity(kinds, target.kind))}
    />
  )
}
