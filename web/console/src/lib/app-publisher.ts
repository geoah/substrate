/** Who publishes the app asking, in the approval page's words (decision
 * 0209). */

import type { AuthorizationRequest } from "@/lib/api/types"

/** The name the page heads with: the verified publisher's name for the app,
 * else the one the app claims. An unverified app is headed by its host,
 * labelled as unverified, so the heading never reads as a verified name:
 * the host is what its client id proves, and the name it claims is a line
 * under it. */
export function appTitle(request: AuthorizationRequest): string {
  const { app } = request
  if (app.verified) return app.publisher.appName || app.name || app.host
  return app.local ? "An app on this device" : `Unverified app at ${app.host}`
}

/** Where the access goes, as the owner reads it. */
export function accessGoesTo(request: AuthorizationRequest): string {
  if (request.app.local || request.redirect.loopback)
    return "an app on this device"
  try {
    return new URL(request.redirect.uri).origin
  } catch {
    return request.redirect.host
  }
}
