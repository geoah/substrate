// Reading a provider's files with Effect: a file's text, bytes and JSON,
// and a value decoded by an Effect Schema, every failure an `Unreadable`
// that names the file and the field (apps/healthspan/docs/importers/
// 00-shared.md, Reading files).

import { Effect, Schema, SchemaIssue } from "effect"
import type { ImportError, InputFile } from "../types"

/** A file that is not what the provider promised. */
export function unreadable(
  path: string,
  message: string,
  field?: string
): ImportError {
  return field === undefined
    ? { _tag: "Unreadable", path, message }
    : { _tag: "Unreadable", path, field, message }
}

/** `value`, or `Unreadable` where it is missing: for a reading that gives
 * undefined on a provider's data it does not understand (a time that is
 * not ISO 8601, a manifest that names no run). */
export function orUnreadable<A>(
  value: A | null | undefined,
  path: string,
  message: string,
  field?: string
): Effect.Effect<A, ImportError> {
  return value === null || value === undefined
    ? Effect.fail(unreadable(path, message, field))
    : Effect.succeed(value)
}

/** A decoder's path as a field: `data[0].attributes.short_id`. */
export function fieldPath(path: readonly unknown[]): string {
  let out = ""
  for (const p of path) {
    const key = p && typeof p === "object" && "key" in p ? p.key : p
    if (typeof key === "number") out += `[${key}]`
    else out += out ? `.${String(key)}` : String(key)
  }
  return out
}

const standard = SchemaIssue.makeFormatterStandardSchemaV1()

/** A value decoded by `schema`, or `Unreadable` naming `path` and the
 * first field that is not what the schema says, with the decoder's
 * message ("Expected string", "Missing key"). The value itself is never
 * put in the message. */
export function decodeValue<A>(
  schema: Schema.Decoder<A>,
  value: unknown,
  path: string
): Effect.Effect<A, ImportError> {
  return Schema.decodeUnknownEffect(schema)(value).pipe(
    Effect.mapError((e) => {
      const first = standard(e.issue).issues[0]
      const field = first?.path?.length ? fieldPath(first.path) : undefined
      return unreadable(path, first?.message ?? e.message, field)
    })
  )
}

/** A file's text, as UTF-8. */
export function readText(file: InputFile): Effect.Effect<string, ImportError> {
  return Effect.tryPromise({
    try: () => file.text(),
    catch: () => unreadable(file.path, "the file could not be read"),
  })
}

/** A file's bytes. */
export function readBytes(
  file: InputFile
): Effect.Effect<Uint8Array, ImportError> {
  return Effect.tryPromise({
    try: () => file.bytes(),
    catch: () => unreadable(file.path, "the file could not be read"),
  })
}

/** Text parsed as JSON, or `Unreadable` naming `path`. */
export function parseJson(
  text: string,
  path: string
): Effect.Effect<unknown, ImportError> {
  return Effect.try({
    try: () => JSON.parse(text) as unknown,
    catch: () => unreadable(path, "the file is not JSON"),
  })
}

/** A JSON file decoded by `schema`. */
export function readJson<A>(
  schema: Schema.Decoder<A>,
  file: InputFile
): Effect.Effect<A, ImportError> {
  return readText(file).pipe(
    Effect.flatMap((text) => parseJson(text, file.path)),
    Effect.flatMap((value) => decodeValue(schema, value, file.path))
  )
}
