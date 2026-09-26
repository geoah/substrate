/** The reference filter's value editor: the referent collection as a list to
 * PICK from, several at once.
 *
 * A stored reference is a record path, and the filter bar used to ask for it
 * as text: nothing on the page said whether the id, the title or the whole
 * path was wanted (owner report, 2026-09-23). A pointer names a record or it
 * does not, so its editor is the collection itself: every record as a row
 * marked the way a record is everywhere (the kind's glyph and the title; the
 * id the filter carries only with technical details on), a search on top,
 * and a check per row, because several referents on one property are one
 * `in` filter, "any of". That is the wire's only several-values form
 * on one property (query.go condReference reads eq, contains and in on a
 * pointer alike), so there is no "all of" to offer.
 *
 * The rows are one page of the collection (`useRecordOptions`), the whole
 * collection for the registry-shaped kinds a pin usually names. Typing
 * filters that page at once and asks the server's search as well, so a person
 * past the first two hundred is still a few keystrokes away (`pickerRows`
 * merges the two), and a chosen record the page does not carry keeps its row
 * and its title through the same batched read by id every pill uses. */

import { useEffect, useState } from "react"
import { CheckIcon } from "lucide-react"

import { KindGlyph } from "@/components/identity/kind-glyph"
import {
  Command,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import type { KindInfo } from "@/lib/api/types"
import { useRecordOptions, useRecordSearch } from "@/lib/identities"
import { displayPlural, lowerFirst, untitled } from "@/lib/kind-names"
import { recordPath } from "@/lib/record-path"
import { pickerRows } from "./picker-rows"
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
  const [technical] = useTechnicalDetails()
  const titles = useChosenTitles(target, kinds, ids)
  // A record whose title is not known reads by its id only where ids are
  // shown at all.
  const fallback = (id: string) => (technical ? id : untitled(target))
  const text = ids.map((id) => titles.get(id) ?? fallback(id)).join(", ")
  return (
    <span className="max-w-72 truncate" title={text}>
      {ids.map((id, i) => {
        const title = titles.get(id)
        return (
          <span key={id}>
            {i > 0 && ", "}
            <span
              className={cn(
                !title && (technical ? "data" : "text-muted-foreground")
              )}
            >
              {title ?? fallback(id)}
            </span>
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
  const [technical] = useTechnicalDetails()
  const [query, setQuery] = useState("")
  const typed = useDebounced(query.trim(), SEARCH_DEBOUNCE_MS)
  const page = useRecordOptions(target.identity, kinds)
  // Typing always asks the server too, so a record past the first page is a
  // few keystrokes away; the page in hand answers at once meanwhile, and the
  // list is filtered here rather than by cmdk, which would hide a stemmed or
  // prefixed server match for lacking the literal letters.
  const searching = typed.length > 0
  const found = useRecordSearch(target.identity, kinds, typed, searching)
  const titles = useChosenTitles(target, kinds, selected)
  const chosen = new Set(selected)
  const rows = pickerRows(
    selected,
    titles,
    page.options,
    searching ? found.options : [],
    query
  )
  const plural = lowerFirst(displayPlural(target))
  // A refused search is said on its own line: the page's own matches still
  // stand, and an empty list is not "Nothing matches" when the server was
  // never heard from.
  const searchError = searching && !found.loading ? found.error : undefined

  function toggle(id: string) {
    onChange(
      chosen.has(id) ? selected.filter((s) => s !== id) : [...selected, id]
    )
  }

  return (
    <Command shouldFilter={false}>
      <CommandInput
        placeholder={`Search ${plural}…`}
        value={query}
        onValueChange={setQuery}
      />
      <CommandList>
        {page.loading ? (
          <div className="flex items-center gap-2 px-3 py-6 text-sm text-muted-foreground">
            <Spinner className="size-3.5" />
            Loading {plural}
          </div>
        ) : page.error ? (
          <div className="px-3 py-6 text-sm text-destructive">{page.error}</div>
        ) : rows.length === 0 && !searchError ? (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">
            {searching && found.loading ? (
              <span className="inline-flex items-center gap-2">
                <Spinner className="size-3.5" />
                Searching
              </span>
            ) : page.options.length || searching ? (
              "Nothing matches."
            ) : (
              `No ${plural} yet.`
            )}
          </div>
        ) : null}
        {rows.length > 0 && (
          <CommandGroup>
            {rows.map((row) => {
              const on = chosen.has(row.value)
              return (
                // The row reads as a record mark: the kind's glyph and the
                // title, the id under it only with technical details on. The
                // check on the right is the state; [&>svg:last-child]:hidden
                // drops CommandItem's own trailing slot, which never shows
                // for a multi-select.
                <CommandItem
                  key={row.value}
                  value={row.value}
                  onSelect={() => toggle(row.value)}
                  data-checked={on || undefined}
                  className="gap-2 [&>svg:last-child]:hidden"
                >
                  <KindGlyph kind={target} size="xs" />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span
                      className={cn(
                        "truncate",
                        !row.title && "text-muted-foreground"
                      )}
                    >
                      {row.title || untitled(target)}
                    </span>
                    {technical && (
                      <span className="truncate font-mono text-[11.5px] text-muted-foreground">
                        {row.value}
                      </span>
                    )}
                  </span>
                  <span
                    aria-hidden
                    data-slot="picker-check"
                    className={cn(
                      "flex size-4 shrink-0 items-center justify-center rounded-[4px] border",
                      on
                        ? "border-primary bg-primary text-primary-foreground"
                        : "border-border-strong bg-background"
                    )}
                  >
                    {on && <CheckIcon className="size-3" strokeWidth={3} />}
                  </span>
                  {on && <span className="sr-only">(chosen)</span>}
                </CommandItem>
              )
            })}
          </CommandGroup>
        )}
        {searchError && (
          <div
            role="alert"
            className={cn(
              "px-3 text-xs text-destructive",
              rows.length > 0 ? "border-t py-2" : "py-6 text-center"
            )}
          >
            The search didn’t finish: {searchError}{" "}
            <button type="button" className="underline" onClick={found.retry}>
              Try again
            </button>
          </div>
        )}
        {searching && found.loading && rows.length > 0 && (
          <div className="flex items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground">
            <Spinner className="size-3" />
            Searching all {plural}
          </div>
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
