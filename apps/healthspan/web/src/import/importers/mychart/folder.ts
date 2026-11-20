// A mychartctl folder as the importer reads it: the manifest (at the top,
// or in the one folder at the top), each resource's items by their file
// names, the lists, and a downloaded file found by its upstream id.

import { Effect, Schema } from "effect"
import { sha256 } from "../../ids"
import {
  decodeValue,
  hex,
  parseJson,
  parseJsonLossless,
  readJson,
  readText,
} from "../shared"
import type { ImportError, Input, InputFile } from "../types"
import { Manifest } from "./schemas"

/** The marks of a mychartctl folder: its manifest, its decoded fields,
 * and the folder it sits in inside the input. */
export interface Found {
  /** "" for a manifest at the top, else `<folder>/`. */
  prefix: string
  manifest: InputFile
}

/** The manifest of a mychartctl folder in the input: `manifest.json` at
 * the top, else the one `<folder>/manifest.json` beside an `items` folder.
 * Undefined where there is none, or several. */
export function findManifest(input: Input): Found | undefined {
  const top = input.files.find((f) => f.path === "manifest.json")
  if (top) return { prefix: "", manifest: top }
  const nested = input.files.filter((f) =>
    /^[^/]+\/manifest\.json$/.test(f.path)
  )
  const withItems = nested.filter((m) => {
    const prefix = m.path.slice(0, -"manifest.json".length)
    return input.files.some((f) => f.path.startsWith(`${prefix}items/`))
  })
  if (withItems.length !== 1) return undefined
  const manifest = withItems[0]
  return { prefix: manifest.path.slice(0, -"manifest.json".length), manifest }
}

/** The manifest's JSON, or undefined where it is not JSON. */
export function peekJson(file: InputFile): Effect.Effect<unknown> {
  return readText(file).pipe(
    Effect.flatMap((t) => parseJson(t, file.path)),
    Effect.orElseSucceed(() => undefined)
  )
}

/** Whether a manifest is mychartctl's: its `tool` names it and it gives the
 * portal's address. */
export function isMyChartManifest(value: unknown): value is Manifest {
  return Schema.is(Manifest)(value) && /^mychartctl(\s|$)/.test(value.tool)
}

/** The portal's host from the manifest's base address, lower case;
 * undefined where it is not an address. */
export function portalHost(baseUrl: string): string | undefined {
  try {
    const host = new URL(baseUrl).hostname.toLowerCase()
    return host || undefined
  } catch {
    return undefined
  }
}

/** An upstream id as mychartctl names its file: MyChart's ids pass
 * through; a trailing `^` is dropped, anything outside `[A-Za-z0-9._-]`
 * becomes `_`, dots at either end go, and a name past 180 characters is
 * cut to 160 and a hash. */
export function safeId(id: string): string {
  const trimmed = id.trim().replace(/\^+$/, "")
  let out = ""
  for (const c of trimmed) out += /^[A-Za-z0-9._-]$/.test(c) ? c : "_"
  out = out.replace(/^\.+|\.+$/g, "")
  if (out.length > 180) {
    const sum = hex(sha256(new TextEncoder().encode(trimmed))).slice(0, 8)
    out = `${out.slice(0, 160)}-${sum}`
  }
  return out
}

/** One item of a resource: its file name without `.json` (mychartctl's
 * file-safe upstream id) and its file. */
export interface Item {
  stem: string
  file: InputFile
}

/** The folder, read. */
export class Folder {
  private readonly byPath: ReadonlyMap<string, InputFile>

  constructor(
    readonly input: Input,
    readonly prefix: string
  ) {
    this.byPath = new Map(input.files.map((f) => [f.path, f]))
  }

  /** `items/<resource>/*.json`, in file name order. */
  items(resource: string): Item[] {
    const dir = `${this.prefix}items/${resource}/`
    return this.input.files
      .filter(
        (f) =>
          f.path.startsWith(dir) &&
          f.path.endsWith(".json") &&
          !f.path.slice(dir.length).includes("/")
      )
      .map((f) => ({
        stem: f.path.slice(dir.length, -".json".length),
        file: f,
      }))
      .sort((a, b) => byCodePoint(a.stem, b.stem))
  }

  /** Every item of a resource decoded, by its stem, in file name order,
   * with its JSON as read. That JSON is kept whole (a lab order's is its
   * report's file), so it is read losslessly: an integer past what a float
   * holds exactly is a `bigint`, never rounded. */
  decodeItems<A>(
    resource: string,
    schema: Schema.Decoder<A>
  ): Effect.Effect<
    { stem: string; path: string; value: A; raw: unknown }[],
    ImportError
  > {
    return Effect.forEach(this.items(resource), (item) =>
      readText(item.file).pipe(
        Effect.flatMap((t) => parseJsonLossless(t, item.file.path)),
        Effect.flatMap((raw) =>
          decodeValue(schema, raw, item.file.path).pipe(
            Effect.map((value) => ({
              stem: item.stem,
              path: item.file.path,
              value,
              raw,
            }))
          )
        )
      )
    )
  }

  /** `lists/<name>.json` decoded, or undefined where it is not there. */
  list<A>(
    name: string,
    schema: Schema.Decoder<A>
  ): Effect.Effect<A | undefined, ImportError> {
    const file = this.byPath.get(`${this.prefix}lists/${name}.json`)
    return file ? readJson(schema, file) : Effect.succeed(undefined)
  }

  /** Whether `lists/<name>.json` is there. */
  hasList(name: string): boolean {
    return this.byPath.has(`${this.prefix}lists/${name}.json`)
  }

  /** The downloaded file of an upstream id: the first, in name order, of
   * `files/<resource>/<id>.<ext>`, the id as mychartctl names its file. */
  file(resource: string, id: string): InputFile | undefined {
    const dir = `${this.prefix}files/${resource}/`
    const names = [...new Set([safeId(id), id])]
    for (const name of names) {
      const hit = this.input.files
        .filter((f) => {
          if (!f.path.startsWith(dir)) return false
          const rest = f.path.slice(dir.length)
          return rest.startsWith(`${name}.`) && !rest.includes("/")
        })
        .sort((a, b) => byCodePoint(a.path, b.path))[0]
      if (hit) return hit
    }
    return undefined
  }
}

/** Code-point order, which file names are listed in. */
export function byCodePoint(a: string, b: string): number {
  const x = [...a]
  const y = [...b]
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    const d = x[i].codePointAt(0)! - y[i].codePointAt(0)!
    if (d) return d
  }
  return x.length - y.length
}
