/** What the shell needs from the build and cannot fetch: an origin whose
 * `connect-src` is `'none'` reads no manifest, so `vite.config.ts`
 * (`guestBuild`) writes the facts into `app-frame.html` as one JSON block,
 * `#guest-build`, and the shell reads it before anything else (`main.ts`).
 * Both import this file, so it names no DOM: the config compiles without
 * one.
 *
 * `imports` is the guest's import map: `react`, its two siblings,
 * `substrate/app` and `substrate/ui`, each the URL of a content-hashed entry
 * chunk in a build and a `/src/...` URL in dev. The hash is what lets the
 * server cache `/assets/` as immutable: a new build is new URLs, never a
 * changed body under an old name, so a shell and its SDK are always one
 * generation. `scripts` and `fonts` are the paths the guest may load, which
 * `pinPolicy` turns into a second policy beside the static one in the HTML:
 * in a build the exact files of the shell's and the five entries' import
 * closure and the font files the console's CSS emitted, in dev the prefixes
 * vite serves from. Policies intersect, so the static `'self'` and the pin
 * together admit a same-origin request for one of these paths and nothing
 * else: a `/api` or `/login` script request is not attemptable. */

import { RESERVED_MODULES } from "../lib/apps/spec"

export const GUEST_BUILD_ID = "guest-build"

export interface GuestBuild {
  imports: Record<string, string>
  scripts: string[]
  fonts: string[]
}

/** The block's text as a `GuestBuild`, or nothing for a block that is absent,
 * not JSON, or short an import map entry: a shell without the whole of it
 * mounts nothing. */
export function parseGuestBuild(
  text: string | null | undefined
): GuestBuild | undefined {
  if (!text) return undefined
  let raw: unknown
  try {
    raw = JSON.parse(text)
  } catch {
    return undefined
  }
  const o = raw as Partial<GuestBuild> | null
  if (!o || typeof o !== "object") return undefined
  const imports = o.imports
  if (!imports || typeof imports !== "object" || Array.isArray(imports)) {
    return undefined
  }
  const strings = (v: unknown): v is string[] =>
    Array.isArray(v) && v.every((x) => typeof x === "string")
  if (!RESERVED_MODULES.every((s) => typeof imports[s] === "string")) {
    return undefined
  }
  if (!strings(o.scripts) || !strings(o.fonts)) return undefined
  return {
    imports: { ...imports },
    scripts: [...o.scripts],
    fonts: [...o.fonts],
  }
}

/** The pin. CSP3 matches a host-source's path exactly, or as a prefix when
 * it ends in `/`, but only behind a host: a bare path is not a source
 * expression, and `*` with a path admits nothing in Chrome. The host is the
 * shell's own URL origin, which `location.origin` reports even where
 * `window.origin` is `null`. `'unsafe-inline'` and `blob:` stay for the
 * reasons the static policy gives; `'self'` is what the pin replaces. */
export function pinPolicy(build: GuestBuild, origin: string): string {
  const at = (paths: string[]) => paths.map((p) => origin + p)
  return [
    ["script-src", ...at(build.scripts), "'unsafe-inline'", "blob:"].join(" "),
    ["font-src", ...at(build.fonts)].join(" "),
  ].join("; ")
}
