import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  ActivityIcon,
  BotIcon,
  FolderIcon,
  HomeIcon,
  PackageIcon,
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
  { title: "Changelog", to: "/changelog", icon: ActivityIcon },
  { title: "Registry", to: "/registry", icon: PackageIcon },
  { title: "Agents", to: "/agents", icon: BotIcon },
] as const

/** ⌘K: jump to any page or type. Records join the list in a later slice. */
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

  function go(run: () => void) {
    onOpenChange(false)
    run()
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Go to"
      description="Jump to a page or a type"
    >
      <Command>
        <CommandInput placeholder="Go to page or type…" />
        <CommandList>
          <CommandEmpty>No matches.</CommandEmpty>
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
                    value={`${k.name} ${k.identity}`}
                    onSelect={() =>
                      go(
                        () =>
                          void navigate({
                            to: "/data/$authority/$plural",
                            params: {
                              authority: a.authority,
                              plural: k.plural,
                            },
                          })
                      )
                    }
                  >
                    {k.name}
                    <span className="ml-auto data text-xs text-muted-foreground">
                      {a.authority}
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
