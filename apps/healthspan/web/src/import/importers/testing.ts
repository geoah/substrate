// What an importer's tests build their inputs and their context from, and
// compare a plan with its golden fixture by. Only tests import it: it reads
// the shared catalogues' YAML and the seeded unit graph from the
// repository's tree through node:fs.

import { readdirSync, readFileSync } from "node:fs"
import { join } from "node:path"
import { Effect, type Result } from "effect"
import { zlibSync } from "fflate"
import type { MetricRecord } from "../../catalogue/types"
import { parseUnitGraph, type UnitGraph } from "../../labs/units"
import { mediaTypeOf } from "./shared/documents"
import type {
  Context,
  Input,
  InputFile,
  KnownSource,
  LeftOut,
  Plan,
  PlannedRecord,
  RecordKey,
  WriteMode,
} from "./types"

// Paths, not URL objects: under jsdom, `URL` is jsdom's, which node:fs
// does not take.
const ROOT = join(import.meta.dirname, "../../../../../..")
const KINDS_DIR = join(ROOT, "kinds/substrate.reamde.dev")
const SEED = join(ROOT, "web/console/src/lib/unit-graph.shipped.json")

// ---- inputs ----

/** An input of invented files by their paths; a string is a file's text,
 * written as UTF-8. Each file's type is its extension's. */
export function inputFromRecord(
  name: string,
  files: Readonly<Record<string, string | Uint8Array>>
): Input {
  const list: InputFile[] = Object.entries(files)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([path, content]) => {
      const bytes =
        typeof content === "string"
          ? new TextEncoder().encode(content)
          : content
      return {
        path,
        size: bytes.length,
        type: mediaTypeOf(path, ""),
        bytes: () => Promise.resolve(bytes.slice()),
        text: () => Promise.resolve(new TextDecoder().decode(bytes)),
      }
    })
  return { name, files: list }
}

// ---- the shared catalogues ----

/** The YAML documents of a package's file, split on `---`. */
function documents(file: string): string[] {
  return readFileSync(file, "utf8").split(/^---$/m)
}

function unquote(s: string): string {
  const t = s.trim()
  if (
    t.length >= 2 &&
    ((t.startsWith('"') && t.endsWith('"')) ||
      (t.startsWith("'") && t.endsWith("'")))
  )
    return t.slice(1, -1)
  return t
}

function field(doc: string, indent: number, name: string): string | undefined {
  const m = new RegExp(`^ {${indent}}${name}: (.+)$`, "m").exec(doc)
  return m ? unquote(m[1]) : undefined
}

const UNIT_REF = "substrate.reamde.dev/core/unit/"

let metricsCache: MetricRecord[] | undefined

/** Every metric record the shared `labs` and `health` catalogues ship, as
 * the context's `metrics` hold them. */
export function sharedMetrics(): MetricRecord[] {
  if (metricsCache) return metricsCache
  const out: MetricRecord[] = []
  for (const pkg of ["labs", "health"])
    for (const doc of documents(join(KINDS_DIR, pkg, "metrics.yaml"))) {
      if (!/^kind: substrate\.reamde\.dev\/observations\/metric$/m.test(doc))
        continue
      const id = field(doc, 2, "id")
      const name = field(doc, 4, "name")
      if (!id || !name) continue
      const unit = new RegExp(
        `^ {4}unit:\\n\\s+ref: ${UNIT_REF.replace(/\./g, "\\.")}(\\S+)$`,
        "m"
      ).exec(doc)?.[1]
      const codes = [...doc.matchAll(/- system: (.+)\n\s+code: (.+)$/gm)].map(
        (m) => ({ system: unquote(m[1]), code: unquote(m[2]) })
      )
      const precision = field(doc, 4, "displayPrecision")
      out.push({
        id,
        name,
        unit: unit ? unquote(unit) : undefined,
        codes,
        category: field(doc, 4, "category"),
        precision: precision === undefined ? undefined : Number(precision),
      })
    }
  metricsCache = out
  return out
}

interface UnitRow {
  id: string
  code?: string
  symbol?: string
  aliases: string[]
  root: string
  factor: number
  offset: number
}

let graphCache: UnitGraph | undefined

/** The unit graph a repository holding the shared `labs` and `health`
 * catalogues answers `GET /api/v1/units` with: the seeded units, and each
 * package's own, rooted through its conversions. */
