/** Pointer input on an ink page: a pen, pencil or marker gesture becomes one
 * `.inkd` stroke, an eraser gesture names the strokes it touches. Points are
 * read in page coordinates through the SVG's own transform, so the page may
 * be drawn at any size. A stylus reports pressure and tilt; a mouse reports
 * neither, and gets the spec's defaults (§6).
 *
 * Once a pen has touched the page, a finger no longer draws: that is the
 * palm resting on the screen while the pen writes. */

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
  onStroke,
  onErase,
  onGestureStart,
}: {
  settings: PenSettings
  enabled: boolean
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
  } | null>(null)
  const penSeen = useRef(false)

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
      if (!enabled || active.current) return
      if (event.pointerType === "pen") penSeen.current = true
      else if (event.pointerType === "touch" && penSeen.current) return
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
      }
      setLive(stroke)
    },
    [enabled, onErase, onGestureStart, sample, settings, toPage]
  )

  const onPointerMove = useCallback(
    (event: PointerEvent<SVGSVGElement>) => {
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
