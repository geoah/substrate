/** One applet's runtime on the console's side (docs/plans/applets.md): the
 * surface its frames build, the state the renderer holds between calls, and
 * the three things that call the server again — a button's action, an
 * edit under one of the screen's `refreshOn` paths, and a change to a kind
 * the screen reads.
 *
 * The server keeps no session, so every call carries the screen, its params
 * and the data model's `/ui`, and the `shown` of what is drawn: a frame for
 * the same screen of the same version updates the data model alone, so an
 * input keeps its focus and its unsent text. The address holds the screen
 * and its params; a frame that moves them writes a history entry, and Back
 * moving them renders that screen again. */

import { useCallback, useEffect, useRef, useState } from "react"

import { isRecord } from "@/lib/a2ui/coerce"
import { pathsOverlap } from "@/lib/a2ui/pointer"
import { SurfaceStore } from "@/lib/a2ui/store"
import { actApplet, renderApplet } from "@/lib/api/applets"
import { nothingSaved } from "@/lib/api/compacted"
import { followChanges } from "@/lib/api/follow"
import {
  ApiError,
  type AppletAction,
  type AppletFrame,
  type AppletHeader,
  type AppletShown,
  type AppletState,
} from "@/lib/api/types"
import { appletLocationKey, type AppletLocation } from "@/lib/applet-location"

/** How long the runtime waits after the last edit or change before it
 * renders again. */
export const APPLET_REFRESH_MS = 300

/** The surface the server draws an applet on when no frame has named one. */
const DEFAULT_SURFACE = "applet"

export interface AppletRuntime {
  store: SurfaceStore
  surfaceId: string
  header?: AppletHeader
  /** No frame has arrived yet. */
  loading: boolean
  /** The render that failed while nothing is drawn: the applet is missing, or
   * the server could not draw it. */
  failure?: ApiError
  /** The last action the server refused, or a refresh that failed over a
   * screen still drawn. The screen keeps its state. */
  refusal?: ApiError
  dismissRefusal: () => void
  /** An action is running. */
  busy: boolean
  /** A render is in flight over a screen still drawn: an edit, a change, a
   * step back. */
  refreshing: boolean
  /** The question an action waits on, the answer that runs it, and whether
   * that answer deletes. */
  confirm?: AppletQuestion
  answer: (yes: boolean) => void
  /** What of the screen could not be drawn, each once. */
  problems: string[]
  addProblem: (message: string) => void
  act: (action: AppletAction) => void
  retry: () => void
  /** The applet's start, as it opens with no state: the way out of a screen
   * that cannot be drawn. */
  home: () => void
}

export interface AppletQuestion {
  question: string
  label?: string
  danger?: boolean
}

