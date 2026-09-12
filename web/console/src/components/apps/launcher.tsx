/** `/apps`: every app, then every view that attaches to the launcher, as a
 * tappable list. An entry whose views name a kind the registry lacks renders
 * greyed and says which package it needs, since a view may precede its
 * package and the write never refused it. */

import { useMemo, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { AppWindowIcon, ChevronRightIcon, LayoutGridIcon } from "lucide-react"

import { AppChrome } from "@/components/apps/app-chrome"
import { AppIcon, LAYOUT_ICONS } from "@/components/apps/icon"
import { useTouchRoot } from "@/components/apps/touch"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { appsQueryOptions, viewsQueryOptions } from "@/lib/api/apps"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { appSpec } from "@/lib/apps/app-spec"
import { packageOf, viewSpec } from "@/lib/apps/view-spec"
import { kindByIdentity } from "@/lib/definition"
import { cn } from "@/lib/utils"

interface Entry {
  key: string
  to: "/apps/$id" | "/views/$id"
  id: string
  name: string
  icon: ReactNode
  sub: string
  /** The packages this entry needs and the repository lacks. */
  missing: string[]
}

export function Launcher() {
  useTouchRoot()
  const registry = useQuery(kindsQueryOptions)
  const views = useQuery(viewsQueryOptions())
  const apps = useQuery(appsQueryOptions())
  const kinds = registry.data

  const entries = useMemo<Entry[]>(() => {
    if (!kinds) return []
    const viewRecords = views.data?.records ?? []
    const specs = new Map(viewRecords.map((v) => [v.id, viewSpec(v, kinds)]))
    const missingOf = (ids: string[]) => [
      ...new Set(
        ids.flatMap((id) => {
          const s = specs.get(id)
          return s?.kind && !kindByIdentity(kinds, s.kind)
            ? [packageOf(s.kind)]
            : []
        })
      ),
    ]
    const appEntries = (apps.data?.records ?? []).map((a): Entry => {
      const s = appSpec(a, viewRecords, kinds)
      return {
        key: `app:${a.id}`,
        to: "/apps/$id",
        id: a.id,
        name: s.name,
        icon: (
          <AppIcon name={s.icon} fallback={AppWindowIcon} className="size-5" />
        ),
        sub:
          s.description ??
          `${s.screens.length} ${s.screens.length === 1 ? "screen" : "screens"}`,
        missing: missingOf(s.screens.map((sc) => sc.view)),
      }
    })
    const viewEntries = viewRecords.flatMap((v): Entry[] => {
      const s = specs.get(v.id)
      if (!s || !s.attach.includes("launcher")) return []
      const Icon = LAYOUT_ICONS[s.layout]
      const k = s.kind ? kindByIdentity(kinds, s.kind) : undefined
      return [
        {
          key: `view:${v.id}`,
          to: "/views/$id",
          id: v.id,
          name: s.name,
          icon: <Icon className="size-5" />,
          sub:
            s.description ??
            [s.layout, k?.name ?? s.trait?.split("/").pop()]
              .filter(Boolean)
              .join(" · "),
          missing: missingOf([v.id]),
        },
      ]
    })
    return [...appEntries, ...viewEntries]
  }, [kinds, views.data, apps.data])

  const pending = registry.isPending || views.isPending || apps.isPending

  return (
    <AppChrome title="Apps">
      {pending ? (
        <div className="flex flex-col">
          {Array.from({ length: 5 }, (_, i) => (
            <div key={i} className="flex items-center gap-3 border-b px-4 py-3">
              <Skeleton className="size-10 rounded-xl" />
              <div className="flex flex-1 flex-col gap-2">
                <Skeleton className="h-4 w-1/2" />
                <Skeleton className="h-3 w-1/3" />
              </div>
            </div>
          ))}
        </div>
      ) : entries.length === 0 ? (
        <Empty className="py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LayoutGridIcon />
            </EmptyMedia>
            <EmptyTitle>No apps yet</EmptyTitle>
            <EmptyDescription>
              Apply a <span className="data">core/view</span> record with{" "}
              <span className="data">attach: [launcher]</span>, or a{" "}
              <span className="data">core/app</span>, and it lists here.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="flex flex-col">
          {entries.map((entry) => {
            const greyed = entry.missing.length > 0
            return (
              <li key={entry.key} className="border-b">
                <Link
                  to={entry.to}
                  params={{ id: entry.id }}
                  className={cn(
                    "flex min-h-14 items-center gap-3 px-4 py-2 select-none [-webkit-touch-callout:none] hover:bg-muted/40 active:bg-muted/60",
                    greyed && "opacity-60"
                  )}
                >
                  <span className="flex size-10 shrink-0 items-center justify-center rounded-xl border bg-muted/40 text-foreground">
                    {entry.icon}
                  </span>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-[0.95rem] font-medium">
                      {entry.name}
                    </span>
                    <span
                      className={cn(
                        "truncate text-xs text-muted-foreground",
                        greyed && "text-warning"
                      )}
                    >
                      {greyed ? `needs ${entry.missing.join(", ")}` : entry.sub}
                    </span>
                  </span>
                  <ChevronRightIcon className="size-4 shrink-0 text-muted-foreground" />
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </AppChrome>
  )
}
