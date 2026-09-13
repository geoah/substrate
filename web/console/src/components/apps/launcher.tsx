/** `/apps`: every app that attaches to the launcher, as a tappable list, in
 * id order with a `home: true` app first on a phone. An entry whose grant
 * names a kind the registry lacks renders greyed and says which package it
 * needs, since an app may precede its package and the write never refused
 * it. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { AppWindowIcon, ChevronRightIcon, LayoutGridIcon } from "lucide-react"

import { AppChrome } from "@/components/apps/app-chrome"
import { AppIcon } from "@/components/apps/icon"
import { useTouchRoot } from "@/components/apps/touch"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useIsMobile } from "@/hooks/use-mobile"
import { appsQueryOptions } from "@/lib/api/apps"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { appSpec, missingPackages } from "@/lib/apps/app-spec"
import type { AppSpec } from "@/lib/apps/spec"
import { cn } from "@/lib/utils"

interface Entry {
  spec: AppSpec
  sub: string
  /** The packages this entry needs and the repository lacks. */
  missing: string[]
}

/** What a row says under the name when the app has no description: the
 * runtime and the kinds it reads, by their bare names. */
function subOf(spec: AppSpec): string {
  const kinds = spec.permissions.reads.kinds.map((k) => k.split("/").pop())
  const traits = spec.permissions.reads.traits.map((t) => t.split("/").pop())
  const reads = [...kinds, ...traits].filter(Boolean).slice(0, 3).join(", ")
  return [spec.runtime, reads].filter(Boolean).join(" · ")
}

export function Launcher() {
  useTouchRoot()
  const isMobile = useIsMobile()
  const registry = useQuery(kindsQueryOptions)
  const apps = useQuery(appsQueryOptions())
  const kinds = registry.data

  const entries = useMemo<Entry[]>(() => {
    if (!kinds) return []
    return (apps.data?.records ?? [])
      .map((record) => appSpec(record, kinds))
      .filter((spec) => spec.attach.includes("launcher"))
      .sort((a, b) => {
        const home = isMobile ? Number(b.home) - Number(a.home) : 0
        return home || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
      })
      .map((spec) => ({
        spec,
        sub: spec.description ?? subOf(spec),
        missing: missingPackages(spec),
      }))
  }, [kinds, apps.data, isMobile])

  const pending = registry.isPending || apps.isPending

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
              Apply a <span className="data">core/app</span> record and it lists
              here.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="flex flex-col">
          {entries.map(({ spec, sub, missing }) => {
            const greyed = missing.length > 0
            return (
              <li key={spec.id} className="border-b">
                <Link
                  to="/apps/$id"
                  params={{ id: spec.id }}
                  className={cn(
                    "flex min-h-14 items-center gap-3 px-4 py-2 select-none [-webkit-touch-callout:none] hover:bg-muted/40 active:bg-muted/60",
                    greyed && "opacity-60"
                  )}
                >
                  <span className="flex size-10 shrink-0 items-center justify-center rounded-xl border bg-muted/40 text-foreground">
                    <AppIcon
                      name={spec.icon}
                      fallback={AppWindowIcon}
                      className="size-5"
                    />
                  </span>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-[0.95rem] font-medium">
                      {spec.name}
                    </span>
                    <span
                      className={cn(
                        "truncate text-xs text-muted-foreground",
                        greyed && "text-warning"
                      )}
                    >
                      {greyed ? `needs ${missing.join(", ")}` : sub}
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
