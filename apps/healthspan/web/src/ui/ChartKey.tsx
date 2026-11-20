import { cx } from "@substrate/react"
import { useId, type HTMLAttributes } from "react"
import type { TrendMarker } from "../lib/trendMath"
import { MarkerShape } from "./MarkerShape"
import { FILL, INK, type Tone } from "./tone"

/** What a key item draws before its label, drawn as the chart draws the
 * mark it names. A glyph with no `tone` is the label's ink; `neutral` is
 * the muted grey (the baseline, the lab range, a reference outline). */
export type ChartKeyGlyph =
  | { kind: "marker"; shape: TrendMarker; tone?: Tone; hasRing?: boolean }
  | { kind: "line"; tone?: Tone; stroke?: "solid" | "dashed" | "dotted" }
  /** A band: a `fill` wash (with stroked edges when `stroke` is set) or an
   * `outline` (edges only, dashed by default). */
  | {
      kind: "band"
      tone: Tone
      variant?: "fill" | "outline"
      stroke?: "dashed" | "dotted"
    }
  | { kind: "hatch" }
  /** A ZoneBar pointer, above or below the track. */
  | { kind: "pointer"; side: "above" | "below" }
  /** A ZoneBar notch: `target` (default) solid, `baseline` hollow. */
  | { kind: "tick"; variant?: "target" | "baseline" }

export interface ChartKeyItem {
  /** Stable id; matched by `current` and by a TrendPoint's `series`. */
  key: string
  label: string
  glyph: ChartKeyGlyph
}

export interface ChartKeyProps extends Omit<
  HTMLAttributes<HTMLUListElement>,
  "children"
> {
  /** The key's accessible name ("Chart key"). */
  label: string
  items: ChartKeyItem[]
  /** `md` for a screen's key, `sm` under one chart. Default `md`. */
  size?: "sm" | "md"
  /** The item emphasised: the source under a crosshair. */
  current?: string
}

const W = 14
const H = 12

/** A glyph's ink: none is the label, neutral the muted grey. */
function ink(tone: Tone | undefined): string {
  if (!tone) return "var(--sx-label)"
  if (tone === "neutral") return "var(--sx-secondary)"
  return INK[tone]
}

