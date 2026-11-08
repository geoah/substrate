/** Applets (docs/plans/applets.md): the repository's applets as cards, one applet
 * in the shell at `/applets/$id`, and the same applet on its own at `/a/$id`.
 * Moss builds them; the owner opens them here or full screen on a phone. */

import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { MaximizeIcon, SparklesIcon } from "lucide-react"

import {
  AppletTile,
  ShellAppletView,
  StandaloneAppletView,
} from "@/components/applets/applet-view"
import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import { buttonVariants } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { appletsQueryOptions } from "@/lib/api/applets"
import type { SubstrateRecord } from "@/lib/api/types"
import {
  appletLocation,
  appletLocationSearch,
  type AppletLocation,
} from "@/lib/applet-location"
import { appletRoute, standaloneAppletRoute } from "@/router"

/** What the empty page suggests asking Moss. */
const FIRST_APP_PROMPT = "Make me an applet for my books"

function text(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined
}

function AppletCard({
  applet,
  technical,
}: {
  applet: SubstrateRecord
  technical: boolean
}) {
  const name = text(applet.properties.name) ?? applet.id
  const description = text(applet.properties.description)
  return (
    <div
      data-slot="applet-card"
      className="flex flex-col rounded-[10px] border bg-background transition-colors hover:border-border-strong"
    >
      <Link
        to="/applets/$id"
        params={{ id: applet.id }}
        className="flex flex-1 flex-col gap-2 rounded-t-[10px] p-3.5 text-foreground no-underline outline-none hover:bg-panel focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <div className="flex items-center gap-2.5">
          <AppletTile icon={text(applet.properties.icon)} />
          <span className="min-w-0 flex-1 font-semibold break-words">
            {name}
          </span>
        </div>
        {description && (
          <p className="line-clamp-3 text-[13px] text-muted-foreground">
            {description}
          </p>
        )}
        {technical && <IdText value={applet.id} />}
      </Link>
      <div className="flex items-center border-t px-2 py-1.5">
        <Link
          to="/a/$id"
          params={{ id: applet.id }}
          className={buttonVariants({ variant: "ghost", size: "sm" })}
        >
          <MaximizeIcon />
          Open full screen
        </Link>
      </div>
    </div>
  )
}

export function AppletsPage() {
  const applets = useQuery(appletsQueryOptions())
  const [technical] = useTechnicalDetails()
  const records = applets.data?.records ?? []
  return (
    <TablePage className="pb-16">
      <PageHeader
        title="Applets"
        stage="alpha"
        description="Small applets over your records. Open one here, or on its own, full screen, on your phone."
      />
      {applets.isPending ? (
        <div className="mt-8 grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-3">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="h-32 rounded-[10px]" />
          ))}
        </div>
      ) : applets.error ? (
        <p className="mt-8 text-sm text-destructive">
          Couldn’t load your applets: {applets.error.message}
        </p>
      ) : !records.length ? (
        <Empty className="mt-8 border border-dashed">
          <EmptyHeader>
            <EmptyTitle>No applets yet</EmptyTitle>
            <EmptyDescription>
              Ask Moss to build one: “{FIRST_APP_PROMPT.toLowerCase()}”.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Link
              to="/agents"
              search={{ prompt: FIRST_APP_PROMPT }}
              className={buttonVariants({ variant: "outline" })}
            >
              <SparklesIcon />
              Ask Moss
            </Link>
          </EmptyContent>
        </Empty>
      ) : (
        <div className="mt-8 grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-3">
          {records.map((applet) => (
            <AppletCard key={applet.id} applet={applet} technical={technical} />
          ))}
        </div>
      )}
    </TablePage>
  )
}

export function AppletPage() {
  const { id } = appletRoute.useParams()
  const search = appletRoute.useSearch()
  const navigate = useNavigate()
  return (
    <ShellAppletView
      key={id}
      appletId={id}
      location={appletLocation(search)}
      navigate={(to: AppletLocation, { replace }) =>
        void navigate({
          to: "/applets/$id",
          params: { id },
          search: appletLocationSearch(to),
          replace,
        })
      }
    />
  )
}

export function StandaloneAppletPage() {
  const { id } = standaloneAppletRoute.useParams()
  const search = standaloneAppletRoute.useSearch()
  const navigate = useNavigate()
  return (
    <StandaloneAppletView
      key={id}
      appletId={id}
      location={appletLocation(search)}
      navigate={(to: AppletLocation, { replace }) =>
        void navigate({
          to: "/a/$id",
          params: { id },
          search: appletLocationSearch(to),
          replace,
        })
      }
    />
  )
}
