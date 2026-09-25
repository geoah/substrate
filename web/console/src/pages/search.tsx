/** /search: the ranked read (`GET /records?q=`) as a page. A query in the
 * search grammar, what to rank by (words, words + meaning, meaning; the
 * reader's choice, and it sticks), an optional collection to narrow the
 * candidates, and the hits in rank order. Technical mode adds each hit's raw
 * per-arm scores and full reference. The query, the arm and the collection
 * live in the URL, so a search is shareable and the back button returns to
 * it. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { CheckIcon, ChevronsUpDownIcon } from "lucide-react"
import { parseAsString, parseAsStringLiteral, useQueryState } from "nuqs"

import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath, KindRef } from "@/components/identity/kind-ref"
import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { RecordRef } from "@/components/identity/record-ref"
import { SearchBox } from "@/components/search-box"
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
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { searchQueryOptions } from "@/lib/api/records"
import type { KindInfo, Scores, SubstrateRecord } from "@/lib/api/types"
import { collectionGroups } from "@/lib/collections"
import { recordTitle } from "@/lib/format"
import { displayPlural } from "@/lib/kind-names"
import {
  SEARCH_GRAMMAR,
  SEARCH_MODE_DESCRIPTION,
  SEARCH_MODE_LABEL,
  SEARCH_MODES,
  loadSearchMode,
  saveSearchMode,
  type SearchMode,
} from "@/lib/search"
import { cn } from "@/lib/utils"

/** How many hits one search asks for. A ranking has no next page. */
const HITS = 50

export function SearchPage() {
  const [technical] = useTechnicalDetails()
  const [q, setQ] = useQueryState("q", parseAsString.withDefault(""))
  // The URL names the arm when it says; otherwise the stored preference.
  const [modeParam, setModeParam] = useQueryState(
    "mode",
    parseAsStringLiteral(SEARCH_MODES)
  )
  const [kind, setKind] = useQueryState("kind", parseAsString.withDefault(""))
  const mode: SearchMode = modeParam ?? loadSearchMode()

  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(() => registry.data ?? [], [registry.data])
  const narrowed = kinds.find((k) => k.identity === kind)

  const words = q.trim()
  const results = useQuery(
    searchQueryOptions(words, {
      mode,
      kinds: narrowed ? [narrowed.identity] : undefined,
      first: HITS,
    })
  )

  return (
    <DocPage className="pb-20">
      <PageHeader
        title="Search"
        description="Every record you keep, ranked against your words."
      />

      <div className="mt-5 flex flex-col gap-3 border-b border-border pb-4">
        <div className="flex flex-wrap items-center gap-2">
          <SearchBox
            className="w-full max-w-xl"
            label="Search your records"
            placeholder="Search your records…"
            autoFocus
            value={q}
            onChange={(next) => void setQ(next || null)}
          />
          <KindPicker
            kinds={kinds}
            value={narrowed}
            onChange={(next) => void setKind(next?.identity ?? null)}
          />
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
          <div
            role="radiogroup"
            aria-label="Rank by"
            className="inline-flex gap-0.5 rounded-[7px] border border-border-strong p-0.5"
          >
            {SEARCH_MODES.map((m) => (
              <button
                key={m}
                type="button"
                role="radio"
                aria-checked={mode === m}
                onClick={() => {
                  saveSearchMode(m)
                  void setModeParam(m)
                }}
                className={cn(
                  "cursor-pointer rounded-[5px] px-2.5 py-1 text-[12.5px] text-muted-foreground",
                  mode === m && "bg-foreground text-background"
                )}
              >
                {SEARCH_MODE_LABEL[m]}
              </button>
            ))}
          </div>
          <p className="max-w-prose text-[12.5px] text-faint">
            {SEARCH_MODE_DESCRIPTION[mode]}
          </p>
        </div>
      </div>

      <div className="pt-4">
        {!words ? (
          <GrammarEmpty />
        ) : results.isError ? (
          <div className="flex flex-col items-start gap-2 py-8">
            <p className="font-medium">This search was refused</p>
            {/* the server's problem, verbatim: it names the arm, the
                provider row or the word that was missing */}
            <p className="text-muted-foreground">{results.error.message}</p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void results.refetch()}
            >
              Try again
            </Button>
          </div>
        ) : results.isPending ? (
          <ul className="flex flex-col" aria-busy>
            {Array.from({ length: 6 }, (_, i) => (
              <li key={i} className="border-b border-border py-3">
                <Skeleton className="h-4 w-2/5" />
                <Skeleton className="mt-2 h-3 w-1/5" />
              </li>
            ))}
          </ul>
        ) : results.data.records.length === 0 ? (
          <p className="py-8 text-muted-foreground">
            Nothing{" "}
            {narrowed ? `in ${displayPlural(narrowed).toLowerCase()} ` : ""}
            matches “{words}” by {SEARCH_MODE_LABEL[mode].toLowerCase()}.
          </p>
        ) : (
          <>
            <div className="mb-1 flex flex-wrap items-baseline justify-between gap-2 text-[12.5px] text-faint">
              <span>
                {results.data.records.length}{" "}
                {results.data.records.length === 1 ? "match" : "matches"}
                {results.data.records.length === HITS
                  ? " (the most one search shows)"
                  : ""}
                , best first
              </span>
              {results.data.pending > 0 && (
                <span>
                  {results.data.pending} values are still being read for
                  meaning, so meaning ranked part of your data.
                </span>
              )}
            </div>
            <ol className="flex flex-col">
              {results.data.records.map((record) => (
                <Hit
                  key={`${record.kind}/${record.id}`}
                  record={record}
                  scores={results.data.scores[`${record.kind}/${record.id}`]}
                  technical={technical}
                />
              ))}
            </ol>
          </>
        )}
      </div>
    </DocPage>
  )
}

