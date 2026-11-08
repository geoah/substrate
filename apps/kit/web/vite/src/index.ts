import { readFileSync } from "node:fs"
import { join, resolve } from "node:path"
import type { Plugin } from "vite"

/** What a build needs from the app's app.json. */
interface AppJSON {
  id: string
  name: string
  accent?: { light?: string; dark?: string }
}

export interface SiteOptions {
  /** The app's app.json, beside its web folder. */
  app: URL
  /** The name under the icon on a home screen: "Tasks". */
  shortName: string
  /** The build's version: `SUBSTRATE_WEB_VERSION`, else this, else the
   * version in the package.json of the Vite root, the app's web package. */
  version?: string
  /** The icons in `public/`, as the web manifest lists them. */
  icon?: string
  appleTouchIcon?: string
  /** The small icon a tab shows. */
  favicon?: string
}

/** Where a build is served, as a URL ending in `/`, such as
 * https://tasks.example.com/ or https://example.com/substrate/tasks/. It
 * names the deployment, so it comes from the environment, never from the
 * tree. Undefined, a build is served at / and signs in as a local app from
 * a loopback address. */
export function siteURL(
  value = process.env.SUBSTRATE_WEB_URL
): string | undefined {
  if (!value) return undefined
  if (!/^https:\/\/[^/]+\/(.*\/)?$/.test(value))
    throw new Error(`SUBSTRATE_WEB_URL is an https URL ending in /: ${value}`)
  return value
}

/** The client metadata document the server fetches at the client id
 * (decision 0194): the app's client id is `<url>client.json` and its one
 * redirect `<url>`, both on the origin it is served from. */
export function clientDocument(url: string, name: string, logo: string) {
  return {
    client_id: `${url}client.json`,
    client_name: name,
    client_uri: url,
    logo_uri: `${url}${logo}`,
    redirect_uris: [url],
    token_endpoint_auth_method: "none",
  }
}

/** The version in the package.json at `root`. */
export function packageVersion(root: string): string {
  const own = JSON.parse(readFileSync(join(root, "package.json"), "utf8")) as {
    version?: unknown
  }
  if (typeof own.version !== "string")
    throw new Error(`${join(root, "package.json")} names no version.`)
  return own.version
}

/** A Substrate web app's build: its base from `SUBSTRATE_WEB_URL`,
 * `__APP_VERSION__` defined, and the client metadata document and web
 * manifest written for the URL it is served at. */
export function substrateSite(opts: SiteOptions): Plugin {
  const url = siteURL()
  const app = JSON.parse(readFileSync(opts.app, "utf8")) as AppJSON
  const icon = opts.icon ?? "icon-512.png"
  const appleTouchIcon = opts.appleTouchIcon ?? "apple-touch-icon.png"
  const favicon = opts.favicon ?? "favicon.png"
  return {
    name: "substrate-site",
    config(user, env) {
      const version =
        process.env.SUBSTRATE_WEB_VERSION ??
        opts.version ??
        packageVersion(resolve(user.root ?? "."))
      return {
        base: url ? new URL(url).pathname : "/",
        define: { __APP_VERSION__: JSON.stringify(version) },
        server: env.command === "serve" ? { host: "127.0.0.1" } : undefined,
      }
    },
    // The links to what it writes and to the icons in public/, made here so
    // index.html names no file that is not beside it in the tree.
    transformIndexHtml() {
      return [
        {
          tag: "link",
          attrs: { rel: "icon", type: "image/png", href: `./${favicon}` },
          injectTo: "head",
        },
        {
          tag: "link",
          attrs: { rel: "apple-touch-icon", href: `./${appleTouchIcon}` },
          injectTo: "head",
        },
        {
          tag: "link",
          attrs: { rel: "manifest", href: "./manifest.webmanifest" },
          injectTo: "head",
        },
      ]
    },
    generateBundle() {
      if (url) {
        this.emitFile({
          type: "asset",
          fileName: "client.json",
          source:
            JSON.stringify(clientDocument(url, app.name, icon), null, 2) + "\n",
        })
      }
      this.emitFile({
        type: "asset",
        fileName: "manifest.webmanifest",
        source: JSON.stringify({
          name: app.name,
          short_name: opts.shortName,
          start_url: "./",
          scope: "./",
          display: "standalone",
          background_color: "#f2f2f7",
          theme_color: app.accent?.light,
          icons: [
            { src: icon, sizes: "512x512", type: "image/png" },
            { src: appleTouchIcon, sizes: "180x180", type: "image/png" },
          ],
        }),
      })
    },
  }
}