/** The 14 by 12 glyph of one key item. */
function Glyph({ glyph, hatchId }: { glyph: ChartKeyGlyph; hatchId: string }) {
  switch (glyph.kind) {
    case "marker":
      return (
        <>
          {glyph.hasRing && (
            <MarkerShape
              shape={glyph.shape}
              cx={W / 2}
              cy={H / 2}
              grow={1.5}
              style={{
                fill: "none",
                stroke: ink(glyph.tone),
                strokeWidth: 1.5,
              }}
            />
          )}
          <MarkerShape
            shape={glyph.shape}
            cx={W / 2}
            cy={H / 2}
            grow={glyph.hasRing ? -1 : 0}
            style={{ fill: ink(glyph.tone) }}
          />
        </>
      )
    case "line": {
      const stroke = glyph.stroke ?? "solid"
      // The trend line is its own blue wherever it is drawn.
      const color =
        glyph.tone === "info" && stroke === "dashed"
          ? "var(--hs-trend)"
          : ink(glyph.tone)
      return (
        <line
          x1={1}
          x2={W - 1}
          y1={H / 2}
          y2={H / 2}
          style={{
            stroke: color,
            strokeWidth: stroke === "solid" ? 2 : 1.5,
            strokeDasharray:
              stroke === "dashed" ? "4 2" : stroke === "dotted" ? "1 3" : "",
            strokeLinecap: stroke === "dashed" ? "butt" : "round",
          }}
        />
      )
    }
    case "band": {
      const neutral = glyph.tone === "neutral"
      const edge = neutral ? "var(--hs-range-edge)" : INK[glyph.tone]
      const edgeStroke = glyph.stroke ?? "dashed"
      const edges = (
        <g
          style={{
            stroke: edge,
            strokeWidth: edgeStroke === "dotted" ? 1 : 1.5,
            strokeDasharray: edgeStroke === "dotted" ? "1 3" : "4 3",
            strokeLinecap: edgeStroke === "dotted" ? "round" : "butt",
          }}
        >
          <line x1={0} x2={W} y1={2.5} y2={2.5} />
          <line x1={0} x2={W} y1={H - 2.5} y2={H - 2.5} />
        </g>
      )
      if (glyph.variant === "outline") return edges
      // A 14 px swatch at a plot's wash strength is invisible, so the key
      // doubles it and outlines it in the tone.
      const wash = neutral
        ? "color-mix(in srgb, var(--hs-zone-neutral) 70%, transparent)"
        : `color-mix(in srgb, ${FILL[glyph.tone]} 28%, transparent)`
      return glyph.stroke ? (
        <>
          <rect x={0} y={2.5} width={W} height={H - 5} style={{ fill: wash }} />
          {edges}
        </>
      ) : (
        <rect
          x={0.5}
          y={1.5}
          width={W - 1}
          height={H - 3}
          style={{
            fill: wash,
            stroke: `color-mix(in srgb, ${neutral ? "var(--hs-range-edge)" : FILL[glyph.tone]} 60%, transparent)`,
            strokeWidth: 1,
          }}
        />
      )
    }
    case "hatch":
      return (
        <>
          <defs>
            <pattern
              id={hatchId}
              width={4}
              height={4}
              patternUnits="userSpaceOnUse"
              patternTransform="rotate(45)"
            >
              <line
                x1={0}
                y1={0}
                x2={0}
                y2={4}
                style={{ stroke: "var(--hs-hatch)", strokeWidth: 1 }}
              />
            </pattern>
          </defs>
          <rect
            x={0.5}
            y={1.5}
            width={W - 1}
            height={H - 3}
            fill={`url(#${hatchId})`}
            style={{ stroke: "var(--hs-hatch)", strokeWidth: 1 }}
          />
        </>
      )
    case "pointer":
      return (
        <>
          <line
            x1={0}
            x2={W}
            y1={H / 2}
            y2={H / 2}
            style={{ stroke: "var(--hs-zone-neutral)", strokeWidth: 3 }}
          />
          <path
            style={{ fill: "var(--sx-label)" }}
            d={
              glyph.side === "above"
                ? `M${W / 2 - 4},0.5h8l-4,4.5Z`
                : `M${W / 2 - 4},${H - 0.5}h8l-4,-4.5Z`
            }
          />
        </>
      )
    case "tick":
      return (
        <>
          <line
            x1={0}
            x2={W}
            y1={H / 2}
            y2={H / 2}
            style={{ stroke: "var(--hs-zone-neutral)", strokeWidth: 3 }}
          />
          {glyph.variant === "baseline" ? (
            <rect
              x={W / 2 - 3}
              y={1.75}
              width={6}
              height={H - 3.5}
              rx={1.5}
              style={{
                fill: "none",
                stroke: "var(--sx-label)",
                strokeWidth: 1.5,
              }}
            />
          ) : (
            <line
              x1={W / 2}
              x2={W / 2}
              y1={1}
              y2={H - 1}
              style={{ stroke: "var(--sx-label)", strokeWidth: 2 }}
            />
          )}
        </>
      )
  }
}

/**
 * The one key for a screen's charts, or for one chart: each item a small
 * glyph drawn as its mark is drawn (a source's marker, the results line,
 * the dashed trend, a zone's wash, the lab range, the hatching, a ring, a
 * bar's pointer or notch) and its label, wrapping as a strip. `current`
 * emphasises one item. Status is never colour alone: the label names what
 * the colour means.
 */
export function ChartKey({
  label,
  items,
  size = "md",
  current,
  className,
  ...rest
}: ChartKeyProps) {
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, "")
  return (
    <ul
      {...rest}
      // A named list: `list-style: none` can drop the implicit role.
      role="list"
      aria-label={label}
      className={cx(
        "m-0 flex list-none flex-wrap items-center gap-x-4 gap-y-1 p-0 text-secondary",
        size === "sm" ? "text-[12px] leading-4" : "text-[13px] leading-5",
        className
      )}
    >
      {items.map((item) => (
        <li
          key={item.key}
          data-key={item.key}
          className={cx(
            "inline-flex min-w-0 items-center gap-1.5",
            item.key === current && "text-label"
          )}
        >
          <svg
            className="shrink-0 overflow-visible"
            width={W}
            height={H}
            viewBox={`0 0 ${W} ${H}`}
            aria-hidden
          >
            <Glyph
              glyph={item.glyph}
              hatchId={`${uid}-${item.key}-hatch`.replace(
                /[^a-zA-Z0-9_-]/g,
                ""
              )}
            />
          </svg>
          <span>{item.label}</span>
        </li>
      ))}
    </ul>
  )
}
