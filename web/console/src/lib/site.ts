/** The deployment's own site (`SUBSTRATE_SITE_DIR`): a name, a tagline, a
 * logo and a landing page an operator may set, read from the `site` block of
 * the discovery document. Every part is optional, and a substrate that sets
 * none looks exactly as one without this module: the doors keep the default
 * brand and a signed-out `/` goes to the sign-in. */

import { useEffect, useState } from "react"

import { fetchDiscoveryDoc } from "@/lib/api/discovery"

/** What `GET /.well-known/substrate/server.json` says under `site`. `logo` and
 * `landing` are paths on this server, such as `/site/logo.svg`. */
export interface SiteInfo {
  name: string
  tagline?: string
  logo?: string
  landing?: string
}

/** A path on this server and nothing else: the landing is mounted into the
 * console's own page and the logo becomes its icon, so a value that names
 * another origin (`//host`, `https://host`) is dropped rather than followed. */
function localPath(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  if (!value.startsWith("/") || value.startsWith("//")) return undefined
  if (value.includes("\\")) return undefined
  return value
}

function text(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

/** The block as the console may use it, or null where there is none. A site
 * without a name is no site: the name is what every surface shows. */
export function readSite(raw: unknown): SiteInfo | null {
  if (!raw || typeof raw !== "object") return null
  const r = raw as Record<string, unknown>
  const name = text(r.name)
  if (!name) return null
  const site: SiteInfo = { name }
  const tagline = text(r.tagline)
  const logo = localPath(r.logo)
  const landing = localPath(r.landing)
  if (tagline) site.tagline = tagline
  if (logo) site.logo = logo
  if (landing) site.landing = landing
  return site
}

/** The answer, once known. A site changes only when the server restarts with
 * another directory, so one answer per tab is enough, unlike the door's
 * policy, which a dev box flips between restarts. */
let known: SiteInfo | null | undefined
let loading: Promise<SiteInfo | null> | null = null
const listeners = new Set<(site: SiteInfo | null) => void>()

/** The site, fetched once per tab. An unreachable discovery reads as no site:
 * the default brand is always a correct page. */
export function loadSite(): Promise<SiteInfo | null> {
  if (known !== undefined) return Promise.resolve(known)
  loading ??= fetchDiscoveryDoc()
    .then((doc) => readSite(doc?.site))
    .catch(() => null)
    .then((site) => {
      known = site
      loading = null
      for (const listener of listeners) listener(site)
      return site
    })
  return loading
}

/** The site if it has already arrived, else undefined. */
export function peekSite(): SiteInfo | null | undefined {
  return known
}

/** Call `listener` when the answer arrives, without asking for it: a page
 * that only follows the site (the tab title) must not be the one that fetches
 * it. Returns the unsubscribe. */
export function onSite(listener: (site: SiteInfo | null) => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** Forget the answer, so the next caller fetches again. Tests use it. */
export function resetSite(): void {
  known = undefined
  loading = null
}

/** The site as a hook: undefined while the first answer is in flight, then
 * the site or null. */
export function useSite(): SiteInfo | null | undefined {
  const [site, setSite] = useState(known)
  useEffect(() => {
    let live = true
    void loadSite().then((next) => {
      if (live) setSite(next)
    })
    return () => {
      live = false
    }
  }, [])
  return site
}

/** A page's tab title, followed by the site's name where one is set. */
export function brandedTitle(title: string): string {
  const name = known?.name
  if (!name || title === name) return title
  return title ? `${title} · ${name}` : name
}

const ICON_TYPES: Record<string, string> = {
  svg: "image/svg+xml",
  png: "image/png",
  ico: "image/x-icon",
  webp: "image/webp",
  gif: "image/gif",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
}

/** Point the tab's icon at the site's logo and name the tab after the site
 * until a page names it. Nothing changes where no site is set. */
export function applySiteBranding(site: SiteInfo | null, doc = document): void {
  if (!site) return
  if (site.logo) {
    let link = doc.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!link) {
      link = doc.createElement("link")
      link.rel = "icon"
      doc.head.appendChild(link)
    }
    const ext = site.logo.split("?")[0].split(".").pop()?.toLowerCase() ?? ""
    const type = ICON_TYPES[ext]
    if (type) link.type = type
    else link.removeAttribute("type")
    link.href = site.logo
  }
  if (doc.title === "Substrate Console") doc.title = site.name
}
