/** An app's authorization request, as the owner answers it (decision 0194).
 * The server parks a request at `GET /oauth/authorize` and sends the browser
 * to the console's `/authorize/{id}`; these read the parked request and answer
 * it under the owner's own bearer. A denial, and an approval with nothing to
 * set up, carry the URL the page navigates to: the app's redirect URI with the
 * code, or with the refusal. An approval that sets something up answers
 * `202` with the state `setting-up` instead, and the page reads the request
 * until it is approved and carries `next` (docs/plans/app-setup.md). */

import { request, rootPath } from "./http"
import type {
  AuthorizationAnswer,
  AuthorizationApproval,
  AuthorizationRequest,
} from "./types"

/** How often the page reads a request while its set up runs. */
export const SETUP_POLL_MS = 1000

function authorizationPath(id: string): string {
  return rootPath("authorizations", id)
}

/** The request with its lists present: Go writes a nil slice as `null`, and
 * the page reads `details` and `setup` as lists whatever the request asked
 * for. */
function withLists(raw: AuthorizationRequest): AuthorizationRequest {
  return {
    ...raw,
    details: raw.details ?? [],
    setup: raw.setup ?? [],
    state: raw.state ?? "pending",
  }
}

/** The parked request: the app, where the code goes, the repository, the
 * scopes resolved against it and what approving would set up. Reading it is
 * when the server fetches the app's own document. */
export async function getAuthorization(
  id: string,
  signal?: AbortSignal
): Promise<AuthorizationRequest> {
  return withLists(
    await request<AuthorizationRequest>(
      "GET",
      authorizationPath(id),
      undefined,
      { signal }
    )
  )
}

/** Whether the request's set up is running, so the page reads it again. */
export function isSettingUp(
  request: AuthorizationRequest | undefined
): boolean {
  return request?.state === "setting-up"
}

/** Approve the checked scopes for one of the offered lifetimes, naming the
 * plan the page showed when there is setup. */
export function approveAuthorization(
  id: string,
  approval: AuthorizationApproval
): Promise<AuthorizationAnswer> {
  return request<AuthorizationAnswer>("POST", authorizationPath(id), approval)
}

/** Deny the request; the app hears `access_denied`. */
export function denyAuthorization(id: string): Promise<AuthorizationAnswer> {
  return request<AuthorizationAnswer>("DELETE", authorizationPath(id))
}
