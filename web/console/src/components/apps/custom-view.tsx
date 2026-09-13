/** `layout: custom`: the view's own HTML document behind a sandboxed frame,
 * talking to the console over the bridge (`lib/apps/bridge/host.ts`). The
 * frame is `sandbox="allow-scripts"` and nothing more, so the document's
 * origin is opaque: `localStorage` throws, `/api` is unreachable, and the
 * only way to a record is a call the host checks against the view's grant.
 * The console document's own `frame-src 'self'` (index.html) is what keeps
 * the document from navigating its frame off-origin with data in the URL;
 * when it tries, the browser commits its error page in the frame, the shell
 * says so on the port, and the view is closed here with a Reload.
 *
 * The grant counts only at OWNER PROVENANCE, and ONE READ decides it. A view
 * is a record like any other, and an agent allowed to write views could
 * repoint `kind`, widen `permissions`, narrow or delete `filter`, all under
 * a `source` the owner wrote. The single-record read is the one wire surface
 * carrying `propertyMeta`, so the source that runs, the grant the bridge
 * checks, the kind and filter the page is read with, and the decision that
 * they are the owner's are ALL taken from that one response: the `spec` this
 * component is mounted with came from some other read of the row (an app
 * screen's collection, a card, or this very query) and is used for its id
 * alone. `source`, `permissions` and `kind` must be present and the owner's;
 * `filter` may be absent, and then no page is pushed. Below that the document
 * is not mounted at all: the view shows its description, says it needs the
 * owner's review, and links to the source. A collection read that holds a
 * newer version of the row than the single read is what the live tail
 * refreshes first, so a newer one triggers a re-read of the single record,
 * once per advance, and the mount follows it.
 *
 * Full-page the frame owns its scroll (a content-sized frame breaks sticky
 * positioning and iOS momentum); in a card, `size-changed` is honored up to
 * 60 vh. The frame is `inert` while a host dialog is open, a document that
 * never says `ui/initialize` within five seconds gets the description and
 * source link drawn above it, and one that stops answering pings is torn
 * down and offered a retry. */

import { useEffect, useMemo, useRef, useState } from "react"
import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query"
import { Link, useRouter } from "@tanstack/react-router"
import { InfoIcon, RotateCcwIcon } from "lucide-react"

import { ProblemStrip } from "@/components/apps/problems"
import { Button } from "@/components/ui/button"
import { useLiveRecords } from "@/hooks/use-live-records"
import { VIEW_NAME, viewQueryOptions } from "@/lib/api/apps"
import {
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  collectionPath,
  request,
  seg,
} from "@/lib/api/http"
import type { Page, SubstrateRecord } from "@/lib/api/types"
import {
  createBridgeHost,
  NOT_GRANTED,
  ownerProvenance,
  type BridgeHost,
  type Provenance,
} from "@/lib/apps/bridge/host"
import {
  MOUNT_TYPE,
  READY_TYPE,
  type HostContext,
  type MountMessage,
  type PrimaryActionParams,
  type Theme,
} from "@/lib/apps/bridge/protocol"
import { viewPageOptions } from "@/lib/apps/queries"
import type { LayoutProps, ViewSpec } from "@/lib/apps/spec"
import { viewSpec } from "@/lib/apps/view-spec"
import { splitKind } from "@/lib/definition"
import { cn } from "@/lib/utils"

const FRAME_URL = "/app-frame.html"

/** How long, from the frame's navigation, a document has to say
 * `ui/initialize` before the view says it has not. The frame stays: a static document that imports no SDK is a
 * legitimate custom view. */
const INITIALIZE_MS = 5_000

/** A card's starting height until the document reports its own. */
const CARD_DEFAULT_PX = 320

/** The tokens the console's stylesheet declares (index.css). Read from the
 * root's computed style so the dark arm is whatever is live. */
const TOKENS = [
  "--background",
  "--foreground",
  "--card",
  "--card-foreground",
  "--popover",
  "--popover-foreground",
  "--primary",
  "--primary-foreground",
  "--secondary",
  "--secondary-foreground",
  "--muted",
  "--muted-foreground",
  "--accent",
  "--accent-foreground",
  "--destructive",
  "--warning",
  "--border",
  "--input",
  "--ring",
  "--chart-1",
  "--chart-2",
  "--chart-3",
  "--chart-4",
  "--chart-5",
  "--radius",
  "--spacing",
  "--font-sans",
]

