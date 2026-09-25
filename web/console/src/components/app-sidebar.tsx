/** The sidebar: the repository at the top, the search that opens ⌘K, the
 * five places (Home, All data, Agents, Tools, Providers), then the
 * collections grouped the way a person meets them ("Your data", one
 * "From <Provider>" per provider) and, at the foot, History, Settings, the
 * Technical details switch and the account menu.
 *
 * Everyday mode lists each group's primary collections by display plural.
 * Technical mode lists the authority / package tree with each kind's own
 * name, can show the supporting and internal kinds too, and adds the
 * substrate's own machinery as a last group. */

import { useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  Link,
  useNavigate,
  useParams,
  useRouterState,
} from "@tanstack/react-router"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  BotIcon,
  ChevronDownIcon,
  ChevronsUpDownIcon,
  CodeIcon,
  DatabaseIcon,
  HistoryIcon,
  HomeIcon,
  LogOutIcon,
  MoonIcon,
  PlugIcon,
  SearchIcon,
  SlidersHorizontalIcon,
  StarIcon,
  SunIcon,
  SunMoonIcon,
  WrenchIcon,
  type LucideIcon,
} from "lucide-react"

import { KindGlyph } from "@/components/identity/kind-glyph"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { ToggleSwitch } from "@/components/nav/toggle-switch"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Kbd } from "@/components/ui/kbd"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  useSidebar,
} from "@/components/ui/sidebar"
import { Skeleton } from "@/components/ui/skeleton"
import {
  useConsolePreferences,
  useSidebarPreferences,
  useTechnicalDetails,
} from "@/hooks/use-console-preferences"
import { PROVIDERS_AUTHORITY } from "@/lib/actor-identity"
import { logout } from "@/lib/api/auth"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { formatCount, recordCountQueryOptions } from "@/lib/api/records"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import type { KindInfo } from "@/lib/api/types"
import {
  collectionGroups,
  groupToggleKey,
  isGroupOpen,
  type CollectionGroup,
} from "@/lib/collections"
import { kindPurpose } from "@/lib/definition"
import { displayPlural } from "@/lib/kind-names"
import { cn } from "@/lib/utils"

const ROW =
  "flex h-[30px] w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-[13.5px] whitespace-nowrap text-muted-foreground no-underline outline-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
const ACTIVE = "bg-hover font-medium text-foreground"

/** Closes the phone sheet once a row is followed. */
function useCloseOnPhone(): () => void {
  const { isMobile, setOpenMobile } = useSidebar()
  return () => {
    if (isMobile) setOpenMobile(false)
  }
}

type Place =
  "/" | "/data" | "/agents" | "/tools" | "/providers" | "/history" | "/settings"

function NavRow({
  to,
  icon: Icon,
  label,
  extra,
}: {
  to: Place
  icon: LucideIcon
  label: string
  extra?: ReactNode
}) {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const close = useCloseOnPhone()
  // All data is the index of /data; a collection under it lights its own row.
  const active =
    to === "/" || to === "/data"
      ? pathname === to
      : pathname === to || pathname.startsWith(`${to}/`)
  return (
    <Link
      to={to}
      onClick={close}
      aria-current={active ? "page" : undefined}
      className={cn(ROW, active && ACTIVE)}
    >
      <Icon className="size-4 shrink-0" strokeWidth={1.8} />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {extra}
    </Link>
  )
}

/** A kind's record count, only when a page already counted it: the sidebar
 * never starts a count walk of its own. */
function CachedCount({ kind }: { kind: KindInfo }) {
  const { authority, pkg, name } = splitKind(kind.identity)
  const count = useQuery({
    ...recordCountQueryOptions(authority, pkg, name),
    enabled: false,
  })
  if (!count.data?.value) return null
  return (
    <span className="ml-auto shrink-0 text-[11.5px] text-faint tabular-nums">
      {formatCount(count.data)}
    </span>
  )
}

function StarButton({ identity }: { identity: string }) {
  const { preferences, busy, change } = useSidebarPreferences()
  const starred = preferences.favorites.includes(identity)
  return (
    <button
      type="button"
      disabled={busy}
      aria-label={`${starred ? "Unstar" : "Star"} ${identity}`}
      aria-pressed={starred}
      title={starred ? "Remove from favorites" : "Add to favorites"}
      className={cn(
        "absolute top-1/2 right-1 grid size-6 -translate-y-1/2 cursor-pointer place-items-center rounded bg-sidebar text-faint opacity-0 group-hover/kind:opacity-100 hover:text-foreground focus-visible:opacity-100 disabled:opacity-0",
        starred && "text-primary"
      )}
      onClick={() =>
        change({ type: "favorite", key: identity, starred: !starred })
      }
    >
      <StarIcon className={cn("size-3.5", starred && "fill-current")} />
    </button>
  )
}

