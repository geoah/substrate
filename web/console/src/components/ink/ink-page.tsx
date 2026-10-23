/** One `.inkd` page as SVG: the paper, its pattern and the strokes in drawing
 * order. Each stroke's outline comes from lib/ink-render's cache, so a page
 * that gains a stroke computes one outline, not all of them. */

import { memo, useId, type ReactNode, type Ref, type SVGProps } from "react"

import {
  PAPER_COLOR,
  type InkDocument,
  type InkPage,
  type InkStroke,
} from "@/lib/ink"
import {
  backgroundPattern,
  cachedInkShape,
  cropBox,
  type InkRect,
  type InkShape,
} from "@/lib/ink-render"
import { cn } from "@/lib/utils"

export function StrokeShape({ shape }: { shape: InkShape | null }) {
  if (!shape) return null
  const style = shape.multiply
    ? { mixBlendMode: "multiply" as const }
    : undefined
  const opacity = shape.opacity < 1 ? shape.opacity : undefined
  return shape.type === "dot" ? (
    <circle
      cx={shape.cx}
      cy={shape.cy}
      r={shape.r}
      fill={shape.fill}
      fillOpacity={opacity}
      style={style}
    />
  ) : (
    <path d={shape.d} fill={shape.fill} fillOpacity={opacity} style={style} />
  )
}

const Strokes = memo(function Strokes({ strokes }: { strokes: InkStroke[] }) {
  return strokes.map((stroke, i) => (
    <StrokeShape key={stroke.id ?? i} shape={cachedInkShape(stroke)} />
  ))
})

function Pattern({
  page,
  width,
  height,
}: {
  page: InkPage
  width: number
  height: number
}) {
  const id = useId()
  const pattern = backgroundPattern(page.background, width, height)
  if (pattern.type === "none") return null
  if (pattern.type === "lines") {
    return (
      <path
        d={pattern.d}
        stroke={pattern.color}
        strokeWidth={1}
        fill="none"
        data-slot="ink-pattern"
      />
    )
  }
  // A tile shifted by half a spacing puts each dot at its center, so the
  // tiles inside the rectangle hold exactly the dots at (i·s, k·s), i, k ≥ 1.
  const half = pattern.spacing / 2
  return (
    <>
      <defs>
        <pattern
          id={id}
          x={half}
          y={half}
          width={pattern.spacing}
          height={pattern.spacing}
          patternUnits="userSpaceOnUse"
        >
          <circle cx={half} cy={half} r={1} fill={pattern.color} />
        </pattern>
      </defs>
      <rect
        x={half}
        y={half}
        width={pattern.columns * pattern.spacing}
        height={pattern.rows * pattern.spacing}
        fill={`url(#${id})`}
        data-slot="ink-pattern"
      />
    </>
  )
}

export interface InkPageProps {
  document: InkDocument
  pageIndex: number
  /** Show only the page's ink plus a margin, the way an embed does. */
  crop?: boolean
  /** Draw the strokes on a transparent page: the layer over a PDF. */
  transparent?: boolean
  className?: string
  /** Drawn over the strokes, in page coordinates: a stroke in progress. */
  children?: ReactNode
  svgRef?: Ref<SVGSVGElement>
  /** Overrides the view box, for a page drawn over a differently sized
   * surface (a PDF page). */
  viewBox?: InkRect
  label?: string
  /** The pointer handlers of a page being drawn on. */
  surfaceProps?: SVGProps<SVGSVGElement>
}

export function InkPageView({
  document,
  pageIndex,
  crop = false,
  transparent = false,
  className,
  children,
  svgRef,
  viewBox,
  label,
  surfaceProps,
}: InkPageProps) {
  const clipId = useId()
  const page = document.pages[pageIndex]
  const { width, height } = document.page
  if (!page) return null
  const box =
    viewBox ??
    (crop ? cropBox(page, width, height) : { x: 0, y: 0, width, height })
  return (
    <svg
      ref={svgRef}
      xmlns="http://www.w3.org/2000/svg"
      viewBox={`${box.x} ${box.y} ${box.width} ${box.height}`}
      role="img"
      aria-label={label ?? `Page ${pageIndex + 1}`}
      className={cn("block h-auto w-full", className)}
      style={{ aspectRatio: `${box.width} / ${box.height}` }}
      data-slot="ink-page"
      {...surfaceProps}
    >
      <defs>
        <clipPath id={clipId}>
          {/* A page over a PDF is the PDF page's size, not the note's. */}
          <rect
            width={viewBox ? viewBox.x + viewBox.width : width}
            height={viewBox ? viewBox.y + viewBox.height : height}
          />
        </clipPath>
      </defs>
      {!transparent && (
        <rect width={width} height={height} fill={PAPER_COLOR} />
      )}
      {!transparent && <Pattern page={page} width={width} height={height} />}
      <g clipPath={`url(#${clipId})`}>
        <Strokes strokes={page.strokes} />
        {children}
      </g>
    </svg>
  )
}
