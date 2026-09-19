/** /search: the ranked read (`GET /records?q=`) as a page. A query in the
 * search grammar, the ARM to rank by (words, words + meaning, meaning — the
 * reader's choice, and it sticks), an optional kind to narrow the candidates,
 * and the hits in rank order with each one's raw per-arm score beside it.
 * The query, arm and kind live in the URL, so a search is shareable and the
 * back button returns to it. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { CheckIcon, ChevronsUpDownIcon, SearchXIcon } from "lucide-react"
import { parseAsString, parseAsStringLiteral, useQueryState } from "nuqs"

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
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { searchQueryOptions } from "@/lib/api/records"
import type { KindInfo, Scores, SubstrateRecord } from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
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
  const [q, setQ] = useQueryState("q", parseAsString.withDefault(""))
  // The URL names the arm when it says; otherwise the stored preference.
  const [modeParam, setModeParam] = useQueryState(
    "mode",
    parseAsStringLiteral(SEARCH_MODES)
  )
  const [kind, setKind] = useQueryState("kind", parseAsString.withDefault(""))
  const mode: SearchMode = modeParam ?? loadSearchMode()

  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(
    () =>
      [...(registry.data ?? [])].sort((a, b) =>
        a.identity.localeCompare(b.identity)
      ),
    [registry.data]
  )
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
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-3">
        <h1 className="text-2xl font-semibold tracking-tight">Search</h1>
        <p className="text-xs text-muted-foreground">
          Every record in this repository, ranked against your words.
        </p>
      </div>

      <div className="flex shrink-0 flex-col gap-3 border-b px-6 pb-4">
        <div className="flex flex-wrap items-center gap-2">
          <SearchBox
            className="w-full max-w-xl"
            label="Search records"
            placeholder="Search records…"
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
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <Tabs
            value={mode}
            onValueChange={(next) => {
              const m = next as SearchMode
              saveSearchMode(m)
              void setModeParam(m)
            }}
          >
            <TabsList aria-label="Rank by">
              {SEARCH_MODES.map((m) => (
                <TabsTrigger key={m} value={m}>
                  {SEARCH_MODE_LABEL[m]}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <p className="max-w-prose text-xs text-muted-foreground">
            {SEARCH_MODE_DESCRIPTION[mode]}
          </p>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto px-6 py-4">
        {!words ? (
          <GrammarEmpty />
        ) : results.isError ? (
          <Empty className="py-16">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <SearchXIcon />
              </EmptyMedia>
              <EmptyTitle>This search was refused</EmptyTitle>
              {/* the server's problem, verbatim: it names the arm, the
                  provider row or the word that was missing */}
              <EmptyDescription>{results.error.message}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void results.refetch()}
              >
                Retry
              </Button>
            </EmptyContent>
          </Empty>
        ) : results.isPending ? (
          <ul className="flex flex-col gap-2" aria-busy>
            {Array.from({ length: 6 }, (_, i) => (
              <li key={i} className="rounded-lg border px-4 py-3">
                <Skeleton className="h-4 w-2/5" />
                <Skeleton className="mt-2 h-3 w-3/5" />
              </li>
            ))}
          </ul>
        ) : results.data.records.length === 0 ? (
          <Empty className="py-16">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <SearchXIcon />
              </EmptyMedia>
              <EmptyTitle>Nothing matches</EmptyTitle>
              <EmptyDescription>
                No record{narrowed ? ` of ${narrowed.name}` : ""} ranks against{" "}
                <span className="data">{words}</span> by{" "}
                {SEARCH_MODE_LABEL[mode].toLowerCase()}.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2 text-xs text-muted-foreground">
              <span>
                {results.data.records.length} hit
                {results.data.records.length === 1 ? "" : "s"}
                {results.data.records.length === HITS
                  ? " (the most one search shows)"
                  : ""}
                , best first
              </span>
              {results.data.pending > 0 && (
                <span>
                  {results.data.pending} values are still being embedded, so the
                  meaning arm ranked a partial index.
                </span>
              )}
            </div>
            <ol className="flex flex-col gap-2">
              {results.data.records.map((record) => (
                <Hit
                  key={`${record.kind}/${record.id}`}
                  record={record}
                  scores={results.data.scores[`${record.kind}/${record.id}`]}
                  kindInfo={kinds.find((k) => k.identity === record.kind)}
                />
              ))}
            </ol>
          </>
        )}
      </div>
    </div>
  )
}

