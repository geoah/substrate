// Numan's exports in what the owner hands in, and each test from the export
// holding its newest version (apps/healthspan/docs/importers/01-numan.md,
// What it reads): every export repeats the older tests, so one test can be
// in several, at several versions.

import { Effect, Option } from "effect"
import { decodeValue, extensionOf, parseJson, readText } from "../shared"
import { epoch, instantMs } from "../shared/time"
import type { ImportError, InputFile, LeftOut } from "../types"
import { NumanExport, type Included, type Test } from "./schema"

/** An export as read: its file, its JSON as parsed (which the reports keep)
 * and as decoded. */
export interface ReadExport {
  file: InputFile
  /** Its place among the exports, in path order. */
  order: number
  raw: { data: readonly unknown[]; included?: unknown }
  doc: NumanExport
}

/** A test as read from the export holding its newest version, with that
 * export's clinical review of it. */
export interface ChosenTest {
  test: Test
  /** The test's own JSON, whole. */
  raw: unknown
  /** The review's own JSON, whole, or null where the export holds none. */
  review: unknown
  from: ReadExport
  /** Its place in the export's `data`. */
  index: number
}

const isObject = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v)

/** Whether a JSON value carries Numan's marks: an object whose `data` is a
 * list of `medical_test` resources, at least one, each with its short id. */
export function hasNumanMarks(value: unknown): boolean {
  if (!isObject(value) || !Array.isArray(value.data) || !value.data.length)
    return false
  return value.data.every(
    (t) =>
      isObject(t) &&
      t.type === "medical_test" &&
      isObject(t.attributes) &&
      typeof t.attributes.short_id === "string"
  )
}

/** The files that can be a Numan export: JSON files at the top of what was
 * handed in, as Numan's web app names its downloads. */
export function candidates(files: readonly InputFile[]): InputFile[] {
  return files.filter(
    (f) => !f.path.includes("/") && extensionOf(f.path) === "json"
  )
}

/** A file's JSON where it carries Numan's marks, else none; a file that is
 * not JSON is none too. Never fails. */
export function numanJson(
  file: InputFile
): Effect.Effect<Option.Option<{ data: readonly unknown[] }>> {
  return readText(file).pipe(
    Effect.flatMap((text) => parseJson(text, file.path)),
    Effect.option,
    Effect.map((o) =>
      Option.filter(o, hasNumanMarks).pipe(
        Option.map((v) => v as { data: readonly unknown[] })
      )
    )
  )
}

/** The exports among the files, decoded, and the files left out (every
 * other file, and an export that is one page of several). An export that
 * carries Numan's marks and is not what its schema says fails the read,
 * naming the file and the field. */
export function readExports(
  files: readonly InputFile[]
): Effect.Effect<{ exports: ReadExport[]; left: LeftOut[] }, ImportError> {
  return Effect.gen(function* () {
    const exports: ReadExport[] = []
    const left: LeftOut[] = []
    const maybe = new Set(candidates(files))
    for (const file of files) {
      const found = maybe.has(file)
        ? yield* numanJson(file)
        : Option.none<{ data: readonly unknown[] }>()
      if (Option.isNone(found)) {
        left.push({ what: file.path, why: "it is not a Numan export" })
        continue
      }
      const raw = found.value
      const doc = yield* decodeValue(NumanExport, raw, file.path)
      exports.push({ file, order: exports.length, raw, doc })
      const page = doc.meta?.pagination
      if (page?.next_page) {
        const at = (n: unknown) =>
          typeof n === "number" || typeof n === "string" ? String(n) : "?"
        left.push({
          what: file.path,
          why: `it is page ${at(page.current_page)} of ${at(page.last_page)}; the tests on later pages come only from other exports`,
        })
      }
    }
    return { exports, left }
  })
}

/** A stamp as milliseconds since the epoch, one naming no offset read as
 * UTC; undefined where it is missing or not ISO 8601. */
