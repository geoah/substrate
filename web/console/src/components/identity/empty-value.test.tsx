// @vitest-environment jsdom
/** One mark for "nothing here": the sheet reads an empty value the way the
 * grid does, and a screen reader hears a word rather than a dash. */

import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { EmptyValue } from "./empty-value"
import { DeclaredValue } from "@/components/property-sheet/property-value"
import { EMPTY_VALUE } from "@/lib/grid-values"
import type { PropSpec } from "@/lib/record-schema"

afterEach(cleanup)

const SPEC = {
  name: "location",
  label: "Location",
  kind: "string",
  required: false,
  repeated: false,
  keyed: false,
  managed: false,
} as PropSpec

describe("EmptyValue", () => {
  it("draws the one mark and says the word", () => {
    const { container } = render(<EmptyValue />)
    expect(container.querySelector("[aria-hidden]")?.textContent).toBe(
      EMPTY_VALUE
    )
    expect(container.querySelector(".sr-only")?.textContent).toBe("Empty")
  })

  it("is what the property sheet reads for a value nobody set", () => {
    for (const value of [undefined, null, ""]) {
      const { container } = render(<DeclaredValue spec={SPEC} value={value} />)
      expect(
        container.querySelector("[data-slot=empty-value] [aria-hidden]")
          ?.textContent
      ).toBe(EMPTY_VALUE)
      cleanup()
    }
  })

  it("gives way to words where the absence says more", () => {
    const { container } = render(<EmptyValue>Cleared</EmptyValue>)
    expect(container.textContent).toBe("Cleared")
  })
})
