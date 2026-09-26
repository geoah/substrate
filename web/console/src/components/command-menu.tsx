/** ⌘K: jump to a record, a collection or a page, or hand what was typed to
 * the Search page. Typing runs the ranked read (words, five hits) after a
 * short pause, and the records it finds come first; collections and pages
 * match when every word typed starts a word of their name. With nothing
 * typed it lists the pages and the collections, grouped the way the sidebar
 * groups them, and every kind is reachable, the supporting and internal ones
 * too, each labelled for what it is. */

import { useEffect, useMemo, useState } from "react"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  BotIcon,
  DatabaseIcon,
  HistoryIcon,
  HomeIcon,
  PlugIcon,
  SearchIcon,
  SlidersHorizontalIcon,
  WrenchIcon,
} from "lucide-react"

import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath } from "@/components/identity/kind-ref"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { RecordRef } from "@/components/identity/record-ref"
import {
  Command,
  CommandDialog,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { providerOfKind } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { searchQueryOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { collectionGroups, type CollectionGroup } from "@/lib/collections"
import { matchesWordPrefixes, typeAheadQuery } from "@/lib/command-match"
import { kindPurpose } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { displayPlural } from "@/lib/kind-names"

const pages = [
  { title: "Home", to: "/", icon: HomeIcon },
  { title: "All data", to: "/data", icon: DatabaseIcon },
  { title: "Agents", to: "/agents", icon: BotIcon },
  { title: "Tools", to: "/tools", icon: WrenchIcon },
  { title: "Providers", to: "/providers", icon: PlugIcon },
  { title: "History", to: "/history", icon: HistoryIcon },
  { title: "Settings", to: "/settings", icon: SlidersHorizontalIcon },
] as const

/** How long typing rests before the records are asked for. */
const DEBOUNCE_MS = 150
/** How many records the palette offers; the Search page has the rest. */
const HITS = 5

function useDebounced(value: string, ms: number): string {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), ms)
    return () => clearTimeout(timer)
  }, [value, ms])
  return settled
}

