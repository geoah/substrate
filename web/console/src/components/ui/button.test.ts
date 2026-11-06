import { describe, expect, it } from "vitest"

import { buttonVariants } from "./button"

// A link styled as a button takes its classes from buttonVariants directly,
// without Button's merge. The base carries `border-transparent`; a variant
// that draws a border must replace it, not sit beside it, or the stylesheet's
// order decides and the outline disappears.
describe("buttonVariants", () => {
  it("leaves no transparent border beside the outline variant's", () => {
    const classes = buttonVariants({ variant: "outline", size: "sm" }).split(
      /\s+/
    )
    expect(classes).toContain("border-border")
    expect(classes).not.toContain("border-transparent")
  })

  it("keeps the transparent border where the variant draws none", () => {
    const classes = buttonVariants({ variant: "ghost" }).split(/\s+/)
    expect(classes).toContain("border-transparent")
  })
})
