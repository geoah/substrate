import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  ActivityIcon,
  BotIcon,
  FolderIcon,
  HomeIcon,
  PackageIcon,
  SearchIcon,
} from "lucide-react"

import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { buildKindNav, kindsQueryOptions } from "@/lib/api/kinds"

const pages = [
  { title: "Overview", to: "/", icon: HomeIcon },
  { title: "Search", to: "/search", icon: SearchIcon },
  { title: "Changelog", to: "/changelog", icon: ActivityIcon },
  { title: "Registry", to: "/registry", icon: PackageIcon },
  { title: "Agents", to: "/agents", icon: BotIcon },
] as const

/** ⌘K: jump to any page or kind, or hand what was typed to the Search page
 * as a records query. The palette itself matches page and kind names; the
 * records live behind the ranked read, one Enter away. */
export function CommandMenu({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const registry = useQuery(kindsQueryOptions)
  const nav = useMemo(
    () => (registry.data ? buildKindNav(registry.data) : undefined),
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
      title="Go to"
      description="Jump to a page or a kind, or search the records"
    >
      <Command>
        <CommandInput
          placeholder="Go to a page or a kind, or search records…"
          value={typed}
          onValueChange={setTyped}
        />
        <CommandList>
          <CommandEmpty>No page or kind matches.</CommandEmpty>
          {query && (
            <CommandGroup heading="Records">
              {/* forceMount-free: the value carries the typed text so the
                  item always matches what filters the list */}
              <CommandItem
                value={`search records ${query}`}
                onSelect={() =>
                  go(
                    () => void navigate({ to: "/search", search: { q: query } })
                  )
                }
              >
                <SearchIcon />
                Search records for{" "}
                <span className="truncate data">{query}</span>
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
          {nav &&
            nav.authorities.map((a) => (
              <CommandGroup key={a.authority} heading={a.authority}>
                {/* the authority page itself — its kinds-at-a-glance table */}
                <CommandItem
                  value={`authority ${a.authority}`}
                  onSelect={() =>
                    go(
                      () =>
                        void navigate({
                          to: "/data/$authority",
                          params: { authority: a.authority },
                        })
                    )
                  }
                >
                  <FolderIcon />
                  {a.authority}
                  <span className="ml-auto data text-xs text-muted-foreground">
                    authority
                  </span>
                </CommandItem>
                {a.kinds.map((k) => (
                  <CommandItem
                    key={k.identity}
                    value={`${k.name} ${k.package} ${k.identity}`}
                    onSelect={() =>
                      go(
                        () =>
                          void navigate({
                            to: "/data/$authority/$pkg/$name",
                            params: {
                              authority: a.authority,
                              pkg: k.package,
                              name: k.name,
                            },
                          })
                      )
                    }
                  >
                    {k.name}
                    <span className="ml-auto data text-xs text-muted-foreground">
                      {a.authority}/{k.package}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
