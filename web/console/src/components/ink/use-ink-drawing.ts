/** Pointer input on an ink page: a pen, pencil or marker gesture becomes one
 * `.inkd` stroke, an eraser gesture names the strokes it touches. Points are
 * read in page coordinates through the SVG's own transform, so the page may
 * be drawn at any size. A stylus reports pressure and tilt; a mouse reports
 * neither, and gets the spec's defaults (§6).
 *
 * A finger scrolls the page unless `fingerDraws` says it draws: on a tablet
 * the pen writes and the palm rests on the screen. The page turns off the
 * browser's own touch scrolling, so a stroke is never read as a scroll, and
 * the finger's scroll is done here. Where a finger draws, a second finger
 * drops the stroke the first began and scrolls instead. While a pen is down
 * a finger does nothing at all, and a scroll the palm began stops. */

import { useCallback, useRef, useState, type PointerEvent } from "react"

import { inkPoint, randomId, type InkStroke, type InkTool } from "@/lib/ink"

export type InkInstrument = InkTool | "eraser"

export interface PenSettings {
  instrument: InkInstrument
  color: string
  /** The stroke's `baseWidth` in page points. */
  width: number
}

/** The eraser's reach around the pointer, in page points. */
export const ERASER_RADIUS = 10

/** PointerEvent's stylus angles, where the browser reports them. */
type StylusEvent = {
  altitudeAngle?: number
  azimuthAngle?: number
  tiltX?: number
  tiltY?: number
}

/** The stylus' angles in the spec's terms: azimuth in the page plane and
 * altitude above it, both radians. A browser without the two angles reports
 * tilt, which converts; a mouse reports neither. */
export function stylusAngles(
  event: StylusEvent & { pointerType: string }
): [number, number] {
  if (event.pointerType !== "pen") return [0, Math.PI / 2]
  if (
    typeof event.altitudeAngle === "number" &&
    typeof event.azimuthAngle === "number"
  )
    return [event.azimuthAngle, event.altitudeAngle]
  const tx = ((event.tiltX ?? 0) * Math.PI) / 180
  const ty = ((event.tiltY ?? 0) * Math.PI) / 180
  if (tx === 0 && ty === 0) return [0, Math.PI / 2]
  const azimuth = Math.atan2(Math.tan(ty), Math.tan(tx))
  const altitude = Math.atan(
    1 / Math.sqrt(Math.tan(tx) ** 2 + Math.tan(ty) ** 2)
  )
  return [azimuth < 0 ? azimuth + 2 * Math.PI : azimuth, altitude]
}

/** A mouse presses at 0.5 whatever it does, and so does a pen that reports
 * none: the spec's default for an unknown pressure. */
function pressureOf(event: { pointerType: string; pressure: number }) {
  if (event.pointerType === "mouse") return 0.5
  return event.pressure > 0 ? event.pressure : 0.5
}