function currentTheme(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light"
}

function readHostStyles(): HostContext["styles"] {
  const computed = getComputedStyle(document.documentElement)
  const variables: Record<string, string> = {}
  for (const name of TOKENS) {
    const value = computed.getPropertyValue(name).trim()
    if (value) variables[name] = value
  }
  const family = variables["--font-sans"] ?? ""
  const fontFaces: string[] = []
  for (const sheet of Array.from(document.styleSheets)) {
    let rules: CSSRuleList
    try {
      rules = sheet.cssRules
    } catch {
      continue
    }
    for (const rule of Array.from(rules)) {
      if (!(rule instanceof CSSFontFaceRule)) continue
      const declared = rule.style
        .getPropertyValue("font-family")
        .replace(/["']/g, "")
      if (declared && family.includes(declared)) fontFaces.push(rule.cssText)
    }
  }
  return { variables, fontFaces }
}

/** `env()` cannot be read from script; a probe element's padding can. */
function measureSafeArea(): HostContext["safeAreaInsets"] {
  const probe = document.createElement("div")
  probe.style.cssText =
    "position:fixed;visibility:hidden;pointer-events:none;" +
    "padding:env(safe-area-inset-top,0px) env(safe-area-inset-right,0px) env(safe-area-inset-bottom,0px) env(safe-area-inset-left,0px)"
  document.body.appendChild(probe)
  const s = getComputedStyle(probe)
  const px = (v: string) => Math.round(parseFloat(v) || 0)
  const insets = {
    top: px(s.paddingTop),
    right: px(s.paddingRight),
    bottom: px(s.paddingBottom),
    left: px(s.paddingLeft),
  }
  probe.remove()
  return insets
}

/** The newest version of the view row that any cached collection read
 * holds. The caller's spec was decoded from one of those reads (an app
 * screen's, a card's, the launcher's) or from the single-record query
 * itself, and the tail invalidates collections, so a collection ahead of the
 * single read means the row changed since the single read was taken. */
function newestCachedVersion(
  client: QueryClient,
  id: string
): number | undefined {
  let newest: number | undefined
  const collections = client.getQueriesData<unknown>({
    queryKey: ["records", CORE_AUTHORITY, CORE_PACKAGE_NAME, VIEW_NAME],
  })
  for (const [, data] of collections) {
    const pages =
      data && typeof data === "object" && "pages" in data
        ? (data as { pages: (Page | undefined)[] }).pages
        : [data as Page | undefined]
    for (const page of pages) {
      for (const row of page?.records ?? []) {
        if (row.id === id && (newest === undefined || row.version > newest)) {
          newest = row.version
        }
      }
    }
  }
  return newest
}

type Phase = "booting" | "live" | "silent" | "lost" | "left"

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 border-b bg-muted/40 px-4 py-2 text-sm text-muted-foreground">
      <InfoIcon className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

export function CustomView({
  spec: mounted,
  kinds,
  ctx,
  onOpenRecord,
}: LayoutProps) {
  const router = useRouter()
  const client = useQueryClient()
  const view = useQuery(viewQueryOptions(mounted.id))
  const { refetch } = view

  // A collection read of the row newer than this one: re-read, once per
  // advance. The single read stays what acts, before and after.
  const knownVersion = newestCachedVersion(client, mounted.id)
  const behind =
    view.data !== undefined &&
    knownVersion !== undefined &&
    view.data.version < knownVersion
  useEffect(() => {
    if (behind) void refetch()
  }, [behind, knownVersion, refetch])

  // Everything below runs off the single-record read alone.
  const own: ViewSpec | undefined = useMemo(
    () => (view.data ? viewSpec(view.data, kinds) : undefined),
    [view.data, kinds]
  )
  const provenance: Provenance = useMemo(
    () => (view.data ? ownerProvenance(view.data) : NOT_GRANTED),
    [view.data]
  )
  const { granted, filtered } = provenance
  const spec = own ?? mounted
  const { problems, ...pageOptions } = viewPageOptions(spec, ctx, kinds)
  const page = useQuery({
    ...pageOptions,
    enabled: pageOptions.enabled && granted && filtered,
  })
  useLiveRecords(granted && own?.kind ? [own.kind] : undefined)

  const [phase, setPhase] = useState<Phase>("booting")
  const [attempt, setAttempt] = useState(0)
  const [height, setHeight] = useState<number>()
  const [primary, setPrimary] = useState<PrimaryActionParams | null>(null)
  const [inert, setInert] = useState(false)

  const frameRef = useRef<HTMLIFrameElement>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const bridgeRef = useRef<BridgeHost>(undefined)
  const latest = useRef({ own, kinds, ctx, onOpenRecord, provenance })
  useEffect(() => {
    latest.current = { own, kinds, ctx, onOpenRecord, provenance }
  })

  const isPage = ctx.mode === "page"
  const source = granted ? own?.source : undefined

  // The frame is navigated from here, not from JSX, so the `ready` listener
  // is always in place before the shell can announce itself, and the one
  // `"*"` post is made only while armed: once per navigation the host itself
  // made, never for a document the guest navigated its frame to.
  useEffect(() => {
    const iframe = frameRef.current
    if (!source || !iframe) return
    let armed = true
    let bridge: BridgeHost | undefined
    let themeObserver: MutationObserver | undefined
    let sizeObserver: ResizeObserver | undefined

    const dimensions = () => {
      const box = boxRef.current?.getBoundingClientRect()
      return {
        width: Math.round(box?.width ?? 0),
        height: Math.round(box?.height ?? 0),
      }
    }
    const displayMode = (): HostContext["displayMode"] =>
      latest.current.ctx.mode === "page" && window.innerWidth < 768
        ? "fullscreen"
        : "inline"

    const onMessage = (e: MessageEvent) => {
      if (!armed || e.source !== iframe.contentWindow) return
      if ((e.data as { type?: unknown } | null)?.type !== READY_TYPE) return
      armed = false
      const channel = new MessageChannel()
      bridge = createBridgeHost({
        // The spec and the provenance are read together off the same
        // single-record response, never one from it and one from `mounted`;
        // `own` exists whenever `source` does, and a read never unsets it.
        view: () => ({
          spec: latest.current.own!,
          kinds: latest.current.kinds,
        }),
        provenance: () => latest.current.provenance,
        callbacks: {
          hostContext: () => ({
            theme: currentTheme(),
            displayMode: displayMode(),
            containerDimensions: dimensions(),
            safeAreaInsets: measureSafeArea(),
            platform: "web",
            locale: navigator.language,
            timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
            styles: readHostStyles(),
          }),
          onInitialized: () => {
            clearTimeout(silence)
            setPhase("live")
          },
          onSizeChanged: ({ height: h }) => {
            if (h !== undefined && latest.current.ctx.mode !== "page") {
              setHeight(Math.max(1, Math.ceil(h)))
            }
          },
          onPrimaryAction: setPrimary,
          onNavigate: async ({ view, record }) => {
            if (view) {
              router.history.push(
                record
                  ? `/views/${encodeURIComponent(view)}/${encodeURIComponent(record.id)}`
                  : `/views/${encodeURIComponent(view)}`
              )
              return
            }
            if (!record) return
            const { authority, pkg, name } = splitKind(record.kind)
            const row = await request<SubstrateRecord>(
              "GET",
              `${collectionPath(authority, pkg, name)}/${seg(record.id)}`
            )
            latest.current.onOpenRecord(row)
          },
          onLost: () => setPhase("lost"),
          onUnloaded: () => setPhase("left"),
        },
      })
      bridge.attach(channel.port1)
      bridgeRef.current = bridge
      const mount: MountMessage = { type: MOUNT_TYPE, html: source }
      iframe.contentWindow!.postMessage(mount, "*", [channel.port2])

      themeObserver = new MutationObserver(() =>
        bridge?.hostContextChanged({ theme: currentTheme() })
      )
      themeObserver.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["class"],
      })
      sizeObserver = new ResizeObserver(() =>
        bridge?.hostContextChanged({
          containerDimensions: dimensions(),
          displayMode: displayMode(),
        })
      )
      if (boxRef.current) sizeObserver.observe(boxRef.current)
    }

    window.addEventListener("message", onMessage)
    iframe.src = FRAME_URL
    // Armed at the navigation, not at readiness: a shell whose module never
    // loads announces nothing, and that silence is the same notice.
    const silence = setTimeout(() => {
      if (!bridge?.initialized) setPhase("silent")
    }, INITIALIZE_MS)

    return () => {
      armed = false
      window.removeEventListener("message", onMessage)
      clearTimeout(silence)
      themeObserver?.disconnect()
      sizeObserver?.disconnect()
      bridge?.teardown()
      if (bridgeRef.current === bridge) bridgeRef.current = undefined
    }
  }, [source, mounted.id, attempt, router])

  // The page reaches the guest through the bridge, which holds it until the
  // guest is ready and re-sends on every change the tail invalidates.
  const pageData = page.data
  useEffect(() => {
    bridgeRef.current?.pushPage(pageData)
  }, [pageData, phase])

  // A host dialog on top of the frame: the guest may not take focus or
  // clicks through it.
  useEffect(() => {
    const check = () =>
      setInert(
        Boolean(document.querySelector('[role="dialog"], [role="alertdialog"]'))
      )
    const observer = new MutationObserver(check)
    observer.observe(document.body, { childList: true, subtree: true })
    return () => observer.disconnect()
  }, [])

  const openSource = (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{
        authority: CORE_AUTHORITY,
        pkg: CORE_PACKAGE_NAME,
        name: VIEW_NAME,
        id: mounted.id,
      }}
      className="underline underline-offset-2 hover:text-foreground"
    >
      Open source
    </Link>
  )

  if (view.isPending) {
    return (
      <p className="px-4 py-3 text-sm text-muted-foreground">
        Loading the view…
      </p>
    )
  }
  if (view.isError) {
    return (
      <Notice>
        The view could not be read: {view.error.message}. {openSource}
      </Notice>
    )
  }

  const retry = () => {
    setPhase("booting")
    setPrimary(null)
    setAttempt((n) => n + 1)
  }
  const description = spec.description ? ` ${spec.description}` : ""
  const closed = phase === "lost" || phase === "left"

  return (
    <div
      data-layout="custom"
      className={cn("flex min-h-0 w-full flex-col", isPage && "h-full flex-1")}
    >
      {!granted && (
        <Notice>
          This view needs the owner&apos;s review before it runs: its source,
          grant, kind or filter was not written by the owner.
          {description} {openSource}
        </Notice>
      )}
      {granted && !filtered && (
        <Notice>
          No page is pushed to this document: the view declares no filter. It
          may still read records through its grant. {openSource}
        </Notice>
      )}
      {problems.length > 0 && <ProblemStrip problems={problems} />}
      {phase === "silent" && (
        <Notice>
          This document has not connected to the console.
          {description} {openSource}
        </Notice>
      )}
      {closed && (
        <div className="flex flex-col items-start gap-3 px-4 py-6 text-sm">
          <p className="text-muted-foreground">
            {phase === "left"
              ? "The document left its frame and was closed."
              : "The document stopped answering and was closed."}
            {description} {openSource}
          </p>
          <Button variant="outline" size="sm" onClick={retry}>
            <RotateCcwIcon />
            Reload
          </Button>
        </div>
      )}
      {source && !closed && (
        <div
          ref={boxRef}
          className={cn("relative min-h-0 w-full", isPage && "flex-1")}
          style={
            isPage
              ? undefined
              : { height: height ?? CARD_DEFAULT_PX, maxHeight: "60vh" }
          }
        >
          <iframe
            key={attempt}
            ref={frameRef}
            title={spec.name}
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
            inert={inert}
            className="block h-full w-full border-0 bg-transparent"
          />
        </div>
      )}
      {primary && phase === "live" && (
        <div className="shrink-0 border-t px-4 py-3">
          <Button
            className="w-full md:w-auto"
            disabled={primary.enabled === false}
            onClick={() => bridgeRef.current?.primaryActionClicked()}
          >
            {primary.label}
          </Button>
        </div>
      )}
    </div>
  )
}
