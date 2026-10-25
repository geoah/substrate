/*! The rendering in this file is adapted from obsidian-inkidian
 * (https://github.com/toffalori/obsidian-inkidian), under its MIT License:
 *
 * Copyright (c) 2026 Thomas Toffalori
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to
 * deal in the Software without restriction, including without limitation the
 * rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
 * sell copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
 * FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS
 * IN THE SOFTWARE.
 */

/** Ink geometry: what the `.inkd` rendering model (§5 backgrounds, §8 ink)
 * draws, as SVG path data a component places. Ported from the Inkidian
 * plugin's `render.ts` (github.com/toffalori/obsidian-inkidian, MIT), so the
 * console and Obsidian draw a note the same way. */

import { getStroke } from "perfect-freehand"

import {
  backgroundColor,
  backgroundSpacing,
  backgroundType,
  strokeBaseWidth,
  strokeColor,
  strokeOpacity,
  strokePoints,
  strokeTool,
  type InkBackground,
  type InkPage,
  type InkStroke,
  type InkTool,
} from "@/lib/ink"

export interface InkRect {
  x: number
  y: number
  width: number
  height: number
}

/** One stroke, ready to draw: a filled outline path, or a dot for a stroke
 * without length (§8). */
export type InkShape =
  | {
      type: "path"
      d: string
      fill: string
      opacity: number
      multiply: boolean
    }
  | {
      type: "dot"
      cx: number
      cy: number
      r: number
      fill: string
      opacity: number
      multiply: boolean
    }

/** The visible width at a point: the pen thins and thickens with pressure,
 * from 0.25 to 1.75 times `baseWidth`; pencil and marker keep their width. */
export function strokeWidthAt(tool: InkTool, baseWidth: number, p: number) {
  return tool === "pen" ? baseWidth * (0.25 + 1.5 * p) : baseWidth
}

/** §8 alpha. An SVG path has one alpha, so pencil and marker use the mean
 * pressure, which the spec allows. */
export function strokeAlpha(tool: InkTool, opacity: number, meanP: number) {
  switch (tool) {
    case "pen":
      return opacity
    case "pencil":
      return opacity * (0.55 + 0.45 * meanP)
    case "marker":
      return opacity * (0.45 + 0.45 * meanP)
  }
}

const shapes = new WeakMap<InkStroke, InkShape | null>()

/** `inkShape`, computed once per stroke object. A stroke is never mutated
 * (lib/ink's edits replace it), so the object is the cache key. */
export function cachedInkShape(stroke: InkStroke): InkShape | null {
  let shape = shapes.get(stroke)
  if (shape === undefined) {
    shape = inkShape(stroke)
    shapes.set(stroke, shape)
  }
  return shape
}

export function inkShape(stroke: InkStroke): InkShape | null {
  const points = strokePoints(stroke)
  if (points.length === 0) return null
  const tool = strokeTool(stroke)
  const baseWidth = strokeBaseWidth(stroke)
  const meanP = points.reduce((sum, [, , p]) => sum + p, 0) / points.length
  const fill = strokeColor(stroke)
  const opacity = round(strokeAlpha(tool, strokeOpacity(stroke), meanP), 3)
  const multiply = tool === "marker"
  const [x0, y0, p0] = points[0]
  if (points.every(([x, y]) => x === x0 && y === y0)) {
    return {
      type: "dot",
      cx: x0,
      cy: y0,
      r: round(strokeWidthAt(tool, baseWidth, p0) / 2, 2),
      fill,
      opacity,
      multiply,
    }
  }
  const outline = getStroke(points, {
    size: baseWidth,
    // perfect-freehand with thinning 0.75 and linear easing draws exactly
    // baseWidth · (0.25 + 1.5·p), the §8 pen width.
    thinning: tool === "pen" ? 0.75 : 0,
    smoothing: 0.5,
    streamline: 0,
    easing: (p) => p,
    simulatePressure: false,
    last: true,
    start: { cap: true, taper: 0 },
    end: { cap: true, taper: 0 },
  })
  return { type: "path", d: outlinePath(outline), fill, opacity, multiply }
}

/** A closed path through an outline, smoothed with quadratic curves through
 * the midpoints. */
export function outlinePath(points: number[][]): string {
  if (points.length === 0) return ""
  if (points.length < 4)
    return `M${points.map(([x, y]) => `${round(x, 2)},${round(y, 2)}`).join("L")}Z`
  const mid = (a: number[], b: number[]) =>
    `${round((a[0] + b[0]) / 2, 2)},${round((a[1] + b[1]) / 2, 2)}`
  let path = `M${round(points[0][0], 2)},${round(points[0][1], 2)}Q${round(points[1][0], 2)},${round(points[1][1], 2)} ${mid(points[1], points[2])}T`
  for (let i = 2; i < points.length - 1; i++)
    path += `${mid(points[i], points[i + 1])} `
  return `${path.trimEnd()}Z`
}