/** One collection in a group. Everyday: glyph and display plural. Technical:
 * the reference's own name, tagged when it is not a primary collection. */
function KindRow({ kind, technical }: { kind: KindInfo; technical: boolean }) {
  const params = useParams({ strict: false })
  const close = useCloseOnPhone()
  const { authority, pkg, name } = splitKind(kind.identity)
  const active =
    params.authority === authority && params.pkg === pkg && params.name === name
  const purpose = kindPurpose(kind)
  return (
    <div className="group/kind relative">
      <Link
        to="/data/$authority/$pkg/$name"
        params={{ authority, pkg, name }}
        onClick={close}
        aria-current={active ? "page" : undefined}
        title={kind.identity}
        className={cn(ROW, "pr-2 pl-3.5", active && ACTIVE)}
      >
        <KindGlyph kind={kind} size="xs" />
        <span
          className={cn(
            "min-w-0 truncate",
            purpose !== "primary" && !active && "text-faint"
          )}
        >
          {technical ? name : displayPlural(kind)}
        </span>
        {technical && purpose !== "primary" && (
          <span className="shrink-0 rounded-[3px] border border-border-strong px-1 text-[10px] leading-4 font-normal text-faint">
            {purpose}
          </span>
        )}
        <CachedCount kind={kind} />
      </Link>
      <StarButton identity={kind.identity} />
    </div>
  )
}

/** One group: a folding heading and its collections. */
export function CollectionGroupNav({ group }: { group: CollectionGroup }) {
  const [technical] = useTechnicalDetails()
  const { preferences, busy, change } = useSidebarPreferences()
  const params = useParams({ strict: false })
  const close = useCloseOnPhone()
  const [showAll, setShowAll] = useState(false)
  const open = isGroupOpen(group, preferences.collapsed)
  const key = groupToggleKey(group)
  const visible = (k: KindInfo) => showAll || kindPurpose(k) === "primary"
  return (
    <div data-slot="collection-group" data-group={group.id}>
      <button
        type="button"
        disabled={busy}
        aria-expanded={open}
        onClick={() =>
          change({
            type: "collapse",
            key,
            collapsed: !preferences.collapsed.includes(key),
          })
        }
        className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 pt-3.5 pb-1 text-left text-[11.5px] font-medium text-faint hover:text-muted-foreground disabled:cursor-default"
      >
        <ChevronDownIcon
          aria-hidden
          className={cn(
            "size-3 shrink-0 transition-transform duration-150",
            !open && "-rotate-90"
          )}
        />
        {group.provider && (
          <ProviderBadge provider={group.provider} size="xs" />
        )}
        <span className="truncate">{group.label}</span>
        {!open && (
          <span className="ml-auto font-normal tabular-nums">
            {technical ? group.kinds.length : group.primary.length}
          </span>
        )}
      </button>
      {open &&
        (technical ? (
          <>
            {group.authorities.map((a) => (
              <div key={a.authority}>
                <Link
                  to="/data/$authority"
                  params={{ authority: a.authority }}
                  onClick={close}
                  className={cn(
                    "block truncate rounded-md px-2 pt-2.5 pb-0.5 font-mono text-[11px] text-faint no-underline hover:text-foreground",
                    params.authority === a.authority &&
                      !params.pkg &&
                      "text-foreground"
                  )}
                >
                  {a.authority}
                </Link>
                {a.packages.map((p) => (
                  <div key={p.identity}>
                    <Link
                      to="/data/$authority/$pkg"
                      params={{ authority: p.authority, pkg: p.package }}
                      onClick={close}
                      className={cn(
                        "flex items-center gap-1.5 rounded-md py-1 pr-2 pl-2.5 text-[11.5px] text-faint no-underline before:h-px before:w-1.5 before:bg-border-strong hover:text-foreground",
                        params.authority === p.authority &&
                          params.pkg === p.package &&
                          !params.name &&
                          "text-foreground"
                      )}
                    >
                      {p.package}
                    </Link>
                    {p.kinds.filter(visible).map((k) => (
                      <KindRow key={k.identity} kind={k} technical />
                    ))}
                  </div>
                ))}
              </div>
            ))}
            {group.hidden.length > 0 && (
              <button
                type="button"
                onClick={() => setShowAll((v) => !v)}
                className="w-full cursor-pointer rounded-md py-1 pr-2 pl-[30px] text-left text-xs text-faint hover:text-muted-foreground"
              >
                {showAll ? "Hide" : "Show"} {group.hidden.length} supporting and
                internal
              </button>
            )}
          </>
        ) : (
          group.primary.map((k) => (
            <KindRow key={k.identity} kind={k} technical={false} />
          ))
        ))}
    </div>
  )
}

