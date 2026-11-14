import { describe, expect, it } from "vitest"

import { isRecordId, kindPatternGrant, kindPointer } from "./kind-pointer"

const KIND = "substrate.reamde.dev/core/kind"

describe("kindPointer", () => {
  it("reads every glob the grant grammar admits as a pattern", () => {
    for (const pattern of [
      "*",
      "providers.substrate.reamde.dev/*",
      "samples.substrate.reamde.dev/tasks/*",
      "~/*",
      "~/tasks/*",
    ]) {
      expect(kindPointer(`${KIND}/${pattern}`)).toEqual({
        shape: "pattern",
        pattern,
      })
    }
  })

  it("reads a pointer at one kind as that kind", () => {
    expect(
      kindPointer(`${KIND}/samples.substrate.reamde.dev/tasks/task`)
    ).toEqual({
      shape: "kind",
      kind: "samples.substrate.reamde.dev/tasks/task",
    })
    // The repository's own kinds sit under `~` (decision record 0259).
    expect(kindPointer(`${KIND}/~/tasks/task`)).toEqual({
      shape: "kind",
      kind: "~/tasks/task",
    })
  })

  it("leaves a pointer at any other kind to the record path", () => {
    expect(
      kindPointer(
        "substrate.reamde.dev/core/function/substrate.reamde.dev/core/ask"
      )
    ).toBeUndefined()
    expect(
      kindPointer("substrate.reamde.dev/llm/provider/openai")
    ).toBeUndefined()
    expect(kindPointer("*")).toBeUndefined()
  })
})

describe("kindPatternGrant", () => {
  it("names the auth kinds a glob over core leaves out", () => {
    expect(kindPatternGrant("*")).toBe(
      "Every kind, except the token, credential and secret kinds"
    )
    expect(kindPatternGrant("substrate.reamde.dev/*")).toBe(
      "Every kind published by substrate.reamde.dev, except the token, credential and secret kinds"
    )
    expect(kindPatternGrant("substrate.reamde.dev/llm/*")).toBe(
      "Every kind in the package substrate.reamde.dev/llm"
    )
  })

  it("names the repository's own kinds as its own, under ~", () => {
    expect(kindPatternGrant("~/*")).toBe("Every kind of your own, under ~")
    expect(kindPatternGrant("~/tasks/*")).toBe(
      "Every kind in the package ~/tasks"
    )
  })
})

describe("isRecordId", () => {
  it("holds the record id alphabet: `*` is outside it", () => {
    expect(isRecordId("kq3v9x2m41pf")).toBe(true)
    expect(isRecordId("samples.substrate.reamde.dev/tasks/task")).toBe(true)
    expect(isRecordId("*")).toBe(false)
    expect(isRecordId("providers.substrate.reamde.dev/*")).toBe(false)
  })

  it("lets an id open with ~/, and only that way", () => {
    expect(isRecordId("~/tasks/task")).toBe(true)
    expect(isRecordId("~tasks")).toBe(false)
    expect(isRecordId("~")).toBe(false)
    expect(isRecordId("a~b")).toBe(true)
  })
})
