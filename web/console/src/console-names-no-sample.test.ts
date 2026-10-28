/** The console gives no kind outside the seeded packages special treatment
 * (decision record 0166): how a sample's or a catalogue's kinds are
 * presented comes from record views, which are data. This holds the
 * console's own source to it, tests and fixtures aside: no string in it names
 * a sample package, a first-party catalogue (decision record 0192), a
 * provider, a shipped metric id or a sample's or a catalogue's kind by its
 * bare name. The names are read from the tree at test time (`samples/`, the
 * packages under `kinds/`, every metric record either tree ships), so a new
 * package is held to the rule the day it lands. */

import { describe, expect, it } from "vitest"
import { parseAllDocuments } from "yaml"

const sources = import.meta.glob<string>(["./**/*.ts", "./**/*.tsx"], {
  query: "?raw",
  import: "default",
  eager: true,
})

const sampleFiles = import.meta.glob<string>("../../../samples/*/*.yaml", {
  query: "?raw",
  import: "default",
  eager: true,
})

const providerFiles = import.meta.glob<string>(
  "../../../kinds/providers.substrate.reamde.dev/*/*.yaml",
  { query: "?raw", import: "default", eager: true }
)

const seedAuthorityFiles = import.meta.glob<string>(
  "../../../kinds/substrate.reamde.dev/*/*.yaml",
  { query: "?raw", import: "default", eager: true }
)

/** The directory a glob key names under its parent: `samples/<name>/…`. */
function dirOf(path: string, parent: string): string {
  const rest = path.slice(path.indexOf(`/${parent}/`) + parent.length + 2)
  return rest.slice(0, rest.indexOf("/"))
}

interface Doc {
  kind?: string
  metadata?: { id?: string }
}

function docsOf(raw: string): (Doc | null)[] {
  return parseAllDocuments(raw).map((doc) => doc.toJS() as Doc | null)
}

const BUNDLE_KIND = "substrate.reamde.dev/core/bundle"

/** The packages under the substrate's own authority, by directory: a
 * first-party catalogue carries a bundle document (kinds_test.go holds the
 * split), and every other one is seeded (`core`, `llm`, `console`,
 * `observations`). The console may name a seeded package, and a sample
 * sharing one's word (the `llm` sample) is held to the other patterns
 * only. */
const { SEEDED, CATALOGUES } = (() => {
  const all = new Set<string>()
  const catalogues = new Set<string>()
  for (const [path, raw] of Object.entries(seedAuthorityFiles)) {
    const dir = dirOf(path, "substrate.reamde.dev")
    all.add(dir)
    if (docsOf(raw).some((d) => d?.kind === BUNDLE_KIND)) catalogues.add(dir)
  }
  return {
    SEEDED: new Set([...all].filter((d) => !catalogues.has(d))),
    CATALOGUES: [...catalogues],
  }
})()

const SAMPLES = [
  ...new Set(Object.keys(sampleFiles).map((p) => dirOf(p, "samples"))),
].filter((s) => !SEEDED.has(s))

const PROVIDERS = [
  ...new Set(
    Object.keys(providerFiles).map((p) =>
      dirOf(p, "providers.substrate.reamde.dev")
    )
  ),
]

/** The files of the samples and the first-party catalogues: the domain
 * vocabulary the console reads only as data. */
const domainFiles = [
  ...Object.values(sampleFiles),
  ...Object.entries(seedAuthorityFiles)
    .filter(([path]) => !SEEDED.has(dirOf(path, "substrate.reamde.dev")))
    .map(([, raw]) => raw),
]

/** Every metric id the tree ships (`health/heart_rate`, `whoop/recovery`),
 * and every kind a sample or a catalogue declares by its bare name
 * (`sleep`, `workout`). */
const { METRIC_IDS, DOMAIN_KINDS } = (() => {
  const metrics = new Set<string>()
  const kinds = new Set<string>()
  for (const raw of domainFiles) {
    for (const d of docsOf(raw)) {
      const id = d?.metadata?.id
      if (!d?.kind || !id) continue
      if (d.kind.endsWith("/metric")) metrics.add(id)
      if (d.kind.endsWith("/core/kind")) kinds.add(id.split("/").pop()!)
    }
  }
  for (const raw of Object.values(providerFiles)) {
    for (const d of docsOf(raw)) {
      const id = d?.metadata?.id
      if (d?.kind?.endsWith("/metric") && id) metrics.add(id)
    }
  }
  return { METRIC_IDS: [...metrics], DOMAIN_KINDS: [...kinds] }
})()