export function useInkDrawing({
  settings,
  enabled,
  fingerDraws = false,
  onStroke,
  onErase,
  onGestureStart,
}: {
  settings: PenSettings
  enabled: boolean
  /** A finger draws; otherwise it scrolls. */
  fingerDraws?: boolean
  /** A finished stroke, ready to append to the page. */
  onStroke: (stroke: InkStroke) => void
  /** The strokes one eraser move touched. */
  onErase: (x: number, y: number) => void
  /** Before the first change of a gesture, so an undo step covers it whole. */
  onGestureStart?: () => void
}) {
  const [live, setLive] = useState<InkStroke | null>(null)
  const active = useRef<{
    id: number
    start: number
    stroke: InkStroke | null
    erasing: boolean
    touch: boolean
  } | null>(null)
  /** The pen that is down, whether or not it is drawing: the palm beside it
   * neither scrolls nor draws. */
  const penDown = useRef<number | null>(null)
  const pan = useRef<{
    id: number
    x: number
    y: number
    scroller: Element
  } | null>(null)

  const toPage = useCallback(
    (svg: SVGSVGElement, clientX: number, clientY: number) => {
      const matrix = svg.getScreenCTM()
      if (!matrix) return null
      const point = new DOMPoint(clientX, clientY).matrixTransform(
        matrix.inverse()
      )
      return { x: point.x, y: point.y }
    },
    []
  )

  const sample = useCallback(
    (
      svg: SVGSVGElement,
      event: PointerEvent<SVGSVGElement> | globalThis.PointerEvent,
      start: number
    ) => {
      const at = toPage(svg, event.clientX, event.clientY)
      if (!at) return null
      const [azimuth, altitude] = stylusAngles(event)
      return inkPoint(
        at.x,
        at.y,
        pressureOf(event),
        (event.timeStamp - start) / 1000,
        azimuth,
        altitude
      )
    },
    [toPage]
  )

  const onPointerDown = useCallback(
    (event: PointerEvent<SVGSVGElement>) => {
      const touch = event.pointerType === "touch"
      if (event.pointerType === "pen") {
        penDown.current = event.pointerId
        // The page must not move under the pen while it draws.
        pan.current = null
      } else if (touch && penDown.current !== null) return
      const second = touch && fingerDraws && active.current?.touch === true
      if (second) {
        active.current = null
        setLive(null)
      }
      if (touch && (!fingerDraws || second || pan.current)) {
        if (pan.current) return
        pan.current = {
          id: event.pointerId,
          x: event.clientX,
          y: event.clientY,
          scroller: scrollerOf(event.currentTarget),
        }
        return
      }
      if (!enabled || active.current) return
      if (event.pointerType === "mouse" && event.button !== 0) return
      const svg = event.currentTarget
      // Capture keeps the stroke when the pen leaves the page mid-line. A
      // pointer the browser no longer knows refuses it, and draws anyway.
      try {
        svg.setPointerCapture(event.pointerId)
      } catch {
        // drawn without capture
      }
      event.preventDefault()
      // A stylus' eraser end, or its barrel button held, erases whatever
      // instrument is chosen.
      const erasing =
        settings.instrument === "eraser" ||
        (event.pointerType === "pen" && (event.buttons & 32) !== 0)
      onGestureStart?.()
      if (erasing) {
        active.current = {
          id: event.pointerId,
          start: event.timeStamp,
          stroke: null,
          erasing,
          touch,
        }
        const at = toPage(svg, event.clientX, event.clientY)
        if (at) onErase(at.x, at.y)
        return
      }
      const first = sample(svg, event, event.timeStamp)
      if (!first) return
      const tool = settings.instrument as InkTool
      const stroke: InkStroke = {
        id: randomId(),
        tool,
        color: settings.color,
        opacity: 1,
        baseWidth: settings.width,
        points: [first],
      }
      active.current = {
        id: event.pointerId,
        start: event.timeStamp,
        stroke,
        erasing: false,
        touch,
      }
      setLive(stroke)
    },
    [enabled, fingerDraws, onErase, onGestureStart, sample, settings, toPage]
  )

  const onPointerMove = useCallback(
    (event: PointerEvent<SVGSVGElement>) => {
      const panning = pan.current
      if (panning && panning.id === event.pointerId) {
        panning.scroller.scrollBy(
          panning.x - event.clientX,
          panning.y - event.clientY
        )
        panning.x = event.clientX
        panning.y = event.clientY
        return
      }
      const gesture = active.current
      if (!gesture || gesture.id !== event.pointerId) return
      const svg = event.currentTarget
      // Coalesced events are the samples the browser merged into this one:
      // a pen reports far more often than the screen redraws.
      const events = event.nativeEvent.getCoalescedEvents?.() ?? []
      const samples = events.length ? events : [event.nativeEvent]
      if (gesture.erasing) {
        for (const e of samples) {
          const at = toPage(svg, e.clientX, e.clientY)
          if (at) onErase(at.x, at.y)
        }
        return
      }
      const stroke = gesture.stroke
      if (!stroke) return
      const points = stroke.points.slice()
      for (const e of samples) {
        const point = sample(svg, e, gesture.start)
        if (!point) continue
        const last = points[points.length - 1]
        if (last && last[0] === point[0] && last[1] === point[1]) continue
        points.push(point)
      }
      const next = { ...stroke, points }
      gesture.stroke = next
      setLive(next)
    },
    [onErase, sample, toPage]
  )

  const finish = useCallback(
    (event: PointerEvent<SVGSVGElement>, keep: boolean) => {
      if (penDown.current === event.pointerId) penDown.current = null
      if (pan.current?.id === event.pointerId) {
        pan.current = null
        return
      }
      const gesture = active.current
      if (!gesture || gesture.id !== event.pointerId) return
      active.current = null
      setLive(null)
      if (keep && gesture.stroke) onStroke(gesture.stroke)
    },
    [onStroke]
  )

  return {
    live,
    handlers: {
      onPointerDown,
      onPointerMove,
      onPointerUp: (event: PointerEvent<SVGSVGElement>) => finish(event, true),
      onPointerCancel: (event: PointerEvent<SVGSVGElement>) =>
        finish(event, false),
    },
  }
}

/** What a finger's scroll moves: the nearest ancestor that scrolls, else the
 * document. */
function scrollerOf(from: Element): Element {
  for (let el = from.parentElement; el; el = el.parentElement) {
    const { overflowY } = getComputedStyle(el)
    if (
      (overflowY === "auto" || overflowY === "scroll") &&
      el.scrollHeight > el.clientHeight
    )
      return el
  }
  return document.scrollingElement ?? document.documentElement
}
