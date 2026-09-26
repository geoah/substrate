import { Fragment, useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, Outlet, useRouterState } from "@tanstack/react-router"
import { SearchIcon } from "lucide-react"

import { NavigationProvider } from "@/components/console-preferences"
import { AppSidebar } from "@/components/app-sidebar"
import { CommandMenu } from "@/components/command-menu"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { SectionBoundary } from "@/components/page-error"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { Button } from "@/components/ui/button"
import { Kbd } from "@/components/ui/kbd"
import { SidebarInset, SidebarTrigger } from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  actorIdentity,
  providerInfo,
  providerOfKind,
  PROVIDERS_AUTHORITY,
} from "@/lib/actor-identity"
import { CR_NAME } from "@/lib/api/changerequests"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME, joinKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { MR_NAME } from "@/lib/api/mergerequests"
import { collectionSource } from "@/lib/collections"
import { kindByIdentity } from "@/lib/definition"
import {
  displayName,
  displayPlural,
  lowerFirst,
  untitled,
} from "@/lib/kind-names"
import { recordTitleQueryOptions } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"

export interface Crumb {
  label: string
  /** A crumb with an address links back to it; label-only crumbs render
   * inert. */
  to?: string
  /** An identifier (a reference segment, an id) set in the copyable voice. */
  mono?: boolean
  /** The kind whose glyph sits beside the label. */
  kind?: string
  /** The provider whose badge sits beside the label. */
  provider?: string
  /** The crumb names a record: the renderer reads its title, and `label` is
   * what shows until it lands. */
  record?: { kind: string; id: string }
}

const decode = (s: string) => {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

/** The crumbs a collection reads as: "Your data / Tasks" or
 * "From Google / Contacts" in everyday words, the reference spelled segment
 * by segment in technical mode. */
function collectionCrumbs(
  authority: string,
  pkg: string,
  name: string,
  technical: boolean,
  linkCollection: boolean
): Crumb[] {
  const kind = joinKind(authority, pkg, name)
  const collection = `/data/${authority}/${pkg}/${name}`
  if (technical) {
    return [
      { label: authority, to: `/data/${authority}`, mono: true },
      { label: pkg, to: `/data/${authority}/${pkg}`, mono: true },
      {
        label: name,
        kind,
        mono: true,
        ...(linkCollection && { to: collection }),
      },
    ]
  }
  const provider = providerOfKind(kind)?.key
  return [
    {
      label: collectionSource(kind),
      to: "/data",
      ...(provider && { provider }),
    },
    {
      label: displayPlural(kind),
      kind,
      ...(linkCollection && { to: collection }),
    },
  ]
}

/** Route depth as crumbs. Pure over the path and the reader's mode, so every
 * route the router serves can be checked to read as where it sits. */
// eslint-disable-next-line react-refresh/only-export-components -- a pure route reading, exported for its test
export function crumbsFor(pathname: string, technical = false): Crumb[] {
  const [head, ...rest] = pathname.split("/").filter(Boolean).map(decode)
  switch (head) {
    case undefined:
      return [{ label: "Home" }]
    case "history":
    case "changelog":
      return [{ label: "History" }]
    case "search":
      return [{ label: "Search" }]
    // A bundle's own settings, the account and its tokens all redirect: the
    // first to its provider, the others here.
    case "settings":
    case "account":
      return [{ label: "Settings" }]
    case "agents":
      if (!rest.length) return [{ label: "Agents" }]
      return [
        { label: "Agents", to: "/agents" },
        technical
          ? { label: rest.join("/"), mono: true }
          : { label: rest.join("/") },
      ]
    case "tools": {
      if (!rest.length) return [{ label: "Tools" }]
      return [
        { label: "Tools", to: "/tools" },
        technical
          ? { label: rest.join("/"), mono: true }
          : {
              // A tool is a function: its plain name is its actor's.
              label: actorIdentity(`function:${rest.join(":")}`).name,
            },
      ]
    }
    case "providers": {
      if (!rest.length) return [{ label: "Providers" }]
      const [authority, pkg] = rest
      if (technical || authority !== PROVIDERS_AUTHORITY || !pkg) {
        return [
          { label: "Providers", to: "/providers" },
          { label: rest.join("/"), mono: true },
        ]
      }
      const provider = providerInfo(pkg)
      return [
        { label: "Providers", to: "/providers" },
        { label: provider.name, provider: provider.key },
      ]
    }
    case "merge-requests":
    case "change-requests": {
      // The queue is the kind's own collection, so the parent crumbs walk back
      // into the data tree.
      const name = head === "merge-requests" ? MR_NAME : CR_NAME
      const id = rest[0] ?? ""
      return [
        ...collectionCrumbs(
          CORE_AUTHORITY,
          CORE_PACKAGE_NAME,
          name,
          technical,
          true
        ),
        {
          label: technical
            ? id
            : untitled(joinKind(CORE_AUTHORITY, CORE_PACKAGE_NAME, name)),
          ...(technical && { mono: true }),
          record: {
            kind: joinKind(CORE_AUTHORITY, CORE_PACKAGE_NAME, name),
            id,
          },
        },
      ]
    }
    case "actors": {
      const actor = rest.join("/")
      return [
        { label: "History", to: "/history" },
        technical
          ? { label: actor, mono: true }
          : { label: actorIdentity(actor).name },
      ]
    }
    case "data": {
      const [authority, pkg, name, id, action] = rest
      if (!authority) return [{ label: "All data" }]
      if (!pkg) {
        return [
          { label: "All data", to: "/data" },
          { label: authority, mono: true },
        ]
      }
      if (!name) {
        return [
          { label: "All data", to: "/data" },
          { label: authority, to: `/data/${authority}`, mono: true },
          { label: pkg, mono: true },
        ]
      }
      const crumbs = collectionCrumbs(
        authority,
        pkg,
        name,
        technical,
        Boolean(id)
      )
      if (!id) return crumbs
      const kind = joinKind(authority, pkg, name)
      if (id === "new") {
        return [...crumbs, { label: `New ${lowerFirst(displayName(kind))}` }]
      }
      crumbs.push({
        label: technical ? id : untitled(kind),
        ...(technical && { mono: true }),
        record: { kind, id },
        ...(action && { to: `/data/${authority}/${pkg}/${name}/${id}` }),
      })
      if (action === "edit") crumbs.push({ label: "Edit" })
      return crumbs
    }
    default:
      return []
  }
}

/** A record crumb reads as its title; technical mode keeps the id, which is
 * what the reader came to copy. */
function RecordCrumbLabel({ crumb }: { crumb: Crumb }) {
  const [technical] = useTechnicalDetails()
  const kinds = useQuery(kindsQueryOptions)
  const record = crumb.record!
  const known = Boolean(kindByIdentity(kinds.data ?? [], record.kind))
  const title = useQuery({
    ...recordTitleQueryOptions(record.kind, record.id),
    enabled: known && !technical,
  })
  if (technical) return <>{crumb.label}</>
  return <>{title.data || crumb.label}</>
}

function CrumbBody({ crumb }: { crumb: Crumb }) {
  return (
    <>
      {crumb.kind && <KindGlyph kind={crumb.kind} size="xs" />}
      {crumb.provider && <ProviderBadge provider={crumb.provider} size="xs" />}
      <span
        className={cn(
          "min-w-0 truncate",
          crumb.mono && "font-mono text-[12px]"
        )}
      >
        {crumb.record ? <RecordCrumbLabel crumb={crumb} /> : crumb.label}
      </span>
    </>
  )
}

function ShellBreadcrumb() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const [technical] = useTechnicalDetails()
  const crumbs = crumbsFor(pathname, technical)
  return (
    <nav aria-label="Breadcrumb" className="min-w-0 flex-1">
      <ol className="flex min-w-0 items-center gap-1 text-[13px] text-muted-foreground">
        {crumbs.map((crumb, i) => {
          const last = i === crumbs.length - 1
          return (
            <Fragment key={`${crumb.label}-${i}`}>
              {i > 0 && (
                <li aria-hidden className="shrink-0 text-faint-deco">
                  /
                </li>
              )}
              <li className="flex min-w-0 items-center">
                {crumb.to && !last ? (
                  <Link
                    to={crumb.to}
                    className="inline-flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-0.5 whitespace-nowrap text-inherit no-underline hover:bg-hover hover:text-foreground"
                  >
                    <CrumbBody crumb={crumb} />
                  </Link>
                ) : (
                  <span
                    aria-current={last ? "page" : undefined}
                    className={cn(
                      "inline-flex min-w-0 items-center gap-1.5 px-1.5 py-0.5 whitespace-nowrap",
                      last && "text-foreground"
                    )}
                  >
                    <CrumbBody crumb={crumb} />
                  </span>
                )}
              </li>
            </Fragment>
          )
        })}
      </ol>
    </nav>
  )
}

