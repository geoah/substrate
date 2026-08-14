import { Fragment, useEffect, useState } from "react"
import { Link, Outlet, useRouterState } from "@tanstack/react-router"
import { SearchIcon } from "lucide-react"

import { AppSidebar } from "@/components/app-sidebar"
import { CommandMenu } from "@/components/command-menu"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { Button } from "@/components/ui/button"
import { Kbd } from "@/components/ui/kbd"
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { CORE_AUTHORITY } from "@/lib/api/http"
import { MR_PLURAL } from "@/lib/api/mergerequests"

interface Crumb {
  label: string
  /** A crumb with an address links back to it (the type crumb on a record
   * page); label-only crumbs render inert. */
  to?: string
  /** Path segments (group, type, record id) speak the data voice — mono,
   * like every id/path in the console. */
  mono?: boolean
}

/** Route depth as crumbs: fixed pages are one crumb; the data routes read
 * Data → group → type → record. */
function crumbsFor(pathname: string): Crumb[] {
  if (pathname === "/") return [{ label: "Overview" }]
  if (pathname.startsWith("/changelog")) return [{ label: "Changelog" }]
  if (pathname.startsWith("/registry/connections/")) {
    const id = decodeURIComponent(
      pathname.slice("/registry/connections/".length)
    )
    return [
      { label: "Registry", to: "/registry" },
      { label: "Connections", to: "/registry/connections" },
      { label: id, mono: true },
    ]
  }
  if (pathname.startsWith("/registry/connections")) {
    return [{ label: "Registry", to: "/registry" }, { label: "Connections" }]
  }
  if (pathname.startsWith("/registry/")) {
    const id = decodeURIComponent(pathname.slice("/registry/".length))
    return [
      { label: "Registry", to: "/registry" },
      { label: id, mono: true },
    ]
  }
  if (pathname.startsWith("/registry")) return [{ label: "Registry" }]
  if (pathname.startsWith("/merge-requests/")) {
    const id = decodeURIComponent(pathname.slice("/merge-requests/".length))
    // The queue is the kind's own collection — there is no bespoke one — so
    // the parent crumb walks back into the data tree.
    return [
      { label: "Data" },
      { label: CORE_AUTHORITY, to: `/data/${CORE_AUTHORITY}`, mono: true },
      {
        label: MR_PLURAL,
        to: `/data/${CORE_AUTHORITY}/${MR_PLURAL}`,
        mono: true,
      },
      { label: id, mono: true },
    ]
  }
  if (pathname.startsWith("/actors/")) {
    const id = decodeURIComponent(pathname.slice("/actors/".length))
    return [{ label: "Actors" }, { label: id, mono: true }]
  }
  if (pathname.startsWith("/data/")) {
    const [group, type, id] = pathname
      .slice("/data/".length)
      .split("/")
      .map((s) => decodeURIComponent(s ?? ""))
    const crumbs: Crumb[] = [
      { label: "Data" },
      {
        label: group ?? "",
        to: type ? `/data/${group}` : undefined,
        mono: true,
      },
    ]
    if (type) {
      crumbs.push({
        label: type,
        to: id ? `/data/${group}/${type}` : undefined,
        mono: true,
      })
    }
    if (id) crumbs.push({ label: id, mono: true })
    return crumbs
  }
  return []
}

function ShellBreadcrumb() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const crumbs = crumbsFor(pathname)
  return (
    <Breadcrumb>
      <BreadcrumbList>
        {crumbs.map((crumb, i) => (
          <Fragment key={`${crumb.label}-${i}`}>
            {i > 0 && <BreadcrumbSeparator />}
            <BreadcrumbItem>
              {i === crumbs.length - 1 ? (
                <BreadcrumbPage className={crumb.mono ? "data" : undefined}>
                  {crumb.label}
                </BreadcrumbPage>
              ) : crumb.to ? (
                <BreadcrumbLink
                  className={crumb.mono ? "data" : undefined}
                  render={<Link to={crumb.to} />}
                >
                  {crumb.label}
                </BreadcrumbLink>
              ) : (
                <BreadcrumbLink className={crumb.mono ? "data" : undefined}>
                  {crumb.label}
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
          </Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  )
}

export function AppShell() {
  const [commandOpen, setCommandOpen] = useState(false)

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "k" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault()
        setCommandOpen((open) => !open)
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [])

  return (
    <SidebarProvider>
      <TooltipProvider delay={250}>
        <AppSidebar />
        <SidebarInset className="flex h-svh min-w-0 flex-col overflow-hidden">
          {/* Rule 2 (GUIDE §5): no separator between the trigger and the
            breadcrumb — the gap carries the seam. */}
          <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger className="-ml-1" />
            <ShellBreadcrumb />
            <div className="ml-auto flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                className="h-8 w-56 justify-start gap-2 px-2.5 font-normal text-muted-foreground"
                onClick={() => setCommandOpen(true)}
              >
                <SearchIcon className="size-3.5" />
                <span>Search…</span>
                <Kbd className="ml-auto">⌘K</Kbd>
              </Button>
            </div>
          </header>
          <div className="flex min-h-0 flex-1 flex-col overflow-auto">
            <Outlet />
          </div>
        </SidebarInset>
        <CommandMenu open={commandOpen} onOpenChange={setCommandOpen} />
      </TooltipProvider>
    </SidebarProvider>
  )
}