export function sharedUnits(): UnitGraph {
  if (graphCache) return graphCache
  const seed = JSON.parse(readFileSync(SEED, "utf8")) as {
    units: {
      id: string
      code?: string
      symbol?: string
      aliases?: string[]
      root: string
      factor: string | number
      offset?: string | number
    }[]
  }
  const rows = new Map<string, UnitRow>()
  const rational = (v: string | number | undefined): number => {
    if (typeof v === "number") return v
    if (!v) return 0
    const [n, d] = v.split("/")
    return d === undefined ? Number(n) : Number(n) / Number(d)
  }
  for (const u of seed.units)
    rows.set(u.id, {
      id: u.id,
      code: u.code,
      symbol: u.symbol,
      aliases: u.aliases ?? [],
      root: u.root,
      factor: rational(u.factor),
      offset: rational(u.offset),
    })
  const added = new Set<string>()
  const conversions: { from: string; to: string; factor: number }[] = []
  for (const pkg of ["labs", "health"]) {
    const dir = join(KINDS_DIR, pkg)
    for (const name of readdirSync(dir).sort()) {
      if (!name.endsWith(".yaml")) continue
      for (const doc of documents(join(dir, name))) {
        const kind = /^kind: (\S+)$/m.exec(doc)?.[1]
        const id = field(doc, 2, "id")
        if (!id) continue
        if (kind === "substrate.reamde.dev/core/unit" && !rows.has(id)) {
          const aliases = /^ {4}aliases:\n((?: {6}- .+\n?)+)/m.exec(doc)
          rows.set(id, {
            id,
            code: field(doc, 4, "code"),
            symbol: field(doc, 4, "symbol"),
            aliases: aliases
              ? aliases[1]
                  .split("\n")
                  .filter(Boolean)
                  .map((l) => unquote(l.replace(/^ {6}- /, "")))
              : [],
            root: id,
            factor: 1,
            offset: 0,
          })
          added.add(id)
        }
        if (kind === "substrate.reamde.dev/core/conversion") {
          const ends = [...doc.matchAll(/^ {4}(from|to):\n\s+ref: (.+)$/gm)]
          const from = ends.find((e) => e[1] === "from")?.[2]
          const to = ends.find((e) => e[1] === "to")?.[2]
          const factor = field(doc, 4, "factor")
          if (from && to && factor)
            conversions.push({
              from: unquote(from).replace(UNIT_REF, ""),
              to: unquote(to).replace(UNIT_REF, ""),
              factor: rational(factor),
            })
        }
      }
    }
  }
  // An added unit joins the component of the unit a conversion names:
  // one `from` is `factor` of `to`, and a value in the root is a value in
  // the unit times the unit's factor.
  for (let changed = true; changed;) {
    changed = false
    for (const c of conversions) {
      const from = rows.get(c.from)
      const to = rows.get(c.to)
      if (!from || !to) continue
      if (added.has(from.id) && from.root === from.id && to.root !== from.id) {
        from.root = to.root
        from.factor = c.factor * to.factor
        added.delete(from.id)
        changed = true
      } else if (added.has(to.id) && to.root === to.id && from.root !== to.id) {
        to.root = from.root
        to.factor = from.factor / c.factor
        added.delete(to.id)
        changed = true
      }
    }
  }
  graphCache = parseUnitGraph({
    units: [...rows.values()].map((u) => ({
      id: u.id,
      code: u.code,
      symbol: u.symbol,
      aliases: u.aliases,
      root: u.root,
      factor: u.factor,
      offset: u.offset,
    })),
  })
  return graphCache
}

/** What a fake context is built from; everything has a default. */
export interface FakeContextOptions {
  /** Default 2026-01-01T12:00:00Z. */
  today?: Date
  /** The ids of the shared metrics the repository holds; every shared
   * metric by default. */
  metrics?: readonly string[]
  /** Metrics besides the shared ones (a repository's own). */
  extraMetrics?: readonly MetricRecord[]
  sources?: readonly KnownSource[]
  answers?: Readonly<Record<string, string>>
  ownerContact?: string
  units?: UnitGraph
}

/** A context as the import screen hands an importer one, from the shared
 * catalogues. */
export function fakeContext(options: FakeContextOptions = {}): Context {
  const all = sharedMetrics()
  const wanted = options.metrics ? new Set(options.metrics) : undefined
  const ctx: Context = {
    today: options.today ?? new Date("2026-01-01T12:00:00Z"),
    metrics: [
      ...(wanted ? all.filter((m) => wanted.has(m.id)) : all),
      ...(options.extraMetrics ?? []),
    ],
    units: options.units ?? sharedUnits(),
    sources: options.sources ?? [],
    answers: options.answers ?? {},
  }
  if (options.ownerContact) ctx.ownerContact = options.ownerContact
  return ctx
}

