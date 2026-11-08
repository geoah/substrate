import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
} from "react"

/** A list's rows dragged into a new order, as SwiftUI's `onMove` and the
 * Mac's list do it: a mouse drags a row at once; a finger holds it first
 * (a hold let go without moving is the row's menu, as a long press is on a
 * phone); Alt with ↑ or ↓ moves the focused row one place. `onMove` gets
 * the row and the index it lands at among the rows as drawn. */
export function useReorder(opts: {
  ids: string[]
  enabled: boolean
  onMove: (id: string, index: number) => void
  /** A held row let go without moving, or a right click. */
  onMenu?: (id: string, at: { x: number; y: number }) => void
  /** Read aloud after a move: "Moved to 2 of 5". */
  announce?: (words: string) => void
}) {
  const elements = useRef(new Map<string, HTMLElement>())
  const [drag, setDrag] = useState<Drag | undefined>()
  const live = useRef(opts)
  live.current = opts
  const gesture = useRef<Gesture | undefined>(undefined)

  const end = useCallback(() => {
    const g = gesture.current
    gesture.current = undefined
    holding = false
    if (g?.timer) clearTimeout(g.timer)
    if (g?.scroll) cancelAnimationFrame(g.scroll)
    document.documentElement.style.removeProperty("user-select")
    setDrag(undefined)
    return g
  }, [])

  useEffect(() => () => void end(), [end])

  const begin = (g: Gesture) => {
    const ids = live.current.ids
    const rects = ids.map(
      (id) => elements.current.get(id)?.getBoundingClientRect() ?? null
    )
    const from = ids.indexOf(g.id)
    const own = rects[from]
    if (!own) return
    g.dragging = true
    g.rects = rects
    g.scroller = scrollParent(elements.current.get(g.id))
    g.scrollTop = g.scroller?.scrollTop ?? window.scrollY
    document.documentElement.style.setProperty("user-select", "none")
    setDrag({ id: g.id, from, over: from, dy: 0, height: own.height })
    const step = () => {
      // Near an edge of what scrolls, the list scrolls under the row.
      const box = g.scroller?.getBoundingClientRect() ?? {
        top: 0,
        bottom: window.innerHeight,
      }
      const edge = 56
      const speed =
        g.y < box.top + edge
          ? -Math.ceil((box.top + edge - g.y) / 6)
          : g.y > box.bottom - edge
            ? Math.ceil((g.y - box.bottom + edge) / 6)
            : 0
      if (speed) {
        if (g.scroller) g.scroller.scrollTop += speed
        else window.scrollBy(0, speed)
        follow(g)
      }
      g.scroll = requestAnimationFrame(step)
    }
    g.scroll = requestAnimationFrame(step)
  }

  const follow = (g: Gesture) => {
    if (!g.dragging || !g.rects) return
    const scrolled =
      (g.scroller?.scrollTop ?? window.scrollY) - (g.scrollTop ?? 0)
    const dy = g.y - g.startY + scrolled
    const from = live.current.ids.indexOf(g.id)
    const own = g.rects[from]
    if (!own) return
    const centre = own.top + own.height / 2 + dy
    let over = 0
    g.rects.forEach((r, i) => {
      if (i !== from && r && r.top + r.height / 2 < centre) over++
    })
    setDrag({ id: g.id, from, over, dy, height: own.height })
  }

  const onPointerDown = (id: string) => (e: PointerEvent<HTMLElement>) => {
    if (!e.isPrimary || e.button !== 0 || gesture.current) return
    const touch = e.pointerType !== "mouse"
    const g: Gesture = {
      id,
      pointer: e.pointerId,
      touch,
      startX: e.clientX,
      startY: e.clientY,
      x: e.clientX,
      y: e.clientY,
      dragging: false,
      held: false,
    }
    gesture.current = g
    if (touch)
      g.timer = window.setTimeout(() => {
        g.held = true
        holding = true
        navigator.vibrate?.(8)
      }, 450)
    const target = e.currentTarget
    const move = (ev: globalThis.PointerEvent) => {
      if (ev.pointerId !== g.pointer) return
      g.x = ev.clientX
      g.y = ev.clientY
      const far = Math.hypot(g.x - g.startX, g.y - g.startY)
      if (!g.dragging) {
        if (g.touch && !g.held) {
          // Moving before the hold is a scroll.
          if (far > 8) finish(ev, true)
          return
        }
        if (far < (g.touch ? 6 : 4) || !live.current.enabled) return
        try {
          target.setPointerCapture(g.pointer)
        } catch {
          // a pointer already gone
        }
        begin(g)
      }
      follow(g)
    }
    const finish = (ev: globalThis.PointerEvent, cancelled = false) => {
      if (ev.pointerId !== g.pointer) return
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", up)
      window.removeEventListener("pointercancel", cancel)
      const done = end()
      if (!done) return
      if (done.dragging) {
        swallowClick()
        if (!cancelled) {
          const ids = live.current.ids
          const from = ids.indexOf(done.id)
          const own = done.rects?.[from]
          if (own && done.rects) {
            const scrolled =
              (done.scroller?.scrollTop ?? window.scrollY) -
              (done.scrollTop ?? 0)
            const centre =
              own.top + own.height / 2 + done.y - done.startY + scrolled
            let over = 0
            done.rects.forEach((r, i) => {
              if (i !== from && r && r.top + r.height / 2 < centre) over++
            })
            if (over !== from) live.current.onMove(done.id, over)
          }
        }
      } else if (done.held && !cancelled) {
        // The browser's own mouse events after a hold would move the focus
        // out of the menu it opens, and click the row.
        window.addEventListener(
          "touchend",
          (t) => t.cancelable && t.preventDefault(),
          {
            once: true,
            passive: false,
          }
        )
        swallowClick()
        live.current.onMenu?.(done.id, { x: done.x, y: done.y })
      }
    }
    const up = (ev: globalThis.PointerEvent) => finish(ev)
    const cancel = (ev: globalThis.PointerEvent) => finish(ev, true)
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", up)
    window.addEventListener("pointercancel", cancel)
  }

  const onContextMenu = (id: string) => (e: MouseEvent<HTMLElement>) => {
    if (!live.current.onMenu) return
    e.preventDefault()
    // A finger's hold is the gesture's own; this is a right click.
    if (gesture.current?.touch) return
    live.current.onMenu(id, { x: e.clientX, y: e.clientY })
  }

  const onKeyDown = (id: string) => (e: KeyboardEvent<HTMLElement>) => {
    if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return
    const { ids, enabled } = live.current
    if (!enabled) return
    const from = ids.indexOf(id)
    const to = e.key === "ArrowUp" ? from - 1 : from + 1
    if (from < 0 || to < 0 || to >= ids.length) return
    e.preventDefault()
    live.current.onMove(id, to)
    live.current.announce?.(`Moved to ${to + 1} of ${ids.length}`)
    // The row keeps the focus once it is drawn in its new place.
    const focused = document.activeElement as HTMLElement | null
    requestAnimationFrame(() => focused?.focus())
  }

  /** What a row's element takes, and where it is drawn while a drag runs. */
  const row = (id: string) => ({
    ref: (el: HTMLElement | null) => {
      if (el) elements.current.set(id, el)
      else elements.current.delete(id)
    },
    onPointerDown: onPointerDown(id),
    onContextMenu: onContextMenu(id),
    onKeyDown: onKeyDown(id),
    style: rowStyle(drag, live.current.ids, id),
    "data-dragging": drag?.id === id ? "" : undefined,
  })

  return { row, dragging: drag?.id }
}

