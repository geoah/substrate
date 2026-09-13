/** One app's guest: its `source` behind a sandboxed frame, talking to the
 * console over the bridge (`lib/apps/bridge/host.ts`). The frame is
 * `sandbox="allow-scripts"` and nothing more, so the document's origin is
 * opaque: `localStorage` throws, `/api` is unreachable, and the only way to
 * a record is a call the host checks against the app's grant.
 *
 * The grant counts only at OWNER PROVENANCE. An app is a record like any
 * other, and an agent allowed to write apps could widen `permissions` under a
 * `source` the owner wrote; the single-record read is the one wire surface
 * carrying `propertyMeta`, and the guest is mounted only when `source`,
 * `modules` and `permissions` (each that is present) were written at
 * `tier: owner`. Below that the frame is not there at all: a line says the
 * app needs the owner's review and links to its record. Nothing real reaches
 * a document whose grant the owner has not written.
 *
 * Full-page the frame owns its scroll (a content-sized frame breaks sticky
 * positioning and iOS momentum); in a card, `size-changed` is honored up to
 * 60 vh. The frame is `inert` while a host dialog is open, a document that
 * never says `ui/initialize` within five seconds gets the description and
 * the record link drawn above it, and one that stops answering pings is torn
 * down and offered a retry. A changed `source` or `modules` re-navigates the
 * frame with the route preserved, which is the dev loop's hot reload. */

import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type Ref,
} from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Link, useRouter } from "@tanstack/react-router"
import { InfoIcon, RotateCcwIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { toast } from "@/components/ui/toast"
import { useLiveRecords } from "@/hooks/use-live-records"
import { APP_NAME } from "@/lib/api/apps"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  createBridgeHost,
  ownerProvenance,
  type BridgeHost,
} from "@/lib/apps/bridge/host"
import {
  MOUNT_TYPE,
  READY_TYPE,
  type HostContext,
  type InputState,
  type MountMessage,
  type NavigateParams,
  type PrimaryActionParams,
  type Theme,
} from "@/lib/apps/bridge/protocol"
import { digest } from "@/lib/apps/digest"
import { expandGrant } from "@/lib/apps/grant"
import type { AppError, AppSpec } from "@/lib/apps/spec"
import { splitKind } from "@/lib/definition"
import { cn } from "@/lib/utils"

const FRAME_URL = "/app-frame.html"

/** How long a document has to say `ui/initialize` before the frame says it
 * has not. The frame stays: a static `html` document that imports no SDK is
 * a legitimate app. */
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

export type FramePhase = "booting" | "live" | "silent" | "lost"

/** What the screen's chrome may ask of the frame: the two host buttons the
 * guest is told about. */
export interface AppFrameHandle {
  primaryActionClicked(): void
  back(): void
}

export interface AppFrameProps {
  spec: AppSpec
  /** The app record as the single-record read served it: the one carrier of
   * `propertyMeta`, which decides provenance. */
  record: SubstrateRecord
  kinds: KindInfo[]
  /** `page` fills its box and owns its scroll; `card` is sized by what the
   * document reports, up to 60 vh. */
  mode: "page" | "card"
  /** The app's own path: the splat under `/apps/$id`, `""` at the root. */
  route?: string
  /** The record an `attach: record` card is mounted on. */
  attachedRecord?: SubstrateRecord
  inputs?: Record<string, InputState>
  /** Bumped to remount (the overflow menu's Reload, the lost phase's retry). */
  attempt?: number
  onTitle?: (text: string) => void
  onPrimaryAction?: (state: PrimaryActionParams | null) => void
  onError?: (error: AppError) => void
  onPhase?: (phase: FramePhase) => void
  /** The guest asked for another in-app path; the mount owns history. */
  onNavigatePath?: (path: string) => void
  ref?: Ref<AppFrameHandle>
  className?: string
}

