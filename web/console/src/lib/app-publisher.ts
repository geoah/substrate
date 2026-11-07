/** Who publishes the app asking, in the approval page's words (decision
 * 0209). */

import type { AuthorizationRequest } from "@/lib/api/types"

/** The name the page leads with: the verified publisher's name for the app,
 * else the one the app claims. An unverified app is named by its host,
 * labelled as unverified, so the page never reads as a verified name: the
 * host is what its client id proves, and the name it claims is a line under
 * it. */
export function appTitle(request: AuthorizationRequest): string {
  const { app } = request
  if (app.verified) return app.publisher.appName || app.name || app.host
  return app.local ? "An app on this device" : `Unverified app at ${app.host}`
}

/** What the page's question calls the app: its verified name, else "this
 * app" or "this assistant", since a heading that repeated an unverified
 * name would read as a fact. */
export function appSubject(
  request: AuthorizationRequest,
  assistant: boolean
): string {
  if (request.app.verified) return appTitle(request)
  return assistant ? "this assistant" : "this app"
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