interface Drag {
  id: string
  from: number
  over: number
  dy: number
  height: number
}

interface Gesture {
  id: string
  pointer: number
  touch: boolean
  startX: number
  startY: number
  x: number
  y: number
  dragging: boolean
  held: boolean
  timer?: number
  scroll?: number
  rects?: (DOMRect | null)[]
  scroller?: HTMLElement
  scrollTop?: number
}

function rowStyle(
  drag: Drag | undefined,
  ids: string[],
  id: string
): CSSProperties | undefined {
  if (!drag) return undefined
  const i = ids.indexOf(id)
  if (id === drag.id)
    return {
      transform: `translateY(${drag.dy}px)`,
      position: "relative",
      zIndex: 10,
      boxShadow: "0 8px 24px rgb(0 0 0 / 0.18)",
      transition: "box-shadow 150ms",
    }
  let shift = 0
  if (drag.from < drag.over && i > drag.from && i <= drag.over)
    shift = -drag.height
  if (drag.from > drag.over && i >= drag.over && i < drag.from)
    shift = drag.height
  return {
    transform: shift ? `translateY(${shift}px)` : undefined,
    transition: "transform 150ms ease",
  }
}

function scrollParent(el: HTMLElement | undefined): HTMLElement | undefined {
  for (let n = el?.parentElement; n; n = n.parentElement) {
    const { overflowY } = getComputedStyle(n)
    if (
      (overflowY === "auto" || overflowY === "scroll") &&
      n.scrollHeight > n.clientHeight
    )
      return n
  }
  return undefined
}

/** The click a drag or a hold ends in opens nothing. */
function swallowClick() {
  const stop = (e: Event) => {
    e.stopPropagation()
    e.preventDefault()
  }
  window.addEventListener("click", stop, { capture: true, once: true })
  setTimeout(() => window.removeEventListener("click", stop, true), 300)
}

/** While a finger holds a row, the page does not scroll under it. */
let holding = false
if (typeof window !== "undefined")
  window.addEventListener(
    "touchmove",
    (e) => {
      if (holding && e.cancelable) e.preventDefault()
    },
    { passive: false }
  )
