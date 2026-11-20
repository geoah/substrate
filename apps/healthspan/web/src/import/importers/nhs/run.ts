// An nhsctl run inside what the owner handed in: the run folder itself, or
// the folder of runs (the newest by its name), or a zip of either; its
// manifest, and each recorded response found by the URL path it answered
// (apps/healthspan/docs/importers/05-nhs.md, What it reads).

import { Effect, Schema } from "effect"
import {
  decodeValue,
  readBytes,
  readJson,
  readJsonLossless,
  unreadable,
} from "../shared"
import type { ImportError, Input, InputFile } from "../types"
import { Envelope, Manifest, type ResponseEntry } from "./schema"

/** The host every NHS App API response nhsctl records comes from. */
const APP_HOST = /(^|\.)nhsapp\.service\.nhs\.uk$/

/** A run folder's name as nhsctl writes it: the run's UTC start. */
const RUN_FOLDER = /^20[^/]*$/

/** One run of nhsctl, found in an input. */
export interface Run {
  /** The run folder's place in the input: "" or `<folder>/`. */
  prefix: string
  /** The run folder's name, where the input is a folder of runs. */
  folder?: string
  manifest: Manifest
  /** How many runs the input holds. */
  runs: number
  files: ReadonlyMap<string, InputFile>
}

/** A URL's host and path, as Python's `urlsplit` splits them: no query, no
 * fragment, the path as written. */
export function urlParts(url: string): { host: string; path: string } {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)([^?#]*)/i.exec(url)
  if (!m) return { host: "", path: "" }
  const host = m[1].replace(/^[^@]*@/, "").replace(/:\d+$/, "")
  return { host: host.toLowerCase(), path: m[2] }
}

/** Whether a manifest is nhsctl's: it names nhsctl as its tool, or every
 * response it records is the NHS App's. */
export function isNhsctl(m: Manifest): boolean {
  if (m.tool && /^nhsctl\b/.test(m.tool)) return true
  return (
    m.responses.length > 0 &&
    m.responses.every((r) => APP_HOST.test(urlParts(r.url).host))
  )
}

/** The manifest to read: the input's own, else the newest run folder's. */
function manifestFile(
  input: Input
):
  | { file: InputFile; prefix: string; folder?: string; runs: number }
  | undefined {
  const root = input.files.find((f) => f.path === "manifest.json")
  if (root) return { file: root, prefix: "", runs: 1 }
  const runs = input.files
    .filter((f) => {
      const parts = f.path.split("/")
      return (
        parts.length === 2 &&
        parts[1] === "manifest.json" &&
        RUN_FOLDER.test(parts[0])
      )
    })
    .sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
  const newest = runs[runs.length - 1]
  if (!newest) return undefined
  const folder = newest.path.slice(0, newest.path.indexOf("/"))
  return { file: newest, prefix: `${folder}/`, folder, runs: runs.length }
}

/** The run an input holds, or undefined where it holds no nhsctl
 * manifest. A manifest that is not what nhsctl writes fails the read. */
export function findRun(
  input: Input
): Effect.Effect<Run | undefined, ImportError> {
  const found = manifestFile(input)
  if (!found) return Effect.succeed(undefined)
  return readJson(Manifest, found.file).pipe(
    Effect.map((manifest) =>
      isNhsctl(manifest)
        ? {
            prefix: found.prefix,
            folder: found.folder,
            manifest,
            runs: found.runs,
            files: new Map(input.files.map((f) => [f.path, f])),
          }
        : undefined
    )
  )
}

/** The responses the run recorded from `path` (a URL path such as
 * `/v1/patient/my-record`) with status 200, in the order they came. */
export function answers(run: Run, path: string | RegExp): ResponseEntry[] {
  return run.manifest.responses.filter((r) => {
    if (r.status !== 200) return false
    const p = urlParts(r.url).path
    return typeof path === "string" ? p === path : path.test(p)
  })
}

/** A file of the run, by its path inside the run folder. */
export function runFile(
  run: Run,
  path: string
): Effect.Effect<InputFile, ImportError> {
  const file = run.files.get(`${run.prefix}${path}`)
  return file
    ? Effect.succeed(file)
    : Effect.fail(
        unreadable(
          `${run.prefix}${path}`,
          "the manifest names this file, and the export does not hold it"
        )
      )
}

/** A response's JSON body, undefined where it has none. The GP system's
 * code ids run to 18 digits, so the body is read losslessly: an integer a
 * float cannot hold exactly is a `bigint`. */
export function bodyOf(
  run: Run,
  entry: ResponseEntry
): Effect.Effect<unknown, ImportError> {
  return runFile(run, entry.file).pipe(
    Effect.flatMap((file) => readJsonLossless(Envelope, file)),
    Effect.map((e) => e.body)
  )
}

/** A body decoded by `schema`, a failure naming the response's file and
 * the field inside its `body`. */
export function decodeAt<A>(
  schema: Schema.Decoder<A>,
  body: unknown,
  path: string
): Effect.Effect<A, ImportError> {
  return decodeValue(schema, body, path).pipe(
    Effect.mapError((e) =>
      e._tag === "Unreadable"
        ? { ...e, field: e.field ? `body.${e.field}` : "body" }
        : e
    )
  )
}

/** A response's body decoded by `schema`; undefined where its body is
 * null or absent. */
export function decodeBody<A>(
  run: Run,
  entry: ResponseEntry,
  schema: Schema.Decoder<A>
): Effect.Effect<A | undefined, ImportError> {
  return bodyOf(run, entry).pipe(
    Effect.flatMap((body) =>
      body === null || body === undefined
        ? Effect.succeed(undefined)
        : decodeAt(schema, body, `${run.prefix}${entry.file}`)
    )
  )
}

/** The body of the last response with status 200 from `path`, decoded by
 * `schema`; undefined where the run recorded none, or its body is null. */
export function lastBody<A>(
  run: Run,
  path: string,
  schema: Schema.Decoder<A>
): Effect.Effect<A | undefined, ImportError> {
  const hits = answers(run, path)
  const last = hits[hits.length - 1]
  return last ? decodeBody(run, last, schema) : Effect.succeed(undefined)
}

/** A response's own bytes, where its body is not JSON (a document's file):
 * the manifest's `bodyFile`. */
export function bodyBytes(
  run: Run,
  entry: ResponseEntry
): Effect.Effect<Uint8Array, ImportError> {
  return entry.bodyFile
    ? runFile(run, entry.bodyFile).pipe(Effect.flatMap(readBytes))
    : Effect.fail(unreadable(entry.file, "the response has no file"))
}