function CollectionGroups() {
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  const repository = useQuery(repositoryQueryOptions)
  const groups = useMemo(
    () =>
      collectionGroups(
        registry.data ?? [],
        repository.data?.authority ?? getRepository() ?? ""
      ),
    [registry.data, repository.data]
  )

  if (registry.isPending) {
    return (
      <div className="flex flex-col gap-2 px-2 pt-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-5 w-full" />
        ))}
      </div>
    )
  }
  if (registry.isError) {
    return (
      <div className="flex flex-col items-start gap-1.5 px-2 pt-4 text-xs text-faint">
        <span>Your collections didn’t load.</span>
        <Button
          variant="outline"
          size="xs"
          onClick={() => void registry.refetch()}
        >
          Try again
        </Button>
      </div>
    )
  }
  return (
    <>
      {groups
        .filter((g) =>
          technical ? true : g.type !== "system" && g.primary.length > 0
        )
        .map((g) => (
          <CollectionGroupNav key={g.id} group={g} />
        ))}
    </>
  )
}

export function Favorites() {
  const { preferences, busy, change } = useSidebarPreferences()
  const [technical] = useTechnicalDetails()
  const params = useParams({ strict: false })
  const close = useCloseOnPhone()
  if (preferences.favorites.length === 0) return null
  return (
    <div data-slot="favorites">
      <div className="px-2 pt-3.5 pb-1 text-[11.5px] font-medium text-faint">
        Favorites
      </div>
      {preferences.favorites.map((identity, index) => {
        const parts = splitKind(identity)
        const active =
          params.authority === parts.authority &&
          params.pkg === parts.pkg &&
          params.name === parts.name
        return (
          <div key={identity} className="group/fav relative">
            <Link
              to="/data/$authority/$pkg/$name"
              params={parts}
              onClick={close}
              aria-label={identity}
              title={identity}
              aria-current={active ? "page" : undefined}
              className={cn(ROW, active && ACTIVE)}
            >
              <KindGlyph kind={identity} size="xs" />
              <span className="min-w-0 truncate">
                {technical ? parts.name : displayPlural(identity)}
              </span>
            </Link>
            <div className="absolute top-1/2 right-1 flex -translate-y-1/2 rounded bg-sidebar opacity-0 group-hover/fav:opacity-100 focus-within:opacity-100">
              <button
                type="button"
                className="grid size-6 cursor-pointer place-items-center rounded text-faint hover:text-foreground disabled:opacity-30"
                disabled={busy || index === 0}
                aria-label={`Move ${identity} up`}
                onClick={() =>
                  change({ type: "move", key: identity, direction: -1 })
                }
              >
                <ArrowUpIcon className="size-3.5" />
              </button>
              <button
                type="button"
                className="grid size-6 cursor-pointer place-items-center rounded text-faint hover:text-foreground disabled:opacity-30"
                disabled={busy || index === preferences.favorites.length - 1}
                aria-label={`Move ${identity} down`}
                onClick={() =>
                  change({ type: "move", key: identity, direction: 1 })
                }
              >
                <ArrowDownIcon className="size-3.5" />
              </button>
              <button
                type="button"
                className="grid size-6 cursor-pointer place-items-center rounded text-primary"
                disabled={busy}
                aria-label={`Unstar ${identity}`}
                onClick={() =>
                  change({ type: "favorite", key: identity, starred: false })
                }
              >
                <StarIcon className="size-3.5 fill-current" />
              </button>
            </div>
          </div>
        )
      })}
    </div>
  )
}

/** How many providers this repository has added, beside the Providers row. */
function ProviderCount() {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const count = (statuses.data ?? []).filter(
    (b) => b.authority === PROVIDERS_AUTHORITY
  ).length
  if (!count) return null
  return (
    <span className="ml-auto text-[11.5px] text-faint tabular-nums">
      {count}
    </span>
  )
}

