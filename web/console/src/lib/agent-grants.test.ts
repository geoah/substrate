/** The grant preconditions, asked of a document. Each of the three is here
 * twice, unpaid and paid, because a hint that never clears is as wrong as one
 * that never fires, and `graphql` is here to stay silent: declaring it IS its
 * grant, so it must never produce a hint.
 *
 * The grants live under `permissions` (`writes` and `reads`), and the messages
 * say those paths, because a hint that names a key the loader no longer knows
 * sends somebody to edit the wrong line. */

import { describe, expect, it } from "vitest"

import {
  HOST_FUNCTION_GRAPHQL,
  HOST_FUNCTION_MUTATE,
  HOST_FUNCTION_PROPOSE,
  HOST_FUNCTION_QUERY,
  RECORD_PATCH_REQUEST_KIND,
  grantHints,
  hostToolsOf,
} from "./agent-grants"

const WIDGET = "crew.test.dev/widget"

function agent(over: Record<string, unknown>): Record<string, unknown> {
  return { authority: "crew.test.dev", model: "opus", ...over }
}

/** A `tools:` list, as an agent declares it: one entry per function named. */
function tools(...functions: string[]): Record<string, unknown> {
  return { tools: functions.map((named) => ({ function: named })) }
}

describe("hostToolsOf", () => {
  it("names the host functions a document's tools reach for, once each", () => {
    expect(
      hostToolsOf(
        agent({
          tools: [
            { function: HOST_FUNCTION_QUERY },
            { function: "crew.test.dev/summarize" },
            { function: HOST_FUNCTION_QUERY, name: "again" },
            { function: HOST_FUNCTION_PROPOSE },
          ],
        })
      )
    ).toEqual([HOST_FUNCTION_QUERY, HOST_FUNCTION_PROPOSE])
  })

  it("ignores a tools list that is not a list of entries", () => {
    expect(hostToolsOf(agent({ tools: "query" }))).toEqual([])
    expect(hostToolsOf(agent({ tools: [null, 7, {}] }))).toEqual([])
  })

  it("reads a tool entry spelled as a PATH, and one spelled short", () => {
    // A pointer's value is `<kind-identity>/<record-id>`, and the short form
    // somebody typed names the same function until the server rewrites it.
    const FN = "core.substrate.reamde.dev/function"
    expect(
      hostToolsOf(
        agent({
          tools: [
            { function: `${FN}/${HOST_FUNCTION_QUERY}` },
            { function: HOST_FUNCTION_PROPOSE },
          ],
        })
      )
    ).toEqual([HOST_FUNCTION_QUERY, HOST_FUNCTION_PROPOSE])
  })

  it("does not read the retired entry key as a tool", () => {
    // An entry named its function under `callable` before the rename. That is
    // a deleted entry key now, so a document still spelling it is one the
    // loader refuses; reading it here would grant a tool the substrate will
    // not admit.
    expect(
      hostToolsOf(agent({ tools: [{ callable: HOST_FUNCTION_QUERY }] }))
    ).toEqual([])
  })
})

describe("grantHints", () => {
  it("says nothing about an agent that names no host tool", () => {
    expect(grantHints(agent(tools("crew.test.dev/summarize")))).toEqual([])
    expect(grantHints(undefined)).toEqual([])
  })

  it("reads a grant spelled as PATHS, and one spelled short", () => {
    const K = "core.substrate.reamde.dev/kind"
    // Paid, both ways round: a grant's entries are pointers at kinds, and the
    // pin makes the path and the short form one value.
    expect(
      grantHints(
        agent({
          ...tools(HOST_FUNCTION_PROPOSE),
          permissions: { writes: [`${K}/${RECORD_PATCH_REQUEST_KIND}`] },
        })
      )
    ).toEqual([])
    expect(
      grantHints(
        agent({
          ...tools(HOST_FUNCTION_QUERY),
          permissions: { reads: { kinds: [`${K}/${WIDGET}`] } },
        })
      )
    ).toEqual([])
  })

  it("query needs the read grant, and an allowlist settles it", () => {
    const unpaid = grantHints(agent(tools(HOST_FUNCTION_QUERY)))
    expect(unpaid).toHaveLength(1)
    expect(unpaid[0].function).toBe(HOST_FUNCTION_QUERY)
    expect(unpaid[0].property).toBe("permissions.reads")
    // The message SAYS the path the loader's own refusal names.
    expect(unpaid[0].message).toContain("data.permissions.reads")

    expect(
      grantHints(
        agent({
          ...tools(HOST_FUNCTION_QUERY),
          permissions: { reads: { kinds: [WIDGET] } },
        })
      )
    ).toEqual([])
  })

  it("query is unpaid while the read grant names no kind at all", () => {
    // `permissions.reads.kinds` is required and non-empty wherever the grant
    // appears, so an empty allowlist is not a grant: it is a load error
    // waiting to happen.
    expect(
      grantHints(
        agent({
          ...tools(HOST_FUNCTION_QUERY),
          permissions: { reads: { kinds: [] } },
        })
      )
    ).toHaveLength(1)
  })

  it("is not fooled by the pre-permissions spelling of either grant", () => {
    // `emit` and `reads` sat bare on `data` before the regroup. A document
    // still spelling them that way is one the loader refuses, so the hints
    // stand rather than reading the old keys as a grant.
    expect(
      grantHints(
        agent({ ...tools(HOST_FUNCTION_QUERY), reads: { kinds: [WIDGET] } })
      )
    ).toHaveLength(1)
    expect(
      grantHints(
        agent({
          ...tools(HOST_FUNCTION_MUTATE),
          emit: [WIDGET],
        })
      )
    ).toHaveLength(1)
  })

  it("propose needs the request kind in the write grant, by name", () => {
    const unpaid = grantHints(
      agent({
        ...tools(HOST_FUNCTION_PROPOSE),
        permissions: { writes: [WIDGET] },
      })
    )
    expect(unpaid).toHaveLength(1)
    expect(unpaid[0].property).toBe("permissions.writes")
    expect(unpaid[0].message).toContain(RECORD_PATCH_REQUEST_KIND)
    expect(unpaid[0].message).toContain("data.permissions.writes")

    expect(
      grantHints(
        agent({
          ...tools(HOST_FUNCTION_PROPOSE),
          permissions: { writes: [WIDGET, RECORD_PATCH_REQUEST_KIND] },
        })
      )
    ).toEqual([])
  })

  it("mutate needs a non-empty write grant, whatever it names", () => {
    const unpaid = grantHints(agent(tools(HOST_FUNCTION_MUTATE)))
    expect(unpaid).toHaveLength(1)
    expect(unpaid[0].property).toBe("permissions.writes")
    expect(unpaid[0].message).toContain("data.permissions.writes")

    expect(
      grantHints(
        agent({
          ...tools(HOST_FUNCTION_MUTATE),
          permissions: { writes: [WIDGET] },
        })
      )
    ).toEqual([])
  })

  it("graphql is its own grant and never earns a hint", () => {
    expect(grantHints(agent(tools(HOST_FUNCTION_GRAPHQL)))).toEqual([])
  })

  it("reports every unmet grant of a document, one per tool", () => {
    const hints = grantHints(
      agent(tools(HOST_FUNCTION_QUERY, HOST_FUNCTION_MUTATE))
    )
    expect(hints.map((h) => h.function)).toEqual([
      HOST_FUNCTION_QUERY,
      HOST_FUNCTION_MUTATE,
    ])
  })
})
