/** An applet, drawn: the runtime's surface under a header that names the
 * screen and the applet, in one of two frames. In the shell the applet reads as a
 * page, with links to its full-screen address and to its definition; the
 * standalone frame is the whole viewport, phone first, a title bar and the
 * screen. The screen draws no title or back button of its own: the frame
 * gives every screen both. */

import { createElement, useEffect, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useCanGoBack, useRouter } from "@tanstack/react-router"
import {
  ArrowLeftIcon,
  ChevronLeftIcon,
  CircleAlertIcon,
  CodeIcon,
  HouseIcon,
  MaximizeIcon,
  XIcon,
} from "lucide-react"

import { A2UISurface } from "@/components/a2ui/surface"
import { launcherIcon } from "@/components/a2ui/icons"
import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { pageTitleClass } from "@/components/identity/page-title"
import { Button, buttonVariants } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "@/components/ui/toast"
import {
  useAppletRuntime,
  type AppletRuntime,
} from "@/hooks/use-applet-runtime"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { splitKind } from "@/lib/api/http"
import type { ApiError, ProblemDetail } from "@/lib/api/types"
import {
  appletLocationSearch,
  type AppletLocation,
} from "@/lib/applet-location"
import { recordQueryOptions } from "@/lib/api/records"
import { APPLET_KIND } from "@/lib/core-kinds"
import { recordTitleQueryOptions } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"

type Navigate = (to: AppletLocation, options: { replace: boolean }) => void

export function AppletTile({
  icon,
  size = "md",
}: {
  icon?: string
  size?: "sm" | "md" | "lg"
}) {
  return (
    <span
      aria-hidden
      className={cn(
        "grid shrink-0 place-items-center rounded-lg bg-muted text-foreground",
        size === "sm" && "size-7 [&>svg]:size-4",
        size === "md" && "size-9 [&>svg]:size-[18px]",
        size === "lg" && "size-11 rounded-xl [&>svg]:size-[22px]"
      )}
    >
      {createElement(launcherIcon(icon), { strokeWidth: 1.8 })}
    </span>
  )
}

/** A refusal in the owner's words: the problems the server named, each
 * where it was found, or its message without the error chain's
 * `substrate:` prefixes. */
function explain(error: ApiError): {
  summary?: string
  details: ProblemDetail[]
} {
  if (error.code === "network")
    return {
      summary: "The substrate couldn’t be reached. Check the connection.",
      details: [],
    }
  if (error.problemDetails.length) return { details: error.problemDetails }
  if (error.problems.length)
    return {
      details: error.problems.map((message) => ({ path: "", message })),
    }
  const text = error.message.replace(/\bsubstrate: /g, "").trim()
  return {
    summary: text
      ? text.charAt(0).toUpperCase() + text.slice(1)
      : "Something went wrong.",
    details: [],
  }
}

function Details({ details }: { details: ProblemDetail[] }) {
  if (!details.length) return null
  return (
    <ul className="space-y-0.5 text-[13px] text-muted-foreground">
      {details.map((p, i) => (
        <li key={i} className="[overflow-wrap:anywhere]">
          {p.path && (
            <>
              <span className="font-mono text-xs text-foreground">
                {p.path}
              </span>{" "}
            </>
          )}
          {p.message}
        </li>
      ))}
    </ul>
  )
}

/** A refused action, pinned to the bottom of the view, where the owner who
 * tapped is still looking, until they dismiss it or act again. */
function Refusal({
  error,
  onDismiss,
}: {
  error: ApiError
  onDismiss: () => void
}) {
  const { summary, details } = explain(error)
  return (
    <div
      role="alert"
      data-slot="applet-refusal"
      className="sticky bottom-[max(env(safe-area-inset-bottom),1rem)] z-10 flex items-start gap-2.5 rounded-lg border border-destructive/40 bg-popover px-3 py-2.5 text-sm text-popover-foreground shadow-lg"
    >
      <CircleAlertIcon className="mt-0.5 size-4 shrink-0 text-destructive" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="font-medium text-foreground">That didn’t go through</p>
        {summary && (
          <p className="[overflow-wrap:anywhere] text-muted-foreground">
            {summary}
          </p>
        )}
        <Details details={details} />
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Dismiss"
        className="pointer-coarse:size-10"
        onClick={onDismiss}
      >
        <XIcon />
      </Button>
    </div>
  )
}

/** What the renderer could not draw, quiet and folded: it is for whoever
 * fixes the applet (Moss, or the owner reading its definition). */
function Problems({ problems }: { problems: string[] }) {
  if (!problems.length) return null
  return (
    <details className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
      <summary className="cursor-pointer select-none">
        {problems.length === 1
          ? "1 part of this screen couldn’t be drawn"
          : `${problems.length} parts of this screen couldn’t be drawn`}
      </summary>
      <ul className="mt-1.5 list-disc space-y-0.5 pl-4 font-mono [overflow-wrap:anywhere]">
        {problems.map((p) => (
          <li key={p}>{p}</li>
        ))}
      </ul>
    </details>
  )
}

