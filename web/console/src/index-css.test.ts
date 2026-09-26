/** The palette's reading promises, checked against the stylesheet itself so a
 * retuned token cannot quietly fall back below what a person reads. */

import { describe, expect, it } from "vitest"

// Vitest empties an imported stylesheet, `?raw` included, so the file is read
// from disk; the console's tsconfig carries no Node types, hence the cast.
const fs: { readFileSync: (path: URL, encoding: "utf8") => string } =
  await import(/* @vite-ignore */ "node:fs" as string)
const css = fs.readFileSync(new URL("./index.css", import.meta.url), "utf8")

function block(selector: string): string {
  const start = css.indexOf(`${selector} {`)
  return css.slice(start, css.indexOf("}", start))
}

function token(scope: string, name: string): string {
  const match = new RegExp(`--${name}:\\s*(#[0-9a-f]{6});`).exec(block(scope))
  if (!match) throw new Error(`--${name} is not a hex colour in ${scope}`)
  return match[1]
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

describe("the palette", () => {
  it("points text-faint at the text tone and keeps the decoration tone", () => {
    expect(css).toContain("--color-faint: var(--faint-text);")
    expect(css).toContain("--color-faint-deco: var(--faint);")
  })

  it("reads faint text more clearly than the decoration tone, on every surface", () => {
    for (const scope of [":root", ".dark"]) {
      const text = token(scope, "faint-text")
      const deco = token(scope, "faint")
      for (const surface of ["background", "sidebar", "panel"]) {
        const on = token(scope, surface)
        expect(contrast(text, on)).toBeGreaterThan(contrast(deco, on))
        expect(contrast(text, on)).toBeGreaterThanOrEqual(3.9)
      }
    }
  })

  it("keeps a light pill's word readable on its soft fill", () => {
    const pairs = [
      ["warning", "warn-soft"],
      ["ok", "ok-soft"],
      ["primary-text", "primary-soft"],
    ]
    for (const [ink, fill] of pairs) {
      expect(
        contrast(token(":root", ink), token(":root", fill))
      ).toBeGreaterThanOrEqual(4.5)
    }
  })
})
