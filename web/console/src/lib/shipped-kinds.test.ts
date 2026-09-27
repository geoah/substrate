/** Every kind this repository ships, read from `kinds/` and `samples/`, held
 * to what the console makes of it: a display name built from known words,
 * and an everyday line that is the declaration's own first sentence. A new
 * shipped kind that splits badly, or whose description opens with API
 * notes, fails here rather than on a reader's screen. */

import { parseAllDocuments } from "yaml"
import { describe, expect, it } from "vitest"

import { everydayDescription, firstSentence, plainSentence } from "./kind-copy"
import type { KindInfo, KindLabel } from "@/lib/api/types"

import { displayName, displayPlural, splitWords } from "./kind-names"

const files = import.meta.glob<string>(
  ["../../../../kinds/**/*.yaml", "../../../../samples/**/*.yaml"],
  { query: "?raw", import: "default", eager: true }
)

interface Shipped {
  identity: string
  singular: string
  description: string
  label?: KindLabel
}

function shipped(): Shipped[] {
  const out: Shipped[] = []
  for (const source of Object.values(files)) {
    for (const doc of parseAllDocuments(source)) {
      const d = doc.toJS() as {
        kind?: string
        metadata?: { id?: string }
        data?: {
          description?: string
          names?: { singular?: string }
          label?: KindLabel
        }
      } | null
      if (d?.kind !== "substrate.reamde.dev/core/kind") continue
      out.push({
        identity: d.metadata?.id ?? "",
        singular: d.data?.names?.singular ?? "",
        description: d.data?.description ?? "",
        label: d.data?.label,
      })
    }
  }
  return out.sort((a, b) => a.identity.localeCompare(b.identity))
}

const KINDS = shipped()
const CORE = "substrate.reamde.dev/"

describe("shipped kinds", () => {
  it("are all read", () => {
    expect(KINDS.length).toBeGreaterThan(100)
    expect(KINDS.map((k) => k.identity)).toContain(
      "providers.substrate.reamde.dev/google/calendarevent"
    )
  })

  it.each(KINDS.map((k) => [k.identity, k.singular]))(
    "%s splits into known words",
    (_, singular) => {
      expect(splitWords(singular), displayName(singular)).toBeDefined()
    }
  )

  it.each(
    KINDS.filter((k) => k.label).map((k) => [k.identity, k.singular, k.label!])
  )("%s reads as its declared label", (identity, singular, label) => {
    const [authority, pkg, name] = identity.split("/")
    const entry: KindInfo = {
      identity,
      name,
      authority,
      package: pkg,
      version: 1,
      source: "published",
      description: "",
      label,
      definition: { names: { singular } },
    }
    expect(displayName(entry)).toBe(label.singular)
    expect(displayPlural(entry)).toBe(label.plural)
  })

  // Core is the substrate's own machinery and its descriptions are written
  // for the people building on it; every other shipped kind opens with a line
  // for the person whose records these are.
  it.each(
    KINDS.filter((k) => !k.identity.startsWith(CORE)).map((k) => [
      k.identity,
      k.description,
    ])
  )("%s opens with a plain sentence", (identity, description) => {
    const first = firstSentence(description)
    expect(plainSentence(first), first).toBe(true)
    expect(everydayDescription(identity, description)).toBe(first)
  })
})