/** A thin bar while the screen is drawn again over what it shows. */
function Refreshing({ className }: { className: string }) {
  return (
    <div
      role="progressbar"
      aria-label="Updating"
      data-slot="applet-refreshing"
      className={cn(
        "pointer-events-none absolute inset-x-0 h-0.5 animate-pulse bg-primary/60",
        className
      )}
    />
  )
}

function Loading() {
  return (
    <div aria-busy className="flex flex-col gap-3" data-slot="applet-loading">
      <Skeleton className="h-9 w-full rounded-lg" />
      <Skeleton className="h-7 w-2/3 rounded-lg" />
      {Array.from({ length: 4 }, (_, i) => (
        <Skeleton key={i} className="h-11 w-full rounded-lg" />
      ))}
    </div>
  )
}

function Missing({ appletId }: { appletId: string }) {
  return (
    <div className="flex flex-col items-start gap-3 py-6">
      <h1 className={pageTitleClass()}>No applet named “{appletId}”</h1>
      <p className="text-muted-foreground">
        It may have been deleted or renamed.
      </p>
      <Link to="/applets" className={buttonVariants({ variant: "outline" })}>
        See your applets
      </Link>
    </div>
  )
}

/** A screen that could not be drawn while nothing is: the frame keeps its
 * header, and the way out is the applet's start, opened as new. */
function Failure({
  error,
  location,
  start,
  runtime,
}: {
  error: ApiError
  location: AppletLocation
  start?: string
  runtime: AppletRuntime
}) {
  const { summary, details } = explain(error)
  const onStart = !location.screen || location.screen === start
  return (
    <div
      data-slot="applet-failure"
      className="flex flex-col items-start gap-3 py-2"
    >
      <h2 className="text-lg font-semibold">
        {onStart ? "This applet couldn’t open" : "This screen couldn’t open"}
      </h2>
      {summary && (
        <p className="[overflow-wrap:anywhere] text-muted-foreground">
          {summary}
        </p>
      )}
      <Details details={details} />
      <div className="flex flex-wrap gap-2 pt-1">
        {!onStart && (
          <Button className="pointer-coarse:h-10" onClick={runtime.home}>
            <HouseIcon />
            Back to the start
          </Button>
        )}
        <Button
          variant="outline"
          className="pointer-coarse:h-10"
          onClick={runtime.retry}
        >
          Try again
        </Button>
      </div>
    </div>
  )
}

/** The screen, its problems and refusals, and the question an action asks:
 * everything both frames draw. */
function Screen({
  runtime,
  progress = true,
}: {
  runtime: AppletRuntime
  /** Draw the refresh bar above the screen; a frame with a bar of its own
   * says no. */
  progress?: boolean
}) {
  return (
    <div className="relative flex min-w-0 flex-col gap-4">
      {progress && runtime.refreshing && <Refreshing className="-top-3" />}
      <A2UISurface
        store={runtime.store}
        surfaceId={runtime.surfaceId}
        onAction={runtime.act}
        onProblem={runtime.addProblem}
        busy={runtime.busy}
        destructive={runtime.header?.destructive}
      />
      <Problems problems={runtime.problems} />
      {runtime.refusal && (
        <Refusal error={runtime.refusal} onDismiss={runtime.dismissRefusal} />
      )}
      {runtime.confirm && (
        <ConfirmDialog
          title={runtime.confirm.question}
          confirm={runtime.confirm.label || "Continue"}
          destructive={runtime.confirm.danger}
          pending={runtime.busy}
          onConfirm={() => runtime.answer(true)}
          onClose={() => runtime.answer(false)}
        />
      )}
    </div>
  )
}

/** Back walks the history. With none behind it (a bookmark, a link opened
 * fresh) a screen other than the start goes to the start, and the start
 * has nowhere to go. */
function useBack(
  location: AppletLocation,
  navigate: Navigate,
  start: string | undefined
): (() => void) | undefined {
  const router = useRouter()
  const canGoBack = useCanGoBack()
  if (canGoBack) return () => router.history.back()
  if (!start || !location.screen || location.screen === start) return undefined
  return () => navigate({ screen: start }, { replace: true })
}

function useRuntime(
  appletId: string,
  location: AppletLocation,
  navigate: Navigate
): AppletRuntime {
  return useAppletRuntime({
    appletId,
    location,
    navigate,
    // A render's notice says the screen asked for did not open, which is
    // news, not a success.
    onNotice: (notice, from) =>
      toast.add({
        type: from === "action" ? "success" : "info",
        title: notice,
      }),
  })
}

/** What the applet is called and its icon: what its last frame said, else,
 * once a screen fails before any frame lands, its record's, else its id. */
function useAppletIdentity(
  appletId: string,
  runtime: AppletRuntime
): { name: string; icon?: string } {
  const header = runtime.header
  const failure = runtime.failure
  const { authority, pkg, name: kindName } = splitKind(APPLET_KIND)
  const record = useQuery({
    ...recordQueryOptions(authority, pkg, kindName, appletId),
    enabled: !header && Boolean(failure) && failure?.status !== 404,
  })
  if (header) return { name: header.name, icon: header.icon }
  const { name, icon } = record.data?.properties ?? {}
  return {
    name: typeof name === "string" && name ? name : appletId,
    icon: typeof icon === "string" ? icon : undefined,
  }
}