export function CommandMenu({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  const groups = useMemo(
    () => collectionGroups(registry.data ?? []),
    [registry.data]
  )
  const [typed, setTyped] = useState("")
  const query = typed.trim()
  const asked = typeAheadQuery(useDebounced(typed, DEBOUNCE_MS))
  const records = useQuery({
    ...searchQueryOptions(asked, { mode: "lexical", first: HITS }),
    enabled: open && asked.length > 0,
    placeholderData: keepPreviousData,
  })

  function go(run: () => void) {
    onOpenChange(false)
    setTyped("")
    run()
  }

  function openCollection(k: KindInfo) {
    const { authority, pkg, name } = splitKind(k.identity)
    go(
      () =>
        void navigate({
          to: "/data/$authority/$pkg/$name",
          params: { authority, pkg, name },
        })
    )
  }

  function openRecord(r: SubstrateRecord) {
    const { authority, pkg, name } = splitKind(r.kind)
    go(
      () =>
        void navigate({
          to: "/data/$authority/$pkg/$name/$id",
          params: { authority, pkg, name, id: r.id },
        })
    )
  }

  // Everyday navigation lists the collections a person keeps; the supporting
  // and internal kinds are one switch away.
  const shown = (g: CollectionGroup) =>
    technical ? [...g.primary, ...g.hidden] : g.primary
  const collections = query
    ? groups
        .flatMap(shown)
        .filter((k) =>
          matchesWordPrefixes(
            query,
            [
              displayPlural(k),
              providerOfKind(k.identity)?.name ?? "",
              technical ? k.identity : "",
            ].join(" ")
          )
        )
    : []
  const matchedPages = pages.filter((p) => matchesWordPrefixes(query, p.title))
  const hits = query && records.data ? records.data.records : []
  // The hits on screen may still answer an earlier spelling of the query.
  const waiting =
    Boolean(query) &&
    !records.isError &&
    (typeAheadQuery(typed) !== asked || records.isFetching)

  // What Enter opens is the first row of what is on screen now: a row that
  // was first before the records landed keeps no claim to the selection.
  const first = query
    ? hits[0]
      ? `record:${hits[0].kind}/${hits[0].id}`
      : collections[0]
        ? `collection:${collections[0].identity}`
        : matchedPages[0]
          ? `page:${matchedPages[0].to}`
          : "search"
    : `page:${pages[0].to}`
  const [selected, setSelected] = useState(first)
  const [shownFirst, setShownFirst] = useState(first)
  if (shownFirst !== first) {
    setShownFirst(first)
    setSelected(first)
  }

  const pagesGroup = matchedPages.length > 0 && (
    <CommandGroup heading="Pages">
      {matchedPages.map((p) => (
        <CommandItem
          key={p.to}
          value={`page:${p.to}`}
          onSelect={() => go(() => void navigate({ to: p.to }))}
        >
          <p.icon />
          {p.title}
        </CommandItem>
      ))}
    </CommandGroup>
  )

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setTyped("")
        onOpenChange(next)
      }}
      title="Search or jump to"
      description="Jump to a record, a collection or a page, or search your records"
    >
      <Command
        shouldFilter={false}
        value={selected}
        onValueChange={setSelected}
      >
        <CommandInput
          placeholder="Search or jump to…"
          value={typed}
          onValueChange={setTyped}
        />
        <CommandList>
          {query ? (
            <>
              {(hits.length > 0 || waiting) && (
                <CommandGroup heading="Records" aria-busy={waiting}>
                  {hits.map((r) => (
                    <CommandItem
                      key={`${r.kind}/${r.id}`}
                      value={`record:${r.kind}/${r.id}`}
                      onSelect={() => openRecord(r)}
                    >
                      <span className="min-w-0 flex-1 truncate">
                        <RecordRef
                          kind={r.kind}
                          id={r.id}
                          title={recordTitle(r.properties ?? {}) || undefined}
                          link={false}
                        />
                      </span>
                      <span className="shrink-0 pl-3 text-[12px] text-faint">
                        {displayPlural(r.kind)}
                      </span>
                    </CommandItem>
                  ))}
                  {hits.length === 0 && (
                    <div className="px-2 py-1.5 text-[13px] text-faint">
                      Loading records…
                    </div>
                  )}
                </CommandGroup>
              )}
              {collections.length > 0 && (
                <CommandGroup heading="Collections">
                  {collections.map((k) => (
                    <CollectionItem
                      key={k.identity}
                      kind={k}
                      technical={technical}
                      showProvider
                      onSelect={() => openCollection(k)}
                    />
                  ))}
                </CommandGroup>
              )}
              {pagesGroup}
              <CommandGroup heading="Search">
                <CommandItem
                  value="search"
                  onSelect={() =>
                    go(
                      () =>
                        void navigate({ to: "/search", search: { q: query } })
                    )
                  }
                >
                  <SearchIcon />
                  <span className="min-w-0 truncate">
                    Search your records for “{query}”
                  </span>
                </CommandItem>
              </CommandGroup>
            </>
          ) : (
            <>
              {pagesGroup}
              {groups.map((g) => {
                const kinds = shown(g)
                if (!kinds.length) return null
                return (
                  <CommandGroup
                    key={g.id}
                    heading={
                      <span className="inline-flex items-center gap-1.5">
                        {g.provider && (
                          <ProviderBadge provider={g.provider} size="xs" />
                        )}
                        {g.label}
                      </span>
                    }
                  >
                    {kinds.map((k) => (
                      <CollectionItem
                        key={k.identity}
                        kind={k}
                        technical={technical}
                        onSelect={() => openCollection(k)}
                      />
                    ))}
                  </CommandGroup>
                )
              })}
            </>
          )}
        </CommandList>
      </Command>
    </CommandDialog>
  )
}

/** One collection: its glyph and plural, the provider it comes from when no
 * group heading says so, its purpose when it is not a primary collection,
 * and its reference in technical mode. */
function CollectionItem({
  kind,
  technical,
  showProvider = false,
  onSelect,
}: {
  kind: KindInfo
  technical: boolean
  showProvider?: boolean
  onSelect: () => void
}) {
  const purpose = kindPurpose(kind)
  const provider = showProvider ? providerOfKind(kind.identity) : undefined
  return (
    <CommandItem value={`collection:${kind.identity}`} onSelect={onSelect}>
      <KindGlyph kind={kind} size="xs" />
      <span className="truncate">{displayPlural(kind)}</span>
      {provider && (
        <span className="inline-flex shrink-0 items-center gap-1 text-[12px] text-faint">
          <ProviderBadge provider={provider} size="xs" />
          {provider.name}
        </span>
      )}
      {purpose !== "primary" && (
        <span className="shrink-0 rounded-[3px] border border-border-strong px-1 text-[11.5px] leading-4 text-faint">
          {purpose}
        </span>
      )}
      {technical && (
        <KindPath
          reference={kind.identity}
          className="ml-auto min-w-0 truncate text-[11px]"
        />
      )}
    </CommandItem>
  )
}
