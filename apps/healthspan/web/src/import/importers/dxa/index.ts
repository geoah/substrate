// DXA scans from a GE Lunar scanner (apps/healthspan/docs/importers/
// 04-dxa.md): the scanning centre's printouts of each scan (PDF, read from
// pdf.js's positioned text), and any trend page typed by hand into JSON
// beside its photo. The printouts name neither the centre nor its clock, so
// both are asked.

import { Effect, Result } from "effect"
import { recordId } from "../../ids"
import type {
  Context,
  ImportError,
  Importer,
  Input,
  InputFile,
  LeftOut,
  Match,
  Plan,
  Question,
} from "../types"
import {
  decodeValue,
  extensionOf,
  knownZone,
  latin1,
  mediaTypeOf,
  parseJson,
  pdfPages,
  readBytes,
  readText,
  slug,
  sourceKey,
  strip,
  unreadable,
} from "../shared"
import { dxaPlan, type PrintoutRead, type TranscriptionRead } from "./plan"
import { readPrintout, type PrintoutKind } from "./printout"
import { looksTranscribed, Transcription, transcribed } from "./transcription"

/** The enCORE report templates a printout's PDF title names, as the
 * scanner's report software prints them through Crystal Reports. */
const REPORTS: Readonly<Record<string, PrintoutKind>> = {
  "CorePatient.rpt": "bone density",
  "CoreTbComp.rpt": "body composition",
  "CompEnhanced.rpt": "trend",
}

const TITLE = /\/Title\s*\(([^()]*)\)/g

/** The printout a PDF's title names, its enCORE report template; undefined
 * for a PDF whose title names none. */
export function enCoreReport(bytes: Uint8Array): PrintoutKind | undefined {
  for (const m of latin1(bytes).matchAll(TITLE))
    for (const [name, kind] of Object.entries(REPORTS))
      if (m[1].endsWith(name)) return kind
  return undefined
}

const isPdf = (f: InputFile) =>
  extensionOf(f.path) === "pdf" || f.type === "application/pdf"
const isJson = (f: InputFile) =>
  extensionOf(f.path) === "json" || f.type === "application/json"

/** What a detect looks at in a PDF: its title, read from its bytes. Files
 * past this size are not opened to look. */
const DETECT_LIMIT = 32 * 1024 * 1024

/** The key a centre's records are found by: its name's slug, or for a name
 * with no Latin letter or digit, its letters and digits as they are, lower
 * case, every other run one `-`. */
export function centreKey(name: string): string {
  const latin = slug(name)
  if (/[a-z0-9]/.test(latin)) return latin
  return name
    .normalize("NFC")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-+|-+$/g, "")
}

/** The namespace a centre's scans are kept under. */
export function centreNamespace(name: string): string {
  return `dxa/${centreKey(name)}`
}

const CENTRE = "centre"
const ZONE = "zone"

/** The centre's name, suggested where the repository holds exactly one
 * source this importer made for a centre (one whose id is the one its own
 * name derives). */
function centreQuestion(ctx: Context): Question {
  const known = ctx.sources.filter(
    (s) =>
      centreKey(s.name) && s.id === recordId(sourceKey(centreNamespace(s.name)))
  )
  const q: Question = {
    key: CENTRE,
    label: "The scanning centre's name",
    help: "The printouts do not name it. The scans are kept under it as their source, so give the same name for every import of one centre's scans.",
    kind: "text",
    required: true,
  }
  if (known.length === 1) q.suggested = known[0].name
  return q
}

function browserZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

/** The centre's time zone, which its clock prints the scan times in;
 * suggested from this browser's. */
function zoneQuestion(): Question {
  const q: Question = {
    key: ZONE,
    label: "The scanning centre's time zone",
    help: "The printouts give the scan's time on the centre's clock, with no zone.",
    kind: "timezone",
    required: true,
  }
  const zone = browserZone()
  if (zone) q.suggested = zone
  return q
}

/** The file a transcription's photo is: by its name in the transcription's
 * folder, else anywhere in the input. */
function photoOf(
  input: Input,
  json: InputFile,
  name: string
): InputFile | undefined {
  const dir = json.path.includes("/")
    ? json.path.slice(0, json.path.lastIndexOf("/") + 1)
    : ""
  const base = (p: string) => p.slice(p.lastIndexOf("/") + 1)
  return (
    input.files.find((f) => f.path === `${dir}${name}`) ??
    input.files.find((f) => base(f.path) === base(name))
  )
}