/** One hit: the record, the collection it sits in, and in technical mode its
 * full reference and each arm's raw score under its own label. */
function Hit({
  record,
  scores,
  technical,
}: {
  record: SubstrateRecord
  scores: Scores | undefined
  technical: boolean
}) {
  const title = recordTitle(record.properties ?? {})
  return (
    <li className="flex flex-col gap-1 border-b border-border py-2.5">
      <div className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <RecordRef
          kind={record.kind}
          id={record.id}
          title={title || undefined}
          className="font-medium"
        />
        {technical && (
          <dl className="flex gap-4 text-xs text-faint">
            {scores?.lexical !== undefined && (
              <div className="flex gap-1">
                <dt>words</dt>
                <dd className="tabular-nums">{scores.lexical.toFixed(3)}</dd>
              </div>
            )}
            {scores?.semantic !== undefined && (
              <div className="flex gap-1">
                <dt>meaning</dt>
                <dd className="tabular-nums">{scores.semantic.toFixed(3)}</dd>
              </div>
            )}
          </dl>
        )}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 text-[12.5px] text-faint">
        <span>in</span>
        <KindRef kind={record.kind} className="text-muted-foreground" />
        {technical && (
          <span className="font-mono text-[11.5px] [overflow-wrap:anywhere]">
            {record.kind}/{record.id}
          </span>
        )}
      </div>
    </li>
  )
}

/** Narrow the candidates to one collection, or none: the ranked read's one
 * filter arm, `filter.kinds`. */
function KindPicker({
  kinds,
  value,
  onChange,
}: {
  kinds: KindInfo[]
  value: KindInfo | undefined
  onChange: (next: KindInfo | undefined) => void
}) {
  const [technical] = useTechnicalDetails()
  const groups = useMemo(() => collectionGroups(kinds), [kinds])
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-8 max-w-72 gap-1.5 font-normal"
            aria-label="Collections to search"
          />
        }
      >
        <span className="text-faint">in</span>
        {value && <KindGlyph kind={value} size="xs" />}
        <span className="truncate">
          {value ? displayPlural(value) : "everything"}
        </span>
        <ChevronsUpDownIcon className="size-3.5 shrink-0 text-faint" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 p-1">
        <Command>
          <CommandInput placeholder="Narrow to a collection…" />
          <CommandList>
            <CommandEmpty>No collection by that name.</CommandEmpty>
            <CommandGroup>
              <CommandItem
                value="everything"
                onSelect={() => onChange(undefined)}
                className="[&>svg:last-child]:hidden"
              >
                <span
                  className={cn(
                    "flex size-4 items-center justify-center",
                    value && "opacity-0"
                  )}
                >
                  <CheckIcon className="size-3" />
                </span>
                everything
              </CommandItem>
            </CommandGroup>
            {groups.map((g) => (
              <CommandGroup key={g.id} heading={g.label}>
                {[...g.primary, ...g.hidden].map((k) => (
                  <CommandItem
                    key={k.identity}
                    value={`${displayPlural(k)} ${g.label} ${k.identity}`}
                    onSelect={() => onChange(k)}
                    className="[&>svg:last-child]:hidden"
                  >
                    <span
                      className={cn(
                        "flex size-4 items-center justify-center",
                        value?.identity !== k.identity && "opacity-0"
                      )}
                    >
                      <CheckIcon className="size-3" />
                    </span>
                    <KindGlyph kind={k} size="xs" />
                    <span className="truncate">{displayPlural(k)}</span>
                    {technical && (
                      <KindPath
                        reference={k.identity}
                        className="ml-auto min-w-0 truncate text-[11px]"
                      />
                    )}
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

/** Before a query: the grammar, one line per form. */
function GrammarEmpty() {
  return (
    <div className="max-w-xl py-6">
      <p className="text-muted-foreground">
        Type words to search every record’s text: titles, names, descriptions
        and every other text a collection keeps. Every word must appear, in any
        form of the word and any case.
      </p>
      <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5">
        {SEARCH_GRAMMAR.map((g) => (
          <div key={g.example} className="contents">
            <dt className="font-mono text-[12.5px]">{g.example}</dt>
            <dd className="text-muted-foreground">{g.means}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