/** The applet as a console page, at `/applets/$id`. */
export function ShellAppletView({
  appletId,
  location,
  navigate,
}: {
  appletId: string
  location: AppletLocation
  navigate: Navigate
}) {
  const runtime = useRuntime(appletId, location, navigate)
  const header = runtime.header
  const back = useBack(location, navigate, header?.start)
  const { name, icon } = useAppletIdentity(appletId, runtime)
  // The breadcrumb reads the applet's record title; the frame already said
  // it, so the crumb never waits on a read of its own.
  const queryClient = useQueryClient()
  useEffect(() => {
    if (!header?.name) return
    queryClient.setQueryData(
      recordTitleQueryOptions(APPLET_KIND, appletId).queryKey,
      header.name
    )
  }, [queryClient, appletId, header?.name])
  const { authority, pkg, name: kindName } = splitKind(APPLET_KIND)
  const failure = runtime.failure
  const pageHeader = (title: string, meta?: ReactNode) => (
    <PageHeader
      glyph={<AppletTile icon={icon} size="lg" />}
      title={title}
      meta={meta}
      actions={
        <>
          <Link
            to="/a/$id"
            params={{ id: appletId }}
            search={appletLocationSearch(location)}
            className={buttonVariants({ variant: "outline" })}
          >
            <MaximizeIcon />
            Open full screen
          </Link>
          <Link
            to="/data/$authority/$pkg/$name/$id"
            params={{ authority, pkg, name: kindName, id: appletId }}
            className={buttonVariants({ variant: "ghost" })}
          >
            <CodeIcon />
            Definition
          </Link>
        </>
      }
    />
  )
  return (
    <DocPage className="flex flex-col gap-5 pb-16">
      {back && (
        <Button
          variant="ghost"
          size="sm"
          className="-ml-2 self-start text-muted-foreground pointer-coarse:h-10"
          onClick={back}
        >
          <ArrowLeftIcon />
          Back
        </Button>
      )}
      {failure?.status === 404 ? (
        <Missing appletId={appletId} />
      ) : failure ? (
        <>
          {pageHeader(name)}
          <Failure
            error={failure}
            location={location}
            start={header?.start}
            runtime={runtime}
          />
        </>
      ) : runtime.loading || !header ? (
        <Loading />
      ) : (
        <>
          {pageHeader(
            header.title,
            header.name !== header.title && (
              <span>
                <span className="sr-only">Applet: </span>
                {header.name}
              </span>
            )
          )}
          <Screen runtime={runtime} />
        </>
      )}
    </DocPage>
  )
}

/** The applet on its own, at `/a/$id`: the whole viewport, a title bar, the
 * screen. */
export function StandaloneAppletView({
  appletId,
  location,
  navigate,
}: {
  appletId: string
  location: AppletLocation
  navigate: Navigate
}) {
  const runtime = useRuntime(appletId, location, navigate)
  const header = runtime.header
  const back = useBack(location, navigate, header?.start)
  const { name, icon } = useAppletIdentity(appletId, runtime)
  const failure = runtime.failure
  const named = Boolean(header) || (failure && failure.status !== 404)
  const title = header && !failure ? header.title : name
  // The screen's title, and the applet's when that says something more.
  useDocumentTitle(
    !named ? "Applet" : title === name ? name : `${title} · ${name}`
  )
  return (
    <div
      data-slot="standalone-applet"
      className="flex min-h-dvh flex-col bg-background text-foreground"
    >
      <header className="sticky top-0 z-20 border-b bg-background/95 pt-[env(safe-area-inset-top)] backdrop-blur supports-[backdrop-filter]:bg-background/80">
        <div className="relative mx-auto flex h-14 w-full max-w-2xl items-center gap-2 px-2">
          {back ? (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Back"
              className="pointer-coarse:size-11"
              onClick={back}
            >
              <ChevronLeftIcon className="size-5" />
            </Button>
          ) : (
            <span className="w-2" />
          )}
          {named && <AppletTile icon={icon} size="sm" />}
          <div className="min-w-0 flex-1 px-1">
            {named && (
              <>
                {title !== name && (
                  <p className="truncate text-[11.5px] leading-tight text-faint">
                    {name}
                  </p>
                )}
                <h1 className="truncate text-[15px] leading-snug font-semibold">
                  {title}
                </h1>
              </>
            )}
          </div>
          {runtime.refreshing && <Refreshing className="-bottom-px" />}
        </div>
      </header>
      <main className="mx-auto w-full max-w-2xl flex-1 px-4 pt-4 pb-[max(env(safe-area-inset-bottom),1.5rem)]">
        {failure?.status === 404 ? (
          <Missing appletId={appletId} />
        ) : failure ? (
          <Failure
            error={failure}
            location={location}
            start={header?.start}
            runtime={runtime}
          />
        ) : runtime.loading || !header ? (
          <Loading />
        ) : (
          <Screen runtime={runtime} progress={false} />
        )}
      </main>
    </div>
  )
}
