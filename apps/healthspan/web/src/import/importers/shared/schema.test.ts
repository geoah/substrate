import { Effect, Schema } from "effect"
import { describe, expect, it } from "vitest"
import { inputFromRecord, runResult } from "../testing"
import {
  decodeValue,
  fieldPath,
  orUnreadable,
  parseJson,
  parseJsonLossless,
  readBytes,
  readJson,
  readJsonLossless,
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

  it("reads an integer past what a float holds whole, losslessly", async () => {
    const text =
      '{"id":14990931000006117,"small":123,"big":-90071992547409930,' +
      '"float":1234567890123456.5,"exp":12345678901234567e2,' +
      '"text":"12345678901234567890","list":[9007199254740993]}'
    // JSON.parse rounds it.
    expect(String((JSON.parse(text) as { id: number }).id)).not.toBe(
      "14990931000006117"
    )
    expect(await Effect.runPromise(parseJsonLossless(text, "x.json"))).toEqual({
      id: 14990931000006117n,
      small: 123,
      big: -90071992547409930n,
      float: 1234567890123456.5,
      // An exponent is a float, as JSON.parse reads it.
      exp: Number("12345678901234567e2"),
      text: "12345678901234567890",
      list: [9007199254740993n],
    })
    // A safe integer of 16 digits, or none past 2^53, reads as parseJson's.
    expect(
      await Effect.runPromise(parseJsonLossless("[1000000000000000, 1.5]", "y"))
    ).toEqual([1000000000000000, 1.5])
    expect(
      failure(await runResult(parseJsonLossless("{not json", "bad.json")))
    ).toEqual(unreadable("bad.json", "the file is not JSON"))
  })

  it("decodes a lossless JSON file through a schema, as readJson does", async () => {
    const Coded = Schema.Struct({
      codeId: Schema.Union([Schema.Number, Schema.BigInt, Schema.String]),
    })
    const input = inputFromRecord("x", {
      "big.json": '{"codeId": 14990931000006116}',
      "small.json": '{"codeId": 22298006}',
      "wrong.json": '{"codeId": null}',
    })
    const [big, small, wrong] = input.files
    expect(await Effect.runPromise(readJsonLossless(Coded, big))).toEqual({
      codeId: 14990931000006116n,
    })
    expect(await Effect.runPromise(readJsonLossless(Coded, small))).toEqual({
      codeId: 22298006,
    })
    expect(
      failure(await runResult(readJsonLossless(Coded, wrong)))
    ).toMatchObject({ _tag: "Unreadable", path: "wrong.json", field: "codeId" })
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
