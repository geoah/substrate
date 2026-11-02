/** Who publishes the app asking, in the approval page's words (decision
 * 0209). */

import type { AuthorizationRequest } from "@/lib/api/types"

/** The name the page heads with: the verified publisher's name for the app,
 * else the one the app claims, else where it runs. */
export function appTitle(request: AuthorizationRequest): string {
  const { app } = request
  if (app.verified) return app.publisher.appName || app.name || app.host
  return app.local ? "An app on this device" : app.host
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