export function Notice({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 border-b bg-muted/40 px-4 py-2 text-sm text-muted-foreground">
      <InfoIcon className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

/** The app's own record page, where the YAML editor is. */
export function OpenRecordLink({
  id,
  children = "Open record",
  className,
}: {
  id: string
  children?: React.ReactNode
  className?: string
}) {
  return (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{
        authority: CORE_AUTHORITY,
        pkg: CORE_PACKAGE_NAME,
        name: APP_NAME,
        id,
      }}
      className={cn(
        "underline underline-offset-2 hover:text-foreground",
        className
      )}
    >
      {children}
    </Link>
  )
}

export function AppFrame({
  spec,
  record,
  kinds,
  mode,
  route = "",
  attachedRecord,
  inputs = {},
  attempt = 0,
  onTitle,
  onPrimaryAction,
  onError,
  onPhase,
  onNavigatePath,
  ref,
  className,
}: AppFrameProps) {
  const router = useRouter()
  const queryClient = useQueryClient()
  const granted = ownerProvenance(record)
  const expanded = useMemo(() => expandGrant(spec, kinds), [spec, kinds])
  const [subscribed, setSubscribed] = useState<string[]>([])
  // The tail covers every kind the grant reads, so a `list` (no subscription)
  // is refetched by the console's own invalidation too; the subscribed set
  // is a subset, lifted for the contract's sake.
  useLiveRecords(granted ? [...expanded.reads, ...subscribed] : undefined)

  const [phase, setPhaseState] = useState<FramePhase>("booting")
  const [height, setHeight] = useState<number>()
  const [inert, setInert] = useState(false)

  const frameRef = useRef<HTMLIFrameElement>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const bridgeRef = useRef<BridgeHost>(undefined)
  const latest = useRef({
    spec,
    kinds,
    expanded,
    route,
    attachedRecord,
    inputs,
    granted,
    mode,
    onTitle,
    onPrimaryAction,
    onError,
    onPhase,
    onNavigatePath,
  })
  useEffect(() => {
    latest.current = {
      spec,
      kinds,
      expanded,
      route,
      attachedRecord,
      inputs,
      granted,
      mode,
      onTitle,
      onPrimaryAction,
      onError,
      onPhase,
      onNavigatePath,
    }
  })
  const setPhase = useCallback((next: FramePhase) => {
    setPhaseState(next)
    latest.current.onPhase?.(next)
  }, [])
  const [localAttempt, setLocalAttempt] = useState(0)

  useImperativeHandle(ref, () => ({
    primaryActionClicked: () => bridgeRef.current?.primaryActionClicked(),
    back: () => bridgeRef.current?.back(),
  }))

  const isPage = mode === "page"
  const { source, modules, runtime, sdk } = spec
  // Keyed on content, not identity: jsonb hands modules back in any order.
  const modulesKey = JSON.stringify(
    Object.keys(modules)
      .sort()
      .map((k) => [k, modules[k]])
  )

  // The frame is navigated from here, not from JSX, so the `ready` listener
  // is always in place before the shell can announce itself, and the one
  // `"*"` post is made only while armed: once per navigation the host itself
  // made, never for a document the guest navigated its frame to.
  useEffect(() => {
    const iframe = frameRef.current
    if (!granted || !source || !iframe) return
    let armed = true
    let cancelled = false
    let bridge: BridgeHost | undefined
    let silence: ReturnType<typeof setTimeout> | undefined
    let themeObserver: MutationObserver | undefined
    let sizeObserver: ResizeObserver | undefined
    const mods = JSON.parse(modulesKey) as [string, string][]
    const modulesNow = Object.fromEntries(mods)

    const dimensions = () => {
      const box = boxRef.current?.getBoundingClientRect()
      return {
        width: Math.round(box?.width ?? 0),
        height: Math.round(box?.height ?? 0),
      }
    }
    const displayMode = (): HostContext["displayMode"] =>
      latest.current.mode === "page" && window.innerWidth < 768
        ? "fullscreen"
        : "inline"

    const toRecordPage = (target: { kind: string; id: string }) => {
      const { authority, pkg, name } = splitKind(target.kind)
      void router.navigate({
        to: "/data/$authority/$pkg/$name/$id",
        params: { authority, pkg, name, id: target.id },
      })
    }

    const onNavigate = (target: NavigateParams) => {
      if (target.back) {
        router.history.back()
        return
      }
      if (target.app) {
        router.history.push(
          `/apps/${encodeURIComponent(target.app)}${target.path ?? ""}`
        )
        return
      }
      if (target.path !== undefined) {
        const path = target.path.startsWith("/")
          ? target.path
          : `/${target.path}`
        const own = latest.current.onNavigatePath
        if (own) own(path)
        else {
          router.history.push(
            `/apps/${encodeURIComponent(latest.current.spec.id)}${path}`
          )
        }
        return
      }
      if (target.record) toRecordPage(target.record)
    }

    const onMessage = (e: MessageEvent) => {
      if (!armed || e.source !== iframe.contentWindow) return
      if ((e.data as { type?: unknown } | null)?.type !== READY_TYPE) return
      armed = false
      const channel = new MessageChannel()
      bridge = createBridgeHost({
        app: () => ({
          spec: latest.current.spec,
          kinds: latest.current.kinds,
        }),
        granted: () => latest.current.granted,
        queryClient,
        callbacks: {
          hostContext: () => {
            const l = latest.current
            return {
              theme: currentTheme(),
              displayMode: displayMode(),
              containerDimensions: dimensions(),
              safeAreaInsets: measureSafeArea(),
              platform: "web",
              locale: navigator.language,
              timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
              styles: readHostStyles(),
              app: {
                id: l.spec.id,
                name: l.spec.name,
                route: l.route,
                record: l.attachedRecord
                  ? { kind: l.attachedRecord.kind, id: l.attachedRecord.id }
                  : undefined,
                inputs: l.inputs,
              },
              kinds: l.expanded.readKinds,
            }
          },
          onInitialized: () => {
            if (silence !== undefined) clearTimeout(silence)
            setPhase("live")
          },
          onSizeChanged: ({ height: h }) => {
            if (h !== undefined && latest.current.mode !== "page") {
              setHeight(Math.max(1, Math.ceil(h)))
            }
          },
          onPrimaryAction: (state) => latest.current.onPrimaryAction?.(state),
          onTitle: (text) => latest.current.onTitle?.(text),
          onToast: (t) =>
            toast.add({
              type: t.type ?? "info",
              title: t.title,
              description: t.description,
            }),
          onNavigate,
          onError: (error) => latest.current.onError?.(error),
          onSubscribedKinds: (kinds) => setSubscribed(kinds),
          onLost: () => setPhase("lost"),
        },
      })
      bridge.attach(channel.port1)
      bridgeRef.current = bridge
      const port = channel.port2
      void digest(source, modulesNow).then((d) => {
        if (cancelled || !iframe.contentWindow) return
        const mount: MountMessage = {
          type: MOUNT_TYPE,
          runtime,
          source,
          modules: modulesNow,
          sdk,
          digest: d,
        }
        iframe.contentWindow.postMessage(mount, "*", [port])
      })

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
      cancelled = true
      window.removeEventListener("message", onMessage)
      if (silence !== undefined) clearTimeout(silence)
      themeObserver?.disconnect()
      sizeObserver?.disconnect()
      bridge?.teardown()
      if (bridgeRef.current === bridge) bridgeRef.current = undefined
    }
  }, [
    granted,
    source,
    modulesKey,
    runtime,
    sdk,
    spec.id,
    attempt,
    localAttempt,
    router,
    queryClient,
    setPhase,
  ])

  // The app's own path moved: the person navigated, or the guest did through
  // the host. Either way the guest hears it once.
  useEffect(() => {
    bridgeRef.current?.routeChanged(route)
  }, [route])

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

  const retry = () => {
    setPhase("booting")
    setLocalAttempt((n) => n + 1)
  }

  const openRecord = <OpenRecordLink id={spec.id} />

  return (
    <div
      data-app-frame={spec.id}
      className={cn(
        "flex min-h-0 w-full flex-col",
        isPage && "h-full flex-1",
        className
      )}
    >
      {!granted && (
        <Notice>
          This app needs the owner&apos;s review before it runs: its source,
          modules or grant was not written by the owner.
          {spec.description ? ` ${spec.description}` : ""} {openRecord}
        </Notice>
      )}
      {phase === "silent" && (
        <Notice>
          This app has not connected to the console.
          {spec.description ? ` ${spec.description}` : ""} {openRecord}
        </Notice>
      )}
      {phase === "lost" && (
        <div className="flex flex-col items-start gap-3 px-4 py-6 text-sm">
          <p className="text-muted-foreground">
            The app stopped answering and was closed.
            {spec.description ? ` ${spec.description}` : ""} {openRecord}
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
            key={`${attempt}:${localAttempt}`}
            ref={frameRef}
            title={spec.name}
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
            inert={inert}
            className="block h-full w-full border-0 bg-transparent"
          />
        </div>
      )}
    </div>
  )
}
