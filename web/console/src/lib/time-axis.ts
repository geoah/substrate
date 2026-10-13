/** The x axis every observations chart draws time on: its margins, its
 * scale and its labelled boundaries. The timeline chart and the record
 * page's hypnogram share it, so a night's stages sit under the heart rate
 * drawn above them, minute for minute. */

import { linearScale, type Scale } from "@/lib/chart"
import {
  axisTicks,
  bucketWords,
  dayBucket,
  labelledTicks,
  type BucketSize,
  type TimeRange,
} from "@/lib/observations-time"

/** The plot's left edge: room for the y axis's labels. */
export const AXIS_LEFT = 52
/** The right margin, and the one a second axis needs for its labels. */
export const AXIS_RIGHT = 14
export const AXIS_RIGHT_LABELLED = 52

export interface TimeAxis {
  x: Scale
  /** The plot's right edge. */
  right: number
  /** Each labelled boundary: its index among the size's boundaries, where
   * its label sits, and the label. */
  labels: { i: number; at: number; text: string }[]
}

/** The range across a chart `width` wide, its size's boundaries labelled
 * whether a bucket holds anything there or not. A label needs about 84px,
 * so every k-th boundary is labelled, k from the boundaries' real spacing:
 * under a pixel apart, a quarter hour over a month is. A day, week or month
 * names a slot, not an instant: its label sits under the middle of the
 * slot, where its bars and points are drawn. */
export function timeAxis(
  domain: TimeRange,
  size: BucketSize,
  width: number,
  rightLabelled = false
): TimeAxis {
  const right = Math.max(
    AXIS_LEFT + 40,
    width - (rightLabelled ? AXIS_RIGHT_LABELLED : AXIS_RIGHT)
  )
  const x = linearScale([domain.start, domain.end], [AXIS_LEFT, right])
  const marks = axisTicks(domain, size)
  const spacing =
    marks.length > 1 ? x(marks[1]) - x(marks[0]) : right - AXIS_LEFT
  const every = spacing > 0 ? Math.max(1, Math.ceil(84 / spacing)) : 1
  const labelled = labelledTicks(marks, size, every)
  const slotted = dayBucket(size)
  const labels = labelled.map((i, n) => {
    const t = marks[i]
    const previous = n > 0 ? marks[labelled[n - 1]] : undefined
    const mid = slotted
      ? (x(Math.max(t, domain.start)) +
          x(Math.min(marks[i + 1] ?? domain.end, domain.end))) /
        2
      : x(t)
    const at = Math.min(Math.max(mid, AXIS_LEFT + 28), right - 28)
    return { i, at, text: bucketWords(t, size, previous) }
  })
  return { x, right, labels }
}