function RepositoryMark({ repository }: { repository: string }) {
  return (
    <span
      aria-hidden
      className="grid size-[22px] shrink-0 place-items-center rounded-md bg-foreground text-xs font-bold text-background"
    >
      {(repository[0] ?? "s").toUpperCase()}
    </span>
  )
}

export function TechnicalSwitchRow() {
  const [technical, setTechnical] = useTechnicalDetails()
  return (
    <div className="flex items-center gap-2 px-2 py-1.5 text-[12.5px] text-muted-foreground">
      <CodeIcon className="size-4 shrink-0" strokeWidth={1.8} />
      <span className="flex-1">Technical details</span>
      <ToggleSwitch
        checked={technical}
        onChange={setTechnical}
        label="Show technical details"
      />
    </div>
  )
}

function AccountMenu() {
  const { isMobile } = useSidebar()
  const navigate = useNavigate()
  const { preferences, set } = useConsolePreferences()
  const repository = getRepository() ?? "Signed in"

  async function signOut() {
    // Signing out revokes the token record this browser holds; a session IS
    // that record. The local copy is dropped either way, so a refused revoke
    // never strands the reader in a console they cannot use.
    await logout()
    void navigate({ to: "/login", replace: true })
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(ROW, "mt-1 h-9 cursor-pointer aria-expanded:bg-hover")}
      >
        <RepositoryMark repository={repository} />
        <span className="min-w-0 flex-1 truncate font-medium text-foreground">
          {repository}
        </span>
        <ChevronsUpDownIcon className="size-3.5 shrink-0 text-faint" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        className="min-w-52"
        side={isMobile ? "bottom" : "right"}
        align="end"
        sideOffset={6}
      >
        <DropdownMenuLabel className="truncate">{repository}</DropdownMenuLabel>
        <DropdownMenuItem render={<Link to="/settings" />}>
          <SlidersHorizontalIcon /> Account and settings
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          value={preferences.theme}
          onValueChange={(value) =>
            set("theme", value as "light" | "dark" | "system")
          }
        >
          <DropdownMenuRadioItem value="system">
            <SunMoonIcon /> System
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="light">
            <SunIcon /> Light
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <MoonIcon /> Dark
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={() => void signOut()}>
          <LogOutIcon /> Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function AppSidebar({ onSearch }: { onSearch: () => void }) {
  const repository = getRepository() ?? "substrate"
  const close = useCloseOnPhone()
  return (
    <Sidebar collapsible="offcanvas">
      <SidebarHeader className="gap-1 px-3 pt-3 pb-2">
        <Link
          to="/"
          onClick={close}
          className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-foreground no-underline hover:bg-hover"
        >
          <RepositoryMark repository={repository} />
          <span className="min-w-0 truncate text-[13.5px] font-semibold">
            {repository}
          </span>
        </Link>
        <button
          type="button"
          onClick={onSearch}
          className="mt-1 flex cursor-pointer items-center gap-2 rounded-md border border-border-strong bg-background px-2 py-1.5 text-left text-[13px] text-faint hover:text-muted-foreground"
        >
          <SearchIcon className="size-4 shrink-0" strokeWidth={1.8} />
          <span className="flex-1 truncate">Search or jump to…</span>
          <Kbd>⌘K</Kbd>
        </button>
      </SidebarHeader>
      <SidebarContent className="gap-0 px-2 pb-3">
        <nav aria-label="Places" className="flex flex-col">
          <NavRow to="/" icon={HomeIcon} label="Home" />
          <NavRow to="/data" icon={DatabaseIcon} label="All data" />
          <NavRow to="/agents" icon={BotIcon} label="Agents" />
          <NavRow to="/tools" icon={WrenchIcon} label="Tools" />
          <NavRow
            to="/providers"
            icon={PlugIcon}
            label="Providers"
            extra={<ProviderCount />}
          />
        </nav>
        <Favorites />
        <nav aria-label="Collections" className="flex flex-col">
          <CollectionGroups />
        </nav>
      </SidebarContent>
      <SidebarFooter className="gap-0 border-t border-sidebar-border p-2">
        <NavRow to="/history" icon={HistoryIcon} label="History" />
        <NavRow to="/settings" icon={SlidersHorizontalIcon} label="Settings" />
        <TechnicalSwitchRow />
        <AccountMenu />
      </SidebarFooter>
    </Sidebar>
  )
}
