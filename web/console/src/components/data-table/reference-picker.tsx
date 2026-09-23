/** The reference filter's value editor: the referent collection as a list to
 * PICK from, several at once.
 *
 * A stored reference is a record path, and the filter bar used to ask for it
 * as text: nothing on the page said whether the id, the title or the whole
 * path was wanted (owner report, 2026-09-23). A pointer names a record or it
 * does not, so its editor is the collection itself: every record as a row
 * with the title a reader recognises and the id the filter carries, a search
 * on top, and a checkbox per row, because several referents on one property
 * are one `in` filter, "any of". That is the wire's only several-values form
 * on one property (query.go condReference reads eq, contains and in on a
 * pointer alike), so there is no "all of" to offer.
 *
 * The rows are one page of the collection (`useRecordOptions`), the whole
 * collection for the registry-shaped kinds a pin usually names. A collection
 * that outran the page is searched server-side as the reader types, so a
 * person past the first two hundred is still a few keystrokes away, and a
 * chosen record the page does not carry keeps its row and its title through
 * the same batched read by id every pill uses. */

import { useEffect, useState } from "react"
import { CheckIcon } from "lucide-react"

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Spinner } from "@/components/ui/spinner"
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import type { KindInfo } from "@/lib/api/types"
import {
  useRecordOptions,
  useRecordSearch,
  type RecordOption,
} from "@/lib/identities"
import { recordPath } from "@/lib/record-path"
import { cn } from "@/lib/utils"

/** How long typing settles before the server is asked. Each keystroke into a
 * capped collection is otherwise its own list read. */
const SEARCH_DEBOUNCE_MS = 200

function useDebounced(value: string, ms: number): string {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), ms)
    return () => clearTimeout(timer)
  }, [value, ms])
  return settled
}

/** The titles of the chosen records, keyed by id, through the batched read
 * every surface resolves a pointer with. A record with no title of its own is
 * absent, and reads as its id. */
function useChosenTitles(
  target: KindInfo,
  kinds: KindInfo[],
  ids: readonly string[]
): ReadonlyMap<string, string> {
  const titles = useReferenceTitles(
    ids.map((id) => recordPath(target.identity, id)),
    kinds
  )
  const out = new Map<string, string>()
  for (const id of ids) {
    const title = titles.get(recordPath(target.identity, id))
    if (title) out.set(id, title)
  }
  return out
}

/** What a reference filter's control reads: the chosen records by title, in
 * the order they were chosen, an id standing in (in the data face) for a
 * record whose title is not known. */
export function ReferenceFilterLabel({
  target,
  kinds,
  ids,
}: {
  target: KindInfo
  kinds: KindInfo[]
  ids: string[]
}) {
  const titles = useChosenTitles(target, kinds, ids)
  const text = ids.map((id) => titles.get(id) ?? id).join(", ")
  return (
    <span className="max-w-72 truncate" title={text}>
      {ids.map((id, i) => {
        const title = titles.get(id)
        return (
          <span key={id}>
            {i > 0 && ", "}
            <span className={cn(!title && "data")}>{title ?? id}</span>
          </span>
        )
      })}
    </span>
  )
}

export function ReferencePicker({
  target,
  kinds,
  selected,
  onChange,
}: {
  /** The kind the reference is pinned to, resolved against the registry. */
  target: KindInfo
  kinds: KindInfo[]
  /** The ids chosen so far, in the order they were chosen. */
  selected: string[]
  onChange: (ids: string[]) => void
}) {
  const [query, setQuery] = useState("")
  const typed = useDebounced(query.trim(), SEARCH_DEBOUNCE_MS)
  const page = useRecordOptions(target.identity, kinds)
  // The page is the whole collection unless it was capped; only then does
  // typing ask the server, and only then is cmdk's own filtering stood down,
  // so a stemmed or prefixed server match is not hidden for lacking the
  // literal letters.
  const searching = page.capped && typed.length > 0
  const found = useRecordSearch(target.identity, kinds, typed, searching)
  const titles = useChosenTitles(target, kinds, selected)
  const offered = searching ? found : page

  const chosen = new Set(selected)
  const byId = new Map(offered.options.map((o) => [o.value, o]))
  // The chosen rows lead, whatever the list below them shows: unchecking one
  // must stay one click away while a search shows something else.
  const rows: RecordOption[] = [
    ...selected.map(
      (id) =>
        byId.get(id) ?? {
          value: id,
          title: titles.get(id) ?? "",
          description: "",
        }
    ),
    ...offered.options.filter((o) => !chosen.has(o.value)),
  ]

  function toggle(id: string) {
    onChange(
      chosen.has(id) ? selected.filter((s) => s !== id) : [...selected, id]
    )
  }

  return (
    <Command shouldFilter={!searching}>
      <CommandInput
        placeholder={`Search ${target.name}…`}
        value={query}
        onValueChange={setQuery}
      />
      <CommandList>
        {offered.loading ? (
          <div className="flex items-center gap-2 px-3 py-6 text-sm text-muted-foreground">
            <Spinner className="size-3.5" />
            Reading the collection
          </div>
        ) : offered.error ? (
          <div className="px-3 py-6 text-sm text-destructive">
            {offered.error}
          </div>
        ) : (
          <CommandEmpty className="text-muted-foreground">
            {page.options.length || searching
              ? "Nothing matches."
              : `There are no ${target.name} records yet.`}
          </CommandEmpty>
        )}
        {rows.length > 0 && (
          <CommandGroup>
            {rows.map((row) => {
              const on = chosen.has(row.value)
              return (
                // The title and the id are both searched, so either finds the
                // row. [&>svg:last-child]:hidden drops CommandItem's built-in
                // trailing check slot: the checkbox on the left is the state.
                <CommandItem
                  key={row.value}
                  value={`${row.value} ${row.title}`}
                  onSelect={() => toggle(row.value)}
                  className="[&>svg:last-child]:hidden"
                >
                  <span
                    className={cn(
                      "flex size-4 shrink-0 items-center justify-center rounded-sm border",
                      on
                        ? "border-primary bg-primary text-primary-foreground"
                        : "opacity-50"
                    )}
                  >
                    {on && <CheckIcon className="size-3" />}
                  </span>
                  <span className="flex min-w-0 flex-col">
                    <span className={cn("truncate", !row.title && "data")}>
                      {row.title || row.value}
                    </span>
                    {row.title && (
                      <span className="truncate data text-xs text-muted-foreground">
                        {row.value}
                      </span>
                    )}
                  </span>
                </CommandItem>
              )
            })}
          </CommandGroup>
        )}
      </CommandList>
      {page.capped && !searching && (
        <p className="border-t px-3 py-2 text-xs text-muted-foreground">
          Showing the first {page.options.length}. Type to search the rest.
        </p>
      )}
    </Command>
  )
}