/** One hit: the record's title (its id when it has none), the full kind
 * reference and id wrapped rather than shortened, and each arm's raw score
 * under its own label. The row is the link to the record. */
function Hit({
  record,
  scores,
  kindInfo,
}: {
  record: SubstrateRecord
  scores: Scores | undefined
  kindInfo: KindInfo | undefined
}) {
  const { authority, pkg, name } = splitKind(record.kind)
  const title = recordTitle(record.properties ?? {})
  const body = (
    <>
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <span className={cn("font-medium", !title && "data")}>
          {title || record.id}
        </span>
        <dl className="flex gap-4 text-xs text-muted-foreground">
          {scores?.lexical !== undefined && (
            <div className="flex gap-1">
              <dt>words</dt>
              <dd className="data">{scores.lexical.toFixed(3)}</dd>
            </div>
          )}
          {scores?.semantic !== undefined && (
            <div className="flex gap-1">
              <dt>meaning</dt>
              <dd className="data">{scores.semantic.toFixed(3)}</dd>
            </div>
          )}
        </dl>
      </div>
      <div className="mt-1 flex flex-wrap items-baseline gap-x-2 text-xs text-muted-foreground">
        {kindInfo && <span>{kindInfo.name}</span>}
        <span className="data break-all">
          {record.kind}/{record.id}
        </span>
      </div>
    </>
  )
  const row =
    "block rounded-lg border px-4 py-3 no-underline transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
  if (!authority || !pkg || !name) {
    return <li className={row}>{body}</li>
  }
  return (
    <li>
      <Link
        to="/data/$authority/$pkg/$name/$id"
        params={{ authority, pkg, name, id: record.id }}
        className={row}
      >
        {body}
      </Link>
    </li>
  )
}

/** Narrow the candidates to one kind, or none: the ranked read's one filter
 * arm, `filter.kinds`. */
function KindPicker({
  kinds,
  value,
  onChange,
}: {
  kinds: KindInfo[]
  value: KindInfo | undefined
  onChange: (next: KindInfo | undefined) => void
}) {
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-8 max-w-72 gap-1.5 font-normal"
            aria-label="Kinds to search"
          />
        }
      >
        <span className="text-muted-foreground">in</span>
        <span className={cn("truncate", value && "data")}>
          {value ? value.identity : "all kinds"}
        </span>
        <ChevronsUpDownIcon className="size-3.5 shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 p-1">
        <Command>
          <CommandInput placeholder="Narrow to a kind…" />
          <CommandList>
            <CommandEmpty>No kind by that name.</CommandEmpty>
            <CommandGroup>
              <CommandItem
                value="all kinds"
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
                all kinds
              </CommandItem>
              {kinds.map((k) => (
                <CommandItem
                  key={k.identity}
                  value={`${k.name} ${k.identity}`}
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
                  <span>{k.name}</span>
                  <span className="ml-auto text-right data text-xs text-muted-foreground">
                    {k.authority}/{k.package}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

/** Before a query: the grammar, one line per form. */
function GrammarEmpty() {
  return (
    <div className="mx-auto max-w-xl py-10">
      <p className="text-sm text-muted-foreground">
        Type words to search every record's indexed text: titles, names,
        descriptions and every other string property a kind declares. Every word
        must appear, in any form of the word and any case.
      </p>
      <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm">
        {SEARCH_GRAMMAR.map((g) => (
          <div key={g.example} className="contents">
            <dt className="data">{g.example}</dt>
            <dd className="text-muted-foreground">{g.means}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