export function useAppletRuntime({
  appletId,
  location,
  navigate,
  onNotice,
}: {
  appletId: string
  location: AppletLocation
  navigate: (to: AppletLocation, options: { replace: boolean }) => void
  /** A line to show once: an action's `notify` (`from` "action"), or where
   * a render landed instead of the screen asked for (`from` "render"). */
  onNotice?: (notice: string, from: "render" | "action") => void
}): AppletRuntime {
  const [store] = useState(() => new SurfaceStore())
  const [surfaceId, setSurfaceId] = useState(DEFAULT_SURFACE)
  const [header, setHeader] = useState<AppletHeader>()
  const [loading, setLoading] = useState(true)
  const [failure, setFailure] = useState<ApiError>()
  const [refusal, setRefusal] = useState<ApiError>()
  const [busy, setBusy] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [confirm, setConfirm] = useState<
    AppletQuestion & { action: AppletAction; state: AppletState }
  >()
  const [problems, setProblems] = useState<string[]>([])

  // What the callbacks below read at call time, not at the render that
  // made them.
  const live = useRef({
    appletId,
    location,
    navigate,
    onNotice,
    surfaceId: DEFAULT_SURFACE,
    header: undefined as AppletHeader | undefined,
    /** The screen and params the last frame put the renderer on. */
    held: undefined as AppletLocation | undefined,
    shown: undefined as AppletShown | undefined,
    /** The location this runtime navigated to itself, so the address
     * catching up is not read as the reader moving. */
    expected: undefined as string | undefined,
    seq: 0,
    acting: false,
    queued: false,
    renderCtrl: undefined as AbortController | undefined,
    timer: undefined as ReturnType<typeof setTimeout> | undefined,
  })
  useEffect(() => {
    Object.assign(live.current, { appletId, location, navigate, onNotice })
  })

  const addProblem = useCallback((message: string) => {
    setProblems((prev) => (prev.includes(message) ? prev : [...prev, message]))
  }, [])

  const currentUi = useCallback((): Record<string, unknown> | undefined => {
    const ui = store.surface(live.current.surfaceId)?.dataModel.ui
    return isRecord(ui) ? ui : undefined
  }, [store])

  const currentState = useCallback((): AppletState => {
    const held = live.current.held ?? live.current.location
    return {
      screen: held.screen ?? "",
      ...(held.params && { params: held.params }),
      ...(currentUi() && { ui: currentUi() }),
    }
  }, [currentUi])

  const applyFrame = useCallback(
    (frame: AppletFrame, moved: "push" | "replace") => {
      const r = live.current
      const created = frame.messages.find((m) => m.createSurface)
      if (created?.createSurface) {
        r.surfaceId = created.createSurface.surfaceId
        setSurfaceId(r.surfaceId)
        setProblems([])
      }
      for (const problem of store.process(frame.messages)) addProblem(problem)
      r.shown = frame.shown
      r.header = frame.applet
      const next: AppletLocation = {
        screen: frame.state.screen,
        params: frame.state.params,
      }
      r.held = next
      setHeader(frame.applet)
      setLoading(false)
      setFailure(undefined)
      if (appletLocationKey(next) !== appletLocationKey(r.location)) {
        r.expected = appletLocationKey(next)
        // A first frame names the screen an address without one opened on;
        // that is the same page, not a step Back should walk.
        r.navigate(next, {
          replace: moved === "replace" || !r.location.screen,
        })
      }
      if (frame.notice)
        r.onNotice?.(frame.notice, moved === "push" ? "action" : "render")
    },
    [addProblem, store]
  )

  const render = useCallback(
    async (state: AppletState | undefined) => {
      const r = live.current
      if (r.acting) {
        r.queued = true
        return
      }
      r.renderCtrl?.abort()
      const ctrl = new AbortController()
      r.renderCtrl = ctrl
      const mine = ++r.seq
      if (r.shown) setRefreshing(true)
      try {
        const frame = await renderApplet(
          r.appletId,
          { ...(state && { state }), ...(r.shown && { shown: r.shown }) },
          ctrl.signal
        )
        if (mine !== r.seq || ctrl.signal.aborted) return
        applyFrame(frame, "replace")
      } catch (err) {
        if (mine !== r.seq || ctrl.signal.aborted) return
        const error =
          err instanceof ApiError
            ? err
            : new ApiError("network", String(err), 0)
        if (r.shown) setRefusal(error)
        else {
          setFailure(error)
          setLoading(false)
        }
      } finally {
        // A newer render owns the flag; an action that aborted this one
        // shows its own.
        if (r.renderCtrl === ctrl) setRefreshing(false)
      }
    },
    [applyFrame]
  )

  const refresh = useCallback(
    (delay = APPLET_REFRESH_MS) => {
      const r = live.current
      clearTimeout(r.timer)
      r.timer = setTimeout(() => void render(currentState()), delay)
    },
    [currentState, render]
  )

  const run = useCallback(
    async (action: AppletAction, confirmed: boolean, asked?: AppletState) => {
      const r = live.current
      if (r.acting) return
      r.acting = true
      setBusy(true)
      setRefusal(undefined)
      clearTimeout(r.timer)
      // A render in flight would land the screen as it was before the
      // action; the action's own frame is the newer one.
      r.renderCtrl?.abort()
      const mine = ++r.seq
      // A yes answers the question as it was asked: the state it was asked
      // over, not what the screen holds by the time the owner answers.
      const state = asked ?? currentState()
      try {
        const frame = await actApplet(r.appletId, {
          state,
          ...(r.shown && { shown: r.shown }),
          action,
          ...(confirmed && { confirmed: true }),
        })
        if (mine === r.seq) applyFrame(frame, "push")
        setConfirm(
          frame.confirm
            ? {
                question: frame.confirm,
                label: frame.confirmLabel,
                danger: frame.confirmDanger,
                action,
                state,
              }
            : undefined
        )
      } catch (err) {
        setRefusal(
          err instanceof ApiError
            ? err
            : new ApiError("network", String(err), 0)
        )
      } finally {
        r.acting = false
        setBusy(false)
        if (r.queued) {
          r.queued = false
          refresh(0)
        }
      }
    },
    [applyFrame, currentState, refresh]
  )

  // The address is where the reader is: the first location renders the
  // applet, and a later one the runtime did not navigate to itself (Back,
  // Forward, a pasted link) renders that screen with the `/ui` held now.
  const locationKey = appletLocationKey(location)
  useEffect(() => {
    const r = live.current
    if (r.expected === locationKey) {
      r.expected = undefined
      return
    }
    r.expected = undefined
    if (r.held && appletLocationKey(r.held) === locationKey) return
    const { screen, params } = r.location
    if (!r.shown || !screen) {
      // Nothing drawn yet, or an address with no screen: the applet opens
      // there, on its declared initial `/ui`.
      void render(screen ? { screen, ...(params && { params }) } : undefined)
    } else {
      void render({
        screen,
        ...(params && { params }),
        ...(currentUi() && { ui: currentUi() }),
      })
    }
    return () => r.renderCtrl?.abort()
  }, [locationKey, render, currentUi])

  // An edit under one of the screen's refreshOn paths re-renders it.
  useEffect(
    () =>
      store.onInputWrite((_surface, path) => {
        const watched = live.current.header?.refreshOn ?? []
        if (watched.some((p) => pathsOverlap(path, p))) refresh()
      }),
    [store, refresh]
  )

  // A change to a kind the screen reads re-renders it, and so does a gap
  // the watch could not tell (a position the server no longer holds). A
  // second such gap within the minute stops the watch (RULE W,
  // compacted.ts), said over the screen still drawn.
  const watchKey = JSON.stringify([...(header?.watch ?? [])].sort())
  useEffect(() => {
    const kinds = JSON.parse(watchKey) as string[]
    if (!kinds.length) return
    const handle = followChanges({
      filter: { kinds },
      reader: {
        key: `applet ${live.current.appletId} ${watchKey}`,
        // The watch opens at the head, and a render resumes no position.
        drop: nothingSaved,
        fail: setRefusal,
      },
      onRow: () => refresh(),
      relist: () => refresh(),
    })
    return () => handle.stop()
  }, [watchKey, refresh])

  useEffect(() => {
    const r = live.current
    return () => {
      clearTimeout(r.timer)
      r.renderCtrl?.abort()
    }
  }, [])

  const act = useCallback(
    (action: AppletAction) => void run(action, false),
    [run]
  )
  const dismissRefusal = useCallback(() => setRefusal(undefined), [])

  return {
    store,
    surfaceId,
    header,
    loading,
    failure,
    refusal,
    dismissRefusal,
    busy,
    refreshing,
    confirm: confirm && {
      question: confirm.question,
      label: confirm.label,
      danger: confirm.danger,
    },
    answer: (yes) => {
      const pending = confirm
      setConfirm(undefined)
      if (yes && pending) void run(pending.action, true, pending.state)
    },
    problems,
    addProblem,
    act,
    retry: () => {
      setLoading(!live.current.shown)
      setFailure(undefined)
      void render(live.current.shown ? currentState() : undefined)
    },
    home: () => {
      setLoading(!live.current.shown)
      setFailure(undefined)
      setRefusal(undefined)
      void render(undefined)
    },
  }
}
