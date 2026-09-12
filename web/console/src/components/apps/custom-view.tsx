/** `layout: custom`: the view's own HTML document behind a sandboxed frame,
 * talking to the console over the bridge (`lib/apps/bridge/host.ts`). The
 * frame is `sandbox="allow-scripts"` and nothing more, so the document's
 * origin is opaque: `localStorage` throws, `/api` is unreachable, and the
 * only way to a record is a call the host checks against the view's grant.
 *
 * The grant counts only at OWNER PROVENANCE. A view is a record like any
 * other, and an agent allowed to write views could repoint `kind`, widen
 * `permissions` or narrow `filter` under a `source` the owner wrote; the
 * single-record read is the one wire surface carrying `propertyMeta`, and
 * the document runs only when `source`, `permissions`, `kind` and `filter`
 * (each that is present) were written at `tier: owner`. Below that the
 * document is not mounted at all: the view shows its description, says it
 * needs the owner's review, and links to the source. Nothing real reaches a
 * document whose grant the owner has not written.
 *
 * Full-page the frame owns its scroll (a content-sized frame breaks sticky
 * positioning and iOS momentum); in a card, `size-changed` is honored up to
 * 60 vh. The frame is `inert` while a host dialog is open, a document that
 * never says `ui/initialize` within five seconds gets the description and
 * source link drawn above it, and one that stops answering pings is torn
 * down and offered a retry. */

import { useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
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
import type { SubstrateRecord } from "@/lib/api/types"
import {
  createBridgeHost,
  ownerProvenance,
  type BridgeHost,
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
import type { LayoutProps } from "@/lib/apps/spec"
import { splitKind } from "@/lib/definition"
import { cn } from "@/lib/utils"

const FRAME_URL = "/app-frame.html"

/** How long a document has to say `ui/initialize` before the view says it
 * has not. The frame stays: a static document that imports no SDK is a
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

type Phase = "booting" | "live" | "silent" | "lost"

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 border-b bg-muted/40 px-4 py-2 text-sm text-muted-foreground">
      <InfoIcon className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

export function CustomView({ spec, kinds, ctx, onOpenRecord }: LayoutProps) {
  const router = useRouter()
  const view = useQuery(viewQueryOptions(spec.id))
  const granted = ownerProvenance(view.data)
  const { problems, ...pageOptions } = viewPageOptions(spec, ctx, kinds)
  const page = useQuery({
    ...pageOptions,
    enabled: pageOptions.enabled && granted,
  })
  useLiveRecords(granted && spec.kind ? [spec.kind] : undefined)

  const [phase, setPhase] = useState<Phase>("booting")
  const [attempt, setAttempt] = useState(0)
  const [height, setHeight] = useState<number>()
  const [primary, setPrimary] = useState<PrimaryActionParams | null>(null)
  const [inert, setInert] = useState(false)

  const frameRef = useRef<HTMLIFrameElement>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const bridgeRef = useRef<BridgeHost>(undefined)
  const latest = useRef({ spec, kinds, ctx, onOpenRecord, granted })
  useEffect(() => {
    latest.current = { spec, kinds, ctx, onOpenRecord, granted }
  })

  const isPage = ctx.mode === "page"
  const source = spec.source

  // The frame is navigated from here, not from JSX, so the `ready` listener
  // is always in place before the shell can announce itself, and the one
  // `"*"` post is made only while armed: once per navigation the host itself
  // made, never for a document the guest navigated its frame to.
  useEffect(() => {
    const iframe = frameRef.current
    if (!granted || !source || !iframe) return
    let armed = true
    let bridge: BridgeHost | undefined
    let silence: ReturnType<typeof setTimeout> | undefined
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
        view: () => ({
          spec: latest.current.spec,
          kinds: latest.current.kinds,
        }),
        granted: () => latest.current.granted,
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
            if (silence !== undefined) clearTimeout(silence)
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
        },
      })
      bridge.attach(channel.port1)
      bridgeRef.current = bridge
      const mount: MountMessage = { type: MOUNT_TYPE, html: source }
      iframe.contentWindow!.postMessage(mount, "*", [channel.port2])

      silence = setTimeout(() => {
        if (!bridge?.initialized) setPhase("silent")
      }, INITIALIZE_MS)

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

    return () => {
      armed = false
      window.removeEventListener("message", onMessage)
      if (silence !== undefined) clearTimeout(silence)
      themeObserver?.disconnect()
      sizeObserver?.disconnect()
      bridge?.teardown()
      if (bridgeRef.current === bridge) bridgeRef.current = undefined
    }
  }, [granted, source, spec.id, attempt, router])

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
        id: spec.id,
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

  return (
    <div
      data-layout="custom"
      className={cn("flex min-h-0 w-full flex-col", isPage && "h-full flex-1")}
    >
      {!granted && (
        <Notice>
          This view needs the owner&apos;s review before it runs: its source,
          grant, kind or filter was not written by the owner.
          {spec.description ? ` ${spec.description}` : ""} {openSource}
        </Notice>
      )}
      {problems.length > 0 && <ProblemStrip problems={problems} />}
      {phase === "silent" && (
        <Notice>
          This document has not connected to the console.
          {spec.description ? ` ${spec.description}` : ""} {openSource}
        </Notice>
      )}
      {phase === "lost" && (
        <div className="flex flex-col items-start gap-3 px-4 py-6 text-sm">
          <p className="text-muted-foreground">
            The document stopped answering and was closed.
            {spec.description ? ` ${spec.description}` : ""} {openSource}
          </p>
          <Button variant="outline" size="sm" onClick={retry}>
            <RotateCcwIcon />
            Reload
          </Button>
        </div>
      )}
      {granted && source && phase !== "lost" && (
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
