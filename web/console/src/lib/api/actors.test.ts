/** The actor directory reads every actor record: an actor is never deleted, so
 * a repository that installed and removed enough code holds more than one
 * page of them, and History filters and names changes by the whole set. */

import { afterEach, describe, expect, it, vi } from "vitest"

import { actorIdentity } from "@/lib/actor-identity"
import { setActorDirectory } from "@/lib/actor-directory"
import { viewActors } from "@/lib/history"

import { ACTORS_PAGE, actorsQueryOptions } from "./actors"
import type { SubstrateRecord } from "./types"

function actorRecord(
  id: string,
  type: string,
  props: Record<string, unknown> = {}
): SubstrateRecord {
  return {
    kind: "substrate.reamde.dev/core/actor",
    id,
    properties: { type, name: id, ...props },
    labels: {},
    version: 1,
    createdAt: "2026-11-13T09:00:00Z",
    updatedAt: "2026-11-13T09:00:00Z",
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  setActorDirectory([])
})

describe("the actor directory", () => {
  it("walks every page, the disabled actors included", async () => {
    const first = Array.from({ length: ACTORS_PAGE }, (_, i) =>
      actorRecord(`fn-${i}`, "function", {
        function: {
          ref: `substrate.reamde.dev/core/function/x.example.com/p/f${i}`,
        },
      })
    )
    const second = [
      actorRecord("person-1", "person"),
      actorRecord("agent-late", "agent", {
        agent: { ref: "substrate.reamde.dev/core/agent/x.example.com/p/late" },
        disabled: true,
      }),
    ]
    const seen: string[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = new URL(String(input), "http://console.test")
        seen.push(url.searchParams.get("after") ?? "")
        const page =
          url.searchParams.get("after") === "c1"
            ? { records: second, head: 9, generation: "g" }
            : { records: first, cursor: "c1", head: 9, generation: "g" }
        return new Response(JSON.stringify(page), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        })
      })
    )

    const actors = await actorsQueryOptions.queryFn!({
      signal: new AbortController().signal,
    } as never)

    expect(seen).toEqual(["", "c1"])
    expect(actors).toHaveLength(ACTORS_PAGE + 2)
    setActorDirectory(actors)
    // History's views filter by the whole set...
    expect(viewActors("agents", actors)).toEqual(["agent-late"])
    expect(viewActors("you", actors)).toEqual(["person-1"])
    // ...and a change by an actor past the first page is named, not "An
    // actor".
    expect(actorIdentity("agent-late").name).not.toBe("An actor")
    expect(actorIdentity("agent-late").disabled).toBe(true)
    expect(actorIdentity("person-1").name).toBe("You")
  })
})
