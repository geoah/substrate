import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  Link,
  useNavigate,
  useParams,
  useRouterState,
} from "@tanstack/react-router"
import {
  ActivityIcon,
  BotIcon,
  ChevronRightIcon,
  ChevronsUpDownIcon,
  FileCode2Icon,
  GitMergeIcon,
  HomeIcon,
  KeyRoundIcon,
  LayersIcon,
  LogOutIcon,
  MoonIcon,
  PackageIcon,
  SunIcon,
  SunMoonIcon,
  UserRoundIcon,
} from "lucide-react"

import { useTheme } from "@/components/theme-provider"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar"
import { logout } from "@/lib/api/auth"
import { formatCount } from "@/lib/api/records"
import { pendingMergeCountQueryOptions } from "@/lib/api/mergerequests"
import {
  buildKindNav,
  kindsQueryOptions,
  type AuthorityNav,
} from "@/lib/api/kinds"
import { getToken, getUsername, maskedToken } from "@/lib/api/session"

const consoleItems = [
  { title: "Overview", to: "/", icon: HomeIcon },
  { title: "Changelog", to: "/changelog", icon: ActivityIcon },
  { title: "Registry", to: "/registry", icon: PackageIcon },
  { title: "Agents", to: "/agents", icon: BotIcon },
  { title: "Merge requests", to: "/merge-requests", icon: GitMergeIcon },
] as const

/** The Merge requests badge: pending suggestions waiting on a verdict —
 * actionable, not alarming, so it keeps the muted voice. Silent at zero. */
function MergeRequestsBadge() {
  const count = useQuery(pendingMergeCountQueryOptions())
  if (!count.data?.value) return null
  return (
    <SidebarMenuBadge className="text-muted-foreground">
      {formatCount(count.data)}
    </SidebarMenuBadge>
  )
}

/* Rule 1 (GUIDE §5): sub rows hover full-width, exactly like top-level rows —
 * the default inset/border of SidebarMenuSub is removed and depth is carried
 * by padding alone. */
const fullWidthSub = "mx-0 translate-x-0 border-l-0 px-0 pb-1.5"

function KindLinks({
  nav,
  className,
}: {
  nav: AuthorityNav
  className: string
}) {
  const params = useParams({ strict: false })
  return (
    <>
      {nav.kinds.map((k) => (
        <SidebarMenuSubItem key={k.identity}>
          <SidebarMenuSubButton
            isActive={
              params.authority === nav.authority && params.plural === k.plural
            }
            className={className}
            render={
              <Link
                to="/data/$authority/$plural"
                params={{ authority: nav.authority, plural: k.plural }}
              />
            }
          >
            <span>{k.name}</span>
          </SidebarMenuSubButton>
        </SidebarMenuSubItem>
      ))}
    </>
  )
}

/** One authority's kinds, collapsible. In v1 authorities replace the old group
 * concept; a repository-local kind (empty authority) reads as "local". */
function AuthorityGroup({ nav }: { nav: AuthorityNav }) {
  const label = nav.authority || "local"
  return (
    <Collapsible
      defaultOpen
      className="group/collapsible"
      render={<SidebarMenuItem />}
    >
      <CollapsibleTrigger
        render={<SidebarMenuButton tooltip={label} className="cursor-pointer" />}
      >
        <FileCode2Icon />
        <span className="truncate">{label}</span>
        <ChevronRightIcon className="ml-auto transition-transform duration-200 group-data-open/collapsible:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <SidebarMenuSub className={fullWidthSub}>
          <KindLinks nav={nav} className="pl-12" />
        </SidebarMenuSub>
      </CollapsibleContent>
    </Collapsible>
  )
}

function DataGroups() {
  const registry = useQuery(kindsQueryOptions)
  const nav = useMemo(
    () => (registry.data ? buildKindNav(registry.data) : undefined),
    [registry.data]
  )

  if (registry.isPending) {
    return (
      <SidebarMenu>
        {Array.from({ length: 4 }, (_, i) => (
          <SidebarMenuItem key={i}>
            <SidebarMenuSkeleton showIcon />
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    )
  }

  if (registry.isError || !nav) {
    return (
      <div className="flex flex-col items-start gap-1 px-2 py-1 text-xs text-sidebar-foreground/70 group-data-[collapsible=icon]:hidden">
        <span>The type registry didn't load.</span>
        <Button
          variant="outline"
          size="sm"
          className="h-6 px-2 text-xs"
          onClick={() => void registry.refetch()}
        >
          Retry
        </Button>
      </div>
    )
  }

  return (
    <SidebarMenu>
      {nav.authorities.map((a) => (
        <AuthorityGroup key={a.authority} nav={a} />
      ))}
    </SidebarMenu>
  )
}

function ActorFooter() {
  const { isMobile } = useSidebar()
  const navigate = useNavigate()
  const { theme, setTheme } = useTheme()
  const token = getToken()
  const username = getUsername()

  async function logOut() {
    // Logging out revokes the token record this browser holds — a session IS
    // that record. The local drop happens either way, so a refused revoke
    // never strands the reader in a console they cannot use.
    await logout()
    void navigate({ to: "/login", replace: true })
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton size="lg" className="aria-expanded:bg-muted" />
            }
          >
            <Avatar className="rounded-lg">
              <AvatarFallback className="rounded-lg">
                <KeyRoundIcon className="size-4" />
              </AvatarFallback>
            </Avatar>
            <div className="grid flex-1 text-left text-sm leading-tight">
              <span className="truncate font-medium">
                {username ?? "Signed in"}
              </span>
              <span className="truncate data text-xs text-sidebar-foreground/70">
                {token ? maskedToken(token) : "no session"}
              </span>
            </div>
            <ChevronsUpDownIcon className="ml-auto size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="min-w-44"
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
          >
            <DropdownMenuItem render={<Link to="/account" />}>
              <UserRoundIcon /> Account
            </DropdownMenuItem>
            <DropdownMenuItem render={<Link to="/account/tokens" />}>
              <KeyRoundIcon /> Tokens
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuRadioGroup
              value={theme}
              onValueChange={(value) =>
                setTheme(value as "light" | "dark" | "system")
              }
            >
              <DropdownMenuRadioItem value="light">
                <SunIcon /> Light
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="dark">
                <MoonIcon /> Dark
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="system">
                <SunMoonIcon /> System
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onClick={() => void logOut()}
            >
              <LogOutIcon /> Log out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}

export function AppSidebar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" render={<Link to="/" />}>
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <LayersIcon className="size-4" />
              </div>
              <div className="grid flex-1 text-left leading-tight">
                <span className="truncate font-semibold">Substrate</span>
                <span className="truncate text-xs text-sidebar-foreground/70">
                  console
                </span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent className="pb-2">
        <SidebarGroup>
          <SidebarGroupLabel>Console</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {consoleItems.map((item) => (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton
                    tooltip={item.title}
                    isActive={
                      item.to === "/"
                        ? pathname === "/"
                        : pathname === item.to ||
                          pathname.startsWith(`${item.to}/`)
                    }
                    render={<Link to={item.to} />}
                  >
                    <item.icon />
                    <span>{item.title}</span>
                  </SidebarMenuButton>
                  {item.to === "/merge-requests" && <MergeRequestsBadge />}
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>Data</SidebarGroupLabel>
          <SidebarGroupContent>
            <DataGroups />
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter className="border-t border-sidebar-border">
        <ActorFooter />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
