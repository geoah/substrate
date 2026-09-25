/** ⌘K: jump to any page or collection, or hand what was typed to the Search
 * page as a records query. Collections are grouped the way the sidebar groups
 * them, by display plural, and every kind is reachable here, the supporting
 * and internal ones too, each labelled for what it is. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
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
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { collectionGroups } from "@/lib/collections"
import { kindPurpose } from "@/lib/definition"
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

  function go(run: () => void) {
    onOpenChange(false)
    setTyped("")
    run()
  }

  const query = typed.trim()

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setTyped("")
        onOpenChange(next)
      }}
      title="Search or jump to"
      description="Jump to a page or a collection, or search your records"
    >
      <Command>
        <CommandInput
          placeholder="Search or jump to…"
          value={typed}
          onValueChange={setTyped}
        />
        <CommandList>
          <CommandEmpty>No page or collection matches.</CommandEmpty>
          {query && (
            <CommandGroup heading="Records">
              {/* the value carries the typed text so the item always matches
                  what filters the list */}
              <CommandItem
                value={`search records ${query}`}
                onSelect={() =>
                  go(
                    () => void navigate({ to: "/search", search: { q: query } })
                  )
                }
              >
                <SearchIcon />
                <span className="min-w-0 truncate">
                  Search your records for “{query}”
                </span>
              </CommandItem>
            </CommandGroup>
          )}
          <CommandGroup heading="Pages">
            {pages.map((p) => (
              <CommandItem
                key={p.to}
                value={`page ${p.title}`}
                onSelect={() => go(() => void navigate({ to: p.to }))}
              >
                <p.icon />
                {p.title}
              </CommandItem>
            ))}
          </CommandGroup>
          {groups.map((g) => (
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
              {[...g.primary, ...g.hidden].map((k) => {
                const { authority, pkg, name } = splitKind(k.identity)
                const purpose = kindPurpose(k)
                const plural = displayPlural(k)
                return (
                  <CommandItem
                    key={k.identity}
                    value={`${plural} ${g.label} ${k.identity}`}
                    onSelect={() =>
                      go(
                        () =>
                          void navigate({
                            to: "/data/$authority/$pkg/$name",
                            params: { authority, pkg, name },
                          })
                      )
                    }
                  >
                    <KindGlyph kind={k} size="xs" />
                    <span className="truncate">{plural}</span>
                    {purpose !== "primary" && (
                      <span className="shrink-0 rounded-[3px] border border-border-strong px-1 text-[10px] leading-4 text-faint">
                        {purpose}
                      </span>
                    )}
                    {technical && (
                      <KindPath
                        reference={k.identity}
                        className="ml-auto min-w-0 truncate text-[11px]"
                      />
                    )}
                  </CommandItem>
                )
              })}
            </CommandGroup>
          ))}
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