export function AppShell() {
  const [commandOpen, setCommandOpen] = useState(false)
  // The shell outlives every page, so a part of it that failed tries again
  // on the next address.
  const pathname = useRouterState({ select: (s) => s.location.pathname })

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
    <NavigationProvider>
      <TooltipProvider delay={250}>
        <SectionBoundary name="The sidebar" resetKey={pathname}>
          <AppSidebar onSearch={() => setCommandOpen(true)} />
        </SectionBoundary>
        <SidebarInset className="flex h-svh min-w-0 flex-col overflow-hidden">
          <header className="flex h-11 shrink-0 items-center gap-2 px-3 md:px-4">
            <SidebarTrigger className="-ml-1 text-muted-foreground" />
            <SectionBoundary name="Where you are" resetKey={pathname}>
              <ShellBreadcrumb />
            </SectionBoundary>
            <Button
              variant="outline"
              size="sm"
              className="hidden h-7 w-48 shrink-0 justify-start gap-2 px-2 font-normal text-faint sm:inline-flex"
              onClick={() => setCommandOpen(true)}
            >
              <SearchIcon className="size-3.5" />
              <span>Search…</span>
              <Kbd className="ml-auto">⌘K</Kbd>
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Search"
              className="sm:hidden"
              onClick={() => setCommandOpen(true)}
            >
              <SearchIcon />
            </Button>
          </header>
          <div className="flex min-h-0 flex-1 flex-col overflow-auto">
            <Outlet />
          </div>
        </SidebarInset>
        <SectionBoundary name="Search" resetKey={pathname}>
          <CommandMenu open={commandOpen} onOpenChange={setCommandOpen} />
        </SectionBoundary>
      </TooltipProvider>
    </NavigationProvider>
  )
}
