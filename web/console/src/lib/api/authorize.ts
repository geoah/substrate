/** An app's authorization request, as the owner answers it (decision 0194).
 * The server parks a request at `GET /oauth/authorize` and sends the browser
 * to the console's `/authorize/{id}`; these read the parked request and answer
 * it under the owner's own bearer. Both answers carry the URL the page
 * navigates to: the app's redirect URI with the code, or with the refusal. */

import { request, rootPath } from "./http"
import type {
  AuthorizationAnswer,
  AuthorizationApproval,
  AuthorizationRequest,
} from "./types"

function authorizationPath(id: string): string {
  return rootPath("authorizations", id)
}

/** The parked request: the app, where the code goes, the repository and the
 * scopes resolved against it. Reading it is when the server fetches the app's
 * own document. */
export function getAuthorization(
  id: string,
  signal?: AbortSignal
): Promise<AuthorizationRequest> {
  return request<AuthorizationRequest>(
    "GET",
    authorizationPath(id),
    undefined,
    { signal }
  )
}

/** Approve the checked scopes for one of the offered lifetimes. */
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
