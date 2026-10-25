/** The ink editor's tool state: which instrument, which color for ink and
 * for the highlighter, and which of three widths. */

import type { InkInstrument, PenSettings } from "./use-ink-drawing"

/** Ink colors, near the iPad app's defaults. */
export const INK_COLORS = [
  { value: "#1C1C1E", label: "Black" },
  { value: "#0A60D6", label: "Blue" },
  { value: "#D92D20", label: "Red" },
  { value: "#1F8A4C", label: "Green" },
  { value: "#7A4CC2", label: "Purple" },
] as const

/** Highlighter colors: light, because a marker multiplies with the paper. */
export const MARKER_COLORS = [
  { value: "#FFD60A", label: "Yellow" },
  { value: "#7FD8FF", label: "Blue" },
  { value: "#FF9AC8", label: "Pink" },
  { value: "#A6F07C", label: "Green" },
] as const

/** Three widths per tool, in page points: fine, medium, bold. The pen's
 * medium is the iPad app's default `baseWidth`. */
const WIDTHS: Record<Exclude<InkInstrument, "eraser">, number[]> = {
  pen: [1.5, 2.5, 4.5],
  pencil: [2, 3, 5],
  marker: [12, 20, 30],
}
export const WIDTH_LABELS = ["Fine", "Medium", "Bold"]

export interface ToolState {
  instrument: InkInstrument
  inkColor: string
  markerColor: string
  /** 0, 1 or 2: fine, medium, bold. */
  width: number
}

export const DEFAULT_TOOLS: ToolState = {
  instrument: "pen",
  inkColor: INK_COLORS[0].value,
  markerColor: MARKER_COLORS[0].value,
  width: 1,
}

/** What the drawing hook needs from the toolbar's state. */
export function penSettings(tools: ToolState): PenSettings {
  const tool = tools.instrument === "eraser" ? "pen" : tools.instrument
  return {
    instrument: tools.instrument,
    color: tool === "marker" ? tools.markerColor : tools.inkColor,
    width: WIDTHS[tool][tools.width] ?? WIDTHS[tool][1],
  }
}