function read(input: Input, ctx: Context): Effect.Effect<Plan, ImportError> {
  return Effect.gen(function* () {
    const centre = strip(ctx.answers[CENTRE] ?? "")
    const zone = strip(ctx.answers[ZONE] ?? "")
    const missing: Question[] = []
    if (!centre || !centreKey(centre)) missing.push(centreQuestion(ctx))
    if (!zone || !knownZone(zone)) missing.push(zoneQuestion())
    if (missing.length)
      return yield* Effect.fail<ImportError>({
        _tag: "NeedsAnswers",
        questions: missing,
      })

    const left: LeftOut[] = []
    const printouts: PrintoutRead[] = []
    const transcriptions: TranscriptionRead[] = []
    const used = new Set<string>()
    const pending: { file: InputFile; t: Transcription }[] = []

    for (const file of input.files) {
      if (isPdf(file)) {
        const bytes = yield* readBytes(file)
        const marked = enCoreReport(bytes) !== undefined
        const pages = yield* Effect.result(pdfPages(bytes, file.path))
        if (Result.isFailure(pages)) {
          // A printout the scanner's software titled is the owner's to
          // fix; any other PDF that will not open is not one.
          if (marked) return yield* Effect.fail(pages.failure)
          left.push({ what: file.path, why: "it is a PDF that will not open" })
          continue
        }
        const out = readPrintout(file.path, pages.success)
        if ("why" in out) {
          left.push({ what: file.path, why: out.why })
          continue
        }
        used.add(file.path)
        left.push(...out.printout.left)
        printouts.push({ printout: out.printout, bytes })
        continue
      }
      if (isJson(file)) {
        const text = yield* readText(file)
        const value = yield* Effect.result(parseJson(text, file.path))
        if (Result.isFailure(value) || !looksTranscribed(value.success))
          continue
        const t = yield* decodeValue(Transcription, value.success, file.path)
        used.add(file.path)
        pending.push({ file, t })
      }
    }

    for (const { file, t } of pending) {
      const page = transcribed(t)
      if ("field" in page)
        return yield* Effect.fail(
          unreadable(file.path, "is not a real date or time", page.field)
        )
      const photo = photoOf(input, file, t.photo)
      const tr: TranscriptionRead = {
        path: file.path,
        day: page.day,
        measured: page.measured,
        analyzed: page.analyzed,
        accession: t.file,
        trend: page.trend,
      }
      if (photo) {
        used.add(photo.path)
        tr.photo = {
          path: photo.path,
          bytes: yield* readBytes(photo),
          kind: "trend photo",
          day: page.day,
          mediaType: mediaTypeOf(
            photo.path,
            photo.type || "application/octet-stream"
          ),
          ext: extensionOf(photo.path),
        }
      } else
        left.push({
          what: file.path,
          why: "the photo it names is not among the files, so its scan's report names no photo",
        })
      transcriptions.push(tr)
    }

    if (!printouts.length && !transcriptions.length)
      return yield* Effect.fail<ImportError>({
        _tag: "NotMine",
        message:
          "There is no GE Lunar printout or transcribed trend page here.",
      })
    for (const f of input.files)
      if (!used.has(f.path) && !left.some((l) => l.what === f.path))
        left.push({
          what: f.path,
          why: "it is not a GE Lunar printout or a transcribed trend page",
        })

    return dxaPlan(
      { name: input.name, printouts, transcriptions, left },
      { centre, zone, namespace: centreNamespace(centre) },
      ctx
    )
  })
}

function detect(input: Input): Effect.Effect<Match | undefined> {
  return Effect.gen(function* () {
    const kinds: PrintoutKind[] = []
    let typed = 0
    for (const file of input.files) {
      if (isPdf(file) && file.size <= DETECT_LIMIT) {
        const bytes = yield* Effect.result(readBytes(file))
        const kind = Result.isSuccess(bytes)
          ? enCoreReport(bytes.success)
          : undefined
        if (kind) kinds.push(kind)
      } else if (isJson(file) && file.size <= DETECT_LIMIT) {
        const text = yield* Effect.result(readText(file))
        if (Result.isFailure(text)) continue
        const value = yield* Effect.result(parseJson(text.success, file.path))
        if (Result.isSuccess(value) && looksTranscribed(value.success)) typed++
      }
    }
    if (!kinds.length && !typed) return undefined
    const parts: string[] = []
    if (kinds.length) {
      const count = (k: PrintoutKind) => kinds.filter((x) => x === k).length
      const of = (["bone density", "body composition", "trend"] as const)
        .filter((k) => count(k))
        .map((k) => `${count(k)} ${k}`)
      parts.push(
        `${kinds.length === 1 ? "A GE Lunar enCORE printout" : `${kinds.length} GE Lunar enCORE printouts`} (${of.join(", ")})`
      )
    }
    if (typed)
      parts.push(
        typed === 1
          ? `${parts.length ? "a" : "A"} transcribed trend page`
          : `${typed} transcribed trend pages`
      )
    return {
      importer: "dxa" as const,
      confidence: 1,
      why: `${parts.join(" and ")}.`,
    }
  })
}

export const dxa: Importer = {
  id: "dxa",
  name: "DXA scan (GE Lunar)",
  description:
    "The scanning centre's GE Lunar printouts of your DXA scans (PDF), and any trend page typed into JSON beside its photo.",
  accepts: { kind: "files", extensions: ["pdf", "json", "jpg", "jpeg", "png"] },
  detect,
  questions: (_input, ctx) =>
    Effect.succeed([centreQuestion(ctx), zoneQuestion()]),
  read,
}
