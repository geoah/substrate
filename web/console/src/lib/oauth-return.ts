/** The substrate's OAuth return page, when the consent tab cannot report to
 * the page that opened it, falls back to the console at
 * `/registry?connected=<account record path>` or `/registry?error=<correlation>`
 * (internal/api/bundles.go). These read that result and say where it goes. */

import { splitRecordPath } from "@/lib/record-path"

/** The two keys the return page may set, as a route's search. */
export interface OAuthReturnSearch {
  connected?: string
  error?: string
}

/** Read the two keys off a parsed search. A router may parse a digit-only
 * correlation as a number, so any scalar is taken as its text. */
export function oauthReturnSearch(
  search: Record<string, unknown>
): OAuthReturnSearch {
  const text = (v: unknown) =>
    (typeof v === "string" || typeof v === "number") && String(v)
      ? String(v)
      : undefined
  return { connected: text(search.connected), error: text(search.error) }
}

/** Where an OAuth return landing on `/registry` goes: the provider page of
 * the account it connected, which the account's kind names (an account kind
 * lives in its provider's package), naming the account so the page scrolls to
 * it; else the Providers list, the result carried along either way. */
export function oauthReturnRedirect(search: Record<string, unknown>):
  | {
      to: "/providers/$authority/$pkg"
      params: { authority: string; pkg: string }
      search: OAuthReturnSearch & { account: string }
    }
  | { to: "/providers"; search: OAuthReturnSearch } {
  const result = oauthReturnSearch(search)
  const path = result.connected ? splitRecordPath(result.connected) : undefined
  if (path) {
    const [authority, pkg] = path.kind.split("/")
    return {
      to: "/providers/$authority/$pkg",
      params: { authority, pkg },
      search: { ...result, account: path.id },
    }
  }
  const out: OAuthReturnSearch = {}
  if (result.connected) out.connected = result.connected
  if (result.error) out.error = result.error
  return { to: "/providers", search: out }
}
