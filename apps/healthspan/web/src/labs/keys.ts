// The Labs chart key: one ChartKey under the toolbar, listing only the
// layers the visible charts draw with the current display settings.

import type { LabsDisplay } from "../lib/display"
import { UNKNOWN_SOURCE, type SourceMark } from "../lib/sourceMarkers"
import type { ChartKeyItem } from "../ui/ChartKey"

/** What the visible charts actually draw. Omitted fields count as drawn
 * (the history screen passes only what it knows). */
export interface LabsKeyDrawn {
  /** Source ids with a drawn point. */
  sources?: Set<string>
  /** Target zone tones some chart draws. */
  zones?: Set<"success" | "warning" | "danger">
  /** Some chart draws a lab range. */
  labRange?: boolean
  /** Some chart draws a trendline. */
  trend?: boolean
  /** Some chart hatches an empty span. */
  hatch?: boolean
  /** Some point has a danger ring (out of range, or in a poor zone). */
  outOfRange?: boolean
  /** Some point has a warning ring (off target in a borderline zone). */
  offTarget?: boolean
}

/** The Labs key: sources in the page-wide shape order, the results line,
 * then each layer the display settings enable and some visible chart
 * draws. Green, yellow and red are the catalogue's zones only; the lab
 * range is its own neutral, dotted glyph. */
export function labsKeyItems(
  display: LabsDisplay,
  markers: Map<string, SourceMark>,
  drawn: LabsKeyDrawn = {}
): ChartKeyItem[] {
  const items: ChartKeyItem[] = []
  for (const m of markers.values()) {
    if (m.id === UNKNOWN_SOURCE && !m.count) continue
    if (drawn.sources && !drawn.sources.has(m.id)) continue
    items.push({
      key: m.id,
      label: m.name,
      glyph: {
        kind: "marker",
        shape: display.sourceShapes ? m.marker : "circle",
      },
    })
  }
  items.push({
    key: "results",
    label: "Results",
    glyph: { kind: "line", stroke: "solid" },
  })
  if (display.trendline && drawn.trend !== false)
    items.push({
      key: "trend",
      label: "Trend, best fit over the shown dates",
      glyph: { kind: "line", tone: "info", stroke: "dashed" },
    })
  const zone = (t: "success" | "warning" | "danger") =>
    display.targetZones && (!drawn.zones || drawn.zones.has(t))
  if (zone("success"))
    items.push({
      key: "target-good",
      label: "Target: good",
      glyph: { kind: "band", tone: "success" },
    })
  if (zone("warning"))
    items.push({
      key: "target-borderline",
      label: "Target: borderline",
      glyph: { kind: "band", tone: "warning" },
    })
  if (zone("danger"))
    items.push({
      key: "target-poor",
      label: "Target: poor",
      glyph: { kind: "band", tone: "danger" },
    })
  if (display.labRange && drawn.labRange !== false)
    items.push({
      key: "lab-range",
      label: "Latest lab range",
      glyph: { kind: "band", tone: "neutral", stroke: "dotted" },
    })
  if (drawn.hatch !== false)
    items.push({
      key: "empty",
      label: "No results yet",
      glyph: { kind: "hatch" },
    })
  if (drawn.outOfRange !== false)
    items.push({
      key: "out-of-range",
      label: "Out of range or in a poor zone",
      glyph: { kind: "marker", shape: "circle", tone: "danger", hasRing: true },
    })
  if (drawn.offTarget !== false)
    items.push({
      key: "off-target",
      label: "Off target",
      glyph: {
        kind: "marker",
        shape: "circle",
        tone: "warning",
        hasRing: true,
      },
    })
  return items
}
