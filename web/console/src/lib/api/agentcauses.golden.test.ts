/** The console's half of the cause guard.
 *
 * `TurnCause` in agents.ts is written BY HAND to mirror the causes
 * internal/substrate/agent.go lists (`AgentCauses`). `agentcauses.golden.json`
 * is where the two meet: a Go test in internal/api writes the server's closed
 * set to it, sorted, and this test asserts the union names exactly those.
 *
 * A union is erased at runtime, so the test cannot list its members. The
 * `Record<TurnCause, true>` literal below will not COMPILE unless it has a
 * key for every member and no other, so `tsc` holds the literal to the union
 * and this holds the literal to the golden. `lib/turn-outcome.ts` keys its
 * sentences by the same union, so a cause added here without words there
 * does not compile either.
 *
 * When this fails, fix the union: the golden is the server's word. */

import { expect, it } from "vitest"

import golden from "./agentcauses.golden.json"
import type { TurnCause } from "./agents"

const causes: Record<TurnCause, true> = {
  allowance_spent: true,
  context_too_long: true,
  deadline: true,
  internal: true,
  interrupted: true,
  model_unavailable: true,
  provider_billing: true,
  provider_key_refused: true,
  provider_overloaded: true,
  provider_rate_limited: true,
  provider_rejected: true,
  provider_unavailable: true,
  requested: true,
  shutdown: true,
  tool_call_limit: true,
  turn_limit: true,
  write_failed: true,
}

it("TurnCause is exactly the server's closed set", () => {
  // The golden is non-empty, or the comparison below proves nothing.
  expect(golden.length).toBeGreaterThan(0)
  expect(Object.keys(causes).sort()).toEqual(golden)
})
