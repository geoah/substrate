/* eslint-disable react-refresh/only-export-components -- the bar and the
 * pure slice it renders live together so a test can hold the one to the other */
/** The phone's bottom tab bar: the console's own, listing what the launcher
 * offers (Overview first, then every `attach: launcher` view and every app,
 * at most five, the last slot becoming More when they overflow), so on a
 * phone the console is the app. Rendered only under 768 px, and never under
 * a view's or an app's own chrome, which draws its own bar and owns the
 * viewport (`staticData.chrome === "app"`). The shell mounts it below its
 * content column; the safe area is padded here so the shell need not know. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useMatches, useRouterState } from "@tanstack/react-router"
import { CircleIcon } from "lucide-react"

import { AppIcon } from "@/components/apps/icon"
import { useIsMobile } from "@/hooks/use-mobile"
import { appsQueryOptions, viewsQueryOptions } from "@/lib/api/apps"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { launcherEntries, type LauncherEntry } from "@/lib/apps/attach"
import { cn } from "@/lib/utils"

/** Five is the most a thumb tells apart on one row. */
export const MAX_TABS = 5

export const OVERVIEW: LauncherEntry = {
  key: "overview",
  label: "Overview",
  icon: "house",
  to: "/",
}

/** The overflow slot: the launcher lists everything. */
export const MORE: LauncherEntry = {
  key: "more",
  label: "More",
  icon: "ellipsis",
  to: "/apps",
}

/** Overview, then the entries; past five the last slot is More. */
export function tabsFor(entries: LauncherEntry[]): LauncherEntry[] {
  const all = [OVERVIEW, ...entries]
  if (all.length <= MAX_TABS) return all
  return [...all.slice(0, MAX_TABS - 1), MORE]
}

export function isActiveTab(entry: LauncherEntry, pathname: string): boolean {
  if (entry.to === "/") return pathname === "/"
  return pathname === entry.to || pathname.startsWith(`${entry.to}/`)
}

export function ConsoleTabBar({
  entries,
  pathname,
}: {
  entries: LauncherEntry[]
  pathname: string
}) {
  const tabs = tabsFor(entries)
  const lit = tabs.find((t) => t.key !== MORE.key && isActiveTab(t, pathname))
  // More is lit for a launcher page no tab carries, the launcher itself
  // included.
  const moreLit =
    !lit && (pathname.startsWith("/apps") || pathname.startsWith("/views"))
  return (
    <nav
      aria-label="Console"
      className="shrink-0 border-t bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur"
    >
      <ul className="flex h-14 items-stretch">
        {tabs.map((tab) => {
          const active = tab.key === MORE.key ? moreLit : tab === lit
          return (
            <li key={tab.key} className="flex min-w-0 flex-1">
              <Link
                to={tab.to}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex min-h-11 min-w-0 flex-1 flex-col items-center justify-center gap-0.5 text-[0.7rem] select-none",
                  active ? "text-primary" : "text-muted-foreground"
                )}
              >
                <AppIcon
                  name={tab.icon}
                  fallback={CircleIcon}
                  className="size-5"
                />
                <span className="w-full truncate px-1 text-center">
                  {tab.label}
                </span>
              </Link>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}

export function ConsoleTabs() {
  const isMobile = useIsMobile()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const matches = useMatches()
  const underAppChrome = matches.some(
    (m) => (m.staticData as { chrome?: string } | undefined)?.chrome === "app"
  )
  // A desktop never shows the bar, so it never pays for the reads.
  const views = useQuery({ ...viewsQueryOptions(), enabled: isMobile })
  const apps = useQuery({ ...appsQueryOptions(), enabled: isMobile })
  const registry = useQuery({ ...kindsQueryOptions, enabled: isMobile })
  const entries = useMemo(
    () =>
      launcherEntries(
        views.data?.records ?? [],
        apps.data?.records ?? [],
        registry.data ?? []
      ),
    [views.data, apps.data, registry.data]
  )
  if (!isMobile || underAppChrome) return null
  return <ConsoleTabBar entries={entries} pathname={pathname} />
}
