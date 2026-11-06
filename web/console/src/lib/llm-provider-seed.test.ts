/** The console's copy of the seeded llm/provider rows is the shipped
 * sample's, field for field: Restore writes it back, and a row restored from
 * a stale copy would carry prices or an endpoint the seed no longer has. */

import { parseAllDocuments } from "yaml"
import { describe, expect, it } from "vitest"

import { LLM_PROVIDER_SEED } from "./llm-provider-seed"

const files = import.meta.glob<string>(
  "../../../../samples/llm/providers.yaml",
  { query: "?raw", import: "default", eager: true }
)

describe("LLM_PROVIDER_SEED", () => {
  it("is the shipped sample's rows, field for field", () => {
    const sources = Object.values(files)
    expect(sources).toHaveLength(1)
    const shipped: Record<string, unknown> = {}
    for (const doc of parseAllDocuments(sources[0])) {
      const d = doc.toJS() as {
        metadata: { id: string }
        data: { properties: Record<string, unknown> }
      }
      shipped[d.metadata.id] = d.data.properties
    }
    expect(LLM_PROVIDER_SEED).toEqual(shipped)
  })
})
