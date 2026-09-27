/** No text below 11.5px (review V3): the scale starts at 12, and a purpose
 * tag, a badge letter or a Developer heading under it is harder to read than
 * anything it sits beside. Holds every size written in pixels or rems in the
 * console's source; an `em` size scales with its parent and is held there. */

import { describe, expect, it } from "vitest"

const FLOOR = 11.5

const sources = import.meta.glob(
  ["/src/**/*.{ts,tsx,css}", "!/src/**/*.test.*"],
  {
    query: "?raw",
    import: "default",
    eager: true,
  }
) as Record<string, string>

/** Every `text-[<n>px]` and `text-[<n>rem]` below the floor, with its file. */
function belowFloor(): string[] {
  const out: string[] = []
  for (const [file, text] of Object.entries(sources)) {
    for (const m of text.matchAll(/text-\[(\d*\.?\d+)(px|rem)\]/g)) {
      const px = Number(m[1]) * (m[2] === "rem" ? 16 : 1)
      if (px < FLOOR) out.push(`${file}: ${m[0]}`)
    }
  }
  return out
}

describe("the type floor", () => {
  it("reads the console's source", () => {
    expect(Object.keys(sources).length).toBeGreaterThan(100)
  })

  it("writes no text size below 11.5px", () => {
    expect(belowFloor()).toEqual([])
  })
})
