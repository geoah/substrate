/** Where an applet is, in the address: `?screen=book&p.id=01JX…`. The screen is
 * its own key and each param is a `p.`-prefixed key, so a detail screen's
 * address reads as what it shows and Back walks the screens the reader
 * opened. The router writes each value in its own JSON-aware spelling, so a
 * param keeps its type (a numeric-looking id stays a string) through a
 * navigation and a reload. */

import { searchText } from "@/lib/search-params"

export interface AppletLocation {
  screen?: string
  params?: Record<string, unknown>
}

const PARAM = "p."

/** The keys an applet's route keeps: `screen` and every `p.<name>`. */
export function appletSearch(
  search: Record<string, unknown>
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  const screen = searchText(search.screen)
  if (screen) out.screen = screen
  for (const [key, value] of Object.entries(search)) {
    if (key.startsWith(PARAM) && key.length > PARAM.length && value != null)
      out[key] = value
  }
  return out
}

export function appletLocation(
  search: Record<string, unknown>
): AppletLocation {
  const kept = appletSearch(search)
  const params: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(kept))
    if (key.startsWith(PARAM)) params[key.slice(PARAM.length)] = value
  return {
    screen: typeof kept.screen === "string" ? kept.screen : undefined,
    params: Object.keys(params).length ? params : undefined,
  }
}

export function appletLocationSearch(
  location: AppletLocation
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  if (location.screen) out.screen = location.screen
  for (const [name, value] of Object.entries(location.params ?? {}))
    if (value != null) out[`${PARAM}${name}`] = value
  return out
}

/** One string per location, equal for the same screen and params whatever
 * order the params arrived in. */
export function appletLocationKey(location: AppletLocation): string {
  const params = Object.entries(location.params ?? {})
    .filter(([, v]) => v != null)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
  return JSON.stringify([location.screen ?? "", params])
}
