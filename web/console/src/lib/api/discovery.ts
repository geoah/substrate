/** Discovery (`GET /api`): what this deployment serves, answered without a
 * token and without opening a repository — which is what lets the DOOR read
 * it. The login and register pages ask it one thing: does this substrate
 * verify a second factor at all?
 *
 * A local substrate booted with `SUBSTRATE_INSECURE_DISABLE_TOTP` verifies no
 * code, so a console that kept demanding six digits would be asking for
 * something nothing checks — and refusing the sign-in itself when the digits
 * are not to hand. */

import { useEffect, useState } from "react"

import { request } from "./http"

export interface AuthPolicy {
  /** False only where the second factor is switched off. */
  totpRequired: boolean
}

interface DiscoveryDoc {
  auth?: Partial<AuthPolicy>
}

/** The STRICT shape, and what an unanswered or unreadable discovery falls back
 * to: a deployment that wants a code and a console that hid the field would
 * refuse every sign-in, while the reverse merely asks for a digit nobody
 * reads. */
export const STRICT_AUTH_POLICY: AuthPolicy = { totpRequired: true }

/** The IN-FLIGHT request, and only that: doors mounting together share one
 * call, and a door mounting later asks again.
 *
 * The answer is not a constant a tab may keep. A dev substrate is restarted
 * from `mise run dev` to `mise run dev:totp` and back, and a console holding
 * the previous answer would then send no code to a door that wants one — an
 * account nobody can sign into until they think to reload. One unauthenticated,
 * repository-free GET per mount is the cheaper side of that trade. */
let pending: Promise<AuthPolicy> | null = null

export function fetchAuthPolicy(): Promise<AuthPolicy> {
  pending ??= request<DiscoveryDoc>("GET", "/api", undefined, {
    anonymous: true,
  })
    .then((doc) => ({ totpRequired: doc?.auth?.totpRequired !== false }))
    // Unreachable discovery must not leave the door unrenderable: assume the
    // strict shape, which is the only safe reading of an answer nobody gave.
    .catch(() => STRICT_AUTH_POLICY)
    .finally(() => {
      pending = null
    })
  return pending
}

/** Drop the in-flight request, so the next caller starts a fresh one. Tests
 * use it to keep one case's answer out of the next. */
export function resetAuthPolicy(): void {
  pending = null
}

/** The policy as a hook. It renders STRICT until the answer lands, so a slow
 * discovery shows the code field rather than a form the substrate refuses. */
export function useAuthPolicy(): AuthPolicy {
  const [policy, setPolicy] = useState<AuthPolicy>(STRICT_AUTH_POLICY)
  useEffect(() => {
    let live = true
    void fetchAuthPolicy().then((next) => {
      if (live) setPolicy(next)
    })
    return () => {
      live = false
    }
  }, [])
  return policy
}
