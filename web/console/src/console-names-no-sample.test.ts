/** The console gives no kind outside the seeded packages special treatment
 * (decision record 0166): how a sample's kinds are presented comes from
 * record views, which are data. This holds the console's own source to it,
 * tests and fixtures aside: no string in it names a sample package, a
 * sample's metric id, or the sleep and workout kinds the record page used
 * to recognise by name. */

import { describe, expect, it } from "vitest"

const sources = import.meta.glob<string>(["./**/*.ts", "./**/*.tsx"], {
  query: "?raw",
  import: "default",
  eager: true,
})

/** Files that are not the console's code: tests, their fixtures and setup.
 * `lib/kind-names.ts` holds a dictionary of words for splitting ANY kind's
 * name, so "sleep" and "workout" there are words, not a branch on a kind. */
function exempt(path: string): boolean {
  return (
    /\.test\.tsx?$/.test(path) ||
    /test-fixtures\.ts$/.test(path) ||
    path.endsWith("/test-setup.ts") ||
    path === "./lib/kind-names.ts"
  )
}

/** The source without its comments, which may name a sample to explain
 * a rule; a string is what code branches on. */
function code(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1")
}

const NAMES: { name: string; pattern: RegExp }[] = [
  {
    name: "a sample package's metric id",
    pattern: /["'`][^"'`\n]*\bhealth\/[a-z]/,
  },
  { name: "the health package", pattern: /["'`]health["'`]/ },
  { name: "the sleep kind", pattern: /["'`]sleep["'`]/ },
  { name: "the workout kind", pattern: /["'`]workout["'`]/ },
  {
    name: "a sample package by its shipped reference",
    pattern: /samples\.substrate\.reamde\.dev\/[a-z]/,
  },
]

describe("the console's source", () => {
  const files = Object.entries(sources).filter(([path]) => !exempt(path))

  it("is read", () => {
    expect(files.length).toBeGreaterThan(100)
    expect(files.map(([path]) => path)).toContain("./pages/record.tsx")
  })

  it("names no sample", () => {
    const found: string[] = []
    for (const [path, source] of files) {
      const body = code(source)
      for (const { name, pattern } of NAMES) {
        const hit = pattern.exec(body)
        if (hit) found.push(`${path}: ${name} (${hit[0]})`)
      }
    }
    expect(found).toEqual([])
  })
})