/** An effect's outcome, its typed failure kept as a value, for a test to
 * assert either side of. */
export function runResult<A, E>(
  effect: Effect.Effect<A, E>
): Promise<Result.Result<A, E>> {
  return Effect.runPromise(Effect.result(effect))
}

// ---- plans and golden fixtures ----

/** A planned record as a golden fixture holds it: JSON, no undefined. */
export interface RecordView {
  kind: string
  namespace: string
  key: string
  mode: WriteMode
  revision?: number
  createOnly?: string[]
  properties: Record<string, unknown>
}

/** A plan as a golden fixture holds it: every record in order, each
 * document by its digest, name, type and size (no bytes), what was left
 * out. */
export interface PlanView {
  importer: string
  name: string
  source?: RecordKey
  records: RecordView[]
  documents: { digest: string; name: string; mediaType: string; size: number }[]
  left: LeftOut[]
}

const json = <T>(v: T): T => JSON.parse(JSON.stringify(v)) as T

/** A plan as JSON a golden fixture is written from and compared with. */
export function viewPlan(plan: Plan): PlanView {
  const view: PlanView = {
    importer: plan.importer,
    name: plan.name,
    records: plan.records.map((r) => {
      const out: RecordView = {
        kind: r.kind,
        namespace: r.namespace,
        key: r.key,
        mode: r.mode,
        properties: json(r.properties),
      }
      if (r.revision !== undefined) out.revision = r.revision
      if (r.createOnly) out.createOnly = [...r.createOnly]
      return out
    }),
    documents: plan.documents.map((d) => ({
      digest: d.digest,
      name: d.name,
      mediaType: d.mediaType,
      size: d.bytes.length,
    })),
    left: plan.left.map((l) => ({ what: l.what, why: l.why })),
  }
  if (plan.source) view.source = json(plan.source)
  return view
}

const show = (v: unknown) => JSON.stringify(v) ?? "undefined"

function diffValue(
  at: string,
  got: unknown,
  want: unknown,
  out: string[]
): void {
  if (Array.isArray(want) && Array.isArray(got)) {
    if (got.length !== want.length)
      out.push(`${at}: ${want.length} items expected, ${got.length} found`)
    for (let i = 0; i < Math.min(got.length, want.length); i++)
      diffValue(`${at}[${i}]`, got[i], want[i], out)
    return
  }
  const isObject = (v: unknown): v is Record<string, unknown> =>
    !!v && typeof v === "object" && !Array.isArray(v)
  if (isObject(want) && isObject(got)) {
    const keys = [
      ...new Set([...Object.keys(want), ...Object.keys(got)]),
    ].sort()
    for (const k of keys) diffValue(at ? `${at}.${k}` : k, got[k], want[k], out)
    return
  }
  if (show(got) !== show(want))
    out.push(`${at}: expected ${show(want)}, found ${show(got)}`)
}

const identity = (r: { kind: string; namespace: string; key: string }) =>
  `${r.kind} ${r.namespace} ${r.key}`

/** How a plan differs from its golden fixture, one line per difference
 * (empty when it does not): records by kind, namespace and key, then their
 * order, mode, revision and every property; documents by digest; what was
 * left out. */
export function planDiff(
  actual: Plan | PlanView,
  expected: PlanView
): string[] {
  const got =
    "documents" in actual && actual.documents.some((d) => "bytes" in d)
      ? viewPlan(actual as Plan)
      : json(actual as PlanView)
  const out: string[] = []
  if (got.importer !== expected.importer)
    out.push(`importer: expected ${expected.importer}, found ${got.importer}`)
  if (got.name !== expected.name)
    out.push(`name: expected ${show(expected.name)}, found ${show(got.name)}`)
  diffValue("source", got.source, expected.source, out)
  const want = new Map(expected.records.map((r) => [identity(r), r]))
  const have = new Map(got.records.map((r) => [identity(r), r]))
  for (const id of want.keys()) if (!have.has(id)) out.push(`${id}: missing`)
  for (const id of have.keys())
    if (!want.has(id)) out.push(`${id}: not expected`)
  const order = got.records.map(identity).filter((id) => want.has(id))
  const wantOrder = expected.records.map(identity).filter((id) => have.has(id))
  const moved = order.findIndex((id, i) => id !== wantOrder[i])
  if (moved >= 0)
    out.push(
      `${order[moved]}: written at position ${moved + 1}, expected ${wantOrder[moved]} there`
    )
  for (const [id, w] of want) {
    const h = have.get(id)
    if (!h) continue
    const d: string[] = []
    diffValue("mode", h.mode, w.mode, d)
    diffValue("revision", h.revision, w.revision, d)
    diffValue("createOnly", h.createOnly, w.createOnly, d)
    diffValue("properties", h.properties, w.properties, d)
    out.push(...d.map((line) => `${id}: ${line}`))
  }
  const docs = new Map(got.documents.map((d) => [d.digest, d]))
  for (const d of expected.documents) {
    const h = docs.get(d.digest)
    if (!h) out.push(`document ${d.name} (${d.digest}): missing`)
    else diffValue(`document ${d.digest}`, h, d, out)
  }
  const wantDocs = new Set(expected.documents.map((d) => d.digest))
  for (const d of got.documents)
    if (!wantDocs.has(d.digest))
      out.push(`document ${d.name} (${d.digest}): not expected`)
  diffValue("left", got.left, expected.left, out)
  return out
}