/** The most lines a pattern draws across one side of a page. A4 at the
 * default spacing is 54; a document asking for more than this (a tiny
 * `spacing`, a vast page) gets blank paper, because the lines would be finer
 * than a screen shows and building them could exhaust the page's memory. */
export const MAX_PATTERN_LINES = 1000

/** The background pattern (§5) as one path of 1 pt lines, or the dot centers
 * for `dots`. Only lines and dots strictly inside the page are drawn. */
export function backgroundPattern(
  background: InkBackground | undefined,
  width: number,
  height: number
):
  | { type: "none" }
  | { type: "lines"; d: string; color: string }
  | {
      type: "dots"
      spacing: number
      columns: number
      rows: number
      color: string
    } {
  const type = backgroundType(background)
  if (type === "blank") return { type: "none" }
  const spacing = backgroundSpacing(background)
  const color = backgroundColor(background)
  const rows = lineCount(height, spacing)
  const columns = type === "lined" ? 0 : lineCount(width, spacing)
  if (!(rows <= MAX_PATTERN_LINES && columns <= MAX_PATTERN_LINES))
    return { type: "none" }
  const horizontal = Array.from(
    { length: rows },
    (_, k) => `M0 ${round((k + 1) * spacing, 2)}H${round(width, 2)}`
  )
  const vertical = Array.from(
    { length: columns },
    (_, k) => `M${round((k + 1) * spacing, 2)} 0V${round(height, 2)}`
  )
  switch (type) {
    case "lined":
      return rows > 0
        ? { type: "lines", d: horizontal.join(""), color }
        : { type: "none" }
    case "grid":
      return rows + columns > 0
        ? { type: "lines", d: [...horizontal, ...vertical].join(""), color }
        : { type: "none" }
    case "dots":
      return { type: "dots", spacing, columns, rows, color }
  }
}

export function lineCount(length: number, spacing: number): number {
  return Math.max(0, Math.ceil(length / spacing) - 1)
}

/** The area a page's ink covers, half the widest stroke included; null for a
 * page without ink. */
export function inkBounds(strokes: InkStroke[]): InkRect | null {
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const stroke of strokes) {
    const tool = strokeTool(stroke)
    const half = strokeWidthAt(tool, strokeBaseWidth(stroke), 1) / 2
    for (const [x, y] of strokePoints(stroke)) {
      minX = Math.min(minX, x - half)
      minY = Math.min(minY, y - half)
      maxX = Math.max(maxX, x + half)
      maxY = Math.max(maxY, y + half)
    }
  }
  return minX === Infinity
    ? null
    : { x: minX, y: minY, width: maxX - minX, height: maxY - minY }
}

/** The view box of a cropped preview: the page's ink plus `margin`, kept on
 * the page, so a small sketch does not bring a whole empty page along. A page
 * without ink is shown whole. */
export function cropBox(
  page: InkPage,
  width: number,
  height: number,
  margin = 32
): InkRect {
  const ink = inkBounds(page.strokes)
  if (!ink) return { x: 0, y: 0, width, height }
  const x = Math.max(0, ink.x - margin)
  const y = Math.max(0, ink.y - margin)
  const w = Math.min(width, ink.x + ink.width + margin) - x
  const h = Math.min(height, ink.y + ink.height + margin) - y
  return w > 0 && h > 0
    ? { x, y, width: w, height: h }
    : { x: 0, y: 0, width, height }
}

/** The strokes an eraser of `radius` touches at (x, y): within reach of a
 * stroke's polyline, its half width included. The strokes themselves, not
 * their ids: a file may repeat an id or leave it out, and erasing by id would
 * take every stroke that shares one. */
export function strokesHit(
  strokes: InkStroke[],
  x: number,
  y: number,
  radius: number
): InkStroke[] {
  const hit: InkStroke[] = []
  for (const stroke of strokes) {
    const points = strokePoints(stroke)
    const reach =
      radius + strokeWidthAt(strokeTool(stroke), strokeBaseWidth(stroke), 1) / 2
    if (points.length === 1) {
      if (Math.hypot(points[0][0] - x, points[0][1] - y) <= reach)
        hit.push(stroke)
      continue
    }
    for (let i = 1; i < points.length; i++) {
      const [ax, ay] = points[i - 1]
      const [bx, by] = points[i]
      if (segmentDistance(x, y, ax, ay, bx, by) <= reach) {
        hit.push(stroke)
        break
      }
    }
  }
  return hit
}

function segmentDistance(
  px: number,
  py: number,
  ax: number,
  ay: number,
  bx: number,
  by: number
): number {
  const dx = bx - ax
  const dy = by - ay
  const length = dx * dx + dy * dy
  const t =
    length === 0
      ? 0
      : Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / length))
  return Math.hypot(px - (ax + t * dx), py - (ay + t * dy))
}

function round(value: number, decimals: number): number {
  const factor = 10 ** decimals
  return Math.round(value * factor) / factor
}
