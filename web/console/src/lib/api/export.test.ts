import { describe, expect, it } from "vitest"

import { exportFileName } from "./export"

describe("exportFileName", () => {
  it("takes the name the server offered", () => {
    expect(
      exportFileName('attachment; filename="ada.example.com-553.tar"')
    ).toBe("ada.example.com-553.tar")
    expect(exportFileName("attachment; filename=ada.tar")).toBe("ada.tar")
  })
  it("falls back to a plain name", () => {
    expect(exportFileName(null)).toBe("substrate-export.tar")
  })
})