/** The planned record of a kind and key (and namespace, where two
 * namespaces share a key). */
export function plannedRecord(
  plan: Plan,
  kind: string,
  key: string,
  namespace?: string
): PlannedRecord | undefined {
  return plan.records.find(
    (r) =>
      r.kind === kind &&
      r.key === key &&
      (namespace === undefined || r.namespace === namespace)
  )
}

// ---- PDFs ----

/** A minimal PDF for a reader's tests: one page per content stream given
 * (PDF content operators, Latin-1), A4, with the standard fonts Helvetica
 * as `/F1`, Helvetica-Bold `/F2`, Helvetica-Oblique `/F3` and Times-Roman
 * `/F4`. `compress` deflates the content streams; `trailingEol` leaves the
 * end-of-line byte some generators write after the deflate data inside the
 * stream's `/Length`. */
export function pdfFixture(
  pages: readonly string[],
  options: { compress?: boolean; trailingEol?: boolean } = {}
): Uint8Array {
  const chunks: Uint8Array[] = []
  let length = 0
  const latin = (s: string) => Uint8Array.from(s, (c) => c.charCodeAt(0) & 0xff)
  const push = (b: string | Uint8Array) => {
    const bytes = typeof b === "string" ? latin(b) : b
    chunks.push(bytes)
    length += bytes.length
  }
  const fonts = [
    "Helvetica",
    "Helvetica-Bold",
    "Helvetica-Oblique",
    "Times-Roman",
  ]
  const firstFont = 3
  const firstPage = firstFont + fonts.length
  const objects: (string | ((n: number) => void))[] = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    `<< /Type /Pages /Kids [${pages.map((_, i) => `${firstPage + i * 2} 0 R`).join(" ")}] /Count ${pages.length} >>`,
    ...fonts.map(
      (f) =>
        `<< /Type /Font /Subtype /Type1 /BaseFont /${f} /Encoding /WinAnsiEncoding >>`
    ),
  ]
  const fontRefs = fonts
    .map((_, i) => `/F${i + 1} ${firstFont + i} 0 R`)
    .join(" ")
  pages.forEach((content, i) => {
    const contentObject = firstPage + i * 2 + 1
    objects.push(
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << ${fontRefs} >> >> /Contents ${contentObject} 0 R >>`
    )
    objects.push((n: number) => {
      let data = latin(content)
      if (options.compress) data = zlibSync(data)
      if (options.trailingEol) {
        const withEol = new Uint8Array(data.length + 1)
        withEol.set(data)
        withEol[data.length] = 0x0a
        data = withEol
      }
      const filter = options.compress ? " /Filter /FlateDecode" : ""
      push(`${n} 0 obj\n<< /Length ${data.length}${filter} >>\nstream\n`)
      push(data)
      push("\nendstream\nendobj\n")
    })
  })
  push("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
  const offsets: number[] = []
  objects.forEach((o, i) => {
    offsets.push(length)
    if (typeof o === "string") push(`${i + 1} 0 obj\n${o}\nendobj\n`)
    else o(i + 1)
  })
  const xref = length
  push(
    `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n${offsets
      .map((o) => `${String(o).padStart(10, "0")} 00000 n \n`)
      .join("")}`
  )
  push(
    `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  )
  const out = new Uint8Array(length)
  let at = 0
  for (const c of chunks) {
    out.set(c, at)
    at += c.length
  }
  return out
}