/** Every kind and property name the seeded packages declare: the console
 * names those freely, whatever sample or catalogue declares a kind of the
 * same word. */
const CORE_WORDS = (() => {
  const words = new Set<string>()
  const walk = (value: unknown, underProperties: boolean) => {
    if (!value || typeof value !== "object") return
    for (const [key, inner] of Object.entries(value)) {
      if (underProperties) words.add(key)
      walk(inner, key === "properties")
    }
  }
  for (const [path, raw] of Object.entries(seedAuthorityFiles)) {
    if (!SEEDED.has(dirOf(path, "substrate.reamde.dev"))) continue
    for (const d of docsOf(raw)) {
      const id = d?.metadata?.id
      if (id) words.add(id.split("/").pop()!)
      walk(d, false)
    }
  }
  return words
})()

/** Files that are not the console's code: tests, their fixtures and setup. */
function exempt(path: string): boolean {
  return (
    /\.test\.tsx?$/.test(path) ||
    /test-fixtures\.ts$/.test(path) ||
    path.endsWith("/test-setup.ts")
  )
}

/** Declarations left out of the scan, each for a reason no data answers
 * yet. `WORDS` is the dictionary the name splitter reads for ANY kind's
 * name, so a sample's word there is a word, not a branch on a kind.
 * `PROPER` and `BRANDS` spell the shipped providers' names and colours,
 * which no provider declares as data; moving them into the providers'
 * bundles is the owner's open decision (commit efaa4235). */
const DICTIONARIES: Record<string, readonly string[]> = {
  "./lib/kind-names.ts": ["WORDS", "PROPER"],
  "./lib/actor-identity.ts": ["BRANDS"],
}

/** The source without its comments, which may name a sample to explain a
 * rule, and without the dictionaries above. */
function code(path: string, source: string): string {
  let body = source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1")
  for (const name of DICTIONARIES[path] ?? []) {
    body = body.replace(
      new RegExp(`(?:export )?const ${name}\\b[\\s\\S]*?\\n[\\]}]\\)?\\n`),
      ""
    )
  }
  return body
}

function escape(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
}

/** The patterns a name may not appear in: as a whole string, as a string's
 * first segment (`health/…`), or as the package segment of a kind
 * reference under any authority (`ada.example.com/health/…`). */
function packagePatterns(word: string, what: string) {
  const w = escape(word)
  return [
    { name: `${what} ${word}`, pattern: new RegExp(`["'\`]${w}["'\`]`) },
    {
      name: `${what} ${word} as a path's first segment`,
      pattern: new RegExp(`["'\`]${w}/[a-z]`),
    },
    {
      name: `${what} ${word} in a kind reference`,
      pattern: new RegExp(`[a-z0-9]\\.[a-z]{2,}/${w}/[a-z]`),
    },
  ]
}

