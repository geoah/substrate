/** What a test reads off the mutation cache to say a typed secret left no
 * trace there: neither in any mutation's key, variables, result or error,
 * nor as a settled mutation kept for its gc time, whose `mutationFn` still
 * holds the form values it closed over. */

import type { QueryClient } from "@tanstack/react-query"
import { expect } from "vitest"

/** Everything the mutation cache holds, as text: where a typed secret would
 * show if any mutation kept it. */
export function mutationText(client: QueryClient): string {
  return JSON.stringify(
    client
      .getMutationCache()
      .getAll()
      .map((m) => ({ key: m.options.mutationKey, state: m.state }))
  )
}

/** Once every write has settled: nothing of `secret` in the cache, and no
 * mutation left in it at all. */
export function expectNoTrace(client: QueryClient, secret: string) {
  expect(mutationText(client)).not.toContain(secret)
  expect(client.getMutationCache().getAll()).toHaveLength(0)
}
