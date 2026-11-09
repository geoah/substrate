import { Effect, Schema } from "effect"
import { describe, expect, it } from "vitest"
import { inputFromRecord, runResult } from "../testing"
import {
  decodeValue,
  fieldPath,
  orUnreadable,
  parseJson,
  readBytes,
  readJson,
  unreadable,
} from "./schema"

const Export = Schema.Struct({
  data: Schema.Array(
    Schema.Struct({
      id: Schema.String,
      attributes: Schema.Struct({
        short_id: Schema.String,
        updated_at: Schema.optional(Schema.NullOr(Schema.String)),
      }),
    })
  ),
})

const failure = <A, E>(r: { _tag: string; failure?: E; success?: A }) =>
  r._tag === "Failure" ? r.failure : undefined

describe("decoding a provider's file", () => {
  it("gives the value where it is what the schema says", async () => {
    const value = { data: [{ id: "a", attributes: { short_id: "T1" } }] }
    expect(
      await Effect.runPromise(decodeValue(Export, value, "x.json"))
    ).toEqual(value)
  })

  it("names the file and the field that is not, never the value", async () => {
    const r = await runResult(
      decodeValue(
        Export,
        { data: [{ id: "a", attributes: { short_id: 12345 } }] },
        "exports/one.json"
      )
    )
    expect(failure(r)).toEqual(
      unreadable(
        "exports/one.json",
        "Expected string",
        "data[0].attributes.short_id"
      )
    )
    const missing = await runResult(
      decodeValue(Export, { data: [{ id: "a" }] }, "one.json")
    )
    expect(failure(missing)).toMatchObject({
      _tag: "Unreadable",
      field: "data[0].attributes",
      message: "Missing key",
    })
    const top = await runResult(decodeValue(Export, null, "one.json"))
    expect(failure(top)).toEqual(unreadable("one.json", "Expected object"))
  })

  it("reads a JSON file, or says it is not JSON", async () => {
    const input = inputFromRecord("x", {
      "good.json": '{"data":[]}',
      "bad.json": "{not json",
    })
    expect(await Effect.runPromise(readJson(Export, input.files[1]))).toEqual({
      data: [],
    })
    expect(failure(await runResult(readJson(Export, input.files[0])))).toEqual(
      unreadable("bad.json", "the file is not JSON")
    )
    expect(failure(await runResult(parseJson("", "e.json")))).toMatchObject({
      path: "e.json",
    })
    expect(await Effect.runPromise(readBytes(input.files[1]))).toHaveLength(11)
  })

  it("makes a reading that found nothing Unreadable", async () => {
    expect(await Effect.runPromise(orUnreadable(0, "m.json", "no run"))).toBe(0)
    expect(
      failure(
        await runResult(orUnreadable(undefined, "m.json", "no run", "runs"))
      )
    ).toEqual({
      _tag: "Unreadable",
      path: "m.json",
      field: "runs",
      message: "no run",
    })
  })

  it("writes a decoder's path as a field", () => {
    expect(fieldPath(["data", 0, "attributes", "short_id"])).toBe(
      "data[0].attributes.short_id"
    )
    expect(fieldPath([0, "a"])).toBe("[0].a")
    expect(fieldPath([])).toBe("")
  })
})