const NAMES: { name: string; pattern: RegExp }[] = [
  ...SAMPLES.flatMap((s) => packagePatterns(s, "the sample package")),
  ...CATALOGUES.flatMap((c) => packagePatterns(c, "the catalogue")),
  ...PROVIDERS.flatMap((p) => packagePatterns(p, "the provider")),
  ...METRIC_IDS.flatMap((id) => {
    const last = id.slice(id.lastIndexOf("/") + 1)
    return [
      {
        name: `the shipped metric ${id}`,
        pattern: new RegExp(`["'\`][^"'\`\\n]*${escape(id)}\\b`),
      },
      // A metric's own word, where it is one no sentence uses
      // (`heart_rate`), also built by concatenation.
      ...(last.includes("_")
        ? [
            {
              name: `the shipped metric's word ${last}`,
              pattern: new RegExp(`["'\`/]${escape(last)}["'\`]`),
            },
          ]
        : []),
    ]
  }),
  // A sample's or a catalogue's kind by its bare name, where code branches
  // on it: compared to, or a case of a switch. Its name as a word (a "note"
  // on a row, a "page" of a table) is no branch. A word a seeded package
  // declares as a kind or a property (`metric`, `source`) is the seed's to
  // name.
  ...DOMAIN_KINDS.filter((k) => !CORE_WORDS.has(k)).map((k) => ({
    name: `a branch on the sample kind ${k}`,
    pattern: new RegExp(`(?:[!=]==?\\s*|\\bcase\\s+)["'\`]${escape(k)}["'\`]`),
  })),
  // `health` is also a sync's health, a field of its own, so only these
  // kind names are refused as keys.
  {
    name: "a sample's kind as an object key",
    pattern: /(^|[{,\s])(sleep|workout)\??\s*:/m,
  },
  {
    name: "a sample package by its shipped reference",
    pattern: /samples\.substrate\.reamde\.dev\/[a-z]/,
  },
  {
    name: "a provider by its shipped reference",
    pattern: /providers\.substrate\.reamde\.dev\/[a-z]/,
  },
]

/** Words a sample also uses as a name that mean something of the
 * console's own in one file, by the pattern they meet there. Each is
 * allowed in that file alone, so the same word anywhere else still fails. */
const OWN_WORDS: Record<string, readonly string[]> = {
  // An icon's name.
  "./lib/kind-glyph.ts": ["the sample package calendar"],
  "./lib/tools.ts": ["the sample package calendar"],
  // The plural of person.
  "./lib/kind-names.ts": ["the sample package people"],
  // A property a record's prose is read from.
  "./lib/record-schema.ts": ["the sample package notes"],
  // A page title's size, and a unit's printed note.
  "./components/identity/page-title.ts": ["a branch on the sample kind page"],
  "./lib/units.ts": ["a branch on the sample kind note"],
}

describe("the console's source", () => {
  const files = Object.entries(sources).filter(([path]) => !exempt(path))

  it("is read, and so are the names it is held to", () => {
    expect(files.length).toBeGreaterThan(100)
    expect(files.map(([path]) => path)).toContain("./pages/record.tsx")
    expect(SAMPLES).toEqual(expect.arrayContaining(["people", "tasks"]))
    expect(SAMPLES).not.toContain("llm")
    expect([...SEEDED].sort()).toEqual(
      ["console", "core", "llm", "observations"].sort()
    )
    expect(CATALOGUES).toEqual(
      expect.arrayContaining(["health", "labs", "environment"])
    )
    expect(PROVIDERS).toEqual(expect.arrayContaining(["whoop", "google"]))
    expect(METRIC_IDS).toEqual(
      expect.arrayContaining([
        "health/heart_rate",
        "labs/ldl_cholesterol",
        "environment/air_temperature",
        "whoop/recovery",
      ])
    )
    expect(DOMAIN_KINDS).toEqual(
      expect.arrayContaining(["sleep", "workout", "labresult", "task"])
    )
    expect(CORE_WORDS.has("observation")).toBe(true)
    expect(CORE_WORDS.has("labresult")).toBe(false)
  })

  it("names no sample, provider or shipped metric", () => {
    const found: string[] = []
    for (const [path, source] of files) {
      const body = code(path, source)
      for (const { name, pattern } of NAMES) {
        if (OWN_WORDS[path]?.includes(name)) continue
        const hit = pattern.exec(body)
        if (hit) found.push(`${path}: ${name} (${hit[0]})`)
      }
    }
    expect(found).toEqual([])
  })

  it("allows a word of the console's own only where it is one", () => {
    const names = new Set(NAMES.map((n) => n.name))
    for (const [path, allowed] of Object.entries(OWN_WORDS)) {
      expect(sources[path], path).toBeDefined()
      for (const name of allowed) expect(names.has(name), name).toBe(true)
    }
  })

  it("catches a name it is meant to", () => {
    const hits = (body: string) =>
      NAMES.filter(({ pattern }) => pattern.test(body)).map((n) => n.name)
    expect(hits('const k = "tasks"')).not.toEqual([])
    expect(hits('const k = "labs"')).not.toEqual([])
    expect(hits('const m = "whoop/recovery"')).not.toEqual([])
    expect(hits('const k = "substrate.reamde.dev/health/sleep"')).not.toEqual(
      []
    )
    expect(hits("const m = `${pkg}/heart_rate`")).not.toEqual([])
    expect(hits('if (provider === "whoop") {}')).not.toEqual([])
    expect(hits('const ref = "ada.example.com/people/person"')).not.toEqual([])
    expect(hits('const route = "/observations/timeline"')).toEqual([])
  })
})
