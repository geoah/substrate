/** The three-step input rule and its one refusal: bound, then `default`,
 * then the sole record; a binding that names nothing the kind holds is
 * `missing` and never silently replaced. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { inputStatus, resolveInput } from "./inputs"

const KIND = "providers.example.com/github/account"
const account = (id: string): SubstrateRecord => ({
  id,
  kind: KIND,
  properties: {},
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

describe("resolveInput", () => {
  it("takes the binding first", () => {
    const state = resolveInput(
      `${KIND}/work`,
      [account("default"), account("work")],
      KIND
    )
    expect(state.state).toBe("bound")
    expect("record" in state && state.record.id).toBe("work")
  })
  it("then the record called default, then the sole record", () => {
    expect(
      resolveInput(undefined, [account("a"), account("default")], KIND)
    ).toMatchObject({ state: "default" })
    expect(resolveInput(undefined, [account("only")], KIND)).toMatchObject({
      state: "sole",
    })
  })
  it("is ambiguous with two and none with zero", () => {
    expect(
      resolveInput(undefined, [account("a"), account("b")], KIND)
    ).toMatchObject({ state: "ambiguous" })
    expect(resolveInput(undefined, [], KIND)).toEqual({ state: "none" })
  })
  it("keeps a binding that no longer resolves as missing, never a fallback", () => {
    const gone = resolveInput(`${KIND}/gone`, [account("default")], KIND)
    expect(gone).toMatchObject({ state: "missing", binding: `${KIND}/gone` })
    const wrongKind = resolveInput(
      "ada.example.com/people/person/default",
      [account("default")],
      KIND
    )
    expect(wrongKind.state).toBe("missing")
    expect(inputStatus("me", gone)).toMatch(/no longer exists/)
  })
})
