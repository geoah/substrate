/** The facet chips under a view's header: one group per declared facet, a
 * label and its values, in one horizontally scrolling row so a phone shows
 * the first group and a swipe finds the rest. A pressed chip is filled; a
 * group holds one value until a second is pressed, when the read turns from
 * `eq` to `in`; a repeated property holds one at a time. Clear leads the
 * row while anything is pressed. The selection is the URL's
 * (`useFacetSelection`), which the screen also reads into the context, so
 * pressing a chip and the rows narrowing are one state. */

import {
  Fragment,
  useEffect,
  useRef,
  useMemo,
  useState,
  useSyncExternalStore,
} from "react"
import { XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { specOf } from "@/lib/apps/cond"
import {
  facetGroups,
  mergeSeen,
  useFacetSelection,
  type ReferentMemory,
} from "@/lib/apps/facets"
import { useReferentTitles } from "@/lib/apps/referents"
import type { ViewSpec } from "@/lib/apps/spec"
import { cn } from "@/lib/utils"

/** The referents this bar has seen, kept outside React state: a narrowed
 * page names fewer referents than the whole did, and a chip must not vanish
 * under the finger that pressed it. Written after each commit, read as a
 * snapshot, and merged with the fresh rows in render so nothing lags. */
function referentMemory() {
  let seen: ReferentMemory = new Map()
  const listeners = new Set<() => void>()
  return {
    subscribe(listener: () => void) {
      listeners.add(listener)
      return () => void listeners.delete(listener)
    },
    snapshot: () => seen,
    remember(next: ReferentMemory) {
      if (next === seen) return
      seen = next
      for (const listener of listeners) listener()
    },
  }
}

function Chip({
  pressed,
  onClick,
  children,
  className,
}: {
  pressed?: boolean
  onClick: () => void
  children: React.ReactNode
  className?: string
}) {
  // A chip pressed before this paint (a shared URL, a reload) may sit past
  // the row's right edge; bring the first such chip into view once, so the
  // narrowing the URL carries is visible without a swipe.
  const ref = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!pressed) return
    const el = ref.current
    const row = el?.parentElement
    if (!el || !row) return
    if (el.offsetLeft + el.offsetWidth > row.scrollLeft + row.clientWidth) {
      row.scrollTo({ left: el.offsetLeft - 16 })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- first paint only
  }, [])
  return (
    <Button
      ref={ref}
      type="button"
      variant={pressed ? "default" : "outline"}
      size="sm"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "h-8 shrink-0 rounded-full px-3 text-[0.8rem] select-none",
        className
      )}
    >
      {children}
    </Button>
  )
}

export function FacetBar({
  spec,
  kind,
  kinds,
  records,
  className,
}: {
  spec: ViewSpec
  kind?: KindInfo
  kinds: KindInfo[]
  /** The rows as loaded, for the referents a reference facet offers. */
  records: SubstrateRecord[]
  className?: string
}) {
  const { selection, toggle, clear, any } = useFacetSelection(spec)
  const referenceNames = useMemo(
    () =>
      spec.facets.filter((name) => {
        const prop = specOf(kind, name)
        return !prop || prop.kind === "reference"
      }),
    [spec.facets, kind]
  )
  const titles = useReferentTitles(records, referenceNames, kinds)

  const [memory] = useState(referentMemory)
  const remembered = useSyncExternalStore(memory.subscribe, memory.snapshot)
  const seen = useMemo(
    () => mergeSeen(remembered, records, referenceNames, titles),
    [remembered, records, referenceNames, titles]
  )
  useEffect(() => memory.remember(seen), [memory, seen])

  const groups = useMemo(
    () => facetGroups(spec, kind, seen, selection),
    [spec, kind, seen, selection]
  )
  if (!groups.length && !any) return null

  return (
    <div
      role="group"
      aria-label="Narrow"
      className={cn(
        "flex min-h-11 [scrollbar-width:none] items-center gap-2 overflow-x-auto border-b px-4 py-1.5 [&::-webkit-scrollbar]:hidden",
        className
      )}
    >
      {any && (
        <Chip onClick={clear} className="gap-1 pl-2">
          <XIcon className="size-3.5" />
          Clear
        </Chip>
      )}
      {groups.map((group, i) => {
        const picked = selection[group.property] ?? []
        return (
          <Fragment key={group.property}>
            {(i > 0 || any) && (
              <span aria-hidden className="h-5 w-px shrink-0 bg-border" />
            )}
            <span className="shrink-0 text-xs text-muted-foreground">
              {group.label}
            </span>
            {group.chips.map((chip) => (
              <Chip
                key={chip.value}
                pressed={picked.includes(chip.value)}
                onClick={() =>
                  toggle(
                    group.property,
                    chip.value,
                    group.single ? "only" : "toggle"
                  )
                }
              >
                {chip.label}
              </Chip>
            ))}
          </Fragment>
        )
      })}
    </div>
  )
}