export function instant(stamp: string | null | undefined): number | undefined {
  if (!stamp) return undefined
  const ms = instantMs(stamp)
  if (ms !== undefined) return ms
  const s = epoch(stamp)
  return s === undefined ? undefined : s * 1000
}

/** The date in a file's name, `YYYYMMDD`: its last run of exactly eight
 * digits; "" where it has none. */
export function nameDate(path: string): string {
  const name = path.slice(path.lastIndexOf("/") + 1)
  const all = [...name.matchAll(/(?<![0-9])([0-9]{8})(?![0-9])/g)]
  return all.length ? all[all.length - 1][1] : ""
}

const isIncluded = (v: Included | readonly Included[]): v is Included =>
  !Array.isArray(v)

/** The reviews an export includes, by id: each `medical_test_review` of
 * `included`, an entry of which is an object or a list of them. The later
 * of two under one id stands. */
function reviews(e: ReadExport): Map<string, { raw: unknown; at: number }> {
  const out = new Map<string, { raw: unknown; at: number }>()
  const raw = Array.isArray(e.raw.included) ? e.raw.included : []
  const decoded = e.doc.included ?? []
  decoded.forEach((item, i) => {
    const rawItem: unknown = raw[i]
    const list: readonly Included[] = isIncluded(item) ? [item] : item
    const rawList: unknown[] = Array.isArray(rawItem) ? rawItem : [rawItem]
    list.forEach((inc, j) => {
      if (inc.type !== "medical_test_review" || inc.id === undefined) return
      const at = Math.max(
        instant(inc.attributes?.updated_at) ?? -Infinity,
        instant(inc.attributes?.created_at) ?? -Infinity
      )
      out.set(inc.id, { raw: rawList[j], at })
    })
  })
  return out
}

/** When an export was made, as the file itself says: the newest time any
 * of its tests or reviews was created or updated. Numan's export carries
 * no time of its own. */
export function exportTime(e: ReadExport): number {
  let at = -Infinity
  for (const t of e.doc.data)
    at = Math.max(
      at,
      instant(t.attributes.updated_at) ?? -Infinity,
      instant(t.attributes.created_at) ?? -Infinity
    )
  for (const r of reviews(e).values()) at = Math.max(at, r.at)
  return at
}

type Rank = readonly [number, number, string, number]

function later(a: Rank, b: Rank): boolean {
  for (let i = 0; i < a.length; i++) {
    if (a[i] === b[i]) continue
    return a[i] > b[i]
  }
  return true
}

/** Every test once, each from the export holding its newest version: the
 * later `updated_at`, then the later export (by the newest time it holds),
 * then the later date in the file's name, then the later file in path
 * order. A test an export leaves out keeps the copy another has. Newest
 * created first. */
export function newestTests(exports: readonly ReadExport[]): ChosenTest[] {
  const best = new Map<string, { rank: Rank; chosen: ChosenTest }>()
  for (const e of exports) {
    const time = exportTime(e)
    const date = nameDate(e.file.path)
    const byId = reviews(e)
    e.doc.data.forEach((test, index) => {
      const sid = test.attributes.short_id
      const rank: Rank = [
        instant(test.attributes.updated_at) ?? -Infinity,
        time,
        date,
        e.order,
      ]
      const had = best.get(sid)
      if (had && !later(rank, had.rank)) return
      const link = test.relationships?.medical_test_review?.data?.id
      const review = link === undefined ? undefined : byId.get(link)
      best.set(sid, {
        rank,
        chosen: {
          test,
          raw: e.raw.data[index],
          review: review?.raw ?? null,
          from: e,
          index,
        },
      })
    })
  }
  return [...best.values()]
    .map((b) => b.chosen)
    .map((c, i) => ({ c, i, at: instant(c.test.attributes.created_at) }))
    .sort((a, b) => (b.at ?? -Infinity) - (a.at ?? -Infinity) || a.i - b.i)
    .map((x) => x.c)
}
