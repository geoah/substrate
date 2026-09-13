/* eslint-disable react-refresh/only-export-components -- the bar and the
 * pure slice it renders live together so a test can hold the one to the other */
/** The phone's bottom tab bar: the console's own, listing what the launcher
 * offers (Overview first, then every app that attaches to the launcher, at
 * most five, the last slot becoming More when they overflow), so on a phone
 * the console is the app. Rendered only under 768 px, and never under an
 * app's own chrome, which draws its own bar and owns the viewport
 * (`staticData.chrome === "app"`). The shell mounts it below its content
 * column; the safe area is padded here so the shell need not know.
 *
 * The shell imports this STATICALLY, so it decodes nothing through
 * `lib/apps`: an entry is the raw row's `name`, `icon`, `home` and id, which
 * is all a tab draws. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useMatches, useRouterState } from "@tanstack/react-router"
import { CircleIcon } from "lucide-react"

import { AppIcon } from "@/components/apps/icon"
import { useIsMobile } from "@/hooks/use-mobile"
import { appAttaches, appsQueryOptions } from "@/lib/api/apps"
import type { SubstrateRecord } from "@/lib/api/types"
import { cn } from "@/lib/utils"

/** One entry in the phone's bottom bar. */
export interface LauncherEntry {
  /** `app:<id>`, or one of the two fixed slots. */
  key: string
  label: string
  /** A lucide icon name, kebab-case. */
  icon: string
  /** The console route the entry opens. */
  to: string
}

/** Five is the most a thumb tells apart on one row. */
export const MAX_TABS = 5

/** An app that declares no icon. */
export const APP_ICON = "layout-grid"

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

function byId(a: SubstrateRecord, b: SubstrateRecord): number {
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
}

/** The launcher apps in id order, a `home: true` app first, so the bar and
 * the launcher list them the same way. */
export function launcherEntries(apps: SubstrateRecord[]): LauncherEntry[] {
  return [...apps]
    .filter((a) => appAttaches(a, "launcher"))
    .sort((a, b) => {
      const ha = a.properties.home === true ? 0 : 1
      const hb = b.properties.home === true ? 0 : 1
      return ha - hb || byId(a, b)
    })
    .map((a): LauncherEntry => {
      const name = a.properties.name
      const icon = a.properties.icon
      return {
        key: `app:${a.id}`,
        label: typeof name === "string" && name ? name : a.id,
        icon: typeof icon === "string" && icon ? icon : APP_ICON,
        to: `/apps/${encodeURIComponent(a.id)}`,
      }
    })
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
  const moreLit = !lit && pathname.startsWith("/apps")
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
  // A desktop never shows the bar, so it never pays for the read.
  const apps = useQuery({ ...appsQueryOptions(), enabled: isMobile })
  const entries = useMemo(
    () => launcherEntries(apps.data?.records ?? []),
    [apps.data]
  )
  if (!isMobile || underAppChrome) return null
  return <ConsoleTabBar entries={entries} pathname={pathname} />
}
