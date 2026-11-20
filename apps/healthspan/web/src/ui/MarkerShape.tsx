import type { CSSProperties } from "react"
import { markerPath, type TrendMarker } from "../lib/trendMath"

/** Radius of the 8 px circle marker. */
export const MARKER_R = 4

/** One chart marker in SVG: a `<circle>` for circles, a `<rect>` for
 * squares and a `<path>` for the other shapes, so a key's glyph is the same
 * shape as the mark it names. `grow` enlarges it: 1.5 for the active point,
 * 3 for the outer ring of a toned point. The caller paints it through
 * `style` or `className`. */
export function MarkerShape(props: {
  shape: TrendMarker
  cx: number
  cy: number
  grow?: number
  className?: string
  style?: CSSProperties
}) {
  const { shape, cx, cy, className, style } = props
  const grow = props.grow ?? 0
  if (shape === "circle")
    return (
      <circle
        className={className}
        style={style}
        cx={cx}
        cy={cy}
        r={Math.max(0.5, MARKER_R + grow)}
        data-marker={shape}
      />
    )
  if (shape === "square") {
    const h = Math.max(0.5, 3.5 + grow)
    return (
      <rect
        className={className}
        style={style}
        x={cx - h}
        y={cy - h}
        width={2 * h}
        height={2 * h}
        data-marker={shape}
      />
    )
  }
  return (
    <path
      className={className}
      style={style}
      d={markerPath(shape, cx, cy, grow)}
      data-marker={shape}
    />
  )
}
